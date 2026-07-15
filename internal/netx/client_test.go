package netx

import (
	"testing"
)

func TestHashSeed(t *testing.T) {
	s1 := HashSeed("1.2.3.4")
	s2 := HashSeed("1.2.3.4")
	s3 := HashSeed("1.2.3.5")
	if s1 != s2 {
		t.Fatal("same input should produce same seed")
	}
	if s1 == s3 {
		t.Fatal("different input should produce different seed")
	}
}

func TestPickUAs(t *testing.T) {
	pool := []string{"ua-a", "ua-b", "ua-c", "ua-d", "ua-e", "ua-f", "ua-g"}
	got := PickUAs(pool, 42)
	if len(got) != 3 {
		t.Fatalf("got %d UAs, want 3", len(got))
	}
	// Determinism: same seed → same selection.
	got2 := PickUAs(pool, 42)
	for i := range got {
		if got[i] != got2[i] {
			t.Fatalf("non-deterministic: got[%d]=%q vs %q", i, got[i], got2[i])
		}
	}
}

func TestPickUAsSmallPool(t *testing.T) {
	pool := []string{"ua-a", "ua-b"}
	got := PickUAs(pool, 42)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
}

func TestPickUAsEmpty(t *testing.T) {
	got := PickUAs(nil, 42)
	if got != nil {
		t.Fatalf("expected nil for empty pool, got %v", got)
	}
}

func TestDetectPlatform(t *testing.T) {
	tests := []struct {
		ua   string
		want Platform
	}{
		{"Mozilla/5.0 (Android 14)", PlatformAndroid},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17)", PlatformIOS},
		{"Mozilla/5.0 (iPad)", PlatformIOS},
		{"Mozilla/5.0 (Macintosh)", PlatformMacOS},
		{"Mozilla/5.0 (X11; Linux x86_64)", PlatformLinux},
		{"Mozilla/5.0 (Windows NT 10.0)", PlatformWindows},
		{"unknown", PlatformWindows},
	}
	for _, tt := range tests {
		got := DetectPlatform(tt.ua)
		if got != tt.want {
			t.Errorf("DetectPlatform(%q) = %q, want %q", tt.ua, got, tt.want)
		}
	}
}

func TestJitterCoord(t *testing.T) {
	base := 35.6762
	got := JitterCoord(base, 0)
	if got != base {
		t.Fatalf("rng=0 should return base: got %v, want %v", got, base)
	}

	// With non-zero range, result should be within ±rng/10000 of base.
	got = JitterCoord(base, 100)
	delta := got - base
	if delta < -0.01 || delta > 0.01 {
		t.Fatalf("jitter out of range: delta=%v", delta)
	}
}

func TestNewClientDefaults(t *testing.T) {
	c, err := NewClient(ClientConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Timeout != 30*1e9 { // 30s
		t.Fatalf("timeout: got %v, want 30s", c.Timeout)
	}
	if c.Jar == nil {
		t.Fatal("cookie jar should not be nil")
	}
}

func TestNewClientInvalidBindIP(t *testing.T) {
	_, err := NewClient(ClientConfig{BindIP: "not-an-ip"})
	if err == nil {
		t.Fatal("expected error for invalid bind IP")
	}
}

func TestNewClientIPPref4(t *testing.T) {
	c, err := NewClient(ClientConfig{IPPref: 4})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("client should not be nil")
	}
}
