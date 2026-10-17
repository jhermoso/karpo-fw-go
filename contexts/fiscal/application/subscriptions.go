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

// PayslipApproved es la copia de Fiscal de payroll.payslip-approved.v1: sólo los campos que
// necesita.
type PayslipApproved struct {
	PayslipID     string `json:"payslipId"`
	Person        string `json:"person"`
	Employer      string `json:"employer"`
	PaymentDate   string `json:"paymentDate"`
	PerceptionKey string `json:"perceptionKey"`
	TaxableBase   string `json:"taxableBase"`
	IncomeTax     string `json:"incomeTax"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PayslipApproved) IntegrationEventType() string { return "payroll.payslip-approved.v1" }

// PayslipCancelled es la copia de Fiscal de payroll.payslip-cancelled.v1.
type PayslipCancelled struct {
	PayslipID string `json:"payslipId"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PayslipCancelled) IntegrationEventType() string { return "payroll.payslip-cancelled.v1" }

// employmentIncome es la clave del Modelo 190 de los rendimientos de los empleados: las nóminas sin
// clave (un catálogo de conceptos sin claves) son rendimientos del trabajo.
const employmentIncome = "A"

// Subscribe registra las reacciones de Fiscal en c (decisión aprobada 2 de docs/NOMINAS.md): cada
// nómina aprobada se convierte en un registro de retención identificado por la nómina; una nómina
// anulada lo anula. Las reentregas no hacen daño.
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

// InvoiceRegistered es la copia de Fiscal de purchases.invoice-registered.v1.
type InvoiceRegistered struct {
	InvoiceID      string `json:"invoiceId"`
	Company        string `json:"company"`
	Supplier       string `json:"supplier"`
	Issued         string `json:"issued"`
	Net            string `json:"net"`
	Withholding    string `json:"withholding"`
	WithholdingKey string `json:"withholdingKey"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (InvoiceRegistered) IntegrationEventType() string { return "purchases.invoice-registered.v1" }

// InvoiceCancelled es la copia de Fiscal de purchases.invoice-cancelled.v1.
type InvoiceCancelled struct {
	InvoiceID string `json:"invoiceId"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (InvoiceCancelled) IntegrationEventType() string { return "purchases.invoice-cancelled.v1" }

// SubscribePurchases registra las retenciones de las facturas recibidas (profesionales, clave G del
// Modelo 190) junto a las de nómina, para que los Modelos 111 y 190 las incluyan (decisión aprobada
// de docs/COMPRAS.md). La retención se devenga en la fecha de la factura; una factura anulada la
// anula.
func SubscribePurchases(c *messaging.Consumer, withholdings domain.WithholdingRepository) {
	messaging.Handle(c, func(ctx context.Context, e InvoiceRegistered, _ app.Envelope) error {
		if e.Withholding == "" || e.WithholdingKey == "" {
			return nil
		}
		id, err1 := fw.ParseUUID(e.InvoiceID)
		payer, err2 := fw.ParseUUID(e.Company)
		supplier, err3 := fw.ParseUUID(e.Supplier)
		issued, err4 := vocab.ParseDate(e.Issued)
		base, err5 := vocab.ParseDecimal(e.Net)
		withheld, err6 := vocab.ParseDecimal(e.Withholding)
		if err := errors.Join(err1, err2, err3, err4, err5, err6); err != nil {
			return fw.Violation("fiscal.invalid_event", "purchases.invoice-registered.v1: "+err.Error())
		}
		if !withheld.IsPositive() {
			return nil
		}
		if _, err := withholdings.Get(ctx, domain.WithholdingID{UUID: id}); err == nil {
			return nil
		} else if !errors.Is(err, fw.ErrNotFound) {
			return err
		}
		w, err := domain.ReconstituteWithholding(domain.WithholdingID{UUID: id}, domain.WithholdingState{Payer: domain.OrganizationID{UUID: payer},
			Recipient: domain.PartyID{UUID: supplier}, PaymentDate: issued, Key: e.WithholdingKey, Perceptions: base, Withheld: withheld})
		if err != nil {
			return err
		}
		return withholdings.Save(ctx, w)
	})
	messaging.Handle(c, func(ctx context.Context, e InvoiceCancelled, _ app.Envelope) error {
		id, err := fw.ParseUUID(e.InvoiceID)
		if err != nil {
			return fw.Violation("fiscal.invalid_event", "purchases.invoice-cancelled.v1 without invoice")
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

// Publications traduce los eventos de dominio al Published Language.
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
