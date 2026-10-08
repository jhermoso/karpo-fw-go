// Package domain is the Purchases model of phase 1: the invoices a company receives from its
// suppliers (registered with their tax breakdown and professional withholding, numbered in the
// register of received invoices) and the purchase profile of each supplier. The C# had a single
// "Purchase Invoice" seed row: no supplier number, no input VAT, no withholding, and every invoice
// lost its billed parties when saved.
package domain

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// InvoiceKind is the stable aggregate type name.
const InvoiceKind = "purchases.received_invoice"

// MaxLines bounds the lines of an invoice.
const MaxLines = 500

// Category is the nature of what was bought: Accounting posts each to its expense account (the
// PGC subgroups 60 and 62).
type Category string

// Categories.
const (
	Goods                Category = "goods"                 // 600
	Rent                 Category = "rent"                  // 621
	Repairs              Category = "repairs"               // 622
	ProfessionalServices Category = "professional-services" // 623
	Transport            Category = "transport"             // 624
	Insurance            Category = "insurance"             // 625
	Advertising          Category = "advertising"           // 627
	Supplies             Category = "supplies"              // 628
	OtherServices        Category = "other-services"        // 629
	FixedAsset           Category = "fixed-asset"           // 21x: an investment, not an expense
)

// Categories lists the valid categories.
var Categories = []Category{Goods, Rent, Repairs, ProfessionalServices, Transport, Insurance, Advertising, Supplies, OtherServices, FixedAsset}

// ValidCategory reports whether c is a category.
func ValidCategory(c Category) bool { return slices.Contains(Categories, c) }

// ProfessionalKey is the Modelo 190 perception key of professional activities, the only
// withholding of phase 1 (rents go to Modelo 115, phase 2).
const ProfessionalKey = "G"

// Line is a line of a received invoice.
type Line struct {
	No          int
	Description string
	Category    Category
	Base        vocab.Decimal // negative on a corrective invoice
	TaxCode     string
	Treatment   string // exempt or not subject, instead of a tax code
}

// TaxableLine is a line as the tax engine needs it.
type TaxableLine struct {
	Ref       string
	Base      vocab.Decimal
	TaxCode   string
	Treatment string
}

// TaxLine is a line of the tax breakdown.
type TaxLine struct {
	TaxType       string
	TaxCode       string
	Treatment     string
	TreatmentKind string
	Rate          vocab.Decimal
	Base          vocab.Decimal
	Amount        vocab.Decimal
}

// Breakdown is the tax breakdown of an invoice.
type Breakdown struct {
	Country string
	Lines   []TaxLine
	Net     vocab.Decimal
	Tax     vocab.Decimal
}

// Taxes calculates the breakdown (a port Purchases owns over the Fiscal TaxEngine, with the
// jurisdiction of the buying company).
type Taxes interface {
	Calculate(ctx context.Context, company OrganizationID, accrual vocab.Date, lines []TaxableLine) (Breakdown, error)
}

// PayTo is an account the invoice is paid to.
type PayTo struct {
	IBAN   vocab.IBAN
	Amount vocab.Decimal
}

// Draft is what is registered: the invoice as the supplier issued it.
type Draft struct {
	Company         OrganizationID
	Supplier        PartyID
	SupplierNumber  string
	Issued          vocab.Date // date of the supplier's invoice (accrual)
	Received        vocab.Date // date it is booked
	Due             vocab.Date
	Lines           []Line
	NonDeductible   bool          // the input tax is a cost (not deductible)
	WithholdingRate vocab.Decimal // percentage on the net
	DeclaredTotal   vocab.Decimal // the total printed on the invoice, checked against the breakdown
	PayTo           []PayTo
	Corrects        InvoiceID // the invoice a corrective one rectifies
}

// InvoiceState is the persisted state of a received invoice.
type InvoiceState struct {
	Draft
	Register     string // number in the register of received invoices
	Year         int
	Breakdown    Breakdown
	Withholding  vocab.Decimal
	Cancelled    bool
	CancelReason string
	Audit        traits.AuditStamp
}

// Invoice is an invoice received from a supplier, booked in the register.
type Invoice struct {
	fw.BaseAggregateRoot[InvoiceID]
	traits.Audited
	s InvoiceState
}

func cents(d vocab.Decimal) bool { return d.Equal(d.Round(2)) }

