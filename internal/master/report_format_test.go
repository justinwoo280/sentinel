package master

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/justinwoo280/sentinel/internal/master/store"
)

// realQualityEnvelope is an actual agent quality result (envelope form)
// captured during live testing — used to verify the Telegram report
// formatter produces readable output rather than a raw JSON dump.
const realQualityEnvelope = `{"ok":true,"msg":"quality check completed","data":{"Head":{"IP":"157.245.204.81"},"Info":{"ASN":null,"Organization":null,"TimeZone":null,"Region":{"Name":null},"Continent":{"Name":null}},"Score":{"IP2LOCATION":null,"SCAMALYTICS":null,"ipapi":"0%","AbuseIPDB":null,"IPQS":null,"DBIP":null},"Media":{"TikTok":{"Status":"Failed","Region":"N/A"},"Netflix":{"Status":"Yes","Region":"SG"},"Youtube":{"Status":"Yes","Region":"SG"},"ChatGPT":{"Status":"Yes","Region":"SG"},"Reddit":{"Status":"No","Region":"N/A"}},"Mail":{"Port25":true,"DNSBlacklist":{"Total":69,"Clean":63,"Marked":5,"Blacklisted":1}}}}`

func TestBuildQualityTelegramReport(t *testing.T) {
	node := &store.Node{NodeName: "docker-agent-0715"}
	report := buildQualityTelegramReport(node, json.RawMessage(realQualityEnvelope))

	// Must NOT be a raw JSON dump.
	if strings.Contains(report, `"ok":true`) || strings.Contains(report, `"Media":{`) {
		t.Fatalf("report still contains raw JSON:\n%s", report)
	}
	// Must contain formatted, human-readable highlights.
	checks := []string{
		"Quality Report: docker-agent-0715",
		"Netflix: Yes (SG)",
		"Youtube: Yes (SG)",
		"ChatGPT: Yes (SG)",
		"ipapi: 0%",
		"Port25: yes",
		"63/69 clean",
		"1 blacklisted",
	}
	for _, c := range checks {
		if !strings.Contains(report, c) {
			t.Errorf("report missing %q\n---\n%s", c, report)
		}
	}
}

func TestBuildQualityTelegramReportNoScores(t *testing.T) {
	// When all commercial scores are null, show the hint.
	env := `{"data":{"Score":{"SCAMALYTICS":null,"ipapi":null},"Media":{},"Mail":{}}}`
	node := &store.Node{NodeName: "n"}
	report := buildQualityTelegramReport(node, json.RawMessage(env))
	if !strings.Contains(report, "configure API keys") {
		t.Fatalf("expected API-key hint, got:\n%s", report)
	}
}
