package application

import (
	"context"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/security/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// DefineRole defines a custom role.
type DefineRole struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Permissions []string `json:"permissions"`
}

// DescribeRole changes the name and the description of a custom role.
type DescribeRole struct {
	ID          domain.RoleID `json:"-"`
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
}

// SetRolePermissions replaces the permissions of a custom role.
type SetRolePermissions struct {
	ID          domain.RoleID `json:"-"`
	Permissions []string      `json:"permissions"`
}

// RetireRole deletes a custom role no user holds.
type RetireRole struct{ ID domain.RoleID }

// GetRole loads a role.
type GetRole struct{ ID domain.RoleID }

// ListRoles lists the roles.
type ListRoles struct{}

// ListPermissions lists the permission catalog.
type ListPermissions struct{}

// RoleDTO is the transport form of a role.
type RoleDTO struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	System      bool     `json:"system"`
	Permissions []string `json:"permissions"`
	Version     int64    `json:"version"`
}

// PermissionDTO is one entry of the permission catalog.
type PermissionDTO struct {
	Code        string `json:"code"`
	Namespace   string `json:"namespace"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
	Description string `json:"description,omitempty"`
	Active      bool   `json:"active"`
}

func roleDTO(r *domain.Role) RoleDTO {
	d := RoleDTO{ID: r.ID().String(), Name: r.Name(), Description: r.Description(), System: r.IsSystem(),
		Permissions: []string{}, Version: r.Version()}
	for _, p := range r.Permissions() {
		d.Permissions = append(d.Permissions, string(p))
	}
	return d
}

func permissionDTOs(entries []*domain.PermissionEntry) []PermissionDTO {
	out := make([]PermissionDTO, 0, len(entries))
	for _, e := range entries {
		c := e.Code()
		d := PermissionDTO{Code: string(c), Description: e.Description(), Active: e.IsActive()}
		if c == domain.Wildcard {
			d.Namespace, d.Resource, d.Action = "*", "*", "*"
		} else {
			d.Namespace, d.Resource, d.Action = c.Namespace(), c.Resource(), c.Action()
		}
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b PermissionDTO) int { return strings.Compare(a.Code, b.Code) })
	return out
}

// requireGlobal reserves roles and the catalog to the global administrator (decision P3): holding
// the permission is not enough. Without an authorization context it fails closed, unlike the C#
// EnsureGlobalAdmin, which let the request through.
func requireGlobal(ctx context.Context) error {
	a, err := administratorOf(ctx)
	if err != nil {
		return err
	}
	if !a.Global {
		return fw.ErrForbidden
	}
	return nil
}

// catalog loads the permissions in force.
func (s *service) catalog(ctx context.Context) (domain.Catalog, error) {
	entries, err := s.Permissions.Find(ctx, domain.PermFieldActive.Eq(true))
	if err != nil {
		return domain.Catalog{}, err
	}
	codes := make([]domain.Permission, len(entries))
	for i, e := range entries {
		codes[i] = e.Code()
	}
	return domain.NewCatalog(codes...), nil
}

// rolePermissions checks the permissions of a command against the catalog.
func (s *service) rolePermissions(ctx context.Context, codes []string) ([]domain.Permission, error) {
	c, err := s.catalog(ctx)
	if err != nil {
		return nil, err
	}
	perms := make([]domain.Permission, len(codes))
	for i, code := range codes {
		perms[i] = domain.Permission(code)
	}
	return perms, c.Check(perms)
}

func (s *service) roleNameFree(ctx context.Context, name string, except domain.RoleID) error {
	taken, err := s.Roles.Exists(ctx, domain.RoleNamed(name).And(domain.RoleFieldID.Ne(except)))
	if err != nil {
		return err
	}
	if taken {
		return fw.Violation("security.role_name_taken", "a role with that name already exists")
	}
	return nil
}

func (s *service) defineRole(ctx context.Context, c DefineRole) (RoleDTO, error) {
	if err := requireGlobal(ctx); err != nil {
		return RoleDTO{}, err
	}
	perms, err := s.rolePermissions(ctx, c.Permissions)
	if err != nil {
		return RoleDTO{}, err
	}
	r, err := domain.DefineRole(domain.NewRoleID(), c.Name, c.Description, perms)
	if err != nil {
		return RoleDTO{}, err
	}
	err = s.UoW.Do(ctx, func(ctx context.Context) error {
		if err := s.roleNameFree(ctx, r.Name(), domain.RoleID{}); err != nil {
			return err
		}
		return s.roles.Create(ctx, r)
	})
	if err != nil {
		return RoleDTO{}, err
	}
	return roleDTO(r), nil
}

func (s *service) describeRole(ctx context.Context, c DescribeRole) (RoleDTO, error) {
	if err := requireGlobal(ctx); err != nil {
		return RoleDTO{}, err
	}
	r, err := s.roles.Update(ctx, c.ID, func(ctx context.Context, r *domain.Role) error {
		if err := s.roleNameFree(ctx, c.Name, r.ID()); err != nil {
			return err
		}
		return r.Describe(c.Name, c.Description)
	})
	if err != nil {
		return RoleDTO{}, err
	}
	return roleDTO(r), nil
}

func (s *service) setRolePermissions(ctx context.Context, c SetRolePermissions) (RoleDTO, error) {
	if err := requireGlobal(ctx); err != nil {
		return RoleDTO{}, err
	}
	perms, err := s.rolePermissions(ctx, c.Permissions)
	if err != nil {
		return RoleDTO{}, err
	}
	r, err := s.roles.Update(ctx, c.ID, func(_ context.Context, r *domain.Role) error { return r.SetPermissions(perms) })
	if err != nil {
		return RoleDTO{}, err
	}
	return roleDTO(r), nil
}

func (s *service) retireRole(ctx context.Context, c RetireRole) (Done, error) {
	if err := requireGlobal(ctx); err != nil {
		return Done{}, err
	}
	return Done{}, s.roles.Delete(ctx, c.ID, func(ctx context.Context, r *domain.Role) error {
		held, err := s.Users.Exists(ctx, domain.HoldingRole(r.ID()))
		if err != nil {
			return err
		}
		if held {
			return fw.Violation("security.role_in_use", "the role is assigned to users")
		}
		return r.Retire()
	})
}

// Catalog keeps the permission catalog in step with what the bounded contexts declare, and the
// system roles in step with the catalog. The host calls Sync on start, after migrating.
type Catalog struct{ s *service }

// CatalogSync reports what a synchronization changed.
type CatalogSync struct {
	Added, Reactivated, Deactivated, RolesUpdated int
}

// Sync declares the permissions of every context (the own ones are always included): new codes
// enter the catalog, codes nobody declares anymore are deactivated and stop granting, and the
// system roles get the permissions the standard rule computes. Custom roles are never touched,
// unlike the C# seeder, which re-added whatever an administrator had removed. It is idempotent.
func (c Catalog) Sync(ctx context.Context, declared ...authz.Permission) (CatalogSync, error) {
	s := c.s
	decls := domain.OwnPermissions()
	for _, p := range declared {
		code := domain.Permission(p)
		if code == domain.Wildcard || slices.ContainsFunc(decls, func(d domain.PermissionDeclaration) bool { return d.Code == code }) {
			continue
		}
		decls = append(decls, domain.PermissionDeclaration{Code: code, Description: domain.DefaultDescription(code)})
	}
	var report CatalogSync
	err := s.UoW.Do(ctx, func(ctx context.Context) error {
		report = CatalogSync{}
		entries, err := s.Permissions.Find(ctx, nil)
		if err != nil {
			return err
		}
		known := map[domain.Permission]*domain.PermissionEntry{}
		for _, e := range entries {
			known[e.Code()] = e
		}
		codes := make([]domain.Permission, 0, len(decls))
		for _, d := range decls {
			codes = append(codes, d.Code)
			e, ok := known[d.Code]
			switch {
			case !ok:
				if e, err = domain.DeclarePermission(d.Code, d.Description); err != nil {
					return err
				}
				report.Added++
			case e.Activate():
				report.Reactivated++
			default:
				continue
			}
			if err := s.Permissions.Save(ctx, e); err != nil {
				return err
			}
		}
		for code, e := range known {
			if !slices.Contains(codes, code) && e.Deactivate() {
				report.Deactivated++
				if err := s.Permissions.Save(ctx, e); err != nil {
					return err
				}
			}
		}
		catalog := domain.NewCatalog(codes...)
		for _, seed := range domain.SystemRoles() {
			r, found, err := first(s.Roles.Find(ctx, domain.RolesWithIDs(seed.ID)))
			if err != nil {
				return err
			}
			if !found {
				if r, err = domain.NewSystemRole(seed, catalog); err != nil {
					return err
				}
				if err := s.roles.Create(ctx, r); err != nil {
					return err
				}
				report.RolesUpdated++
				continue
			}
			if !slices.Equal(r.Permissions(), domain.StandardPermissions(seed.ID, catalog)) {
				if _, err := s.roles.Update(ctx, seed.ID, func(_ context.Context, r *domain.Role) error {
					r.SyncSystemPermissions(catalog)
					return nil
				}); err != nil {
					return err
				}
				report.RolesUpdated++
			}
		}
		return nil
	})
	return report, err
}

// Bootstrap creates the first global administrator of an installation (decision 7).
type Bootstrap struct{ s *service }

// Outcomes of Bootstrap.Run.
const (
	BootstrapNotNeeded     = "an active global administrator already exists"
	BootstrapNoCredentials = "no bootstrap credentials: nothing created"
	BootstrapCreated       = "bootstrap administrator created"
)

// Run creates the bootstrap administrator only when no active global administrator exists and
// both credentials are given (the host reads them from the environment, never from versioned
// configuration). The user must change the password before doing anything else. If the user name
// is already taken, nothing is created and no role is granted: in C# the existing user was
// promoted to global administrator keeping its own password. The catalog must be synchronized
// first. It never logs nor returns the password.
func (b Bootstrap) Run(ctx context.Context, username, password string) (string, error) {
	s := b.s
	exists, err := s.Users.Exists(ctx, domain.GlobalAdministrators(domain.UserID{}))
	if err != nil {
		return "", err
	}
	if exists {
		return BootstrapNotNeeded, nil
	}
	if strings.TrimSpace(username) == "" || password == "" {
		return BootstrapNoCredentials, nil
	}
	if err := domain.CheckPassword(password, username); err != nil {
		return "", err
	}
	role, err := s.Roles.Get(ctx, domain.RoleGlobalSuperAdmin)
	if err != nil {
		return "", err
	}
	hash, err := s.Hasher.Hash(password)
	if err != nil {
		return "", err
	}
	err = s.UoW.Do(ctx, func(ctx context.Context) error {
		if err := s.usernameFree(ctx, username, domain.UserID{}); err != nil {
			return err
		}
		id := domain.BootstrapAdminUser
		if taken, err := s.Users.Exists(ctx, domain.UsersWithIDs(id)); err != nil {
			return err
		} else if taken {
			id = domain.NewUserID()
		}
		u, err := domain.RegisterUser(id, domain.BootstrapAdminParty, username)
		if err != nil {
			return err
		}
		if err := u.SetPassword(hash, true, false); err != nil {
			return err
		}
		if err := u.AssignRole(role); err != nil {
			return err
		}
		return s.users.Create(ctx, u)
	})
	if err != nil {
		return "", err
	}
	return BootstrapCreated, nil
}
