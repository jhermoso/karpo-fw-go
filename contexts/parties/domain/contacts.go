package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// ContactID identifies a contact of a party (child entity; other contexts may reference it, as
// the C# commercial profiles referenced bill-to and ship-to contact mechanisms).
type ContactID struct{ fw.UUID }

// NewContactID returns a new identity.
func NewContactID() ContactID { return ContactID{fw.NewUUID()} }

// ParseContactID parses a textual identity.
func ParseContactID(s string) (ContactID, error) { u, err := fw.ParseUUID(s); return ContactID{u}, err }

// ContactKind is the channel of a contact.
type ContactKind string

// Contact kinds (the C# ContactMechanismType rows; social media is a Web URL).
const (
	ContactEmail  ContactKind = "email"
	ContactPhone  ContactKind = "phone"
	ContactFax    ContactKind = "fax"
	ContactWeb    ContactKind = "web"
	ContactPostal ContactKind = "postal"
)

// Purpose tells what a contact is used for. Each purpose is held by at most one current contact
// of each kind (one default e-mail, one billing address...). The C# DEFAULT-POSTAL,
// DEFAULT-EMAIL, DEFAULT-PHONE and DEFAULT-FAX are PurposeDefault on the matching kind.
type Purpose string

// Purposes.
const (
	PurposeDefault  Purpose = "default"
	PurposeBilling  Purpose = "billing"
	PurposeShipping Purpose = "shipping"
	PurposeHome     Purpose = "home"
	PurposeWork     Purpose = "work"
)

// ParsePurpose validates a purpose.
func ParsePurpose(s string) (Purpose, error) {
	switch p := Purpose(strings.ToLower(strings.TrimSpace(s))); p {
	case PurposeDefault, PurposeBilling, PurposeShipping, PurposeHome, PurposeWork:
		return p, nil
	}
	var v fw.Validation
	v.Add("purposes", "enum", "purpose must be default, billing, shipping, home or work")
	return "", v.Err()
}

// GeoRef references the Geography context by identity (decision of the context map: geography
// is its own context; the address keeps the ids and validates them through a port).
type GeoRef struct {
	PostalCode fw.UUID // postal code entry, optional
	Boundary   fw.UUID // municipality or smallest boundary, optional
}

// PostalAddress is a postal address (value object).
type PostalAddress struct {
	StreetType string // code of the reference catalog (CL, AV, PZ...), optional
	Line1      string
	Line2      string
	Directions string
	PostalCode string
	Locality   string
	Region     string
	Country    vocab.CountryCode
	Geo        GeoRef
}

func (a PostalAddress) normalized() (PostalAddress, error) {
	a.StreetType, a.Line1, a.Line2 = strings.ToUpper(clean(a.StreetType)), clean(a.Line1), clean(a.Line2)
	a.Directions, a.PostalCode, a.Locality, a.Region = clean(a.Directions), strings.ToUpper(clean(a.PostalCode)), clean(a.Locality), clean(a.Region)
	var v fw.Validation
	v.Require(a.Line1 != "", "address.line1", "required", "the first address line is required")
	v.Require(!a.Country.IsZero(), "address.country", "required", "country is required")
	for field, s := range map[string]struct {
		v   string
		max int
	}{"address.streetType": {a.StreetType, 10}, "address.line1": {a.Line1, 200}, "address.line2": {a.Line2, 200},
		"address.directions": {a.Directions, 500}, "address.postalCode": {a.PostalCode, 20},
		"address.locality": {a.Locality, 100}, "address.region": {a.Region, 100}} {
		v.Require(utf8.RuneCountInString(s.v) <= s.max, field, "length", "too long")
	}
	return a, v.Err()
}

// String returns a one-line rendering of the address.
func (a PostalAddress) String() string {
	parts := []string{strings.TrimSpace(a.StreetType + " " + a.Line1), a.Line2, strings.TrimSpace(a.PostalCode + " " + a.Locality), a.Region, a.Country.String()}
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), ", ")
}

// Contact is a contact mechanism of a party with its purposes and validity (it merges the C#
// ContactMechanism, PartyContactMechanism and PartyContactMechanismPurpose, whose purposes were
// not even linked to the association).
type Contact struct {
	ID              ContactID
	Kind            ContactKind
	Value           string        // e-mail, E.164 phone or URL; empty for postal
	Address         PostalAddress // postal only
	Purposes        []Purpose
	NonSolicitation bool // the party does not want to be contacted for marketing here
	Period          vocab.ValidPeriod
}

// IsActiveAt reports whether the contact is in force at t.
func (c Contact) IsActiveAt(t time.Time) bool { return c.Period.IsActiveAt(t) }

// Has reports whether the contact holds a purpose.
func (c Contact) Has(p Purpose) bool { return slices.Contains(c.Purposes, p) }

// ContactData is what a user provides for a contact.
type ContactData struct {
	Kind            ContactKind
	Value           string
	Address         PostalAddress
	Purposes        []Purpose
	NonSolicitation bool
}

