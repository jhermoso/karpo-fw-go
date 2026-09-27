// Package domain is the model of the Billing bounded context: invoices with their lines, the tax
// breakdown calculated by Fiscal and frozen at issue, corrective invoices and the series that
// number them without gaps. Customers belong to Parties, taxes to Fiscal, orders and shipments to
// their contexts: this context references them by identity.
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
	// SeriesID identifies a series.
	SeriesID struct{ fw.UUID }
	// InvoiceID identifies an invoice.
	InvoiceID struct{ fw.UUID }
	// LineID identifies an invoice line.
	LineID struct{ fw.UUID }
	// OrganizationID is the seller, an internal organization of the Parties context.
	OrganizationID struct{ fw.UUID }
	// PartyID is the customer, a party of the Parties context.
	PartyID struct{ fw.UUID }
)

// SeriesKind is the stable aggregate type name.
const SeriesKind = "billing.series"

// Series numbers the invoices of a seller for a year, without gaps: a number is taken only in the
// unit of work that issues the invoice (the C# DocumentSeries of the Documents subdomain, whose
// issuer did not check the organization of an invoice nor the year). Corrective invoices go in
// series of their own, as the invoicing regulation requires.
type Series struct {
	fw.BaseAggregateRoot[SeriesID]
	traits.Audited
	s SeriesState
}

// SeriesState is the persisted state of a series.
type SeriesState struct {
	Seller     OrganizationID
	Code       string
	Year       int
	Last       int64
	Corrective bool
	Active     bool
	Audit      traits.AuditStamp
}

// ReconstituteSeries rebuilds a series.
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

// OpenSeries creates an active series.
func OpenSeries(id SeriesID, s SeriesState) (*Series, error) {
	s.Last, s.Active = 0, true
	return ReconstituteSeries(id, s)
}

// State returns the state.
func (s *Series) State() SeriesState { return s.s }

// Take returns the next number (CODE-YYYY-NNNNNN) for an invoice of a seller, a year and a kind.
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

// Close closes the series.
func (s *Series) Close() { s.s.Active = false }

// AuditSnapshot implements traits.Snapshotter.
func (s *Series) AuditSnapshot() map[string]any {
	return map[string]any{"code": s.s.Code, "year": s.s.Year, "last": s.s.Last, "active": s.s.Active}
}

// Series fields.
var (
	SerFieldSeller = spec.Comparable("seller", func(s *Series) OrganizationID { return s.s.Seller })
	SerFieldCode   = spec.Ordered("code", func(s *Series) string { return s.s.Code })
	SerFieldYear   = spec.Comparable("series_year", func(s *Series) int { return s.s.Year })
)
