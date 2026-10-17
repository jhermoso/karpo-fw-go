package application

import "github.com/jhermoso/karpo-fw-go/pkg/application/authz"

// Permisos (los endpoints sectoriales del C# sólo exigían autenticación). El catálogo es el mismo
// para todas las empresas; lo que cada empresa tiene encendido es otra cosa.
var (
	PermCatalogRead      = authz.MustPermission("Modules.Catalog.Read")
	PermCatalogUpdate    = authz.MustPermission("Modules.Catalog.Update")
	PermActivationRead   = authz.MustPermission("Modules.Activation.Read")
	PermActivationUpdate = authz.MustPermission("Modules.Activation.Update")
)

// Permissions devuelve los permisos que este contexto declara al catálogo de Security.
func Permissions() []authz.Permission {
	return []authz.Permission{PermCatalogRead, PermCatalogUpdate, PermActivationRead, PermActivationUpdate}
}
