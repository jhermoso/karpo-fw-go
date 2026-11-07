package orders_test

import (
	"context"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/orders"
	oapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	odomain "github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// zones hace de anfitrión: la zona de cada empresa; la que no está, UTC.
type zones map[string]*time.Location

func (z zones) Today(_ context.Context, company fw.UUID) (vocab.Date, error) {
	return vocab.Today(z[company.String()]), nil
}

// Un pedido y un presupuesto sin fecha son del día de la empresa: el 14 de octubre a las 22:30 UTC
// ya es el 15 en Madrid, pero aún el 14 en Canarias (la empresa que guarda Atlantic/Canary) y en
// UTC (sin zona).
func TestOrders_TodayIsTheCompanys(t *testing.T) {
	defer fw.SetClock(fake.New(time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC)))()
	madrid, _ := time.LoadLocation("Europe/Madrid")
	canary, _ := time.LoadLocation("Atlantic/Canary")
	inMadrid, inCanaries, inUTC := fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String()
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	m := orders.Compose(sw, nil, nil, orders.WithToday(zones{inMadrid: madrid, inCanaries: canary}))
	ctx := adminContext(t)
	for company, want := range map[string]vocab.Date{inMadrid: vocab.MustDate(2026, 10, 15), inCanaries: vocab.MustDate(2026, 10, 14),
		inUTC: vocab.MustDate(2026, 10, 14)} {
		o, err := m.Service.DraftOrder.Handle(ctx, oapp.DraftOrder{Company: company, Customer: fw.NewUUID().String()})
		if err != nil || o.Date != want.String() {
			t.Fatalf("order of %s: %s %v, want %s", company, o.Date, err, want)
		}
		q, err := m.Service.DraftQuote.Handle(ctx, oapp.DraftQuote{Company: company, Customer: fw.NewUUID().String()})
		if err != nil || q.Date != want.String() || q.ValidUntil != want.AddDays(30).String() {
			t.Fatalf("quote of %s: %s–%s %v, want %s", company, q.Date, q.ValidUntil, err, want)
		}
	}
	// Una fecha explícita se respeta.
	o, err := m.Service.DraftOrder.Handle(ctx, oapp.DraftOrder{Company: inMadrid, Customer: fw.NewUUID().String(), Date: vocab.MustDate(2026, 10, 1)})
	if err != nil || o.Date != "2026-10-01" {
		t.Fatalf("explicit date: %s %v", o.Date, err)
	}
}

// sellAnything hace de Products: cualquier producto se vende a 10, sin almacenar.
type sellAnything struct{}

func (sellAnything) Price(_ context.Context, company odomain.OrganizationID, _ odomain.ProductID, _ odomain.PriceListID, _ vocab.Decimal,
	_ vocab.Date) (odomain.Item, bool, error) {
	return odomain.Item{Company: company, SKU: "SRV", Name: "Servicio", UoM: "ud", TaxCode: "G21", Sellable: true,
		UnitPrice: vocab.DecimalFromInt(10)}, true, nil
}

// Una entrega sin fecha y el pedido que nace al aceptar un presupuesto son del día de la empresa,
// calculado dentro del reintento: el 15 en Madrid, el 14 en Canarias.
func TestOrders_DeliveryAndAcceptanceOnTheCompanysDay(t *testing.T) {
	defer fw.SetClock(fake.New(time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC)))()
	madrid, _ := time.LoadLocation("Europe/Madrid")
	canary, _ := time.LoadLocation("Atlantic/Canary")
	inMadrid, inCanaries := fw.NewUUID().String(), fw.NewUUID().String()
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	m := orders.Compose(sw, sellAnything{}, nil, orders.WithToday(zones{inMadrid: madrid, inCanaries: canary}))
	ctx := adminContext(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for company, want := range map[string]string{inMadrid: "2026-10-15", inCanaries: "2026-10-14"} {
		customer, product := fw.NewUUID().String(), fw.NewUUID().String()
		o, err := m.Service.DraftOrder.Handle(ctx, oapp.DraftOrder{Company: company, Customer: customer, Date: vocab.MustDate(2026, 10, 1)})
		must(err)
		id, _ := odomain.ParseOrderID(o.ID)
		_, err = m.Service.AddLine.Handle(ctx, oapp.AddLine{ID: id, Product: product, Quantity: "2"})
		must(err)
		_, err = m.Service.Confirm.Handle(ctx, oapp.ConfirmOrder{ID: id})
		must(err)
		note, err := m.Service.Deliver.Handle(ctx, oapp.Deliver{ID: id})
		if err != nil || note.Date != want {
			t.Fatalf("delivery of %s: %s %v, want %s", company, note.Date, err, want)
		}

		q, err := m.Service.DraftQuote.Handle(ctx, oapp.DraftQuote{Company: company, Customer: customer, Date: vocab.MustDate(2026, 10, 1)})
		must(err)
		qid, _ := odomain.ParseQuoteID(q.ID)
		_, err = m.Service.AddQuoteLine.Handle(ctx, oapp.AddQuoteLine{ID: qid, Product: product, Quantity: "1"})
		must(err)
		_, err = m.Service.SendQuote.Handle(ctx, oapp.SendQuote{ID: qid})
		must(err)
		accepted, err := m.Service.AcceptQuote.Handle(ctx, oapp.AcceptQuote{ID: qid})
		if err != nil || accepted.Order.Date != want {
			t.Fatalf("order from the quote of %s: %s %v, want %s", company, accepted.Order.Date, err, want)
		}
	}
}
