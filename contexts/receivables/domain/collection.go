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

// CollectionKind es el nombre estable del tipo de agregado.
const CollectionKind = "receivables.collection"

// Method es cómo se recibió un cobro (la siembra payment_method_type del C# más el adeudo directo y
// la compensación).
type Method int

// Medios.
const (
	Cash Method = iota + 1
	Transfer
	Card
	Check
	DirectDebit
	Offset // un abono (factura rectificativa) compensado con vencimientos abiertos
)

var methods = map[Method]string{Cash: "cash", Transfer: "transfer", Card: "card", Check: "check", DirectDebit: "direct-debit", Offset: "offset"}

// String devuelve el nombre estable.
func (m Method) String() string { return methods[m] }

// ParseMethod analiza el nombre de un medio.
func ParseMethod(s string) (Method, bool) {
	for k, n := range methods {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// AllocationID identifica una aplicación.
type AllocationID struct{ fw.UUID }

// Allocation aplica parte de un cobro a un vencimiento de un cobro pendiente.
type Allocation struct {
	ID          AllocationID
	Receivable  ReceivableID
	Installment int
	Amount      vocab.Decimal
	On          vocab.Date
}

// CollectionState es el estado persistido de un cobro.
type CollectionState struct {
	Seller      OrganizationID
	Payer       PartyID
	Date        vocab.Date
	Amount      vocab.Decimal
	Currency    vocab.CurrencyCode
	Method      Method
	Reference   string
	Cancelled   bool
	Allocations []Allocation
	Audit       traits.AuditStamp
}

// Collection es dinero recibido de un cliente (el Payment + PaymentApplication del C#, que dejaban
// cobrar una factura de más, reducir un pago por debajo de lo aplicado y cambiar un pago después de
// emitido).
type Collection struct {
	fw.BaseAggregateRoot[CollectionID]
	traits.Audited
	s CollectionState
}

// ReconstituteCollection reconstruye un cobro.
func ReconstituteCollection(id CollectionID, s CollectionState) (*Collection, error) {
	base, err := fw.NewBaseAggregateRoot(CollectionKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Seller.IsZero() && !s.Payer.IsZero(), "payer", "required", "seller and payer are required")
	v.Require(!s.Date.IsZero(), "date", "required", "the date is required")
	_, ok := methods[s.Method]
	v.Require(ok, "method", "enum", "unknown method")
	if s.Method == Offset {
		v.Require(s.Amount.IsZero(), "amount", "offset", "an offset moves no money")
	} else {
		v.Require(s.Amount.IsPositive() && s.Amount.Equal(s.Amount.Round(2)), "amount", "range", "a positive amount in cents")
	}
	v.Require(s.Currency.String() == "EUR", "currency", "supported", "only euros for now")
	s.Reference = strings.TrimSpace(s.Reference)
	v.Require(utf8.RuneCountInString(s.Reference) <= 100, "reference", "length", "at most 100 characters")
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Allocations = slices.Clone(s.Allocations)
	return &Collection{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// RegisterCollection anota dinero recibido.
func RegisterCollection(id CollectionID, s CollectionState) (*Collection, error) {
	s.Cancelled, s.Allocations = false, nil
	c, err := ReconstituteCollection(id, s)
	if err != nil {
		return nil, err
	}
	if s.Method != Offset {
		c.Raise(CollectionRegistered{EventMeta: c.NewEventMeta(), Payer: s.Payer.String(), Amount: s.Amount.StringFixed(2), Method: s.Method.String()})
	}
	return c, nil
}

// State devuelve el estado (las aplicaciones son una copia).
func (c *Collection) State() CollectionState {
	s := c.s
	s.Allocations = slices.Clone(s.Allocations)
	return s
}

// Allocated devuelve la suma de las aplicaciones.
func (c *Collection) Allocated() vocab.Decimal {
	a := vocab.DecimalFromInt(0)
	for _, x := range c.s.Allocations {
		a = a.Add(x.Amount)
	}
	return a
}

// Unallocated devuelve lo que queda por aplicar.
func (c *Collection) Unallocated() vocab.Decimal { return c.s.Amount.Sub(c.Allocated()) }

// Allocate aplica parte del cobro a un vencimiento. Invariantes: un cobro activo, y las
// aplicaciones nunca superan el importe (una compensación aplica importes positivos y negativos que
// deben cuadrar, lo que se comprueba con Balanced). El cobro pendiente comprueba su propio lado.
func (c *Collection) Allocate(r ReceivableID, installment int, amount vocab.Decimal, on vocab.Date) (AllocationID, error) {
	if c.s.Cancelled {
		return AllocationID{}, fw.Violation("receivables.collection_cancelled", "the collection is cancelled")
	}
	if c.s.Method != Offset && (!amount.IsPositive() || amount.GreaterThan(c.Unallocated())) {
		return AllocationID{}, fw.Violation("receivables.over_allocated", "the allocations exceed the amount collected")
	}
	a := Allocation{ID: AllocationID{fw.NewUUID()}, Receivable: r, Installment: installment, Amount: amount, On: on}
	c.s.Allocations = append(slices.Clone(c.s.Allocations), a)
	c.Raise(CollectionAllocated{EventMeta: c.NewEventMeta(), Seller: c.s.Seller.String(), Payer: c.s.Payer.String(), Method: c.s.Method.String(), Receivable: r.String(), Installment: installment, Amount: amount.StringFixed(2),
		On: on.String()})
	return a.ID, nil
}

// Balanced dice si una compensación suma cero.
func (c *Collection) Balanced() bool { return c.s.Method != Offset || c.Allocated().IsZero() }

// Deallocate deshace una aplicación y la devuelve (el cobro pendiente lo revierte quien llama).
func (c *Collection) Deallocate(id AllocationID) (Allocation, error) {
	if c.s.Method == Offset {
		return Allocation{}, fw.Violation("receivables.offset_reversal", "an offset is reversed as a whole: cancel it")
	}
	k := slices.IndexFunc(c.s.Allocations, func(a Allocation) bool { return a.ID == id })
	if k < 0 {
		return Allocation{}, fw.NotFound("receivables.allocation", id)
	}
	a := c.s.Allocations[k]
	c.s.Allocations = slices.Delete(slices.Clone(c.s.Allocations), k, k+1)
	c.Raise(AllocationReversed{EventMeta: c.NewEventMeta(), Seller: c.s.Seller.String(), Payer: c.s.Payer.String(), Receivable: a.Receivable.String(), Installment: a.Installment, Amount: a.Amount.StringFixed(2)})
	return a, nil
}

// Cancel anula el cobro y devuelve sus aplicaciones para que las revierta quien llama (un cobro
// nunca se borra).
func (c *Collection) Cancel() ([]Allocation, error) {
	if c.s.Cancelled {
		return nil, fw.Violation("receivables.collection_cancelled", "the collection is already cancelled")
	}
	out := slices.Clone(c.s.Allocations)
	for _, a := range out {
		c.Raise(AllocationReversed{EventMeta: c.NewEventMeta(), Seller: c.s.Seller.String(), Payer: c.s.Payer.String(), Receivable: a.Receivable.String(), Installment: a.Installment, Amount: a.Amount.StringFixed(2)})
	}
	c.s.Cancelled, c.s.Allocations = true, nil
	return out, nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (c *Collection) AuditSnapshot() map[string]any {
	return map[string]any{"amount": c.s.Amount.String(), "allocated": c.Allocated().String(), "cancelled": c.s.Cancelled}
}

// Campos del cobro.
var (
	ColFieldSeller    = spec.Comparable("seller", func(c *Collection) OrganizationID { return c.s.Seller })
	ColFieldPayer     = spec.Comparable("payer", func(c *Collection) PartyID { return c.s.Payer })
	ColFieldDate      = spec.OrderedBy("collected_on", func(c *Collection) vocab.Date { return c.s.Date }, vocab.CompareDates)
	ColFieldCancelled = spec.Comparable("cancelled", func(c *Collection) bool { return c.s.Cancelled })
	ColFieldReference = spec.Comparable("reference", func(c *Collection) string { return c.s.Reference })
)