func checkDraft(v *fw.Validation, d *Draft) {
	v.Require(!d.Company.IsZero() && !d.Supplier.IsZero(), "supplier", "required", "company and supplier are required")
	d.SupplierNumber = strings.TrimSpace(d.SupplierNumber)
	v.Require(d.SupplierNumber != "" && utf8.RuneCountInString(d.SupplierNumber) <= 60, "supplierNumber", "length", "the supplier's number, 1 to 60 characters")
	v.Require(!d.Issued.IsZero() && !d.Received.IsZero() && !d.Received.Before(d.Issued), "received", "range", "issue date and a reception not before it")
	v.Require(!d.Due.IsZero() && !d.Due.Before(d.Issued), "due", "range", "a due date not before the issue date")
	v.Require(len(d.Lines) > 0 && len(d.Lines) <= MaxLines, "lines", "count", fmt.Sprintf("from 1 to %d lines", MaxLines))
	for i, l := range d.Lines {
		f := fmt.Sprintf("lines[%d]", i)
		v.Require(l.No == i+1, f, "order", "lines numbered from 1")
		v.Require(utf8.RuneCountInString(strings.TrimSpace(l.Description)) <= 200, f+".description", "length", "at most 200 characters")
		v.Require(ValidCategory(l.Category), f+".category", "enum", "an expense category")
		v.Require(!l.Base.IsZero() && cents(l.Base), f+".base", "range", "a non-zero base in cents")
		v.Require((l.TaxCode == "") != (l.Treatment == ""), f+".taxCode", "required", "a tax code or a treatment")
	}
	v.Require(!d.WithholdingRate.IsNegative() && !d.WithholdingRate.GreaterThan(vocab.DecimalFromInt(50)), "withholdingRate", "range", "from 0 to 50")
	v.Require(cents(d.DeclaredTotal), "declaredTotal", "range", "the declared total in cents")
	for i, p := range d.PayTo {
		v.Require(!p.IBAN.IsZero() && p.Amount.IsPositive() && cents(p.Amount), fmt.Sprintf("payTo[%d]", i), "range", "an IBAN and a positive amount")
	}
}

