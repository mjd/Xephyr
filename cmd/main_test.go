package main

import (
	"io"
	"log"
	"strings"
	"testing"
	"time"
)

// newTestApp returns a minimal application with discarded logs, suitable for
// unit-testing handlers that do not make network calls.
func newTestApp() *application {
	discard := log.New(io.Discard, "", 0)
	return &application{
		infoLog:  discard,
		errorLog: discard,
	}
}

// ── generateHoroscope ────────────────────────────────────────────────────────

func TestGenerateHoroscope_Deterministic(t *testing.T) {
	date := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	a := generateHoroscope(401, date)
	b := generateHoroscope(401, date)
	if a != b {
		t.Errorf("same inputs produced different output:\n  %s\n  %s", a, b)
	}
}

func TestGenerateHoroscope_DifferentDatesDifferentOutput(t *testing.T) {
	d1 := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC)
	if generateHoroscope(401, d1) == generateHoroscope(401, d2) {
		t.Error("different dates produced identical horoscope")
	}
}

func TestGenerateHoroscope_DifferentIDsDifferentOutput(t *testing.T) {
	date := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	if generateHoroscope(401, date) == generateHoroscope(402, date) {
		t.Error("different user IDs produced identical horoscope")
	}
}

func TestGenerateHoroscope_UTCNormalization(t *testing.T) {
	// Times that differ only by timezone offset on the same UTC day should
	// produce the same horoscope.
	utc := time.Date(2026, 6, 3, 1, 0, 0, 0, time.UTC)
	eastern := utc.In(time.FixedZone("EST", -5*3600)) // same instant, different wall clock
	if generateHoroscope(401, utc) != generateHoroscope(401, eastern) {
		t.Error("same UTC instant but different timezone produced different horoscope")
	}
}

func TestGenerateHoroscope_NonEmpty(t *testing.T) {
	date := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	h := generateHoroscope(1818, date)
	if strings.TrimSpace(h) == "" {
		t.Error("generateHoroscope returned empty string")
	}
}

func TestGenerateHoroscope_ThreeSentences(t *testing.T) {
	// The output is "opener prediction closer" — each drawn from a separate bank.
	// We verify that the opener, prediction, and closer each come from their
	// respective banks by checking that the output starts with a known opener
	// and ends with a known closer for a deterministic seed.
	date := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)

	for _, id := range []int{1, 42, 401, 1234, 5678, 1818, 99999} {
		h := generateHoroscope(id, date)
		foundOpener := false
		for _, o := range horoscopeOpeners {
			if strings.HasPrefix(h, o) {
				foundOpener = true
				break
			}
		}
		if !foundOpener {
			t.Errorf("id=%d: output does not start with any known opener:\n  %s", id, h)
		}

		foundCloser := false
		for _, c := range horoscopeClosers {
			if strings.HasSuffix(h, c) {
				foundCloser = true
				break
			}
		}
		if !foundCloser {
			t.Errorf("id=%d: output does not end with any known closer:\n  %s", id, h)
		}
	}
}

func TestGenerateHoroscope_ContainsPrediction(t *testing.T) {
	date := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	for _, id := range []int{1, 42, 401, 1234, 5678} {
		h := generateHoroscope(id, date)
		found := false
		for _, p := range horoscopePredictions {
			if strings.Contains(h, p) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("id=%d: output does not contain any known prediction:\n  %s", id, h)
		}
	}
}

func TestGenerateHoroscope_BanksNonEmpty(t *testing.T) {
	if len(horoscopeOpeners) == 0 {
		t.Error("horoscopeOpeners is empty")
	}
	if len(horoscopePredictions) == 0 {
		t.Error("horoscopePredictions is empty")
	}
	if len(horoscopeClosers) == 0 {
		t.Error("horoscopeClosers is empty")
	}
}

func TestGenerateHoroscope_BankEntriesNonEmpty(t *testing.T) {
	for i, s := range horoscopeOpeners {
		if strings.TrimSpace(s) == "" {
			t.Errorf("horoscopeOpeners[%d] is blank", i)
		}
	}
	for i, s := range horoscopePredictions {
		if strings.TrimSpace(s) == "" {
			t.Errorf("horoscopePredictions[%d] is blank", i)
		}
	}
	for i, s := range horoscopeClosers {
		if strings.TrimSpace(s) == "" {
			t.Errorf("horoscopeClosers[%d] is blank", i)
		}
	}
}

