package wbgo

import (
	"os"
	"path"
	"io/ioutil"
	"log"
	"log/syslog"
)

var (
	Error *log.Logger
	Warn *log.Logger
	Info  *log.Logger
	Debug *log.Logger
	debuggingEnabled bool = false
	useSyslog = false
)

func init() {
	Error = log.New(os.Stderr, "ERROR: ", log.LstdFlags)
	Warn = log.New(os.Stderr, "WARNING: ", log.LstdFlags)
	Info = log.New(os.Stderr, "INFO: ", log.LstdFlags)
	Debug = log.New(ioutil.Discard, "", 0)
}

func makeSyslogger(priority syslog.Priority, prefix string) *log.Logger {
	writer, err := syslog.New(syslog.LOG_DAEMON | syslog.LOG_INFO, path.Base(os.Args[0]))
	if err != nil {
		log.Panicf("syslog init failed: %s", err)
	}
	return log.New(writer, prefix, 0)
}

func SetDebuggingEnabled(enable bool) {
	debuggingEnabled = enable
	updateDebugLogger()
}

func updateDebugLogger() {
	switch {
	case !debuggingEnabled:
		Debug = log.New(ioutil.Discard, "", 0)
	case useSyslog:
		Debug = makeSyslogger(syslog.LOG_DAEMON | syslog.LOG_DEBUG, "DEBUG: ")
	default:
		Debug = log.New(os.Stderr, "DEBUG: ", log.LstdFlags)
	}
}

func UseSyslog() {
	useSyslog = true
	Error = makeSyslogger(syslog.LOG_DAEMON | syslog.LOG_ERR, "ERROR: ")
	Warn = makeSyslogger(syslog.LOG_DAEMON | syslog.LOG_WARNING, "WARNING: ")
	Info = makeSyslogger(syslog.LOG_DAEMON | syslog.LOG_INFO, "INFO: ")
	updateDebugLogger()
}
