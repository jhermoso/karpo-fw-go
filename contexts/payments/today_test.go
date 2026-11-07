package payments_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/payments"
	papp "github.com/jhermoso/karpo-fw-go/contexts/payments/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/payments/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// zones hace de anfitrión: la zona de cada party; la que no está, UTC.
type zones map[string]*time.Location

func (z zones) Today(_ context.Context, company fw.UUID) (vocab.Date, error) {
	return vocab.Today(z[company.String()]), nil
}

// Una aplicación de pago sin fecha es del día de la empresa que paga, no del beneficiario: el 14
// de octubre a las 22:30 UTC ya es el 15 en Madrid y aún el 14 en Canarias.
func TestPayments_TodayIsThePayingCompanys(t *testing.T) {
	defer fw.SetClock(fake.New(time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC)))()
	madrid, _ := time.LoadLocation("Europe/Madrid")
	canary, _ := time.LoadLocation("Atlantic/Canary")
	inMadrid, inCanaries := fw.NewUUID().String(), fw.NewUUID().String()
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	m := payments.Compose(sw, nil, payments.WithToday(zones{inMadrid: madrid, inCanaries: canary}))
	ac, err := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	if err != nil {
		t.Fatal(err)
	}
	ac.GlobalAdmin = true
	ctx := authz.WithContext(context.Background(), ac)
	// Cada empresa paga a la otra: el beneficiario está en la otra zona.
	for _, c := range []struct{ company, payee, want string }{{inMadrid, inCanaries, "2026-10-15"}, {inCanaries, inMadrid, "2026-10-14"}} {
		invoice := fw.NewUUID().String()
		e := papp.InvoiceRegistered{InvoiceID: invoice, Company: c.company, Supplier: c.payee, SupplierNumber: "P-1", Issued: "2026-10-01",
			Due: "2026-10-31", Payable: "100.00"}
		data, _ := json.Marshal(e)
		if err := m.Consumer.HandleMessage(context.Background(), application.Envelope{ID: fw.NewUUID().String(), Type: e.IntegrationEventType(),
			Source: "purchases", Subject: invoice, OccurredAt: fw.Now(), Data: data}); err != nil {
			t.Fatal(err)
		}
		ps, err := m.Service.SearchPayables.Handle(ctx, papp.SearchPayables{Company: c.company})
		if err != nil || len(ps.Items) != 1 {
			t.Fatalf("payables of %s: %+v %v", c.company, ps, err)
		}
		p, err := m.Service.RegisterPayment.Handle(ctx, papp.RegisterPayment{Company: c.company, Payee: c.payee, Date: vocab.MustDate(2026, 10, 10),
			Amount: "100", Method: "transfer"})
		if err != nil {
			t.Fatal(err)
		}
		id, _ := pdomain.ParsePaymentID(p.ID)
		p, err = m.Service.Allocate.Handle(ctx, papp.Allocate{ID: id, AllocationInput: papp.AllocationInput{Payable: ps.Items[0].ID, Amount: "100"}})
		if err != nil || len(p.Allocations) != 1 || p.Allocations[0].On != c.want {
			t.Fatalf("allocation of %s: %+v %v, want %s", c.company, p.Allocations, err, c.want)
		}
	}
}
