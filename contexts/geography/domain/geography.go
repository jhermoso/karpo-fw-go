// Package domain is the model of the Geography and reference data bounded context: territorial
// boundaries and their hierarchy (continents, countries, regions, provinces, municipalities,
// supranational unions), postal codes, and the per-country reference data (country profiles,
// currencies, languages, time zones, street types). It is read-mostly data, seeded from the
// Karpo catalogs with the same GUIDs, that other contexts reference by identity.
package domain

import (
	"slices"
	"strings"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Aggregate type names.
const (
	BoundaryKind   = "geography.boundary"
	PostalCodeKind = "geography.postal_code"
	CountryKind    = "geography.country"
)

// BoundaryID identifies a territorial boundary (a country is a boundary too).
type BoundaryID struct{ fw.UUID }

// ParseBoundaryID parses a textual identity.
func ParseBoundaryID(s string) (BoundaryID, error) {
	u, err := fw.ParseUUID(s)
	return BoundaryID{u}, err
}

// BoundaryTypeID identifies a boundary type (Country, Province, Municipality...).
type BoundaryTypeID struct{ fw.UUID }

// MustBoundaryTypeID parses a well-known identity.
func MustBoundaryTypeID(s string) BoundaryTypeID { return BoundaryTypeID{fw.MustParseUUID(s)} }

// Well-known boundary types (C# WellKnownCatalog.GeographicBoundaryTypes).
var (
	TypeContinent     = MustBoundaryTypeID("10000000-0000-0000-0005-000000000001")
	TypeSubcontinent  = MustBoundaryTypeID("10000000-0000-0000-0005-000000000002")
	TypeCountry       = MustBoundaryTypeID("10000000-0000-0000-0005-000000000003")
	TypeProvince      = MustBoundaryTypeID("10000000-0000-0000-0005-000000000005")
	TypeRegion        = MustBoundaryTypeID("10000000-0000-0000-0005-000000000006")
	TypeMunicipality  = MustBoundaryTypeID("10000000-0000-0000-0005-000000000009")
	TypeEconomicUnion = MustBoundaryTypeID("10000000-0000-0000-0005-000000000017")
)

// BoundaryType is an entry of the boundary type catalog.
type BoundaryType struct {
	ID          BoundaryTypeID
	Name        string
	Description string
	Active      bool
}

// LinkKind is the kind of a link between boundaries.
type LinkKind string

// Link kinds: a boundary is contained in one parent (municipality in province) and may be a
// member of several groupings (country in the European Union).
const (
	Containment LinkKind = "containment"
	Membership  LinkKind = "membership"
)

// Link relates a boundary to a parent or grouping (the C# GeographicBoundaryAssociation, whose
// From was the child and To the parent).
type Link struct {
	ID     fw.UUID
	Parent BoundaryID
	Kind   LinkKind
}

// Codes are the official codes of a boundary.
type Codes struct {
	Geo          string // ISO 3166 for countries, M49 for regions, national codes below
	Abbreviation string
	INE          string // Spanish statistics institute
	NUTS         string // EU statistical regions
	LAU          string // EU local administrative units
}

// Boundary is a territorial unit. Its links are child entities: one containment parent at most.
type Boundary struct {
	fw.BaseAggregateRoot[BoundaryID]
	name   string
	typ    BoundaryTypeID
	codes  Codes
	active bool
	links  []Link
}

// ReconstituteBoundary rebuilds a boundary from stored data.
func ReconstituteBoundary(id BoundaryID, name string, typ BoundaryTypeID, codes Codes, active bool, links []Link) (*Boundary, error) {
	base, err := fw.NewBaseAggregateRoot(BoundaryKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(strings.TrimSpace(name) != "", "name", "required", "a boundary needs a name")
	v.Require(!typ.IsZero(), "type", "required", "a boundary needs a type")
	containment := 0
	for _, l := range links {
		if l.Kind == Containment {
			containment++
		}
		v.Require(l.Parent != id, "links", "self", "a boundary cannot contain itself")
	}
	v.Require(containment <= 1, "links", "containment", "a boundary has one containing parent at most")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Boundary{BaseAggregateRoot: base, name: name, typ: typ, codes: codes, active: active, links: slices.Clone(links)}, nil
}

// Name returns the name.
func (b *Boundary) Name() string { return b.name }

// Type returns the boundary type.
func (b *Boundary) Type() BoundaryTypeID { return b.typ }

// Codes returns the official codes.
func (b *Boundary) Codes() Codes { return b.codes }

// IsActive reports whether the boundary is in use.
func (b *Boundary) IsActive() bool { return b.active }

// Links returns a copy of the links.
func (b *Boundary) Links() []Link { return slices.Clone(b.links) }

// Parent returns the containing parent, if any.
func (b *Boundary) Parent() (BoundaryID, bool) {
	for _, l := range b.links {
		if l.Kind == Containment {
			return l.Parent, true
		}
	}
	return BoundaryID{}, false
}

// Groupings returns the groupings the boundary is a member of.
func (b *Boundary) Groupings() []BoundaryID {
	var out []BoundaryID
	for _, l := range b.links {
		if l.Kind == Membership {
			out = append(out, l.Parent)
		}
	}
	return out
}

// Boundary fields and specifications.
var (
	FieldBoundaryID   = spec.Comparable("id", func(b *Boundary) BoundaryID { return b.ID() })
	FieldBoundaryName = spec.Text("name", (*Boundary).Name)
	FieldBoundaryType = spec.Comparable("type", (*Boundary).Type)
	FieldGeoCode      = spec.Comparable("geo_code", func(b *Boundary) string { return b.codes.Geo })
	FieldAbbreviation = spec.Comparable("abbreviation", func(b *Boundary) string { return b.codes.Abbreviation })
	FieldLinks        = spec.Collection("links", (*Boundary).Links)
	LinkFieldParent   = spec.Comparable("parent", func(l Link) BoundaryID { return l.Parent })
	LinkFieldKind     = spec.Comparable("kind", func(l Link) LinkKind { return l.Kind })
)

// BoundariesWithIDs matches the given boundaries.
func BoundariesWithIDs(ids ...BoundaryID) spec.Spec[*Boundary] { return FieldBoundaryID.In(ids...) }

// ChildrenOf matches the boundaries directly contained in any of the parents.
func ChildrenOf(parents ...BoundaryID) spec.Spec[*Boundary] {
	return FieldLinks.Any(spec.And(LinkFieldParent.In(parents...), LinkFieldKind.Eq(Containment)))
}

// MembersOf matches the boundaries that are members of any of the groupings.
func MembersOf(groupings ...BoundaryID) spec.Spec[*Boundary] {
	return FieldLinks.Any(spec.And(LinkFieldParent.In(groupings...), LinkFieldKind.Eq(Membership)))
}

// ---------------------------------------------------------------------------------------------
// Postal codes
// ---------------------------------------------------------------------------------------------

// PostalCodeID identifies a postal code entry: one code in one boundary (a code covering several
// municipalities has one entry per municipality).
type PostalCodeID struct{ fw.UUID }

// ParsePostalCodeID parses a textual identity.
func ParsePostalCodeID(s string) (PostalCodeID, error) {
	u, err := fw.ParseUUID(s)
	return PostalCodeID{u}, err
}

// PostalCode is a postal code entry of a boundary.
type PostalCode struct {
	fw.BaseAggregateRoot[PostalCodeID]
	code     string
	boundary BoundaryID
}

// ReconstitutePostalCode rebuilds a postal code entry.
func ReconstitutePostalCode(id PostalCodeID, code string, boundary BoundaryID) (*PostalCode, error) {
	base, err := fw.NewBaseAggregateRoot(PostalCodeKind, id)
	if err != nil {
		return nil, err
	}
	code = NormalizePostalCode(code)
	if code == "" || boundary.IsZero() {
		var v fw.Validation
		v.Add("code", "required", "a postal code entry needs a code and a boundary")
		return nil, v.Err()
	}
	return &PostalCode{BaseAggregateRoot: base, code: code, boundary: boundary}, nil
}

// NormalizePostalCode upper-cases a postal code and collapses its spaces.
func NormalizePostalCode(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), " "))
}

