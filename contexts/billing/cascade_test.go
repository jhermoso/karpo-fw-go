package billing_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/billing"
	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	bdomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// La cascada cliente > empresa del cálculo de la factura (D-144, D-150): lo que el cliente fija en
// sus condiciones gana a lo de la empresa, campo a campo, y lo que no fija sigue a la empresa. Vale
// para la factura directa, el borrador de un albarán (que sigue llevando al menos 4 decimales) y el
// pendiente de un cliente mensual; y se congela al preparar el borrador: cambiar después las
// condiciones no toca los borradores hechos. 1 × 0,125 con un 50 %: 0,06 redondeando una vez, 0,07
// redondeando antes el bruto.
func TestBilling_CustomerCalculationOverTheCompany(t *testing.T) {
	terms := customerTerms{}
	conf := &settings{by: map[string]bapp.CompanySettings{}}
	h := compose(t, billing.WithPricing(&catalog{}, terms), billing.WithSettings(conf))
	acme, ana := h.seller(t)
	bs, actx := h.billing.Service, h.adminCtx
	u, err := fw.ParseUUID(ana)
	h.ok(err)
	customer := bdomain.PartyID{UUID: u}
	conf.set(acme, bapp.CompanySettings{Calculation: bdomain.Calculation{Rounding: bdomain.RoundGrossThenDiscount, PriceDecimals: 4}})

	direct := func() bapp.InvoiceDTO {
		t.Helper()
		inv, err := bs.DraftInvoice.Handle(actx, bapp.DraftInvoice{Seller: acme, Customer: ana, DetailsInput: bapp.DetailsInput{Description: "Tornillos"}})
		h.ok(err)
		id, _ := bdomain.ParseInvoiceID(inv.ID)
		inv, err = bs.AddLine.Handle(actx, bapp.AddLine{ID: id, Description: "Tornillo", Quantity: "1", UnitPrice: "0.12", Discount: "50", TaxCode: "G21"})
		h.ok(err)
		return inv
	}

	// Sin nada en sus condiciones, el cliente sigue a la empresa.
	if d := direct(); d.LineRounding != "gross_then_discount" || d.PriceDecimals != 4 {
		t.Fatalf("without terms of its own, the company's calculation: %+v", d)
	}
	// Sólo los decimales: el redondeo sigue siendo el de la empresa.
	two := 2
	terms[customer] = bdomain.Terms{Discount: vocab.DecimalFromInt(0), PriceDecimals: &two}
	if d := direct(); d.LineRounding != "gross_then_discount" || d.PriceDecimals != 2 {
		t.Fatalf("only the price decimals of the customer: %+v", d)
	}
	// Los dos.
	terms[customer] = bdomain.Terms{Discount: vocab.DecimalFromInt(0), Rounding: bdomain.RoundOnce, PriceDecimals: &two}
	frozen := direct()
	if frozen.LineRounding != "once" || frozen.PriceDecimals != 2 {
		t.Fatalf("the calculation of the customer: %+v", frozen)
	}

	// El albarán: el redondeo del cliente y al menos 4 decimales.
	send := func(number, periodicity string) {
		t.Helper()
		line := map[string]any{"sku": "T", "description": "Tornillo", "taxCode": "G21", "quantity": "1", "unitPrice": "0.1250", "discount": "50.0000",
			"netPrice": "0.0625", "amount": "0.06"}
		data, _ := json.Marshal(map[string]any{"deliveryId": fw.NewUUID().String(), "orderNumber": "P-" + number, "company": acme, "customer": ana,
			"number": number, "date": "2026-10-03", "billingPeriodicity": periodicity, "lines": []map[string]any{line}})
		h.ok(h.billing.Consumer.HandleMessage(context.Background(), application.Envelope{ID: fw.NewUUID().String(), Type: "orders.delivery-issued.v1",
			Source: "orders", Data: data}))
	}
	send("A-1", "per_delivery")
	var delivery bapp.InvoiceDTO
	for _, d := range h.drafts(acme) {
		if d.SourceRef == "A-1" {
			delivery = d
		}
	}
	if delivery.LineRounding != "once" || delivery.PriceDecimals != 4 || delivery.Net != "0.06" {
		t.Fatalf("the draft of a delivery note, with the rounding of the customer: %+v", delivery)
	}

	// El pendiente de un cliente mensual se calcula como lo calculará el cierre.
	send("M-1", "monthly")
	pending, err := bs.SearchPendingDeliveries.Handle(actx, bapp.SearchPendingDeliveries{Seller: acme, Customer: ana})
	if err != nil || len(pending.Items) != 1 || pending.Items[0].Net != "0.06" {
		t.Fatalf("the pending delivery note, with the rounding of the customer: %+v %v", pending, err)
	}

	// Sin sus condiciones, vuelve la empresa; el borrador ya hecho se queda como estaba.
	delete(terms, customer)
	if d := direct(); d.LineRounding != "gross_then_discount" || d.PriceDecimals != 4 {
		t.Fatalf("back to the company: %+v", d)
	}
	id, _ := bdomain.ParseInvoiceID(frozen.ID)
	got, err := bs.GetInvoice.Handle(actx, bapp.GetInvoice{ID: id})
	if err != nil || got.LineRounding != "once" || got.PriceDecimals != 2 {
		t.Fatalf("a draft keeps the calculation it was prepared with: %+v %v", got, err)
	}
	pending, err = bs.SearchPendingDeliveries.Handle(actx, bapp.SearchPendingDeliveries{Seller: acme, Customer: ana})
	if err != nil || len(pending.Items) != 1 || pending.Items[0].Net != "0.07" {
		t.Fatalf("the pending delivery note follows the company again: %+v %v", pending, err)
	}
}
