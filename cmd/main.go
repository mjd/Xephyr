package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/reiver/go-telnet"
)

type config struct {
	srvAddr       string
	yirpAPIAddr   string
	yirpapikey    string
	username      string
	password      string
	weatherapikey    string
	finnhubapikey    string
	coingeckoapikey  string
	coingeckoBaseURL string
}

type application struct {
	config   config
	infoLog  *log.Logger
	errorLog *log.Logger
	version  string
}

var version string = "1.0"

func main() {
	var cfg config

	flag.StringVar(&cfg.srvAddr, "s", "dino.surly.org:6250", "Server:port address")
	flag.StringVar(&cfg.yirpAPIAddr, "yirpaddr", "https://api.yirp.org/v1/shorten", "Yirp API Address")

	flag.Parse()

	cfg.username = os.Getenv("BOT_USERNAME")
	cfg.password = os.Getenv("BOT_PASSWORD")
	cfg.yirpapikey = os.Getenv("YIRP_APIKEY")
	cfg.weatherapikey = os.Getenv("WEATHER_APIKEY")
	cfg.finnhubapikey = os.Getenv("FINNHUB_APIKEY")
	cfg.coingeckoapikey = os.Getenv("COINGECKO_APIKEY")
	cfg.coingeckoBaseURL = "https://api.coingecko.com/api/v3"

	infoLog := log.New(os.Stdout, "INFO\t", log.Ldate|log.Ltime)
	errorLog := log.New(os.Stdout, "ERROR\t", log.Ldate|log.Ltime|log.Lshortfile)

	app := &application{
		config:   cfg,
		infoLog:  infoLog,
		errorLog: errorLog,
		version:  version,
	}

	fmt.Println("Xepher MUSH Bot version:", app.version)

	err := telnet.DialToAndCall(app.config.srvAddr, caller{*app})

	if err != nil {
		log.Fatal(err)
	}
	if err != nil {
		errorLog.Fatal(err)
	}

}

type caller struct {
	app application
}

type YirpRequest struct {
	ApiKey  string `json:"api_key"`
	LongUrl string `json:"long_url"`
	Domain  string `json:"domain,omitempty"`
}

type YirpResponse struct {
	ShortUrl  string `json:"short_url"`
	LongUrl   string `json:"long_url"`
	CreatedAt string `json:"created_at"`
}

func (app *application) botSend(w telnet.Writer, data string) {
	app.infoLog.Println(data)
	_, err := w.Write([]byte(data))
	if err != nil {
		app.errorLog.Println(err)
	}
}

type WeatherAPIResponse struct {
	Location struct {
		Name    string  `json:"name"`
		Region  string  `json:"region"`
		Country string  `json:"country"`
		Lat     float64 `json:"lat"`
		Lon     float64 `json:"lon"`
	} `json:"location"`

	Current struct {
		Last_updated string  `json:"last_updated"`
		Temp_c       float64 `json:"temp_c"`
		Temp_f       float64 `json:"temp_f"`
		Condition    struct {
			Text string `json:"text"`
		} `json:"condition"`
		Wind_mph float64 `json:"wind_mph"`
		Wind_kph float64 `json:"wind_kph"`
		Wind_dir string  `json:"wind_dir"`
		Humidity float64 `json:"humidity"`
	} `json:"current"`
}

type FinnhubQuoteResponse struct {
	C  float64 `json:"c"`  // Current price
	D  float64 `json:"d"`  // Change
	Dp float64 `json:"dp"` // Percent change
	H  float64 `json:"h"`  // High price of the day
	L  float64 `json:"l"`  // Low price of the day
	O  float64 `json:"o"`  // Open price of the day
	Pc float64 `json:"pc"` // Previous close price
}

type FinnhubSearchResponse struct {
	Count  int `json:"count"`
	Result []struct {
		Description string `json:"description"`
		Symbol      string `json:"symbol"`
		Type        string `json:"type"`
	} `json:"result"`
}

type CoinGeckoSearchResponse struct {
	Coins []struct {
		ID     string `json:"id"`
		Symbol string `json:"symbol"`
		Name   string `json:"name"`
	} `json:"coins"`
}

func (app *application) translateText(sourceLang, targetLang, text string) (string, error) {
	// Build the Google Translate API URL
	baseURL := "https://translate.googleapis.com/translate_a/single"
	params := url.Values{}
	params.Add("client", "gtx")
	params.Add("sl", sourceLang)
	params.Add("tl", targetLang)
	params.Add("dt", "t")
	params.Add("dt", "rm") // Also get romanization
	params.Add("q", text)

	fullURL := baseURL + "?" + params.Encode()

	// Make the HTTP request
	res, err := http.Get(fullURL)
	if err != nil {
		app.errorLog.Printf("translation request failed: %s", err)
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode > 299 {
		result := "Translation error: API returned code: " + strconv.Itoa(res.StatusCode)
		app.errorLog.Println(result)
		return result, nil
	}

	// Read the response body
	body, err := io.ReadAll(res.Body)
	if err != nil {
		app.errorLog.Printf("Translation request failed ReadAll: %s", err)
		return "", err
	}

	// Parse the JSON response
	// The response is a nested array structure: [[[translated_text, original_text, ...]]]
	var response []interface{}
	err = json.Unmarshal(body, &response)
	if err != nil {
		app.errorLog.Printf("Translation response parse failed: %s", err)
		return "", err
	}

	// Extract the translated text
	var translatedText string
	if len(response) > 0 {
		if translations, ok := response[0].([]interface{}); ok && len(translations) > 0 {
			if translation, ok := translations[0].([]interface{}); ok && len(translation) > 0 {
				if text, ok := translation[0].(string); ok {
					translatedText = text
				}
			}
		}
	}

	if translatedText == "" {
		return "", fmt.Errorf("unable to parse translation response")
	}

	// Extract romanization of the translated text (if available)
	// Romanization is at response[0][1][2]
	var romanization string
	if len(response) > 0 {
		if translations, ok := response[0].([]interface{}); ok && len(translations) > 1 {
			if romArray, ok := translations[1].([]interface{}); ok && len(romArray) > 2 {
				if rom, ok := romArray[2].(string); ok && rom != "" {
					romanization = rom
				}
			}
		}
	}

	// If romanization exists and differs from the translated text, include it
	// Use \ to escape brackets for MUSH
	if romanization != "" && romanization != translatedText {
		return translatedText + " \\[" + romanization + "\\]", nil
	}

	return translatedText, nil
}

func (app *application) sendWeatherRequest(query string) (string, error) {
	res, err := http.Get("https://api.weatherapi.com/v1/current.json?key=" + app.config.weatherapikey + "&q=" + query + "&aqi=no")

	if err != nil {
		app.errorLog.Printf("weather request failed: %s", err)
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode == 400 {
		result := "Weather error: " + query + " not found. Try using a city state or city country pair.\n"
		fmt.Println(result)
		return result, nil
	}

	if res.StatusCode > 299 {
		result := "Weather error: API returned code: " + strconv.Itoa(res.StatusCode) + "\n"
		fmt.Println(result)
		return result, nil
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		app.errorLog.Printf("Weather request failed ReadAll: %s", err)
		return "", err
	}

	var weatherResponse WeatherAPIResponse
	err = json.Unmarshal(body, &weatherResponse)
	if err != nil {
		fmt.Println("error:", err)
	}

	var locationRegion string
	var result string

	if strings.HasPrefix(weatherResponse.Location.Country, "United States of America") || strings.HasPrefix(weatherResponse.Location.Country, "USA") {
		locationRegion = weatherResponse.Location.Region
		result = fmt.Sprintf("%v, %v: %v %.1fF %.1f%%%% %.1fmph %v\n", weatherResponse.Location.Name, locationRegion, weatherResponse.Current.Condition.Text, weatherResponse.Current.Temp_f, weatherResponse.Current.Humidity, weatherResponse.Current.Wind_mph, weatherResponse.Current.Wind_dir)
	} else {
		locationRegion = weatherResponse.Location.Country
		result = fmt.Sprintf("%v, %v: %v %.1fC %.1f%%%% %.1fkph %v\n", weatherResponse.Location.Name, locationRegion, weatherResponse.Current.Condition.Text, weatherResponse.Current.Temp_c, weatherResponse.Current.Humidity, weatherResponse.Current.Wind_kph, weatherResponse.Current.Wind_dir)
	}

	return result, nil
}

func (app *application) getStockQuote(query string) (string, error) {
	query = strings.TrimSpace(query)
	symbol := strings.ToUpper(query)
	companyName := ""

	// If query doesn't look like a ticker symbol, search for it first
	if len(query) > 5 || strings.Contains(query, " ") {
		searchURL := fmt.Sprintf("https://finnhub.io/api/v1/search?q=%s&token=%s",
			url.QueryEscape(query), app.config.finnhubapikey)

		res, err := http.Get(searchURL)
		if err != nil {
			app.errorLog.Printf("stock search request failed: %s", err)
			return "", err
		}
		defer res.Body.Close()

		if res.StatusCode != 200 {
			return fmt.Sprintf("Stock error: API returned code %d\n", res.StatusCode), nil
		}

		var searchResponse FinnhubSearchResponse
		if err := json.NewDecoder(res.Body).Decode(&searchResponse); err != nil {
			app.errorLog.Printf("stock search parse failed: %s", err)
			return "", err
		}

		if searchResponse.Count == 0 || len(searchResponse.Result) == 0 {
			return fmt.Sprintf("Stock error: no results found for '%s'\n", query), nil
		}

		// Use the first result
		symbol = searchResponse.Result[0].Symbol
		companyName = searchResponse.Result[0].Description
	}

	// Get the quote
	quoteURL := fmt.Sprintf("https://finnhub.io/api/v1/quote?symbol=%s&token=%s",
		symbol, app.config.finnhubapikey)

	res, err := http.Get(quoteURL)
	if err != nil {
		app.errorLog.Printf("stock quote request failed: %s", err)
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return fmt.Sprintf("Stock error: API returned code %d\n", res.StatusCode), nil
	}

	var quoteResponse FinnhubQuoteResponse
	if err := json.NewDecoder(res.Body).Decode(&quoteResponse); err != nil {
		app.errorLog.Printf("stock quote parse failed: %s", err)
		return "", err
	}

	if quoteResponse.C == 0 {
		return fmt.Sprintf("Stock error: no quote found for '%s'\n", symbol), nil
	}

	// If we didn't get company name from search, fetch it via profile
	if companyName == "" {
		profileURL := fmt.Sprintf("https://finnhub.io/api/v1/stock/profile2?symbol=%s&token=%s",
			symbol, app.config.finnhubapikey)
		res, err := http.Get(profileURL)
		if err == nil {
			defer res.Body.Close()
			var profile struct {
				Name string `json:"name"`
			}
			if json.NewDecoder(res.Body).Decode(&profile) == nil && profile.Name != "" {
				companyName = profile.Name
			}
		}
	}

	if companyName == "" {
		companyName = symbol
	}

	// Format change with + or - sign
	changeSign := ""
	if quoteResponse.D >= 0 {
		changeSign = "+"
	}

	result := fmt.Sprintf("%s(%s): $%.2f %s%.2f (%s%.2f%%%%)\n",
		symbol, companyName, quoteResponse.C,
		changeSign, quoteResponse.D,
		changeSign, quoteResponse.Dp)
	return result, nil
}

func formatUSD(price float64) string {
	intPart := int(price)
	frac := fmt.Sprintf("%.2f", price-float64(intPart))[1:] // ".xx"
	return "$" + formatWithCommas(intPart) + frac
}

func (app *application) getCryptoQuote(query string) (string, error) {
	query = strings.TrimSpace(query)

	searchURL := app.config.coingeckoBaseURL + "/search?query=" + url.QueryEscape(query)
	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return "", err
	}
	if app.config.coingeckoapikey != "" {
		req.Header.Set("x-cg-demo-api-key", app.config.coingeckoapikey)
	}

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		app.errorLog.Printf("crypto search request failed: %s", err)
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return fmt.Sprintf("Crypto error: search API returned code %d\n", res.StatusCode), nil
	}

	var searchResp CoinGeckoSearchResponse
	if err := json.NewDecoder(res.Body).Decode(&searchResp); err != nil {
		app.errorLog.Printf("crypto search parse failed: %s", err)
		return "", err
	}

	if len(searchResp.Coins) == 0 {
		return fmt.Sprintf("Crypto error: no results found for '%s'\n", query), nil
	}

	// Start with CoinGecko's top-ranked result.
	// For short ticker-style queries (<=5 chars), also scan for an exact symbol match —
	// but skip this for longer name-style queries like "bitcoin" to avoid meme coins
	// that happen to use the name as their symbol (e.g. harrypotterobamasonic10in -> BITCOIN).
	coinID := searchResp.Coins[0].ID
	coinSymbol := strings.ToUpper(searchResp.Coins[0].Symbol)
	coinName := searchResp.Coins[0].Name
	if len(query) <= 5 {
		upperQuery := strings.ToUpper(query)
		for _, c := range searchResp.Coins {
			if strings.ToUpper(c.Symbol) == upperQuery {
				coinID = c.ID
				coinSymbol = strings.ToUpper(c.Symbol)
				coinName = c.Name
				break
			}
		}
	}

	priceURL := fmt.Sprintf(
		app.config.coingeckoBaseURL+"/simple/price?ids=%s&vs_currencies=usd&include_24hr_change=true",
		url.QueryEscape(coinID),
	)
	req, err = http.NewRequest("GET", priceURL, nil)
	if err != nil {
		return "", err
	}
	if app.config.coingeckoapikey != "" {
		req.Header.Set("x-cg-demo-api-key", app.config.coingeckoapikey)
	}

	res, err = client.Do(req)
	if err != nil {
		app.errorLog.Printf("crypto price request failed: %s", err)
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return fmt.Sprintf("Crypto error: price API returned code %d\n", res.StatusCode), nil
	}

	var priceResp map[string]map[string]float64
	if err := json.NewDecoder(res.Body).Decode(&priceResp); err != nil {
		app.errorLog.Printf("crypto price parse failed: %s", err)
		return "", err
	}

	coinData, ok := priceResp[coinID]
	if !ok {
		return fmt.Sprintf("Crypto error: no price data for '%s'\n", query), nil
	}

	price := coinData["usd"]
	change24h := coinData["usd_24h_change"]
	changeSign := ""
	if change24h >= 0 {
		changeSign = "+"
	}

	return fmt.Sprintf("%s(%s): %s (%s%.2f%%%% 24h)\n", coinSymbol, coinName, formatUSD(price), changeSign, change24h), nil
}

