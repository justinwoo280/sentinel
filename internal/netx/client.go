// Package netx provides HTTP client construction and network utilities
// for the Agent's keepalive modules.
package netx

import (
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/justinwoo280/sentinel/internal/cookiejarx"
)

// ClientConfig controls the construction of an HTTP client.
type ClientConfig struct {
	BindIP        string         // local IP to bind; empty = system default
	IPPref        int            // 4 or 6; 0 = auto
	Timeout       time.Duration  // per-request timeout; 0 = 30s
	CookieJar     http.CookieJar // optional external jar; nil = new in-memory jar
	CookieJarPath string         // if set with CookieJar==nil, persistent jar is created
	CookieMaxAge  time.Duration  // max age for persistent jar (default 14 days)
}

// NewClient builds an *http.Client that binds to a specific local IP
// (if configured), forces IPv4/IPv6, and carries a cookie jar.
//
// If BindIP is set but not present on any local interface, the bind is
// silently dropped and the system default route is used (mirrors the
// original project's graceful degradation).
func NewClient(cfg ClientConfig) (*http.Client, error) {
	var jar http.CookieJar
	if cfg.CookieJar != nil {
		jar = cfg.CookieJar
	} else if cfg.CookieJarPath != "" {
		maxAge := cfg.CookieMaxAge
		if maxAge == 0 {
			maxAge = 14 * 24 * time.Hour // 14 days
		}
		pj, err := cookiejarxNew(cfg.CookieJarPath, maxAge)
		if err != nil {
			return nil, fmt.Errorf("netx: persistent cookie jar: %w", err)
		}
		jar = pj
	} else {
		inner, err := cookiejar.New(nil)
		if err != nil {
			return nil, fmt.Errorf("netx: cookie jar: %w", err)
		}
		jar = inner
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	network := "tcp"

	if cfg.BindIP != "" {
		ipStr := strings.Trim(cfg.BindIP, "[]")
		ip := net.ParseIP(ipStr)
		if ip == nil {
			return nil, fmt.Errorf("netx: invalid bind IP %q", cfg.BindIP)
		}
		if !isLocalIP(ip) {
			// Graceful degradation: IP not on any interface.
			// Fall back to system default route.
		} else {
			dialer.LocalAddr = &net.TCPAddr{IP: ip}
			if ip.To4() != nil {
				network = "tcp4"
			} else {
				network = "tcp6"
			}
		}
	}

	if cfg.IPPref == 4 {
		network = "tcp4"
	} else if cfg.IPPref == 6 {
		network = "tcp6"
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, addr)
			},
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
			DisableKeepAlives:     true,
		},
		Jar:     jar,
		Timeout: timeout,
	}, nil
}

// cookiejarxNew is a bridge to cookiejarx.New (kept as a local alias
// to avoid import cycle issues in tests).
func cookiejarxNew(path string, maxAge time.Duration) (http.CookieJar, error) {
	return cookiejarx.New(path, maxAge)
}

// isLocalIP reports whether ip is assigned to any local interface.
func isLocalIP(ip net.IP) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return true // assume yes if we can't check
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			if ipnet.IP.Equal(ip) {
				return true
			}
		}
	}
	return false
}

// HashSeed returns a deterministic uint32 seed from a string (typically
// the agent's public IP). Used for hash-seeded UA selection so each
// node has a stable device fingerprint.
func HashSeed(s string) uint32 {
	return crc32.ChecksumIEEE([]byte(s))
}

// PickUAs selects 3 UAs from pool using a hash seed, mirroring the
// original mod_google.sh's hash-seeded persona logic.
func PickUAs(pool []string, seed uint32) []string {
	n := len(pool)
	if n == 0 {
		return nil
	}
	if n <= 3 {
		out := make([]string, n)
		copy(out, pool)
		return out
	}
	idx1 := int(seed) % n
	idx2 := int(seed*17) % n
	idx3 := int(seed*31) % n
	return []string{pool[idx1], pool[idx2], pool[idx3]}
}

// Platform identifies an OS platform inferred from a User-Agent string.
type Platform string

const (
	PlatformWindows Platform = "windows"
	PlatformAndroid Platform = "android"
	PlatformIOS     Platform = "ios"
	PlatformMacOS   Platform = "macos"
	PlatformLinux   Platform = "linux"
)

func DetectPlatform(ua string) Platform {
	switch {
	case strings.Contains(ua, "Android"):
		return PlatformAndroid
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad"):
		return PlatformIOS
	case strings.Contains(ua, "Macintosh"):
		return PlatformMacOS
	case strings.Contains(ua, "Linux"):
		return PlatformLinux
	default:
		return PlatformWindows
	}
}

// JitterCoord adds a small random offset to a base coordinate.
// range_ is in units of 1/10000 degree (~1.1m at the equator).
func JitterCoord(base float64, rng int) float64 {
	if rng <= 0 {
		return base
	}
	offset := float64(rand.Intn(rng*2)-rng) / 10000.0
	return base + offset
}

// EncodeQuery URL-encodes a string for use in search queries.
func EncodeQuery(s string) string {
	return url.QueryEscape(s)
}

// DetectPublicIP fetches the agent's public IP from an API.
func DetectPublicIP(ipPref int) (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	network := "tcp"
	if ipPref == 4 {
		network = "tcp4"
	} else if ipPref == 6 {
		network = "tcp6"
	}
	client.Transport = &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, addr)
		},
	}
	resp, err := client.Get("https://api.ip.sb/ip")
	if err != nil {
		return "", fmt.Errorf("netx: detect public IP: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

// DetectPublicIPChecked fetches the public IP and verifies it is not a
// WARP/tunnel fake public IP. If a fake public IP is detected, an error is
// returned describing the reason (DESIGN.md §5.5).
func DetectPublicIPChecked(ipPref int) (string, error) {
	ip, err := DetectPublicIP(ipPref)
	if err != nil {
		return "", err
	}
	if fake, reason := CheckFakePublicIP(ip); fake {
		return "", fmt.Errorf("netx: %s", reason)
	}
	return ip, nil
}
