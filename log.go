package wbgo

import (
	"io/ioutil"
	"log"
	"log/syslog"
	"os"
	"path"
	"sync"
)

var (
	Error            *log.Logger
	Warn             *log.Logger
	Info             *log.Logger
	Debug            *log.Logger
	debuggingEnabled bool = false
	useSyslog             = false
	keepDebug             = false
	debugMutex       sync.Mutex
)

func init() {
	Error = log.New(os.Stderr, "ERROR: ", log.LstdFlags)
	Warn = log.New(os.Stderr, "WARNING: ", log.LstdFlags)
	Info = log.New(os.Stderr, "INFO: ", log.LstdFlags)
	Debug = log.New(ioutil.Discard, "", 0)
}

func makeSyslogger(priority syslog.Priority, prefix string) *log.Logger {
	writer, err := syslog.New(syslog.LOG_DAEMON|syslog.LOG_INFO, path.Base(os.Args[0]))
	if err != nil {
		log.Panicf("syslog init failed: %s", err)
	}
	return log.New(writer, prefix, 0)
}

func SetDebuggingEnabled(enable bool) {
	debugMutex.Lock()
	debuggingEnabled = enable
	debugMutex.Unlock()
	updateDebugLogger()
}

func DebuggingEnabled() bool {
	debugMutex.Lock()
	defer debugMutex.Unlock()
	return debuggingEnabled
}

func updateDebugLogger() {
	debugMutex.Lock()
	defer debugMutex.Unlock()
	switch {
	case keepDebug:
		return
	case !debuggingEnabled:
		Debug = log.New(ioutil.Discard, "", 0)
	case useSyslog:
		Debug = makeSyslogger(syslog.LOG_DAEMON|syslog.LOG_DEBUG, "DEBUG: ")
	default:
		Debug = log.New(os.Stderr, "DEBUG: ", log.LstdFlags)
	}
}

func UseSyslog() {
	useSyslog = true
	Error = makeSyslogger(syslog.LOG_DAEMON|syslog.LOG_ERR, "ERROR: ")
	Warn = makeSyslogger(syslog.LOG_DAEMON|syslog.LOG_WARNING, "WARNING: ")
	Info = makeSyslogger(syslog.LOG_DAEMON|syslog.LOG_INFO, "INFO: ")
	updateDebugLogger()
}
