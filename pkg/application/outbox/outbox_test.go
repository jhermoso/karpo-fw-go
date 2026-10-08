package outbox_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/internal/apptest"
	"github.com/jhermoso/karpo-fw-go/pkg/events"
	rt "github.com/jhermoso/karpo-fw-go/pkg/testing/repotest"
)

func TestOutboxRelay_DeliversDecodedEventsAtLeastOnce(t *testing.T) {
	f := apptest.New(t)
	var got []rt.WidgetRenamed
	fail := true
	events.Subscribe(f.H.Bus, func(ctx context.Context, e rt.WidgetRenamed) error {
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

	w := f.Create(t, "Alpha")
	if _, err := f.Orch.Update(context.Background(), w.ID(), func(_ context.Context, w *rt.Widget) error { return w.Rename("Beta") }); err != nil {
		t.Fatal(err)
	}

	n, err := f.Relay.RelayOnce(context.Background())
	if err != nil || n != 1 { // created delivered, renamed failed
		t.Fatalf("first pass: delivered %d, err %v", n, err)
	}
	if msgs := f.Pending(t); len(msgs) != 1 || msgs[0].Attempts != 1 || msgs[0].LastError != "transient" {
		t.Fatalf("failed message must stay pending with its attempt recorded: %+v", msgs)
	}
	if n, err = f.Relay.RelayOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("second pass: delivered %d, err %v", n, err)
	}
	if len(got) != 1 || got[0].From != "Alpha" || got[0].To != "Beta" || got[0].AggregateID != w.ID().String() {
		t.Fatalf("event not decoded properly: %+v", got)
	}
	if len(f.Pending(t)) != 0 {
		t.Fatal("outbox must be empty")
	}
}
