package domain

import (
	"context"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Repositorios (contratos; las implementaciones están en infrastructure).
type (
	// PartyRepository guarda parties.
	PartyRepository = fw.Repository[PartyID, *Party]
	// RelationshipRepository guarda relaciones.
	RelationshipRepository = fw.Repository[RelationshipID, *Relationship]
)

// Catalogs los leen los casos de uso; cambian a través de sus propios casos de uso (no en esta
// fase) y de las migraciones.
type Catalogs interface {
	RoleTypes(ctx context.Context) ([]RoleType, error)
	// AddRoleType añade un tipo de rol al catálogo. Quien llama ya ha comprobado que encaja.
	AddRoleType(ctx context.Context, t RoleType) error
	RelationshipTypes(ctx context.Context) ([]RelationshipType, error)
	// AddRelationshipType añade un tipo de relación al catálogo. Quien llama ya ha comprobado que encaja.
	AddRelationshipType(ctx context.Context, t RelationshipType) error
	DocumentTypes(ctx context.Context) ([]DocumentType, error)
	CountryDocumentRules(ctx context.Context) ([]CountryDocumentRule, error)
	ClassificationTypes(ctx context.Context) ([]ClassificationType, error)
	FacilityRoleTypes(ctx context.Context) ([]FacilityRoleType, error)
}

// ---------------------------------------------------------------------------------------------
// Campos y especificaciones de Party
// ---------------------------------------------------------------------------------------------

func roleEnd(r PartyRole) *time.Time {
	if t, ok := r.Period.To(); ok {
		return &t
	}
	return nil
}

// Campos de Party.
var (
	FieldID     = spec.Comparable("id", func(p *Party) PartyID { return p.ID() })
	FieldKind   = spec.Comparable("kind", (*Party).Kind)
	FieldName   = spec.Text("name", (*Party).Name)
	FieldActive = spec.Comparable("active", (*Party).IsActive)
	FieldTest   = spec.Comparable("test", (*Party).IsTest)
	FieldRoles  = spec.Collection("roles", (*Party).Roles)

	FieldShared = spec.Comparable("shared", (*Party).IsShared)
	// FieldDuplicate distingue las parties marcadas como duplicado de otra (D-64).
	FieldDuplicate = spec.Comparable("duplicate", (*Party).IsDuplicate)
	// FieldDocumentIssue distingue las parties marcadas con un documento de identidad rechazado.
	FieldDocumentIssue = spec.Comparable("doc_issue", (*Party).HasDocumentIssue)
	// FieldDataQualityIssues son las incidencias de calidad de datos de la ficha (D-92).
	FieldDataQualityIssues = spec.Collection("data_quality_issues", (*Party).DataQualityIssues)
	// IssueFieldOwner es la organización dueña del contacto de una incidencia (D-122); nulo, de todos.
	IssueFieldOwner   = spec.Optional("owner_id", DataQualityIssue.OwnerOf)
	FieldAffiliations = spec.Collection("affiliations", (*Party).Affiliations)
	AffFieldOrg       = spec.Comparable("organization", func(a Affiliation) PartyID { return a.Organization })
	AffFieldFrom      = spec.Time("valid_from", func(a Affiliation) time.Time { return a.Period.From() })
	AffFieldUntil     = spec.OptionalTime("valid_to", func(a Affiliation) *time.Time {
		if t, ok := a.Period.To(); ok {
			return &t
		}
		return nil
	})
	FieldIdentifications = spec.Collection("identifications", (*Party).Identifications)
	FieldClassifications = spec.Collection("classifications", (*Party).Classifications)

	IdFieldType    = spec.Comparable("doc_type", func(i Identification) DocumentTypeID { return i.Type })
	IdFieldCountry = spec.Comparable("country", func(i Identification) string { return i.Country.String() })
	IdFieldNumber  = spec.Comparable("number", func(i Identification) string { return i.Number })

	ClassFieldType  = spec.Comparable("class_type", func(c Classification) ClassificationTypeID { return c.Type })
	ClassFieldFrom  = spec.Time("valid_from", func(c Classification) time.Time { return c.Period.From() })
	ClassFieldUntil = spec.OptionalTime("valid_to", func(c Classification) *time.Time {
		if t, ok := c.Period.To(); ok {
			return &t
		}
		return nil
	})

	RoleFieldType  = spec.Comparable("role_type", func(r PartyRole) RoleTypeID { return r.RoleType })
	RoleFieldFrom  = spec.Time("valid_from", func(r PartyRole) time.Time { return r.Period.From() })
	RoleFieldUntil = spec.OptionalTime("valid_to", roleEnd)
	// RoleFieldAnnulled distingue los roles dados por error y retirados, que no cuentan para nada.
	RoleFieldAnnulled = spec.Comparable("annulled", func(r PartyRole) bool { return r.Annulled })
)

// WithIDs selecciona las parties dadas.
func WithIDs(ids ...PartyID) spec.Spec[*Party] { return FieldID.In(ids...) }

// VisibleTo selecciona las parties que un ámbito de organizaciones puede ver en t (decisión P1):
// las propias organizaciones, las entradas del catálogo compartido y las parties con una afiliación
// activa a alguna de ellas.
func VisibleTo(orgs []PartyID, t time.Time) spec.Spec[*Party] {
	return spec.Or(WithIDs(orgs...), FieldShared.Eq(true), AffiliatedAt(t, orgs...))
}

// AffiliatedAt selecciona las parties afiliadas a alguna de las organizaciones en t.
func AffiliatedAt(t time.Time, orgs ...PartyID) spec.Spec[*Party] {
	return FieldAffiliations.Any(spec.And(
		AffFieldOrg.In(orgs...),
		AffFieldFrom.AtOrBefore(t),
		AffFieldUntil.IsNull().Or(AffFieldUntil.After(t)),
	))
}

// OwnedBy selecciona las relaciones en las que uno de los lados es una de las organizaciones.
func OwnedBy(orgs []PartyID) spec.Spec[*Relationship] {
	return RelFieldFrom.In(orgs...).Or(RelFieldTo.In(orgs...))
}

// HoldsDocument selecciona las parties que tienen el documento (la comprobación de duplicados
// entre parties).
func HoldsDocument(t DocumentTypeID, country string, number string) spec.Spec[*Party] {
	return FieldIdentifications.Any(spec.And(IdFieldType.Eq(t), IdFieldCountry.Eq(country), IdFieldNumber.Eq(number)))
}

// WithDocumentNumber selecciona las parties que tienen un documento con ese número canónico.
func WithDocumentNumber(number string) spec.Spec[*Party] {
	return FieldIdentifications.Any(IdFieldNumber.Eq(number))
}

// WithDataQualityIssues selecciona las parties con alguna incidencia de calidad de datos (D-92).
func WithDataQualityIssues() spec.Spec[*Party] { return FieldDataQualityIssues.Any(nil) }

// WithDataQualityIssuesSeenBy selecciona las parties con alguna incidencia que ve un ámbito de
// organizaciones: la de un contacto de todos o la de un contacto suyo (D-122).
func WithDataQualityIssuesSeenBy(orgs []PartyID) spec.Spec[*Party] {
	visible := IssueFieldOwner.IsNull()
	if len(orgs) > 0 {
		visible = visible.Or(IssueFieldOwner.In(orgs...))
	}
	return FieldDataQualityIssues.Any(visible)
}

// ClassifiedAt selecciona las parties con alguna de las clasificaciones en t.
func ClassifiedAt(t time.Time, types ...ClassificationTypeID) spec.Spec[*Party] {
	return FieldClassifications.Any(spec.And(
		ClassFieldType.In(types...),
		ClassFieldFrom.AtOrBefore(t),
		ClassFieldUntil.IsNull().Or(ClassFieldUntil.After(t)),
	))
}

// NameContains selecciona las parties cuyo nombre visible contiene text, sin distinguir mayúsculas.
func NameContains(text string) spec.Spec[*Party] { return FieldName.ContainsFold(text) }

// OfKind selecciona personas u organizaciones.
func OfKind(k Kind) spec.Spec[*Party] { return FieldKind.Eq(k) }

// HasPlayed selecciona las parties con alguno de los tipos de rol, en cualquier momento: los
// vigentes, los terminados y los que todavía no han empezado.
func HasPlayed(roles ...RoleTypeID) spec.Spec[*Party] {
	return FieldRoles.Any(spec.And(RoleFieldType.In(roles...), RoleFieldAnnulled.Eq(false)))
}

// Active selecciona las parties activas.
func Active() spec.Spec[*Party] { return FieldActive.Eq(true) }

// PlaysAt selecciona las parties que desempeñan alguno de los tipos de rol en t. Hay que pasar el
// rol y sus descendientes (Catalog.Descendants) para respetar la herencia virtual de los roles.
func PlaysAt(t time.Time, roles ...RoleTypeID) spec.Spec[*Party] {
	return FieldRoles.Any(spec.And(
		RoleFieldType.In(roles...),
		RoleFieldAnnulled.Eq(false),
		RoleFieldFrom.AtOrBefore(t),
		RoleFieldUntil.IsNull().Or(RoleFieldUntil.After(t)),
	))
}

// ---------------------------------------------------------------------------------------------
// Campos y especificaciones de Relationship
// ---------------------------------------------------------------------------------------------

// Campos de Relationship.
var (
	RelFieldID    = spec.Comparable("id", (*Relationship).ID)
	RelFieldType  = spec.Comparable("type", (*Relationship).Type)
	RelFieldFrom  = spec.Comparable("from_party", (*Relationship).From)
	RelFieldTo    = spec.Comparable("to_party", (*Relationship).To)
	RelFieldSince = spec.Time("valid_from", (*Relationship).Since)
	RelFieldUntil = spec.OptionalTime("valid_to", (*Relationship).Until)

	RelFieldTrialUntil = spec.OptionalTime("trial_until", (*Relationship).TrialUntil)

	RelFieldPromotionCode = spec.Comparable("promotion_code", (*Relationship).PromotionCode)

	RelFieldCustomerNumber = spec.Comparable("customer_number", (*Relationship).CustomerNumber)

	RelFieldSupplierNumber = spec.Comparable("supplier_number", (*Relationship).SupplierNumber)

	RelFieldTransferredFrom = spec.Comparable("transferred_from", (*Relationship).TransferredFrom)
)

// InTrialAt selecciona las relaciones de prospect cuya prueba está vigente en t.
func InTrialAt(t time.Time) spec.Spec[*Relationship] {
	return ActiveAt(t).And(RelFieldTrialUntil.After(t))
}

// Involving selecciona las relaciones en las que p es cualquiera de los dos lados.
func Involving(p PartyID) spec.Spec[*Relationship] { return RelFieldFrom.Eq(p).Or(RelFieldTo.Eq(p)) }

// ActiveAt selecciona las relaciones vigentes en t.
func ActiveAt(t time.Time) spec.Spec[*Relationship] {
	return RelFieldSince.AtOrBefore(t).And(RelFieldUntil.IsNull().Or(RelFieldUntil.After(t)))
}

// NotEndedAt selecciona las relaciones que no han terminado en t: las vigentes y las que todavía
// no han empezado.
func NotEndedAt(t time.Time) spec.Spec[*Relationship] {
	return RelFieldUntil.IsNull().Or(RelFieldUntil.After(t))
}

// SameRelationship selecciona las relaciones del mismo tipo entre las mismas parties (en los dos
// sentidos en los tipos simétricos, en los que FromRole es igual a ToRole) todavía abiertas o que
// terminan después de t: la comprobación de duplicados de Establish.
func SameRelationship(t RelationshipType, from, to PartyID, since time.Time) spec.Spec[*Relationship] {
	pair := RelFieldFrom.Eq(from).And(RelFieldTo.Eq(to))
	if t.FromRole == t.ToRole {
		pair = pair.Or(RelFieldFrom.Eq(to).And(RelFieldTo.Eq(from)))
	}
	return spec.And(RelFieldType.Eq(t.ID), pair, RelFieldUntil.IsNull().Or(RelFieldUntil.After(since)))
}
