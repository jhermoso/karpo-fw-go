package geography

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// catalogs follows the backend of the switch.
type catalogs struct {
	b *hotswap.Binding[domain.Catalogs]
}

func with[T any](ctx context.Context, b *hotswap.Binding[domain.Catalogs], fn func(domain.Catalogs, context.Context) (T, error)) (out T, err error) {
	err = b.With(ctx, func(ctx context.Context, c domain.Catalogs) error { out, err = fn(c, ctx); return err })
	return out, err
}

func (c catalogs) BoundaryTypes(ctx context.Context) ([]domain.BoundaryType, error) {
	return with(ctx, c.b, domain.Catalogs.BoundaryTypes)
}

func (c catalogs) Currencies(ctx context.Context) ([]domain.Currency, error) {
	return with(ctx, c.b, domain.Catalogs.Currencies)
}

func (c catalogs) Languages(ctx context.Context) ([]domain.Language, error) {
	return with(ctx, c.b, domain.Catalogs.Languages)
}

func (c catalogs) TimeZones(ctx context.Context) ([]domain.TimeZone, error) {
	return with(ctx, c.b, domain.Catalogs.TimeZones)
}

func (c catalogs) StreetTypes(ctx context.Context) ([]domain.StreetType, error) {
	return with(ctx, c.b, domain.Catalogs.StreetTypes)
}
