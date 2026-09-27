package domain

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Period is a filing period in AEAT notation: months 01–12, quarters 1T–4T, the year 0A.
type Period struct {
	Periodicity Periodicity
	Index       int // month 1–12, quarter 1–4, 0 for the year
}

// ParsePeriod parses an AEAT period code.
func ParsePeriod(s string) (Period, bool) {
	s = strings.ToUpper(strings.TrimSpace(s))
	switch {
	case s == "0A":
		return Period{Periodicity: Annual}, true
	case len(s) == 2 && s[1] == 'T' && s[0] >= '1' && s[0] <= '4':
		return Period{Periodicity: Quarterly, Index: int(s[0] - '0')}, true
	case len(s) == 2:
		if m, err := strconv.Atoi(s); err == nil && m >= 1 && m <= 12 {
			return Period{Periodicity: Monthly, Index: m}, true
		}
	}
	return Period{}, false
}

// String returns the AEAT code.
func (p Period) String() string {
	switch p.Periodicity {
	case Monthly:
		return fmt.Sprintf("%02d", p.Index)
	case Quarterly:
		return fmt.Sprintf("%dT", p.Index)
	}
	return "0A"
}

// Bounds returns the first and last day of the period in a calendar year.
func (p Period) Bounds(year int) (vocab.Date, vocab.Date) {
	switch p.Periodicity {
	case Monthly:
		start := vocab.MustDate(year, time.Month(p.Index), 1)
		return start, start.AddMonths(1).AddDays(-1)
	case Quarterly:
		start := vocab.MustDate(year, time.Month(3*p.Index-2), 1)
		return start, start.AddMonths(3).AddDays(-1)
	}
	return vocab.MustDate(year, 1, 1), vocab.MustDate(year, 12, 31)
}

// WithholdingKind is the stable aggregate type name.
const WithholdingKind = "fiscal.withholding"

// Withholding is a payment with withholding reported by Payroll (the projection of
// payroll.payslip-approved.v1; its identity is the payslip's). The C# forms read the payroll
// tables directly and dated the payments by the start of the payslip period; here they are dated
// by the payment date.
type Withholding struct {
	fw.BaseAggregateRoot[WithholdingID]
	s WithholdingState
}

// WithholdingState is the persisted state of a withholding.
type WithholdingState struct {
	Payer       OrganizationID
	Recipient   PartyID
	PaymentDate vocab.Date
	Key         string        // Modelo 190 perception key
	Perceptions vocab.Decimal // withholding base
	Withheld    vocab.Decimal
	Cancelled   bool
}

// ReconstituteWithholding rebuilds a withholding.
func ReconstituteWithholding(id WithholdingID, s WithholdingState) (*Withholding, error) {
	base, err := fw.NewBaseAggregateRoot(WithholdingKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Payer.IsZero() && !s.Recipient.IsZero(), "payer", "required", "payer and recipient are required")
	v.Require(!s.PaymentDate.IsZero(), "paymentDate", "required", "the payment date is required")
	v.Require(len(s.Key) == 1, "key", "length", "a one-letter perception key")
	v.Require(!s.Perceptions.IsNegative() && !s.Withheld.IsNegative() && !s.Withheld.GreaterThan(s.Perceptions), "withheld", "range",
		"non-negative amounts, the withholding not above its base")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Withholding{BaseAggregateRoot: base, s: s}, nil
}

// State returns the state.
func (w *Withholding) State() WithholdingState { return w.s }

// Cancel marks the withholding as cancelled (its payslip was cancelled).
func (w *Withholding) Cancel() { w.s.Cancelled = true }

// Withholding fields.
var (
	WhFieldID        = spec.Comparable("id", func(w *Withholding) WithholdingID { return w.ID() })
	WhFieldPayer     = spec.Comparable("payer", func(w *Withholding) OrganizationID { return w.s.Payer })
	WhFieldPaid      = spec.OrderedBy("payment_date", func(w *Withholding) vocab.Date { return w.s.PaymentDate }, vocab.CompareDates)
	WhFieldCancelled = spec.Comparable("cancelled", func(w *Withholding) bool { return w.s.Cancelled })
)

