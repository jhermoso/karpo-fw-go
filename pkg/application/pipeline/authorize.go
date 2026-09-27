package pipeline

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
)

// RequirePermission rejects the request unless the authorization context of ctx grants p
// (domain.ErrUnauthorized without context, domain.ErrForbidden without permission). Unlike an
// HTTP policy it protects every entry point of the handler: HTTP, jobs, messages and tests.
func RequirePermission[Req, Res any](p authz.Permission) application.Middleware[Req, Res] {
	p = authz.MustPermission(string(p))
	return func(next application.Handler[Req, Res]) application.Handler[Req, Res] {
		return application.HandlerFunc[Req, Res](func(ctx context.Context, req Req) (Res, error) {
			if err := authz.Require(ctx, p); err != nil {
				var zero Res
				return zero, err
			}
			return next.Handle(ctx, req)
		})
	}
}
