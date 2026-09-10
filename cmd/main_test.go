package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/reiver/go-telnet"
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

// ── parseLatLon ──────────────────────────────────────────────────────────────

func TestParseLatLon(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// colon separator – various sign combinations
		{"48.8566:2.3522", "48.8566,2.3522"},
		{"-33.8688:151.2093", "-33.8688,151.2093"},
		{"40.7128:-74.0060", "40.7128,-74.0060"},   // NYC: positive lat, negative lon
		{"-54.8019:-68.3030", "-54.8019,-68.3030"}, // both negative
		{"0:0", "0,0"},
		// space separator
		{"48.8566 2.3522", "48.8566,2.3522"},
		{"-33.8688 151.2093", "-33.8688,151.2093"},
		{"40.7128 -74.0060", "40.7128,-74.0060"},
		// integers as coords
		{"51 0", "51,0"},
		{"90:-180", "90,-180"}, // boundary values
		// extra whitespace is trimmed
		{"  48.8566 : 2.3522 ", "48.8566,2.3522"},
		// not lat/lon — pass through unchanged
		{"New York", "New York"},
		{"Paris, France", "Paris, France"},
		{"London", "London"},
		{"48.8566", "48.8566"},                         // single float, no separator
		{"abc:def", "abc:def"},                         // non-numeric
		{"48.8566:not_a_float", "48.8566:not_a_float"}, // mixed valid:invalid
		{"not_a_float:2.3522", "not_a_float:2.3522"},   // invalid:valid
	}
	for _, tc := range cases {
		got := parseLatLon(tc.in)
		if got != tc.want {
			t.Errorf("parseLatLon(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── aqiLabel ─────────────────────────────────────────────────────────────────

func TestAqiLabel(t *testing.T) {
	cases := []struct {
		aqi  int
		want string
	}{
		{0, "Good"},
		{50, "Good"},
		{51, "Moderate"},
		{100, "Moderate"},
		{101, "Unhealthy(SG)"},
		{150, "Unhealthy(SG)"},
		{151, "Unhealthy"},
		{200, "Unhealthy"},
		{201, "VeryUnhealthy"},
		{300, "VeryUnhealthy"},
		{301, "Hazardous"},
		{500, "Hazardous"},
	}
	for _, tc := range cases {
		got := aqiLabel(tc.aqi)
		if got != tc.want {
			t.Errorf("aqiLabel(%d) = %q, want %q", tc.aqi, got, tc.want)
		}
	}
}

// ── formatUSD ─────────────────────────────────────────────────────────────────

func TestFormatUSD(t *testing.T) {
	cases := []struct {
		price float64
		want  string
	}{
		{0.08, "$0.08"},
		{1.00, "$1.00"},
		{99.99, "$99.99"},
		{1000.00, "$1,000.00"},
		{1728.58, "$1,728.58"},
		{63995.00, "$63,995.00"},
		{1234567.89, "$1,234,567.89"},
	}
	for _, tc := range cases {
		got := formatUSD(tc.price)
		if got != tc.want {
			t.Errorf("formatUSD(%v) = %q, want %q", tc.price, got, tc.want)
		}
	}
}

// ── getCryptoQuote helpers ────────────────────────────────────────────────────

// newCoinGeckoServer starts a test HTTP server that serves the given search and
// price response bodies as JSON from /search and /simple/price paths.
func newCoinGeckoServer(t *testing.T, searchBody, priceBody interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/search"):
			json.NewEncoder(w).Encode(searchBody)
		case strings.HasPrefix(r.URL.Path, "/simple/price"):
			json.NewEncoder(w).Encode(priceBody)
		default:
			t.Errorf("unexpected CoinGecko request path: %s", r.URL.Path)
			http.Error(w, "not found", 404)
		}
	}))
}

// newCryptoApp wraps newTestApp and points coingeckoBaseURL at the given server.
func newCryptoApp(t *testing.T, baseURL string) *application {
	t.Helper()
	app := newTestApp()
	app.config.coingeckoBaseURL = baseURL
	return app
}

// ── getCryptoQuote ────────────────────────────────────────────────────────────

func TestGetCryptoQuote_Basic(t *testing.T) {
	search := map[string]interface{}{
		"coins": []map[string]string{
			{"id": "bitcoin", "symbol": "BTC", "name": "Bitcoin"},
		},
	}
	price := map[string]map[string]float64{
		"bitcoin": {"usd": 63995.0, "usd_24h_change": 0.21},
	}
	srv := newCoinGeckoServer(t, search, price)
	defer srv.Close()

	result, err := newCryptoApp(t, srv.URL).getCryptoQuote("btc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "BTC(Bitcoin): $63,995.00 +134.11 (+0.21%% 24h)\n"
	if result != want {
		t.Errorf("getCryptoQuote() = %q, want %q", result, want)
	}
}

func TestGetCryptoQuote_OutputFormat(t *testing.T) {
	// Verify the complete format: SYMBOL(Name): $X,XXX.XX (+/-X.XX%% 24h)\n
	search := map[string]interface{}{
		"coins": []map[string]string{
			{"id": "ethereum", "symbol": "ETH", "name": "Ethereum"},
		},
	}
	price := map[string]map[string]float64{
		"ethereum": {"usd": 1728.58, "usd_24h_change": -0.12},
	}
	srv := newCoinGeckoServer(t, search, price)
	defer srv.Close()

	result, err := newCryptoApp(t, srv.URL).getCryptoQuote("eth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "ETH(Ethereum): $1,728.58 -2.08 (-0.12%% 24h)\n"
	if result != want {
		t.Errorf("getCryptoQuote() format mismatch:\n  got  %q\n  want %q", result, want)
	}
}

func TestGetCryptoQuote_DeltaComputed(t *testing.T) {
	// Delta must be derived from price and 24h pct: delta = price - price/(1+pct/100).
	// Verify both positive and negative cases appear between the price and the parens.
	cases := []struct {
		sym, id, name string
		price, pct    float64
		wantDelta     string
	}{
		{"BTC", "bitcoin", "Bitcoin", 63995.0, 0.21, "+134.11"},
		{"ETH", "ethereum", "Ethereum", 1728.58, -0.12, "-2.08"},
		{"SOL", "solana", "Solana", 150.0, 5.0, "+7.14"},
	}
	for _, tc := range cases {
		search := map[string]interface{}{
			"coins": []map[string]string{
				{"id": tc.id, "symbol": tc.sym, "name": tc.name},
			},
		}
		price := map[string]map[string]float64{
			tc.id: {"usd": tc.price, "usd_24h_change": tc.pct},
		}
		srv := newCoinGeckoServer(t, search, price)
		result, err := newCryptoApp(t, srv.URL).getCryptoQuote(tc.sym)
		srv.Close()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.sym, err)
		}
		if !strings.Contains(result, tc.wantDelta+" (") {
			t.Errorf("%s: expected delta %q before paren, got: %q", tc.sym, tc.wantDelta, result)
		}
	}
}

