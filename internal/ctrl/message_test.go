package ctrl

import (
	"strings"
	"testing"
)

func TestDecodeCommand_AllowList(t *testing.T) {
	// Valid command decodes.
	if _, err := DecodeCommand([]byte(`{"id":"1","cmd":"run"}`)); err != nil {
		t.Fatalf("valid run command rejected: %v", err)
	}
	// Unknown command is rejected (SR-1).
	if _, err := DecodeCommand([]byte(`{"id":"1","cmd":"rm-rf"}`)); err == nil {
		t.Fatal("unknown command accepted; SR-1 violated")
	}
	// Unknown field is rejected (SR-5 DisallowUnknownFields).
	if _, err := DecodeCommand([]byte(`{"cmd":"run","evil":"x"}`)); err == nil {
		t.Fatal("unknown field accepted; SR-5 violated")
	}
	// Oversize is rejected before parsing (SR-5).
	big := `{"cmd":"run","params":{"alias":"` + strings.Repeat("a", MaxMessageBytes) + `"}}`
	if _, err := DecodeCommand([]byte(big)); err == nil {
		t.Fatal("oversize message accepted; SR-5 violated")
	}
}

// FuzzDecodeCommand asserts the invariant from DESIGN.md §12:
// on ANY input, DecodeCommand never panics and, when it succeeds,
// the resulting command is always in the allow-list (SR-1).
func FuzzDecodeCommand(f *testing.F) {
	f.Add([]byte(`{"id":"1","cmd":"run"}`))
	f.Add([]byte(`{"cmd":"config.toggle","params":{"mod":"google","state":true}}`))
	f.Add([]byte(`{"cmd":"config.rename","params":{"alias":"东京-1"}}`))
	f.Add([]byte(`{"cmd":"rm-rf"}`))
	f.Add([]byte(`{"cmd":123}`))
	f.Add([]byte(`not json at all`))
	f.Add([]byte(``))
	f.Add([]byte(`{"cmd":"run","params":{"state":"true"}}`)) // type confusion

	f.Fuzz(func(t *testing.T, raw []byte) {
		msg, err := DecodeCommand(raw)
		if err != nil {
			return // clean rejection is the acceptable outcome
		}
		if !IsValidCommand(msg.Cmd) {
			t.Fatalf("DecodeCommand returned non-allow-listed cmd %q on success", msg.Cmd)
		}
	})
}
