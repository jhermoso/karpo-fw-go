package domain

// Namespace es el primer segmento de los permisos de este contexto.
const Namespace = "Security"

// Roles del sistema (WellKnownSecurityCatalog.Roles de C#, los mismos GUID). No se pueden
// renombrar, editar ni retirar, y sus permisos los calcula StandardPermissions.
var (
	RoleGlobalSuperAdmin  = MustRoleID("20000000-0000-0000-0001-000000000001")
	RoleOrganizationAdmin = MustRoleID("20000000-0000-0000-0001-000000000002")
	RoleStandardUser      = MustRoleID("20000000-0000-0000-0001-000000000003")
	RoleReadOnlyUser      = MustRoleID("20000000-0000-0000-0001-000000000004")
	RoleCustomer          = MustRoleID("20000000-0000-0000-0001-000000000005")
)

// GlobalSuperAdminName es el nombre que el resolvedor de autorización reconoce como administrador global.
const GlobalSuperAdminName = "GlobalSuperAdmin"

// SystemRole es la semilla de un rol del sistema.
type SystemRole struct {
	ID          RoleID
	Name        string
	Description string
}

// SystemRoles devuelve la semilla de los roles del sistema.
func SystemRoles() []SystemRole {
	return []SystemRole{
		{RoleGlobalSuperAdmin, GlobalSuperAdminName, "Full access to the entire system"},
		{RoleOrganizationAdmin, "OrganizationAdmin", "Administrator for assigned organizations"},
		{RoleStandardUser, "StandardUser", "Standard user with basic permissions"},
		{RoleReadOnlyUser, "ReadOnlyUser", "Read-only access"},
		{RoleCustomer, "Customer", "Customer account (own data only)"},
	}
}

// Administrador de arranque (WellKnownSecurityCatalog.Users de C#, los mismos GUID): el
// administrador global técnico creado en el primer arranque a partir de credenciales del entorno.
// Su party es una identidad reservada: Security nunca escribe en Parties (decisión 7).
var (
	BootstrapAdminUser  = MustUserID("20000000-0000-0000-0002-000000000003")
	BootstrapAdminParty = PartyID{MustUserID("20000000-0000-0000-0002-000000000004").UUID}
)

// Permisos del propio contexto Security.
const (
	PermUserRead     Permission = "Security.User.Read"
	PermUserCreate   Permission = "Security.User.Create"
	PermUserUpdate   Permission = "Security.User.Update"
	PermUserRole     Permission = "Security.UserRole.Assign"
	PermAccessRead   Permission = "Security.OrganizationAccess.Read"
	PermAccessUpdate Permission = "Security.OrganizationAccess.Update"
	PermRoleRead     Permission = "Security.Role.Read"
	PermRoleUpdate   Permission = "Security.Role.Update"
)

// PermissionDeclaration es un permiso con su descripción, tal como lo declara un contexto.
type PermissionDeclaration struct {
	Code        Permission
	Description string
}

// OwnPermissions devuelve los permisos que declara Security, más el comodín.
func OwnPermissions() []PermissionDeclaration {
	return []PermissionDeclaration{
		{Wildcard, DefaultDescription(Wildcard)},
		{PermUserRead, "See users, their roles, organization accesses and identities"},
		{PermUserCreate, "Register users"},
		{PermUserUpdate, "Rename, deactivate, reactivate and unlock users, reset passwords and link identities"},
		{PermUserRole, "Assign and revoke the roles of a user"},
		{PermAccessRead, "See the organization accesses of a user"},
		{PermAccessUpdate, "Grant, change and revoke organization accesses"},
		{PermRoleRead, "See roles and the permission catalog"},
		{PermRoleUpdate, "Define, edit and retire roles (global administrator only)"},
	}
}

// GlobalOnlyPermissions nunca se dan a OrganizationAdmin: los roles y el catálogo son del
// administrador global (decisión P3).
var GlobalOnlyPermissions = []Permission{PermRoleUpdate}