func (app *application) sendUrlToYirp(url string) (string, error) {
	app.errorLog.Printf("sendUrlToYirp url: %s\n", url)
	yirpRequest := YirpRequest{
		ApiKey:  app.config.yirpapikey,
		LongUrl: url,
	}
	marshalled, err := json.Marshal(yirpRequest)
	if err != nil {
		app.errorLog.Printf("impossible to marshall yirpRequest: %s", err)
		return "", err
	}

	req, err := http.NewRequest("POST", app.config.yirpAPIAddr, bytes.NewReader(marshalled))
	if err != nil {
		app.errorLog.Printf("impossible to build request: %s", err)
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	// create http client
	// do not forget to set timeout; otherwise, no timeout!
	client := http.Client{Timeout: 10 * time.Second}
	// send the request
	res, err := client.Do(req)
	if err != nil {
		app.errorLog.Printf("impossible to send request: %s", err)
		return "", err
	}
	app.infoLog.Printf("status Code: %d", res.StatusCode)

	// we do not forget to close the body to free resources
	// defer will execute that at the end of the current function
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		app.errorLog.Println(res.Status)

		result := "Yirp error: URL API returned code: " + strconv.Itoa(res.StatusCode) + "\n"
		return "", errors.New(result)
	}

	var yirpResponse YirpResponse
	err = json.NewDecoder(res.Body).Decode(&yirpResponse)
	if err != nil {
		fmt.Println(err)
		return "", err
	}
	fmt.Println(yirpResponse)

	return yirpResponse.ShortUrl, nil
}

