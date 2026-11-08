package infrastructure

import (
	"context"
	"fmt"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	orders "github.com/jhermoso/karpo-fw-go/contexts/orders/contracts"
	products "github.com/jhermoso/karpo-fw-go/contexts/products/contracts"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// ProductsCatalog adapta los contratos Catalog y Pricing de Products al puerto Catalog de Billing
// (ACL, D-148).
type ProductsCatalog struct {
	Catalog products.Catalog
	Pricing products.Pricing
}

var _ domain.Catalog = ProductsCatalog{}

// Product implementa domain.Catalog.
func (p ProductsCatalog) Product(ctx context.Context, id domain.ProductID) (domain.Product, bool, error) {
	refs, err := p.Catalog.Products(ctx, []string{id.String()})
	if err != nil {
		return domain.Product{}, false, err
	}
	r, ok := refs[id.String()]
	if !ok {
		return domain.Product{}, false, nil
	}
	owner, err := fw.ParseUUID(r.Company)
	if err != nil {
		return domain.Product{}, false, fmt.Errorf("billing: invalid product from Products: %+v", r)
	}
	out := domain.Product{Company: domain.OrganizationID{UUID: owner}, Name: r.Name, TaxCode: r.TaxCode, Sellable: r.ForSale && !r.BlockedSales}
	if r.Discontinued != "" {
		if out.Retired, err = vocab.ParseDate(r.Discontinued); err != nil {
			return domain.Product{}, false, fmt.Errorf("billing: invalid product from Products: %+v", r)
		}
	}
	return out, true, nil
}

// Price implementa domain.Catalog.
func (p ProductsCatalog) Price(ctx context.Context, r domain.PriceRequest) (domain.Price, error) {
	customer := ""
	if !r.Customer.IsZero() {
		customer = r.Customer.String()
	}
	q, err := p.Pricing.Quote(ctx, products.QuoteRequest{Company: r.Company.String(), Customer: customer, Product: r.Product.String(),
		PriceList: r.PriceList, Quantity: r.Quantity.String(), On: r.On.String()})
	if err != nil {
		return domain.Price{}, err
	}
	price, err1 := vocab.ParseDecimal(q.UnitPrice)
	discount := vocab.DecimalFromInt(0)
	var err2 error
	if q.Discount != "" {
		discount, err2 = vocab.ParseDecimal(q.Discount)
	}
	if err1 != nil || err2 != nil {
		return domain.Price{}, fmt.Errorf("billing: invalid quote from Products: %+v", q)
	}
	return domain.Price{UnitPrice: price, Discount: discount, Source: q.Source}, nil
}

// OrdersTerms adapta el contrato CustomerTerms de Orders al puerto CustomerTerms de Billing (ACL,
// D-148): Billing no lee las tablas de Orders.
type OrdersTerms struct{ Customers orders.CustomerTerms }

var _ domain.CustomerTerms = OrdersTerms{}

// Terms implementa domain.CustomerTerms.
func (o OrdersTerms) Terms(ctx context.Context, company domain.OrganizationID, customer domain.PartyID) (domain.Terms, error) {
	t, err := o.Customers.Terms(ctx, company.String(), customer.String())
	if err != nil {
		return domain.Terms{}, err
	}
	out := domain.Terms{PriceList: t.PriceList, Discount: vocab.DecimalFromInt(0), Rounding: domain.LineRounding(t.LineRounding),
		PriceDecimals: t.PriceDecimals}
	if t.Discount != "" {
		if out.Discount, err = vocab.ParseDecimal(t.Discount); err != nil {
			return domain.Terms{}, fmt.Errorf("billing: invalid terms from Orders: %+v", t)
		}
	}
	return out, nil
}
