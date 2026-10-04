// Package distribution exposes the Security use cases over HTTP and adapts the token issuer of
// the host. The sign-in routes are public and mounted outside the authorization middleware; the
// rest needs an authorization context.
package distribution

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	sapp "github.com/jhermoso/karpo-fw-go/contexts/security/application"
	"github.com/jhermoso/karpo-fw-go/contexts/security/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// HS256Issuer adapts the shared-secret JWT of the framework to the TokenIssuer port. It is the
// only place where Security meets that token format: the token carries who the user is (sub,
// username, partyId) and nothing about what the user may do.
type HS256Issuer struct{ JWT *jwtauth.HS256 }

// IssueAccessToken implements application.TokenIssuer.
func (i HS256Issuer) IssueAccessToken(_ context.Context, subject fw.UUID, username string, party fw.UUID, expiresAt time.Time) (string, error) {
	return i.JWT.Issue(jwtauth.Claims{Subject: subject.String(), Username: username, PartyID: party.String(), ExpiresAt: expiresAt.Unix()})
}

// Module is the HTTP module of Security.
type Module struct{ svc *sapp.Service }

// NewModule builds the HTTP module.
func NewModule(svc *sapp.Service) *Module { return &Module{svc: svc} }

var _ distribution.EndpointModule = (*Module)(nil)

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var val fw.Validation
		val.Add("body", "json", "the body is not the expected JSON")
		return val.Err()
	}
	return nil
}

func pathID[T any](r *http.Request, name string, parse func(string) (T, error)) (T, error) {
	v, err := parse(r.PathValue(name))
	if err != nil {
		var zero T
		return zero, fmt.Errorf("%w: invalid %s", fw.ErrValidation, name)
	}
	return v, nil
}

// respond writes the result of a use case; responses with tokens or authorization data are never
// cached by intermediaries.
func respond[T any](w http.ResponseWriter, r *http.Request, v T, err error, status int) {
	w.Header().Set("Cache-Control", "no-store")
	distribution.Respond(w, r, v, err, status)
}

// command decodes the body into C and runs the use case.
func command[C, R any](handle func(context.Context, C) (R, error), prepare func(*http.Request, *C) error, status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var c C
		if r.ContentLength != 0 {
			if err := decode(r, &c); err != nil {
				distribution.WriteError(w, r, err)
				return
			}
		}
		if prepare != nil {
			if err := prepare(r, &c); err != nil {
				distribution.WriteError(w, r, err)
				return
			}
		}
		out, err := handle(r.Context(), c)
		if err == nil && status == http.StatusNoContent {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		respond(w, r, out, err, status)
	}
}

// RegisterPublicRoutes mounts the routes that authenticate: they receive credentials, not an
// access token, so they go outside distribution.Authorize.
func (m *Module) RegisterPublicRoutes(mux *http.ServeMux) {
	svc := m.svc
	mux.HandleFunc("POST /api/auth/login", command(svc.Login.Handle, nil, http.StatusOK))
	mux.HandleFunc("POST /api/auth/refresh", command(svc.Refresh.Handle, nil, http.StatusOK))
	mux.HandleFunc("POST /api/auth/logout", command(svc.Logout.Handle, nil, http.StatusNoContent))
}

