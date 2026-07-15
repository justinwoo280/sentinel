// Package agent implements the Agent lifecycle: loads config, detects
// public IP, creates keepalive modules and scheduler, connects to
// Master via the EWP control channel, and implements ctrl.Executor
// to dispatch incoming commands.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/justinwoo280/sentinel/internal/agent/agentlog"
	"github.com/justinwoo280/sentinel/internal/agent/modules"
	"github.com/justinwoo280/sentinel/internal/agent/modules/quality"
	"github.com/justinwoo280/sentinel/internal/agent/ota"
	"github.com/justinwoo280/sentinel/internal/config"
	"github.com/justinwoo280/sentinel/internal/ctrl"
	"github.com/justinwoo280/sentinel/internal/geo"
	"github.com/justinwoo280/sentinel/internal/geoip"
	"github.com/justinwoo280/sentinel/internal/netx"
)

// Agent is the edge-node runtime. It owns the keepalive modules,
// scheduler, and control-channel client, and implements ctrl.Executor
// so the Master can dispatch commands to it.
type Agent struct {
	cfg       config.AgentConfig
	cfgPath   string
	log       *slog.Logger
	publicIP  string
	google    *modules.GoogleModule
	trust     *modules.TrustModule
	scheduler *Scheduler
	ctrl      *ctrl.Client
	logBuf    *agentlog.Buffer
	geoip     *geoip.Manager
	startTime time.Time
	mu        sync.Mutex
}

// New creates an Agent from a loaded config. It detects the public IP
// and initialises the keepalive modules and scheduler.
func New(cfg config.AgentConfig, cfgPath string, log *slog.Logger) (*Agent, error) {
	if log == nil {
		log = slog.Default()
	}

	publicIP, err := netx.DetectPublicIPChecked(cfg.Network.IPPref)
	if err != nil {
		if fake, _ := netx.CheckFakePublicIP(cfg.Network.BindIP); !fake {
			log.Warn("public IP detection failed, falling back to bind IP",
				"err", err, "bind_ip", cfg.Network.BindIP)
			publicIP = cfg.Network.BindIP
		} else {
			log.Error("fake public IP detected (WARP/TUN), refusing to start",
				"err", err)
			return nil, fmt.Errorf("agent: %w", err)
		}
	}
	if publicIP == "" {
		publicIP = "unknown"
	}

	// Capture every subsequent log record (from this point on, including
	// google/trust/quality/scheduler/ctrl) into a ring buffer so the
	// Master's `log` command has real content to show, not just the one
	// hardcoded "report generated" entry Report() used to push.
	logBuf := agentlog.New(500)
	log = slog.New(agentlog.NewHandler(log.Handler(), logBuf))

	a := &Agent{
		cfg:       cfg,
		cfgPath:   cfgPath,
		log:       log,
		publicIP:  publicIP,
		logBuf:    logBuf,
		startTime: time.Now(),
	}

	// Initialise GeoIP manager (downloads mmdb if not present).
	if cfg.GeoIP.Enabled {
		gm, err := geoip.NewManager(geoip.Config{
			Enabled:        cfg.GeoIP.Enabled,
			LicenseKey:     cfg.GeoIP.LicenseKey,
			DBPath:         cfg.GeoIP.DBPath,
			UpdateInterval: time.Duration(cfg.GeoIP.UpdateInterval),
			DownloadURL:    cfg.GeoIP.DownloadURL,
		}, log.With("module", "geoip"))
		if err != nil {
			log.Warn("geoip manager init failed", "err", err)
		}
		a.geoip = gm
	}

	// Resolve the selected city's region data once at startup (base
	// coordinates, lang_params, trust whitelist). This is the single
	// source of truth for geo data; the config only stores the three
	// short IDs (country/state/city), never the resolved values, so
	// there is nothing here to drift out of sync on disk.
	city, err := geo.LoadCityRegion(cfg.Region.Code, cfg.Region.State, cfg.Region.City)
	if err != nil {
		return nil, fmt.Errorf("agent: load city region %s/%s/%s: %w",
			cfg.Region.Code, cfg.Region.State, cfg.Region.City, err)
	}
	log.Info("region data loaded", "region_name", city.RegionName,
		"lat", city.GoogleModule.BaseLat, "lon", city.GoogleModule.BaseLon,
		"lang_params", city.GoogleModule.LangParams,
		"trust_urls", len(city.TrustModule.WhiteURLs))

	a.google = modules.NewGoogle(cfg, log.With("module", "google"), publicIP, city)
	a.trust = modules.NewTrust(cfg, log.With("module", "trust"), publicIP, city)
	a.scheduler = NewScheduler(cfg, a, log.With("module", "scheduler"))
	return a, nil
}

