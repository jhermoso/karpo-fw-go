package integration

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/host/mailbox"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

type fickleListener struct {
	refuse error
	got    []string
}

func (*fickleListener) Name() string { return "accounting" }

func (l *fickleListener) HandleMessage(_ context.Context, env app.Envelope) error {
	if l.refuse != nil {
		return l.refuse
	}
	l.got = append(l.got, env.ID)
	return nil
}

// TestMailbox keeps on every engine what a listener refuses, delivers it later in order, gives it
// up after too many tries and lets an administrator discard it.
func TestMailbox(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			mailbox.DropAll(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{mailbox.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			if err := m.Verify(ctx); err != nil {
				t.Fatal(err)
			}
			office, broker := mailbox.New(hotswap.New(db)), inprocess.NewBroker()
			l := &fickleListener{refuse: fw.Violation("accounting.no_ledger", "la empresa Ñandú no tiene libro")}
			broker.Subscribe(l.Name(), office.For(l))
			root, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "root", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			root.GlobalAdmin = true
			admin := authz.WithContext(ctx, root)
			send := func(id, subject string, minute int) {
				t.Helper()
				if err := broker.Send(ctx, app.Envelope{ID: id, Type: "billing.invoice-issued.v1", Source: "billing", Subject: subject,
					OccurredAt: time.Date(2026, 10, 9, 10, minute, 0, 0, time.UTC), Data: json.RawMessage(`{"cliente":"Íñigo Núñez"}`)}); err != nil {
					t.Fatal(err)
				}
			}
			send("m1", "inv-1", 1)
			send("m2", "inv-1", 2)
			send("m1", "inv-1", 1)
			kept, err := office.Search(admin, mailbox.Search{Size: 10})
			if err != nil || kept.Total != 2 || kept.Items[0].Attempts != 1 || kept.Items[0].Reason == "" || kept.Items[1].Code != mailbox.BehindCode ||
				string(kept.Items[0].Data) != `{"cliente":"Íñigo Núñez"}` || kept.Items[0].OccurredAt != "2026-10-09T10:01:00Z" {
				t.Fatalf("kept: %+v %v", kept.Items, err)
			}
			if r, err := office.Redeliver(ctx, false); err != nil || r != (mailbox.Redelivered{Waiting: 2}) {
				t.Fatalf("not due: %+v %v", r, err)
			}
			l.refuse = nil
			if r, err := office.Redeliver(ctx, true); err != nil || r != (mailbox.Redelivered{Delivered: 2}) || !slices.Equal(l.got, []string{"m1", "m2"}) {
				t.Fatalf("redelivered: %+v %v %v", r, err, l.got)
			}

			l.refuse = errors.New("down")
			send("m3", "inv-2", 3)
			for range mailbox.MaxAttempts - 1 {
				if _, err := office.Redeliver(ctx, true); err != nil {
					t.Fatal(err)
				}
			}
			given, err := office.Search(admin, mailbox.Search{Status: "given-up"})
			if err != nil || given.Total != 1 || given.Items[0].Attempts != mailbox.MaxAttempts || given.Items[0].NextAttempt != "" {
				t.Fatalf("given up: %+v %v", given.Items, err)
			}
			id, _ := mailbox.ParseID(given.Items[0].ID)
			d, err := office.Discard(admin, mailbox.Discard{ID: id, Note: "asentado a mano"})
			if err != nil || d.Status != "discarded" || d.Note != "asentado a mano" {
				t.Fatalf("discard: %+v %v", d, err)
			}
			if r, err := office.Retry(admin, mailbox.Retry{}); err != nil || r != (mailbox.Redelivered{}) {
				t.Fatalf("nothing left: %+v %v", r, err)
			}
		})
	}
}
