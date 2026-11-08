package orders_test

import (
	"testing"

	oapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	odomain "github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	dapp "github.com/jhermoso/karpo-fw-go/contexts/products/application"
	ddomain "github.com/jhermoso/karpo-fw-go/contexts/products/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// TestOrders_SpecialPriceWithCustomerDiscount comprueba D-148 desde Orders: el precio especial del
// cliente gana a su tarifa (aunque ésta tenga un tramo por cantidad más barato) y el descuento del
// cliente se encadena encima del descuento del precio especial, en pedidos y en presupuestos.
func TestOrders_SpecialPriceWithCustomerDiscount(t *testing.T) {
	h := compose(t)
	ctx := h.adminCtx
	acme, err := h.parties.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme",
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	ana, err := h.parties.Service.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "Núñez"})
	h.ok(err)
	luis, err := h.parties.Service.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Luis", FirstSurname: "Pérez"})
	h.ok(err)
	tornillo, err := h.prod.Service.RegisterProduct.Handle(ctx, dapp.RegisterProduct{Company: acme.ID, SKU: "TOR-M8", DetailsInput: dapp.DetailsInput{
		Name: "Tornillo M8", Kind: "good", UoM: "ea", TaxCode: "G21", BasePrice: "0.125", ForSale: true}})
	h.ok(err)
	list, err := h.prod.Service.CreatePriceList.Handle(ctx, dapp.CreatePriceList{Company: acme.ID, Code: "MAYOR", Name: "Mayoristas"})
	h.ok(err)
	lid, _ := ddomain.ParsePriceListID(list.ID)
	_, err = h.prod.Service.SetPrice.Handle(ctx, dapp.SetPrice{ID: lid, Product: tornillo.ID, MinQuantity: "1000", UnitPrice: "0.07", From: vocab.MustDate(2026, 1, 1)})
	h.ok(err)
	_, err = h.prod.Service.SetSpecialPrice.Handle(ctx, dapp.SetSpecialPrice{Company: acme.ID, Customer: ana.ID, Product: tornillo.ID, UnitPrice: "0.09",
		Discount: "5", From: vocab.MustDate(2026, 1, 1)})
	h.ok(err)
	for _, c := range []string{ana.ID, luis.ID} {
		_, err = h.ord.Service.SetTerms.Handle(ctx, oapp.SetTerms{Company: acme.ID, Customer: c, PriceList: list.ID, Discount: "10"})
		h.ok(err)
	}

	order := func(customer string) oapp.OrderDTO {
		o, err := h.ord.Service.DraftOrder.Handle(ctx, oapp.DraftOrder{Company: acme.ID, Customer: customer, Date: vocab.MustDate(2026, 10, 4)})
		h.ok(err)
		oid, _ := odomain.ParseOrderID(o.ID)
		o, err = h.ord.Service.AddLine.Handle(ctx, oapp.AddLine{ID: oid, Product: tornillo.ID, Quantity: "2000"})
		h.ok(err)
		return o
	}
	// Ana: 0.09 × 0.95 × 0.90 = 0.07695 → 0.0770 (4 decimales); 2000 × 0.0770 = 153.90.
	if o := order(ana.ID); o.Lines[0].UnitPrice != "0.0900" || o.Lines[0].Discount != "5.00" || o.CustomerDiscount != "10.00" ||
		o.Lines[0].NetPrice != "0.0770" {
		t.Fatalf("special price with the customer discount: %+v", o.Lines[0])
	}
	// Luis, sin precio especial: el tramo de su tarifa, 0.07 × 0.90 = 0.063.
	if o := order(luis.ID); o.Lines[0].UnitPrice != "0.0700" || o.Lines[0].NetPrice != "0.0630" {
		t.Fatalf("customer list: %+v", o.Lines[0])
	}

	q, err := h.ord.Service.DraftQuote.Handle(ctx, oapp.DraftQuote{Company: acme.ID, Customer: ana.ID, Date: vocab.MustDate(2026, 10, 4)})
	h.ok(err)
	qid, _ := odomain.ParseQuoteID(q.ID)
	q, err = h.ord.Service.AddQuoteLine.Handle(ctx, oapp.AddQuoteLine{ID: qid, Product: tornillo.ID, Quantity: "2000"})
	h.ok(err)
	if q.Lines[0].UnitPrice != "0.0900" || q.Lines[0].Discount != "5.00" || q.Lines[0].NetPrice != "0.0770" {
		t.Fatalf("quote with a special price: %+v", q.Lines[0])
	}
}
