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
