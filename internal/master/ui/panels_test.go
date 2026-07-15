package ui

import (
	"strings"
	"testing"

	"github.com/justinwoo280/sentinel/internal/master/store"
)

func TestMainMenuHasOTAButton(t *testing.T) {
	_, kb := MainMenu("1.0.0", 3)
	found := false
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData == CBOTA {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("main menu missing Global OTA button")
	}
}

func TestNodePanelHasOTAButton(t *testing.T) {
	n := &store.Node{UUID: "u1", NodeName: "n1", EnableOTA: true}
	_, kb := NodePanel(n, true)
	found := false
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if strings.HasPrefix(btn.CallbackData, CBOTA+"|") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("node panel missing OTA button")
	}
}

func TestConfirmOTA(t *testing.T) {
	text, kb := ConfirmOTA("u1", "tokyo-1")
	if !strings.Contains(text, "tokyo-1") {
		t.Errorf("confirm text missing node name: %q", text)
	}
	yes, cancel := false, false
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData == CBOTAYes+"|u1" {
				yes = true
			}
			if btn.CallbackData == CBNode+"|u1" {
				cancel = true
			}
		}
	}
	if !yes {
		t.Error("missing confirm (yes) button")
	}
	if !cancel {
		t.Error("missing cancel button")
	}
}

func TestConfirmGlobalOTA(t *testing.T) {
	text, kb := ConfirmGlobalOTA(5)
	if !strings.Contains(text, "5") {
		t.Errorf("global OTA text missing count: %q", text)
	}
	yes := false
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData == CBOTAYes {
				yes = true
			}
		}
	}
	if !yes {
		t.Error("missing global confirm button")
	}
}

func TestConfirmDelete(t *testing.T) {
	text, kb := ConfirmDelete("u1", "node-x")
	if !strings.Contains(text, "node-x") {
		t.Errorf("delete text missing node name: %q", text)
	}
	yes := false
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData == CBDeleteYes+"|u1" {
				yes = true
			}
		}
	}
	if !yes {
		t.Error("missing delete-yes button")
	}
}

func TestRegionFlag(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{"JP", "\U0001F1EF\U0001F1F5"},
		{"US", "\U0001F1FA\U0001F1F8"},
		{"UK", "\U0001F1EC\U0001F1E7"}, // UK → GB flag
		{"GB", "\U0001F1EC\U0001F1E7"},
		{"", ""},
		{"X", ""},
		{"123", ""},
		{"1A", ""},
	}
	for _, tt := range tests {
		got := regionFlag(tt.code)
		if got != tt.want {
			t.Errorf("regionFlag(%q) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestNodeListGroupsWithFlags(t *testing.T) {
	nodes := []store.Node{
		{UUID: "u1", NodeName: "n1", Region: "JP"},
		{UUID: "u2", NodeName: "n2", Region: "US"},
		{UUID: "u3", NodeName: "n3", Region: "JP"},
	}
	text, kb := NodeList(nodes)
	// Should contain flag emojis in the text.
	if !strings.Contains(text, "\U0001F1EF\U0001F1F5") {
		t.Error("node list text missing JP flag")
	}
	if !strings.Contains(text, "\U0001F1FA\U0001F1F8") {
		t.Error("node list text missing US flag")
	}
	// Should have 3 node buttons + 1 back button.
	btnCount := 0
	for _, row := range kb.InlineKeyboard {
		btnCount += len(row)
	}
	if btnCount != 4 {
		t.Errorf("expected 4 buttons (3 nodes + back), got %d", btnCount)
	}
}

func TestNodeListEmpty(t *testing.T) {
	text, _ := NodeList(nil)
	if !strings.Contains(text, "No registered nodes") {
		t.Errorf("empty node list should say so: %q", text)
	}
}
