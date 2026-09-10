package main

import (
	_ "embed"
	"strconv"
	"strings"
	"sync"
)

// airports.tsv is derived from the public domain OurAirports dataset
// (https://ourairports.com/data/), keeping every airport with a three-letter
// IATA code and scheduled service. Its columns are IATA code, the city served,
// the region gbw prints after that city, the ISO country code, latitude and
// longitude. The region is the state for US airports and the country
// elsewhere, matching how a geocoded location is labelled. Districts the
// source carries in trailing parentheses are dropped, so SYD reads as "Sydney"
// rather than "Sydney (Mascot)".
//
//go:embed airports.tsv
var airportData string

type airport struct {
	Name        string
	Region      string
	CountryCode string
	Latitude    float64
	Longitude   float64
}

// iataMetroCodes covers the metropolitan area codes, which name a city rather
// than a single field and so do not appear in the airport table at all.
var iataMetroCodes = map[string]string{
	"BJS": "PEK",
	"BUE": "EZE",
	"CHI": "ORD",
	"LON": "LHR",
	"MIL": "MXP",
	"MOW": "SVO",
	"NYC": "JFK",
	"OSA": "KIX",
	"PAR": "CDG",
	"RIO": "GIG",
	"ROM": "FCO",
	"SAO": "GRU",
	"SEL": "ICN",
	"STO": "ARN",
	"TYO": "HND",
	"WAS": "DCA",
}

var (
	airportsOnce   sync.Once
	airportsByIATA map[string]airport
)

func loadAirports() map[string]airport {
	airportsOnce.Do(func() {
		lines := strings.Split(strings.TrimSpace(airportData), "\n")
		airportsByIATA = make(map[string]airport, len(lines))

		for _, line := range lines {
			fields := strings.Split(line, "\t")
			if len(fields) != 6 {
				continue
			}
			latitude, errLat := strconv.ParseFloat(fields[4], 64)
			longitude, errLon := strconv.ParseFloat(fields[5], 64)
			if errLat != nil || errLon != nil {
				continue
			}
			airportsByIATA[fields[0]] = airport{
				Name:        fields[1],
				Region:      fields[2],
				CountryCode: fields[3],
				Latitude:    latitude,
				Longitude:   longitude,
			}
		}
	})
	return airportsByIATA
}

// lookupAirport resolves an IATA code, following a metropolitan area code to
// the field it is usually taken to mean. The "iata:" prefix weatherapi.com
// required is accepted so old habits keep working, but it is not needed.
func lookupAirport(code string) (airport, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.TrimSpace(strings.TrimPrefix(code, "IATA:"))

	if len(code) != 3 || !isAlphaUpper(code) {
		return airport{}, false
	}
	if primary, ok := iataMetroCodes[code]; ok {
		code = primary
	}

	found, ok := loadAirports()[code]
	return found, ok
}

func isAlphaUpper(s string) bool {
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

// place renders an airport the way a geocoding hit would be, so the two
// resolution paths format identically.
func (a airport) place() OpenMeteoPlace {
	resolved := OpenMeteoPlace{
		Name:        a.Name,
		Latitude:    a.Latitude,
		Longitude:   a.Longitude,
		CountryCode: a.CountryCode,
	}
	if a.CountryCode == "US" {
		resolved.Admin1 = a.Region
	} else {
		resolved.Country = a.Region
	}
	return resolved
}
