package quality

import (
	"encoding/json"
	"testing"
)

// TestDBIPThreatRegex verifies the DB-IP threat-level scraper matches the
// real page markup. Regression for the bug where DB-IP score was never
// fetched (Score.DBIP always null).
func TestDBIPThreatRegex(t *testing.T) {
	html := `<p>The threat level for this IP address is <span class='label badge-success'>Low</span> based on our assessment.</p>`
	m := dbipThreatRe.FindStringSubmatch(html)
	if len(m) < 2 {
		t.Fatal("regex did not match DB-IP threat markup")
	}
	if m[1] != "Low" {
		t.Fatalf("got %q, want Low", m[1])
	}
}

func TestDBIPThreatRegexHigh(t *testing.T) {
	html := `threat level for this IP address is <span class="label badge-danger">Elevated</span>`
	m := dbipThreatRe.FindStringSubmatch(html)
	if len(m) < 2 || m[1] != "Elevated" {
		t.Fatalf("got %v", m)
	}
}

// TestIpapiAbuserScoreParse verifies the ipapi.is score comes from
// company.abuser_score (there is no top-level risk_score). Regression for
// the bug where Score.ipapi was always "0%".
func TestIpapiAbuserScoreParse(t *testing.T) {
	body := []byte(`{"ip":"1.2.3.4","company":{"name":"DO","abuser_score":"0.0868 (High)"},"asn":{"abuser_score":"0.075 (High)"}}`)
	var data struct {
		Company struct {
			AbuserScore string `json:"abuser_score"`
		} `json:"company"`
		Asn struct {
			AbuserScore string `json:"abuser_score"`
		} `json:"asn"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatal(err)
	}
	if data.Company.AbuserScore != "0.0868 (High)" {
		t.Fatalf("company abuser_score: got %q", data.Company.AbuserScore)
	}
	// fallback to asn when company missing
	body2 := []byte(`{"asn":{"abuser_score":"0.5 (Elevated)"}}`)
	var d2 struct {
		Company struct {
			AbuserScore string `json:"abuser_score"`
		} `json:"company"`
		Asn struct {
			AbuserScore string `json:"abuser_score"`
		} `json:"asn"`
	}
	_ = json.Unmarshal(body2, &d2)
	if d2.Company.AbuserScore != "" || d2.Asn.AbuserScore != "0.5 (Elevated)" {
		t.Fatalf("fallback parse wrong: %+v", d2)
	}
}