// ── checkLineForRegexps – horoscope dispatch ─────────────────────────────────

func TestCheckLine_HoroscopeBasic(t *testing.T) {
	app := newTestApp()
	line := `[Dino(#1234)] Dino says "gravybot horoscope #401"`
	cmd, err := app.checkLineForRegexps(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(cmd, "pose H> ") {
		t.Errorf("expected pose H> prefix, got: %q", cmd)
	}
	if !strings.HasSuffix(cmd, "\n") {
		t.Errorf("expected trailing newline, got: %q", cmd)
	}
}

func TestCheckLine_HoroscopeCaseInsensitive(t *testing.T) {
	app := newTestApp()
	variants := []string{
		`[Dino(#1234)] Dino says "GRAVYBOT HOROSCOPE #401"`,
		`[Dino(#1234)] Dino says "Gravybot Horoscope #401"`,
		`[Dino(#1234)] Dino says "gravybot horoscope #401"`,
	}
	for _, line := range variants {
		cmd, err := app.checkLineForRegexps(line)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", line, err)
		}
		if !strings.HasPrefix(cmd, "pose H> ") {
			t.Errorf("expected pose H> prefix for %q, got: %q", line, cmd)
		}
	}
}

func TestCheckLine_HoroscopeDeterministic(t *testing.T) {
	// Two calls on the same day for the same player ID must return the same
	// horoscope. We fake a fixed date by calling generateHoroscope directly
	// and verify the regex dispatch embeds content from the banks.
	app := newTestApp()
	line := `[Dino(#1234)] Dino says "gravybot horoscope #1818"`
	cmd1, _ := app.checkLineForRegexps(line)
	cmd2, _ := app.checkLineForRegexps(line)
	// Strip the trailing newline for comparison; both should be identical
	// within the same second (same UTC date).
	if strings.TrimRight(cmd1, "\n") != strings.TrimRight(cmd2, "\n") {
		t.Errorf("two calls on same day returned different horoscopes:\n  %s\n  %s", cmd1, cmd2)
	}
}

func TestCheckLine_HoroscopeNoMatchOnOtherCommands(t *testing.T) {
	app := newTestApp()
	// Lines that should NOT trigger the horoscope handler.
	nonMatches := []string{
		`[Dino(#1234)] Dino says "gravybot weather New York"`,
		`[Dino(#1234)] Dino says "gravybot stock AAPL"`,
		`[Dino(#1234)] Dino says "gravybot horoscope"`,        // missing dbref
		`[Dino(#1234)] Dino says "gravybot horoscope foobar"`, // non-numeric dbref
		`plain line with no bracket prefix`,
	}
	for _, line := range nonMatches {
		cmd, err := app.checkLineForRegexps(line)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", line, err)
		}
		if strings.HasPrefix(cmd, "pose H> ") {
			t.Errorf("horoscope handler triggered unexpectedly for: %q", line)
		}
	}
}

func TestCheckLine_HoroscopeOutputNotEmpty(t *testing.T) {
	app := newTestApp()
	line := `[Player(#5678)] Player says "gravybot horoscope #42"`
	cmd, err := app.checkLineForRegexps(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body := strings.TrimPrefix(cmd, "pose H> ")
	body = strings.TrimRight(body, "\n")
	if strings.TrimSpace(body) == "" {
		t.Error("horoscope body is empty")
	}
}

// ── checkLineForRegexps – existing commands (regression) ─────────────────────

func TestCheckLine_HangoutPage(t *testing.T) {
	app := newTestApp()
	line := `[Dino(#1234)] Dino pages: hangout`
	cmd, err := app.checkLineForRegexps(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != "@dolist me={gautoreturn on;hangout}\n" {
		t.Errorf("unexpected hangout command: %q", cmd)
	}
}

func TestCheckLine_HomePage(t *testing.T) {
	app := newTestApp()
	line := `[Dino(#1234)] Dino pages: home`
	cmd, err := app.checkLineForRegexps(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != "@dolist me={gautoreturn off;home}\n" {
		t.Errorf("unexpected home command: %q", cmd)
	}
}

func TestCheckLine_NoMatch(t *testing.T) {
	app := newTestApp()
	cmd, err := app.checkLineForRegexps("some random mush output line")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != "" {
		t.Errorf("expected empty command, got: %q", cmd)
	}
}
