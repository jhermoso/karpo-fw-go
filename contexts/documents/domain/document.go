// Package domain is the Documents model: the register of the documents the other contexts issue
// (invoices, orders, delivery notes, received invoices, payslips, tax filings), each with its
// number, date, counterparty and the document it comes from, so the trail order → delivery note →
// invoice → corrective invoice can be followed in one place. In C# Documents numbered every fact
// from a shared series whose counter was not safe under concurrency; here each context numbers
// its own documents and Documents only records what was issued.
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

// DocumentKind is the stable aggregate type name.
const DocumentKind = "documents.document"

// Identities of the context.
type (
	// DocumentID identifies an entry of the register.
	DocumentID struct{ fw.UUID }
	// OrganizationID is the company the document belongs to, an internal organization of Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID is the counterparty: customer, supplier, employee.
	PartyID struct{ fw.UUID }
)

// Type is what a document is (the C# fact types that have an issuing context).
type Type string

// Types.
const (
	Invoice         Type = "invoice"
	CreditNote      Type = "credit-note"
	Order           Type = "order"
	DeliveryNote    Type = "delivery-note"
	ReceivedInvoice Type = "received-invoice"
	Payslip         Type = "payslip"
	TaxFiling       Type = "tax-filing"
)

// Types lists the valid types.
var Types = []Type{Invoice, CreditNote, Order, DeliveryNote, ReceivedInvoice, Payslip, TaxFiling}

// Relations between a document and the one it comes from (of the six seeded in C#, the two that
// something writes).
const (
	OriginatesFrom = "originates-from"
	Rectifies      = "rectifies"
)

// Ref points at the fact a document stands for: its type and its identity in the issuing context.
type Ref struct {
	Type Type
	ID   string
}

// IsZero reports whether the reference is empty.
func (r Ref) IsZero() bool { return r == Ref{} }

func (r Ref) valid() bool {
	return slices.Contains(Types, r.Type) && r.ID != "" && len(r.ID) <= 64
}

// DocumentState is the persisted state of an entry.
type DocumentState struct {
	Company      OrganizationID
	Fact         Ref
	Number       string // the number the issuing context gave it
	Reference    string // the number someone else gave it (the supplier's, on a received invoice)
	Date         vocab.Date
	Party        PartyID
	Total        vocab.Decimal
	HasTotal     bool
	Origin       Ref // the document it comes from
	Relation     string
	Cancelled    bool
	CancelReason string
	Audit        traits.AuditStamp
}

// Document is an entry of the register: a document some context issued.
type Document struct {
	fw.BaseAggregateRoot[DocumentID]
	traits.Audited
	s DocumentState
}

// ReconstituteDocument rebuilds an entry.
func ReconstituteDocument(id DocumentID, s DocumentState) (*Document, error) {
	base, err := fw.NewBaseAggregateRoot(DocumentKind, id)
	if err != nil {
		return nil, err
	}
	s.Number, s.Reference, s.CancelReason = strings.TrimSpace(s.Number), strings.TrimSpace(s.Reference), strings.TrimSpace(s.CancelReason)
	var v fw.Validation
	v.Require(!s.Company.IsZero(), "company", "required", "the company is required")
	v.Require(s.Fact.valid(), "fact", "format", "a document type and the identity of the fact")
	v.Require(s.Number != "" && utf8.RuneCountInString(s.Number) <= 60, "number", "length", "a number of 1 to 60 characters")
	v.Require(utf8.RuneCountInString(s.Reference) <= 60, "reference", "length", "at most 60 characters")
	v.Require(!s.Date.IsZero(), "date", "required", "the date is required")
	v.Require(s.Origin.IsZero() == (s.Relation == ""), "relation", "required", "an origin has a relation, and only an origin")
	v.Require(s.Origin.IsZero() || (s.Origin.valid() && s.Origin != s.Fact), "origin", "format", "another document")
	v.Require(s.Relation == "" || s.Relation == OriginatesFrom || s.Relation == Rectifies, "relation", "enum", "originates-from or rectifies")
	v.Require(utf8.RuneCountInString(s.CancelReason) <= 200, "cancelReason", "length", "at most 200 characters")
	if err := v.Err(); err != nil {
		return nil, err
	}
	if !s.HasTotal {
		s.Total = vocab.DecimalFromInt(0)
	}
	return &Document{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// RecordDocument enters an issued document in the register.
func RecordDocument(id DocumentID, s DocumentState) (*Document, error) {
	s.Cancelled, s.CancelReason = false, ""
	return ReconstituteDocument(id, s)
}

// State returns the state.
func (d *Document) State() DocumentState { return d.s }

// Cancel marks the document as cancelled by its issuing context. It reports whether anything
// changed: hearing of a cancellation twice is not an error.
func (d *Document) Cancel(reason string) bool {
	if d.s.Cancelled {
		return false
	}
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > 200 {
		reason = string([]rune(reason)[:200])
	}
	d.s.Cancelled, d.s.CancelReason = true, reason
	return true
}

// AuditSnapshot implements traits.Snapshotter.
func (d *Document) AuditSnapshot() map[string]any {
	return map[string]any{"type": string(d.s.Fact.Type), "number": d.s.Number, "cancelled": d.s.Cancelled}
}

// Document fields.
var (
	DocFieldCompany    = spec.Comparable("company", func(d *Document) OrganizationID { return d.s.Company })
	DocFieldType       = spec.Comparable("doc_type", func(d *Document) string { return string(d.s.Fact.Type) })
	DocFieldFact       = spec.Comparable("fact_id", func(d *Document) string { return d.s.Fact.ID })
	DocFieldNumber     = spec.Text("doc_number", func(d *Document) string { return d.s.Number })
	DocFieldDate       = spec.OrderedBy("doc_date", func(d *Document) vocab.Date { return d.s.Date }, vocab.CompareDates)
	DocFieldParty      = spec.Comparable("party", func(d *Document) PartyID { return d.s.Party })
	DocFieldOriginType = spec.Comparable("origin_type", func(d *Document) string { return string(d.s.Origin.Type) })
	DocFieldOriginID   = spec.Comparable("origin_id", func(d *Document) string { return d.s.Origin.ID })
	DocFieldCancelled  = spec.Comparable("cancelled", func(d *Document) bool { return d.s.Cancelled })
)

// ByFact selects the entry of a fact.
func ByFact(r Ref) spec.Spec[*Document] {
	return DocFieldType.Eq(string(r.Type)).And(DocFieldFact.Eq(r.ID))
}

// DerivedFrom selects the documents that come from one.
func DerivedFrom(r Ref) spec.Spec[*Document] {
	return DocFieldOriginType.Eq(string(r.Type)).And(DocFieldOriginID.Eq(r.ID))
}

// NewDocumentID returns a new identity.
func NewDocumentID() DocumentID { return DocumentID{fw.NewUUID()} }

// ParseDocumentID parses a textual identity.
func ParseDocumentID(s string) (DocumentID, error) {
	u, err := fw.ParseUUID(s)
	return DocumentID{u}, err
}

// DocumentRepository stores the register.
type DocumentRepository = fw.Repository[DocumentID, *Document]