var horoscopeOpeners = []string{
	// Core
	"The stars have aligned in a pattern that concerns them more than you.",
	"Cosmic forces are in motion, and several of them know your name.",
	"The celestial map shows interesting developments in your vicinity.",
	"The universe has been drafting a memo about you.",
	"Mercury is doing something unusual, but it remains unclear if that affects you personally.",
	"Planetary alignments suggest a day of unexpected texture.",
	"The stars have reviewed your recent choices and wish to offer guidance.",
	"Venus is in your corner, though she is distracted at the moment.",
	"The moon has been watching you with increasing interest.",
	"Astrological indicators point toward an inflection point.",
	"The cosmos has cleared its schedule for your particular situation.",
	"Saturn's return has been filing paperwork about your future.",
	"The celestial bodies have convened and reached a tentative consensus.",
	"Your star chart reveals a pattern your past self would not have predicted.",
	"The sky above is performing specifically for you today.",
	"Jupiter's position suggests that what you have been building is about to be tested.",
	"The stars have seen worse. You will be fine.",
	"Cosmic weather: partly cloudy with a high chance of revelation.",
	"The planets are arguing, but none of their arguments are about you.",
	"Mars is involved, which is either very good or very bad depending on your specific situation.",
	"The cosmos has been reviewing its notes on you and has some follow-up questions.",
	"Neptune's influence is strong today, meaning nothing is quite as solid as it appears.",
	"Uranus is making a face. This is either meaningless or extremely significant.",
	"The alignment of the outer planets has been noted and filed.",
	"Your ascending node is ascending, as it does.",
	"The zodiac wheel has turned, and your quadrant is now facing something interesting.",
	"Retrograde energies are clearing, but the cleanup is not yet complete.",
	"The celestial mechanics of today suggest a pivot.",
	"Something in the heavens is pointing at you. Try not to look up.",
	"The astral plane is buzzing with activity in your general direction.",
	"Your sun sign has sent a formal request to the universe regarding your situation.",
	"The stars are aligned in a way that astrologers describe as notable.",
	"Planetary conjunctions this morning produced an energy that is heading your way.",
	"The cosmos is watching, as it always is, but today with slightly more attention.",
	"A rare celestial configuration has been in effect since midnight.",
	"The moon's phase is significant today, though its effects are subtle.",
	"The universe has opened a new tab with your name on it.",
	"Cosmic timing is rarely this precise. Note the day.",
	"The galactic background radiation is doing something personal.",
	"Your chart has been highlighted in the celestial filing system.",
	"The stars have cleared a window for you. It does not stay open long.",
	"Heavenly bodies are in motion, and their momentum favors your cause.",
	"The universe has received your unspoken request and is processing it.",
	"A convergence of influences has singled out your circumstances for attention.",
	"The celestial committee has your case on today's agenda.",
	"Something in the upper atmosphere has been your advocate.",
	"The planets are in an arrangement that has not occurred in several years.",
	"Today's sky contains a message. The challenge is reading it correctly.",
	"The universe is rarely subtle. Today it is being unusually direct.",
	"All signs point in the same direction. That direction involves you.",
	"The space between stars carries a signal you are finally ready to receive.",
	"Your sign is at the center of today's cosmic geometry.",
	"Something has shifted in the larger pattern, and the shift landed near you.",
	"The celestial bodies are in conversation, and you are the subject.",
	"The arc of the cosmos is long, but today it bends toward your particular coordinates.",
	"The universe maintains records. Yours are currently under review.",
	"Cosmic pressure is building in a way that will eventually require your participation.",
	"The stars have been watching this develop for some time.",
	"Forces larger than your current concerns are about to intersect with them.",
	"The heavens have been rehearsing something on your behalf.",
	"Your birth chart has been cited in an ongoing cosmic negotiation.",
	"The universe is not indifferent to your situation. In fact, quite the opposite.",
	"Something above and beyond has been arranging things in the background.",
	"The cosmic ledger shows a balance shifting in your direction.",
	"Your relationship with the universe is entering a new phase.",
	"Today the cosmos is less of a backdrop and more of a participant.",
	"The planets are filing a motion that directly concerns your immediate future.",
	"The stars this morning suggest a day worth paying attention to.",
	"Celestial mechanics are operating in your favor, which is not always the case.",
	"The heavens have been preparing something for you. Today is the delivery.",
	"The universe rarely acts on anyone's behalf specifically, but today is an exception.",
	"Your portion of the sky is unusually active this morning.",
	"The cosmic weather system has produced something unusual in your forecast.",
	"The stars' opinions of your situation are warmer than you might expect.",
	"Your celestial address has been receiving a great deal of mail lately.",
	"The universe has been connecting dots that you have not yet noticed.",
	"A favorable wind has been building in the astrological sense.",
	"Today the sky is not neutral. It has chosen a side.",
	"The planets' current positions create a specific kind of opportunity.",
	"What the stars are arranging today has been in the works for some time.",
	"The universe is a patient planner. Your patience is about to be rewarded.",
	"Today your star chart reads like the opening chapter of something good.",
	"The cosmic register shows your account is due for a credit.",
	"The celestial alignment this morning is one that astrologers mark with asterisks.",
	"Astrological conditions favor your ambitions, at least until evening.",
	"The universe does not do things arbitrarily. Today's arrangement means something.",
	"Your place in the larger pattern has become suddenly prominent.",
	"The stars are offering you something. Whether you take it is your business.",
	"Cosmic energies have been pooling in your section of the chart.",
	"The sky this morning read your situation and made adjustments.",
	"Something celestial has been quietly advocating for you.",
	"The stars have weighed your situation and found it worthy of intervention.",
	"The universe is a large place, but today it is focused on a small one.",
	"Your astrological season is peaking in a way that matters.",
	"The planets have positioned themselves as if they expected something from you.",
	"Today the stars are less observational and more directional.",
	"What the cosmos has arranged for you is not coincidence. It is orchestration.",
	"The celestial backdrop is unusually vivid today.",
	"The stars have assigned you a co-pilot for this portion of the journey.",
	"Astrological forces have been doing your groundwork while you slept.",
	"The universe has placed something in your path. Do not step over it.",
	"Today the sky is not performing for everyone. It is performing for you.",
	"The cosmic calendar has today circled in a color you have not seen before.",
	"Heavenly arrangements of this specificity do not occur by accident.",
	"The universe has been building toward today in ways you could not have noticed.",
	"The stars are in a mood. Fortunately, that mood is generous.",
	"Celestial forces have been warming up for this particular moment.",
	"Today's cosmic configuration is unusual enough to warrant your full attention.",
	"The universe's plans for you have entered a new and more active phase.",
	"Your presence in the larger pattern has been requested.",
	"Something vast and largely indifferent has made an exception for you today.",
	"The stars have revised their position on your situation.",
	"Astrological forces are converging at a point that happens to be your life.",
	"The planets have arranged themselves into a shape that rhymes with your situation.",
	"The cosmos has flagged today as significant. Trust that judgment.",
	"The sky has been reviewing your file and wishes to make a recommendation.",
	"Your celestial circumstances have improved without your having to do anything.",
	"The stars have been conferring. They have reached an encouraging conclusion.",
	"The universe is sending something. The delivery window is today.",
	"Cosmic forces that have been dormant are beginning to move again.",
	"Your portion of the zodiac is currently in the sun, figuratively and perhaps literally.",
	"The celestial machinery has been tuned specifically for today's requirements.",
	"The stars are not neutral about your situation. They have opinions.",
	"Something vast has been moving in your direction for longer than you know.",
	"The universe has issued a favorable rating on your current trajectory.",
	"The cosmic winds have shifted in a way that favors your course.",
	"Celestial developments of this morning affect a small number of people. You are among them.",
	"The stars are in agreement, which is rare and worth noting.",
	"Today the universe is less of a bystander and more of a collaborator.",
	"The planetary positions of this morning create an opening that did not exist yesterday.",
	"Your celestial case has been moved to the top of the queue.",
	"The cosmos has been patient. It is now less patient.",
	"The universe has been calibrating something specifically for your circumstances.",
	"Today's alignment was not an accident. The universe does not do accidents.",
	"The stars' position on you has softened considerably in the past few weeks.",
	"Something in the larger order has been rearranging itself on your behalf.",
	"The cosmic pressure that has been building is about to find a release valve.",
	"Your situation has been elevated to priority status in the celestial schedule.",
	"The universe is rarely this organized. Enjoy it while it lasts.",
	"Today the stars are speaking clearly. Listen.",
	"The cosmos has placed you at an interesting intersection.",
	"Your celestial chart shows a door that is open today and may not be tomorrow.",
	"The stars have noted your recent efforts and are prepared to respond.",
	"Cosmic developments that began some time ago are arriving at your doorstep.",
	"The universe is in the business of balance. Your balance sheet looks good today.",
	"Planetary movements this week have been building toward today specifically.",
	"The stars have assigned you a front row seat to something significant.",
	"Something celestial has been making arrangements you were not aware of.",
	"The cosmos has a long memory and a reliable sense of fairness.",
	"Today the universe is not keeping its distance.",
	"The stars are indicating that what you believe is possible may actually be possible.",
	"Cosmic forces are in position. The rest is up to you.",
	"Today the sky is less a ceiling and more a door.",
	"The universe has been in your corner longer than you realize.",
	"The celestial situation this morning is one of quiet readiness.",
	"The planets are done deliberating. Today they act.",
	"Your celestial conditions are among the better ones available right now.",
	"The stars have tipped their hand. The hand contains something useful.",
	"Something the universe has been working on is nearly ready.",
	"The cosmic alignment of this particular morning is one that happens rarely.",
	"Today the stars are operating with unusual precision.",
	"The universe has adjusted its focus in your direction.",
	"Your celestial portfolio is performing well this week.",
	"The cosmos has left a message. It is short and actionable.",
	"The stars are sending a signal that is clearer today than it has been in months.",
	"Planetary forces are converging in a way that astrologers find genuinely interesting.",
	"The universe has been paying attention to you. Today it makes a move.",
	"Celestial conditions today are unusually favorable for the kind of thing you have in mind.",
	"Your star chart this morning looks like something a professional would describe as promising.",
	"The cosmos has been preparing the ground. Today you plant.",
	"The universe has assigned favorable conditions to your quadrant of the sky.",
	"The stars have reviewed the situation and are cautiously optimistic.",
	"Something large and patient in the cosmos has been waiting for this moment.",
	"The celestial arrangement today is one that occurs roughly once a year and usually means something.",
	"Your cosmic standing has improved significantly in recent weeks.",
	"The universe is setting a table. You will want to be seated when it is ready.",
	"Astrological circumstances have been building toward a favorable configuration.",
	"The stars have been gathering their arguments and are now prepared to make their case.",
	"Something the cosmos arranged a long time ago is finally bearing fruit.",
	"The universe is not in the habit of wasting good cosmic weather on the undeserving.",
	"Today your celestial conditions are among the most dynamic of the year.",
	"The planets have been slowly rotating into alignment. They are nearly there.",
	"Your star chart today looks like good news translated into astrology.",
	"The cosmos has granted you an unusual degree of favorable positioning.",
	"The celestial machinery has been running quietly. Today it outputs something.",
	"The universe has decided to participate in your day rather than observe it.",
	"The stars have been watching the situation develop with growing approval.",
	"Something celestial has been building toward a delivery. Today is the day.",
	"The cosmic tides have shifted and the current now runs in your direction.",
	"The universe has looked at your circumstances and decided to weigh in.",
	"Your celestial coordinates are passing through an unusually promising sector.",
	"The stars have a plan. You are in it.",
	"Cosmic forces that have been circling your situation are now ready to land.",
	"The universe is not obligated to help, but today it appears willing.",
	"The astrological weather today is clear and dynamic with a chance of revelation.",
	"Something in the arrangement of the heavens today speaks specifically to your situation.",
	"The stars are in the middle of saying something. Today you hear the important part.",
	"Your situation in the cosmic record has been upgraded without notice.",
	"The universe has been building a case. Today it presents it.",
	"The celestial bodies have agreed on something. The subject is your near future.",
	"The stars are not in a holding pattern today. They are moving.",
	"The universe has reviewed its inventory and found something with your name on it.",
	"Something has shifted at the planetary level that has a direct line to your circumstances.",
	"The cosmos has logged today as a threshold. Cross it.",
	"The stars are ready. The only question is whether you are.",
	"The universe does not always show its hand. Today is a notable exception.",
	"Celestial forces have converged into a configuration that is difficult to ignore.",
	"The stars have been consulting among themselves. The conclusion involves you.",
	"A cosmic wave that has been building is about to arrive on your shore.",
	"The universe has filed a favorable report about your current trajectory.",
	"The gravy of circumstance is thick today. Do not let it congeal.",
	"Everything is gravy - and today, gravy is everything.",
	// Dinosaur
	"Like the Velociraptor approaching an unsuspecting herbivore, opportunity moves faster than you think.",
	"The great extinction that cleared your path happened longer ago than you realize.",
	"Prehistoric forces - older than memory, older than bone - are stirring in your favor.",
	"The fossil record of your ambitions has finally reached a readable stratum.",
	"The Jurassic period of your recent struggles is drawing to a close.",
	"Like a Triceratops standing its ground, your position is stronger than it looks.",
	"The Cretaceous-level disruption you survived has made you formidable.",
	"The age of giants is not over. It has simply relocated to your vicinity.",
	"The geologic clock, which measures in millions of years, ticks in your favor today.",
	"Like the Brachiosaurus reaching for the highest leaves, your reach exceeds what those below can see.",
	"The sedimentary layers of your experience are compressed into something valuable.",
	"The Mesozoic era of your career is giving way to something warmer-blooded.",
	"Ancient and reliable instincts are more trustworthy today than modern ones.",
	"The tar pit is behind you. The open plain is ahead.",
	"Your species has survived worse. Context is everything.",
	"The Ankylosaur in you - armored, patient, slow to move - is correct today.",
	"A Pterodactyl-level view of your situation reveals things the ground perspective misses.",
	"Like the mosasaur that ruled the ancient seas, your domain is larger than you have claimed.",
	"The mass extinction event in your professional life has ended. Adapt accordingly.",
	"Carboniferous-era patience is what today requires. You have it.",
	"The iridium layer in your life story marks a before and after. You are now after.",
	"Like the Spinosaurus near water, you are strongest in your natural environment.",
	"The Paleocene dawn - that first morning after everything changed - is exactly where you are.",
	"Your evolutionary line has been quietly building toward this moment.",
	"The meteorite has already landed. What remains is how you respond.",
	"The Permian extinction was worse. You have already survived more than you think.",
	"Like the theropod that learned to use its forelimbs cleverly, your constraints are actually assets.",
	"The Eocene period of gradual warming that follows catastrophe - that is where you are.",
	"Your Triassic origin story is more resilient than the Jurassic ones everyone prefers.",
	"The feathered descendants of the great dinosaurs built nests that outlasted them all.",
	// Dolphin
	"Your echolocation is unusually accurate today. Trust the signal.",
	"The pod has spoken. The currents agree.",
	"Something large and benevolent moves through the deep water of your circumstances.",
	"The ocean of possibility has swells today - ride them rather than fight them.",
	"Your sonar is pinging something real. Do not dismiss the echo.",
	"The deep current that moves beneath visible events is running in your direction.",
	"The pod has been watching from below. What it has seen encourages it.",
	"Something in the cold deep has shifted, and the shift is surfacing near you.",
	"The sea floor is stable beneath the turbulence. Your situation is similar.",
	"Your clicks and whistles are finally being received by the right audience.",
	"The ocean is not indifferent to your situation. Today it is actively helpful.",
	"The tide in your circumstances has been changing for some time. Today you notice it.",
	"The deep cetacean intelligence in you knows something your surface mind has been ignoring.",
	"Something that has been swimming parallel to your path is about to join it.",
	"The pod moves together. You have been alone too long.",
	"Beneath the chop of your immediate concerns, a strong favorable current runs.",
	"The bioluminescent trail you are leaving is more visible to others than to you.",
	"Your frequency is unusually clear today. Signal broadly.",
	"The sea has given you good news in a language you are capable of hearing.",
	"The depths that seemed threatening are, on reflection, navigable.",
	"The ocean of your circumstances is colder than you would like but richer than it looks.",
	// Thorazine
	"The flatness you have been feeling is not emptiness - it is the stillness before something important.",
	"Clarity arrives today, as it sometimes does, from an unexpected pharmacological angle.",
	"The haze has thinned enough to see through, if you look carefully.",
	"Your particular chemical equilibrium is, today, exactly what the situation requires.",
	"The fog that has defined your recent weeks is lifting. The landscape beneath is not what you expected.",
	"Whatever has been keeping you level has been doing good work. Trust it.",
	"The dosage of reality you have been receiving has finally reached therapeutic levels.",
	"Something that was blurred at the edges is beginning to sharpen. Allow it.",
	"The medication of circumstance has brought you to a useful plateau.",
	"Your emotional hemodynamics are stable today. Build on that.",
	"The sedative effect of recent setbacks has worn off at exactly the right moment.",
	"What felt like numbness was actually a very considered form of rest.",
	"The prescription for your situation has finally been properly filled.",
	"Your neurochemical forecast is overcast with a high chance of sudden lucidity.",
	"The pharmacological metaphor of your life suggests today is a good day for a dosage review.",
	// EDM
	"Get ready for this. The drop is coming and the cosmos helped produce it.",
	"There is no limit - no limit - to the possibilities your day contains.",
	"The universe is playing something with a hard four-on-the-floor beat, and it is aimed at you.",
	"The rave of your circumstances is just getting started. The real tracks have not played yet.",
	"The BPM of cosmic events is rising. Your body already knows this.",
	"The lasers are cutting through the smoke of your uncertainty.",
	"The intro has been very long. The breakdown is finally here.",
	"You have got the power. It has been yours since the beginning of the set.",
	"The rhythm is gonna get you where you need to be. Let it.",
	"The warmup DJs are packing up. Something better is about to take the stage.",
	"This is a tribal dance kind of day. Move accordingly.",
	"The warehouse is packed with opportunity. Navigate it.",
	"Your particular frequency is finally matching the room's frequency.",
	"The build has been extended past the point of comfort. The release is worth it.",
	"Show me love - this is the message the universe is sending today.",
	"The twilight zone of your uncertainty is passing. Dawn is a 140-BPM thing.",
	"The four AM of your situation is the hour when everything becomes clear.",
	"The strobes of insight are firing in sequence today.",
	"What is love? The universe has an answer for you today, and it arrives at full volume.",
	"The synth line that has been building in the background of your life is about to drop.",
	"Your personal rave has reached the peak hour. This is the moment you dressed for.",
	"I sit on acid and what I see is your future arriving at high speed and high resolution.",
	"The rough bass of circumstance has been mixed with something sweeter. The blend is today.",
	"Lords of the situation you are in - that is what you are becoming.",
}

