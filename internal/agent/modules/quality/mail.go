package quality

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"
)

// MailResult holds mail connectivity and DNSBL results.
type MailResult struct {
	Port25    *bool
	Providers map[string]*bool
	DNSBL     DNSBL
}

func (q *Quality) queryMail(ctx context.Context) MailResult {
	result := MailResult{
		Providers: make(map[string]*bool),
	}

	// Check port25 outbound connectivity.
	result.Port25 = q.checkPort25(ctx)

	// Check mail provider connectivity (concurrent).
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, name := range mailProviderOrder {
		wg.Add(1)
		go func(provider string) {
			defer wg.Done()
			ok := q.checkMailProvider(ctx, provider)
			mu.Lock()
			result.Providers[provider] = &ok
			mu.Unlock()
		}(name)
	}
	wg.Wait()

	// DNSBL check (IPv4 only).
	if q.ipPref != 6 && isIPv4(q.ip) {
		result.DNSBL = q.checkDNSBL(ctx, q.ip)
	}

	return result
}

func (r MailResult) ToJSON() Mail {
	mail := Mail{
		Port25: r.Port25,
	}
	for _, name := range mailProviderOrder {
		val := r.Providers[name]
		switch name {
		case "Gmail":
			mail.Gmail = val
		case "Outlook":
			mail.Outlook = val
		case "Yahoo":
			mail.Yahoo = val
		case "Apple":
			mail.Apple = val
		case "QQ":
			mail.QQ = val
		case "MailRU":
			mail.MailRU = val
		case "AOL":
			mail.AOL = val
		case "GMX":
			mail.GMX = val
		case "MailCOM":
			mail.MailCOM = val
		case "163":
			mail.M163 = val
		case "Sohu":
			mail.Sohu = val
		case "Sina":
			mail.Sina = val
		}
	}
	mail.DNSBlacklist = r.DNSBL
	return mail
}

func (q *Quality) checkPort25(ctx context.Context) *bool {
	// Try connecting to a known SMTP server on port 25.
	dialer := net.Dialer{Timeout: 10 * time.Second}
	if q.bindIP != "" {
		ip := net.ParseIP(q.bindIP)
		if ip != nil {
			dialer.LocalAddr = &net.TCPAddr{IP: ip}
		}
	}

	conn, err := dialer.DialContext(ctx, "tcp", mailTestServer)
	if err != nil {
		f := false
		return &f
	}
	defer conn.Close()

	// Read SMTP greeting.
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		f := false
		return &f
	}
	greeting := string(buf[:n])
	if strings.Contains(greeting, "220") {
		t := true
		return &t
	}
	f := false
	return &f
}

func (q *Quality) checkMailProvider(ctx context.Context, provider string) bool {
	domain, ok := mailProviderDomains[provider]
	if !ok {
		return false
	}

	// Resolve MX records.
	mxs, err := net.LookupMX(domain)
	if err != nil || len(mxs) == 0 {
		return false
	}

	// Try connecting to the first MX on port 25.
	dialer := net.Dialer{Timeout: 5 * time.Second}
	if q.bindIP != "" {
		ip := net.ParseIP(q.bindIP)
		if ip != nil {
			dialer.LocalAddr = &net.TCPAddr{IP: ip}
		}
	}

	for _, mx := range mxs {
		addr := fmt.Sprintf("%s:25", strings.TrimSuffix(mx.Host, "."))
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			continue
		}

		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		buf := make([]byte, 256)
		n, err := conn.Read(buf)
		conn.Close()
		if err != nil || n == 0 {
			continue
		}
		if strings.Contains(string(buf[:n]), "220") {
			return true
		}
	}
	return false
}

func (q *Quality) checkDNSBL(ctx context.Context, ip string) DNSBL {
	// Reverse the IP for DNSBL lookup.
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return DNSBL{}
	}
	reversed := fmt.Sprintf("%s.%s.%s.%s", parts[3], parts[2], parts[1], parts[0])

	total := 0
	clean := 0
	blacklisted := 0
	other := 0

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, dnsbl := range dnsblList {
		wg.Add(1)
		go func(d string) {
			defer wg.Done()
			query := fmt.Sprintf("%s.%s", reversed, d)
			lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			resolver := net.DefaultResolver
			addrs, err := resolver.LookupHost(lookupCtx, query)

			mu.Lock()
			total++
			if err != nil || len(addrs) == 0 {
				clean++
			} else if len(addrs) > 0 && addrs[0] == "127.0.0.2" {
				blacklisted++
			} else {
				other++
			}
			mu.Unlock()
		}(dnsbl)
	}
	wg.Wait()

	return DNSBL{
		Total:       &total,
		Clean:       &clean,
		Marked:      &other,
		Blacklisted: &blacklisted,
	}
}

func isIPv4(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.To4() != nil
}

// Avoid unused import.
var _ = smtp.Dial
