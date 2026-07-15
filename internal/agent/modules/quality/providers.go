package quality

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"
)

// --- Scamalytics ---

func (q *Quality) fetchScamalytics(ctx context.Context) (score *string, proxy, vpn, tor, server, abuser, robot *string, countryCode *string, usageType *string) {
	if q.keys.Scamalytics == "" {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := urlScamalytics + q.keys.Scamalytics + "/" + q.ip
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", q.ua)
	resp, err := q.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data struct {
		Score   int    `json:"score"`
		Risk    string `json:"risk"`
		Proxy   string `json:"proxy"`
		VPN     string `json:"vpn"`
		Tor     string `json:"tor"`
		Server  string `json:"host"`
		Country string `json:"country"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return
	}

	s := strconv.Itoa(data.Score)
	score = &s
	proxy = riskStr(data.Proxy)
	vpn = riskStr(data.VPN)
	tor = riskStr(data.Tor)
	server = riskStr(data.Server)
	if data.Country != "" {
		countryCode = &data.Country
	}
	return
}

// --- AbuseIPDB ---

func (q *Quality) fetchAbuseIPDB(ctx context.Context) (score *string, usageType *string, abuser *string) {
	if q.keys.AbuseIPDB == "" {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", urlAbuseIPDB, nil)
	qv := req.URL.Query()
	qv.Set("ipAddress", q.ip)
	qv.Set("maxAgeInDays", "90")
	req.URL.RawQuery = qv.Encode()
	req.Header.Set("Key", q.keys.AbuseIPDB)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", q.ua)

	resp, err := q.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data struct {
		Data struct {
			AbuseConfidenceScore int    `json:"abuseConfidenceScore"`
			UsageType            string `json:"usageType"`
			IsPublic             bool   `json:"isPublic"`
			TotalReports         int    `json:"totalReports"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return
	}

	s := strconv.Itoa(data.Data.AbuseConfidenceScore)
	score = &s
	if data.Data.UsageType != "" {
		usageType = &data.Data.UsageType
	}
	if data.Data.TotalReports > 0 {
		b := "true"
		abuser = &b
	} else {
		b := "false"
		abuser = &b
	}
	return
}

// --- IP2Location ---

func (q *Quality) fetchIP2Location(ctx context.Context) (score *string, proxy, vpn, tor, server, abuser, robot *string, countryCode *string, usageType *string) {
	if q.keys.IP2Location == "" {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", urlIP2Location, nil)
	qv := req.URL.Query()
	qv.Set("key", q.keys.IP2Location)
	qv.Set("ip", q.ip)
	req.URL.RawQuery = qv.Encode()
	req.Header.Set("User-Agent", q.ua)

	resp, err := q.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data struct {
		Proxies struct {
			Proxy  string `json:"proxy"`
			VPN    string `json:"vpn"`
			Tor    string `json:"tor"`
			Server string `json:"server"`
			Abuser string `json:"abuser"`
			Robot  string `json:"robot"`
		} `json:"proxies"`
		Threat struct {
			Score int `json:"score"`
		} `json:"threat"`
		CountryCode string `json:"country_code"`
		UsageType   string `json:"usage_type"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return
	}

	s := strconv.Itoa(data.Threat.Score)
	score = &s
	proxy = riskStr(data.Proxies.Proxy)
	vpn = riskStr(data.Proxies.VPN)
	tor = riskStr(data.Proxies.Tor)
	server = riskStr(data.Proxies.Server)
	abuser = riskStr(data.Proxies.Abuser)
	robot = riskStr(data.Proxies.Robot)
	if data.CountryCode != "" {
		countryCode = &data.CountryCode
	}
	if data.UsageType != "" {
		usageType = &data.UsageType
	}
	return
}

// --- IPQS ---

func (q *Quality) fetchIPQS(ctx context.Context) (score *string, proxy, vpn, tor, server, abuser, robot *string, countryCode *string, usageType *string) {
	if q.keys.IPQS == "" {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := urlIPQS + q.keys.IPQS + "/" + q.ip
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", q.ua)

	resp, err := q.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data struct {
		FraudScore     int    `json:"fraud_score"`
		Proxy          string `json:"proxy"`
		VPN            string `json:"vpn"`
		Tor            string `json:"tor"`
		IsCrawler      bool   `json:"is_crawler"`
		IsServer       bool   `json:"is_server"`
		RecentAbuse    bool   `json:"recent_abuse"`
		Country        string `json:"country_code"`
		ConnectionType string `json:"connection_type"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return
	}

	s := strconv.Itoa(data.FraudScore)
	score = &s
	proxy = riskStr(data.Proxy)
	vpn = riskStr(data.VPN)
	tor = riskStr(data.Tor)
	server = boolStr(data.IsServer)
	abuser = boolStr(data.RecentAbuse)
	robot = boolStr(data.IsCrawler)
	if data.Country != "" {
		countryCode = &data.Country
	}
	if data.ConnectionType != "" {
		usageType = &data.ConnectionType
	}
	return
}

// --- ipdata ---

func (q *Quality) fetchIPData(ctx context.Context) (proxy, vpn, tor, server, abuser, robot *string, countryCode *string) {
	if q.keys.IPData == "" {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := urlIPData + q.ip + "?api-key=" + q.keys.IPData
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", q.ua)

	resp, err := q.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data struct {
		Threat struct {
			IsTor           bool `json:"is_tor"`
			IsProxy         bool `json:"is_proxy"`
			IsVPN           bool `json:"is_vpn"`
			IsDatacenter    bool `json:"is_datacenter"`
			IsBogon         bool `json:"is_bogon"`
			IsAnonymous     bool `json:"is_anonymous"`
			IsKnownAbuser   bool `json:"is_known_abuser"`
			IsKnownAttacker bool `json:"is_known_attacker"`
			IsBot           bool `json:"is_bot"`
		} `json:"threat"`
		CountryCode string `json:"country_code"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return
	}

	proxy = boolStr(data.Threat.IsProxy)
	vpn = boolStr(data.Threat.IsVPN)
	tor = boolStr(data.Threat.IsTor)
	server = boolStr(data.Threat.IsDatacenter)
	abuser = boolStr(data.Threat.IsKnownAbuser)
	robot = boolStr(data.Threat.IsBot)
	if data.CountryCode != "" {
		countryCode = &data.CountryCode
	}
	return
}

// --- IPinfo (with key, full data) ---

func (q *Quality) fetchIPinfoFull(ctx context.Context) (usageType, companyType *string, proxy *string, countryCode *string, privacy *struct {
	VPN     bool `json:"vpn"`
	Proxy   bool `json:"proxy"`
	Tor     bool `json:"tor"`
	Hosting bool `json:"hosting"`
}) {
	if q.keys.IPInfo == "" {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := urlIPinfoAPI + q.ip + "?token=" + q.keys.IPInfo
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", q.ua)
	resp, err := q.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data struct {
		Org     string `json:"org"`
		Country string `json:"country"`
		Privacy struct {
			VPN     bool `json:"vpn"`
			Proxy   bool `json:"proxy"`
			Tor     bool `json:"tor"`
			Hosting bool `json:"hosting"`
		} `json:"privacy"`
		Abuse struct {
			UsageType string `json:"usage_type"`
		} `json:"abuse"`
		Company struct {
			Type string `json:"type"`
		} `json:"company"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return
	}

	if data.Abuse.UsageType != "" {
		usageType = &data.Abuse.UsageType
	}
	if data.Company.Type != "" {
		companyType = &data.Company.Type
	}
	proxy = boolStr(data.Privacy.Proxy)
	if data.Country != "" {
		countryCode = &data.Country
	}
	p := data.Privacy
	privacy = &p
	return
}

// --- Helpers ---

// riskStr converts a risk string ("YES"/"NO"/"") to a bool pointer.
func riskStr(s string) *string {
	switch s {
	case "YES", "yes", "true", "1":
		b := "true"
		return &b
	case "NO", "no", "false", "0":
		b := "false"
		return &b
	}
	return nil
}
