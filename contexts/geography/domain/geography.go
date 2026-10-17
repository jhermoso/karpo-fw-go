// Package domain es el modelo del contexto delimitado de Geografía y datos de referencia: los
// límites territoriales y su jerarquía (continentes, países, regiones, provincias, municipios,
// uniones supranacionales), los códigos postales y los datos de referencia de cada país (perfiles
// de país, monedas, idiomas, husos horarios, tipos de vía). Son datos que casi sólo se leen,
// sembrados desde los catálogos de Karpo con los mismos GUID, y que otros contextos referencian
// por identidad.
package domain

import (
	"slices"
	"strings"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Nombres de tipo de agregado.
const (
	BoundaryKind   = "geography.boundary"
	PostalCodeKind = "geography.postal_code"
	CountryKind    = "geography.country"
)

// BoundaryID identifica un límite territorial (un país también es un límite).
type BoundaryID struct{ fw.UUID }

// ParseBoundaryID interpreta una identidad en texto.
func ParseBoundaryID(s string) (BoundaryID, error) {
	u, err := fw.ParseUUID(s)
	return BoundaryID{u}, err
}

// BoundaryTypeID identifica un tipo de límite (País, Provincia, Municipio...).
type BoundaryTypeID struct{ fw.UUID }

// MustBoundaryTypeID interpreta una identidad conocida.
func MustBoundaryTypeID(s string) BoundaryTypeID { return BoundaryTypeID{fw.MustParseUUID(s)} }

// Tipos de límite conocidos (WellKnownCatalog.GeographicBoundaryTypes del C#).
var (
	TypeContinent     = MustBoundaryTypeID("10000000-0000-0000-0005-000000000001")
	TypeSubcontinent  = MustBoundaryTypeID("10000000-0000-0000-0005-000000000002")
	TypeCountry       = MustBoundaryTypeID("10000000-0000-0000-0005-000000000003")
	TypeProvince      = MustBoundaryTypeID("10000000-0000-0000-0005-000000000005")
	TypeRegion        = MustBoundaryTypeID("10000000-0000-0000-0005-000000000006")
	TypeAutonomous    = MustBoundaryTypeID("10000000-0000-0000-0005-000000000015") // comunidades y ciudades autónomas españolas
	TypeIsland        = MustBoundaryTypeID("10000000-0000-0000-0005-000000000019") // islas que son una región NUTS 3
	TypeMunicipality  = MustBoundaryTypeID("10000000-0000-0000-0005-000000000009")
	TypeEconomicUnion = MustBoundaryTypeID("10000000-0000-0000-0005-000000000017")
)

// BoundaryType es una entrada del catálogo de tipos de límite.
type BoundaryType struct {
	ID          BoundaryTypeID
	Name        string
	Description string
	Active      bool
}

// LinkKind es la clase de un vínculo entre límites.
type LinkKind string

// Clases de vínculo: un límite está contenido en un padre (el municipio en la provincia) y puede
// ser miembro de varias agrupaciones (el país en la Unión Europea).
const (
	Containment LinkKind = "containment"
	Membership  LinkKind = "membership"
)

// Link relaciona un límite con un padre o una agrupación (el GeographicBoundaryAssociation del
// C#, cuyo From era el hijo y To el padre).
type Link struct {
	ID     fw.UUID
	Parent BoundaryID
	Kind   LinkKind
}

// Codes son los códigos oficiales de un límite.
type Codes struct {
	Geo          string // ISO 3166 para los países, M49 para las regiones, códigos nacionales por debajo
	Abbreviation string
	INE          string // Instituto Nacional de Estadística
	NUTS         string // regiones estadísticas de la UE
	LAU          string // unidades administrativas locales de la UE
}

// Level es el peldaño de un límite en la escalera que comparten todos los países: el país, sus
// tres niveles de regiones y las unidades locales. En un país de la Unión Europea las regiones son
// sus NUTS 1, 2 y 3 y las unidades locales sus LAU. Cómo se llama cada peldaño, y cuál es la
// división principal del país, lo dice su CountryLevel: la comunidad autónoma española es el
// nivel 2 y el Land alemán es el nivel 1.
type Level string

// Niveles. Un límite fuera de la escalera (un continente, una agrupación, una provincia insular
// cuyo nivel 3 son sus islas) tiene NoLevel.
const (
	NoLevel      Level = ""
	LevelCountry Level = "country"
	Level1       Level = "1"
	Level2       Level = "2"
	Level3       Level = "3"
	LevelLocal   Level = "local"
)

// ParseLevel interpreta un nivel; el texto vacío es NoLevel.
func ParseLevel(s string) (Level, error) {
	switch l := Level(strings.ToLower(strings.TrimSpace(s))); l {
	case NoLevel, LevelCountry, Level1, Level2, Level3, LevelLocal:
		return l, nil
	}
	var v fw.Validation
	v.Add("level", "format", "level must be country, 1, 2, 3 or local")
	return NoLevel, v.Err()
}

// NUTSVersion es la versión de la clasificación NUTS que trae la semilla (los códigos españoles
// son los mismos en NUTS 2021 y NUTS 2024).
const NUTSVersion = "2024"

// Position sitúa un límite: el país al que pertenece (ISO 3166-1 alfa-2; vacío por encima de los
// países) y su nivel.
type Position struct {
	Country string
	Level   Level
}

// CountryLevel da nombre a un nivel en un país.
type CountryLevel struct {
	Country        vocab.CountryCode
	Level          Level
	Name           string // cómo lo llama el país (Comunidad autónoma, Land...)
	Administrative bool   // false para un nivel meramente estadístico (el nivel 1 español)
	Principal      bool   // la división principal del país, la que pide un formulario
}

// Boundary es una unidad territorial. Sus vínculos son entidades hijas: como mucho un padre de
// contención.
type Boundary struct {
	fw.BaseAggregateRoot[BoundaryID]
	name     string
	typ      BoundaryTypeID
	codes    Codes
	position Position
	active   bool
	links    []Link
}

// ReconstituteBoundary reconstruye un límite a partir de los datos guardados.
func ReconstituteBoundary(id BoundaryID, name string, typ BoundaryTypeID, codes Codes, position Position, active bool, links []Link) (*Boundary, error) {
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
	level, err := ParseLevel(string(position.Level))
	v.Merge("", err)
	position = Position{Country: strings.ToUpper(strings.TrimSpace(position.Country)), Level: level}
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Boundary{BaseAggregateRoot: base, name: name, typ: typ, codes: codes, position: position, active: active,
		links: slices.Clone(links)}, nil
}

// Name devuelve el nombre.
func (b *Boundary) Name() string { return b.name }

// Type devuelve el tipo de límite.
func (b *Boundary) Type() BoundaryTypeID { return b.typ }

// Codes devuelve los códigos oficiales.
func (b *Boundary) Codes() Codes { return b.codes }

// Position devuelve el país y el nivel del límite.
func (b *Boundary) Position() Position { return b.position }

// IsActive dice si el límite está en uso.
func (b *Boundary) IsActive() bool { return b.active }

// Links devuelve una copia de los vínculos.
func (b *Boundary) Links() []Link { return slices.Clone(b.links) }

// Parent devuelve el padre que lo contiene, si lo hay.
func (b *Boundary) Parent() (BoundaryID, bool) {
	for _, l := range b.links {
		if l.Kind == Containment {
			return l.Parent, true
		}
	}
	return BoundaryID{}, false
}

// Groupings devuelve las agrupaciones de las que el límite es miembro.
func (b *Boundary) Groupings() []BoundaryID {
	var out []BoundaryID
	for _, l := range b.links {
		if l.Kind == Membership {
			out = append(out, l.Parent)
		}
	}
	return out
}

// Campos y especificaciones de los límites.
var (
	FieldBoundaryID   = spec.Comparable("id", func(b *Boundary) BoundaryID { return b.ID() })
	FieldBoundaryName = spec.Text("name", (*Boundary).Name)
	FieldBoundaryType = spec.Comparable("type", (*Boundary).Type)
	FieldGeoCode      = spec.Comparable("geo_code", func(b *Boundary) string { return b.codes.Geo })
	FieldAbbreviation = spec.Comparable("abbreviation", func(b *Boundary) string { return b.codes.Abbreviation })
	FieldCountry      = spec.Comparable("country", func(b *Boundary) string { return b.position.Country })
	FieldLevel        = spec.Comparable("level", func(b *Boundary) string { return string(b.position.Level) })
	FieldLinks        = spec.Collection("links", (*Boundary).Links)
	LinkFieldParent   = spec.Comparable("parent", func(l Link) BoundaryID { return l.Parent })
	LinkFieldKind     = spec.Comparable("kind", func(l Link) LinkKind { return l.Kind })
)

// BoundariesWithIDs selecciona los límites dados.
func BoundariesWithIDs(ids ...BoundaryID) spec.Spec[*Boundary] { return FieldBoundaryID.In(ids...) }

// ChildrenOf selecciona los límites contenidos directamente en alguno de los padres.
func ChildrenOf(parents ...BoundaryID) spec.Spec[*Boundary] {
	return FieldLinks.Any(spec.And(LinkFieldParent.In(parents...), LinkFieldKind.Eq(Containment)))
}

// MembersOf selecciona los límites que son miembros de alguna de las agrupaciones.
func MembersOf(groupings ...BoundaryID) spec.Spec[*Boundary] {
	return FieldLinks.Any(spec.And(LinkFieldParent.In(groupings...), LinkFieldKind.Eq(Membership)))
}

// ---------------------------------------------------------------------------------------------
// Códigos postales
// ---------------------------------------------------------------------------------------------

// PostalCodeID identifica una entrada de código postal: un código en un límite (un código que
// abarca varios municipios tiene una entrada por municipio).
type PostalCodeID struct{ fw.UUID }

// ParsePostalCodeID interpreta una identidad en texto.
func ParsePostalCodeID(s string) (PostalCodeID, error) {
	u, err := fw.ParseUUID(s)
	return PostalCodeID{u}, err
}

// PostalCode es una entrada de código postal de un límite.
type PostalCode struct {
	fw.BaseAggregateRoot[PostalCodeID]
	code     string
	boundary BoundaryID
}

// ReconstitutePostalCode reconstruye una entrada de código postal.
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

// NormalizePostalCode pasa un código postal a mayúsculas y reduce sus espacios.
func NormalizePostalCode(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), " "))
}

