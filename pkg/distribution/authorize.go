package distribution

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// OrganizationScopeHeader carries the organizations the caller wants to work on
// (comma-separated UUIDs); the effective scope is its intersection with the grants.
const OrganizationScopeHeader = "X-Organization-Scope"

// RetryAfterIndeterminate is the Retry-After (seconds) sent when authorization is indeterminate.
const RetryAfterIndeterminate = "5"

// Authorize authenticates the bearer token and resolves the authorization context once per
// request (contract v1), before the endpoints:
//
//   - invalid X-Organization-Scope -> 400
//   - missing/invalid credentials or incomplete principal -> 401 with WWW-Authenticate: Bearer
//   - Deny -> 403; Indeterminate -> 503 with Retry-After: 5 (fail closed)
//   - Allow -> authz.WithContext plus application.WithActor, so the audit stamps the resolved
//     subject and never an identity supplied by the client.
//
// It replaces TenantActorContext for authenticated APIs; there is no Legacy fallback.
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
			if ch := r.Header.Get(ChannelHeader); ch != "" {
				ctx = application.WithChannel(ctx, ch)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequirePermission protects one endpoint with a permission (the C# RequirePermission policy).
// Prefer pipeline.RequirePermission on the use case, which also protects non-HTTP entry points;
// this one is for endpoints without a handler.
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
