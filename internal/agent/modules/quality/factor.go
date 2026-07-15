package quality

import (
	"context"
	"encoding/json"
	"io"
	"time"
)

// FactorResult holds the risk factor booleans from 9 sources.
type FactorResult struct {
	CountryCode map[string]*string
	Proxy       map[string]*string
	Tor         map[string]*string
	VPN         map[string]*string
	Server      map[string]*string
	Abuser      map[string]*string
	Robot       map[string]*string
}

// Source names for the factor matrix.
const (
	srcIP2Location = "IP2LOCATION"
	srcIpapi       = "ipapi"
	srcIPRegistry  = "ipregistry"
	srcIPQS        = "IPQS"
	srcScamalytics = "SCAMALYTICS"
	srcIPData      = "ipdata"
	srcIPInfo      = "IPinfo"
	srcIPWHOIS     = "IPWHOIS"
	srcDBIP        = "DBIP"
)

func newFactorMap() map[string]*string {
	return map[string]*string{
		srcIP2Location: nil,
		srcIpapi:       nil,
		srcIPRegistry:  nil,
		srcIPQS:        nil,
		srcScamalytics: nil,
		srcIPData:      nil,
		srcIPInfo:      nil,
		srcIPWHOIS:     nil,
		srcDBIP:        nil,
	}
}

func (q *Quality) queryFactor(ctx context.Context) FactorResult {
	result := FactorResult{
		CountryCode: newFactorMap(),
		Proxy:       newFactorMap(),
		Tor:         newFactorMap(),
		VPN:         newFactorMap(),
		Server:      newFactorMap(),
		Abuser:      newFactorMap(),
		Robot:       newFactorMap(),
	}

	// ipapi.is (free).
	q.fetchIpapiFactors(ctx, &result)

	// Scamalytics.
	if _, proxy, vpn, tor, server, _, robot, cc, _ := q.fetchScamalytics(ctx); true {
		if proxy != nil {
			result.Proxy[srcScamalytics] = proxy
		}
		if vpn != nil {
			result.VPN[srcScamalytics] = vpn
		}
		if tor != nil {
			result.Tor[srcScamalytics] = tor
		}
		if server != nil {
			result.Server[srcScamalytics] = server
		}
		if robot != nil {
			result.Robot[srcScamalytics] = robot
		}
		if cc != nil {
			result.CountryCode[srcScamalytics] = cc
		}
	}

	// IP2Location.
	if _, proxy, vpn, tor, server, abuser, robot, cc, _ := q.fetchIP2Location(ctx); true {
		if proxy != nil {
			result.Proxy[srcIP2Location] = proxy
		}
		if vpn != nil {
			result.VPN[srcIP2Location] = vpn
		}
		if tor != nil {
			result.Tor[srcIP2Location] = tor
		}
		if server != nil {
			result.Server[srcIP2Location] = server
		}
		if abuser != nil {
			result.Abuser[srcIP2Location] = abuser
		}
		if robot != nil {
			result.Robot[srcIP2Location] = robot
		}
		if cc != nil {
			result.CountryCode[srcIP2Location] = cc
		}
	}

	// IPQS.
	if _, proxy, vpn, tor, server, abuser, robot, cc, _ := q.fetchIPQS(ctx); true {
		if proxy != nil {
			result.Proxy[srcIPQS] = proxy
		}
		if vpn != nil {
			result.VPN[srcIPQS] = vpn
		}
		if tor != nil {
			result.Tor[srcIPQS] = tor
		}
		if server != nil {
			result.Server[srcIPQS] = server
		}
		if abuser != nil {
			result.Abuser[srcIPQS] = abuser
		}
		if robot != nil {
			result.Robot[srcIPQS] = robot
		}
		if cc != nil {
			result.CountryCode[srcIPQS] = cc
		}
	}

	// ipdata.
	if proxy, vpn, tor, server, abuser, robot, cc := q.fetchIPData(ctx); true {
		if proxy != nil {
			result.Proxy[srcIPData] = proxy
		}
		if vpn != nil {
			result.VPN[srcIPData] = vpn
		}
		if tor != nil {
			result.Tor[srcIPData] = tor
		}
		if server != nil {
			result.Server[srcIPData] = server
		}
		if abuser != nil {
			result.Abuser[srcIPData] = abuser
		}
		if robot != nil {
			result.Robot[srcIPData] = robot
		}
		if cc != nil {
			result.CountryCode[srcIPData] = cc
		}
	}

	// IPinfo (with key).
	if usageType, _, proxy, cc, privacy := q.fetchIPinfoFull(ctx); true {
		if proxy != nil {
			result.Proxy[srcIPInfo] = proxy
		}
		if cc != nil {
			result.CountryCode[srcIPInfo] = cc
		}
		if privacy != nil {
			result.VPN[srcIPInfo] = boolStr(privacy.VPN)
			result.Tor[srcIPInfo] = boolStr(privacy.Tor)
			result.Server[srcIPInfo] = boolStr(privacy.Hosting)
		}
		_ = usageType
	}

	return result
}

func (q *Quality) fetchIpapiFactors(ctx context.Context, result *FactorResult) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := q.httpGet(ctx, urlIpapi+q.ip)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data struct {
		IsDatacenter *bool `json:"is_datacenter"`
		IsVpn        *bool `json:"is_vpn"`
		IsTor        *bool `json:"is_tor"`
		IsProxy      *bool `json:"is_proxy"`
		IsAbuser     *bool `json:"is_abuser"`
		IsCrawler    *bool `json:"is_crawler"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return
	}

	if data.IsProxy != nil {
		result.Proxy[srcIpapi] = boolStr(*data.IsProxy)
	}
	if data.IsVpn != nil {
		result.VPN[srcIpapi] = boolStr(*data.IsVpn)
	}
	if data.IsTor != nil {
		result.Tor[srcIpapi] = boolStr(*data.IsTor)
	}
	if data.IsDatacenter != nil {
		result.Server[srcIpapi] = boolStr(*data.IsDatacenter)
	}
	if data.IsAbuser != nil {
		result.Abuser[srcIpapi] = boolStr(*data.IsAbuser)
	}
	if data.IsCrawler != nil {
		result.Robot[srcIpapi] = boolStr(*data.IsCrawler)
	}
}

func boolStr(b bool) *string {
	if b {
		s := "true"
		return &s
	}
	s := "false"
	return &s
}

func (r FactorResult) ToJSON() Factor {
	return Factor(r)
}
