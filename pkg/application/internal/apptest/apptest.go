// Package apptest provides the shared fixture of the application implementation tests:
// an in-memory store, a Widget repository, an Orchestrator recording into the outbox,
// and a Relay delivering to an in-process bus.
package apptest

import (
	"context"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/events"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	rt "github.com/jhermoso/karpo-fw-go/pkg/testing/repotest"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/testkit"
)

// Fixture wires the application implementations over in-memory adapters.
type Fixture struct {
	H     *testkit.Harness
	Repo  *memory.Repository[rt.WidgetID, *rt.Widget]
	Orch  *orchestration.Orchestrator[rt.WidgetID, *rt.Widget]
	Relay *outbox.Relay
}

// New creates a Fixture with the domain clock frozen at a fixed instant.
func New(t *testing.T) *Fixture {
	h := testkit.NewHarness(t, time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC))
	repo := memory.NewRepository[rt.WidgetID, *rt.Widget](h.Store, memory.WithClone((*rt.Widget).Clone))
	orch := orchestration.New[rt.WidgetID, *rt.Widget](repo, h.Store, orchestration.WithOutbox(outbox.NewRecorder(h.Outbox)))
	events.Register[rt.WidgetCreated](h.Registry)
	events.Register[rt.WidgetRenamed](h.Registry)
	return &Fixture{H: h, Repo: repo, Orch: orch, Relay: outbox.NewRelay(h.Outbox, h.Registry, h.Bus)}
}

// Create registers a new widget through the orchestrator.
func (f *Fixture) Create(t *testing.T, name string) *rt.Widget {
	t.Helper()
	w, err := rt.NewWidget(rt.NewWidgetID(), name, 100, true, nil, domain.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Orch.Create(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	return w
}

// Pending returns the pending outbox messages.
func (f *Fixture) Pending(t *testing.T) []application.OutboxMessage {
	t.Helper()
	msgs, err := f.H.Outbox.Pending(context.Background(), 100, 10)
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}
