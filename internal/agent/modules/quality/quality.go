// Package quality implements the IP quality check module (DESIGN.md §5.4):
// a 1:1 Go-native reimplementation of xykt/IPQuality's six detection
// modules. Output JSON schema matches xykt's output.json exactly.
package quality

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Quality is the IP quality check module. It runs all six detection
// modules concurrently and assembles the output JSON + Markdown report.
type Quality struct {
	ip     string
	ua     string
	bindIP string
	ipPref int
	log    *slog.Logger
	http   *http.Client
	keys   APIKeys
	geoip  GeoIPLookup
}

// GeoIPLookup is the interface for GeoIP lookups (provided by geoip.Manager).
type GeoIPLookup interface {
	Lookup(ip string) *GeoIPResult
}

// GeoIPResult is a subset of geoip.LookupResult local to this package
// to avoid a circular dependency. ASN/Organization are only populated
// when the local GeoLite2-ASN database is available; a zero ASN means
// the caller should fall back to a remote source (IPinfo/ipapi.is).
type GeoIPResult struct {
	CountryCode   string
	CountryName   string
	CityName      string
	Latitude      float64
	Longitude     float64
	TimeZone      string
	Continent     string
	ContinentCode string
	PostalCode    string
	ASN           uint
	Organization  string
}

// APIKeys holds optional API keys for commercial data sources. Sources
// without keys degrade gracefully (return null/empty).
type APIKeys struct {
	Scamalytics string `yaml:"scamalytics"`
	AbuseIPDB   string `yaml:"abuseipdb"`
	IP2Location string `yaml:"ip2location"`
	IPQS        string `yaml:"ipqs"`
	IPData      string `yaml:"ipdata"`
	IPInfo      string `yaml:"ipinfo"`
}

// New creates a Quality module for the given IP. The http.Client is
// configured to bind to bindIP and prefer ipPref (4/6).
func New(ip, bindIP string, ipPref int, ua string, keys APIKeys, log *slog.Logger) *Quality {
	return newQuality(ip, bindIP, ipPref, ua, keys, log, nil)
}

// NewWithGeoIP creates a Quality module with a GeoIP lookup for the
// Info module (uses local Maxmind mmdb instead of API calls).
func NewWithGeoIP(ip, bindIP string, ipPref int, ua string, keys APIKeys, log *slog.Logger, gl GeoIPLookup) *Quality {
	return newQuality(ip, bindIP, ipPref, ua, keys, log, gl)
}

func newQuality(ip, bindIP string, ipPref int, ua string, keys APIKeys, log *slog.Logger, gl GeoIPLookup) *Quality {
	if log == nil {
		log = slog.Default()
	}
	if ua == "" {
		ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36"
	}
	return &Quality{
		ip:     ip,
		ua:     ua,
		bindIP: bindIP,
		ipPref: ipPref,
		log:    log,
		keys:   keys,
		http:   newHTTPClient(bindIP, ipPref, 10*time.Second),
		geoip:  gl,
	}
}

// Result is the complete output matching xykt's JSON schema.
type Result struct {
	Head   Head   `json:"Head"`
	Info   Info   `json:"Info"`
	Type   Type   `json:"Type"`
	Score  Score  `json:"Score"`
	Factor Factor `json:"Factor"`
	Media  Media  `json:"Media"`
	Mail   Mail   `json:"Mail"`
}

// Run executes all six detection modules concurrently and returns the
// assembled result + Markdown report.
func (q *Quality) Run(ctx context.Context) (*Result, string, error) {
	if q.ip == "" {
		return nil, "", fmt.Errorf("quality: no IP to check")
	}

	// Concurrent data source queries.
	var (
		info   InfoResult
		typ    TypeResult
		score  ScoreResult
		factor FactorResult
		media  MediaResult
		mail   MailResult
	)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() { defer wg.Done(); info = q.queryInfo(ctx) }()

	wg.Add(1)
	go func() { defer wg.Done(); typ = q.queryType(ctx) }()

	wg.Add(1)
	go func() { defer wg.Done(); score = q.queryScore(ctx) }()

	wg.Add(1)
	go func() { defer wg.Done(); factor = q.queryFactor(ctx) }()

	wg.Add(1)
	go func() { defer wg.Done(); media = q.queryMedia(ctx) }()

	wg.Add(1)
	go func() { defer wg.Done(); mail = q.queryMail(ctx) }()

	wg.Wait()

	result := &Result{
		Head: Head{
			IP:      q.ip,
			Time:    time.Now().UTC().Format("2006-01-02 15:04:05 MST"),
			Version: "sentinel-go",
		},
		Info:   info.ToJSON(),
		Type:   typ.ToJSON(),
		Score:  score.ToJSON(),
		Factor: factor.ToJSON(),
		Media:  media.ToJSON(),
		Mail:   mail.ToJSON(),
	}

	report := assembleReport(result)
	return result, report, nil
}

// JSON returns the marshalled JSON output.
func (r *Result) JSON() string {
	data, _ := json.MarshalIndent(r, "", "  ")
	return string(data)
}
