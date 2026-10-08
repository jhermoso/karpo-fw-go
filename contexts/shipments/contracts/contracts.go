// Package contracts is what other bounded contexts may depend on: the Published Language of
// Shipments.
package contracts

// Source is the name of the publishing bounded context.
const Source = "shipments"

// ShipmentDispatchedV1 is published when a shipment leaves. SourceType/SourceID point at the
// document it dispatches (orders.delivery and the delivery note), SourceRef is its number.
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

// IntegrationEventType implements application.IntegrationEvent.
func (ShipmentDispatchedV1) IntegrationEventType() string { return "shipments.shipment-dispatched.v1" }

// ShipmentDeliveredV1 is published when a shipment reaches its recipient.
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

// IntegrationEventType implements application.IntegrationEvent.
func (ShipmentDeliveredV1) IntegrationEventType() string { return "shipments.shipment-delivered.v1" }

// ShipmentReturnedV1 is published when a shipment comes back undelivered.
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

// IntegrationEventType implements application.IntegrationEvent.
func (ShipmentReturnedV1) IntegrationEventType() string { return "shipments.shipment-returned.v1" }
