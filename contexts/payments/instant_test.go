package payments_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/payments"
	papp "github.com/jhermoso/karpo-fw-go/contexts/payments/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

// Un modelo presentado a las 23:30 UTC del 14 de octubre se emite (y, fuera de plazo, vence) el
// día del declarante: el 15 en Kiritimati (UTC+14), el 14 sin zona.
func TestPayments_TaxIssuedOnTheDeclarantsDay(t *testing.T) {
	kiritimati, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Fatal(err)
	}
	early, inUTC := fw.NewUUID().String(), fw.NewUUID().String()
	sw := hotswap.New(memory.NewStore("memory"))
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	m := payments.Compose(sw, nil, payments.WithToday(zones{early: kiritimati}))
	ac, err := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	if err != nil {
		t.Fatal(err)
	}
	ac.GlobalAdmin = true
	ctx := authz.WithContext(context.Background(), ac)
	at := time.Date(2026, 10, 14, 23, 30, 0, 0, time.UTC)
	for company, want := range map[string]string{early: "2026-10-15", inUTC: "2026-10-14"} {
		e := papp.FilingSubmitted{FilingID: fw.NewUUID().String(), Declarant: company, Form: "111", Year: 2026, Period: "2T", Withheld: "300.00"}
		data, _ := json.Marshal(e)
		if err := m.Consumer.HandleMessage(context.Background(), application.Envelope{ID: fw.NewUUID().String(), Type: e.IntegrationEventType(),
			Source: "fiscal", Subject: e.FilingID, OccurredAt: at, Data: data}); err != nil {
			t.Fatal(err)
		}
		ps, err := m.Service.SearchPayables.Handle(ctx, papp.SearchPayables{Company: company})
		if err != nil || len(ps.Items) != 1 || ps.Items[0].Issued != want || ps.Items[0].Due != want {
			t.Fatalf("payables of %s: %+v %v, want issued and due %s", company, ps.Items, err, want)
		}
	}
}
