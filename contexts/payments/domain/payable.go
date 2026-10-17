// Package domain es el modelo de Payments: lo que debe una empresa (obligaciones de pago de
// facturas de proveedor, nóminas aprobadas y modelos fiscales presentados) y el dinero que paga,
// aplicado a ellas. El C# tenía un Payment con un estado numérico que nada hacía cumplir,
// aplicaciones a facturas que podían pagarlas de más, y ninguna obligación de pago.
package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// PayableKind es el nombre estable del tipo de agregado.
const PayableKind = "payments.payable"

// Kind es de dónde viene una obligación.
type Kind int

// Clases.
const (
	SupplierInvoice Kind = iota + 1 // lo que se paga al proveedor de una factura recibida (Purchases)
	Payroll                         // el líquido de una nómina aprobada
	Tax                             // el importe de un modelo fiscal presentado (Modelo 111)
)

var kinds = map[Kind]string{SupplierInvoice: "supplier-invoice", Payroll: "payroll", Tax: "tax"}

// String devuelve el nombre estable.
func (k Kind) String() string { return kinds[k] }

// ParseKind interpreta el nombre de una clase.
func ParseKind(s string) (Kind, bool) {
	for k, n := range kinds {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// Source es el hecho del que viene una obligación (único por empresa).
type Source struct {
	Type string // p. ej. "purchases.invoice-registered.v1", "payroll.payslip-approved.v1", "fiscal.filing-submitted.v1"
	ID   string
}

// PayTo es una cuenta bancaria a la que se paga una obligación por transferencia, y cuánto de ella.
type PayTo struct {
	IBAN   vocab.IBAN
	Amount vocab.Decimal
}

// PayableState es el estado persistido de una obligación.
type PayableState struct {
	Company     OrganizationID
	Payee       PartyID // cero para una administración tributaria
	Authority   string  // la administración tributaria (AEAT) cuando no hay party beneficiaria
	Kind        Kind
	Source      Source
	Document    string // número de factura de proveedor, referencia de nómina o de modelo fiscal
	Description string
	Issued      vocab.Date
	Due         vocab.Date
	Currency    vocab.CurrencyCode
	Amount      vocab.Decimal
	Paid        vocab.Decimal
	PayTo       []PayTo
	Cancelled   bool
	Audit       traits.AuditStamp
}

// Payable es lo que debe una empresa y cuándo.
type Payable struct {
	fw.BaseAggregateRoot[PayableID]
	traits.Audited
	s PayableState
}

func cents(d vocab.Decimal) bool { return d.Equal(d.Round(2)) }

func checkPayTo(v *fw.Validation, amount vocab.Decimal, to []PayTo) {
	if len(to) == 0 {
		return
	}
	sum := vocab.DecimalFromInt(0)
	for i, p := range to {
		v.Require(!p.IBAN.IsZero(), fmt.Sprintf("payTo[%d].iban", i), "required", "an IBAN is required")
		v.Require(p.Amount.IsPositive() && cents(p.Amount), fmt.Sprintf("payTo[%d].amount", i), "range", "a positive amount in cents")
		sum = sum.Add(p.Amount)
	}
	v.Require(len(to) <= 10, "payTo", "count", "at most 10 accounts")
	v.Require(sum.Equal(amount), "payTo", "sum", "the accounts add up to the amount")
}

// ReconstitutePayable reconstruye una obligación.
func ReconstitutePayable(id PayableID, s PayableState) (*Payable, error) {
	base, err := fw.NewBaseAggregateRoot(PayableKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	_, ok := kinds[s.Kind]
	v.Require(ok, "kind", "enum", "supplier-invoice, payroll or tax")
	v.Require(!s.Company.IsZero(), "company", "required", "the company is required")
	s.Authority = strings.TrimSpace(s.Authority)
	if s.Kind == Tax {
		v.Require(s.Authority != "" && len(s.Authority) <= 20, "authority", "required", "the tax authority is required")
	} else {
		v.Require(!s.Payee.IsZero(), "payee", "required", "the payee is required")
	}
	v.Require(s.Source.Type != "" && s.Source.ID != "" && len(s.Source.Type) <= 80 && len(s.Source.ID) <= 80, "source", "required", "the source is required")
	s.Document = strings.TrimSpace(s.Document)
	v.Require(s.Document != "" && utf8.RuneCountInString(s.Document) <= 60, "document", "length", "a document reference of 1 to 60 characters")
	s.Description = strings.TrimSpace(s.Description)
	v.Require(utf8.RuneCountInString(s.Description) <= 200, "description", "length", "at most 200 characters")
	v.Require(!s.Issued.IsZero() && !s.Due.IsZero() && !s.Due.Before(s.Issued), "due", "range", "a due date not before the issue date")
	v.Require(s.Currency.String() == "EUR", "currency", "supported", "only euros for now")
	v.Require(s.Amount.IsPositive() && cents(s.Amount), "amount", "range", "a positive amount in cents")
	v.Require(!s.Paid.IsNegative() && !s.Paid.GreaterThan(s.Amount), "paid", "range", "paid within the amount")
	checkPayTo(&v, s.Amount, s.PayTo)
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.PayTo = slices.Clone(s.PayTo)
	return &Payable{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// RegisterPayable registra una obligación nueva.
func RegisterPayable(id PayableID, s PayableState) (*Payable, error) {
	s.Paid, s.Cancelled = vocab.DecimalFromInt(0), false
	p, err := ReconstitutePayable(id, s)
	if err != nil {
		return nil, err
	}
	p.Raise(PayableRegistered{EventMeta: p.NewEventMeta(), Kind: s.Kind.String(), Document: p.s.Document, Amount: s.Amount.StringFixed(2),
		Due: s.Due.String()})
	return p, nil
}

// State devuelve el estado (las cuentas son una copia).
func (p *Payable) State() PayableState {
	s := p.s
	s.PayTo = slices.Clone(s.PayTo)
	return s
}

// Open devuelve lo que queda por pagar (cero cuando está anulada).
func (p *Payable) Open() vocab.Decimal {
	if p.s.Cancelled {
		return vocab.DecimalFromInt(0)
	}
	return p.s.Amount.Sub(p.s.Paid)
}

// Settled dice si está todo pagado.
func (p *Payable) Settled() bool { return p.s.Paid.Equal(p.s.Amount) }

// Payee nombra a quién se paga: la party, o la administración tributaria.
func (p *Payable) Payee() string {
	if p.s.Kind == Tax {
		return p.s.Authority
	}
	return p.s.Payee.String()
}

// Apply registra un importe pagado. Invariantes: una obligación activa, céntimos positivos, nunca
// más de lo abierto (el C# dejaba pagar una factura de más).
func (p *Payable) Apply(amount vocab.Decimal) error {
	if p.s.Cancelled {
		return fw.Violation("payments.payable_cancelled", "the payable is cancelled")
	}
	if !amount.IsPositive() || !cents(amount) {
		return fw.Violation("payments.amount", "a positive amount in cents")
	}
	if amount.GreaterThan(p.Open()) {
		return fw.Violation("payments.over_paid", "the amount exceeds what is open")
	}
	p.s.Paid = p.s.Paid.Add(amount)
	if p.Settled() {
		p.Raise(PayableSettled{EventMeta: p.NewEventMeta(), Company: p.s.Company.String(), Kind: p.s.Kind.String(), Document: p.s.Document})
	}
	return nil
}

// Unapply revierte un importe pagado (un pago anulado o una transferencia rechazada).
func (p *Payable) Unapply(amount vocab.Decimal) error {
	if !amount.IsPositive() || amount.GreaterThan(p.s.Paid) {
		return fw.Violation("payments.unapply", "cannot reverse more than was paid")
	}
	p.s.Paid = p.s.Paid.Sub(amount)
	return nil
}

// Cancel retira una obligación de la que aún no se ha pagado nada (una nómina anulada, un modelo
// fiscal revertido, una factura de proveedor registrada por error). Una pagada en parte se liquida
// o se reembolsa antes.
func (p *Payable) Cancel(reason string) error {
	if p.s.Cancelled {
		return fw.Violation("payments.payable_cancelled", "the payable is already cancelled")
	}
	if p.s.Paid.IsPositive() {
		return fw.Violation("payments.payable_paid", "the payable has payments: cancel them first")
	}
	p.s.Cancelled = true
	p.Raise(PayableCancelled{EventMeta: p.NewEventMeta(), Kind: p.s.Kind.String(), Document: p.s.Document, Reason: strings.TrimSpace(reason)})
	return nil
}

// SetPayTo sustituye las cuentas a las que se paga la obligación (vacío: sin datos de transferencia).
func (p *Payable) SetPayTo(to []PayTo) error {
	if p.s.Cancelled || p.Settled() {
		return fw.Violation("payments.payable_closed", "the payable is cancelled or settled")
	}
	var v fw.Validation
	checkPayTo(&v, p.s.Amount, to)
	if err := v.Err(); err != nil {
		return err
	}
	p.s.PayTo = slices.Clone(to)
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (p *Payable) AuditSnapshot() map[string]any {
	return map[string]any{"document": p.s.Document, "amount": p.s.Amount.String(), "paid": p.s.Paid.String(), "cancelled": p.s.Cancelled}
}

// TaxDue devuelve el último día para pagar un modelo fiscal autoliquidado de un periodo (código
// AEAT: 01–12 o 1T–4T): el día 20 del mes siguiente al periodo, y el 30 de enero para el último.
func TaxDue(year int, period string) (vocab.Date, error) {
	var month int
	switch {
	case len(period) == 2 && period[1] == 'T' && period[0] >= '1' && period[0] <= '4':
		month = int(period[0]-'0') * 3
	case len(period) == 2 && period >= "01" && period <= "12" && period[1] >= '0' && period[1] <= '9':
		month = int(period[0]-'0')*10 + int(period[1]-'0')
	default:
		return vocab.Date{}, fw.Violation("payments.tax_period", "the period "+period+" has no payment")
	}
	if month == 12 {
		return vocab.NewDate(year+1, 1, 30)
	}
	return vocab.NewDate(year, time.Month(month+1), 20)
}

// Campos de la obligación.
var (
	PayFieldID        = spec.Comparable("id", func(p *Payable) PayableID { return p.ID() })
	PayFieldCompany   = spec.Comparable("company", func(p *Payable) OrganizationID { return p.s.Company })
	PayFieldPayee     = spec.Comparable("payee", func(p *Payable) PartyID { return p.s.Payee })
	PayFieldKind      = spec.Comparable("kind", func(p *Payable) string { return p.s.Kind.String() })
	PayFieldSrcType   = spec.Comparable("source_type", func(p *Payable) string { return p.s.Source.Type })
	PayFieldSrcID     = spec.Comparable("source_id", func(p *Payable) string { return p.s.Source.ID })
	PayFieldDocument  = spec.Comparable("document", func(p *Payable) string { return p.s.Document })
	PayFieldDue       = spec.OrderedBy("due_on", func(p *Payable) vocab.Date { return p.s.Due }, vocab.CompareDates)
	PayFieldSettled   = spec.Comparable("settled", (*Payable).Settled)
	PayFieldCancelled = spec.Comparable("cancelled", func(p *Payable) bool { return p.s.Cancelled })
)
