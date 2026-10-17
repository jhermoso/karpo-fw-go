package authz

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// ScopeSpec restringe una consulta a las organizaciones del ámbito de autorización de ctx: la
// contrapartida en Go de los filtros IOrganizationScopeProvider de C# (159 ficheros), expresada
// como especificación para que el filtro se ejecute en la base de datos como cualquier otro.
//
//	s := authz.ScopeSpec(ctx, FieldOwner, func(u domain.UUID) OrgID { return OrgID{u} })
//	page, err := repo.FindPage(ctx, s.And(userFilter), pageReq)
//
// Los administradores globales reciben todas las filas; sin contexto de autorización no coincide ninguna (falla cerrado).
func ScopeSpec[T any, ID comparable](ctx context.Context, owner spec.Field[T, ID], wrap func(domain.UUID) ID) spec.Spec[T] {
	c, ok := FromContext(ctx)
	if !ok {
		return spec.None[T]()
	}
	if c.GlobalAdmin {
		return spec.All[T]()
	}
	ids := make([]ID, len(c.EffectiveOrganizations))
	for i, u := range c.EffectiveOrganizations {
		ids[i] = wrap(u)
	}
	return owner.In(ids...)
}
