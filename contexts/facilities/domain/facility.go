// Package domain is the model of the Facilities bounded context: the places an organization owns
// or operates (warehouses, plants, buildings, offices, floors, rooms...), their hierarchy and
// their location. It was split out of Parties (context map approved on 2026-09-27): the facility
// has its own location instead of borrowing Party contact mechanisms; the roles parties play at a
// facility (headquarters, work center...) stay in Parties.
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

// FacilityKind is the stable aggregate type name.
const FacilityKind = "facilities.facility"

// FacilityID identifies a facility (same GUIDs as the C# IdFacility).
type FacilityID struct{ fw.UUID }

// NewFacilityID returns a new identity.
func NewFacilityID() FacilityID { return FacilityID{fw.NewUUID()} }

// ParseFacilityID parses a textual identity.
func ParseFacilityID(s string) (FacilityID, error) {
	u, err := fw.ParseUUID(s)
	return FacilityID{u}, err
}

// OrganizationID is the internal organization (a Parties party) that owns the facility.
type OrganizationID struct{ fw.UUID }

// FacilityTypeID identifies a facility type.
type FacilityTypeID struct{ fw.UUID }

// MustFacilityTypeID parses a well-known identity.
func MustFacilityTypeID(s string) FacilityTypeID { return FacilityTypeID{fw.MustParseUUID(s)} }

// FacilityType is an entry of the facility type catalog. Types that receive the public (offices,
// currency exchange offices) require a complete location: address and phone.
type FacilityType struct {
	ID              FacilityTypeID
	Name            string
	Description     string
	RequiresContact bool
	Active          bool
}

// GeoRef references the Geography context by identity.
type GeoRef struct {
	PostalCode fw.UUID
	Boundary   fw.UUID
}

// Address is the postal address of a facility.
type Address struct {
	StreetType string
	Line1      string
	Line2      string
	PostalCode string
	Locality   string
	Region     string
	Country    vocab.CountryCode
	Geo        GeoRef
}

// IsZero reports whether the address is absent.
func (a Address) IsZero() bool { return a.Line1 == "" && a.Country.IsZero() }

// String renders the address in one line.
func (a Address) String() string {
	parts := []string{strings.TrimSpace(a.StreetType + " " + a.Line1), a.Line2, strings.TrimSpace(a.PostalCode + " " + a.Locality), a.Region, a.Country.String()}
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), ", ")
}

// Location is where a facility is and how to reach it (value object). It replaces the C#
// FacilityContactMechanism, which borrowed Party contact mechanisms.
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
	if !a.IsZero() {
		v.Require(a.Line1 != "", "location.address.line1", "required", "the first address line is required")
		v.Require(!a.Country.IsZero(), "location.address.country", "required", "country is required")
	}
	for field, s := range map[string]string{"line1": a.Line1, "line2": a.Line2, "locality": a.Locality, "region": a.Region} {
		v.Require(utf8.RuneCountInString(s) <= 200, "location.address."+field, "length", "too long")
	}
	l.Address = a
	return l, v.Err()
}

// Facility is a place owned or operated by an internal organization.
type Facility struct {
	fw.BaseAggregateRoot[FacilityID]
	traits.Activation
	traits.Audited
	owner       OrganizationID
	typ         FacilityTypeID
	name        string
	description string
	partOf      *FacilityID
	area        vocab.Decimal // square meters; zero when unknown
	location    Location
}

// FacilityState is the persisted state of a facility.
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

// Reconstitute rebuilds a facility from persisted state.
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

// Register creates a facility. Types that require contact need an address and a phone.
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

// Owner returns the owning organization.
func (f *Facility) Owner() OrganizationID { return f.owner }

// Type returns the facility type.
func (f *Facility) Type() FacilityTypeID { return f.typ }

// Name returns the name.
func (f *Facility) Name() string { return f.name }

// Description returns the description.
func (f *Facility) Description() string { return f.description }

// PartOf returns the facility this one is part of, if any.
func (f *Facility) PartOf() *FacilityID {
	if f.partOf == nil {
		return nil
	}
	p := *f.partOf
	return &p
}

// Area returns the area in square meters (zero when unknown).
func (f *Facility) Area() vocab.Decimal { return f.area }

// Location returns the location.
func (f *Facility) Location() Location { return f.location }

func (f *Facility) requireActive(action string) error {
	if !f.IsActive() {
		return fw.Violation("facilities.inactive", "an inactive facility cannot "+action)
	}
	return nil
}

// Rename changes the name.
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

// Relocate replaces the location, keeping the contact requirements of the type.
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

// MoveUnder makes the facility part of parent (nil detaches it). The parent belongs to the same
// organization; the application checks there is no cycle (it needs the parent's ancestors).
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

// SetArea records the area in square meters.
func (f *Facility) SetArea(m2 vocab.Decimal) error {
	if m2.Sign() < 0 {
		var v fw.Validation
		v.Add("area", "range", "the area cannot be negative")
		return v.Err()
	}
	f.area = m2
	return nil
}

// Activate reactivates the facility.
func (f *Facility) Activate() {
	if f.Activation.Activate() {
		f.Raise(FacilityActivationChanged{EventMeta: f.NewEventMeta(), Active: true})
	}
}

// Deactivate deactivates the facility.
func (f *Facility) Deactivate() {
	if f.Activation.Deactivate() {
		f.Raise(FacilityActivationChanged{EventMeta: f.NewEventMeta(), Active: false})
	}
}

// AuditSnapshot implements traits.Snapshotter.
func (f *Facility) AuditSnapshot() map[string]any {
	return map[string]any{"name": f.name, "type": f.typ.String(), "description": f.description, "partOf": optID(f.partOf),
		"area": f.area.String(), "address": f.location.Address.String(), "phone": f.location.Phone.String(),
		"email": f.location.Email.String(), "active": f.IsActive()}
}

// Facility fields and specifications.
var (
	FieldID     = spec.Comparable("id", func(f *Facility) FacilityID { return f.ID() })
	FieldOwner  = spec.Comparable("organization", (*Facility).Owner)
	FieldType   = spec.Comparable("type", (*Facility).Type)
	FieldName   = spec.Text("name", (*Facility).Name)
	FieldActive = spec.Comparable("active", (*Facility).IsActive)
	FieldPartOf = spec.Optional("part_of", (*Facility).PartOf)
)

// OwnedBy matches the facilities of the organizations.
func OwnedBy(orgs ...OrganizationID) spec.Spec[*Facility] { return FieldOwner.In(orgs...) }

// WithIDs matches the given facilities.
func WithIDs(ids ...FacilityID) spec.Spec[*Facility] { return FieldID.In(ids...) }

// PartsOf matches the facilities directly inside any of the parents.
func PartsOf(parents ...FacilityID) spec.Spec[*Facility] { return FieldPartOf.In(parents...) }
