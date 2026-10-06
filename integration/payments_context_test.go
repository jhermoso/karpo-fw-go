package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting"
	aapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	ainfra "github.com/jhermoso/karpo-fw-go/contexts/accounting/infrastructure"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	fcinfra "github.com/jhermoso/karpo-fw-go/contexts/facilities/infrastructure"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	ginfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	hinfra "github.com/jhermoso/karpo-fw-go/contexts/hr/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/payments"
	yapp "github.com/jhermoso/karpo-fw-go/contexts/payments/application"
	ydomain "github.com/jhermoso/karpo-fw-go/contexts/payments/domain"
	yinfra "github.com/jhermoso/karpo-fw-go/contexts/payments/infrastructure"
	prinfra "github.com/jhermoso/karpo-fw-go/contexts/payroll/infrastructure"
	rinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/treasury"
	tapp "github.com/jhermoso/karpo-fw-go/contexts/treasury/application"
	tdomain "github.com/jhermoso/karpo-fw-go/contexts/treasury/domain"
	tinfra "github.com/jhermoso/karpo-fw-go/contexts/treasury/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// TestPaymentsContext runs Payments with Treasury, Accounting and Parties on every engine:
// a supplier invoice with its account and a tax form (through the SQL inbox) as payables, a
// transfer order proposed from them, its pain.001 file, executed (payments registered through the
// inbox and posted by Accounting) and a transfer rejected (payment cancelled, entry reversed), a
// cash payment and the round trip of payables and payments.
func TestPaymentsContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			yinfra.DropAll(ctx, db)
			ainfra.DropAll(ctx, db)
			tinfra.DropAll(ctx, db)
			rinfra.DropAll(ctx, db)
			binfra.DropAll(ctx, db)
			finfra.DropAll(ctx, db)
			prinfra.DropAll(ctx, db)
			hinfra.DropAll(ctx, db)
			fcinfra.DropAll(ctx, db)
			ginfra.DropAll(ctx, db)
			dropPartiesTables(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), yinfra.Migrations(), tinfra.Migrations(), ainfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			sw := hotswap.New(db)
			pm := parties.Compose(sw, nil)
			ym := payments.Compose(sw, nil)
			tm := treasury.Compose(sw, nil, tinfra.PaymentsPayables{Payable: ym.Payable}, tinfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})
			am := accounting.Compose(sw)
			broker := inprocess.NewBroker()
			broker.Subscribe("payments", ym.Consumer)
			// The received invoices are played with the fields Payments reads: Accounting posts only the payments here.
			broker.Subscribe("accounting", am.Consumer, "payments.payment-allocated.v1", "payments.allocation-reversed.v1")
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			deliver := func() {
				for moved := true; moved; {
					moved = false
					for _, r := range []interface {
						RelayOnce(context.Context) (int, error)
					}{tm.Relay(broker), ym.Relay(broker)} {
						n, err := r.RelayOnce(ctx)
						must(err)
						moved = moved || n > 0
					}
				}
			}
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			svc := ym.Service

			acme, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme, S.A.", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			must(err)
			supplier, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Suministros Muñoz, S.L."})
			must(err)
			for code, name := range map[string]string{"4000": "Proveedores", "5700": "Caja", "5720": "Bancos", "4751": "Retenciones"} {
				_, err := am.Service.CreateAccount.Handle(actx, aapp.CreateAccount{Company: acme.ID, Code: code, Name: name, Postable: true})
				must(err)
			}
			_, err = am.Service.OpenLedger.Handle(actx, aapp.OpenLedger{Company: acme.ID, StartMonth: 1, Accounts: map[string]string{"suppliers": "4000",
				"cash": "5700", "bank": "5720", "withholding-payable": "4751"}})
			must(err)
			balances := func(want map[string]string) {
				t.Helper()
				rows, err := am.Service.TrialBalance.Handle(actx, aapp.TrialBalance{Company: acme.ID})
				must(err)
				got := map[string]string{}
				for _, r := range rows {
					got[r.Account] = r.Balance
				}
				for a, b := range want {
					if got[a] != b {
						t.Fatalf("balance of %s = %q, want %s (all: %v)", a, got[a], b, got)
					}
				}
			}

			// Two received invoices (one paid to two accounts, sent twice) and a Modelo 111 through the inbox.
			send := func(id, typ string, data any) {
				raw, _ := json.Marshal(data)
				must(broker.Send(ctx, application.Envelope{ID: id, Type: typ, OccurredAt: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), Data: raw}))
			}
			f1, f2 := fw.NewUUID().String(), fw.NewUUID().String()
			first := map[string]any{"invoiceId": f1, "company": acme.ID, "supplier": supplier.ID, "supplierNumber": "F-1/2026", "issued": "2026-09-15",
				"due": "2026-10-05", "payable": "99.99", "payTo": []map[string]any{{"iban": "ES9121000418450200051332", "amount": "60.00"},
					{"iban": "ES7921000813610123456789", "amount": "39.99"}}}
			send("f1-a", "purchases.invoice-registered.v1", first)
			send("f1-b", "purchases.invoice-registered.v1", first)
			send("f2", "purchases.invoice-registered.v1", map[string]any{"invoiceId": f2, "company": acme.ID, "supplier": supplier.ID,
				"supplierNumber": "F-2/2026", "issued": "2026-09-20", "due": "2026-11-20", "payable": "50.00"})
			sup, err := svc.SearchPayables.Handle(actx, yapp.SearchPayables{Company: acme.ID, Kind: "supplier-invoice"})
			if err != nil || len(sup.Items) != 2 {
				t.Fatalf("supplier payables: %+v %v", sup.Items, err)
			}
			inv, inv2 := sup.Items[0], sup.Items[1]
			filing := fw.NewUUID().String()
			data, _ := json.Marshal(map[string]any{"filingId": filing, "declarant": acme.ID, "form": "111", "year": 2026, "period": "09", "withheld": "216.00"})
			for i := 0; i < 2; i++ {
				must(broker.Send(ctx, application.Envelope{ID: "filing-" + filing, Type: "fiscal.filing-submitted.v1", OccurredAt: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), Data: data}))
			}
			id1, _ := ydomain.ParsePayableID(inv.ID)
			p1, err := svc.GetPayable.Handle(actx, yapp.GetPayable{ID: id1})
			if err != nil || len(p1.PayTo) != 2 || p1.PayTo[1].Amount != "39.99" || p1.Document != "F-1/2026" || p1.Issued != "2026-09-15" || p1.SourceID != f1 {
				t.Fatalf("payable round trip: %+v %v", p1, err)
			}
			taxes, err := svc.SearchPayables.Handle(actx, yapp.SearchPayables{Company: acme.ID, Kind: "tax"})
			if err != nil || len(taxes.Items) != 1 || taxes.Items[0].Due != "2026-10-20" || taxes.Items[0].Payee != "AEAT" || taxes.Items[0].Document != "Modelo 111 2026-09" {
				t.Fatalf("tax payable: %+v %v", taxes.Items, err)
			}

			// A transfer order with the first invoice (the second is not due, the tax has no account).
			acc, err := tm.Service.OpenAccount.Handle(actx, tapp.OpenAccount{Owner: acme.ID, IBAN: "ES1000492352082414205416", BIC: "BSCHESMMXXX", Alias: "Pagos",
				Payments: true, Opened: vocab.MustDate(2020, 1, 1)})
			must(err)
			order, err := tm.Service.ProposeTransfers.Handle(actx, tapp.ProposeTransfers{Debtor: acme.ID, Account: acc.ID, ExecutionDate: vocab.MustDate(2026, 10, 5),
				DueTo: vocab.MustDate(2026, 10, 31)})
			if err != nil || len(order.Transfers) != 2 || order.Total != "99.99" || order.WithoutAccount != 1 || order.Transfers[0].PayeeName != "Suministros Muñoz, S.L." {
				t.Fatalf("proposal: %+v %v", order, err)
			}
			oid, _ := tdomain.ParseTransferOrderID(order.ID)
			// The order is generated on 4 October whatever the day the test runs: an order is not
			// executed before being generated.
			restore := fw.SetClock(fake.New(time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)))
			order, err = tm.Service.GenerateTransfers.Handle(actx, tapp.GenerateTransfers{ID: oid})
			restore()
			must(err)
			file, err := tm.Service.TransferFile.Handle(actx, tapp.GetTransferFile{ID: oid})
			if err != nil || !strings.Contains(string(file), "<CtrlSum>99.99</CtrlSum>") || !strings.Contains(string(file), "<Nm>Suministros Munoz, S.L.</Nm>") ||
				!strings.Contains(string(file), "<BIC>BSCHESMMXXX</BIC>") {
				t.Fatalf("pain.001 from the stored order: %v\n%s", err, file)
			}
			_, err = tm.Service.SettleTransfers.Handle(actx, tapp.SettleTransfers{ID: oid, On: vocab.MustDate(2026, 10, 5)})
			must(err)
			deliver()
			p1, _ = svc.GetPayable.Handle(actx, yapp.GetPayable{ID: id1})
			if !p1.Settled {
				t.Fatalf("paid by transfer: %+v", p1)
			}
			balances(map[string]string{"4000": "99.99", "5720": "-99.99"})

			_, err = tm.Service.RejectTransfer.Handle(actx, tapp.RejectTransfer{ID: oid, EndToEnd: order.Transfers[1].EndToEnd, On: vocab.MustDate(2026, 10, 6),
				Reason: "AC01"})
			must(err)
			deliver()
			p1, _ = svc.GetPayable.Handle(actx, yapp.GetPayable{ID: id1})
			if p1.Settled || p1.Open != order.Transfers[1].Amount {
				t.Fatalf("rejected transfer: %+v / %+v", p1, order.Transfers[1])
			}
			balances(map[string]string{"4000": p1.Paid, "5720": "-" + p1.Paid})

			// The tax at the bank and the second invoice in cash, the payment round trip.
			pay, err := svc.RegisterPayment.Handle(actx, yapp.RegisterPayment{Company: acme.ID, Authority: "AEAT", Date: vocab.MustDate(2026, 10, 20), Amount: "216.00",
				Method: "transfer", Reference: "NRC-0001", Allocations: []yapp.AllocationInput{{Payable: taxes.Items[0].ID, Amount: "216.00"}}})
			must(err)
			cash, err := svc.RegisterPayment.Handle(actx, yapp.RegisterPayment{Company: acme.ID, Payee: supplier.ID, Date: vocab.MustDate(2026, 10, 21), Amount: "80.00",
				Method: "cash", Reference: "Recibo 7", Allocations: []yapp.AllocationInput{{Payable: inv2.ID, Amount: "50.00"}}})
			must(err)
			cid, _ := ydomain.ParsePaymentID(cash.ID)
			cash, err = svc.GetPayment.Handle(actx, yapp.GetPayment{ID: cid})
			if err != nil || cash.Unallocated != "30.00" || len(cash.Allocations) != 1 || cash.Allocations[0].Kind != "supplier-invoice" || cash.Reference != "Recibo 7" ||
				cash.Method != "cash" || cash.Payee != supplier.ID {
				t.Fatalf("payment round trip: %+v %v", cash, err)
			}
			pid, _ := ydomain.ParsePaymentID(pay.ID)
			pay, err = svc.GetPayment.Handle(actx, yapp.GetPayment{ID: pid})
			if err != nil || pay.Payee != "AEAT" || pay.Allocations[0].Kind != "tax" {
				t.Fatalf("tax payment round trip: %+v %v", pay, err)
			}
			deliver()
			balances(map[string]string{"4751": "216.00", "5700": "-50.00"})
			if _, err := svc.CancelPayment.Handle(actx, yapp.CancelPayment{ID: cid}); err != nil {
				t.Fatal(err)
			}
			deliver()
			balances(map[string]string{"5700": "0.00"})
			all, err := svc.SearchPayments.Handle(actx, yapp.SearchPayments{Company: acme.ID})
			if err != nil || len(all.Items) != 4 {
				t.Fatalf("payments: %+v %v", all.Items, err)
			}
		})
	}
}
