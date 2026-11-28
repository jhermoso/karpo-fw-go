package infrastructure

import (
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
)

// MapKey es una fila de la tabla de claves del mapa: una entidad de la tesela de delimitaciones,
// con la misma clave y el mismo nivel que da la API de geografía.
type MapKey struct {
	Code, ParentCode, Name string
	Level, Kind            string
	HasChildren            bool
	LAU, INE               string
	IslandOf               string // la isla del municipio insular, para disolver su geometría
}

// MapKeyHeader son las columnas de keys.csv.
var MapKeyHeader = []string{"code", "parentCode", "name", "level", "kind", "hasChildren", "lauCode", "ine", "islandOf"}

// Row devuelve la fila de keys.csv.
func (k MapKey) Row() []string {
	has := "false"
	if k.HasChildren {
		has = "true"
	}
	return []string{k.Code, k.ParentCode, k.Name, k.Level, k.Kind, has, k.LAU, k.INE, k.IslandOf}
}

// MapKeys construye la tabla de claves del mapa a partir de la semilla, con la misma regla
// (domain.MapCode y domain.MapParents) que la API. Sólo entran los límites activos de la escalera,
// ordenados por clave.
func MapKeys(s *Seed, maxDepth int) ([]MapKey, error) {
	byID := make(map[domain.BoundaryID]*domain.Boundary, len(s.Boundaries))
	for _, b := range s.Boundaries {
		byID[b.ID()] = b
	}
	parents, err := domain.MapParents(s.Boundaries, maxDepth, func(ids []domain.BoundaryID) ([]*domain.Boundary, error) {
		out := make([]*domain.Boundary, 0, len(ids))
		for _, id := range ids {
			if b, ok := byID[id]; ok {
				out = append(out, b)
			}
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	var keys []MapKey
	kindOf := map[string]string{}
	for _, b := range s.Boundaries {
		code, kind, ok := b.MapCode()
		if !ok || !b.IsActive() {
			continue
		}
		kindOf[code] = kind
		keys = append(keys, MapKey{Code: code, ParentCode: parents[b.ID()], Name: b.Name(), Level: string(b.Position().Level),
			Kind: kind, LAU: b.Codes().LAU, INE: b.Codes().INE})
	}
	children := map[string]bool{}
	for _, k := range keys {
		children[k.ParentCode] = true
	}
	for i := range keys {
		keys[i].HasChildren = children[keys[i].Code]
		if keys[i].Kind == domain.MapKindMunicipality && kindOf[keys[i].ParentCode] == domain.MapKindIsland {
			keys[i].IslandOf = keys[i].ParentCode
		}
		if keys[i].Kind != domain.MapKindMunicipality {
			keys[i].LAU = ""
		}
	}
	slices.SortFunc(keys, func(a, b MapKey) int { return strings.Compare(a.Code, b.Code) })
	return keys, nil
}
