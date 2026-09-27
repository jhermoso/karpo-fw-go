package domain

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// InvoiceKind is the stable aggregate type name.
const InvoiceKind = "billing.invoice"

// MaxLines bounds the lines of an invoice.
const MaxLines = 500

var hundred = vocab.DecimalFromInt(100)

// Kind of invoice: ordinary or corrective (rectificativa). The C# modelled exemption as an
// invoice type ("Exenta") and credit notes as a separate header without lines.
type Kind int

// Invoice kinds.
const (
	Ordinary Kind = iota + 1
	Corrective
)

var kinds = map[Kind]string{Ordinary: "ordinary", Corrective: "corrective"}

// String returns the stable name.
func (k Kind) String() string { return kinds[k] }

// Status of an invoice: a draft changes and can be discarded; an issued invoice is immutable and
// is corrected with a corrective invoice (the C# let issued invoices change and be deleted).
type Status int

// Statuses.
const (
	Draft Status = iota + 1
	Issued
)

var statuses = map[Status]string{Draft: "draft", Issued: "issued"}

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

// CorrectionReasons are the causes of a corrective invoice (R1: error founded in law and art. 80
// One, Two and Six LIVA; R2: insolvency, art. 80 Three; R3: bad debts, art. 80 Four; R4: other
// causes; R5: simplified invoices).
var CorrectionReasons = []string{"R1", "R2", "R3", "R4", "R5"}

// Line is a line of an invoice. Its net amount is quantity × unit price less the discount,
// rounded to the cent half away from zero (the C# stored the unrounded product).
type Line struct {
	ID          LineID
	Description string
	Quantity    vocab.Decimal
	UnitPrice   vocab.Decimal
	Discount    vocab.Decimal // percentage
	TaxCode     string
	Treatment   string
	Net         vocab.Decimal
}

// LineInput is a new line.
type LineInput struct {
	Description string
	Quantity    vocab.Decimal
	UnitPrice   vocab.Decimal
	Discount    vocab.Decimal
	TaxCode     string
	Treatment   string
}

// Identity is the fiscal identification of a party on an invoice, frozen at issue.
type Identity struct {
	NIF     string
	Name    string
	Country string
}

// TaxLine is a line of the tax breakdown frozen at issue.
type TaxLine struct {
	TaxType         string
	TaxCode         string
	Treatment       string
	TreatmentKind   string
	Rate            vocab.Decimal
	Base            vocab.Decimal
	Amount          vocab.Decimal
	SurchargeRate   vocab.Decimal
	SurchargeAmount vocab.Decimal
}

// Breakdown is the tax breakdown of an invoice.
type Breakdown struct {
	Country   string
	Lines     []TaxLine
	Net       vocab.Decimal
	Tax       vocab.Decimal
	Surcharge vocab.Decimal
}

// Total returns net + taxes + surcharge.
func (b Breakdown) Total() vocab.Decimal { return b.Net.Add(b.Tax).Add(b.Surcharge) }

// Invoice is a sales invoice of a seller to a customer, with its lines.
type Invoice struct {
	fw.BaseAggregateRoot[InvoiceID]
	traits.Audited
	s InvoiceState
}

// InvoiceState is the persisted state of an invoice.
type InvoiceState struct {
	Seller               OrganizationID
	Customer             PartyID
	Kind                 Kind
	Status               Status
	Currency             vocab.CurrencyCode
	Description          string
	OperationDate        vocab.Date // zero: the issue date
	DueDate              vocab.Date
	EquivalenceSurcharge bool
	Corrects             InvoiceID
	Reason               string
	Lines                []Line
	Series               SeriesID
	Number               string
	IssueDate            vocab.Date
	SellerIdentity       Identity
	CustomerIdentity     Identity
	Taxes                Breakdown
	Audit                traits.AuditStamp
}

