package domain

import (
	"context"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Repositories (contracts; implementations live in infrastructure).
type (
	// PartyRepository stores parties.
	PartyRepository = fw.Repository[PartyID, *Party]
	// RelationshipRepository stores relationships.
	RelationshipRepository = fw.Repository[RelationshipID, *Relationship]
)

// Catalogs are read by the use cases; they change through their own use cases (not in this
// phase) and through migrations.
type Catalogs interface {
	RoleTypes(ctx context.Context) ([]RoleType, error)
	RelationshipTypes(ctx context.Context) ([]RelationshipType, error)
	DocumentTypes(ctx context.Context) ([]DocumentType, error)
	CountryDocumentRules(ctx context.Context) ([]CountryDocumentRule, error)
	ClassificationTypes(ctx context.Context) ([]ClassificationType, error)
	FacilityRoleTypes(ctx context.Context) ([]FacilityRoleType, error)
}

// ---------------------------------------------------------------------------------------------
// Party fields and specifications
// ---------------------------------------------------------------------------------------------

func roleEnd(r PartyRole) *time.Time {
	if t, ok := r.Period.To(); ok {
		return &t
	}
	return nil
}

// Party fields.
var (
	FieldID     = spec.Comparable("id", func(p *Party) PartyID { return p.ID() })
	FieldKind   = spec.Comparable("kind", (*Party).Kind)
	FieldName   = spec.Text("name", (*Party).Name)
	FieldActive = spec.Comparable("active", (*Party).IsActive)
	FieldTest   = spec.Comparable("test", (*Party).IsTest)
	FieldRoles  = spec.Collection("roles", (*Party).Roles)

	FieldShared       = spec.Comparable("shared", (*Party).IsShared)
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
)

// WithIDs matches the given parties.
func WithIDs(ids ...PartyID) spec.Spec[*Party] { return FieldID.In(ids...) }

// VisibleTo matches the parties an organization scope may see at t (decision P1): the
// organizations themselves, shared catalog entries, and parties with an active affiliation to
// one of them.
func VisibleTo(orgs []PartyID, t time.Time) spec.Spec[*Party] {
	return spec.Or(WithIDs(orgs...), FieldShared.Eq(true), AffiliatedAt(t, orgs...))
}

// AffiliatedAt matches parties affiliated with any of the organizations at t.
func AffiliatedAt(t time.Time, orgs ...PartyID) spec.Spec[*Party] {
	return FieldAffiliations.Any(spec.And(
		AffFieldOrg.In(orgs...),
		AffFieldFrom.AtOrBefore(t),
		AffFieldUntil.IsNull().Or(AffFieldUntil.After(t)),
	))
}

// OwnedBy matches the relationships where one side is one of the organizations.
func OwnedBy(orgs []PartyID) spec.Spec[*Relationship] {
	return RelFieldFrom.In(orgs...).Or(RelFieldTo.In(orgs...))
}

// HoldsDocument matches parties holding the document (the duplicate check across parties).
func HoldsDocument(t DocumentTypeID, country string, number string) spec.Spec[*Party] {
	return FieldIdentifications.Any(spec.And(IdFieldType.Eq(t), IdFieldCountry.Eq(country), IdFieldNumber.Eq(number)))
}

// WithDocumentNumber matches parties holding a document with that canonical number.
func WithDocumentNumber(number string) spec.Spec[*Party] {
	return FieldIdentifications.Any(IdFieldNumber.Eq(number))
}

// ClassifiedAt matches parties with any of the classifications at t.
func ClassifiedAt(t time.Time, types ...ClassificationTypeID) spec.Spec[*Party] {
	return FieldClassifications.Any(spec.And(
		ClassFieldType.In(types...),
		ClassFieldFrom.AtOrBefore(t),
		ClassFieldUntil.IsNull().Or(ClassFieldUntil.After(t)),
	))
}

// NameContains matches parties whose display name contains text, ignoring case.
func NameContains(text string) spec.Spec[*Party] { return FieldName.ContainsFold(text) }

// OfKind matches people or organizations.
func OfKind(k Kind) spec.Spec[*Party] { return FieldKind.Eq(k) }

// Active matches active parties.
func Active() spec.Spec[*Party] { return FieldActive.Eq(true) }

// PlaysAt matches parties playing any of the role types at t. Pass the role and its
// descendants (Catalog.Descendants) to honour the virtual inheritance of roles.
func PlaysAt(t time.Time, roles ...RoleTypeID) spec.Spec[*Party] {
	return FieldRoles.Any(spec.And(
		RoleFieldType.In(roles...),
		RoleFieldFrom.AtOrBefore(t),
		RoleFieldUntil.IsNull().Or(RoleFieldUntil.After(t)),
	))
}

// ---------------------------------------------------------------------------------------------
// Relationship fields and specifications
// ---------------------------------------------------------------------------------------------

// Relationship fields.
var (
	RelFieldType  = spec.Comparable("type", (*Relationship).Type)
	RelFieldFrom  = spec.Comparable("from_party", (*Relationship).From)
	RelFieldTo    = spec.Comparable("to_party", (*Relationship).To)
	RelFieldSince = spec.Time("valid_from", (*Relationship).Since)
	RelFieldUntil = spec.OptionalTime("valid_to", (*Relationship).Until)

	RelFieldTrialUntil = spec.OptionalTime("trial_until", (*Relationship).TrialUntil)

	RelFieldPromotionCode = spec.Comparable("promotion_code", (*Relationship).PromotionCode)
)

// InTrialAt matches the prospect relationships whose trial is in force at t.
func InTrialAt(t time.Time) spec.Spec[*Relationship] {
	return ActiveAt(t).And(RelFieldTrialUntil.After(t))
}

// Involving matches relationships where p is either side.
func Involving(p PartyID) spec.Spec[*Relationship] { return RelFieldFrom.Eq(p).Or(RelFieldTo.Eq(p)) }

// ActiveAt matches relationships in force at t.
func ActiveAt(t time.Time) spec.Spec[*Relationship] {
	return RelFieldSince.AtOrBefore(t).And(RelFieldUntil.IsNull().Or(RelFieldUntil.After(t)))
}

// SameRelationship matches relationships of the same type between the same parties (both
// directions for symmetric types, where FromRole equals ToRole) still open or ending after t:
// the duplicate check of Establish.
func SameRelationship(t RelationshipType, from, to PartyID, since time.Time) spec.Spec[*Relationship] {
	pair := RelFieldFrom.Eq(from).And(RelFieldTo.Eq(to))
	if t.FromRole == t.ToRole {
		pair = pair.Or(RelFieldFrom.Eq(to).And(RelFieldTo.Eq(from)))
	}
	return spec.And(RelFieldType.Eq(t.ID), pair, RelFieldUntil.IsNull().Or(RelFieldUntil.After(since)))
}
