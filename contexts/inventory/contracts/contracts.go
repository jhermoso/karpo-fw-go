// Package contracts is what other bounded contexts may depend on: the Published Language of
// Inventory and its availability port (Orders checks what it can promise; Accounting will value
// the stock variation from the movements).
package contracts

import "context"

// Source is the name of the publishing bounded context.
const Source = "inventory"

// StockMovedV1 is published for each movement of the stock ledger. Quantity and Value are signed
// (positive in, negative out); quantities and unit costs have up to four decimals, Value two.
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
	Balance    string `json:"balance"` // on hand in the warehouse after the movement
	Lot        string `json:"lot,omitempty"`
	SourceType string `json:"sourceType,omitempty"`
	SourceID   string `json:"sourceId,omitempty"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (StockMovedV1) IntegrationEventType() string { return "inventory.stock-moved.v1" }

// Stock is the stock of a product: in a warehouse, or in all those of the company.
type Stock struct {
	OnHand    string `json:"onHand"`
	Reserved  string `json:"reserved"`
	Available string `json:"available"`
}

// Availability answers the stock of a product of a company (warehouse empty: all of them).
type Availability interface {
	Stock(ctx context.Context, company, product, warehouse string) (Stock, error)
}
