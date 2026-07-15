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

// RegionData contains per-region configuration.
type RegionData struct {
	TrustModule TrustModule `json:"trust_module"`
}

type TrustModule struct {
	WhiteURLs []string `json:"white_urls"`
}

// LoadRegion reads the region JSON for the given code.
func LoadRegion(regionCode string) (*RegionData, error) {
	raw, err := data.FS.ReadFile("regions/" + regionCode + ".json")
	if err != nil {
		return nil, fmt.Errorf("geo: region %s: %w", regionCode, err)
	}
	var rd RegionData
	if err := json.Unmarshal(raw, &rd); err != nil {
		return nil, fmt.Errorf("geo: parse region %s: %w", regionCode, err)
	}
	return &rd, nil
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
