package main

import (
	"strings"
	"testing"
)

func TestLookupPostalCode(t *testing.T) {
	cases := []struct {
		code      string
		wantName  string
		wantState string
	}{
		// The code that started this: the geocoder has 81501 through 81506
		// hanging off Grand Junction but not 81507.
		{"81507", "Grand Junction", "Colorado"},
		{"81501", "Grand Junction", "Colorado"},
		{"81523", "Glade Park", "Colorado"},
		{" 81507 ", "Grand Junction", "Colorado"}, // stray spaces survive
		{"10001", "New York", "New York"},
		{"80202", "Denver", "Colorado"},
		{"96813", "Honolulu", "Hawaii"},
		{"99501", "Anchorage", "Alaska"},
		{"20500", "Washington", "District of Columbia"},
	}
	for _, tc := range cases {
		found, ok := lookupPostalCode(tc.code)
		if !ok {
			t.Errorf("lookupPostalCode(%q) found nothing", tc.code)
			continue
		}
		if found.Name != tc.wantName || found.State != tc.wantState {
			t.Errorf("lookupPostalCode(%q) = %q/%q, want %q/%q",
				tc.code, found.Name, found.State, tc.wantName, tc.wantState)
		}
	}
}

// Anything that is not five digits belongs to the geocoder, not the table.
func TestLookupPostalCode_Rejects(t *testing.T) {
	for _, code := range []string{"", "8150", "815077", "8150a", "denver", "SW1A", "-1234", "1234.5", "81 07"} {
		if found, ok := lookupPostalCode(code); ok {
			t.Errorf("lookupPostalCode(%q) = %q, want no match", code, found.Name)
		}
	}
}

// 00000 is well formed and unassigned, which is the case that separates "not a
// ZIP" from "a ZIP we do not have".
func TestLookupPostalCode_UnassignedCode(t *testing.T) {
	if found, ok := lookupPostalCode("00000"); ok {
		t.Errorf("lookupPostalCode(00000) = %q, want no match", found.Name)
	}
}

func TestPostalTableLoaded(t *testing.T) {
	table := loadPostalCodes()
	if len(table) < 40000 {
		t.Fatalf("postal table has %d entries, want at least 40000", len(table))
	}

	for code, place := range table {
		if len(code) != 5 || !isDigits(code) {
			t.Fatalf("malformed ZIP %q in table", code)
		}
		if place.Name == "" || place.State == "" {
			t.Fatalf("ZIP %q has an empty name or state: %+v", code, place)
		}
		// A row that lost its coordinates would silently report the weather
		// in the Gulf of Guinea.
		if place.Latitude == 0 && place.Longitude == 0 {
			t.Fatalf("ZIP %q sits at null island", code)
		}
	}
}

// Military routing codes name a mail facility rather than a place, so they are
// deliberately absent.
func TestPostalTable_ExcludesMilitaryCodes(t *testing.T) {
	for _, code := range []string{"09001", "09007", "34001", "96271"} {
		if found, ok := lookupPostalCode(code); ok {
			t.Errorf("lookupPostalCode(%q) = %q, want the APO/FPO codes left out", code, found.Name)
		}
	}
}

// A ZIP has to render exactly the way a geocoded US hit does, or the same city
// would print differently depending on which path resolved it.
func TestPostalPlace_MatchesGeocodedShape(t *testing.T) {
	found, ok := lookupPostalCode("81507")
	if !ok {
		t.Fatal("lookupPostalCode(81507) found nothing")
	}
	place := found.place()

	if place.Name != "Grand Junction" {
		t.Errorf("Name = %q, want Grand Junction", place.Name)
	}
	// sendWeatherRequest prints Admin1 for US places and Country elsewhere, so
	// the state has to land in Admin1.
	if place.Admin1 != "Colorado" {
		t.Errorf("Admin1 = %q, want Colorado", place.Admin1)
	}
	if place.CountryCode != "US" {
		t.Errorf("CountryCode = %q, want US", place.CountryCode)
	}
	if place.Country != "United States" {
		t.Errorf("Country = %q, want United States", place.Country)
	}
	if place.Latitude == 0 || place.Longitude == 0 {
		t.Errorf("coordinates = %v,%v, want the ZIP's own position", place.Latitude, place.Longitude)
	}
}

