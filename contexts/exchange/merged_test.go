package exchange_test

import (
	"context"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/exchange"
	xapp "github.com/jhermoso/karpo-fw-go/contexts/exchange/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// mergedParties hace de Parties: dice qué parties están fusionadas en otra.
type mergedParties struct{ ids []string }

func (m *mergedParties) MergedParties(context.Context) ([]string, error) { return m.ids, nil }

// El listado de reservas deja fuera por defecto las de un cliente fusionado en otra party, y las
// trae con includeMergedParties; la reserva pedida por su identidad se sigue encontrando
// (docs/DECISIONES-COMUNES.md, D-100).
func TestReservationsLeaveOutMergedParties(t *testing.T) {
	merged := &mergedParties{}
	h := compose(t, exchange.WithMergedParties(merged))
	acme := fw.NewUUID().String()
	for _, u := range []string{"pricer", "clerk", "viewer"} {
		h.grant(u, acme)
	}
	h.must(h.do("PUT", "/api/exchange/currencies", "pricer", map[string]any{"company": acme, "code": "USD", "name": "Dólar", "facial": "5"}, nil), 200, "USD")
	h.must(h.do("POST", "/api/exchange/rates", "pricer", map[string]any{"company": acme, "code": "USD", "rate": "0.92"}, nil), 200, "rate")
	ana, bea := fw.NewUUID().String(), fw.NewUUID().String()
	merged.ids = []string{bea}

	pickup := fw.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	ids := map[string]string{}
	for _, c := range []string{ana, bea} {
		var res xapp.ReservationDTO
		h.must(h.do("POST", "/api/exchange/reservations", "clerk", map[string]any{"company": acme, "customer": c, "channel": "web",
			"pickup": pickup.Format(time.RFC3339), "lines": []map[string]any{{"currency": "USD", "amount": "100"}}}, &res), 201, "reservation")
		ids[c] = res.ID
	}

	customers := func(query string) []string {
		t.Helper()
		var page fw.Page[xapp.ReservationDTO]
		h.must(h.do("GET", "/api/exchange/reservations?company="+acme+query, "viewer", nil, &page), 200, "reservations"+query)
		out := []string{}
		for _, it := range page.Items {
			out = append(out, it.Customer)
		}
		return out
	}
	if got := customers(""); len(got) != 1 || got[0] != ana {
		t.Fatalf("reservations: %v, want only %s", got, ana)
	}
	if got := customers("&includeMergedParties=true"); len(got) != 2 {
		t.Fatalf("reservations with includeMergedParties: %v", got)
	}
	h.must(h.do("GET", "/api/exchange/reservations/"+ids[bea], "viewer", nil, nil), 200, "the reservation of a merged party, by id")
}
