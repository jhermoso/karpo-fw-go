// Package contracts es aquello de lo que pueden depender los demás contextos delimitados: el
// Published Language de Orders (Inventory retiene, da salida y libera las existencias de los
// pedidos; Billing facturará los albaranes).
package contracts

import "context"

// Source es el nombre del contexto delimitado que publica.
const Source = "orders"

// TermsRef son las condiciones de venta de una empresa con un cliente que necesitan otros
// contextos: su tarifa (el id de una tarifa de Products; "" si no tiene) y su descuento (un
// porcentaje con hasta dos decimales; "0" si no tiene).
type TermsRef struct {
	PriceList string `json:"priceList,omitempty"`
	Discount  string `json:"discount"`
}

// CustomerTerms dice las condiciones de venta de una empresa con un cliente; las de cero si no
// tiene. Billing las usa para poner precio a la línea de una factura directa (D-148): la tarifa
// entra en la cadena de Products y el descuento se encadena encima. Sólo lectura; sirve a otros
// contextos, sin los permisos del usuario.
type CustomerTerms interface {
	Terms(ctx context.Context, company, customer string) (TermsRef, error)
}

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

// DeliveryLine es una línea de un albarán. UnitPrice (hasta cuatro decimales) y Discount (el
// descuento que se aplica, porcentaje con hasta cuatro decimales) son los de una línea de factura:
// Amount = Quantity × UnitPrice × (1 − Discount / 100), al céntimo, una sola vez (D-149). Los dos
// llegaron después: un albarán de antes no los lleva, y quien los lee factura entonces NetPrice
// sin descuento. NetPrice (cuatro decimales) es informativo y sigue estando para quien ya lo lea;
// Amount tiene dos.
type DeliveryLine struct {
	Line        int    `json:"line"`
	Product     string `json:"product"`
	SKU         string `json:"sku"`
	Description string `json:"description"`
	UoM         string `json:"uom"`
	TaxCode     string `json:"taxCode,omitempty"`
	Stocked     bool   `json:"stocked"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unitPrice,omitempty"`
	Discount    string `json:"discount,omitempty"`
	NetPrice    string `json:"netPrice"`
	Amount      string `json:"amount"`
}

// DeliveryIssuedV1 se publica al emitir un albarán: Inventory da salida a sus líneas con
// existencias contra las reservas del pedido; Billing lo factura. Total es antes de impuestos.
// BillingPeriodicity es la del cliente al emitirlo (D-146): per_delivery, Billing prepara su
// borrador; monthly, lo deja pendiente hasta el cierre del periodo. Falta en los de antes.
type DeliveryIssuedV1 struct {
	DeliveryID         string         `json:"deliveryId"`
	OrderID            string         `json:"orderId"`
	OrderNumber        string         `json:"orderNumber"`
	Company            string         `json:"company"`
	Customer           string         `json:"customer"`
	Warehouse          string         `json:"warehouse,omitempty"`
	Number             string         `json:"number"`
	Date               string         `json:"date"`
	BillingPeriodicity string         `json:"billingPeriodicity,omitempty"`
	Total              string         `json:"total"`
	Lines              []DeliveryLine `json:"lines"`
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
