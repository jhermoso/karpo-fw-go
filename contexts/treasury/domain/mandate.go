package domain

import (
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Scheme is the SEPA direct debit scheme.
type Scheme int

// Schemes.
const (
	Core Scheme = iota + 1
	B2B
)

var schemes = map[Scheme]string{Core: "CORE", B2B: "B2B"}

// String returns the scheme code of the pain.008 file.
func (s Scheme) String() string { return schemes[s] }

// ParseScheme parses a scheme code.
func ParseScheme(s string) (Scheme, bool) {
	for k, n := range schemes {
		if n == strings.ToUpper(s) {
			return k, true
		}
	}
	return 0, false
}

// MandateKind is the stable aggregate type name.
const MandateKind = "treasury.mandate"

// MandateExpiry is how long a mandate lives without being used (36 months, SEPA rulebook).
const MandateExpiry = 36

// MandateState is the persisted state of a mandate.
type MandateState struct {
	Creditor  OrganizationID
	Debtor    PartyID
	IBAN      vocab.IBAN
	Reference string
	Scheme    Scheme
	Signed    vocab.Date
	LastUsed  vocab.Date // zero: never used (the next collection is FRST)
	Revoked   vocab.Date
	Audit     traits.AuditStamp
}

// Mandate is the authorization of a debtor to collect from its account by direct debit (the C#
// only had a free MandateReference on the commercial profile, overwritten by the import).
type Mandate struct {
	fw.BaseAggregateRoot[MandateID]
	traits.Audited
	s MandateState
}

// ReconstituteMandate rebuilds a mandate.
func ReconstituteMandate(id MandateID, s MandateState) (*Mandate, error) {
	base, err := fw.NewBaseAggregateRoot(MandateKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Creditor.IsZero() && !s.Debtor.IsZero(), "debtor", "required", "creditor and debtor are required")
	v.Require(!s.IBAN.IsZero(), "iban", "required", "the debtor IBAN is required")
	s.Reference = strings.TrimSpace(s.Reference)
	v.Require(s.Reference != "" && utf8.RuneCountInString(s.Reference) <= 35 && sepaText(s.Reference), "reference", "format",
		"a reference of 1 to 35 SEPA characters")
	_, ok := schemes[s.Scheme]
	v.Require(ok, "scheme", "enum", "CORE or B2B")
	v.Require(!s.Signed.IsZero(), "signed", "required", "the signature date is required")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Mandate{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State returns the state.
func (m *Mandate) State() MandateState { return m.s }

// UsableOn reports whether the mandate can be used on a date: signed, not revoked, and used (or
// signed) within the last 36 months.
func (m *Mandate) UsableOn(d vocab.Date) bool {
	if d.Before(m.s.Signed) || (!m.s.Revoked.IsZero() && !d.Before(m.s.Revoked)) {
		return false
	}
	last := m.s.Signed
	if !m.s.LastUsed.IsZero() {
		last = m.s.LastUsed
	}
	return d.Before(last.AddMonths(MandateExpiry))
}

// Sequence returns the sequence type of the next collection: FRST for the first, RCUR after.
func (m *Mandate) Sequence() string {
	if m.s.LastUsed.IsZero() {
		return "FRST"
	}
	return "RCUR"
}

// Use records a collection on a date.
func (m *Mandate) Use(on vocab.Date) error {
	if !m.UsableOn(on) {
		return fw.Violation("treasury.mandate_unusable", "the mandate is revoked, expired or not yet signed")
	}
	if m.s.LastUsed.IsZero() || on.After(m.s.LastUsed) {
		m.s.LastUsed = on
	}
	return nil
}

// Revoke revokes the mandate from a date.
func (m *Mandate) Revoke(on vocab.Date) error {
	if on.Before(m.s.Signed) {
		return fw.Violation("treasury.revoke_before_signature", "a mandate cannot be revoked before it is signed")
	}
	m.s.Revoked = on
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (m *Mandate) AuditSnapshot() map[string]any {
	return map[string]any{"reference": m.s.Reference, "lastUsed": m.s.LastUsed.String(), "revoked": m.s.Revoked.String()}
}

// sepaText reports whether a text uses only the SEPA basic Latin character set.
func sepaText(s string) bool {
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("/-?:().,'+ ", r)
		if !ok {
			return false
		}
	}
	return true
}

// Mandate fields.
var (
	ManFieldCreditor  = spec.Comparable("creditor", func(m *Mandate) OrganizationID { return m.s.Creditor })
	ManFieldDebtor    = spec.Comparable("debtor", func(m *Mandate) PartyID { return m.s.Debtor })
	ManFieldReference = spec.Ordered("reference", func(m *Mandate) string { return m.s.Reference })
)