// PaidBetween matches the withholdings of a payer paid in a period, not cancelled.
func PaidBetween(payer OrganizationID, from, to vocab.Date) spec.Spec[*Withholding] {
	return spec.And(WhFieldPayer.Eq(payer), WhFieldPaid.Ge(from), WhFieldPaid.Le(to), WhFieldCancelled.Eq(false))
}

// Identity is the tax identity of a declarant or a recipient.
type Identity struct {
	NIF      string
	Name     string
	Province string
}

// Recipient is a line of a filing: a recipient and a perception key with their amounts.
type Recipient struct {
	Party       PartyID
	Identity    Identity
	Key         string
	Payments    int
	Perceptions vocab.Decimal
	Withheld    vocab.Decimal
}

// Summarize groups withholdings by recipient and key, ordered by recipient (one line of the
// Modelo 190 each).
func Summarize(ws []*Withholding, identities map[PartyID]Identity) []Recipient {
	type k struct {
		p   PartyID
		key string
	}
	lines := map[k]*Recipient{}
	for _, w := range ws {
		s := w.State()
		if s.Cancelled {
			continue
		}
		r, ok := lines[k{s.Recipient, s.Key}]
		if !ok {
			r = &Recipient{Party: s.Recipient, Identity: identities[s.Recipient], Key: s.Key, Perceptions: vocab.DecimalFromInt(0),
				Withheld: vocab.DecimalFromInt(0)}
			lines[k{s.Recipient, s.Key}] = r
		}
		r.Payments++
		r.Perceptions = r.Perceptions.Add(s.Perceptions)
		r.Withheld = r.Withheld.Add(s.Withheld)
	}
	out := make([]Recipient, 0, len(lines))
	for _, r := range lines {
		out = append(out, *r)
	}
	slices.SortFunc(out, func(a, b Recipient) int {
		if c := bytes.Compare(a.Party.Bytes(), b.Party.Bytes()); c != 0 {
			return c
		}
		return strings.Compare(a.Key, b.Key)
	})
	return out
}

// Totals of a filing.
type Totals struct {
	Recipients  int
	Perceptions vocab.Decimal
	Withheld    vocab.Decimal
}

// TotalsOf adds up the lines (recipients are counted once, whatever their keys).
func TotalsOf(rs []Recipient) Totals {
	t := Totals{Perceptions: vocab.DecimalFromInt(0), Withheld: vocab.DecimalFromInt(0)}
	seen := map[PartyID]bool{}
	for _, r := range rs {
		if !seen[r.Party] {
			seen[r.Party] = true
			t.Recipients++
		}
		t.Perceptions = t.Perceptions.Add(r.Perceptions)
		t.Withheld = t.Withheld.Add(r.Withheld)
	}
	return t
}

// FilingStatus is the status of a filing.
type FilingStatus int

// Statuses: a draft is regenerated freely and can be discarded; a submitted filing has its number
// and AEAT reference; a reverted one no longer counts and frees its period (the C# kept the slot
// blocked).
const (
	StatusDraft FilingStatus = iota + 1
	StatusSubmitted
	StatusReverted
)

var filingStatuses = map[FilingStatus]string{StatusDraft: "draft", StatusSubmitted: "submitted", StatusReverted: "reverted"}

// String returns the stable name.
func (s FilingStatus) String() string { return filingStatuses[s] }

// ParseFilingStatus parses a status name.
func ParseFilingStatus(s string) (FilingStatus, bool) { return parseEnum(filingStatuses, s) }

// FilingKind is the stable aggregate type name.
const FilingKind = "fiscal.filing"

// Generated lists the forms this phase generates.
var Generated = []Form{"111", "190"}

// Filing is a tax form of a declarant for a period (it unifies the C# OfficialFormSubmission and
// the Advisory TaxFiling, each with its own numbering).
type Filing struct {
	fw.BaseAggregateRoot[FilingID]
	traits.Audited
	s FilingState
}

// FilingState is the persisted state of a filing.
type FilingState struct {
	Declarant    OrganizationID
	Form         Form
	Year         int
	Period       Period
	Status       FilingStatus
	Identity     Identity
	Recipients   []Recipient
	Number       int64
	Reference    string
	SubmittedAt  time.Time
	RevertReason string
	Audit        traits.AuditStamp
}

