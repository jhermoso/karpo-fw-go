package outbox_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

func failingRelay(t *testing.T, opts ...outbox.RelayOption) *outbox.Relay {
	t.Helper()
	store := memory.NewOutbox(memory.NewStore("test"))
	err := store.Append(context.Background(), application.OutboxMessage{ID: "m-1", EventType: "widget.renamed",
		Payload: []byte(`{}`), OccurredAt: time.Now().UTC(), CorrelationID: "flow-9", CausationID: "cmd-3"})
	if err != nil {
		t.Fatal(err)
	}
	return outbox.NewForwarder(store, func(context.Context, application.OutboxMessage) error {
		return errors.New("broker unreachable")
	}, opts...)
}

// captureDefault redirects the logger of the process for the duration of the test.
func captureDefault(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func TestRelay_IsNotSilentByDefault(t *testing.T) {
	buf := captureDefault(t)

	if n, err := failingRelay(t).RelayOnce(context.Background()); n != 0 || err != nil {
		t.Fatalf("RelayOnce: %d, %v", n, err)
	}

	out := buf.String()
	for _, want := range []string{"outbox delivery failed", "message_id=m-1", "event_type=widget.renamed",
		"correlation_id=flow-9", "causation_id=cmd-3", "attempt=1", "broker unreachable"} {
		if !strings.Contains(out, want) {
			t.Errorf("the default logger must report the failure with %q:\n%s", want, out)
		}
	}
}

func TestRelay_CanBeSilencedOnPurpose(t *testing.T) {
	buf := captureDefault(t)

	if _, err := failingRelay(t, outbox.WithRelayLogger(log.Discard())).RelayOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("log.Discard must silence the relay:\n%s", buf.String())
	}
}
