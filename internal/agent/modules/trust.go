package modules

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"time"

	"github.com/justinwoo280/sentinel/internal/config"
	"github.com/justinwoo280/sentinel/internal/ctrl"
	"github.com/justinwoo280/sentinel/internal/geo"
	"github.com/justinwoo280/sentinel/internal/netx"
)

// TrustModule visits high-reputation whitelist sites to accumulate
// visitor trust for the IP (DESIGN.md §5.3).
type TrustModule struct {
	cfg      config.AgentConfig
	log      *slog.Logger
	publicIP string
	city     *geo.CityRegion // resolved once at Agent startup; nil-safe (falls back)
}

// NewTrust creates a TrustModule. city is the resolved city-level region
// data (trust_module.white_urls) for cfg.Region.Code/State/City, loaded
// once at Agent startup via geo.LoadCityRegion.
func NewTrust(cfg config.AgentConfig, log *slog.Logger, publicIP string, city *geo.CityRegion) *TrustModule {
	return &TrustModule{cfg: cfg, log: log, publicIP: publicIP, city: city}
}

// fallbackURLs are used when region data has no whitelist.
var fallbackURLs = []string{
	"https://en.wikipedia.org/wiki/Special:Random",
	"https://www.apple.com/",
	"https://www.microsoft.com/",
}

func (m *TrustModule) Run(ctx context.Context) ctrl.Result {
	// 1. Load whitelist URLs (city-level, real regional sites).
	urls := fallbackURLs
	if m.city != nil && len(m.city.TrustModule.WhiteURLs) > 0 {
		urls = m.city.TrustModule.WhiteURLs
	}

	// 2. Hash-seeded UA.
	uas, err := geo.LoadUAs()
	if err != nil {
		return ctrl.Result{OK: false, Msg: "load UAs: " + err.Error()}
	}
	seed := netx.HashSeed(m.publicIP)
	uaPool := netx.PickUAs(uas, seed)
	sessionUA := uaPool[rand.Intn(len(uaPool))]

	m.log.Info("trust session starting",
		"ip", m.publicIP, "urls", len(urls),
		"ua", sessionUA[:min(40, len(sessionUA))]+"...")

	// 3. HTTP client with persistent cookie jar.
	cookiePath := fmt.Sprintf("/var/lib/sentinel/cookies/trust_%d.txt", netx.HashSeed(m.publicIP))
	client, err := netx.NewClient(netx.ClientConfig{
		BindIP:        m.cfg.Network.BindIP,
		IPPref:        m.cfg.Network.IPPref,
		Timeout:       20 * time.Second,
		CookieJarPath: cookiePath,
		CookieMaxAge:  14 * 24 * time.Hour,
	})
	if err != nil {
		return ctrl.Result{OK: false, Msg: "http client: " + err.Error()}
	}

	// 4. Visits with poisson sleep.
	steps := 3 + rand.Intn(4) // 3-6
	success := 0

	for i := 0; i < steps; i++ {
		if ctx.Err() != nil {
			return ctrl.Result{OK: false, Msg: "cancelled"}
		}

		target := urls[rand.Intn(len(urls))]
		code := doTrustVisit(client, target, sessionUA)
		if code != "" && (code[0] == '2' || code[0] == '3') {
			success++
			m.log.Debug("trust visit done", "i", i+1, "total", steps,
				"code", code, "url", target[:min(40, len(target))])
		} else {
			m.log.Warn("trust visit failed", "i", i+1,
				"code", code, "url", target[:min(40, len(target))])
		}

		if i < steps-1 {
			sleep := poissonSleep()
			select {
			case <-ctx.Done():
				return ctrl.Result{OK: false, Msg: "cancelled"}
			case <-time.After(sleep):
			}
		}
	}

	if success >= steps/2 {
		return ctrl.Result{OK: true,
			Msg: fmt.Sprintf("warmup complete (%d/%d)", success, steps)}
	}
	return ctrl.Result{OK: false,
		Msg: fmt.Sprintf("warmup partially blocked (%d/%d)", success, steps)}
}

func doTrustVisit(client *http.Client, url, ua string) string {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	resp.Body.Close()
	return resp.Status
}

// poissonSleep mirrors the original mod_trust.sh sleep distribution:
// 45%: 8-20s, 35%: 20-60s, 15%: 60-180s, 5%: 180-480s.
func poissonSleep() time.Duration {
	dice := rand.Intn(100)
	switch {
	case dice < 45:
		return time.Duration(8+rand.Intn(13)) * time.Second
	case dice < 80:
		return time.Duration(20+rand.Intn(41)) * time.Second
	case dice < 95:
		return time.Duration(60+rand.Intn(121)) * time.Second
	default:
		return time.Duration(180+rand.Intn(301)) * time.Second
	}
}
