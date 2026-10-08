package application

import "github.com/jhermoso/karpo-fw-go/pkg/application/authz"

// Permissions (the C# asked for a global administrator to import, and for nothing but a session to
// write references or open and close runs). Executing an import also needs, record by record,
// whatever the context that owns the record asks of any caller.
var (
	PermRunRead       = authz.MustPermission("Imports.Run.Read")
	PermRunExecute    = authz.MustPermission("Imports.Run.Execute")
	PermReferenceRead = authz.MustPermission("Imports.Reference.Read")
)

// Permissions returns the permissions this context declares to the Security catalog.
func Permissions() []authz.Permission {
	return []authz.Permission{PermRunRead, PermRunExecute, PermReferenceRead}
}
