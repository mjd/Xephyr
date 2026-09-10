package main

import (
	"strings"
	"testing"
)

func TestLookupAirport(t *testing.T) {
	cases := []struct {
		code       string
		wantName   string
		wantRegion string
		wantCC     string
	}{
		{"LHR", "London", "United Kingdom", "GB"},
		{"lhr", "London", "United Kingdom", "GB"},   // codes are case-insensitive
		{" LHR ", "London", "United Kingdom", "GB"}, // and survive stray spaces
		{"DEN", "Denver", "Colorado", "US"},         // US airports carry a state
		{"NRT", "Narita", "Japan", "JP"},
		{"SYD", "Sydney", "Australia", "AU"}, // parenthesised district dropped
		{"JFK", "New York", "New York", "US"},
	}
	for _, tc := range cases {
		found, ok := lookupAirport(tc.code)
		if !ok {
			t.Errorf("lookupAirport(%q) found nothing", tc.code)
			continue
		}
		if found.Name != tc.wantName || found.Region != tc.wantRegion || found.CountryCode != tc.wantCC {
			t.Errorf("lookupAirport(%q) = %q/%q/%q, want %q/%q/%q",
				tc.code, found.Name, found.Region, found.CountryCode,
				tc.wantName, tc.wantRegion, tc.wantCC)
		}
	}
}

// weatherapi.com needed an "iata:" prefix, so anyone in the habit of typing it
// should not be punished for the switch.
func TestLookupAirport_AcceptsIATAPrefix(t *testing.T) {
	for _, code := range []string{"iata:LHR", "IATA:lhr", "iata: LHR"} {
		found, ok := lookupAirport(code)
		if !ok || found.Name != "London" {
			t.Errorf("lookupAirport(%q) = (%q, %v), want London", code, found.Name, ok)
		}
	}
}

// Metropolitan codes name a city, not a field, so they are absent from the
// source data and have to be redirected by hand.
func TestLookupAirport_MetroCodes(t *testing.T) {
	cases := map[string]string{
		"NYC": "New York",
		"LON": "London",
		"PAR": "Paris",
		"TYO": "Tokyo",
		"CHI": "Chicago",
		"ROM": "Rome",
	}
	for code, wantName := range cases {
		found, ok := lookupAirport(code)
		if !ok {
			t.Errorf("lookupAirport(%q) found nothing", code)
			continue
		}
		if !strings.EqualFold(found.Name, wantName) {
			t.Errorf("lookupAirport(%q) = %q, want %q", code, found.Name, wantName)
		}
	}
}

func TestLookupAirport_Rejects(t *testing.T) {
	// Anything that is not exactly three letters has to fall through to the
	// geocoder, or ordinary place names would start resolving to airports.
	for _, code := range []string{"", "ZZ", "ZZZZ", "denver", "london", "80202", "D3N", "  ", "iata:"} {
		if found, ok := lookupAirport(code); ok {
			t.Errorf("lookupAirport(%q) matched %q, want no match", code, found.Name)
		}
	}
}

func TestLookupAirport_UnknownCode(t *testing.T) {
	if _, ok := lookupAirport("ZZZ"); ok {
		t.Error("lookupAirport(ZZZ) matched an airport")
	}
}

// A truncated or mis-parsed table would still let most tests pass, so check the
// embedded data actually loaded.
func TestAirportTableLoaded(t *testing.T) {
	loaded := loadAirports()
	if len(loaded) < 4000 {
		t.Fatalf("airport table has %d entries, want the full dataset", len(loaded))
	}
	for _, code := range []string{"LHR", "JFK", "DEN", "NRT", "SYD", "CDG", "DXB", "GRU", "JNB", "ICN"} {
		if _, ok := loaded[code]; !ok {
			t.Errorf("airport table is missing %s", code)
		}
	}
	for code, found := range loaded {
		if len(code) != 3 || !isAlphaUpper(code) {
			t.Errorf("airport table has a malformed code %q", code)
			break
		}
		if found.Name == "" || found.Region == "" || found.CountryCode == "" {
			t.Errorf("airport %s has an empty field: %+v", code, found)
			break
		}
		if found.Latitude == 0 && found.Longitude == 0 {
			t.Errorf("airport %s has null island coordinates", code)
			break
		}
	}
}

func TestAirportPlace_RegionPlacement(t *testing.T) {
	us := airport{Name: "Denver", Region: "Colorado", CountryCode: "US"}.place()
	if us.Admin1 != "Colorado" || us.Country != "" {
		t.Errorf("US airport rendered as %+v, want the region in Admin1", us)
	}

	other := airport{Name: "London", Region: "United Kingdom", CountryCode: "GB"}.place()
	if other.Country != "United Kingdom" || other.Admin1 != "" {
		t.Errorf("non-US airport rendered as %+v, want the region in Country", other)
	}
}
