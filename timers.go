package wbgo

import (
	"time"
)

// Timer is fakeable timer interface
type Timer interface {
	// GetChannel retrieves a channel that signals timer expiration
	GetChannel() <-chan time.Time

	// Stop stops the timer
	Stop()
}

// RTimer is reusable Timer with Reset method
type RTimer interface {
	// embed generic Timer here
	Timer

	// Reset changes the timer to expire after new duration d
	Reset(d time.Duration)
}

// RealTicker incapsulates a real time.Ticker
type RealTicker struct {
	innerTicker *time.Ticker
}

func NewRealTicker(d time.Duration) *RealTicker {
	return &RealTicker{time.NewTicker(d)}
}

func (ticker *RealTicker) GetChannel() <-chan time.Time {
	if ticker.innerTicker == nil {
		panic("trying to get channel from a stopped ticker")
	}
	return ticker.innerTicker.C
}

func (ticker *RealTicker) Stop() {
	if ticker.innerTicker != nil {
		ticker.innerTicker.Stop()
		ticker.innerTicker = nil
	}
}

// RealTimer incapsulates a real time.Ticker
type RealTimer struct {
	innerTimer *time.Timer
}

func NewRealTimer(d time.Duration) *RealTimer {
	return &RealTimer{time.NewTimer(d)}
}

func (timer *RealTimer) GetChannel() <-chan time.Time {
	if timer.innerTimer == nil {
		panic("trying to get channel from a stopped timer")
	}
	return timer.innerTimer.C
}

func (timer *RealTimer) Stop() {
	if timer.innerTimer != nil {
		timer.innerTimer.Stop()
		timer.innerTimer = nil
	}
}

// RealRTimer incapsulates a real time.Timer with Reset() method
type RealRTimer struct {
	RealTimer
}

// Reset resets an existing timer or creates a new one with given duration
func (timer *RealRTimer) Reset(d time.Duration) {
	if timer.innerTimer == nil {
		timer.innerTimer = time.NewTimer(d)
	} else {
		timer.innerTimer.Reset(d)
	}
}

func NewRealRTimer(d time.Duration) *RealRTimer {
	return &RealRTimer{RealTimer{time.NewTimer(d)}}
}