func TestGetCryptoQuote_NegativeChange(t *testing.T) {
	search := map[string]interface{}{
		"coins": []map[string]string{
			{"id": "dogecoin", "symbol": "DOGE", "name": "Dogecoin"},
		},
	}
	price := map[string]map[string]float64{
		"dogecoin": {"usd": 0.08, "usd_24h_change": -0.99},
	}
	srv := newCoinGeckoServer(t, search, price)
	defer srv.Close()

	result, err := newCryptoApp(t, srv.URL).getCryptoQuote("doge")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "(-0.99%%") {
		t.Errorf("expected negative change in result, got: %q", result)
	}
	if strings.Contains(result, "+-") || strings.Contains(result, "++") {
		t.Errorf("unexpected double sign in result: %q", result)
	}
}

func TestGetCryptoQuote_NoResults(t *testing.T) {
	search := map[string]interface{}{"coins": []interface{}{}}
	srv := newCoinGeckoServer(t, search, nil)
	defer srv.Close()

	result, err := newCryptoApp(t, srv.URL).getCryptoQuote("unknowncoin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result, "Crypto error:") {
		t.Errorf("expected 'Crypto error:' prefix, got: %q", result)
	}
}

func TestGetCryptoQuote_NoPriceData(t *testing.T) {
	search := map[string]interface{}{
		"coins": []map[string]string{
			{"id": "bitcoin", "symbol": "BTC", "name": "Bitcoin"},
		},
	}
	// Price response deliberately omits the "bitcoin" key.
	price := map[string]map[string]float64{}
	srv := newCoinGeckoServer(t, search, price)
	defer srv.Close()

	result, err := newCryptoApp(t, srv.URL).getCryptoQuote("btc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result, "Crypto error:") {
		t.Errorf("expected 'Crypto error:' prefix, got: %q", result)
	}
}

func TestGetCryptoQuote_SearchAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429) // rate limited
	}))
	defer srv.Close()

	result, err := newCryptoApp(t, srv.URL).getCryptoQuote("btc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result, "Crypto error:") {
		t.Errorf("expected 'Crypto error:' prefix on non-200 search, got: %q", result)
	}
}

func TestGetCryptoQuote_ShortQuerySymbolMatch(t *testing.T) {
	// First result does not match "BTC" exactly; the exact symbol match appears
	// second. For a short query (len <= 5) the exact match should win.
	search := map[string]interface{}{
		"coins": []map[string]string{
			{"id": "wrapped-btc", "symbol": "WBTC", "name": "Wrapped Bitcoin"},
			{"id": "bitcoin", "symbol": "BTC", "name": "Bitcoin"},
		},
	}
	price := map[string]map[string]float64{
		"bitcoin": {"usd": 64000.0, "usd_24h_change": 0.5},
	}
	srv := newCoinGeckoServer(t, search, price)
	defer srv.Close()

	result, err := newCryptoApp(t, srv.URL).getCryptoQuote("btc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result, "BTC(Bitcoin):") {
		t.Errorf("short query should select by exact symbol match, got: %q", result)
	}
}

func TestGetCryptoQuote_LongQueryTrustsRanking(t *testing.T) {
	// For queries longer than 5 chars, trust CoinGecko's ranked first result.
	// A meme coin with symbol "BITCOIN" later in the list must NOT override it.
	search := map[string]interface{}{
		"coins": []map[string]string{
			{"id": "bitcoin", "symbol": "BTC", "name": "Bitcoin"},
			{"id": "harrypotterobamasonic10in", "symbol": "BITCOIN", "name": "HarryPotterObamaSonic10Inu"},
		},
	}
	price := map[string]map[string]float64{
		"bitcoin": {"usd": 64000.0, "usd_24h_change": 0.5},
	}
	srv := newCoinGeckoServer(t, search, price)
	defer srv.Close()

	result, err := newCryptoApp(t, srv.URL).getCryptoQuote("bitcoin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result, "BTC(Bitcoin):") {
		t.Errorf("long query should use top-ranked result, not meme coin symbol match, got: %q", result)
	}
}

// ── checkLineForRegexps – crypto dispatch ────────────────────────────────────

