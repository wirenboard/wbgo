package wbgo

import (
	"fmt"
	"time"
	"testing"
	"github.com/stretchr/testify/assert"
)

const (
	WAIT_INTERVAL_MS = 10
	WAIT_COUNT = 300
)

func WaitFor(t *testing.T, pred func() bool) {
	for n := 0; !pred() && n < WAIT_COUNT; n++ {
		time.Sleep(WAIT_INTERVAL_MS * time.Millisecond)
	}
}

type Recorder struct {
	t *testing.T
	logs []string
	ch chan struct{}
}

func (rec *Recorder) InitRecorder(t *testing.T) {
	rec.t = t
	rec.ch = make(chan struct{}, 1000)
	rec.Reset()
}

func (rec *Recorder) Rec(format string, args... interface{}) {
	item := fmt.Sprintf(format, args...)
	rec.t.Log("REC: ", item)
	rec.logs = append(rec.logs, item)
	rec.ch <- struct{}{}
}

func (rec *Recorder) Verify(logs... string) {
	if logs == nil {
		assert.Equal(rec.t, 0, len(rec.logs), "rec log count")
	} else {
		for _ = range logs {
			<- rec.ch
		}
		assert.Equal(rec.t, logs, rec.logs, "rec logs")
	}
	rec.Reset()
}

func (rec *Recorder) Reset() {
	rec.logs = make([]string, 0, 1000)
}

func (rec *Recorder) T() *testing.T {
	return rec.t
}
