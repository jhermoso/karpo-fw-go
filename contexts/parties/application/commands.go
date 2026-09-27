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
)

// RegisterPerson registers a person, optionally with initial roles (the C# CreatePartyWithRole).
type RegisterPerson struct {
	GivenName     string   `json:"givenName"`
	FirstSurname  string   `json:"firstSurname"`
	SecondSurname string   `json:"secondSurname,omitempty"`
	Gender        string   `json:"gender,omitempty"`
	BirthDate     string   `json:"birthDate,omitempty"` // YYYY-MM-DD
	MaritalStatus string   `json:"maritalStatus,omitempty"`
	Roles         []string `json:"roles,omitempty"`
	RequestID     string   `json:"-"` // Idempotency-Key
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
	LegalName string   `json:"legalName"`
	TradeName string   `json:"tradeName,omitempty"`
	Roles     []string `json:"roles,omitempty"`
	RequestID string   `json:"-"`
}

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

// EstablishRelationship relates two parties (the roles come from the relationship type).
type EstablishRelationship struct {
	Type   string     `json:"type"`
	From   string     `json:"fromParty"`
	To     string     `json:"toParty"`
	Since  *time.Time `json:"since,omitempty"`
	Remark string     `json:"remark,omitempty"`
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
	Text       string
	Kind       string
	Role       string // role type id: parties playing it (or a role below it) now
	ActiveOnly bool
	Page, Size int
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
