package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/jhermoso/karpo-fw-go/contexts/documents/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Las copias en Documents de los eventos que registra (sólo los campos que necesita).

// InvoiceIssued es billing.invoice-issued.v1.
type InvoiceIssued struct {
	InvoiceID  string `json:"invoiceId"`
	Number     string `json:"number"`
	Kind       string `json:"kind"`
	Corrects   string `json:"corrects"`
	SourceType string `json:"sourceType"`
	SourceID   string `json:"sourceId"`
	Seller     string `json:"seller"`
	Customer   string `json:"customer"`
	IssueDate  string `json:"issueDate"`
	Total      string `json:"total"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (InvoiceIssued) IntegrationEventType() string { return "billing.invoice-issued.v1" }

// OrderConfirmed es orders.order-confirmed.v1.
type OrderConfirmed struct {
	OrderID  string `json:"orderId"`
	Company  string `json:"company"`
	Customer string `json:"customer"`
	Number   string `json:"number"`
	Total    string `json:"total"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (OrderConfirmed) IntegrationEventType() string { return "orders.order-confirmed.v1" }

// DeliveryIssued es orders.delivery-issued.v1.
type DeliveryIssued struct {
	DeliveryID string `json:"deliveryId"`
	OrderID    string `json:"orderId"`
	Company    string `json:"company"`
	Customer   string `json:"customer"`
	Number     string `json:"number"`
	Date       string `json:"date"`
	Total      string `json:"total"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (DeliveryIssued) IntegrationEventType() string { return "orders.delivery-issued.v1" }

// PurchaseRegistered es purchases.invoice-registered.v1.
type PurchaseRegistered struct {
	InvoiceID      string `json:"invoiceId"`
	Company        string `json:"company"`
	Supplier       string `json:"supplier"`
	SupplierNumber string `json:"supplierNumber"`
	Register       string `json:"register"`
	Received       string `json:"received"`
	Corrects       string `json:"corrects"`
	Total          string `json:"total"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PurchaseRegistered) IntegrationEventType() string { return "purchases.invoice-registered.v1" }

// PurchaseCancelled es purchases.invoice-cancelled.v1.
type PurchaseCancelled struct {
	InvoiceID string `json:"invoiceId"`
	Reason    string `json:"reason"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PurchaseCancelled) IntegrationEventType() string { return "purchases.invoice-cancelled.v1" }

// PayslipApproved es payroll.payslip-approved.v1.
type PayslipApproved struct {
	PayslipID   string `json:"payslipId"`
	Person      string `json:"person"`
	Employer    string `json:"employer"`
	Kind        string `json:"kind"`
	PeriodStart string `json:"periodStart"`
	PaymentDate string `json:"paymentDate"`
	Net         string `json:"net"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PayslipApproved) IntegrationEventType() string { return "payroll.payslip-approved.v1" }

// PayslipCancelled es payroll.payslip-cancelled.v1.
type PayslipCancelled struct {
	PayslipID string `json:"payslipId"`
	Reason    string `json:"reason"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PayslipCancelled) IntegrationEventType() string { return "payroll.payslip-cancelled.v1" }

// FilingSubmitted es fiscal.filing-submitted.v1.
type FilingSubmitted struct {
	FilingID  string `json:"filingId"`
	Declarant string `json:"declarant"`
	Form      string `json:"form"`
	Year      int    `json:"year"`
	Period    string `json:"period"`
	Withheld  string `json:"withheld"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (FilingSubmitted) IntegrationEventType() string { return "fiscal.filing-submitted.v1" }

// FilingReverted es fiscal.filing-reverted.v1.
type FilingReverted struct {
	FilingID string `json:"filingId"`
	Reason   string `json:"reason"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (FilingReverted) IntegrationEventType() string { return "fiscal.filing-reverted.v1" }

// entry construye el estado de una anotación a partir del texto de un evento.
type entry struct {
	event                       string
	company, party, date, total string
	fact, origin                domain.Ref
	number, reference, relation string
}

func (e entry) state() (domain.DocumentState, error) {
	var errs error
	company, err := fw.ParseUUID(e.company)
	errs = errors.Join(errs, err)
	date, err := vocab.ParseDate(e.date)
	errs = errors.Join(errs, err)
	s := domain.DocumentState{Company: domain.OrganizationID{UUID: company}, Fact: e.fact, Number: e.number, Reference: e.reference, Date: date,
		Origin: e.origin, Relation: e.relation}
	if e.party != "" {
		party, err := fw.ParseUUID(e.party)
		errs = errors.Join(errs, err)
		s.Party = domain.PartyID{UUID: party}
	}
	if e.total != "" {
		total, err := vocab.ParseDecimal(e.total)
		errs = errors.Join(errs, err)
		s.Total, s.HasTotal = total, true
	}
	if errs != nil {
		return domain.DocumentState{}, fw.Violation("documents.invalid_event", e.event+": "+errs.Error())
	}
	return s, nil
}

// Subscribe lleva el registro: cada documento que emite un contexto recibe su anotación, una vez, y
// las anulaciones la marcan. Documents nunca escribe en otro contexto ni numera nada.
func Subscribe(c *messaging.Consumer, d Deps) {
	s := newService(d)
	record := func(ctx context.Context, e entry) error {
		if existing, err := s.byFact(ctx, e.fact); err != nil || existing != nil {
			return err // ya anotado: el hecho se emite una vez
		}
		st, err := e.state()
		if err != nil {
			return err
		}
		doc, err := domain.RecordDocument(domain.NewDocumentID(), st)
		if err != nil {
			return fw.Violation("documents.invalid_event", e.event+": "+err.Error())
		}
		return s.docs.Create(ctx, doc)
	}
	cancel := func(ctx context.Context, ref domain.Ref, reason string) error {
		doc, err := s.byFact(ctx, ref)
		if err != nil || doc == nil || doc.State().Cancelled {
			return err // desconocido para el registro, o ya anulado
		}
		_, err = s.docs.Update(ctx, doc.ID(), func(_ context.Context, d *domain.Document) error {
			d.Cancel(reason)
			return nil
		})
		return err
	}

	// Una factura de venta, ordinaria o rectificativa: rectifica otra, o viene de un albarán.
	messaging.Handle(c, func(ctx context.Context, e InvoiceIssued, _ app.Envelope) error {
		x := entry{event: e.IntegrationEventType(), company: e.Seller, party: e.Customer, date: e.IssueDate, total: e.Total, number: e.Number,
			fact: domain.Ref{Type: domain.Invoice, ID: e.InvoiceID}}
		switch {
		case e.Kind == "corrective":
			x.fact.Type = domain.CreditNote
			if e.Corrects != "" {
				x.origin, x.relation = domain.Ref{Type: domain.Invoice, ID: e.Corrects}, domain.Rectifies
			}
		case e.SourceType == "orders.delivery" && e.SourceID != "":
			x.origin, x.relation = domain.Ref{Type: domain.DeliveryNote, ID: e.SourceID}, domain.OriginatesFrom
		}
		return record(ctx, x)
	})
	// Un pedido confirmado no lleva fecha propia: el día en que se confirmó.
	messaging.Handle(c, func(ctx context.Context, e OrderConfirmed, env app.Envelope) error {
		return record(ctx, entry{event: e.IntegrationEventType(), company: e.Company, party: e.Customer, date: vocab.DateOf(env.OccurredAt).String(),
			total: e.Total, number: e.Number, fact: domain.Ref{Type: domain.Order, ID: e.OrderID}})
	})
	messaging.Handle(c, func(ctx context.Context, e DeliveryIssued, _ app.Envelope) error {
		return record(ctx, entry{event: e.IntegrationEventType(), company: e.Company, party: e.Customer, date: e.Date, total: e.Total, number: e.Number,
			fact: domain.Ref{Type: domain.DeliveryNote, ID: e.DeliveryID}, origin: domain.Ref{Type: domain.Order, ID: e.OrderID}, relation: domain.OriginatesFrom})
	})
	// Una factura recibida: nuestro número de registro, y el del proveedor como referencia.
	messaging.Handle(c, func(ctx context.Context, e PurchaseRegistered, _ app.Envelope) error {
		x := entry{event: e.IntegrationEventType(), company: e.Company, party: e.Supplier, date: e.Received, total: e.Total, number: e.Register,
			reference: e.SupplierNumber, fact: domain.Ref{Type: domain.ReceivedInvoice, ID: e.InvoiceID}}
		if e.Corrects != "" {
			x.origin, x.relation = domain.Ref{Type: domain.ReceivedInvoice, ID: e.Corrects}, domain.Rectifies
		}
		return record(ctx, x)
	})
	messaging.Handle(c, func(ctx context.Context, e PurchaseCancelled, _ app.Envelope) error {
		return cancel(ctx, domain.Ref{Type: domain.ReceivedInvoice, ID: e.InvoiceID}, e.Reason)
	})
	// Una nómina no tiene número: su tipo y su mes.
	messaging.Handle(c, func(ctx context.Context, e PayslipApproved, _ app.Envelope) error {
		month := e.PeriodStart
		if len(month) >= 7 {
			month = month[:7]
		}
		return record(ctx, entry{event: e.IntegrationEventType(), company: e.Employer, party: e.Person, date: e.PaymentDate, total: e.Net,
			number: e.Kind + " " + month, fact: domain.Ref{Type: domain.Payslip, ID: e.PayslipID}})
	})
	messaging.Handle(c, func(ctx context.Context, e PayslipCancelled, _ app.Envelope) error {
		return cancel(ctx, domain.Ref{Type: domain.Payslip, ID: e.PayslipID}, e.Reason)
	})
	// Una declaración: modelo, año y periodo, el día en que se presentó.
	messaging.Handle(c, func(ctx context.Context, e FilingSubmitted, env app.Envelope) error {
		return record(ctx, entry{event: e.IntegrationEventType(), company: e.Declarant, date: vocab.DateOf(env.OccurredAt).String(), total: e.Withheld,
			number: fmt.Sprintf("%s-%d-%s", e.Form, e.Year, e.Period), fact: domain.Ref{Type: domain.TaxFiling, ID: e.FilingID}})
	})
	messaging.Handle(c, func(ctx context.Context, e FilingReverted, _ app.Envelope) error {
		return cancel(ctx, domain.Ref{Type: domain.TaxFiling, ID: e.FilingID}, e.Reason)
	})
}
