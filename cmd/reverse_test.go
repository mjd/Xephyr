package main

import (
	"math"
	"strings"
	"testing"
)

func TestReverseGeocode(t *testing.T) {
	cases := []struct {
		name     string
		lat, lon float64
		want     string
	}{
		{"downtown Denver", 39.7392, -104.9903, "Denver, Colorado"},
		{"Reykjavik", 64.1466, -21.9426, "Reykjavík, Iceland"},
		{"the 81507 Redlands", 39.0157, -108.6129, "Grand Junction, Colorado"},
		// Nothing within 25km, but a city close enough to be worth naming.
		{"the Wyoming basin", 44.0, -107.5, "near Sheridan, Wyoming"},
		{"central Montana", 46.9, -110.0, "near Great Falls, Montana"},
	}
	for _, tc := range cases {
		got, ok := reverseGeocode(tc.lat, tc.lon)
		if !ok {
			t.Errorf("%s: reverseGeocode(%v, %v) found nothing, want %q", tc.name, tc.lat, tc.lon, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The nearest city to central Tokyo is the Eifuku neighbourhood, under a
// kilometre away. Anyone asking about those coordinates means Tokyo, so within
// nameRadiusKm population decides rather than distance.
func TestReverseGeocode_PopulationBeatsProximity(t *testing.T) {
	const lat, lon = 35.6762, 139.6503

	var nearest city
	nearestKm := math.Inf(1)
	for _, candidate := range loadCities() {
		if d := haversineKm(lat, lon, candidate.Latitude, candidate.Longitude); d < nearestKm {
			nearest, nearestKm = candidate, d
		}
	}
	if nearest.Name == "Tokyo" {
		t.Fatalf("test is toothless: Tokyo is already the nearest city at %.1fkm", nearestKm)
	}

	got, ok := reverseGeocode(lat, lon)
	if !ok || got != "Tokyo, Japan" {
		t.Errorf("got %q (%v), want Tokyo, Japan rather than the nearer %q at %.1fkm",
			got, ok, nearest.Name, nearestKm)
	}
}

// Past nearRadiusKm the nearest city says less than the coordinates do.
func TestReverseGeocode_TooFarToName(t *testing.T) {
	cases := []struct {
		name     string
		lat, lon float64
	}{
		{"rural Nevada, 182km from Elko", 39.5, -117.0},
		{"the mid Pacific", 0, -140},
		{"the Southern Ocean", -60, 100},
	}
	for _, tc := range cases {
		if got, ok := reverseGeocode(tc.lat, tc.lon); ok {
			t.Errorf("%s: got %q, want the coordinates left alone", tc.name, got)
		}
	}
}

// The "near" qualifier is the whole difference between a landmark and a claim
// about where someone is.
func TestReverseGeocode_QualifiesDistantCities(t *testing.T) {
	close, ok := reverseGeocode(39.7392, -104.9903)
	if !ok || strings.HasPrefix(close, "near ") {
		t.Errorf("downtown Denver = %q, want no qualifier", close)
	}
	far, ok := reverseGeocode(44.0, -107.5)
	if !ok || !strings.HasPrefix(far, "near ") {
		t.Errorf("the Wyoming basin = %q, want a near qualifier", far)
	}
}

func TestHaversineKm(t *testing.T) {
	cases := []struct {
		name                   string
		lat1, lon1, lat2, lon2 float64
		wantKm                 float64
	}{
		{"a point to itself", 39.7392, -104.9903, 39.7392, -104.9903, 0},
		{"Denver to Boulder", 39.7392, -104.9903, 40.015, -105.2705, 38.5},
		{"London to Paris", 51.5074, -0.1278, 48.8566, 2.3522, 343.5},
		{"pole to pole", 90, 0, -90, 0, 20015},
		// Longitude wrapping is the classic way to get this wrong: these two
		// straddle the antimeridian and are close, not half a world apart.
		{"across the antimeridian", 0, 179.5, 0, -179.5, 111.2},
	}
	for _, tc := range cases {
		got := haversineKm(tc.lat1, tc.lon1, tc.lat2, tc.lon2)
		if math.Abs(got-tc.wantKm) > 1 {
			t.Errorf("%s: got %.1fkm, want %.1fkm", tc.name, got, tc.wantKm)
		}
	}
}

func TestCityTableLoaded(t *testing.T) {
	table := loadCities()
	if len(table) < 34000 {
		t.Fatalf("city table has %d entries, want at least 34000", len(table))
	}

	for _, place := range table {
		if place.Name == "" || place.Region == "" {
			t.Fatalf("city has an empty name or region: %+v", place)
		}
		if place.Latitude < -90 || place.Latitude > 90 || place.Longitude < -180 || place.Longitude > 180 {
			t.Fatalf("city %q is off the globe: %v,%v", place.Name, place.Latitude, place.Longitude)
		}
		// Zero is legitimate -- Ngerulmud and post-volcano Plymouth are both
		// capitals with no residents -- but a negative would break the
		// population tie-break outright.
		if place.Population < 0 {
			t.Fatalf("city %q has population %d", place.Name, place.Population)
		}
	}
}

func TestParseCities_SkipsUnusableRows(t *testing.T) {
	parsed := parseCities(strings.Join([]string{
		"Denver\tColorado\t39.7392\t-104.9847\t729019",
		"Boulder\tColorado\t40.015\t-105.2705",          // too few columns
		"Aurora\tColorado\t39.7\t-104.8\t325078\textra", // too many
		"Nowhere\tColorado\tnorth\t-104.8\t1000",        // latitude is not a number
		"Nowhere\tColorado\t39.7\twest\t1000",           // longitude is not a number
		"Nowhere\tColorado\t39.7\t-104.8\tmany",         // population is not a number
		"\tColorado\t39.7\t-104.8\t1000",                // no name to print
		"Nowhere\t\t39.7\t-104.8\t1000",                 // no region to print
		"Tokyo\tJapan\t35.6895\t139.6917\t9733276",
	}, "\n"))

	if len(parsed) != 2 {
		t.Fatalf("parsed %d rows (%v), want only the two usable ones", len(parsed), cityNames(parsed))
	}
	if parsed[0].Name != "Denver" || parsed[0].Population != 729019 {
		t.Errorf("first row = %+v, want Denver at 729019", parsed[0])
	}
	if parsed[1].Name != "Tokyo" || parsed[1].Latitude != 35.6895 {
		t.Errorf("second row = %+v, want Tokyo at 35.6895", parsed[1])
	}
}

func TestParseCities_Empty(t *testing.T) {
	if parsed := parseCities(""); len(parsed) != 0 {
		t.Errorf("parseCities(\"\") = %v, want empty", cityNames(parsed))
	}
}

func cityNames(places []city) []string {
	names := make([]string, 0, len(places))
	for _, place := range places {
		names = append(names, place.Name)
	}
	return names
}

func BenchmarkReverseGeocode(b *testing.B) {
	loadCities()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reverseGeocode(39.7392, -104.9903)
	}
}

// Three rows in the source have no population at all, and one of those is not
// a town: GeoNames files a spot in Kenya under "Thomas Magena home". Asking for
// the coordinates it sits exactly on has to return a real place -- Kisii, the
// most populous within range at 10km -- rather than the record underfoot.
func TestReverseGeocode_ZeroPopulationLosesToARealTown(t *testing.T) {
	got, ok := reverseGeocode(-0.7702, 34.7508)
	if !ok || got != "Kisii, Kenya" {
		t.Errorf("got %q (%v), want Kisii, Kenya rather than the unpopulated record underfoot", got, ok)
	}
}

// But where it is the only thing for hundreds of kilometres, a capital with no
// residents still beats printing coordinates: the nearest alternative to
// Palau's capital is 892km away in the Philippines.
func TestReverseGeocode_ZeroPopulationStillNamesTheLonely(t *testing.T) {
	got, ok := reverseGeocode(7.5008, 134.6238)
	if !ok || got != "Ngerulmud, Palau" {
		t.Errorf("got %q (%v), want Ngerulmud, Palau", got, ok)
	}
}
