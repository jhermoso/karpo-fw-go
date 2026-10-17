package domain

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// UserKind es el nombre estable del tipo de agregado.
const UserKind = "security.user"

// AccessLevel es el acceso que tiene un usuario a una organización (contrato v1, P2: Full escribe,
// ReadOnly lee, Restricted se comporta como ReadOnly).
type AccessLevel string

// Niveles de acceso.
const (
	Full       AccessLevel = "Full"
	ReadOnly   AccessLevel = "ReadOnly"
	Restricted AccessLevel = "Restricted"
)

// ParseAccessLevel interpreta un nivel. A diferencia del texto libre de C#, se rechaza un nivel desconocido.
func ParseAccessLevel(s string) (AccessLevel, error) {
	switch l := AccessLevel(s); l {
	case Full, ReadOnly, Restricted:
		return l, nil
	}
	var v fw.Validation
	v.Add("level", "unknown", "the access level is Full, ReadOnly or Restricted")
	return "", v.Err()
}

// OrganizationAccess es el acceso de un usuario a una organización interna. IncludeSubsidiaries
// viaja en el contrato de autorización pero no expande nada en v1 (decisión P2).
type OrganizationAccess struct {
	Organization        OrganizationID
	Level               AccessLevel
	IncludeSubsidiaries bool
}

// ExternalIdentity vincula un usuario con una identidad de un proveedor de identidad externo: el
// emisor de sus tokens y el sujeto dentro de ese emisor (decisión 1). El par es único en la
// instalación.
type ExternalIdentity struct {
	Issuer  string
	Subject string
}

// NewExternalIdentity valida una identidad externa.
func NewExternalIdentity(issuer, subject string) (ExternalIdentity, error) {
	id := ExternalIdentity{Issuer: strings.TrimSpace(issuer), Subject: strings.TrimSpace(subject)}
	var v fw.Validation
	v.Require(id.Issuer != "" && len(id.Issuer) <= 200, "issuer", "length", "the issuer is required, at most 200 characters")
	v.Require(id.Subject != "" && len(id.Subject) <= 200, "subject", "length", "the subject is required, at most 200 characters")
	return id, v.Err()
}

