// Package domain es el modelo del contexto delimitado Security: los usuarios y cómo se autentican
// (contraseña, identidades externas), los roles y los permisos que llevan, el acceso a
// organizaciones, las sesiones de refresco, el catálogo de permisos y las reglas de la
// administración delegada. Es la traducción a Go del subdominio Security de ErpKernel en C# (ver
// docs/SEGURIDAD.md): la autenticación dice quién llama; este contexto dice qué puede hacer quien
// llama y sobre qué organizaciones.
package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// UserID identifica un usuario de seguridad (los mismos GUID que IdSecurityUser en C#).
type UserID struct{ fw.UUID }

// NewUserID devuelve una identidad nueva.
func NewUserID() UserID { return UserID{fw.NewUUID()} }

// ParseUserID interpreta una identidad en texto.
func ParseUserID(s string) (UserID, error) {
	u, err := fw.ParseUUID(s)
	return UserID{u}, err
}

// MustUserID interpreta una identidad conocida.
func MustUserID(s string) UserID { return UserID{fw.MustParseUUID(s)} }

// RoleID identifica un rol (los mismos GUID que IdSecurityRole en C# para los sembrados).
type RoleID struct{ fw.UUID }

// NewRoleID devuelve una identidad nueva.
func NewRoleID() RoleID { return RoleID{fw.NewUUID()} }

// ParseRoleID interpreta una identidad en texto.
func ParseRoleID(s string) (RoleID, error) {
	u, err := fw.ParseUUID(s)
	return RoleID{u}, err
}

// MustRoleID interpreta una identidad conocida.
func MustRoleID(s string) RoleID { return RoleID{fw.MustParseUUID(s)} }

// SessionID identifica una sesión de refresco.
type SessionID struct{ fw.UUID }

// NewSessionID devuelve una identidad nueva.
func NewSessionID() SessionID { return SessionID{fw.NewUUID()} }

// PartyID referencia, por identidad, la party de Parties que hay detrás de un usuario.
type PartyID struct{ fw.UUID }

// OrganizationID referencia, por identidad, una organización interna (una party de Parties).
type OrganizationID struct{ fw.UUID }

// ParseOrganizationID interpreta una identidad en texto.
func ParseOrganizationID(s string) (OrganizationID, error) {
	u, err := fw.ParseUUID(s)
	return OrganizationID{u}, err
}
