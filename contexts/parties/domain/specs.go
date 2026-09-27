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

	RoleFieldType  = spec.Comparable("role_type", func(r PartyRole) RoleTypeID { return r.RoleType })
	RoleFieldFrom  = spec.Time("valid_from", func(r PartyRole) time.Time { return r.Period.From() })
	RoleFieldUntil = spec.OptionalTime("valid_to", roleEnd)
)

// WithIDs matches the given parties.
func WithIDs(ids ...PartyID) spec.Spec[*Party] { return FieldID.In(ids...) }

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
)

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
