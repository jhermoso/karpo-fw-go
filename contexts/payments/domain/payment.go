package domain

import (
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// PaymentKind es el nombre estable del tipo de agregado.
const PaymentKind = "payments.payment"

// Method es cómo se pagó el dinero (la siembra payment_method_type de C# más la domiciliación, un
// proveedor que carga en la cuenta de la empresa).
type Method int

// Formas de pago.
const (
	Cash Method = iota + 1
	Transfer
	Card
	Check
	DirectDebit
)

var methods = map[Method]string{Cash: "cash", Transfer: "transfer", Card: "card", Check: "check", DirectDebit: "direct-debit"}

// String devuelve el nombre estable.
func (m Method) String() string { return methods[m] }

// ParseMethod interpreta el nombre de una forma de pago.
func ParseMethod(s string) (Method, bool) {
	for k, n := range methods {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// Allocation aplica parte de un pago a una obligación.
type Allocation struct {
	ID      AllocationID
	Payable PayableID
	Kind    Kind // de la obligación, para el Published Language
	Amount  vocab.Decimal
	On      vocab.Date
}

// PaymentState es el estado persistido de un pago.
type PaymentState struct {
	Company     OrganizationID
	Payee       PartyID // cero para una administración tributaria
	Authority   string
	Date        vocab.Date
	Amount      vocab.Decimal
	Currency    vocab.CurrencyCode
	Method      Method
	Reference   string // id end-to-end de una transferencia, número de cheque…
	Cancelled   bool
	Allocations []Allocation
	Audit       traits.AuditStamp
}

// Payment es dinero que paga una empresa (el Payment de C#, cuyo importe podía bajar de lo
// aplicado y que se podía editar o borrar después de emitido). No se borra nunca: se anula, y sus
// aplicaciones se revierten.
type Payment struct {
	fw.BaseAggregateRoot[PaymentID]
	traits.Audited
	s PaymentState
}

// ReconstitutePayment reconstruye un pago.
func ReconstitutePayment(id PaymentID, s PaymentState) (*Payment, error) {
	base, err := fw.NewBaseAggregateRoot(PaymentKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero(), "company", "required", "the company is required")
	s.Authority = strings.TrimSpace(s.Authority)
	v.Require(s.Payee.IsZero() != (s.Authority == ""), "payee", "required", "a payee party or a tax authority")
	v.Require(len(s.Authority) <= 20, "authority", "length", "at most 20 characters")
	v.Require(!s.Date.IsZero(), "date", "required", "the date is required")
	_, ok := methods[s.Method]
	v.Require(ok, "method", "enum", "cash, transfer, card, check or direct-debit")
	v.Require(s.Amount.IsPositive() && cents(s.Amount), "amount", "range", "a positive amount in cents")
	v.Require(s.Currency.String() == "EUR", "currency", "supported", "only euros for now")
	s.Reference = strings.TrimSpace(s.Reference)
	v.Require(utf8.RuneCountInString(s.Reference) <= 100, "reference", "length", "at most 100 characters")
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Allocations = slices.Clone(s.Allocations)
	return &Payment{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// RegisterPayment registra dinero pagado.
func RegisterPayment(id PaymentID, s PaymentState) (*Payment, error) {
	s.Cancelled, s.Allocations = false, nil
	p, err := ReconstitutePayment(id, s)
	if err != nil {
		return nil, err
	}
	p.Raise(PaymentRegistered{EventMeta: p.NewEventMeta(), Payee: p.payee(), Amount: s.Amount.StringFixed(2), Method: s.Method.String()})
	return p, nil
}

func (p *Payment) payee() string {
	if p.s.Payee.IsZero() {
		return p.s.Authority
	}
	return p.s.Payee.String()
}

// State devuelve el estado (las aplicaciones son una copia).
func (p *Payment) State() PaymentState {
	s := p.s
	s.Allocations = slices.Clone(s.Allocations)
	return s
}

// Allocated devuelve la suma de las aplicaciones.
func (p *Payment) Allocated() vocab.Decimal {
	a := vocab.DecimalFromInt(0)
	for _, x := range p.s.Allocations {
		a = a.Add(x.Amount)
	}
	return a
}

// Unallocated devuelve lo que queda por aplicar.
func (p *Payment) Unallocated() vocab.Decimal { return p.s.Amount.Sub(p.Allocated()) }

// Allocate aplica parte del pago a una obligación del mismo beneficiario (lo comprueba quien llama,
// que también lo aplica en la obligación). Las aplicaciones nunca superan el importe.
func (p *Payment) Allocate(to PayableID, kind Kind, amount vocab.Decimal, on vocab.Date) (AllocationID, error) {
	if p.s.Cancelled {
		return AllocationID{}, fw.Violation("payments.payment_cancelled", "the payment is cancelled")
	}
	if !amount.IsPositive() || !cents(amount) || amount.GreaterThan(p.Unallocated()) {
		return AllocationID{}, fw.Violation("payments.over_allocated", "the allocations exceed the amount paid")
	}
	a := Allocation{ID: AllocationID{fw.NewUUID()}, Payable: to, Kind: kind, Amount: amount, On: on}
	p.s.Allocations = append(slices.Clone(p.s.Allocations), a)
	p.Raise(PaymentAllocated{EventMeta: p.NewEventMeta(), Company: p.s.Company.String(), Payee: p.payee(), Method: p.s.Method.String(),
		Payable: to.String(), Kind: kind.String(), Amount: amount.StringFixed(2), On: on.String()})
	return a.ID, nil
}

// Deallocate revierte una aplicación y la devuelve (quien llama revierte la obligación).
func (p *Payment) Deallocate(id AllocationID) (Allocation, error) {
	k := slices.IndexFunc(p.s.Allocations, func(a Allocation) bool { return a.ID == id })
	if k < 0 {
		return Allocation{}, fw.NotFound("payments.allocation", id)
	}
	a := p.s.Allocations[k]
	p.s.Allocations = slices.Delete(slices.Clone(p.s.Allocations), k, k+1)
	p.raiseReversed(a)
	return a, nil
}

func (p *Payment) raiseReversed(a Allocation) {
	p.Raise(AllocationReversed{EventMeta: p.NewEventMeta(), Company: p.s.Company.String(), Payee: p.payee(), Payable: a.Payable.String(),
		Kind: a.Kind.String(), Amount: a.Amount.StringFixed(2)})
}

// Cancel anula el pago y devuelve sus aplicaciones para que quien llama las revierta.
func (p *Payment) Cancel() ([]Allocation, error) {
	if p.s.Cancelled {
		return nil, fw.Violation("payments.payment_cancelled", "the payment is already cancelled")
	}
	out := slices.Clone(p.s.Allocations)
	for _, a := range out {
		p.raiseReversed(a)
	}
	p.s.Cancelled, p.s.Allocations = true, nil
	return out, nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (p *Payment) AuditSnapshot() map[string]any {
	return map[string]any{"amount": p.s.Amount.String(), "allocated": p.Allocated().String(), "cancelled": p.s.Cancelled}
}

// Campos del pago.
var (
	PmtFieldCompany   = spec.Comparable("company", func(p *Payment) OrganizationID { return p.s.Company })
	PmtFieldPayee     = spec.Comparable("payee", func(p *Payment) PartyID { return p.s.Payee })
	PmtFieldDate      = spec.OrderedBy("paid_on", func(p *Payment) vocab.Date { return p.s.Date }, vocab.CompareDates)
	PmtFieldCancelled = spec.Comparable("cancelled", func(p *Payment) bool { return p.s.Cancelled })
	PmtFieldReference = spec.Comparable("reference", func(p *Payment) string { return p.s.Reference })
)