func TestParsePostalCodes_SkipsUnusableRows(t *testing.T) {
	parsed := parsePostalCodes(strings.Join([]string{
		"81507\tGrand Junction\tColorado\t39.0157\t-108.6129",
		"81501\tGrand Junction\tColorado",                       // too few columns
		"81502\tGrand Junction\tColorado\t39.0783\t-108.5457\t", // too many
		"81503\tClifton\tColorado\tnorth\t-108.4",               // latitude is not a number
		"81504\tClifton\tColorado\t39.09\twest",                 // longitude is not a number
		"80202\tDenver\tColorado\t39.7491\t-104.9946",
	}, "\n"))

	if len(parsed) != 2 {
		t.Fatalf("parsed %d rows (%v), want only the two usable ones", len(parsed), keysOfPostal(parsed))
	}
	if got := parsed["81507"]; got.Name != "Grand Junction" || got.Latitude != 39.0157 || got.Longitude != -108.6129 {
		t.Errorf("81507 = %+v, want Grand Junction at 39.0157,-108.6129", got)
	}
	if got := parsed["80202"]; got.State != "Colorado" {
		t.Errorf("80202 state = %q, want Colorado", got.State)
	}
}

func TestParsePostalCodes_Empty(t *testing.T) {
	if parsed := parsePostalCodes(""); len(parsed) != 0 {
		t.Errorf("parsePostalCodes(\"\") = %v, want empty", keysOfPostal(parsed))
	}
}

func keysOfPostal(m map[string]postalPlace) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// GeoNames files the territories as separate countries, so they are merged in
// from their own files and would be the easiest thing in the table to lose.
// 00901 and 00802 returned nothing at all before this table existed.
func TestLookupPostalCode_Territories(t *testing.T) {
	cases := []struct {
		code      string
		wantName  string
		wantState string
	}{
		{"00901", "San Juan", "Puerto Rico"},
		{"00680", "Mayaguez", "Puerto Rico"},
		{"00802", "St Thomas", "U.S. Virgin Islands"},
		{"96910", "Hagatna", "Guam"},
		{"96799", "Pago Pago", "American Samoa"},
		{"96951", "Rota", "Northern Mariana Islands"},
	}
	for _, tc := range cases {
		found, ok := lookupPostalCode(tc.code)
		if !ok {
			t.Errorf("lookupPostalCode(%q) found nothing, want %s", tc.code, tc.wantName)
			continue
		}
		if found.Name != tc.wantName || found.State != tc.wantState {
			t.Errorf("lookupPostalCode(%q) = %q/%q, want %q/%q",
				tc.code, found.Name, found.State, tc.wantName, tc.wantState)
		}
	}
}

// Every territory has to survive a regeneration of the table, not just the one
// or two that happen to be spot-checked above.
func TestPostalTable_CoversEveryTerritory(t *testing.T) {
	want := []string{"Puerto Rico", "U.S. Virgin Islands", "Guam", "American Samoa", "Northern Mariana Islands"}

	seen := make(map[string]int)
	for _, place := range loadPostalCodes() {
		seen[place.State]++
	}
	for _, region := range want {
		if seen[region] == 0 {
			t.Errorf("no ZIPs in the table for %s", region)
		}
	}
}

// A ZIP is text, not a number. Anything that parses one as an integer along the
// way turns 01001 into 1001, which is a different place in a different state.
func TestLookupPostalCode_LeadingZeros(t *testing.T) {
	cases := []struct {
		code      string
		wantName  string
		wantState string
	}{
		{"01001", "Agawam", "Massachusetts"},
		{"02139", "Cambridge", "Massachusetts"},
		{"00901", "San Juan", "Puerto Rico"},
	}
	for _, tc := range cases {
		found, ok := lookupPostalCode(tc.code)
		if !ok || found.Name != tc.wantName || found.State != tc.wantState {
			t.Errorf("lookupPostalCode(%q) = %q/%q (%v), want %q/%q",
				tc.code, found.Name, found.State, ok, tc.wantName, tc.wantState)
		}
	}
	// The truncated form must not quietly resolve to something else.
	if found, ok := lookupPostalCode("1001"); ok {
		t.Errorf("lookupPostalCode(1001) = %q, want no match for a four digit code", found.Name)
	}
}
