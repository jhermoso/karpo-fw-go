package domain

// Namespace is the first segment of the permissions of this context.
const Namespace = "Security"

// System roles (C# WellKnownSecurityCatalog.Roles, same GUIDs). They cannot be renamed, edited or
// retired, and their permissions are computed by StandardPermissions.
var (
	RoleGlobalSuperAdmin  = MustRoleID("20000000-0000-0000-0001-000000000001")
	RoleOrganizationAdmin = MustRoleID("20000000-0000-0000-0001-000000000002")
	RoleStandardUser      = MustRoleID("20000000-0000-0000-0001-000000000003")
	RoleReadOnlyUser      = MustRoleID("20000000-0000-0000-0001-000000000004")
	RoleCustomer          = MustRoleID("20000000-0000-0000-0001-000000000005")
)

// GlobalSuperAdminName is the name the authorization resolver recognizes as global administrator.
const GlobalSuperAdminName = "GlobalSuperAdmin"

// SystemRole is the seed of a system role.
type SystemRole struct {
	ID          RoleID
	Name        string
	Description string
}

// SystemRoles returns the seed of the system roles.
func SystemRoles() []SystemRole {
	return []SystemRole{
		{RoleGlobalSuperAdmin, GlobalSuperAdminName, "Full access to the entire system"},
		{RoleOrganizationAdmin, "OrganizationAdmin", "Administrator for assigned organizations"},
		{RoleStandardUser, "StandardUser", "Standard user with basic permissions"},
		{RoleReadOnlyUser, "ReadOnlyUser", "Read-only access"},
		{RoleCustomer, "Customer", "Customer account (own data only)"},
	}
}

// Bootstrap administrator (C# WellKnownSecurityCatalog.Users, same GUIDs): the technical global
// administrator created on first start from environment credentials. Its party is a reserved
// identity: Security never writes into Parties (decision 7).
var (
	BootstrapAdminUser  = MustUserID("20000000-0000-0000-0002-000000000003")
	BootstrapAdminParty = PartyID{MustUserID("20000000-0000-0000-0002-000000000004").UUID}
)

// Permissions of the Security context itself.
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

// PermissionDeclaration is a permission with its description, as a context declares it.
type PermissionDeclaration struct {
	Code        Permission
	Description string
}

// OwnPermissions returns the permissions Security declares, plus the wildcard.
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

// GlobalOnlyPermissions are never given to OrganizationAdmin: roles and the catalog belong to the
// global administrator (decision P3).
var GlobalOnlyPermissions = []Permission{PermRoleUpdate}
