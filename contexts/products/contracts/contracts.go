// Package contracts es aquello de lo que pueden depender otros contextos delimitados: el Published
// Language de Products y sus puertos (Inventory comprueba lo que almacena, Orders y Billing ponen
// precio e impuestos a lo que venden, Purchases a lo que compra).
package contracts

import "context"

// Source es el nombre del contexto delimitado que publica.
const Source = "products"

// ProductRef es lo que otros contextos necesitan de un producto.
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
	Discontinued    string `json:"discontinued,omitempty"` // fecha civil desde la que está retirado
	StandardCost    string `json:"standardCost"`
}

// MaxBatch es el número máximo de ids por llamada.
const MaxBatch = 900

// Catalog resuelve productos por id (los ids desconocidos no aparecen).
type Catalog interface {
	Products(ctx context.Context, ids []string) (map[string]ProductRef, error)
}

// Quote es el precio de un producto para una cantidad en una fecha. Los importes son cadenas
// decimales de hasta cuatro decimales; Net es el precio unitario tras el descuento.
type Quote struct {
	UnitPrice string `json:"unitPrice"`
	Discount  string `json:"discount"`
	Net       string `json:"net"`
	Source    string `json:"source"` // list | base
}

// Pricing pone precio a un producto de una empresa para una cantidad en una fecha civil, con una
// lista de precios de la empresa (vacía: el precio base del producto).
type Pricing interface {
	Quote(ctx context.Context, company, product, priceList, quantity, on string) (Quote, error)
}

// ProductRegisteredV1 se publica cuando un producto entra en el catálogo de una empresa.
type ProductRegisteredV1 struct {
	ProductID string `json:"productId"`
	Company   string `json:"company"`
	SKU       string `json:"sku"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (ProductRegisteredV1) IntegrationEventType() string { return "products.product-registered.v1" }

// ProductDiscontinuedV1 se publica cuando un producto se retira desde una fecha civil.
type ProductDiscontinuedV1 struct {
	ProductID string `json:"productId"`
	Company   string `json:"company"`
	SKU       string `json:"sku"`
	On        string `json:"on"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (ProductDiscontinuedV1) IntegrationEventType() string {
	return "products.product-discontinued.v1"
}
