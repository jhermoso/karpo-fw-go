package domain

import (
	"maps"
	"slices"
	"strings"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Role is a function of an account in automatic entries.
type Role string

// Roles of the posting profile (PGC accounts they usually point to).
const (
	RoleRevenue             Role = "revenue"                  // 700/705
	RoleCustomers           Role = "customers"                // 430
	RoleOutputTax           Role = "output-tax"               // 477
	RoleSurcharge           Role = "surcharge"                // 477 (equivalence surcharge)
	RoleCash                Role = "cash"                     // 570
	RoleBank                Role = "bank"                     // 572
	RoleDirectDebitClearing Role = "direct-debit-clearing"    // 4312
	RoleWages               Role = "wages"                    // 640
	RoleEmployerSS          Role = "employer-social-security" // 642
	RoleSSPayable           Role = "social-security-payable"  // 476
	RoleWithholding         Role = "withholding-payable"      // 4751
	RoleOtherDeductions     Role = "other-deductions"         // 4659
	RoleNetPay              Role = "net-pay-payable"          // 465
	RoleSuppliers           Role = "suppliers"                // 400/410
	RoleInputTax            Role = "input-tax"                // 472
	RolePurchases           Role = "purchases"                // 600 (goods)
	RoleRent                Role = "rent"                     // 621
	RoleRepairs             Role = "repairs"                  // 622
	RoleProfessional        Role = "professional-services"    // 623
	RoleTransport           Role = "transport"                // 624
	RoleInsurance           Role = "insurance"                // 625
	RoleAdvertising         Role = "advertising"              // 627
	RoleSupplies            Role = "supplies"                 // 628
	RoleOtherServices       Role = "other-services"           // 629
	RoleFixedAssets         Role = "fixed-assets"             // 21x
	RoleAccumulatedDepr     Role = "accumulated-depreciation" // 281
	RoleDepreciation        Role = "depreciation-expense"     // 681
	RoleAssetReceivable     Role = "asset-sale-receivable"    // 543
	RoleAssetLoss           Role = "asset-disposal-loss"      // 671
	RoleAssetGain           Role = "asset-disposal-gain"      // 771
)

// Roles lists the roles a posting profile may define.
var Roles = []Role{RoleRevenue, RoleCustomers, RoleOutputTax, RoleSurcharge, RoleCash, RoleBank, RoleDirectDebitClearing, RoleWages,
	RoleEmployerSS, RoleSSPayable, RoleWithholding, RoleOtherDeductions, RoleNetPay, RoleSuppliers,
	RoleInputTax, RolePurchases, RoleRent, RoleRepairs, RoleProfessional, RoleTransport, RoleInsurance, RoleAdvertising, RoleSupplies, RoleOtherServices,
	RoleFixedAssets, RoleAccumulatedDepr, RoleDepreciation, RoleAssetReceivable, RoleAssetLoss, RoleAssetGain}

// LedgerKind is the stable aggregate type name.
const LedgerKind = "accounting.ledger"

// Period is a month of a fiscal year (1–12, counted from the start month).
type Period struct {
	Year  int // fiscal year, named by the calendar year it starts in
	Month int // 1–12 within the fiscal year
}

// LedgerState is the persisted state of a ledger.
type LedgerState struct {
	Company    OrganizationID
	StartMonth int               // month the fiscal year starts (the C# FiscalYearStartMonth)
	Accounts   map[Role]string   // posting profile: account code per role
	TaxCodes   map[string]string // output tax account per tax code (overrides the output-tax role)
	Closed     []Period
	Audit      traits.AuditStamp
}

// Ledger is the accounting setup of a company: fiscal year, closed periods and posting profile
// (the C# AccountingPeriod had neither dates nor status, and nothing decided the accounts of an
// entry).
type Ledger struct {
	fw.BaseAggregateRoot[LedgerID]
	traits.Audited
	s LedgerState
}

// ReconstituteLedger rebuilds a ledger.
func ReconstituteLedger(id LedgerID, s LedgerState) (*Ledger, error) {
	base, err := fw.NewBaseAggregateRoot(LedgerKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero(), "company", "required", "a ledger belongs to a company")
	v.Require(s.StartMonth >= 1 && s.StartMonth <= 12, "startMonth", "range", "a month from 1 to 12")
	checkProfile(&v, s.Accounts, s.TaxCodes)
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Accounts, s.TaxCodes, s.Closed = maps.Clone(s.Accounts), maps.Clone(s.TaxCodes), slices.Clone(s.Closed)
	if s.Accounts == nil {
		s.Accounts = map[Role]string{}
	}
	if s.TaxCodes == nil {
		s.TaxCodes = map[string]string{}
	}
	return &Ledger{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

func checkProfile(v *fw.Validation, accounts map[Role]string, taxes map[string]string) {
	for r, c := range accounts {
		v.Require(slices.Contains(Roles, r), "accounts", "role", "unknown role "+string(r))
		v.Require(ValidCode(c), "accounts."+string(r), "format", "an account code")
	}
	for t, c := range taxes {
		v.Require(t != "" && len(t) <= 5, "taxCodes", "code", "tax codes of 1 to 5 characters")
		v.Require(ValidCode(c), "taxCodes."+t, "format", "an account code")
	}
}

// State returns the state (maps and slices are copies).
func (l *Ledger) State() LedgerState {
	s := l.s
	s.Accounts, s.TaxCodes, s.Closed = maps.Clone(s.Accounts), maps.Clone(s.TaxCodes), slices.Clone(s.Closed)
	return s
}

// PeriodOf returns the fiscal period of a date.
func (l *Ledger) PeriodOf(d vocab.Date) Period {
	m := int(d.Month())
	year := d.Year()
	if m < l.s.StartMonth {
		year--
	}
	return Period{Year: year, Month: (m-l.s.StartMonth+12)%12 + 1}
}

// Bounds returns the first and last day of a fiscal year.
func (l *Ledger) Bounds(year int) (vocab.Date, vocab.Date) {
	start := vocab.MustDate(year, time.Month(l.s.StartMonth), 1)
	return start, start.AddMonths(12).AddDays(-1)
}

// IsClosed reports whether a period is closed.
func (l *Ledger) IsClosed(p Period) bool { return slices.Contains(l.s.Closed, p) }

// Close closes a period (entries dated in it are refused).
func (l *Ledger) Close(p Period) error {
	if p.Month < 1 || p.Month > 12 {
		return fw.Violation("accounting.period_invalid", "a period from 1 to 12")
	}
	if !l.IsClosed(p) {
		l.s.Closed = append(slices.Clone(l.s.Closed), p)
	}
	return nil
}

// Reopen reopens a period.
func (l *Ledger) Reopen(p Period) {
	l.s.Closed = slices.DeleteFunc(slices.Clone(l.s.Closed), func(x Period) bool { return x == p })
}

// SetProfile replaces the posting profile.
func (l *Ledger) SetProfile(accounts map[Role]string, taxes map[string]string) error {
	var v fw.Validation
	checkProfile(&v, accounts, taxes)
	if err := v.Err(); err != nil {
		return err
	}
	l.s.Accounts, l.s.TaxCodes = maps.Clone(accounts), maps.Clone(taxes)
	return nil
}

// AccountFor returns the account of a role.
func (l *Ledger) AccountFor(r Role) (string, error) {
	c, ok := l.s.Accounts[r]
	if !ok {
		return "", fw.Violation("accounting.role_undefined", "the posting profile has no account for "+string(r))
	}
	return c, nil
}

// TaxAccount returns the output tax account of a tax code (its override, else the role).
func (l *Ledger) TaxAccount(taxCode string) (string, error) {
	if c, ok := l.s.TaxCodes[strings.ToUpper(taxCode)]; ok {
		return c, nil
	}
	return l.AccountFor(RoleOutputTax)
}

// AuditSnapshot implements traits.Snapshotter.
func (l *Ledger) AuditSnapshot() map[string]any {
	return map[string]any{"startMonth": l.s.StartMonth, "roles": len(l.s.Accounts), "closed": len(l.s.Closed)}
}

// Ledger fields.
var LedFieldCompany = spec.Comparable("company", func(l *Ledger) OrganizationID { return l.s.Company })
