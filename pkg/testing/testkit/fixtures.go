// Package testkit provides common testing harnesses, mocks, and fixtures for Karpo applications.
package testkit

import (
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/events/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// Harness bundles common test fixtures (fake clock, in-process bus, and memory unit of work).
type Harness struct {
	Clock *fake.FakeClock
	Bus   *inprocess.InProcessBus
	UoW   *memory.MemoryUnitOfWork
}

// NewHarness creates a new test Harness initialized with fixed time.
func NewHarness(initialTime time.Time) *Harness {
	return &Harness{
		Clock: fake.New(initialTime),
		Bus:   inprocess.New(),
		UoW:   &memory.MemoryUnitOfWork{},
	}
}
