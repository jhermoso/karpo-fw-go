package domain

import (
	"context"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Repositorios (contratos; las implementaciones están en infrastructure). Los datos se mantienen
// con migraciones; los casos de uso de esta fase sólo leen.
type (
	BoundaryRepository   = fw.Repository[BoundaryID, *Boundary]
	PostalCodeRepository = fw.Repository[PostalCodeID, *PostalCode]
	CountryRepository    = fw.Repository[CountryID, *Country]
)

// Catalogs son las listas de referencia pequeñas, que se leen enteras.
type Catalogs interface {
	BoundaryTypes(ctx context.Context) ([]BoundaryType, error)
	CountryLevels(ctx context.Context) ([]CountryLevel, error)
	Currencies(ctx context.Context) ([]Currency, error)
	Languages(ctx context.Context) ([]Language, error)
	TimeZones(ctx context.Context) ([]TimeZone, error)
	StreetTypes(ctx context.Context) ([]StreetType, error)
}
