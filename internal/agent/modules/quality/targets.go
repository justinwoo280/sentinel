package quality

import (
	_ "embed"
	"strings"
)

// Targets are the compile-time fixed URL constants for all data sources
// and media unlock tests (DESIGN.md SR-3: no URL from message fields).

// --- Data source endpoints ---

const (
	// IPinfo free demo widget (no key required).
	urlIPinfo = "https://ipinfo.io/widget/demo/"

	// ipapi.is free API (no key required).
	urlIpapi = "https://api.ipapi.is/?q="

	// DB-IP HTML page (no key required, scraped).
	urlDBIP = "https://db-ip.com/"

	// ipregistry (key scraped from homepage or fallback).
	urlIPregistry = "https://api.ipregistry.co/"
	// ipregistryFallbackKey is the fallback key when scraping fails.
	ipregistryFallbackKey = "sb69ksjcajfs4c"

	// Scamalytics API (requires key).
	urlScamalytics = "https://api12.scamalytics.com/ipq/v1/"

	// AbuseIPDB API v2 (requires key).
	urlAbuseIPDB = "https://api.abuseipdb.com/api/v2/check"

	// IP2Location API (requires key).
	urlIP2Location = "https://api.ip2location.io/"

	// IPQS API (requires key).
	urlIPQS = "https://www.ipqualityscore.com/api/json/ip/"

	// ipdata API (requires key).
	urlIPData = "https://api.ipdata.co/"

	// IPinfo API (requires key for full data).
	urlIPinfoAPI = "https://ipinfo.io/"
)

// --- Media unlock test endpoints ---

const (
	urlTikTok        = "https://www.tiktok.com/"
	urlTikTokExplore = "https://www.tiktok.com/explore"

	urlDisneyDevices = "https://disney.api.edge.bamgrid.com/devices"
	urlDisneyToken   = "https://disney.api.edge.bamgrid.com/token"
	urlDisneyGraphQL = "https://disney.api.edge.bamgrid.com/graph/v1/device/graphql"
	urlDisneyPlus    = "https://disneyplus.com"

	// Disney bearer token (hardcoded in xykt, public, for device registration).
	disneyBearer = "ZGlzbmV5JmJyb3dzZXImMS4wLjA.Cu56AgSfBTDag5NiRA81oLHkDZfu5L3CKadnefEAY84"

	urlNetflixTitle1 = "https://www.netflix.com/title/81280792"
	urlNetflixTitle2 = "https://www.netflix.com/title/70143836"

	urlYouTubePremium = "https://www.youtube.com/premium"
	urlPrimeVideo     = "https://www.primevideo.com"
	urlReddit         = "https://www.reddit.com/"

	// ChatGPT / OpenAI
	urlOpenAICompliance = "https://api.openai.com/compliance/cookie_requirements"
	urlIOSChatOpenAI    = "https://ios.chat.openai.com/"
	urlChatOpenAITrace  = "https://chat.openai.com/cdn-cgi/trace"
	urlChatGPTFavicon   = "https://chatgpt.com/favicon.ico"

	// Cloudflare trace (for ChatGPT country code detection).
	urlCloudflareTrace = "https://www.cloudflare.com/cdn-cgi/trace"
)

// --- Mail provider domains ---

var mailProviderDomains = map[string]string{
	"Gmail":   "gmail.com",
	"Outlook": "outlook.com",
	"Yahoo":   "yahoo.com",
	"Apple":   "me.com",
	"MailRU":  "mail.ru",
	"AOL":     "aol.com",
	"GMX":     "gmx.com",
	"MailCOM": "mail.com",
	"163":     "163.com",
	"Sohu":    "sohu.com",
	"Sina":    "sina.com",
	"QQ":      "qq.com",
}

// mailProviderOrder is the canonical order for mail provider testing.
var mailProviderOrder = []string{
	"Gmail", "Outlook", "Yahoo", "Apple", "QQ",
	"MailRU", "AOL", "GMX", "MailCOM", "163", "Sohu", "Sina",
}

// mailTestServer is used for port25 outbound check.
const mailTestServer = "smtp.mailgun.org:25"

// dnsblList is the embedded DNS blacklist database list (76+ entries).
//
//go:embed dnsbl.list
var dnsblListRaw string

var dnsblList = func() []string {
	lines := strings.Split(strings.TrimSpace(dnsblListRaw), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}()
