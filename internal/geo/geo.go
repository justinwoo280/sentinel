// Package geo provides access to embedded geographic data: the global
// region topology (map.json), UA pool, per-region keyword lists, and
// region anchor data. All data is compiled into the binary via go:embed
// in the data package (DESIGN.md §1: no runtime downloads).
package geo

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/justinwoo280/sentinel/data"
)

// LoadUAs reads the embedded user-agent pool.
func LoadUAs() ([]string, error) {
	raw, err := data.FS.ReadFile("user_agents.txt")
	if err != nil {
		return nil, fmt.Errorf("geo: read user_agents: %w", err)
	}
	return splitLines(raw), nil
}

// LoadKeywords reads the keyword file for the given region code.
// e.g. regionCode="JP" → "keywords/kw_JP.txt".
func LoadKeywords(regionCode string) ([]string, error) {
	raw, err := data.FS.ReadFile("keywords/kw_" + regionCode + ".txt")
	if err != nil {
		return nil, fmt.Errorf("geo: keywords for %s: %w", regionCode, err)
	}
	return splitLines(raw), nil
}

// CityRegion is the per-city region data (data/regions/<country>/<state>/<city>.json),
// matching the original project's schema exactly:
//
//	{
//	  "region_name": "...",
//	  "google_module": {"base_lat":.., "base_lon":.., "lang_params":"hl=ja&gl=JP", "valid_url_suffix":".."},
//	  "trust_module": {"white_urls":[...], "static_urls":[...]}
//	}
//
// valid_url_suffix and static_urls are carried for schema parity with the
// original data files but are not consumed by any detection logic (the
// original project doesn't read them at runtime either).
type CityRegion struct {
	RegionName   string           `json:"region_name"`
	GoogleModule CityGoogleModule `json:"google_module"`
	TrustModule  CityTrustModule  `json:"trust_module"`
}

type CityGoogleModule struct {
	BaseLat        float64 `json:"base_lat"`
	BaseLon        float64 `json:"base_lon"`
	LangParams     string  `json:"lang_params"`
	ValidURLSuffix string  `json:"valid_url_suffix"`
}

type CityTrustModule struct {
	WhiteURLs  []string `json:"white_urls"`
	StaticURLs []string `json:"static_urls"`
}

// LoadCityRegion reads the city-level region JSON for the given
// country/state/city ID triple, e.g. ("DE", "NW", "Aachen") →
// "regions/DE/NW/Aachen.json".
func LoadCityRegion(countryID, stateID, cityID string) (*CityRegion, error) {
	path := fmt.Sprintf("regions/%s/%s/%s.json", countryID, stateID, cityID)
	raw, err := data.FS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("geo: city region %s: %w", path, err)
	}
	var cr CityRegion
	if err := json.Unmarshal(raw, &cr); err != nil {
		return nil, fmt.Errorf("geo: parse city region %s: %w", path, err)
	}
	return &cr, nil
}

// MapData is the global region topology (data/map.json).
type MapData struct {
	Version    string      `json:"version"`
	Continents []Continent `json:"continents"`
}

type Continent struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Countries []Country `json:"countries"`
}

type Country struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	KeywordFile string  `json:"keyword_file"`
	States      []State `json:"states"`
}

type State struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Cities []City `json:"cities"`
}

type City struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// LoadMap reads the embedded global map.
func LoadMap() (*MapData, error) {
	raw, err := data.FS.ReadFile("map.json")
	if err != nil {
		return nil, fmt.Errorf("geo: read map: %w", err)
	}
	var m MapData
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("geo: parse map: %w", err)
	}
	return &m, nil
}

func splitLines(raw []byte) []string {
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
