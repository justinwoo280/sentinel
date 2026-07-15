// Package modules implements the Agent's keepalive modules.
package modules

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/justinwoo280/sentinel/internal/config"
	"github.com/justinwoo280/sentinel/internal/ctrl"
	"github.com/justinwoo280/sentinel/internal/geo"
	"github.com/justinwoo280/sentinel/internal/netx"
)

// GoogleModule performs simulated browsing of Google services to
// build the IP's geographic profile, then probes the current geo
// assignment via three independent methods (DESIGN.md §5.2).
type GoogleModule struct {
	cfg      config.AgentConfig
	log      *slog.Logger
	publicIP string
	city     *geo.CityRegion // resolved once at Agent startup; nil-safe (falls back)
}

// NewGoogle creates a GoogleModule. city is the resolved city-level region
// data (base_lat/base_lon/lang_params) for cfg.Region.Code/State/City,
// loaded once at Agent startup via geo.LoadCityRegion.
func NewGoogle(cfg config.AgentConfig, log *slog.Logger, publicIP string, city *geo.CityRegion) *GoogleModule {
	return &GoogleModule{cfg: cfg, log: log, publicIP: publicIP, city: city}
}

// probeUA is the clean, modern Chrome UA used for geo-detection probes
// (mirrors the original PROBE_UA constant in mod_google.sh).
const probeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// ytGLRe matches YouTube's geo fields in HTML.
var ytGLRe = regexp.MustCompile(`"(?:contentRegion|countryCode|INNERTUBE_CONTEXT_GL|GL)":"([A-Za-z]{2})"`)

