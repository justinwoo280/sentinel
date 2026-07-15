package geo

import (
	"strings"
	"testing"
)

func TestLoadUAs(t *testing.T) {
	uas, err := LoadUAs()
	if err != nil {
		t.Fatal(err)
	}
	if len(uas) == 0 {
		t.Fatal("expected non-empty UA pool")
	}
}

func TestLoadKeywords(t *testing.T) {
	kw, err := LoadKeywords("JP")
	if err != nil {
		t.Fatal(err)
	}
	if len(kw) == 0 {
		t.Fatal("expected non-empty keyword list for JP")
	}
}

func TestLoadKeywordsMissing(t *testing.T) {
	_, err := LoadKeywords("ZZZ")
	if err == nil {
		t.Fatal("expected error for missing region keywords")
	}
}

func TestLoadCityRegion(t *testing.T) {
	cr, err := LoadCityRegion("JP", "Default", "Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	if len(cr.TrustModule.WhiteURLs) == 0 {
		t.Fatal("expected non-empty white URLs for JP/Default/Tokyo")
	}
	if cr.GoogleModule.BaseLat == 0 || cr.GoogleModule.BaseLon == 0 {
		t.Fatal("expected non-zero base coordinates")
	}
	if cr.GoogleModule.LangParams == "" {
		t.Fatal("expected non-empty lang_params")
	}
	if !strings.Contains(cr.GoogleModule.LangParams, "gl=") {
		t.Errorf("lang_params should include gl=, got %q", cr.GoogleModule.LangParams)
	}
}

func TestLoadCityRegionMissing(t *testing.T) {
	if _, err := LoadCityRegion("ZZ", "Default", "Nowhere"); err == nil {
		t.Fatal("expected error for missing city region")
	}
}

func TestLoadMap(t *testing.T) {
	m, err := LoadMap()
	if err != nil {
		t.Fatal(err)
	}
	if m.Version == "" {
		t.Fatal("expected non-empty version")
	}
	if len(m.Continents) == 0 {
		t.Fatal("expected non-empty continents")
	}
}

// TestAllCountriesHaveKeywords verifies every country in map.json has a
// non-empty keyword file.
func TestAllCountriesHaveKeywords(t *testing.T) {
	m, err := LoadMap()
	if err != nil {
		t.Fatal(err)
	}
	for _, cont := range m.Continents {
		for _, country := range cont.Countries {
			kw, err := LoadKeywords(country.ID)
			if err != nil {
				t.Errorf("country %s: missing keyword file: %v", country.ID, err)
				continue
			}
			if len(kw) < 3 {
				t.Errorf("country %s: keyword list too short (%d entries)", country.ID, len(kw))
			}
		}
	}
}

// TestAllCitiesHaveRegionData is the guardrail for the 4-level region
// data migration: every single country/state/city path referenced by
// map.json MUST resolve to a valid, fully-populated city region JSON.
// This walks the entire embedded tree (66 cities as of writing) so any
// missing file, bad path, or malformed JSON fails the build immediately
// instead of surfacing as a silent runtime "N/A".
func TestAllCitiesHaveRegionData(t *testing.T) {
	m, err := LoadMap()
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, cont := range m.Continents {
		for _, country := range cont.Countries {
			for _, state := range country.States {
				for _, city := range state.Cities {
					total++
					cr, err := LoadCityRegion(country.ID, state.ID, city.ID)
					if err != nil {
						t.Errorf("%s/%s/%s: %v", country.ID, state.ID, city.ID, err)
						continue
					}
					if cr.RegionName == "" {
						t.Errorf("%s/%s/%s: empty region_name", country.ID, state.ID, city.ID)
					}
					if cr.GoogleModule.BaseLat == 0 && cr.GoogleModule.BaseLon == 0 {
						t.Errorf("%s/%s/%s: zero coordinates", country.ID, state.ID, city.ID)
					}
					if cr.GoogleModule.LangParams == "" {
						t.Errorf("%s/%s/%s: empty lang_params", country.ID, state.ID, city.ID)
					}
					if len(cr.TrustModule.WhiteURLs) == 0 {
						t.Errorf("%s/%s/%s: no trust white_urls", country.ID, state.ID, city.ID)
					}
				}
			}
		}
	}
	if total == 0 {
		t.Fatal("map.json has no cities at all — test is not exercising anything")
	}
	t.Logf("verified %d city region files", total)
}

func TestSplitLines(t *testing.T) {
	out := splitLines([]byte("  a\nb\n\n  c  \n"))
	want := []string{"a", "b", "c"}
	if len(out) != len(want) {
		t.Fatalf("got %v, want %v", out, want)
	}
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("out[%d]=%q, want %q", i, out[i], want[i])
		}
	}
}
