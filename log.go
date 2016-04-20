package wbgo

import (
	"io/ioutil"
	"log"
	"log/syslog"
	"os"
	"path"
	"sync"
	"sync/atomic"
)

var (
	Error *log.Logger
	Warn  *log.Logger
	Info  *log.Logger
	Debug *log.Logger
	// we want debug flag access to be FAST so we use sync.atomic
	// to access it
	debuggingEnabled int32 = 0
	useSyslog              = false
	keepDebug              = false
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
	if enable {
		atomic.StoreInt32(&debuggingEnabled, 1)
	} else {
		atomic.StoreInt32(&debuggingEnabled, 0)
	}
	updateDebugLogger()
}

func DebuggingEnabled() bool {
	return atomic.LoadInt32(&debuggingEnabled) != 0
}

func updateDebugLogger() {
	// avoid debug flag / logger inconsitency
	debugMutex.Lock()
	defer debugMutex.Unlock()
	switch {
	case keepDebug:
		return
	case !DebuggingEnabled():
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
