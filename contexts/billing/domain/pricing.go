package domain

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// De dónde sale el precio de una línea con producto (D-148): un nivel de la cadena de Products, o
// escrito a mano. Una línea sin producto no lo dice.
const (
	PriceSpecial      = "special"       // el precio especial del cliente para ese producto
	PriceCustomerList = "customer_list" // la tarifa del cliente (sus condiciones en Orders)
	PriceDefaultList  = "default_list"  // la tarifa general de la empresa
	PriceBase         = "base"          // el precio base del producto
	PriceManual       = "manual"        // escrito en la línea
)

var priceSources = []string{PriceSpecial, PriceCustomerList, PriceDefaultList, PriceBase, PriceManual}

// Product es lo que Billing necesita de un producto para facturarlo sin pedido.
type Product struct {
	Company  OrganizationID
	Name     string
	TaxCode  string
	Sellable bool       // a la venta y no bloqueado
	Retired  vocab.Date // cero si no está retirado
}

// PriceRequest es lo que se pide para poner precio a una línea: la empresa vendedora, el cliente,
// el producto, la tarifa del cliente ("" si no tiene), la cantidad y la fecha de la factura.
type PriceRequest struct {
	Company   OrganizationID
	Customer  PartyID
	Product   ProductID
	PriceList string
	Quantity  vocab.Decimal
	On        vocab.Date
}

// Price es el precio de un producto: el precio unitario y el descuento del nivel que gana, y cuál
// es (PriceSpecial, PriceCustomerList, PriceDefaultList o PriceBase).
type Price struct {
	UnitPrice vocab.Decimal
	Discount  vocab.Decimal
	Source    string
}

// Catalog resuelve un producto y le pone precio con la cadena completa de Products (un puerto
// propio de Billing sobre los contratos Catalog y Pricing de Products). Un producto desconocido se
// señala con ok false.
type Catalog interface {
	Product(ctx context.Context, id ProductID) (p Product, ok bool, err error)
	Price(ctx context.Context, r PriceRequest) (Price, error)
}

// Terms son las condiciones de venta de la empresa con un cliente: su tarifa ("" si no tiene) y su
// descuento (cero si no tiene).
type Terms struct {
	PriceList string
	Discount  vocab.Decimal
	// Rounding y PriceDecimals son el cálculo de las facturas del cliente cuando no es el de la
	// empresa (la cascada cliente > empresa de D-144, D-150); vacío y nil, el de la empresa.
	Rounding      LineRounding
	PriceDecimals *int
}

// Over es el cálculo de las facturas del cliente: el de la empresa con lo que el cliente cambia.
func (t Terms) Over(company Calculation) Calculation {
	if t.Rounding != "" {
		company.Rounding = t.Rounding
	}
	if t.PriceDecimals != nil {
		company.PriceDecimals = *t.PriceDecimals
	}
	return company
}

// CustomerTerms dice las condiciones de venta de una empresa con un cliente (un puerto propio de
// Billing sobre el contrato CustomerTerms de Orders): las de cero si no tiene.
type CustomerTerms interface {
	Terms(ctx context.Context, company OrganizationID, customer PartyID) (Terms, error)
}
