package main

import (
	_ "embed"
	"math"
	"strconv"
	"strings"
	"sync"
)

// cities.tsv is derived from the GeoNames cities15000 dump
// (https://download.geonames.org/export/dump/, CC BY 4.0), every populated
// place of at least 15,000 people. Open-Meteo has no reverse geocoder, so a
// coordinate query had nothing to print but the coordinates; this is what turns
// them back into a name. Its columns are the place, the region gbw prints after
// it, latitude, longitude and population. The region is the state for US places
// and the country elsewhere, matching how a geocoded location is labelled.
//
//go:embed cities.tsv
var cityData string

type city struct {
	Name       string
	Region     string
	Latitude   float64
	Longitude  float64
	Population int
}

const (
	// nameRadiusKm is how close a city has to be for the coordinates to be
	// called by its name outright.
	nameRadiusKm = 25
	// nearRadiusKm is how far out a city may still be worth mentioning as a
	// landmark. Past it -- open ocean, deep wilderness -- naming the nearest
	// city says less than the coordinates do, so the coordinates stay.
	nearRadiusKm = 150
	// kmPerDegreeLat is deliberately a little short so the latitude prefilter
	// errs towards keeping a city rather than dropping one just inside range.
	kmPerDegreeLat = 110.0
	earthRadiusKm  = 6371.0
)

var (
	citiesOnce sync.Once
	cities     []city
)

func loadCities() []city {
	citiesOnce.Do(func() { cities = parseCities(cityData) })
	return cities
}

// parseCities skips rows it cannot make sense of rather than failing the table,
// so one bad line in a regenerated dataset costs a single city instead of every
// coordinate lookup.
func parseCities(data string) []city {
	lines := strings.Split(strings.TrimSpace(data), "\n")
	parsed := make([]city, 0, len(lines))

	for _, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			continue
		}
		latitude, errLat := strconv.ParseFloat(fields[2], 64)
		longitude, errLon := strconv.ParseFloat(fields[3], 64)
		population, errPop := strconv.Atoi(fields[4])
		if errLat != nil || errLon != nil || errPop != nil {
			continue
		}
		if fields[0] == "" || fields[1] == "" {
			continue
		}
		parsed = append(parsed, city{
			Name:       fields[0],
			Region:     fields[1],
			Latitude:   latitude,
			Longitude:  longitude,
			Population: population,
		})
	}

	return parsed
}

// haversineKm is the great circle distance between two coordinates.
func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	phi1 := lat1 * math.Pi / 180
	phi2 := lat2 * math.Pi / 180
	dPhi := (lat2 - lat1) * math.Pi / 180
	dLambda := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(dPhi/2)*math.Sin(dPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(dLambda/2)*math.Sin(dLambda/2)

	return 2 * earthRadiusKm * math.Asin(math.Sqrt(a))
}

// reverseGeocode names a coordinate after a nearby city.
//
// Inside nameRadiusKm it picks the most populous city rather than the closest.
// Central Tokyo is a kilometre from Eifuku and four from Tokyo itself, and
// anyone asking about those coordinates means Tokyo; the same reasoning names a
// suburb's coordinates after the metropolis it belongs to. Only when nothing is
// that close does the nearest city win, and then it is qualified as "near",
// because 80km from Elko is not Elko.
func reverseGeocode(lat, lon float64) (string, bool) {
	var (
		populous    city
		nearest     city
		nearestKm   = math.Inf(1)
		havePopulou bool
		haveNearest bool
	)

	for _, candidate := range loadCities() {
		// Anything this far north or south cannot be within nearRadiusKm, and
		// skipping it here avoids a trigonometric distance for every one of the
		// thirty four thousand rows.
		if math.Abs(candidate.Latitude-lat) > nearRadiusKm/kmPerDegreeLat {
			continue
		}

		distance := haversineKm(lat, lon, candidate.Latitude, candidate.Longitude)
		if distance > nearRadiusKm {
			continue
		}
		if !haveNearest || distance < nearestKm {
			nearest, nearestKm, haveNearest = candidate, distance, true
		}
		if distance <= nameRadiusKm && (!havePopulou || candidate.Population > populous.Population) {
			populous, havePopulou = candidate, true
		}
	}

	switch {
	case havePopulou:
		return populous.Name + ", " + populous.Region, true
	case haveNearest:
		return "near " + nearest.Name + ", " + nearest.Region, true
	default:
		return "", false
	}
}
