package quality

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestResultJSON(t *testing.T) {
	status := "Yes"
	region := "US"
	typ := "DNS"
	ascn := "AS12345"
	org := "Test ISP"
	country := "United States"
	cc := "US"
	geoType := "Geo-consistent"

	result := &Result{
		Head: Head{IP: "1.2.3.4", Version: "test"},
		Info: Info{
			ASN:          &ascn,
			Organization: &org,
			Region:       RegionInfo{Code: &cc, Name: &country},
			Type:         &geoType,
		},
		Media: Media{
			Netflix: MediaEntry{Status: &status, Region: &region, Type: &typ},
		},
	}

	j, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	// Verify top-level keys exist.
	var raw map[string]any
	if err := json.Unmarshal(j, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"Head", "Info", "Type", "Score", "Factor", "Media", "Mail"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("missing top-level key %q in JSON", key)
		}
	}

	// Verify Head.IP.
	head := raw["Head"].(map[string]any)
	if head["IP"] != "1.2.3.4" {
		t.Fatalf("Head.IP: got %v, want 1.2.3.4", head["IP"])
	}

	// Verify Media.Netflix.Status.
	media := raw["Media"].(map[string]any)
	netflix := media["Netflix"].(map[string]any)
	if netflix["Status"] != "Yes" {
		t.Fatalf("Netflix status: got %v, want Yes", netflix["Status"])
	}
}

func TestAssembleReport(t *testing.T) {
	status := "Yes"
	region := "US"
	trueVal := true
	score := "12"
	ascn := "AS12345"
	org := "Test ISP"
	cc := "US"
	country := "United States"
	geoType := "Geo-consistent"

	result := &Result{
		Head: Head{IP: "1.2.3.4", Time: "2025-01-01 00:00:00 UTC", Version: "test"},
		Info: Info{
			ASN:          &ascn,
			Organization: &org,
			Region:       RegionInfo{Code: &cc, Name: &country},
			Type:         &geoType,
		},
		Score: Score{
			SCAMALYTICS: &score,
		},
		Media: Media{
			Netflix: MediaEntry{Status: &status, Region: &region},
		},
		Mail: Mail{
			Port25: &trueVal,
		},
	}

	report := assembleReport(result)
	if !strings.Contains(report, "1.2.3.4") {
		t.Fatal("report should contain IP")
	}
	if !strings.Contains(report, "AS12345") {
		t.Fatal("report should contain ASN")
	}
	if !strings.Contains(report, "Netflix") {
		t.Fatal("report should contain Netflix")
	}
	if !strings.Contains(report, "Port25") {
		t.Fatal("report should contain Port25")
	}
	if !strings.Contains(report, "Scamalytics") {
		t.Fatal("report should contain Scamalytics")
	}
}

func TestScoreFormatting(t *testing.T) {
	val := "42"
	s := formatScore("Test", &val)
	if !strings.Contains(s, "Test: 42") {
		t.Fatalf("expected 'Test: 42', got %q", s)
	}

	s = formatScore("Test", nil)
	if !strings.Contains(s, "N/A") {
		t.Fatalf("expected N/A for nil score, got %q", s)
	}
}

func TestFactorFormatting(t *testing.T) {
	m := map[string]*string{
		"ipapi":       ptrStr("true"),
		"Scamalytics": ptrStr("false"),
		"IP2Location": nil,
	}
	s := formatFactor("Proxy", m)
	if !strings.Contains(s, "1 yes") {
		t.Fatalf("expected '1 yes', got %q", s)
	}
	if !strings.Contains(s, "1 no") {
		t.Fatalf("expected '1 no', got %q", s)
	}
	if !strings.Contains(s, "1 unknown") {
		t.Fatalf("expected '1 unknown', got %q", s)
	}
}

func TestMediaEntry(t *testing.T) {
	e := okEntry("US", "DNS")
	if *e.Status != "Yes" || *e.Region != "US" || *e.Type != "DNS" {
		t.Fatalf("okEntry wrong: %+v", e)
	}

	e = failedEntry()
	if *e.Status != "Failed" {
		t.Fatalf("failedEntry wrong: %+v", e)
	}

	e = noEntry()
	if *e.Status != "No" {
		t.Fatalf("noEntry wrong: %+v", e)
	}
}

func TestBoolStr(t *testing.T) {
	s := boolStr(true)
	if *s != "true" {
		t.Fatalf("boolStr(true) = %q, want 'true'", *s)
	}
	s = boolStr(false)
	if *s != "false" {
		t.Fatalf("boolStr(false) = %q, want 'false'", *s)
	}
}

func TestIsIPv4(t *testing.T) {
	if !isIPv4("1.2.3.4") {
		t.Fatal("1.2.3.4 should be IPv4")
	}
	if isIPv4("::1") {
		t.Fatal("::1 should not be IPv4")
	}
	if isIPv4("not-an-ip") {
		t.Fatal("not-an-ip should not be IPv4")
	}
}

func TestContinentName(t *testing.T) {
	if continentName("US") != "North America" {
		t.Fatal("US should be North America")
	}
	if continentName("JP") != "Asia" {
		t.Fatal("JP should be Asia")
	}
	if continentName("XX") != "" {
		t.Fatal("XX should be empty")
	}
}

func TestQualityNew(t *testing.T) {
	q := New("1.2.3.4", "", 4, "", APIKeys{}, nil)
	if q.ip != "1.2.3.4" {
		t.Fatalf("ip: got %q, want 1.2.3.4", q.ip)
	}
	if q.ua == "" {
		t.Fatal("UA should not be empty (default)")
	}
}

func TestQualityJSONOutput(t *testing.T) {
	result := &Result{
		Head: Head{IP: "1.2.3.4"},
	}
	j := result.JSON()
	if !strings.Contains(j, "1.2.3.4") {
		t.Fatal("JSON should contain IP")
	}
}

func TestRunEmptyIP(t *testing.T) {
	q := New("", "", 4, "", APIKeys{}, nil)
	_, _, err := q.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for empty IP")
	}
}
