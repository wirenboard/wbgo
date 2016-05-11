package testutils

import (
	"fmt"
	"github.com/contactless/wbgo"
	"github.com/stretchr/objx"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"io/ioutil"
	"log"
	"os"
	"regexp"
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
	require.FailNow(t, "WaitFor() failed")
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

func (f *Fixture) Ckf(msg string, err error) {
	// named Ckf() to avoid name conflicts in suites
	if err != nil {
		require.FailNow(f.t, msg, "%s", err)
	}
}

type RecMatcherFunc func(item string) bool

type RecMatcher struct {
	text string
	fn   RecMatcherFunc
}

func NewRecMatcher(text string, fn RecMatcherFunc) *RecMatcher {
	return &RecMatcher{text, fn}
}

type RegexpCaptureHandler func(submatches []string) bool

// RegexpCaptureMatcherWithCustomText returns a matcher that matches
// the specified regexp against the item being verified, then invokes
// handler with submatches slice as an argument. Succeeds only in
// case handler returns true. The speicified text is used in
// mismatch logs.
func RegexpCaptureMatcherWithCustomText(itemRx string, text string, handler RegexpCaptureHandler) *RecMatcher {
	rx := regexp.MustCompile(itemRx)
	return NewRecMatcher(
		itemRx,
		func(item string) bool {
			m := rx.FindStringSubmatch(item)
			if m == nil {
				wbgo.Error.Printf(
					"RegexpCaptureMatcher: regexp mismatch: %s against %s",
					item, rx)
				return false
			}
			return handler(m)
		})
}

// RegexpCaptureMatcher is the same as RegexpCaptureMatcherWithCustomText
// but uses itemRx as mismatch text
func RegexpCaptureMatcher(itemRx string, handler RegexpCaptureHandler) *RecMatcher {
	return RegexpCaptureMatcherWithCustomText(itemRx, itemRx, handler)
}

// JSONRecMatcher returns a matcher that matches a first
// group captured by regular expression rx against JSON
// value. If rx has no groups, the whole text matched
// by rx is used
func JSONRecMatcher(value objx.Map, itemRx string) *RecMatcher {
	valStr := value.MustJSON()
	return RegexpCaptureMatcherWithCustomText(
		itemRx, valStr,
		func(m []string) bool {
			var text string
			if len(m) > 1 {
				text = m[1]
			} else {
				text = m[0]
			}
			actualValue, err := objx.FromJSON(text)
			if err != nil {
				wbgo.Error.Printf("JSONRecMatcher: failed to convert value to JSON: %s", text)
				return false
			}
			actualValueStr := actualValue.MustJSON() // fix key order
			if valStr != actualValueStr {
				wbgo.Error.Printf(
					"JSON value mismatch: %s (expected) != %s (actual)",
					valStr, actualValueStr)
				return false
			}
			return true
		})
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
		require.FailNow(rec.t, "unexpected logs", "%s", logItem)
	}
}

func (rec *Recorder) verify(sortLogs bool, msg string, logs []interface{}) {
	if logs == nil {
		rec.VerifyEmpty()
	} else {
		actualLogs := make([]interface{}, 0, len(logs))
		logs = logs[:]
		for n, expectedItem := range logs {
			timer := time.NewTimer(REC_ITEM_TIMEOUT_MS * time.Millisecond)
			select {
			case <-timer.C:
				require.FailNow(rec.t, "timed out waiting for log item",
					"%s", expectedItem)
			case logItem := <-rec.ch:
				timer.Stop()
				// If a regular expression is specified and it
				// matches, replace expected log item with
				// actual log item. If it doesn't match, replace
				// it with regular expression source text
				if n < len(logs) {
					switch logs[n].(type) {
					case *regexp.Regexp:
						rx := expectedItem.(*regexp.Regexp)
						if rx.FindStringIndex(logItem) != nil {
							logs[n] = logItem
						} else {
							logs[n] = rx.String()
						}
					case *RecMatcher:
						matcher := expectedItem.(*RecMatcher)
						if matcher.fn(logItem) {
							logs[n] = logItem
						} else {
							logs[n] = matcher.text
						}
					}
				}
				actualLogs = append(actualLogs, logItem)
			}
		}
		if sortLogs {
			// FIXME: this will fail on regexps
			strLogs := make([]string, len(logs))
			for n, log := range logs {
				strLogs[n] = log.(string)
			}
			strActualLogs := make([]string, len(actualLogs))
			for n, log := range actualLogs {
				strActualLogs[n] = log.(string)
			}
			sort.Strings(strLogs)
			sort.Strings(strActualLogs)
			require.Equal(rec.t, strLogs, strActualLogs, msg)
		} else {
			require.Equal(rec.t, logs, actualLogs, msg)
		}
	}
}

