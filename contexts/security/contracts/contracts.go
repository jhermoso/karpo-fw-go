// Package contracts es aquello de lo que pueden depender otros contextos delimitados y anfitriones:
// el Published Language de Security (eventos de integración versionados), su directorio de
// usuarios y el puerto por el que se conecta un proveedor de identidad externo. Ningún evento lleva
// contraseñas, hashes ni tokens.
//
// El puerto principal que ofrece Security no se declara aquí: es el authz.Directory del framework,
// implementado por el módulo de Security, que el resolvedor de autorización genérico lee en cada
// petición.
package contracts

import (
	"context"
	"time"
)

// Source es el nombre del contexto delimitado que publica (Envelope.Source).
const Source = "security"

// MaxBatch es el número máximo de ids por llamada al directorio.
const MaxBatch = 900

// UserRef es lo que otros contextos necesitan de un usuario.
type UserRef struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	PartyID  string `json:"partyId"`
	Active   bool   `json:"active"`
}

// Users resuelve usuarios por lotes. Los usuarios que no existen no aparecen.
type Users interface {
	// Resolve devuelve los usuarios con esos ids, por id.
	Resolve(ctx context.Context, userIDs []string) (map[string]UserRef, error)
	// ByParty devuelve los usuarios de esas parties, por id de party.
	ByParty(ctx context.Context, partyIDs []string) (map[string][]UserRef, error)
}

// ExternalIdentity es quién dice un proveedor de identidad externo que es quien llama: el emisor
// del token y el sujeto dentro de ese emisor.
type ExternalIdentity struct {
	Issuer  string
	Subject string
}

// TokenVerifier valida el token de un proveedor de identidad externo (firma, emisor, audiencia,
// vigencia) y devuelve la identidad que afirma. Lo implementa un adaptador por proveedor (OIDC con
// claves públicas); Security sólo vincula la identidad con uno de sus usuarios.
type TokenVerifier interface {
	Verify(ctx context.Context, credentials string) (ExternalIdentity, error)
}

// UserRegisteredV1 se publica cuando se da de alta un usuario.
type UserRegisteredV1 struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	PartyID  string `json:"partyId"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (UserRegisteredV1) IntegrationEventType() string { return "security.user-registered.v1" }

// UserDeactivatedV1 se publica cuando se desactiva un usuario: ya no puede iniciar sesión.
type UserDeactivatedV1 struct {
	UserID string `json:"userId"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (UserDeactivatedV1) IntegrationEventType() string { return "security.user-deactivated.v1" }

// UserReactivatedV1 se publica cuando se reactiva un usuario.
type UserReactivatedV1 struct {
	UserID string `json:"userId"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (UserReactivatedV1) IntegrationEventType() string { return "security.user-reactivated.v1" }

// UserLockedV1 se publica cuando demasiados logins fallidos bloquean a un usuario.
type UserLockedV1 struct {
	UserID string    `json:"userId"`
	Until  time.Time `json:"until"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (UserLockedV1) IntegrationEventType() string { return "security.user-locked.v1" }

// UserPasswordChangedV1 se publica cuando cambia la contraseña de un usuario.
type UserPasswordChangedV1 struct {
	UserID string `json:"userId"`
	Reset  bool   `json:"reset"` // fijada por un administrador
}

// IntegrationEventType implementa application.IntegrationEvent.
func (UserPasswordChangedV1) IntegrationEventType() string {
	return "security.user-password-changed.v1"
}

// UserRoleAssignedV1 se publica cuando se asigna un rol a un usuario.
type UserRoleAssignedV1 struct {
	UserID string `json:"userId"`
	RoleID string `json:"roleId"`
	Role   string `json:"role"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (UserRoleAssignedV1) IntegrationEventType() string { return "security.user-role-assigned.v1" }

// UserRoleRevokedV1 se publica cuando se retira un rol a un usuario.
type UserRoleRevokedV1 struct {
	UserID string `json:"userId"`
	RoleID string `json:"roleId"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (UserRoleRevokedV1) IntegrationEventType() string { return "security.user-role-revoked.v1" }

// OrganizationAccessGrantedV1 se publica cuando un usuario recibe acceso a una organización.
type OrganizationAccessGrantedV1 struct {
	UserID       string `json:"userId"`
	Organization string `json:"organization"`
	Level        string `json:"level"` // Full | ReadOnly | Restricted
}

// IntegrationEventType implementa application.IntegrationEvent.
func (OrganizationAccessGrantedV1) IntegrationEventType() string {
	return "security.organization-access-granted.v1"
}

// OrganizationAccessChangedV1 se publica cuando cambia el nivel de acceso de un usuario.
type OrganizationAccessChangedV1 struct {
	UserID       string `json:"userId"`
	Organization string `json:"organization"`
	Level        string `json:"level"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (OrganizationAccessChangedV1) IntegrationEventType() string {
	return "security.organization-access-changed.v1"
}

// OrganizationAccessRevokedV1 se publica cuando un usuario pierde el acceso a una organización.
type OrganizationAccessRevokedV1 struct {
	UserID       string `json:"userId"`
	Organization string `json:"organization"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (OrganizationAccessRevokedV1) IntegrationEventType() string {
	return "security.organization-access-revoked.v1"
}

// IdentityLinkedV1 se publica cuando una identidad externa se vincula con un usuario.
type IdentityLinkedV1 struct {
	UserID string `json:"userId"`
	Issuer string `json:"issuer"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (IdentityLinkedV1) IntegrationEventType() string { return "security.identity-linked.v1" }

// IdentityUnlinkedV1 se publica cuando una identidad externa se desvincula de un usuario.
type IdentityUnlinkedV1 struct {
	UserID string `json:"userId"`
	Issuer string `json:"issuer"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (IdentityUnlinkedV1) IntegrationEventType() string { return "security.identity-unlinked.v1" }

// RoleDefinedV1 se publica cuando se define un rol propio.
type RoleDefinedV1 struct {
	RoleID      string   `json:"roleId"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (RoleDefinedV1) IntegrationEventType() string { return "security.role-defined.v1" }

// RolePermissionsChangedV1 se publica cuando cambian los permisos de un rol.
type RolePermissionsChangedV1 struct {
	RoleID      string   `json:"roleId"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (RolePermissionsChangedV1) IntegrationEventType() string {
	return "security.role-permissions-changed.v1"
}

// RoleRetiredV1 se publica cuando se retira un rol propio.
type RoleRetiredV1 struct {
	RoleID string `json:"roleId"`
	Name   string `json:"name"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (RoleRetiredV1) IntegrationEventType() string { return "security.role-retired.v1" }
