package accounting_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting"
	aapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

// Un cobro deshecho a las 23:30 UTC del 12 de marzo se anula el día de la empresa: el 13 en
// Kiritimati (UTC+14), el 12 sin zona.
func TestAccounting_ReversalFromAnEventIsOnTheCompanysDay(t *testing.T) {
	kiritimati, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Fatal(err)
	}
	early, inUTC := fw.NewUUID().String(), fw.NewUUID().String()
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	m := accounting.Compose(sw, accounting.WithToday(zones{early: kiritimati}))
	ac, err := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	if err != nil {
		t.Fatal(err)
	}
	ac.GlobalAdmin = true
	ctx := authz.WithContext(context.Background(), ac)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 3, 12, 23, 30, 0, 0, time.UTC)
	send := func(typ string, data any) {
		t.Helper()
		raw, _ := json.Marshal(data)
		must(m.Consumer.HandleMessage(ctx, app.Envelope{ID: fw.NewUUID().String(), Type: typ, Source: "test", OccurredAt: at, Data: raw}))
	}
	for company, want := range map[string]string{early: "2026-03-13", inUTC: "2026-03-12"} {
		for code, name := range map[string]string{"4300": "Clientes", "5700": "Caja"} {
			_, err := m.Service.CreateAccount.Handle(ctx, aapp.CreateAccount{Company: company, Code: code, Name: name, Postable: true})
			must(err)
		}
		_, err := m.Service.OpenLedger.Handle(ctx, aapp.OpenLedger{Company: company, StartMonth: 1, Accounts: map[string]string{"customers": "4300", "cash": "5700"}})
		must(err)
		collection, invoice := fw.NewUUID().String(), fw.NewUUID().String()
		send("receivables.collection-allocated.v1", map[string]any{"collectionId": collection, "seller": company, "payer": fw.NewUUID().String(),
			"method": "cash", "invoiceId": invoice, "installment": 1, "amount": "121.00", "on": "2026-03-10"})
		send("receivables.allocation-reversed.v1", map[string]any{"collectionId": collection, "seller": company, "invoiceId": invoice, "installment": 1})
		p, err := m.Service.SearchEntries.Handle(ctx, aapp.SearchEntries{Company: company})
		must(err)
		var got string
		for _, e := range p.Items {
			if e.Reverses != "" {
				got = e.Date
			}
		}
		if got != want {
			t.Fatalf("reversal of %s: %q, want %s (%+v)", company, got, want, p.Items)
		}
	}
}
