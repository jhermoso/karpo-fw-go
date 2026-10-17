package distribution

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// OrganizationScopeHeader lleva las organizaciones con las que quiere trabajar quien llama
// (UUID separados por comas); el ámbito efectivo es su intersección con las concesiones.
const OrganizationScopeHeader = "X-Organization-Scope"

// RetryAfterIndeterminate es el Retry-After (segundos) que se envía cuando la autorización es indeterminada.
const RetryAfterIndeterminate = "5"

// Authorize autentica el token bearer y resuelve el contexto de autorización una vez por
// petición (contrato v1), antes de los endpoints:
//
//   - X-Organization-Scope no válido -> 400
//   - credenciales ausentes o no válidas, o principal incompleto -> 401 con WWW-Authenticate: Bearer
//   - Deny -> 403; Indeterminate -> 503 con Retry-After: 5 (falla cerrado)
//   - Allow -> authz.WithContext más application.WithActor, de modo que la auditoría sella el
//     sujeto resuelto y nunca una identidad aportada por el cliente.
//
// Sustituye a TenantActorContext en las API autenticadas; no hay alternativa Legacy.
func Authorize(authn authz.Authenticator, resolver authz.Resolver) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			requested, err := parseScope(r.Header.Get(OrganizationScopeHeader))
			if err != nil {
				WriteJSON(w, http.StatusBadRequest, ProblemDetails{Status: http.StatusBadRequest,
					Title: "Invalid Organization Scope", Detail: "X-Organization-Scope must hold organization identifiers"})
				return
			}
			credentials := r.Header.Get("Authorization")
			p, err := authn.Authenticate(ctx, credentials)
			if err != nil {
				unauthorized(w, r, err)
				return
			}
			res := resolver.Resolve(ctx, p, authz.Request{Credentials: credentials,
				RequestedOrganizations: requested, CorrelationID: application.CorrelationID(ctx)})
			if res.Outcome == authz.Allow && res.Context == nil {
				res = authz.Undetermined("allow-without-context")
			}
			if err := res.Err(); err != nil {
				switch {
				case errors.Is(err, domain.ErrUnauthorized):
					unauthorized(w, r, err)
				case errors.Is(err, authz.ErrIndeterminate):
					w.Header().Set("Retry-After", RetryAfterIndeterminate)
					WriteJSON(w, http.StatusServiceUnavailable, ProblemDetails{Status: http.StatusServiceUnavailable,
						Title: "Service Unavailable", Detail: "authorization could not be determined"})
				default:
					WriteError(w, r, err)
				}
				return
			}
			ctx = authz.WithContext(ctx, res.Context)
			ctx = application.WithActor(ctx, res.Context.Actor())
			noteActor(ctx)
			if ch := r.Header.Get(ChannelHeader); ch != "" {
				ctx = application.WithChannel(ctx, ch)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequirePermission protege un endpoint con un permiso (la política RequirePermission de C#).
// Es preferible pipeline.RequirePermission en el caso de uso, que protege también las entradas
// que no son HTTP; ésta es para endpoints sin manejador.
func RequirePermission(p authz.Permission, next http.Handler) http.Handler {
	p = authz.MustPermission(string(p))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := authz.Require(r.Context(), p); err != nil {
			WriteError(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func unauthorized(w http.ResponseWriter, r *http.Request, err error) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	WriteError(w, r, err)
}

func parseScope(h string) ([]domain.UUID, error) {
	if strings.TrimSpace(h) == "" {
		return nil, nil
	}
	var out []domain.UUID
	for _, s := range strings.Split(h, ",") {
		id, err := domain.ParseUUID(strings.TrimSpace(s))
		if err != nil || id.IsZero() {
			return nil, domain.ErrValidation
		}
		out = append(out, id)
	}
	return out, nil
}
