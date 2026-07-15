package install

import (
	"path/filepath"
	"testing"
)

func TestGenerateNodeName(t *testing.T) {
	name := generateNodeName()
	if name == "" {
		t.Fatal("node name should not be empty")
	}
	// Should contain a dash and be lowercase.
	if len(name) < 3 {
		t.Fatalf("node name too short: %q", name)
	}
}

func TestBuildConfig(t *testing.T) {
	i := &Installer{
		region:      "JP",
		regionName:  "Japan",
		lat:         35.6762,
		lon:         139.6503,
		keywordFile: "kw_JP.txt",
		masterAddr:  "master.example.com:8443",
		masterPub:   "testpubkey",
		uuid:        "11111111-2222-3333-4444-555555555555",
		nodeName:    "test-node",
		alias:       "Test",
		google:      true,
		trust:       true,
		ota:         true,
	}

	cfg := i.buildConfig()
	if cfg.Node.Name != "test-node" {
		t.Fatalf("node name: got %q, want test-node", cfg.Node.Name)
	}
	if cfg.Region.Code != "JP" {
		t.Fatalf("region code: got %q, want JP", cfg.Region.Code)
	}
	if cfg.Master.Addr != "master.example.com:8443" {
		t.Fatalf("master addr: got %q, want master.example.com:8443", cfg.Master.Addr)
	}
	if cfg.Master.UUID != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("uuid mismatch")
	}
	if !cfg.Master.Enabled {
		t.Fatal("master should be enabled")
	}
}

func TestWriteConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")

	i := &Installer{
		region: "US", regionName: "United States",
		lat: 40.7, lon: -74.0, keywordFile: "kw_US.txt",
		masterAddr: "m:8443", masterPub: "key", uuid: "uuid",
		nodeName: "node", google: true, trust: true, ota: true,
	}
	cfg := i.buildConfig()

	if err := i.writeConfig(path, cfg); err != nil {
		t.Fatal(err)
	}

	// Verify file exists.
	if _, err := filepath.Glob(path); err != nil {
		t.Fatal(err)
	}
}
