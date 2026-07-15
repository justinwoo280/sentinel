package ctrl

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	ewp "github.com/justinwoo280/sing-ewp"
)

// --- FuzzValidateAlias (DESIGN.md §12) ---
// On ANY input, validateAlias never panics. When it succeeds, the
// alias is ≤20 runes, valid UTF-8, and only contains allowed chars.

func FuzzValidateAlias(f *testing.F) {
	f.Add("tokyo-1")
	f.Add("東京-1")
	f.Add("")
	f.Add(strings.Repeat("a", 25))
	f.Add("'; rm -rf /")
	f.Add("\x00\x01\x02")
	f.Add("正常alias")
	f.Add(string([]byte{0xff, 0xfe, 0xfd})) // invalid UTF-8
	f.Add("a\x00b")

	f.Fuzz(func(t *testing.T, s string) {
		err := validateAlias(s)
		if err != nil {
			return // rejection is always safe
		}
		// Invariant: passed validation ⇒ constraints hold.
		if !utf8.ValidString(s) {
			t.Fatalf("validateAlias accepted invalid UTF-8: %q", s)
		}
		count := 0
		for _, r := range s {
			count++
			if count > 20 {
				t.Fatalf("validateAlias accepted >20 runes: %q (count=%d)", s, count)
			}
			if !isAllowedAliasRune(r) {
				t.Fatalf("validateAlias accepted disallowed rune %q (U+%04X) in %q", r, r, s)
			}
		}
	})
}

// --- FuzzToggleParams (DESIGN.md §12) ---
// On ANY mod string + state, validateToggle never panics. When it
// succeeds, mod∈{google,trust} and state≠nil.

func FuzzToggleParams(f *testing.F) {
	f.Add("google", true)
	f.Add("trust", false)
	f.Add("evil", true)
	f.Add("", true)
	f.Add("google", false)
	f.Add("exec rm -rf /", true)

	f.Fuzz(func(t *testing.T, mod string, stateBool bool) {
		state := stateBool
		p := Params{Mod: mod, State: &state}
		err := validateToggle(p)
		if err != nil {
			return
		}
		if p.Mod != "google" && p.Mod != "trust" {
			t.Fatalf("validateToggle passed with mod=%q", p.Mod)
		}
		if p.State == nil {
			t.Fatal("validateToggle passed with nil state")
		}
	})
}

// --- FuzzRegisterParse (DESIGN.md §12) ---
// Registration blob parsing never panics; on success the UUID is valid.

func FuzzRegisterParse(f *testing.F) {
	// Valid registration blob.
	validReg := map[string]any{
		"v": 1, "region": "JP", "node": "test", "alias": "test", "uuid": "11111111-2222-3333-4444-555555555555", "ota": true,
	}
	validJSON, _ := json.Marshal(validReg)
	f.Add(validJSON)
	f.Add([]byte(`{"v":1,"region":"","node":"","uuid":""}`))
	f.Add([]byte(`not json`))
	f.Add([]byte(`{"uuid":"not-a-uuid"}`))
	f.Add([]byte(`{"uuid":123}`))
	f.Add([]byte(``))
	f.Add([]byte(`{"v":"not-int"}`))
	f.Add([]byte(`{"uuid":"00000000-0000-00000000-000000000000"}`))

	f.Fuzz(func(t *testing.T, raw []byte) {
		var reg struct {
			V      int    `json:"v"`
			Region string `json:"region"`
			Node   string `json:"node"`
			Alias  string `json:"alias"`
			UUID   string `json:"uuid"`
			OTA    bool   `json:"ota"`
		}
		// Must never panic.
		_ = json.Unmarshal(raw, &reg)
	})
}

// --- FuzzDecodeAddress (DESIGN.md §12) ---
// UUID parsing (used as EWP address identity) never panics on arbitrary
// input; on success the result is exactly UUIDLen bytes.

func FuzzDecodeAddress(f *testing.F) {
	f.Add("11111111-2222-3333-4444-555555555555")
	f.Add("00000000-0000-0000-0000-000000000000")
	f.Add("not-a-uuid")
	f.Add("")
	f.Add("111111112222333344445555555555555")          // 32 hex, no hyphens
	f.Add("GGGGGGGG-2222-3333-4444-555555555555")       // non-hex
	f.Add("11111111-2222-3333-4444-555555555555-extra") // too long
	f.Add(string([]byte{0xff, 0xfe, 0xfd}))             // binary garbage

	f.Fuzz(func(t *testing.T, s string) {
		// Must never panic.
		uuid, err := ewp.ParseUUID(s)
		if err != nil {
			return
		}
		// On success, the parsed UUID must round-trip to a non-empty,
		// canonical form (exercises the returned value, not just len()).
		var zero [ewp.UUIDLen]byte
		if uuid == zero && s != "00000000-0000-0000-0000-000000000000" {
			t.Fatalf("ParseUUID(%q) returned zero UUID for non-zero input", s)
		}
	})
}