// Run executes one Google keepalive session.
func (m *GoogleModule) Run(ctx context.Context) ctrl.Result {
	// 1. Load data.
	uas, err := geo.LoadUAs()
	if err != nil {
		return ctrl.Result{OK: false, Msg: "load UAs: " + err.Error()}
	}
	keywords, err := geo.LoadKeywords(m.cfg.Region.Code)
	if err != nil {
		return ctrl.Result{OK: false, Msg: "load keywords: " + err.Error()}
	}
	if len(keywords) == 0 || len(uas) == 0 {
		return ctrl.Result{OK: false, Msg: "empty UA pool or keyword pool"}
	}

	if m.city == nil {
		return ctrl.Result{OK: false, Msg: "no city region data loaded"}
	}

	// 2. Hash-seeded UA selection.
	seed := netx.HashSeed(m.publicIP)
	uaPool := netx.PickUAs(uas, seed)
	sessionUA := uaPool[rand.Intn(len(uaPool))]
	platform := netx.DetectPlatform(sessionUA)
	// langParams is the full query-string fragment from the city's
	// region JSON, e.g. "hl=ja&gl=JP" — used verbatim in URLs, matching
	// the original mod_google.sh's ${LANG_PARAMS} usage exactly.
	langParams := m.city.GoogleModule.LangParams

	// 3. Jittered coordinates (café-level location), from the city's
	// base coordinates (city-level precision, not just country-level).
	sessionLat := netx.JitterCoord(m.city.GoogleModule.BaseLat, 270)
	sessionLon := netx.JitterCoord(m.city.GoogleModule.BaseLon, 270)

	m.log.Info("google session starting",
		"platform", platform, "ip", m.publicIP,
		"ua", sessionUA[:min(40, len(sessionUA))]+"...",
		"coords", fmt.Sprintf("%.4f,%.4f", sessionLat, sessionLon))

	// 4. HTTP client with persistent cookie jar.
	cookiePath := fmt.Sprintf("/var/lib/sentinel/cookies/google_%d.txt", netx.HashSeed(m.publicIP))
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

	// 5. Browsing actions.
	totalActions := 5 + rand.Intn(4) // 5-8
	refs := &refererChain{}

	for i := 0; i < totalActions; i++ {
		if ctx.Err() != nil {
			return ctrl.Result{OK: false, Msg: "cancelled"}
		}

		kw := keywords[rand.Intn(len(keywords))]
		encoded := netx.EncodeQuery(kw)
		actionLat := netx.JitterCoord(sessionLat, 1)
		actionLon := netx.JitterCoord(sessionLon, 1)

		url, bizType := pickAction(platform, encoded, actionLat, actionLon, langParams)

		// 70% chance to carry same-business referer.
		ref := ""
		if rand.Intn(100) < 70 {
			ref = refs.get(bizType)
		}

		code := doRequest(client, url, sessionUA, ref)
		if code != "" {
			m.log.Debug("action done", "i", i+1, "total", totalActions,
				"type", bizType, "code", code)
			if strings.HasPrefix(code, "2") || strings.HasPrefix(code, "3") {
				refs.set(bizType, url)
			}
		} else {
			m.log.Warn("action failed", "i", i+1, "type", bizType)
			refs.clear(bizType)
		}

		if i < totalActions-1 {
			sleep := time.Duration(45+rand.Intn(31)) * time.Second
			select {
			case <-ctx.Done():
				return ctrl.Result{OK: false, Msg: "cancelled"}
			case <-time.After(sleep):
			}
		}
	}

	// 6. Three-probe geo detection.
	m.log.Info("running geo probes")
	probeClient, _ := netx.NewClient(netx.ClientConfig{
		BindIP:  m.cfg.Network.BindIP,
		IPPref:  m.cfg.Network.IPPref,
		Timeout: 15 * time.Second,
	})

	jumpGL := probeJumpRedirect(probeClient)
	ytPremGL := probeYouTube(probeClient, "https://www.youtube.com/premium")
	ytMusicGL := probeYouTube(probeClient, "https://music.youtube.com/")

	targetCC := m.cfg.Region.Code
	if targetCC == "UK" {
		targetCC = "GB"
	}

	status := geoVerdict(jumpGL, ytPremGL, ytMusicGL, targetCC)
	m.log.Info("geo probe result", "status", status,
		"jump", jumpGL, "prem", ytPremGL, "music", ytMusicGL,
		"target", targetCC)

	return ctrl.Result{OK: true, Msg: status}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type bizType string

const (
	bizSearch  bizType = "search"
	bizNews    bizType = "news"
	bizMaps    bizType = "maps"
	bizEco     bizType = "eco"
	bizNetTest bizType = "nettest"
)

type refererChain struct {
	search, news, maps, eco string
}

func (r *refererChain) get(t bizType) string {
	switch t {
	case bizSearch:
		return r.search
	case bizNews:
		return r.news
	case bizMaps:
		return r.maps
	case bizEco:
		return r.eco
	default:
		return ""
	}
}

func (r *refererChain) set(t bizType, url string) {
	switch t {
	case bizSearch:
		r.search = url
	case bizNews:
		r.news = url
	case bizMaps:
		r.maps = url
	case bizEco:
		r.eco = url
	}
}

func (r *refererChain) clear(t bizType) { r.set(t, "") }

// pickAction chooses a platform-specific action URL. langParams is the
// full query-string fragment from the city region data (e.g.
// "hl=ja&gl=JP"), inserted verbatim — matching the original
// mod_google.sh's ${LANG_PARAMS} usage exactly (both hl and gl are sent,
// not just hl).
func pickAction(platform netx.Platform, encKW string, lat, lon float64, langParams string) (string, bizType) {
	dice := rand.Intn(100)
	switch platform {
	case netx.PlatformAndroid:
		switch {
		case dice < 25:
			return fmt.Sprintf("https://www.google.com/search?q=%s&%s", encKW, langParams), bizSearch
		case dice < 55:
			return fmt.Sprintf("https://news.google.com/home?%s", langParams), bizNews
		case dice < 85:
			return fmt.Sprintf("https://www.google.com/maps/search/%s/@%.4f,%.4f,17z?%s", encKW, lat, lon, langParams), bizMaps
		default:
			return "https://connectivitycheck.gstatic.com/generate_204", bizNetTest
		}
	case netx.PlatformIOS, netx.PlatformMacOS:
		switch {
		case dice < 30:
			return fmt.Sprintf("https://www.google.com/search?q=%s&%s", encKW, langParams), bizSearch
		case dice < 65:
			return fmt.Sprintf("https://news.google.com/home?%s", langParams), bizNews
		case dice < 90:
			return fmt.Sprintf("https://www.google.com/maps/search/%s/@%.4f,%.4f,17z?%s", encKW, lat, lon, langParams), bizMaps
		default:
			return "https://captive.apple.com/hotspot-detect.html", bizNetTest
		}
	default: // Windows / Linux
		switch {
		case dice < 20:
			return fmt.Sprintf("https://www.google.com/search?q=%s&%s", encKW, langParams), bizSearch
		case dice < 60:
			return fmt.Sprintf("https://news.google.com/home?%s", langParams), bizNews
		case dice < 80:
			ecoURLs := []string{"https://about.google/", "https://safety.google/",
				fmt.Sprintf("https://support.google.com/?hl=%s", extractHL(langParams))}
			return ecoURLs[rand.Intn(len(ecoURLs))], bizEco
		default:
			return fmt.Sprintf("https://www.google.com/maps/search/%s/@%.4f,%.4f,17z?%s", encKW, lat, lon, langParams), bizMaps
		}
	}
}

// extractHL pulls the "hl" value out of a lang_params string like
// "hl=ja&gl=JP" → "ja". Returns "en" if not found (mirrors the original
// project's fallback; note the original's equivalent support.google.com
// URL actually referenced an undefined $LANG_ACCEPT variable and always
// sent an empty hl — we intentionally fix that rather than reproduce it).
func extractHL(langParams string) string {
	for _, part := range strings.Split(langParams, "&") {
		if v, ok := strings.CutPrefix(part, "hl="); ok && v != "" {
			return v
		}
	}
	return "en"
}

func doRequest(client *http.Client, url, ua, referer string) string {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", ua)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	resp.Body.Close()
	return resp.Status
}

// probeJumpRedirect checks www.google.com's Location header for geo.
func probeJumpRedirect(client *http.Client) string {
	req, _ := http.NewRequest("GET", "http://www.google.com/", nil)
	req.Header.Set("User-Agent", probeUA)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse // don't follow; read Location
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "US"
	}
	if strings.Contains(loc, ".google.cn") || strings.Contains(loc, "gl=CN") {
		return "CN"
	}
	if strings.Contains(loc, "gl=") {
		if m := regexp.MustCompile(`gl=([A-Za-z]{2})`).FindStringSubmatch(loc); len(m) > 1 {
			return strings.ToUpper(m[1])
		}
	}
	// Parse domain suffix.
	domainRe := regexp.MustCompile(`google\.([a-z.]+)`)
	if m := domainRe.FindStringSubmatch(loc); len(m) > 1 {
		return domainToCC(m[1])
	}
	return "US"
}