var usernamePattern = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}._@+-]*$`)

// NormalizeUsername valida un nombre de usuario: de 3 a 100 caracteres, letras, dígitos y
// . _ @ + -, empezando por una letra o un dígito. Las mayúsculas se conservan para mostrarlo; la
// unicidad no las distingue.
func NormalizeUsername(s string) (string, error) {
	s = strings.TrimSpace(s)
	var v fw.Validation
	n := utf8.RuneCountInString(s)
	v.Require(n >= 3 && n <= 100, "username", "length", "a user name has 3 to 100 characters")
	v.Require(n < 3 || usernamePattern.MatchString(s), "username", "format", "a user name has letters, digits and . _ @ + -")
	return s, v.Err()
}

// UsernameKey devuelve la forma que se usa para que los nombres de usuario sean únicos y para buscarlos.
func UsernameKey(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// LockoutPolicy bloquea a un usuario tras demasiados logins fallidos.
type LockoutPolicy struct {
	MaxAttempts int
	Duration    time.Duration
}

// DefaultLockout es el valor por defecto de C#: 5 intentos fallidos bloquean al usuario 15 minutos.
var DefaultLockout = LockoutPolicy{MaxAttempts: 5, Duration: 15 * time.Minute}

// User es quien puede iniciar sesión y actuar: una party con credenciales (una contraseña
// opcional, identidades externas), roles y accesos a organizaciones. Un usuario nunca se borra,
// sólo se desactiva.
type User struct {
	fw.BaseAggregateRoot[UserID]
	traits.Activation
	traits.Audited
	party              PartyID
	username           string
	passwordHash       string // hash autodescriptivo; vacío cuando el usuario no tiene contraseña
	mustChangePassword bool
	failedAttempts     int
	lockedUntil        *time.Time
	lastLoginAt        *time.Time
	roles              []RoleID
	accesses           []OrganizationAccess
	identities         []ExternalIdentity
}

// UserState es el estado persistido de un usuario.
type UserState struct {
	Party              PartyID
	Username           string
	PasswordHash       string
	MustChangePassword bool
	FailedAttempts     int
	LockedUntil        *time.Time
	LastLoginAt        *time.Time
	Roles              []RoleID
	Accesses           []OrganizationAccess
	Identities         []ExternalIdentity
	Active             bool
	Audit              traits.AuditStamp
}

// ReconstituteUser reconstruye un usuario a partir del estado persistido.
func ReconstituteUser(id UserID, s UserState) (*User, error) {
	base, err := fw.NewBaseAggregateRoot(UserKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Party.IsZero(), "party", "required", "a user is a party")
	username, err := NormalizeUsername(s.Username)
	v.Merge("", err)
	v.Require(s.FailedAttempts >= 0, "failedAttempts", "range", "cannot be negative")
	u := &User{BaseAggregateRoot: base, Activation: traits.RestoredActivation(s.Active), Audited: traits.RestoredAudit(s.Audit),
		party: s.Party, username: username, passwordHash: s.PasswordHash, mustChangePassword: s.MustChangePassword,
		failedAttempts: s.FailedAttempts, lockedUntil: cloneTime(s.LockedUntil), lastLoginAt: cloneTime(s.LastLoginAt)}
	for _, r := range s.Roles {
		v.Require(!r.IsZero() && !slices.Contains(u.roles, r), "roles", "duplicate", "a role is assigned once")
		u.roles = append(u.roles, r)
	}
	for _, a := range s.Accesses {
		_, err := ParseAccessLevel(string(a.Level))
		v.Merge("accesses", err)
		v.Require(!a.Organization.IsZero() && u.accessIndex(a.Organization) < 0, "accesses", "duplicate", "one access per organization")
		u.accesses = append(u.accesses, a)
	}
	for _, i := range s.Identities {
		n, err := NewExternalIdentity(i.Issuer, i.Subject)
		v.Merge("identities", err)
		v.Require(!slices.Contains(u.identities, n), "identities", "duplicate", "an identity is linked once")
		u.identities = append(u.identities, n)
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	// Un orden estable devuelva lo que devuelva el motor (SQL Server ordena los GUID por grupos de bytes).
	slices.SortFunc(u.roles, func(a, b RoleID) int { return strings.Compare(a.String(), b.String()) })
	slices.SortFunc(u.accesses, func(a, b OrganizationAccess) int {
		return strings.Compare(a.Organization.String(), b.Organization.String())
	})
	slices.SortFunc(u.identities, func(a, b ExternalIdentity) int {
		return strings.Compare(a.Issuer+"\x00"+a.Subject, b.Issuer+"\x00"+b.Subject)
	})
	return u, nil
}

// RegisterUser crea un usuario activo sin credenciales, roles ni accesos.
func RegisterUser(id UserID, party PartyID, username string) (*User, error) {
	u, err := ReconstituteUser(id, UserState{Party: party, Username: username, Active: true})
	if err != nil {
		return nil, err
	}
	u.Raise(UserRegistered{EventMeta: u.NewEventMeta(), Username: u.username, Party: party.String()})
	return u, nil
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := t.UTC()
	return &c
}

// Party devuelve la party que hay detrás del usuario.
func (u *User) Party() PartyID { return u.party }

// Username devuelve el nombre de usuario tal como se dio de alta.
func (u *User) Username() string { return u.username }

// UsernameKey devuelve la forma de búsqueda del nombre de usuario.
func (u *User) UsernameKey() string { return UsernameKey(u.username) }

// PasswordHash devuelve el hash guardado ("" cuando el usuario no tiene contraseña). Nunca sale de
// la capa de aplicación.
func (u *User) PasswordHash() string { return u.passwordHash }

// HasPassword dice si el usuario puede iniciar sesión con contraseña.
func (u *User) HasPassword() bool { return u.passwordHash != "" }

// MustChangePassword dice si el usuario tiene que cambiar la contraseña antes de hacer nada.
func (u *User) MustChangePassword() bool { return u.mustChangePassword }

// FailedAttempts devuelve los logins fallidos consecutivos.
func (u *User) FailedAttempts() int { return u.failedAttempts }

// LockedUntil devuelve el final del bloqueo, si lo hay.
func (u *User) LockedUntil() *time.Time { return cloneTime(u.lockedUntil) }

// LastLoginAt devuelve el último login con contraseña correcto, si lo hay.
func (u *User) LastLoginAt() *time.Time { return cloneTime(u.lastLoginAt) }

// Roles devuelve los roles asignados.
func (u *User) Roles() []RoleID { return slices.Clone(u.roles) }

// Accesses devuelve los accesos a organizaciones.
func (u *User) Accesses() []OrganizationAccess { return slices.Clone(u.accesses) }

// Identities devuelve las identidades externas vinculadas.
func (u *User) Identities() []ExternalIdentity { return slices.Clone(u.identities) }

// HasRole dice si el rol está asignado.
func (u *User) HasRole(r RoleID) bool { return slices.Contains(u.roles, r) }

func (u *User) accessIndex(org OrganizationID) int {
	return slices.IndexFunc(u.accesses, func(a OrganizationAccess) bool { return a.Organization == org })
}

// IsLockedAt dice si el usuario está bloqueado en t.
func (u *User) IsLockedAt(t time.Time) bool { return u.lockedUntil != nil && t.Before(*u.lockedUntil) }

// CanSignInAt dice si el usuario puede autenticarse en t: activo y no bloqueado.
func (u *User) CanSignInAt(t time.Time) bool { return u.IsActive() && !u.IsLockedAt(t) }

func (u *User) requireActive(action string) error {
	if !u.IsActive() {
		return fw.Violation("security.inactive_user", "an inactive user cannot "+action)
	}
	return nil
}

// Rename cambia el nombre de usuario.
func (u *User) Rename(username string) error {
	if err := u.requireActive("be renamed"); err != nil {
		return err
	}
	username, err := NormalizeUsername(username)
	if err != nil {
		return err
	}
	if username == u.username {
		return nil
	}
	u.username = username
	u.Raise(UserRenamed{EventMeta: u.NewEventMeta(), Username: username})
	return nil
}

// SetPassword guarda un hash de contraseña nuevo (calculado por la aplicación a partir de una
// contraseña que pasó la política). reset dice si la fijó un administrador; mustChange obliga al
// usuario a cambiarla antes de hacer nada más. Cambiar la contraseña levanta el bloqueo.
func (u *User) SetPassword(hash string, mustChange, reset bool) error {
	if err := u.requireActive("change the password"); err != nil {
		return err
	}
	if hash == "" {
		var v fw.Validation
		v.Add("password", "required", "a password hash is required")
		return v.Err()
	}
	u.passwordHash, u.mustChangePassword = hash, mustChange
	u.failedAttempts, u.lockedUntil = 0, nil
	u.Raise(PasswordChanged{EventMeta: u.NewEventMeta(), Reset: reset})
	return nil
}

// UpgradePasswordHash sustituye el hash de la misma contraseña por uno más fuerte (más iteraciones
// u otro algoritmo) tras un login correcto. No es un cambio de contraseña.
func (u *User) UpgradePasswordHash(hash string) {
	if hash != "" && u.passwordHash != "" {
		u.passwordHash = hash
	}
}

// RecordLogin anota un login con contraseña correcto.
func (u *User) RecordLogin(at time.Time) {
	u.failedAttempts, u.lockedUntil = 0, nil
	u.lastLoginAt = cloneTime(&at)
	u.Raise(UserLoggedIn{EventMeta: u.NewEventMeta()})
}

// RecordFailedLogin cuenta una comprobación de contraseña fallida y bloquea al usuario cuando lo
// dice la política. Dice si el usuario quedó bloqueado.
func (u *User) RecordFailedLogin(p LockoutPolicy, at time.Time) bool {
	if u.lockedUntil != nil && !at.Before(*u.lockedUntil) { // un bloqueo vencido empieza una cuenta nueva
		u.failedAttempts, u.lockedUntil = 0, nil
	}
	u.failedAttempts++
	u.Raise(LoginFailed{EventMeta: u.NewEventMeta(), Attempts: u.failedAttempts})
	if p.MaxAttempts <= 0 || u.failedAttempts < p.MaxAttempts {
		return false
	}
	until := at.Add(p.Duration).UTC()
	u.lockedUntil = &until
	u.Raise(UserLocked{EventMeta: u.NewEventMeta(), Until: until})
	return true
}

// Unlock levanta el bloqueo y olvida los intentos fallidos.
func (u *User) Unlock() {
	if u.lockedUntil == nil && u.failedAttempts == 0 {
		return
	}
	u.failedAttempts, u.lockedUntil = 0, nil
	u.Raise(UserUnlocked{EventMeta: u.NewEventMeta()})
}

// Deactivate desactiva al usuario: ya no puede autenticarse ni ser autorizado.
func (u *User) Deactivate() {
	if u.Activation.Deactivate() {
		u.Raise(UserActivationChanged{EventMeta: u.NewEventMeta(), Active: false})
	}
}

// Activate reactiva al usuario.
func (u *User) Activate() {
	if u.Activation.Activate() {
		u.Raise(UserActivationChanged{EventMeta: u.NewEventMeta(), Active: true})
	}
}

// AssignRole asigna un rol. Asignarlo dos veces no cambia nada.
func (u *User) AssignRole(r *Role) error {
	if err := u.requireActive("receive roles"); err != nil {
		return err
	}
	if u.HasRole(r.ID()) {
		return nil
	}
	u.roles = append(slices.Clone(u.roles), r.ID())
	u.Raise(UserRoleAssigned{EventMeta: u.NewEventMeta(), Role: r.ID().String(), RoleName: r.Name()})
	return nil
}

// RevokeRole quita un rol. Dice si el usuario lo tenía.
func (u *User) RevokeRole(id RoleID) bool {
	if !u.HasRole(id) {
		return false
	}
	u.roles = slices.DeleteFunc(slices.Clone(u.roles), func(r RoleID) bool { return r == id })
	u.Raise(UserRoleRevoked{EventMeta: u.NewEventMeta(), Role: id.String()})
	return true
}

// GrantAccess da al usuario acceso a una organización, o cambia el acceso que ya tiene.
func (u *User) GrantAccess(a OrganizationAccess) error {
	if err := u.requireActive("receive organization accesses"); err != nil {
		return err
	}
	var v fw.Validation
	v.Require(!a.Organization.IsZero(), "organization", "required", "the organization is required")
	_, err := ParseAccessLevel(string(a.Level))
	v.Merge("", err)
	if err := v.Err(); err != nil {
		return err
	}
	next := slices.Clone(u.accesses)
	if i := u.accessIndex(a.Organization); i >= 0 {
		if next[i] == a {
			return nil
		}
		next[i] = a
		u.accesses = next
		u.Raise(AccessChanged{EventMeta: u.NewEventMeta(), Organization: a.Organization.String(), Level: string(a.Level)})
		return nil
	}
	u.accesses = append(next, a)
	u.Raise(AccessGranted{EventMeta: u.NewEventMeta(), Organization: a.Organization.String(), Level: string(a.Level)})
	return nil
}

// RevokeAccess quita el acceso a una organización. Dice si el usuario lo tenía.
func (u *User) RevokeAccess(org OrganizationID) bool {
	i := u.accessIndex(org)
	if i < 0 {
		return false
	}
	u.accesses = slices.Delete(slices.Clone(u.accesses), i, i+1)
	u.Raise(AccessRevoked{EventMeta: u.NewEventMeta(), Organization: org.String()})
	return true
}

// LinkIdentity vincula una identidad externa. La aplicación comprueba que ningún otro usuario la tiene.
func (u *User) LinkIdentity(id ExternalIdentity) error {
	if err := u.requireActive("link identities"); err != nil {
		return err
	}
	id, err := NewExternalIdentity(id.Issuer, id.Subject)
	if err != nil {
		return err
	}
	if slices.Contains(u.identities, id) {
		return nil
	}
	u.identities = append(slices.Clone(u.identities), id)
	u.Raise(IdentityLinked{EventMeta: u.NewEventMeta(), Issuer: id.Issuer, Subject: id.Subject})
	return nil
}

// UnlinkIdentity quita una identidad externa. Dice si el usuario la tenía.
func (u *User) UnlinkIdentity(id ExternalIdentity) bool {
	i := slices.Index(u.identities, id)
	if i < 0 {
		return false
	}
	u.identities = slices.Delete(slices.Clone(u.identities), i, i+1)
	u.Raise(IdentityUnlinked{EventMeta: u.NewEventMeta(), Issuer: id.Issuer, Subject: id.Subject})
	return true
}

// AuditSnapshot implementa traits.Snapshotter. Nunca expone el hash de la contraseña, y deja fuera
// los contadores de login, que cambian en cada inicio de sesión.
func (u *User) AuditSnapshot() map[string]any {
	roles := make([]string, len(u.roles))
	for i, r := range u.roles {
		roles[i] = r.String()
	}
	slices.Sort(roles)
	accesses := make([]string, len(u.accesses))
	for i, a := range u.accesses {
		accesses[i] = a.Organization.String() + ":" + string(a.Level)
	}
	slices.Sort(accesses)
	identities := make([]string, len(u.identities))
	for i, id := range u.identities {
		identities[i] = id.Issuer + "|" + id.Subject
	}
	slices.Sort(identities)
	return map[string]any{"username": u.username, "party": u.party.String(), "active": u.IsActive(),
		"hasPassword": u.HasPassword(), "mustChangePassword": u.mustChangePassword, "locked": u.lockedUntil != nil,
		"roles": strings.Join(roles, ","), "accesses": strings.Join(accesses, ","), "identities": strings.Join(identities, ",")}
}
