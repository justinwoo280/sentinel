package cli

import (
	"testing"

	ewp "github.com/justinwoo280/sing-ewp"
)

// TestDerivePublicKeyMatchesGenerated verifies that derivePublicKey
// reconstructs the same public key that GenerateServerStaticKeypair
// produced from the corresponding private key. This is essential: on
// `master init` re-run, we derive the pub key from the stored priv key
// and must get the exact key agents were configured with.
func TestDerivePublicKeyMatchesGenerated(t *testing.T) {
	priv, pub, err := ewp.GenerateServerStaticKeypair()
	if err != nil {
		t.Fatalf("GenerateServerStaticKeypair: %v", err)
	}
	derived, err := derivePublicKey(priv)
	if err != nil {
		t.Fatalf("derivePublicKey: %v", err)
	}
	if derived != pub {
		t.Fatalf("derived pub key %q != generated %q", derived, pub)
	}
}

func TestDerivePublicKeyInvalid(t *testing.T) {
	if _, err := derivePublicKey("not-base64!!!"); err == nil {
		t.Fatal("expected error for invalid base64")
	}
	if _, err := derivePublicKey("YWJj"); err == nil {
		t.Fatal("expected error for wrong-length key")
	}
}

func TestKeyExists(t *testing.T) {
	if keyExists("/nonexistent/path/to/key") {
		t.Fatal("keyExists returned true for missing file")
	}
	// A path that certainly exists.
	if !keyExists("/etc/hostname") && !keyExists("/etc/hosts") {
		t.Skip("no stable existing file to test against")
	}
}
