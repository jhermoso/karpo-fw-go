package sqlconformance

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/mysql"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/oracle"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlserver"
)

// OutboxDDL returns the outbox DDL of a dialect name.
func OutboxDDL(dialect, table string) []string {
	switch dialect {
	case "sqlite":
		return sqlite.OutboxDDL(table)
	case "postgres":
		return postgres.OutboxDDL(table)
	case "sqlserver":
		return sqlserver.OutboxDDL(table)
	case "oracle":
		return oracle.OutboxDDL(table)
	case "mysql":
		return mysql.OutboxDDL(table)
	}
	return nil
}

// OutboxTable is the table RunOutbox and ResetOutbox use.
const OutboxTable = "conformance_outbox"

// ResetOutbox drops (ignoring errors) and recreates the conformance outbox and returns it.
func ResetOutbox(t *testing.T, db *sqlrepo.DB) *sqlrepo.Outbox {
	t.Helper()
	ctx := context.Background()
	_, _ = db.ExecContext(ctx, "DROP TABLE "+OutboxTable)
	for _, s := range OutboxDDL(db.Dialect().Name(), OutboxTable) {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("outbox ddl: %v\n%s", err, s)
		}
	}
	box, err := sqlrepo.NewOutbox(db, OutboxTable)
	if err != nil {
		t.Fatal(err)
	}
	return box
}

// OutboxMessages builds n messages one millisecond apart, about the given subjects in turn.
func OutboxMessages(n int, subjects ...string) []application.OutboxMessage {
	base := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	out := make([]application.OutboxMessage, n)
	for i := range n {
		out[i] = application.OutboxMessage{
			ID: fmt.Sprintf("00000000-0000-7000-8000-%012d", i+1), EventType: "conformance.thing-happened.v1",
			AggregateType: "thing", AggregateID: subjects[i%len(subjects)], AggregateVersion: int64(i + 1),
			Payload: []byte(fmt.Sprintf(`{"seq":%d}`, i+1)), OccurredAt: base.Add(time.Duration(i) * time.Millisecond),
			CorrelationID: "corr", CausationID: "cause",
		}
	}
	return out
}

// RunOutbox checks the SQL outbox on db: the store itself, and the claims that let several
// relays share it (application.OutboxClaimer). The checks with two relays at once need row
// locks, so they are skipped on SQLite, which has a single writer.
func RunOutbox(t *testing.T, db *sqlrepo.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	box := ResetOutbox(t, db)
	msgs := OutboxMessages(5, "a", "b")
	ids := func(ms []application.OutboxMessage) []string {
		out := make([]string, len(ms))
		for i, m := range ms {
			out[i] = m.ID
		}
		return out
	}
	want := func(what string, got []application.OutboxMessage, err error, ms ...application.OutboxMessage) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if !slices.Equal(ids(got), ids(ms)) {
			t.Fatalf("%s: got %v, want %v", what, ids(got), ids(ms))
		}
	}

	if err := box.Append(ctx, msgs...); err != nil {
		t.Fatal(err)
	}
	pending, err := box.Pending(ctx, 10, 3)
	want("pending, oldest first", pending, err, msgs...)
	if got := pending[0]; got.EventType != msgs[0].EventType || got.AggregateID != "a" || got.AggregateVersion != 1 ||
		string(got.Payload) != string(msgs[0].Payload) || !got.OccurredAt.Equal(msgs[0].OccurredAt) ||
		got.CorrelationID != "corr" || got.CausationID != "cause" {
		t.Fatalf("round trip: %+v", got)
	}

	if _, err := box.Claim(ctx, 10, 3); err == nil {
		t.Fatal("a claim needs a unit of work to hold the messages")
	}
	boom := errors.New("boom")
	if err := db.Do(ctx, func(ctx context.Context) error {
		got, err := box.Claim(ctx, 2, 3)
		want("claim", got, err, msgs[0], msgs[1])
		if err := box.MarkProcessed(ctx, msgs[0].ID); err != nil {
			t.Fatal(err)
		}
		return boom
	}); !errors.Is(err, boom) {
		t.Fatalf("expected rollback, got %v", err)
	}
	pending, err = box.Pending(ctx, 10, 3)
	want("a rolled back pass leaves everything pending", pending, err, msgs...)

	if db.Dialect().Name() != "sqlite" {
		// Relay A holds the two oldest messages while relay B and a publisher work.
		held, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		go func() {
			done <- db.Do(ctx, func(ctx context.Context) error {
				got, err := box.Claim(ctx, 2, 3)
				if err != nil || len(got) != 2 {
					close(held)
					return fmt.Errorf("relay A claimed %v: %w", ids(got), err)
				}
				close(held)
				<-release
				if err := box.MarkProcessed(ctx, msgs[0].ID); err != nil {
					return err
				}
				return box.MarkFailed(ctx, msgs[1].ID, errors.New("consumer down"))
			})
		}()
		<-held
		started := time.Now()
		late := OutboxMessages(6, "a", "b")[5:]
		if err := db.Do(ctx, func(held context.Context) error {
			got, err := box.Claim(held, 10, 3)
			want("relay B must not overtake the messages relay A holds", got, err)
			// A publisher, on its own connection, while both relays are in their pass.
			if err := box.Append(ctx, late...); err != nil {
				t.Fatalf("publishing while the relays hold messages: %v", err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if waited := time.Since(started); waited > 5*time.Second {
			t.Fatalf("relay B and the publisher must not wait for relay A: %s", waited)
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		msgs = append(msgs, late...)
	} else {
		if err := db.Do(ctx, func(ctx context.Context) error {
			if err := box.MarkProcessed(ctx, msgs[0].ID); err != nil {
				return err
			}
			return box.MarkFailed(ctx, msgs[1].ID, errors.New("consumer down"))
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.Do(ctx, func(ctx context.Context) error {
		got, err := box.Claim(ctx, 10, 3)
		want("once relay A is done, the rest can be claimed in order", got, err, msgs[1:]...)
		if got[0].Attempts != 1 || got[0].LastError != "consumer down" {
			t.Fatalf("failed attempt not recorded: %+v", got[0])
		}
		for range 2 {
			if err := box.MarkFailed(ctx, msgs[1].ID, errors.New("consumer down")); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Do(ctx, func(ctx context.Context) error {
		got, err := box.Claim(ctx, 10, 3)
		want("a message out of attempts is parked and no longer holds the rest back", got, err, msgs[2:]...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
