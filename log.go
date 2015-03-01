package wbgo

import (
	"os"
	"io/ioutil"
	"log"
)

var (
	Error *log.Logger
	Warn *log.Logger
	Info  *log.Logger
	Debug *log.Logger
)

func init() {
	Error = log.New(os.Stderr, "ERROR: ", log.LstdFlags)
	Warn = log.New(os.Stderr, "WARNING: ", log.LstdFlags)
	Info = log.New(os.Stderr, "INFO: ", log.LstdFlags)
	Debug = log.New(ioutil.Discard, "", 0)
}

func SetDebuggingEnabled(enable bool) {
	if (enable) {
		Debug = log.New(os.Stderr, "DEBUG: ", log.LstdFlags)
	} else {
		Debug = log.New(ioutil.Discard, "", 0)
	}	
}
