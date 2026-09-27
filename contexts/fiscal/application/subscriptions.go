package application

import (
	"context"
	"errors"

	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// PayslipApproved is the Fiscal copy of payroll.payslip-approved.v1: only the fields it needs.
type PayslipApproved struct {
	PayslipID     string `json:"payslipId"`
	Person        string `json:"person"`
	Employer      string `json:"employer"`
	PaymentDate   string `json:"paymentDate"`
	PerceptionKey string `json:"perceptionKey"`
	TaxableBase   string `json:"taxableBase"`
	IncomeTax     string `json:"incomeTax"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PayslipApproved) IntegrationEventType() string { return "payroll.payslip-approved.v1" }

// PayslipCancelled is the Fiscal copy of payroll.payslip-cancelled.v1.
type PayslipCancelled struct {
	PayslipID string `json:"payslipId"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PayslipCancelled) IntegrationEventType() string { return "payroll.payslip-cancelled.v1" }

// employmentIncome is the Modelo 190 key of employees' income: payslips without a key (a concept
// catalog without keys) are employment income.
const employmentIncome = "A"

// Subscribe registers the reactions of Fiscal on c (approved decision 2 of docs/NOMINAS.md): each
// approved payslip becomes a withholding record identified by the payslip; a cancelled payslip
// cancels it. Redeliveries are harmless.
func Subscribe(c *messaging.Consumer, withholdings domain.WithholdingRepository) {
	messaging.Handle(c, func(ctx context.Context, e PayslipApproved, _ app.Envelope) error {
		id, err1 := fw.ParseUUID(e.PayslipID)
		payer, err2 := fw.ParseUUID(e.Employer)
		person, err3 := fw.ParseUUID(e.Person)
		paid, err4 := vocab.ParseDate(e.PaymentDate)
		base, err5 := vocab.ParseDecimal(e.TaxableBase)
		withheld, err6 := vocab.ParseDecimal(e.IncomeTax)
		if err := errors.Join(err1, err2, err3, err4, err5, err6); err != nil {
			return fw.Violation("fiscal.invalid_event", "payroll.payslip-approved.v1: "+err.Error())
		}
		if _, err := withholdings.Get(ctx, domain.WithholdingID{UUID: id}); err == nil {
			return nil
		} else if !errors.Is(err, fw.ErrNotFound) {
			return err
		}
		key := e.PerceptionKey
		if key == "" {
			key = employmentIncome
		}
		w, err := domain.ReconstituteWithholding(domain.WithholdingID{UUID: id}, domain.WithholdingState{Payer: domain.OrganizationID{UUID: payer},
			Recipient: domain.PartyID{UUID: person}, PaymentDate: paid, Key: key, Perceptions: base, Withheld: withheld})
		if err != nil {
			return err
		}
		return withholdings.Save(ctx, w)
	})
	messaging.Handle(c, func(ctx context.Context, e PayslipCancelled, _ app.Envelope) error {
		id, err := fw.ParseUUID(e.PayslipID)
		if err != nil {
			return fw.Violation("fiscal.invalid_event", "payroll.payslip-cancelled.v1 without payslip")
		}
		w, err := withholdings.Get(ctx, domain.WithholdingID{UUID: id})
		if errors.Is(err, fw.ErrNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		if w.State().Cancelled {
			return nil
		}
		w.Cancel()
		return withholdings.Save(ctx, w)
	})
}

// Publications translates the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.FilingSubmitted) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.FilingSubmittedV1{FilingID: e.AggregateID, Declarant: e.Declarant, Form: e.Form, Year: e.Year,
			Period: e.Period, Number: e.Number, Recipients: e.Recipients, Perceptions: e.Perceptions, Withheld: e.Withheld}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.FilingReverted) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.FilingRevertedV1{FilingID: e.AggregateID, Reason: e.Reason}}, nil
	})
	return r
}
