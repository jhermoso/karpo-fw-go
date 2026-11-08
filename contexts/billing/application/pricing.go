package application

import (
	"context"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// errPricingUnavailable rechaza una línea con producto cuando Billing no tiene los puertos que la
// necesita: Products siempre, y Orders con el precio automático.
var errPricingUnavailable = fw.Violation("billing.automatic_pricing_unavailable",
	"a line with a product needs the products and prices of Products and the terms of Orders, and Billing has not got them")

// withProduct completa una línea con producto de un borrador (D-148, docs/FACTURACION.md, «Precio
// automático»). El producto tiene que ser del vendedor; en una factura ordinaria, además, a la venta
// y no retirado en la fecha de la factura. Si la línea no los trae, la descripción es el nombre del
// producto y el código de impuesto, el suyo.
//
// El precio es automático salvo que la empresa lo escriba a mano (automatic_pricing a false), la
// línea traiga su precio o la factura sea una rectificativa (que corrige lo que se facturó, no lo que
// costaría hoy): entonces el precio es el escrito, obligatorio, y el descuento el escrito o
// ninguno. Si es automático, Products da el precio con la cadena completa (el especial del cliente,
// su tarifa de Orders, la general de la empresa o el precio base), redondeado a los decimales de la
// factura (D-144), y el descuento del nivel que gana se encadena con el del cliente en Orders; un
// descuento escrito sustituye a ese encadenado.
func (s service) withProduct(ctx context.Context, i *domain.Invoice, in *domain.LineInput, priceWritten, discountWritten bool) error {
	if s.Catalog == nil {
		return errPricingUnavailable
	}
	st := i.State()
	p, ok, err := s.Catalog.Product(ctx, in.Product)
	if err != nil {
		return err
	}
	if !ok || p.Company != st.Seller {
		return fw.Violation("billing.unknown_product", "the product is not in the catalog of the seller")
	}
	on, err := s.lineDate(ctx, st)
	if err != nil {
		return err
	}
	if st.Kind == domain.Ordinary {
		if !p.Retired.IsZero() && !on.Before(p.Retired) {
			return fw.Violation("billing.product_discontinued", "the product is discontinued on the date of the invoice")
		}
		if !p.Sellable {
			return fw.Violation("billing.product_not_for_sale", "the product is not for sale or its sales are blocked")
		}
	}
	if strings.TrimSpace(in.Description) == "" {
		in.Description = p.Name
	}
	if strings.TrimSpace(in.TaxCode) == "" && strings.TrimSpace(in.Treatment) == "" {
		in.TaxCode = p.TaxCode
	}
	settings, err := s.settings(ctx, st.Seller)
	if err != nil {
		return err
	}
	if priceWritten || settings.ManualPricing || st.Kind == domain.Corrective {
		if !priceWritten {
			var v fw.Validation
			v.Add("unitPrice", "required", "the price of the line is written: automatic pricing is off for the company, or the invoice is corrective")
			return v.Err()
		}
		in.PriceSource = domain.PriceManual
		return nil
	}
	if s.Terms == nil {
		return errPricingUnavailable
	}
	if !in.Quantity.IsPositive() {
		return nil // la cantidad la rechaza la factura, antes de pedir ningún precio
	}
	terms, err := s.Terms.Terms(ctx, st.Seller, st.Customer)
	if err != nil {
		return err
	}
	price, err := s.Catalog.Price(ctx, domain.PriceRequest{Company: st.Seller, Customer: st.Customer, Product: in.Product,
		PriceList: terms.PriceList, Quantity: in.Quantity, On: on})
	if err != nil {
		return err
	}
	in.UnitPrice, in.PriceSource = price.UnitPrice.Round(int32(st.Calculation.PriceDecimals)), price.Source
	if !discountWritten {
		// La columna del descuento tiene 4 decimales: dos descuentos de 2 decimales encadenados pueden
		// dar hasta 6 (12,5 y 7,25 dan 18,84375 → 18,8438).
		in.Discount = vocab.ChainedDiscount(price.Discount, terms.Discount).Round(4)
	}
	return nil
}

// lineDate es la fecha con la que se pone precio a una línea: la de la operación del borrador o, si
// no tiene, hoy en la zona horaria del vendedor.
func (s service) lineDate(ctx context.Context, st domain.InvoiceState) (vocab.Date, error) {
	if !st.OperationDate.IsZero() {
		return st.OperationDate, nil
	}
	seller := domain.PartyID{UUID: st.Seller.UUID}
	country := ""
	if s.Zones != nil {
		ids, err := s.Identities.Identities(ctx, []domain.PartyID{seller})
		if err != nil {
			return vocab.Date{}, err
		}
		country = ids[seller].Country
	}
	return s.today(ctx, seller, country)
}