var horoscopePredictions = []string{
	// Core
	"An unexpected conversation will clarify something you have been misreading for months.",
	"A financial opportunity arrives in a form you will not immediately recognize.",
	"Someone from your past is thinking about you with more complexity than you would expect.",
	"A creative endeavor gains momentum if you simply refuse to abandon it today.",
	"The decision you have been postponing has made its own choice. Review it.",
	"You will be offered something. Accept it.",
	"Your instincts about a specific person are correct. Act accordingly.",
	"Something you built quietly is about to be noticed loudly.",
	"The meeting you dread will end earlier than expected and better than feared.",
	"A small, overlooked detail turns out to be the entire point.",
	"Someone will ask your opinion. Give it honestly.",
	"The obstacle is not in front of you - you have already passed it.",
	"Today's frustration is tomorrow's amusing anecdote.",
	"What you call bad timing is actually very precise timing.",
	"A stranger holds a piece of your puzzle. Engage with strangers.",
	"What you have been calling a flaw is actually a distinguishing feature.",
	"The answer is closer than the question makes it seem.",
	"Two things you thought were unrelated are about to reveal their relationship.",
	"A long-delayed acknowledgment is on its way to you.",
	"The project you abandoned is asking to be revisited.",
	"Your most recent mistake has been silently correcting itself.",
	"A relationship that seemed dormant is about to wake up.",
	"The idea you mentioned once and never mentioned again is the right one.",
	"Something you did weeks ago is paying dividends today.",
	"An old door is not locked. It is merely stuck. Push harder.",
	"The people who have been watching you work are about to say something.",
	"A connection you did not know you had will surface before the day ends.",
	"The thing you need is in a room you have not entered yet.",
	"Your most underrated quality is about to become your most useful one.",
	"An apology is coming. Accept it without adding to it.",
	"A plan you considered foolish is about to prove itself.",
	"The conversation you have been avoiding is the one you need most.",
	"Someone in your circle is about to do something that surprises you favorably.",
	"The work you have been doing in private is about to require a public stage.",
	"An obstacle that seemed permanent is about to be revealed as temporary.",
	"Today you will understand something you have been pretending to understand.",
	"The opportunity is not new. You have walked past it before. This time, stop.",
	"A long-running uncertainty reaches a conclusion today.",
	"What you have been rehearsing, privately and mentally, is about to get a real audition.",
	"The recognition you have been waiting for is arriving from an unexpected direction.",
	"A collaboration is forming around you without your awareness.",
	"Something you tried once that failed is ready to succeed under different circumstances.",
	"A lesson you learned the hard way is about to be extremely useful.",
	"The waiting is nearly over. Something that has taken a long time is almost ready.",
	"A resource you overlooked is more valuable than you priced it.",
	"Today your patience - which has been tested significantly - begins to yield returns.",
	"The quiet progress you have been making is larger than it looks from inside it.",
	"An unexpected advocate has been speaking well of you in rooms you have not been in.",
	"A detour you resented will be revealed as a shortcut in retrospect.",
	"The skill you dismissed as minor is about to be the one that matters.",
	"Something complicated is about to simplify itself.",
	"The door you knocked on without answer is being opened from the inside.",
	"A conflict that has been draining your energy is moving toward resolution.",
	"What you thought was the end of a chapter is actually a transition to a better one.",
	"The silence from a person who matters has been thoughtful, not indifferent.",
	"An opportunity is ripening. Do not pick it too early.",
	"The version of yourself you have been working toward is arriving ahead of schedule.",
	"A number - literal or metaphorical - is about to change in your favor.",
	"Today the variables in a persistent problem align for a solution.",
	"Someone is about to give you exactly what you asked for. Check that you still want it.",
	"A talent you have kept private deserves a slightly larger audience.",
	"The system you set up and forgot about is quietly producing results.",
	"What you have been comparing yourself to unfavorably is not the right comparison.",
	"An intervention of the friendly kind is available if you ask for it.",
	"The thing you keep saying you will do tomorrow - today is the day.",
	"A piece of news you have been dreading will land softer than you expected.",
	"The groundwork you laid when things were quiet is now supporting something real.",
	"An instinct you suppressed out of politeness was right. You will find this out today.",
	"Something is being repaired in the background of your life without your having to manage it.",
	"The person you need to talk to is also thinking about calling you.",
	"A creative block you have been suffering will clear today under unexpected circumstances.",
	"The compromise you agreed to reluctantly is going to work better than the original plan.",
	"Something that cost you greatly will be repaid, though perhaps not in the currency you expected.",
	"The question you have been asking has an answer. You have already heard it - go back and listen.",
	"A relationship you wrote off as finished has more chapters remaining.",
	"The hard thing you did recently has changed more than you know.",
	"Something you thought required a large effort can be solved with a small and specific one.",
	"The right person to help you already knows you. Reach out.",
	"A goal that seemed abstract is becoming tangible.",
	"What someone said to you that hurt was, unfortunately, useful. Use it.",
	"The boundary you are considering setting is the correct one.",
	"An offer is about to be made. Do not answer immediately.",
	"Something you assumed was finished is actually in a third act.",
	"The work you did on yourself last year is paying dividends in your circumstances today.",
	"A lucky encounter is available if you leave the house - or the familiar.",
	"The structure you thought was collapsing was actually reorganizing.",
	"Someone has already forgiven you. You do not need to carry that any longer.",
	"A door that seemed welded shut has developed a hinge.",
	"The long game you have been playing is about to reveal why you played it.",
	"What looks like a setback from inside it is a course correction from outside.",
	"A yes that was long overdue is on its way to you.",
	"The thing that has been just out of reach has moved closer while you were looking elsewhere.",
	"Today's unexpected event is a gift even if it does not feel like one immediately.",
	"The version of this situation you have been dreading is not the version that will occur.",
	"A conversation you had that you thought went poorly had a different effect than you think.",
	"The idea that seems too simple is probably the correct one.",
	"Something has changed in the background of a situation you thought was static.",
	"An old talent is ready to be applied to a new problem.",
	"The person you have been trying to reach has been trying to reach you.",
	"A fear you have been feeding will starve today if you stop feeding it.",
	"Something you own is more valuable than you have assigned it.",
	"The favor you did a long time ago is about to be returned.",
	"A situation that required patience will today reward it.",
	"The problem that seemed to require everyone's input requires only one person's decision.",
	"A plan that seemed impossible last month has become merely difficult. Keep going.",
	"Something is ending, and the ending is better than the continuation would have been.",
	"The situation you are in has been worse than it is now. Acknowledge the progress.",
	"A collaboration that seems unlikely is actually the right configuration.",
	"What you are about to do is braver than it seems from inside the decision.",
	"The timing you have been waiting for has arrived. You may not feel ready, but you are.",
	"A burden you have been carrying alone is ready to be shared.",
	"Something you dismissed as luck was actually skill. Claim it.",
	"The path that seemed like a detour is the path.",
	"Today you receive clarity on something that has been intentionally obscured.",
	"The person you are becoming is being noticed by people worth impressing.",
	"A specific action you have been postponing will take less time than you think.",
	"The resource you need has been available the whole time. You have just been looking in the wrong direction.",
	"A gamble you made in earnest is about to pay.",
	"Something you have been wrong about will be corrected today, and you will be glad.",
	"The situation looks different from the angle you have not tried yet.",
	"An apology you owe is, if given today, likely to land well.",
	"The help you need is available from someone who is waiting to be asked.",
	"A deadline you have been dreading is actually a starting gun.",
	"Something you said once in passing planted a seed. Today it grows visible.",
	"The risk you have been calculating is smaller than your calculations suggest.",
	"What has been slow is about to move faster.",
	"The obstacle you have been navigating around can now be removed entirely.",
	"A realization that arrives today will reframe several preceding weeks.",
	"The effort you put in when no one was watching is about to be seen.",
	"Today a choice presents itself that, once made, simplifies everything downstream.",
	"Something you have been bracing for will either not arrive or arrive gently.",
	"A conversation you have been putting off is, today, suddenly easy.",
	"The version of this situation you are imagining is worse than the actual one.",
	"What has been resisting is about to yield.",
	"Today's events will seem unremarkable as they happen and significant in retrospect.",
	"The slow-moving thing in your life is about to accelerate.",
	"An alignment you did not arrange for yourself has been arranged for you.",
	"Something you need to hear will arrive today from an unlikely source.",
	"A boundary you softened recently will prove to have been the right call.",
	"The pattern you have been trying to break is weaker than it was last month.",
	"What you have been half-doing deserves to be fully done.",
	"A conversation is being had about you in your absence. The tone is warmer than you would assume.",
	"The situation is not stuck. It is compressing. There is a difference.",
	"An encounter that seems minor will have lasting relevance.",
	"The negotiation you are in has more room than the other party is showing.",
	"What you built is sturdier than the way you have been describing it to yourself.",
	"Today the correct answer to a question you have been avoiding is obvious.",
	"A creative spark has been waiting for a specific kind of boredom to ignite it. Today it has it.",
	"The next step, which you have been unable to see, becomes visible today.",
	"Someone is about to offer you more than you asked for.",
	"The thing that has been missing from a plan reveals itself today.",
	"A situation that required you to be patient is now asking you to move.",
	"What seemed like the wrong direction has led somewhere right.",
	"The relationship you have been maintaining at low effort deserves more. Give it today.",
	"A professional connection you have been neglecting is still warm enough to revive.",
	"The idea is ready. The only thing it is waiting for is you.",
	"Something you have been told is impossible is merely difficult. There is a difference.",
	"An argument you abandoned midway is worth finishing.",
	"The version of yourself you present to the world is underselling the actual you.",
	"What you have been circling without approaching is, today, approachable.",
	"A task that has been weighing on you will dissolve faster than you think once begun.",
	"The feedback you received was hard to hear. It was also correct.",
	"A solution exists that you have not yet considered because it requires asking someone.",
	"What has seemed like a closed door has actually been open, just set to push instead of pull.",
	"Today an unlikely coincidence will, on reflection, seem inevitable.",
	"The effort feels lopsided because you are doing most of it. That changes today.",
	"A minor investment of attention pays an outsized return before the week ends.",
	"The person you have been underestimating will impress you before sundown.",
	"Something you attempted too early and failed at is now exactly within your reach.",
	"The strategy you have been defending may benefit from a gentle revision.",
	"A new piece of information shifts the entire picture into a better frame.",
	"The energy you have been putting into resistance would be well redirected into motion.",
	"A longstanding misunderstanding corrects itself today without your having to intervene.",
	"The goal that seemed to require massive action can be advanced significantly with a small one.",
	"Today you are more persuasive than usual. Use it on something that matters.",
	"A yes that has been sitting on someone's desk is about to be sent.",
	"Something you lost track of resurfaces in a useful form.",
	"The part of the plan you skipped is now the part that matters most.",
	"A shortcut you dismissed as too good to be true turns out to be both good and true.",
	"The thing you have been overthinking is simpler than the thoughts you have been having about it.",
	"An offer arrives today. Do not overthink whether you deserve it.",
	"The person you trusted your instincts about is proving those instincts correct.",
	"Today the work you have been treating as a duty becomes something you want to do.",
	"A problem that has occupied your thinking for weeks resolves itself while you are thinking about something else.",
	"What has been holding you back is less structural than it appears.",
	"The collaboration you dismissed as unlikely is, on reflection, the right move.",
	"A skill you learned in a previous chapter is needed in this one.",
	"The resistance you are encountering is not opposition - it is information.",
	"Something you did entirely for others is about to benefit you as well.",
	"The plan needs one more element. You already have it. Look again.",
	"Today's challenge contains its own solution if you read the whole thing.",
	"A favorable introduction is about to be made on your behalf.",
	"What the situation requires of you today is the thing you are best at.",
	"The finish line is closer than your current pace suggests.",
	"An unexpected endorsement arrives from someone whose opinion carries weight.",
	"The thing you almost gave up on yesterday is worth continuing today.",
	"A long-postponed conversation turns out to be far easier than its postponement suggested.",
	"Something is returning to you that you had written off as gone.",
	"The universe is not asking you to be ready. It is asking you to begin.",
	"Today the stars conspire toward a long-delayed reunion of ideas, people, or intentions.",
	"What you have been treating as a hobby is ready to be taken more seriously.",
	"The project that stalled over a small technical issue is finally unblocked.",
	"An alliance you have been resisting out of pride is worth reconsidering.",
	"The next move in your situation is not a large one. It is a precise one.",
	"A risk you thought was irresponsible turns out to have been appropriately calibrated.",
	"Today you can do in one hour what has been taking you a week.",
	"The help that arrives today will not look like what you asked for. Accept it anyway.",
	"A plan b that you prepared reluctantly turns out to be the plan.",
	"The answer you have been waiting for from someone else was available from yourself the whole time.",
	"The gravy train has not left the station. In fact, it is adding a car.",
	"What you have been cooking has finally thickened into something worth serving.",
	// Dinosaur
	"Your territorial instincts are correct today - defend your creative space.",
	"Like the small feathered dinosaurs that became birds, your adaptations are working.",
	"The herbivores around you mean no harm. The predators are currently sleeping. Proceed.",
	"You are not going extinct. You are evolving. The difference is everything.",
	"Like the Ankylosaur in a conflict, your defense is your best offense today.",
	"The nesting instinct is strong and correct. Secure what you have built.",
	"Your scales have been earned. Wear them accordingly.",
	"The pack behavior you have been resisting is actually what the situation calls for.",
	"Like the sauropod that outlasted predators through sheer size, your persistence is the strategy.",
	"Your instinct to migrate - professionally or personally - is a sound one.",
	"The environment is changing. Adapt faster than you think you need to.",
	"Like a Triceratops standing its ground, your refusal to yield today is correct.",
	"The small, fast creature in your ecosystem is not a threat. It is a messenger.",
	"Your foraging instincts are well calibrated today. Follow them.",
	"The swamp of your current situation has solid ground underneath it. Keep moving.",
	"The predator-prey dynamic in your situation is inverting. Note the shift.",
	"What looks like competition is actually co-evolution. Engage rather than flee.",
	"Your nest - literal or metaphorical - is more valuable than you have priced it.",
	"The long-necked view is available to you today. Rise above the immediate vegetation.",
	"Like the armored dinosaurs that outlasted the swift ones, your durability is your advantage.",
	"The stampede is in a different direction. Step aside and let it pass.",
	"Your cold-blooded assessment of the situation is exactly what it needs.",
	"The volcanic activity in your professional landscape will subside. Build on high ground for now.",
	"Like the dinosaurs that survived by moving into new ecological niches, a pivot is available.",
	"The dominant species in your environment is not as dominant as it presents itself.",
	"Your tracks - professional and personal - are more visible to others than you realize.",
	"The watering hole situation requires patience and good timing. Wait.",
	"The egg you have been incubating is close to hatching. Do not abandon it now.",
	"Something large and territorial is vacating your space. Move in behind it.",
	"The fossils of your old strategies are interesting but not load-bearing. Build fresh.",
	// Dolphin
	"Communication is your primary advantage today. Use it like sonar - send clearly, listen harder.",
	"Play is not a distraction from your goals; it is how you locate them.",
	"Your pod is closer than you think. Surface and signal.",
	"Leap when it feels right. The water will receive you.",
	"The joy you have been deferring is the thing that makes everything else possible.",
	"Navigate by feeling as much as by sight. The two are equally reliable today.",
	"A current you have been fighting runs in a useful direction. Swim with it.",
	"The social intelligence you have been undervaluing is your greatest asset today.",
	"Your ability to read the room is sharper than usual. Trust the reading.",
	"Signal broadly and see what signals back. You will be surprised.",
	"Play the long game the way dolphins play: joyfully and at speed.",
	"The surface is where the action is today. Come up for air and for visibility.",
	"Something needs your full sensory attention - not just your analytical mind.",
	"The school of fish you are pursuing is larger than the one visible from the surface.",
	"Your instinct to circle back and investigate is correct today.",
	"What the pod knows collectively is more than what you know individually. Ask them.",
	"The buoyancy you need is available if you stop fighting the water.",
	"Someone in your circle is sending a distress call. Listen for it.",
	"The clear water of a simple solution is right below the murky layer. Dive through it.",
	"Your curiosity today is not a distraction. It is the whole point.",
	"The signature whistle of someone important to you is in the water. Listen.",
	// Thorazine
	"The urgency you feel is, in clinical terms, a construct. Proceed at your own pace.",
	"What appears to be inertia is actually a very sophisticated holding pattern.",
	"The blurred edges of today will sharpen by evening without any action on your part.",
	"The side effects of your current situation include an unusual capacity for patience.",
	"Your emotional flattening has not removed your instincts. They are still operational.",
	"The prescribed course of action is the same as it has been: stay the course.",
	"What looks like a reduced affect is actually very careful attention.",
	"The stabilization you have achieved is not stagnation. Build from this baseline.",
	"Your tolerance for the situation has increased because the situation has slightly improved.",
	"The contraindication in your current plan is not what you think it is.",
	"The therapeutic window for today's opportunity is narrow. Do not miss it.",
	"What requires adjustment is the dosage, not the direction.",
	"Your current symptom - a vague sense that something is about to change - is diagnostic.",
	"The breakthrough is not a dramatic one. It is a quiet titration toward clarity.",
	"You have been at a therapeutic dose of difficulty. The prescription is about to change.",
	// EDM
	"Pump up the volume on a project you have been keeping too quiet.",
	"Move your body toward what matters. The rhythm will follow.",
	"You have got the power. The question is whether you intend to use it.",
	"Everybody dance now - including in the areas of your life where dancing seems unlikely.",
	"The dancefloor of opportunity is open. It does not stay open past 4 AM.",
	"The remix of your current situation is better than the original.",
	"The rave that is your life requires your full attendance. Show up.",
	"Follow the bassline. It knows where it is going even when the melody does not.",
	"The DJ has seen your request and is building toward it.",
	"Your energy today matches the room's energy. Use that alignment.",
	"The silence before the drop is not emptiness. It is anticipation.",
	"The crowd you have been waiting to find is on a different floor. Go find it.",
	"What has been underground is ready to surface. The mainstream is about to catch up to you.",
	"The track has been building since the beginning of this week. The payoff is today.",
	"Release the old set. The new one is built and ready.",
	"The acid house of your intuition has been running all night. Trust what it has produced.",
	"Step into the light - literal and figurative - and be seen doing what you do.",
	"Your frequency has been niche. That is about to change.",
	"The floor is yours. You have been waiting for permission that was never required.",
	"Pump up the jam on the thing that has been low-energy for too long.",
	"Snap: you have got the power today. Activate it.",
	"No limit. Use this directive liberally in all applicable areas of your life.",
	"The rougher elements of your recent experience have been mixed into something interesting.",
	"Tonight you will go beyond your limit. Today, prepare.",
}

