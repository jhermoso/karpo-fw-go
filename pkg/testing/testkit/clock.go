package testkit

import (
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// FixClock sets the domain clock to start for the rest of the test and restores it at cleanup.
// Unlike the frozen clock of NewHarness it keeps advancing with real time, so timestamps stay
// ordered (UUID v7, outbox, audit, sessions).
//
// Scenario tests whose dates are literals (an order on 2026-10-04, a settlement on 2026-10-05)
// use it so they do not depend on the day they run: an operation without an explicit date takes
// "today" from the domain clock, and with the real clock it drifts away from the literals (a
// delivery note numbered in next year's series, a settlement before the order was generated).
// Do not use it in tests that run in parallel: the domain clock is process-wide.
func FixClock(t testing.TB, start time.Time) {
	t.Helper()
	t.Cleanup(domain.SetClock(advancingClock{start: start, origin: time.Now()}))
}

type advancingClock struct{ start, origin time.Time }

func (c advancingClock) Now() time.Time { return c.start.Add(time.Since(c.origin)) }
