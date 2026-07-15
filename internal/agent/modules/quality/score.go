package quality

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
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

	// ipapi.is risk score (free, no key).
	result.Ipapi = q.fetchIpapiScore(ctx)

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

	var data struct {
		RiskScore int `json:"risk_score"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil
	}
	s := strconv.Itoa(data.RiskScore) + "%"
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
