// Package application holds the Parties use cases: commands and queries, their handlers decorated
// with the framework pipeline (permissions, validation, idempotency, retries), the DTOs, the
// translation to the Published Language and the in-process party directory.
package application

import (
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permissions of the context ({Subdomain}.{Resource}.{Action}, as in the C# PermissionCodes).
var (
	PermPartyRead          = authz.MustPermission("Parties.Party.Read")
	PermPartyCreate        = authz.MustPermission("Parties.Party.Create")
	PermPartyUpdate        = authz.MustPermission("Parties.Party.Update")
	PermRoleAssign         = authz.MustPermission("Parties.PartyRole.Assign")
	PermRelationshipRead   = authz.MustPermission("Parties.Relationship.Read")
	PermRelationshipCreate = authz.MustPermission("Parties.Relationship.Create")
	PermRelationshipEnd    = authz.MustPermission("Parties.Relationship.Terminate")
	// PermRelationshipUpdate changes the details a relationship carries because of its type.
	PermRelationshipUpdate = authz.MustPermission("Parties.Relationship.Update")
	// PermRelationshipSetTrial grants, extends or withdraws the free trial of a prospect. It is an
	// action of its own, not an update, so that the standard rule of the Security catalog (read,
	// create, update) does not hand it to every standard user: giving time away is a commercial
	// decision.
	PermRelationshipSetTrial = authz.MustPermission("Parties.Relationship.SetTrial")
)

// Permissions returns the permissions this context declares to the Security catalog.
func Permissions() []authz.Permission {
	return []authz.Permission{PermPartyRead, PermPartyCreate, PermPartyUpdate, PermRoleAssign, PermRelationshipRead,
		PermRelationshipCreate, PermRelationshipEnd, PermRelationshipUpdate, PermRelationshipSetTrial}
}

// RegisterPerson registers a person, optionally with initial roles (the C# CreatePartyWithRole).
type RegisterPerson struct {
	GivenName     string          `json:"givenName"`
	FirstSurname  string          `json:"firstSurname"`
	SecondSurname string          `json:"secondSurname,omitempty"`
	Gender        string          `json:"gender,omitempty"`
	BirthDate     string          `json:"birthDate,omitempty"` // YYYY-MM-DD
	MaritalStatus string          `json:"maritalStatus,omitempty"`
	Roles         []string        `json:"roles,omitempty"`
	Affiliation   *NewAffiliation `json:"affiliation,omitempty"` // required unless global administrator
	RequestID     string          `json:"-"`                     // Idempotency-Key
}

// IdempotencyKey implements application.IdempotencyKeyed.
func (c RegisterPerson) IdempotencyKey() string { return c.RequestID }

func (c RegisterPerson) details() (domain.PersonDetails, error) {
	var v fw.Validation
	name, err := domain.NewPersonalName(c.GivenName, c.FirstSurname, c.SecondSurname)
	v.Merge("", err)
	g, err := domain.ParseGender(c.Gender)
	v.Merge("", err)
	m, err := domain.ParseMaritalStatus(c.MaritalStatus)
	v.Merge("", err)
	var birth vocab.Date
	if c.BirthDate != "" {
		if birth, err = vocab.ParseDate(c.BirthDate); err != nil {
			v.Add("birthDate", "format", "birth date must be YYYY-MM-DD")
		}
	}
	return domain.PersonDetails{Name: name, Gender: g, BirthDate: birth, MaritalStatus: m}, v.Err()
}

// RegisterOrganization registers an organization, optionally with initial roles.
type RegisterOrganization struct {
	LegalName   string          `json:"legalName"`
	TradeName   string          `json:"tradeName,omitempty"`
	LegalForm   string          `json:"legalForm,omitempty"`
	Roles       []string        `json:"roles,omitempty"`
	Affiliation *NewAffiliation `json:"affiliation,omitempty"`
	RequestID   string          `json:"-"`
}

// SetLegalForm changes the legal form of an organization.
type SetLegalForm struct {
	ID        domain.PartyID `json:"-"`
	LegalForm string         `json:"legalForm"`
}

// SetShared makes a party a shared catalog entry visible to every organization (global
// administrators only).
type SetShared struct {
	ID     domain.PartyID `json:"-"`
	Shared bool           `json:"shared"`
}

// ListInternalOrganizations lists the internal organizations of the caller's scope.
type ListInternalOrganizations struct{}

// IdempotencyKey implements application.IdempotencyKeyed.
func (c RegisterOrganization) IdempotencyKey() string { return c.RequestID }

// RenameParty changes the name of a person (given name and surnames) or an organization
// (legal and trade name); the fields of the other kind are ignored.
type RenameParty struct {
	ID            domain.PartyID `json:"-"`
	GivenName     string         `json:"givenName,omitempty"`
	FirstSurname  string         `json:"firstSurname,omitempty"`
	SecondSurname string         `json:"secondSurname,omitempty"`
	LegalName     string         `json:"legalName,omitempty"`
	TradeName     string         `json:"tradeName,omitempty"`
}

// UpdatePerson replaces the gender, birth date and marital status of a person (empty: unknown).
// The name changes with RenameParty.
type UpdatePerson struct {
	ID            domain.PartyID `json:"-"`
	Gender        string         `json:"gender,omitempty"`
	BirthDate     string         `json:"birthDate,omitempty"` // YYYY-MM-DD
	MaritalStatus string         `json:"maritalStatus,omitempty"`
}

// SetPartyActive activates or deactivates a party (the C# toggle).
type SetPartyActive struct {
	ID     domain.PartyID `json:"-"`
	Active bool           `json:"active"`
}

// AssignRole makes a party play a role from a moment on (now when From is nil).
type AssignRole struct {
	PartyID  domain.PartyID `json:"-"`
	RoleType string         `json:"roleType"`
	From     *time.Time     `json:"from,omitempty"`
}

// EndRole stops a role at a moment (now when At is nil).
type EndRole struct {
	PartyID domain.PartyID     `json:"-"`
	RoleID  domain.PartyRoleID `json:"-"`
	At      *time.Time         `json:"at,omitempty"`
}

// EstablishRelationship relates two parties (the roles come from the relationship type). The
// details of the type, if it has any, may come with it.
type EstablishRelationship struct {
	Type   string     `json:"type"`
	From   string     `json:"fromParty"`
	To     string     `json:"toParty"`
	Since  *time.Time `json:"since,omitempty"`
	Remark string     `json:"remark,omitempty"`
	RelationshipDetailsInput
}

// RelationshipDetailsInput are the details a relationship may carry because of its type; each one
// is accepted only by the relationship types that have it.
type RelationshipDetailsInput struct {
	Prospect  *ProspectInput  `json:"prospect,omitempty"`  // prospect relationships
	Ownership *OwnershipInput `json:"ownership,omitempty"` // ownership relationships
}

// apply sets the details on a relationship of type rt.
func (d RelationshipDetailsInput) apply(r *domain.Relationship, rt domain.RelationshipType) error {
	if d.Prospect != nil {
		if err := r.SetTrial(rt, d.Prospect.TrialUntil); err != nil {
			return err
		}
	}
	if d.Ownership != nil {
		share, err := parseShare(d.Ownership.Share)
		if err != nil {
			return err
		}
		return r.SetOwnershipShare(rt, share)
	}
	return nil
}

// ProspectInput are the details of a prospect relationship.
type ProspectInput struct {
	TrialUntil *time.Time `json:"trialUntil"` // null: no trial
}

// OwnershipInput are the details of an ownership relationship.
type OwnershipInput struct {
	Share *string `json:"share"` // points with two decimals at most ("30"); null: unknown
}

func parseShare(s *string) (*vocab.Percentage, error) {
	if s == nil {
		return nil, nil
	}
	d, err := vocab.ParseDecimal(*s)
	if err != nil {
		var v fw.Validation
		v.Add("share", "format", "share must be a decimal number")
		return nil, v.Err()
	}
	p := vocab.NewPercentage(d)
	return &p, nil
}

// SetOwnershipShare records, corrects or (with a null share) clears the stake of the shareholder
// in an ownership relationship.
type SetOwnershipShare struct {
	ID    domain.RelationshipID `json:"-"`
	Share *string               `json:"share"`
}

// SetPromotionCode gives a collaborator its promotion code, changes it or (with an empty code)
// takes it away.
type SetPromotionCode struct {
	ID            domain.RelationshipID `json:"-"`
	PromotionCode string                `json:"promotionCode"`
}

// SetProspectTrial grants, extends, shortens or (with a null end) withdraws the trial of a
// prospect relationship.
type SetProspectTrial struct {
	ID         domain.RelationshipID `json:"-"`
	TrialUntil *time.Time            `json:"trialUntil"`
}

// TerminateRelationship ends a relationship (now when At is nil).
type TerminateRelationship struct {
	ID domain.RelationshipID `json:"-"`
	At *time.Time            `json:"at,omitempty"`
}

// GetParty loads one party.
type GetParty struct{ ID domain.PartyID }

// SearchParties searches parties; every criterion becomes part of one specification executed by
// the store.
type SearchParties struct {
	Text           string
	Kind           string
	Role           string // role type id: parties playing it (or a role below it) now
	Document       string // identification number
	Organization   string // internal organization id: parties affiliated with it now
	Facility       string // facility id: parties with a role at it now
	Classification string // classification type id: parties classified so now
	ActiveOnly     bool
	Page, Size     int
}

// PartyRelationships lists the relationships of a party.
type PartyRelationships struct {
	PartyID    domain.PartyID
	ActiveOnly bool
}

// ListRoleTypes lists the role type catalog.
type ListRoleTypes struct{}

// ListRelationshipTypes lists the relationship type catalog.
type ListRelationshipTypes struct{}

func nowOr(t *time.Time) time.Time {
	if t == nil {
		return fw.Now()
	}
	return t.UTC()
}
