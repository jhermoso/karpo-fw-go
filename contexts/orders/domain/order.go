// Package domain is the Orders model of phase 1: the sales orders of a company (priced lines,
// confirmation, stock held by Inventory, deliveries), the delivery notes that serve them and the
// terms of each customer. The C# workflow passed its tests on fakes and could not run on the real
// schema; nothing linked orders, shipments, delivery notes and stock, reservations were never
// released and the stored unit price already carried a discount stored again beside it.
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

// Aggregate type names.
const (
	OrderKind    = "orders.sales_order"
	DeliveryKind = "orders.delivery"
	TermsKind    = "orders.customer_terms"
	CounterKind  = "orders.counter"
)

// MaxLines bounds the lines of an order.
const MaxLines = 500

// Identities of the context.
type (
	// OrderID identifies a sales order.
	OrderID struct{ fw.UUID }
	// DeliveryID identifies a delivery note.
	DeliveryID struct{ fw.UUID }
	// TermsID identifies the terms of a customer.
	TermsID struct{ fw.UUID }
	// CounterID identifies a numbering counter.
	CounterID struct{ fw.UUID }
	// OrganizationID is the selling company, an internal organization of the Parties context.
	OrganizationID struct{ fw.UUID }
	// PartyID is the customer, a party of the Parties context.
	PartyID struct{ fw.UUID }
	// ProductID is a product of the Products context.
	ProductID struct{ fw.UUID }
	// WarehouseID is a warehouse of the Inventory context.
	WarehouseID struct{ fw.UUID }
	// PriceListID is a price list of the Products context.
	PriceListID struct{ fw.UUID }
)

// Status is the status of an order (the C# kept three unsynchronised ones, two of them freely
// writable).
type Status int

// Statuses. A confirmed order is served by deliveries until it is delivered in full, closed with
// part pending, or cancelled before any delivery.
const (
	Draft Status = iota + 1
	Confirmed
	Delivered
	Closed
	Cancelled
)

var statuses = map[Status]string{Draft: "draft", Confirmed: "confirmed", Delivered: "delivered", Closed: "closed", Cancelled: "cancelled"}

// String returns the stable name.
func (s Status) String() string { return statuses[s] }