// Details are the editable header fields of a draft.
type Details struct {
	Description          string
	OperationDate        vocab.Date
	DueDate              vocab.Date
	EquivalenceSurcharge bool
}

func checkDetails(v *fw.Validation, d *Details) {
	d.Description = strings.TrimSpace(d.Description)
	v.Require(utf8.RuneCountInString(d.Description) <= 500, "description", "length", "at most 500 characters")
}

// ReconstituteInvoice rebuilds an invoice.
func ReconstituteInvoice(id InvoiceID, s InvoiceState) (*Invoice, error) {
	base, err := fw.NewBaseAggregateRoot(InvoiceKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Seller.IsZero() && !s.Customer.IsZero(), "customer", "required", "seller and customer are required")
	_, okKind := kinds[s.Kind]
	v.Require(okKind, "kind", "enum", "ordinary or corrective")
	_, okStatus := statuses[s.Status]
	v.Require(okStatus, "status", "enum", "unknown status")
	v.Require(s.Currency.String() == "EUR", "currency", "supported", "only euros for now (the Spanish jurisdiction)")
	v.Require(s.Kind != Corrective || (!s.Corrects.IsZero() && slices.Contains(CorrectionReasons, s.Reason)), "reason", "corrective",
		"a corrective invoice names the invoice it corrects and a reason R1–R5")
	v.Require(s.Kind == Corrective || (s.Corrects.IsZero() && s.Reason == ""), "corrects", "ordinary", "only corrective invoices correct")
	v.Require(len(s.Lines) <= MaxLines, "lines", "count", "too many lines")
	d := Details{Description: s.Description, OperationDate: s.OperationDate, DueDate: s.DueDate, EquivalenceSurcharge: s.EquivalenceSurcharge}
	checkDetails(&v, &d)
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Description = d.Description
	s.Lines = slices.Clone(s.Lines)
	return &Invoice{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// DraftInvoice creates a draft. A corrective draft copies the customer of the original, which the
// application checks is issued and of the same seller.
func DraftInvoice(id InvoiceID, s InvoiceState) (*Invoice, error) {
	s.Status, s.Lines, s.Series, s.Number, s.IssueDate = Draft, nil, SeriesID{}, "", vocab.Date{}
	s.SellerIdentity, s.CustomerIdentity, s.Taxes = Identity{}, Identity{}, Breakdown{}
	if s.Currency.IsZero() {
		s.Currency = vocab.MustCurrencyCode("EUR")
	}
	return ReconstituteInvoice(id, s)
}

// State returns the state (lines and taxes are copies).
func (i *Invoice) State() InvoiceState {
	s := i.s
	s.Lines = slices.Clone(s.Lines)
	s.Taxes.Lines = slices.Clone(s.Taxes.Lines)
	return s
}

// Net returns the sum of the line nets.
func (i *Invoice) Net() vocab.Decimal {
	n := vocab.DecimalFromInt(0)
	for _, l := range i.s.Lines {
		n = n.Add(l.Net)
	}
	return n
}

func (i *Invoice) mustBeDraft() error {
	if i.s.Status != Draft {
		return fw.Violation("billing.invoice_not_draft", "an issued invoice does not change: issue a corrective invoice")
	}
	return nil
}

// AddLine adds a line (drafts only). An ordinary invoice has positive quantities and non-negative
// prices; a corrective invoice (by differences) may have negative quantities.
func (i *Invoice) AddLine(in LineInput) (LineID, error) {
	if err := i.mustBeDraft(); err != nil {
		return LineID{}, err
	}
	if len(i.s.Lines) >= MaxLines {
		return LineID{}, fw.Violation("billing.too_many_lines", "the invoice has too many lines")
	}
	var v fw.Validation
	desc := strings.TrimSpace(in.Description)
	v.Require(desc != "" && utf8.RuneCountInString(desc) <= 500, "description", "length", "a description of 1 to 500 characters")
	v.Require(!in.Quantity.IsZero() && (i.s.Kind == Corrective || in.Quantity.IsPositive()), "quantity", "range",
		"a positive quantity (negative only in corrective invoices)")
	v.Require(!in.UnitPrice.IsNegative(), "unitPrice", "range", "a non-negative unit price")
	v.Require(!in.Discount.IsNegative() && in.Discount.LessThanOrEqual(hundred), "discount", "range", "a discount from 0 to 100")
	code, treat := strings.ToUpper(strings.TrimSpace(in.TaxCode)), strings.ToUpper(strings.TrimSpace(in.Treatment))
	v.Require(code != "" || treat != "", "taxCode", "required", "a tax code or an exempt or not subject treatment")
	v.Require(utf8.RuneCountInString(code) <= 5 && utf8.RuneCountInString(treat) <= 5, "taxCode", "length", "codes of at most 5 characters")
	if err := v.Err(); err != nil {
		return LineID{}, err
	}
	gross := in.Quantity.Mul(in.UnitPrice)
	net := gross.Sub(gross.Mul(in.Discount).Div(hundred)).Round(2)
	l := Line{ID: LineID{fw.NewUUID()}, Description: desc, Quantity: in.Quantity, UnitPrice: in.UnitPrice, Discount: in.Discount,
		TaxCode: code, Treatment: treat, Net: net}
	i.s.Lines = append(slices.Clone(i.s.Lines), l)
	return l.ID, nil
}

// RemoveLine removes a line (drafts only).
func (i *Invoice) RemoveLine(id LineID) error {
	if err := i.mustBeDraft(); err != nil {
		return err
	}
	k := slices.IndexFunc(i.s.Lines, func(l Line) bool { return l.ID == id })
	if k < 0 {
		return fw.NotFound("billing.invoice_line", id)
	}
	i.s.Lines = slices.Delete(slices.Clone(i.s.Lines), k, k+1)
	return nil
}

// SetDetails replaces the editable header fields (drafts only).
func (i *Invoice) SetDetails(d Details) error {
	if err := i.mustBeDraft(); err != nil {
		return err
	}
	var v fw.Validation
	checkDetails(&v, &d)
	if err := v.Err(); err != nil {
		return err
	}
	i.s.Description, i.s.OperationDate, i.s.DueDate, i.s.EquivalenceSurcharge = d.Description, d.OperationDate, d.DueDate, d.EquivalenceSurcharge
	return nil
}

// AccrualDate returns the date the taxes accrue on: the operation date, or the issue date.
func (i *Invoice) AccrualDate(issue vocab.Date) vocab.Date {
	if !i.s.OperationDate.IsZero() {
		return i.s.OperationDate
	}
	return issue
}

// Issuance is what the application gathers to issue an invoice.
type Issuance struct {
	Series   SeriesID
	Number   string
	Date     vocab.Date
	Seller   Identity
	Customer Identity
	Taxes    Breakdown
}

// Issue freezes the invoice with its number, the identities of the parties and the tax breakdown.
// Invariants: at least one line; an ordinary invoice has a positive net and a corrective one a
// non-zero net; the breakdown covers exactly the net of the lines; the seller has a valid Spanish
// tax number and the customer a tax number; the due date does not precede the issue date.
func (i *Invoice) Issue(x Issuance) error {
	if err := i.mustBeDraft(); err != nil {
		return err
	}
	net := i.Net()
	switch {
	case len(i.s.Lines) == 0:
		return fw.Violation("billing.no_lines", "an invoice needs at least one line")
	case i.s.Kind == Ordinary && !net.IsPositive():
		return fw.Violation("billing.non_positive_net", "an ordinary invoice has a positive net amount")
	case i.s.Kind == Corrective && net.IsZero():
		return fw.Violation("billing.zero_correction", "a corrective invoice changes some amount")
	case !x.Taxes.Net.Equal(net):
		return fw.Violation("billing.breakdown_mismatch", "the tax breakdown does not cover the lines")
	case x.Date.IsZero() || x.Number == "":
		return fw.Violation("billing.issue_incomplete", "the issue needs a date and a number")
	case !i.s.DueDate.IsZero() && i.s.DueDate.Before(x.Date):
		return fw.Violation("billing.due_before_issue", "the due date precedes the issue date")
	}
	if ok, _ := vocab.ValidCheckDigit("ES_NIF", x.Seller.NIF); !ok {
		return fw.Violation("billing.seller_nif", "the seller has no valid tax number")
	}
	if strings.TrimSpace(x.Customer.NIF) == "" {
		return fw.Violation("billing.customer_nif", "the customer has no tax number (simplified invoices are not supported yet)")
	}
	i.s.Status, i.s.Series, i.s.Number, i.s.IssueDate = Issued, x.Series, x.Number, x.Date
	i.s.SellerIdentity, i.s.CustomerIdentity, i.s.Taxes = x.Seller, x.Customer, x.Taxes
	i.s.Taxes.Lines = slices.Clone(x.Taxes.Lines)
	i.Raise(InvoiceIssued{EventMeta: i.NewEventMeta(), Invoice: i.State()})
	return nil
}

// Discard checks a draft can be deleted (an issued invoice never is).
func (i *Invoice) Discard() error { return i.mustBeDraft() }

// AuditSnapshot implements traits.Snapshotter.
func (i *Invoice) AuditSnapshot() map[string]any {
	return map[string]any{"status": i.s.Status.String(), "number": i.s.Number, "lines": len(i.s.Lines), "net": i.Net().String()}
}

// Invoice fields.
var (
	InvFieldID       = spec.Comparable("id", func(i *Invoice) InvoiceID { return i.ID() })
	InvFieldSeller   = spec.Comparable("seller", func(i *Invoice) OrganizationID { return i.s.Seller })
	InvFieldCustomer = spec.Comparable("customer", func(i *Invoice) PartyID { return i.s.Customer })
	InvFieldStatus   = spec.Comparable("status", func(i *Invoice) int { return int(i.s.Status) })
	InvFieldCorrects = spec.Comparable("corrects", func(i *Invoice) InvoiceID { return i.s.Corrects })
	InvFieldIssued   = spec.OrderedBy("issue_date", func(i *Invoice) vocab.Date { return i.s.IssueDate }, vocab.CompareDates)
	InvFieldNumber   = spec.Ordered("invoice_number", func(i *Invoice) string { return i.s.Number })
)

// Repositories and ports of the context.
type (
	SeriesRepository  = fw.Repository[SeriesID, *Series]
	InvoiceRepository = fw.Repository[InvoiceID, *Invoice]
)

// TaxableLine is a line sent to the tax engine.
type TaxableLine struct {
	Ref       string
	Base      vocab.Decimal
	TaxCode   string
	Treatment string
}

// Taxes calculates the tax breakdown of an invoice (a port Billing owns; an adapter implements it
// over the Fiscal TaxEngine, which picks the jurisdiction of the seller).
type Taxes interface {
	Calculate(ctx context.Context, seller OrganizationID, accrual vocab.Date, surcharge bool, lines []TaxableLine) (Breakdown, error)
}

// Identities resolves the fiscal identities of parties (a port Billing owns; an adapter implements
// it over the Parties TaxIdentities contract).
type Identities interface {
	Identities(ctx context.Context, parties []PartyID) (map[PartyID]Identity, error)
}

// TaxableLines returns the lines of the invoice as the tax engine needs them.
func (i *Invoice) TaxableLines() []TaxableLine {
	out := make([]TaxableLine, 0, len(i.s.Lines))
	for _, l := range i.s.Lines {
		out = append(out, TaxableLine{Ref: l.ID.String(), Base: l.Net, TaxCode: l.TaxCode, Treatment: l.Treatment})
	}
	return out
}
