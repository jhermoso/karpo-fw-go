package application

import "github.com/jhermoso/karpo-fw-go/pkg/application/authz"

// Permissions devuelve los permisos que este contexto declara al catálogo de Security.
func Permissions() []authz.Permission {
	return []authz.Permission{
		PermCatalogRead, PermCatalogUpdate, PermTaxpayerRead, PermTaxpayerWrite, PermFilingRead,
		PermFilingCreate, PermFilingSubmit,
	}
}
