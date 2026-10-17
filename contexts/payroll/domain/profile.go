package domain

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// ProfileKind es el nombre estable del tipo de agregado.
const ProfileKind = "payroll.profile"

// Periodicity es la periodicidad del salario pactado (el BaseSalary de C# no tenía, y la
// importación de SAGE guardaba en él el bruto mensual).
type Periodicity int

// Periodicidades.
const (
	Monthly Periodicity = iota + 1
	Annual
)

var periodicities = map[Periodicity]string{Monthly: "monthly", Annual: "annual"}

// String devuelve el nombre estable.
func (p Periodicity) String() string { return periodicities[p] }

// ParsePeriodicity interpreta el nombre de una periodicidad.
func ParsePeriodicity(s string) (Periodicity, bool) {
	for k, n := range periodicities {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// Salary es el salario pactado: un importe, su periodicidad y las pagas al año (12, 14 o 15).
type Salary struct {
	Amount          vocab.Decimal
	Periodicity     Periodicity
	PaymentsPerYear int
}

// IsZero dice si no se pactó ningún salario.
func (s Salary) IsZero() bool { return s.Amount.IsZero() && s.Periodicity == 0 }

// Annual devuelve el importe anual pactado.
func (s Salary) Annual() vocab.Decimal {
	if s.Periodicity == Monthly {
		return s.Amount.Mul(vocab.DecimalFromInt(int64(s.PaymentsPerYear)))
	}
	return s.Amount
}

// SplitID identifica un reparto del pago.
type SplitID struct{ fw.UUID }

// Split envía parte del líquido a una cuenta bancaria: un porcentaje, un importe fijo o el residual
// (lo que queda). Primero van los embargos, luego por prioridad.
type Split struct {
	ID          SplitID
	IBAN        vocab.IBAN
	Percent     vocab.Decimal
	Amount      vocab.Decimal
	Residual    bool
	Priority    int
	Garnishment bool
	From        vocab.Date
	Until       vocab.Date // cero: abierto
}

func (s Split) activeOn(d vocab.Date) bool {
	return !d.Before(s.From) && (s.Until.IsZero() || !d.After(s.Until))
}

func (s Split) overlaps(o Split) bool {
	return (s.Until.IsZero() || !o.From.After(s.Until)) && (o.Until.IsZero() || !s.From.After(o.Until))
}

// Payment es una transferencia de parte del líquido.
type Payment struct {
	IBAN        vocab.IBAN
	Amount      vocab.Decimal
	Garnishment bool
}

// Profile son los datos de nómina de una relación laboral (la parte retributiva del EmployeeProfile
// de C# y las filas de EmployeePayrollAccount): grupo de cotización, tipo de retención, salario
// pactado, cuenta de cotización a la Seguridad Social del empleador y repartos del pago. Un perfil
// por relación laboral.
type Profile struct {
	fw.BaseAggregateRoot[ProfileID]
	traits.Audited
	employment        EmploymentID
	person            PersonID
	employer          OrganizationID
	contributionGroup int
	incomeTaxRate     vocab.Decimal
	salary            Salary
	account           EmployerAccountID
	splits            []Split
}

// ProfileState es el estado persistido de un perfil.
type ProfileState struct {
	Employment        EmploymentID
	Person            PersonID
	Employer          OrganizationID
	ContributionGroup int
	IncomeTaxRate     vocab.Decimal
	Salary            Salary
	Account           EmployerAccountID
	Splits            []Split
	Audit             traits.AuditStamp
}

// Terms son las condiciones editables de un perfil.
type Terms struct {
	ContributionGroup int
	IncomeTaxRate     vocab.Decimal
	Salary            Salary
	Account           EmployerAccountID
}

func (t Terms) validate(v *fw.Validation) {
	v.Require(t.ContributionGroup >= 1 && t.ContributionGroup <= 11, "contributionGroup", "range", "a contribution group from 1 to 11")
	v.Require(!t.IncomeTaxRate.IsNegative() && t.IncomeTaxRate.LessThanOrEqual(hundred) && t.IncomeTaxRate.Equal(t.IncomeTaxRate.Round(2)),
		"incomeTaxRate", "range", "a percentage from 0 to 100 with two decimals")
	if !t.Salary.IsZero() {
		_, ok := periodicities[t.Salary.Periodicity]
		v.Require(ok, "salary.periodicity", "enum", "monthly or annual")
		v.Require(t.Salary.Amount.IsPositive() && t.Salary.Amount.Equal(t.Salary.Amount.Round(2)), "salary.amount", "range", "a positive amount in cents")
		v.Require(slices.Contains([]int{12, 14, 15}, t.Salary.PaymentsPerYear), "salary.paymentsPerYear", "enum", "12, 14 or 15 payments")
	}
}

// ReconstituteProfile reconstruye un perfil.
func ReconstituteProfile(id ProfileID, s ProfileState) (*Profile, error) {
	base, err := fw.NewBaseAggregateRoot(ProfileKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Employment.IsZero() && !s.Person.IsZero() && !s.Employer.IsZero(), "employment", "required", "a profile belongs to an employment")
	Terms{ContributionGroup: s.ContributionGroup, IncomeTaxRate: s.IncomeTaxRate, Salary: s.Salary, Account: s.Account}.validate(&v)
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Profile{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), employment: s.Employment, person: s.Person,
		employer: s.Employer, contributionGroup: s.ContributionGroup, incomeTaxRate: s.IncomeTaxRate, salary: s.Salary, account: s.Account,
		splits: slices.Clone(s.Splits)}, nil
}

// OpenProfile crea el perfil de una relación laboral.
func OpenProfile(id ProfileID, s ProfileState) (*Profile, error) {
	s.Splits = nil
	p, err := ReconstituteProfile(id, s)
	if err != nil {
		return nil, err
	}
	p.Raise(ProfileChanged{EventMeta: p.NewEventMeta(), Employment: s.Employment.String()})
	return p, nil
}

// Employment devuelve la relación laboral de HR.
func (p *Profile) Employment() EmploymentID { return p.employment }

// Person devuelve el empleado.
func (p *Profile) Person() PersonID { return p.person }

// Employer devuelve el empleador.
func (p *Profile) Employer() OrganizationID { return p.employer }

// Terms devuelve las condiciones editables.
func (p *Profile) Terms() Terms {
	return Terms{ContributionGroup: p.contributionGroup, IncomeTaxRate: p.incomeTaxRate, Salary: p.salary, Account: p.account}
}

// Splits devuelve una copia de los repartos del pago.
func (p *Profile) Splits() []Split { return slices.Clone(p.splits) }

// SetTerms sustituye las condiciones. La aplicación comprueba que la cuenta de cotización es del
// empleador.
func (p *Profile) SetTerms(t Terms) error {
	var v fw.Validation
	t.validate(&v)
	if err := v.Err(); err != nil {
		return err
	}
	p.contributionGroup, p.incomeTaxRate, p.salary, p.account = t.ContributionGroup, t.IncomeTaxRate, t.Salary, t.Account
	p.Raise(ProfileChanged{EventMeta: p.NewEventMeta(), Employment: p.employment.String()})
	return nil
}

// AddSplit añade un reparto del pago. Invariantes (el C# sólo las comprobaba en un validador de
// aplicación e ignoraba las fechas de vigencia): exactamente uno de porcentaje, importe o residual;
// como mucho un residual a la vez; los porcentajes vigentes a la vez suman como mucho 100.
func (p *Profile) AddSplit(s Split) (SplitID, error) {
	var v fw.Validation
	v.Require(!s.IBAN.IsZero(), "iban", "required", "the bank account is required")
	modes := 0
	for _, set := range []bool{!s.Percent.IsZero(), !s.Amount.IsZero(), s.Residual} {
		if set {
			modes++
		}
	}
	v.Require(modes == 1, "split", "mode", "a split is a percentage, an amount or the residual")
	v.Require(s.Percent.IsZero() || (s.Percent.IsPositive() && s.Percent.LessThanOrEqual(hundred)), "percent", "range", "over 0 and up to 100")
	v.Require(s.Amount.IsZero() || (s.Amount.IsPositive() && s.Amount.Equal(s.Amount.Round(2))), "amount", "range", "a positive amount in cents")
	v.Require(!s.From.IsZero() && (s.Until.IsZero() || !s.Until.Before(s.From)), "from", "order", "a validity whose end does not precede its start")
	if err := v.Err(); err != nil {
		return SplitID{}, err
	}
	for _, o := range p.splits {
		if !o.overlaps(s) {
			continue
		}
		if o.Residual && s.Residual {
			return SplitID{}, fw.Violation("payroll.two_residuals", "only one account takes the residual at a time")
		}
	}
	if !s.Percent.IsZero() {
		// La suma se comprueba en cada inicio dentro del reparto nuevo (donde puede crecer).
		for _, at := range p.startsWithin(s) {
			total := s.Percent
			for _, o := range p.splits {
				if o.activeOn(at) {
					total = total.Add(o.Percent)
				}
			}
			if total.GreaterThan(hundred) {
				return SplitID{}, fw.Violation("payroll.split_over_100", "the percentages in force add up to more than 100")
			}
		}
	}
	s.ID = SplitID{fw.NewUUID()}
	p.splits = append(slices.Clone(p.splits), s)
	return s.ID, nil
}

func (p *Profile) startsWithin(s Split) []vocab.Date {
	out := []vocab.Date{s.From}
	for _, o := range p.splits {
		if o.overlaps(s) && o.From.After(s.From) {
			out = append(out, o.From)
		}
	}
	return out
}

// EndSplit termina un reparto en una fecha.
func (p *Profile) EndSplit(id SplitID, on vocab.Date) error {
	i := slices.IndexFunc(p.splits, func(s Split) bool { return s.ID == id })
	if i < 0 {
		return fw.NotFound("payroll.split", id)
	}
	if on.Before(p.splits[i].From) {
		return fw.Violation("payroll.split_end_before_start", "a split cannot end before it starts")
	}
	p.splits = slices.Clone(p.splits)
	p.splits[i].Until = on
	return nil
}

// Distribute reparte un líquido entre las cuentas vigentes en una fecha: primero los embargos,
// luego por prioridad; los importes fijos y los porcentajes (redondeados a céntimos) nunca superan
// lo que queda, y la cuenta residual se lleva el resto. Sin repartos no hay nada que repartir; con
// repartos y dinero sobrante, hace falta una cuenta residual.
func (p *Profile) Distribute(net vocab.Decimal, on vocab.Date) ([]Payment, error) {
	var active []Split
	for _, s := range p.splits {
		if s.activeOn(on) {
			active = append(active, s)
		}
	}
	if len(active) == 0 || !net.IsPositive() {
		return nil, nil
	}
	slices.SortStableFunc(active, func(a, b Split) int {
		if a.Garnishment != b.Garnishment {
			if a.Garnishment {
				return -1
			}
			return 1
		}
		if a.Residual != b.Residual {
			if b.Residual {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Priority, b.Priority)
	})
	left := net
	var out []Payment
	for _, s := range active {
		amount := s.Amount
		switch {
		case s.Residual:
			amount = left
		case !s.Percent.IsZero():
			amount = net.Mul(s.Percent).Div(hundred).Round(2)
		}
		if amount.GreaterThan(left) {
			amount = left
		}
		if amount.IsPositive() {
			out = append(out, Payment{IBAN: s.IBAN, Amount: amount, Garnishment: s.Garnishment})
			left = left.Sub(amount)
		}
	}
	if left.IsPositive() {
		return nil, fw.Violation("payroll.no_residual_account", "part of the net pay has no account: add a residual split")
	}
	return out, nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (p *Profile) AuditSnapshot() map[string]any {
	return map[string]any{"contributionGroup": p.contributionGroup, "incomeTaxRate": p.incomeTaxRate.String(),
		"salary": p.salary.Amount.String(), "splits": len(p.splits)}
}

// Campos del perfil.
var (
	ProfFieldID         = spec.Comparable("id", func(p *Profile) ProfileID { return p.ID() })
	ProfFieldEmployment = spec.Comparable("employment", (*Profile).Employment)
	ProfFieldPerson     = spec.Comparable("person", (*Profile).Person)
	ProfFieldEmployer   = spec.Comparable("employer", (*Profile).Employer)
)

// checkText recorta y limita un texto corto.
func checkText(v *fw.Validation, field, s string, max int) string {
	s = strings.TrimSpace(s)
	v.Require(utf8.RuneCountInString(s) <= max, field, "length", "too long")
	return s
}
