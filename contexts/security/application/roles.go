package application

import (
	"context"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/security/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// DefineRole define un rol propio.
type DefineRole struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Permissions []string `json:"permissions"`
}

// DescribeRole cambia el nombre y la descripción de un rol propio.
type DescribeRole struct {
	ID          domain.RoleID `json:"-"`
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
}

// SetRolePermissions sustituye los permisos de un rol. A un rol del sistema sólo se le dejan
// permisos que le da la regla estándar (D-98).
type SetRolePermissions struct {
	ID          domain.RoleID `json:"-"`
	Permissions []string      `json:"permissions"`
}

// RetireRole borra un rol propio que ningún usuario tiene.
type RetireRole struct{ ID domain.RoleID }

// GetRole carga un rol.
type GetRole struct{ ID domain.RoleID }

// ListRoles lista los roles.
type ListRoles struct{}

// ListPermissions lista el catálogo de permisos.
type ListPermissions struct{}

// RoleDTO es la forma de transporte de un rol.
type RoleDTO struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	System      bool     `json:"system"`
	Permissions []string `json:"permissions"`
	// Withdrawn son, de un rol del sistema, los permisos que la regla estándar le dio y el
	// administrador global le retiró (D-98): los que se le pueden devolver. Puede incluir uno que el
	// catálogo desactivó después; devolverlo da 400 mientras siga desactivado.
	Withdrawn []string `json:"withdrawn"`
	Version   int64    `json:"version"`
}

// PermissionDTO es una entrada del catálogo de permisos.
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
		Permissions: []string{}, Withdrawn: []string{}, Version: r.Version()}
	for _, p := range r.Permissions() {
		d.Permissions = append(d.Permissions, string(p))
	}
	for _, p := range r.Withdrawn() {
		d.Withdrawn = append(d.Withdrawn, string(p))
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

// requireGlobal reserva los roles y el catálogo al administrador global (decisión P3): no basta con
// tener el permiso. Sin contexto de autorización falla cerrado, a diferencia del EnsureGlobalAdmin
// de C#, que dejaba pasar la petición.
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

// catalog carga los permisos en vigor.
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

// rolePermissions comprueba los permisos de un comando contra el catálogo, que devuelve.
func (s *service) rolePermissions(ctx context.Context, codes []string) ([]domain.Permission, domain.Catalog, error) {
	c, err := s.catalog(ctx)
	if err != nil {
		return nil, c, err
	}
	perms := make([]domain.Permission, len(codes))
	for i, code := range codes {
		perms[i] = domain.Permission(code)
	}
	return perms, c, c.Check(perms)
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
	perms, _, err := s.rolePermissions(ctx, c.Permissions)
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
	perms, catalog, err := s.rolePermissions(ctx, c.Permissions)
	if err != nil {
		return RoleDTO{}, err
	}
	r, err := s.roles.Update(ctx, c.ID, func(_ context.Context, r *domain.Role) error { return r.SetPermissions(perms, catalog) })
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

// Catalog mantiene el catálogo de permisos al paso de lo que declaran los contextos delimitados, y
// los roles del sistema al paso del catálogo. El anfitrión llama a Sync al arrancar, tras migrar.
type Catalog struct{ s *service }

// CatalogSync informa de lo que cambió una sincronización.
type CatalogSync struct {
	Added, Reactivated, Deactivated, RolesUpdated int
}

// Sync declara los permisos de cada contexto (los propios siempre se incluyen): los códigos nuevos
// entran en el catálogo, los que ya nadie declara se desactivan y dejan de conceder, y los roles
// del sistema reciben, una sola vez, los permisos que les da la regla estándar: lo que el
// administrador global retiró a un rol del sistema no vuelve (D-98). Los roles propios nunca se
// tocan, a diferencia del sembrador de C#, que volvía a añadir lo que un administrador hubiera
// quitado. Es idempotente.
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
			if r.SystemSyncPending(catalog) {
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

// Bootstrap crea el primer administrador global de una instalación (decisión 7).
type Bootstrap struct{ s *service }

// Resultados de Bootstrap.Run.
const (
	BootstrapNotNeeded     = "an active global administrator already exists"
	BootstrapNoCredentials = "no bootstrap credentials: nothing created"
	BootstrapCreated       = "bootstrap administrator created"
)

// Run crea el administrador de arranque sólo cuando no existe ningún administrador global activo y
// se dan las dos credenciales (el anfitrión las lee del entorno, nunca de configuración
// versionada). El usuario tiene que cambiar la contraseña antes de hacer nada más. Si el nombre de
// usuario ya está cogido, no se crea nada ni se concede ningún rol: en C# el usuario existente se
// ascendía a administrador global conservando su propia contraseña. Antes hay que sincronizar el
// catálogo. Nunca escribe en el log ni devuelve la contraseña.
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
