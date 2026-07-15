package quality

import (
	"context"
	"net"
	"net/http"
	"time"
)

// newHTTPClient creates an *http.Client with optional IP binding and
// v4/v6 preference.
func newHTTPClient(bindIP string, ipPref int, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	network := "tcp"

	if bindIP != "" {
		ip := net.ParseIP(bindIP)
		if ip != nil && isLocalIP(ip) {
			dialer.LocalAddr = &net.TCPAddr{IP: ip}
			if ip.To4() != nil {
				network = "tcp4"
			} else {
				network = "tcp6"
			}
		}
	}

	if ipPref == 4 {
		network = "tcp4"
	} else if ipPref == 6 {
		network = "tcp6"
	}

	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, addr)
			},
		},
		Timeout: timeout,
	}
}

func isLocalIP(ip net.IP) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return true
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