// ReconstituteFiling rebuilds a filing.
func ReconstituteFiling(id FilingID, s FilingState) (*Filing, error) {
	base, err := fw.NewBaseAggregateRoot(FilingKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Declarant.IsZero(), "declarant", "required", "the declarant is required")
	allowed, known := Forms[s.Form]
	v.Require(known && slices.Contains(Generated, s.Form), "form", "unsupported", "only 111 and 190 are generated")
	v.Require(!known || slices.Contains(allowed, s.Period.Periodicity), "period", "form", "the form is not filed for that period")
	v.Require(s.Year >= 1990 && s.Year <= 9999, "year", "range", "a valid year")
	_, ok := filingStatuses[s.Status]
	v.Require(ok, "status", "enum", "unknown status")
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Recipients = slices.Clone(s.Recipients)
	return &Filing{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// DraftFiling generates a draft filing.
func DraftFiling(id FilingID, declarant OrganizationID, form Form, year int, period Period, identity Identity, rs []Recipient) (*Filing, error) {
	f, err := ReconstituteFiling(id, FilingState{Declarant: declarant, Form: form, Year: year, Period: period, Status: StatusDraft,
		Identity: identity, Recipients: rs})
	if err != nil {
		return nil, err
	}
	f.Raise(FilingDrafted{EventMeta: f.NewEventMeta(), Declarant: declarant.String(), Form: string(form), Year: year, Period: period.String()})
	return f, nil
}

// State returns the state (the recipients are a copy).
func (f *Filing) State() FilingState {
	s := f.s
	s.Recipients = slices.Clone(s.Recipients)
	return s
}

// Totals returns the totals of the lines.
func (f *Filing) Totals() Totals { return TotalsOf(f.s.Recipients) }

// Regenerate replaces the lines of a draft with fresh data.
func (f *Filing) Regenerate(identity Identity, rs []Recipient) error {
	if f.s.Status != StatusDraft {
		return fw.Violation("fiscal.filing_not_draft", "only a draft filing is regenerated")
	}
	f.s.Identity, f.s.Recipients = identity, slices.Clone(rs)
	return nil
}

// Problems lists what prevents the submission: tax numbers with wrong control characters or
// missing, and provinces missing in a Modelo 190 (the C# wrote "PENDIENTE-NIF" into the file).
func (f *Filing) Problems() []string {
	var out []string
	if ok, _ := vocab.ValidCheckDigit("ES_NIF", f.s.Identity.NIF); !ok {
		out = append(out, "declarant: invalid or missing NIF")
	}
	if f.s.Form == "190" {
		for _, r := range f.s.Recipients {
			if ok, _ := vocab.ValidCheckDigit("ES_NIF", r.Identity.NIF); !ok {
				out = append(out, "recipient "+r.Party.String()+": invalid or missing NIF")
			}
			if len(r.Identity.Province) != 2 {
				out = append(out, "recipient "+r.Party.String()+": missing province")
			}
		}
	}
	return out
}

// Submit records the submission with its number and the AEAT reference (CSV).
func (f *Filing) Submit(number int64, reference string, at time.Time) error {
	if f.s.Status != StatusDraft {
		return fw.Violation("fiscal.filing_not_draft", "only a draft filing is submitted")
	}
	if p := f.Problems(); len(p) > 0 {
		return fw.Violation("fiscal.filing_incomplete", strings.Join(p, "; "))
	}
	var v fw.Validation
	reference = text(&v, "reference", reference, 0, 64)
	v.Require(number > 0, "number", "range", "a positive number")
	if err := v.Err(); err != nil {
		return err
	}
	f.s.Status, f.s.Number, f.s.Reference, f.s.SubmittedAt = StatusSubmitted, number, reference, at.UTC()
	t := f.Totals()
	f.Raise(FilingSubmitted{EventMeta: f.NewEventMeta(), Declarant: f.s.Declarant.String(), Form: string(f.s.Form), Year: f.s.Year,
		Period: f.s.Period.String(), Number: number, Recipients: t.Recipients, Perceptions: t.Perceptions.StringFixed(2),
		Withheld: t.Withheld.StringFixed(2)})
	return nil
}

// Revert annuls a submitted filing (a complementary or substitutive one follows).
func (f *Filing) Revert(reason string) error {
	if f.s.Status != StatusSubmitted {
		return fw.Violation("fiscal.filing_not_submitted", "only a submitted filing is reverted; a draft is discarded")
	}
	var v fw.Validation
	reason = text(&v, "reason", reason, 1, 200)
	if err := v.Err(); err != nil {
		return err
	}
	f.s.Status, f.s.RevertReason = StatusReverted, reason
	f.Raise(FilingReverted{EventMeta: f.NewEventMeta(), Reason: reason})
	return nil
}

// Discard checks a filing can be deleted: only drafts are.
func (f *Filing) Discard() error {
	if f.s.Status != StatusDraft {
		return fw.Violation("fiscal.filing_not_draft", "only a draft filing is discarded")
	}
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (f *Filing) AuditSnapshot() map[string]any {
	t := f.Totals()
	return map[string]any{"status": f.s.Status.String(), "number": f.s.Number, "recipients": t.Recipients, "withheld": t.Withheld.String()}
}

// Filing fields.
var (
	FilFieldID        = spec.Comparable("id", func(f *Filing) FilingID { return f.ID() })
	FilFieldDeclarant = spec.Comparable("declarant", func(f *Filing) OrganizationID { return f.s.Declarant })
	FilFieldForm      = spec.Comparable("form", func(f *Filing) string { return string(f.s.Form) })
	FilFieldYear      = spec.Ordered("fiscal_year", func(f *Filing) int { return f.s.Year })
	FilFieldPeriod    = spec.Comparable("period", func(f *Filing) string { return f.s.Period.String() })
	FilFieldStatus    = spec.Comparable("status", func(f *Filing) int { return int(f.s.Status) })
)

// SameSlot matches the filings of a declarant, form, year and period that are not reverted.
func SameSlot(d OrganizationID, form Form, year int, p Period) spec.Spec[*Filing] {
	return spec.And(FilFieldDeclarant.Eq(d), FilFieldForm.Eq(string(form)), FilFieldYear.Eq(year), FilFieldPeriod.Eq(p.String()),
		FilFieldStatus.Ne(int(StatusReverted)))
}

// CounterKind is the stable aggregate type name.
const CounterKind = "fiscal.filing_counter"

// Counter numbers the submissions of a form by declarant and year. It is an aggregate so that
// optimistic concurrency protects it (the C# read, incremented and wrote without a lock).
type Counter struct {
	fw.BaseAggregateRoot[CounterID]
	declarant OrganizationID
	form      Form
	year      int
	last      int64
}

// ReconstituteCounter rebuilds a counter.
func ReconstituteCounter(id CounterID, declarant OrganizationID, form Form, year int, last int64) (*Counter, error) {
	base, err := fw.NewBaseAggregateRoot(CounterKind, id)
	if err != nil {
		return nil, err
	}
	if declarant.IsZero() || form == "" || year < 1990 || last < 0 {
		var v fw.Validation
		v.Add("counter", "invalid", "a counter needs a declarant, a form, a year and a non-negative number")
		return nil, v.Err()
	}
	return &Counter{BaseAggregateRoot: base, declarant: declarant, form: form, year: year, last: last}, nil
}

// Next returns the next number.
func (c *Counter) Next() int64 { c.last++; return c.last }

// Values returns the key and the last number.
func (c *Counter) Values() (OrganizationID, Form, int, int64) {
	return c.declarant, c.form, c.year, c.last
}

// Counter fields.
var (
	CntFieldDeclarant = spec.Comparable("declarant", func(c *Counter) OrganizationID { return c.declarant })
	CntFieldForm      = spec.Comparable("form", func(c *Counter) string { return string(c.form) })
	CntFieldYear      = spec.Comparable("fiscal_year", func(c *Counter) int { return c.year })
)

// Repositories and ports of the context.
type (
	TaxRateRepository     = fw.Repository[TaxRateID, *TaxRate]
	TreatmentRepository   = fw.Repository[TreatmentID, *Treatment]
	TaxpayerRepository    = fw.Repository[TaxpayerID, *Taxpayer]
	FilingRepository      = fw.Repository[FilingID, *Filing]
	CounterRepository     = fw.Repository[CounterID, *Counter]
	WithholdingRepository = fw.Repository[WithholdingID, *Withholding]
)

// Identities resolves the tax identities of parties (a port Fiscal owns; an adapter implements it
// over the Parties contracts).
type Identities interface {
	Identities(ctx context.Context, parties []PartyID) (map[PartyID]Identity, error)
}