func (rec *Recorder) Verify(logs ...interface{}) {
	rec.verify(false, "rec logs", logs)
}

func (rec *Recorder) VerifyUnordered(logs ...interface{}) {
	rec.verify(true, "rec logs (unordered)", logs)
}

func (rec *Recorder) SkipTill(logItem string) {
	timer := time.NewTimer(REC_EMPTY_WAIT_TIME_MS * time.Millisecond)
	for {
		select {
		case <-timer.C:
			require.FailNow(rec.t, "timed out waiting for log", "%s", logItem)
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

func (tl *TestLog) WaitForMessages() {
	WaitFor(tl.t, func() (r bool) {
		tl.Lock()
		defer tl.Unlock()
		r = !tl.pristine
		tl.pristine = true
		return
	})
}

var errorTestLog, warnTestLog *TestLog

// SetupTestLogging sets up the logging output in such way
// that it's only shown if the current test fails.
func SetupTestLogging(t *testing.T) {
	errorTestLog = NewTestLog(t)
	wbgo.Error = log.New(errorTestLog, "ERROR: ", log.Lshortfile)
	warnTestLog = NewTestLog(t)
	wbgo.Warn = log.New(warnTestLog, "WARNING: ", log.Lshortfile)
	wbgo.Info = log.New(NewTestLog(t), "INFO: ", log.Lshortfile)
	// keep=true to make SetDebuggingEnabled() keep Debug
	wbgo.SetDebugLogger(log.New(NewTestLog(t), "DEBUG: ", log.Lshortfile), true)
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

func WaitForErrors(t *testing.T) {
	errorTestLog.WaitForMessages()
}

func WaitForWarnings(t *testing.T) {
	warnTestLog.WaitForMessages()
}

// SetupTempDir creates a temporary directory to be used in tests and
// makes it the current directory. In case of an error, makes the test
// fail. Returns the path to the temporary directory and cleanup
// function that removes the directory and changes back to the directory
// that was current before SetupTempDir was called.
func SetupTempDir(t *testing.T) (string, func()) {
	wd, err := os.Getwd()
	if err != nil {
		require.FailNow(t, "couldn't get the current directory")
		return "", nil // never reached
	}

	dir, err := ioutil.TempDir(os.TempDir(), "wbgotest")
	if err != nil {
		require.FailNow(t, "couldn't create temporary directory")
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

func (suite *Suite) TearDownTest() {
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

func (suite *Suite) WaitForErrors() {
	WaitForErrors(suite.T())
}

func (suite *Suite) WaitForWarnings() {
	WaitForWarnings(suite.T())
}

func (suite *Suite) WaitFor(pred func() bool) {
	WaitFor(suite.T(), pred)
}

func (suite *Suite) Ck(msg string, err error) {
	if err != nil {
		suite.Require().Fail(msg, "%s", err)
	}
}

// RunSuites runs the specified test suites
func RunSuites(t *testing.T, suites ...suite.TestingSuite) {
	for _, s := range suites {
		suite.Run(t, s)
	}
}
