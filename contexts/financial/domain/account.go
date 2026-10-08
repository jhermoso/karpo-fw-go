// Package domain is the model of the Financial (Cuentas de clientes) bounded context: the
// accounts an institution keeps for its customers, who holds each, what it is used for and
// whether it can operate. In C# this was split in two models that never met: FinancialAccount, an
// empty shell with no data, and BankAccount, which held everything that was real.
package domain

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// AccountKind is the aggregate type name of an account.
const AccountKind = "financial.account"

// Limits of an account.
const (
	MaxHolders = 50
	MaxUses    = 50
)

// Identities of the context.
type (
	// AccountID identifies an account.
	AccountID struct{ fw.UUID }
	// OrganizationID is the institution, an internal organization of Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID is a customer or anyone related to an account.
	PartyID struct{ fw.UUID }
)

// NewAccountID returns a fresh identity.
func NewAccountID() AccountID { return AccountID{fw.NewUUID()} }

// ParseAccountID parses a textual identity.
func ParseAccountID(s string) (AccountID, error) { u, err := fw.ParseUUID(s); return AccountID{u}, err }

// Status is whether an account can operate.
type Status string

// Statuses (the C# Active, Blocked and Abandoned, the ones of Apiscore, plus the closing it lacked).
const (
	Active    Status = "active"
	Blocked   Status = "blocked"   // it cannot operate until it is released
	Abandoned Status = "abandoned" // its holder stopped using it; the money waits apart
	Closed    Status = "closed"
)

// Statuses lists the valid statuses.
var Statuses = []Status{Active, Blocked, Abandoned, Closed}

// Roles of a party on an account (the C# PartyBankAccountRole).
const (
	RoleHolder      = "holder"
	RoleAuthorized  = "authorized"
	RoleBeneficiary = "beneficiary"
)

// Roles lists the valid roles.
var Roles = []string{RoleHolder, RoleAuthorized, RoleBeneficiary}

// Uses an account may have (the ten the C# seeded as a table; the accounts of the institution
// itself are those of Treasury).
var Uses = []string{"customer-payment", "virtual-multicurrency", "agent", "player", "player-operating", "player-transit", "cash-operating",
	"segregated", "abandonment"}

// Holder is a party related to an account between two days.
type Holder struct {
	Party   PartyID
	Role    string
	From    vocab.Date
	Thru    vocab.Date // the last day; zero while it lasts
	Primary bool       // the one the account is filed under
}

// Current reports whether the relation lasts.
func (h Holder) Current() bool { return h.Thru.IsZero() }

// Use is something an account is used for between two days.
type Use struct {
	Code string
	From vocab.Date
	Thru vocab.Date
}

// AccountState is the persisted state of an account.
type AccountState struct {
	Company  OrganizationID
	Number   string     // the IBAN without spaces, or the institution's own identifier when it is virtual
	Virtual  bool       // it has no IBAN: it exists only in the books of the institution
	IBAN     vocab.IBAN // of an account that is not virtual
	BIC      string
	Currency vocab.CurrencyCode
	Name     string
	Product  fw.UUID // the product of Products it is an instance of
	Status   Status
	Demo     bool // a test account of the institution: it is never real money
	Opened   vocab.Date
	Closed   vocab.Date
	Reason   string // of the block, the abandonment or the closing
	Holders  []Holder
	Uses     []Use
	Audit    traits.AuditStamp
}

// Account is an account an institution keeps for its customers.
type Account struct {
	fw.BaseAggregateRoot[AccountID]
	traits.Audited
	s AccountState
}

// NormalizeNumber removes the spaces and raises the letters of an account number.
func NormalizeNumber(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), ""))
}

