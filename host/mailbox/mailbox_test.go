package mailbox_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/host/mailbox"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	_ "modernc.org/sqlite"
)

// listener is a consumer that takes what it is given once (as the inbox of a real one makes
// sure), unless it is told to refuse.
type listener struct {
	name   string
	refuse error
	got    []string
	tried  int
}

func (l *listener) Name() string { return l.name }

func (l *listener) HandleMessage(_ context.Context, env app.Envelope) error {
	l.tried++
	if l.refuse != nil {
		return l.refuse
	}
	if !slices.Contains(l.got, env.ID) {
		l.got = append(l.got, env.ID)
	}
	return nil
}

func message(id, subject string, minute int) app.Envelope {
	return app.Envelope{ID: id, Type: "billing.invoice-issued.v1", Source: "billing", Subject: subject,
		OccurredAt: time.Date(2026, 10, 9, 10, minute, 0, 0, time.UTC), Data: json.RawMessage(`{"n":"` + id + `"}`)}
}

func scenario(t *testing.T, sw *hotswap.Switch) {
	ctx := context.Background()
	office := mailbox.New(sw)
	broker := inprocess.NewBroker()
	books := &listener{name: "accounting", refuse: fw.Violation("accounting.no_ledger", "the company has no ledger")}
	dues := &listener{name: "receivables"}
	for _, l := range []*listener{books, dues} {
		broker.Subscribe(l.Name(), office.For(l))
	}
	adminCtx, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "root", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	adminCtx.GlobalAdmin = true
	admin := authz.WithContext(ctx, adminCtx)
	kept := func(status string) []mailbox.DTO {
		t.Helper()
		p, err := office.Search(admin, mailbox.Search{Status: status, Size: 100})
		if err != nil {
			t.Fatal(err)
		}
		return p.Items
	}
	send := func(env app.Envelope) {
		t.Helper()
		if err := broker.Send(ctx, env); err != nil {
			t.Fatalf("the publisher is never told a listener refused: %v", err)
		}
	}

	// A listener refuses a message: the other gets it, the publisher is not told, the message is kept.
	send(message("m1", "inv-1", 1))
	if !slices.Equal(dues.got, []string{"m1"}) || books.tried != 1 {
		t.Fatalf("the other listener: %v, tried %d", dues.got, books.tried)
	}
	w := kept("")
	if len(w) != 1 || w[0].Consumer != "accounting" || w[0].Status != "waiting" || w[0].Attempts != 1 || w[0].Code != "accounting.no_ledger" ||
		w[0].Subject != "inv-1" || w[0].NextAttempt == "" || string(w[0].Data) != `{"n":"m1"}` {
		t.Fatalf("kept: %+v", w)
	}
	// Sent again: nothing new, and the listener is not asked again.
	send(message("m1", "inv-1", 1))
	// About the same thing: it waits behind, untried. About another: it is tried on its own.
	send(message("m2", "inv-1", 2))
	if books.tried != 1 || len(kept("")) != 2 {
		t.Fatalf("behind: tried %d, kept %+v", books.tried, kept(""))
	}
	books.refuse = nil
	send(message("m3", "inv-2", 3))
	if !slices.Equal(books.got, []string{"m3"}) || !slices.Equal(dues.got, []string{"m1", "m2", "m3"}) {
		t.Fatalf("another thing goes through: %v %v", books.got, dues.got)
	}
	// Not due yet: a round does nothing. When it is asked for, both go in the order they happened.
	if r, err := office.Redeliver(ctx, false); err != nil || r != (mailbox.Redelivered{Waiting: 2}) || len(books.got) != 1 {
		t.Fatalf("not due: %+v %v", r, err)
	}
	if r, err := office.Redeliver(ctx, true); err != nil || r != (mailbox.Redelivered{Delivered: 2}) || !slices.Equal(books.got, []string{"m3", "m1", "m2"}) {
		t.Fatalf("redelivered: %+v %v %v", r, err, books.got)
	}
	if len(kept("")) != 0 {
		t.Fatalf("what was delivered is not kept: %+v", kept(""))
	}

	// A listener that keeps refusing: after ten tries the message is given up, and what comes
	// after it about the same thing waits.
	books.refuse = errors.New("the database is down")
	send(message("m4", "inv-3", 4))
	send(message("m5", "inv-3", 5))
	for range mailbox.MaxAttempts - 2 {
		if r, err := office.Redeliver(ctx, true); err != nil || r != (mailbox.Redelivered{Waiting: 2}) {
			t.Fatalf("still refusing: %+v %v", r, err)
		}
	}
	if r, err := office.Redeliver(ctx, true); err != nil || r != (mailbox.Redelivered{Waiting: 1, GivenUp: 1}) {
		t.Fatalf("given up: %+v %v", r, err)
	}
	given := kept("given-up")
	if len(given) != 1 || given[0].Attempts != mailbox.MaxAttempts || given[0].Code != "error" || given[0].Reason != "the database is down" || given[0].NextAttempt != "" {
		t.Fatalf("given up: %+v", given)
	}
	tried := books.tried
	books.refuse = nil
	if r, err := office.Redeliver(ctx, true); err != nil || r != (mailbox.Redelivered{Waiting: 1, GivenUp: 1}) || books.tried != tried {
		t.Fatalf("what was given up is not tried on its own: %+v %v", r, err)
	}

	// Only a global administrator looks at the mailboxes and decides.
	clerk, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "clerk", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	cctx := authz.WithContext(ctx, clerk)
	gid, _ := mailbox.ParseID(given[0].ID)
	if _, err := office.Search(cctx, mailbox.Search{}); !errors.Is(err, fw.ErrForbidden) {
		t.Fatalf("search: %v", err)
	}
	if _, err := office.Retry(cctx, mailbox.Retry{}); !errors.Is(err, fw.ErrForbidden) {
		t.Fatalf("retry: %v", err)
	}
	if _, err := office.Discard(cctx, mailbox.Discard{ID: gid, Note: "x"}); !errors.Is(err, fw.ErrForbidden) {
		t.Fatalf("discard: %v", err)
	}
	if _, err := office.Search(admin, mailbox.Search{Status: "lost"}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("an unknown status: %v", err)
	}
	// Discarding says why; then what waited behind goes through.
	var rule *fw.RuleViolationError
	if _, err := office.Discard(admin, mailbox.Discard{ID: gid}); !errors.As(err, &rule) || rule.Code != "mailbox.discard_note" {
		t.Fatalf("discard without a note: %v", err)
	}
	d, err := office.Discard(admin, mailbox.Discard{ID: gid, Note: "posted by hand"})
	if err != nil || d.Status != "discarded" || d.Note != "posted by hand" || d.ResolvedAt == "" {
		t.Fatalf("discard: %+v %v", d, err)
	}
	if _, err := office.Discard(admin, mailbox.Discard{ID: gid, Note: "again"}); !errors.As(err, &rule) || rule.Code != "mailbox.not_open" {
		t.Fatalf("discard twice: %v", err)
	}
	if r, err := office.Redeliver(ctx, false); err != nil || r != (mailbox.Redelivered{Delivered: 1}) || books.got[len(books.got)-1] != "m5" {
		t.Fatalf("what waited behind: %+v %v %v", r, err, books.got)
	}
	send(message("m4", "inv-3", 4)) // the discarded message sent again: it stays discarded
	if slices.Contains(books.got, "m4") || len(kept("")) != 1 {
		t.Fatalf("discarded for good: %v %+v", books.got, kept(""))
	}

	// Asking to retry puts back what was given up and tries it at once.
	books.refuse = errors.New("still down")
	send(message("m6", "", 6)) // about nothing in particular: it holds nobody back
	send(message("m7", "", 7))
	for range mailbox.MaxAttempts - 1 {
		if _, err := office.Redeliver(ctx, true); err != nil {
			t.Fatal(err)
		}
	}
	if g := kept("given-up"); len(g) != 2 {
		t.Fatalf("two given up: %+v", g)
	}
	books.refuse = nil
	one := kept("given-up")[0]
	if r, err := office.Retry(admin, mailbox.Retry{ID: one.ID}); err != nil || r != (mailbox.Redelivered{Delivered: 1, GivenUp: 1}) {
		t.Fatalf("retry one: %+v %v", r, err)
	}
	if r, err := office.Retry(admin, mailbox.Retry{}); err != nil || r != (mailbox.Redelivered{Delivered: 1}) || len(kept("waiting"))+len(kept("given-up")) != 0 {
		t.Fatalf("retry all: %+v %v", r, err)
	}
	if _, err := office.Retry(admin, mailbox.Retry{ID: "x"}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("retry of nothing: %v", err)
	}
}

func TestMailbox_OnMemory(t *testing.T) {
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	scenario(t, sw)
}

func TestMailbox_OnSQLite(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mailbox.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{mailbox.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	scenario(t, hotswap.New(db))
}

func TestBackoff(t *testing.T) {
	now := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	d, err := mailbox.Keep(mailbox.NewID(), mailbox.State{Consumer: "c", EventType: "t", EnvelopeID: "e", Data: "{}"}, "x", "y", now)
	if err != nil {
		t.Fatal(err)
	}
	waits := []time.Duration{d.State().NextAttempt.Sub(now)}
	for range 7 {
		d.Refused("x", "y", now)
		waits = append(waits, d.State().NextAttempt.Sub(now))
	}
	if want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour}; !slices.Equal(waits, want) {
		t.Fatalf("waits: %v", waits)
	}
}
