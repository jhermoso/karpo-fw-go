package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
)

// mapParents devuelve, para cada límite, el code de su primer antecesor en la escalera del mapa.
// Sube por rondas, una consulta (por tramos de 500) por ronda, como CountryOf: nunca una por
// límite.
func mapParents(ctx context.Context, p Ports, bs []*domain.Boundary) (map[domain.BoundaryID]string, error) {
	return domain.MapParents(bs, MaxDepth, func(ids []domain.BoundaryID) ([]*domain.Boundary, error) {
		return p.boundaries(ctx, ids)
	})
}
