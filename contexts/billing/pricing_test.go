package billing_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/billing"
	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	bdomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// catalog es, como Products en el anfitrión, el catálogo y la cadena de precios: aquí, un precio
// por cliente, con el nivel del que sale. Apunta lo que se le pide.
type catalog struct {
	mu       sync.Mutex
	products map[bdomain.ProductID]bdomain.Product
	prices   map[bdomain.PartyID]bdomain.Price
	asked    []bdomain.PriceRequest
}

func (c *catalog) Product(_ context.Context, id bdomain.ProductID) (bdomain.Product, bool, error) {
	p, ok := c.products[id]
	return p, ok, nil
}

func (c *catalog) Price(_ context.Context, r bdomain.PriceRequest) (bdomain.Price, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, r)
	return c.prices[r.Customer], nil
}

func (c *catalog) requests() []bdomain.PriceRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]bdomain.PriceRequest(nil), c.asked...)
}

// customerTerms son, como en Orders, la tarifa y el descuento de cada cliente.
type customerTerms map[bdomain.PartyID]bdomain.Terms

func (t customerTerms) Terms(_ context.Context, _ bdomain.OrganizationID, customer bdomain.PartyID) (bdomain.Terms, error) {
	if x, ok := t[customer]; ok {
		return x, nil
	}
	return bdomain.Terms{Discount: vocab.DecimalFromInt(0)}, nil
}

