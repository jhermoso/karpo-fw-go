package domain

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// QuoteKind is the aggregate type name of a quote.
const QuoteKind = "orders.quote"

// DefaultValidityDays is how long a quote holds its prices when nobody says otherwise.
const DefaultValidityDays = 30

// QuoteID identifies a quote.
type QuoteID struct{ fw.UUID }

// NewQuoteID returns a fresh identity.
func NewQuoteID() QuoteID { return QuoteID{fw.NewUUID()} }

// ParseQuoteID parses a textual identity.
func ParseQuoteID(s string) (QuoteID, error) { u, err := fw.ParseUUID(s); return QuoteID{u}, err }

// QuoteStatus is where a quote is. The C# kept a bare number (0, 1, 2) anyone could set, and an
// issued quote could still be edited, deleted or sent back.
type QuoteStatus string

// Statuses of a quote. A draft is worked on; once sent it has its number and no longer changes:
// the customer accepts it (it becomes an order), rejects it, or lets it expire; the company may
// withdraw it before any of that.
const (
	QuoteDraft     QuoteStatus = "draft"
	QuoteSent      QuoteStatus = "sent"
	QuoteAccepted  QuoteStatus = "accepted"
	QuoteRejected  QuoteStatus = "rejected"
	QuoteExpired   QuoteStatus = "expired"
	QuoteWithdrawn QuoteStatus = "withdrawn"
)

// QuoteStatuses lists the valid statuses.
var QuoteStatuses = []QuoteStatus{QuoteDraft, QuoteSent, QuoteAccepted, QuoteRejected, QuoteExpired, QuoteWithdrawn}

// QuoteLine is a product offered at a price: the same prices an order line has, without what an
// order adds when it is served.
type QuoteLine struct {
	No          int
	Product     ProductID
	SKU         string
	Description string
	UoM         string
	TaxCode     string
	Stocked     bool
	Quantity    vocab.Decimal
	UnitPrice   vocab.Decimal
	Discount    vocab.Decimal // percentage, of the price list
	NetPrice    vocab.Decimal
	Amount      vocab.Decimal
}

// QuoteState is the persisted state of a quote.
type QuoteState struct {
	Company          OrganizationID
	Customer         PartyID
	Number           string // assigned when it is sent
	Date             vocab.Date
	ValidUntil       vocab.Date // the last day its prices hold
	PriceList        PriceListID
	CustomerDiscount vocab.Decimal
	Reference        string
	Notes            string
	Status           QuoteStatus
	Lines            []QuoteLine
	Reason           string  // of the rejection or the withdrawal
	Order            OrderID // the order it became
	Audit            traits.AuditStamp
}

// Quote is an offer to a customer: products, quantities and prices that hold until a day.
type Quote struct {
	fw.BaseAggregateRoot[QuoteID]
	traits.Audited
	s QuoteState
}

