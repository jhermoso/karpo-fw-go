package application

import (
	"context"
	"fmt"

	"github.com/jhermoso/karpo-fw-go/contexts/payroll/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/payroll/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Remittance implements contracts.Remittance (one query per batch; it serves Treasury, not users).
type Remittance struct {
	Payslips domain.PayslipRepository
	Profiles domain.ProfileRepository
}

var _ contracts.Remittance = Remittance{}

// NetPayments implements contracts.Remittance: the net pay of each approved payslip split with
// the profile of its employment in force on the payment date.
func (r Remittance) NetPayments(ctx context.Context, payslipIDs []string) (map[string][]contracts.PaymentRef, error) {
	if len(payslipIDs) > contracts.MaxBatch {
		return nil, fmt.Errorf("%w: at most %d ids per call", fw.ErrValidation, contracts.MaxBatch)
	}
	var ids []domain.PayslipID
	for _, s := range payslipIDs {
		if id, err := domain.ParsePayslipID(s); err == nil && !id.IsZero() {
			ids = append(ids, id)
		}
	}
	out := map[string][]contracts.PaymentRef{}
	if len(ids) == 0 {
		return out, nil
	}
	ps, err := r.Payslips.Find(ctx, domain.PayFieldID.In(ids...).And(domain.PayFieldStatus.Eq(int(domain.Approved))))
	if err != nil {
		return nil, err
	}
	var employments []domain.EmploymentID
	for _, p := range ps {
		employments = append(employments, p.Employment())
	}
	profiles := map[domain.EmploymentID]*domain.Profile{}
	if len(employments) > 0 {
		prs, err := r.Profiles.Find(ctx, domain.ProfFieldEmployment.In(employments...))
		if err != nil {
			return nil, err
		}
		for _, pr := range prs {
			profiles[pr.Employment()] = pr
		}
	}
	for _, p := range ps {
		pr, ok := profiles[p.Employment()]
		if !ok {
			continue
		}
		pays, err := pr.Distribute(p.Totals().Net, p.PaymentDate())
		if err != nil {
			return nil, fmt.Errorf("payslip %s: %w", p.ID(), err)
		}
		for _, pay := range pays {
			out[p.ID().String()] = append(out[p.ID().String()], contracts.PaymentRef{IBAN: pay.IBAN.String(), Amount: money(pay.Amount),
				Garnishment: pay.Garnishment})
		}
	}
	return out, nil
}

// Publications translates the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.PayslipApproved) ([]app.IntegrationEvent, error) {
		t := e.Totals
		return []app.IntegrationEvent{contracts.PayslipApprovedV1{PayslipID: e.AggregateID, Employment: e.Employment, Person: e.Person,
			Employer: e.Employer, Kind: e.Kind, PeriodStart: e.Start, PeriodEnd: e.End, PaymentDate: e.PaymentDate,
			EmployerAccount: e.EmployerAccount, PerceptionKey: e.PerceptionKey, Gross: money(t.Gross), ContributionBase: money(t.ContributionBase),
			TaxableBase: money(t.TaxableBase), SocialSecurity: money(t.SocialSecurity), IncomeTax: money(t.IncomeTax),
			OtherDeductions: money(t.OtherDeductions), Net: money(t.Net), EmployerCost: money(t.EmployerCost)}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.PayslipCancelled) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.PayslipCancelledV1{PayslipID: e.AggregateID, Reason: e.Reason}}, nil
	})
	return r
}