// El precio automático de la línea con producto de una factura directa (D-148), con Products y
// Orders de mentira: un caso por nivel de la cadena, con la tarifa del cliente y la fecha de la
// operación en la petición; el descuento del cliente encadenado; los decimales de la factura; el
// precio y el descuento escritos, que mandan; el precio a mano de la empresa; el producto ajeno,
// desconocido, retirado o bloqueado (422); la línea sin producto, como siempre; y sin puertos, 422.
func TestBilling_AutomaticPricing(t *testing.T) {
	seller, other := bdomain.OrganizationID{UUID: fw.NewUUID()}, bdomain.OrganizationID{UUID: fw.NewUUID()}
	tornillo, ajeno, retirado, bloqueado := bdomain.ProductID{UUID: fw.NewUUID()}, bdomain.ProductID{UUID: fw.NewUUID()},
		bdomain.ProductID{UUID: fw.NewUUID()}, bdomain.ProductID{UUID: fw.NewUUID()}
	special, listed, general, base := bdomain.PartyID{UUID: fw.NewUUID()}, bdomain.PartyID{UUID: fw.NewUUID()}, bdomain.PartyID{UUID: fw.NewUUID()},
		bdomain.PartyID{UUID: fw.NewUUID()}
	priceList := fw.NewUUID().String()
	cat := &catalog{
		products: map[bdomain.ProductID]bdomain.Product{
			tornillo:  {Company: seller, Name: "Tornillo M8", TaxCode: "G21", Sellable: true},
			ajeno:     {Company: other, Name: "Ajeno", TaxCode: "G21", Sellable: true},
			retirado:  {Company: seller, Name: "Retirado", TaxCode: "G21", Sellable: true, Retired: vocab.MustDate(2026, 10, 1)},
			bloqueado: {Company: seller, Name: "Bloqueado", TaxCode: "G21"},
		},
		prices: map[bdomain.PartyID]bdomain.Price{
			special: {UnitPrice: vocab.MustDecimal("2"), Discount: vocab.MustDecimal("5"), Source: bdomain.PriceSpecial},
			listed:  {UnitPrice: vocab.MustDecimal("1.8"), Discount: vocab.DecimalFromInt(0), Source: bdomain.PriceCustomerList},
			general: {UnitPrice: vocab.MustDecimal("1.9"), Discount: vocab.DecimalFromInt(0), Source: bdomain.PriceDefaultList},
			base:    {UnitPrice: vocab.MustDecimal("0.125"), Discount: vocab.DecimalFromInt(0), Source: bdomain.PriceBase},
		},
	}
	terms := customerTerms{special: {Discount: vocab.MustDecimal("10")}, listed: {PriceList: priceList, Discount: vocab.DecimalFromInt(0)}}
	conf := &settings{by: map[string]bapp.CompanySettings{}}
	h := compose(t, billing.WithPricing(cat, terms), billing.WithSettings(conf))
	actx, bs := h.adminCtx, h.billing.Service

	draft := func(customer bdomain.PartyID) bdomain.InvoiceID {
		t.Helper()
		inv, err := bs.DraftInvoice.Handle(actx, bapp.DraftInvoice{Seller: seller.String(), Customer: customer.String(),
			DetailsInput: bapp.DetailsInput{Description: "Venta directa", OperationDate: vocab.MustDate(2026, 10, 15)}})
		h.ok(err)
		id, _ := bdomain.ParseInvoiceID(inv.ID)
		return id
	}
	line := func(customer bdomain.PartyID, c bapp.AddLine) bapp.LineDTO {
		t.Helper()
		c.ID = draft(customer)
		inv, err := bs.AddLine.Handle(actx, c)
		h.ok(err)
		return inv.Lines[0]
	}

	// Un caso por nivel de la cadena: precio, descuento del nivel encadenado con el del cliente,
	// nivel, producto, descripción y código de impuesto del producto.
	for _, c := range []struct {
		customer             bdomain.PartyID
		price, discount, net string
		source               string
	}{
		{special, "2", "14.50", "17.10", bdomain.PriceSpecial}, // 10 × 2 × 0,95 × 0,90
		{listed, "1.8", "", "18.00", bdomain.PriceCustomerList},
		{general, "1.9", "", "19.00", bdomain.PriceDefaultList},
		{base, "0.125", "", "1.25", bdomain.PriceBase},
	} {
		l := line(c.customer, bapp.AddLine{Product: tornillo.String(), Quantity: "10"})
		if l.UnitPrice != c.price || l.Discount != c.discount || l.Net != c.net || l.PriceSource != c.source || l.Product != tornillo.String() ||
			l.TaxCode != "G21" || l.Description != "Tornillo M8" {
			t.Fatalf("%s: %+v", c.source, l)
		}
	}
	asked := cat.requests()
	if len(asked) != 4 {
		t.Fatalf("one price per line: %+v", asked)
	}
	if r := asked[1]; r.Company != seller || r.Customer != listed || r.Product != tornillo || r.PriceList != priceList ||
		r.Quantity.String() != "10" || r.On != vocab.MustDate(2026, 10, 15) {
		t.Fatalf("the request has the list of the customer and the date of the operation: %+v", r)
	}

	// Los decimales del precio de la factura (D-144) se aplican al precio obtenido: 0.125 con 2, 0.13.
	conf.set(seller.String(), bapp.CompanySettings{Calculation: bdomain.Calculation{Rounding: bdomain.RoundOnce, PriceDecimals: 2}})
	if l := line(base, bapp.AddLine{Product: tornillo.String(), Quantity: "10"}); l.UnitPrice != "0.13" || l.Net != "1.30" {
		t.Fatalf("the price decimals of the invoice: %+v", l)
	}
	conf.set(seller.String(), bapp.CompanySettings{})

	// Lo escrito manda: con precio, el precio y el descuento escritos (ninguno, si no se escribe);
	// con sólo el descuento, el precio automático y el descuento escrito en lugar del encadenado.
	if l := line(special, bapp.AddLine{Product: tornillo.String(), Quantity: "10", UnitPrice: "1.5"}); l.UnitPrice != "1.5" || l.Discount != "" ||
		l.Net != "15.00" || l.PriceSource != bdomain.PriceManual || l.TaxCode != "G21" {
		t.Fatalf("a written price: %+v", l)
	}
	if l := line(special, bapp.AddLine{Product: tornillo.String(), Quantity: "10", Discount: "3", Description: "Tornillo a granel"}); l.UnitPrice != "2" ||
		l.Discount != "3.00" || l.Net != "19.40" || l.PriceSource != bdomain.PriceSpecial || l.Description != "Tornillo a granel" {
		t.Fatalf("a written discount: %+v", l)
	}

	// automatic_pricing a false: el precio se escribe, y el producto da la descripción y el impuesto.
	conf.set(seller.String(), bapp.CompanySettings{ManualPricing: true})
	before := len(cat.requests())
	if l := line(special, bapp.AddLine{Product: tornillo.String(), Quantity: "1", UnitPrice: "7"}); l.UnitPrice != "7" || l.Net != "7.00" ||
		l.PriceSource != bdomain.PriceManual || l.TaxCode != "G21" || l.Description != "Tornillo M8" {
		t.Fatalf("manual pricing: %+v", l)
	}
	if _, err := bs.AddLine.Handle(actx, bapp.AddLine{ID: draft(special), Product: tornillo.String(), Quantity: "1"}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("manual pricing without a price: %v", err)
	}
	if len(cat.requests()) != before {
		t.Fatal("manual pricing asks Products no price")
	}
	conf.set(seller.String(), bapp.CompanySettings{})

	// Sin producto, como siempre: ni producto, ni nivel, ni precio pedido.
	if l := line(general, bapp.AddLine{Description: "Portes", Quantity: "1", UnitPrice: "5", TaxCode: "G21"}); l.Product != "" || l.PriceSource != "" ||
		l.Net != "5.00" || len(cat.requests()) != before {
		t.Fatalf("a line without a product: %+v", l)
	}

	// Producto ajeno, desconocido, retirado en la fecha de la factura o bloqueado: 422.
	h.grant("clerk", seller.String())
	path := "/api/billing/invoices/" + draft(general).String() + "/lines"
	for product, code := range map[string]string{ajeno.String(): "billing.unknown_product", fw.NewUUID().String(): "billing.unknown_product",
		retirado.String(): "billing.product_discontinued", bloqueado.String(): "billing.product_not_for_sale"} {
		h.refused("POST", path, "clerk", map[string]string{"product": product, "quantity": "1"}, code, "product "+code)
	}
	var out bapp.InvoiceDTO
	h.must(h.do("POST", path, "clerk", map[string]string{"product": tornillo.String(), "quantity": "2"}, &out), 200, "a line with a product by HTTP")
	if len(out.Lines) != 1 || out.Lines[0].PriceSource != bdomain.PriceDefaultList || out.Lines[0].Net != "3.80" {
		t.Fatalf("by HTTP: %+v", out.Lines)
	}

	// Sin los puertos, la línea con producto no se puede completar: 422; la de siempre, sí.
	bare := compose(t)
	inv, err := bare.billing.Service.DraftInvoice.Handle(bare.adminCtx, bapp.DraftInvoice{Seller: seller.String(), Customer: general.String()})
	bare.ok(err)
	id, _ := bdomain.ParseInvoiceID(inv.ID)
	var rv *fw.RuleViolationError
	if _, err := bare.billing.Service.AddLine.Handle(bare.adminCtx, bapp.AddLine{ID: id, Product: tornillo.String(), Quantity: "1"}); !errors.As(err, &rv) ||
		rv.Code != "billing.automatic_pricing_unavailable" {
		t.Fatalf("without the ports: %v", err)
	}
	_, err = bare.billing.Service.AddLine.Handle(bare.adminCtx, bapp.AddLine{ID: id, Description: "Portes", Quantity: "1", UnitPrice: "5", TaxCode: "G21"})
	bare.ok(err)
}
