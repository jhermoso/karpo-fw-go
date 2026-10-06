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

// The Documents copies of the events it records (only the fields it needs).

// InvoiceIssued is billing.invoice-issued.v1.
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

// IntegrationEventType implements application.IntegrationEvent.
func (InvoiceIssued) IntegrationEventType() string { return "billing.invoice-issued.v1" }

// OrderConfirmed is orders.order-confirmed.v1.
type OrderConfirmed struct {
	OrderID  string `json:"orderId"`
	Company  string `json:"company"`
	Customer string `json:"customer"`
	Number   string `json:"number"`
	Total    string `json:"total"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (OrderConfirmed) IntegrationEventType() string { return "orders.order-confirmed.v1" }

// DeliveryIssued is orders.delivery-issued.v1.
type DeliveryIssued struct {
	DeliveryID string `json:"deliveryId"`
	OrderID    string `json:"orderId"`
	Company    string `json:"company"`
	Customer   string `json:"customer"`
	Number     string `json:"number"`
	Date       string `json:"date"`
	Total      string `json:"total"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (DeliveryIssued) IntegrationEventType() string { return "orders.delivery-issued.v1" }

// PurchaseRegistered is purchases.invoice-registered.v1.
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

// IntegrationEventType implements application.IntegrationEvent.
func (PurchaseRegistered) IntegrationEventType() string { return "purchases.invoice-registered.v1" }

// PurchaseCancelled is purchases.invoice-cancelled.v1.
type PurchaseCancelled struct {
	InvoiceID string `json:"invoiceId"`
	Reason    string `json:"reason"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PurchaseCancelled) IntegrationEventType() string { return "purchases.invoice-cancelled.v1" }

// PayslipApproved is payroll.payslip-approved.v1.
type PayslipApproved struct {
	PayslipID   string `json:"payslipId"`
	Person      string `json:"person"`
	Employer    string `json:"employer"`
	Kind        string `json:"kind"`
	PeriodStart string `json:"periodStart"`
	PaymentDate string `json:"paymentDate"`
	Net         string `json:"net"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PayslipApproved) IntegrationEventType() string { return "payroll.payslip-approved.v1" }

// PayslipCancelled is payroll.payslip-cancelled.v1.
type PayslipCancelled struct {
	PayslipID string `json:"payslipId"`
	Reason    string `json:"reason"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PayslipCancelled) IntegrationEventType() string { return "payroll.payslip-cancelled.v1" }

// FilingSubmitted is fiscal.filing-submitted.v1.
type FilingSubmitted struct {
	FilingID  string `json:"filingId"`
	Declarant string `json:"declarant"`
	Form      string `json:"form"`
	Year      int    `json:"year"`
	Period    string `json:"period"`
	Withheld  string `json:"withheld"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (FilingSubmitted) IntegrationEventType() string { return "fiscal.filing-submitted.v1" }

// FilingReverted is fiscal.filing-reverted.v1.
type FilingReverted struct {
	FilingID string `json:"filingId"`
	Reason   string `json:"reason"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (FilingReverted) IntegrationEventType() string { return "fiscal.filing-reverted.v1" }

// entry builds the state of an entry from the text of an event.
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

// Subscribe keeps the register: every document a context issues gets its entry, once, and the
// cancellations mark it. Documents never writes into another context nor numbers anything.
func Subscribe(c *messaging.Consumer, d Deps) {
	s := newService(d)
	record := func(ctx context.Context, e entry) error {
		if existing, err := s.byFact(ctx, e.fact); err != nil || existing != nil {
			return err // recorded already: the fact is issued once
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
			return err // unknown to the register, or cancelled already
		}
		_, err = s.docs.Update(ctx, doc.ID(), func(_ context.Context, d *domain.Document) error {
			d.Cancel(reason)
			return nil
		})
		return err
	}

	// A sales invoice, ordinary or corrective: it rectifies another, or comes from a delivery note.
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
	// A confirmed order carries no date of its own: the day it was confirmed.
	messaging.Handle(c, func(ctx context.Context, e OrderConfirmed, env app.Envelope) error {
		return record(ctx, entry{event: e.IntegrationEventType(), company: e.Company, party: e.Customer, date: vocab.DateOf(env.OccurredAt).String(),
			total: e.Total, number: e.Number, fact: domain.Ref{Type: domain.Order, ID: e.OrderID}})
	})
	messaging.Handle(c, func(ctx context.Context, e DeliveryIssued, _ app.Envelope) error {
		return record(ctx, entry{event: e.IntegrationEventType(), company: e.Company, party: e.Customer, date: e.Date, total: e.Total, number: e.Number,
			fact: domain.Ref{Type: domain.DeliveryNote, ID: e.DeliveryID}, origin: domain.Ref{Type: domain.Order, ID: e.OrderID}, relation: domain.OriginatesFrom})
	})
	// A received invoice: our register number, and the supplier's as reference.
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
	// A payslip has no number: its kind and month.
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
	// A tax filing: form, year and period, on the day it was submitted.
	messaging.Handle(c, func(ctx context.Context, e FilingSubmitted, env app.Envelope) error {
		return record(ctx, entry{event: e.IntegrationEventType(), company: e.Declarant, date: vocab.DateOf(env.OccurredAt).String(), total: e.Withheld,
			number: fmt.Sprintf("%s-%d-%s", e.Form, e.Year, e.Period), fact: domain.Ref{Type: domain.TaxFiling, ID: e.FilingID}})
	})
	messaging.Handle(c, func(ctx context.Context, e FilingReverted, _ app.Envelope) error {
		return cancel(ctx, domain.Ref{Type: domain.TaxFiling, ID: e.FilingID}, e.Reason)
	})
}
