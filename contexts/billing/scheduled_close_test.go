package billing_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/billing"
	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// madrid es la zona de todas las empresas, por los dos puertos que implementa el anfitrión.
type madrid struct{ loc *time.Location }

func (m madrid) Location(context.Context, string, string) (*time.Location, error) { return m.loc, nil }

func (m madrid) DateOf(_ context.Context, _ fw.UUID, at time.Time) (vocab.Date, error) {
	return vocab.DateOf(at.In(m.loc)), nil
}

// El cierre programado (D-146): cierra el mes anterior, contado en la zona de la empresa y una hora
// después de acabar (a las 00:30 UTC del 1 de noviembre son las 01:30 en Madrid, y octubre queda
// atrás; en UTC aún no ha pasado la hora);
// deja pendientes los albaranes del mes en curso; la segunda vez no prepara nada; y un albarán de
// octubre que llega tarde va en su propio borrador en la vez siguiente.
func TestBilling_CloseDuePeriods(t *testing.T) {
	defer fw.SetClock(fake.New(time.Date(2026, 11, 1, 0, 30, 0, 0, time.UTC)))()
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Skip("no time zone database:", err)
	}
	for _, zoned := range []bool{false, true} {
		var opts []billing.Option
		if zoned {
			opts = append(opts, billing.WithZones(madrid{loc}))
		}
		h := compose(t, opts...)
		acme, ana := h.seller(t)
		bs, actx := h.billing.Service, h.adminCtx
		send := func(number, date string) {
			t.Helper()
			line := map[string]any{"sku": "T", "description": "Tornillo", "taxCode": "G21", "quantity": "10", "unitPrice": "1.0000", "discount": "0",
				"netPrice": "1.0000", "amount": "10.00"}
			data, _ := json.Marshal(map[string]any{"deliveryId": fw.NewUUID().String(), "orderNumber": "P-" + number, "company": acme, "customer": ana,
				"number": number, "date": date, "billingPeriodicity": "monthly", "lines": []map[string]any{line}})
			h.ok(h.billing.Consumer.HandleMessage(context.Background(), application.Envelope{ID: fw.NewUUID().String(), Type: "orders.delivery-issued.v1",
				Source: "orders", Data: data}))
		}
		send("M-1", "2026-10-03")
		send("M-2", "2026-11-01") // ya noviembre en Madrid

		due := func() bapp.ClosePeriodResult {
			t.Helper()
			res, err := bs.CloseDuePeriods.Handle(actx, bapp.CloseDuePeriods{Seller: acme})
			h.ok(err)
			if len(res.Failed) != 0 {
				t.Fatalf("failed: %+v", res.Failed)
			}
			return res
		}
		pending := func() int {
			t.Helper()
			p, err := bs.SearchPendingDeliveries.Handle(actx, bapp.SearchPendingDeliveries{Seller: acme})
			h.ok(err)
			return len(p.Items)
		}
		if !zoned {
			// En UTC, octubre acabó hace media hora: aún no se cierra.
			if res := due(); len(res.Drafts) != 0 || pending() != 2 {
				t.Fatalf("in UTC, October is not over for an hour yet: %+v", res)
			}
			continue
		}
		res := due()
		if len(res.Drafts) != 1 || res.Drafts[0].SourceRef != "M-1" || res.Drafts[0].OperationDate != "2026-10-03" || pending() != 1 {
			t.Fatalf("October closed, November pending: %+v", res)
		}
		if res := due(); len(res.Drafts) != 0 {
			t.Fatalf("a second time prepares nothing: %+v", res)
		}
		send("L-1", "2026-10-20") // de octubre, llega tarde
		if res := due(); len(res.Drafts) != 1 || res.Drafts[0].SourceRef != "L-1" || pending() != 1 {
			t.Fatalf("a late delivery note of October, in its own draft: %+v", res)
		}
		if _, err := bs.CloseDuePeriods.Handle(actx, bapp.CloseDuePeriods{Seller: "x"}); err == nil {
			t.Fatal("a seller that is not an id")
		}
	}
}
