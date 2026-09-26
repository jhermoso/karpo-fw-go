package application_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/events"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	rt "github.com/jhermoso/karpo-fw-go/pkg/testing/repotest"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/testkit"
)

type renameWidget struct {
	ID   rt.WidgetID
	Name string
	Key  string
}

func (c renameWidget) Validate() error {
	var v domain.Validation
	v.Require(!c.ID.IsZero(), "id", "required", "id is required")
	v.Require(c.Name != "", "name", "required", "name is required")
	return v.Err()
}

func (c renameWidget) IdempotencyKey() string { return c.Key }

type fixture struct {
	h     *testkit.Harness
	repo  *memory.Repository[rt.WidgetID, *rt.Widget]
	orch  *application.Orchestrator[rt.WidgetID, *rt.Widget]
	relay *application.OutboxRelay
}

func newFixture(t *testing.T) *fixture {
	h := testkit.NewHarness(t, time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC))
	repo := memory.NewRepository[rt.WidgetID, *rt.Widget](h.Store, memory.WithClone((*rt.Widget).Clone))
	orch := application.NewOrchestrator[rt.WidgetID, *rt.Widget](repo, h.Store, application.WithOutbox(application.NewOutbox(h.Outbox)))
	events.Register[rt.WidgetCreated](h.Registry)
	events.Register[rt.WidgetRenamed](h.Registry)
	return &fixture{h: h, repo: repo, orch: orch, relay: application.NewOutboxRelay(h.Outbox, h.Registry, h.Bus)}
}