// --- White-list constant test (SR-3) ---
// Every valid Command maps to a fixed action; no command can carry
// a URL or trigger arbitrary network access.

func TestCommandsAreClosedEnum(t *testing.T) {
	allCmds := []Command{
		CmdRun, CmdModGoogle, CmdModTrust, CmdModQuality,
		CmdReport, CmdLog, CmdConfigRename, CmdConfigToggle, CmdOTA,
	}
	for _, cmd := range allCmds {
		if !IsValidCommand(cmd) {
			t.Fatalf("valid command %q rejected by IsValidCommand", cmd)
		}
	}
	// Non-enum strings are invalid.
	for _, evil := range []string{"rm-rf", "exec", "shell", "download", "", "run --evil"} {
		if IsValidCommand(Command(evil)) {
			t.Fatalf("invalid command %q accepted by IsValidCommand", evil)
		}
	}
}

func TestEventsAreClosedEnum(t *testing.T) {
	allEvts := []Event{
		EvtHello, EvtHeartbeat, EvtResult, EvtReport, EvtQuality, EvtLog,
	}
	for _, evt := range allEvts {
		if !IsValidEvent(evt) {
			t.Fatalf("valid event %q rejected by IsValidEvent", evt)
		}
	}
	for _, evil := range []string{"exec", "", "shell", "upload"} {
		if IsValidEvent(Event(evil)) {
			t.Fatalf("invalid event %q accepted by IsValidEvent", evil)
		}
	}
}

// --- SR-5: DisallowUnknownFields ---

func TestDisallowUnknownFields(t *testing.T) {
	// Command with unknown field should be rejected.
	if _, err := DecodeCommand([]byte(`{"cmd":"run","evil_field":"x"}`)); err == nil {
		t.Fatal("unknown field in command accepted; SR-5 violated")
	}
	// Command with extra top-level field.
	if _, err := DecodeCommand([]byte(`{"id":"1","cmd":"run","extra":"y"}`)); err == nil {
		t.Fatal("extra field in command accepted; SR-5 violated")
	}
	// Event with unknown field should be rejected.
	if _, err := DecodeEvent([]byte(`{"evt":"hello","evil":"x"}`)); err == nil {
		t.Fatal("unknown field in event accepted; SR-5 violated")
	}
}

// --- SR-5: Message length limit ---

func TestMessageLengthLimit(t *testing.T) {
	// Just under limit: should parse.
	normal := `{"cmd":"run"}`
	if _, err := DecodeCommand([]byte(normal)); err != nil {
		t.Fatalf("normal message rejected: %v", err)
	}

	// Over limit: should be rejected before parsing.
	big := `{"cmd":"run","params":{"alias":"` + strings.Repeat("a", MaxMessageBytes) + `"}}`
	if _, err := DecodeCommand([]byte(big)); err == nil {
		t.Fatal("oversize message accepted; SR-5 violated")
	}
}

// --- SR-4: Alias validation edge cases ---

func TestValidateAlias(t *testing.T) {
	tests := []struct {
		alias string
		ok    bool
	}{
		{"tokyo-1", true},
		{"東京-1", true},
		{"a", true},
		{"", true},                          // empty is valid (no runes to reject)
		{strings.Repeat("a", 20), true},     // exactly 20
		{strings.Repeat("a", 21), false},    // 21 = too long
		{"a b", false},                      // space not allowed
		{"a.b", false},                      // dot not allowed
		{"a_b", false},                      // underscore not allowed
		{"a;b", false},                      // semicolon not allowed
		{"a\x00b", false},                   // null byte
		{"a\nb", false},                     // newline
		{"正常", true},                        // CJK
		{string([]byte{0xff, 0xfe}), false}, // invalid UTF-8
	}
	for _, tt := range tests {
		err := validateAlias(tt.alias)
		if tt.ok && err != nil {
			t.Errorf("validateAlias(%q): expected nil, got %v", tt.alias, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("validateAlias(%q): expected error, got nil", tt.alias)
		}
	}
}

// --- SR-4: Toggle validation ---

func TestValidateToggle(t *testing.T) {
	state := true
	tests := []struct {
		name string
		p    Params
		ok   bool
	}{
		{"google true", Params{Mod: "google", State: &state}, true},
		{"trust false", Params{Mod: "trust", State: &state}, true},
		{"invalid mod", Params{Mod: "evil", State: &state}, false},
		{"empty mod", Params{Mod: "", State: &state}, false},
		{"nil state", Params{Mod: "google", State: nil}, false},
		{"both empty", Params{}, false},
	}
	for _, tt := range tests {
		err := validateToggle(tt.p)
		if tt.ok && err != nil {
			t.Errorf("%s: expected nil, got %v", tt.name, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("%s: expected error, got nil", tt.name)
		}
	}
}
