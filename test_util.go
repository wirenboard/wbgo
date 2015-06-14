package wbgo

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"sync"
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

var testStartTime = time.Date(2015, 2, 27, 19, 33, 17, 0, time.UTC)

// WaitFor waits for the function specified by pred to return true.
// The test fails if it takes too long.
func WaitFor(t *testing.T, pred func() bool) {
	for n := 0; n < WAIT_COUNT; n++ {
		if pred() {
			return
		}
		time.Sleep(WAIT_INTERVAL_MS * time.Millisecond)
	}
	t.Fatalf("WaitFor() failed")
}

type Fixture struct {
	t *testing.T
}

func (fixture *Fixture) T() *testing.T {
	return fixture.t
}

func NewFixture(t *testing.T) *Fixture {
	return &Fixture{t}
}

type Recorder struct {
	*Fixture
	ch            chan string
	emptyWaitTime time.Duration
}

func NewRecorder(t *testing.T) *Recorder {
	rec := &Recorder{
		Fixture:       NewFixture(t),
		ch:            make(chan string, 1000),
		emptyWaitTime: REC_EMPTY_WAIT_TIME_MS * time.Millisecond,
	}
	return rec
}

func (rec *Recorder) Rec(format string, args ...interface{}) {
	item := fmt.Sprintf(format, args...)
	rec.t.Log("REC: ", item)
	rec.ch <- item
}

func (rec *Recorder) SetEmptyWaitTime(duration time.Duration) {
	rec.emptyWaitTime = duration
}

func (rec *Recorder) VerifyEmpty() {
	// this may not always work but will help catch
	// errors at least in some cases
	timer := time.NewTimer(rec.emptyWaitTime)
	select {
	case <-timer.C:
		return
	case logItem := <-rec.ch:
		timer.Stop()
		rec.t.Fatalf("unexpected logs: %s", logItem)
	}
}

func (rec *Recorder) verify(sortLogs bool, msg string, logs []string) {
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
		if sortLogs {
			sort.Strings(logs)
			sort.Strings(actualLogs)
		}
		require.Equal(rec.t, logs, actualLogs, msg)
	}
}

func (rec *Recorder) Verify(logs ...string) {
	rec.verify(false, "rec logs", logs)
}

func (rec *Recorder) VerifyUnordered(logs ...string) {
	rec.verify(true, "rec logs (unordered)", logs)
}

func (rec *Recorder) SkipTill(logItem string) {
	timer := time.NewTimer(REC_EMPTY_WAIT_TIME_MS * time.Millisecond)
	for {
		select {
		case <-timer.C:
			rec.t.Fatalf("timed out waiting for log: %s", logItem)
			return
		case l := <-rec.ch:
			if l == logItem {
				timer.Stop()
				return
			}
		}
	}
}

// TestLog makes it possible to use log module with testing's
// logging functions, so that the logging output is only
// shown when the test fails. Note that a part at the end
// of output that is not newline-terminated is not displayed.
type TestLog struct {
	sync.Mutex
	buf      []byte
	acc      []byte
	t        *testing.T
	pristine bool
}

func NewTestLog(t *testing.T) *TestLog {
	buf := make([]byte, 0, 1024)
	return &TestLog{buf: buf, acc: buf[:0], t: t, pristine: true}
}

func (tl *TestLog) Write(p []byte) (n int, err error) {
	tl.Lock()
	defer tl.Unlock()
	tl.pristine = false
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

func (tl *TestLog) VerifyPristine(msg string) {
	tl.Lock()
	defer tl.Unlock()
	if !tl.pristine {
		tl.t.Fatal(msg)
	}
}

func (tl *TestLog) VerifyUsed(msg string) {
	tl.Lock()
	defer tl.Unlock()
	if tl.pristine {
		tl.t.Fatal(msg)
	}
	tl.pristine = true
}

var errorTestLog, warnTestLog *TestLog

// SetupTestLogging sets up the logging output in such way
// that it's only shown if the current test fails.
func SetupTestLogging(t *testing.T) {
	errorTestLog = NewTestLog(t)
	Error = log.New(errorTestLog, "ERROR: ", log.Lshortfile)
	warnTestLog = NewTestLog(t)
	Warn = log.New(warnTestLog, "WARNING: ", log.Lshortfile)
	Info = log.New(NewTestLog(t), "INFO: ", log.Lshortfile)
	Debug = log.New(NewTestLog(t), "DEBUG: ", log.Lshortfile)
}

func EnsureNoErrorsOrWarnings(t *testing.T) {
	errorTestLog.VerifyPristine("Errors detected")
	warnTestLog.VerifyPristine("Warnings detected")
}

func EnsureGotErrors(t *testing.T) {
	errorTestLog.VerifyUsed("No errors detected (but should be)")
}

func EnsureGotWarnings(t *testing.T) {
	warnTestLog.VerifyUsed("No warnings detected (but should be)")
}

// SetupTempDir creates a temporary directory to be used in tests and
// makes it the current directory. In case of an error, makes the test
// fail. Returns the path to the temporary directory and cleanup
// function that removes the directory and changes back to the directory
// that was current before SetupTempDir was called.
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

type Suite struct {
	suite.Suite
}

func (suite *Suite) SetupTest() {
	SetupTestLogging(suite.T())
}

func (suite *Suite) TearDown() {
	suite.EnsureNoErrorsOrWarnings()
}

func (suite *Suite) EnsureNoErrorsOrWarnings() {
	EnsureNoErrorsOrWarnings(suite.T())
}

func (suite *Suite) EnsureGotErrors() {
	EnsureGotErrors(suite.T())
}

func (suite *Suite) EnsureGotWarnings() {
	EnsureGotWarnings(suite.T())
}

// RunSuite is here just to avoid an extra import
// of testify's suite.Run
func RunSuite(t *testing.T, s suite.TestingSuite) {
	suite.Run(t, s)
}
