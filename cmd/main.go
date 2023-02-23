package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/reiver/go-telnet"
)

type config struct {
	srvAddr  string
	username string
	password string
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
	flag.StringVar(&cfg.username, "u", "username", "Username")
	flag.StringVar(&cfg.password, "p", "passowrd", "Password")

	flag.Parse()

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

func (c caller) CallTELNET(ctx telnet.Context, w telnet.Writer, r telnet.Reader) {
	w.Write([]byte("connect " + c.app.config.username + " " + c.app.config.password + "\n"))

	var buffer [1]byte // Seems like the length of the buffer needs to be small, otherwise will have to wait for buffer to fill up.
	p := buffer[:]

	var line bytes.Buffer

	for {
		// Read 1 byte.
		n, err := r.Read(p)
		if n <= 0 && nil == err {
			continue
		} else if n <= 0 && nil != err {
			break
		}

		line.WriteByte(p[0])
		if p[0] == '\n' {
			lineString := line.String()
			println(lineString)

			// TODO: Process lineString HERE
		}

	}
}
