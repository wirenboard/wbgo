package wbgo

import (
	"fmt"
	"sort"
	"time"
	"testing"
	"github.com/stretchr/testify/require"
)

const (
	WAIT_INTERVAL_MS = 10
	WAIT_COUNT = 300
	REC_EMPTY_WAIT_TIME_MS = 50
	REC_SKIP_TIME_MS = 3000
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
	t *testing.T
	ch chan string
}

func (rec *Recorder) InitRecorder(t *testing.T) {
	rec.t = t
	rec.ch = make(chan string, 1000)
}

func (rec *Recorder) Rec(format string, args... interface{}) {
	item := fmt.Sprintf(format, args...)
	rec.t.Log("REC: ", item)
	rec.ch <- item
}

func (rec *Recorder) VerifyEmpty() {
	// this may not always work but will help catch
	// errors at least in some cases
	timer := time.NewTimer(REC_EMPTY_WAIT_TIME_MS * time.Millisecond)
	select {
	case <- timer.C:
		return
	case log := <- rec.ch:
		timer.Stop()
		rec.t.Fatalf("unexpected logs: %s", log)
	}
}

func (rec *Recorder) Verify(logs... string) {
	if logs == nil {
		rec.VerifyEmpty()
	} else {
		actualLogs := make([]string, 0, len(logs))
		for _ = range logs {
			actualLogs = append(actualLogs, <-rec.ch)
		}
		require.Equal(rec.t, logs, actualLogs, "rec logs")
	}
}

func (rec *Recorder) VerifyUnordered(logs... string) {
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

func (rec *Recorder) SkipTill(log string) {
	timer := time.NewTimer(REC_EMPTY_WAIT_TIME_MS * time.Millisecond)
	for {
		select {
		case <- timer.C:
			rec.t.Fatalf("failed waiting for log: %s", log)
			return
		case l := <- rec.ch:
			if l == log {
				timer.Stop()
				return
			}
		}
	}
}

func (rec *Recorder) T() *testing.T {
	return rec.t
}
