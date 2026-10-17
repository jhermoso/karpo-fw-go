package domain

import (
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// UserRegistered se emite cuando se da de alta un usuario.
type UserRegistered struct {
	fw.EventMeta
	Username string `json:"username"`
	Party    string `json:"party"`
}

// EventType implementa domain.Event.
func (UserRegistered) EventType() string { return "security.user_registered" }

// UserRenamed se emite cuando cambia el nombre de usuario.
type UserRenamed struct {
	fw.EventMeta
	Username string `json:"username"`
}

// EventType implementa domain.Event.
func (UserRenamed) EventType() string { return "security.user_renamed" }

// UserActivationChanged se emite cuando un usuario se desactiva o se reactiva.
type UserActivationChanged struct {
	fw.EventMeta
	Active bool `json:"active"`
}

// EventType implementa domain.Event.
func (UserActivationChanged) EventType() string { return "security.user_activation_changed" }

// PasswordChanged se emite cuando cambia la contraseña. Nunca lleva la contraseña ni su hash.
type PasswordChanged struct {
	fw.EventMeta
	Reset bool `json:"reset"` // fijada por un administrador
}

// EventType implementa domain.Event.
func (PasswordChanged) EventType() string { return "security.password_changed" }

// UserLoggedIn se emite en un login con contraseña correcto (sólo auditoría; no se publica).
type UserLoggedIn struct{ fw.EventMeta }

// EventType implementa domain.Event.
func (UserLoggedIn) EventType() string { return "security.user_logged_in" }

// LoginFailed se emite en una comprobación de contraseña fallida (sólo auditoría; no se publica).
type LoginFailed struct {
	fw.EventMeta
	Attempts int `json:"attempts"`
}

// EventType implementa domain.Event.
func (LoginFailed) EventType() string { return "security.login_failed" }

// UserLocked se emite cuando demasiados logins fallidos bloquean al usuario.
type UserLocked struct {
	fw.EventMeta
	Until time.Time `json:"until"`
}

// EventType implementa domain.Event.
func (UserLocked) EventType() string { return "security.user_locked" }

// UserUnlocked se emite cuando un administrador levanta el bloqueo.
type UserUnlocked struct{ fw.EventMeta }

// EventType implementa domain.Event.
func (UserUnlocked) EventType() string { return "security.user_unlocked" }

// UserRoleAssigned se emite cuando se asigna un rol a un usuario.
type UserRoleAssigned struct {
	fw.EventMeta
	Role     string `json:"role"`
	RoleName string `json:"roleName"`
}

// EventType implementa domain.Event.
func (UserRoleAssigned) EventType() string { return "security.user_role_assigned" }

// UserRoleRevoked se emite cuando se retira un rol a un usuario.
type UserRoleRevoked struct {
	fw.EventMeta
	Role string `json:"role"`
}

// EventType implementa domain.Event.
func (UserRoleRevoked) EventType() string { return "security.user_role_revoked" }

// AccessGranted se emite cuando un usuario recibe acceso a una organización.
type AccessGranted struct {
	fw.EventMeta
	Organization string `json:"organization"`
	Level        string `json:"level"`
}

// EventType implementa domain.Event.
func (AccessGranted) EventType() string { return "security.access_granted" }

// AccessChanged se emite cuando cambia el acceso de un usuario a una organización.
type AccessChanged struct {
	fw.EventMeta
	Organization string `json:"organization"`
	Level        string `json:"level"`
}

// EventType implementa domain.Event.
func (AccessChanged) EventType() string { return "security.access_changed" }

// AccessRevoked se emite cuando un usuario pierde el acceso a una organización.
type AccessRevoked struct {
	fw.EventMeta
	Organization string `json:"organization"`
}

// EventType implementa domain.Event.
func (AccessRevoked) EventType() string { return "security.access_revoked" }

// IdentityLinked se emite cuando una identidad externa se vincula con un usuario.
type IdentityLinked struct {
	fw.EventMeta
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

// EventType implementa domain.Event.
func (IdentityLinked) EventType() string { return "security.identity_linked" }

// IdentityUnlinked se emite cuando se desvincula una identidad externa.
type IdentityUnlinked struct {
	fw.EventMeta
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

// EventType implementa domain.Event.
func (IdentityUnlinked) EventType() string { return "security.identity_unlinked" }

// RoleDefined se emite cuando se define un rol propio.
type RoleDefined struct {
	fw.EventMeta
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// EventType implementa domain.Event.
func (RoleDefined) EventType() string { return "security.role_defined" }

// RoleDescribed se emite cuando cambia el nombre o la descripción de un rol.
type RoleDescribed struct {
	fw.EventMeta
	Name string `json:"name"`
}

// EventType implementa domain.Event.
func (RoleDescribed) EventType() string { return "security.role_described" }

// RolePermissionsChanged se emite cuando cambian los permisos de un rol.
type RolePermissionsChanged struct {
	fw.EventMeta
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// EventType implementa domain.Event.
func (RolePermissionsChanged) EventType() string { return "security.role_permissions_changed" }

// RoleRetired se emite cuando se retira un rol propio.
type RoleRetired struct {
	fw.EventMeta
	Name string `json:"name"`
}

// EventType implementa domain.Event.
func (RoleRetired) EventType() string { return "security.role_retired" }

// Repositorios del contexto.
type (
	// UserRepository guarda usuarios.
	UserRepository = fw.Repository[UserID, *User]
	// RoleRepository guarda roles.
	RoleRepository = fw.Repository[RoleID, *Role]
	// PermissionRepository guarda el catálogo de permisos.
	PermissionRepository = fw.Repository[PermissionID, *PermissionEntry]
	// SessionRepository guarda sesiones de refresco.
	SessionRepository = fw.Repository[SessionID, *Session]
)

// Política de contraseñas (decisión 3): sólo longitud, sin reglas de composición.
const (
	MinPasswordLength = 12
	MaxPasswordLength = 128
)

// CheckPassword aplica la política de contraseñas: de 12 a 128 caracteres y distinta del nombre de
// usuario. Sustituye al C#, que aceptaba cualquier contraseña.
func CheckPassword(password, username string) error {
	var v fw.Validation
	n := utf8.RuneCountInString(password)
	v.Require(n >= MinPasswordLength, "password", "too_short", "a password has at least 12 characters")
	v.Require(n <= MaxPasswordLength, "password", "too_long", "a password has at most 128 characters")
	v.Require(UsernameKey(password) != UsernameKey(username), "password", "username", "the password cannot be the user name")
	return v.Err()
}
