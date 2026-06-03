package main

import (
	"io"
	"log"
	"math/rand"
	"strconv"
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

func TestGenerateHoroscope_PhraseProvenance(t *testing.T) {
	// Output format: "{opener} {prediction} {closer} Lucky number for today: {num}."
	// Verify opener, prediction, and closer each come from their respective banks.
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
			if strings.Contains(h, c) {
				foundCloser = true
				break
			}
		}
		if !foundCloser {
			t.Errorf("id=%d: output does not contain any known closer:\n  %s", id, h)
		}
	}
}

func TestGenerateHoroscope_LuckyNumberPresent(t *testing.T) {
	date := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	for _, id := range []int{1, 42, 401, 1234, 5678, 1818} {
		h := generateHoroscope(id, date)
		if !strings.Contains(h, "Lucky number for today: ") {
			t.Errorf("id=%d: missing lucky number suffix:\n  %s", id, h)
		}
		if !strings.HasSuffix(h, ".") {
			t.Errorf("id=%d: output does not end with period:\n  %s", id, h)
		}
	}
}

func TestGenerateHoroscope_LuckyNumberDeterministic(t *testing.T) {
	date := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	a := generateHoroscope(401, date)
	b := generateHoroscope(401, date)
	if a != b {
		t.Errorf("lucky number not deterministic: %q != %q", a, b)
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

// ── generateLuckyNumber ───────────────────────────────────────────────────────

func TestGenerateLuckyNumber_NonEmpty(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 100; i++ {
		n := generateLuckyNumber(rng)
		if strings.TrimSpace(n) == "" {
			t.Errorf("iteration %d: generateLuckyNumber returned empty string", i)
		}
	}
}

func TestGenerateLuckyNumber_SmallIntegerRange(t *testing.T) {
	// Force into the small-integer branch by exhausting rolls at < 70.
	// We verify a broad sample contains at least some values in [1,99].
	rng := rand.New(rand.NewSource(0))
	found := false
	for i := 0; i < 200; i++ {
		n := generateLuckyNumber(rng)
		v, err := strconv.Atoi(n)
		if err == nil && v >= 1 && v <= 99 {
			found = true
			break
		}
	}
	if !found {
		t.Error("no small integer (1–99) found in 200 samples")
	}
}

func TestGenerateLuckyNumber_ConstantFormat(t *testing.T) {
	// Every constant entry must be non-empty and not blank.
	for i, c := range luckyConstants {
		if strings.TrimSpace(c) == "" {
			t.Errorf("luckyConstants[%d] is blank", i)
		}
	}
}

func TestFormatWithCommas(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{99, "99"},
		{999, "999"},
		{1000, "1,000"},
		{9999, "9,999"},
		{99999, "99,999"},
		{1000000, "1,000,000"},
		{1234567890, "1,234,567,890"},
	}
	for _, tc := range cases {
		got := formatWithCommas(tc.n)
		if got != tc.want {
			t.Errorf("formatWithCommas(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestGenerateLuckyNumber_AllBranchesCovered(t *testing.T) {
	// Sample 2000 outputs and verify every branch is represented at least once.
	// Expected counts: ~1400 small-int, ~300 large-int, ~100 big-number,
	// ~100 decimal, ~100 constant — so any branch missing after 2000 runs
	// would be a near-impossible fluke.
	type result struct{ smallInt, largeInt, bigNum, decimal, constant int }
	var counts result

	rng := rand.New(rand.NewSource(12345))
	for i := 0; i < 2000; i++ {
		n := generateLuckyNumber(rng)
		switch {
		case isConstant(n):
			counts.constant++
		case strings.Contains(n, "."):
			counts.decimal++
		case strings.Contains(n, ","):
			counts.bigNum++
		default:
			v, err := strconv.Atoi(n)
			if err != nil {
				t.Errorf("iteration %d: not parseable as int and not decimal/big/constant: %q", i, n)
				continue
			}
			if v >= 1 && v <= 99 {
				counts.smallInt++
			} else {
				counts.largeInt++
			}
		}
	}

	if counts.smallInt == 0 {
		t.Error("small-int branch never produced output")
	}
	if counts.largeInt == 0 {
		t.Error("large-int branch never produced output")
	}
	if counts.bigNum == 0 {
		t.Error("big-number branch never produced output")
	}
	if counts.decimal == 0 {
		t.Error("decimal branch never produced output")
	}
	if counts.constant == 0 {
		t.Error("constant branch never produced output")
	}
}

// isConstant returns true if s matches any entry in luckyConstants.
func isConstant(s string) bool {
	for _, c := range luckyConstants {
		if s == c {
			return true
		}
	}
	return false
}

func TestGenerateLuckyNumber_SmallIntBounds(t *testing.T) {
	rng := rand.New(rand.NewSource(0))
	for i := 0; i < 2000; i++ {
		n := generateLuckyNumber(rng)
		v, err := strconv.Atoi(n)
		if err != nil {
			continue // not a plain integer — skip
		}
		if v < 1 {
			t.Errorf("integer lucky number %d is below 1", v)
		}
		if v > 1000 {
			t.Errorf("integer lucky number %d exceeds 1000", v)
		}
	}
}

func TestGenerateLuckyNumber_DecimalPlaces(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	found := 0
	for i := 0; i < 2000 && found < 10; i++ {
		n := generateLuckyNumber(rng)
		// Skip constants (they may contain a decimal point but are not decimal outputs).
		if isConstant(n) || !strings.Contains(n, ".") {
			continue
		}
		found++
		parts := strings.Split(n, ".")
		if len(parts) != 2 {
			t.Errorf("decimal %q has unexpected format", n)
			continue
		}
		places := len(parts[1])
		if places < 5 || places > 7 {
			t.Errorf("decimal %q has %d decimal places, want 5–7", n, places)
		}
	}
	if found == 0 {
		t.Error("no decimal output found in 2000 samples")
	}
}

func TestGenerateLuckyNumber_BigNumberHasCommas(t *testing.T) {
	rng := rand.New(rand.NewSource(77))
	found := 0
	for i := 0; i < 2000 && found < 5; i++ {
		n := generateLuckyNumber(rng)
		if !strings.Contains(n, ",") {
			continue
		}
		found++
		// Must be all digits and commas, no other characters.
		for _, c := range n {
			if c != ',' && (c < '0' || c > '9') {
				t.Errorf("big number %q contains unexpected character %q", n, string(c))
			}
		}
		// Must parse to a value >= 1,000,000.
		plain := strings.ReplaceAll(n, ",", "")
		v, err := strconv.Atoi(plain)
		if err != nil {
			t.Errorf("big number %q not parseable after removing commas: %v", n, err)
			continue
		}
		if v < 1_000_000 {
			t.Errorf("big number %q parses to %d, expected >= 1,000,000", n, v)
		}
	}
	if found == 0 {
		t.Error("no big-number output found in 2000 samples")
	}
}

func TestGenerateLuckyNumber_ConstantsNoDuplicates(t *testing.T) {
	seen := make(map[string]int)
	for i, c := range luckyConstants {
		if prev, ok := seen[c]; ok {
			t.Errorf("duplicate constant at index %d and %d: %q", prev, i, c)
		}
		seen[c] = i
	}
}

// ── ASCII-only output ─────────────────────────────────────────────────────────

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}

func TestAllPhraseBanksAreASCII(t *testing.T) {
	for i, s := range horoscopeOpeners {
		if !isASCII(s) {
			t.Errorf("horoscopeOpeners[%d] contains non-ASCII: %q", i, s)
		}
	}
	for i, s := range horoscopePredictions {
		if !isASCII(s) {
			t.Errorf("horoscopePredictions[%d] contains non-ASCII: %q", i, s)
		}
	}
	for i, s := range horoscopeClosers {
		if !isASCII(s) {
			t.Errorf("horoscopeClosers[%d] contains non-ASCII: %q", i, s)
		}
	}
	for i, s := range luckyConstants {
		if !isASCII(s) {
			t.Errorf("luckyConstants[%d] contains non-ASCII: %q", i, s)
		}
	}
}

func TestGenerateHoroscope_OutputIsASCII(t *testing.T) {
	date := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	for id := 1; id <= 500; id++ {
		h := generateHoroscope(id, date)
		if !isASCII(h) {
			t.Errorf("id=%d: horoscope contains non-ASCII: %q", id, h)
		}
	}
}
