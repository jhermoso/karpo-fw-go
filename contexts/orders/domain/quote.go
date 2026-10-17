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

// QuoteKind es el nombre de tipo del agregado de un presupuesto.
const QuoteKind = "orders.quote"

// DefaultValidityDays es cuánto mantiene un presupuesto sus precios cuando nadie dice otra cosa.
const DefaultValidityDays = 30

// QuoteID identifica un presupuesto.
type QuoteID struct{ fw.UUID }

// NewQuoteID devuelve una identidad nueva.
func NewQuoteID() QuoteID { return QuoteID{fw.NewUUID()} }

// ParseQuoteID interpreta una identidad en texto.
func ParseQuoteID(s string) (QuoteID, error) { u, err := fw.ParseUUID(s); return QuoteID{u}, err }

// QuoteStatus es dónde está un presupuesto. El C# guardaba un número sin más (0, 1, 2) que
// cualquiera podía poner, y un presupuesto emitido aún se podía editar, borrar o devolver.
type QuoteStatus string

// Estados de un presupuesto. Un borrador se trabaja; una vez enviado tiene su número y ya no cambia:
// el cliente lo acepta (se convierte en pedido), lo rechaza o lo deja caducar; la empresa puede
// retirarlo antes de nada de eso.
const (
	QuoteDraft     QuoteStatus = "draft"
	QuoteSent      QuoteStatus = "sent"
	QuoteAccepted  QuoteStatus = "accepted"
	QuoteRejected  QuoteStatus = "rejected"
	QuoteExpired   QuoteStatus = "expired"
	QuoteWithdrawn QuoteStatus = "withdrawn"
)

// QuoteStatuses enumera los estados válidos.
var QuoteStatuses = []QuoteStatus{QuoteDraft, QuoteSent, QuoteAccepted, QuoteRejected, QuoteExpired, QuoteWithdrawn}

// QuoteLine es un producto ofrecido a un precio: los mismos precios que tiene una línea de pedido,
// sin lo que un pedido añade al servirse.
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
	Discount    vocab.Decimal // porcentaje, de la tarifa
	NetPrice    vocab.Decimal
	Amount      vocab.Decimal
}

// QuoteState es el estado persistido de un presupuesto.
type QuoteState struct {
	Company          OrganizationID
	Customer         PartyID
	Number           string // se asigna al enviarlo
	Date             vocab.Date
	ValidUntil       vocab.Date // el último día en que valen sus precios
	PriceList        PriceListID
	CustomerDiscount vocab.Decimal
	Reference        string
	Notes            string
	Status           QuoteStatus
	Lines            []QuoteLine
	Reason           string  // del rechazo o la retirada
	Order            OrderID // el pedido en que se convirtió
	Audit            traits.AuditStamp
}

// Quote es una oferta a un cliente: productos, cantidades y precios que valen hasta un día.
type Quote struct {
	fw.BaseAggregateRoot[QuoteID]
	traits.Audited
	s QuoteState
}

// ReconstituteQuote reconstruye un presupuesto.
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
	// Un borrador aún no tiene número; uno retirado antes de enviarse nunca lo tuvo.
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

// DraftQuote abre un borrador. Sin validez, vale DefaultValidityDays desde su fecha.
func DraftQuote(id QuoteID, s QuoteState) (*Quote, error) {
	s.Status, s.Number, s.Lines, s.Reason, s.Order = QuoteDraft, "", nil, "", OrderID{}
	if s.ValidUntil.IsZero() && !s.Date.IsZero() {
		s.ValidUntil = s.Date.AddDays(DefaultValidityDays)
	}
	return ReconstituteQuote(id, s)
}

// State devuelve el estado (las líneas son una copia).
func (q *Quote) State() QuoteState {
	s := q.s
	s.Lines = slices.Clone(s.Lines)
	return s
}

// Total devuelve la suma de las líneas, antes de impuestos.
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

// Change sustituye lo que un borrador dice de sí mismo: sus fechas, la referencia del cliente y las
// notas. Los precios de sus líneas se quedan como se tomaron.
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

// AddLine añade una línea con precio a un borrador y devuelve su número. Se le pone precio como a
// una línea de pedido: el descuento de la tarifa y luego el del cliente.
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

// RemoveLine quita una línea de un borrador.
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

// Send cierra la oferta con su número: desde aquí ya no cambia.
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

// Accept registra que el cliente tomó la oferta un día dentro de su validez, y el pedido en que se
// convirtió.
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
	if q.s.Number != "" { // un borrador que nadie vio termina sin avisar a nadie
		q.closed("")
	}
	return nil
}

// Reject registra que el cliente dijo que no.
func (q *Quote) Reject(reason string) error {
	if err := q.mustBe(QuoteSent, "orders.quote_not_sent", "only a quote that was sent is rejected"); err != nil {
		return err
	}
	return q.end(QuoteRejected, reason)
}

// Withdraw retira un borrador o una oferta que nadie ha contestado aún.
func (q *Quote) Withdraw(reason string) error {
	if q.s.Status != QuoteDraft && q.s.Status != QuoteSent {
		return fw.Violation("orders.quote_closed", "a quote "+string(q.s.Status)+" is not withdrawn")
	}
	return q.end(QuoteWithdrawn, reason)
}

// Due dice si un presupuesto enviado ha pasado su validez en un día.
func (q *Quote) Due(today vocab.Date) bool {
	return q.s.Status == QuoteSent && today.After(q.s.ValidUntil)
}

// Expire cierra un presupuesto enviado que nadie contestó a tiempo. Dice si lo hizo.
func (q *Quote) Expire(today vocab.Date) bool {
	if !q.Due(today) {
		return false
	}
	_ = q.end(QuoteExpired, "")
	return true
}

// AuditSnapshot implementa traits.Snapshotter.
func (q *Quote) AuditSnapshot() map[string]any {
	return map[string]any{"number": q.s.Number, "status": string(q.s.Status), "validUntil": q.s.ValidUntil.String(), "total": q.Total().String(),
		"lines": len(q.s.Lines), "order": q.s.Order.String()}
}

// Campos del presupuesto.
var (
	QuoFieldCompany  = spec.Comparable("company", func(q *Quote) OrganizationID { return q.s.Company })
	QuoFieldCustomer = spec.Comparable("customer", func(q *Quote) PartyID { return q.s.Customer })
	QuoFieldStatus   = spec.Comparable("status", func(q *Quote) string { return string(q.s.Status) })
	QuoFieldDate     = spec.OrderedBy("quote_date", func(q *Quote) vocab.Date { return q.s.Date }, vocab.CompareDates)
	QuoFieldNumber   = spec.Ordered("quote_number", func(q *Quote) string { return q.s.Number })
)

// QuoteRepository guarda presupuestos.
type QuoteRepository = fw.Repository[QuoteID, *Quote]

// Eventos de un presupuesto.
type (
	// QuoteSentOut se lanza al enviar un presupuesto al cliente.
	QuoteSentOut struct {
		fw.EventMeta
		Company    string     `json:"company"`
		Customer   string     `json:"customer"`
		Number     string     `json:"number"`
		Date       vocab.Date `json:"date"`
		ValidUntil vocab.Date `json:"validUntil"`
		Total      string     `json:"total"`
	}
	// QuoteClosed se lanza cuando un presupuesto enviado se acepta, se rechaza, se retira o caduca.
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

// Implementaciones de EventType.
func (QuoteSentOut) EventType() string { return "orders.quote_sent" }
func (QuoteClosed) EventType() string  { return "orders.quote_closed" }
