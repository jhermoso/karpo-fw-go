// Package domain es el modelo de Modules: el catálogo de lo que el producto sabe hacer (los
// módulos que contrata una empresa, las capacidades que los atraviesan y el sector económico de
// una empresa) y lo que cada empresa tiene encendido. En el C# eran tres tablas con tres
// proveedores propios, uno de los cuales deducía una capacidad de un rol de party y, sin ámbito,
// caía en «todo».
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

// Nombres de tipo de agregado.
const (
	FeatureKind    = "modules.feature"
	ActivationKind = "modules.activation"
)

// Identidades del contexto.
type (
	// FeatureID identifica una entrada del catálogo.
	FeatureID struct{ fw.UUID }
	// ActivationID identifica lo que una empresa tiene (o tuvo) encendido.
	ActivationID struct{ fw.UUID }
	// OrganizationID es una empresa, una organización interna del contexto Parties.
	OrganizationID struct{ fw.UUID }
)

// Kind es lo que es una funcionalidad.
type Kind string

// Clases. Una empresa tiene cualquier número de módulos y capacidades, y un sector.
const (
	Module     Kind = "module"     // lo que contrata una empresa: contabilidad, ventas, tesorería…
	Capability Kind = "capability" // lo que atraviesa los módulos: financiera, logística…
	Sector     Kind = "sector"     // el sector económico de la empresa
)

// Kinds lista las clases válidas.
var Kinds = []Kind{Module, Capability, Sector}

// Exclusive dice si una empresa tiene como mucho una funcionalidad de la clase.
func (k Kind) Exclusive() bool { return k == Sector }

// NormalizeCode pasa a minúsculas y recorta un código de funcionalidad.
func NormalizeCode(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// ValidCode dice si s es un código de funcionalidad: de 1 a 30 letras minúsculas, dígitos o
// guiones.
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

// FeatureState es el estado persistido de una entrada del catálogo.
type FeatureState struct {
	Kind        Kind
	Code        string
	Name        string
	Description string
	Retired     bool // ya no se ofrece: sigue donde está encendida, y no se enciende de nuevo
	Audit       traits.AuditStamp
}

// Feature es una entrada del catálogo, la misma para todas las empresas.
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

// ReconstituteFeature reconstruye una entrada del catálogo.
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

// State devuelve el estado.
func (f *Feature) State() FeatureState { return f.s }

// Change sustituye el nombre y la descripción, y retira o restaura la entrada. La clase y el
// código nunca cambian: son a lo que se refieren las empresas y la interfaz de usuario.
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

// AuditSnapshot implementa traits.Snapshotter.
func (f *Feature) AuditSnapshot() map[string]any {
	return map[string]any{"kind": string(f.s.Kind), "code": f.s.Code, "name": f.s.Name, "retired": f.s.Retired}
}

// ActivationState es el estado persistido de una activación.
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

// Activation es una funcionalidad de una empresa: una por empresa y funcionalidad, que se enciende
// y se apaga (la fila de interruptor del C#, cuya regla de «una sola fila activa» sólo vivía en un
// índice filtrado).
type Activation struct {
	fw.BaseAggregateRoot[ActivationID]
	traits.Audited
	s ActivationState
}

// ReconstituteActivation reconstruye una activación.
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

// NewActivation crea la fila de una empresa y una funcionalidad, apagada.
func NewActivation(id ActivationID, org OrganizationID, f *Feature) (*Activation, error) {
	return ReconstituteActivation(id, ActivationState{Organization: org, Kind: f.s.Kind, Code: f.s.Code})
}

// State devuelve el estado.
func (a *Activation) State() ActivationState { return a.s }

func checkNotes(notes string) (string, error) {
	notes = strings.TrimSpace(notes)
	if utf8.RuneCountInString(notes) > 500 {
		return "", fw.Violation("modules.notes", "notes of at most 500 characters")
	}
	return notes, nil
}

// Activate enciende la funcionalidad. Dice si algo cambió: encender lo que ya está encendido sólo
// sustituye las notas.
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

// Deactivate apaga la funcionalidad. Dice si algo cambió.
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

// AuditSnapshot implementa traits.Snapshotter.
func (a *Activation) AuditSnapshot() map[string]any {
	return map[string]any{"organization": a.s.Organization.String(), "kind": string(a.s.Kind), "code": a.s.Code, "active": a.s.Active}
}

// Campos.
var (
	FtFieldKind    = spec.Ordered("kind", func(f *Feature) string { return string(f.s.Kind) })
	FtFieldCode    = spec.Ordered("code", func(f *Feature) string { return f.s.Code })
	FtFieldRetired = spec.Comparable("retired", func(f *Feature) bool { return f.s.Retired })

	ActFieldOrganization = spec.Comparable("organization", func(a *Activation) OrganizationID { return a.s.Organization })
	ActFieldKind         = spec.Ordered("kind", func(a *Activation) string { return string(a.s.Kind) })
	ActFieldCode         = spec.Ordered("code", func(a *Activation) string { return a.s.Code })
	ActFieldActive       = spec.Comparable("active", func(a *Activation) bool { return a.s.Active })
)

// Eventos de las activaciones.
type (
	// FeatureActivated se lanza cuando una empresa enciende una funcionalidad.
	FeatureActivated struct {
		fw.EventMeta
		Organization string `json:"organization"`
		Kind         string `json:"kind"`
		Code         string `json:"code"`
	}
	// FeatureDeactivated se lanza cuando una empresa apaga una funcionalidad.
	FeatureDeactivated struct {
		fw.EventMeta
		Organization string `json:"organization"`
		Kind         string `json:"kind"`
		Code         string `json:"code"`
	}
)

// Implementaciones de EventType.
func (FeatureActivated) EventType() string   { return "modules.feature_activated" }
func (FeatureDeactivated) EventType() string { return "modules.feature_deactivated" }

// Identidades.
func NewFeatureID() FeatureID       { return FeatureID{fw.NewUUID()} }
func NewActivationID() ActivationID { return ActivationID{fw.NewUUID()} }

// ParseFeatureID interpreta una identidad en texto.
func ParseFeatureID(s string) (FeatureID, error) { u, err := fw.ParseUUID(s); return FeatureID{u}, err }

// Repositorios.
type (
	FeatureRepository    = fw.Repository[FeatureID, *Feature]
	ActivationRepository = fw.Repository[ActivationID, *Activation]
)

// Seed es el catálogo con el que empieza el producto: los ocho módulos sembrados en el C#, con sus
// códigos (la interfaz de usuario se refiere a ellos), y las capacidades que nombraba el código del C#.
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
