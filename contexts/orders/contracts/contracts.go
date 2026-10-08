// Package contracts is what other bounded contexts may depend on: the Published Language of
// Orders (Inventory holds, issues and releases the stock of orders; Billing will invoice the
// delivery notes).
package contracts

// Source is the name of the publishing bounded context.
const Source = "orders"

// OrderConfirmedV1 is published when a sales order is confirmed. Total is before taxes.
type OrderConfirmedV1 struct {
	OrderID  string `json:"orderId"`
	Company  string `json:"company"`
	Customer string `json:"customer"`
	Number   string `json:"number"`
	Total    string `json:"total"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (OrderConfirmedV1) IntegrationEventType() string { return "orders.order-confirmed.v1" }

// StockLine is a quantity of an order line to hold.
type StockLine struct {
	Line     int    `json:"line"`
	Product  string `json:"product"`
	Quantity string `json:"quantity"`
}

// StockRequestedV1 is published when an order asks to hold stock: on confirmation and each time
// it asks again for what it is still short of. Inventory holds what is available and answers with
// inventory.stock-reserved.v1 (source type orders.sales-order, source id "order|line").
type StockRequestedV1 struct {
	OrderID   string      `json:"orderId"`
	Company   string      `json:"company"`
	Warehouse string      `json:"warehouse"`
	Lines     []StockLine `json:"lines"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (StockRequestedV1) IntegrationEventType() string { return "orders.stock-requested.v1" }

// OrderClosedV1 is published when an order is cancelled or closed with part pending: what its
// lines still hold is released. Status is cancelled or closed.
type OrderClosedV1 struct {
	OrderID string `json:"orderId"`
	Company string `json:"company"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Lines   []int  `json:"lines"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (OrderClosedV1) IntegrationEventType() string { return "orders.order-closed.v1" }

// DeliveryLine is a line of a delivery note. NetPrice has up to four decimals; Amount two.
type DeliveryLine struct {
	Line        int    `json:"line"`
	Product     string `json:"product"`
	SKU         string `json:"sku"`
	Description string `json:"description"`
	UoM         string `json:"uom"`
	TaxCode     string `json:"taxCode,omitempty"`
	Stocked     bool   `json:"stocked"`
	Quantity    string `json:"quantity"`
	NetPrice    string `json:"netPrice"`
	Amount      string `json:"amount"`
}

// DeliveryIssuedV1 is published when a delivery note is issued: Inventory issues its stocked
// lines against the reservations of the order. Total is before taxes.
type DeliveryIssuedV1 struct {
	DeliveryID  string         `json:"deliveryId"`
	OrderID     string         `json:"orderId"`
	OrderNumber string         `json:"orderNumber"`
	Company     string         `json:"company"`
	Customer    string         `json:"customer"`
	Warehouse   string         `json:"warehouse,omitempty"`
	Number      string         `json:"number"`
	Date        string         `json:"date"`
	Total       string         `json:"total"`
	Lines       []DeliveryLine `json:"lines"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (DeliveryIssuedV1) IntegrationEventType() string { return "orders.delivery-issued.v1" }

// QuoteSentV1 is published when a quote is sent to a customer, with its number. Date and
// ValidUntil are days (YYYY-MM-DD); Total is before taxes.
type QuoteSentV1 struct {
	QuoteID    string `json:"quoteId"`
	Company    string `json:"company"`
	Customer   string `json:"customer"`
	Number     string `json:"number"`
	Date       string `json:"date"`
	ValidUntil string `json:"validUntil"`
	Total      string `json:"total"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (QuoteSentV1) IntegrationEventType() string { return "orders.quote-sent.v1" }

// QuoteClosedV1 is published when a sent quote is accepted (OrderID is the draft order it
// became), rejected, withdrawn or expires.
type QuoteClosedV1 struct {
	QuoteID  string `json:"quoteId"`
	Company  string `json:"company"`
	Customer string `json:"customer"`
	Number   string `json:"number"`
	Status   string `json:"status"` // accepted | rejected | withdrawn | expired
	Reason   string `json:"reason,omitempty"`
	OrderID  string `json:"orderId,omitempty"`
	Total    string `json:"total"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (QuoteClosedV1) IntegrationEventType() string { return "orders.quote-closed.v1" }