var horoscopeClosers = []string{
	// Core
	"Lucky number: higher than you would guess, lower than you would hope.",
	"Avoid making irreversible decisions before you have eaten something.",
	"Your lucky color is the one you see when you close your eyes.",
	"Beware of those who agree with everything you say.",
	"Today's energy peaks at an inconvenient moment - plan around it.",
	"Lucky object: something broken that still mostly works.",
	"Mercury is in retrograde in your checking account.",
	"Do not, under any circumstances, look directly at Tuesday.",
	"Someone is thinking about you right now. They are confused but not displeased.",
	"Lucky hour: the one you were not planning to use.",
	"The fine print of today's events is worth reading.",
	"A number you have not thought about in years will become relevant before midnight.",
	"Whatever you have been carrying, today is an acceptable day to set it down briefly.",
	"Watch for a sign that resembles something you have dismissed before.",
	"Your lucky element is whichever one you remember from chemistry class.",
	"Something small will save you. Keep your eyes open for it.",
	"The person who frustrates you most today is trying, in their own limited way.",
	"Caution is warranted, but not the amount you are currently applying.",
	"Lucky meal: whatever you almost did not order.",
	"The word you are looking for rhymes with something you already know.",
	"Someone will contradict you today. They will be half right.",
	"Your lucky direction is the one you have been avoiding.",
	"The stranger who catches your eye knows something useful. Do not overthink how to find out.",
	"Lucky stone: something you picked up somewhere and never threw away.",
	"Avoid expressing your most unfiltered opinion before 10 AM.",
	"The email you have been drafting should be sent.",
	"Lucky time: three hours from now.",
	"Something you said in passing this week was remembered by someone important.",
	"Your lucky instrument is the one you played badly once and gave up on.",
	"The hinge is on the left. Remember that.",
	"Beware of people who describe everything as amazing.",
	"Your lucky season is the one you are currently in.",
	"The first offer is not the final offer.",
	"Someone nearby needs exactly what you have been meaning to give away.",
	"Your lucky animal is the one you secretly identify with but never mention.",
	"The middle option is correct.",
	"A text message you have not yet sent will matter more than the one you have been obsessing over.",
	"Lucky footwear: comfortable.",
	"The second-to-last item on your list is the one worth doing today.",
	"Avoid volunteering for things that have not been asked of you yet.",
	"Lucky plant: something that requires almost no water.",
	"The shortcut you are considering has a hidden toll. Factor it in.",
	"Your lucky shape is the one you draw when you are thinking about something else.",
	"The meeting could have been an email. Send the email.",
	"Something in your home is in the wrong place. Moving it will help.",
	"Lucky material: whatever does not require ironing.",
	"Beware of certainty today, especially your own.",
	"Your lucky number is the one you just thought of before reading this.",
	"The thing you are about to apologize for is not worth apologizing for.",
	"Lucky transport: the slow one.",
	"Something you thought was over is not over.",
	"Your lucky phrase for today is: I will think about it.",
	"The last one in the box is usually the best one.",
	"Beware of anyone who has never been wrong in their telling of a story.",
	"Lucky time of day: later than you planned.",
	"The question is not whether you can. It is whether you should today.",
	"Your lucky condiment is whatever you reach for without thinking.",
	"The thing you are saving for a special occasion should be used now.",
	"Lucky position: closer to the exit than usual.",
	"The sentence that begins I should probably just is usually correct.",
	"Beware of deals that seem to require a decision before tomorrow.",
	"Your lucky medium is the one you abandoned.",
	"The second impression is more accurate than the first.",
	"Lucky metal: whatever your keys are made of.",
	"The appointment you almost cancelled is the one worth keeping.",
	"Something you have been carrying for exactly one year is finally ready to be put down.",
	"Lucky temperature: just below what you would have expected.",
	"The note you made to yourself and forgot about is the relevant one.",
	"Beware of those who are always right in retrospect.",
	"Your lucky weather is whatever is happening outside right now.",
	"The rule you have been bending is not as flexible as you have been treating it.",
	"Lucky door: the one that requires you to pull rather than push.",
	"The long version of the story contains something the short version does not.",
	"Your lucky sound is one you heard this morning and did not notice.",
	"The price is negotiable. Everything is negotiable.",
	"Lucky size: slightly larger than you initially thought necessary.",
	"Beware of advice that arrives in the form of a story about someone else.",
	"Your lucky intersection: the second one past the obvious choice.",
	"The habit you are trying to break has a structural cause you have not addressed.",
	"Lucky weight: lighter than it looks.",
	"The second page of the contract is where the interesting things are.",
	"Your lucky chair is the one you never sit in.",
	"The thing you are waiting to have before you start is not a prerequisite.",
	"Lucky book: the one you did not finish.",
	"The exit you almost took last week was, in retrospect, correct.",
	"Your lucky mistake is the small one. The large ones are not lucky.",
	"The first question was the right question. Return to it.",
	"Lucky distance: just past where you usually stop.",
	"The version of events that everyone agrees on is missing something.",
	"Your lucky side: the opposite of your dominant one.",
	"The boundary you drew last month is already being tested. Hold it.",
	"Lucky cloud formation: cumulus.",
	"The elevator is fine, but the stairs offer something useful today.",
	"Your lucky silence is the one that follows the thing you almost said.",
	"The box labeled later has something in it that belongs in now.",
	"Lucky coin: the one in your pocket you forgot about.",
	"The angle of approach matters more than the force of approach.",
	"Your lucky hour is the one between midnight and whatever comes after it.",
	"The plan that got a lot of feedback needs less feedback and more action.",
	"Lucky compass direction: slightly off from due north.",
	"The reservation you almost made would have been worth making.",
	"Your lucky pause: the one before you respond.",
	"The last time you were in this situation, you did the right thing. Do it again.",
	"Lucky version: the draft before the final draft.",
	"The thing that seems too fast is actually the appropriate speed.",
	"Your lucky noise is the background kind - not silence, not overwhelming.",
	"The detail everyone else has been glossing over is the one you noticed. Trust that.",
	"Lucky combination: two things that seem unlikely to work together.",
	"The gift you were given that you never properly used is ready to be used.",
	"Your lucky failure: the most recent one. It is still teaching.",
	"The map is correct. Your reading of it has been off.",
	"Lucky ingredient: the one you almost left out.",
	"The reason you have been reluctant is also the reason you should proceed.",
	"Your lucky window: the one facing the direction you have not been looking.",
	"The argument that went badly had a valid point buried inside it. Find it.",
	"Lucky version number: not the first, not the latest.",
	"The password you keep almost remembering will come to you when you stop trying.",
	"Your lucky platform: the one you have been underusing.",
	"The deadline is a gift, not a threat. Use the pressure.",
	"Lucky floor: not the top, not the bottom.",
	"Something you agreed to out of obligation turns out to be something you want.",
	"Your lucky verb for today: begin.",
	"The person who asks you a favor today is giving you one in return.",
	"Lucky radius: arms' length.",
	"The last draft was better. Return to it.",
	"Your lucky exception is the rule you have never applied to yourself.",
	"The disagreement you avoided having will come around again with compounded interest. Have it now.",
	"Lucky altitude: higher than comfortable, lower than dangerous.",
	"The discount you did not ask for is available if you ask.",
	"Your lucky moment of the day is the one you were not watching for.",
	"The short version of the answer is the true version.",
	"Lucky interval: slightly longer than your patience usually allows.",
	"The system is working fine. The interpretation of the system is the problem.",
	"Your lucky paradox: the thing that costs you most also earns you most.",
	"The backup plan is not inferior. Commit to it.",
	"Lucky location: somewhere you have been meaning to go back to.",
	"The thought you interrupted yourself from finishing is worth finishing.",
	"Your lucky form of communication: the written word.",
	"The second cup is better than the first.",
	"Lucky era: this one. It is better than it seems.",
	"The version of the story you tell yourself is missing a few facts. Update it.",
	"Your lucky approach: slower and more deliberate than usual.",
	"The person who seems difficult is navigable with a different key.",
	"Lucky symbol: whatever you drew last time you were waiting for something.",
	"The last time you gave yourself permission to do that, it went well.",
	"Your lucky absence: the meeting you opt out of today.",
	"The obvious solution is not wrong just because it is obvious.",
	"Lucky bridge: the one between where you are and where you have been avoiding.",
	"The earlier version of you would have been delighted by where you are now.",
	"Your lucky redundancy: the backup of the backup.",
	"The polite refusal is available to you today and is worth practicing.",
	"Lucky margin: more than comfortable, less than lavish.",
	"The thing you said you would never do again is not the thing you are about to do. Proceed.",
	"Your lucky interruption: the one that saves you from your own momentum.",
	"The tool you reach for second is the right one for today.",
	"Lucky contrast: whatever makes your situation look different from a different angle.",
	"The long way around is, today, the short way.",
	"Your lucky credential: the experience, not the certificate.",
	"The pattern you keep noticing is not a coincidence.",
	"Lucky iteration: the one after the disappointing one.",
	"The habit you maintain for reasons you cannot articulate is doing something important.",
	"Your lucky threshold: the one you have been standing at for three weeks.",
	"The question is not when. The question is whether.",
	"Lucky archive: something you wrote but did not send.",
	"The second read of that document reveals something the first missed.",
	"Your lucky constraint: the one that is forcing a better solution.",
	"The version of events in which you are not at fault is more accurate than you have allowed.",
	"Lucky ratio: better than fifty-fifty.",
	"The note to self you keep not writing is worth writing.",
	"Your lucky resistance: the gentle kind.",
	"The thing you are building has been building longer than you know.",
	"Lucky medium: between the two extremes you have been choosing between.",
	"The day you look back on as a turning point is happening during an unremarkable afternoon.",
	"Your lucky alignment: the one between what you say and what you do.",
	"The person you thought had forgotten about you has not.",
	"Lucky measure: the one you have been using imprecisely.",
	"The instinct you keep overriding with logic is correct today.",
	"Your lucky ground floor: you are on it.",
	"The pause between the question and the answer is where the answer actually lives.",
	"Lucky frame: the one around the thing you have been too close to see clearly.",
	"The habit of second-guessing yourself is working against you today. Stop.",
	"Your lucky precedent: the time this worked before.",
	"The edge you have been maintaining cost you something. Today it pays back.",
	"Lucky iteration count: more than one, fewer than ten.",
	"The thing you have been rehearsing in your head needs to be said out loud.",
	"Your lucky category: the one you have been difficult to classify in.",
	"The bridge behind you is still there if you need it.",
	"Lucky revision: the one that removes more than it adds.",
	"The version of success you have been picturing is underselling the actual outcome.",
	"Your lucky delay: the one that made space for something better to arrive.",
	"The situation contains an exit you have not mapped yet.",
	"Lucky precedent: something that worked once and was never tried again.",
	"The gap between your expectations and reality is smaller today than it has been.",
	"Your lucky concession: the one that costs you little and returns much.",
	"The message that arrived without words is the most important one.",
	"Lucky overlap: the place where two things you care about intersect.",
	"The risk profile of the thing you are afraid to do has improved without your noticing.",
	"Your lucky silence: what you do not say today is as important as what you do.",
	"The map of your situation needs updating. The terrain has changed.",
	"Lucky correction: the small one that changes the trajectory dramatically.",
	"The gravy of today's situation is rich. Use it while it is warm.",
	"Lucky sauce: thicker than expected, better than it looks.",
	// Dinosaur
	"Avoid anything resembling a Chicxulub-scale disruption. You will know it when you see it.",
	"Lucky claw: the middle one.",
	"The small mammals in your life are more resourceful than you have credited them.",
	"Your thick hide is not a liability. Today it is armor.",
	"The amber preserving your past self is beautiful, but you are not in it anymore.",
	"Lucky geological era: the current one. Survivorship bias works in your favor.",
	"Beware of ferns. In the Jurassic they meant danger. This remains true today.",
	"Your lucky bone is the one that healed after the injury.",
	"The sediment is compressed. The fossil is forming. Do not disturb the process.",
	"Beware of things moving too fast to process. The fast ones were usually the predators.",
	"Lucky stratum: the one slightly below the surface.",
	"Your protective plates are on the correct side of your body.",
	"The warm inland sea of your comfort zone has advantages. Also limitations.",
	"Lucky spike: the one you use only when necessary.",
	"The ice age metaphor for your career is ending. Warmer climates ahead.",
	"Beware the slow-moving herd. It can crush you without noticing.",
	"Your lucky footprint: the one you leave that is deeper than expected.",
	"The extinction-level event in your inbox has passed. Clear the debris.",
	"Lucky mineral deposit: pyrite is a warning. Check the assay.",
	"Beware of environments that look hospitable but are not.",
	"Your lucky scale: the one at the base of the neck, where flexibility matters.",
	"The feathers you have been growing will serve a different purpose than flight.",
	"Lucky herbivore move: eating a lot and moving slowly. Effective today.",
	"Beware anything that has been waiting in a swamp for longer than you have.",
	"Your lucky geological feature: not the fault line.",
	"The cold blood in a warm room is a diagnostic, not a verdict.",
	"Lucky ancient wisdom: if it worked for 150 million years, do not reinvent it.",
	"Beware the clever biped. Quick hands, fast minds, unreliable loyalties.",
	"Your lucky epoch: not the Permian.",
	"The extinction of your old habits creates a niche. Fill it deliberately.",
	// Dolphin
	"Lucky sound: high-pitched and joyful, just above human hearing.",
	"Beware shallow water. It is more dangerous than the open sea.",
	"Avoid tuna nets disguised as exceptional opportunities.",
	"Your blowhole is a metaphor. Use it: surface, breathe, submerge.",
	"Lucky depth: deeper than comfortable, shallower than crushing.",
	"Beware those who cannot read the current. They will swim into yours.",
	"Your lucky signature whistle: use it more often.",
	"The tidal pull is real and not something to fight today.",
	"Lucky temperature: cold enough to think clearly.",
	"Beware those who can only see to the surface.",
	"Your lucky wave: the one behind the one you were watching.",
	"The pod rule: when one surfaces, all surface.",
	"Lucky mineral: salt. In the water and in the wound, it serves a purpose.",
	"Beware the net that looks like open water.",
	"Your lucky current: the one that runs deep and fast and invisible from above.",
	"The seagrass meadow of your comfort zone has nutritional limits. Venture further.",
	"Lucky bioluminescence: the kind that shows up in darkness.",
	"Beware large slow things that seem harmless.",
	"Your lucky dive: the one you were not sure you could make.",
	"The click train of your intuition is carrying more information than you are processing.",
	"Lucky surface: the one where the air is freshest.",
	// Thorazine
	"Side effects may include sudden clarity, mild purpose, and an unsettling acceptance of things.",
	"Do not discontinue anything that is currently working, regardless of what the label says.",
	"The recommended dosage of caution is lower than the manufacturer suggests.",
	"Lucky symptom: the manageable one.",
	"Beware of anyone who claims their thinking has no side effects.",
	"Your lucky contraindication: the thing you should not mix with today's ambitions.",
	"The half-life of today's anxiety is shorter than it feels.",
	"Lucky receptor: the one that responds to rest.",
	"Beware of therapeutic approaches that require you to feel worse before feeling better - unless you are currently in one.",
	"Your lucky mechanism of action: patience applied consistently over time.",
	"The titration is nearly complete. Do not rush the last adjustments.",
	"Lucky dosage form: extended release.",
	"Beware anything with a black box warning in your circumstances.",
	"Your lucky pharmacokinetic property: a slow onset with a long half-life.",
	"The protocol requires one more cycle. Follow it.",
	// EDM
	"Lucky tempo: faster than your current pace.",
	"The DJ of fate has your next track queued. It opens with a long build.",
	"No limit. This applies to your ambitions and, today, to your lucky numbers.",
	"Untz untz untz. You know what that means. Act accordingly.",
	"Lucky BPM: 138.",
	"Beware of anyone who wants to slow the tempo when you have finally got it right.",
	"Your lucky drop: the one at 3:47 that nobody saw coming.",
	"The rave ends, but the feeling does not have to.",
	"Lucky light show: the one where the strobes sync to something internal.",
	"Beware the silence between tracks. It is where poor decisions are made.",
	"Your lucky side of the record: the B-side that became the A-side.",
	"The kick drum of your ambition is on beat. Keep it there.",
	"Lucky venue: somewhere underground that you have to know about.",
	"Beware those who only listen to the radio edit.",
	"Your lucky vinyl: the one with the skip that you worked into the set.",
	"The acid line resolves. Eventually. Stay with it.",
	"Lucky hour: 4 AM. That is when clarity arrives for those still standing.",
	"Beware the encore. Sometimes the set was perfect. Let it end.",
	"Your lucky sample: something borrowed from before you were born.",
	"The crowd at the club of your life is waiting. They have been waiting.",
	"Lucky mix: rougher elements cut with something sweet.",
	"Beware anyone who cannot feel the beat. They are unreliable partners on the floor.",
	"Your lucky track: the one that nobody expected to work but that cleared the floor.",
	"The night is young. In the metaphorical sense, it is always 11 PM.",
}

