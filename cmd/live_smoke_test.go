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
		parseLatLon("39.7392 -104.9903"),
	} {
		got, err := app.sendWeatherRequest(loc)
		if err != nil {
			t.Errorf("%-22q ERROR %v", loc, err)
			continue
		}
		t.Logf("%-22q -> %s", loc, got)
	}
}
