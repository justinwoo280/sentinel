package netx

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// warpCIDRs are IP ranges commonly used by Cloudflare WARP and similar
// tunnel/VPN services. When the detected public IP falls into one of these
// ranges it is considered a "fake public IP" (tunnel egress) rather than a
// genuine VPS public IP.
var warpCIDRs = []string{
	// Cloudflare WARP / Argo Tunnel egress ranges.
	"104.16.0.0/13",
	"104.24.0.0/14",
	"172.64.0.0/13",
	"162.158.0.0/15",
	"131.0.72.0/22",
	// CGNAT range — some VPN/tunnel products use this.
	"100.64.0.0/10",
}

var parsedWARP []*net.IPNet

func init() {
	for _, cidr := range warpCIDRs {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("netx: invalid WARP CIDR %q", cidr))
		}
		parsedWARP = append(parsedWARP, n)
	}
}

// IsWARPIP checks whether the given IP address falls within known WARP/tunnel
// CIDR ranges. Returns true if the IP is likely a tunnel egress address rather
// than a genuine VPS public IP (DESIGN.md §5.5).
func IsWARPIP(ipStr string) bool {
	ip := net.ParseIP(strings.Trim(ipStr, "[]"))
	if ip == nil {
		return false
	}
	for _, n := range parsedWARP {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// tunInterfacePrefixes lists interface name prefixes that indicate a
// tunnel/VPN interface on Linux.
var tunInterfacePrefixes = []string{
	"tun",
	"tap",
	"wg",
	"wg0",
	"ppp",
	"ipip",
	"sit",
	"gre",
	"nord",
	"nordlynx",
	"proton",
	"ovpn",
}

// IsTUNInterface reports whether the default route's egress interface
// looks like a tunnel/VPN interface. It reads /proc/net/route to find the
// default route interface, then checks /proc/net/dev or net.Interfaces for
// matching names.
//
// If the route table cannot be read (non-Linux, container without /proc),
// it returns false (no detection, not a false positive).
func IsTUNInterface() bool {
	ifaceName, err := defaultRouteInterface()
	if err != nil || ifaceName == "" {
		return false
	}
	lower := strings.ToLower(ifaceName)
	for _, prefix := range tunInterfacePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// defaultRouteInterface reads /proc/net/route and returns the interface
// name for the default route (destination 00000000).
func defaultRouteInterface() (string, error) {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if i == 0 {
			continue // header
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		// Field 1 = destination, field 7 = mask.
		// Default route has destination 00000000 and mask 00000000.
		if fields[1] == "00000000" {
			return fields[0], nil
		}
	}
	return "", nil
}

// CheckFakePublicIP combines IsWARPIP and IsTUNInterface to give a single
// verdict: if either the detected public IP is in WARP ranges or the default
// route goes through a TUN interface, the IP is considered a fake public IP.
//
// Returns (isFake, reason). When not fake, reason is empty.
func CheckFakePublicIP(publicIP string) (bool, string) {
	if IsWARPIP(publicIP) {
		return true, fmt.Sprintf("public IP %s is in WARP/tunnel CIDR range", publicIP)
	}
	if IsTUNInterface() {
		iface, _ := defaultRouteInterface()
		return true, fmt.Sprintf("default route via tunnel interface %q", iface)
	}
	return false, ""
}
