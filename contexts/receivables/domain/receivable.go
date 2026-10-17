package domain

import (
	"slices"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// ReceivableKind es el nombre estable del tipo de agregado.
const ReceivableKind = "receivables.receivable"

// Installment es un vencimiento de un cobro pendiente.
type Installment struct {
	No        int
	Due       vocab.Date
	Amount    vocab.Decimal // negativo en los abonos (facturas rectificativas)
	Collected vocab.Decimal // mismo signo que Amount
}

// Open devuelve lo que queda por cobrar (mismo signo que Amount).
func (i Installment) Open() vocab.Decimal { return i.Amount.Sub(i.Collected) }

// ReceivableState es el estado persistido de un cobro pendiente.
type ReceivableState struct {
	Seller       OrganizationID
	Customer     PartyID
	Number       string
	Issued       vocab.Date
	Currency     vocab.CurrencyCode
	Total        vocab.Decimal
	Installments []Installment
	Audit        traits.AuditStamp
}

// Receivable es lo que un cliente debe por una factura emitida (o lo que el vendedor debe devolver por
// una factura rectificativa), repartido en vencimientos. El C# no tenía cobros pendientes: nada
// calculaba vencimientos y nada marcaba nunca una factura como pagada.
type Receivable struct {
	fw.BaseAggregateRoot[ReceivableID]
	traits.Audited
	s ReceivableState
}

// ReconstituteReceivable reconstruye un cobro pendiente.
func ReconstituteReceivable(id ReceivableID, s ReceivableState) (*Receivable, error) {
	base, err := fw.NewBaseAggregateRoot(ReceivableKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Seller.IsZero() && !s.Customer.IsZero(), "customer", "required", "seller and customer are required")
	v.Require(s.Number != "" && !s.Issued.IsZero(), "number", "required", "the invoice number and date are required")
	v.Require(!s.Total.IsZero() && s.Total.Equal(s.Total.Round(2)), "total", "range", "a non-zero total in cents")
	v.Require(len(s.Installments) > 0, "installments", "required", "at least one installment")
	sum := vocab.DecimalFromInt(0)
	for k, i := range s.Installments {
		sum = sum.Add(i.Amount)
		v.Require(i.No == k+1 && !i.Due.IsZero(), "installments", "order", "installments numbered from 1 with a due date")
		v.Require(i.Amount.Sign() == s.Total.Sign() && (i.Collected.IsZero() || i.Collected.Sign() == s.Total.Sign()) &&
			!i.Collected.Abs().GreaterThan(i.Amount.Abs()), "installments", "range", "amounts with the sign of the total, collected within the amount")
	}
	v.Require(sum.Equal(s.Total), "installments", "sum", "the installments add up to the total")
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Installments = slices.Clone(s.Installments)
	return &Receivable{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// OpenReceivable crea el cobro pendiente de una factura con sus vencimientos.
func OpenReceivable(invoice ReceivableID, s ReceivableState, schedule []Due) (*Receivable, error) {
	s.Installments = nil
	for k, d := range schedule {
		s.Installments = append(s.Installments, Installment{No: k + 1, Due: d.Date, Amount: d.Amount, Collected: vocab.DecimalFromInt(0)})
	}
	r, err := ReconstituteReceivable(invoice, s)
	if err != nil {
		return nil, err
	}
	r.Raise(ReceivableOpened{EventMeta: r.NewEventMeta(), Customer: s.Customer.String(), Number: s.Number, Total: s.Total.StringFixed(2)})
	return r, nil
}

// State devuelve el estado (los vencimientos son una copia).
func (r *Receivable) State() ReceivableState {
	s := r.s
	s.Installments = slices.Clone(s.Installments)
	return s
}

// Open devuelve lo que queda por cobrar.
func (r *Receivable) Open() vocab.Decimal {
	o := vocab.DecimalFromInt(0)
	for _, i := range r.s.Installments {
		o = o.Add(i.Open())
	}
	return o
}

// Overdue devuelve lo que queda por cobrar de los vencimientos anteriores a una fecha.
func (r *Receivable) Overdue(on vocab.Date) vocab.Decimal {
	o := vocab.DecimalFromInt(0)
	for _, i := range r.s.Installments {
		if i.Due.Before(on) {
			o = o.Add(i.Open())
		}
	}
	return o
}

// Settled dice si no queda nada por cobrar.
func (r *Receivable) Settled() bool { return r.Open().IsZero() }

// Apply anota un importe contra un vencimiento. Invariantes (el C# sólo ponía tope por el lado del
// pago): el importe tiene el signo del vencimiento y no supera lo que queda abierto.
func (r *Receivable) Apply(no int, amount vocab.Decimal) error {
	k := slices.IndexFunc(r.s.Installments, func(i Installment) bool { return i.No == no })
	if k < 0 {
		return fw.NotFound("receivables.installment", fwInt(no))
	}
	i := r.s.Installments[k]
	if amount.IsZero() || amount.Sign() != i.Amount.Sign() || !amount.Equal(amount.Round(2)) {
		return fw.Violation("receivables.amount_sign", "the amount must be in cents and have the sign of the installment")
	}
	if amount.Abs().GreaterThan(i.Open().Abs()) {
		return fw.Violation("receivables.over_collected", "the amount exceeds what is open in the installment")
	}
	r.s.Installments = slices.Clone(r.s.Installments)
	r.s.Installments[k].Collected = i.Collected.Add(amount)
	if r.Settled() {
		r.Raise(ReceivableSettled{EventMeta: r.NewEventMeta(), Customer: r.s.Customer.String(), Number: r.s.Number})
	}
	return nil
}

// Unapply deshace un importe anotado contra un vencimiento.
func (r *Receivable) Unapply(no int, amount vocab.Decimal) error {
	k := slices.IndexFunc(r.s.Installments, func(i Installment) bool { return i.No == no })
	if k < 0 {
		return fw.NotFound("receivables.installment", fwInt(no))
	}
	i := r.s.Installments[k]
	if amount.Sign() != i.Amount.Sign() || amount.Abs().GreaterThan(i.Collected.Abs()) {
		return fw.Violation("receivables.unapply", "cannot reverse more than was collected")
	}
	r.s.Installments = slices.Clone(r.s.Installments)
	r.s.Installments[k].Collected = i.Collected.Sub(amount)
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (r *Receivable) AuditSnapshot() map[string]any {
	return map[string]any{"number": r.s.Number, "open": r.Open().String()}
}

type fwInt int

func (i fwInt) String() string { return vocab.DecimalFromInt(int64(i)).String() }

// Campos del cobro pendiente.
var (
	RecFieldID       = spec.Comparable("id", func(r *Receivable) ReceivableID { return r.ID() })
	RecFieldSeller   = spec.Comparable("seller", func(r *Receivable) OrganizationID { return r.s.Seller })
	RecFieldCustomer = spec.Comparable("customer", func(r *Receivable) PartyID { return r.s.Customer })
	RecFieldSettled  = spec.Comparable("settled", (*Receivable).Settled)
	RecFieldIssued   = spec.OrderedBy("issued", func(r *Receivable) vocab.Date { return r.s.Issued }, vocab.CompareDates)
)

// Repositorios y puertos del contexto.
type (
	TermsRepository         = fw.Repository[TermsID, *Terms]
	ReceivableRepository    = fw.Repository[ReceivableID, *Receivable]
	CollectionRepository    = fw.Repository[CollectionID, *Collection]
	CreditProfileRepository = fw.Repository[CreditProfileID, *CreditProfile]
)
