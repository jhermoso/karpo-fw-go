// Package contracts es aquello de lo que pueden depender otros contextos delimitados: el Published
// Language de Shipments.
package contracts

// Source es el nombre del contexto delimitado que publica.
const Source = "shipments"

// ShipmentDispatchedV1 se publica cuando sale un envío. SourceType/SourceID apuntan al documento
// que expide (orders.delivery y el albarán); SourceRef es su número.
type ShipmentDispatchedV1 struct {
	ShipmentID string `json:"shipmentId"`
	Company    string `json:"company"`
	Customer   string `json:"customer"`
	SourceType string `json:"sourceType,omitempty"`
	SourceID   string `json:"sourceId,omitempty"`
	SourceRef  string `json:"sourceRef,omitempty"`
	Method     string `json:"method"` // truck | courier | rail | air | ocean | pickup
	Carrier    string `json:"carrier,omitempty"`
	Tracking   string `json:"tracking,omitempty"`
	Packages   int    `json:"packages"`
	Date       string `json:"date"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (ShipmentDispatchedV1) IntegrationEventType() string { return "shipments.shipment-dispatched.v1" }

// ShipmentDeliveredV1 se publica cuando un envío llega a su destinatario.
type ShipmentDeliveredV1 struct {
	ShipmentID string `json:"shipmentId"`
	Company    string `json:"company"`
	Customer   string `json:"customer"`
	SourceType string `json:"sourceType,omitempty"`
	SourceID   string `json:"sourceId,omitempty"`
	SourceRef  string `json:"sourceRef,omitempty"`
	Date       string `json:"date"`
	ReceivedBy string `json:"receivedBy,omitempty"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (ShipmentDeliveredV1) IntegrationEventType() string { return "shipments.shipment-delivered.v1" }

// ShipmentReturnedV1 se publica cuando un envío vuelve sin entregar.
type ShipmentReturnedV1 struct {
	ShipmentID string `json:"shipmentId"`
	Company    string `json:"company"`
	Customer   string `json:"customer"`
	SourceType string `json:"sourceType,omitempty"`
	SourceID   string `json:"sourceId,omitempty"`
	SourceRef  string `json:"sourceRef,omitempty"`
	Date       string `json:"date"`
	Reason     string `json:"reason"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (ShipmentReturnedV1) IntegrationEventType() string { return "shipments.shipment-returned.v1" }