// Code devuelve el código postal.
func (p *PostalCode) Code() string { return p.code }

// Boundary devuelve el límite al que pertenece el código.
func (p *PostalCode) Boundary() BoundaryID { return p.boundary }

// Campos de los códigos postales.
var (
	FieldPostalCode     = spec.Comparable("code", (*PostalCode).Code)
	FieldPostalBoundary = spec.Comparable("boundary", (*PostalCode).Boundary)
)

// ---------------------------------------------------------------------------------------------
// Catálogos de referencia
// ---------------------------------------------------------------------------------------------

// Currency es una moneda ISO 4217. (Facial y CurrencyDenomination del C# pertenecen al cambio de
// divisas, en el contexto Financial, no a los datos de referencia.)
type Currency struct {
	ID         fw.UUID
	Code       string
	Name       string
	Symbol     string
	MinorUnits int
	Crypto     bool
	Active     bool
}

// Language es un idioma ISO 639.
type Language struct {
	ID         fw.UUID
	Code       string
	Name       string
	NativeName string
	Active     bool
}

// TimeZone es un huso horario IANA.
type TimeZone struct {
	ID               fw.UUID
	Code             string
	Name             string
	UTCOffsetMinutes int
	ObservesDST      bool
	Active           bool
}

// StreetType es un tipo de vía de un país (Calle, Avenida...).
type StreetType struct {
	ID           fw.UUID
	Country      vocab.CountryCode
	Code         string
	Name         string
	Abbreviation string
	Active       bool
}