func TestCheckLine_CryptoPrefixLower(t *testing.T) {
	search := map[string]interface{}{
		"coins": []map[string]string{
			{"id": "bitcoin", "symbol": "BTC", "name": "Bitcoin"},
		},
	}
	price := map[string]map[string]float64{
		"bitcoin": {"usd": 64000.0, "usd_24h_change": 0.5},
	}
	srv := newCoinGeckoServer(t, search, price)
	defer srv.Close()

	app := newCryptoApp(t, srv.URL)
	cmd, err := app.checkLineForRegexps(`[Dino(#1234)] Dino says "gbs c:btc"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(cmd, "pose S> ") {
		t.Errorf("expected 'pose S> ' prefix, got: %q", cmd)
	}
	if !strings.Contains(cmd, "BTC(Bitcoin)") {
		t.Errorf("expected crypto result in output, got: %q", cmd)
	}
	if !strings.Contains(cmd, "24h") {
		t.Errorf("expected '24h' label in crypto output, got: %q", cmd)
	}
}

func TestCheckLine_CryptoPrefixUpper(t *testing.T) {
	search := map[string]interface{}{
		"coins": []map[string]string{
			{"id": "bitcoin", "symbol": "BTC", "name": "Bitcoin"},
		},
	}
	price := map[string]map[string]float64{
		"bitcoin": {"usd": 64000.0, "usd_24h_change": 0.5},
	}
	srv := newCoinGeckoServer(t, search, price)
	defer srv.Close()

	app := newCryptoApp(t, srv.URL)
	cmd, err := app.checkLineForRegexps(`[Dino(#1234)] Dino says "gbs C:btc"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(cmd, "pose S> ") {
		t.Errorf("expected 'pose S> ' prefix for uppercase C: prefix, got: %q", cmd)
	}
	if !strings.Contains(cmd, "BTC(Bitcoin)") {
		t.Errorf("expected crypto result in output, got: %q", cmd)
	}
}

func TestCheckLine_CryptoPrefixMixedCase(t *testing.T) {
	// "C:biTcOin" — upper C:, mixed-case name query
	search := map[string]interface{}{
		"coins": []map[string]string{
			{"id": "bitcoin", "symbol": "BTC", "name": "Bitcoin"},
		},
	}
	price := map[string]map[string]float64{
		"bitcoin": {"usd": 64000.0, "usd_24h_change": 0.5},
	}
	srv := newCoinGeckoServer(t, search, price)
	defer srv.Close()

	app := newCryptoApp(t, srv.URL)
	cmd, err := app.checkLineForRegexps(`[Dino(#1234)] Dino says "gbs C:biTcOin"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(cmd, "pose S> ") {
		t.Errorf("expected 'pose S> ' prefix, got: %q", cmd)
	}
	if !strings.Contains(cmd, "BTC(Bitcoin)") {
		t.Errorf("expected crypto result in output, got: %q", cmd)
	}
}

func TestCheckLine_CryptoNoMatchOnStockLine(t *testing.T) {
	// A plain stock ticker without c: prefix must NOT route to crypto.
	// We check that the output does not contain the "24h" crypto label.
	// (The stock API call will fail with no key, returning a Stock error.)
	app := newTestApp()
	cmd, err := app.checkLineForRegexps(`[Dino(#1234)] Dino says "gbs AAPL"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(cmd, "24h") {
		t.Errorf("stock line should not produce crypto (24h) output, got: %q", cmd)
	}
	if strings.HasPrefix(cmd, "pose H>") {
		t.Errorf("stock line must not trigger horoscope handler, got: %q", cmd)
	}
}

// ── CallTELNET / go-telnet integration ───────────────────────────────────────

// caller must satisfy telnet.Caller for telnet.DialToAndCall in main to
// compile. go-telnet is an untagged module pinned by pseudo-version, so this
// assertion turns a future interface change into a clear compile-time failure
// here rather than an error at the dial site.
var _ telnet.Caller = caller{}

// newTelnetTestApp returns an application with a real (capturable) info log
// and the given credentials, plus the buffer its info log writes to.
func newTelnetTestApp(username, password string) (*application, *bytes.Buffer) {
	logBuf := &bytes.Buffer{}
	return &application{
		config:   config{username: username, password: password},
		infoLog:  log.New(logBuf, "", 0),
		errorLog: log.New(io.Discard, "", 0),
	}, logBuf
}

// runCallTELNET drives CallTELNET to completion, failing the test rather than
// hanging the suite if the read loop does not terminate.
func runCallTELNET(t *testing.T, app *application, r telnet.Reader) string {
	t.Helper()
	var out bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		caller{app: *app}.CallTELNET(nil, &out, r)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("CallTELNET did not return; read loop failed to terminate on reader error")
	}
	return out.String()
}

// errReader always fails, exercising the loop's error exit.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestCallTELNET_WritesConnectHandshake(t *testing.T) {
	app, _ := newTelnetTestApp("gravybot", "s3cret")
	got := runCallTELNET(t, app, strings.NewReader(""))
	if !strings.HasPrefix(got, "connect gravybot s3cret\n") {
		t.Errorf("expected connect handshake first, got: %q", got)
	}
}

func TestCallTELNET_LogsConnectWithoutPassword(t *testing.T) {
	app, logBuf := newTelnetTestApp("gravybot", "hunter2")
	runCallTELNET(t, app, strings.NewReader(""))
	logged := logBuf.String()
	if !strings.Contains(logged, "connect gravybot <password>") {
		t.Errorf("expected redacted connect line in log, got: %q", logged)
	}
	if strings.Contains(logged, "hunter2") {
		t.Errorf("password leaked into info log: %q", logged)
	}
}

func TestCallTELNET_LogsUsernameContainingFormatVerbs(t *testing.T) {
	// Regression: the connect line was logged as
	//   Printf("connect " + username + " <password>\n")
	// which fed the username to Printf as part of the format string, so a
	// username containing a verb was interpreted as formatting instead of
	// being printed literally.
	app, logBuf := newTelnetTestApp("bot%s%d%v", "s3cret")
	runCallTELNET(t, app, strings.NewReader(""))
	logged := logBuf.String()
	if !strings.Contains(logged, "connect bot%s%d%v <password>") {
		t.Errorf("username with format verbs not logged literally: %q", logged)
	}
	if strings.Contains(logged, "%!") {
		t.Errorf("format-verb artifacts in log: %q", logged)
	}
}

func TestCallTELNET_DispatchesMatchedLine(t *testing.T) {
	app, _ := newTelnetTestApp("gravybot", "s3cret")
	got := runCallTELNET(t, app, strings.NewReader("[Dino(#1234)] Dino pages: hangout\n"))
	if !strings.Contains(got, "@@\n") {
		t.Errorf("expected keepalive in output, got: %q", got)
	}
	if !strings.Contains(got, "@dolist me={gautoreturn on;hangout}\n") {
		t.Errorf("expected hangout command in output, got: %q", got)
	}
}

func TestCallTELNET_UnmatchedLineSendsOnlyKeepalive(t *testing.T) {
	app, _ := newTelnetTestApp("gravybot", "s3cret")
	got := runCallTELNET(t, app, strings.NewReader("some random mush output line\n"))
	rest := strings.TrimPrefix(got, "connect gravybot s3cret\n")
	if rest != "@@\n" {
		t.Errorf("expected only keepalive after handshake, got: %q", rest)
	}
}

func TestCallTELNET_ProcessesMultipleLines(t *testing.T) {
	app, _ := newTelnetTestApp("gravybot", "s3cret")
	input := "[Dino(#1234)] Dino pages: hangout\n[Dino(#1234)] Dino pages: home\n"
	got := runCallTELNET(t, app, strings.NewReader(input))
	if !strings.Contains(got, "@dolist me={gautoreturn on;hangout}\n") {
		t.Errorf("first line not dispatched: %q", got)
	}
	if !strings.Contains(got, "@dolist me={gautoreturn off;home}\n") {
		t.Errorf("second line not dispatched: %q", got)
	}
	if n := strings.Count(got, "@@\n"); n != 2 {
		t.Errorf("expected 2 keepalives for 2 lines, got %d: %q", n, got)
	}
}

func TestCallTELNET_IgnoresUnterminatedTrailingLine(t *testing.T) {
	// Dispatch is driven by '\n'; a partial line at EOF must not fire.
	app, _ := newTelnetTestApp("gravybot", "s3cret")
	got := runCallTELNET(t, app, strings.NewReader("[Dino(#1234)] Dino pages: hangout"))
	if strings.Contains(got, "@dolist") {
		t.Errorf("unterminated line should not dispatch, got: %q", got)
	}
}

func TestCallTELNET_TerminatesOnReadError(t *testing.T) {
	app, _ := newTelnetTestApp("gravybot", "s3cret")
	got := runCallTELNET(t, app, errReader{})
	if !strings.HasPrefix(got, "connect gravybot s3cret\n") {
		t.Errorf("expected handshake before read error, got: %q", got)
	}
}

// ── Open-Meteo helpers ───────────────────────────────────────────────────────

func TestWeatherCodeText(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{0, "Clear"},
		{3, "Overcast"},
		{45, "Fog"},
		{63, "Rain"},
		{75, "Heavy snow"},
		{95, "Thunderstorm"},
		{99, "Thunderstorm with heavy hail"},
		{7, "Unknown"},   // gap inside the WMO table
		{999, "Unknown"}, // outside it entirely
	}
	for _, tc := range cases {
		if got := weatherCodeText(tc.code); got != tc.want {
			t.Errorf("weatherCodeText(%d) = %q, want %q", tc.code, got, tc.want)
		}
	}
}

func TestWindCompass(t *testing.T) {
	cases := []struct {
		deg  float64
		want string
	}{
		{0, "N"},
		{11, "N"},   // rounds down into the north sector
		{12, "NNE"}, // first bearing that rounds up out of it
		{45, "NE"},
		{90, "E"},
		{180, "S"},
		{262, "W"},
		{270, "W"},
		{348, "NNW"}, // last bearing in the NNW sector
		{349, "N"},   // wraps back to north
		{360, "N"},
		{-10, "N"},   // negative bearings must not index out of range
		{-30, "NNW"}, // and must wrap forwards, not panic
	}
	for _, tc := range cases {
		if got := windCompass(tc.deg); got != tc.want {
			t.Errorf("windCompass(%v) = %q, want %q", tc.deg, got, tc.want)
		}
	}
}

func TestCToF(t *testing.T) {
	cases := []struct{ c, want float64 }{
		{0, 32},
		{100, 212},
		{-40, -40},
		{24.5, 76.1},
	}
	for _, tc := range cases {
		if got := cToF(tc.c); math.Abs(got-tc.want) > 0.001 {
			t.Errorf("cToF(%v) = %v, want %v", tc.c, got, tc.want)
		}
	}
}

func TestKphToMph(t *testing.T) {
	if got := kphToMph(1.609344); math.Abs(got-1) > 0.0001 {
		t.Errorf("kphToMph(1.609344) = %v, want 1", got)
	}
	if got := kphToMph(0); got != 0 {
		t.Errorf("kphToMph(0) = %v, want 0", got)
	}
}

// ── isLatLon ─────────────────────────────────────────────────────────────────

func TestIsLatLon(t *testing.T) {
	cases := []struct {
		in      string
		wantLat float64
		wantLon float64
		wantOK  bool
	}{
		{"39.7392,-104.9903", 39.7392, -104.9903, true},
		{" 39.7392 , -104.9903 ", 39.7392, -104.9903, true},
		{"0,0", 0, 0, true},
		{"denver", 0, 0, false},
		{"80202", 0, 0, false}, // a bare zip is a place, not half a coordinate
		{"75001", 0, 0, false},
		{"denver,co", 0, 0, false},
		{"39.7392", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, tc := range cases {
		lat, lon, ok := isLatLon(tc.in)
		if ok != tc.wantOK || lat != tc.wantLat || lon != tc.wantLon {
			t.Errorf("isLatLon(%q) = (%v, %v, %v), want (%v, %v, %v)",
				tc.in, lat, lon, ok, tc.wantLat, tc.wantLon, tc.wantOK)
		}
	}
}

// ── matchPlace ───────────────────────────────────────────────────────────────

func TestMatchPlace(t *testing.T) {
	londons := []OpenMeteoPlace{
		{Name: "London", CountryCode: "GB", Country: "United Kingdom", Admin1: "England"},
		{Name: "London", CountryCode: "CA", Country: "Canada", Admin1: "Ontario"},
		{Name: "London", CountryCode: "US", Country: "United States", Admin1: "Ohio"},
	}
	cases := []struct {
		qualifier   string
		wantCountry string
		wantOK      bool
	}{
		{"england", "GB", true},        // admin1, exact
		{"canada", "CA", true},         // country, exact
		{"gb", "GB", true},             // country code
		{"uk", "GB", true},             // country alias
		{"oh", "US", true},             // US state abbreviation
		{"United Kingdom", "GB", true}, // case-insensitive country
		{"onta", "CA", true},           // prefix fallback
		{"france", "", false},          // no hit
		{"", "", false},                // empty qualifier never matches
	}
	for _, tc := range cases {
		place, ok := matchPlace(londons, tc.qualifier)
		if ok != tc.wantOK || place.CountryCode != tc.wantCountry {
			t.Errorf("matchPlace(%q) = (%q, %v), want (%q, %v)",
				tc.qualifier, place.CountryCode, ok, tc.wantCountry, tc.wantOK)
		}
	}
}

func TestMatchPlace_EmptyCandidates(t *testing.T) {
	if _, ok := matchPlace(nil, "england"); ok {
		t.Error("matchPlace(nil) reported a match")
	}
}

// ── Open-Meteo stub server ───────────────────────────────────────────────────

// newOpenMeteoApp points an application at a stub standing in for all three
// Open-Meteo services. geocode is handed the requested name so a test can vary
// its answer per lookup, which is how the qualifier retries are exercised.
func newOpenMeteoApp(t *testing.T, geocode func(name string) string, forecast, airQuality string) *application {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, geocode(r.URL.Query().Get("name")))
	})
	mux.HandleFunc("/forecast", func(w http.ResponseWriter, r *http.Request) {
		if forecast == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		io.WriteString(w, forecast)
	})
	mux.HandleFunc("/air-quality", func(w http.ResponseWriter, r *http.Request) {
		if airQuality == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		io.WriteString(w, airQuality)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	app := newTestApp()
	app.config.openMeteoGeocodeURL = srv.URL + "/search"
	app.config.openMeteoForecastURL = srv.URL + "/forecast"
	app.config.openMeteoAirQualityURL = srv.URL + "/air-quality"
	return app
}

const stubForecast = `{"current":{"temperature_2m":24.5,"relative_humidity_2m":19,"wind_speed_10m":8.9,"wind_direction_10m":110,"weather_code":0}}`

const stubAirQuality = `{"current":{"us_aqi":49}}`

func geocodeEmpty(string) string { return `{}` }

// ── resolvePlace ─────────────────────────────────────────────────────────────

func TestResolvePlace_DirectHit(t *testing.T) {
	app := newOpenMeteoApp(t, func(string) string {
		return `{"results":[{"name":"Denver","latitude":39.74,"longitude":-104.98,"country_code":"US","country":"United States","admin1":"Colorado"}]}`
	}, stubForecast, stubAirQuality)

	place, found, err := app.resolvePlace("denver")
	if err != nil || !found {
		t.Fatalf("resolvePlace = (found %v, err %v), want a hit", found, err)
	}
	if place.Name != "Denver" || place.Admin1 != "Colorado" {
		t.Errorf("resolved %+v, want Denver/Colorado", place)
	}
}

// The whole-string lookup misses for "london england" because the geocoder
// indexes bare place names; the retry has to drop "england" and use it to pick
// between the Londons.
func TestResolvePlace_QualifierRetry(t *testing.T) {
	var asked []string
	app := newOpenMeteoApp(t, func(name string) string {
		asked = append(asked, name)
		if name != "london" {
			return `{}`
		}
		return `{"results":[
			{"name":"London","country_code":"CA","country":"Canada","admin1":"Ontario"},
			{"name":"London","country_code":"GB","country":"United Kingdom","admin1":"England"}
		]}`
	}, stubForecast, stubAirQuality)

	place, found, err := app.resolvePlace("london england")
	if err != nil || !found {
		t.Fatalf("resolvePlace = (found %v, err %v), want a hit", found, err)
	}
	if place.CountryCode != "GB" {
		t.Errorf("resolved %+v, want the English London", place)
	}
	if len(asked) != 2 || asked[0] != "london england" || asked[1] != "london" {
		t.Errorf("geocoder asked for %v, want the full string then the bare name", asked)
	}
}

// A city name can itself be several words, so the retry has to shorten from the
// right rather than assume the first word is the city.
func TestResolvePlace_MultiWordCity(t *testing.T) {
	app := newOpenMeteoApp(t, func(name string) string {
		if name != "salt lake city" {
			return `{}`
		}
		return `{"results":[{"name":"Salt Lake City","country_code":"US","country":"United States","admin1":"Utah"}]}`
	}, stubForecast, stubAirQuality)

	place, found, err := app.resolvePlace("salt lake city utah")
	if err != nil || !found {
		t.Fatalf("resolvePlace = (found %v, err %v), want a hit", found, err)
	}
	if place.Name != "Salt Lake City" {
		t.Errorf("resolved %+v, want Salt Lake City", place)
	}
}

func TestResolvePlace_NotFound(t *testing.T) {
	app := newOpenMeteoApp(t, geocodeEmpty, stubForecast, stubAirQuality)

	if _, found, err := app.resolvePlace("vars ontario"); found || err != nil {
		t.Errorf("resolvePlace = (found %v, err %v), want no hit and no error", found, err)
	}
}

func TestResolvePlace_Empty(t *testing.T) {
	app := newOpenMeteoApp(t, geocodeEmpty, stubForecast, stubAirQuality)

	if _, found, _ := app.resolvePlace("   "); found {
		t.Error("resolvePlace(blank) reported a hit")
	}
}

// ── sendWeatherRequest ───────────────────────────────────────────────────────

func TestSendWeatherRequest_USUsesTheStateAsRegion(t *testing.T) {
	app := newOpenMeteoApp(t, func(string) string {
		return `{"results":[{"name":"Denver","latitude":39.74,"longitude":-104.98,"country_code":"US","country":"United States","admin1":"Colorado"}]}`
	}, stubForecast, stubAirQuality)

	got, err := app.sendWeatherRequest("denver")
	if err != nil {
		t.Fatalf("sendWeatherRequest: %v", err)
	}
	want := "Denver, Colorado: Clear 24.5C/76.1F 19.0%% 8.9kph/5.5mph ESE Good:49\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestSendWeatherRequest_NonUSUsesTheCountryAsRegion(t *testing.T) {
	app := newOpenMeteoApp(t, func(string) string {
		return `{"results":[{"name":"London","latitude":51.5,"longitude":-0.13,"country_code":"GB","country":"United Kingdom","admin1":"England"}]}`
	}, stubForecast, stubAirQuality)

	got, err := app.sendWeatherRequest("london")
	if err != nil {
		t.Fatalf("sendWeatherRequest: %v", err)
	}
	want := "London, United Kingdom: Clear 24.5C/76.1F 19.0%% 8.9kph/5.5mph ESE Good:49\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// Coordinates skip the geocoder entirely, so the stub would fail the test if
// they did not: it answers every lookup with no results. The name comes off the
// embedded city table instead.
func TestSendWeatherRequest_Coordinates(t *testing.T) {
	app := newOpenMeteoApp(t, geocodeEmpty, stubForecast, stubAirQuality)

	got, err := app.sendWeatherRequest("39.7392,-104.9903")
	if err != nil {
		t.Fatalf("sendWeatherRequest: %v", err)
	}
	want := "Denver, Colorado: Clear 24.5C/76.1F 19.0%% 8.9kph/5.5mph ESE Good:49\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestSendWeatherRequest_NotFound(t *testing.T) {
	app := newOpenMeteoApp(t, geocodeEmpty, stubForecast, stubAirQuality)

	got, err := app.sendWeatherRequest("vars ontario")
	if err != nil {
		t.Fatalf("sendWeatherRequest: %v", err)
	}
	if !strings.Contains(got, "vars ontario not found") {
		t.Errorf("got %q, want a not-found message naming the location", got)
	}
}

// A null reading is the API saying it has no data for the point, which must not
// print as an AQI of 0 ("Good:0").
func TestSendWeatherRequest_OmitsAQIWhenNull(t *testing.T) {
	app := newOpenMeteoApp(t, func(string) string {
		return `{"results":[{"name":"London","country_code":"GB","country":"United Kingdom"}]}`
	}, stubForecast, `{"current":{"us_aqi":null}}`)

	got, err := app.sendWeatherRequest("london")
	if err != nil {
		t.Fatalf("sendWeatherRequest: %v", err)
	}
	if strings.Contains(got, "Good") || strings.Contains(got, ":0") {
		t.Errorf("got %q, want no AQI segment", got)
	}
	if !strings.HasSuffix(got, "ESE\n") {
		t.Errorf("got %q, want the line to end at the wind direction", got)
	}
}

// Air quality is a separate service, so losing it must not cost the forecast.
func TestSendWeatherRequest_SurvivesAQIFailure(t *testing.T) {
	app := newOpenMeteoApp(t, func(string) string {
		return `{"results":[{"name":"London","country_code":"GB","country":"United Kingdom"}]}`
	}, stubForecast, "")

	got, err := app.sendWeatherRequest("london")
	if err != nil {
		t.Fatalf("sendWeatherRequest: %v", err)
	}
	if !strings.HasPrefix(got, "London, United Kingdom: Clear") {
		t.Errorf("got %q, want the forecast despite the air quality failure", got)
	}
}

func TestSendWeatherRequest_ZeroAQIIsReported(t *testing.T) {
	app := newOpenMeteoApp(t, func(string) string {
		return `{"results":[{"name":"London","country_code":"GB","country":"United Kingdom"}]}`
	}, stubForecast, `{"current":{"us_aqi":0}}`)

	got, err := app.sendWeatherRequest("london")
	if err != nil {
		t.Fatalf("sendWeatherRequest: %v", err)
	}
	if !strings.HasSuffix(got, "Good:0\n") {
		t.Errorf("got %q, want a genuine zero reading to print", got)
	}
}

// The forecast is the one call gbw cannot do without, so its failure has to
// surface as an error rather than a half-filled line.
func TestSendWeatherRequest_ForecastFailureIsAnError(t *testing.T) {
	app := newOpenMeteoApp(t, func(string) string {
		return `{"results":[{"name":"London","country_code":"GB","country":"United Kingdom"}]}`
	}, "", stubAirQuality)

	if _, err := app.sendWeatherRequest("london"); err == nil {
		t.Error("sendWeatherRequest returned no error when the forecast API failed")
	}
}

func TestGeocodeSearch_RejectsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	app := newTestApp()
	app.config.openMeteoGeocodeURL = srv.URL

	if _, err := app.geocodeSearch("london", 1); err == nil {
		t.Error("geocodeSearch accepted an HTTP 429")
	}
}

func TestGeocodeSearch_RejectsMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "not json")
	}))
	t.Cleanup(srv.Close)

	app := newTestApp()
	app.config.openMeteoGeocodeURL = srv.URL

	if _, err := app.geocodeSearch("london", 1); err == nil {
		t.Error("geocodeSearch accepted a malformed body")
	}
}

