// Package domain es el modelo de Documents: el registro de los documentos que emiten los otros
// contextos (facturas, pedidos, albaranes, facturas recibidas, nóminas, declaraciones), cada uno
// con su número, fecha, contraparte y el documento del que viene, para poder seguir en un solo
// sitio la traza pedido → albarán → factura → factura rectificativa. En C#, Documents numeraba cada
// hecho con una serie compartida cuyo contador no era seguro con concurrencia; aquí cada contexto
// numera sus propios documentos y Documents sólo registra lo emitido.
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

// DocumentKind es el nombre estable del tipo de agregado.
const DocumentKind = "documents.document"

// Identidades del contexto.
type (
	// DocumentID identifica una anotación del registro.
	DocumentID struct{ fw.UUID }
	// OrganizationID es la empresa a la que pertenece el documento, una organización interna de Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID es la contraparte: cliente, proveedor, empleado.
	PartyID struct{ fw.UUID }
)

// Type es qué es un documento (los tipos de hecho del C# que tienen un contexto emisor).
type Type string

// Tipos.
const (
	Invoice         Type = "invoice"
	CreditNote      Type = "credit-note"
	Order           Type = "order"
	DeliveryNote    Type = "delivery-note"
	ReceivedInvoice Type = "received-invoice"
	Payslip         Type = "payslip"
	TaxFiling       Type = "tax-filing"
)

// Types lista los tipos válidos.
var Types = []Type{Invoice, CreditNote, Order, DeliveryNote, ReceivedInvoice, Payslip, TaxFiling}

// Relaciones entre un documento y aquél del que viene (de las seis sembradas en C#, las dos que algo
// escribe).
const (
	OriginatesFrom = "originates-from"
	Rectifies      = "rectifies"
)

// Ref apunta al hecho al que representa un documento: su tipo y su identidad en el contexto emisor.
type Ref struct {
	Type Type
	ID   string
}

// IsZero dice si la referencia está vacía.
func (r Ref) IsZero() bool { return r == Ref{} }

func (r Ref) valid() bool {
	return slices.Contains(Types, r.Type) && r.ID != "" && len(r.ID) <= 64
}

// DocumentState es el estado persistido de una anotación.
type DocumentState struct {
	Company      OrganizationID
	Fact         Ref
	Number       string // el número que le dio el contexto emisor
	Reference    string // el número que le dio otro (el del proveedor, en una factura recibida)
	Date         vocab.Date
	Party        PartyID
	Total        vocab.Decimal
	HasTotal     bool
	Origin       Ref // el documento del que viene
	Relation     string
	Cancelled    bool
	CancelReason string
	Audit        traits.AuditStamp
}

// Document es una anotación del registro: un documento que emitió algún contexto.
type Document struct {
	fw.BaseAggregateRoot[DocumentID]
	traits.Audited
	s DocumentState
}

// ReconstituteDocument reconstruye una anotación.
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

// RecordDocument anota en el registro un documento emitido.
func RecordDocument(id DocumentID, s DocumentState) (*Document, error) {
	s.Cancelled, s.CancelReason = false, ""
	return ReconstituteDocument(id, s)
}

// State devuelve el estado.
func (d *Document) State() DocumentState { return d.s }

// Cancel marca el documento como anulado por su contexto emisor. Dice si algo cambió: enterarse dos
// veces de una anulación no es un error.
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

// AuditSnapshot implementa traits.Snapshotter.
func (d *Document) AuditSnapshot() map[string]any {
	return map[string]any{"type": string(d.s.Fact.Type), "number": d.s.Number, "cancelled": d.s.Cancelled}
}

// Campos del documento.
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

// ByFact selecciona la anotación de un hecho.
func ByFact(r Ref) spec.Spec[*Document] {
	return DocFieldType.Eq(string(r.Type)).And(DocFieldFact.Eq(r.ID))
}

// DerivedFrom selecciona los documentos que vienen de uno.
func DerivedFrom(r Ref) spec.Spec[*Document] {
	return DocFieldOriginType.Eq(string(r.Type)).And(DocFieldOriginID.Eq(r.ID))
}

// NewDocumentID devuelve una identidad nueva.
func NewDocumentID() DocumentID { return DocumentID{fw.NewUUID()} }

// ParseDocumentID interpreta una identidad en texto.
func ParseDocumentID(s string) (DocumentID, error) {
	u, err := fw.ParseUUID(s)
	return DocumentID{u}, err
}

// DocumentRepository guarda el registro.
type DocumentRepository = fw.Repository[DocumentID, *Document]
