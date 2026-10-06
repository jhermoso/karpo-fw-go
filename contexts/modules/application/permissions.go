package application

import "github.com/jhermoso/karpo-fw-go/pkg/application/authz"

// Permissions (the C# sectorial endpoints required authentication only). The catalog is the same
// for every company; what each company has on is another matter.
var (
	PermCatalogRead      = authz.MustPermission("Modules.Catalog.Read")
	PermCatalogUpdate    = authz.MustPermission("Modules.Catalog.Update")
	PermActivationRead   = authz.MustPermission("Modules.Activation.Read")
	PermActivationUpdate = authz.MustPermission("Modules.Activation.Update")
)

// Permissions returns the permissions this context declares to the Security catalog.
func Permissions() []authz.Permission {
	return []authz.Permission{PermCatalogRead, PermCatalogUpdate, PermActivationRead, PermActivationUpdate}
}
