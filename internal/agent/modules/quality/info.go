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
	r, ok := parseIPinfoBody(body)
	if !ok {
		return false
	}
	*result = mergeInfo(*result, r)
	return true
}

// parseIPinfoBody parses an IPinfo response (demo widget or plain API) into
// an InfoResult. The demo widget wraps fields under a "data" object and
// represents "asn" as an object; the plain API is flat. Extracted as a
// pure function for testability.
func parseIPinfoBody(body []byte) (InfoResult, bool) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return InfoResult{}, false
	}
	data := raw
	if inner, ok := raw["data"].(map[string]any); ok {
		data = inner
	}

	var result InfoResult
	// "asn" may be an object {asn,name,domain,...} (widget) or a string.
	switch asn := data["asn"].(type) {
	case map[string]any:
		result.IPinfoASN = str(asn["asn"])
		result.IPinfoOrg = str(asn["name"])
	default:
		result.IPinfoASN = str(data["asn"])
	}
	if result.IPinfoOrg == "" {
		result.IPinfoOrg = str(data["org"])
	}
	if loc, ok := data["loc"].(string); ok {
		parts := strings.SplitN(loc, ",", 2)
		if len(parts) == 2 {
			result.IPinfoLat = parts[0]
			result.IPinfoLon = parts[1]
		}
	}
	result.IPinfoCity = str(data["city"])
	result.IPinfoRegion = str(data["region"])
	// The widget uses "country" for the ISO code (e.g. "SG"); the plain
	// API may use "country_code".
	cc := str(data["country_code"])
	if cc == "" {
		cc = str(data["country"])
	}
	result.IPinfoCountryCode = cc
	result.IPinfoCountry = cc
	if cc != "" {
		result.IPinfoContinent = continentName(cc)
	}
	result.IPinfoTimezone = str(data["timezone"])
	result.IPinfoPostal = str(data["postal"])
	return result, true
}

// mergeInfo copies IPinfo fields from src into dst (dst may already hold
// GeoIP data). Only the IPinfo* fields are overwritten.
func mergeInfo(dst, src InfoResult) InfoResult {
	dst.IPinfoASN = src.IPinfoASN
	dst.IPinfoOrg = src.IPinfoOrg
	dst.IPinfoLat = src.IPinfoLat
	dst.IPinfoLon = src.IPinfoLon
	dst.IPinfoCity = src.IPinfoCity
	dst.IPinfoRegion = src.IPinfoRegion
	dst.IPinfoCountry = src.IPinfoCountry
	dst.IPinfoCountryCode = src.IPinfoCountryCode
	dst.IPinfoContinent = src.IPinfoContinent
	dst.IPinfoTimezone = src.IPinfoTimezone
	dst.IPinfoPostal = src.IPinfoPostal
	return dst
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
		Asn struct {
			ASN   string `json:"asn"`
			Org   string `json:"org"`
			Descr string `json:"descr"`
		} `json:"asn"`
		Company struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"company"`
		Datacenter struct {
			Datacenter string `json:"datacenter"`
		} `json:"datacenter"`
		IsDatacenter *bool `json:"is_datacenter"`
		IsVpn        *bool `json:"is_vpn"`
		IsTor        *bool `json:"is_tor"`
		IsProxy      *bool `json:"is_proxy"`
		IsAbuser     *bool `json:"is_abuser"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		q.log.Debug("ipapi decode failed", "err", err)
		return false
	}

	// ASN
	result.IpapiASN = data.Asn.ASN
	// Organization: prefer company name, fall back to ASN org/descr.
	result.IpapiOrg = data.Company.Name
	if result.IpapiOrg == "" {
		result.IpapiOrg = data.Asn.Org
	}
	if result.IpapiOrg == "" {
		result.IpapiOrg = data.Asn.Descr
	}
	result.IpapiCompany = data.Company.Name
	// Usage type: company type (e.g. "hosting"), or datacenter name.
	result.IpapiUsage = data.Company.Type
	if result.IpapiUsage == "" && data.IsDatacenter != nil && *data.IsDatacenter {
		result.IpapiUsage = "hosting"
	}

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
		// ASN/Org fall back to ipapi.is when IPinfo lacks them.
		asn := r.IPinfoASN
		if asn == "" {
			asn = r.IpapiASN
		}
		org := r.IPinfoOrg
		if org == "" {
			org = r.IpapiOrg
		}
		info.ASN = s(asn)
		info.Organization = s(org)
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
