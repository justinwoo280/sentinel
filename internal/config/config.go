// Package config loads and saves YAML configuration for the Agent
// and Master roles.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration for YAML (un)marshalling as a
// human-readable string ("20m", "1s", etc.).
type Duration time.Duration

func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(dur)
	return nil
}

// ---------------------------------------------------------------------------
// Agent config
// ---------------------------------------------------------------------------

type AgentConfig struct {
	Node      NodeConfig      `yaml:"node"`
	Region    RegionConfig    `yaml:"region"`
	Network   NetworkConfig   `yaml:"network"`
	Modules   ModulesConfig   `yaml:"modules"`
	Master    MasterConn      `yaml:"master"`
	Reconnect ReconnectConfig `yaml:"reconnect"`
	Schedule  ScheduleConfig  `yaml:"schedule"`
	GeoIP     GeoIPConfig     `yaml:"geoip"`
	Quality   QualityConfig   `yaml:"quality"`
}

type NodeConfig struct {
	Name  string `yaml:"name"`
	Alias string `yaml:"alias"`
}

// RegionConfig identifies the selected country/state/city (matching the
// original project's 4-level region hierarchy: continent -> country ->
// state -> city). Only the three short IDs are stored; the actual
// coordinates, lang_params, and trust whitelist are resolved at runtime
// from the embedded city JSON (internal/geo.LoadCityRegion) — this is the
// single source of truth, so it is never duplicated into config (avoiding
// stale/drifted data between the two).
type RegionConfig struct {
	Code        string `yaml:"code"`  // country ID, e.g. "DE"
	Name        string `yaml:"name"`  // display name, e.g. "Germany (德国) — Nuremberg (纽伦堡)"
	State       string `yaml:"state"` // state ID, e.g. "BY" (or "Default")
	City        string `yaml:"city"`  // city ID, e.g. "Nuremberg"
	KeywordFile string `yaml:"keyword_file"`
}

type NetworkConfig struct {
	BindIP string `yaml:"bind_ip"`
	IPPref int    `yaml:"ip_pref"` // 4 or 6
}

type ModulesConfig struct {
	Google bool `yaml:"google"`
	Trust  bool `yaml:"trust"`
}

type MasterConn struct {
	Enabled   bool   `yaml:"enabled"`
	Addr      string `yaml:"addr"`
	StaticPub string `yaml:"static_pub"`
	UUID      string `yaml:"uuid"`
	OTA       bool   `yaml:"ota"`
}

type ReconnectConfig struct {
	MinBackoff Duration `yaml:"min_backoff"`
	MaxBackoff Duration `yaml:"max_backoff"`
	Heartbeat  Duration `yaml:"heartbeat"`
}

type ScheduleConfig struct {
	Interval Duration `yaml:"interval"`
	Jitter   Duration `yaml:"jitter"`
}

// GeoIPConfig controls Maxmind mmdb download and updates.
type GeoIPConfig struct {
	Enabled        bool     `yaml:"enabled"`
	LicenseKey     string   `yaml:"license_key"`
	DBPath         string   `yaml:"db_path"`
	UpdateInterval Duration `yaml:"update_interval"`
	DownloadURL    string   `yaml:"download_url"`
}

// QualityConfig holds settings for the IP quality-check module.
type QualityConfig struct {
	// APIKeys are optional keys for commercial data sources. Any source
	// left blank is skipped and degrades gracefully (its fields become
	// null in the report). All are optional; the free sources always run.
	APIKeys QualityAPIKeys `yaml:"api_keys"`
}

// QualityAPIKeys mirrors the quality module's APIKeys struct. It is
// defined here (rather than importing the quality package) to keep the
// config package free of agent-module dependencies. Field order/tags
// must stay in sync with quality.APIKeys.
type QualityAPIKeys struct {
	Scamalytics string `yaml:"scamalytics"`
	AbuseIPDB   string `yaml:"abuseipdb"`
	IP2Location string `yaml:"ip2location"`
	IPQS        string `yaml:"ipqs"`
	IPData      string `yaml:"ipdata"`
	IPInfo      string `yaml:"ipinfo"`
}

