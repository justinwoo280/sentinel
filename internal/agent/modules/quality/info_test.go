package quality

import (
	"os"
	"testing"
)

// TestParseIPinfoWidgetNested verifies the fix for the bug where the
// IPinfo demo widget's nested {"input":..,"data":{..}} shape (with "asn"
// as an object) was parsed at the top level, yielding all-N/A Info.
func TestParseIPinfoWidgetNested(t *testing.T) {
	body := []byte(`{
	  "input": "157.245.204.81",
	  "data": {
	    "ip": "157.245.204.81",
	    "city": "Singapore",
	    "region": "Singapore",
	    "country": "SG",
	    "loc": "1.3215,103.6957",
	    "org": "AS14061 DigitalOcean, LLC",
	    "postal": "627753",
	    "timezone": "Asia/Singapore",
	    "asn": {"asn": "AS14061", "name": "DigitalOcean, LLC", "domain": "digitalocean.com"}
	  }
	}`)
	r, ok := parseIPinfoBody(body)
	if !ok {
		t.Fatal("parse failed")
	}
	if r.IPinfoASN != "AS14061" {
		t.Errorf("ASN: got %q, want AS14061", r.IPinfoASN)
	}
	if r.IPinfoOrg != "DigitalOcean, LLC" {
		t.Errorf("Org: got %q, want DigitalOcean, LLC", r.IPinfoOrg)
	}
	if r.IPinfoCity != "Singapore" {
		t.Errorf("City: got %q, want Singapore", r.IPinfoCity)
	}
	if r.IPinfoCountryCode != "SG" {
		t.Errorf("CountryCode: got %q, want SG", r.IPinfoCountryCode)
	}
	if r.IPinfoLat != "1.3215" || r.IPinfoLon != "103.6957" {
		t.Errorf("Loc: got %q,%q", r.IPinfoLat, r.IPinfoLon)
	}
	if r.IPinfoTimezone != "Asia/Singapore" {
		t.Errorf("TZ: got %q", r.IPinfoTimezone)
	}
}

// TestParseIPinfoFlat verifies the plain (non-widget) API shape still works.
func TestParseIPinfoFlat(t *testing.T) {
	body := []byte(`{"ip":"8.8.8.8","city":"Mountain View","country":"US","asn":"AS15169","org":"Google LLC","timezone":"America/Los_Angeles"}`)
	r, ok := parseIPinfoBody(body)
	if !ok {
		t.Fatal("parse failed")
	}
	if r.IPinfoASN != "AS15169" || r.IPinfoOrg != "Google LLC" || r.IPinfoCountryCode != "US" {
		t.Errorf("flat parse wrong: %+v", r)
	}
}

// TestParseIPinfoLiveResponse runs against a captured live response if
// present (best-effort; skipped in CI).
func TestParseIPinfoLiveResponse(t *testing.T) {
	body, err := os.ReadFile("/tmp/opencode/ipinfo_resp.json")
	if err != nil {
		t.Skip("no captured response")
	}
	r, ok := parseIPinfoBody(body)
	if !ok || r.IPinfoCity == "" || r.IPinfoASN == "" {
		t.Fatalf("live parse produced empty Info: ok=%v %+v", ok, r)
	}
}

// TestToJSONGeoIPFallsBackToIpapiASNOrg is a regression test for a bug
// where enabling GeoIP (mmdb-city, which has no ASN data) permanently
// blanked ASN/Org instead of falling back to ipapi.is when the IPinfo
// ASN/Org lookup also failed (e.g. rate-limited). ToJSON must still
// surface ipapi.is's ASN/Org whenever IPinfo's are empty, regardless of
// why they're empty.
func TestToJSONGeoIPFallsBackToIpapiASNOrg(t *testing.T) {
	r := InfoResult{
		IPinfoOK: true, // set by the GeoIP branch in queryInfo
		// IPinfoASN/IPinfoOrg intentionally left empty, simulating a
		// failed (or not-yet-run) IPinfo ASN/Org lookup.
		IPinfoCity:        "Tokyo",
		IPinfoCountryCode: "JP",
		IpapiOK:           true,
		IpapiASN:          "AS2516",
		IpapiOrg:          "KDDI CORPORATION",
	}
	info := r.ToJSON()
	if info.ASN == nil || *info.ASN != "AS2516" {
		t.Errorf("ASN: got %v, want AS2516", info.ASN)
	}
	if info.Organization == nil || *info.Organization != "KDDI CORPORATION" {
		t.Errorf("Organization: got %v, want KDDI CORPORATION", info.Organization)
	}
}

// TestToJSONPrefersIPinfoASNOrgOverIpapi ensures that when the IPinfo
// ASN/Org lookup does succeed (the common case after the fix in
// queryInfo), it takes priority over the ipapi.is fallback.
func TestToJSONPrefersIPinfoASNOrgOverIpapi(t *testing.T) {
	r := InfoResult{
		IPinfoOK:  true,
		IPinfoASN: "AS4713",
		IPinfoOrg: "NTT Communications",
		IpapiOK:   true,
		IpapiASN:  "AS2516",
		IpapiOrg:  "KDDI CORPORATION",
	}
	info := r.ToJSON()
	if info.ASN == nil || *info.ASN != "AS4713" {
		t.Errorf("ASN: got %v, want AS4713 (IPinfo should take priority)", info.ASN)
	}
	if info.Organization == nil || *info.Organization != "NTT Communications" {
		t.Errorf("Organization: got %v, want NTT Communications (IPinfo should take priority)", info.Organization)
	}
}
