package domain

import (
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// PayslipKind is the stable aggregate type name.
const PayslipKind = "payroll.payslip"

// MaxLines bounds the lines of a payslip.
const MaxLines = 200

var (
	hundred = vocab.DecimalFromInt(100)
	zero    = vocab.DecimalFromInt(0)
)

// Kind of payslip: one ordinary payslip per period, and extra pays or a settlement (finiquito)
// apart.
type Kind int

// Payslip kinds.
const (
	Ordinary Kind = iota + 1
	ExtraPay
	Settlement
)

var payslipKinds = map[Kind]string{Ordinary: "ordinary", ExtraPay: "extra-pay", Settlement: "settlement"}

// String returns the stable name.
func (k Kind) String() string { return payslipKinds[k] }

// ParseKind parses a payslip kind name.
func ParseKind(s string) (Kind, bool) {
	for k, n := range payslipKinds {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// Status of a payslip (the C# used magic ints 0 and 2, and let issued payslips change or be
// deleted).
type Status int

// Statuses: a draft changes freely and can be discarded; an approved payslip is immutable and
// is corrected by cancelling it and drafting a new one.
const (
	Draft Status = iota + 1
	Approved
	Cancelled
)

var statuses = map[Status]string{Draft: "draft", Approved: "approved", Cancelled: "cancelled"}

// String returns the stable name.
func (s Status) String() string { return statuses[s] }

// ParseStatus parses a status name.
func ParseStatus(s string) (Status, bool) {
	for k, n := range statuses {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// LineID identifies a payslip line.
type LineID struct{ fw.UUID }

// Line is a line of a payslip. It keeps a snapshot of its concept (kind, bases, key) so that later
// catalog changes do not alter an approved payslip.
type Line struct {
	ID            LineID
	Concept       ConceptID
	Code          string
	Kind          ConceptKind
	Contributable bool
	Taxable       bool
	PerceptionKey string
	Description   string
	Quantity      vocab.Decimal // with UnitAmount: amount = quantity × unit
	UnitAmount    vocab.Decimal
	Base          vocab.Decimal // with Percent: amount = base × percent / 100
	Percent       vocab.Decimal
	Amount        vocab.Decimal
}

// LineInput is a new line: an amount, a quantity and a unit amount, or a base and a percentage.
// The amount of the last two is computed, rounded to cents half away from zero (the C# only
// checked Amount > 0 and never Base × %).
type LineInput struct {
	Description string
	Amount      vocab.Decimal
	Quantity    vocab.Decimal
	UnitAmount  vocab.Decimal
	Base        vocab.Decimal
	Percent     vocab.Decimal
}

// NewLine builds a line of a concept.
func NewLine(c Concept, in LineInput) (Line, error) {
	var v fw.Validation
	v.Require(c.Active, "concept", "inactive", "the concept is not active")
	desc := strings.TrimSpace(in.Description)
	if desc == "" {
		desc = c.Name
	}
	v.Require(utf8.RuneCountInString(desc) <= 200, "description", "length", "at most 200 characters")
	byUnits := !in.Quantity.IsZero() || !in.UnitAmount.IsZero()
	byRate := !in.Base.IsZero() || !in.Percent.IsZero()
	amount := in.Amount
	switch {
	case byUnits && byRate:
		v.Add("amount", "mode", "a line is an amount, quantity × unit or base × percent")
	case byUnits:
		v.Require(in.Quantity.IsPositive() && in.UnitAmount.IsPositive(), "quantity", "range", "quantity and unit amount must be positive")
		v.Require(in.Amount.IsZero(), "amount", "mode", "the amount is computed from quantity × unit")
		amount = in.Quantity.Mul(in.UnitAmount).Round(2)
	case byRate:
		v.Require(in.Base.IsPositive(), "base", "range", "the base must be positive")
		v.Require(in.Percent.IsPositive() && in.Percent.LessThanOrEqual(hundred), "percent", "range", "a percentage over 0 and up to 100")
		v.Require(in.Amount.IsZero(), "amount", "mode", "the amount is computed from base × percent")
		amount = in.Base.Mul(in.Percent).Div(hundred).Round(2)
	default:
		v.Require(!in.Amount.IsNegative() && in.Amount.Equal(in.Amount.Round(2)), "amount", "range", "a non-negative amount in cents")
	}
	if c.Kind != KindInformation {
		v.Require(amount.IsPositive(), "amount", "range", "the amount must be positive")
	}
	if err := v.Err(); err != nil {
		return Line{}, err
	}
	return Line{ID: LineID{fw.NewUUID()}, Concept: c.ID, Code: c.Code, Kind: c.Kind, Contributable: c.Contributable, Taxable: c.Taxable,
		PerceptionKey: c.PerceptionKey, Description: desc, Quantity: in.Quantity, UnitAmount: in.UnitAmount, Base: in.Base,
		Percent: in.Percent, Amount: amount}, nil
}

// Totals are derived from the lines (the C# trusted the gross and net sent by the client).
type Totals struct {
	Gross            vocab.Decimal // Σ earnings
	ContributionBase vocab.Decimal // Σ contributable earnings
	TaxableBase      vocab.Decimal // Σ taxable earnings
	SocialSecurity   vocab.Decimal // Σ employee contributions
	IncomeTax        vocab.Decimal // Σ IRPF withholdings
	OtherDeductions  vocab.Decimal // Σ other deductions
	Net              vocab.Decimal // gross − deductions
	EmployerCost     vocab.Decimal // Σ employer contributions
	CompanyCost      vocab.Decimal // gross + employer contributions
}

// TotalsOf adds up lines.
func TotalsOf(lines []Line) Totals {
	t := Totals{Gross: zero, ContributionBase: zero, TaxableBase: zero, SocialSecurity: zero, IncomeTax: zero, OtherDeductions: zero,
		EmployerCost: zero}
	for _, l := range lines {
		switch l.Kind {
		case KindEarning:
			t.Gross = t.Gross.Add(l.Amount)
			if l.Contributable {
				t.ContributionBase = t.ContributionBase.Add(l.Amount)
			}
			if l.Taxable {
				t.TaxableBase = t.TaxableBase.Add(l.Amount)
			}
		case KindSocialSecurity:
			t.SocialSecurity = t.SocialSecurity.Add(l.Amount)
		case KindIncomeTax:
			t.IncomeTax = t.IncomeTax.Add(l.Amount)
		case KindDeduction:
			t.OtherDeductions = t.OtherDeductions.Add(l.Amount)
		case KindEmployerCost:
			t.EmployerCost = t.EmployerCost.Add(l.Amount)
		}
	}
	t.Net = t.Gross.Sub(t.SocialSecurity).Sub(t.IncomeTax).Sub(t.OtherDeductions)
	t.CompanyCost = t.Gross.Add(t.EmployerCost)
	return t
}

// Snapshot is what the payslip keeps of HR and of the payroll profile when it is drafted.
type Snapshot struct {
	EmployeeNumber    string
	Agreement         AgreementID
	WorkCenter        WorkCenterID
	ContributionGroup int
	IncomeTaxRate     vocab.Decimal
	EmployerAccount   EmployerAccountID
}

// Payslip is the pay document of an employment for a period, with its lines.
type Payslip struct {
	fw.BaseAggregateRoot[PayslipID]
	traits.Audited
	employment   EmploymentID
	person       PersonID
	employer     OrganizationID
	kind         Kind
	start, end   vocab.Date
	paymentDate  vocab.Date
	status       Status
	snapshot     Snapshot
	lines        []Line
	cancelReason string
}

// PayslipState is the persisted state of a payslip.
type PayslipState struct {
	Employment   EmploymentID
	Person       PersonID
	Employer     OrganizationID
	Kind         Kind
	Start, End   vocab.Date
	PaymentDate  vocab.Date
	Status       Status
	Snapshot     Snapshot
	Lines        []Line
	CancelReason string
	Audit        traits.AuditStamp
}

// ReconstitutePayslip rebuilds a payslip.
func ReconstitutePayslip(id PayslipID, s PayslipState) (*Payslip, error) {
	base, err := fw.NewBaseAggregateRoot(PayslipKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Employment.IsZero() && !s.Person.IsZero() && !s.Employer.IsZero(), "employment", "required",
		"a payslip belongs to an employment of a person with an employer")
	_, kind := payslipKinds[s.Kind]
	v.Require(kind, "kind", "enum", "unknown payslip kind")
	_, status := statuses[s.Status]
	v.Require(status, "status", "enum", "unknown status")
	v.Require(!s.Start.IsZero() && !s.End.IsZero() && !s.End.Before(s.Start), "period", "order", "a period whose end does not precede its start")
	v.Require(s.Start.IsZero() || !s.End.After(s.Start.AddMonths(1).AddDays(-1)), "period", "length", "a period of at most one month")
	v.Require(!s.PaymentDate.IsZero(), "paymentDate", "required", "the payment date is required")
	v.Require(len(s.Lines) <= MaxLines, "lines", "count", "too many lines")
	v.Require(s.Snapshot.ContributionGroup >= 0 && s.Snapshot.ContributionGroup <= 11, "contributionGroup", "range", "1 to 11")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Payslip{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), employment: s.Employment, person: s.Person,
		employer: s.Employer, kind: s.Kind, start: s.Start, end: s.End, paymentDate: s.PaymentDate, status: s.Status, snapshot: s.Snapshot,
		lines: slices.Clone(s.Lines), cancelReason: s.CancelReason}, nil
}

// DraftPayslip creates a draft payslip. The application checks the employment is active in the
// period and that no other payslip of the kind exists for it.
func DraftPayslip(id PayslipID, s PayslipState) (*Payslip, error) {
	s.Status, s.Lines, s.CancelReason = Draft, nil, ""
	p, err := ReconstitutePayslip(id, s)
	if err != nil {
		return nil, err
	}
	p.Raise(PayslipDrafted{EventMeta: p.NewEventMeta(), Employment: s.Employment.String(), Employer: s.Employer.String(),
		Kind: s.Kind.String(), Start: s.Start.String(), End: s.End.String()})
	return p, nil
}

// Employment returns the HR employment.
func (p *Payslip) Employment() EmploymentID { return p.employment }

// Person returns the employee.
func (p *Payslip) Person() PersonID { return p.person }

// Employer returns the employer.
func (p *Payslip) Employer() OrganizationID { return p.employer }

// Kind returns the payslip kind.
func (p *Payslip) Kind() Kind { return p.kind }

// Period returns the period (civil dates, both included).
func (p *Payslip) Period() (vocab.Date, vocab.Date) { return p.start, p.end }

// PaymentDate returns the payment date.
func (p *Payslip) PaymentDate() vocab.Date { return p.paymentDate }

// Status returns the status.
func (p *Payslip) Status() Status { return p.status }

// Snapshot returns what the payslip keeps of HR and the profile.
func (p *Payslip) Snapshot() Snapshot { return p.snapshot }

// Lines returns a copy of the lines.
func (p *Payslip) Lines() []Line { return slices.Clone(p.lines) }

// Totals returns the totals derived from the lines.
func (p *Payslip) Totals() Totals { return TotalsOf(p.lines) }

// CancelReason returns why an approved payslip was cancelled.
func (p *Payslip) CancelReason() string { return p.cancelReason }

func (p *Payslip) mustBeDraft() error {
	if p.status != Draft {
		return fw.Violation("payroll.payslip_not_draft", "only a draft payslip changes")
	}
	return nil
}

// AddLine adds a line (drafts only).
func (p *Payslip) AddLine(l Line) (LineID, error) {
	if err := p.mustBeDraft(); err != nil {
		return LineID{}, err
	}
	if len(p.lines) >= MaxLines {
		return LineID{}, fw.Violation("payroll.too_many_lines", "the payslip has too many lines")
	}
	p.lines = append(slices.Clone(p.lines), l)
	return l.ID, nil
}

// RemoveLine removes a line (drafts only).
func (p *Payslip) RemoveLine(id LineID) error {
	if err := p.mustBeDraft(); err != nil {
		return err
	}
	i := slices.IndexFunc(p.lines, func(l Line) bool { return l.ID == id })
	if i < 0 {
		return fw.NotFound("payroll.payslip_line", id)
	}
	p.lines = slices.Delete(slices.Clone(p.lines), i, i+1)
	return nil
}

// Reschedule changes the payment date (drafts only).
func (p *Payslip) Reschedule(d vocab.Date) error {
	if err := p.mustBeDraft(); err != nil {
		return err
	}
	if d.IsZero() {
		return fw.Violation("payroll.payment_date_required", "the payment date is required")
	}
	p.paymentDate = d
	return nil
}

// Approve freezes the payslip. Invariants: at least one earning, the net is not negative, the
// withholding does not exceed its base and the employee contributions do not exceed the gross.
func (p *Payslip) Approve() error {
	if err := p.mustBeDraft(); err != nil {
		return err
	}
	t := p.Totals()
	switch {
	case !t.Gross.IsPositive():
		return fw.Violation("payroll.no_earnings", "a payslip needs at least one earning")
	case t.Net.IsNegative():
		return fw.Violation("payroll.negative_net", "the deductions exceed the gross")
	case t.IncomeTax.GreaterThan(t.TaxableBase):
		return fw.Violation("payroll.withholding_over_base", "the withholding exceeds the taxable base")
	case t.SocialSecurity.GreaterThan(t.Gross):
		return fw.Violation("payroll.contributions_over_gross", "the contributions exceed the gross")
	}
	p.status = Approved
	e := PayslipApproved{EventMeta: p.NewEventMeta(), Employment: p.employment.String(), Person: p.person.String(),
		Employer: p.employer.String(), Kind: p.kind.String(), Start: p.start.String(), End: p.end.String(),
		PaymentDate: p.paymentDate.String(), Totals: t}
	if !p.snapshot.EmployerAccount.IsZero() {
		e.EmployerAccount = p.snapshot.EmployerAccount.String()
	}
	for _, l := range p.lines {
		if l.Kind == KindEarning && l.PerceptionKey != "" {
			e.PerceptionKey = l.PerceptionKey
			break
		}
	}
	p.Raise(e)
	return nil
}

// Cancel annuls an approved payslip; a new draft corrects it.
func (p *Payslip) Cancel(reason string) error {
	if p.status != Approved {
		return fw.Violation("payroll.payslip_not_approved", "only an approved payslip is cancelled; a draft is discarded")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > 200 {
		var v fw.Validation
		v.Add("reason", "length", "a reason of 1 to 200 characters is required")
		return v.Err()
	}
	p.status, p.cancelReason = Cancelled, reason
	p.Raise(PayslipCancelled{EventMeta: p.NewEventMeta(), Reason: reason})
	return nil
}

// Discard checks a payslip can be deleted: only drafts are (the C# hard-deleted issued ones).
func (p *Payslip) Discard() error { return p.mustBeDraft() }

// AuditSnapshot implements traits.Snapshotter.
func (p *Payslip) AuditSnapshot() map[string]any {
	t := p.Totals()
	return map[string]any{"status": p.status.String(), "lines": len(p.lines), "gross": t.Gross.String(), "net": t.Net.String(),
		"paymentDate": p.paymentDate.String()}
}

// Payslip fields and specifications.
var (
	PayFieldID         = spec.Comparable("id", func(p *Payslip) PayslipID { return p.ID() })
	PayFieldEmployment = spec.Comparable("employment", (*Payslip).Employment)
	PayFieldPerson     = spec.Comparable("person", (*Payslip).Person)
	PayFieldEmployer   = spec.Comparable("employer", (*Payslip).Employer)
	PayFieldKind       = spec.Comparable("kind", func(p *Payslip) int { return int(p.kind) })
	PayFieldStatus     = spec.Comparable("status", func(p *Payslip) int { return int(p.status) })
	PayFieldStart      = spec.OrderedBy("period_start", func(p *Payslip) vocab.Date { return p.start }, vocab.CompareDates)
)

// SamePeriod matches the payslips of an employment, kind and period start that are not cancelled.
func SamePeriod(e EmploymentID, k Kind, start vocab.Date) spec.Spec[*Payslip] {
	return spec.And(PayFieldEmployment.Eq(e), PayFieldKind.Eq(int(k)), PayFieldStart.Eq(start), PayFieldStatus.Ne(int(Cancelled)))
}
