// Package distribution expone por HTTP los casos de uso de Security y adapta el emisor de tokens
// del anfitrión. Las rutas de inicio de sesión son públicas y se montan fuera del middleware de
// autorización; el resto necesita un contexto de autorización.
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

// HS256Issuer adapta el JWT de secreto compartido del framework al puerto TokenIssuer. Es el único
// sitio donde Security se encuentra con ese formato de token: el token lleva quién es el usuario
// (sub, username, partyId) y nada sobre lo que puede hacer.
type HS256Issuer struct{ JWT *jwtauth.HS256 }

// IssueAccessToken implementa application.TokenIssuer.
func (i HS256Issuer) IssueAccessToken(_ context.Context, subject fw.UUID, username string, party fw.UUID, expiresAt time.Time) (string, error) {
	return i.JWT.Issue(jwtauth.Claims{Subject: subject.String(), Username: username, PartyID: party.String(), ExpiresAt: expiresAt.Unix()})
}

// Module es el módulo HTTP de Security.
type Module struct{ svc *sapp.Service }

// NewModule construye el módulo HTTP.
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

// respond escribe el resultado de un caso de uso; los intermediarios nunca guardan en caché las
// respuestas con tokens o datos de autorización.
func respond[T any](w http.ResponseWriter, r *http.Request, v T, err error, status int) {
	w.Header().Set("Cache-Control", "no-store")
	distribution.Respond(w, r, v, err, status)
}

// command decodifica el cuerpo en C y ejecuta el caso de uso.
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

// RegisterPublicRoutes monta las rutas que autentican: reciben credenciales, no un token de
// acceso, así que van fuera de distribution.Authorize.
func (m *Module) RegisterPublicRoutes(mux *http.ServeMux) {
	svc := m.svc
	mux.HandleFunc("POST /api/auth/login", command(svc.Login.Handle, nil, http.StatusOK))
	mux.HandleFunc("POST /api/auth/refresh", command(svc.Refresh.Handle, nil, http.StatusOK))
	mux.HandleFunc("POST /api/auth/logout", command(svc.Logout.Handle, nil, http.StatusNoContent))
}

// RegisterRoutes implementa distribution.EndpointModule (rutas tras distribution.Authorize).
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	svc := m.svc
	userID := func(r *http.Request) (domain.UserID, error) { return pathID(r, "id", domain.ParseUserID) }
	roleID := func(r *http.Request, name string) (domain.RoleID, error) { return pathID(r, name, domain.ParseRoleID) }

	mux.HandleFunc("POST /api/auth/change-password", command(svc.ChangePassword.Handle, nil, http.StatusOK))
	// La autorización resuelta de quien llama, con la forma del contrato (authz.Resolution): con lo
	// que un front pinta sus menús, y la base de un futuro resolvedor Http.
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
