package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestQualityAPIKeysRoundTrip(t *testing.T) {
	cfg := DefaultAgent()
	cfg.Node.Name = "n1"
	cfg.Region.Code = "JP"
	cfg.Quality.APIKeys = QualityAPIKeys{
		Scamalytics: "sa-key",
		AbuseIPDB:   "abuse-key",
		IP2Location: "ip2-key",
		IPQS:        "ipqs-key",
		IPData:      "ipdata-key",
		IPInfo:      "ipinfo-key",
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var loaded AgentConfig
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Quality.APIKeys != cfg.Quality.APIKeys {
		t.Fatalf("quality keys mismatch:\n got  %+v\n want %+v",
			loaded.Quality.APIKeys, cfg.Quality.APIKeys)
	}
	// Verify the yaml section is nested under quality.api_keys.
	if !contains(string(data), "api_keys:") || !contains(string(data), "scamalytics: sa-key") {
		t.Fatalf("unexpected yaml layout:\n%s", data)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func TestDurationMarshal(t *testing.T) {
	d := Duration(5 * time.Minute)
	got, err := d.MarshalYAML()
	if err != nil {
		t.Fatal(err)
	}
	if got != "5m0s" {
		t.Fatalf("got %v, want 5m0s", got)
	}
}

func TestDurationRoundTrip(t *testing.T) {
	original := Duration(5 * time.Minute)
	data, err := yaml.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Duration
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded != original {
		t.Fatalf("got %v, want %v", loaded, original)
	}
}

func TestAgentConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")

	cfg := DefaultAgent()
	cfg.Node.Name = "test-node"
	cfg.Node.Alias = "TestAlias"
	cfg.Region.Code = "JP"
	cfg.Region.Name = "Japan"
	cfg.Region.Lat = 35.6762
	cfg.Region.Lon = 139.6503
	cfg.Master.Enabled = false

	if err := SaveAgent(path, cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadAgent(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Node.Name != "test-node" {
		t.Fatalf("name: got %q, want test-node", loaded.Node.Name)
	}
	if loaded.Region.Code != "JP" {
		t.Fatalf("region: got %q, want JP", loaded.Region.Code)
	}
	if loaded.Modules.Google != true {
		t.Fatal("google should be true by default")
	}
	if loaded.Schedule.Interval != Duration(20*time.Minute) {
		t.Fatalf("interval: got %v, want 20m", loaded.Schedule.Interval)
	}
}

func TestAgentConfigValidate(t *testing.T) {
	// Missing node.name
	cfg := DefaultAgent()
	cfg.Region.Code = "US"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing node.name")
	}

	// Missing region.code
	cfg = DefaultAgent()
	cfg.Node.Name = "x"
	cfg.Region.Code = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing region.code")
	}

	// Master enabled but missing fields
	cfg = DefaultAgent()
	cfg.Node.Name = "x"
	cfg.Region.Code = "US"
	cfg.Master.Enabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for master enabled without addr/uuid/pub")
	}

	// Valid standalone
	cfg = DefaultAgent()
	cfg.Node.Name = "x"
	cfg.Region.Code = "US"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMasterConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "master.yaml")

	cfg := DefaultMaster()
	cfg.Telegram.Token = "test-token"
	cfg.Telegram.AdminIDs = []int64{111, 222}
	cfg.Control.Listen = ":9999"

	if err := SaveMaster(path, cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadMaster(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Telegram.Token != "test-token" {
		t.Fatalf("token: got %q, want test-token", loaded.Telegram.Token)
	}
	if loaded.Control.Listen != ":9999" {
		t.Fatalf("listen: got %q, want :9999", loaded.Control.Listen)
	}
	if len(loaded.Telegram.AdminIDs) != 2 || loaded.Telegram.AdminIDs[0] != 111 || loaded.Telegram.AdminIDs[1] != 222 {
		t.Fatalf("admin_ids: got %v, want [111 222]", loaded.Telegram.AdminIDs)
	}
}

func TestMasterConfigValidate(t *testing.T) {
	// Valid config.
	cfg := DefaultMaster()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default should be valid: %v", err)
	}

	// Missing listen.
	cfg = DefaultMaster()
	cfg.Control.Listen = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing listen")
	}

	// Missing static key path.
	cfg = DefaultMaster()
	cfg.Control.StaticKeyPath = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing static_key_path")
	}

	// Missing store path.
	cfg = DefaultMaster()
	cfg.Store.Path = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for missing store.path")
	}
}

func TestLoadAgentMissingFile(t *testing.T) {
	_, err := LoadAgent("/nonexistent/path/agent.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !os.IsNotExist(unwrapFileErr(err)) {
		t.Fatalf("expected os.IsNotExist, got %v", err)
	}
}

func unwrapFileErr(err error) error {
	for {
		if _, ok := err.(interface{ Unwrap() error }); !ok {
			return err
		}
		next := err.(interface{ Unwrap() error }).Unwrap()
		if next == nil {
			return err
		}
		err = next
	}
}
