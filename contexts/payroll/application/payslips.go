package application

import (
	"context"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/payroll/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// DraftPayslip drafts the payslip of an employee for a period.
type DraftPayslip struct {
	Person      string     `json:"person"`
	Employer    string     `json:"employer"`
	Kind        string     `json:"kind,omitempty"` // ordinary (default), extra-pay, settlement
	Start       vocab.Date `json:"start"`
	End         vocab.Date `json:"end"`
	PaymentDate vocab.Date `json:"paymentDate,omitzero"` // default: the end of the period
}

// AddLine adds a line of a concept (by id or code): an amount, quantity × unit or base × percent.
type AddLine struct {
	ID          domain.PayslipID `json:"-"`
	Concept     string           `json:"concept"`
	Description string           `json:"description,omitempty"`
	Amount      string           `json:"amount,omitempty"`
	Quantity    string           `json:"quantity,omitempty"`
	UnitAmount  string           `json:"unitAmount,omitempty"`
	Base        string           `json:"base,omitempty"`
	Percent     string           `json:"percent,omitempty"`
}

// RemoveLine removes a line of a draft.
type RemoveLine struct {
	ID   domain.PayslipID `json:"-"`
	Line string           `json:"line"`
}

// Reschedule changes the payment date of a draft.
type Reschedule struct {
	ID          domain.PayslipID `json:"-"`
	PaymentDate vocab.Date       `json:"paymentDate"`
}

// ApprovePayslip approves a draft.
type ApprovePayslip struct {
	ID domain.PayslipID `json:"-"`
}

// CancelPayslip cancels an approved payslip.
type CancelPayslip struct {
	ID     domain.PayslipID `json:"-"`
	Reason string           `json:"reason"`
}

// DiscardPayslip deletes a draft.
type DiscardPayslip struct{ ID domain.PayslipID }

// GetPayslip loads a payslip.
type GetPayslip struct{ ID domain.PayslipID }

// SearchPayslips searches payslips of the caller's scope.
type SearchPayslips struct {
	Employer, Person, Status string
	From, To                 string // period start range (civil dates)
	Page, Size               int
}

// LineDTO is the transport form of a line.
type LineDTO struct {
	ID          string `json:"id"`
	Concept     string `json:"concept"`
	Code        string `json:"code"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Quantity    string `json:"quantity,omitempty"`
	UnitAmount  string `json:"unitAmount,omitempty"`
	Base        string `json:"base,omitempty"`
	Percent     string `json:"percent,omitempty"`
	Amount      string `json:"amount"`
}

// TotalsDTO is the transport form of the totals.
type TotalsDTO struct {
	Gross            string `json:"gross"`
	ContributionBase string `json:"contributionBase"`
	TaxableBase      string `json:"taxableBase"`
	SocialSecurity   string `json:"socialSecurity"`
	IncomeTax        string `json:"incomeTax"`
	OtherDeductions  string `json:"otherDeductions"`
	Net              string `json:"net"`
	EmployerCost     string `json:"employerCost"`
	CompanyCost      string `json:"companyCost"`
}

// PayslipDTO is the transport form of a payslip.
type PayslipDTO struct {
	ID                string    `json:"id"`
	Employment        string    `json:"employment"`
	Person            string    `json:"person"`
	Employer          string    `json:"employer"`
	EmployeeNumber    string    `json:"employeeNumber,omitempty"`
	Kind              string    `json:"kind"`
	Start             string    `json:"start"`
	End               string    `json:"end"`
	PaymentDate       string    `json:"paymentDate"`
	Status            string    `json:"status"`
	Agreement         string    `json:"agreement,omitempty"`
	WorkCenter        string    `json:"workCenter,omitempty"`
	ContributionGroup int       `json:"contributionGroup,omitempty"`
	IncomeTaxRate     string    `json:"incomeTaxRate,omitempty"`
	EmployerAccount   string    `json:"employerAccount,omitempty"`
	Lines             []LineDTO `json:"lines"`
	Totals            TotalsDTO `json:"totals"`
	CancelReason      string    `json:"cancelReason,omitempty"`
	Version           int64     `json:"version"`
	ModifiedBy        string    `json:"modifiedBy,omitempty"`
}

func money(d vocab.Decimal) string { return d.StringFixed(2) }

func optID(u fw.UUID) string {
	if u.IsZero() {
		return ""
	}
	return u.String()
}

func payslipDTO(p *domain.Payslip) PayslipDTO {
	start, end := p.Period()
	sn := p.Snapshot()
	t := p.Totals()
	d := PayslipDTO{ID: p.ID().String(), Employment: p.Employment().String(), Person: p.Person().String(), Employer: p.Employer().String(),
		EmployeeNumber: sn.EmployeeNumber, Kind: p.Kind().String(), Start: start.String(), End: end.String(),
		PaymentDate: p.PaymentDate().String(), Status: p.Status().String(), Agreement: optID(sn.Agreement.UUID),
		WorkCenter: optID(sn.WorkCenter.UUID), ContributionGroup: sn.ContributionGroup, IncomeTaxRate: decimalText(sn.IncomeTaxRate),
		EmployerAccount: optID(sn.EmployerAccount.UUID), Lines: []LineDTO{}, CancelReason: p.CancelReason(), Version: p.Version(),
		ModifiedBy: p.ModifiedBy().Name,
		Totals: TotalsDTO{Gross: money(t.Gross), ContributionBase: money(t.ContributionBase), TaxableBase: money(t.TaxableBase),
			SocialSecurity: money(t.SocialSecurity), IncomeTax: money(t.IncomeTax), OtherDeductions: money(t.OtherDeductions),
			Net: money(t.Net), EmployerCost: money(t.EmployerCost), CompanyCost: money(t.CompanyCost)}}
	for _, l := range p.Lines() {
		d.Lines = append(d.Lines, LineDTO{ID: l.ID.String(), Concept: l.Concept.String(), Code: l.Code, Kind: l.Kind.String(),
			Description: l.Description, Quantity: decimalText(l.Quantity), UnitAmount: decimalText(l.UnitAmount), Base: decimalText(l.Base),
			Percent: decimalText(l.Percent), Amount: money(l.Amount)})
	}
	return d
}

func (s service) concept(ctx context.Context, ref string) (domain.Concept, error) {
	cs, err := s.Catalogs.Concepts(ctx)
	if err != nil {
		return domain.Concept{}, err
	}
	for _, c := range cs {
		if c.ID.String() == strings.ToLower(ref) || c.Code == strings.ToUpper(strings.TrimSpace(ref)) {
			return c, nil
		}
	}
	var v fw.Validation
	v.Add("concept", "unknown", "unknown pay concept")
	return domain.Concept{}, v.Err()
}

func (s service) payslipUseCases(svc *Service) {
	update := func(ctx context.Context, id domain.PayslipID, fn func(context.Context, *domain.Payslip) error) (PayslipDTO, error) {
		sc := scopeOf(ctx)
		p, err := s.payslips.Update(ctx, id, func(ctx context.Context, p *domain.Payslip) error {
			if err := sc.check(domain.PayslipKind, p.ID(), p.Employer(), true); err != nil {
				return err
			}
			return fn(ctx, p)
		})
		if err != nil {
			return PayslipDTO{}, err
		}
		return payslipDTO(p), nil
	}

	svc.DraftPayslip = guard(PermPayslipCreate, func(ctx context.Context, c DraftPayslip) (PayslipDTO, error) {
		var v fw.Validation
		person := domain.PersonID{UUID: parseID(&v, "person", c.Person)}
		employer := domain.OrganizationID{UUID: parseID(&v, "employer", c.Employer)}
		kind := domain.Ordinary
		if c.Kind != "" {
			k, ok := domain.ParseKind(c.Kind)
			v.Require(ok, "kind", "enum", "ordinary, extra-pay or settlement")
			kind = k
		}
		v.Require(!c.Start.IsZero() && !c.End.IsZero(), "period", "required", "start and end are required")
		if err := v.Err(); err != nil {
			return PayslipDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", employer, employer, true); err != nil {
			return PayslipDTO{}, err
		}
		emp, ok, err := s.Employments.EmploymentOn(ctx, person, employer, c.Start)
		if err == nil && !ok {
			emp, ok, err = s.Employments.EmploymentOn(ctx, person, employer, c.End)
		}
		if err != nil {
			return PayslipDTO{}, err
		}
		if !ok {
			return PayslipDTO{}, fw.Violation("payroll.not_employed", "the person is not an employee of the employer in the period")
		}
		same, err := s.Payslips.Find(ctx, domain.SamePeriod(emp.ID, kind, c.Start))
		if err != nil {
			return PayslipDTO{}, err
		}
		if len(same) > 0 {
			return PayslipDTO{}, fw.Violation("payroll.duplicate_payslip", "the employment already has a payslip of that kind for the period")
		}
		snap := domain.Snapshot{EmployeeNumber: emp.Number, Agreement: emp.Agreement, WorkCenter: emp.WorkCenter}
		profiles, err := s.Profiles.Find(ctx, domain.ProfFieldEmployment.Eq(emp.ID))
		if err != nil {
			return PayslipDTO{}, err
		}
		if len(profiles) > 0 {
			t := profiles[0].Terms()
			snap.ContributionGroup, snap.IncomeTaxRate, snap.EmployerAccount = t.ContributionGroup, t.IncomeTaxRate, t.Account
		}
		pay := c.PaymentDate
		if pay.IsZero() {
			pay = c.End
		}
		p, err := domain.DraftPayslip(domain.NewPayslipID(), domain.PayslipState{Employment: emp.ID, Person: person, Employer: employer,
			Kind: kind, Start: c.Start, End: c.End, PaymentDate: pay, Snapshot: snap})
		if err != nil {
			return PayslipDTO{}, err
		}
		if err := s.payslips.Create(ctx, p); err != nil {
			return PayslipDTO{}, err
		}
		return payslipDTO(p), nil
	}, pipeline.Transactional[DraftPayslip, PayslipDTO](s.UoW))

	svc.AddLine = guard(PermPayslipUpdate, func(ctx context.Context, c AddLine) (PayslipDTO, error) {
		var v fw.Validation
		in := domain.LineInput{Description: c.Description, Amount: parseDecimal(&v, "amount", c.Amount),
			Quantity: parseDecimal(&v, "quantity", c.Quantity), UnitAmount: parseDecimal(&v, "unitAmount", c.UnitAmount),
			Base: parseDecimal(&v, "base", c.Base), Percent: parseDecimal(&v, "percent", c.Percent)}
		if err := v.Err(); err != nil {
			return PayslipDTO{}, err
		}
		concept, err := s.concept(ctx, c.Concept)
		if err != nil {
			return PayslipDTO{}, err
		}
		line, err := domain.NewLine(concept, in)
		if err != nil {
			return PayslipDTO{}, err
		}
		return update(ctx, c.ID, func(_ context.Context, p *domain.Payslip) error { _, err := p.AddLine(line); return err })
	}, retry[AddLine, PayslipDTO]())

	svc.RemoveLine = guard(PermPayslipUpdate, func(ctx context.Context, c RemoveLine) (PayslipDTO, error) {
		var v fw.Validation
		line := domain.LineID{UUID: parseID(&v, "line", c.Line)}
		if err := v.Err(); err != nil {
			return PayslipDTO{}, err
		}
		return update(ctx, c.ID, func(_ context.Context, p *domain.Payslip) error { return p.RemoveLine(line) })
	}, retry[RemoveLine, PayslipDTO]())

	svc.Reschedule = guard(PermPayslipUpdate, func(ctx context.Context, c Reschedule) (PayslipDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, p *domain.Payslip) error { return p.Reschedule(c.PaymentDate) })
	}, retry[Reschedule, PayslipDTO]())

	svc.Approve = guard(PermPayslipApprove, func(ctx context.Context, c ApprovePayslip) (PayslipDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, p *domain.Payslip) error { return p.Approve() })
	}, retry[ApprovePayslip, PayslipDTO]())

	svc.Cancel = guard(PermPayslipApprove, func(ctx context.Context, c CancelPayslip) (PayslipDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, p *domain.Payslip) error { return p.Cancel(c.Reason) })
	}, retry[CancelPayslip, PayslipDTO]())

	svc.Discard = guard(PermPayslipUpdate, func(ctx context.Context, c DiscardPayslip) (struct{}, error) {
		sc := scopeOf(ctx)
		return struct{}{}, s.payslips.Delete(ctx, c.ID, func(_ context.Context, p *domain.Payslip) error {
			if err := sc.check(domain.PayslipKind, p.ID(), p.Employer(), true); err != nil {
				return err
			}
			return p.Discard()
		})
	})

	svc.GetPayslip = guard(PermPayslipRead, func(ctx context.Context, q GetPayslip) (PayslipDTO, error) {
		p, err := s.Payslips.Get(ctx, q.ID)
		if err != nil {
			return PayslipDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.PayslipKind, p.ID(), p.Employer(), false); err != nil {
			return PayslipDTO{}, err
		}
		return payslipDTO(p), nil
	})

	svc.SearchPayslips = guard(PermPayslipRead, func(ctx context.Context, q SearchPayslips) (fw.Page[PayslipDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Payslip]{within(scopeOf(ctx), domain.PayFieldEmployer)}
		if q.Employer != "" {
			parts = append(parts, domain.PayFieldEmployer.Eq(domain.OrganizationID{UUID: parseID(&v, "employer", q.Employer)}))
		}
		if q.Person != "" {
			parts = append(parts, domain.PayFieldPerson.Eq(domain.PersonID{UUID: parseID(&v, "person", q.Person)}))
		}
		if q.Status != "" {
			st, ok := domain.ParseStatus(q.Status)
			v.Require(ok, "status", "enum", "draft, approved or cancelled")
			parts = append(parts, domain.PayFieldStatus.Eq(int(st)))
		}
		for field, s := range map[string]string{"from": q.From, "to": q.To} {
			if s == "" {
				continue
			}
			d, err := vocab.ParseDate(s)
			v.Require(err == nil, field, "format", "a date YYYY-MM-DD is required")
			if field == "from" {
				parts = append(parts, domain.PayFieldStart.Ge(d))
			} else {
				parts = append(parts, domain.PayFieldStart.Le(d))
			}
		}
		if err := v.Err(); err != nil {
			return fw.Page[PayslipDTO]{}, err
		}
		page, err := s.Payslips.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.PayFieldStart.Desc()))
		if err != nil {
			return fw.Page[PayslipDTO]{}, err
		}
		return fw.MapPage(page, payslipDTO), nil
	})
}
