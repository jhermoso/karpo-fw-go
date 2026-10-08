// Package domain is the Modules model: the catalog of what the product can do (modules a company
// contracts, capabilities that cut across them, and the economic sector of a company) and what
// each company has switched on. In C# these were three tables with three providers of their own,
// one of which derived a capability from a party role and fell back to "everything" without a
// scope.
package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// Aggregate type names.
const (
	FeatureKind    = "modules.feature"
	ActivationKind = "modules.activation"
)

// Identities of the context.
type (
	// FeatureID identifies an entry of the catalog.
	FeatureID struct{ fw.UUID }
	// ActivationID identifies what a company has (or had) switched on.
	ActivationID struct{ fw.UUID }
	// OrganizationID is a company, an internal organization of the Parties context.
	OrganizationID struct{ fw.UUID }
)

// Kind is what a feature is.
type Kind string

// Kinds. A company has any number of modules and capabilities, and one sector.
const (
	Module     Kind = "module"     // what a company contracts: accounting, sales, treasury…
	Capability Kind = "capability" // what cuts across modules: financial, logistics…
	Sector     Kind = "sector"     // the economic sector of the company
)

// Kinds lists the valid kinds.
var Kinds = []Kind{Module, Capability, Sector}

// Exclusive reports whether a company has at most one feature of the kind.
func (k Kind) Exclusive() bool { return k == Sector }

// NormalizeCode lowers and trims a feature code.
func NormalizeCode(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// ValidCode reports whether s is a feature code: 1 to 30 lowercase letters, digits or dashes.
func ValidCode(s string) bool {
	if s == "" || len(s) > 30 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return false
		}
	}
	return true
}

// FeatureState is the persisted state of a catalog entry.
type FeatureState struct {
	Kind        Kind
	Code        string
	Name        string
	Description string
	Retired     bool // no longer offered: it stays where it is on, and is not switched on anew
	Audit       traits.AuditStamp
}

// Feature is an entry of the catalog, the same for every company.
type Feature struct {
	fw.BaseAggregateRoot[FeatureID]
	traits.Audited
	s FeatureState
}

func checkFeature(v *fw.Validation, s *FeatureState) {
	s.Name, s.Description = strings.TrimSpace(s.Name), strings.TrimSpace(s.Description)
	v.Require(s.Name != "" && utf8.RuneCountInString(s.Name) <= 100, "name", "length", "a name of 1 to 100 characters")
	v.Require(utf8.RuneCountInString(s.Description) <= 500, "description", "length", "at most 500 characters")
}

