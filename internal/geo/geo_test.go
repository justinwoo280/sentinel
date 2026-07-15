package geo

import (
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

func TestLoadRegion(t *testing.T) {
	rd, err := LoadRegion("JP")
	if err != nil {
		t.Fatal(err)
	}
	if len(rd.TrustModule.WhiteURLs) == 0 {
		t.Fatal("expected non-empty white URLs for JP")
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

func TestAllCountriesHaveData(t *testing.T) {
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

			rd, err := LoadRegion(country.ID)
			if err != nil {
				t.Errorf("country %s: missing region JSON: %v", country.ID, err)
				continue
			}
			if len(rd.TrustModule.WhiteURLs) == 0 {
				t.Errorf("country %s: region JSON has no white_urls", country.ID)
			}
		}
	}
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
