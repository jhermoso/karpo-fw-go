// Package domain es el modelo del contexto delimitado Facilities: los lugares que una organización
// posee o explota (almacenes, fábricas, edificios, oficinas, plantas, salas...), su
// jerarquía y su ubicación. Se separó de Parties (mapa de contextos aprobado el 2026-09-27): la
// instalación tiene su propia ubicación en vez de tomar prestados los mecanismos de contacto de
// Party; los papeles que las parties desempeñan en una instalación (sede, centro de trabajo...) se
// quedan en Parties.
package domain

import (
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// FacilityKind es el nombre estable del tipo de agregado.
const FacilityKind = "facilities.facility"

// FacilityID identifica una instalación (los mismos GUID que IdFacility en el C#).
type FacilityID struct{ fw.UUID }

// NewFacilityID devuelve una identidad nueva.
func NewFacilityID() FacilityID { return FacilityID{fw.NewUUID()} }

// ParseFacilityID interpreta una identidad en texto.
func ParseFacilityID(s string) (FacilityID, error) {
	u, err := fw.ParseUUID(s)
	return FacilityID{u}, err
}

// OrganizationID es la organización interna (una party de Parties) dueña de la instalación.
type OrganizationID struct{ fw.UUID }

// FacilityTypeID identifica un tipo de instalación.
type FacilityTypeID struct{ fw.UUID }

// MustFacilityTypeID interpreta una identidad conocida.
func MustFacilityTypeID(s string) FacilityTypeID { return FacilityTypeID{fw.MustParseUUID(s)} }

// FacilityType es una entrada del catálogo de tipos de instalación. Los tipos que atienden al
// público (oficinas, oficinas de cambio de divisa) exigen una ubicación completa: dirección y
// teléfono.
type FacilityType struct {
	ID              FacilityTypeID
	Name            string
	Description     string
	RequiresContact bool
	Active          bool
}

// GeoRef referencia el contexto Geography por identidad.
type GeoRef struct {
	PostalCode fw.UUID
	Boundary   fw.UUID
}

// Address es la dirección postal de una instalación.
type Address struct {
	StreetType string
	Line1      string
	Premises   vocab.Premises // en qué punto de la calle, pieza a pieza; opcional
	Line2      string
	PostalCode string
	Locality   string
	Region     string
	Country    vocab.CountryCode
	Geo        GeoRef
}

// IsZero dice si falta la dirección.
func (a Address) IsZero() bool { return a.Line1 == "" && a.Country.IsZero() }

// String presenta la dirección en una línea.
func (a Address) String() string {
	parts := []string{strings.TrimSpace(a.StreetType + " " + a.Line1 + " " + a.Premises.String()), a.Line2, strings.TrimSpace(a.PostalCode + " " + a.Locality), a.Region, a.Country.String()}
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), ", ")
}

