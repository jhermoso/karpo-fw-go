// Package domain es el modelo del contexto delimitado Billing: las facturas con sus líneas, el
// desglose de impuestos que calcula Fiscal y que se congela al emitir, las facturas rectificativas
// y las series que las numeran sin huecos. Los clientes pertenecen a Parties, los impuestos a
// Fiscal, los pedidos y envíos a sus contextos: este contexto los referencia por identidad.
package domain

import (
	"fmt"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

type (
	// SeriesID identifica una serie.
	SeriesID struct{ fw.UUID }
	// InvoiceID identifica una factura.
	InvoiceID struct{ fw.UUID }
	// LineID identifica una línea de factura.
	LineID struct{ fw.UUID }
	// OrganizationID es el vendedor, una organización interna del contexto Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID es el cliente, una party del contexto Parties.
	PartyID struct{ fw.UUID }
)

// SeriesKind es el nombre estable del tipo de agregado.
const SeriesKind = "billing.series"

// Series numera las facturas de un vendedor para un año, sin huecos: un número sólo se toma en la
// unidad de trabajo que emite la factura (el DocumentSeries del subdominio Documents del C#, cuyo
// emisor no comprobaba ni la organización de una factura ni el año). Las facturas rectificativas
// van en series propias, como exige el reglamento de facturación.
type Series struct {
	fw.BaseAggregateRoot[SeriesID]
	traits.Audited
	s SeriesState
}

// SeriesState es el estado persistido de una serie.
type SeriesState struct {
	Seller     OrganizationID
	Code       string
	Year       int
	Last       int64
	Corrective bool
	Active     bool
	Audit      traits.AuditStamp
}

// ReconstituteSeries reconstruye una serie.
func ReconstituteSeries(id SeriesID, s SeriesState) (*Series, error) {
	base, err := fw.NewBaseAggregateRoot(SeriesKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	s.Code = strings.ToUpper(strings.TrimSpace(s.Code))
	v.Require(s.Code != "" && utf8.RuneCountInString(s.Code) <= 20 && !strings.ContainsAny(s.Code, " /"), "code", "format",
		"a code of 1 to 20 characters without spaces or slashes")
	v.Require(!s.Seller.IsZero(), "seller", "required", "a series belongs to a seller")
	v.Require(s.Year >= 2000 && s.Year <= 9999, "year", "range", "a valid year")
	v.Require(s.Last >= 0, "last", "range", "a non-negative number")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Series{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// OpenSeries crea una serie activa.
func OpenSeries(id SeriesID, s SeriesState) (*Series, error) {
	s.Last, s.Active = 0, true
	return ReconstituteSeries(id, s)
}

// State devuelve el estado.
func (s *Series) State() SeriesState { return s.s }

// Take devuelve el número siguiente (CODE-YYYY-NNNNNN) para una factura de un vendedor, un año y un tipo.
func (s *Series) Take(seller OrganizationID, year int, corrective bool) (string, error) {
	switch {
	case !s.s.Active:
		return "", fw.Violation("billing.series_closed", "the series is closed")
	case s.s.Seller != seller:
		return "", fw.Violation("billing.series_other_seller", "the series belongs to another seller")
	case s.s.Year != year:
		return "", fw.Violation("billing.series_other_year", "the series is of another year")
	case s.s.Corrective != corrective:
		return "", fw.Violation("billing.series_kind", "corrective invoices go in corrective series, and only they")
	}
	s.s.Last++
	return fmt.Sprintf("%s-%d-%06d", s.s.Code, s.s.Year, s.s.Last), nil
}

// Close cierra la serie.
func (s *Series) Close() { s.s.Active = false }

// AuditSnapshot implementa traits.Snapshotter.
func (s *Series) AuditSnapshot() map[string]any {
	return map[string]any{"code": s.s.Code, "year": s.s.Year, "last": s.s.Last, "active": s.s.Active}
}

// Campos de la serie.
var (
	SerFieldSeller = spec.Comparable("seller", func(s *Series) OrganizationID { return s.s.Seller })
	SerFieldCode   = spec.Ordered("code", func(s *Series) string { return s.s.Code })
	SerFieldYear   = spec.Comparable("series_year", func(s *Series) int { return s.s.Year })
)
