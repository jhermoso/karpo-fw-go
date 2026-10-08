// Package domain is the model of the Treasury bounded context: the bank accounts of each internal
// organization, the SEPA direct debit mandates of its customers and the remittances that collect
// the installments Receivables has open (with the ISO 20022 pain.008 file for the bank). Invoices
// belong to Billing, installments and collections to Receivables, parties to Parties: this context
// references them by identity.
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
	// AccountID identifies a bank account.
	AccountID struct{ fw.UUID }
	// MandateID identifies a mandate.
	MandateID struct{ fw.UUID }
	// RemittanceID identifies a remittance.
	RemittanceID struct{ fw.UUID }
	// OrganizationID is an internal organization of the Parties context.
	OrganizationID struct{ fw.UUID }
	// PartyID is a party (the debtor) of the Parties context.
	PartyID struct{ fw.UUID }
	// InvoiceID is an invoice of Billing (and its receivable in Receivables).
	InvoiceID struct{ fw.UUID }
)

// ValidBIC checks a BIC: 8 or 11 characters, bank (4 letters), country (2 letters), location and
// branch alphanumeric.
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

// CreditorID builds the SEPA creditor identifier (AT-02): country, check digits, business code and
// national identifier; the check digits are ISO 7064 MOD 97-10 over the national identifier and the
// country followed by 00 (the business code does not count).
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

// AccountKind is the stable aggregate type name.
const AccountKind = "treasury.bank_account"

// AccountState is the persisted state of a bank account.
type AccountState struct {
	Owner          OrganizationID
	IBAN           vocab.IBAN
	BIC            string
	Alias          string
	Currency       vocab.CurrencyCode
	Collections    bool   // used to collect (direct debits)
	Payments       bool   // used to pay
	CreditorSuffix string // SEPA creditor business code (3 characters, 000 by default)
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

// Account is a bank account of an internal organization (the C# BankAccount of FinancialKernel,
// duplicated by OwnBankAccount of Parties; the IBAN now always passes the MOD-97 check).
type Account struct {
	fw.BaseAggregateRoot[AccountID]
	traits.Audited
	s AccountState
}

// ReconstituteAccount rebuilds an account.
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

// State returns the state.
func (a *Account) State() AccountState { return a.s }

// OpenOn reports whether the account is open on a date.
func (a *Account) OpenOn(d vocab.Date) bool {
	return !d.Before(a.s.Opened) && (a.s.Closed.IsZero() || !d.After(a.s.Closed))
}

// Close closes the account on a date.
func (a *Account) Close(on vocab.Date) error {
	if on.Before(a.s.Opened) {
		return fw.Violation("treasury.close_before_open", "an account cannot close before it opens")
	}
	a.s.Closed = on
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (a *Account) AuditSnapshot() map[string]any {
	return map[string]any{"iban": a.s.IBAN.String(), "alias": a.s.Alias, "closed": a.s.Closed.String()}
}

// Account fields.
var (
	AccFieldOwner = spec.Comparable("owner", func(a *Account) OrganizationID { return a.s.Owner })
	AccFieldIBAN  = spec.Comparable("iban", func(a *Account) string { return a.s.IBAN.String() })
)
