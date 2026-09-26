package domain

import (
	"sync/atomic"
	"time"
)

// Clock is the minimal time source the domain needs. pkg/time/real and pkg/time/fake satisfy it.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

var clock atomic.Value // holds clockHolder

type clockHolder struct{ c Clock }

func init() { clock.Store(clockHolder{systemClock{}}) }

// Now returns the current UTC time according to the domain clock.
// Aggregates use it to timestamp domain events so tests can make time deterministic.
func Now() time.Time { return clock.Load().(clockHolder).c.Now().UTC() }

// SetClock replaces the domain clock and returns a function that restores the previous one.
// Intended for composition roots and tests.
func SetClock(c Clock) (restore func()) {
	if c == nil {
		c = systemClock{}
	}
	prev := clock.Swap(clockHolder{c})
	return func() { clock.Store(prev) }
}