// ParseStatus parses a status name.
func ParseStatus(s string) (Status, bool) {
	for k, n := range statuses {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

func zero() vocab.Decimal { return vocab.DecimalFromInt(0) }

func hundred() vocab.Decimal { return vocab.DecimalFromInt(100) }

func quantity(q vocab.Decimal) bool { return q.IsPositive() && q.Equal(q.Round(4)) }

func percent(p vocab.Decimal) bool {
	return !p.IsNegative() && !p.GreaterThan(hundred()) && p.Equal(p.Round(2))
}

// Line is a line of an order. UnitPrice is the price before any discount; Discount is the one of
// the price list; the customer discount of the order applies on top. NetPrice and Amount are
// derived, so no discount is ever applied twice.
type Line struct {
	No          int
	Product     ProductID
	SKU         string
	Description string
	UoM         string
	TaxCode     string
	Stocked     bool // its stock is held and issued by Inventory
	Quantity    vocab.Decimal
	UnitPrice   vocab.Decimal
	Discount    vocab.Decimal // percentage
	NetPrice    vocab.Decimal // unit price after both discounts, 4 decimals
	Amount      vocab.Decimal // quantity × net price, in cents
	Reserved    vocab.Decimal // held by Inventory and not yet delivered
	Delivered   vocab.Decimal
}

// Pending returns what is still to deliver.
func (l Line) Pending() vocab.Decimal { return l.Quantity.Sub(l.Delivered) }

// Short returns what is pending and not held: what Inventory is still asked for.
func (l Line) Short() vocab.Decimal {
	if !l.Stocked {
		return zero()
	}
	return l.Pending().Sub(l.Reserved)
}

// price derives the net price and the amount of a line.
func price(l Line, customerDiscount vocab.Decimal) Line {
	l.NetPrice = l.UnitPrice.Mul(hundred().Sub(l.Discount)).Div(hundred()).Mul(hundred().Sub(customerDiscount)).Div(hundred()).Round(4)
	l.Amount = l.NetPrice.Mul(l.Quantity).Round(2)
	return l
}

// OrderState is the persisted state of an order.
type OrderState struct {
	Company          OrganizationID
	Customer         PartyID
	Number           string // assigned on confirmation
	Date             vocab.Date
	Warehouse        WarehouseID // where its stocked lines are served from
	PriceList        PriceListID // the list its lines were priced with
	CustomerDiscount vocab.Decimal
	Reference        string // the customer's own reference
	Notes            string
	Status           Status
	Lines            []Line
	CloseReason      string
	Audit            traits.AuditStamp
}

// Order is a sales order.
type Order struct {
	fw.BaseAggregateRoot[OrderID]
	traits.Audited
	s OrderState
}

// ReconstituteOrder rebuilds an order.
func ReconstituteOrder(id OrderID, s OrderState) (*Order, error) {
	base, err := fw.NewBaseAggregateRoot(OrderKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero() && !s.Customer.IsZero(), "customer", "required", "company and customer are required")
	v.Require(!s.Date.IsZero(), "date", "required", "the date is required")
	_, ok := statuses[s.Status]
	v.Require(ok, "status", "enum", "unknown status")
	v.Require(percent(s.CustomerDiscount), "customerDiscount", "range", "a percentage from 0 to 100")
	s.Reference, s.Notes = strings.TrimSpace(s.Reference), strings.TrimSpace(s.Notes)
	v.Require(utf8.RuneCountInString(s.Reference) <= 40 && utf8.RuneCountInString(s.Notes) <= 500, "notes", "length", "a reference of up to 40 and notes of up to 500 characters")
	v.Require(len(s.Lines) <= MaxLines, "lines", "count", "too many lines")
	for i, l := range s.Lines {
		f := fmt.Sprintf("lines[%d]", i)
		v.Require(l.No > 0 && !l.Product.IsZero(), f, "required", "a numbered line of a product")
		v.Require(quantity(l.Quantity), f+".quantity", "range", "a positive quantity of up to 4 decimals")
		v.Require(!l.UnitPrice.IsNegative() && l.UnitPrice.Equal(l.UnitPrice.Round(4)) && percent(l.Discount), f+".unitPrice", "range",
			"a non-negative price of up to 4 decimals and a discount from 0 to 100")
		v.Require(!l.Delivered.IsNegative() && !l.Delivered.GreaterThan(l.Quantity) && !l.Reserved.IsNegative() && !l.Reserved.GreaterThan(l.Pending()),
			f+".delivered", "range", "delivered within the quantity, reserved within what is pending")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Lines = slices.Clone(s.Lines)
	return &Order{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// DraftOrder opens a draft.
func DraftOrder(id OrderID, s OrderState) (*Order, error) {
	s.Status, s.Number, s.Lines, s.CloseReason = Draft, "", nil, ""
	return ReconstituteOrder(id, s)
}

// State returns the state (lines are a copy).
func (o *Order) State() OrderState {
	s := o.s
	s.Lines = slices.Clone(s.Lines)
	return s
}

// Total returns the sum of the lines, before taxes.
func (o *Order) Total() vocab.Decimal {
	t := zero()
	for _, l := range o.s.Lines {
		t = t.Add(l.Amount)
	}
	return t
}

// PendingAmount returns the amount of what is still to deliver.
func (o *Order) PendingAmount() vocab.Decimal {
	t := zero()
	for _, l := range o.s.Lines {
		t = t.Add(l.NetPrice.Mul(l.Pending()).Round(2))
	}
	return t
}

func (o *Order) mustBe(st Status, code, msg string) error {
	if o.s.Status != st {
		return fw.Violation(code, msg)
	}
	return nil
}

// AddLine adds a priced line to a draft and returns its number.
func (o *Order) AddLine(l Line) (int, error) {
	if err := o.mustBe(Draft, "orders.not_draft", "only a draft order changes"); err != nil {
		return 0, err
	}
	if len(o.s.Lines) >= MaxLines {
		return 0, fw.Violation("orders.too_many_lines", "the order is full")
	}
	l.No = 1
	for _, x := range o.s.Lines {
		l.No = max(l.No, x.No+1)
	}
	l.Reserved, l.Delivered = zero(), zero()
	s := o.s
	s.Lines = append(slices.Clone(o.s.Lines), price(l, o.s.CustomerDiscount))
	if _, err := ReconstituteOrder(o.ID(), s); err != nil {
		return 0, err
	}
	o.s.Lines = s.Lines
	return l.No, nil
}

// RemoveLine removes a line from a draft.
func (o *Order) RemoveLine(no int) error {
	if err := o.mustBe(Draft, "orders.not_draft", "only a draft order changes"); err != nil {
		return err
	}
	k := slices.IndexFunc(o.s.Lines, func(l Line) bool { return l.No == no })
	if k < 0 {
		return fw.NotFound("orders.line", lineNo(no))
	}
	o.s.Lines = slices.Delete(slices.Clone(o.s.Lines), k, k+1)
	return nil
}

// Confirm commits the order with its number. A confirmed order no longer changes: it is served,
// closed or cancelled.
func (o *Order) Confirm(number string) error {
	if err := o.mustBe(Draft, "orders.not_draft", "only a draft order is confirmed"); err != nil {
		return err
	}
	if len(o.s.Lines) == 0 {
		return fw.Violation("orders.empty", "an order needs at least one line")
	}
	if number == "" {
		return fw.Violation("orders.number", "the number is required")
	}
	if o.stocked() && o.s.Warehouse.IsZero() {
		return fw.Violation("orders.warehouse", "an order with stocked products is served from a warehouse")
	}
	o.s.Status, o.s.Number = Confirmed, number
	o.Raise(OrderConfirmed{EventMeta: o.NewEventMeta(), Company: o.s.Company.String(), Customer: o.s.Customer.String(), Number: number,
		Total: o.Total().StringFixed(2)})
	o.requestStock()
	return nil
}

func (o *Order) stocked() bool {
	return slices.ContainsFunc(o.s.Lines, func(l Line) bool { return l.Stocked })
}

// requestStock raises the request of what is pending and not held, if anything.
func (o *Order) requestStock() bool {
	var want []StockLine
	for _, l := range o.s.Lines {
		if short := l.Short(); short.IsPositive() {
			want = append(want, StockLine{Line: l.No, Product: l.Product.String(), Quantity: short.String()})
		}
	}
	if len(want) == 0 {
		return false
	}
	o.Raise(StockRequested{EventMeta: o.NewEventMeta(), Company: o.s.Company.String(), Warehouse: o.s.Warehouse.String(), Lines: want})
	return true
}

// RequestStock asks Inventory again for what is still short.
func (o *Order) RequestStock() error {
	if err := o.mustBe(Confirmed, "orders.not_confirmed", "only a confirmed order holds stock"); err != nil {
		return err
	}
	if !o.requestStock() {
		return fw.Violation("orders.nothing_short", "everything pending is already held")
	}
	return nil
}

// Hold records stock held by Inventory for a line: it is added to what the line holds, never
// beyond what is pending. It is ignored when the order no longer waits for it.
func (o *Order) Hold(no int, added vocab.Decimal) {
	if o.s.Status != Confirmed || !added.IsPositive() {
		return
	}
	k := slices.IndexFunc(o.s.Lines, func(l Line) bool { return l.No == no })
	if k < 0 {
		return
	}
	o.s.Lines = slices.Clone(o.s.Lines)
	l := &o.s.Lines[k]
	l.Reserved = l.Reserved.Add(added)
	if l.Reserved.GreaterThan(l.Pending()) {
		l.Reserved = l.Pending()
	}
}

// Pick is a quantity of a line to deliver.
type Pick struct {
	Line     int
	Quantity vocab.Decimal
}

// Deliver serves part of the order and returns the lines of its delivery note. Invariants: a
// confirmed order, positive quantities within what is pending and, for a stocked line, within
// what Inventory holds for it (the C# had no link between an order and what left the warehouse).
func (o *Order) Deliver(picks []Pick) ([]DeliveryLine, error) {
	if err := o.mustBe(Confirmed, "orders.not_confirmed", "only a confirmed order is delivered"); err != nil {
		return nil, err
	}
	if len(picks) == 0 {
		return nil, fw.Violation("orders.empty_delivery", "a delivery has at least one line")
	}
	lines := slices.Clone(o.s.Lines)
	var out []DeliveryLine
	for i, p := range picks {
		k := slices.IndexFunc(lines, func(l Line) bool { return l.No == p.Line })
		if k < 0 {
			return nil, fw.NotFound("orders.line", lineNo(p.Line))
		}
		if slices.ContainsFunc(picks[:i], func(x Pick) bool { return x.Line == p.Line }) {
			return nil, fw.Violation("orders.delivery_duplicate", "each line once per delivery")
		}
		l := &lines[k]
		if !quantity(p.Quantity) || p.Quantity.GreaterThan(l.Pending()) {
			return nil, fw.Violation("orders.over_delivered", fmt.Sprintf("line %d has %s pending", l.No, l.Pending()))
		}
		if l.Stocked && p.Quantity.GreaterThan(l.Reserved) {
			return nil, fw.Violation("orders.not_reserved", fmt.Sprintf("line %d has only %s held in the warehouse", l.No, l.Reserved))
		}
		if l.Stocked {
			l.Reserved = l.Reserved.Sub(p.Quantity)
		}
		l.Delivered = l.Delivered.Add(p.Quantity)
		out = append(out, DeliveryLine{Line: l.No, Product: l.Product, SKU: l.SKU, Description: l.Description, UoM: l.UoM, TaxCode: l.TaxCode,
			Stocked: l.Stocked, Quantity: p.Quantity, NetPrice: l.NetPrice, Amount: l.NetPrice.Mul(p.Quantity).Round(2)})
	}
	o.s.Lines = lines
	if !slices.ContainsFunc(lines, func(l Line) bool { return l.Pending().IsPositive() }) {
		o.s.Status = Delivered
	}
	return out, nil
}

func (o *Order) end(st Status, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > 200 {
		return fw.Violation("orders.reason", "a reason of 1 to 200 characters")
	}
	var held []int
	lines := slices.Clone(o.s.Lines)
	for k := range lines {
		if lines[k].Stocked && o.s.Status == Confirmed {
			held = append(held, lines[k].No) // Inventory may hold, or be about to hold, stock for it
		}
		lines[k].Reserved = zero()
	}
	o.s.Lines, o.s.Status, o.s.CloseReason = lines, st, reason
	o.Raise(OrderClosed{EventMeta: o.NewEventMeta(), Company: o.s.Company.String(), Status: st.String(), Reason: reason, Lines: held})
	return nil
}

// Cancel withdraws an order nothing was delivered of; what it holds is released.
func (o *Order) Cancel(reason string) error {
	if o.s.Status != Draft && o.s.Status != Confirmed {
		return fw.Violation("orders.closed", "the order is already "+o.s.Status.String())
	}
	if slices.ContainsFunc(o.s.Lines, func(l Line) bool { return l.Delivered.IsPositive() }) {
		return fw.Violation("orders.delivered", "part of the order was delivered: close it instead")
	}
	return o.end(Cancelled, reason)
}

// Close ends a partly delivered order: what is pending will not be served and what it holds is
// released.
func (o *Order) Close(reason string) error {
	if err := o.mustBe(Confirmed, "orders.not_confirmed", "only a confirmed order is closed"); err != nil {
		return err
	}
	if !slices.ContainsFunc(o.s.Lines, func(l Line) bool { return l.Delivered.IsPositive() }) {
		return fw.Violation("orders.nothing_delivered", "nothing was delivered: cancel it instead")
	}
	return o.end(Closed, reason)
}

// AuditSnapshot implements traits.Snapshotter.
func (o *Order) AuditSnapshot() map[string]any {
	return map[string]any{"number": o.s.Number, "status": o.s.Status.String(), "total": o.Total().String(), "lines": len(o.s.Lines)}
}

type lineNo int

func (n lineNo) String() string { return fmt.Sprint(int(n)) }

// Order fields.
var (
	OrdFieldCompany  = spec.Comparable("company", func(o *Order) OrganizationID { return o.s.Company })
	OrdFieldCustomer = spec.Comparable("customer", func(o *Order) PartyID { return o.s.Customer })
	OrdFieldStatus   = spec.Comparable("status", func(o *Order) int { return int(o.s.Status) })
	OrdFieldDate     = spec.OrderedBy("order_date", func(o *Order) vocab.Date { return o.s.Date }, vocab.CompareDates)
	OrdFieldNumber   = spec.Ordered("order_number", func(o *Order) string { return o.s.Number })
)

// DeliveryLine is a line of a delivery note.
type DeliveryLine struct {
	Line        int // the order line it serves
	Product     ProductID
	SKU         string
	Description string
	UoM         string
	TaxCode     string
	Stocked     bool
	Quantity    vocab.Decimal
	NetPrice    vocab.Decimal
	Amount      vocab.Decimal
}

// DeliveryState is the persisted state of a delivery note.
type DeliveryState struct {
	Company     OrganizationID
	Customer    PartyID
	Order       OrderID
	OrderNumber string
	Number      string
	Date        vocab.Date
	Warehouse   WarehouseID
	Lines       []DeliveryLine
	Invoice     string // id of the invoice of Billing that bills it
	InvoiceNo   string
	Audit       traits.AuditStamp
}

// Delivery is a delivery note (albarán): what left for the customer of an order, with its lines
// (the C# DeliveryNote was a header with no lines and no order, editable after being issued).
type Delivery struct {
	fw.BaseAggregateRoot[DeliveryID]
	traits.Audited
	s DeliveryState
}

// ReconstituteDelivery rebuilds a delivery note.
func ReconstituteDelivery(id DeliveryID, s DeliveryState) (*Delivery, error) {
	base, err := fw.NewBaseAggregateRoot(DeliveryKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero() && !s.Customer.IsZero() && !s.Order.IsZero(), "order", "required", "company, customer and order are required")
	v.Require(s.Number != "" && !s.Date.IsZero() && len(s.Lines) > 0, "number", "required", "number, date and lines are required")
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Lines = slices.Clone(s.Lines)
	return &Delivery{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// MarkInvoiced records the invoice that bills the delivery note (its goods and prices never
// change; this is the only thing added to it). The first invoice stays.
func (d *Delivery) MarkInvoiced(invoice, number string) {
	if d.s.Invoice == "" && invoice != "" {
		d.s.Invoice, d.s.InvoiceNo = invoice, number
	}
}

// IssueDelivery issues the delivery note of what an order just delivered. It never changes.
func IssueDelivery(id DeliveryID, o *Order, number string, date vocab.Date, lines []DeliveryLine) (*Delivery, error) {
	os := o.State()
	d, err := ReconstituteDelivery(id, DeliveryState{Company: os.Company, Customer: os.Customer, Order: o.ID(), OrderNumber: os.Number, Number: number,
		Date: date, Warehouse: os.Warehouse, Lines: lines})
	if err != nil {
		return nil, err
	}
	d.Raise(DeliveryIssued{EventMeta: d.NewEventMeta(), Snapshot: d.s})
	return d, nil
}

// State returns the state (lines are a copy).
func (d *Delivery) State() DeliveryState {
	s := d.s
	s.Lines = slices.Clone(s.Lines)
	return s
}

// Total returns the sum of the lines, before taxes.
func (d *Delivery) Total() vocab.Decimal {
	t := zero()
	for _, l := range d.s.Lines {
		t = t.Add(l.Amount)
	}
	return t
}

// AuditSnapshot implements traits.Snapshotter.
func (d *Delivery) AuditSnapshot() map[string]any {
	return map[string]any{"number": d.s.Number, "order": d.s.OrderNumber, "total": d.Total().String()}
}

// Delivery fields.
var (
	DelFieldCompany  = spec.Comparable("company", func(d *Delivery) OrganizationID { return d.s.Company })
	DelFieldCustomer = spec.Comparable("customer", func(d *Delivery) PartyID { return d.s.Customer })
	DelFieldOrder    = spec.Comparable("order_id", func(d *Delivery) OrderID { return d.s.Order })
	DelFieldNumber   = spec.Ordered("delivery_number", func(d *Delivery) string { return d.s.Number })
	DelFieldInvoiced = spec.Comparable("invoiced", func(d *Delivery) bool { return d.s.Invoice != "" })
)

// TermsState is the persisted state of the terms of a customer.
type TermsState struct {
	Company       OrganizationID
	Customer      PartyID
	PriceList     PriceListID
	Discount      vocab.Decimal // percentage on every line
	BlockOrders   bool
	BlockDelivery bool
	Audit         traits.AuditStamp
}

// Terms are the sales terms a company gives a customer (the sales side of the C#
// CustomerRelationshipCommercialProfile, which was looked up ignoring the company, whose
// BlockDelivery nothing enforced and whose MaxRisk compared one order alone: the credit limit is
// in Receivables).
type Terms struct {
	fw.BaseAggregateRoot[TermsID]
	traits.Audited
	s TermsState
}

// ReconstituteTerms rebuilds the terms of a customer.
func ReconstituteTerms(id TermsID, s TermsState) (*Terms, error) {
	base, err := fw.NewBaseAggregateRoot(TermsKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero() && !s.Customer.IsZero(), "customer", "required", "company and customer are required")
	v.Require(percent(s.Discount), "discount", "range", "a percentage from 0 to 100")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Terms{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State returns the state.
func (t *Terms) State() TermsState { return t.s }

// Change replaces the terms (company and customer stay).
func (t *Terms) Change(s TermsState) error {
	s.Company, s.Customer, s.Audit = t.s.Company, t.s.Customer, t.s.Audit
	if _, err := ReconstituteTerms(t.ID(), s); err != nil {
		return err
	}
	t.s = s
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (t *Terms) AuditSnapshot() map[string]any {
	return map[string]any{"discount": t.s.Discount.String(), "blockOrders": t.s.BlockOrders, "blockDelivery": t.s.BlockDelivery}
}

// Terms fields.
var (
	TrmFieldCompany  = spec.Comparable("company", func(t *Terms) OrganizationID { return t.s.Company })
	TrmFieldCustomer = spec.Comparable("customer", func(t *Terms) PartyID { return t.s.Customer })
)

// Counter numbers the orders or the delivery notes of a company and year.
type Counter struct {
	fw.BaseAggregateRoot[CounterID]
	company OrganizationID
	series  string // PED or ALB
	year    int
	last    int64
}

// ReconstituteCounter rebuilds a counter.
func ReconstituteCounter(id CounterID, company OrganizationID, series string, year int, last int64) (*Counter, error) {
	base, err := fw.NewBaseAggregateRoot(CounterKind, id)
	if err != nil {
		return nil, err
	}
	if company.IsZero() || series == "" || len(series) > 5 || year < 1990 || last < 0 {
		return nil, fmt.Errorf("%w: invalid counter", fw.ErrValidation)
	}
	return &Counter{BaseAggregateRoot: base, company: company, series: series, year: year, last: last}, nil
}

// Next takes the next number, rendered SERIES-YEAR-NNNNNN.
func (c *Counter) Next() string {
	c.last++
	return fmt.Sprintf("%s-%d-%06d", c.series, c.year, c.last)
}

// Values returns the persisted values.
func (c *Counter) Values() (OrganizationID, string, int, int64) {
	return c.company, c.series, c.year, c.last
}

// Counter fields.
var (
	CntFieldCompany = spec.Comparable("company", func(c *Counter) OrganizationID { return c.company })
	CntFieldSeries  = spec.Comparable("series", func(c *Counter) string { return c.series })
	CntFieldYear    = spec.Comparable("fiscal_year", func(c *Counter) int { return c.year })
)

// Events of the context.
type (
	// OrderConfirmed is raised when an order is confirmed.
	OrderConfirmed struct {
		fw.EventMeta
		Company  string `json:"company"`
		Customer string `json:"customer"`
		Number   string `json:"number"`
		Total    string `json:"total"`
	}
	// StockLine is a quantity of a line to hold.
	StockLine struct {
		Line     int    `json:"line"`
		Product  string `json:"product"`
		Quantity string `json:"quantity"`
	}
	// StockRequested is raised when an order asks Inventory to hold what it is short of.
	StockRequested struct {
		fw.EventMeta
		Company   string      `json:"company"`
		Warehouse string      `json:"warehouse"`
		Lines     []StockLine `json:"lines"`
	}
	// OrderClosed is raised when an order is cancelled or closed, with the lines that may hold stock.
	OrderClosed struct {
		fw.EventMeta
		Company string `json:"company"`
		Status  string `json:"status"`
		Reason  string `json:"reason"`
		Lines   []int  `json:"lines"`
	}
	// DeliveryIssued is raised when a delivery note is issued, with its state.
	DeliveryIssued struct {
		fw.EventMeta
		Snapshot DeliveryState `json:"snapshot"`
	}
)

// EventType implementations.
func (OrderConfirmed) EventType() string { return "orders.order_confirmed" }
func (StockRequested) EventType() string { return "orders.stock_requested" }
func (OrderClosed) EventType() string    { return "orders.order_closed" }
func (DeliveryIssued) EventType() string { return "orders.delivery_issued" }

// New identities and parsing.
func NewOrderID() OrderID       { return OrderID{fw.NewUUID()} }
func NewDeliveryID() DeliveryID { return DeliveryID{fw.NewUUID()} }
func NewTermsID() TermsID       { return TermsID{fw.NewUUID()} }
func NewCounterID() CounterID   { return CounterID{fw.NewUUID()} }

// ParseOrderID parses a textual identity.
func ParseOrderID(s string) (OrderID, error) { u, err := fw.ParseUUID(s); return OrderID{u}, err }

// ParseDeliveryID parses a textual identity.
func ParseDeliveryID(s string) (DeliveryID, error) {
	u, err := fw.ParseUUID(s)
	return DeliveryID{u}, err
}

// Repositories of the context.
type (
	OrderRepository    = fw.Repository[OrderID, *Order]
	DeliveryRepository = fw.Repository[DeliveryID, *Delivery]
	TermsRepository    = fw.Repository[TermsID, *Terms]
	CounterRepository  = fw.Repository[CounterID, *Counter]
)

// Item is what Orders needs of a product to sell it.
type Item struct {
	Company   OrganizationID
	SKU       string
	Name      string
	UoM       string
	TaxCode   string
	Stocked   bool
	Sellable  bool // for sale, not blocked
	Retired   vocab.Date
	UnitPrice vocab.Decimal // from the price list or the base price
	Discount  vocab.Decimal
}

// Catalog prices a product for a customer order (a port Orders owns over the Products Catalog and
// Pricing contracts). An unknown product is reported with ok false.
type Catalog interface {
	Price(ctx context.Context, company OrganizationID, product ProductID, list PriceListID, q vocab.Decimal, on vocab.Date) (Item, bool, error)
}

// Credit is the credit situation of a customer with the company (a port Orders owns over the
// Receivables Credit contract, approved decision 2 of docs/COBROS.md).
type Credit struct {
	Blocked   bool
	Limited   bool
	Available vocab.Decimal
}

// CreditCheck answers the credit of a customer on a date; nil: no credit control.
type CreditCheck interface {
	Credit(ctx context.Context, company OrganizationID, customer PartyID, on vocab.Date) (Credit, error)
}
