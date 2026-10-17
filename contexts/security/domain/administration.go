package domain

import (
	"fmt"
	"slices"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Administrator es quien realiza un acto de administración delegada, tal como se resuelve para la
// petición: sus permisos, las organizaciones de su ámbito y las que puede escribir (acceso Full).
//
// Las tres reglas de la administración delegada (decisión 5) son sus métodos:
//
//  1. nadie concede lo que no tiene (CanAssign, CanGrant);
//  2. cada administrador administra a los usuarios que abarca por completo (CanAdminister, AdministrableBy);
//  3. nadie se administra a sí mismo (cada regla rechaza el propio usuario del administrador). La
//     otra mitad de la regla 3, que siempre quede un administrador global activo, necesita a los
//     otros usuarios y la comprueba la aplicación con GlobalAdministrators.
type Administrator struct {
	User          UserID
	Global        bool
	Permissions   []Permission
	Organizations []OrganizationID // ámbito efectivo: lo que ve el administrador
	Full          []OrganizationID // organizaciones con acceso Full dentro del ámbito
}

func forbidden(msg string) error { return fmt.Errorf("%w: %s", fw.ErrForbidden, msg) }

func (a Administrator) holds(p Permission) bool {
	return a.Global || slices.Contains(a.Permissions, Wildcard) || slices.Contains(a.Permissions, p)
}

func (a Administrator) notSelf(u UserID) error {
	if a.User == u {
		return forbidden("nobody administers their own user")
	}
	return nil
}

// CanAssign comprueba la regla 1 para los roles: para asignar o retirar un rol el administrador
// tiene que tener cada permiso que lleva el rol. Sustituye la comprobación de C# por nombre de rol
// y el nivel de jerarquía sin usar: un rol propio con más permisos que los del administrador queda
// fuera de su alcance.
func (a Administrator) CanAssign(r *Role) error {
	for _, p := range r.Permissions() {
		if !a.holds(p) {
			return forbidden("the role carries permissions you do not have")
		}
	}
	return nil
}

// CanGrant comprueba la regla 1 para las organizaciones: para conceder, cambiar o retirar un acceso
// a una organización el administrador necesita acceso Full a ella.
func (a Administrator) CanGrant(org OrganizationID) error {
	if a.Global || slices.Contains(a.Full, org) {
		return nil
	}
	return forbidden("organization outside the writable scope")
}

// Sees dice si el usuario es visible para el administrador: tiene un acceso a una organización del
// ámbito del administrador.
func (a Administrator) Sees(u *User) bool {
	return a.Global || VisibleTo(a.Organizations...).IsSatisfiedBy(u)
}

// Encompasses dice si el administrador abarca por completo al usuario (regla 2).
func (a Administrator) Encompasses(u *User) bool {
	return a.Global || AdministrableBy(a.Full...).IsSatisfiedBy(u)
}

// CanAdminister comprueba las reglas 2 y 3 para un usuario: el administrador puede renombrar,
// desactivar, desbloquear, restablecer la contraseña, vincular identidades o cambiar los roles de
// un usuario sólo cuando tiene acceso Full a cada organización en la que trabaja el usuario. Un
// usuario sin accesos es del administrador global. En C# bastaba una sola organización compartida,
// aunque fuera de sólo lectura.
func (a Administrator) CanAdminister(u *User) error {
	if err := a.notSelf(u.ID()); err != nil {
		return err
	}
	if !a.Encompasses(u) {
		return forbidden("you do not have full access to every organization of the user")
	}
	return nil
}

// CanChangeAccess comprueba las reglas 1 y 3 para el acceso de un usuario a una organización.
func (a Administrator) CanChangeAccess(u *User, org OrganizationID) error {
	if err := a.notSelf(u.ID()); err != nil {
		return err
	}
	return a.CanGrant(org)
}

// Campos y especificaciones de User.
var (
	UserFieldID          = spec.Comparable("id", func(u *User) UserID { return u.ID() })
	UserFieldUsername    = spec.Text("username", (*User).Username)
	UserFieldUsernameKey = spec.Comparable("username_key", (*User).UsernameKey)
	UserFieldParty       = spec.Comparable("party", (*User).Party)
	UserFieldActive      = spec.Comparable("active", (*User).IsActive)

	UserFieldRoles      = spec.Collection("roles", (*User).Roles)
	UserRoleFieldRole   = spec.Comparable("role_id", func(r RoleID) RoleID { return r })
	UserFieldAccesses   = spec.Collection("accesses", (*User).Accesses)
	AccessFieldOrg      = spec.Comparable("organization", func(a OrganizationAccess) OrganizationID { return a.Organization })
	UserFieldIdentities = spec.Collection("identities", (*User).Identities)
	IdentityFieldIssuer = spec.Comparable("issuer", func(i ExternalIdentity) string { return i.Issuer })
	IdentityFieldSubj   = spec.Comparable("subject", func(i ExternalIdentity) string { return i.Subject })
)

// UsersWithIDs selecciona los usuarios dados.
func UsersWithIDs(ids ...UserID) spec.Spec[*User] { return UserFieldID.In(ids...) }

// UserNamed selecciona el usuario con ese nombre de usuario, sin distinguir mayúsculas.
func UserNamed(username string) spec.Spec[*User] {
	return UserFieldUsernameKey.Eq(UsernameKey(username))
}

// UsersOfParties selecciona los usuarios de las parties.
func UsersOfParties(parties ...PartyID) spec.Spec[*User] { return UserFieldParty.In(parties...) }

// WithIdentity selecciona el usuario vinculado con una identidad externa.
func WithIdentity(id ExternalIdentity) spec.Spec[*User] {
	return UserFieldIdentities.Any(IdentityFieldIssuer.Eq(id.Issuer).And(IdentityFieldSubj.Eq(id.Subject)))
}

// HoldingRole selecciona los usuarios con cualquiera de los roles.
func HoldingRole(roles ...RoleID) spec.Spec[*User] {
	if len(roles) == 0 {
		return spec.None[*User]()
	}
	return UserFieldRoles.Any(UserRoleFieldRole.In(roles...))
}

// VisibleTo es la regla de ámbito de Security para leer: un usuario no pertenece a una
// organización, tiene accesos; es visible para quien tiene una de esas organizaciones en su ámbito.
func VisibleTo(orgs ...OrganizationID) spec.Spec[*User] {
	if len(orgs) == 0 {
		return spec.None[*User]()
	}
	return UserFieldAccesses.Any(AccessFieldOrg.In(orgs...))
}

// AdministrableBy es la regla de ámbito de Security para administrar (regla 2): los usuarios con
// al menos un acceso, todos ellos a organizaciones donde el administrador tiene acceso Full.
func AdministrableBy(full ...OrganizationID) spec.Spec[*User] {
	if len(full) == 0 {
		return spec.None[*User]()
	}
	return UserFieldAccesses.Any(AccessFieldOrg.In(full...)).And(UserFieldAccesses.None(AccessFieldOrg.NotIn(full...)))
}

// GlobalAdministrators selecciona los usuarios activos con el rol GlobalSuperAdmin, salvo except:
// la regla 3 exige que siempre quede uno.
func GlobalAdministrators(except UserID) spec.Spec[*User] {
	return UserFieldActive.Eq(true).And(HoldingRole(RoleGlobalSuperAdmin), UserFieldID.Ne(except))
}

// ErrLastGlobalAdmin se devuelve cuando un acto dejaría la instalación sin un administrador global
// activo.
var ErrLastGlobalAdmin = fw.Violation("security.last_global_admin", "an active global administrator must always remain")
