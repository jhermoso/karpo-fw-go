package application

import "github.com/jhermoso/karpo-fw-go/pkg/application/authz"

// Permisos (el C# declaraba recursos Shipments.* que ningún endpoint exigía). Preparar un envío y
// moverlo (salida, entrega, devolución, anulación) son permisos distintos.
var (
	PermShipmentRead     = authz.MustPermission("Shipments.Shipment.Read")
	PermShipmentUpdate   = authz.MustPermission("Shipments.Shipment.Update")
	PermShipmentDispatch = authz.MustPermission("Shipments.Shipment.Dispatch")
	PermCarrierRead      = authz.MustPermission("Shipments.Carrier.Read")
	PermCarrierUpdate    = authz.MustPermission("Shipments.Carrier.Update")
)

// Permissions devuelve los permisos que este contexto declara al catálogo de Security.
func Permissions() []authz.Permission {
	return []authz.Permission{PermShipmentRead, PermShipmentUpdate, PermShipmentDispatch, PermCarrierRead, PermCarrierUpdate}
}
