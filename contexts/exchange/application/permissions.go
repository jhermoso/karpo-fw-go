package application

import "github.com/jhermoso/karpo-fw-go/pkg/application/authz"

// Permisos (los endpoints de reservas del C# sólo exigían autenticación). Fijar cambios y márgenes,
// tomar reservas y hacerlas avanzar van por separado. Cotizar tiene permiso propio (D-137, D-141):
// sólo lo recibe un rol propio al que lo asigne el administrador (ver
// AssignedOnlyPermissions de Security).
var (
	PermPricingRead         = authz.MustPermission("Exchange.Pricing.Read")
	PermPricingUpdate       = authz.MustPermission("Exchange.Pricing.Update")
	PermReservationRead     = authz.MustPermission("Exchange.Reservation.Read")
	PermReservationCreate   = authz.MustPermission("Exchange.Reservation.Create")
	PermReservationProgress = authz.MustPermission("Exchange.Reservation.Progress")
	PermQuoteRead           = authz.MustPermission("Exchange.Quote.Read")
)

// Permissions devuelve los permisos que este contexto declara al catálogo de Security.
func Permissions() []authz.Permission {
	return []authz.Permission{PermPricingRead, PermPricingUpdate, PermReservationRead, PermReservationCreate, PermReservationProgress, PermQuoteRead}
}