// ReconstituteQuote rebuilds a quote.
func ReconstituteQuote(id QuoteID, s QuoteState) (*Quote, error) {
	base, err := fw.NewBaseAggregateRoot(QuoteKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero() && !s.Customer.IsZero(), "customer", "required", "company and customer are required")
	v.Require(!s.Date.IsZero() && !s.ValidUntil.IsZero() && !s.ValidUntil.Before(s.Date), "validUntil", "range", "a date, and a validity that does not end before it")
	v.Require(slices.Contains(QuoteStatuses, s.Status), "status", "enum", "unknown status")
	v.Require(percent(s.CustomerDiscount), "customerDiscount", "range", "a percentage from 0 to 100")
	s.Reference, s.Notes, s.Reason = strings.TrimSpace(s.Reference), strings.TrimSpace(s.Notes), strings.TrimSpace(s.Reason)
	v.Require(utf8.RuneCountInString(s.Reference) <= 40 && utf8.RuneCountInString(s.Notes) <= 500 && utf8.RuneCountInString(s.Reason) <= 200, "notes", "length",
		"a reference of up to 40, notes of up to 500 and a reason of up to 200 characters")
	// A draft has no number yet; one withdrawn before it was sent never had it.
	v.Require((s.Number == "") == (s.Status == QuoteDraft) || s.Status == QuoteWithdrawn, "number", "state", "a quote has a number from the moment it is sent")
	v.Require(s.Order.IsZero() || s.Status == QuoteAccepted, "order", "state", "only an accepted quote became an order")
	v.Require(len(s.Lines) <= MaxLines, "lines", "count", "too many lines")
	for i, l := range s.Lines {
		f := fmt.Sprintf("lines[%d]", i)
		v.Require(l.No > 0 && !l.Product.IsZero(), f, "required", "a numbered line of a product")
		v.Require(quantity(l.Quantity), f+".quantity", "range", "a positive quantity of up to 4 decimals")
		v.Require(!l.UnitPrice.IsNegative() && l.UnitPrice.Equal(l.UnitPrice.Round(4)) && percent(l.Discount), f+".unitPrice", "range",
			"a non-negative price of up to 4 decimals and a discount from 0 to 100")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Lines = slices.Clone(s.Lines)
	return &Quote{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// DraftQuote opens a draft. Without a validity it holds DefaultValidityDays from its date.
func DraftQuote(id QuoteID, s QuoteState) (*Quote, error) {
	s.Status, s.Number, s.Lines, s.Reason, s.Order = QuoteDraft, "", nil, "", OrderID{}
	if s.ValidUntil.IsZero() && !s.Date.IsZero() {
		s.ValidUntil = s.Date.AddDays(DefaultValidityDays)
	}
	return ReconstituteQuote(id, s)
}

// State returns the state (lines are a copy).
func (q *Quote) State() QuoteState {
	s := q.s
	s.Lines = slices.Clone(s.Lines)
	return s
}

// Total returns the sum of the lines, before taxes.
func (q *Quote) Total() vocab.Decimal {
	t := zero()
	for _, l := range q.s.Lines {
		t = t.Add(l.Amount)
	}
	return t
}

func (q *Quote) mustBe(st QuoteStatus, code, msg string) error {
	if q.s.Status != st {
		return fw.Violation(code, msg)
	}
	return nil
}

// Change replaces what a draft says of itself: its dates, the customer's reference and the notes.
// The prices of its lines stay as they were taken.
func (q *Quote) Change(date, validUntil vocab.Date, reference, notes string) error {
	if err := q.mustBe(QuoteDraft, "orders.quote_not_draft", "only a draft quote changes"); err != nil {
		return err
	}
	s := q.s
	s.Date, s.ValidUntil, s.Reference, s.Notes = date, validUntil, reference, notes
	n, err := ReconstituteQuote(q.ID(), s)
	if err != nil {
		return err
	}
	q.s = n.s
	return nil
}

// AddLine adds a priced line to a draft and returns its number. It is priced as an order line is:
// the discount of the list, then the one of the customer.
func (q *Quote) AddLine(l QuoteLine) (int, error) {
	if err := q.mustBe(QuoteDraft, "orders.quote_not_draft", "only a draft quote changes"); err != nil {
		return 0, err
	}
	if len(q.s.Lines) >= MaxLines {
		return 0, fw.Violation("orders.too_many_lines", "the quote is full")
	}
	l.No = 1
	for _, x := range q.s.Lines {
		l.No = max(l.No, x.No+1)
	}
	p := price(Line{Quantity: l.Quantity, UnitPrice: l.UnitPrice, Discount: l.Discount}, q.s.CustomerDiscount)
	l.NetPrice, l.Amount = p.NetPrice, p.Amount
	s := q.s
	s.Lines = append(slices.Clone(q.s.Lines), l)
	if _, err := ReconstituteQuote(q.ID(), s); err != nil {
		return 0, err
	}
	q.s.Lines = s.Lines
	return l.No, nil
}

// RemoveLine removes a line from a draft.
func (q *Quote) RemoveLine(no int) error {
	if err := q.mustBe(QuoteDraft, "orders.quote_not_draft", "only a draft quote changes"); err != nil {
		return err
	}
	k := slices.IndexFunc(q.s.Lines, func(l QuoteLine) bool { return l.No == no })
	if k < 0 {
		return fw.NotFound("orders.quote_line", lineNo(no))
	}
	q.s.Lines = slices.Delete(slices.Clone(q.s.Lines), k, k+1)
	return nil
}

func (q *Quote) closed(order string) {
	q.Raise(QuoteClosed{EventMeta: q.NewEventMeta(), Company: q.s.Company.String(), Customer: q.s.Customer.String(), Number: q.s.Number,
		Status: string(q.s.Status), Reason: q.s.Reason, Order: order, Total: q.Total().StringFixed(2)})
}

// Send commits the offer with its number: from here on it does not change.
func (q *Quote) Send(number string, today vocab.Date) error {
	if err := q.mustBe(QuoteDraft, "orders.quote_not_draft", "only a draft quote is sent"); err != nil {
		return err
	}
	if len(q.s.Lines) == 0 {
		return fw.Violation("orders.quote_empty", "a quote needs at least one line")
	}
	if number == "" {
		return fw.Violation("orders.number", "the number is required")
	}
	if today.After(q.s.ValidUntil) {
		return fw.Violation("orders.quote_expired", "the quote held until "+q.s.ValidUntil.String())
	}
	q.s.Status, q.s.Number = QuoteSent, number
	q.Raise(QuoteSentOut{EventMeta: q.NewEventMeta(), Company: q.s.Company.String(), Customer: q.s.Customer.String(), Number: number,
		Date: q.s.Date, ValidUntil: q.s.ValidUntil, Total: q.Total().StringFixed(2)})
	return nil
}

// Accept records that the customer took the offer on a day within its validity, and the order it
// became.
func (q *Quote) Accept(order OrderID, on vocab.Date) error {
	if err := q.mustBe(QuoteSent, "orders.quote_not_sent", "only a quote that was sent is accepted"); err != nil {
		return err
	}
	if on.After(q.s.ValidUntil) {
		return fw.Violation("orders.quote_expired", "the quote held until "+q.s.ValidUntil.String())
	}
	if order.IsZero() {
		return fw.Violation("orders.quote_order", "an accepted quote becomes an order")
	}
	q.s.Status, q.s.Order = QuoteAccepted, order
	q.closed(order.String())
	return nil
}

func (q *Quote) end(to QuoteStatus, reason string) error {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > 200 {
		return fw.Violation("orders.reason", "a reason of at most 200 characters")
	}
	q.s.Status, q.s.Reason = to, reason
	if q.s.Number != "" { // a draft nobody saw ends without telling anyone
		q.closed("")
	}
	return nil
}

// Reject records that the customer said no.
func (q *Quote) Reject(reason string) error {
	if err := q.mustBe(QuoteSent, "orders.quote_not_sent", "only a quote that was sent is rejected"); err != nil {
		return err
	}
	return q.end(QuoteRejected, reason)
}

// Withdraw takes back a draft or an offer nobody answered yet.
func (q *Quote) Withdraw(reason string) error {
	if q.s.Status != QuoteDraft && q.s.Status != QuoteSent {
		return fw.Violation("orders.quote_closed", "a quote "+string(q.s.Status)+" is not withdrawn")
	}
	return q.end(QuoteWithdrawn, reason)
}

// Due reports whether a sent quote is past its validity on a day.
func (q *Quote) Due(today vocab.Date) bool {
	return q.s.Status == QuoteSent && today.After(q.s.ValidUntil)
}

// Expire closes a sent quote nobody answered in time. It reports whether it did.
func (q *Quote) Expire(today vocab.Date) bool {
	if !q.Due(today) {
		return false
	}
	_ = q.end(QuoteExpired, "")
	return true
}

// AuditSnapshot implements traits.Snapshotter.
func (q *Quote) AuditSnapshot() map[string]any {
	return map[string]any{"number": q.s.Number, "status": string(q.s.Status), "validUntil": q.s.ValidUntil.String(), "total": q.Total().String(),
		"lines": len(q.s.Lines), "order": q.s.Order.String()}
}

// Quote fields.
var (
	QuoFieldCompany  = spec.Comparable("company", func(q *Quote) OrganizationID { return q.s.Company })
	QuoFieldCustomer = spec.Comparable("customer", func(q *Quote) PartyID { return q.s.Customer })
	QuoFieldStatus   = spec.Comparable("status", func(q *Quote) string { return string(q.s.Status) })
	QuoFieldDate     = spec.OrderedBy("quote_date", func(q *Quote) vocab.Date { return q.s.Date }, vocab.CompareDates)
	QuoFieldNumber   = spec.Ordered("quote_number", func(q *Quote) string { return q.s.Number })
)

// QuoteRepository stores quotes.
type QuoteRepository = fw.Repository[QuoteID, *Quote]

// Events of a quote.
type (
	// QuoteSentOut is raised when a quote is sent to the customer.
	QuoteSentOut struct {
		fw.EventMeta
		Company    string     `json:"company"`
		Customer   string     `json:"customer"`
		Number     string     `json:"number"`
		Date       vocab.Date `json:"date"`
		ValidUntil vocab.Date `json:"validUntil"`
		Total      string     `json:"total"`
	}
	// QuoteClosed is raised when a sent quote is accepted, rejected, withdrawn or expires.
	QuoteClosed struct {
		fw.EventMeta
		Company  string `json:"company"`
		Customer string `json:"customer"`
		Number   string `json:"number"`
		Status   string `json:"status"`
		Reason   string `json:"reason"`
		Order    string `json:"order"`
		Total    string `json:"total"`
	}
)

// EventType implementations.
func (QuoteSentOut) EventType() string { return "orders.quote_sent" }
func (QuoteClosed) EventType() string  { return "orders.quote_closed" }