func validBIC(s string) bool {
	if len(s) != 8 && len(s) != 11 {
		return false
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func validNumber(s string) bool {
	if len(s) < 8 || len(s) > 34 {
		return false
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// ReconstituteAccount rebuilds an account.
func ReconstituteAccount(id AccountID, s AccountState) (*Account, error) {
	base, err := fw.NewBaseAggregateRoot(AccountKind, id)
	if err != nil {
		return nil, err
	}
	s.Number, s.BIC = NormalizeNumber(s.Number), strings.ToUpper(strings.TrimSpace(s.BIC))
	s.Name, s.Reason = strings.TrimSpace(s.Name), strings.TrimSpace(s.Reason)
	var v fw.Validation
	v.Require(!s.Company.IsZero(), "company", "required", "the institution that keeps the account")
	if s.Virtual {
		v.Require(s.IBAN.IsZero() && validNumber(s.Number), "number", "format", "an identifier of 8 to 34 letters, digits and dashes")
	} else {
		v.Require(!s.IBAN.IsZero() && s.Number == s.IBAN.String(), "iban", "required", "an account that is not virtual has an IBAN")
	}
	v.Require(s.BIC == "" || validBIC(s.BIC), "bic", "format", "a BIC of 8 or 11 characters")
	v.Require(!s.Currency.IsZero(), "currency", "required", "the currency is required")
	v.Require(utf8.RuneCountInString(s.Name) <= 200 && utf8.RuneCountInString(s.Reason) <= 200, "name", "length",
		"a name and a reason of at most 200 characters")
	v.Require(slices.Contains(Statuses, s.Status), "status", "enum", "a status")
	v.Require(!s.Opened.IsZero(), "opened", "required", "the day it was opened")
	v.Require((s.Status == Closed) == !s.Closed.IsZero() && (s.Closed.IsZero() || !s.Closed.Before(s.Opened)), "closed", "state",
		"a closed account, and only it, has the day it was closed, not before it was opened")
	v.Require(len(s.Holders) <= MaxHolders && len(s.Uses) <= MaxUses, "holders", "count", "too many holders or uses")
	primaries, holders := 0, 0
	seen := map[string]bool{}
	for i, h := range s.Holders {
		f := fmt.Sprintf("holders[%d]", i)
		v.Require(!h.Party.IsZero() && slices.Contains(Roles, h.Role), f, "required", "a party with a role: holder, authorized or beneficiary")
		v.Require(!h.From.IsZero() && (h.Thru.IsZero() || !h.Thru.Before(h.From)), f, "range", "from a day, and not until before it")
		if h.Current() {
			k := h.Party.String() + "|" + h.Role
			v.Require(!seen[k], f, "duplicate", "a party has each role once at a time")
			seen[k] = true
			if h.Role == RoleHolder {
				holders++
			}
			if h.Primary {
				primaries++
				v.Require(h.Role == RoleHolder, f, "primary", "the account is filed under one of its holders")
			}
		} else {
			v.Require(!h.Primary, f, "primary", "the account is filed under someone who still holds it")
		}
	}
	// A closed account keeps its history, not its holders.
	v.Require(s.Status == Closed || holders >= 1 && primaries == 1, "holders", "required", "an account has at least one holder, and is filed under one of them")
	open := map[string]bool{}
	for i, u := range s.Uses {
		f := fmt.Sprintf("uses[%d]", i)
		v.Require(slices.Contains(Uses, u.Code), f, "enum", "a use of the catalogue")
		v.Require(!u.From.IsZero() && (u.Thru.IsZero() || !u.Thru.Before(u.From)), f, "range", "from a day, and not until before it")
		if u.Thru.IsZero() {
			v.Require(!open[u.Code], f, "duplicate", "an account has each use once at a time")
			open[u.Code] = true
		}
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Holders, s.Uses = slices.Clone(s.Holders), slices.Clone(s.Uses)
	return &Account{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// Opening is what an account is opened with.
type Opening struct {
	Company  OrganizationID
	Number   string
	Virtual  bool
	BIC      string
	Currency vocab.CurrencyCode
	Name     string
	Product  fw.UUID
	Demo     bool
	Opened   vocab.Date
	Holder   PartyID
	Uses     []string
}

// Open opens an account for a customer, who holds it from that day and under whom it is filed.
// The number of an account that is not virtual is an IBAN, and its check digits must be right
// (the C# checked only its length).
func Open(id AccountID, o Opening) (*Account, error) {
	s := AccountState{Company: o.Company, Number: NormalizeNumber(o.Number), Virtual: o.Virtual, BIC: o.BIC, Currency: o.Currency, Name: o.Name,
		Product: o.Product, Status: Active, Demo: o.Demo, Opened: o.Opened,
		Holders: []Holder{{Party: o.Holder, Role: RoleHolder, From: o.Opened, Primary: true}}}
	if !o.Virtual {
		iban, err := vocab.NewIBAN(s.Number)
		if err != nil {
			var v fw.Validation
			v.Add("number", "iban", "the number of an account that is not virtual is an IBAN: "+err.Error())
			return nil, v.Err()
		}
		s.IBAN, s.Number = iban, iban.String()
	}
	for _, code := range o.Uses {
		s.Uses = append(s.Uses, Use{Code: code, From: o.Opened})
	}
	a, err := ReconstituteAccount(id, s)
	if err != nil {
		return nil, err
	}
	a.Raise(AccountOpened{EventMeta: a.NewEventMeta(), Company: s.Company.String(), Number: a.s.Number, Currency: s.Currency.String(), Holder: o.Holder.String(),
		Virtual: s.Virtual, Demo: s.Demo, Opened: s.Opened})
	return a, nil
}

// State returns the state (slices are copies).
func (a *Account) State() AccountState {
	s := a.s
	s.Holders, s.Uses = slices.Clone(s.Holders), slices.Clone(s.Uses)
	return s
}

// PrimaryHolder returns the holder the account is filed under (zero for a closed account that
// ended them all).
func (a *Account) PrimaryHolder() PartyID {
	for _, h := range a.s.Holders {
		if h.Primary {
			return h.Party
		}
	}
	return PartyID{}
}

// try applies a change if the account stays valid.
func (a *Account) try(change func(*AccountState)) error {
	s := a.State()
	change(&s)
	n, err := ReconstituteAccount(a.ID(), s)
	if err != nil {
		return err
	}
	a.s = n.s
	return nil
}

func (a *Account) open() error {
	if a.s.Status == Closed {
		return fw.Violation("financial.closed", "a closed account does not change")
	}
	return nil
}

// Describe changes the name, the BIC and the product of an account.
func (a *Account) Describe(name, bic string, product fw.UUID) error {
	if err := a.open(); err != nil {
		return err
	}
	return a.try(func(s *AccountState) { s.Name, s.BIC, s.Product = name, bic, product })
}

// Relate gives a party a role on the account from a day. As primary, the account is filed under
// it from then on.
func (a *Account) Relate(party PartyID, role string, from vocab.Date, primary bool) error {
	if err := a.open(); err != nil {
		return err
	}
	for _, h := range a.s.Holders {
		if h.Current() && h.Party == party && h.Role == role {
			return fw.Violation("financial.already_related", "the party already has that role on the account")
		}
	}
	return a.try(func(s *AccountState) {
		if primary {
			for i := range s.Holders {
				s.Holders[i].Primary = false
			}
		}
		s.Holders = append(s.Holders, Holder{Party: party, Role: role, From: from, Primary: primary})
	})
}

// Unrelate ends the role of a party on a day. The last holder cannot leave, and whoever the
// account is filed under leaves only after another takes its place.
func (a *Account) Unrelate(party PartyID, role string, on vocab.Date) error {
	if err := a.open(); err != nil {
		return err
	}
	k := slices.IndexFunc(a.s.Holders, func(h Holder) bool { return h.Current() && h.Party == party && h.Role == role })
	if k < 0 {
		return fw.Violation("financial.not_related", "the party does not have that role on the account")
	}
	if a.s.Holders[k].Primary {
		return fw.Violation("financial.primary_holder", "the account is filed under this holder: file it under another first")
	}
	return a.try(func(s *AccountState) { s.Holders[k].Thru = on })
}

// FileUnder files the account under another of its holders.
func (a *Account) FileUnder(party PartyID) error {
	if err := a.open(); err != nil {
		return err
	}
	k := slices.IndexFunc(a.s.Holders, func(h Holder) bool { return h.Current() && h.Party == party && h.Role == RoleHolder })
	if k < 0 {
		return fw.Violation("financial.not_related", "the party does not hold the account")
	}
	return a.try(func(s *AccountState) {
		for i := range s.Holders {
			s.Holders[i].Primary = i == k
		}
	})
}

// Assign gives the account a use from a day.
func (a *Account) Assign(code string, from vocab.Date) error {
	if err := a.open(); err != nil {
		return err
	}
	if a.Used(code) {
		return fw.Violation("financial.already_used", "the account already has that use")
	}
	return a.try(func(s *AccountState) { s.Uses = append(s.Uses, Use{Code: code, From: from}) })
}

// Withdraw ends a use of the account on a day.
func (a *Account) Withdraw(code string, on vocab.Date) error {
	if err := a.open(); err != nil {
		return err
	}
	k := slices.IndexFunc(a.s.Uses, func(u Use) bool { return u.Code == code && u.Thru.IsZero() })
	if k < 0 {
		return fw.Violation("financial.not_used", "the account does not have that use")
	}
	return a.try(func(s *AccountState) { s.Uses[k].Thru = on })
}

// Used reports whether the account has a use now.
func (a *Account) Used(code string) bool {
	return slices.ContainsFunc(a.s.Uses, func(u Use) bool { return u.Code == code && u.Thru.IsZero() })
}

func (a *Account) become(to Status, reason string, closed vocab.Date) error {
	from := a.s.Status
	if err := a.try(func(s *AccountState) { s.Status, s.Reason, s.Closed = to, reason, closed }); err != nil {
		return err
	}
	a.Raise(AccountStatusChanged{EventMeta: a.NewEventMeta(), Company: a.s.Company.String(), Number: a.s.Number, Holder: a.PrimaryHolder().String(),
		From: string(from), To: string(to), Reason: a.s.Reason})
	return nil
}

func reasoned(reason string) error {
	if strings.TrimSpace(reason) == "" {
		return fw.Violation("financial.reason", "say why")
	}
	return nil
}

// Block stops an active account from operating, saying why.
func (a *Account) Block(reason string) error {
	if a.s.Status != Active {
		return fw.Violation("financial.transition", "only an active account is blocked; this one is "+string(a.s.Status))
	}
	if err := reasoned(reason); err != nil {
		return err
	}
	return a.become(Blocked, reason, vocab.Date{})
}

// Abandon sets apart an account its holder stopped using, saying why.
func (a *Account) Abandon(reason string) error {
	if a.s.Status != Active && a.s.Status != Blocked {
		return fw.Violation("financial.transition", "an account "+string(a.s.Status)+" is not set apart as abandoned")
	}
	if err := reasoned(reason); err != nil {
		return err
	}
	return a.become(Abandoned, reason, vocab.Date{})
}

// Release lets a blocked or abandoned account operate again.
func (a *Account) Release() error {
	if a.s.Status != Blocked && a.s.Status != Abandoned {
		return fw.Violation("financial.transition", "only a blocked or abandoned account is released; this one is "+string(a.s.Status))
	}
	return a.become(Active, "", vocab.Date{})
}

// Close ends the account on a day: its holders and its uses end with it, and it changes no more.
func (a *Account) Close(on vocab.Date, reason string) error {
	if err := a.open(); err != nil {
		return err
	}
	if on.Before(a.s.Opened) {
		return fw.Violation("financial.closing_date", "an account is not closed before it was opened")
	}
	holder := a.PrimaryHolder()
	from := a.s.Status
	if err := a.try(func(s *AccountState) {
		s.Status, s.Reason, s.Closed = Closed, reason, on
		for i := range s.Holders {
			if s.Holders[i].Current() {
				s.Holders[i].Thru, s.Holders[i].Primary = later(on, s.Holders[i].From), false
			}
		}
		for i := range s.Uses {
			if s.Uses[i].Thru.IsZero() {
				s.Uses[i].Thru = later(on, s.Uses[i].From)
			}
		}
	}); err != nil {
		return err
	}
	a.Raise(AccountStatusChanged{EventMeta: a.NewEventMeta(), Company: a.s.Company.String(), Number: a.s.Number, Holder: holder.String(), From: string(from),
		To: string(Closed), Reason: a.s.Reason})
	return nil
}

func later(a, b vocab.Date) vocab.Date {
	if a.Before(b) {
		return b
	}
	return a
}

// AuditSnapshot implements traits.Snapshotter.
func (a *Account) AuditSnapshot() map[string]any {
	holders, uses := []string{}, []string{}
	for _, h := range a.s.Holders {
		if h.Current() {
			holders = append(holders, h.Role+":"+h.Party.String())
		}
	}
	for _, u := range a.s.Uses {
		if u.Thru.IsZero() {
			uses = append(uses, u.Code)
		}
	}
	slices.Sort(holders)
	slices.Sort(uses)
	return map[string]any{"number": a.s.Number, "status": string(a.s.Status), "reason": a.s.Reason, "name": a.s.Name, "holders": strings.Join(holders, ","),
		"uses": strings.Join(uses, ","), "primary": a.PrimaryHolder().String()}
}

// Account fields.
var (
	AccFieldCompany  = spec.Comparable("company", func(a *Account) OrganizationID { return a.s.Company })
	AccFieldNumber   = spec.Ordered("account_number", func(a *Account) string { return a.s.Number })
	AccFieldStatus   = spec.Comparable("status", func(a *Account) string { return string(a.s.Status) })
	AccFieldCurrency = spec.Comparable("currency", func(a *Account) string { return a.s.Currency.String() })
	AccFieldDemo     = spec.Comparable("demo", func(a *Account) bool { return a.s.Demo })
	AccFieldVirtual  = spec.Comparable("virtual_account", func(a *Account) bool { return a.s.Virtual })
	AccFieldName     = spec.Text("account_name", func(a *Account) string { return a.s.Name })
	AccFieldHolders  = spec.Collection("holders", func(a *Account) []Holder { return a.s.Holders })
	HolFieldParty    = spec.Comparable("party", func(h Holder) PartyID { return h.Party })
	HolFieldCurrent  = spec.Comparable("current_holder", func(h Holder) bool { return h.Current() })
	AccFieldUses     = spec.Collection("uses", func(a *Account) []Use { return a.s.Uses })
	UseFieldCode     = spec.Comparable("use_code", func(u Use) string { return u.Code })
	UseFieldCurrent  = spec.Comparable("current_use", func(u Use) bool { return u.Thru.IsZero() })
)

// AccountRepository stores accounts.
type AccountRepository = fw.Repository[AccountID, *Account]

// Events of an account.
type (
	// AccountOpened is raised when an account is opened.
	AccountOpened struct {
		fw.EventMeta
		Company  string     `json:"company"`
		Number   string     `json:"number"`
		Currency string     `json:"currency"`
		Holder   string     `json:"holder"`
		Virtual  bool       `json:"virtual"`
		Demo     bool       `json:"demo"`
		Opened   vocab.Date `json:"opened"`
	}
	// AccountStatusChanged is raised when an account is blocked, set apart, released or closed.
	AccountStatusChanged struct {
		fw.EventMeta
		Company string `json:"company"`
		Number  string `json:"number"`
		Holder  string `json:"holder"`
		From    string `json:"from"`
		To      string `json:"to"`
		Reason  string `json:"reason"`
	}
)

// EventType implementations.
func (AccountOpened) EventType() string        { return "financial.account_opened" }
func (AccountStatusChanged) EventType() string { return "financial.account_status_changed" }