// The name has to survive URL encoding or multi-word lookups silently miss.
func TestGeocodeSearch_EscapesTheName(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("name")
		io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)

	app := newTestApp()
	app.config.openMeteoGeocodeURL = srv.URL

	if _, err := app.geocodeSearch("salt lake city", 5); err != nil {
		t.Fatalf("geocodeSearch: %v", err)
	}
	if got != "salt lake city" {
		t.Errorf("server saw name=%q, want %q", got, "salt lake city")
	}
}

// "ca" has to expand to California before matching, otherwise it collides with
// Canada's country code and picks the wrong continent.
func TestMatchPlace_StateAbbrevBeatsCountryCode(t *testing.T) {
	places := []OpenMeteoPlace{
		{Name: "London", CountryCode: "CA", Country: "Canada", Admin1: "Ontario"},
		{Name: "Lancaster", CountryCode: "US", Country: "United States", Admin1: "California"},
	}
	place, ok := matchPlace(places, "ca")
	if !ok || place.Admin1 != "California" {
		t.Errorf("matchPlace(%q) = (%+v, %v), want California", "ca", place, ok)
	}
}

// Every reading carries both unit systems, so neither an American nor a
// metric reader has to convert in their head.
func TestSendWeatherRequest_AlwaysShowsBothUnits(t *testing.T) {
	app := newOpenMeteoApp(t, func(string) string {
		return `{"results":[{"name":"Reykjavik","country_code":"IS","country":"Iceland"}]}`
	}, `{"current":{"temperature_2m":0,"relative_humidity_2m":80,"wind_speed_10m":1.609344,"wind_direction_10m":0,"weather_code":3}}`, stubAirQuality)

	got, err := app.sendWeatherRequest("reykjavik")
	if err != nil {
		t.Fatalf("sendWeatherRequest: %v", err)
	}
	want := "Reykjavik, Iceland: Overcast 0.0C/32.0F 80.0%% 1.6kph/1.0mph N Good:49\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// A bare zip has to survive coordinate parsing and come back off the embedded
// table, without the geocoder being consulted at all.
func TestSendWeatherRequest_ZipCode(t *testing.T) {
	var asked []string
	app := newOpenMeteoApp(t, func(name string) string {
		asked = append(asked, name)
		return `{}`
	}, stubForecast, stubAirQuality)

	got, err := app.sendWeatherRequest("80202")
	if err != nil {
		t.Fatalf("sendWeatherRequest: %v", err)
	}
	if len(asked) != 0 {
		t.Errorf("geocoder called with %v, want the zip settled from the table", asked)
	}
	if !strings.HasPrefix(got, "Denver, Colorado:") {
		t.Errorf("got %q, want the zip resolved to Denver", got)
	}
}

// The stub geocoder answers every lookup with no results, so an airport code
// that still resolves can only have come from the embedded table.
func TestSendWeatherRequest_AirportCode(t *testing.T) {
	app := newOpenMeteoApp(t, geocodeEmpty, stubForecast, stubAirQuality)

	cases := []struct {
		query string
		want  string
	}{
		{"LHR", "London, United Kingdom:"},
		{"den", "Denver, Colorado:"},
		{"iata:NRT", "Narita, Japan:"},
		{"NYC", "New York, New York:"},
	}
	for _, tc := range cases {
		got, err := app.sendWeatherRequest(tc.query)
		if err != nil {
			t.Errorf("sendWeatherRequest(%q): %v", tc.query, err)
			continue
		}
		if !strings.HasPrefix(got, tc.want) {
			t.Errorf("sendWeatherRequest(%q) = %q, want it to start %q", tc.query, got, tc.want)
		}
	}
}

// An unknown three-letter code must fall through to the geocoder rather than
// dead-ending in the airport table.
func TestSendWeatherRequest_UnknownAirportCodeFallsThrough(t *testing.T) {
	var asked []string
	app := newOpenMeteoApp(t, func(name string) string {
		asked = append(asked, name)
		return `{"results":[{"name":"Zzz","country_code":"NL","country":"Netherlands"}]}`
	}, stubForecast, stubAirQuality)

	got, err := app.sendWeatherRequest("ZZZ")
	if err != nil {
		t.Fatalf("sendWeatherRequest: %v", err)
	}
	if len(asked) == 0 {
		t.Fatal("geocoder was never consulted for an unknown code")
	}
	if !strings.HasPrefix(got, "Zzz, Netherlands:") {
		t.Errorf("got %q, want the geocoded result", got)
	}
}

// ── Open-Meteo transport failures ────────────────────────────────────────────

// unreachableURL is a server that is guaranteed not to answer, so a caller's
// transport error path runs without waiting on a timeout.
func unreachableURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	return url
}

