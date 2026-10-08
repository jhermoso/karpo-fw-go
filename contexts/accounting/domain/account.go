// Package domain is the model of the Accounting bounded context: the chart of accounts of each
// company, its ledger (fiscal year, closed periods and the posting profile that decides the
// accounts of automatic entries) and the journal entries, always balanced, numbered without gaps
// and linked to the fact that produced them. Invoices, collections, remittances and payslips
// belong to their contexts: Accounting posts what they publish.
package domain

import (
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

type (
	// AccountID identifies an account.
	AccountID struct{ fw.UUID }
	// LedgerID identifies a ledger.
	LedgerID struct{ fw.UUID }
	// EntryID identifies a journal entry.
	EntryID struct{ fw.UUID }
	// CounterID identifies an entry counter.
	CounterID struct{ fw.UUID }
	// OrganizationID is an internal organization (a company) of the Parties context.
	OrganizationID struct{ fw.UUID }
	// PartyID is a party (customer, employee…) of the Parties context.
	PartyID struct{ fw.UUID }
)

// Nature of an account by its PGC group: balance sheet (1–5), income statement (6–7) or income
// and expense recognized in equity (8–9).
type Nature int

// Natures.
const (
	BalanceSheet Nature = iota + 1
	IncomeStatement
	EquityIncome
)

var natures = map[Nature]string{BalanceSheet: "balance-sheet", IncomeStatement: "income-statement", EquityIncome: "equity-income"}

// String returns the stable name.
func (n Nature) String() string { return natures[n] }

// ValidCode checks an account code: 1 to 12 digits, the first one a PGC group (1–9).
func ValidCode(c string) bool {
	if len(c) == 0 || len(c) > 12 || c[0] == '0' {
		return false
	}
	for _, r := range c {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// NatureOf returns the nature of a code by its group.
func NatureOf(code string) Nature {
	switch code[0] {
	case '6', '7':
		return IncomeStatement
	case '8', '9':
		return EquityIncome
	}
	return BalanceSheet
}

// AccountKind is the stable aggregate type name.
const AccountKind = "accounting.account"

// AccountState is the persisted state of an account.
type AccountState struct {
	Company  OrganizationID
	Code     string
	Name     string
	Postable bool // detail account; headers (groups, subgroups) only aggregate
	Active   bool
	Audit    traits.AuditStamp
}

// Account is an account of the chart of a company (the C# split it in GeneralLedgerAccount, with
// the code in its Name, GlAccountProfile, with the code again, and AccountingPlan; the
// hierarchy is the code prefix, as in the PGC).
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
	v.Require(!s.Company.IsZero(), "company", "required", "an account belongs to a company")
	s.Code = strings.TrimSpace(s.Code)
	v.Require(ValidCode(s.Code), "code", "format", "1 to 12 digits, starting with a PGC group 1–9")
	s.Name = strings.TrimSpace(s.Name)
	v.Require(s.Name != "" && utf8.RuneCountInString(s.Name) <= 200, "name", "length", "a name of 1 to 200 characters")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Account{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State returns the state.
func (a *Account) State() AccountState { return a.s }

// Nature returns the nature of the account.
func (a *Account) Nature() Nature { return NatureOf(a.s.Code) }

// Rename changes the name.
func (a *Account) Rename(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 200 {
		var v fw.Validation
		v.Add("name", "length", "a name of 1 to 200 characters")
		return v.Err()
	}
	a.s.Name = name
	return nil
}

// Deactivate retires the account (entries already posted keep it).
func (a *Account) Deactivate() { a.s.Active = false }

// AuditSnapshot implements traits.Snapshotter.
func (a *Account) AuditSnapshot() map[string]any {
	return map[string]any{"code": a.s.Code, "name": a.s.Name, "postable": a.s.Postable, "active": a.s.Active}
}

// Account fields.
var (
	AccFieldCompany = spec.Comparable("company", func(a *Account) OrganizationID { return a.s.Company })
	AccFieldCode    = spec.Ordered("code", func(a *Account) string { return a.s.Code })
)