// DefaultAgent returns an agent config with sensible defaults applied.
func DefaultAgent() AgentConfig {
	return AgentConfig{
		Network: NetworkConfig{IPPref: 4},
		Modules: ModulesConfig{Google: true, Trust: true},
		Reconnect: ReconnectConfig{
			MinBackoff: Duration(1 * time.Second),
			MaxBackoff: Duration(60 * time.Second),
			Heartbeat:  Duration(30 * time.Second),
		},
		Schedule: ScheduleConfig{
			Interval: Duration(20 * time.Minute),
			Jitter:   Duration(180 * time.Second),
		},
		GeoIP: GeoIPConfig{
			Enabled:        true,
			DBPath:         "/var/lib/sentinel/GeoLite2-City.mmdb",
			UpdateInterval: Duration(24 * time.Hour),
		},
	}
}

// LoadAgent reads and parses an agent YAML file.
func LoadAgent(path string) (AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AgentConfig{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	cfg := DefaultAgent()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return AgentConfig{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return AgentConfig{}, err
	}
	return cfg, nil
}

// Validate checks required fields.
func (c AgentConfig) Validate() error {
	if c.Node.Name == "" {
		return errors.New("config: node.name is required")
	}
	if c.Region.Code == "" {
		return errors.New("config: region.code is required")
	}
	if c.Region.State == "" {
		return errors.New("config: region.state is required")
	}
	if c.Region.City == "" {
		return errors.New("config: region.city is required")
	}
	if c.Master.Enabled {
		if c.Master.Addr == "" {
			return errors.New("config: master.addr is required when master.enabled")
		}
		if c.Master.UUID == "" {
			return errors.New("config: master.uuid is required when master.enabled")
		}
		if c.Master.StaticPub == "" {
			return errors.New("config: master.static_pub is required when master.enabled")
		}
	}
	return nil
}

// SaveAgent writes the config to a YAML file.
func SaveAgent(path string, cfg AgentConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// ---------------------------------------------------------------------------
// Master config
// ---------------------------------------------------------------------------

type MasterConfig struct {
	Master   MasterNodeConfig `yaml:"master"`
	Telegram TelegramConfig   `yaml:"telegram"`
	Store    StoreConfig      `yaml:"store"`
	Control  ControlConfig    `yaml:"control"`
}

type MasterNodeConfig struct {
	NodeName string `yaml:"node_name"`
	Version  string `yaml:"version"`
}

type TelegramConfig struct {
	Token     string `yaml:"token"`
	EnableOTA bool   `yaml:"enable_ota"`
	// AdminIDs is the allowlist of Telegram user IDs permitted to control
	// the bot. If empty, the bot is fail-closed (denies everyone) — you
	// MUST add at least one admin ID before the bot is usable.
	AdminIDs []int64 `yaml:"admin_ids"`
}

type StoreConfig struct {
	Path string `yaml:"path"`
}

type ControlConfig struct {
	Listen        string `yaml:"listen"`
	StaticKeyPath string `yaml:"static_key_path"`
}

func DefaultMaster() MasterConfig {
	return MasterConfig{
		Master:  MasterNodeConfig{Version: "dev"},
		Store:   StoreConfig{Path: "/var/lib/sentinel/master.db"},
		Control: ControlConfig{Listen: ":8443", StaticKeyPath: "/var/lib/sentinel/master_static.key"},
	}
}

func LoadMaster(path string) (MasterConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return MasterConfig{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	cfg := DefaultMaster()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return MasterConfig{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return MasterConfig{}, err
	}
	return cfg, nil
}

// Validate checks required master config fields.
func (c MasterConfig) Validate() error {
	if c.Control.Listen == "" {
		return errors.New("config: control.listen is required")
	}
	if c.Control.StaticKeyPath == "" {
		return errors.New("config: control.static_key_path is required")
	}
	if c.Store.Path == "" {
		return errors.New("config: store.path is required")
	}
	return nil
}

// SaveMaster writes the master config to a YAML file.
func SaveMaster(path string, cfg MasterConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}