func TestGeocodeSearch_TransportError(t *testing.T) {
	app := newTestApp()
	app.config.openMeteoGeocodeURL = unreachableURL(t)

	if _, err := app.geocodeSearch("london", 1); err == nil {
		t.Error("geocodeSearch returned no error when the host was unreachable")
	}
}

func TestFetchCurrentWeather_TransportError(t *testing.T) {
	app := newTestApp()
	app.config.openMeteoForecastURL = unreachableURL(t)

	if _, err := app.fetchCurrentWeather(51.5, -0.13); err == nil {
		t.Error("fetchCurrentWeather returned no error when the host was unreachable")
	}
}

func TestFetchCurrentWeather_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "{not json")
	}))
	t.Cleanup(srv.Close)

	app := newTestApp()
	app.config.openMeteoForecastURL = srv.URL

	if _, err := app.fetchCurrentWeather(51.5, -0.13); err == nil {
		t.Error("fetchCurrentWeather accepted a malformed body")
	}
}

func TestFetchUSAQI_TransportError(t *testing.T) {
	app := newTestApp()
	app.config.openMeteoAirQualityURL = unreachableURL(t)

	if _, found, err := app.fetchUSAQI(51.5, -0.13); err == nil || found {
		t.Errorf("fetchUSAQI = (found %v, err %v), want a failure", found, err)
	}
}

