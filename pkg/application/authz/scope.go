package authz

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// ScopeSpec restricts a query to the organizations in the authorization scope of ctx: the Go
// counterpart of the C# IOrganizationScopeProvider filters (159 files), expressed as a
// specification so the filter runs in the database like any other.
//
//	s := authz.ScopeSpec(ctx, FieldOwner, func(u domain.UUID) OrgID { return OrgID{u} })
//	page, err := repo.FindPage(ctx, s.And(userFilter), pageReq)
//
// Global admins get every row; without an authorization context no row matches (fail closed).
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
