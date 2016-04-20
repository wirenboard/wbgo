package testutils

import (
	"github.com/contactless/wbgo"
	"github.com/stretchr/testify/assert"
	"sync"
	"testing"
	"time"
)

type FakeTimerFixture struct {
	*Fixture
	rec         *Recorder
	nextTimerId int
	timers      map[int]*fakeTimer
	currentTime time.Time
}

// TBD: use Setup instead
func NewFakeTimerFixture(t *testing.T, rec *Recorder) *FakeTimerFixture {
	return &FakeTimerFixture{NewFixture(t), rec, 1, make(map[int]*fakeTimer), testStartTime}
}

func (fixture *FakeTimerFixture) ResetTimerIndex() {
	fixture.nextTimerId = 1
}

func (fixture *FakeTimerFixture) NewFakeTimerOrTicker(id int, d time.Duration, periodic bool) wbgo.Timer {
	if id < 0 {
		id = fixture.nextTimerId
		fixture.nextTimerId++
	}
	timer := &fakeTimer{
		t:        fixture.t,
		id:       id,
		c:        make(chan time.Time),
		d:        d,
		periodic: periodic,
		active:   true,
		rec:      fixture.rec,
	}
	fixture.timers[id] = timer
	what := "timer"
	if periodic {
		what = "ticker"
	}
	timer.rec.Rec("new fake %s: %d, %d", what, id, d/time.Millisecond)
	return timer
}

func (fixture *FakeTimerFixture) NewFakeTimer(d time.Duration) wbgo.Timer {
	return fixture.NewFakeTimerOrTicker(-1, d, false)
}

func (fixture *FakeTimerFixture) NewFakeTicker(d time.Duration) wbgo.Timer {
	return fixture.NewFakeTimerOrTicker(-1, d, true)
}

func (fixture *FakeTimerFixture) CurrentTime() time.Time {
	return fixture.currentTime
}

func (fixture *FakeTimerFixture) AdvanceTime(d time.Duration) time.Time {
	fixture.currentTime = fixture.currentTime.Add(d)
	return fixture.currentTime
}

func (fixture *FakeTimerFixture) FireTimer(id int, ts time.Time) {
	if timer, found := fixture.timers[id]; !found {
		fixture.t.Fatalf("FakeTimerFixture.FireTimer(): bad timer id: %d", id)
	} else {
		timer.fire(ts)
	}
}

type fakeTimer struct {
	sync.Mutex
	t        *testing.T
	id       int
	c        chan time.Time
	d        time.Duration
	periodic bool
	active   bool
	rec      *Recorder
}

func (timer *fakeTimer) GetChannel() <-chan time.Time {
	return timer.c
}

func (timer *fakeTimer) fire(t time.Time) {
	timer.Lock()
	defer timer.Unlock()
	timer.rec.Rec("timer.fire(): %d", timer.id)
	assert.True(timer.t, timer.active)
	timer.c <- t
	if !timer.periodic {
		timer.active = false
	}
}

func (timer *fakeTimer) Stop() {
	// note that we don't close timer here,
	// mimicking the behavior of real timers and tickers
	timer.Lock()
	defer timer.Unlock()
	timer.active = false
	timer.rec.Rec("timer.Stop(): %d", timer.id)
}
