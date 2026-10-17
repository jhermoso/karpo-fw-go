// Package domain es el modelo del contexto delimitado Treasury: las cuentas bancarias de cada
// organización interna, los mandatos de adeudo directo SEPA de sus clientes y las remesas que
// cobran los vencimientos que Receivables tiene abiertos (con el fichero ISO 20022 pain.008 para
// el banco). Las facturas son de Billing, los vencimientos y cobros de Receivables, las parties de
// Parties: este contexto los referencia por su identidad.
package domain

import (
	"strconv"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

type (
	// AccountID identifica una cuenta bancaria.
	AccountID struct{ fw.UUID }
	// MandateID identifica un mandato.
	MandateID struct{ fw.UUID }
	// RemittanceID identifica una remesa.
	RemittanceID struct{ fw.UUID }
	// OrganizationID es una organización interna del contexto Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID es una party (el deudor) del contexto Parties.
	PartyID struct{ fw.UUID }
	// InvoiceID es una factura de Billing (y su cuenta por cobrar en Receivables).
	InvoiceID struct{ fw.UUID }
)

// ValidBIC comprueba un BIC: 8 u 11 caracteres, banco (4 letras), país (2 letras), localidad y
// sucursal alfanuméricas.
func ValidBIC(b string) bool {
	if len(b) != 8 && len(b) != 11 {
		return false
	}
	for i, r := range b {
		letter := r >= 'A' && r <= 'Z'
		digit := r >= '0' && r <= '9'
		if (i < 6 && !letter) || (i >= 6 && !letter && !digit) {
			return false
		}
	}
	return true
}

// CreditorID construye el identificador de acreedor SEPA (AT-02): país, dígitos de control, sufijo
// y identificador nacional; los dígitos de control son ISO 7064 MOD 97-10 sobre el identificador
// nacional y el país seguido de 00 (el sufijo no cuenta).
func CreditorID(country, business, national string) (string, bool) {
	country, business = strings.ToUpper(country), strings.ToUpper(business)
	national = vocab.NormalizeDocumentNumber(national)
	if len(country) != 2 || len(business) != 3 || national == "" {
		return "", false
	}
	digits := ""
	for _, r := range national + country + "00" {
		switch {
		case r >= '0' && r <= '9':
			digits += string(r)
		case r >= 'A' && r <= 'Z':
			digits += strconv.Itoa(int(r-'A') + 10)
		default:
			return "", false
		}
	}
	rem := 0
	for _, r := range digits {
		rem = (rem*10 + int(r-'0')) % 97
	}
	return country + twoDigits(98-rem) + business + national, true
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// AccountKind es el nombre estable del tipo de agregado.
const AccountKind = "treasury.bank_account"

// AccountState es el estado persistido de una cuenta bancaria.
type AccountState struct {
	Owner          OrganizationID
	IBAN           vocab.IBAN
	BIC            string
	Alias          string
	Currency       vocab.CurrencyCode
	Collections    bool   // se usa para cobrar (adeudos)
	Payments       bool   // se usa para pagar
	CreditorSuffix string // sufijo de acreedor SEPA (3 caracteres, 000 por defecto)
	Opened         vocab.Date
	Closed         vocab.Date
	Audit          traits.AuditStamp
}

func (s *AccountState) check(v *fw.Validation) {
	v.Require(!s.Owner.IsZero(), "owner", "required", "an account belongs to an internal organization")
	v.Require(!s.IBAN.IsZero(), "iban", "required", "the IBAN is required")
	s.BIC = strings.ToUpper(strings.TrimSpace(s.BIC))
	v.Require(s.BIC == "" || ValidBIC(s.BIC), "bic", "format", "a BIC of 8 or 11 characters")
	s.Alias = strings.TrimSpace(s.Alias)
	v.Require(s.Alias != "" && utf8.RuneCountInString(s.Alias) <= 100, "alias", "length", "an alias of 1 to 100 characters")
	v.Require(s.Currency.String() == "EUR", "currency", "supported", "only euro accounts for now (SEPA)")
	s.CreditorSuffix = strings.ToUpper(strings.TrimSpace(s.CreditorSuffix))
	if s.CreditorSuffix == "" {
		s.CreditorSuffix = "000"
	}
	v.Require(len(s.CreditorSuffix) == 3, "creditorSuffix", "length", "3 characters")
	v.Require(!s.Opened.IsZero() && (s.Closed.IsZero() || !s.Closed.Before(s.Opened)), "opened", "order", "an opening date not after the closing")
}

// Account es una cuenta bancaria de una organización interna (el BankAccount de FinancialKernel de
// C#, duplicado por el OwnBankAccount de Parties; el IBAN ahora pasa siempre la comprobación
// MOD-97).
type Account struct {
	fw.BaseAggregateRoot[AccountID]
	traits.Audited
	s AccountState
}

// ReconstituteAccount reconstruye una cuenta.
func ReconstituteAccount(id AccountID, s AccountState) (*Account, error) {
	base, err := fw.NewBaseAggregateRoot(AccountKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	s.check(&v)
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Account{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State devuelve el estado.
func (a *Account) State() AccountState { return a.s }

// OpenOn dice si la cuenta está abierta en una fecha.
func (a *Account) OpenOn(d vocab.Date) bool {
	return !d.Before(a.s.Opened) && (a.s.Closed.IsZero() || !d.After(a.s.Closed))
}

// Close cierra la cuenta en una fecha.
func (a *Account) Close(on vocab.Date) error {
	if on.Before(a.s.Opened) {
		return fw.Violation("treasury.close_before_open", "an account cannot close before it opens")
	}
	a.s.Closed = on
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (a *Account) AuditSnapshot() map[string]any {
	return map[string]any{"iban": a.s.IBAN.String(), "alias": a.s.Alias, "closed": a.s.Closed.String()}
}

// Campos de la cuenta.
var (
	AccFieldOwner = spec.Comparable("owner", func(a *Account) OrganizationID { return a.s.Owner })
	AccFieldIBAN  = spec.Comparable("iban", func(a *Account) string { return a.s.IBAN.String() })
)
