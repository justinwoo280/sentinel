package quality

import (
	"context"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"time"
)

// ScoreResult holds risk scores from multiple sources.
type ScoreResult struct {
	IP2Location *string
	Scamalytics *string
	Ipapi       *string
	AbuseIPDB   *string
	IPQS        *string
	DBIP        *string
}

func (q *Quality) queryScore(ctx context.Context) ScoreResult {
	result := ScoreResult{}

	// ipapi.is abuser score (free, no key).
	result.Ipapi = q.fetchIpapiScore(ctx)

	// DB-IP threat level (free, scraped, no key).
	result.DBIP = q.fetchDBIPScore(ctx)

	// Scamalytics (requires key).
	if s, _, _, _, _, _, _, _, _ := q.fetchScamalytics(ctx); s != nil {
		result.Scamalytics = s
	}

	// AbuseIPDB (requires key).
	if s, _, _ := q.fetchAbuseIPDB(ctx); s != nil {
		result.AbuseIPDB = s
	}

	// IP2Location (requires key).
	if s, _, _, _, _, _, _, _, _ := q.fetchIP2Location(ctx); s != nil {
		result.IP2Location = s
	}

	// IPQS (requires key).
	if s, _, _, _, _, _, _, _, _ := q.fetchIPQS(ctx); s != nil {
		result.IPQS = s
	}

	return result
}

func (q *Quality) fetchIpapiScore(ctx context.Context) *string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := q.httpGet(ctx, urlIpapi+q.ip)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// ipapi.is has no top-level risk_score; the risk signal is
	// company.abuser_score / asn.abuser_score, e.g. "0.0868 (High)".
	var data struct {
		Company struct {
			AbuserScore string `json:"abuser_score"`
		} `json:"company"`
		Asn struct {
			AbuserScore string `json:"abuser_score"`
		} `json:"asn"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil
	}
	s := data.Company.AbuserScore
	if s == "" {
		s = data.Asn.AbuserScore
	}
	if s == "" {
		return nil
	}
	return &s
}

// dbipThreatRe extracts the DB-IP threat level label, e.g.
//
//	threat level for this IP address is <span class='...'>Low</span>
var dbipThreatRe = regexp.MustCompile(`(?i)threat level for this IP address is\s*<span[^>]*>([^<]+)</span>`)

// fetchDBIPScore scrapes the DB-IP page for the threat level (free, no key).
func (q *Quality) fetchDBIPScore(ctx context.Context) *string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := q.httpGet(ctx, urlDBIP+q.ip)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	m := dbipThreatRe.FindSubmatch(body)
	if len(m) < 2 {
		return nil
	}
	s := strings.TrimSpace(string(m[1]))
	if s == "" {
		return nil
	}
	return &s
}

func (r ScoreResult) ToJSON() Score {
	return Score{
		IP2LOCATION: r.IP2Location,
		SCAMALYTICS: r.Scamalytics,
		Ipapi:       r.Ipapi,
		AbuseIPDB:   r.AbuseIPDB,
		IPQS:        r.IPQS,
		DBIP:        r.DBIP,
	}
}
