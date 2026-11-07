package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// CompanyToday dice qué día es hoy para una empresa: el de su zona horaria, no el de UTC. A las
// 23:30 UTC del 14 de octubre, en Madrid ya es el 15, y un pedido sin fecha de una empresa de
// Madrid es del 15 (docs/ARQUITECTURA.md, «Hoy es el de la empresa»). En el anfitrión, la zona que
// la empresa guarda en su perfil de Parties; si no, la principal del país de su identidad fiscal en
// Geography; si no, UTC (D-133). nil es UTC, como en las pruebas y en un contexto sin anfitrión.
// Un fallo al preguntar (Parties, Geography) hace fallar la operación que pedía la fecha.
//
// Se pregunta dentro del cierre que se reintenta: un reintento después de medianoche toma el día
// nuevo, igual que el resto de lo que lee.
type CompanyToday interface {
	Today(ctx context.Context, company domain.UUID) (vocab.Date, error)
}

// TodayOf es hoy para company según t; con t nil, el de UTC.
func TodayOf(ctx context.Context, t CompanyToday, company domain.UUID) (vocab.Date, error) {
	if t == nil {
		return vocab.Today(nil), nil
	}
	return t.Today(ctx, company)
}

// OrToday es d si trae fecha; si no, hoy para company según t.
func OrToday(ctx context.Context, t CompanyToday, company domain.UUID, d vocab.Date) (vocab.Date, error) {
	if !d.IsZero() {
		return d, nil
	}
	return TodayOf(ctx, t, company)
}
