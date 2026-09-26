package domain

import (
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Repository is the Parties repository contract: the framework contract specialized for Party.
type Repository = fw.Repository[PartyID, *Party]

// Typed fields: the vocabulary specifications are written in. Names are logical; the
// infrastructure maps them to columns.
var (
	FieldID           = spec.Comparable[*Party, PartyID]("id", (*Party).ID)
	FieldType         = spec.Comparable[*Party, PartyType]("type", (*Party).Type)
	FieldLegalName    = spec.Text[*Party]("legal_name", (*Party).LegalName)
	FieldTaxID        = spec.Text[*Party]("tax_id", func(p *Party) string { return p.TaxID().String() })
	FieldActive       = spec.Comparable[*Party, bool]("active", (*Party).Active)
	FieldRegisteredAt = spec.Time[*Party]("registered_at", (*Party).RegisteredAt)
	FieldContacts     = spec.Collection[*Party, Contact]("contacts", (*Party).Contacts)

	ContactKindField  = spec.Comparable[Contact, ContactKind]("kind", func(c Contact) ContactKind { return c.Kind })
	ContactValueField = spec.Text[Contact]("value", func(c Contact) string { return c.Value })
	ContactPrimary    = spec.Comparable[Contact, bool]("is_primary", func(c Contact) bool { return c.Primary })
)

// Active selects active parties.
func Active() spec.Spec[*Party] { return FieldActive.Eq(true) }

// WithTaxID selects the party with a given tax id.
func WithTaxID(t TaxID) spec.Spec[*Party] { return FieldTaxID.Eq(t.String()) }

// NameContains selects parties whose legal name contains text, ignoring case.
func NameContains(text string) spec.Spec[*Party] { return FieldLegalName.ContainsFold(text) }

// RegisteredSince selects parties registered at or after t.
func RegisteredSince(t time.Time) spec.Spec[*Party] { return FieldRegisteredAt.AtOrAfter(t) }

// ReachableBy selects parties with a primary contact of the given kind.
func ReachableBy(kind ContactKind) spec.Spec[*Party] {
	return FieldContacts.Any(ContactKindField.Eq(kind).And(ContactPrimary.Eq(true)))
}

// MinContactsName is the name of the custom specification MinContacts.
const MinContactsName = "parties.min_contacts"

// MinContacts selects parties with at least n contacts. It cannot be expressed with the standard
// nodes (it aggregates), so it is a custom specification: every adapter provides its translation.
func MinContacts(n int) spec.Spec[*Party] {
	return spec.Custom(MinContactsName, func(p *Party) bool { return len(p.contacts) >= n }, int64(n))
}
