package treasury_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/treasury"
	tapp "github.com/jhermoso/karpo-fw-go/contexts/treasury/application"
	"github.com/jhermoso/karpo-fw-go/contexts/treasury/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// zones hace de anfitrión: la zona de cada empresa; la que no está, UTC.
type zones map[string]*time.Location

func (z zones) DateOf(_ context.Context, company fw.UUID, at time.Time) (vocab.Date, error) {
	if loc := z[company.String()]; loc != nil {
		return vocab.DateOf(at.In(loc)), nil
	}
	return vocab.DateOf(at.UTC()), nil
}

// Una orden generada a las 23:30 UTC del 1 de octubre es del día de la empresa que paga: en
// Kiritimati (UTC+14) ya es el 2, y el banco no la ejecuta el 1; sin zona, sí.
func TestTreasury_TransferOrderGeneratedOnTheDebtorsDay(t *testing.T) {
	defer fw.SetClock(fake.New(time.Date(2026, 10, 1, 23, 30, 0, 0, time.UTC)))()
	kiritimati, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Fatal(err)
	}
	early, inUTC := fw.NewUUID().String(), fw.NewUUID().String()
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	world := &others{}
	svc := treasury.Compose(sw, world, world, world, treasury.WithToday(zones{early: kiritimati})).Service
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
	day := func(d int) vocab.Date { return vocab.MustDate(2026, 10, d) }
	payee, _ := vocab.NewIBAN("ES6621000418401234567891")
	ibans := map[string]string{early: "ES7921000813610123456789", inUTC: "ES9121000418450200051332"}
	for company, wantOnThe1st := range map[string]bool{early: false, inUTC: true} {
		acc, err := svc.OpenAccount.Handle(ctx, tapp.OpenAccount{Owner: company, IBAN: ibans[company], Alias: "Principal", Payments: true,
			Opened: vocab.MustDate(2020, 1, 1)})
		must(err)
		world.pay = []domain.PayableDue{{Payable: domain.PayableID{UUID: fw.NewUUID()}, Kind: "supplier-invoice", Document: "FR-2026-000001",
			Payee: fw.NewUUID().String(), Due: day(5), PayTo: []domain.PayableAccount{{IBAN: payee, Amount: vocab.MustDecimal("100.00")}}}}
		order, err := svc.ProposeTransfers.Handle(ctx, tapp.ProposeTransfers{Debtor: company, Account: acc.ID, ExecutionDate: day(5), DueTo: day(5)})
		must(err)
		oid, _ := domain.ParseTransferOrderID(order.ID)
		_, err = svc.GenerateTransfers.Handle(ctx, tapp.GenerateTransfers{ID: oid})
		must(err)
		_, err = svc.SettleTransfers.Handle(ctx, tapp.SettleTransfers{ID: oid, On: day(1)})
		var rv *fw.RuleViolationError
		if wantOnThe1st != (err == nil) || (err != nil && (!errors.As(err, &rv) || rv.Code != "treasury.settle_date")) {
			t.Fatalf("settle on the 1st for %s: %v, want accepted %v", company, err, wantOnThe1st)
		}
		if !wantOnThe1st {
			_, err = svc.SettleTransfers.Handle(ctx, tapp.SettleTransfers{ID: oid, On: day(2)})
			must(err)
		}
	}
}
