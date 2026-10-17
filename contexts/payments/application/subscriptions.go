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

// Las copias de Payments de los eventos que consume (sólo los campos que necesita).

// PayslipApproved es payroll.payslip-approved.v1.
type PayslipApproved struct {
	PayslipID   string `json:"payslipId"`
	Person      string `json:"person"`
	Employer    string `json:"employer"`
	PeriodEnd   string `json:"periodEnd"`
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

// InvoiceRegistered es purchases.invoice-registered.v1.
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

// IntegrationEventType implementa application.IntegrationEvent.
func (InvoiceRegistered) IntegrationEventType() string { return "purchases.invoice-registered.v1" }

// InvoiceCancelled es purchases.invoice-cancelled.v1.
type InvoiceCancelled struct {
	InvoiceID string `json:"invoiceId"`
	Reason    string `json:"reason"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (InvoiceCancelled) IntegrationEventType() string { return "purchases.invoice-cancelled.v1" }

// TransferExecuted es treasury.transfer-executed.v1.
type TransferExecuted struct {
	EndToEnd   string `json:"endToEnd"`
	Debtor     string `json:"debtor"`
	PayableID  string `json:"payableId"`
	Amount     string `json:"amount"`
	ExecutedOn string `json:"executedOn"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (TransferExecuted) IntegrationEventType() string { return "treasury.transfer-executed.v1" }

// TransferRejected es treasury.transfer-rejected.v1.
type TransferRejected struct {
	EndToEnd string `json:"endToEnd"`
	Debtor   string `json:"debtor"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (TransferRejected) IntegrationEventType() string { return "treasury.transfer-rejected.v1" }

// TaxAuthority es el beneficiario de las obligaciones fiscales de la fase 1 (modelos españoles).
const TaxAuthority = "AEAT"

func invalid(event string, err error) error {
	return fw.Violation("payments.invalid_event", event+": "+err.Error())
}

// Subscribe registra las reacciones a los demás contextos (decisiones aprobadas de
// docs/NOMINAS.md, docs/FISCAL.md, docs/TESORERIA.md y docs/COMPRAS.md: cada uno publica, Payments
// registra lo que se debe y Treasury ejecuta las transferencias):
//
//   - una factura recibida debe lo que se paga al proveedor (su total menos la retención) en su
//     fecha de vencimiento, a las cuentas que lleva; una rectificativa (negativa) no debe nada
//     aquí; una factura anulada retira esa obligación mientras no esté pagada;
//   - una nómina aprobada debe su líquido al empleado en su fecha de pago, a las cuentas de su
//     reparto (el puerto Remittance de Payroll); una anulada retira esa obligación mientras no esté pagada;
//   - un modelo fiscal presentado con importe lo debe a la administración tributaria en su plazo
//     legal; uno revertido retira esa obligación mientras no esté pagado;
//   - una transferencia ejecutada se convierte en un pago por transferencia aplicado a su
//     obligación (sin aplicar si la obligación se pagó entretanto, para que decida una persona);
//     una rechazada anula ese pago.
//
// Los mensajes entregados otra vez no hacen daño: las obligaciones son únicas por origen y los pagos por
// referencia.
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
				continue // una obligación pagada la reembolsa una persona, no se retira
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
			return nil // un abono del proveedor: las compensaciones son de la fase 2
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
				st.PayTo = splits[e.PayslipID] // un reparto que no cuadra se deja para una persona
			}
		}
		return create(ctx, st)
	})
	messaging.Handle(c, func(ctx context.Context, e PayslipCancelled, _ app.Envelope) error {
		return withdraw(ctx, payslipApproved, e.PayslipID, "payslip cancelled: "+e.Reason)
	})

	const filingSubmitted = "fiscal.filing-submitted.v1"
	messaging.Handle(c, func(ctx context.Context, e FilingSubmitted, env app.Envelope) error {
		if strings.TrimSpace(e.Form) == "190" { // informativo: nada que pagar
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
			return nil // un periodo sin pago (resúmenes anuales)
		}
		issued := vocab.DateOf(env.OccurredAt)
		if due.Before(issued) {
			due = issued // presentado tarde: vence en el acto
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

// PayablePort implementa contracts.Payable (sirve a Treasury, no a usuarios).
type PayablePort struct{ Payables domain.PayableRepository }

var _ contracts.Payable = PayablePort{}

// DueForTransfer implementa contracts.Payable.
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
			continue // pagada en parte: el resto lo decide una persona
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