// probeYouTube extracts the geo code from a YouTube page.
func probeYouTube(client *http.Client, url string) string {
	client.CheckRedirect = nil // follow redirects for YT
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", probeUA)
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body := make([]byte, 512*1024) // read up to 512KB
	n, _ := resp.Body.Read(body)
	if strings.Contains(string(body[:n]), "www.google.cn") {
		return "CN"
	}
	if m := ytGLRe.FindSubmatch(body[:n]); len(m) > 1 {
		return strings.ToUpper(string(m[1]))
	}
	return ""
}

func domainToCC(domain string) string {
	switch domain {
	case "com":
		return "US"
	case "com.hk":
		return "HK"
	case "com.tw":
		return "TW"
	case "co.jp":
		return "JP"
	case "co.uk":
		return "GB"
	case "co.kr":
		return "KR"
	case "co.in":
		return "IN"
	case "co.id":
		return "ID"
	case "co.th":
		return "TH"
	case "com.sg":
		return "SG"
	case "com.my":
		return "MY"
	case "com.au":
		return "AU"
	case "com.br":
		return "BR"
	case "com.mx":
		return "MX"
	case "cn":
		return "CN"
	default:
		parts := strings.Split(domain, ".")
		last := parts[len(parts)-1]
		if len(last) == 2 {
			return strings.ToUpper(last)
		}
		return "US"
	}
}

func geoVerdict(jump, prem, music, targetCC string) string {
	isCN := jump == "CN" || prem == "CN" || music == "CN"
	if isCN {
		return fmt.Sprintf("ERROR: region flagged as CN (jump=%s prem=%s music=%s)", jump, prem, music)
	}
	ytMatch := prem == targetCC || music == targetCC
	if ytMatch {
		return fmt.Sprintf("OK: region matches (jump=%s prem=%s music=%s target=%s)", jump, prem, music, targetCC)
	}
	return fmt.Sprintf("DRIFT: target=%s actual jump=%s prem=%s music=%s", targetCC, jump, prem, music)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
