package application

import (
	"context"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/security/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Service exposes the use cases.
type Service struct {
	// Authentication with the own password. Login, Refresh and Logout are anonymous; ChangePassword
	// needs an authenticated caller and no permission.
	Login          app.CommandHandler[Login, Tokens]
	Refresh        app.CommandHandler[Refresh, Tokens]
	Logout         app.CommandHandler[Logout, Done]
	ChangePassword app.CommandHandler[ChangePassword, Tokens]

	RegisterUser   app.CommandHandler[RegisterUser, UserDTO]
	RenameUser     app.CommandHandler[RenameUser, UserDTO]
	SetUserActive  app.CommandHandler[SetUserActive, UserDTO]
	UnlockUser     app.CommandHandler[UnlockUser, UserDTO]
	ResetPassword  app.CommandHandler[ResetPassword, UserDTO]
	AssignRole     app.CommandHandler[AssignRole, UserDTO]
	RevokeRole     app.CommandHandler[RevokeRole, UserDTO]
	GrantAccess    app.CommandHandler[GrantAccess, UserDTO]
	RevokeAccess   app.CommandHandler[RevokeAccess, UserDTO]
	LinkIdentity   app.CommandHandler[LinkIdentity, UserDTO]
	UnlinkIdentity app.CommandHandler[UnlinkIdentity, UserDTO]
	GetUser        app.QueryHandler[GetUser, UserDTO]
	SearchUsers    app.QueryHandler[SearchUsers, fw.Page[UserDTO]]

	DefineRole         app.CommandHandler[DefineRole, RoleDTO]
	DescribeRole       app.CommandHandler[DescribeRole, RoleDTO]
	SetRolePermissions app.CommandHandler[SetRolePermissions, RoleDTO]
	RetireRole         app.CommandHandler[RetireRole, Done]
	GetRole            app.QueryHandler[GetRole, RoleDTO]
	ListRoles          app.QueryHandler[ListRoles, []RoleDTO]
	ListPermissions    app.QueryHandler[ListPermissions, []PermissionDTO]

	// Catalog keeps the permission catalog and the system roles; Bootstrap creates the first
	// global administrator. Both are for the host, not for users.
	Catalog   Catalog
	Bootstrap Bootstrap
}

type service struct {
	Deps
	users     *orchestration.Orchestrator[domain.UserID, *domain.User]
	roles     *orchestration.Orchestrator[domain.RoleID, *domain.Role]
	dummyHash string // verified when there is no user to check, so every failed login costs the same
}

// NewService wires the use cases.
func NewService(d Deps) *Service {
	d = d.withDefaults()
	var opts []orchestration.Option
	if d.Recorder != nil {
		opts = append(opts, orchestration.WithOutbox(d.Recorder))
	}
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	s := &service{Deps: d,
		users: orchestration.New[domain.UserID, *domain.User](d.Users, d.UoW, opts...),
		roles: orchestration.New[domain.RoleID, *domain.Role](d.Roles, d.UoW, opts...)}
	s.dummyHash, _ = d.Hasher.Hash("no user holds this password")
	retries, backoff := 3, 10*time.Millisecond

	svc := &Service{Catalog: Catalog{s: s}, Bootstrap: Bootstrap{s: s}}
	svc.Login = app.Chain[Login, Tokens](app.HandlerFunc[Login, Tokens](s.login), pipeline.RetryOnConflict[Login, Tokens](retries, backoff))
	svc.Refresh = app.HandlerFunc[Refresh, Tokens](s.refresh)
	svc.Logout = app.HandlerFunc[Logout, Done](s.logout)
	svc.ChangePassword = app.Chain[ChangePassword, Tokens](app.HandlerFunc[ChangePassword, Tokens](s.changePassword),
		pipeline.RetryOnConflict[ChangePassword, Tokens](retries, backoff))

	svc.RegisterUser = guard(PermUserCreate, s.registerUser)
	svc.RenameUser = guard(PermUserUpdate, func(ctx context.Context, c RenameUser) (UserDTO, error) {
		return s.administer(ctx, c.ID, func(ctx context.Context, _ domain.Administrator, u *domain.User) error {
			if err := s.usernameFree(ctx, c.Username, u.ID()); err != nil {
				return err
			}
			return u.Rename(c.Username)
		})
	}, pipeline.RetryOnConflict[RenameUser, UserDTO](retries, backoff))
	svc.SetUserActive = guard(PermUserUpdate, func(ctx context.Context, c SetUserActive) (UserDTO, error) {
		return s.administer(ctx, c.ID, func(ctx context.Context, _ domain.Administrator, u *domain.User) error {
			if c.Active {
				u.Activate()
				return nil
			}
			if err := s.keepsGlobalAdmin(ctx, u); err != nil {
				return err
			}
			u.Deactivate()
			return s.endSessions(ctx, domain.LiveSessionsOf(u.ID()), domain.EndedDeactivated, fw.Now())
		})
	}, pipeline.RetryOnConflict[SetUserActive, UserDTO](retries, backoff))
	svc.UnlockUser = guard(PermUserUpdate, func(ctx context.Context, c UnlockUser) (UserDTO, error) {
		return s.administer(ctx, c.ID, func(_ context.Context, _ domain.Administrator, u *domain.User) error {
			u.Unlock()
			return nil
		})
	}, pipeline.RetryOnConflict[UnlockUser, UserDTO](retries, backoff))
	svc.ResetPassword = guard(PermUserUpdate, func(ctx context.Context, c ResetPassword) (UserDTO, error) {
		a, err := administratorOf(ctx)
		if err != nil {
			return UserDTO{}, err
		}
		target, err := d.Users.Get(ctx, c.ID)
		if err != nil {
			return UserDTO{}, err
		}
		if !a.Sees(target) && a.User != c.ID {
			return UserDTO{}, fw.NotFound(domain.UserKind, c.ID)
		}
		if err := a.CanAdminister(target); err != nil {
			return UserDTO{}, err
		}
		if err := domain.CheckPassword(c.Password, target.Username()); err != nil {
			return UserDTO{}, err
		}
		hash, err := d.Hasher.Hash(c.Password) // computed outside the transaction: it is slow on purpose
		if err != nil {
			return UserDTO{}, err
		}
		return s.administer(ctx, c.ID, func(ctx context.Context, _ domain.Administrator, u *domain.User) error {
			if err := u.SetPassword(hash, true, true); err != nil {
				return err
			}
			return s.endSessions(ctx, domain.LiveSessionsOf(u.ID()), domain.EndedPassword, fw.Now())
		})
	})
	svc.AssignRole = guard(PermUserRole, func(ctx context.Context, c AssignRole) (UserDTO, error) {
		return s.administer(ctx, c.ID, func(ctx context.Context, a domain.Administrator, u *domain.User) error {
			role, err := d.Roles.Get(ctx, c.Role)
			if err != nil {
				return err
			}
			if err := a.CanAssign(role); err != nil {
				return err
			}
			return u.AssignRole(role)
		})
	}, pipeline.RetryOnConflict[AssignRole, UserDTO](retries, backoff))
	svc.RevokeRole = guard(PermUserRole, func(ctx context.Context, c RevokeRole) (UserDTO, error) {
		return s.administer(ctx, c.ID, func(ctx context.Context, a domain.Administrator, u *domain.User) error {
			role, err := d.Roles.Get(ctx, c.Role)
			if err != nil {
				return err
			}
			if err := a.CanAssign(role); err != nil {
				return err
			}
			if c.Role == domain.RoleGlobalSuperAdmin {
				if err := s.keepsGlobalAdmin(ctx, u); err != nil {
					return err
				}
			}
			u.RevokeRole(c.Role)
			return nil
		})
	}, pipeline.RetryOnConflict[RevokeRole, UserDTO](retries, backoff))
	svc.GrantAccess = guard(PermAccessUpdate, func(ctx context.Context, c GrantAccess) (UserDTO, error) {
		return s.changeAccess(ctx, c.ID, c.Organization, func(ctx context.Context, u *domain.User) error {
			access, err := s.parseAccess(ctx, c.Organization, c.Level, c.IncludeSubsidiaries)
			if err != nil {
				return err
			}
			return u.GrantAccess(access)
		})
	}, pipeline.RetryOnConflict[GrantAccess, UserDTO](retries, backoff))
	svc.RevokeAccess = guard(PermAccessUpdate, func(ctx context.Context, c RevokeAccess) (UserDTO, error) {
		return s.changeAccess(ctx, c.ID, c.Organization, func(_ context.Context, u *domain.User) error {
			u.RevokeAccess(c.Organization)
			return nil
		})
	}, pipeline.RetryOnConflict[RevokeAccess, UserDTO](retries, backoff))
	svc.LinkIdentity = guard(PermUserUpdate, func(ctx context.Context, c LinkIdentity) (UserDTO, error) {
		id, err := domain.NewExternalIdentity(c.Issuer, c.Subject)
		if err != nil {
			return UserDTO{}, err
		}
		return s.administer(ctx, c.ID, func(ctx context.Context, _ domain.Administrator, u *domain.User) error {
			taken, err := d.Users.Exists(ctx, domain.WithIdentity(id).And(domain.UserFieldID.Ne(u.ID())))
			if err != nil {
				return err
			}
			if taken {
				return fw.Violation("security.identity_taken", "the identity is already linked with another user")
			}
			return u.LinkIdentity(id)
		})
	}, pipeline.RetryOnConflict[LinkIdentity, UserDTO](retries, backoff))
	svc.UnlinkIdentity = guard(PermUserUpdate, func(ctx context.Context, c UnlinkIdentity) (UserDTO, error) {
		return s.administer(ctx, c.ID, func(_ context.Context, _ domain.Administrator, u *domain.User) error {
			u.UnlinkIdentity(domain.ExternalIdentity{Issuer: c.Issuer, Subject: c.Subject})
			return nil
		})
	}, pipeline.RetryOnConflict[UnlinkIdentity, UserDTO](retries, backoff))
	svc.GetUser = guard(PermUserRead, func(ctx context.Context, q GetUser) (UserDTO, error) {
		a, err := administratorOf(ctx)
		if err != nil {
			return UserDTO{}, err
		}
		u, err := d.Users.Get(ctx, q.ID)
		if err != nil {
			return UserDTO{}, err
		}
		if !a.Sees(u) && a.User != u.ID() {
			return UserDTO{}, fw.NotFound(domain.UserKind, q.ID)
		}
		return s.userResult(ctx, u, nil)
	})
	svc.SearchUsers = guard(PermUserRead, s.searchUsers)

	svc.DefineRole = guard(PermRoleUpdate, s.defineRole)
	svc.DescribeRole = guard(PermRoleUpdate, s.describeRole, pipeline.RetryOnConflict[DescribeRole, RoleDTO](retries, backoff))
	svc.SetRolePermissions = guard(PermRoleUpdate, s.setRolePermissions, pipeline.RetryOnConflict[SetRolePermissions, RoleDTO](retries, backoff))
	svc.RetireRole = guard(PermRoleUpdate, s.retireRole)
	svc.GetRole = guard(PermRoleRead, func(ctx context.Context, q GetRole) (RoleDTO, error) {
		r, err := d.Roles.Get(ctx, q.ID)
		if err != nil {
			return RoleDTO{}, err
		}
		return roleDTO(r), nil
	})
	svc.ListRoles = guard(PermRoleRead, func(ctx context.Context, _ ListRoles) ([]RoleDTO, error) {
		roles, err := d.Roles.Find(ctx, nil, domain.RoleFieldName.Asc())
		if err != nil {
			return nil, err
		}
		return app.MapSlice(roles, roleDTO), nil
	})
	svc.ListPermissions = guard(PermRoleRead, func(ctx context.Context, _ ListPermissions) ([]PermissionDTO, error) {
		entries, err := d.Permissions.Find(ctx, nil)
		if err != nil {
			return nil, err
		}
		return permissionDTOs(entries), nil
	})
	return svc
}
