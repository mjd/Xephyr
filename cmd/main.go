package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
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

func (app *application) sendUrlToYirp(url string) (string, error) {
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

		return "", errors.New(res.Status)
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
	re := regexp.MustCompile(`\[.*\(#\d+\)\] .+ pages: hangout$`)
	matched := re.MatchString(line)
	if matched {
		command := "hangout\n"
		return command, nil
	}
	re = regexp.MustCompile(`\[.*\(#\d+\)\] .+ pages: home$`)
	matched = re.MatchString(line)
	if matched {
		command := "home\n"
		return command, nil
	}

	return "", nil
}

func (app *application) checkLineForUrls(line string) (string, error) {
	var urlFlag string = "GRAVYURLMATCH"
	var delimeterRegex string = "[\\s]"

	s := regexp.MustCompile(delimeterRegex).Split(line, -1)
	if len(s) < 3 {
		return "", nil
	}

	if s[0] == urlFlag && len(s) >= 3 {
		authorID := s[1]
		longUrl := s[2]
		if strings.HasPrefix(strings.ToLower(longUrl), "www") {
			longUrl = "http://" + longUrl
		}
		u, err := url.ParseRequestURI(longUrl)
		if err != nil {
			return "", err
		}

		shortUrl, err := app.sendUrlToYirp(u.String())
		if err == nil && shortUrl != "" {
			botData := "add_url " + authorID + " " + shortUrl + " " + u.String() + "\n"
			botData = botData + "@trigger me/TRIGGER_LAST_URL\n"
			return botData, nil
		}
	}
	return "", nil
}

func (c caller) CallTELNET(ctx telnet.Context, w telnet.Writer, r telnet.Reader) {
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
			command, err := c.app.checkLineForUrls(lineString)
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
			line.Reset()
		}
	}
}
