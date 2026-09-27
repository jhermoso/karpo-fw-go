package domain

import (
	"context"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Repositories (contracts; implementations live in infrastructure). The data is maintained by
// migrations; the use cases of this phase only read.
type (
	BoundaryRepository   = fw.Repository[BoundaryID, *Boundary]
	PostalCodeRepository = fw.Repository[PostalCodeID, *PostalCode]
	CountryRepository    = fw.Repository[CountryID, *Country]
)

// Catalogs are the small reference lists, read whole.
type Catalogs interface {
	BoundaryTypes(ctx context.Context) ([]BoundaryType, error)
	Currencies(ctx context.Context) ([]Currency, error)
	Languages(ctx context.Context) ([]Language, error)
	TimeZones(ctx context.Context) ([]TimeZone, error)
	StreetTypes(ctx context.Context) ([]StreetType, error)
}
