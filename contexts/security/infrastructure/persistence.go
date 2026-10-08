package infrastructure

import (
	"context"
	"fmt"

	parties "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/security/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

func opt(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// UserMapping maps User to sec_users and its three child tables.
func UserMapping() sqlrepo.Mapping[domain.UserID, *domain.User] {
	return sqlrepo.Mapping[domain.UserID, *domain.User]{
		Table: "sec_users",
		Columns: sqlrepo.WithAuditColumns("party", "username", "username_key", "password_hash", "must_change_password",
			"failed_attempts", "locked_until", "last_login_at", "active"),
		Dehydrate: func(u *domain.User) (sqlrepo.Values, error) {
			return sqlrepo.AuditStampValues(sqlrepo.Values{"party": u.Party(), "username": u.Username(), "username_key": u.UsernameKey(),
				"password_hash": opt(u.PasswordHash()), "must_change_password": u.MustChangePassword(),
				"failed_attempts": int64(u.FailedAttempts()), "locked_until": u.LockedUntil(), "last_login_at": u.LastLoginAt(),
				"active": u.IsActive()}, u.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.User, error) {
			s := domain.UserState{Party: domain.PartyID{UUID: r.UUID("party")}, Username: r.String("username"),
				PasswordHash: r.String("password_hash"), MustChangePassword: r.Bool("must_change_password"),
				FailedAttempts: int(r.Int64("failed_attempts")), LockedUntil: r.NullTime("locked_until"),
				LastLoginAt: r.NullTime("last_login_at"), Active: r.Bool("active"), Audit: r.AuditStamp()}
			for _, c := range children.Of("roles") {
				s.Roles = append(s.Roles, domain.RoleID{UUID: c.UUID("role_id")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			for _, c := range children.Of("accesses") {
				s.Accesses = append(s.Accesses, domain.OrganizationAccess{Organization: domain.OrganizationID{UUID: c.UUID("organization")},
					Level: domain.AccessLevel(c.String("access_level")), IncludeSubsidiaries: c.Bool("include_subsidiaries")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			for _, c := range children.Of("identities") {
				s.Identities = append(s.Identities, domain.ExternalIdentity{Issuer: c.String("issuer"), Subject: c.String("subject")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteUser(domain.UserID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.User]{{
			Name: "roles", Table: "sec_user_roles", ForeignKey: "user_id", Columns: []string{"role_id"},
			Dehydrate: func(u *domain.User) ([]sqlrepo.Values, error) {
				var out []sqlrepo.Values
				for _, r := range u.Roles() {
					out = append(out, sqlrepo.Values{"role_id": r})
				}
				return out, nil
			},
		}, {
			Name: "accesses", Table: "sec_user_accesses", ForeignKey: "user_id",
			Columns: []string{"organization", "access_level", "include_subsidiaries"},
			Dehydrate: func(u *domain.User) ([]sqlrepo.Values, error) {
				var out []sqlrepo.Values
				for _, a := range u.Accesses() {
					out = append(out, sqlrepo.Values{"organization": a.Organization, "access_level": string(a.Level),
						"include_subsidiaries": a.IncludeSubsidiaries})
				}
				return out, nil
			},
		}, {
			Name: "identities", Table: "sec_user_identities", ForeignKey: "user_id", Columns: []string{"issuer", "subject"},
			Dehydrate: func(u *domain.User) ([]sqlrepo.Values, error) {
				var out []sqlrepo.Values
				for _, i := range u.Identities() {
					out = append(out, sqlrepo.Values{"issuer": i.Issuer, "subject": i.Subject})
				}
				return out, nil
			},
		}},
	}
}

// RoleMapping maps Role to sec_roles and sec_role_permissions.
func RoleMapping() sqlrepo.Mapping[domain.RoleID, *domain.Role] {
	return sqlrepo.Mapping[domain.RoleID, *domain.Role]{
		Table:   "sec_roles",
		Columns: sqlrepo.WithAuditColumns("name", "name_key", "description", "system_role"),
		Dehydrate: func(r *domain.Role) (sqlrepo.Values, error) {
			return sqlrepo.AuditStampValues(sqlrepo.Values{"name": r.Name(), "name_key": r.NameKey(),
				"description": opt(r.Description()), "system_role": r.IsSystem()}, r.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Role, error) {
			s := domain.RoleState{Name: r.String("name"), Description: r.String("description"), System: r.Bool("system_role"),
				Audit: r.AuditStamp()}
			for _, c := range children.Of("permissions") {
				s.Permissions = append(s.Permissions, domain.Permission(c.String("code")))
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteRole(domain.RoleID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Role]{{
			Name: "permissions", Table: "sec_role_permissions", ForeignKey: "role_id", Columns: []string{"code"},
			Dehydrate: func(r *domain.Role) ([]sqlrepo.Values, error) {
				var out []sqlrepo.Values
				for _, p := range r.Permissions() {
					out = append(out, sqlrepo.Values{"code": string(p)})
				}
				return out, nil
			},
		}},
	}
}

// PermissionMapping maps the catalog entries to sec_permissions.
func PermissionMapping() sqlrepo.Mapping[domain.PermissionID, *domain.PermissionEntry] {
	return sqlrepo.Mapping[domain.PermissionID, *domain.PermissionEntry]{
		Table:   "sec_permissions",
		Columns: []string{"code", "description", "active"},
		Dehydrate: func(p *domain.PermissionEntry) (sqlrepo.Values, error) {
			return sqlrepo.Values{"code": string(p.Code()), "description": opt(p.Description()), "active": p.IsActive()}, nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.PermissionEntry, error) {
			code, description, active := r.String("code"), r.String("description"), r.Bool("active")
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstitutePermission(domain.Permission(code), description, active)
		},
	}
}

// SessionMapping maps Session to sec_sessions.
func SessionMapping() sqlrepo.Mapping[domain.SessionID, *domain.Session] {
	return sqlrepo.Mapping[domain.SessionID, *domain.Session]{
		Table:   "sec_sessions",
		Columns: []string{"user_id", "family", "token_hash", "issued_at", "expires_at", "ended_at", "reason"},
		Dehydrate: func(s *domain.Session) (sqlrepo.Values, error) {
			return sqlrepo.Values{"user_id": s.User(), "family": s.Family(), "token_hash": s.TokenHash(), "issued_at": s.IssuedAt(),
				"expires_at": s.ExpiresAt(), "ended_at": s.EndedAt(), "reason": opt(s.Reason())}, nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Session, error) {
			s := domain.SessionState{User: domain.UserID{UUID: r.UUID("user_id")}, Family: r.UUID("family"),
				TokenHash: r.String("token_hash"), IssuedAt: r.Time("issued_at"), ExpiresAt: r.Time("expires_at"),
				EndedAt: r.NullTime("ended_at"), Reason: r.String("reason")}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteSession(domain.SessionID{UUID: r.UUID("id")}, s)
		},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("security: unsupported backend %T", b) }

// UserRepositoryFactory builds the user repository.
func UserRepositoryFactory(b hotswap.Backend) (domain.UserRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, UserMapping())
	case *memory.Store:
		return memory.NewRepository[domain.UserID, *domain.User](db), nil
	}
	return nil, unsupported(b)
}

// RoleRepositoryFactory builds the role repository.
func RoleRepositoryFactory(b hotswap.Backend) (domain.RoleRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, RoleMapping())
	case *memory.Store:
		return memory.NewRepository[domain.RoleID, *domain.Role](db), nil
	}
	return nil, unsupported(b)
}

// PermissionRepositoryFactory builds the permission catalog repository.
func PermissionRepositoryFactory(b hotswap.Backend) (domain.PermissionRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, PermissionMapping())
	case *memory.Store:
		return memory.NewRepository[domain.PermissionID, *domain.PermissionEntry](db), nil
	}
	return nil, unsupported(b)
}

// SessionRepositoryFactory builds the session repository.
func SessionRepositoryFactory(b hotswap.Backend) (domain.SessionRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, SessionMapping())
	case *memory.Store:
		return memory.NewRepository[domain.SessionID, *domain.Session](db), nil
	}
	return nil, unsupported(b)
}

// OutboxFactory builds the domain event outbox.
func OutboxFactory(b hotswap.Backend) (application.OutboxStore, error) { return outbox(b, TableOutbox) }

// IntegrationOutboxFactory builds the Published Language outbox.
func IntegrationOutboxFactory(b hotswap.Backend) (application.OutboxStore, error) {
	return outbox(b, TableIntegrationOutbox)
}

func outbox(b hotswap.Backend, table string) (application.OutboxStore, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewOutbox(db, table)
	case *memory.Store:
		return memory.NewOutbox(db), nil
	}
	return nil, unsupported(b)
}

// AuditLogFactory builds the audit log.
func AuditLogFactory(b hotswap.Backend) (application.AuditLog, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewAuditLog(db, TableAuditLog)
	case *memory.Store:
		return memory.NewAuditLog(db), nil
	}
	return nil, unsupported(b)
}

// PartiesDirectory adapts the Parties directory to the ports of Security (ACL): the party of a
// user must exist, and accesses are granted to internal organizations only.
type PartiesDirectory struct {
	Directory     parties.Directory
	Organizations parties.InternalOrganizationCatalog
}

// PartyExists implements application.PartyDirectory.
func (p PartiesDirectory) PartyExists(ctx context.Context, party fw.UUID) (bool, error) {
	if party == domain.BootstrapAdminParty.UUID { // the reserved technical party is never in Parties
		return true, nil
	}
	found, err := p.Directory.Resolve(ctx, []string{party.String()})
	if err != nil {
		return false, err
	}
	_, ok := found[party.String()]
	return ok, nil
}

// IsInternalOrganization implements application.InternalOrganizations.
func (p PartiesDirectory) IsInternalOrganization(ctx context.Context, organization fw.UUID) (bool, error) {
	all, err := p.Organizations.All(ctx)
	if err != nil {
		return false, err
	}
	for _, o := range all {
		if o.ID == organization.String() {
			return true, nil
		}
	}
	return false, nil
}
