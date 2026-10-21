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

// PayslipKind es el nombre estable del tipo de agregado.
const PayslipKind = "payroll.payslip"

// MaxLines limita las líneas de una nómina.
const MaxLines = 200

var (
	hundred = vocab.DecimalFromInt(100)
	zero    = vocab.DecimalFromInt(0)
)

// places dice si d tiene como mucho n decimales, los de su columna (pay_payslip_lines: cantidad,
// importe unitario y base (18,4), porcentaje (5,2); pay_profile_splits: porcentaje (5,2)).
func places(d vocab.Decimal, n int32) bool { return d.Equal(d.Round(n)) }

// Kind es la clase de nómina: una nómina ordinaria por periodo, y las pagas extra o un finiquito
// aparte.
type Kind int

// Clases de nómina.
const (
	Ordinary Kind = iota + 1
	ExtraPay
	Settlement
)

var payslipKinds = map[Kind]string{Ordinary: "ordinary", ExtraPay: "extra-pay", Settlement: "settlement"}

// String devuelve el nombre estable.
func (k Kind) String() string { return payslipKinds[k] }

// ParseKind interpreta el nombre de una clase de nómina.
func ParseKind(s string) (Kind, bool) {
	for k, n := range payslipKinds {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// Status es el estado de una nómina (el C# usaba los enteros mágicos 0 y 2, y dejaba cambiar o
// borrar las nóminas emitidas).
type Status int

// Estados: un borrador cambia libremente y se puede descartar; una nómina aprobada es inmutable y
// se corrige anulándola y preparando una nueva.
const (
	Draft Status = iota + 1
	Approved
	Cancelled
)

var statuses = map[Status]string{Draft: "draft", Approved: "approved", Cancelled: "cancelled"}

// String devuelve el nombre estable.
func (s Status) String() string { return statuses[s] }

// ParseStatus interpreta el nombre de un estado.
func ParseStatus(s string) (Status, bool) {
	for k, n := range statuses {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// LineID identifica una línea de nómina.
type LineID struct{ fw.UUID }

// Line es una línea de una nómina. Guarda una instantánea de su concepto (clase, bases, clave) para
// que los cambios posteriores del catálogo no alteren una nómina aprobada.
type Line struct {
	ID            LineID
	Concept       ConceptID
	Code          string
	Kind          ConceptKind
	Contributable bool
	Taxable       bool
	PerceptionKey string
	Description   string
	Quantity      vocab.Decimal // con UnitAmount: importe = cantidad × unidad
	UnitAmount    vocab.Decimal
	Base          vocab.Decimal // con Percent: importe = base × porcentaje / 100
	Percent       vocab.Decimal
	Amount        vocab.Decimal
}

// LineInput es una línea nueva: un importe, una cantidad y un importe unitario, o una base y un
// porcentaje. El importe de las dos últimas se calcula, redondeado a céntimos con la mitad
// alejándose de cero (el C# sólo comprobaba Amount > 0 y nunca Base × %).
type LineInput struct {
	Description string
	Amount      vocab.Decimal
	Quantity    vocab.Decimal
	UnitAmount  vocab.Decimal
	Base        vocab.Decimal
	Percent     vocab.Decimal
}

// NewLine construye una línea de un concepto.
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
		v.Require(in.Quantity.IsPositive() && in.UnitAmount.IsPositive() && places(in.Quantity, 4) && places(in.UnitAmount, 4), "quantity", "range",
			"quantity and unit amount must be positive, with up to 4 decimals")
		v.Require(in.Amount.IsZero(), "amount", "mode", "the amount is computed from quantity × unit")
		amount = in.Quantity.Mul(in.UnitAmount).Round(2)
	case byRate:
		v.Require(in.Base.IsPositive() && places(in.Base, 4), "base", "range", "the base must be positive, with up to 4 decimals")
		v.Require(in.Percent.IsPositive() && in.Percent.LessThanOrEqual(hundred) && places(in.Percent, 2), "percent", "range",
			"a percentage over 0 and up to 100 with two decimals")
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

// Totals se derivan de las líneas (el C# se fiaba del bruto y el líquido que enviaba el cliente).
type Totals struct {
	Gross            vocab.Decimal // Σ devengos
	ContributionBase vocab.Decimal // Σ devengos cotizables
	TaxableBase      vocab.Decimal // Σ devengos sujetos a retención
	SocialSecurity   vocab.Decimal // Σ cotizaciones del trabajador
	IncomeTax        vocab.Decimal // Σ retenciones de IRPF
	OtherDeductions  vocab.Decimal // Σ otras deducciones
	Net              vocab.Decimal // bruto − deducciones
	EmployerCost     vocab.Decimal // Σ cotizaciones de la empresa
	CompanyCost      vocab.Decimal // bruto + cotizaciones de la empresa
}

// TotalsOf suma líneas.
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

// Snapshot es lo que la nómina guarda de HR y del perfil de nómina al prepararse.
type Snapshot struct {
	EmployeeNumber    string
	Agreement         AgreementID
	WorkCenter        WorkCenterID
	ContributionGroup int
	IncomeTaxRate     vocab.Decimal
	EmployerAccount   EmployerAccountID
}

// Payslip es el recibo de salarios de una relación laboral para un periodo, con sus líneas.
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

// PayslipState es el estado persistido de una nómina.
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

// ReconstitutePayslip reconstruye una nómina.
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

// DraftPayslip crea una nómina en borrador. La aplicación comprueba que la relación laboral está
// activa en el periodo y que no existe otra nómina de esa clase para ella en el periodo.
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

// Employment devuelve la relación laboral de HR.
func (p *Payslip) Employment() EmploymentID { return p.employment }

// Person devuelve el empleado.
func (p *Payslip) Person() PersonID { return p.person }

// Employer devuelve el empleador.
func (p *Payslip) Employer() OrganizationID { return p.employer }

// Kind devuelve la clase de nómina.
func (p *Payslip) Kind() Kind { return p.kind }

// Period devuelve el periodo (fechas civiles, las dos incluidas).
func (p *Payslip) Period() (vocab.Date, vocab.Date) { return p.start, p.end }

// PaymentDate devuelve la fecha de pago.
func (p *Payslip) PaymentDate() vocab.Date { return p.paymentDate }

// Status devuelve el estado.
func (p *Payslip) Status() Status { return p.status }

// Snapshot devuelve lo que la nómina guarda de HR y del perfil.
func (p *Payslip) Snapshot() Snapshot { return p.snapshot }

// Lines devuelve una copia de las líneas.
func (p *Payslip) Lines() []Line { return slices.Clone(p.lines) }

// Totals devuelve los totales derivados de las líneas.
func (p *Payslip) Totals() Totals { return TotalsOf(p.lines) }

// CancelReason devuelve por qué se anuló una nómina aprobada.
func (p *Payslip) CancelReason() string { return p.cancelReason }

func (p *Payslip) mustBeDraft() error {
	if p.status != Draft {
		return fw.Violation("payroll.payslip_not_draft", "only a draft payslip changes")
	}
	return nil
}

// AddLine añade una línea (sólo en borradores).
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

// RemoveLine quita una línea (sólo en borradores).
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

// Reschedule cambia la fecha de pago (sólo en borradores).
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

// Approve congela la nómina. Invariantes: al menos un devengo, el líquido no es negativo, la
// retención no supera su base y las cotizaciones del trabajador no superan el bruto.
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

// Cancel anula una nómina aprobada; la corrige un borrador nuevo.
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

// Discard comprueba que una nómina se puede borrar: sólo los borradores (el C# borraba físicamente
// las emitidas).
func (p *Payslip) Discard() error { return p.mustBeDraft() }

// AuditSnapshot implementa traits.Snapshotter.
func (p *Payslip) AuditSnapshot() map[string]any {
	t := p.Totals()
	return map[string]any{"status": p.status.String(), "lines": len(p.lines), "gross": t.Gross.String(), "net": t.Net.String(),
		"paymentDate": p.paymentDate.String()}
}

// Campos y especificaciones de la nómina.
var (
	PayFieldID         = spec.Comparable("id", func(p *Payslip) PayslipID { return p.ID() })
	PayFieldEmployment = spec.Comparable("employment", (*Payslip).Employment)
	PayFieldPerson     = spec.Comparable("person", (*Payslip).Person)
	PayFieldEmployer   = spec.Comparable("employer", (*Payslip).Employer)
	PayFieldKind       = spec.Comparable("kind", func(p *Payslip) int { return int(p.kind) })
	PayFieldStatus     = spec.Comparable("status", func(p *Payslip) int { return int(p.status) })
	PayFieldStart      = spec.OrderedBy("period_start", func(p *Payslip) vocab.Date { return p.start }, vocab.CompareDates)
)

// SamePeriod selecciona las nóminas no anuladas de una relación laboral, clase e inicio de periodo.
func SamePeriod(e EmploymentID, k Kind, start vocab.Date) spec.Spec[*Payslip] {
	return spec.And(PayFieldEmployment.Eq(e), PayFieldKind.Eq(int(k)), PayFieldStart.Eq(start), PayFieldStatus.Ne(int(Cancelled)))
}