// Location es dónde está una instalación y cómo llegar a ella (objeto de valor). Sustituye al
// FacilityContactMechanism del C#, que tomaba prestados los mecanismos de contacto de Party.
type Location struct {
	Address Address
	Phone   vocab.Phone
	Email   vocab.Email
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

func (l Location) normalized() (Location, error) {
	a := l.Address
	a.StreetType, a.Line1, a.Line2 = strings.ToUpper(clean(a.StreetType)), clean(a.Line1), clean(a.Line2)
	a.PostalCode, a.Locality, a.Region = strings.ToUpper(clean(a.PostalCode)), clean(a.Locality), clean(a.Region)
	var v fw.Validation
	premises, err := a.Premises.Normalized("location.address")
	v.Merge("", err)
	a.Premises = premises
	if !a.IsZero() || !a.Premises.IsZero() {
		v.Require(a.Line1 != "", "location.address.line1", "required", "the first address line is required")
		v.Require(!a.Country.IsZero(), "location.address.country", "required", "country is required")
	}
	for field, s := range map[string]string{"line1": a.Line1, "line2": a.Line2, "locality": a.Locality, "region": a.Region} {
		v.Require(utf8.RuneCountInString(s) <= 200, "location.address."+field, "length", "too long")
	}
	l.Address = a
	return l, v.Err()
}

// Facility es un lugar que posee o explota una organización interna.
type Facility struct {
	fw.BaseAggregateRoot[FacilityID]
	traits.Activation
	traits.Audited
	owner       OrganizationID
	typ         FacilityTypeID
	name        string
	description string
	partOf      *FacilityID
	area        vocab.Decimal // metros cuadrados; cero si no se conoce
	location    Location
}

// FacilityState es el estado persistido de una instalación.
type FacilityState struct {
	Owner       OrganizationID
	Type        FacilityTypeID
	Name        string
	Description string
	PartOf      *FacilityID
	Area        vocab.Decimal
	Location    Location
	Active      bool
	Audit       traits.AuditStamp
}

// Reconstitute reconstruye una instalación a partir del estado persistido.
func Reconstitute(id FacilityID, s FacilityState) (*Facility, error) {
	base, err := fw.NewBaseAggregateRoot(FacilityKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Owner.IsZero(), "organization", "required", "a facility belongs to an organization")
	v.Require(!s.Type.IsZero(), "type", "required", "a facility needs a type")
	name := clean(s.Name)
	v.Require(name != "" && utf8.RuneCountInString(name) <= 200, "name", "length", "a facility needs a name of at most 200 characters")
	v.Require(utf8.RuneCountInString(s.Description) <= 1000, "description", "length", "at most 1000 characters")
	v.Require(s.PartOf == nil || s.PartOf.UUID != id.UUID, "partOf", "self", "a facility cannot be part of itself")
	v.Require(s.Area.Sign() >= 0, "area", "range", "the area cannot be negative")
	if err := v.Err(); err != nil {
		return nil, err
	}
	f := &Facility{BaseAggregateRoot: base, Activation: traits.RestoredActivation(s.Active), Audited: traits.RestoredAudit(s.Audit),
		owner: s.Owner, typ: s.Type, name: name, description: strings.TrimSpace(s.Description), area: s.Area, location: s.Location}
	if s.PartOf != nil {
		p := *s.PartOf
		f.partOf = &p
	}
	return f, nil
}

// Register crea una instalación. Los tipos que exigen contacto necesitan dirección y teléfono.
func Register(id FacilityID, t FacilityType, s FacilityState) (*Facility, error) {
	if !t.Active {
		var v fw.Validation
		v.Add("type", "inactive", "the facility type is not active")
		return nil, v.Err()
	}
	s.Type, s.Active = t.ID, true
	loc, err := s.Location.normalized()
	if err != nil {
		return nil, err
	}
	s.Location = loc
	if err := requireContact(t, loc); err != nil {
		return nil, err
	}
	f, err := Reconstitute(id, s)
	if err != nil {
		return nil, err
	}
	f.Raise(FacilityRegistered{EventMeta: f.NewEventMeta(), Organization: s.Owner.String(), Type: t.ID.String(), Name: f.name,
		PartOf: optID(f.partOf), Address: loc.Address.String()})
	return f, nil
}

func requireContact(t FacilityType, l Location) error {
	if !t.RequiresContact {
		return nil
	}
	var missing []string
	if l.Phone.IsZero() {
		missing = append(missing, "a phone")
	}
	if l.Address.IsZero() {
		missing = append(missing, "an address")
	}
	if len(missing) > 0 {
		return fw.Violation("facilities.contact_required", "a "+t.Name+" needs "+strings.Join(missing, " and "))
	}
	return nil
}

func optID(id *FacilityID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// Owner devuelve la organización dueña.
func (f *Facility) Owner() OrganizationID { return f.owner }

// Type devuelve el tipo de instalación.
func (f *Facility) Type() FacilityTypeID { return f.typ }

// Name devuelve el nombre.
func (f *Facility) Name() string { return f.name }

// Description devuelve la descripción.
func (f *Facility) Description() string { return f.description }

// PartOf devuelve la instalación de la que ésta forma parte, si la hay.
func (f *Facility) PartOf() *FacilityID {
	if f.partOf == nil {
		return nil
	}
	p := *f.partOf
	return &p
}

// Area devuelve la superficie en metros cuadrados (cero si no se conoce).
func (f *Facility) Area() vocab.Decimal { return f.area }

// Location devuelve la ubicación.
func (f *Facility) Location() Location { return f.location }

func (f *Facility) requireActive(action string) error {
	if !f.IsActive() {
		return fw.Violation("facilities.inactive", "an inactive facility cannot "+action)
	}
	return nil
}

// Rename cambia el nombre.
func (f *Facility) Rename(name string) error {
	if err := f.requireActive("be renamed"); err != nil {
		return err
	}
	name = clean(name)
	if name == "" || utf8.RuneCountInString(name) > 200 {
		var v fw.Validation
		v.Add("name", "length", "a facility needs a name of at most 200 characters")
		return v.Err()
	}
	if name == f.name {
		return nil
	}
	f.name = name
	f.Raise(FacilityRenamed{EventMeta: f.NewEventMeta(), Name: name})
	return nil
}

// Relocate sustituye la ubicación, manteniendo los requisitos de contacto del tipo.
func (f *Facility) Relocate(t FacilityType, l Location) error {
	if err := f.requireActive("be relocated"); err != nil {
		return err
	}
	if t.ID != f.typ {
		return fw.Violation("facilities.type_mismatch", "wrong facility type")
	}
	l, err := l.normalized()
	if err != nil {
		return err
	}
	if err := requireContact(t, l); err != nil {
		return err
	}
	f.location = l
	f.Raise(FacilityRelocated{EventMeta: f.NewEventMeta(), Address: l.Address.String(), Phone: l.Phone.String()})
	return nil
}

// MoveUnder hace que la instalación forme parte de parent (nil la separa). El padre pertenece a la
// misma organización; la aplicación comprueba que no haya ciclos (necesita los antepasados del
// padre).
func (f *Facility) MoveUnder(parent *Facility) error {
	if err := f.requireActive("be moved"); err != nil {
		return err
	}
	var to *FacilityID
	if parent != nil {
		if parent.ID() == f.ID() {
			return fw.Violation("facilities.hierarchy_cycle", "a facility cannot be part of itself")
		}
		if parent.owner != f.owner {
			return fw.Violation("facilities.other_organization", "a facility can only be part of a facility of the same organization")
		}
		if !parent.IsActive() {
			return fw.Violation("facilities.inactive", "the parent facility is inactive")
		}
		id := parent.ID()
		to = &id
	}
	if optID(to) == optID(f.partOf) {
		return nil
	}
	f.partOf = to
	f.Raise(FacilityMoved{EventMeta: f.NewEventMeta(), PartOf: optID(to)})
	return nil
}

// SetArea registra la superficie en metros cuadrados.
func (f *Facility) SetArea(m2 vocab.Decimal) error {
	if m2.Sign() < 0 {
		var v fw.Validation
		v.Add("area", "range", "the area cannot be negative")
		return v.Err()
	}
	f.area = m2
	return nil
}

// Activate reactiva la instalación.
func (f *Facility) Activate() {
	if f.Activation.Activate() {
		f.Raise(FacilityActivationChanged{EventMeta: f.NewEventMeta(), Active: true})
	}
}

// Deactivate desactiva la instalación.
func (f *Facility) Deactivate() {
	if f.Activation.Deactivate() {
		f.Raise(FacilityActivationChanged{EventMeta: f.NewEventMeta(), Active: false})
	}
}

// AuditSnapshot implementa traits.Snapshotter.
func (f *Facility) AuditSnapshot() map[string]any {
	return map[string]any{"name": f.name, "type": f.typ.String(), "description": f.description, "partOf": optID(f.partOf),
		"area": f.area.String(), "address": f.location.Address.String(), "phone": f.location.Phone.String(),
		"email": f.location.Email.String(), "active": f.IsActive()}
}

// Campos y especificaciones de Facility.
var (
	FieldID     = spec.Comparable("id", func(f *Facility) FacilityID { return f.ID() })
	FieldOwner  = spec.Comparable("organization", (*Facility).Owner)
	FieldType   = spec.Comparable("type", (*Facility).Type)
	FieldName   = spec.Text("name", (*Facility).Name)
	FieldActive = spec.Comparable("active", (*Facility).IsActive)
	FieldPartOf = spec.Optional("part_of", (*Facility).PartOf)
)

// OwnedBy selecciona las instalaciones de las organizaciones.
func OwnedBy(orgs ...OrganizationID) spec.Spec[*Facility] { return FieldOwner.In(orgs...) }

// WithIDs selecciona las instalaciones dadas.
func WithIDs(ids ...FacilityID) spec.Spec[*Facility] { return FieldID.In(ids...) }

// PartsOf selecciona las instalaciones que están directamente dentro de cualquiera de los padres.
func PartsOf(parents ...FacilityID) spec.Spec[*Facility] { return FieldPartOf.In(parents...) }