func TestFetchUSAQI_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "{not json")
	}))
	t.Cleanup(srv.Close)

	app := newTestApp()
	app.config.openMeteoAirQualityURL = srv.URL

	if _, found, err := app.fetchUSAQI(51.5, -0.13); err == nil || found {
		t.Errorf("fetchUSAQI = (found %v, err %v), want a failure", found, err)
	}
}

// The retry lookups are just as capable of failing as the first one, and a
// transport error there must not read as "no such place".
func TestResolvePlace_PropagatesRetryError(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			io.WriteString(w, `{}`)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	app := newTestApp()
	app.config.openMeteoGeocodeURL = srv.URL

	_, found, err := app.resolvePlace("london england")
	if err == nil {
		t.Error("resolvePlace swallowed a failure in the retry lookup")
	}
	if found {
		t.Error("resolvePlace reported a hit despite the failure")
	}
}

// ── checkLineForRegexps – weather dispatch ───────────────────────────────────

func weatherLine(query string) string {
	return `[Dino(#1234)] Dino says "gravybot weather ` + query + `"`
}

func TestCheckLine_WeatherSingleLocation(t *testing.T) {
	app := newOpenMeteoApp(t, func(string) string {
		return `{"results":[{"name":"Denver","country_code":"US","country":"United States","admin1":"Colorado"}]}`
	}, stubForecast, stubAirQuality)

	cmd, err := app.checkLineForRegexps(weatherLine("denver"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "pose W> Denver, Colorado: Clear 24.5C/76.1F 19.0%% 8.9kph/5.5mph ESE Good:49\n"
	if cmd != want {
		t.Errorf("got  %q\nwant %q", cmd, want)
	}
}

func TestCheckLine_WeatherSplitsOnCommas(t *testing.T) {
	app := newOpenMeteoApp(t, func(name string) string {
		return `{"results":[{"name":"` + strings.ToUpper(name[:1]) + name[1:] + `","country_code":"FR","country":"France"}]}`
	}, stubForecast, stubAirQuality)

	cmd, err := app.checkLineForRegexps(weatherLine("paris, lyon, nice"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.Count(cmd, "pose W> "); got != 3 {
		t.Errorf("got %d poses, want 3: %q", got, cmd)
	}
	for _, city := range []string{"Paris", "Lyon", "Nice"} {
		if !strings.Contains(cmd, city+", France") {
			t.Errorf("%s missing from %q", city, cmd)
		}
	}
}

// The cap keeps one line from turning into a wall of poses.
func TestCheckLine_WeatherCapsAtFiveLocations(t *testing.T) {
	var lookups int
	app := newOpenMeteoApp(t, func(name string) string {
		lookups++
		return `{"results":[{"name":"` + name + `","country_code":"FR","country":"France"}]}`
	}, stubForecast, stubAirQuality)

	cmd, err := app.checkLineForRegexps(weatherLine("a, b, c, d, e, f, g"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.Count(cmd, "pose W> "); got != 5 {
		t.Errorf("got %d poses, want 5", got)
	}
	if lookups != 5 {
		t.Errorf("made %d lookups, want 5 -- the surplus locations should never be fetched", lookups)
	}
}

func TestCheckLine_WeatherSkipsBlankSegments(t *testing.T) {
	app := newOpenMeteoApp(t, func(name string) string {
		if strings.TrimSpace(name) == "" {
			t.Errorf("geocoder asked for an empty location")
		}
		return `{"results":[{"name":"Paris","country_code":"FR","country":"France"}]}`
	}, stubForecast, stubAirQuality)

	cmd, err := app.checkLineForRegexps(weatherLine("paris, , ,"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.Count(cmd, "pose W> "); got != 1 {
		t.Errorf("got %d poses, want 1: %q", got, cmd)
	}
}

// A space-separated coordinate pair has to survive the dispatcher and reach
// sendWeatherRequest as a normalised pair, skipping the geocoder entirely.
func TestCheckLine_WeatherLatLonPair(t *testing.T) {
	app := newOpenMeteoApp(t, geocodeEmpty, stubForecast, stubAirQuality)

	cmd, err := app.checkLineForRegexps(weatherLine("39.7392 -104.9903"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "pose W> Denver, Colorado: Clear 24.5C/76.1F 19.0%% 8.9kph/5.5mph ESE Good:49\n"
	if cmd != want {
		t.Errorf("got  %q\nwant %q", cmd, want)
	}
}

// A failed lookup has to report as one bad location, not take the line down.
func TestCheckLine_WeatherReportsRequestFailure(t *testing.T) {
	app := newOpenMeteoApp(t, func(string) string {
		return `{"results":[{"name":"Paris","country_code":"FR","country":"France"}]}`
	}, "", stubAirQuality)

	cmd, err := app.checkLineForRegexps(weatherLine("paris"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != "pose W> Error: weather api call failed.\n" {
		t.Errorf("got %q, want the failure notice", cmd)
	}
}

func TestCheckLine_WeatherAirportCode(t *testing.T) {
	app := newOpenMeteoApp(t, geocodeEmpty, stubForecast, stubAirQuality)

	cmd, err := app.checkLineForRegexps(weatherLine("LHR"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(cmd, "pose W> London, United Kingdom:") {
		t.Errorf("got %q, want the airport resolved", cmd)
	}
}

// truncatedBodyURL promises more bytes than it delivers and then drops the
// connection, so the client fails while reading a response it already
// accepted. What it does send is deliberately valid JSON for all three
// endpoints: if the short read were ignored, the partial body would decode
// cleanly and a truncated response would surface as a confident 99C reading
// instead of an error.
func truncatedBodyURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "4096")
		io.WriteString(w, `{"results":[],"current":{"temperature_2m":99,"us_aqi":42}}`)
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestGeocodeSearch_TruncatedBody(t *testing.T) {
	app := newTestApp()
	app.config.openMeteoGeocodeURL = truncatedBodyURL(t)

	if _, err := app.geocodeSearch("london", 1); err == nil {
		t.Error("geocodeSearch accepted a truncated body")
	}
}

func TestFetchCurrentWeather_TruncatedBody(t *testing.T) {
	app := newTestApp()
	app.config.openMeteoForecastURL = truncatedBodyURL(t)

	forecast, err := app.fetchCurrentWeather(51.5, -0.13)
	if err == nil {
		t.Fatalf("fetchCurrentWeather accepted a truncated body, returning %+v", forecast.Current)
	}
}

func TestFetchUSAQI_TruncatedBody(t *testing.T) {
	app := newTestApp()
	app.config.openMeteoAirQualityURL = truncatedBodyURL(t)

	if _, found, err := app.fetchUSAQI(51.5, -0.13); err == nil || found {
		t.Errorf("fetchUSAQI = (found %v, err %v), want a failure", found, err)
	}
}

// ── ZIP fallback ─────────────────────────────────────────────────────────────

// The gap the table exists to close: the geocoder carries 81501 through 81506
// but not 81507, so an unlisted ZIP has to come off the embedded table with its
// own coordinates rather than the city's -- without a geocoder round trip.
func TestResolvePlace_ZipResolvesFromTable(t *testing.T) {
	var asked []string
	app := newOpenMeteoApp(t, func(name string) string {
		asked = append(asked, name)
		return `{}`
	}, stubForecast, stubAirQuality)

	place, found, err := app.resolvePlace("81507")
	if err != nil {
		t.Fatalf("resolvePlace: %v", err)
	}
	if !found {
		t.Fatal("resolvePlace(81507) found nothing, want the ZIP table to answer")
	}
	if place.Name != "Grand Junction" || place.Admin1 != "Colorado" {
		t.Errorf("got %q/%q, want Grand Junction/Colorado", place.Name, place.Admin1)
	}
	if place.Latitude != 39.0157 || place.Longitude != -108.6129 {
		t.Errorf("got %v,%v, want the ZIP's own 39.0157,-108.6129", place.Latitude, place.Longitude)
	}
	if len(asked) != 0 {
		t.Errorf("geocoder called %d times (%v), want the table to answer without one", len(asked), asked)
	}
}

// Five digit codes are not ours alone: 75001 is Paris as well as Addison,
// Texas, and 28001 is Madrid as well as Albemarle, North Carolina. A bare five
// digit number is read as a US ZIP, so the table answers even when the
// geocoder would happily have named the foreign city.
func TestResolvePlace_ZipTableOutranksGeocoder(t *testing.T) {
	var asked []string
	app := newOpenMeteoApp(t, func(name string) string {
		asked = append(asked, name)
		return `{"results":[{"name":"Paris","latitude":48.85,"longitude":2.35,"country_code":"FR","country":"France"}]}`
	}, stubForecast, stubAirQuality)

	place, found, err := app.resolvePlace("75001")
	if err != nil {
		t.Fatalf("resolvePlace: %v", err)
	}
	if !found || place.Name != "Addison" || place.Admin1 != "Texas" {
		t.Errorf("got %q/%q, want Addison/Texas rather than the Paris arrondissement",
			place.Name, place.Admin1)
	}
	if len(asked) != 0 {
		t.Errorf("geocoder called %d times (%v), want the ZIP settled locally", len(asked), asked)
	}
}

// The table must not turn a genuine miss into a wrong answer.
func TestResolvePlace_MissIsStillAMiss(t *testing.T) {
	app := newOpenMeteoApp(t, geocodeEmpty, stubForecast, stubAirQuality)

	for _, loc := range []string{"nowheresville", "00000", "8150"} {
		place, found, err := app.resolvePlace(loc)
		if err != nil {
			t.Fatalf("resolvePlace(%q): %v", loc, err)
		}
		if found {
			t.Errorf("resolvePlace(%q) = %q, want no match", loc, place.Name)
		}
	}
}

func TestSendWeatherRequest_UnlistedZip(t *testing.T) {
	app := newOpenMeteoApp(t, geocodeEmpty, stubForecast, stubAirQuality)

	got, err := app.sendWeatherRequest("81507")
	if err != nil {
		t.Fatalf("sendWeatherRequest: %v", err)
	}
	want := "Grand Junction, Colorado: Clear 24.5C/76.1F 19.0%% 8.9kph/5.5mph ESE Good:49\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// A territory ZIP has to print its territory where a state would go, so
// "San Juan, Puerto Rico" reads the way "Denver, Colorado" does.
func TestSendWeatherRequest_TerritoryZip(t *testing.T) {
	app := newOpenMeteoApp(t, geocodeEmpty, stubForecast, stubAirQuality)

	got, err := app.sendWeatherRequest("00901")
	if err != nil {
		t.Fatalf("sendWeatherRequest: %v", err)
	}
	want := "San Juan, Puerto Rico: Clear 24.5C/76.1F 19.0%% 8.9kph/5.5mph ESE Good:49\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
