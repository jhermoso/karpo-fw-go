package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/financial"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/financial/application"
	fdomain "github.com/jhermoso/karpo-fw-go/contexts/financial/domain"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/financial/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// anyInstitution plays Parties: here every company is a financial institution.
type anyInstitution struct{}

func (anyInstitution) IsFinancialInstitution(context.Context, fdomain.OrganizationID) (bool, error) {
	return true, nil
}

// TestFinancialContext runs Financial on every engine: an account with its holders, uses, dates
// and accents as each engine gives them back, the number unique in its company and free in
// another, the searches through the holders and uses that last (booleans and children on every
// engine), the statuses, the closing that ends everything, the statistics and the directory other
// contexts ask.
func TestFinancialContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			finfra.DropAll(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{finfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			fm := financial.Compose(hotswap.New(db), anyInstitution{})
			svc := fm.Service
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			violates := func(err error, code string) {
				t.Helper()
				var rv *fw.RuleViolationError
				if !errors.As(err, &rv) || rv.Code != code {
					t.Fatalf("want %s: %v", code, err)
				}
			}
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			acme, globex := fw.NewUUID().String(), fw.NewUUID().String()
			ana, luis := fw.NewUUID().String(), fw.NewUUID().String()
			today := vocab.DateOf(fw.Now())
			id := func(a fapp.AccountDTO) fdomain.AccountID { x, _ := fdomain.ParseAccountID(a.ID); return x }

			// The product and the agreement an account is opened under, with their dates and accents.
			product, err := svc.DefineProduct.Handle(actx, fapp.DefineProduct{Company: acme, Code: "cta-pago", Name: "Cuenta de pago básica", Family: "payment",
				Description: "Sin comisión de mantenimiento", RegulatoryCode: "mifid-x1"})
			must(err)
			_, err = svc.DefineProduct.Handle(actx, fapp.DefineProduct{Company: acme, Code: "CTA-PAGO", Name: "Otra", Family: "payment"})
			violates(err, "financial.duplicate_code")
			contract, err := svc.SignAgreement.Handle(actx, fapp.SignAgreement{Company: acme, Customer: ana, Number: "ctr-2026-001", Name: "Contrato marco de Íñigo",
				Product: product.ID, Signed: today.AddDays(-20), Thru: today.AddDays(345)})
			must(err)
			_, err = svc.SignAgreement.Handle(actx, fapp.SignAgreement{Company: acme, Customer: luis, Number: "CTR-2026-001", Name: "Otro", Family: "loan"})
			violates(err, "financial.duplicate_number")
			pid, _ := fdomain.ParseProductID(product.ID)
			cid, _ := fdomain.ParseAgreementID(contract.ID)
			storedProduct, err := svc.GetProduct.Handle(actx, fapp.GetProduct{ID: pid})
			if err != nil || storedProduct.Code != "CTA-PAGO" || storedProduct.Name != "Cuenta de pago básica" || storedProduct.RegulatoryCode != "MIFID-X1" ||
				storedProduct.Description != "Sin comisión de mantenimiento" || !storedProduct.Offered || storedProduct.Discontinued != "" {
				t.Fatalf("stored product: %+v %v", storedProduct, err)
			}
			storedContract, err := svc.GetAgreement.Handle(actx, fapp.GetAgreement{ID: cid})
			if err != nil || storedContract.Number != "CTR-2026-001" || storedContract.Name != "Contrato marco de Íñigo" || storedContract.Family != "payment" ||
				storedContract.Product != product.ID || storedContract.Signed != today.AddDays(-20).String() || storedContract.From != today.AddDays(-20).String() ||
				storedContract.Thru != today.AddDays(345).String() || storedContract.Status != "in-force" || storedContract.Customer != ana {
				t.Fatalf("stored agreement: %+v %v", storedContract, err)
			}
			if p, err := svc.SearchAgreements.Handle(actx, fapp.SearchAgreements{Company: acme, Customer: ana, Product: product.ID, Status: "in-force"}); err != nil || p.Total != 1 {
				t.Fatalf("agreements of ana: %+v %v", p, err)
			}

			acc, err := svc.Open.Handle(actx, fapp.OpenAccount{Company: acme, Number: "es91 2100 0418 4502 0005 1332", Holder: ana, Name: "Cuenta de pago de Íñigo y Ana",
				BIC: "caixesbb", Uses: []string{"customer-payment"}, Opened: today.AddDays(-10), Product: product.ID, Agreement: contract.ID})
			must(err)
			_, err = svc.Open.Handle(actx, fapp.OpenAccount{Company: acme, Number: "ES9121000418450200051332", Holder: luis})
			violates(err, "financial.duplicate_number")
			if _, err := svc.Open.Handle(actx, fapp.OpenAccount{Company: globex, Number: "ES9121000418450200051332", Holder: luis}); err != nil {
				t.Fatalf("the same number in another institution: %v", err)
			}
			wallet, err := svc.Open.Handle(actx, fapp.OpenAccount{Company: acme, Number: "virt-000001", Virtual: true, Holder: luis, Currency: "usd",
				Uses: []string{"virtual-multicurrency"}, Demo: true})
			must(err)

			_, err = svc.Relate.Handle(actx, fapp.RelateParty{ID: id(acc), Party: luis, Role: "holder", From: today.AddDays(-5)})
			must(err)
			_, err = svc.FileUnder.Handle(actx, fapp.FileUnder{ID: id(acc), Party: luis})
			must(err)
			_, err = svc.Unrelate.Handle(actx, fapp.UnrelateParty{ID: id(acc), Party: ana, Role: "holder", On: today.AddDays(-1)})
			must(err)
			_, err = svc.Assign.Handle(actx, fapp.ChangeUse{ID: id(acc), Use: "player"})
			must(err)
			_, err = svc.Withdraw.Handle(actx, fapp.ChangeUse{ID: id(acc), Use: "customer-payment", On: today})
			must(err)
			_, err = svc.Block.Handle(actx, fapp.ChangeStatus{ID: id(acc), Reason: "Orden judicial nº 123/2026"})
			must(err)

			// As stored.
			got, err := svc.Get.Handle(actx, fapp.GetAccount{Company: acme, Number: "ES91 2100 0418 4502 0005 1332"})
			must(err)
			if got.ID != acc.ID || got.Number != "ES9121000418450200051332" || got.BIC != "CAIXESBB" || got.Name != "Cuenta de pago de Íñigo y Ana" ||
				got.Status != "blocked" || got.Reason != "Orden judicial nº 123/2026" || got.Opened != today.AddDays(-10).String() || got.Closed != "" ||
				got.Holder != luis || got.Version != 7 || got.Product != product.ID || got.Agreement != contract.ID || len(got.Holders) != 2 || len(got.Uses) != 2 ||
				got.Holders[0] != (fapp.HolderDTO{Party: ana, Role: "holder", From: today.AddDays(-10).String(), Thru: today.AddDays(-1).String()}) ||
				got.Holders[1] != (fapp.HolderDTO{Party: luis, Role: "holder", From: today.AddDays(-5).String(), Primary: true}) ||
				got.Uses[0] != (fapp.UseDTO{Use: "customer-payment", From: today.AddDays(-10).String(), Thru: today.String()}) ||
				got.Uses[1] != (fapp.UseDTO{Use: "player", From: today.String()}) {
				t.Fatalf("stored account: %+v", got)
			}

			search := func(q fapp.SearchAccounts, want ...string) {
				t.Helper()
				p, err := svc.Search.Handle(actx, q)
				must(err)
				if int(p.Total) != len(want) {
					t.Fatalf("%+v: %d accounts, want %d", q, p.Total, len(want))
				}
				for i, w := range want {
					if p.Items[i].ID != w {
						t.Fatalf("%+v: item %d is %s", q, i, p.Items[i].Number)
					}
				}
			}
			search(fapp.SearchAccounts{Company: acme}, acc.ID, wallet.ID)
			search(fapp.SearchAccounts{Company: acme, Party: luis}, acc.ID, wallet.ID)
			search(fapp.SearchAccounts{Company: acme, Party: ana}) // she left
			search(fapp.SearchAccounts{Company: acme, Use: "customer-payment"})
			search(fapp.SearchAccounts{Company: acme, Use: "player"}, acc.ID)
			search(fapp.SearchAccounts{Company: acme, Demo: "true"}, wallet.ID)
			search(fapp.SearchAccounts{Company: acme, Product: product.ID, Agreement: contract.ID}, acc.ID)
			ended, err := svc.TerminateAgreement.Handle(actx, fapp.TerminateAgreement{ID: cid, On: today.AddDays(-1), Reason: "A petición del cliente"})
			if err != nil || ended.Status != "terminated" || ended.Thru != today.AddDays(-1).String() {
				t.Fatalf("terminated: %+v %v", ended, err)
			}
			_, err = svc.Open.Handle(actx, fapp.OpenAccount{Company: acme, Number: "DE89370400440532013000", Holder: ana, Agreement: contract.ID})
			violates(err, "financial.agreement_not_in_force")
			gone, err := svc.DiscontinueProduct.Handle(actx, fapp.DiscontinueProduct{ID: pid, On: today})
			if err != nil || gone.Offered || gone.Discontinued != today.String() {
				t.Fatalf("discontinued: %+v %v", gone, err)
			}
			search(fapp.SearchAccounts{Company: acme, Demo: "false", Status: "blocked", Currency: "eur", Name: "íñigo"}, acc.ID)

			stats, err := svc.Stats.Handle(actx, fapp.GetStats{Company: acme})
			if err != nil || stats.Total != 1 || stats.Demo != 1 || stats.ByStatus["blocked"] != 1 || stats.ByUse["player"] != 1 || stats.ByUse["customer-payment"] != 0 {
				t.Fatalf("stats: %+v %v", stats, err)
			}
			refs, err := fm.Accounts.OfParty(ctx, acme, luis)
			if err != nil || len(refs) != 2 || refs[0].ID != acc.ID || refs[0].Operable || !refs[1].Operable {
				t.Fatalf("accounts of luis: %+v %v", refs, err)
			}

			_, err = svc.Release.Handle(actx, fapp.ChangeStatus{ID: id(acc)})
			must(err)
			closed, err := svc.Close.Handle(actx, fapp.ChangeStatus{ID: id(acc), Reason: "A petición del titular"})
			must(err)
			again, err := svc.Get.Handle(actx, fapp.GetAccount{ID: id(acc)})
			if err != nil || again.Status != "closed" || again.Closed != today.String() || again.Holder != "" || again.Holders[1].Thru != today.String() ||
				again.Holders[1].Primary || again.Uses[1].Thru != today.String() || again.Version != closed.Version {
				t.Fatalf("closed: %+v %v", again, err)
			}
			_, err = svc.Describe.Handle(actx, fapp.DescribeAccount{ID: id(acc), Name: "x"})
			violates(err, "financial.closed")
			search(fapp.SearchAccounts{Company: acme, Party: luis}, wallet.ID)
			// An agreement signed and terminated; three accounts opened; blocked, released and closed.
			if n, err := fm.Relay(inprocess.NewBroker()).RelayOnce(ctx); err != nil || n != 8 {
				t.Fatalf("published: %d %v", n, err)
			}
		})
	}
}
