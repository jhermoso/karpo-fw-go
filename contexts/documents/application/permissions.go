package application

import "github.com/jhermoso/karpo-fw-go/pkg/application/authz"

// PermDocumentRead lee el registro (los endpoints de documentos del C# sólo exigían autenticación).
// Nada lo escribe a mano: las anotaciones vienen de los eventos de los contextos emisores.
var PermDocumentRead = authz.MustPermission("Documents.Document.Read")

// Permissions devuelve los permisos que este contexto declara al catálogo de Security.
func Permissions() []authz.Permission { return []authz.Permission{PermDocumentRead} }