// ReconstituteInvoice rebuilds an invoice.
func ReconstituteInvoice(id InvoiceID, s InvoiceState) (*Invoice, error) {
	base, err := fw.NewBaseAggregateRoot(InvoiceKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	checkDraft(&v, &s.Draft)
	v.Require(s.Register != "" && s.Year >= 1990, "register", "required", "the register number")
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Lines, s.PayTo, s.Breakdown.Lines = slices.Clone(s.Lines), slices.Clone(s.PayTo), slices.Clone(s.Breakdown.Lines)
	return &Invoice{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// RegisterNumber renders the number of the register of received invoices.
func RegisterNumber(year int, n int64) string { return fmt.Sprintf("FR-%d-%06d", year, n) }

// Register books a received invoice with its breakdown and register number. Invariants (none of
// them existed in the C#): the breakdown covers the lines, the computed total equals the total
// printed by the supplier, the withholding is the rate on the net, the accounts add up to what is
// paid to the supplier (total minus withholding), and a corrective invoice is negative.
func Register(id InvoiceID, d Draft, b Breakdown, year int, n int64) (*Invoice, error) {
	var v fw.Validation
	checkDraft(&v, &d)
	if err := v.Err(); err != nil {
		return nil, err
	}
	net := vocab.DecimalFromInt(0)
	for _, l := range d.Lines {
		net = net.Add(l.Base)
	}
	if !b.Net.Equal(net) {
		return nil, fw.Violation("purchases.breakdown", "the tax breakdown does not cover the lines")
	}
	total := b.Net.Add(b.Tax)
	if !total.Equal(d.DeclaredTotal) {
		return nil, fw.Violation("purchases.total_mismatch", "the invoice total is "+total.StringFixed(2)+", not "+d.DeclaredTotal.StringFixed(2))
	}
	if total.IsZero() {
		return nil, fw.Violation("purchases.zero_total", "an invoice of zero")
	}
	if !d.Corrects.IsZero() && total.IsPositive() {
		return nil, fw.Violation("purchases.corrective_sign", "a corrective invoice reduces what is owed")
	}
	withholding := net.Mul(d.WithholdingRate).Div(vocab.DecimalFromInt(100)).Round(2)
	if !d.WithholdingRate.IsZero() && !slices.ContainsFunc(d.Lines, func(l Line) bool { return l.Category == ProfessionalServices }) {
		return nil, fw.Violation("purchases.withholding", "phase 1 withholds only on professional services")
	}
	if len(d.PayTo) > 0 {
		sum := vocab.DecimalFromInt(0)
		for _, p := range d.PayTo {
			sum = sum.Add(p.Amount)
		}
		if !sum.Equal(total.Sub(withholding)) {
			return nil, fw.Violation("purchases.pay_to", "the accounts add up to what is paid ("+total.Sub(withholding).StringFixed(2)+")")
		}
	}
	inv, err := ReconstituteInvoice(id, InvoiceState{Draft: d, Register: RegisterNumber(year, n), Year: year, Breakdown: b, Withholding: withholding})
	if err != nil {
		return nil, err
	}
	inv.Raise(InvoiceRegistered{EventMeta: inv.NewEventMeta(), Snapshot: inv.State()})
	return inv, nil
}

// State returns the state (slices are copies).
func (i *Invoice) State() InvoiceState {
	s := i.s
	s.Lines, s.PayTo, s.Breakdown.Lines = slices.Clone(s.Lines), slices.Clone(s.PayTo), slices.Clone(s.Breakdown.Lines)
	return s
}

// Total returns net plus taxes.
func (i *Invoice) Total() vocab.Decimal { return i.s.Breakdown.Net.Add(i.s.Breakdown.Tax) }

// Payable returns what is paid to the supplier: the total minus the withholding.
func (i *Invoice) Payable() vocab.Decimal { return i.Total().Sub(i.s.Withholding) }

// Expense is the cost of a category: its bases, plus its share of the tax when not deductible.
type Expense struct {
	Category Category
	Amount   vocab.Decimal
}

// Expenses returns the cost per category (in the order of first appearance) and the deductible
// tax. A non-deductible tax is shared among the categories in proportion to their bases, the last
// one taking the rounding.
func (i *Invoice) Expenses() ([]Expense, vocab.Decimal) {
	var out []Expense
	for _, l := range i.s.Lines {
		k := slices.IndexFunc(out, func(e Expense) bool { return e.Category == l.Category })
		if k < 0 {
			out = append(out, Expense{Category: l.Category, Amount: vocab.DecimalFromInt(0)})
			k = len(out) - 1
		}
		out[k].Amount = out[k].Amount.Add(l.Base)
	}
	tax := i.s.Breakdown.Tax
	if !i.s.NonDeductible || tax.IsZero() {
		return out, tax
	}
	left := tax
	for k := range out {
		share := left
		if k < len(out)-1 {
			share = tax.Mul(out[k].Amount).Div(i.s.Breakdown.Net).Round(2)
		}
		out[k].Amount = out[k].Amount.Add(share)
		left = left.Sub(share)
	}
	return out, vocab.DecimalFromInt(0)
}

// Cancel annuls a registered invoice booked by mistake (a real correction is a corrective invoice
// of the supplier).
func (i *Invoice) Cancel(reason string) error {
	if i.s.Cancelled {
		return fw.Violation("purchases.invoice_cancelled", "the invoice is already cancelled")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > 200 {
		return fw.Violation("purchases.cancel_reason", "a reason of 1 to 200 characters")
	}
	i.s.Cancelled, i.s.CancelReason = true, reason
	i.Raise(InvoiceCancelled{EventMeta: i.NewEventMeta(), Company: i.s.Company.String(), Register: i.s.Register, Reason: reason})
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (i *Invoice) AuditSnapshot() map[string]any {
	return map[string]any{"register": i.s.Register, "supplierNumber": i.s.SupplierNumber, "total": i.Total().String(), "cancelled": i.s.Cancelled}
}

// Invoice fields.
var (
	InvFieldCompany   = spec.Comparable("company", func(i *Invoice) OrganizationID { return i.s.Company })
	InvFieldSupplier  = spec.Comparable("supplier", func(i *Invoice) PartyID { return i.s.Supplier })
	InvFieldNumber    = spec.Comparable("supplier_number", func(i *Invoice) string { return i.s.SupplierNumber })
	InvFieldYear      = spec.Comparable("fiscal_year", func(i *Invoice) int { return i.s.Year })
	InvFieldIssued    = spec.OrderedBy("issued", func(i *Invoice) vocab.Date { return i.s.Issued }, vocab.CompareDates)
	InvFieldReceived  = spec.OrderedBy("received", func(i *Invoice) vocab.Date { return i.s.Received }, vocab.CompareDates)
	InvFieldRegister  = spec.Ordered("register_number", func(i *Invoice) string { return i.s.Register })
	InvFieldCancelled = spec.Comparable("cancelled", func(i *Invoice) bool { return i.s.Cancelled })
)

// Counter numbers the register of received invoices of a company and year.
type Counter struct {
	fw.BaseAggregateRoot[CounterID]
	company OrganizationID
	year    int
	last    int64
}

// ReconstituteCounter rebuilds a counter.
func ReconstituteCounter(id CounterID, company OrganizationID, year int, last int64) (*Counter, error) {
	base, err := fw.NewBaseAggregateRoot(CounterKind, id)
	if err != nil {
		return nil, err
	}
	if company.IsZero() || year < 1990 || last < 0 {
		return nil, fmt.Errorf("%w: invalid register counter", fw.ErrValidation)
	}
	return &Counter{BaseAggregateRoot: base, company: company, year: year, last: last}, nil
}

// Next takes the next number.
func (c *Counter) Next() int64 { c.last++; return c.last }

// Values returns the persisted values.
func (c *Counter) Values() (OrganizationID, int, int64) { return c.company, c.year, c.last }

// Counter fields.
var (
	CntFieldCompany = spec.Comparable("company", func(c *Counter) OrganizationID { return c.company })
	CntFieldYear    = spec.Comparable("fiscal_year", func(c *Counter) int { return c.year })
)

// CounterKind is the stable aggregate type name.
const CounterKind = "purchases.register_counter"
