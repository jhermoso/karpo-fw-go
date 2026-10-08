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

// CollectionKind is the stable aggregate type name.
const CollectionKind = "receivables.collection"

// Method is how a collection was received (the C# payment_method_type seed plus direct debit and
// offset).
type Method int

// Methods.
const (
	Cash Method = iota + 1
	Transfer
	Card
	Check
	DirectDebit
	Offset // a credit (corrective invoice) netted against open installments
)

var methods = map[Method]string{Cash: "cash", Transfer: "transfer", Card: "card", Check: "check", DirectDebit: "direct-debit", Offset: "offset"}

// String returns the stable name.
func (m Method) String() string { return methods[m] }

// ParseMethod parses a method name.
func ParseMethod(s string) (Method, bool) {
	for k, n := range methods {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// AllocationID identifies an allocation.
type AllocationID struct{ fw.UUID }

// Allocation applies part of a collection to an installment of a receivable.
type Allocation struct {
	ID          AllocationID
	Receivable  ReceivableID
	Installment int
	Amount      vocab.Decimal
	On          vocab.Date
}

// CollectionState is the persisted state of a collection.
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

// Collection is money received from a customer (the C# Payment + PaymentApplication, which let an
// invoice be over-collected, a payment reduced below what was applied, and a payment change after
// being issued).
type Collection struct {
	fw.BaseAggregateRoot[CollectionID]
	traits.Audited
	s CollectionState
}

// ReconstituteCollection rebuilds a collection.
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

// RegisterCollection records money received.
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

// State returns the state (allocations are a copy).
func (c *Collection) State() CollectionState {
	s := c.s
	s.Allocations = slices.Clone(s.Allocations)
	return s
}

// Allocated returns the sum of the allocations.
func (c *Collection) Allocated() vocab.Decimal {
	a := vocab.DecimalFromInt(0)
	for _, x := range c.s.Allocations {
		a = a.Add(x.Amount)
	}
	return a
}

// Unallocated returns what remains to allocate.
func (c *Collection) Unallocated() vocab.Decimal { return c.s.Amount.Sub(c.Allocated()) }

// Allocate applies part of the collection to an installment. Invariants: an active collection,
// and the allocations never exceed the amount (an offset allocates positive and negative amounts
// that must balance, checked with Balanced). The receivable checks its own side.
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

// Balanced reports whether an offset nets to zero.
func (c *Collection) Balanced() bool { return c.s.Method != Offset || c.Allocated().IsZero() }

// Deallocate reverses an allocation and returns it (the receivable is reverted by the caller).
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

// Cancel cancels the collection and returns its allocations for the caller to revert (a
// collection is never deleted).
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

// AuditSnapshot implements traits.Snapshotter.
func (c *Collection) AuditSnapshot() map[string]any {
	return map[string]any{"amount": c.s.Amount.String(), "allocated": c.Allocated().String(), "cancelled": c.s.Cancelled}
}

// Collection fields.
var (
	ColFieldSeller    = spec.Comparable("seller", func(c *Collection) OrganizationID { return c.s.Seller })
	ColFieldPayer     = spec.Comparable("payer", func(c *Collection) PartyID { return c.s.Payer })
	ColFieldDate      = spec.OrderedBy("collected_on", func(c *Collection) vocab.Date { return c.s.Date }, vocab.CompareDates)
	ColFieldCancelled = spec.Comparable("cancelled", func(c *Collection) bool { return c.s.Cancelled })
	ColFieldReference = spec.Comparable("reference", func(c *Collection) string { return c.s.Reference })
)
