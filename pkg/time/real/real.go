// Package real provides an implementation of Clock using the standard operating system clock.
package real

import (
	stdtime "time"

	"github.com/jhermoso/karpo-fw-go/pkg/time"
)

// RealClock delegates directly to the standard library time functions.
type RealClock struct{}

// New creates a new RealClock.
func New() time.Clock {
	return &RealClock{}
}

// Now returns the current local time from the OS.
func (c *RealClock) Now() stdtime.Time {
	return stdtime.Now()
}

// Since returns the elapsed time since t.
func (c *RealClock) Since(t stdtime.Time) stdtime.Duration {
	return stdtime.Since(t)
}

// Sleep pauses the current goroutine for at least duration d.
func (c *RealClock) Sleep(d stdtime.Duration) {
	stdtime.Sleep(d)
}
