package orchestration_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/internal/apptest"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/events"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	rt "github.com/jhermoso/karpo-fw-go/pkg/testing/repotest"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/testkit"
)

func TestOrchestrator_RecordsEventsInTheSameUnitOfWork(t *testing.T) {
	f := apptest.New(t)
	ctx := application.WithCorrelationID(context.Background(), "corr-42")
	w, _ := rt.NewWidget(rt.NewWidgetID(), "Alpha", 100, true, nil, domain.Now())
	if err := f.Orch.Create(ctx, w); err != nil {
		t.Fatal(err)
	}
	if len(w.PendingEvents()) != 0 || w.Version() != 1 {
		t.Fatalf("events must be cleared and version set: events=%d v=%d", len(w.PendingEvents()), w.Version())
	}

	renamed, err := f.Orch.Update(ctx, w.ID(), func(_ context.Context, w *rt.Widget) error { return w.Rename("Beta") })
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name() != "Beta" || renamed.Version() != 2 {
		t.Fatalf("unexpected state %s v%d", renamed.Name(), renamed.Version())
	}

	msgs := f.Pending(t)
	if len(msgs) != 2 || msgs[0].EventType != "repotest.widget_created" || msgs[1].EventType != "repotest.widget_renamed" {
		t.Fatalf("unexpected outbox: %+v", msgs)
	}
	if msgs[1].AggregateID != w.ID().String() || msgs[1].AggregateVersion != 2 || msgs[1].CorrelationID != "corr-42" {
		t.Fatalf("outbox metadata not propagated: %+v", msgs[1])
	}
}

func TestOrchestrator_FailedBehaviourPersistsNothing(t *testing.T) {
	f := apptest.New(t)
	w := f.Create(t, "Alpha")
	before := len(f.Pending(t))

	_, err := f.Orch.Update(context.Background(), w.ID(), func(_ context.Context, w *rt.Widget) error {
		if err := w.Rename("Changed"); err != nil {
			return err
		}
		return domain.Violation("widget.frozen", "widget is frozen")
	})
	if !errors.Is(err, domain.ErrRuleViolation) {
		t.Fatalf("expected rule violation, got %v", err)
	}
	got, _ := f.Repo.Get(context.Background(), w.ID())
	if got.Name() != "Alpha" || len(f.Pending(t)) != before {
		t.Fatalf("failed behaviour leaked state or events: %s, outbox %d", got.Name(), len(f.Pending(t)))
	}
}

func TestOrchestrator_ExecuteReturnsTypedResult(t *testing.T) {
	f := apptest.New(t)
	w := f.Create(t, "Alpha")
	old, err := orchestration.Execute(context.Background(), f.Orch, w.ID(), func(_ context.Context, w *rt.Widget) (string, error) {
		prev := w.Name()
		return prev, w.Rename("Gamma")
	})
	if err != nil || old != "Alpha" {
		t.Fatalf("expected typed result 'Alpha', got %q (%v)", old, err)
	}
}

func TestOrchestrator_DeleteAndNotFound(t *testing.T) {
	f := apptest.New(t)
	w := f.Create(t, "Alpha")
	if err := f.Orch.Delete(context.Background(), w.ID(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Orch.Update(context.Background(), w.ID(), func(context.Context, *rt.Widget) error { return nil }); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestOrchestrator_PublishesAfterCommit(t *testing.T) {
	h := testkit.NewHarness(t, time.Now())
	repo := memory.NewRepository[rt.WidgetID, *rt.Widget](h.Store)
	orch := orchestration.New[rt.WidgetID, *rt.Widget](repo, h.Store, orchestration.WithPublisher(h.Bus))

	var seen []string
	events.Subscribe(h.Bus, func(_ context.Context, e rt.WidgetCreated) error {
		seen = append(seen, e.Name)
		return nil
	})
	w, _ := rt.NewWidget(rt.NewWidgetID(), "Alpha", 1, true, nil, time.Now())
	if err := orch.Create(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(seen, []string{"Alpha"}) {
		t.Fatalf("expected post-commit publication, got %v", seen)
	}

	failing := testkit.NewHarness(t, time.Now())
	failing.Bus.Subscribe(events.Wildcard, application.EventHandlerFunc(func(context.Context, events.Event) error { return errors.New("down") }))
	orch = orchestration.New[rt.WidgetID, *rt.Widget](memory.NewRepository[rt.WidgetID, *rt.Widget](failing.Store), failing.Store, orchestration.WithPublisher(failing.Bus))
	w2, _ := rt.NewWidget(rt.NewWidgetID(), "Beta", 1, true, nil, time.Now())
	if err := orch.Create(context.Background(), w2); !errors.Is(err, orchestration.ErrEventsNotPublished) {
		t.Fatalf("expected ErrEventsNotPublished, got %v", err)
	}
	if _, err := orch.Repository().Get(context.Background(), w2.ID()); err != nil {
		t.Fatalf("state must stay committed: %v", err)
	}
}
