// Package domain es el modelo del contexto delimitado de Recursos Humanos (RRHH): los puestos con
// sus titulares y sus líneas de dependencia, las relaciones laborales con sus contratos de trabajo,
// los centros de trabajo y los convenios colectivos. Las personas y las organizaciones pertenecen
// a Parties, las instalaciones a Facilities, los datos salariales y fiscales a Payroll: este
// contexto los referencia por identidad.
package domain

import (
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

type (
	// PositionID identifica un puesto.
	PositionID struct{ fw.UUID }
	// PositionTypeID identifica un tipo de puesto.
	PositionTypeID struct{ fw.UUID }
	// PositionStatusID identifica un estado de puesto.
	PositionStatusID struct{ fw.UUID }
	// PositionClassID identifica una clasificación de puestos.
	PositionClassID struct{ fw.UUID }
	// AgreementID identifica un convenio colectivo.
	AgreementID struct{ fw.UUID }
	// EmploymentID identifica una relación laboral.
	EmploymentID struct{ fw.UUID }
	// ContractID identifica un contrato de trabajo (hijo de una relación laboral).
	ContractID struct{ fw.UUID }
	// WorkCenterID identifica un centro de trabajo.
	WorkCenterID struct{ fw.UUID }
	// PersonID es una persona del contexto Parties.
	PersonID struct{ fw.UUID }
	// OrganizationID es una organización (organización interna o unidad) del contexto Parties.
	OrganizationID struct{ fw.UUID }
	// FacilityID es una instalación del contexto Facilities.
	FacilityID struct{ fw.UUID }
)

// MustPositionStatusID interpreta una identidad conocida.
func MustPositionStatusID(s string) PositionStatusID { return PositionStatusID{fw.MustParseUUID(s)} }

// MustPositionTypeID interpreta una identidad conocida.
func MustPositionTypeID(s string) PositionTypeID { return PositionTypeID{fw.MustParseUUID(s)} }

// MustPositionClassID interpreta una identidad conocida.
func MustPositionClassID(s string) PositionClassID { return PositionClassID{fw.MustParseUUID(s)} }

// MustAgreementID interpreta una identidad conocida.
func MustAgreementID(s string) AgreementID { return AgreementID{fw.MustParseUUID(s)} }

// Estados de puesto conocidos (WellKnownRRHHCatalog del C#).
var (
	StatusActive          = MustPositionStatusID("20000000-0000-0000-0001-000000000001")
	StatusInactive        = MustPositionStatusID("20000000-0000-0000-0001-000000000002")
	StatusVacant          = MustPositionStatusID("20000000-0000-0000-0001-000000000003")
	StatusFrozen          = MustPositionStatusID("20000000-0000-0000-0001-000000000004")
	StatusPendingApproval = MustPositionStatusID("20000000-0000-0000-0001-000000000005")
	StatusBudgeted        = MustPositionStatusID("20000000-0000-0000-0001-000000000006")
)

// PositionStatus es una entrada del catálogo de estados de puesto. Vacant se deduce (un puesto
// activo sin titular actual) y no se puede fijar.
type PositionStatus struct {
	ID          PositionStatusID
	Name        string
	Description string
	Active      bool
}

// PositionClass es una clasificación de tipos de puesto (Dirección, Mando, Técnico...).
type PositionClass struct {
	ID     PositionClassID
	Name   string
	Active bool
}

// PositionTypeClass clasifica un tipo de puesto, con las horas semanales estándar de esa clase.
type PositionTypeClass struct {
	Class               PositionClassID
	StandardWeeklyHours vocab.Decimal
}

// PositionType es una entrada del catálogo de tipos de puesto (Chief Executive Officer, Administrativo...).
type PositionType struct {
	ID          PositionTypeID
	Title       string
	Description string
	Active      bool
	Classes     []PositionTypeClass
}

// AgreementScope es el ámbito de un convenio colectivo.
type AgreementScope int

// Ámbitos de convenio (los valores de ConvenioColectivoScope del C#).
const (
	ScopeNational AgreementScope = iota
	ScopeRegional
	ScopeProvincial
	ScopeCompany
)

// Agreement es un convenio colectivo: un solo catálogo para los dos conceptos del C#
// (ConvenioColectivo en RRHH y CollectiveAgreement en Parties). Un convenio de empresa pertenece a
// su organización.
type Agreement struct {
	ID              AgreementID
	Code            string
	Name            string
	Scope           AgreementScope
	Organization    OrganizationID // sólo los convenios de empresa
	TerritorialCode string
	SectoralCode    string
	Start           vocab.Date
	End             vocab.Date // cero mientras está abierto
	Active          bool
}

// Validate comprueba un convenio.
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

// InForceOn dice si el convenio se aplica en una fecha.
func (a Agreement) InForceOn(d vocab.Date) bool {
	return a.Active && !d.Before(a.Start) && (a.End.IsZero() || !d.After(a.End))
}
