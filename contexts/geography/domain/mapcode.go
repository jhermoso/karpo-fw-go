package domain

// Clases de límite en el mapa (la propiedad kind de la tesela).
const (
	MapKindCountry      = "country"
	MapKindRegion       = "region"
	MapKindProvince     = "province"
	MapKindIsland       = "island"
	MapKindMunicipality = "municipality"
)

// MapCode devuelve la clave pública de b en el mapa y su clase. La clave es la misma en la API de
// geografía y en la tesela de delimitaciones:
//
//	país           ISO 3166-1 alfa-2                    ES
//	comunidad      ISO 3166-2 (país-abreviatura)        ES-MD
//	provincia      INE de 2 cifras                      28
//	isla           NUTS 3 (excepción documentada, D-c)  ES532
//	municipio      INE de 5 cifras                      28079
//
// ok es false si b no está en la escalera del mapa: nivel distinto de country, 2, 3 o local (las
// agrupaciones NUTS 1 y las provincias insulares, que no tienen nivel), tipo sin regla o clave sin
// dato. Nunca devuelve una clave vacía con ok.
func (b *Boundary) MapCode() (code, kind string, ok bool) {
	switch b.position.Level {
	case LevelCountry, Level2, Level3, LevelLocal:
	default:
		return "", "", false
	}
	switch b.typ {
	case TypeCountry:
		code, kind = b.codes.Geo, MapKindCountry
	case TypeAutonomous:
		if b.position.Country != "" && b.codes.Abbreviation != "" {
			code = b.position.Country + "-" + b.codes.Abbreviation
		}
		kind = MapKindRegion
	case TypeProvince:
		code, kind = digits(b.codes.INE, 2), MapKindProvince
	case TypeIsland:
		code, kind = b.codes.Geo, MapKindIsland
	case TypeMunicipality:
		code, kind = digits(b.codes.INE, 5), MapKindMunicipality
	}
	if code == "" {
		return "", "", false
	}
	return code, kind, true
}

// digits devuelve s si son exactamente n cifras; si no, el texto vacío.
func digits(s string, n int) string {
	if len(s) != n {
		return ""
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return s
}

// MapParents devuelve, para cada límite de bs que está en la escalera del mapa, el MapCode de su
// primer antecesor que también lo está: el padre de 07040 es Mallorca (ES532) y el de Mallorca,
// Illes Balears (ES-IB), saltándose la provincia 07, que no tiene nivel. Los límites fuera de la
// escalera y los países (cuyos antecesores son continentes) no tienen entrada.
//
// load carga límites por identidad; se llama una vez por ronda, con todos los padres pendientes y
// sin repetir, y como mucho maxDepth veces.
func MapParents(bs []*Boundary, maxDepth int, load func([]BoundaryID) ([]*Boundary, error)) (map[BoundaryID]string, error) {
	out := map[BoundaryID]string{}
	pending := map[BoundaryID]BoundaryID{} // origen -> antecesor por cargar
	for _, b := range bs {
		if _, _, ok := b.MapCode(); !ok || b.typ == TypeCountry {
			continue
		}
		if p, ok := b.Parent(); ok {
			pending[b.ID()] = p
		}
	}
	for round := 0; round < maxDepth && len(pending) > 0; round++ {
		var ids []BoundaryID
		seen := map[BoundaryID]bool{}
		for _, p := range pending {
			if !seen[p] {
				seen[p] = true
				ids = append(ids, p)
			}
		}
		loaded, err := load(ids)
		if err != nil {
			return nil, err
		}
		byID := make(map[BoundaryID]*Boundary, len(loaded))
		for _, b := range loaded {
			byID[b.ID()] = b
		}
		for origin, p := range pending {
			b, found := byID[p]
			if !found {
				delete(pending, origin)
				continue
			}
			if code, _, ok := b.MapCode(); ok {
				out[origin] = code
				delete(pending, origin)
			} else if up, ok := b.Parent(); ok {
				pending[origin] = up
			} else {
				delete(pending, origin)
			}
		}
	}
	return out, nil
}
