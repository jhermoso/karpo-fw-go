// Package domain is the Parties bounded context domain model (UDM Vol. 1 "Party"), written only
// against the framework's pure domain packages. It shows the Go translation of the Karpo C#
// tactical patterns: typed identity, value objects with validating constructors, an aggregate
// root with child entities, invariants, domain events and specifications.
package domain

import (
	"slices"
	"strings"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Kind is the stable aggregate type name.
const Kind = "parties.party"

// PartyID identifies a Party.
type PartyID struct{ fw.UUID }

// NewPartyID returns a new identity.
func NewPartyID() PartyID { return PartyID{fw.NewUUID()} }

// ParsePartyID parses a textual identity.
func ParsePartyID(s string) (PartyID, error) {
	u, err := fw.ParseUUID(s)
	return PartyID{u}, err
}

// PartyType distinguishes people from organizations.
type PartyType string

// Party types.
const (
	Person       PartyType = "person"
	Organization PartyType = "organization"
)

// TaxID is a normalized tax identifier (value object).
type TaxID struct{ value string }

// NewTaxID validates and normalizes a tax identifier.
func NewTaxID(s string) (TaxID, error) {
	n := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), "-", ""))
	var v fw.Validation
	v.Require(len(n) >= 5 && len(n) <= 20, "taxId", "format", "tax id must have between 5 and 20 characters")
	if err := v.Err(); err != nil {
		return TaxID{}, err
	}
	return TaxID{value: n}, nil
}

func (t TaxID) String() string { return t.value }

// ContactKind is the channel of a contact mechanism.
type ContactKind string

// Contact kinds.
const (
	Email ContactKind = "email"
	Phone ContactKind = "phone"
)

// Contact is a child value of the Party aggregate.
type Contact struct {
	Kind    ContactKind
	Value   string
	Primary bool
}

// Party is the aggregate root.
type Party struct {
	fw.BaseAggregateRoot[PartyID]
	partyType    PartyType
	legalName    string
	taxID        TaxID
	active       bool
	registeredAt time.Time
	contacts     []Contact
}

// Register creates a new active party and raises PartyRegistered.
func Register(id PartyID, t PartyType, legalName string, taxID TaxID) (*Party, error) {
	p, err := Reconstitute(id, t, legalName, taxID, true, fw.Now(), nil)
	if err != nil {
		return nil, err
	}
	p.Raise(PartyRegistered{EventMeta: p.NewEventMeta(), Type: string(t), LegalName: p.legalName, TaxID: taxID.String()})
	return p, nil
}

// Reconstitute rebuilds a party from persisted state (no events, invariants re-checked).
func Reconstitute(id PartyID, t PartyType, legalName string, taxID TaxID, active bool, registeredAt time.Time, contacts []Contact) (*Party, error) {
	base, err := fw.NewBaseAggregateRoot(Kind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(t == Person || t == Organization, "type", "enum", "type must be person or organization")
	v.Require(strings.TrimSpace(legalName) != "", "legalName", "required", "legal name is required")
	v.Require(taxID != TaxID{}, "taxId", "required", "tax id is required")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Party{
		BaseAggregateRoot: base, partyType: t, legalName: strings.TrimSpace(legalName), taxID: taxID,
		active: active, registeredAt: registeredAt.UTC(), contacts: slices.Clone(contacts),
	}, nil
}

// Type returns the party type.
func (p *Party) Type() PartyType { return p.partyType }

// LegalName returns the legal name.
func (p *Party) LegalName() string { return p.legalName }

// TaxID returns the tax identifier.
func (p *Party) TaxID() TaxID { return p.taxID }

// Active reports whether the party is active.
func (p *Party) Active() bool { return p.active }

// RegisteredAt returns the registration instant.
func (p *Party) RegisteredAt() time.Time { return p.registeredAt }

// Contacts returns a copy of the contacts.
func (p *Party) Contacts() []Contact { return slices.Clone(p.contacts) }

// Rename changes the legal name.
func (p *Party) Rename(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		var v fw.Validation
		v.Add("legalName", "required", "legal name is required")
		return v.Err()
	}
	if !p.active {
		return fw.Violation("parties.inactive", "an inactive party cannot be renamed")
	}
	if name == p.legalName {
		return nil
	}
	old := p.legalName
	p.legalName = name
	p.Raise(PartyRenamed{EventMeta: p.NewEventMeta(), From: old, To: name})
	return nil
}

// AddContact adds a contact. Invariants: no duplicates, at most one primary contact per kind.
func (p *Party) AddContact(c Contact) error {
	var v fw.Validation
	v.Require(c.Kind == Email || c.Kind == Phone, "kind", "enum", "kind must be email or phone")
	v.Require(strings.TrimSpace(c.Value) != "", "value", "required", "value is required")
	if c.Kind == Email {
		v.Require(strings.Contains(c.Value, "@"), "value", "format", "invalid e-mail")
	}
	if err := v.Err(); err != nil {
		return err
	}
	for _, existing := range p.contacts {
		if existing.Kind == c.Kind && strings.EqualFold(existing.Value, c.Value) {
			return fw.Violation("parties.duplicate_contact", "contact already registered")
		}
	}
	if c.Primary {
		for i := range p.contacts {
			if p.contacts[i].Kind == c.Kind {
				p.contacts[i].Primary = false
			}
		}
	}
	p.contacts = append(slices.Clone(p.contacts), c)
	p.Raise(ContactAdded{EventMeta: p.NewEventMeta(), Kind: string(c.Kind), Value: c.Value, Primary: c.Primary})
	return nil
}

// Deactivate marks the party inactive.
func (p *Party) Deactivate() {
	if p.active {
		p.active = false
		p.Raise(PartyDeactivated{EventMeta: p.NewEventMeta()})
	}
}
