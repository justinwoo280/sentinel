package quality

import (
	"context"
	"encoding/json"
	"time"
)

// TypeResult holds the raw data for the Type module.
type TypeResult struct {
	Usage   map[string]*string
	Company map[string]*string
}

func (q *Quality) queryType(ctx context.Context) TypeResult {
	result := TypeResult{
		Usage:   make(map[string]*string),
		Company: make(map[string]*string),
	}

	// ipapi.is usage type + company (free, no key).
	if usage, company := q.fetchIpapiType(ctx); true {
		if usage != nil {
			result.Usage["ipapi"] = usage
		}
		if company != nil {
			result.Company["ipapi"] = company
		}
	}

	// AbuseIPDB usage type (requires key).
	if _, usage, _ := q.fetchAbuseIPDB(ctx); usage != nil {
		result.Usage["AbuseIPDB"] = usage
	}

	// IP2Location usage type (requires key).
	if _, _, _, _, _, _, _, _, usage := q.fetchIP2Location(ctx); usage != nil {
		result.Usage["IP2LOCATION"] = usage
	}

	// IPQS usage type (requires key).
	if _, _, _, _, _, _, _, _, usage := q.fetchIPQS(ctx); usage != nil {
		result.Usage["IPQS"] = usage
	}

	// IPinfo usage + company type (requires key).
	if usage, company, _, _, _ := q.fetchIPinfoFull(ctx); true {
		if usage != nil {
			result.Usage["IPinfo"] = usage
		}
		if company != nil {
			result.Company["IPinfo"] = company
		}
	}

	return result
}

func (q *Quality) fetchIpapiType(ctx context.Context) (usage, company *string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	body, err := q.httpGetBody(ctx, urlIpapi+q.ip)
	if err != nil {
		return nil, nil
	}

	var data struct {
		UsageType string `json:"usage_type"`
		Company   string `json:"company"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, nil
	}
	if data.UsageType != "" {
		usage = &data.UsageType
	}
	if data.Company != "" {
		company = &data.Company
	}
	return
}

func (r TypeResult) ToJSON() Type {
	return Type(r)
}
