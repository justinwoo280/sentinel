package quality

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// InfoResult holds the raw data from info providers before JSON assembly.
type InfoResult struct {
	// IPinfo free demo
	IPinfoASN            string
	IPinfoOrg            string
	IPinfoLat            string
	IPinfoLon            string
	IPinfoCity           string
	IPinfoRegion         string
	IPinfoCountry        string
	IPinfoCountryCode    string
	IPinfoContinent      string
	IPinfoRegCountryCode string
	IPinfoRegCountry     string
	IPinfoTimezone       string
	IPinfoPostal         string

	// ipapi.is
	IpapiASN     string
	IpapiOrg     string
	IpapiCompany string
	IpapiUsage   string
	IpapiScore   string

	// Error flags
	IPinfoOK bool
	IpapiOK  bool
}

func (q *Quality) queryInfo(ctx context.Context) InfoResult {
	var result InfoResult

	// If GeoIP (local Maxmind mmdb) is available, use it as primary source.
	if q.geoip != nil {
		if gr := q.geoip.Lookup(q.ip); gr != nil {
			result.IPinfoASN = ""
			result.IPinfoOrg = ""
			result.IPinfoLat = fmt.Sprintf("%.4f", gr.Latitude)
			result.IPinfoLon = fmt.Sprintf("%.4f", gr.Longitude)
			result.IPinfoCity = gr.CityName
			result.IPinfoCountry = gr.CountryName
			result.IPinfoCountryCode = gr.CountryCode
			result.IPinfoContinent = gr.Continent
			result.IPinfoTimezone = gr.TimeZone
			result.IPinfoPostal = gr.PostalCode
			result.IPinfoRegCountryCode = gr.CountryCode // mmdb doesn't have registered country
			result.IPinfoRegCountry = gr.CountryName
			result.IPinfoOK = true

			// Also query ipapi.is for score/usage type (free, no key).
			result.IpapiOK = q.fetchIpapi(ctx, &result)
			return result
		}
	}

	// Fallback: query IPinfo (free demo widget, no key).
	result.IPinfoOK = q.fetchIPinfo(ctx, &result)

	// Query ipapi.is (free, no key).
	result.IpapiOK = q.fetchIpapi(ctx, &result)

	return result
}

func (q *Quality) fetchIPinfo(ctx context.Context, result *InfoResult) bool {
	resp, err := q.httpGet(ctx, urlIPinfo+q.ip)
	if err != nil {
		q.log.Debug("ipinfo fetch failed", "err", err)
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return false
	}

	result.IPinfoASN = str(data["asn"])
	result.IPinfoOrg = str(data["org"])
	if loc, ok := data["loc"].(string); ok {
		parts := strings.SplitN(loc, ",", 2)
		if len(parts) == 2 {
			result.IPinfoLat = parts[0]
			result.IPinfoLon = parts[1]
		}
	}
	result.IPinfoCity = str(data["city"])
	result.IPinfoRegion = str(data["region"])
	result.IPinfoCountry = str(data["country"])
	result.IPinfoCountryCode = str(data["country_code"])
	if cc := result.IPinfoCountryCode; cc != "" {
		result.IPinfoContinent = continentName(cc)
	}
	result.IPinfoTimezone = str(data["timezone"])
	result.IPinfoPostal = str(data["postal"])

	return true
}

func (q *Quality) fetchIpapi(ctx context.Context, result *InfoResult) bool {
	resp, err := q.httpGet(ctx, urlIpapi+q.ip)
	if err != nil {
		q.log.Debug("ipapi fetch failed", "err", err)
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data struct {
		Asn          any    `json:"asn"`
		Company      string `json:"company"`
		Usage        string `json:"usage_type"`
		IsDatacenter *bool  `json:"is_datacenter"`
		IsVpn        *bool  `json:"is_vpn"`
		IsTor        *bool  `json:"is_tor"`
		IsProxy      *bool  `json:"is_proxy"`
		IsAbuser     *bool  `json:"is_abuser"`
		RiskScore    int    `json:"risk_score"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return false
	}

	result.IpapiOrg = data.Company
	result.IpapiUsage = data.Usage
	result.IpapiScore = itoa(data.RiskScore)

	return true
}

func (r InfoResult) ToJSON() Info {
	info := Info{}

	if r.IPinfoOK {
		s := func(v string) *string {
			if v == "" {
				return nil
			}
			return &v
		}
		info.ASN = s(r.IPinfoASN)
		info.Organization = s(r.IPinfoOrg)
		info.Latitude = s(r.IPinfoLat)
		info.Longitude = s(r.IPinfoLon)
		info.TimeZone = s(r.IPinfoTimezone)
		info.City = CityInfo{
			Name:       s(r.IPinfoCity),
			PostalCode: s(r.IPinfoPostal),
		}
		info.Region = RegionInfo{
			Code: s(r.IPinfoCountryCode),
			Name: s(r.IPinfoCountry),
		}
		info.Continent = ContinentInfo{
			Name: s(r.IPinfoContinent),
		}
		info.RegisteredRegion = RegionInfo{
			Code: s(r.IPinfoRegCountryCode),
			Name: s(r.IPinfoRegCountry),
		}
		if r.IPinfoCountryCode != "" && r.IPinfoCountryCode == r.IPinfoRegCountryCode {
			t := "Geo-consistent"
			info.Type = &t
		} else if r.IPinfoCountryCode != "" {
			t := "Geo-discrepant"
			info.Type = &t
		}
	}

	return info
}

// --- helpers ---

func (q *Quality) httpGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", q.ua)
	req.Header.Set("Accept", "application/json, text/html, */*")
	return q.http.Do(req)
}

func str(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return itoa(int(t))
	case bool:
		if t {
			return "true"
		}
		return "false"
	}
	return ""
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func ptrStr(s string) *string { return &s }

func continentName(countryCode string) string {
	continents := map[string]string{
		"US": "North America", "CA": "North America", "MX": "North America",
		"GB": "Europe", "DE": "Europe", "FR": "Europe", "NL": "Europe",
		"JP": "Asia", "KR": "Asia", "CN": "Asia", "HK": "Asia", "SG": "Asia",
		"AU": "Oceania", "NZ": "Oceania",
		"BR": "South America", "AR": "South America",
		"ZA": "Africa", "NG": "Africa", "EG": "Africa",
	}
	if name, ok := continents[countryCode]; ok {
		return name
	}
	return ""
}
