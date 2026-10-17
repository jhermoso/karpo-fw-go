package domain

import (
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Scheme es el esquema de adeudo directo SEPA.
type Scheme int

// Esquemas.
const (
	Core Scheme = iota + 1
	B2B
)

var schemes = map[Scheme]string{Core: "CORE", B2B: "B2B"}

// String devuelve el código de esquema del fichero pain.008.
func (s Scheme) String() string { return schemes[s] }

// ParseScheme interpreta un código de esquema.
func ParseScheme(s string) (Scheme, bool) {
	for k, n := range schemes {
		if n == strings.ToUpper(s) {
			return k, true
		}
	}
	return 0, false
}

// MandateKind es el nombre estable del tipo de agregado.
const MandateKind = "treasury.mandate"

// MandateExpiry es cuánto dura un mandato sin usarse (36 meses, reglamento SEPA).
const MandateExpiry = 36

// MandateState es el estado persistido de un mandato.
type MandateState struct {
	Creditor  OrganizationID
	Debtor    PartyID
	IBAN      vocab.IBAN
	Reference string
	Scheme    Scheme
	Signed    vocab.Date
	LastUsed  vocab.Date // cero: nunca usado (el próximo cobro es FRST)
	Revoked   vocab.Date
	Audit     traits.AuditStamp
}

// Mandate es la autorización de un deudor para cobrar de su cuenta por adeudo directo (el C# sólo
// tenía un MandateReference libre en el perfil comercial, que la importación sobrescribía).
type Mandate struct {
	fw.BaseAggregateRoot[MandateID]
	traits.Audited
	s MandateState
}

// ReconstituteMandate reconstruye un mandato.
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

// State devuelve el estado.
func (m *Mandate) State() MandateState { return m.s }

// UsableOn dice si el mandato se puede usar en una fecha: firmado, no revocado, y usado (o
// firmado) en los últimos 36 meses.
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

// Sequence devuelve el tipo de secuencia del próximo cobro: FRST para el primero, RCUR después.
func (m *Mandate) Sequence() string {
	if m.s.LastUsed.IsZero() {
		return "FRST"
	}
	return "RCUR"
}

// Use registra un cobro en una fecha.
func (m *Mandate) Use(on vocab.Date) error {
	if !m.UsableOn(on) {
		return fw.Violation("treasury.mandate_unusable", "the mandate is revoked, expired or not yet signed")
	}
	if m.s.LastUsed.IsZero() || on.After(m.s.LastUsed) {
		m.s.LastUsed = on
	}
	return nil
}

// Revoke revoca el mandato desde una fecha.
func (m *Mandate) Revoke(on vocab.Date) error {
	if on.Before(m.s.Signed) {
		return fw.Violation("treasury.revoke_before_signature", "a mandate cannot be revoked before it is signed")
	}
	m.s.Revoked = on
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (m *Mandate) AuditSnapshot() map[string]any {
	return map[string]any{"reference": m.s.Reference, "lastUsed": m.s.LastUsed.String(), "revoked": m.s.Revoked.String()}
}

// sepaText dice si un texto usa sólo el juego de caracteres latino básico de SEPA.
func sepaText(s string) bool {
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("/-?:().,'+ ", r)
		if !ok {
			return false
		}
	}
	return true
}

// Campos del mandato.
var (
	ManFieldCreditor  = spec.Comparable("creditor", func(m *Mandate) OrganizationID { return m.s.Creditor })
	ManFieldDebtor    = spec.Comparable("debtor", func(m *Mandate) PartyID { return m.s.Debtor })
	ManFieldReference = spec.Ordered("reference", func(m *Mandate) string { return m.s.Reference })
)