var luckyConstants = []string{
	// Transcendental and mathematical constants
	"pi (3.1415927)",
	"e (2.7182818)",
	"phi (1.6180340)",
	"tau (6.2831853)",
	"sqrt(2) (1.4142136)",
	"sqrt(3) (1.7320508)",
	"sqrt(5) (2.2360680)",
	"gamma (0.5772157)",
	"ln(2) (0.6931472)",
	"ln(10) (2.3025851)",
	"Feigenbaum delta (4.6692017)",
	"zeta(3) (1.2020569)",
	"Catalan's constant (0.9159656)",
	"Khinchin's constant (2.6854520)",
	"Glaisher's constant (1.2824272)",
	"Mills' constant (1.3063779)",
	"omega (0.5671433)",
	"1/sqrt(2*pi) (0.3989423)",
	"Plastic constant (1.3247180)",
	"Champernowne constant (0.1234567)",
	// Physical and scientific constants
	"c (2.9979246 * 10^8 m/s)",
	"h (6.6260702 * 10^-34 J*s)",
	"h-bar (1.0545718 * 10^-34 J*s)",
	"G (6.6743015 * 10^-11 N*m^2/kg^2)",
	"kB (1.3806503 * 10^-23 J/K)",
	"NA (6.0221408 * 10^23 /mol)",
	"alpha (7.2973526 * 10^-3)",
	"me (9.1093837 * 10^-31 kg)",
	"R (8.3144598 J/(mol*K))",
	"sigma (5.6703744 * 10^-8 W/(m^2*K^4))",
}

