package install

import (
	"testing"

	"github.com/justinwoo280/sentinel/internal/geo"
)

func TestParseChoiceValid(t *testing.T) {
	tests := []struct {
		raw       string
		max       int
		allowBack bool
		want      int
	}{
		{"1", 5, false, 1},
		{"5", 5, false, 5},
		{"3", 9, true, 3},
		{"", 9, false, 1},      // empty defaults to 1
		{"  2  ", 9, false, 2}, // whitespace trimmed
		{"0", 9, true, 0},      // back
	}
	for _, tt := range tests {
		got, err := parseChoice(tt.raw, tt.max, tt.allowBack)
		if err != nil {
			t.Errorf("parseChoice(%q, %d, %v): unexpected error: %v", tt.raw, tt.max, tt.allowBack, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseChoice(%q, %d, %v) = %d, want %d", tt.raw, tt.max, tt.allowBack, got, tt.want)
		}
	}
}

func TestParseChoiceInvalid(t *testing.T) {
	tests := []struct {
		raw       string
		max       int
		allowBack bool
	}{
		{"0", 5, false},   // 0 not allowed when allowBack=false
		{"-1", 5, true},   // negative
		{"6", 5, true},    // above max
		{"abc", 5, false}, // not a number
		{"1.5", 5, false}, // not an integer
	}
	for _, tt := range tests {
		if _, err := parseChoice(tt.raw, tt.max, tt.allowBack); err == nil {
			t.Errorf("parseChoice(%q, %d, %v): expected error, got nil", tt.raw, tt.max, tt.allowBack)
		}
	}
}

// TestAllRegionsResolveToCityData is a regression guardrail for the
// 4-level region migration: every city reachable via map.json must load
// a valid CityRegion (base_lat/base_lon/lang_params/white_urls), exactly
// mirroring what selectRegion() verifies interactively.
func TestAllRegionsResolveToCityData(t *testing.T) {
	m, err := geo.LoadMap()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, cont := range m.Continents {
		for _, country := range cont.Countries {
			for _, state := range country.States {
				for _, city := range state.Cities {
					count++
					if _, err := geo.LoadCityRegion(country.ID, state.ID, city.ID); err != nil {
						t.Errorf("%s/%s/%s: %v", country.ID, state.ID, city.ID, err)
					}
				}
			}
		}
	}
	if count == 0 {
		t.Fatal("no cities found in map data — test isn't exercising anything")
	}
}
