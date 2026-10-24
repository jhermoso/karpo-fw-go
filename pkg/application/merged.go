package application

import (
	"context"
	"fmt"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// MergedParties dice qué parties están ahora fusionadas en otra (en Parties, las marcadas como
// «Duplicate Of»). Un listado de registros deja fuera por defecto los de esas parties y sólo los
// trae si se le pide con includeMergedParties; un registro pedido por su identidad se sigue
// encontrando (docs/DECISIONES-COMUNES.md, D-64 y D-100). El anfitrión lo cablea a los contextos
// sin los permisos del usuario (D-103); nil es «ninguna».
type MergedParties interface {
	MergedParties(ctx context.Context) ([]string, error)
}

// MergedIDs devuelve las parties fusionadas convertidas al tipo de identidad del contexto con id,
// o nada si include es cierto (se piden también las fusionadas) o m es nil.
func MergedIDs[V any](ctx context.Context, m MergedParties, include bool, id func(domain.UUID) V) ([]V, error) {
	if include || m == nil {
		return nil, nil
	}
	ids, err := m.MergedParties(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]V, 0, len(ids))
	for _, s := range ids {
		u, err := domain.ParseUUID(s)
		if err != nil {
			return nil, fmt.Errorf("merged parties: %w", err)
		}
		out = append(out, id(u))
	}
	return out, nil
}

// WithoutMerged devuelve lo que hay que añadir a la especificación de un listado para dejar fuera
// los registros cuyo campo f es una party fusionada («f NOT IN (…)»). Nada si include es cierto,
// si m es nil o si no hay ninguna fusionada. Un listado que filtra explícitamente por una party en
// ese campo pasa include cierto: los registros de esa party salen aunque esté fusionada (D-108).
//
// Cada fusionada es un parámetro de la consulta. La traducción a SQL trocea las listas largas en
// varios IN (Oracle admite 1000 por lista), pero eso no quita el límite de SQL Server, que es de
// 2100 parámetros por petición: con unas 2000 fichas fusionadas los listados fallarían allí. La
// salida está anotada en BACKLOG.md (una subconsulta o una tabla de fusionadas).
func WithoutMerged[T any, V comparable](ctx context.Context, m MergedParties, include bool, f spec.Field[T, V], id func(domain.UUID) V) ([]spec.Specification[T], error) {
	ids, err := MergedIDs(ctx, m, include, id)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	return []spec.Specification[T]{f.NotIn(ids...)}, nil
}

// WithoutMergedOptional es WithoutMerged para un campo que admite nulo: los registros sin party se
// quedan («f IS NULL OR NOT (f IN (…))»; un NOT IN solo los perdería en SQL).
func WithoutMergedOptional[T any, V comparable](ctx context.Context, m MergedParties, include bool, f spec.OptionalField[T, V], id func(domain.UUID) V) ([]spec.Specification[T], error) {
	ids, err := MergedIDs(ctx, m, include, id)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	return []spec.Specification[T]{spec.Or(f.IsNull(), spec.Not(f.In(ids...)))}, nil
}
