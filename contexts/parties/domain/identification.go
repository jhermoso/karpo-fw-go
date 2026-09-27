package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// DocumentTypeID identifies an identity document type (same GUIDs as the C# catalog).
type DocumentTypeID struct{ fw.UUID }

// ParseDocumentTypeID parses a textual identity.
func ParseDocumentTypeID(s string) (DocumentTypeID, error) {
	u, err := fw.ParseUUID(s)
	return DocumentTypeID{u}, err
}

// MustDocumentTypeID parses a well-known identity.
func MustDocumentTypeID(s string) DocumentTypeID { return DocumentTypeID{fw.MustParseUUID(s)} }

// IdentificationID identifies an identification of a party (child entity).
type IdentificationID struct{ fw.UUID }

// NewIdentificationID returns a new identity.
func NewIdentificationID() IdentificationID { return IdentificationID{fw.NewUUID()} }

// ParseIdentificationID parses a textual identity.
func ParseIdentificationID(s string) (IdentificationID, error) {
	u, err := fw.ParseUUID(s)
	return IdentificationID{u}, err
}

// DocumentType is an entry of the identity document catalog (passport, national id, tax id...).
type DocumentType struct {
	ID                       DocumentTypeID
	Code                     string // ISO 20022 style: CCPT, NIDN, TXID, DRLC, SOSE, ARNU, OTHR
	Name                     vocab.Name
	DefaultPattern           string // used when no country rule applies
	RequiresExpiry           bool
	RequiresIssuingAuthority bool
	Active                   bool
}

// CountryDocumentRule adapts a document type to a country: availability, format and check digit
// (the C# country_document_rule). Nil requirement flags inherit the document type.
type CountryDocumentRule struct {
	ID                       string
	Country                  vocab.CountryCode
	DocumentType             DocumentTypeID
	Available                bool
	Default                  bool
	DisplayOrder             int
	Pattern                  string
	MinLength, MaxLength     int
	CheckDigit               string // vocab.ValidCheckDigit key
	RequiresExpiry           *bool
	RequiresIssuingAuthority *bool
	Active                   bool
}

// DocumentPolicy validates identification numbers with the document catalog and the country
// rules. The C# only flagged a "format status" after saving; here an invalid number is refused.
type DocumentPolicy struct {
	types    map[DocumentTypeID]DocumentType
	rules    map[string]CountryDocumentRule
	patterns map[string]*regexp.Regexp
}

func ruleKey(c vocab.CountryCode, t DocumentTypeID) string { return c.String() + "/" + t.String() }

// NewDocumentPolicy builds the policy, compiling every pattern and checking every check-digit
// key up front: a misconfigured catalog fails at load time, not on the first customer.
func NewDocumentPolicy(types []DocumentType, rules []CountryDocumentRule) (*DocumentPolicy, error) {
	p := &DocumentPolicy{types: map[DocumentTypeID]DocumentType{}, rules: map[string]CountryDocumentRule{}, patterns: map[string]*regexp.Regexp{}}
	compile := func(pattern string) error {
		if pattern == "" || p.patterns[pattern] != nil {
			return nil
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("%w: document pattern %q: %v", fw.ErrValidation, pattern, err)
		}
		p.patterns[pattern] = re
		return nil
	}
	for _, t := range types {
		if err := compile(t.DefaultPattern); err != nil {
			return nil, err
		}
		p.types[t.ID] = t
	}
	for _, r := range rules {
		if _, ok := p.types[r.DocumentType]; !ok {
			return nil, fmt.Errorf("%w: rule %s references unknown document type %s", fw.ErrValidation, r.ID, r.DocumentType)
		}
		if err := compile(r.Pattern); err != nil {
			return nil, err
		}
		if r.CheckDigit != "" {
			if _, known := vocab.ValidCheckDigit(r.CheckDigit, ""); !known {
				return nil, fmt.Errorf("%w: rule %s uses unknown check digit %s", fw.ErrValidation, r.ID, r.CheckDigit)
			}
		}
		if r.Active {
			p.rules[ruleKey(r.Country, r.DocumentType)] = r
		}
	}
	return p, nil
}

// DocumentType returns a document type.
func (p *DocumentPolicy) DocumentType(id DocumentTypeID) (DocumentType, bool) {
	t, ok := p.types[id]
	return t, ok
}

// Options returns the document types available in a country, in display order (the C# country
// reference query used by the forms).
func (p *DocumentPolicy) Options(country vocab.CountryCode) []CountryDocumentRule {
	var out []CountryDocumentRule
	for _, r := range p.rules {
		if r.Country == country && r.Available {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b CountryDocumentRule) int { return a.DisplayOrder - b.DisplayOrder })
	return out
}

// IdentificationData is what a user provides for an identification.
type IdentificationData struct {
	Type             DocumentTypeID
	Country          vocab.CountryCode
	Number           string
	IssuingAuthority string
	IssuedOn         vocab.Date // optional
	ExpiresOn        vocab.Date // required when the rule or type says so
}