// PublicIP returns the detected public IP (for hello/registration).
func (a *Agent) PublicIP() string { return a.publicIP }

// Start launches the scheduler and control channel. Blocks until ctx
// is cancelled.
func (a *Agent) Start(ctx context.Context) error {
	// Start control channel if master is enabled.
	if a.cfg.Master.Enabled {
		hello := ctrl.HelloData{
			Node:    a.cfg.Node.Name,
			Alias:   a.cfg.Node.Alias,
			Region:  a.cfg.Region.Code,
			IP:      a.publicIP,
			Version: "dev",
			Google:  a.cfg.Modules.Google,
			Trust:   a.cfg.Modules.Trust,
		}
		cli, err := ctrl.NewClient(ctrl.ClientConfig{
			MasterAddr:   a.cfg.Master.Addr,
			UUID:         a.cfg.Master.UUID,
			ServerPubB64: a.cfg.Master.StaticPub,
			Heartbeat:    time.Duration(a.cfg.Reconnect.Heartbeat),
			MinBackoff:   time.Duration(a.cfg.Reconnect.MinBackoff),
			MaxBackoff:   time.Duration(a.cfg.Reconnect.MaxBackoff),
			Hello:        hello,
		}, a, a.log.With("module", "ctrl"))
		if err != nil {
			return fmt.Errorf("agent: create control client: %w", err)
		}
		a.ctrl = cli
		go func() {
			if err := cli.Run(ctx); err != nil && ctx.Err() == nil {
				a.log.Error("control channel stopped", "err", err)
			}
		}()
		a.log.Info("control channel started", "master", a.cfg.Master.Addr)
	} else {
		a.log.Info("running in standalone mode (no master connection)")
	}

	// Start GeoIP daily refresh loop.
	if a.geoip != nil {
		go a.geoip.StartRefreshLoop(ctx)
		a.log.Info("geoip refresh loop started",
			"interval", time.Duration(a.cfg.GeoIP.UpdateInterval))
	}

	// Start scheduler (blocks).
	a.scheduler.Run(ctx)
	// Context cancellation (SIGTERM) is a clean shutdown, not an error.
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// ctrl.Executor implementation
// ---------------------------------------------------------------------------

// Run triggers one keepalive cycle via the scheduler's probability wheel.
func (a *Agent) Run(ctx context.Context) ctrl.Result {
	return a.scheduler.RunOne(ctx)
}

func (a *Agent) ModGoogle(ctx context.Context) ctrl.Result {
	return a.google.Run(ctx)
}

func (a *Agent) ModTrust(ctx context.Context) ctrl.Result {
	return a.trust.Run(ctx)
}

func (a *Agent) ModQuality(ctx context.Context) ctrl.Result {
	keys := quality.APIKeys{
		Scamalytics: a.cfg.Quality.APIKeys.Scamalytics,
		AbuseIPDB:   a.cfg.Quality.APIKeys.AbuseIPDB,
		IP2Location: a.cfg.Quality.APIKeys.IP2Location,
		IPQS:        a.cfg.Quality.APIKeys.IPQS,
		IPData:      a.cfg.Quality.APIKeys.IPData,
		IPInfo:      a.cfg.Quality.APIKeys.IPInfo,
	}
	var q *quality.Quality
	if a.geoip != nil && a.geoip.IsAvailable() {
		q = quality.NewWithGeoIP(a.publicIP, a.cfg.Network.BindIP, a.cfg.Network.IPPref,
			"", keys, a.log, &geoipAdapter{gm: a.geoip})
	} else {
		q = quality.New(a.publicIP, a.cfg.Network.BindIP, a.cfg.Network.IPPref,
			"", keys, a.log)
	}
	result, _, err := q.Run(ctx)
	if err != nil {
		return ctrl.Result{OK: false, Msg: "quality: " + err.Error()}
	}
	return ctrl.Result{
		OK:   true,
		Msg:  "quality check completed",
		Data: json.RawMessage(result.JSON()),
	}
}

// geoipAdapter bridges geoip.Manager to quality.GeoIPLookup.
type geoipAdapter struct {
	gm *geoip.Manager
}

func (g *geoipAdapter) Lookup(ip string) *quality.GeoIPResult {
	r := g.gm.Lookup(ip)
	if r == nil {
		return nil
	}
	return &quality.GeoIPResult{
		CountryCode:   r.CountryCode,
		CountryName:   r.CountryName,
		CityName:      r.CityName,
		Latitude:      r.Latitude,
		Longitude:     r.Longitude,
		TimeZone:      r.TimeZone,
		Continent:     r.Continent,
		ContinentCode: r.ContinentCode,
		PostalCode:    r.PostalCode,
	}
}

func (a *Agent) Report(ctx context.Context) ctrl.Result {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()

	report := fmt.Sprintf("Node: %s\nAlias: %s\nRegion: %s (%s)\nIP: %s\nUptime: %s\nGoogle: %v\nTrust: %v\nOTA: %v\nModules: google=%v trust=%v",
		cfg.Node.Name,
		cfg.Node.Alias,
		cfg.Region.Code, cfg.Region.Name,
		a.publicIP,
		time.Since(a.startTime).Round(time.Second),
		cfg.Master.OTA,
		true, // running
		cfg.Master.OTA,
		cfg.Modules.Google, cfg.Modules.Trust,
	)

	a.log.Info("report generated", "module", "report")
	return ctrl.Result{
		OK:  true,
		Msg: report,
	}
}

func (a *Agent) Log(ctx context.Context) ctrl.Result {
	lines := a.logBuf.Format(50)
	if lines == "" {
		lines = "(no logs captured yet)"
	}
	return ctrl.Result{
		OK:  true,
		Msg: lines,
	}
}

func (a *Agent) ConfigRename(_ context.Context, alias string) ctrl.Result {
	a.mu.Lock()
	a.cfg.Node.Alias = alias
	cfg := a.cfg
	a.mu.Unlock()
	if err := config.SaveAgent(a.cfgPath, cfg); err != nil {
		return ctrl.Result{OK: false, Msg: "save config: " + err.Error()}
	}
	a.log.Info("alias updated", "alias", alias)
	return ctrl.Result{OK: true, Msg: "alias updated to " + alias}
}

func (a *Agent) ConfigToggle(_ context.Context, mod string, state bool) ctrl.Result {
	a.mu.Lock()
	switch mod {
	case "google":
		a.cfg.Modules.Google = state
	case "trust":
		a.cfg.Modules.Trust = state
	default:
		a.mu.Unlock()
		return ctrl.Result{OK: false, Msg: "unknown module: " + mod}
	}
	cfg := a.cfg
	a.mu.Unlock()
	if err := config.SaveAgent(a.cfgPath, cfg); err != nil {
		return ctrl.Result{OK: false, Msg: "save config: " + err.Error()}
	}
	a.scheduler.cfg = cfg.Modules
	a.log.Info("module toggled", "mod", mod, "state", state)
	return ctrl.Result{OK: true, Msg: fmt.Sprintf("module %s set to %v", mod, state)}
}

func (a *Agent) OTA(ctx context.Context) ctrl.Result {
	if !a.cfg.Master.OTA {
		return ctrl.Result{OK: false, Msg: "OTA not enabled on this agent"}
	}
	// OTA download URL comes from the master via a dedicated OTA
	// payload. The standard CmdOTA returns a gate check (SR-6);
	// PerformOTA handles the actual download+replace.
	return ctrl.Result{OK: false, Msg: "OTA: download URL not provided in command payload"}
}

// PerformOTA performs the actual OTA update. This is called when the
// master sends an OTA command with a download URL and optional hash.
func (a *Agent) PerformOTA(ctx context.Context, url, sha256 string) ctrl.Result {
	if !a.cfg.Master.OTA {
		return ctrl.Result{OK: false, Msg: "OTA not enabled on this agent"}
	}
	a.log.Info("starting OTA update", "url", url)
	result, err := ota.Download(ctx, ota.UpdateConfig{
		URL:    url,
		SHA256: sha256,
	})
	if err != nil {
		a.log.Error("OTA failed", "err", err)
		return ctrl.Result{OK: false, Msg: "OTA: " + err.Error()}
	}
	a.log.Info("OTA update completed", "msg", result.Msg)
	// Signal restart via systemd (exit 0 → systemd restarts with new binary).
	go func() {
		a.log.Info("restarting after OTA update")
		_ = ota.Restart() // never returns (calls os.Exit)
	}()
	return ctrl.Result{OK: true, Msg: result.Msg}
}
