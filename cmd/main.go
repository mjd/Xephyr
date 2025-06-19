package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
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
	srvAddr     string
	yirpAPIAddr string
	yirpapikey  string
	username    string
	password    string
	weatherapikey string
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

func (app *application) sendWeatherRequestOLD(query string) (string, error) {
	res, err := http.Get("https://wttr.in/" + query + "?format=%l:+%C+%t+%h+%p+%w")

	if err != nil {
		app.errorLog.Printf("weather request failed: %s", err)
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode == 404 {
		result := "Weather error: " + query + " not found. Try using a City State or City Country pair.\n"
		fmt.Println(result)
		return result, nil
	}

	if res.StatusCode > 299 {
		result := "Weather error: API returned code: " + strconv.Itoa(res.StatusCode) + "\n"
		fmt.Println(result)
		return result, nil
	}

	if res.ContentLength < 10 {
		app.errorLog.Printf("Weather request failed ContentLength: %d", res.ContentLength)
		return "", fmt.Errorf("weather request failed ContentLength: %d", res.ContentLength)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		app.errorLog.Printf("Weather request failed ReadAll: %s", err)
		return "", err
	}

	r := strings.NewReplacer("\n", "%r", "’", "'", "―", "-", "\\", "\\\\", "%", "\\%", ";", "\\;", "[", "\\[", "]", "\\]",
		"{", "\\{", "}", "\\}")
	result := "Weather report: " + r.Replace(string(body)) + " https://wttr.in/" + query + "\n"
	fmt.Println(result)

	return result, nil
}

type WeatherAPIResponse struct {
	Location struct {
		Name    string `json:"name"`
		Region  string `json:"region"`
		Country string `json:"country"`
		Lat     float64 `json:"lat"`
		Lon     float64 `json:"lon"`
	} `json:"location"`

	Current struct {
		Last_updated string  `json:"last_updated"`
		Temp_c       float64 `json:"temp_c"`
		Temp_f       float64 `json:"temp_f"`
		Condition    struct {
			Text     string  `json:"text"`
		} `json:"condition"`
		Wind_mph float64 `json:"wind_mph"`
		Wind_kph float64 `json:"wind_kph"`
		Wind_dir string  `json:"wind_dir"`
		Humidity float64 `json:"humidity"`
	} `json:"current"`
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
	
	if (strings.HasPrefix(weatherResponse.Location.Country, "United States of America") || strings.HasPrefix(weatherResponse.Location.Country, "USA")) {
		locationRegion = weatherResponse.Location.Region
		result = fmt.Sprintf("Weather %v, %v: %v %.1fF %.1f%%%% %.1fmph %v\n", weatherResponse.Location.Name, locationRegion, weatherResponse.Current.Condition.Text, weatherResponse.Current.Temp_f, weatherResponse.Current.Humidity, weatherResponse.Current.Wind_mph, weatherResponse.Current.Wind_dir)
	} else {
		locationRegion = weatherResponse.Location.Country
		result = fmt.Sprintf("Weather %v, %v: %v %.1fC %.1f%%%% %.1fkph %v\n", weatherResponse.Location.Name, locationRegion, weatherResponse.Current.Condition.Text, weatherResponse.Current.Temp_c, weatherResponse.Current.Humidity, weatherResponse.Current.Wind_kph, weatherResponse.Current.Wind_dir)
	}

	return result, nil
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
		return result, nil
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

	re = regexp.MustCompile(`(?i)\[.*\(#\d+\)\] .+ says "Gravybot\,? weather (.+)"$`)
	s := re.FindSubmatch([]byte(line))

	if s != nil {
		if len(s) < 2 {
			fmt.Println("GRAVYWEATHER wrong len")
			return "", nil
		} else {
			query := url.QueryEscape(string(s[1]))

			response, err := app.sendWeatherRequest(query)
			if err != nil {
				fmt.Println("GRAVYWEATHER request fail")
				fmt.Println(err)

				response = "Error: weather api call failed."
			}

//			responseOLD, err := app.sendWeatherRequestOLD(query)
//			if err != nil {
//				fmt.Println("GRAVYWEATHEROLD request fail")
//				fmt.Println(err)
//
//				responseOLD = "Error: " + string(err.Error())
//			}

			command := "pose > " + response + "\n"
			return command, nil
		}
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
