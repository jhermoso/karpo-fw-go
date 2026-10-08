package application

import "github.com/jhermoso/karpo-fw-go/pkg/application/authz"

// Permissions returns the permissions this context declares to the Security catalog.
func Permissions() []authz.Permission {
	return []authz.Permission{
		PermPayslipRead, PermPayslipCreate, PermPayslipUpdate, PermPayslipApprove, PermProfileRead,
		PermProfileUpdate, PermAccountRead, PermAccountCreate, PermAccountUpdate, PermCatalogRead,
		PermRemittanceRead,
	}
}