// RegisterRoutes implements distribution.EndpointModule (routes behind distribution.Authorize).
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	svc := m.svc
	userID := func(r *http.Request) (domain.UserID, error) { return pathID(r, "id", domain.ParseUserID) }
	roleID := func(r *http.Request, name string) (domain.RoleID, error) { return pathID(r, name, domain.ParseRoleID) }

	mux.HandleFunc("POST /api/auth/change-password", command(svc.ChangePassword.Handle, nil, http.StatusOK))
	// The resolved authorization of the caller, in the shape of the contract (authz.Resolution):
	// what a front paints its menus with, and the base of a future Http resolver.
	mux.HandleFunc("GET /api/auth/context", func(w http.ResponseWriter, r *http.Request) {
		ac, ok := authz.FromContext(r.Context())
		if !ok {
			distribution.WriteError(w, r, fw.ErrUnauthorized)
			return
		}
		respond(w, r, authz.Allowed(ac), nil, http.StatusOK)
	})

	mux.HandleFunc("GET /api/security/users", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		page, err := svc.SearchUsers.Handle(r.Context(), sapp.SearchUsers{Text: q.Get("q"), Organization: q.Get("organization"),
			Role: q.Get("role"), ActiveOnly: q.Get("active") == "true", Administrable: q.Get("administrable") == "true",
			Page: atoi("page"), Size: atoi("size")})
		respond(w, r, page, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/security/users", command(svc.RegisterUser.Handle, nil, http.StatusCreated))
	mux.HandleFunc("GET /api/security/users/{id}", command(svc.GetUser.Handle, func(r *http.Request, q *sapp.GetUser) (err error) {
		q.ID, err = userID(r)
		return err
	}, http.StatusOK))
	mux.HandleFunc("PUT /api/security/users/{id}/username", command(svc.RenameUser.Handle, func(r *http.Request, c *sapp.RenameUser) (err error) {
		c.ID, err = userID(r)
		return err
	}, http.StatusOK))
	active := func(on bool) http.HandlerFunc {
		return command(svc.SetUserActive.Handle, func(r *http.Request, c *sapp.SetUserActive) (err error) {
			c.ID, err = userID(r)
			c.Active = on
			return err
		}, http.StatusOK)
	}
	mux.HandleFunc("POST /api/security/users/{id}/deactivate", active(false))
	mux.HandleFunc("POST /api/security/users/{id}/activate", active(true))
	mux.HandleFunc("POST /api/security/users/{id}/unlock", command(svc.UnlockUser.Handle, func(r *http.Request, c *sapp.UnlockUser) (err error) {
		c.ID, err = userID(r)
		return err
	}, http.StatusOK))
	mux.HandleFunc("POST /api/security/users/{id}/reset-password", command(svc.ResetPassword.Handle, func(r *http.Request, c *sapp.ResetPassword) (err error) {
		c.ID, err = userID(r)
		return err
	}, http.StatusOK))
	mux.HandleFunc("PUT /api/security/users/{id}/roles/{role}", command(svc.AssignRole.Handle, func(r *http.Request, c *sapp.AssignRole) (err error) {
		if c.ID, err = userID(r); err != nil {
			return err
		}
		c.Role, err = roleID(r, "role")
		return err
	}, http.StatusOK))
	mux.HandleFunc("DELETE /api/security/users/{id}/roles/{role}", command(svc.RevokeRole.Handle, func(r *http.Request, c *sapp.RevokeRole) (err error) {
		if c.ID, err = userID(r); err != nil {
			return err
		}
		c.Role, err = roleID(r, "role")
		return err
	}, http.StatusOK))
	mux.HandleFunc("PUT /api/security/users/{id}/organizations/{organization}", command(svc.GrantAccess.Handle, func(r *http.Request, c *sapp.GrantAccess) (err error) {
		if c.ID, err = userID(r); err != nil {
			return err
		}
		c.Organization, err = pathID(r, "organization", domain.ParseOrganizationID)
		return err
	}, http.StatusOK))
	mux.HandleFunc("DELETE /api/security/users/{id}/organizations/{organization}", command(svc.RevokeAccess.Handle, func(r *http.Request, c *sapp.RevokeAccess) (err error) {
		if c.ID, err = userID(r); err != nil {
			return err
		}
		c.Organization, err = pathID(r, "organization", domain.ParseOrganizationID)
		return err
	}, http.StatusOK))
	mux.HandleFunc("POST /api/security/users/{id}/identities", command(svc.LinkIdentity.Handle, func(r *http.Request, c *sapp.LinkIdentity) (err error) {
		c.ID, err = userID(r)
		return err
	}, http.StatusOK))
	mux.HandleFunc("DELETE /api/security/users/{id}/identities", command(svc.UnlinkIdentity.Handle, func(r *http.Request, c *sapp.UnlinkIdentity) (err error) {
		c.ID, err = userID(r)
		return err
	}, http.StatusOK))

	mux.HandleFunc("GET /api/security/roles", command(svc.ListRoles.Handle, nil, http.StatusOK))
	mux.HandleFunc("POST /api/security/roles", command(svc.DefineRole.Handle, nil, http.StatusCreated))
	mux.HandleFunc("GET /api/security/roles/{id}", command(svc.GetRole.Handle, func(r *http.Request, q *sapp.GetRole) (err error) {
		q.ID, err = roleID(r, "id")
		return err
	}, http.StatusOK))
	mux.HandleFunc("PUT /api/security/roles/{id}", command(svc.DescribeRole.Handle, func(r *http.Request, c *sapp.DescribeRole) (err error) {
		c.ID, err = roleID(r, "id")
		return err
	}, http.StatusOK))
	mux.HandleFunc("PUT /api/security/roles/{id}/permissions", command(svc.SetRolePermissions.Handle, func(r *http.Request, c *sapp.SetRolePermissions) (err error) {
		c.ID, err = roleID(r, "id")
		return err
	}, http.StatusOK))
	mux.HandleFunc("DELETE /api/security/roles/{id}", command(svc.RetireRole.Handle, func(r *http.Request, c *sapp.RetireRole) (err error) {
		c.ID, err = roleID(r, "id")
		return err
	}, http.StatusNoContent))
	mux.HandleFunc("GET /api/security/permissions", command(svc.ListPermissions.Handle, nil, http.StatusOK))
}
