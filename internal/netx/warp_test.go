package netx

import (
	"testing"
)

func TestIsWARPTrue(t *testing.T) {
	tests := []string{
		"104.16.0.1",
		"104.24.0.1",
		"172.64.0.1",
		"162.158.0.1",
		"100.64.0.1",
		"100.127.255.255",
	}
	for _, ip := range tests {
		if !IsWARPIP(ip) {
			t.Errorf("IsWARPIP(%q) = false, want true", ip)
		}
	}
}

func TestIsWARPFalse(t *testing.T) {
	tests := []string{
		"1.2.3.4",
		"8.8.8.8",
		"203.0.113.1",
		"198.51.100.1",
		"192.0.2.1",
	}
	for _, ip := range tests {
		if IsWARPIP(ip) {
			t.Errorf("IsWARPIP(%q) = true, want false", ip)
		}
	}
}

func TestIsWARPInvalidIP(t *testing.T) {
	if IsWARPIP("not-an-ip") {
		t.Fatal("invalid IP should return false, not panic")
	}
	if IsWARPIP("") {
		t.Fatal("empty string should return false")
	}
}

func TestIsTUNInterface(t *testing.T) {
	// On any platform this should not panic.
	// On Linux with /proc/net/route it may return true/false; on others false.
	_ = IsTUNInterface()
}

func TestCheckFakePublicIP(t *testing.T) {
	isFake, reason := CheckFakePublicIP("1.2.3.4")
	if isFake {
		t.Fatalf("1.2.3.4 should not be fake: %s", reason)
	}
	if reason != "" {
		t.Fatalf("non-fake IP should have empty reason, got %q", reason)
	}

	isFake, reason = CheckFakePublicIP("104.16.0.1")
	if !isFake {
		t.Fatal("104.16.0.1 should be flagged as WARP")
	}
	if reason == "" {
		t.Fatal("reason should not be empty for fake IP")
	}
}
