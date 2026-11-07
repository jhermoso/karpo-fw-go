package documents_test

import (
	"context"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/documents"
	dapp "github.com/jhermoso/karpo-fw-go/contexts/documents/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// zones hace de anfitrión: la zona de cada empresa; la que no está, UTC.
type zones map[string]*time.Location

func (z zones) DateOf(_ context.Context, company fw.UUID, at time.Time) (vocab.Date, error) {
	if loc := z[company.String()]; loc != nil {
		return vocab.DateOf(at.In(loc)), nil
	}
	return vocab.DateOf(at.UTC()), nil
}

// Un pedido confirmado y una declaración presentada a las 23:30 UTC del 4 de mayo son del día de
// la empresa: el 5 en Kiritimati (UTC+14), el 4 sin zona.
func TestDocuments_DateFromAnEventIsTheCompanysDay(t *testing.T) {
	kiritimati, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Fatal(err)
	}
	early, inUTC := fw.NewUUID().String(), fw.NewUUID().String()
	h := compose(t, documents.WithToday(zones{early: kiritimati}))
	at := time.Date(2026, 5, 4, 23, 30, 0, 0, time.UTC)
	for company, want := range map[string]string{early: "2026-05-05", inUTC: "2026-05-04"} {
		h.grant("viewer", company)
		if err := h.send(fw.NewUUID().String(), "orders.order-confirmed.v1", at, map[string]any{"orderId": fw.NewUUID().String(), "company": company,
			"customer": fw.NewUUID().String(), "number": "PED-2026-000001", "total": "242.00"}); err != nil {
			t.Fatal(err)
		}
		if err := h.send(fw.NewUUID().String(), "fiscal.filing-submitted.v1", at, map[string]any{"filingId": fw.NewUUID().String(), "declarant": company,
			"form": "111", "year": 2026, "period": "1T", "withheld": "300.00"}); err != nil {
			t.Fatal(err)
		}
		var page fw.Page[dapp.DocumentDTO]
		h.must(h.get("/api/documents?company="+company, "viewer", &page), 200, "documents")
		if len(page.Items) != 2 || page.Items[0].Date != want || page.Items[1].Date != want {
			t.Fatalf("documents of %s: %+v, want %s", company, page.Items, want)
		}
	}
}
