package domain

import (
	"slices"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// FacilityRoleTypeID identifies what a party does at a facility (headquarters, work center...).
type FacilityRoleTypeID struct{ fw.UUID }

// MustFacilityRoleTypeID parses a well-known identity.
func MustFacilityRoleTypeID(s string) FacilityRoleTypeID {
	return FacilityRoleTypeID{fw.MustParseUUID(s)}
}

// ParseFacilityRoleTypeID parses a textual identity.
func ParseFacilityRoleTypeID(s string) (FacilityRoleTypeID, error) {
	u, err := fw.ParseUUID(s)
	return FacilityRoleTypeID{u}, err
}

// FacilityRoleID identifies a facility role of a party (child entity).
type FacilityRoleID struct{ fw.UUID }

// ParseFacilityRoleID parses a textual identity.
func ParseFacilityRoleID(s string) (FacilityRoleID, error) {
	u, err := fw.ParseUUID(s)
	return FacilityRoleID{u}, err
}

// FacilityRoleType is an entry of the facility role catalog.
type FacilityRoleType struct {
	ID          FacilityRoleTypeID
	Name        string
	Description string
	Active      bool
}

// Well-known facility role types (C# WellKnownCatalog.FacilityRoleTypes and the seed).
var (
	FacilityHeadquarters  = MustFacilityRoleTypeID("10000000-0000-0000-0009-000000000001")
	FacilityBranch        = MustFacilityRoleTypeID("10000000-0000-0000-0009-000000000002")
	FacilityManufacturing = MustFacilityRoleTypeID("10000000-0000-0000-0009-000000000003")
	FacilityStorage       = MustFacilityRoleTypeID("10000000-0000-0000-0009-000000000004")
	FacilityAdministative = MustFacilityRoleTypeID("10000000-0000-0000-0009-000000000005")
	FacilityResearch      = MustFacilityRoleTypeID("10000000-0000-0000-0009-000000000006")
	FacilityWorkCenter    = MustFacilityRoleTypeID("10000000-0000-0000-0009-000000000007")
	FacilityOwnOperator   = MustFacilityRoleTypeID("10000000-0000-0000-0009-000000000008")
	FacilityThirdOperator = MustFacilityRoleTypeID("10000000-0000-0000-0009-000000000009")
)

// WellKnownFacilityRoleTypes returns the seed of the facility role catalog (the C# code catalog
// lacked the two operator roles, which only existed in the seed).
func WellKnownFacilityRoleTypes() []FacilityRoleType {
	return []FacilityRoleType{
		{ID: FacilityHeadquarters, Name: "Headquarters", Description: "Primary corporate headquarters", Active: true},
		{ID: FacilityBranch, Name: "Branch", Description: "Branch office or satellite location", Active: true},
		{ID: FacilityManufacturing, Name: "Manufacturing", Description: "Manufacturing or production role", Active: true},
		{ID: FacilityStorage, Name: "Storage", Description: "Storage and warehousing role", Active: true},
		{ID: FacilityAdministative, Name: "Administrative", Description: "Administrative and management role", Active: true},
		{ID: FacilityResearch, Name: "Research", Description: "Research and development role", Active: true},
		{ID: FacilityWorkCenter, Name: "Work Center", Description: "Physical work center hosting one or more departments", Active: true},
		{ID: FacilityOwnOperator, Name: "Own Operator", Description: "Currency exchange office operated by the organization itself", Active: true},
		{ID: FacilityThirdOperator, Name: "Third-Party Operator", Description: "Currency exchange office operated by a third party", Active: true},
	}
}

// FacilityRole is a role the party plays at a facility of the Facilities context (the C#
// PartyFacility, which stays in Parties: it is the party side of the relationship).
type FacilityRole struct {
	ID       FacilityRoleID
	Facility fw.UUID
	RoleType FacilityRoleTypeID
	Period   vocab.ValidPeriod
}

// AssignFacilityRole makes the party play a role at a facility from a moment on. The facility
// is checked by the application against the Facilities directory. Invariants: a known, active
// role type; no overlapping period of the same role at the same facility.
func (p *Party) AssignFacilityRole(types []FacilityRoleType, facility fw.UUID, role FacilityRoleTypeID, from time.Time) (FacilityRoleID, error) {
	if err := p.requireActive("be given facility roles"); err != nil {
		return FacilityRoleID{}, err
	}
	i := slices.IndexFunc(types, func(t FacilityRoleType) bool { return t.ID == role })
	if i < 0 || !types[i].Active {
		var v fw.Validation
		v.Add("roleType", "unknown", "unknown or inactive facility role")
		return FacilityRoleID{}, v.Err()
	}
	period, err := vocab.OpenPeriodFrom(from)
	if err != nil {
		return FacilityRoleID{}, err
	}
	for _, r := range p.facilityRoles {
		if r.Facility == facility && r.RoleType == role && overlaps(r.Period, period) {
			return FacilityRoleID{}, fw.Violation("parties.facility_role_overlap", "the party already plays this role at the facility")
		}
	}
	r := FacilityRole{ID: FacilityRoleID{fw.NewUUID()}, Facility: facility, RoleType: role, Period: period}
	p.facilityRoles = append(slices.Clone(p.facilityRoles), r)
	p.Raise(PartyFacilityRoleAssigned{EventMeta: p.NewEventMeta(), RoleID: r.ID.String(), Facility: facility.String(),
		RoleType: role.String(), From: period.From()})
	return r.ID, nil
}

// EndFacilityRole ends a facility role at a moment (not before it started).
func (p *Party) EndFacilityRole(id FacilityRoleID, at time.Time) error {
	k := slices.IndexFunc(p.facilityRoles, func(r FacilityRole) bool { return r.ID == id })
	if k < 0 {
		return fw.NotFound("parties.facility_role", id)
	}
	r := p.facilityRoles[k]
	if end, closed := r.Period.To(); closed && !at.Before(end) {
		return nil
	}
	period, err := vocab.NewValidPeriod(r.Period.From(), &at)
	if err != nil {
		return fw.Violation("parties.facility_role_end_before_start", "a facility role cannot end before it starts")
	}
	p.facilityRoles = slices.Clone(p.facilityRoles)
	p.facilityRoles[k].Period = period
	p.Raise(PartyFacilityRoleEnded{EventMeta: p.NewEventMeta(), RoleID: id.String(), Facility: r.Facility.String(), At: at.UTC()})
	return nil
}

// FacilityRoles returns a copy of the facility roles, current or past.
func (p *Party) FacilityRoles() []FacilityRole { return slices.Clone(p.facilityRoles) }

// PartyFacilityRoleAssigned is raised when a party starts playing a role at a facility.
type PartyFacilityRoleAssigned struct {
	fw.EventMeta
	RoleID   string    `json:"roleId"`
	Facility string    `json:"facility"`
	RoleType string    `json:"roleType"`
	From     time.Time `json:"from"`
}

// EventType implements domain.Event.
func (PartyFacilityRoleAssigned) EventType() string { return "parties.facility_role_assigned" }

// PartyFacilityRoleEnded is raised when a facility role ends.
type PartyFacilityRoleEnded struct {
	fw.EventMeta
	RoleID   string    `json:"roleId"`
	Facility string    `json:"facility"`
	At       time.Time `json:"at"`
}

// EventType implements domain.Event.
func (PartyFacilityRoleEnded) EventType() string { return "parties.facility_role_ended" }

// Facility role fields.
var (
	FieldFacilityRoles = spec.Collection("facility_roles", (*Party).FacilityRoles)
	FacFieldFacility   = spec.Comparable("facility", func(r FacilityRole) fw.UUID { return r.Facility })
	FacFieldFrom       = spec.Time("valid_from", func(r FacilityRole) time.Time { return r.Period.From() })
	FacFieldUntil      = spec.OptionalTime("valid_to", func(r FacilityRole) *time.Time {
		if t, ok := r.Period.To(); ok {
			return &t
		}
		return nil
	})
)

// AtFacility matches parties playing any role at the facility at t (employees of an office).
func AtFacility(t time.Time, facility fw.UUID) spec.Spec[*Party] {
	return FieldFacilityRoles.Any(spec.And(FacFieldFacility.Eq(facility), FacFieldFrom.AtOrBefore(t),
		FacFieldUntil.IsNull().Or(FacFieldUntil.After(t))))
}
