package main

import (
	"io"
	"log"
	"os"
	"testing"
)

func TestLiveOpenMeteoSmoke(t *testing.T) {
	if os.Getenv("LIVE_SMOKE") == "" {
		t.Skip("set LIVE_SMOKE=1 to hit the real Open-Meteo APIs")
	}
	app := &application{
		config: config{
			openMeteoGeocodeURL:    "https://geocoding-api.open-meteo.com/v1/search",
			openMeteoForecastURL:   "https://api.open-meteo.com/v1/forecast",
			openMeteoAirQualityURL: "https://air-quality-api.open-meteo.com/v1/air-quality",
		},
		infoLog:  log.New(io.Discard, "", 0),
		errorLog: log.New(os.Stderr, "ERR ", 0),
	}
	for _, loc := range []string{
		"denver co", "london england", "london", "paris france",
		"salt lake city utah", "tokyo", "dino", "vars ontario",
		"LHR", "den", "iata:NRT", "NYC", "SYD", "GIG", "ZZZ",
		// A bare five digit number is a US ZIP: 80202 and 81507 are Denver and
		// Grand Junction, and 75001 and 28001 are Addison and Albemarle rather
		// than the Paris and Madrid codes sharing their digits.
		"80202", "81507", "81523", "75001", "28001",
		// Territories come from GeoNames' separate country files.
		"00901", "96910",
		// The three coordinate tiers: named outright, named as a landmark,
		// and too far from anywhere to name at all.
		parseLatLon("39.7392 -104.9903"),
		parseLatLon("44.0 -107.5"),
		parseLatLon("0 -140"),
	} {
		got, err := app.sendWeatherRequest(loc)
		if err != nil {
			t.Errorf("%-22q ERROR %v", loc, err)
			continue
		}
		t.Logf("%-22q -> %s", loc, got)
	}
}
