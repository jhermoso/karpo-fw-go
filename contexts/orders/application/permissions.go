package application

import "github.com/jhermoso/karpo-fw-go/pkg/application/authz"

// Permissions returns the permissions this context declares to the Security catalog.
func Permissions() []authz.Permission {
	return []authz.Permission{
		PermOrderRead, PermOrderUpdate, PermOrderConfirm, PermOrderDeliver, PermOrderCancel,
		PermTermsRead, PermTermsUpdate,
	}
}
