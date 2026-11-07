package exchange_test

import (
	"context"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/exchange"
	xapp "github.com/jhermoso/karpo-fw-go/contexts/exchange/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
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

// Una reserva hecha a las 23:30 UTC del 4 de mayo para recoger a las 23:30 UTC del 6 se registra
// y se recoge el día de la empresa: el 5 y el 7 en Kiritimati (UTC+14), el 4 y el 6 sin zona. Lo
// dicen el filtro por días y el resumen diario.
func TestExchange_ReservationDaysAreTheCompanys(t *testing.T) {
	now := time.Date(2026, 5, 4, 23, 30, 0, 0, time.UTC)
	defer fw.SetClock(fake.New(now))()
	kiritimati, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Fatal(err)
	}
	early, inUTC := fw.NewUUID().String(), fw.NewUUID().String()
	h := compose(t, exchange.WithToday(zones{early: kiritimati}))
	svc, ctx := h.exg.Service, h.adminCtx
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for company, want := range map[string][2]string{early: {"2026-05-05", "2026-05-07"}, inUTC: {"2026-05-04", "2026-05-06"}} {
		_, err := svc.SetCurrency.Handle(ctx, xapp.SetCurrency{Company: company, Code: "USD", Name: "Dólar", Facial: "5"})
		must(err)
		_, err = svc.SetRate.Handle(ctx, xapp.SetRate{Company: company, Code: "USD", Rate: "0.92"})
		must(err)
		_, err = svc.Reserve.Handle(ctx, xapp.Reserve{Company: company, Customer: fw.NewUUID().String(), Channel: "web", Pickup: now.Add(48 * time.Hour),
			Lines: []xapp.ReserveLine{{Currency: "USD", Amount: "100"}}})
		must(err)
		created, pickup := want[0], want[1]
		found, err := svc.Search.Handle(ctx, xapp.SearchReservations{Company: company, CreatedFrom: created, CreatedTo: created, PickupFrom: pickup, PickupTo: pickup})
		must(err)
		if len(found.Items) != 1 {
			t.Fatalf("reservations of %s registered on %s for %s: %+v", company, created, pickup, found.Items)
		}
		d, err := svc.Dashboard.Handle(ctx, xapp.GetDashboard{Company: company, From: "2026-05-01", To: "2026-05-31"})
		must(err)
		if len(d.Daily) != 1 || d.Daily[0].Date != created {
			t.Fatalf("daily summary of %s: %+v, want %s", company, d.Daily, created)
		}
	}
}
