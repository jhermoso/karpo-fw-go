package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting"
	aapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	adomain "github.com/jhermoso/karpo-fw-go/contexts/accounting/domain"
	ainfra "github.com/jhermoso/karpo-fw-go/contexts/accounting/infrastructure"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestAccountingParking runs on every engine what Accounting does with the facts of a company
// that has no books yet: the consumer's own unit of work is undone when a rule refuses the fact
// (its inbox does not remember it), the fact is kept as it came (a long JSON with accents, its
// instants), those behind it wait in order, and all are posted once the books are open.
func TestAccountingParking(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			ainfra.DropAll(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{ainfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			am := accounting.Compose(hotswap.New(db))
			svc := am.Service
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			shop := fw.NewUUID().String()
			happened := time.Date(2026, 3, 10, 9, 30, 15, 0, time.UTC)
			invoice := func(number string, after time.Duration) app.Envelope {
				id := fw.NewUUID().String()
				raw, _ := json.Marshal(map[string]any{"invoiceId": id, "number": number, "seller": shop, "customer": fw.NewUUID().String(),
					"issueDate": "2026-03-10", "net": "100.00", "total": "121.00", "taxes": []map[string]any{{"taxCode": "G", "amount": "21.00"}},
					"notes": "Señor Núñez: " + strings.Repeat("línea de la factura con eñes y acentos; ", 200)})
				return app.Envelope{ID: fw.NewUUID().String(), Type: "billing.invoice-issued.v1", Source: "billing", Subject: id, OccurredAt: happened.Add(after),
					CorrelationID: "corr-" + number, Data: raw}
			}
			first, second := invoice("F-1", 0), invoice("F-2", time.Minute)

			var rv *fw.RuleViolationError
			if err := am.Consumer.HandleMessage(ctx, first); !errors.As(err, &rv) || rv.Code != "accounting.no_ledger" {
				t.Fatalf("the consumer alone refuses it: %v", err)
			}
			// The second arrives first: they are still posted in the order they happened.
			must(am.Parking.HandleMessage(ctx, second))
			must(am.Parking.HandleMessage(ctx, first))
			must(am.Parking.HandleMessage(ctx, first))
			kept, err := svc.SearchParked.Handle(actx, aapp.SearchParked{Company: shop, Status: "parked"})
			must(err)
			if kept.Total != 2 || kept.Items[0].EventType != "billing.invoice-issued.v1" || kept.Items[0].Subject != second.Subject ||
				kept.Items[0].Code != "accounting.no_ledger" || kept.Items[1].Code != adomain.WaitingCode || kept.Items[0].OccurredAt != "2026-03-10T09:31:15Z" ||
				kept.Items[1].OccurredAt != "2026-03-10T09:30:15Z" || kept.Items[0].Company != shop || kept.Items[0].Source != "billing" {
				t.Fatalf("kept: %+v", kept.Items)
			}
			var back, sent map[string]any
			_ = json.Unmarshal(kept.Items[0].Data, &back)
			_ = json.Unmarshal(second.Data, &sent)
			if back["notes"] != sent["notes"] || back["number"] != "F-2" || len(kept.Items[0].Data) < 7000 {
				t.Fatalf("the fact as it came: %d bytes", len(kept.Items[0].Data))
			}

			for code, name := range map[string]string{"4300": "Clientes", "7000": "Ventas", "4770": "HP IVA repercutido"} {
				_, err := svc.CreateAccount.Handle(actx, aapp.CreateAccount{Company: shop, Code: code, Name: name, Postable: true})
				must(err)
			}
			_, err = svc.OpenLedger.Handle(actx, aapp.OpenLedger{Company: shop, StartMonth: 1,
				Accounts: map[string]string{"customers": "4300", "revenue": "7000", "output-tax": "4770"}})
			must(err)
			done, err := svc.RetryParked.Handle(actx, aapp.RetryParked{})
			if err != nil || done.Posted != 2 || done.Waiting != 0 {
				t.Fatalf("retry: %+v %v", done, err)
			}
			entries, err := svc.SearchEntries.Handle(actx, aapp.SearchEntries{Company: shop})
			must(err)
			if entries.Total != 2 {
				t.Fatalf("entries: %d", entries.Total)
			}
			numbers := []string{}
			for _, x := range entries.Items {
				numbers = append(numbers, x.Description)
			}
			if !strings.Contains(strings.Join(numbers, "|"), "F-1") || !strings.Contains(strings.Join(numbers, "|"), "F-2") {
				t.Fatalf("both invoices: %v", numbers)
			}
			posted, err := svc.SearchParked.Handle(actx, aapp.SearchParked{Company: shop, Status: "posted"})
			if err != nil || posted.Total != 2 || posted.Items[0].Attempts != 2 || posted.Items[0].ResolvedAt == "" || posted.Items[0].Code != "" {
				t.Fatalf("posted: %+v %v", posted.Items, err)
			}
			// Sent again after all that: nothing is posted twice, nothing is kept again.
			must(am.Parking.HandleMessage(ctx, first))
			must(am.Consumer.HandleMessage(ctx, second))
			if again, _ := svc.SearchEntries.Handle(actx, aapp.SearchEntries{Company: shop}); again.Total != 2 {
				t.Fatalf("entries after a redelivery: %d", again.Total)
			}
		})
	}
}
