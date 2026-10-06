package application

import "github.com/jhermoso/karpo-fw-go/pkg/application/authz"

// PermDocumentRead reads the register (the C# document endpoints required authentication only).
// Nothing writes it by hand: entries come from the events of the issuing contexts.
var PermDocumentRead = authz.MustPermission("Documents.Document.Read")

// Permissions returns the permissions this context declares to the Security catalog.
func Permissions() []authz.Permission { return []authz.Permission{PermDocumentRead} }
