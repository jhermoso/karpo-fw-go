package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// The Accounting copies of the events it posts (only the fields it needs).

// InvoiceIssued is billing.invoice-issued.v1.
type InvoiceIssued struct {
	InvoiceID string `json:"invoiceId"`
	Number    string `json:"number"`
	Seller    string `json:"seller"`
	Customer  string `json:"customer"`
	IssueDate string `json:"issueDate"`
	Net       string `json:"net"`
	Total     string `json:"total"`
	Taxes     []struct {
		TaxCode         string `json:"taxCode"`
		Amount          string `json:"amount"`
		SurchargeAmount string `json:"surchargeAmount"`
	} `json:"taxes"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (InvoiceIssued) IntegrationEventType() string { return "billing.invoice-issued.v1" }

// CollectionAllocated is receivables.collection-allocated.v1.
type CollectionAllocated struct {
	CollectionID string `json:"collectionId"`
	Seller       string `json:"seller"`
	Payer        string `json:"payer"`
	Method       string `json:"method"`
	InvoiceID    string `json:"invoiceId"`
	Installment  int    `json:"installment"`
	Amount       string `json:"amount"`
	On           string `json:"on"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (CollectionAllocated) IntegrationEventType() string {
	return "receivables.collection-allocated.v1"
}

// AllocationReversed is receivables.allocation-reversed.v1.
type AllocationReversed struct {
	CollectionID string `json:"collectionId"`
	Seller       string `json:"seller"`
	InvoiceID    string `json:"invoiceId"`
	Installment  int    `json:"installment"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (AllocationReversed) IntegrationEventType() string { return "receivables.allocation-reversed.v1" }

// DirectDebitCollected is treasury.direct-debit-collected.v1.
type DirectDebitCollected struct {
	RemittanceID string `json:"remittanceId"`
	EndToEnd     string `json:"endToEnd"`
	Creditor     string `json:"creditor"`
	Amount       string `json:"amount"`
	CollectedOn  string `json:"collectedOn"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (DirectDebitCollected) IntegrationEventType() string {
	return "treasury.direct-debit-collected.v1"
}

// DirectDebitReturned is treasury.direct-debit-returned.v1.
type DirectDebitReturned struct {
	RemittanceID string `json:"remittanceId"`
	EndToEnd     string `json:"endToEnd"`
	Creditor     string `json:"creditor"`
	Amount       string `json:"amount"`
	ReturnedOn   string `json:"returnedOn"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (DirectDebitReturned) IntegrationEventType() string { return "treasury.direct-debit-returned.v1" }

// PayslipApproved is payroll.payslip-approved.v1.
type PayslipApproved struct {
	PayslipID       string `json:"payslipId"`
	Person          string `json:"person"`
	Employer        string `json:"employer"`
	PeriodEnd       string `json:"periodEnd"`
	Gross           string `json:"gross"`
	SocialSecurity  string `json:"socialSecurity"`
	IncomeTax       string `json:"incomeTax"`
	OtherDeductions string `json:"otherDeductions"`
	Net             string `json:"net"`
	EmployerCost    string `json:"employerCost"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PayslipApproved) IntegrationEventType() string { return "payroll.payslip-approved.v1" }

// PayslipCancelled is payroll.payslip-cancelled.v1.
type PayslipCancelled struct {
	PayslipID string `json:"payslipId"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PayslipCancelled) IntegrationEventType() string { return "payroll.payslip-cancelled.v1" }

// PaymentAllocated is payments.payment-allocated.v1.
type PaymentAllocated struct {
	PaymentID string `json:"paymentId"`
	Company   string `json:"company"`
	Payee     string `json:"payee"`
	Method    string `json:"method"`
	PayableID string `json:"payableId"`
	Kind      string `json:"kind"`
	Amount    string `json:"amount"`
	On        string `json:"on"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PaymentAllocated) IntegrationEventType() string { return "payments.payment-allocated.v1" }

// PaymentAllocationReversed is payments.allocation-reversed.v1.
type PaymentAllocationReversed struct {
	PaymentID string `json:"paymentId"`
	Company   string `json:"company"`
	PayableID string `json:"payableId"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PaymentAllocationReversed) IntegrationEventType() string {
	return "payments.allocation-reversed.v1"
}

type parser struct{ err error }

func (p *parser) id(s string) fw.UUID {
	u, err := fw.ParseUUID(s)
	p.err = errors.Join(p.err, err)
	return u
}

func (p *parser) date(s string) vocab.Date {
	d, err := vocab.ParseDate(s)
	p.err = errors.Join(p.err, err)
	return d
}

func (p *parser) amount(s string) vocab.Decimal {
	if s == "" {
		return vocab.DecimalFromInt(0)
	}
	d, err := vocab.ParseDecimal(s)
	p.err = errors.Join(p.err, err)
	return d
}

func (p *parser) check(event string) error {
	if p.err != nil {
		return fw.Violation("accounting.invalid_event", event+": "+p.err.Error())
	}
	return nil
}

// Subscribe posts the facts of the other contexts (approved decision 5 of docs/COBROS.md and
// docs/TESORERIA.md: each context publishes, Accounting posts). The posting profile of the
// company's ledger decides the accounts; each event posts once (source id).
func Subscribe(c *messaging.Consumer, d Deps) {
	s := newService(d)
	role := func(l *domain.Ledger, r domain.Role, err *error) string {
		code, e := l.AccountFor(r)
		*err = errors.Join(*err, e)
		return code
	}
	withLedger := func(ctx context.Context, company domain.OrganizationID, build func(*domain.Ledger) (domain.Draft, error)) error {
		l, err := s.ledgerOf(ctx, company)
		if err != nil {
			return err
		}
		draft, err := build(l)
		if err != nil {
			return err
		}
		_, err = s.post(ctx, draft)
		return err
	}
	// reverseSource reverses the latest entry of a source key that is not reversed yet, on the date
	// of the fact or on the date of the entry when the fact arrives dated earlier.
	reverseSource := func(ctx context.Context, company domain.OrganizationID, srcType, key string, on vocab.Date, src domain.Source) error {
		found, err := s.Entries.Find(ctx, spec.And(domain.EntFieldCompany.Eq(company), domain.EntFieldSrcType.Eq(srcType),
			domain.EntFieldSrcKey.Eq(key), domain.EntFieldReversed.Eq(false)), domain.EntFieldNumber.Desc())
		if err != nil || len(found) == 0 {
			return err
		}
		done, err := s.Entries.Exists(ctx, spec.And(domain.EntFieldCompany.Eq(company), domain.EntFieldSrcType.Eq(src.Type), domain.EntFieldSrcID.Eq(src.ID)))
		if err != nil || done {
			return err
		}
		if orig := found[0].State().Date; on.Before(orig) {
			on = orig // a reversal never precedes what it reverses
		}
		_, err = s.reverse(ctx, found[0].ID(), on, src)
		return err
	}

	// Sales invoice: customers (total) against revenue (net), output tax and surcharge.
	messaging.Handle(c, func(ctx context.Context, e InvoiceIssued, _ app.Envelope) error {
		var p parser
		company, customer, on := p.id(e.Seller), p.id(e.Customer), p.date(e.IssueDate)
		net, total := p.amount(e.Net), p.amount(e.Total)
		if err := p.check("billing.invoice-issued.v1"); err != nil {
			return err
		}
		return withLedger(ctx, domain.OrganizationID{UUID: company}, func(l *domain.Ledger) (domain.Draft, error) {
			var err error
			cust := domain.PartyID{UUID: customer}
			lines := []domain.Line{
				{Account: role(l, domain.RoleCustomers, &err), Party: cust, Debit: total, Description: "Factura " + e.Number},
				{Account: role(l, domain.RoleRevenue, &err), Credit: net},
			}
			for _, t := range e.Taxes {
				if amount := p.amount(t.Amount); !amount.IsZero() {
					acc, e2 := l.TaxAccount(t.TaxCode)
					err = errors.Join(err, e2)
					lines = append(lines, domain.Line{Account: acc, Credit: amount, Description: "Cuota " + t.TaxCode})
				}
				if sur := p.amount(t.SurchargeAmount); !sur.IsZero() {
					lines = append(lines, domain.Line{Account: role(l, domain.RoleSurcharge, &err), Credit: sur, Description: "Recargo " + t.TaxCode})
				}
			}
			if err := errors.Join(err, p.check("billing.invoice-issued.v1")); err != nil {
				return domain.Draft{}, err
			}
			return domain.Draft{Company: domain.OrganizationID{UUID: company}, Date: on, Description: "Factura " + e.Number,
				Source: domain.Source{Type: "billing.invoice-issued.v1", ID: e.InvoiceID, Key: e.InvoiceID}, Lines: lines}, nil
		})
	})

	// Collection allocated: the account of the method against customers. Offsets net two
	// invoices of the same customer: nothing to post.
	methodRole := map[string]domain.Role{"cash": domain.RoleCash, "transfer": domain.RoleBank, "card": domain.RoleBank, "check": domain.RoleBank,
		"direct-debit": domain.RoleDirectDebitClearing}
	messaging.Handle(c, func(ctx context.Context, e CollectionAllocated, env app.Envelope) error {
		r, ok := methodRole[e.Method]
		if !ok {
			return nil
		}
		var p parser
		company, payer, on, amount := p.id(e.Seller), p.id(e.Payer), p.date(e.On), p.amount(e.Amount)
		if err := p.check("receivables.collection-allocated.v1"); err != nil {
			return err
		}
		key := fmt.Sprintf("%s|%s|%d", e.CollectionID, e.InvoiceID, e.Installment)
		return withLedger(ctx, domain.OrganizationID{UUID: company}, func(l *domain.Ledger) (domain.Draft, error) {
			var err error
			lines := []domain.Line{{Account: role(l, r, &err), Debit: amount},
				{Account: role(l, domain.RoleCustomers, &err), Party: domain.PartyID{UUID: payer}, Credit: amount}}
			return domain.Draft{Company: domain.OrganizationID{UUID: company}, Date: on, Description: "Cobro (" + e.Method + ")",
				Source: domain.Source{Type: "receivables.collection-allocated.v1", ID: env.ID, Key: key}, Lines: lines}, err
		})
	})
	messaging.Handle(c, func(ctx context.Context, e AllocationReversed, env app.Envelope) error {
		var p parser
		company := p.id(e.Seller)
		if err := p.check("receivables.allocation-reversed.v1"); err != nil {
			return err
		}
		key := fmt.Sprintf("%s|%s|%d", e.CollectionID, e.InvoiceID, e.Installment)
		return reverseSource(ctx, domain.OrganizationID{UUID: company}, "receivables.collection-allocated.v1", key, vocab.DateOf(env.OccurredAt),
			domain.Source{Type: "receivables.allocation-reversed.v1", ID: env.ID, Key: key})
	})

	// Direct debits: the bank against the clearing account of direct debits (4312), and back.
	messaging.Handle(c, func(ctx context.Context, e DirectDebitCollected, _ app.Envelope) error {
		var p parser
		company, on, amount := p.id(e.Creditor), p.date(e.CollectedOn), p.amount(e.Amount)
		if err := p.check("treasury.direct-debit-collected.v1"); err != nil {
			return err
		}
		return withLedger(ctx, domain.OrganizationID{UUID: company}, func(l *domain.Ledger) (domain.Draft, error) {
			var err error
			lines := []domain.Line{{Account: role(l, domain.RoleBank, &err), Debit: amount}, {Account: role(l, domain.RoleDirectDebitClearing, &err), Credit: amount}}
			return domain.Draft{Company: domain.OrganizationID{UUID: company}, Date: on, Description: "Adeudo cobrado " + e.EndToEnd,
				Source: domain.Source{Type: "treasury.direct-debit-collected.v1", ID: e.RemittanceID + "|" + e.EndToEnd, Key: e.EndToEnd}, Lines: lines}, err
		})
	})
	messaging.Handle(c, func(ctx context.Context, e DirectDebitReturned, _ app.Envelope) error {
		var p parser
		company, on, amount := p.id(e.Creditor), p.date(e.ReturnedOn), p.amount(e.Amount)
		if err := p.check("treasury.direct-debit-returned.v1"); err != nil {
			return err
		}
		return withLedger(ctx, domain.OrganizationID{UUID: company}, func(l *domain.Ledger) (domain.Draft, error) {
			var err error
			lines := []domain.Line{{Account: role(l, domain.RoleDirectDebitClearing, &err), Debit: amount}, {Account: role(l, domain.RoleBank, &err), Credit: amount}}
			return domain.Draft{Company: domain.OrganizationID{UUID: company}, Date: on, Description: "Adeudo devuelto " + e.EndToEnd,
				Source: domain.Source{Type: "treasury.direct-debit-returned.v1", ID: e.RemittanceID + "|" + e.EndToEnd, Key: e.EndToEnd}, Lines: lines}, err
		})
	})

	// Payment allocated: the account of what was owed (suppliers, net pay, withholdings) against the
	// bank or cash. The party is the payee when it is a party (not the tax authority).
	kindRole := map[string]domain.Role{"supplier-invoice": domain.RoleSuppliers, "payroll": domain.RoleNetPay, "tax": domain.RoleWithholding}
	messaging.Handle(c, func(ctx context.Context, e PaymentAllocated, env app.Envelope) error {
		owed, ok := kindRole[e.Kind]
		if !ok {
			return fw.Violation("accounting.invalid_event", "payments.payment-allocated.v1: unknown kind "+e.Kind)
		}
		var p parser
		company, on, amount := p.id(e.Company), p.date(e.On), p.amount(e.Amount)
		if err := p.check("payments.payment-allocated.v1"); err != nil {
			return err
		}
		var payee domain.PartyID
		if u, err := fw.ParseUUID(e.Payee); err == nil {
			payee = domain.PartyID{UUID: u}
		}
		out := domain.RoleBank
		if e.Method == "cash" {
			out = domain.RoleCash
		}
		key := e.PaymentID + "|" + e.PayableID
		return withLedger(ctx, domain.OrganizationID{UUID: company}, func(l *domain.Ledger) (domain.Draft, error) {
			var err error
			lines := []domain.Line{{Account: role(l, owed, &err), Party: payee, Debit: amount}, {Account: role(l, out, &err), Credit: amount}}
			return domain.Draft{Company: domain.OrganizationID{UUID: company}, Date: on, Description: "Pago (" + e.Method + ")",
				Source: domain.Source{Type: "payments.payment-allocated.v1", ID: env.ID, Key: key}, Lines: lines}, err
		})
	})
	messaging.Handle(c, func(ctx context.Context, e PaymentAllocationReversed, env app.Envelope) error {
		var p parser
		company := p.id(e.Company)
		if err := p.check("payments.allocation-reversed.v1"); err != nil {
			return err
		}
		key := e.PaymentID + "|" + e.PayableID
		return reverseSource(ctx, domain.OrganizationID{UUID: company}, "payments.payment-allocated.v1", key, vocab.DateOf(env.OccurredAt),
			domain.Source{Type: "payments.allocation-reversed.v1", ID: env.ID, Key: key})
	})

	// Payslip: wages and employer social security against their payables and the net pay.
	messaging.Handle(c, func(ctx context.Context, e PayslipApproved, _ app.Envelope) error {
		var p parser
		company, person, on := p.id(e.Employer), p.id(e.Person), p.date(e.PeriodEnd)
		gross, ss, irpf, other, net, employer := p.amount(e.Gross), p.amount(e.SocialSecurity), p.amount(e.IncomeTax), p.amount(e.OtherDeductions),
			p.amount(e.Net), p.amount(e.EmployerCost)
		if err := p.check("payroll.payslip-approved.v1"); err != nil {
			return err
		}
		return withLedger(ctx, domain.OrganizationID{UUID: company}, func(l *domain.Ledger) (domain.Draft, error) {
			var err error
			emp := domain.PartyID{UUID: person}
			lines := []domain.Line{
				{Account: role(l, domain.RoleWages, &err), Party: emp, Debit: gross},
				{Account: role(l, domain.RoleEmployerSS, &err), Party: emp, Debit: employer},
				{Account: role(l, domain.RoleSSPayable, &err), Credit: ss.Add(employer)},
				{Account: role(l, domain.RoleWithholding, &err), Party: emp, Credit: irpf},
				{Account: role(l, domain.RoleNetPay, &err), Party: emp, Credit: net},
			}
			if !other.IsZero() {
				lines = append(lines, domain.Line{Account: role(l, domain.RoleOtherDeductions, &err), Party: emp, Credit: other})
			}
			return domain.Draft{Company: domain.OrganizationID{UUID: company}, Date: on, Description: "Nómina",
				Source: domain.Source{Type: "payroll.payslip-approved.v1", ID: e.PayslipID, Key: e.PayslipID}, Lines: lines}, err
		})
	})
	messaging.Handle(c, func(ctx context.Context, e PayslipCancelled, env app.Envelope) error {
		found, err := s.Entries.Find(ctx, spec.And(domain.EntFieldSrcType.Eq("payroll.payslip-approved.v1"), domain.EntFieldSrcKey.Eq(e.PayslipID)))
		if err != nil || len(found) == 0 {
			return err
		}
		company := found[0].State().Company
		return reverseSource(ctx, company, "payroll.payslip-approved.v1", e.PayslipID, vocab.DateOf(env.OccurredAt),
			domain.Source{Type: "payroll.payslip-cancelled.v1", ID: e.PayslipID, Key: e.PayslipID})
	})
}
