// Package domain is the model of the Human Resources (RRHH) bounded context: positions and their
// holders and reporting lines, employments with their labor contracts, work centers and
// collective agreements. People and organizations belong to Parties, facilities to Facilities,
// pay and tax data to Payroll: this context references them by identity.
package domain

import (
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

type (
	// PositionID identifies a position.
	PositionID struct{ fw.UUID }
	// PositionTypeID identifies a position type.
	PositionTypeID struct{ fw.UUID }
	// PositionStatusID identifies a position status.
	PositionStatusID struct{ fw.UUID }
	// PositionClassID identifies a position classification.
	PositionClassID struct{ fw.UUID }
	// AgreementID identifies a collective agreement.
	AgreementID struct{ fw.UUID }
	// EmploymentID identifies an employment.
	EmploymentID struct{ fw.UUID }
	// ContractID identifies a labor contract (child of an employment).
	ContractID struct{ fw.UUID }
	// WorkCenterID identifies a work center.
	WorkCenterID struct{ fw.UUID }
	// PersonID is a person of the Parties context.
	PersonID struct{ fw.UUID }
	// OrganizationID is an organization (internal organization or unit) of the Parties context.
	OrganizationID struct{ fw.UUID }
	// FacilityID is a facility of the Facilities context.
	FacilityID struct{ fw.UUID }
)

// MustPositionStatusID parses a well-known identity.
func MustPositionStatusID(s string) PositionStatusID { return PositionStatusID{fw.MustParseUUID(s)} }

// MustPositionTypeID parses a well-known identity.
func MustPositionTypeID(s string) PositionTypeID { return PositionTypeID{fw.MustParseUUID(s)} }

// MustPositionClassID parses a well-known identity.
func MustPositionClassID(s string) PositionClassID { return PositionClassID{fw.MustParseUUID(s)} }

// MustAgreementID parses a well-known identity.
func MustAgreementID(s string) AgreementID { return AgreementID{fw.MustParseUUID(s)} }

// Well-known position statuses (C# WellKnownRRHHCatalog).
var (
	StatusActive          = MustPositionStatusID("20000000-0000-0000-0001-000000000001")
	StatusInactive        = MustPositionStatusID("20000000-0000-0000-0001-000000000002")
	StatusVacant          = MustPositionStatusID("20000000-0000-0000-0001-000000000003")
	StatusFrozen          = MustPositionStatusID("20000000-0000-0000-0001-000000000004")
	StatusPendingApproval = MustPositionStatusID("20000000-0000-0000-0001-000000000005")
	StatusBudgeted        = MustPositionStatusID("20000000-0000-0000-0001-000000000006")
)

// PositionStatus is an entry of the position status catalog. Vacant is derived (an active position
// without a current holder) and cannot be set.
type PositionStatus struct {
	ID          PositionStatusID
	Name        string
	Description string
	Active      bool
}

// PositionClass is a classification of position types (Dirección, Mando, Técnico...).
type PositionClass struct {
	ID     PositionClassID
	Name   string
	Active bool
}

// PositionTypeClass classifies a position type, with the standard weekly hours of that class.
type PositionTypeClass struct {
	Class               PositionClassID
	StandardWeeklyHours vocab.Decimal
}

// PositionType is an entry of the position type catalog (Chief Executive Officer, Administrativo...).
type PositionType struct {
	ID          PositionTypeID
	Title       string
	Description string
	Active      bool
	Classes     []PositionTypeClass
}

// AgreementScope is the scope of a collective agreement.
type AgreementScope int

// Agreement scopes (the C# ConvenioColectivoScope values).
const (
	ScopeNational AgreementScope = iota
	ScopeRegional
	ScopeProvincial
	ScopeCompany
)

// Agreement is a collective agreement: one catalog for the two C# concepts (ConvenioColectivo in
// RRHH and CollectiveAgreement in Parties). A company agreement belongs to its organization.
type Agreement struct {
	ID              AgreementID
	Code            string
	Name            string
	Scope           AgreementScope
	Organization    OrganizationID // company agreements only
	TerritorialCode string
	SectoralCode    string
	Start           vocab.Date
	End             vocab.Date // zero when open
	Active          bool
}

// Validate checks an agreement.
func (a Agreement) Validate() error {
	var v fw.Validation
	code := strings.TrimSpace(a.Code)
	v.Require(code != "" && utf8.RuneCountInString(code) <= 60, "code", "length", "a code of 1 to 60 characters is required")
	v.Require(strings.TrimSpace(a.Name) != "" && utf8.RuneCountInString(a.Name) <= 200, "name", "length", "a name of 1 to 200 characters is required")
	v.Require(a.Scope >= ScopeNational && a.Scope <= ScopeCompany, "scope", "enum", "unknown scope")
	v.Require((a.Scope == ScopeCompany) == !a.Organization.IsZero(), "organization", "scope",
		"a company agreement belongs to one organization, and only company agreements do")
	v.Require(!a.Start.IsZero(), "start", "required", "the start date is required")
	v.Require(a.End.IsZero() || !a.End.Before(a.Start), "end", "order", "the end cannot precede the start")
	return v.Err()
}

// InForceOn reports whether the agreement applies on a date.
func (a Agreement) InForceOn(d vocab.Date) bool {
	return a.Active && !d.Before(a.Start) && (a.End.IsZero() || !d.After(a.End))
}
