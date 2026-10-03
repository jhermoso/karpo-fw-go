// Package testkit provides common testing harnesses, fakes and fixtures for Karpo applications.
package testkit

import (
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/events"
	"github.com/jhermoso/karpo-fw-go/pkg/events/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// Harness bundles common test fixtures: a fake clock (also installed as the domain clock),
// an in-process bus, an in-memory store (unit of work) with its outbox, and an event registry.
type Harness struct {
	Clock    *fake.FakeClock
	Bus      *inprocess.Bus
	Store    *memory.Store
	Outbox   *memory.Outbox
	Registry *events.Registry
}

// NewHarness creates a Harness frozen at initialTime. When t is not nil the domain clock is
// restored at the end of the test.
func NewHarness(t testing.TB, initialTime time.Time) *Harness {
	clock := fake.New(initialTime)
	restore := domain.SetClock(clock)
	if t != nil {
		t.Cleanup(restore)
	}
	store := memory.NewStore("testkit")
	return &Harness{
		Clock:    clock,
		Bus:      inprocess.New(),
		Store:    store,
		Outbox:   memory.NewOutbox(store),
		Registry: events.NewRegistry(),
	}
}
