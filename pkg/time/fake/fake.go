// Package fake provides a controllable Clock implementation for deterministic testing.
package fake

import (
	"sync"
	stdtime "time"

	"github.com/jhermoso/karpo-fw-go/pkg/time"
)

// FakeClock is a thread-safe mock implementation of time.Clock.
type FakeClock struct {
	mu      sync.RWMutex
	current stdtime.Time
}

// New creates a new FakeClock initialized at the specified initial time.
func New(initial stdtime.Time) *FakeClock {
	return &FakeClock{
		current: initial,
	}
}

// Now returns the frozen or manually advanced time.
func (c *FakeClock) Now() stdtime.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// Since returns the difference between current mock time and t.
func (c *FakeClock) Since(t stdtime.Time) stdtime.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current.Sub(t)
}

// Sleep simulates a sleep by immediately advancing the clock by duration d.
func (c *FakeClock) Sleep(d stdtime.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(d)
}

// Advance shifts the mock time forward by duration d.
func (c *FakeClock) Advance(d stdtime.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(d)
}

// Set sets the mock time to a specific target time.
func (c *FakeClock) Set(target stdtime.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = target
}

var _ time.Clock = (*FakeClock)(nil)
