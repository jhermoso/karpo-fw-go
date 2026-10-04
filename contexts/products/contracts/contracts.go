// Package contracts is what other bounded contexts may depend on: the Published Language of
// Products and its ports (Inventory checks what it stocks, Orders and Billing price and tax what
// they sell, Purchases what it buys).
package contracts

import "context"

// Source is the name of the publishing bounded context.
const Source = "products"

// ProductRef is what other contexts need of a product.
type ProductRef struct {
	ID              string `json:"id"`
	Company         string `json:"company"`
	SKU             string `json:"sku"`
	Name            string `json:"name"`
	Kind            string `json:"kind"` // good | service
	UoM             string `json:"uom"`
	TaxCode         string `json:"taxCode,omitempty"`
	ExpenseCategory string `json:"expenseCategory,omitempty"`
	ForSale         bool   `json:"forSale"`
	ForPurchase     bool   `json:"forPurchase"`
	Stocked         bool   `json:"stocked"`
	Tracking        string `json:"tracking"` // none | lot | serial
	BlockedSales    bool   `json:"blockedSales"`
	BlockedPurchase bool   `json:"blockedPurchase"`
	Discontinued    string `json:"discontinued,omitempty"` // civil date from which it is retired
	StandardCost    string `json:"standardCost"`
}

// MaxBatch is the maximum number of ids per call.
const MaxBatch = 900

// Catalog resolves products by id (unknown ids are absent).
type Catalog interface {
	Products(ctx context.Context, ids []string) (map[string]ProductRef, error)
}

// Quote is the price of a product for a quantity on a date. Amounts are decimal strings with up to
// four decimals; Net is the unit price after the discount.
type Quote struct {
	UnitPrice string `json:"unitPrice"`
	Discount  string `json:"discount"`
	Net       string `json:"net"`
	Source    string `json:"source"` // list | base
}

// Pricing prices a product of a company for a quantity on a civil date, with a price list of the
// company (empty: the base price of the product).
type Pricing interface {
	Quote(ctx context.Context, company, product, priceList, quantity, on string) (Quote, error)
}

// ProductRegisteredV1 is published when a product enters the catalog of a company.
type ProductRegisteredV1 struct {
	ProductID string `json:"productId"`
	Company   string `json:"company"`
	SKU       string `json:"sku"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (ProductRegisteredV1) IntegrationEventType() string { return "products.product-registered.v1" }

// ProductDiscontinuedV1 is published when a product is retired from a civil date.
type ProductDiscontinuedV1 struct {
	ProductID string `json:"productId"`
	Company   string `json:"company"`
	SKU       string `json:"sku"`
	On        string `json:"on"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (ProductDiscontinuedV1) IntegrationEventType() string {
	return "products.product-discontinued.v1"
}