func formatWithCommas(n int) string {
	s := strconv.Itoa(n)
	var result []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, s[i])
	}
	return string(result)
}

func generateLuckyNumber(rng *rand.Rand) string {
	roll := rng.Intn(100)
	switch {
	case roll < 70:
		// common small integer 1-99
		return strconv.Itoa(rng.Intn(99) + 1)
	case roll < 85:
		// larger integer 101-1000
		return strconv.Itoa(rng.Intn(900) + 101)
	case roll < 90:
		// large number: millions or billions
		if rng.Intn(2) == 0 {
			return formatWithCommas(rng.Intn(998_000_000) + 1_000_000)
		}
		return formatWithCommas(rng.Intn(998_000_000_000) + 1_000_000_000)
	case roll < 95:
		// decimal with 5-7 places
		places := rng.Intn(3) + 5
		return fmt.Sprintf("%.*f", places, float64(rng.Intn(100))+rng.Float64())
	default:
		// transcendental or physical constant
		return luckyConstants[rng.Intn(len(luckyConstants))]
	}
}

func generateHoroscope(userID int, date time.Time) string {
	dateStr := date.UTC().Format("2006-01-02")
	h := fnv.New64a()
	h.Write([]byte(fmt.Sprintf("%d-%s", userID, dateStr)))
	rng := rand.New(rand.NewSource(int64(h.Sum64())))

	opener := horoscopeOpeners[rng.Intn(len(horoscopeOpeners))]
	prediction := horoscopePredictions[rng.Intn(len(horoscopePredictions))]
	closer := horoscopeClosers[rng.Intn(len(horoscopeClosers))]
	luckyNum := generateLuckyNumber(rng)

	return fmt.Sprintf("%s %s %s Lucky number for today: %s.", opener, prediction, closer, luckyNum)
}

