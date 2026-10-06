package application

import "github.com/jhermoso/karpo-fw-go/pkg/application/authz"

// Permissions (the C# declared Shipments.* resources no endpoint enforced). Preparing a shipment
// and moving it (dispatch, delivery, return, cancellation) are separate.
var (
	PermShipmentRead     = authz.MustPermission("Shipments.Shipment.Read")
	PermShipmentUpdate   = authz.MustPermission("Shipments.Shipment.Update")
	PermShipmentDispatch = authz.MustPermission("Shipments.Shipment.Dispatch")
	PermCarrierRead      = authz.MustPermission("Shipments.Carrier.Read")
	PermCarrierUpdate    = authz.MustPermission("Shipments.Carrier.Update")
)

// Permissions returns the permissions this context declares to the Security catalog.
func Permissions() []authz.Permission {
	return []authz.Permission{PermShipmentRead, PermShipmentUpdate, PermShipmentDispatch, PermCarrierRead, PermCarrierUpdate}
}
