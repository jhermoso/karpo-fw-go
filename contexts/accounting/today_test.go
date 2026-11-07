package accounting_test

import (
	"context"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting"
	aapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	adomain "github.com/jhermoso/karpo-fw-go/contexts/accounting/domain"
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

// Un asiento revertido sin fecha se revierte el día de la empresa: el 14 de octubre a las 22:30
// UTC ya es el 15 en Madrid, pero aún el 14 en Canarias (la empresa que guarda Atlantic/Canary) y
// en UTC (sin zona).
func TestAccounting_ReversalIsOnTheCompanysDay(t *testing.T) {
	defer fw.SetClock(fake.New(time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC)))()
	madrid, _ := time.LoadLocation("Europe/Madrid")
	canary, _ := time.LoadLocation("Atlantic/Canary")
	inMadrid, inCanaries, inUTC := fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String()
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	m := accounting.Compose(sw, accounting.WithToday(zones{inMadrid: madrid, inCanaries: canary}))
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
	for company, want := range map[string]vocab.Date{inMadrid: vocab.MustDate(2026, 10, 15), inCanaries: vocab.MustDate(2026, 10, 14),
		inUTC: vocab.MustDate(2026, 10, 14)} {
		for code, name := range map[string]string{"5700": "Caja", "5720": "Bancos"} {
			_, err := m.Service.CreateAccount.Handle(ctx, aapp.CreateAccount{Company: company, Code: code, Name: name, Postable: true})
			must(err)
		}
		_, err := m.Service.OpenLedger.Handle(ctx, aapp.OpenLedger{Company: company, StartMonth: 1})
		must(err)
		e, err := m.Service.PostEntry.Handle(ctx, aapp.PostEntry{Company: company, Date: vocab.MustDate(2026, 10, 5), Description: "Retirada de caja",
			Lines: []aapp.LineInput{{Account: "5700", Debit: "500"}, {Account: "5720", Credit: "500"}}})
		must(err)
		id, _ := adomain.ParseEntryID(e.ID)
		r, err := m.Service.ReverseEntry.Handle(ctx, aapp.ReverseEntry{ID: id})
		if err != nil || r.Date != want.String() {
			t.Fatalf("reversal of %s: %s %v, want %s", company, r.Date, err, want)
		}
	}
}
