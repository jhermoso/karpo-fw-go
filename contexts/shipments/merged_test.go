package shipments_test

import (
	"context"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/shipments"
	sapp "github.com/jhermoso/karpo-fw-go/contexts/shipments/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// mergedParties hace de Parties: dice qué parties están fusionadas en otra.
type mergedParties struct{ ids []string }

func (m *mergedParties) MergedParties(context.Context) ([]string, error) { return m.ids, nil }

// El listado de envíos deja fuera por defecto los de un cliente fusionado en otra party y los
// trae con includeMergedParties; el envío pedido por su identidad se sigue encontrando
// (docs/DECISIONES-COMUNES.md, D-64 y D-100).
func TestShipmentsLeaveOutMergedParties(t *testing.T) {
	merged := &mergedParties{}
	h := compose(t, shipments.WithMergedParties(merged))
	id := func() string { return fw.NewUUID().String() }
	acme, ana, bea := id(), id(), id()
	h.grant("viewer", acme)
	merged.ids = []string{bea}

	goods := []map[string]any{{"line": 1, "sku": "TOR-01", "description": "Tornillos", "uom": "ud", "stocked": true, "quantity": "10"}}
	h.deliveryNote("merged-1", id(), acme, ana, "ALB-2026-000001", goods)
	h.deliveryNote("merged-2", id(), acme, bea, "ALB-2026-000002", goods)

	var page fw.Page[sapp.ShipmentDTO]
	path := "/api/shipments?company=" + acme
	h.must(h.do("GET", path, "viewer", nil, &page), 200, "shipments")
	if len(page.Items) != 1 || page.Items[0].Customer != ana {
		t.Fatalf("shipments: %+v, want only %s", page.Items, ana)
	}
	h.must(h.do("GET", path+"&includeMergedParties=true", "viewer", nil, &page), 200, "shipments")
	if len(page.Items) != 2 {
		t.Fatalf("shipments with includeMergedParties: %+v", page.Items)
	}
	var beas string
	for _, s := range page.Items {
		if s.Customer == bea {
			beas = s.ID
		}
	}
	h.must(h.do("GET", "/api/shipments/"+beas, "viewer", nil, nil), 200, "the shipment of a merged party, by id")
}
