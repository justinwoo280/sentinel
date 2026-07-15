package install

import (
	"os"
	"strings"
	"testing"

	ewp "github.com/justinwoo280/sing-ewp"
)

func TestServiceNameAndPaths(t *testing.T) {
	if serviceName(RoleAgent) != "sentinel-agent" {
		t.Errorf("agent service name wrong: %s", serviceName(RoleAgent))
	}
	if serviceName(RoleMaster) != "sentinel-master" {
		t.Errorf("master service name wrong: %s", serviceName(RoleMaster))
	}
	if unitPath(RoleAgent) != agentUnit {
		t.Errorf("agent unit path wrong: %s", unitPath(RoleAgent))
	}
	if unitPath(RoleMaster) != masterUnit {
		t.Errorf("master unit path wrong: %s", unitPath(RoleMaster))
	}
	if configPathFor(RoleAgent) != agentConfig {
		t.Errorf("agent config path wrong")
	}
	if configPathFor(RoleMaster) != masterConfig {
		t.Errorf("master config path wrong")
	}
}

func TestUnitTemplateAgent(t *testing.T) {
	u := unitTemplate(RoleAgent)
	checks := []string{
		"Description=Sentinel Agent",
		"/usr/local/bin/sentinel agent -c /etc/sentinel/agent.yaml",
		"Restart=always",
		"WantedBy=multi-user.target",
	}
	for _, c := range checks {
		if !strings.Contains(u, c) {
			t.Errorf("agent unit missing %q\n%s", c, u)
		}
	}
}

func TestUnitTemplateMaster(t *testing.T) {
	u := unitTemplate(RoleMaster)
	checks := []string{
		"Description=Sentinel Master",
		"/usr/local/bin/sentinel master -c /etc/sentinel/master.yaml",
	}
	for _, c := range checks {
		if !strings.Contains(u, c) {
			t.Errorf("master unit missing %q\n%s", c, u)
		}
	}
	// Master unit must NOT run as agent.
	if strings.Contains(u, "sentinel agent") {
		t.Error("master unit incorrectly references agent")
	}
}

func TestDerivePublicKeyFromFile(t *testing.T) {
	priv, pub, err := ewp.GenerateServerStaticKeypair()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	keyPath := dir + "/static.key"
	if err := os.WriteFile(keyPath, []byte(priv), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := DerivePublicKeyFromFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != pub {
		t.Fatalf("derived pub %q != generated %q", got, pub)
	}
}

func TestDerivePublicKeyFromFileMissing(t *testing.T) {
	if _, err := DerivePublicKeyFromFile("/nonexistent/key"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestStatusNoPanic(t *testing.T) {
	// Should not panic regardless of environment.
	_ = Status(RoleAgent)
	_ = Status(RoleMaster)
}

func TestRegistrationBlobRoundTrip(t *testing.T) {
	blob, err := RegistrationBlobFromParts("JP", "tokyo-1", "东京",
		"11111111-2222-3333-4444-555555555555", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(blob, "SENTINEL-REG:") {
		t.Fatalf("blob missing prefix: %s", blob)
	}
}