func (d ContactData) normalized() (ContactData, error) {
	var v fw.Validation
	switch d.Kind {
	case ContactEmail:
		e, err := vocab.NewEmail(d.Value)
		v.Merge("value", err)
		d.Value = e.String()
	case ContactPhone, ContactFax:
		ph, err := vocab.NewPhone(d.Value)
		v.Merge("value", err)
		d.Value = ph.String()
	case ContactWeb:
		u, err := vocab.NewURL(d.Value)
		v.Merge("value", err)
		d.Value = u.String()
	case ContactPostal:
		a, err := d.Address.normalized()
		v.Merge("", err)
		d.Address, d.Value = a, ""
	default:
		v.Add("kind", "enum", "kind must be email, phone, fax, web or postal")
	}
	slices.Sort(d.Purposes)
	d.Purposes = slices.Compact(d.Purposes)
	return d, v.Err()
}

func (d ContactData) sameMechanism(c Contact) bool {
	if d.Kind != c.Kind {
		return false
	}
	if d.Kind == ContactPostal {
		return strings.EqualFold(d.Address.String(), c.Address.String())
	}
	return strings.EqualFold(d.Value, c.Value)
}

// AddContact adds a contact from a moment on. Invariants: a valid value for its kind, not the
// same mechanism twice while current, and each purpose held by one current contact of the kind
// (a new holder takes the purpose from the previous one).
func (p *Party) AddContact(d ContactData, from time.Time) (ContactID, error) {
	if err := p.requireActive("get contacts"); err != nil {
		return ContactID{}, err
	}
	d, err := d.normalized()
	if err != nil {
		return ContactID{}, err
	}
	period, err := vocab.OpenPeriodFrom(from)
	if err != nil {
		return ContactID{}, err
	}
	for _, c := range p.contacts {
		if d.sameMechanism(c) && c.Period.Overlaps(period) {
			return ContactID{}, fw.Violation("parties.duplicate_contact", "the party already has this contact")
		}
	}
	c := Contact{ID: NewContactID(), Kind: d.Kind, Value: d.Value, Address: d.Address, NonSolicitation: d.NonSolicitation, Period: period}
	p.contacts = append(slices.Clone(p.contacts), c)
	p.assignPurposes(len(p.contacts)-1, d.Purposes)
	c = p.contacts[len(p.contacts)-1]
	p.Raise(ContactAdded{EventMeta: p.NewEventMeta(), ContactID: c.ID.String(), Kind: string(c.Kind), Value: c.Value,
		Address: c.Address.String(), Purposes: purposesOf(c.Purposes), From: period.From()})
	return c.ID, nil
}

// SetContactPurposes replaces the purposes of a current contact.
func (p *Party) SetContactPurposes(id ContactID, purposes []Purpose) error {
	k := slices.IndexFunc(p.contacts, func(c Contact) bool { return c.ID == id })
	if k < 0 {
		return fw.NotFound("parties.contact", id)
	}
	if !p.contacts[k].IsActiveAt(fw.Now()) {
		return fw.Violation("parties.contact_ended", "an ended contact cannot change its purposes")
	}
	p.contacts = slices.Clone(p.contacts)
	p.assignPurposes(k, purposes)
	c := p.contacts[k]
	p.Raise(ContactPurposesChanged{EventMeta: p.NewEventMeta(), ContactID: id.String(), Purposes: purposesOf(c.Purposes)})
	return nil
}

// assignPurposes gives purposes to contact k and takes them from the other current contacts of
// the same kind. p.contacts must already be a private copy.
func (p *Party) assignPurposes(k int, purposes []Purpose) {
	purposes = slices.Compact(slices.Sorted(slices.Values(purposes)))
	now := fw.Now()
	for i := range p.contacts {
		if i == k || p.contacts[i].Kind != p.contacts[k].Kind || !p.contacts[i].IsActiveAt(now) {
			continue
		}
		p.contacts[i].Purposes = slices.DeleteFunc(slices.Clone(p.contacts[i].Purposes), func(x Purpose) bool { return slices.Contains(purposes, x) })
	}
	p.contacts[k].Purposes = purposes
}

// EndContact stops using a contact at a moment (not before it started). It loses its purposes.
func (p *Party) EndContact(id ContactID, at time.Time) error {
	k := slices.IndexFunc(p.contacts, func(c Contact) bool { return c.ID == id })
	if k < 0 {
		return fw.NotFound("parties.contact", id)
	}
	c := p.contacts[k]
	if end, closed := c.Period.To(); closed && !at.Before(end) {
		return nil
	}
	period, err := vocab.NewValidPeriod(c.Period.From(), &at)
	if err != nil {
		return fw.Violation("parties.contact_end_before_start", "a contact cannot end before it starts")
	}
	p.contacts = slices.Clone(p.contacts)
	p.contacts[k].Period, p.contacts[k].Purposes = period, nil
	p.Raise(ContactEnded{EventMeta: p.NewEventMeta(), ContactID: id.String(), At: at.UTC()})
	return nil
}

// Contacts returns a copy of the contacts, current or past.
func (p *Party) Contacts() []Contact {
	out := slices.Clone(p.contacts)
	for i := range out {
		out[i].Purposes = slices.Clone(out[i].Purposes)
	}
	return out
}

// ContactFor returns the current contact of a kind holding a purpose (e.g. the default e-mail).
func (p *Party) ContactFor(kind ContactKind, purpose Purpose) (Contact, bool) {
	now := fw.Now()
	for _, c := range p.contacts {
		if c.Kind == kind && c.IsActiveAt(now) && c.Has(purpose) {
			return c, true
		}
	}
	return Contact{}, false
}

func purposesOf(ps []Purpose) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = string(p)
	}
	return out
}
