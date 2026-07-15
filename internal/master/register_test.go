package master

import (
	"testing"

	"github.com/justinwoo280/sentinel/internal/protocol"
)

func TestExtractRegBlob(t *testing.T) {
	// Build a real blob.
	reg := &protocol.Registration{
		Version: protocol.RegVersion,
		Region:  "JP",
		Node:    "docker-agent-0715",
		Alias:   "Docker测试",
		UUID:    "38f6a3e0-d841-4948-b2af-81a3ad683d92",
	}
	blob, err := reg.Encode()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		in   string
	}{
		{"bare blob", blob},
		{"with /register prefix", "/register " + blob},
		{"with leading/trailing space", "   /register  " + blob + "  "},
		{"with newline", "/register\n" + blob},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractRegBlob(c.in)
			// The extracted blob must decode successfully.
			parsed, err := protocol.Decode(got)
			if err != nil {
				t.Fatalf("extractRegBlob(%q) -> %q did not decode: %v", c.in, got, err)
			}
			if parsed.UUID != reg.UUID || parsed.Node != reg.Node || parsed.Alias != reg.Alias {
				t.Fatalf("decoded mismatch: got %+v", parsed)
			}
		})
	}
}

func TestExtractRegBlobNoPrefix(t *testing.T) {
	// No prefix present: returns trimmed input unchanged (Decode errors later).
	if got := extractRegBlob("  garbage  "); got != "garbage" {
		t.Fatalf("got %q, want %q", got, "garbage")
	}
}
