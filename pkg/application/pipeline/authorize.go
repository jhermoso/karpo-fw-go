package pipeline

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
)

// RequirePermission rechaza la petición salvo que el contexto de autorización de ctx conceda p
// (domain.ErrUnauthorized sin contexto, domain.ErrForbidden sin permiso). A diferencia de una
// política HTTP, protege todos los puntos de entrada del manejador: HTTP, trabajos, mensajes y pruebas.
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