func (f *fixture) create(t *testing.T, name string) *rt.Widget {
	t.Helper()
	w, err := rt.NewWidget(rt.NewWidgetID(), name, 100, true, nil, domain.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.orch.Create(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	return w
}

func (f *fixture) pending(t *testing.T) []application.OutboxMessage {
	t.Helper()
	msgs, err := f.h.Outbox.Pending(context.Background(), 100, 10)
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}

func TestOrchestrator_RecordsEventsInTheSameUnitOfWork(t *testing.T) {
	f := newFixture(t)
	ctx := application.WithCorrelationID(context.Background(), "corr-42")
	w, _ := rt.NewWidget(rt.NewWidgetID(), "Alpha", 100, true, nil, domain.Now())
	if err := f.orch.Create(ctx, w); err != nil {
		t.Fatal(err)
	}
	if len(w.PendingEvents()) != 0 || w.Version() != 1 {
		t.Fatalf("events must be cleared and version set: events=%d v=%d", len(w.PendingEvents()), w.Version())
	}

	renamed, err := f.orch.Update(ctx, w.ID(), func(_ context.Context, w *rt.Widget) error { return w.Rename("Beta") })
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name() != "Beta" || renamed.Version() != 2 {
		t.Fatalf("unexpected state %s v%d", renamed.Name(), renamed.Version())
	}

	msgs := f.pending(t)
	if len(msgs) != 2 || msgs[0].EventType != "repotest.widget_created" || msgs[1].EventType != "repotest.widget_renamed" {
		t.Fatalf("unexpected outbox: %+v", msgs)
	}
	if msgs[1].AggregateID != w.ID().String() || msgs[1].AggregateVersion != 2 || msgs[1].CorrelationID != "corr-42" {
		t.Fatalf("outbox metadata not propagated: %+v", msgs[1])
	}
}

func TestOrchestrator_FailedBehaviourPersistsNothing(t *testing.T) {
	f := newFixture(t)
	w := f.create(t, "Alpha")
	before := len(f.pending(t))

	_, err := f.orch.Update(context.Background(), w.ID(), func(_ context.Context, w *rt.Widget) error {
		if err := w.Rename("Changed"); err != nil {
			return err
		}
		return domain.Violation("widget.frozen", "widget is frozen")
	})
	if !errors.Is(err, domain.ErrRuleViolation) {
		t.Fatalf("expected rule violation, got %v", err)
	}
	got, _ := f.repo.Get(context.Background(), w.ID())
	if got.Name() != "Alpha" || len(f.pending(t)) != before {
		t.Fatalf("failed behaviour leaked state or events: %s, outbox %d", got.Name(), len(f.pending(t)))
	}
}

func TestOrchestrator_ExecuteReturnsTypedResult(t *testing.T) {
	f := newFixture(t)
	w := f.create(t, "Alpha")
	old, err := application.Execute(context.Background(), f.orch, w.ID(), func(_ context.Context, w *rt.Widget) (string, error) {
		prev := w.Name()
		return prev, w.Rename("Gamma")
	})
	if err != nil || old != "Alpha" {
		t.Fatalf("expected typed result 'Alpha', got %q (%v)", old, err)
	}
}

func TestOrchestrator_DeleteAndNotFound(t *testing.T) {
	f := newFixture(t)
	w := f.create(t, "Alpha")
	if err := f.orch.Delete(context.Background(), w.ID(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.orch.Update(context.Background(), w.ID(), func(context.Context, *rt.Widget) error { return nil }); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestOrchestrator_PublishesAfterCommit(t *testing.T) {
	h := testkit.NewHarness(t, time.Now())
	repo := memory.NewRepository[rt.WidgetID, *rt.Widget](h.Store)
	orch := application.NewOrchestrator[rt.WidgetID, *rt.Widget](repo, h.Store, application.WithPublisher(h.Bus))

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
	failing.Bus.Subscribe(events.Wildcard, events.HandlerFunc(func(context.Context, events.Event) error { return errors.New("down") }))
	orch = application.NewOrchestrator[rt.WidgetID, *rt.Widget](memory.NewRepository[rt.WidgetID, *rt.Widget](failing.Store), failing.Store, application.WithPublisher(failing.Bus))
	w2, _ := rt.NewWidget(rt.NewWidgetID(), "Beta", 1, true, nil, time.Now())
	if err := orch.Create(context.Background(), w2); !errors.Is(err, application.ErrEventsNotPublished) {
		t.Fatalf("expected ErrEventsNotPublished, got %v", err)
	}
	if _, err := orch.Repository().Get(context.Background(), w2.ID()); err != nil {
		t.Fatalf("state must stay committed: %v", err)
	}
}

func TestOutboxRelay_DeliversDecodedEventsAtLeastOnce(t *testing.T) {
	f := newFixture(t)
	var got []rt.WidgetRenamed
	fail := true
	events.Subscribe(f.h.Bus, func(ctx context.Context, e rt.WidgetRenamed) error {
		if fail {
			fail = false
			return errors.New("transient")
		}
		if application.CausationID(ctx) == "" {
			t.Error("relay must set the causation id")
		}
		got = append(got, e)
		return nil
	})

	w := f.create(t, "Alpha")
	if _, err := f.orch.Update(context.Background(), w.ID(), func(_ context.Context, w *rt.Widget) error { return w.Rename("Beta") }); err != nil {
		t.Fatal(err)
	}

	n, err := f.relay.RelayOnce(context.Background())
	if err != nil || n != 1 { // created delivered, renamed failed
		t.Fatalf("first pass: delivered %d, err %v", n, err)
	}
	if msgs := f.pending(t); len(msgs) != 1 || msgs[0].Attempts != 1 || msgs[0].LastError != "transient" {
		t.Fatalf("failed message must stay pending with its attempt recorded: %+v", msgs)
	}
	if n, err = f.relay.RelayOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("second pass: delivered %d, err %v", n, err)
	}
	if len(got) != 1 || got[0].From != "Alpha" || got[0].To != "Beta" || got[0].AggregateID != w.ID().String() {
		t.Fatalf("event not decoded properly: %+v", got)
	}
	if len(f.pending(t)) != 0 {
		t.Fatal("outbox must be empty")
	}
}

func TestMiddleware_ChainOrderValidationTransactionAndIdempotency(t *testing.T) {
	f := newFixture(t)
	w := f.create(t, "Alpha")

	var trace []string
	tracer := func(name string) application.Middleware[renameWidget, string] {
		return func(next application.Handler[renameWidget, string]) application.Handler[renameWidget, string] {
			return application.HandlerFunc[renameWidget, string](func(ctx context.Context, c renameWidget) (string, error) {
				trace = append(trace, name+">")
				out, err := next.Handle(ctx, c)
				trace = append(trace, "<"+name)
				return out, err
			})
		}
	}
	calls := 0
	core := application.HandlerFunc[renameWidget, string](func(ctx context.Context, c renameWidget) (string, error) {
		calls++
		updated, err := f.orch.Update(ctx, c.ID, func(_ context.Context, w *rt.Widget) error { return w.Rename(c.Name) })
		if err != nil {
			return "", err
		}
		return updated.Name(), nil
	})
	h := application.Chain[renameWidget, string](core,
		tracer("outer"),
		application.Idempotent[renameWidget, string](memory.NewIdempotencyStore(), time.Hour),
		application.Validating[renameWidget, string](),
		application.RetryOnConflict[renameWidget, string](3, time.Millisecond),
		application.Transactional[renameWidget, string](f.h.Store),
		tracer("inner"),
	)

	if _, err := h.Handle(context.Background(), renameWidget{}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if calls != 0 {
		t.Fatal("invalid command must not reach the handler")
	}

	out, err := h.Handle(context.Background(), renameWidget{ID: w.ID(), Name: "Beta", Key: "k1"})
	if err != nil || out != "Beta" {
		t.Fatalf("unexpected result %q %v", out, err)
	}
	out, err = h.Handle(context.Background(), renameWidget{ID: w.ID(), Name: "Beta", Key: "k1"})
	if err != nil || out != "Beta" || calls != 1 {
		t.Fatalf("idempotent replay must return the stored result without executing: calls=%d out=%q err=%v", calls, out, err)
	}
	want := []string{"outer>", "<outer", "outer>", "inner>", "<inner", "<outer", "outer>", "<outer"}
	if !slices.Equal(trace, want) {
		t.Fatalf("middleware order\nwant %v\ngot  %v", want, trace)
	}
}

func TestRetryOnConflict(t *testing.T) {
	attempts := 0
	h := application.Chain[int, int](
		application.HandlerFunc[int, int](func(context.Context, int) (int, error) {
			attempts++
			if attempts < 3 {
				return 0, domain.Conflict("x", domain.NewUUID(), 1, "stale version")
			}
			return 7, nil
		}),
		application.RetryOnConflict[int, int](3, 0),
	)
	if v, err := h.Handle(context.Background(), 0); err != nil || v != 7 || attempts != 3 {
		t.Fatalf("v=%d err=%v attempts=%d", v, err, attempts)
	}
}

type mod struct {
	name string
	deps []string
	log  *[]string
	fail bool
}

func (m mod) Name() string           { return m.name }
func (m mod) Dependencies() []string { return m.deps }
func (m mod) Start(context.Context) error {
	if m.fail {
		return errors.New("boom")
	}
	*m.log = append(*m.log, "start "+m.name)
	return nil
}
func (m mod) Stop(context.Context) error { *m.log = append(*m.log, "stop "+m.name); return nil }

func TestHost_OrdersModulesByDependencies(t *testing.T) {
	var log []string
	host, err := application.NewHost(
		mod{name: "ErpDetail", deps: []string{"ErpKernel"}, log: &log},
		mod{name: "Fw", log: &log},
		mod{name: "ErpKernel", deps: []string{"Fw"}, log: &log},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := host.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"start Fw", "start ErpKernel", "start ErpDetail", "stop ErpDetail", "stop ErpKernel", "stop Fw"}
	if !slices.Equal(log, want) {
		t.Fatalf("want %v got %v", want, log)
	}

	if _, err := application.NewHost(mod{name: "A", deps: []string{"B"}}, mod{name: "B", deps: []string{"A"}}); err == nil {
		t.Fatal("expected cycle error")
	}
	if _, err := application.NewHost(mod{name: "A", deps: []string{"Missing"}}); err == nil {
		t.Fatal("expected unknown dependency error")
	}

	log = nil
	host, _ = application.NewHost(mod{name: "Fw", log: &log}, mod{name: "Bad", deps: []string{"Fw"}, fail: true, log: &log})
	if err := host.Start(context.Background()); err == nil {
		t.Fatal("expected start failure")
	}
	if !slices.Equal(log, []string{"start Fw", "stop Fw"}) {
		t.Fatalf("started modules must be stopped on failure: %v", log)
	}
}
