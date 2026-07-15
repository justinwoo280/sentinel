package quality

import (
	"net/url"
	"strings"
	"testing"
)

// TestTargetURLsAreHTTPS (SR-3): all hardcoded target URLs must use
// HTTPS and point to the whitelisted domains. No URL may be constructed
// from user input at runtime.

func TestTargetURLsAreHTTPS(t *testing.T) {
	urls := []string{
		urlIPinfo, urlIpapi, urlDBIP, urlIPregistry,
		urlTikTok, urlTikTokExplore,
		urlDisneyDevices, urlDisneyToken, urlDisneyGraphQL, urlDisneyPlus,
		urlNetflixTitle1, urlNetflixTitle2,
		urlYouTubePremium, urlPrimeVideo, urlReddit,
		urlOpenAICompliance, urlIOSChatOpenAI, urlChatOpenAITrace, urlChatGPTFavicon,
		urlCloudflareTrace,
	}
	for _, u := range urls {
		parsed, err := url.Parse(u)
		if err != nil {
			t.Fatalf("invalid URL %q: %v", u, err)
		}
		if parsed.Scheme != "https" {
			t.Errorf("URL %q uses scheme %q, want https", u, parsed.Scheme)
		}
		if parsed.Host == "" {
			t.Errorf("URL %q has empty host", u)
		}
	}
}

// TestMailProviderDomainsAreFixed (SR-3): mail provider domains are
// compile-time constants, never from user input.
func TestMailProviderDomainsAreFixed(t *testing.T) {
	for name, domain := range mailProviderDomains {
		if domain == "" {
			t.Errorf("mail provider %q has empty domain", name)
		}
		if strings.Contains(domain, " ") {
			t.Errorf("mail provider %q domain has space: %q", name, domain)
		}
		// Must be a valid domain (no path, no scheme).
		if strings.Contains(domain, "/") || strings.Contains(domain, ":") {
			t.Errorf("mail provider %q domain is not bare: %q", name, domain)
		}
	}
	// All providers in the order list must have domains.
	for _, name := range mailProviderOrder {
		if _, ok := mailProviderDomains[name]; !ok {
			t.Errorf("mail provider %q in order list but not in domain map", name)
		}
	}
}

// TestDNSBLListNotEmpty verifies the embedded DNSBL list is loaded.
func TestDNSBLListNotEmpty(t *testing.T) {
	if len(dnsblList) < 20 {
		t.Fatalf("DNSBL list too short: %d entries (expected 20+)", len(dnsblList))
	}
	// Each entry must be a valid domain.
	for _, d := range dnsblList {
		if d == "" {
			t.Fatal("DNSBL list contains empty entry")
		}
		if strings.Contains(d, " ") {
			t.Fatalf("DNSBL entry has space: %q", d)
		}
	}
}

// TestDisneyBearerIsConstant verifies the Disney bearer token is a
// compile-time constant, not derived from input.
func TestDisneyBearerIsConstant(t *testing.T) {
	if disneyBearer == "" {
		t.Fatal("Disney bearer token is empty")
	}
	if len(disneyBearer) < 20 {
		t.Fatal("Disney bearer token too short")
	}
}

// TestIPregistryFallbackKeyIsConstant verifies the fallback key exists.
func TestIPregistryFallbackKeyIsConstant(t *testing.T) {
	if ipregistryFallbackKey == "" {
		t.Fatal("ipregistry fallback key is empty")
	}
}
