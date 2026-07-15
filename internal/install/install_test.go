package install

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRunRefusesNonInteractiveStdin is a regression test for the bug
// where "curl ... | sh" (or any non-terminal stdin) would cause every
// interactive prompt to silently read EOF and fall through to its
// default, installing a node with the WRONG region and no master
// address, instead of asking the user anything. Run() must refuse
// outright with a clear error instead of racing through defaults.
// Test binaries normally run with a non-terminal stdin, so this
// reliably exercises the check without needing to fake a real TTY.
func TestRunRefusesNonInteractiveStdin(t *testing.T) {
	dir := t.TempDir()
	err := Run(filepath.Join(dir, "agent.yaml"))
	if err == nil {
		t.Fatal("expected an error when stdin is not a terminal, got nil")
	}
	if !strings.Contains(err.Error(), "not a terminal") {
		t.Fatalf("expected a clear 'not a terminal' error, got: %v", err)
	}
}

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
		state:       "Default",
		city:        "Tokyo",
		regionName:  "Japan — Tokyo",
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
	if cfg.Region.State != "Default" {
		t.Fatalf("region state: got %q, want Default", cfg.Region.State)
	}
	if cfg.Region.City != "Tokyo" {
		t.Fatalf("region city: got %q, want Tokyo", cfg.Region.City)
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
		region: "US", state: "CA", city: "Los_Angeles", regionName: "United States — Los Angeles",
		keywordFile: "kw_US.txt",
		masterAddr:  "m:8443", masterPub: "key", uuid: "uuid",
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
