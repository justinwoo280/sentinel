package master

import (
	"testing"

	ewp "github.com/justinwoo280/sing-ewp"
)

// TestUUIDCanonicalMatchesRegistration is a regression test for the bug
// where OnHello looked up nodes by the no-hyphen hex form of the UUID
// bytes, while registration stored the canonical hyphenated form from the
// SENTINEL-REG blob — so hellos were reported as "unregistered agent" and
// node info (IP/online/last_seen) never updated.
func TestUUIDCanonicalMatchesRegistration(t *testing.T) {
	const canonical = "38f6a3e0-d841-4948-b2af-81a3ad683d92"

	// Parse to bytes the way the EWP handshake delivers them...
	b, err := ewp.ParseUUID(canonical)
	if err != nil {
		t.Fatal(err)
	}
	// ...then format back. It MUST equal the canonical string that
	// registration (protocol.Decode) stores in the DB.
	if got := uuidCanonical(b); got != canonical {
		t.Fatalf("uuidCanonical round-trip mismatch:\n got  %q\n want %q", got, canonical)
	}
}

func TestUUIDCanonicalFormat(t *testing.T) {
	var b [ewp.UUIDLen]byte
	for i := range b {
		b[i] = byte(i)
	}
	// 000102030405060708090a0b0c0d0e0f -> 8-4-4-4-12
	want := "00010203-0405-0607-0809-0a0b0c0d0e0f"
	if got := uuidCanonical(b); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
