package wbgo

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"testing"
	"time"
)

const (
	WAIT_INTERVAL_MS       = 10
	WAIT_COUNT             = 300
	REC_EMPTY_WAIT_TIME_MS = 50
	REC_SKIP_TIME_MS       = 3000
	REC_ITEM_TIMEOUT_MS    = 5000
)

func WaitFor(t *testing.T, pred func() bool) {
	for n := 0; n < WAIT_COUNT; n++ {
		if pred() {
			return
		}
		time.Sleep(WAIT_INTERVAL_MS * time.Millisecond)
	}
	t.Fatalf("WaitFor() failed")
}

type Recorder struct {
	t  *testing.T
	ch chan string
}

func (rec *Recorder) InitRecorder(t *testing.T) {
	rec.t = t
	rec.ch = make(chan string, 1000)
}

func (rec *Recorder) Rec(format string, args ...interface{}) {
	item := fmt.Sprintf(format, args...)
	rec.t.Log("REC: ", item)
	rec.ch <- item
}

func (rec *Recorder) VerifyEmpty() {
	// this may not always work but will help catch
	// errors at least in some cases
	timer := time.NewTimer(REC_EMPTY_WAIT_TIME_MS * time.Millisecond)
	select {
	case <-timer.C:
		return
	case logItem := <-rec.ch:
		timer.Stop()
		rec.t.Fatalf("unexpected logs: %s", logItem)
	}
}

func (rec *Recorder) Verify(logs ...string) {
	if logs == nil {
		rec.VerifyEmpty()
	} else {
		actualLogs := make([]string, 0, len(logs))
		for _, expectedItem := range logs {
			timer := time.NewTimer(REC_ITEM_TIMEOUT_MS * time.Millisecond)
			select {
			case <-timer.C:
				rec.t.Fatalf("timed out waiting for log item: %s", expectedItem)
			case logItem := <-rec.ch:
				timer.Stop()
				actualLogs = append(actualLogs, logItem)
			}
		}
		require.Equal(rec.t, logs, actualLogs, "rec logs")
	}
}

func (rec *Recorder) VerifyUnordered(logs ...string) {
	if logs == nil {
		rec.VerifyEmpty()
	} else {
		sort.Strings(logs)
		actualLogs := make([]string, 0, len(logs))
		for _ = range logs {
			actualLogs = append(actualLogs, <-rec.ch)
		}
		sort.Strings(actualLogs)
		require.Equal(rec.t, logs, actualLogs, "rec logs (unordered)")
	}
}

func (rec *Recorder) SkipTill(logItem string) {
	timer := time.NewTimer(REC_EMPTY_WAIT_TIME_MS * time.Millisecond)
	for {
		select {
		case <-timer.C:
			rec.t.Fatalf("failed waiting for log: %s", logItem)
			return
		case l := <-rec.ch:
			if l == logItem {
				timer.Stop()
				return
			}
		}
	}
}

func (rec *Recorder) T() *testing.T {
	return rec.t
}

// TestLog makes it possible to use log module with testing's
// logging functions, so that the logging output is only
// shown when the test fails. Note that a part at the end
// of output that is not newline-terminated is not displayed.
type TestLog struct {
	buf []byte
	acc []byte
	t   *testing.T
}

func NewTestLog(t *testing.T) *TestLog {
	buf := make([]byte, 0, 1024)
	return &TestLog{buf, buf[:0], t}
}

func (tl *TestLog) Write(p []byte) (n int, err error) {
	tl.acc = append(tl.acc, p...)
	s := 0
	for i := 0; i < len(tl.acc); i++ {
		if tl.acc[i] == 10 {
			tl.t.Log(string(tl.acc[s:i]))
			s = i + 1
		}
	}
	if s == len(tl.acc) {
		tl.acc = tl.buf[:0]
	} else {
		tl.acc = tl.acc[s:]
	}
	return len(p), nil
}

func SetupTestLogging(t *testing.T) {
	Error = log.New(NewTestLog(t), "ERROR: ", log.Lshortfile)
	Warn = log.New(NewTestLog(t), "WARNING: ", log.Lshortfile)
	Info = log.New(NewTestLog(t), "INFO: ", log.Lshortfile)
	Debug = log.New(NewTestLog(t), "DEBUG: ", log.Lshortfile)
}

// SetupTempDir sets up a temporary directory to be used in tests.
// In case of an error, makes the test fail.
func SetupTempDir(t *testing.T) (string, func()) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("couldn't get the current directory")
		return "", nil // never reached
	}

	dir, err := ioutil.TempDir(os.TempDir(), "ruletest")
	if err != nil {
		t.Fatalf("couldn't create temporary directory")
		return "", nil // never reached
	}

	os.Chdir(dir)
	return dir, func() {
		os.RemoveAll(dir)
		os.Chdir(wd)
	}
}
