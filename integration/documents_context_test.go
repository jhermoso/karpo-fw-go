package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	billing "github.com/jhermoso/karpo-fw-go/contexts/billing/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/documents"
	dapp "github.com/jhermoso/karpo-fw-go/contexts/documents/application"
	ddomain "github.com/jhermoso/karpo-fw-go/contexts/documents/domain"
	dinfra "github.com/jhermoso/karpo-fw-go/contexts/documents/infrastructure"
	fiscal "github.com/jhermoso/karpo-fw-go/contexts/fiscal/contracts"
	orders "github.com/jhermoso/karpo-fw-go/contexts/orders/contracts"
	payroll "github.com/jhermoso/karpo-fw-go/contexts/payroll/contracts"
	purchases "github.com/jhermoso/karpo-fw-go/contexts/purchases/contracts"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestDocumentsContext runs Documents on every engine, fed with the Published Language of the
// issuing contexts as they declare it (their contract types, so a renamed field breaks here): the
// register through the SQL inbox, one entry per fact, cancellations, the round trip of the
// optional columns, the searches (text, dates, party) and the trail.
func TestDocumentsContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			dinfra.DropAll(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{dinfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			sw := hotswap.New(db)
			dm := documents.Compose(sw)
			broker := inprocess.NewBroker()
			broker.Subscribe("documents", dm.Consumer)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			n := 0
			send := func(at time.Time, ev application.IntegrationEvent) {
				t.Helper()
				n++
				raw, err := json.Marshal(ev)
				must(err)
				env := application.Envelope{ID: fw.NewUUID().String(), Type: ev.IntegrationEventType(), Source: "test", OccurredAt: at, Data: raw}
				must(broker.Send(ctx, env))
				must(broker.Send(ctx, env)) // delivered twice: the inbox drops the second
			}
			id := func() string { return fw.NewUUID().String() }
			day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 10, 0, 0, 0, time.UTC) }
			acme, customer, supplier, employee := id(), id(), id(), id()
			order, delivery, invoice, credit, received, payslip, filing := id(), id(), id(), id(), id(), id(), id()

			send(day(5, 4), orders.OrderConfirmedV1{OrderID: order, Company: acme, Customer: customer, Number: "PED-2026-000001", Total: "242.00"})
			send(day(5, 6), orders.DeliveryIssuedV1{DeliveryID: delivery, OrderID: order, OrderNumber: "PED-2026-000001", Company: acme, Customer: customer,
				Number: "ALB-2026-000001", Date: "2026-05-06", Total: "242.00"})
			send(day(5, 8), billing.InvoiceIssuedV1{InvoiceID: invoice, Number: "A-2026-000001", Kind: "ordinary", SourceType: "orders.delivery",
				SourceID: delivery, Seller: acme, Customer: customer, IssueDate: "2026-05-08", Total: "242.00"})
			send(day(5, 20), billing.InvoiceIssuedV1{InvoiceID: credit, Number: "R-2026-000001", Kind: "corrective", Corrects: invoice, Seller: acme,
				Customer: customer, IssueDate: "2026-05-20", Total: "-24.20"})
			send(day(5, 10), purchases.InvoiceRegisteredV1{InvoiceID: received, Company: acme, Supplier: supplier, SupplierNumber: "2026/0042",
				Register: "FR-2026-000001", Received: "2026-05-10", Total: "1210.00"})
			send(day(5, 11), purchases.InvoiceCancelledV1{InvoiceID: received, Company: acme, Register: "FR-2026-000001", Reason: "duplicada"})
			send(day(5, 28), payroll.PayslipApprovedV1{PayslipID: payslip, Person: employee, Employer: acme, Kind: "ordinary", PeriodStart: "2026-05-01",
				PeriodEnd: "2026-05-31", PaymentDate: "2026-05-29", Net: "1500.00"})
			send(day(7, 15), fiscal.FilingSubmittedV1{FilingID: filing, Declarant: acme, Form: "111", Year: 2026, Period: "2T", Withheld: "300.00"})
			send(day(7, 16), fiscal.FilingRevertedV1{FilingID: filing, Reason: "error en las bases"})
			send(day(7, 17), payroll.PayslipCancelledV1{PayslipID: payslip, Reason: "recalculada"})

			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			svc := dm.Service

			all, err := svc.Search.Handle(actx, dapp.SearchDocuments{Company: acme})
			if err != nil || len(all.Items) != 7 || all.Items[0].Type != "order" || all.Items[0].Date != "2026-05-04" || all.Items[6].Type != "tax-filing" {
				t.Fatalf("register: %+v %v", all.Items, err)
			}
			rec, err := svc.Get.Handle(actx, dapp.GetDocument{Type: "received-invoice", FactID: received})
			if err != nil || rec.Number != "FR-2026-000001" || rec.Reference != "2026/0042" || rec.Party != supplier || rec.Total != "1210.00" ||
				!rec.Cancelled || rec.CancelReason != "duplicada" || rec.Version != 2 || rec.OriginType != "" || rec.Relation != "" {
				t.Fatalf("received invoice round trip: %+v %v", rec, err)
			}
			fil, err := svc.Get.Handle(actx, dapp.GetDocument{Type: "tax-filing", FactID: filing})
			if err != nil || fil.Number != "111-2026-2T" || fil.Date != "2026-07-15" || fil.Party != "" || !fil.Cancelled {
				t.Fatalf("filing round trip: %+v %v", fil, err)
			}
			crd, err := svc.Get.Handle(actx, dapp.GetDocument{Type: "credit-note", FactID: credit})
			if err != nil || crd.OriginType != "invoice" || crd.OriginID != invoice || crd.Relation != "rectifies" || crd.Total != "-24.20" {
				t.Fatalf("credit note round trip: %+v %v", crd, err)
			}
			for query, want := range map[dapp.SearchDocuments]int{
				{Company: acme, Type: "invoice"}:                      1,
				{Company: acme, Party: customer}:                      4,
				{Company: acme, Number: "alb"}:                        1,
				{Company: acme, From: "2026-05-10", To: "2026-05-31"}: 3,
				{Company: acme, Live: true}:                           4,
			} {
				if p, err := svc.Search.Handle(actx, query); err != nil || len(p.Items) != want {
					t.Fatalf("search %+v: %d documents, want %d (%v)", query, len(p.Items), want, err)
				}
			}
			cid, _ := ddomain.ParseDocumentID(crd.ID)
			trail, err := svc.Trail.Handle(actx, dapp.GetTrail{ID: cid})
			if err != nil || len(trail) != 4 || trail[0].Number != "PED-2026-000001" || trail[1].Number != "ALB-2026-000001" ||
				trail[2].Number != "A-2026-000001" || trail[3].Number != "R-2026-000001" {
				t.Fatalf("trail: %+v %v", trail, err)
			}
			got, found, err := dm.Register.ByFact(ctx, "delivery-note", delivery)
			if err != nil || !found || got.Number != "ALB-2026-000001" || got.Date != "2026-05-06" {
				t.Fatalf("port: %+v %v %v", got, found, err)
			}
			if n != 10 {
				t.Fatalf("facts sent: %d", n)
			}
		})
	}
}
