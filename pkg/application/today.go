package application

import (
	"context"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// CompanyToday dice qué día es para una empresa en un instante: el de su zona horaria, no el de
// UTC. A las 23:30 UTC del 14 de octubre, en Madrid ya es el 15: un pedido sin fecha de una empresa
// de Madrid es del 15, y un evento de ese instante también (docs/ARQUITECTURA.md, «Hoy es el de la
// empresa»). Hoy es la fecha del reloj del dominio (TodayOf). En el anfitrión, la zona que la
// empresa guarda en su perfil de Parties; si no, la principal del país de su identidad fiscal en
// Geography; si no, UTC (D-133). nil es UTC, como en las pruebas y en un contexto sin anfitrión.
// Un fallo al preguntar (Parties, Geography) hace fallar la operación que pedía la fecha.
//
// Se pregunta dentro del cierre que se reintenta: un reintento después de medianoche toma el día
// nuevo, igual que el resto de lo que lee.
type CompanyToday interface {
	DateOf(ctx context.Context, company domain.UUID, at time.Time) (vocab.Date, error)
}

// DateOf es la fecha de at para company según t; con t nil, la de UTC.
func DateOf(ctx context.Context, t CompanyToday, company domain.UUID, at time.Time) (vocab.Date, error) {
	if t == nil {
		return vocab.DateOf(at.UTC()), nil
	}
	return t.DateOf(ctx, company, at)
}

// TodayOf es hoy para company según t; con t nil, el de UTC.
func TodayOf(ctx context.Context, t CompanyToday, company domain.UUID) (vocab.Date, error) {
	return DateOf(ctx, t, company, domain.Now())
}

// OrToday es d si trae fecha; si no, hoy para company según t.
func OrToday(ctx context.Context, t CompanyToday, company domain.UUID, d vocab.Date) (vocab.Date, error) {
	if !d.IsZero() {
		return d, nil
	}
	return TodayOf(ctx, t, company)
}