// ReconstituteFeature rebuilds a catalog entry.
func ReconstituteFeature(id FeatureID, s FeatureState) (*Feature, error) {
	base, err := fw.NewBaseAggregateRoot(FeatureKind, id)
	if err != nil {
		return nil, err
	}
	s.Code = NormalizeCode(s.Code)
	var v fw.Validation
	v.Require(slices.Contains(Kinds, s.Kind), "kind", "enum", "module, capability or sector")
	v.Require(ValidCode(s.Code), "code", "format", "1 to 30 lowercase letters, digits or dashes")
	checkFeature(&v, &s)
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Feature{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State returns the state.
func (f *Feature) State() FeatureState { return f.s }

// Change replaces the name and description, and retires or restores the entry. Kind and code
// never change: they are what companies and the user interface refer to.
func (f *Feature) Change(name, description string, retired bool) error {
	s := f.s
	s.Name, s.Description, s.Retired = name, description, retired
	var v fw.Validation
	checkFeature(&v, &s)
	if err := v.Err(); err != nil {
		return err
	}
	f.s = s
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (f *Feature) AuditSnapshot() map[string]any {
	return map[string]any{"kind": string(f.s.Kind), "code": f.s.Code, "name": f.s.Name, "retired": f.s.Retired}
}

// ActivationState is the persisted state of an activation.
type ActivationState struct {
	Organization  OrganizationID
	Kind          Kind
	Code          string
	Active        bool
	ActivatedAt   time.Time
	ActivatedBy   string
	DeactivatedAt time.Time
	DeactivatedBy string
	Notes         string
	Audit         traits.AuditStamp
}

// Activation is a feature of a company: one per company and feature, switched on and off (the
// C# toggle row, whose "one active row" rule lived only in a filtered index).
type Activation struct {
	fw.BaseAggregateRoot[ActivationID]
	traits.Audited
	s ActivationState
}

// ReconstituteActivation rebuilds an activation.
func ReconstituteActivation(id ActivationID, s ActivationState) (*Activation, error) {
	base, err := fw.NewBaseAggregateRoot(ActivationKind, id)
	if err != nil {
		return nil, err
	}
	s.Code, s.Notes = NormalizeCode(s.Code), strings.TrimSpace(s.Notes)
	var v fw.Validation
	v.Require(!s.Organization.IsZero(), "organization", "required", "the company is required")
	v.Require(slices.Contains(Kinds, s.Kind), "kind", "enum", "module, capability or sector")
	v.Require(ValidCode(s.Code), "code", "format", "1 to 30 lowercase letters, digits or dashes")
	v.Require(utf8.RuneCountInString(s.Notes) <= 500, "notes", "length", "at most 500 characters")
	v.Require(utf8.RuneCountInString(s.ActivatedBy) <= 200 && utf8.RuneCountInString(s.DeactivatedBy) <= 200, "by", "length", "at most 200 characters")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Activation{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// NewActivation creates the row of a company and a feature, switched off.
func NewActivation(id ActivationID, org OrganizationID, f *Feature) (*Activation, error) {
	return ReconstituteActivation(id, ActivationState{Organization: org, Kind: f.s.Kind, Code: f.s.Code})
}

// State returns the state.
func (a *Activation) State() ActivationState { return a.s }

func checkNotes(notes string) (string, error) {
	notes = strings.TrimSpace(notes)
	if utf8.RuneCountInString(notes) > 500 {
		return "", fw.Violation("modules.notes", "notes of at most 500 characters")
	}
	return notes, nil
}

// Activate switches the feature on. It reports whether anything changed: switching on what is on
// only replaces the notes.
func (a *Activation) Activate(by string, at time.Time, notes string) (bool, error) {
	notes, err := checkNotes(notes)
	if err != nil {
		return false, err
	}
	if a.s.Active {
		changed := notes != "" && notes != a.s.Notes
		if changed {
			a.s.Notes = notes
		}
		return changed, nil
	}
	a.s.Active, a.s.ActivatedAt, a.s.ActivatedBy = true, at, by
	a.s.DeactivatedAt, a.s.DeactivatedBy = time.Time{}, ""
	if notes != "" {
		a.s.Notes = notes
	}
	a.Raise(FeatureActivated{EventMeta: a.NewEventMeta(), Organization: a.s.Organization.String(), Kind: string(a.s.Kind), Code: a.s.Code})
	return true, nil
}

// Deactivate switches the feature off. It reports whether anything changed.
func (a *Activation) Deactivate(by string, at time.Time, notes string) (bool, error) {
	notes, err := checkNotes(notes)
	if err != nil {
		return false, err
	}
	if !a.s.Active {
		return false, nil
	}
	a.s.Active, a.s.DeactivatedAt, a.s.DeactivatedBy = false, at, by
	if notes != "" {
		a.s.Notes = notes
	}
	a.Raise(FeatureDeactivated{EventMeta: a.NewEventMeta(), Organization: a.s.Organization.String(), Kind: string(a.s.Kind), Code: a.s.Code})
	return true, nil
}

// AuditSnapshot implements traits.Snapshotter.
func (a *Activation) AuditSnapshot() map[string]any {
	return map[string]any{"organization": a.s.Organization.String(), "kind": string(a.s.Kind), "code": a.s.Code, "active": a.s.Active}
}

// Fields.
var (
	FtFieldKind    = spec.Ordered("kind", func(f *Feature) string { return string(f.s.Kind) })
	FtFieldCode    = spec.Ordered("code", func(f *Feature) string { return f.s.Code })
	FtFieldRetired = spec.Comparable("retired", func(f *Feature) bool { return f.s.Retired })

	ActFieldOrganization = spec.Comparable("organization", func(a *Activation) OrganizationID { return a.s.Organization })
	ActFieldKind         = spec.Ordered("kind", func(a *Activation) string { return string(a.s.Kind) })
	ActFieldCode         = spec.Ordered("code", func(a *Activation) string { return a.s.Code })
	ActFieldActive       = spec.Comparable("active", func(a *Activation) bool { return a.s.Active })
)

// Events of activations.
type (
	// FeatureActivated is raised when a company switches a feature on.
	FeatureActivated struct {
		fw.EventMeta
		Organization string `json:"organization"`
		Kind         string `json:"kind"`
		Code         string `json:"code"`
	}
	// FeatureDeactivated is raised when a company switches a feature off.
	FeatureDeactivated struct {
		fw.EventMeta
		Organization string `json:"organization"`
		Kind         string `json:"kind"`
		Code         string `json:"code"`
	}
)

// EventType implementations.
func (FeatureActivated) EventType() string   { return "modules.feature_activated" }
func (FeatureDeactivated) EventType() string { return "modules.feature_deactivated" }

// Identities.
func NewFeatureID() FeatureID       { return FeatureID{fw.NewUUID()} }
func NewActivationID() ActivationID { return ActivationID{fw.NewUUID()} }

// ParseFeatureID parses a textual identity.
func ParseFeatureID(s string) (FeatureID, error) { u, err := fw.ParseUUID(s); return FeatureID{u}, err }

// Repositories.
type (
	FeatureRepository    = fw.Repository[FeatureID, *Feature]
	ActivationRepository = fw.Repository[ActivationID, *Activation]
)

// Seed is the catalog the product starts with: the eight modules seeded in C#, with their codes
// (the user interface refers to them), and the capabilities its code named.
var Seed = []FeatureState{
	{Kind: Module, Code: "contabilidad", Name: "Contabilidad", Description: "Contabilidad general y plan de cuentas"},
	{Kind: Module, Code: "rrhh", Name: "Recursos Humanos", Description: "Empleados, nóminas y Seguridad Social"},
	{Kind: Module, Code: "compras", Name: "Compras", Description: "Facturas recibidas y proveedores"},
	{Kind: Module, Code: "ventas", Name: "Ventas", Description: "Pedidos de venta, facturación y clientes"},
	{Kind: Module, Code: "tesoreria", Name: "Tesorería", Description: "Bancos, cobros y pagos"},
	{Kind: Module, Code: "inmovilizado", Name: "Inmovilizado", Description: "Activos fijos y amortización"},
	{Kind: Module, Code: "inventario", Name: "Inventario", Description: "Almacenes y existencias"},
	{Kind: Module, Code: "crm", Name: "CRM", Description: "Gestión comercial y oportunidades"},
	{Kind: Capability, Code: "financial", Name: "Servicios financieros"},
	{Kind: Capability, Code: "logistics", Name: "Logística"},
	{Kind: Capability, Code: "manufacturing", Name: "Fabricación"},
	{Kind: Capability, Code: "consulting", Name: "Consultoría"},
}
