// Package contracts es aquello de lo que pueden depender los demás contextos delimitados: el
// Published Language de Orders (Inventory retiene, da salida y libera las existencias de los
// pedidos; Billing facturará los albaranes).
package contracts

// Source es el nombre del contexto delimitado que publica.
const Source = "orders"

// OrderConfirmedV1 se publica al confirmar un pedido de venta. Total es antes de impuestos.
type OrderConfirmedV1 struct {
	OrderID  string `json:"orderId"`
	Company  string `json:"company"`
	Customer string `json:"customer"`
	Number   string `json:"number"`
	Total    string `json:"total"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (OrderConfirmedV1) IntegrationEventType() string { return "orders.order-confirmed.v1" }

// StockLine es una cantidad de una línea de pedido que se retiene.
type StockLine struct {
	Line     int    `json:"line"`
	Product  string `json:"product"`
	Quantity string `json:"quantity"`
}

// StockRequestedV1 se publica cuando un pedido pide retener existencias: al confirmarse y cada vez
// que vuelve a pedir lo que aún le falta. Inventory retiene lo disponible y responde con
// inventory.stock-reserved.v1 (tipo de origen orders.sales-order, id de origen "order|line").
type StockRequestedV1 struct {
	OrderID   string      `json:"orderId"`
	Company   string      `json:"company"`
	Warehouse string      `json:"warehouse"`
	Lines     []StockLine `json:"lines"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (StockRequestedV1) IntegrationEventType() string { return "orders.stock-requested.v1" }

// OrderClosedV1 se publica cuando un pedido se anula o se cierra con parte pendiente: lo que aún
// retienen sus líneas se libera. Status es cancelled o closed.
type OrderClosedV1 struct {
	OrderID string `json:"orderId"`
	Company string `json:"company"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Lines   []int  `json:"lines"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (OrderClosedV1) IntegrationEventType() string { return "orders.order-closed.v1" }

// DeliveryLine es una línea de un albarán. NetPrice tiene hasta cuatro decimales; Amount, dos.
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

// DeliveryIssuedV1 se publica al emitir un albarán: Inventory da salida a sus líneas con
// existencias contra las reservas del pedido. Total es antes de impuestos.
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

// IntegrationEventType implementa application.IntegrationEvent.
func (DeliveryIssuedV1) IntegrationEventType() string { return "orders.delivery-issued.v1" }

// QuoteSentV1 se publica al enviar un presupuesto a un cliente, con su número. Date y ValidUntil
// son días (YYYY-MM-DD); Total es antes de impuestos.
type QuoteSentV1 struct {
	QuoteID    string `json:"quoteId"`
	Company    string `json:"company"`
	Customer   string `json:"customer"`
	Number     string `json:"number"`
	Date       string `json:"date"`
	ValidUntil string `json:"validUntil"`
	Total      string `json:"total"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (QuoteSentV1) IntegrationEventType() string { return "orders.quote-sent.v1" }

// QuoteClosedV1 se publica cuando un presupuesto enviado se acepta (OrderID es el pedido en
// borrador en que se convirtió), se rechaza, se retira o caduca.
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

// IntegrationEventType implementa application.IntegrationEvent.
func (QuoteClosedV1) IntegrationEventType() string { return "orders.quote-closed.v1" }
