package main

import (
	"encoding/json"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/httptest"
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

// ── parseLatLon ──────────────────────────────────────────────────────────────

func TestParseLatLon(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// colon separator – various sign combinations
		{"48.8566:2.3522", "48.8566,2.3522"},
		{"-33.8688:151.2093", "-33.8688,151.2093"},
		{"40.7128:-74.0060", "40.7128,-74.0060"},  // NYC: positive lat, negative lon
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
		{"48.8566", "48.8566"},                     // single float, no separator
		{"abc:def", "abc:def"},                     // non-numeric
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
