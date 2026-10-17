// Package contracts es aquello de lo que pueden depender otros contextos delimitados: el Published
// Language de Inventario y su puerto de disponibilidad (Orders comprueba lo que puede prometer;
// Accounting valorará la variación de existencias a partir de los movimientos).
package contracts

import "context"

// Source es el nombre del contexto delimitado que publica.
const Source = "inventory"

// StockMovedV1 se publica por cada movimiento del libro de existencias. Quantity y Value llevan
// signo (positivo si entra, negativo si sale); las cantidades y los costes unitarios tienen hasta
// cuatro decimales, Value dos.
type StockMovedV1 struct {
	MovementID string `json:"movementId"`
	Company    string `json:"company"`
	Warehouse  string `json:"warehouse"`
	Product    string `json:"product"`
	Kind       string `json:"kind"` // receipt | issue | adjustment | transfer-out | transfer-in
	Date       string `json:"date"`
	Quantity   string `json:"quantity"`
	UnitCost   string `json:"unitCost"`
	Value      string `json:"value"`
	Balance    string `json:"balance"` // lo que hay en mano en el almacén después del movimiento
	Lot        string `json:"lot,omitempty"`
	SourceType string `json:"sourceType,omitempty"`
	SourceID   string `json:"sourceId,omitempty"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (StockMovedV1) IntegrationEventType() string { return "inventory.stock-moved.v1" }

// StockReservedV1 se publica cuando se retienen existencias para un origen (una línea de pedido):
// Added ahora, Held en total por su reserva. Una petición que no encuentra existencias no publica
// nada.
type StockReservedV1 struct {
	ReservationID string `json:"reservationId"`
	Company       string `json:"company"`
	Warehouse     string `json:"warehouse"`
	Product       string `json:"product"`
	SourceType    string `json:"sourceType"`
	SourceID      string `json:"sourceId"`
	Added         string `json:"added"`
	Held          string `json:"held"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (StockReservedV1) IntegrationEventType() string { return "inventory.stock-reserved.v1" }

// Stock son las existencias de un producto: en un almacén, o en todos los de la empresa.
type Stock struct {
	OnHand    string `json:"onHand"`
	Reserved  string `json:"reserved"`
	Available string `json:"available"`
}

// Availability responde las existencias de un producto de una empresa (warehouse vacío: todos los
// almacenes).
type Availability interface {
	Stock(ctx context.Context, company, product, warehouse string) (Stock, error)
}