// Code returns the postal code.
func (p *PostalCode) Code() string { return p.code }

// Boundary returns the boundary the code belongs to.
func (p *PostalCode) Boundary() BoundaryID { return p.boundary }

// Postal code fields.
var (
	FieldPostalCode     = spec.Comparable("code", (*PostalCode).Code)
	FieldPostalBoundary = spec.Comparable("boundary", (*PostalCode).Boundary)
)

// ---------------------------------------------------------------------------------------------
// Reference catalogs
// ---------------------------------------------------------------------------------------------

// Currency is an ISO 4217 currency. (The C# Facial and CurrencyDenomination belong to currency
// exchange, in the Financial context, not to reference data.)
type Currency struct {
	ID         fw.UUID
	Code       string
	Name       string
	Symbol     string
	MinorUnits int
	Crypto     bool
	Active     bool
}

// Language is an ISO 639 language.
type Language struct {
	ID         fw.UUID
	Code       string
	Name       string
	NativeName string
	Active     bool
}

// TimeZone is an IANA time zone.
type TimeZone struct {
	ID               fw.UUID
	Code             string
	Name             string
	UTCOffsetMinutes int
	ObservesDST      bool
	Active           bool
}

// StreetType is a street type of a country (Calle, Avenida...).
type StreetType struct {
	ID           fw.UUID
	Country      vocab.CountryCode
	Code         string
	Name         string
	Abbreviation string
	Active       bool
}