func (app *application) checkLineForRegexps(line string) (string, error) {
	var userID string

	re := regexp.MustCompile(`^\[.*\((#\d+)\)\]`)
	userIDMatch := re.FindSubmatch([]byte(line))
	if len(userIDMatch) < 2 {
		return "", nil
	}
	userID = string(userIDMatch[1])

	re = regexp.MustCompile(`(http\:|https\:|ftp\:|ftps\:|telnet\:|telnets\:|ssh\:|www\.)[^ \"]+`)

	urls := re.FindAll([]byte(line), -1)
	if len(urls) > 0 {
		return app.processUrls(userID, urls)
	}

	re = regexp.MustCompile(`\[.*\(#\d+\)\] .+ pages: hangout$`)
	matched := re.MatchString(line)
	if matched {
		command := "@dolist me={gautoreturn on;hangout}\n"
		return command, nil
	}
	re = regexp.MustCompile(`\[.*\(#\d+\)\] .+ pages: home$`)
	matched = re.MatchString(line)
	if matched {
		command := "@dolist me={gautoreturn off;home}\n"
		return command, nil
	}

	re = regexp.MustCompile(`(?i)\[.*\(#\d+\)\] .+ says "Gravybot\,? translate (\S+) (\S+) (.*)"$`)
	s := re.FindSubmatch([]byte(line))
	if s != nil {
		if len(s) != 4 {
			fmt.Println("GRAVYTRANSLATE wrong len")
			return "", nil
		} else {
			sourceLang := string(s[1])
			targetLang := string(s[2])
			textToTranslate := string(s[3])

			translatedText, err := app.translateText(sourceLang, targetLang, textToTranslate)
			if err != nil {
				fmt.Println("GRAVYTRANSLATE request fail")
				fmt.Println(err)
				translatedText = "Error: translation failed."
			}

			command := "pose T> " + translatedText + "\n"

			return command, nil
		}
	}

	re = regexp.MustCompile(`(?i)\[.*\(#\d+\)\] .+ says "Gravybot\,? weather (.+)"$`)
	s = re.FindSubmatch([]byte(line))

	if s != nil {
		if len(s) < 2 {
			fmt.Println("GRAVYWEATHER wrong len")
			return "", nil
		} else {
			locations := strings.Split(string(s[1]), ",")
			if len(locations) > 5 {
				locations = locations[:5]
			}
			var commands []string
			for _, loc := range locations {
				loc = strings.TrimSpace(loc)
				if loc == "" {
					continue
				}
				query := url.QueryEscape(loc)

				response, err := app.sendWeatherRequest(query)
				if err != nil {
					fmt.Println("GRAVYWEATHER request fail")
					fmt.Println(err)
					response = "Error: weather api call failed.\n"
				}
				commands = append(commands, "pose W> "+response)
			}

			return strings.Join(commands, ""), nil
		}
	}

	re = regexp.MustCompile(`(?i)\[.*\(#\d+\)\] .+ says "(gbs|Gravybot\,? stock) (.+)"$`)
	s = re.FindSubmatch([]byte(line))

	if s != nil {
		if len(s) < 3 {
			fmt.Println("GBS wrong len")
			return "", nil
		} else {
			symbols := strings.Split(string(s[2]), ",")
			if len(symbols) > 5 {
				symbols = symbols[:5]
			}
			var commands []string
			for _, sym := range symbols {
				sym = strings.TrimSpace(sym)
				if sym == "" {
					continue
				}

				if strings.HasPrefix(strings.ToLower(sym), "c:") {
					cryptoQuery := sym[2:]
					response, err := app.getCryptoQuote(cryptoQuery)
					if err != nil {
						fmt.Println("GBC request fail")
						fmt.Println(err)
						response = "Error: crypto quote api call failed.\n"
					}
					commands = append(commands, "pose S> "+response)
				} else {
					response, err := app.getStockQuote(sym)
					if err != nil {
						fmt.Println("GBS request fail")
						fmt.Println(err)
						response = "Error: stock quote api call failed.\n"
					}
					commands = append(commands, "pose S> "+response)
				}
			}

			return strings.Join(commands, ""), nil
		}
	}

	re = regexp.MustCompile(`(?i)\[.*\(#\d+\)\] .+ says "gravybot horoscope (#\d+)"$`)
	s = re.FindSubmatch([]byte(line))

	if s != nil {
		if len(s) < 2 {
			fmt.Println("GBH wrong len")
			return "", nil
		}
		dbrefNum, err := strconv.Atoi(strings.TrimPrefix(string(s[1]), "#"))
		if err != nil {
			return "pose H> Error: invalid player ID.\n", nil
		}
		horoscope := generateHoroscope(dbrefNum, time.Now().UTC())
		return "pose H> " + horoscope + "\n", nil
	}

	return "", nil
}

func (app *application) processUrls(authorID string, urls [][]byte) (string, error) {
	var botData string = ""
	if len(urls) > 0 {
		for _, urlBytes := range urls {
			longUrl := string(urlBytes)
			if strings.HasPrefix(strings.ToLower(longUrl), "www") {
				longUrl = "http://" + longUrl
			}
			u, err := url.Parse(longUrl)
			if err != nil {
				return "", err
			}

			shortUrl, err := app.sendUrlToYirp(u.String())
			if err == nil && shortUrl != "" {
				botData = botData + "add_url " + authorID + " " + shortUrl + " " + u.String() + "\n"
				botData = botData + "@trigger me/TRIGGER_LAST_URL\n"
			}
		}
	}
	app.infoLog.Printf("botData: %s\n", botData)

	return botData, nil
}

func (c caller) CallTELNET(ctx telnet.Context, w telnet.Writer, r telnet.Reader) {
	var command string = ""
	c.app.infoLog.Printf("connect " + c.app.config.username + " <password>\n")
	w.Write([]byte("connect " + c.app.config.username + " " + c.app.config.password + "\n"))

	var buffer [1]byte // Seems like the length of the buffer needs to be small, otherwise will have to wait for buffer to fill up.
	p := buffer[:]

	var line bytes.Buffer

	for {
		// Read 1 byte.
		n, err := r.Read(p)
		if n <= 0 && nil == err {
			c.app.infoLog.Println("READ 0")
			continue
		} else if n <= 0 && nil != err {
			break
		}

		line.WriteByte(p[0])
		if p[0] == '\n' {
			lineString := strings.TrimSpace(line.String())

			c.app.infoLog.Println(lineString)
			if command == "" {
				command, err = c.app.checkLineForRegexps(lineString)
			}

			if err != nil {
				c.app.errorLog.Println(err)
			}
			c.app.botSend(w, "@@\n")
			if command != "" {
				c.app.botSend(w, command)
			}
			command = ""
			line.Reset()
		}
	}
}