// Validate checks an identification and returns its canonical number (upper case, without
// spaces, and without separators unless the country format needs them).
func (p *DocumentPolicy) Validate(d IdentificationData) (string, error) {
	var v fw.Validation
	t, ok := p.types[d.Type]
	if !ok || !t.Active {
		v.Add("documentType", "unknown", "unknown or inactive document type")
		return "", v.Err()
	}
	if d.Country.IsZero() {
		v.Add("country", "required", "issuing country is required")
		return "", v.Err()
	}
	compact := strings.ToUpper(strings.Join(strings.Fields(d.Number), ""))
	if compact == "" || utf8.RuneCountInString(compact) > 60 {
		v.Add("number", "length", "document number must have 1 to 60 characters")
		return "", v.Err()
	}
	pattern, requiresExpiry, requiresAuthority := t.DefaultPattern, t.RequiresExpiry, t.RequiresIssuingAuthority
	rule, hasRule := p.rules[ruleKey(d.Country, d.Type)]
	if hasRule {
		if !rule.Available {
			return "", fw.Violation("parties.document_not_available", t.Name.String()+" is not issued in "+d.Country.String())
		}
		if rule.Pattern != "" {
			pattern = rule.Pattern
		}
		if rule.RequiresExpiry != nil {
			requiresExpiry = *rule.RequiresExpiry
		}
		if rule.RequiresIssuingAuthority != nil {
			requiresAuthority = *rule.RequiresIssuingAuthority
		}
	}
	// The canonical number drops separators (12345678-Z is 12345678Z) unless the format needs
	// them (the Finnish century sign, the optional Swedish dash): the first form that fits wins.
	fits := func(n string) []string {
		var problems []string
		l := utf8.RuneCountInString(n)
		if hasRule && rule.MinLength > 0 && l < rule.MinLength {
			problems = append(problems, fmt.Sprintf("at least %d characters", rule.MinLength))
		}
		if hasRule && rule.MaxLength > 0 && l > rule.MaxLength {
			problems = append(problems, fmt.Sprintf("at most %d characters", rule.MaxLength))
		}
		if pattern != "" && !p.patterns[pattern].MatchString(n) {
			problems = append(problems, "document number does not have the format of "+t.Name.String()+" in "+d.Country.String())
		}
		return problems
	}
	number := vocab.NormalizeDocumentNumber(compact)
	if problems := fits(number); len(problems) > 0 {
		number = compact
		for _, msg := range fits(compact) {
			v.Add("number", "format", msg)
		}
	}
	if hasRule && rule.CheckDigit != "" && !v.HasErrors() {
		if ok, _ := vocab.ValidCheckDigit(rule.CheckDigit, number); !ok {
			v.Add("number", "check_digit", "document number check digit does not match")
		}
	}
	v.Require(!requiresExpiry || !d.ExpiresOn.IsZero(), "expiresOn", "required", t.Name.String()+" requires an expiry date")
	v.Require(!requiresAuthority || strings.TrimSpace(d.IssuingAuthority) != "", "issuingAuthority", "required",
		t.Name.String()+" requires the issuing authority")
	v.Require(d.IssuedOn.IsZero() || d.ExpiresOn.IsZero() || d.IssuedOn.Before(d.ExpiresOn), "expiresOn", "order", "expiry must follow issue")
	v.Require(utf8.RuneCountInString(d.IssuingAuthority) <= 200, "issuingAuthority", "length", "at most 200 characters")
	return number, v.Err()
}

// Identification is an identity document of a party (child entity of Party).
type Identification struct {
	ID               IdentificationID
	Type             DocumentTypeID
	Country          vocab.CountryCode
	Number           string
	IssuingAuthority string
	IssuedOn         vocab.Date
	ExpiresOn        vocab.Date
	Primary          bool
}

// Matches reports whether the identification is the same document (type, country, number).
func (i Identification) Matches(t DocumentTypeID, c vocab.CountryCode, number string) bool {
	return i.Type == t && i.Country == c && i.Number == number
}

// AddIdentification validates and adds an identity document. Invariants: a valid number for the
// type and country, not registered twice in the party, one primary identification (the first
// one is primary; a new primary demotes the previous one).
func (p *Party) AddIdentification(policy *DocumentPolicy, d IdentificationData, primary bool) (IdentificationID, error) {
	if err := p.requireActive("get identifications"); err != nil {
		return IdentificationID{}, err
	}
	number, err := policy.Validate(d)
	if err != nil {
		return IdentificationID{}, err
	}
	for _, i := range p.identifications {
		if i.Matches(d.Type, d.Country, number) {
			return IdentificationID{}, fw.Violation("parties.duplicate_identification", "the party already has this document")
		}
	}
	id := Identification{ID: NewIdentificationID(), Type: d.Type, Country: d.Country, Number: number,
		IssuingAuthority: strings.TrimSpace(d.IssuingAuthority), IssuedOn: d.IssuedOn, ExpiresOn: d.ExpiresOn,
		Primary: primary || len(p.identifications) == 0}
	ids := slices.Clone(p.identifications)
	if id.Primary {
		for k := range ids {
			ids[k].Primary = false
		}
	}
	p.identifications = append(ids, id)
	p.Raise(IdentificationAdded{EventMeta: p.NewEventMeta(), IdentificationID: id.ID.String(), DocumentType: d.Type.String(),
		Country: d.Country.String(), Number: number, Primary: id.Primary})
	return id.ID, nil
}

// RemoveIdentification removes an identity document.
func (p *Party) RemoveIdentification(id IdentificationID) error {
	k := slices.IndexFunc(p.identifications, func(i Identification) bool { return i.ID == id })
	if k < 0 {
		return fw.NotFound("parties.identification", id)
	}
	removed := p.identifications[k]
	p.identifications = slices.Delete(slices.Clone(p.identifications), k, k+1)
	p.Raise(IdentificationRemoved{EventMeta: p.NewEventMeta(), IdentificationID: id.String(), DocumentType: removed.Type.String(),
		Country: removed.Country.String(), Number: removed.Number})
	return nil
}

// Identifications returns a copy of the identity documents.
func (p *Party) Identifications() []Identification { return slices.Clone(p.identifications) }

// PrimaryIdentification returns the primary document, if any.
func (p *Party) PrimaryIdentification() (Identification, bool) {
	for _, i := range p.identifications {
		if i.Primary {
			return i, true
		}
	}
	return Identification{}, false
}
