// Package time defines the clock abstraction for Karpo applications.
// Decoupling time from the system clock enables deterministic testing for expiration, timeouts, and temporal workflows.
package time

import (
	"time"
)

// Clock provides an abstraction over time queries and pauses.
type Clock interface {
	// Now returns the current time.
	Now() time.Time

	// Since returns the elapsed time since t.
	Since(t time.Time) time.Duration

	// Sleep pauses the current execution for at least duration d.
	Sleep(d time.Duration)
}
