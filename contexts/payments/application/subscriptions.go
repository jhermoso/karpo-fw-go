package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/payments/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/payments/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// The Payments copies of the events it consumes (only the fields it needs).

// PayslipApproved is payroll.payslip-approved.v1.
type PayslipApproved struct {
	PayslipID   string `json:"payslipId"`
	Person      string `json:"person"`
	Employer    string `json:"employer"`
	PeriodEnd   string `json:"periodEnd"`
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

// InvoiceRegistered is purchases.invoice-registered.v1.
type InvoiceRegistered struct {
	InvoiceID      string `json:"invoiceId"`
	Company        string `json:"company"`
	Supplier       string `json:"supplier"`
	SupplierNumber string `json:"supplierNumber"`
	Issued         string `json:"issued"`
	Due            string `json:"due"`
	Payable        string `json:"payable"`
	PayTo          []struct {
		IBAN   string `json:"iban"`
		Amount string `json:"amount"`
	} `json:"payTo"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (InvoiceRegistered) IntegrationEventType() string { return "purchases.invoice-registered.v1" }

// InvoiceCancelled is purchases.invoice-cancelled.v1.
type InvoiceCancelled struct {
	InvoiceID string `json:"invoiceId"`
	Reason    string `json:"reason"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (InvoiceCancelled) IntegrationEventType() string { return "purchases.invoice-cancelled.v1" }

// TransferExecuted is treasury.transfer-executed.v1.
type TransferExecuted struct {
	EndToEnd   string `json:"endToEnd"`
	Debtor     string `json:"debtor"`
	PayableID  string `json:"payableId"`
	Amount     string `json:"amount"`
	ExecutedOn string `json:"executedOn"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (TransferExecuted) IntegrationEventType() string { return "treasury.transfer-executed.v1" }

// TransferRejected is treasury.transfer-rejected.v1.
type TransferRejected struct {
	EndToEnd string `json:"endToEnd"`
	Debtor   string `json:"debtor"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (TransferRejected) IntegrationEventType() string { return "treasury.transfer-rejected.v1" }

// TaxAuthority is the payee of the tax payables of phase 1 (Spanish forms).
const TaxAuthority = "AEAT"

func invalid(event string, err error) error {
	return fw.Violation("payments.invalid_event", event+": "+err.Error())
}

// Subscribe registers the reactions to the other contexts (approved decisions of docs/NOMINAS.md,
// docs/FISCAL.md, docs/TESORERIA.md and docs/COMPRAS.md: each publishes, Payments records what is
// owed and Treasury executes the transfers):
//
//   - a received invoice owes what is paid to the supplier (its total minus the withholding) on
//     its due date, to the accounts it carries; a corrective one (negative) owes nothing here; a
//     cancelled invoice withdraws it while unpaid;
//   - an approved payslip owes its net pay to the employee on its payment date, to the accounts
//     of its split (Payroll's Remittance port); a cancelled one withdraws it while unpaid;
//   - a submitted tax form with an amount owes it to the tax authority by its legal deadline; a
//     reverted one withdraws it while unpaid;
//   - an executed transfer becomes a payment by transfer allocated to its payable (left
//     unallocated if the payable was paid meanwhile, for a person to decide); a rejected one
//     cancels that payment.
//
// Redeliveries are harmless: payables are unique by source and payments by reference.
func Subscribe(c *messaging.Consumer, d Deps) {
	s := newService(d)
	bySource := func(ctx context.Context, typ, id string) ([]*domain.Payable, error) {
		return s.Payables.Find(ctx, spec.And(domain.PayFieldSrcType.Eq(typ), domain.PayFieldSrcID.Eq(id)))
	}
	create := func(ctx context.Context, st domain.PayableState) error {
		if found, err := s.Payables.Exists(ctx, spec.And(domain.PayFieldCompany.Eq(st.Company), domain.PayFieldSrcType.Eq(st.Source.Type),
			domain.PayFieldSrcID.Eq(st.Source.ID))); err != nil || found {
			return err
		}
		p, err := domain.RegisterPayable(domain.NewPayableID(), st)
		if err != nil {
			return err
		}
		return s.payables.Create(ctx, p)
	}
	withdraw := func(ctx context.Context, typ, id, reason string) error {
		found, err := bySource(ctx, typ, id)
		if err != nil {
			return err
		}
		for _, p := range found {
			if p.State().Cancelled || p.State().Paid.IsPositive() {
				continue // a paid obligation is refunded by a person, not withdrawn
			}
			if _, err := s.payables.Update(ctx, p.ID(), func(_ context.Context, p *domain.Payable) error { return p.Cancel(reason) }); err != nil {
				return err
			}
		}
		return nil
	}

	const invoiceRegistered = "purchases.invoice-registered.v1"
	messaging.Handle(c, func(ctx context.Context, e InvoiceRegistered, _ app.Envelope) error {
		company, err1 := fw.ParseUUID(e.Company)
		supplier, err2 := fw.ParseUUID(e.Supplier)
		issued, err3 := vocab.ParseDate(e.Issued)
		due, err4 := vocab.ParseDate(e.Due)
		amount, err5 := vocab.ParseDecimal(e.Payable)
		if err := errors.Join(err1, err2, err3, err4, err5); err != nil {
			return invalid(invoiceRegistered, err)
		}
		if !amount.IsPositive() {
			return nil // a credit of the supplier: offsets are phase 2
		}
		st := domain.PayableState{Company: domain.OrganizationID{UUID: company}, Payee: domain.PartyID{UUID: supplier}, Kind: domain.SupplierInvoice,
			Source: domain.Source{Type: invoiceRegistered, ID: e.InvoiceID}, Document: e.SupplierNumber, Issued: issued, Due: due, Currency: euro,
			Amount: amount}
		for _, t := range e.PayTo {
			iban, err1 := vocab.NewIBAN(t.IBAN)
			a, err2 := vocab.ParseDecimal(t.Amount)
			if err := errors.Join(err1, err2); err != nil {
				return invalid(invoiceRegistered, err)
			}
			st.PayTo = append(st.PayTo, domain.PayTo{IBAN: iban, Amount: a})
		}
		return create(ctx, st)
	})
	messaging.Handle(c, func(ctx context.Context, e InvoiceCancelled, _ app.Envelope) error {
		return withdraw(ctx, invoiceRegistered, e.InvoiceID, "invoice cancelled: "+e.Reason)
	})

	const payslipApproved = "payroll.payslip-approved.v1"
	messaging.Handle(c, func(ctx context.Context, e PayslipApproved, _ app.Envelope) error {
		employer, err1 := fw.ParseUUID(e.Employer)
		person, err2 := fw.ParseUUID(e.Person)
		end, err3 := vocab.ParseDate(e.PeriodEnd)
		net, err4 := vocab.ParseDecimal(e.Net)
		if err := errors.Join(err1, err2, err3, err4); err != nil {
			return invalid(payslipApproved, err)
		}
		if !net.IsPositive() {
			return nil
		}
		due := end
		if pay, err := vocab.ParseDate(e.PaymentDate); err == nil && e.PaymentDate != "" && !pay.Before(end) {
			due = pay
		}
		st := domain.PayableState{Company: domain.OrganizationID{UUID: employer}, Payee: domain.PartyID{UUID: person}, Kind: domain.Payroll,
			Source: domain.Source{Type: payslipApproved, ID: e.PayslipID}, Document: "Nómina " + e.PeriodEnd[:7], Issued: end, Due: due, Currency: euro,
			Amount: net}
		if s.NetPay != nil {
			splits, err := s.NetPay.NetPayments(ctx, []string{e.PayslipID})
			if err != nil {
				return err
			}
			sum := vocab.DecimalFromInt(0)
			for _, t := range splits[e.PayslipID] {
				sum = sum.Add(t.Amount)
			}
			if sum.Equal(net) {
				st.PayTo = splits[e.PayslipID] // a split that does not add up is left for a person
			}
		}
		return create(ctx, st)
	})
	messaging.Handle(c, func(ctx context.Context, e PayslipCancelled, _ app.Envelope) error {
		return withdraw(ctx, payslipApproved, e.PayslipID, "payslip cancelled: "+e.Reason)
	})

	const filingSubmitted = "fiscal.filing-submitted.v1"
	messaging.Handle(c, func(ctx context.Context, e FilingSubmitted, env app.Envelope) error {
		if strings.TrimSpace(e.Form) == "190" { // informative: nothing to pay
			return nil
		}
		declarant, err1 := fw.ParseUUID(e.Declarant)
		amount, err2 := vocab.ParseDecimal(e.Withheld)
		if err := errors.Join(err1, err2); err != nil {
			return invalid(filingSubmitted, err)
		}
		if !amount.IsPositive() {
			return nil
		}
		due, err := domain.TaxDue(e.Year, e.Period)
		if err != nil {
			return nil // a period without payment (annual summaries)
		}
		issued := vocab.DateOf(env.OccurredAt)
		if due.Before(issued) {
			due = issued // filed late: due at once
		}
		return create(ctx, domain.PayableState{Company: domain.OrganizationID{UUID: declarant}, Authority: TaxAuthority, Kind: domain.Tax,
			Source: domain.Source{Type: filingSubmitted, ID: e.FilingID}, Document: fmt.Sprintf("Modelo %s %d-%s", e.Form, e.Year, e.Period),
			Issued: issued, Due: due, Currency: euro, Amount: amount})
	})
	messaging.Handle(c, func(ctx context.Context, e FilingReverted, _ app.Envelope) error {
		return withdraw(ctx, filingSubmitted, e.FilingID, "filing reverted: "+e.Reason)
	})

	byReference := func(ctx context.Context, company domain.OrganizationID, ref string) ([]*domain.Payment, error) {
		return s.Payments.Find(ctx, spec.And(domain.PmtFieldCompany.Eq(company), domain.PmtFieldReference.Eq(ref)))
	}
	messaging.Handle(c, func(ctx context.Context, e TransferExecuted, _ app.Envelope) error {
		debtor, err1 := fw.ParseUUID(e.Debtor)
		pid, err2 := domain.ParsePayableID(e.PayableID)
		on, err3 := vocab.ParseDate(e.ExecutedOn)
		amount, err4 := vocab.ParseDecimal(e.Amount)
		if err := errors.Join(err1, err2, err3, err4); err != nil {
			return invalid("treasury.transfer-executed.v1", err)
		}
		company := domain.OrganizationID{UUID: debtor}
		if found, err := byReference(ctx, company, e.EndToEnd); err != nil || len(found) > 0 {
			return err
		}
		b, err := s.Payables.Get(ctx, pid)
		if err != nil {
			return err
		}
		bs := b.State()
		if bs.Company != company {
			return fw.Violation("payments.invalid_event", "treasury.transfer-executed.v1: the payable is of another company")
		}
		p, err := domain.RegisterPayment(domain.NewPaymentID(), domain.PaymentState{Company: company, Payee: bs.Payee, Authority: bs.Authority, Date: on,
			Amount: amount, Currency: euro, Method: domain.Transfer, Reference: e.EndToEnd})
		if err != nil {
			return err
		}
		if err := s.apply(ctx, p, AllocationInput{Payable: pid.String(), Amount: e.Amount}, on); err != nil &&
			!errors.Is(err, fw.ErrRuleViolation) && !errors.Is(err, fw.ErrNotFound) {
			return err
		}
		return s.payments.Create(ctx, p)
	})
	messaging.Handle(c, func(ctx context.Context, e TransferRejected, _ app.Envelope) error {
		debtor, err := fw.ParseUUID(e.Debtor)
		if err != nil {
			return invalid("treasury.transfer-rejected.v1", err)
		}
		found, err := byReference(ctx, domain.OrganizationID{UUID: debtor}, e.EndToEnd)
		if err != nil {
			return err
		}
		for _, p := range found {
			if p.State().Cancelled {
				continue
			}
			if _, err := s.payments.Update(ctx, p.ID(), func(ctx context.Context, p *domain.Payment) error {
				back, err := p.Cancel()
				if err != nil {
					return err
				}
				return s.revert(ctx, back)
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// PayablePort implements contracts.Payable (it serves Treasury, not users).
type PayablePort struct{ Payables domain.PayableRepository }

var _ contracts.Payable = PayablePort{}

// DueForTransfer implements contracts.Payable.
func (p PayablePort) DueForTransfer(ctx context.Context, company, dueTo string) ([]contracts.DueItem, int, error) {
	var v fw.Validation
	cid := domain.OrganizationID{UUID: parseID(&v, "company", company)}
	to, err := vocab.ParseDate(dueTo)
	v.Require(err == nil, "dueTo", "format", "a date YYYY-MM-DD is required")
	if err := v.Err(); err != nil {
		return nil, 0, err
	}
	ps, err := p.Payables.Find(ctx, spec.And(domain.PayFieldCompany.Eq(cid), domain.PayFieldSettled.Eq(false), domain.PayFieldCancelled.Eq(false),
		domain.PayFieldDue.Le(to)), domain.PayFieldDue.Asc())
	if err != nil {
		return nil, 0, err
	}
	out, without := []contracts.DueItem{}, 0
	for _, b := range ps {
		s := b.State()
		if !s.Paid.IsZero() {
			continue // partly paid: a person decides the rest
		}
		if len(s.PayTo) == 0 {
			without++
			continue
		}
		it := contracts.DueItem{PayableID: b.ID().String(), Kind: s.Kind.String(), Document: s.Document, Payee: b.Payee(), Due: s.Due.String(),
			Amount: money(s.Amount)}
		for _, t := range s.PayTo {
			it.PayTo = append(it.PayTo, contracts.PayTo{IBAN: t.IBAN.String(), Amount: money(t.Amount)})
		}
		out = append(out, it)
	}
	return out, without, nil
}
