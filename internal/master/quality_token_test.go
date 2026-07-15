package master

import (
	"log/slog"
	"testing"
	"time"

	"github.com/justinwoo280/sentinel/internal/ctrl"
	"github.com/justinwoo280/sentinel/internal/master/ui"
)

func newTestMaster() *Master {
	return &Master{
		log:           slog.Default(),
		qualityTokens: make(map[string]*qualityToken),
		pendingRename: make(map[int64]string),
		admins:        make(map[int64]bool),
	}
}

func TestGenQualityTokenUnique(t *testing.T) {
	m := newTestMaster()
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		tok := m.genQualityToken()
		if tok == "" {
			t.Fatal("empty token")
		}
		if seen[tok] {
			t.Fatalf("duplicate token: %q", tok)
		}
		seen[tok] = true
	}
}

func TestCleanupQualityTokens(t *testing.T) {
	m := newTestMaster()
	// Fresh token — should survive.
	m.qualityTokens["fresh"] = &qualityToken{CreatedAt: time.Now()}
	// Old token — should be purged.
	m.qualityTokens["old"] = &qualityToken{CreatedAt: time.Now().Add(-20 * time.Minute)}

	m.cleanupQualityTokens()

	if _, ok := m.qualityTokens["fresh"]; !ok {
		t.Error("fresh token was incorrectly purged")
	}
	if _, ok := m.qualityTokens["old"]; ok {
		t.Error("old token was not purged")
	}
}

func TestActionToCommand(t *testing.T) {
	tests := []struct {
		action string
		want   ctrl.Command
		ok     bool
	}{
		{ui.CBRun, ctrl.CmdRun, true},
		{ui.CBGoogle, ctrl.CmdModGoogle, true},
		{ui.CBTrust, ctrl.CmdModTrust, true},
		{ui.CBQuality, ctrl.CmdModQuality, true},
		{ui.CBReport, ctrl.CmdReport, true},
		{ui.CBLog, ctrl.CmdLog, true},
		{"bogus", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, err := actionToCommand(tt.action)
		if tt.ok && err != nil {
			t.Errorf("actionToCommand(%q): unexpected error %v", tt.action, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("actionToCommand(%q): expected error, got nil", tt.action)
		}
		if tt.ok && got != tt.want {
			t.Errorf("actionToCommand(%q) = %q, want %q", tt.action, got, tt.want)
		}
	}
}

func TestTruncStr(t *testing.T) {
	if got := truncStr("hello", 3); got != "hel" {
		t.Errorf("truncStr = %q, want hel", got)
	}
	if got := truncStr("hi", 5); got != "hi" {
		t.Errorf("truncStr = %q, want hi", got)
	}
}

func TestPtrStrOr(t *testing.T) {
	s := "value"
	if got := ptrStrOr(&s, "fb"); got != "value" {
		t.Errorf("ptrStrOr(&s) = %q, want value", got)
	}
	if got := ptrStrOr(nil, "fb"); got != "fb" {
		t.Errorf("ptrStrOr(nil) = %q, want fb", got)
	}
	empty := ""
	if got := ptrStrOr(&empty, "fb"); got != "fb" {
		t.Errorf("ptrStrOr(&empty) = %q, want fb", got)
	}
}
