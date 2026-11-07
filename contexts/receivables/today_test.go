package receivables_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/receivables"
	rapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	rdomain "github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
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

// Una aplicación de cobro sin fecha es del día del vendedor, y lo vencido también se cuenta en ese
// día: el 14 de octubre a las 22:30 UTC ya es el 15 en Madrid, pero aún el 14 en Canarias (el
// vendedor que guarda Atlantic/Canary) y en UTC (sin zona).
func TestReceivables_TodayIsTheSellers(t *testing.T) {
	defer fw.SetClock(fake.New(time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC)))()
	madrid, _ := time.LoadLocation("Europe/Madrid")
	canary, _ := time.LoadLocation("Atlantic/Canary")
	inMadrid, inCanaries, inUTC := fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String()
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	m := receivables.Compose(sw, nil, receivables.WithToday(zones{inMadrid: madrid, inCanaries: canary}))
	ctx := adminContext(t)
	for seller, want := range map[string]vocab.Date{inMadrid: vocab.MustDate(2026, 10, 15), inCanaries: vocab.MustDate(2026, 10, 14),
		inUTC: vocab.MustDate(2026, 10, 14)} {
		customer, invoice := fw.NewUUID().String(), fw.NewUUID().String()
		// Una factura que vence el 14: el 15 ya está vencida.
		e := rapp.InvoiceIssued{InvoiceID: invoice, Number: "FA-1", Seller: seller, Customer: customer, IssueDate: "2026-10-01", DueDate: "2026-10-14",
			Currency: "EUR", Total: "100.00"}
		data, _ := json.Marshal(e)
		if err := m.Consumer.HandleMessage(context.Background(), application.Envelope{ID: fw.NewUUID().String(), Type: e.IntegrationEventType(),
			Source: "billing", Subject: invoice, OccurredAt: fw.Now(), Data: data}); err != nil {
			t.Fatal(err)
		}
		// El riesgo sin fecha: por el puerto que usa Orders y por el caso de uso de la API.
		port, err := m.Credit.Exposure(ctx, seller, customer, "")
		if err != nil {
			t.Fatal(err)
		}
		api, err := m.Service.GetExposure.Handle(ctx, rapp.GetExposure{Seller: seller, Customer: customer})
		if err != nil {
			t.Fatal(err)
		}
		for _, exp := range []string{port.Overdue, api.Overdue} {
			overdue, _ := vocab.ParseDecimal(exp)
			if overdue.IsZero() != (want == vocab.MustDate(2026, 10, 14)) {
				t.Fatalf("overdue for %s on %s: %s", seller, want, exp)
			}
		}

		col, err := m.Service.RegisterCollection.Handle(ctx, rapp.RegisterCollection{Seller: seller, Payer: customer, Date: vocab.MustDate(2026, 10, 10),
			Amount: "100", Method: "transfer"})
		if err != nil {
			t.Fatal(err)
		}
		id, _ := rdomain.ParseCollectionID(col.ID)
		col, err = m.Service.Allocate.Handle(ctx, rapp.Allocate{ID: id, AllocationInput: rapp.AllocationInput{Invoice: invoice, Installment: 1, Amount: "100"}})
		if err != nil || len(col.Allocations) != 1 || col.Allocations[0].On != want.String() {
			t.Fatalf("allocation of %s: %+v %v, want %s", seller, col.Allocations, err, want)
		}
	}
}
