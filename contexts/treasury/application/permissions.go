package application

import "github.com/jhermoso/karpo-fw-go/pkg/application/authz"

// Permissions returns the permissions this context declares to the Security catalog.
func Permissions() []authz.Permission {
	return []authz.Permission{
		PermAccountRead, PermAccountUpdate, PermMandateRead, PermMandateUpdate, PermRemittanceRead,
		PermRemittanceEdit, PermRemittanceSend, PermRemittanceBank, PermTransferRead, PermTransferEdit,
		PermTransferSend, PermTransferBank, PermStatementRead, PermStatementImport, PermStatementReconcile,
	}
}
