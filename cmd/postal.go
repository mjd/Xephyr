package main

import (
	_ "embed"
	"strconv"
	"strings"
	"sync"
)

// postal.tsv is derived from the GeoNames postal code dataset
// (https://download.geonames.org/export/zip/, CC BY 4.0), which carries a
// coordinate for every US ZIP in its own right. Open-Meteo's geocoder searches
// populated places and matches a ZIP only when GeoNames happened to hang it off
// one of them, so an outlying code like 81507 finds nothing there even though
// 81501 through 81506 all resolve to Grand Junction. Its columns are the ZIP,
// the place it names, the state gbw prints after that place, latitude and
// longitude.
//
// Military APO/FPO codes are left out. They route mail rather than name a
// place, and the one coordinate GeoNames gives "APO AE" is a servicing
// facility in Sicily, which would answer a soldier in Kuwait with the weather
// somewhere they have never been.
//
//go:embed postal.tsv
var postalData string

type postalPlace struct {
	Name      string
	State     string
	Latitude  float64
	Longitude float64
}

var (
	postalOnce   sync.Once
	postalByCode map[string]postalPlace
)

func loadPostalCodes() map[string]postalPlace {
	postalOnce.Do(func() { postalByCode = parsePostalCodes(postalData) })
	return postalByCode
}

// parsePostalCodes skips rows it cannot make sense of rather than failing the
// table, so one bad line in a regenerated dataset costs a single ZIP instead of
// every postal lookup.
func parsePostalCodes(data string) map[string]postalPlace {
	lines := strings.Split(strings.TrimSpace(data), "\n")
	parsed := make(map[string]postalPlace, len(lines))

	for _, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			continue
		}
		latitude, errLat := strconv.ParseFloat(fields[3], 64)
		longitude, errLon := strconv.ParseFloat(fields[4], 64)
		if errLat != nil || errLon != nil {
			continue
		}
		parsed[fields[0]] = postalPlace{
			Name:      fields[1],
			State:     fields[2],
			Latitude:  latitude,
			Longitude: longitude,
		}
	}

	return parsed
}

// lookupPostalCode resolves a five digit US ZIP.
func lookupPostalCode(code string) (postalPlace, bool) {
	code = strings.TrimSpace(code)

	if len(code) != 5 || !isDigits(code) {
		return postalPlace{}, false
	}

	found, ok := loadPostalCodes()[code]
	return found, ok
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// place renders a ZIP the way a geocoding hit would, so the two resolution
// paths format identically.
func (p postalPlace) place() OpenMeteoPlace {
	return OpenMeteoPlace{
		Name:        p.Name,
		Admin1:      p.State,
		Country:     "United States",
		CountryCode: "US",
		Latitude:    p.Latitude,
		Longitude:   p.Longitude,
	}
}
