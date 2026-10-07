package application

import "github.com/jhermoso/karpo-fw-go/pkg/application/authz"

// Permissions (the C# reservation endpoints required authentication only). Setting rates and
// margins, taking reservations and moving them are separate.
var (
	PermPricingRead         = authz.MustPermission("Exchange.Pricing.Read")
	PermPricingUpdate       = authz.MustPermission("Exchange.Pricing.Update")
	PermReservationRead     = authz.MustPermission("Exchange.Reservation.Read")
	PermReservationCreate   = authz.MustPermission("Exchange.Reservation.Create")
	PermReservationProgress = authz.MustPermission("Exchange.Reservation.Progress")
)

// Permissions returns the permissions this context declares to the Security catalog.
func Permissions() []authz.Permission {
	return []authz.Permission{PermPricingRead, PermPricingUpdate, PermReservationRead, PermReservationCreate, PermReservationProgress}
}
