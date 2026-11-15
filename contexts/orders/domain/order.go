// Package domain es el modelo de Orders de la fase 1: los pedidos de venta de una empresa (líneas
// con precio, confirmación, existencias retenidas por Inventory, entregas), los albaranes que los
// sirven y las condiciones de cada cliente. El flujo de C# pasaba sus pruebas sobre falsos y no
// podía funcionar sobre el esquema real; nada enlazaba pedidos, envíos, albaranes y existencias, las
// reservas no se liberaban nunca y el precio unitario guardado ya llevaba un descuento que se
// guardaba otra vez a su lado.
package domain

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Nombres de tipo de los agregados.
const (
	OrderKind    = "orders.sales_order"
	DeliveryKind = "orders.delivery"
	TermsKind    = "orders.customer_terms"
	CounterKind  = "orders.counter"
)

// MaxLines limita las líneas de un pedido.
const MaxLines = 500

// Identidades del contexto.
type (
	// OrderID identifica un pedido de venta.
	OrderID struct{ fw.UUID }
	// DeliveryID identifica un albarán.
	DeliveryID struct{ fw.UUID }
	// TermsID identifica las condiciones de un cliente.
	TermsID struct{ fw.UUID }
	// CounterID identifica un contador de numeración.
	CounterID struct{ fw.UUID }
	// OrganizationID es la empresa que vende, una organización interna del contexto Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID es el cliente, una party del contexto Parties.
	PartyID struct{ fw.UUID }
	// ProductID es un producto del contexto Products.
	ProductID struct{ fw.UUID }
	// WarehouseID es un almacén del contexto Inventory.
	WarehouseID struct{ fw.UUID }
	// PriceListID es una tarifa del contexto Products.
	PriceListID struct{ fw.UUID }
)

// Status es el estado de un pedido (el C# llevaba tres sin sincronizar, dos de ellos de escritura
// libre).
type Status int

// Estados. Un pedido confirmado se sirve con entregas hasta que se entrega entero, se cierra con
// parte pendiente o se anula antes de ninguna entrega.
const (
	Draft Status = iota + 1
	Confirmed
	Delivered
	Closed
	Cancelled
)

var statuses = map[Status]string{Draft: "draft", Confirmed: "confirmed", Delivered: "delivered", Closed: "closed", Cancelled: "cancelled"}

// String devuelve el nombre estable.
func (s Status) String() string { return statuses[s] }

// ParseStatus interpreta el nombre de un estado.
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

// discount4 es un descuento de 0 a 100 con los 4 decimales de la columna del descuento de una
// línea de factura.
func discount4(p vocab.Decimal) bool {
	return !p.IsNegative() && !p.GreaterThan(hundred()) && p.Equal(p.Round(4))
}

// EffectiveDiscount es el descuento que se aplica a una línea: el de la tarifa encadenado con el
// del cliente (vocab.ChainedDiscount) y redondeado a 4 decimales, como lo guarda la línea de la
// factura (D-148, D-149). 5 % y 2,5 % dan 7,375 %; 12,5 % y 7,25 %, 18,84375 → 18,8438 %.
func EffectiveDiscount(line, customer vocab.Decimal) vocab.Decimal {
	return vocab.ChainedDiscount(line, customer).Round(4)
}

// LineAmount es el importe de una cantidad a un precio con un descuento (un porcentaje), calculado
// como el neto de una línea de factura con el redondeo por omisión (once): cantidad × precio menos
// el descuento, redondeado una sola vez al céntimo, el medio alejándose de cero (D-149). 1000 × 1 €
// con un 7,375 % dan 926,25; con el precio neto unitario redondeado antes (0,9263) darían 926,30.
func LineAmount(quantity, unitPrice, discount vocab.Decimal) vocab.Decimal {
	gross := quantity.Mul(unitPrice)
	return gross.Sub(gross.Mul(discount).Div(hundred())).Round(2)
}

// Line es una línea de un pedido. UnitPrice es el precio antes de cualquier descuento; Discount es
// el de la tarifa; ChainedDiscount, el que se aplica: el de la tarifa encadenado con el del cliente
// del pedido, a 4 decimales. Amount se calcula como el neto de una línea de factura (LineAmount,
// D-149), así que el pedido, su albarán y su factura dan el mismo importe y ningún descuento se
// aplica dos veces. NetPrice es sólo informativo: no entra en ningún importe.
type Line struct {
	No              int
	Product         ProductID
	SKU             string
	Description     string
	UoM             string
	TaxCode         string
	Stocked         bool // Inventory retiene y da salida a sus existencias
	Quantity        vocab.Decimal
	UnitPrice       vocab.Decimal
	Discount        vocab.Decimal // porcentaje, el de la tarifa
	ChainedDiscount vocab.Decimal // porcentaje que se aplica: el de la tarifa y el del cliente, 4 decimales
	NetPrice        vocab.Decimal // informativo: precio unitario con ChainedDiscount, 4 decimales
	Amount          vocab.Decimal // LineAmount(cantidad, precio, ChainedDiscount), en céntimos
	Reserved        vocab.Decimal // retenido por Inventory y aún no entregado
	Delivered       vocab.Decimal
	// Invoiced es, en un servicio que se factura desde el pedido (D-155), la cantidad ya enviada a
	// Facturación; cero en las demás líneas.
	Invoiced vocab.Decimal
}

// Pending devuelve lo que queda por entregar.
func (l Line) Pending() vocab.Decimal { return l.Quantity.Sub(l.Delivered) }

// Short devuelve lo pendiente y no retenido: lo que aún se pide a Inventory.
func (l Line) Short() vocab.Decimal {
	if !l.Stocked {
		return zero()
	}
	return l.Pending().Sub(l.Reserved)
}

// price deriva el descuento que se aplica, el precio neto informativo y el importe de una línea:
// el descuento del cliente se encadena con el de la línea (EffectiveDiscount, el mismo que guarda
// Billing en la factura directa, D-148) y el importe sale del precio, no del neto (D-149).
func price(l Line, customerDiscount vocab.Decimal) Line {
	l.ChainedDiscount = EffectiveDiscount(l.Discount, customerDiscount)
	l.NetPrice = l.UnitPrice.Mul(hundred().Sub(l.ChainedDiscount)).Div(hundred()).Round(4)
	l.Amount = LineAmount(l.Quantity, l.UnitPrice, l.ChainedDiscount)
	return l
}

// OrderState es el estado persistido de un pedido.
type OrderState struct {
	Company          OrganizationID
	Customer         PartyID
	Number           string // se asigna al confirmar
	Date             vocab.Date
	Warehouse        WarehouseID // desde donde se sirven sus líneas con existencias
	PriceList        PriceListID // la tarifa con la que se puso precio a sus líneas
	CustomerDiscount vocab.Decimal
	Reference        string // la referencia propia del cliente
	Notes            string
	Status           Status
	Lines            []Line
	CloseReason      string
	// ServiceInvoicings son las cantidades de servicios enviadas a Facturación (D-155), una fila
	// por emisión y línea; las de un borrador descartado se quitan.
	ServiceInvoicings []ServiceInvoicing
	Audit             traits.AuditStamp
}

// ServiceInvoicing es la cantidad de una línea de servicio que una emisión envió a Facturación
// (D-155).
type ServiceInvoicing struct {
	ID       fw.UUID
	Line     int
	Quantity vocab.Decimal
}

// Order es un pedido de venta.
type Order struct {
	fw.BaseAggregateRoot[OrderID]
	traits.Audited
	s OrderState
}

// ReconstituteOrder reconstruye un pedido.
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
		v.Require(discount4(l.ChainedDiscount), f+".chainedDiscount", "range", "a discount from 0 to 100 with up to 4 decimals")
		v.Require(!l.Delivered.IsNegative() && !l.Delivered.GreaterThan(l.Quantity) && !l.Reserved.IsNegative() && !l.Reserved.GreaterThan(l.Pending()),
			f+".delivered", "range", "delivered within the quantity, reserved within what is pending")
		v.Require(!l.Invoiced.IsNegative() && !l.Invoiced.GreaterThan(l.Quantity), f+".invoiced", "range", "invoiced within the quantity")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Lines, s.ServiceInvoicings = slices.Clone(s.Lines), slices.Clone(s.ServiceInvoicings)
	return &Order{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// DraftOrder abre un borrador.
func DraftOrder(id OrderID, s OrderState) (*Order, error) {
	s.Status, s.Number, s.Lines, s.CloseReason = Draft, "", nil, ""
	return ReconstituteOrder(id, s)
}

// State devuelve el estado (las líneas son una copia).
func (o *Order) State() OrderState {
	s := o.s
	s.Lines, s.ServiceInvoicings = slices.Clone(s.Lines), slices.Clone(s.ServiceInvoicings)
	return s
}

// Total devuelve la suma de las líneas, antes de impuestos.
func (o *Order) Total() vocab.Decimal {
	t := zero()
	for _, l := range o.s.Lines {
		t = t.Add(l.Amount)
	}
	return t
}

// PendingAmount devuelve el importe de lo que queda por entregar, línea a línea como lo facturaría
// su albarán.
func (o *Order) PendingAmount() vocab.Decimal {
	t := zero()
	for _, l := range o.s.Lines {
		t = t.Add(LineAmount(l.Pending(), l.UnitPrice, l.ChainedDiscount))
	}
	return t
}

func (o *Order) mustBe(st Status, code, msg string) error {
	if o.s.Status != st {
		return fw.Violation(code, msg)
	}
	return nil
}

// AddLine añade una línea con precio a un borrador y devuelve su número.
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
	l.Reserved, l.Delivered, l.Invoiced = zero(), zero(), zero()
	s := o.s
	s.Lines = append(slices.Clone(o.s.Lines), price(l, o.s.CustomerDiscount))
	if _, err := ReconstituteOrder(o.ID(), s); err != nil {
		return 0, err
	}
	o.s.Lines = s.Lines
	return l.No, nil
}

// RemoveLine quita una línea de un borrador.
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

// Confirm cierra el pedido con su número. Un pedido confirmado ya no cambia: se sirve, se
// cierra o se anula.
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

// requestStock lanza la petición de lo pendiente y no retenido, si hay algo.
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

// RequestStock vuelve a pedir a Inventory lo que aún falta.
func (o *Order) RequestStock() error {
	if err := o.mustBe(Confirmed, "orders.not_confirmed", "only a confirmed order holds stock"); err != nil {
		return err
	}
	if !o.requestStock() {
		return fw.Violation("orders.nothing_short", "everything pending is already held")
	}
	return nil
}

// Hold registra existencias retenidas por Inventory para una línea: se suman a lo que la línea
// retiene, nunca más allá de lo pendiente. Se ignora cuando el pedido ya no las espera.
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

// Pick es una cantidad de una línea que se entrega.
type Pick struct {
	Line     int
	Quantity vocab.Decimal
}

// Deliver sirve parte del pedido y devuelve las líneas de su albarán. Invariantes: un pedido
// confirmado, cantidades positivas dentro de lo pendiente y, en una línea con existencias, dentro
// de lo que Inventory retiene para ella (el C# no enlazaba un pedido con lo que salía del almacén).
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
			Stocked: l.Stocked, Quantity: p.Quantity, UnitPrice: l.UnitPrice, Discount: l.ChainedDiscount, NetPrice: l.NetPrice,
			Amount: LineAmount(p.Quantity, l.UnitPrice, l.ChainedDiscount)})
	}
	o.s.Lines = lines
	if !slices.ContainsFunc(lines, func(l Line) bool { return l.Pending().IsPositive() }) {
		o.s.Status = Delivered
		o.servingCompleted()
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
			held = append(held, lines[k].No) // Inventory puede retener, o estar a punto de retener, existencias para ella
		}
		lines[k].Reserved = zero()
	}
	o.s.Lines, o.s.Status, o.s.CloseReason = lines, st, reason
	o.Raise(OrderClosed{EventMeta: o.NewEventMeta(), Company: o.s.Company.String(), Status: st.String(), Reason: reason, Lines: held})
	return nil
}

// Cancel retira un pedido del que no se entregó nada; lo que retiene se libera.
func (o *Order) Cancel(reason string) error {
	if o.s.Status != Draft && o.s.Status != Confirmed {
		return fw.Violation("orders.closed", "the order is already "+o.s.Status.String())
	}
	if slices.ContainsFunc(o.s.Lines, func(l Line) bool { return l.Delivered.IsPositive() }) {
		return fw.Violation("orders.delivered", "part of the order was delivered: close it instead")
	}
	// Servicios ya enviados a facturar (D-155, on_confirm o a petición): anular dejaría una
	// factura sin pedido. Se descarta antes su borrador o se rectifica la factura.
	if slices.ContainsFunc(o.s.Lines, func(l Line) bool { return l.Invoiced.IsPositive() }) {
		return fw.Violation("orders.services_invoiced", "services of the order were invoiced: discard their draft or correct the invoice first")
	}
	return o.end(Cancelled, reason)
}

// Close termina un pedido entregado en parte: lo pendiente no se servirá y lo que retiene se
// libera. Ya no queda nada pendiente de servir: el pedido queda servido (D-153).
func (o *Order) Close(reason string) error {
	if err := o.mustBe(Confirmed, "orders.not_confirmed", "only a confirmed order is closed"); err != nil {
		return err
	}
	if !slices.ContainsFunc(o.s.Lines, func(l Line) bool { return l.Delivered.IsPositive() }) {
		return fw.Violation("orders.nothing_delivered", "nothing was delivered: cancel it instead")
	}
	if err := o.end(Closed, reason); err != nil {
		return err
	}
	o.servingCompleted()
	return nil
}

// ServedEntirely dice si al pedido no le queda nada pendiente de servir: entregado entero, o
// cerrado con lo pendiente que ya no se servirá (D-153).
func (o *Order) ServedEntirely() bool { return o.s.Status == Delivered || o.s.Status == Closed }

// servingCompleted lanza SalesOrderServingCompleted: el pedido pasa a no tener nada pendiente de
// servir (D-153).
func (o *Order) servingCompleted() {
	o.Raise(SalesOrderServingCompleted{EventMeta: o.NewEventMeta(), Company: o.s.Company.String(), Customer: o.s.Customer.String(), Number: o.s.Number})
}

// ReopenServing lanza SalesOrderServingReopened si el pedido estaba servido entero (D-153). En el
// dominio de Go aún no hay nada que reabra un pedido: no hay devoluciones de venta, las líneas no
// cambian después de confirmar y entregado, cerrado y anulado son finales. Es el punto por el que
// se reabrirá (la devolución de venta, cuando llegue, lo llamará después de devolver a pendiente lo
// que vuelve); hoy sólo lo usan las pruebas.
func (o *Order) ReopenServing() {
	if !o.ServedEntirely() {
		return
	}
	o.Raise(SalesOrderServingReopened{EventMeta: o.NewEventMeta(), Company: o.s.Company.String(), Customer: o.s.Customer.String(), Number: o.s.Number})
}

// Cuándo se facturan los servicios que se facturan desde el pedido (D-155; Javier, 2026-11-15:
// las tres configurables, por defecto al servir). Es el ajuste de la empresa en Parties.
const (
	ServiceInvoicingOnServe   = "on_serve"
	ServiceInvoicingOnConfirm = "on_confirm"
	ServiceInvoicingManual    = "manual"
)

// InvoiceServedServices envía a Facturación lo servido y aún no facturado de las líneas de
// servicio direct (al servir, on_serve). Devuelve el id de la emisión, o cero si no había nada.
func (o *Order) InvoiceServedServices(direct map[int]bool, id fw.UUID, date vocab.Date) fw.UUID {
	return o.invoiceServices(direct, id, date, func(l Line) vocab.Decimal { return l.Delivered })
}

// InvoiceServices envía a Facturación lo aún no facturado de las líneas de servicio direct: la
// cantidad entera (on_confirm y manual), o sólo lo servido si onlyServed (on_serve: nunca lo no
// servido) o si el pedido se cerró con algo pendiente. Sólo un pedido confirmado, entregado o
// cerrado; sin nada que facturar, 422 orders.nothing_to_invoice.
func (o *Order) InvoiceServices(direct map[int]bool, id fw.UUID, date vocab.Date, onlyServed bool) (fw.UUID, error) {
	switch o.s.Status {
	case Confirmed, Delivered, Closed:
	default:
		return fw.UUID{}, fw.Violation("orders.not_confirmed", "only a confirmed order invoices its services")
	}
	served := onlyServed || o.s.Status == Closed
	got := o.invoiceServices(direct, id, date, func(l Line) vocab.Decimal {
		if served {
			return l.Delivered
		}
		return l.Quantity
	})
	if got.IsZero() {
		return got, fw.Violation("orders.nothing_to_invoice", "the order has no service pending to invoice")
	}
	return got, nil
}

// invoiceServices factura de cada línea direct lo que va de lo ya facturado a upTo: sólo esa
// diferencia, nunca dos veces lo mismo, y lanza ServicesInvoiceable con el id de la emisión.
func (o *Order) invoiceServices(direct map[int]bool, id fw.UUID, date vocab.Date, upTo func(Line) vocab.Decimal) fw.UUID {
	lines := slices.Clone(o.s.Lines)
	var out []ServiceInvoiceLine
	var rows []ServiceInvoicing
	for k := range lines {
		l := &lines[k]
		q := upTo(*l).Sub(l.Invoiced)
		if !direct[l.No] || !q.IsPositive() {
			continue
		}
		l.Invoiced = l.Invoiced.Add(q)
		rows = append(rows, ServiceInvoicing{ID: id, Line: l.No, Quantity: q})
		out = append(out, ServiceInvoiceLine{Line: l.No, Product: l.Product.String(), SKU: l.SKU, Description: l.Description, TaxCode: l.TaxCode,
			Quantity: q.String(), UnitPrice: l.UnitPrice.StringFixed(4), Discount: l.ChainedDiscount.StringFixed(4)})
	}
	if len(out) == 0 {
		return fw.UUID{}
	}
	o.s.Lines, o.s.ServiceInvoicings = lines, append(slices.Clone(o.s.ServiceInvoicings), rows...)
	o.Raise(ServicesInvoiceable{EventMeta: o.NewEventMeta(), Invoicing: id.String(), Company: o.s.Company.String(), Customer: o.s.Customer.String(),
		Number: o.s.Number, Date: date.String(), Lines: out})
	return id
}

// ReleaseServiceInvoicing devuelve a pendiente de facturar lo que envió una emisión cuyo borrador
// se ha descartado en Facturación (D-155, como D-154 con los albaranes). Una emisión que ya no
// está (descartada antes) no hace nada.
func (o *Order) ReleaseServiceInvoicing(id fw.UUID) {
	lines := slices.Clone(o.s.Lines)
	var keep []ServiceInvoicing
	for _, r := range o.s.ServiceInvoicings {
		if r.ID != id {
			keep = append(keep, r)
			continue
		}
		if k := slices.IndexFunc(lines, func(l Line) bool { return l.No == r.Line }); k >= 0 {
			lines[k].Invoiced = lines[k].Invoiced.Sub(r.Quantity)
			if lines[k].Invoiced.IsNegative() {
				lines[k].Invoiced = zero()
			}
		}
	}
	o.s.Lines, o.s.ServiceInvoicings = lines, keep
}

// AuditSnapshot implementa traits.Snapshotter.
func (o *Order) AuditSnapshot() map[string]any {
	return map[string]any{"number": o.s.Number, "status": o.s.Status.String(), "total": o.Total().String(), "lines": len(o.s.Lines)}
}

type lineNo int

func (n lineNo) String() string { return fmt.Sprint(int(n)) }

// Campos del pedido.
var (
	OrdFieldCompany  = spec.Comparable("company", func(o *Order) OrganizationID { return o.s.Company })
	OrdFieldCustomer = spec.Comparable("customer", func(o *Order) PartyID { return o.s.Customer })
	OrdFieldStatus   = spec.Comparable("status", func(o *Order) int { return int(o.s.Status) })
	OrdFieldDate     = spec.OrderedBy("order_date", func(o *Order) vocab.Date { return o.s.Date }, vocab.CompareDates)
	OrdFieldNumber   = spec.Ordered("order_number", func(o *Order) string { return o.s.Number })
)

// DeliveryLine es una línea de un albarán. Lleva el precio y el descuento que se aplica (el
// encadenado de la línea del pedido), como una línea de factura, y su importe se calcula igual
// (LineAmount, D-149): la factura del albarán da lo mismo al céntimo. NetPrice es informativo. En
// los albaranes de antes de D-149, UnitPrice es el precio neto y Discount, cero.
type DeliveryLine struct {
	Line        int // la línea del pedido que sirve
	Product     ProductID
	SKU         string
	Description string
	UoM         string
	TaxCode     string
	Stocked     bool
	Quantity    vocab.Decimal
	UnitPrice   vocab.Decimal
	Discount    vocab.Decimal // porcentaje, 4 decimales
	NetPrice    vocab.Decimal // informativo
	Amount      vocab.Decimal
}

// DeliveryState es el estado persistido de un albarán.
type DeliveryState struct {
	Company     OrganizationID
	Customer    PartyID
	Order       OrderID
	OrderNumber string
	Number      string
	Date        vocab.Date
	Warehouse   WarehouseID
	Lines       []DeliveryLine
	Invoice     string // id de la factura de Billing que lo factura
	InvoiceNo   string
	Audit       traits.AuditStamp
}

// Delivery es un albarán: lo que salió para el cliente de un pedido, con sus líneas (el
// DeliveryNote de C# era una cabecera sin líneas ni pedido, editable después de emitido).
type Delivery struct {
	fw.BaseAggregateRoot[DeliveryID]
	traits.Audited
	s DeliveryState
}

// ReconstituteDelivery reconstruye un albarán.
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

// MarkInvoiced registra la factura que factura el albarán (sus mercancías y precios no cambian
// nunca; esto es lo único que se le añade). Se queda la primera factura.
func (d *Delivery) MarkInvoiced(invoice, number string) {
	if d.s.Invoice == "" && invoice != "" {
		d.s.Invoice, d.s.InvoiceNo = invoice, number
	}
}

// IssueDelivery emite el albarán de lo que un pedido acaba de entregar. No cambia nunca. El evento
// lleva la periodicidad de facturación del cliente (D-146), para que Billing sepa si prepara su
// borrador o lo deja pendiente del cierre del periodo, y si el pedido queda servido entero con
// él (OrderComplete, D-153).
func IssueDelivery(id DeliveryID, o *Order, number string, date vocab.Date, lines []DeliveryLine, billing BillingPeriodicity) (*Delivery, error) {
	os := o.State()
	d, err := ReconstituteDelivery(id, DeliveryState{Company: os.Company, Customer: os.Customer, Order: o.ID(), OrderNumber: os.Number, Number: number,
		Date: date, Warehouse: os.Warehouse, Lines: lines})
	if err != nil {
		return nil, err
	}
	if billing == "" {
		billing = PerDelivery
	}
	d.Raise(DeliveryIssued{EventMeta: d.NewEventMeta(), Snapshot: d.s, BillingPeriodicity: billing, OrderComplete: o.ServedEntirely()})
	return d, nil
}

// State devuelve el estado (las líneas son una copia).
func (d *Delivery) State() DeliveryState {
	s := d.s
	s.Lines = slices.Clone(s.Lines)
	return s
}

// Total devuelve la suma de las líneas, antes de impuestos.
func (d *Delivery) Total() vocab.Decimal {
	t := zero()
	for _, l := range d.s.Lines {
		t = t.Add(l.Amount)
	}
	return t
}

// AuditSnapshot implementa traits.Snapshotter.
func (d *Delivery) AuditSnapshot() map[string]any {
	return map[string]any{"number": d.s.Number, "order": d.s.OrderNumber, "total": d.Total().String()}
}

// Campos del albarán.
var (
	DelFieldCompany  = spec.Comparable("company", func(d *Delivery) OrganizationID { return d.s.Company })
	DelFieldCustomer = spec.Comparable("customer", func(d *Delivery) PartyID { return d.s.Customer })
	DelFieldOrder    = spec.Comparable("order_id", func(d *Delivery) OrderID { return d.s.Order })
	DelFieldNumber   = spec.Ordered("delivery_number", func(d *Delivery) string { return d.s.Number })
	DelFieldInvoiced = spec.Comparable("invoiced", func(d *Delivery) bool { return d.s.Invoice != "" })
)

// TermsState es el estado persistido de las condiciones de un cliente.
type TermsState struct {
	Company       OrganizationID
	Customer      PartyID
	PriceList     PriceListID
	Discount      vocab.Decimal // porcentaje sobre cada línea
	BlockOrders   bool
	BlockDelivery bool
	// BlockReason dice por qué están bloqueados los pedidos o las entregas (D-122); se borra al
	// desbloquear los dos. UnblockReason dice por qué se desbloqueó la última vez; se borra al
	// bloquear (D-156).
	BlockReason   string
	UnblockReason string
	// BlockEvents es el historial de bloqueos y desbloqueos (ord_terms_block_events), del más
	// antiguo al más reciente.
	BlockEvents []BlockEvent
	// BillingPeriodicity dice cuándo se le factura lo entregado (D-146); vacía, PerDelivery.
	BillingPeriodicity BillingPeriodicity
	// LineRounding y PriceDecimals son el cálculo de las facturas del cliente cuando no es el de la
	// empresa (la cascada cliente > empresa de D-144, D-150): once o gross_then_discount, y de 0 a
	// 6 decimales en el precio. Vacío y nil, los de la empresa.
	LineRounding  string
	PriceDecimals *int
	// PartialBillingPolicy es la política de facturación parcial del cliente, que gana a la de la
	// empresa (D-153, cascada de D-144): allow_partial o require_complete_order; vacía, la de la
	// empresa.
	PartialBillingPolicy string
	Audit                traits.AuditStamp
}

// Políticas de facturación parcial de un pedido (D-153), las de la empresa en Parties.
const (
	PartialBillingAllowPartial         = "allow_partial"
	PartialBillingRequireCompleteOrder = "require_complete_order"
)

// Redondeos de la línea que puede pedir un cliente (los de la empresa, D-144).
const (
	RoundOnce              = "once"
	RoundGrossThenDiscount = "gross_then_discount"
	// MaxPriceDecimals son los decimales del precio que puede pedir un cliente, como su empresa.
	MaxPriceDecimals = 6
)

// BillingPeriodicity es cuándo se factura a un cliente lo que se le entrega (D-146; el
// BillingPeriodicity del perfil comercial de Trench, que guarda un código de Sage).
type BillingPeriodicity string

// Periodicidades de facturación.
const (
	// PerDelivery, un borrador de factura por albarán, en cuanto se emite (lo de siempre).
	PerDelivery BillingPeriodicity = "per_delivery"
	// Monthly, una factura con todos los albaranes del periodo, al cerrarlo en Billing.
	Monthly BillingPeriodicity = "monthly"
)

// Terms son las condiciones de venta que una empresa da a un cliente (la parte de ventas del
// CustomerRelationshipCommercialProfile de C#, que se buscaba sin mirar la empresa, cuyo
// BlockDelivery nada hacía cumplir y cuyo MaxRisk comparaba un solo pedido: el límite de crédito
// está en Receivables).
type Terms struct {
	fw.BaseAggregateRoot[TermsID]
	traits.Audited
	s TermsState
}

// MaxBlockReason es lo que cabe en el motivo de un bloqueo (el BlockReason de Trench).
const MaxBlockReason = 200

// ReconstituteTerms reconstruye las condiciones de un cliente.
func ReconstituteTerms(id TermsID, s TermsState) (*Terms, error) {
	base, err := fw.NewBaseAggregateRoot(TermsKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero() && !s.Customer.IsZero(), "customer", "required", "company and customer are required")
	v.Require(percent(s.Discount), "discount", "range", "a percentage from 0 to 100")
	if s.BillingPeriodicity == "" {
		s.BillingPeriodicity = PerDelivery
	}
	v.Require(s.BillingPeriodicity == PerDelivery || s.BillingPeriodicity == Monthly, "billingPeriodicity", "enum", "per_delivery or monthly")
	v.Require(s.LineRounding == "" || s.LineRounding == RoundOnce || s.LineRounding == RoundGrossThenDiscount, "lineRounding", "enum",
		"once or gross_then_discount")
	v.Require(s.PartialBillingPolicy == "" || s.PartialBillingPolicy == PartialBillingAllowPartial || s.PartialBillingPolicy == PartialBillingRequireCompleteOrder,
		"partialBillingPolicy", "enum", "allow_partial or require_complete_order")
	if s.PriceDecimals != nil {
		n := *s.PriceDecimals // una copia: el agregado no comparte memoria con quien lo llama
		s.PriceDecimals = &n
		v.Require(n >= 0 && n <= MaxPriceDecimals, "priceDecimals", "range", "from 0 to 6")
	}
	if !s.BlockOrders && !s.BlockDelivery {
		s.BlockReason = ""
	}
	s.BlockReason, s.UnblockReason = strings.TrimSpace(s.BlockReason), strings.TrimSpace(s.UnblockReason)
	checkReason(&v, "blockReason", s.BlockReason)
	checkReason(&v, "unblockReason", s.UnblockReason)
	s.BlockEvents = slices.Clone(s.BlockEvents)
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Terms{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

func checkReason(v *fw.Validation, field, r string) {
	v.Require(utf8.RuneCountInString(r) <= MaxBlockReason, field, "length", "the reason has at most 200 characters")
	v.Require(!strings.ContainsFunc(r, unicode.IsControl), field, "format", "no control characters")
}

// State devuelve el estado.
func (t *Terms) State() TermsState { return t.s }

// Change sustituye las condiciones (empresa y cliente se quedan). El bloqueo no cambia aquí: tiene
// sus órdenes, Block y Unblock (D-156).
func (t *Terms) Change(s TermsState) error {
	s.Company, s.Customer, s.Audit = t.s.Company, t.s.Customer, t.s.Audit
	s.BlockOrders, s.BlockDelivery, s.BlockReason, s.UnblockReason, s.BlockEvents = t.s.BlockOrders, t.s.BlockDelivery, t.s.BlockReason,
		t.s.UnblockReason, t.s.BlockEvents
	n, err := ReconstituteTerms(t.ID(), s)
	if err != nil {
		return err
	}
	t.s = n.s
	return nil
}

// BlockScope es lo que se bloquea de un cliente: los pedidos, las entregas o los dos (el
// CommercialBlockScope de Trench, con sus nombres).
type BlockScope struct {
	Orders   bool
	Delivery bool
}

// Ámbitos del bloqueo, como se escriben.
const (
	ScopeOrders   = "orders"
	ScopeDelivery = "delivery"
)

// ParseBlockScope lee los ámbitos ("orders", "delivery"); ninguno es el ámbito vacío.
func ParseBlockScope(texts []string) (BlockScope, error) {
	var s BlockScope
	for _, t := range texts {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case ScopeOrders:
			s.Orders = true
		case ScopeDelivery:
			s.Delivery = true
		default:
			var v fw.Validation
			v.Add("scopes", "enum", "orders or delivery")
			return BlockScope{}, v.Err()
		}
	}
	return s, nil
}

// Texts devuelve los ámbitos como se escriben.
func (s BlockScope) Texts() []string {
	out := []string{}
	if s.Orders {
		out = append(out, ScopeOrders)
	}
	if s.Delivery {
		out = append(out, ScopeDelivery)
	}
	return out
}

// Empty dice si no hay ningún ámbito.
func (s BlockScope) Empty() bool { return !s.Orders && !s.Delivery }

// BlockEvent es un bloqueo o un desbloqueo efectivo (D-156 ampliada; el commercial_block_event de
// Trench): qué ámbitos cambiaron, quién, cuándo y por qué. Si pasó por aprobación, Approval es la
// solicitud y By, quien la aprobó.
type BlockEvent struct {
	Blocked  bool
	Scope    BlockScope
	Reason   string
	At       time.Time
	By       vocab.Actor
	Approval fw.UUID
}

// Códigos de las infracciones del bloqueo (D-156).
const (
	CodeBlockReasonRequired        = "orders.block_reason_required"
	CodeUnblockReasonRequired      = "orders.unblock_reason_required"
	CodeBlockScopeRequired         = "orders.block_scope_required"
	CodeBlockChangeRequiresCommand = "orders.block_change_requires_command"
)

// CheckBlock comprueba una orden de bloquear (blocked) o desbloquear: el motivo es obligatorio,
// de 200 caracteres como mucho y sin caracteres de control; bloquear pide al menos un ámbito
// (desbloquear sin ámbito es desbloquear todo).
func CheckBlock(blocked bool, scope BlockScope, reason string) error {
	reason = strings.TrimSpace(reason)
	switch {
	case blocked && scope.Empty():
		return fw.Violation(CodeBlockScopeRequired, "say what to block: orders, delivery or both")
	case reason == "" && blocked:
		return fw.Violation(CodeBlockReasonRequired, "blocking a customer needs a reason")
	case reason == "":
		return fw.Violation(CodeUnblockReasonRequired, "unblocking a customer needs a reason")
	}
	var v fw.Validation
	checkReason(&v, "reason", reason)
	return v.Err()
}

// Changes devuelve los ámbitos que una orden de bloquear (blocked) o desbloquear cambiaría de
// verdad; desbloquear sin ámbito es desbloquear lo que esté bloqueado.
func (t *Terms) Changes(blocked bool, scope BlockScope) BlockScope {
	if !blocked && scope.Empty() {
		scope = BlockScope{Orders: true, Delivery: true}
	}
	return BlockScope{Orders: scope.Orders && t.s.BlockOrders != blocked, Delivery: scope.Delivery && t.s.BlockDelivery != blocked}
}

// SetBlocked bloquea (blocked) o desbloquea los ámbitos con su motivo, que es obligatorio, y deja
// el cambio en el historial con quién, cuándo y la solicitud aprobada, si la hubo. Devuelve lo que
// ha cambiado de verdad (vacío si ya estaba así). Bloquear sustituye el motivo vigente; desbloquear
// guarda el suyo y borra el del bloqueo cuando no queda nada bloqueado.
func (t *Terms) SetBlocked(blocked bool, scope BlockScope, reason string, by vocab.Actor, at time.Time, approval fw.UUID) (BlockScope, error) {
	if err := CheckBlock(blocked, scope, reason); err != nil {
		return BlockScope{}, err
	}
	reason = strings.TrimSpace(reason)
	changed := t.Changes(blocked, scope)
	if changed.Empty() {
		return changed, nil
	}
	if changed.Orders {
		t.s.BlockOrders = blocked
	}
	if changed.Delivery {
		t.s.BlockDelivery = blocked
	}
	if blocked {
		t.s.BlockReason, t.s.UnblockReason = reason, ""
	} else {
		t.s.UnblockReason = reason
		if !t.s.BlockOrders && !t.s.BlockDelivery {
			t.s.BlockReason = ""
		}
	}
	t.s.BlockEvents = append(slices.Clone(t.s.BlockEvents), BlockEvent{Blocked: blocked, Scope: changed, Reason: reason, At: at, By: by, Approval: approval})
	return changed, nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (t *Terms) AuditSnapshot() map[string]any {
	s := map[string]any{"discount": t.s.Discount.String(), "blockOrders": t.s.BlockOrders, "blockDelivery": t.s.BlockDelivery,
		"billingPeriodicity": string(t.s.BillingPeriodicity)}
	if t.s.BlockReason != "" {
		s["blockReason"] = t.s.BlockReason
	}
	if t.s.UnblockReason != "" {
		s["unblockReason"] = t.s.UnblockReason
	}
	if t.s.LineRounding != "" {
		s["lineRounding"] = t.s.LineRounding
	}
	if t.s.PriceDecimals != nil {
		s["priceDecimals"] = *t.s.PriceDecimals
	}
	if t.s.PartialBillingPolicy != "" {
		s["partialBillingPolicy"] = t.s.PartialBillingPolicy
	}
	return s
}

// Campos de las condiciones.
var (
	TrmFieldCompany  = spec.Comparable("company", func(t *Terms) OrganizationID { return t.s.Company })
	TrmFieldCustomer = spec.Comparable("customer", func(t *Terms) PartyID { return t.s.Customer })
)

// Counter numera los pedidos o los albaranes de una empresa y año.
type Counter struct {
	fw.BaseAggregateRoot[CounterID]
	company OrganizationID
	series  string // PED o ALB
	year    int
	last    int64
}

// ReconstituteCounter reconstruye un contador.
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

// Next toma el número siguiente, con la forma SERIE-AÑO-NNNNNN.
func (c *Counter) Next() string {
	c.last++
	return fmt.Sprintf("%s-%d-%06d", c.series, c.year, c.last)
}

// Values devuelve los valores persistidos.
func (c *Counter) Values() (OrganizationID, string, int, int64) {
	return c.company, c.series, c.year, c.last
}

// Campos del contador.
var (
	CntFieldCompany = spec.Comparable("company", func(c *Counter) OrganizationID { return c.company })
	CntFieldSeries  = spec.Comparable("series", func(c *Counter) string { return c.series })
	CntFieldYear    = spec.Comparable("fiscal_year", func(c *Counter) int { return c.year })
)

// Eventos del contexto.
type (
	// OrderConfirmed se lanza al confirmar un pedido.
	OrderConfirmed struct {
		fw.EventMeta
		Company  string `json:"company"`
		Customer string `json:"customer"`
		Number   string `json:"number"`
		Total    string `json:"total"`
	}
	// StockLine es una cantidad de una línea que se retiene.
	StockLine struct {
		Line     int    `json:"line"`
		Product  string `json:"product"`
		Quantity string `json:"quantity"`
	}
	// StockRequested se lanza cuando un pedido pide a Inventory que retenga lo que le falta.
	StockRequested struct {
		fw.EventMeta
		Company   string      `json:"company"`
		Warehouse string      `json:"warehouse"`
		Lines     []StockLine `json:"lines"`
	}
	// OrderClosed se lanza al anular o cerrar un pedido, con las líneas que pueden retener existencias.
	OrderClosed struct {
		fw.EventMeta
		Company string `json:"company"`
		Status  string `json:"status"`
		Reason  string `json:"reason"`
		Lines   []int  `json:"lines"`
	}
	// DeliveryIssued se lanza al emitir un albarán, con su estado.
	DeliveryIssued struct {
		fw.EventMeta
		Snapshot           DeliveryState      `json:"snapshot"`
		BillingPeriodicity BillingPeriodicity `json:"billingPeriodicity"`
		// OrderComplete dice si al pedido no le queda nada pendiente de servir tras el albarán (D-153).
		OrderComplete bool `json:"orderComplete"`
	}
	// SalesOrderServingCompleted se lanza cuando a un pedido deja de quedarle nada pendiente de
	// servir: por su último albarán o al cerrarlo con lo pendiente (D-153).
	SalesOrderServingCompleted struct {
		fw.EventMeta
		Company  string `json:"company"`
		Customer string `json:"customer"`
		Number   string `json:"number"`
	}
	// ServiceInvoiceLine es la cantidad de una línea de servicio que se envía a facturar, con su
	// precio y el descuento que se aplica (D-155).
	ServiceInvoiceLine struct {
		Line        int    `json:"line"`
		Product     string `json:"product"`
		SKU         string `json:"sku"`
		Description string `json:"description"`
		TaxCode     string `json:"taxCode"`
		Quantity    string `json:"quantity"`
		UnitPrice   string `json:"unitPrice"`
		Discount    string `json:"discount"`
	}
	// ServicesInvoiceable se lanza cuando un pedido envía a facturar sus servicios (D-155): sólo lo
	// que toca ahora, con el id de la emisión.
	ServicesInvoiceable struct {
		fw.EventMeta
		Invoicing string               `json:"invoicing"`
		Company   string               `json:"company"`
		Customer  string               `json:"customer"`
		Number    string               `json:"number"`
		Date      string               `json:"date"`
		Lines     []ServiceInvoiceLine `json:"lines"`
	}
	// SalesOrderServingReopened se lanza cuando un pedido servido entero vuelve a tener algo
	// pendiente de servir (D-153).
	SalesOrderServingReopened struct {
		fw.EventMeta
		Company  string `json:"company"`
		Customer string `json:"customer"`
		Number   string `json:"number"`
	}
)

// Implementaciones de EventType.
func (OrderConfirmed) EventType() string { return "orders.order_confirmed" }
func (StockRequested) EventType() string { return "orders.stock_requested" }
func (OrderClosed) EventType() string    { return "orders.order_closed" }
func (DeliveryIssued) EventType() string { return "orders.delivery_issued" }
func (SalesOrderServingCompleted) EventType() string {
	return "orders.sales_order_serving_completed"
}
func (ServicesInvoiceable) EventType() string { return "orders.services_invoiceable" }
func (SalesOrderServingReopened) EventType() string {
	return "orders.sales_order_serving_reopened"
}

// Identidades nuevas e interpretación.
func NewOrderID() OrderID       { return OrderID{fw.NewUUID()} }
func NewDeliveryID() DeliveryID { return DeliveryID{fw.NewUUID()} }
func NewTermsID() TermsID       { return TermsID{fw.NewUUID()} }
func NewCounterID() CounterID   { return CounterID{fw.NewUUID()} }

// ParseOrderID interpreta una identidad en texto.
func ParseOrderID(s string) (OrderID, error) { u, err := fw.ParseUUID(s); return OrderID{u}, err }

// ParseDeliveryID interpreta una identidad en texto.
func ParseDeliveryID(s string) (DeliveryID, error) {
	u, err := fw.ParseUUID(s)
	return DeliveryID{u}, err
}

// Repositorios del contexto.
type (
	OrderRepository    = fw.Repository[OrderID, *Order]
	DeliveryRepository = fw.Repository[DeliveryID, *Delivery]
	TermsRepository    = fw.Repository[TermsID, *Terms]
	CounterRepository  = fw.Repository[CounterID, *Counter]
)

// Item es lo que Orders necesita de un producto para venderlo.
type Item struct {
	Company   OrganizationID
	SKU       string
	Name      string
	UoM       string
	TaxCode   string
	Stocked   bool
	Sellable  bool // a la venta, no bloqueado
	Retired   vocab.Date
	UnitPrice vocab.Decimal // del precio especial, la tarifa del cliente, la general o el precio base (D-148)
	Discount  vocab.Decimal
}

// Catalog pone precio a un producto para un pedido de un cliente (un puerto propio de Orders sobre
// los contratos Catalog y Pricing de Products): el precio especial del cliente, su tarifa, la general
// de la empresa o el precio base. Un producto desconocido se señala con ok false.
type Catalog interface {
	Price(ctx context.Context, company OrganizationID, customer PartyID, product ProductID, list PriceListID, q vocab.Decimal, on vocab.Date) (Item, bool,
		error)
}

// Credit es la situación de crédito de un cliente con la empresa (un puerto propio de Orders sobre
// el contrato Credit de Receivables, decisión aprobada 2 de docs/COBROS.md).
type Credit struct {
	Blocked   bool
	Limited   bool
	Available vocab.Decimal
}

// Blocks responde si una party no participa más que en consultas (el duplicado de otra, en
// Parties): no se le vende (docs/DECISIONES-COMUNES.md, D-70). nil: nadie está bloqueado.
type Blocks interface {
	Blocked(ctx context.Context, party PartyID) (bool, error)
}

// Cómo se sirve una línea de servicio de un pedido (D-155).
const (
	// ServiceFulfilmentDeliveryNote: con albarán, sin pedir ni mover stock (lo de siempre).
	ServiceFulfilmentDeliveryNote = "delivery_note"
	// ServiceFulfilmentDirectInvoice: no va al albarán; se factura directamente desde el pedido.
	ServiceFulfilmentDirectInvoice = "direct_invoice"
)

// ResolveServiceFulfilment decide cómo se sirve un servicio: el ajuste del producto, si lo tiene;
// si no, el de la empresa; si tampoco, con albarán (D-155).
func ResolveServiceFulfilment(product, company string) string {
	if product != "" {
		return product
	}
	if company != "" {
		return company
	}
	return ServiceFulfilmentDeliveryNote
}

// ServiceFulfilment dice los ajustes de D-155 (un puerto propio de Orders sobre los contratos de
// Products y de Parties). Services devuelve, de los productos dados, los que son servicios, con su
// ajuste propio ("" sin ajuste); CompanyMode, el ajuste de la empresa ("" sin ajuste). nil: todos
// los servicios van con albarán.
//
// CompanyInvoicingMoment es cuándo factura la empresa esos servicios: ServiceInvoicingOnServe,
// ServiceInvoicingOnConfirm o ServiceInvoicingManual ("" sin ajuste: al servir).
type ServiceFulfilment interface {
	Services(ctx context.Context, products []ProductID) (map[ProductID]string, error)
	CompanyMode(ctx context.Context, company OrganizationID) (string, error)
	CompanyInvoicingMoment(ctx context.Context, company OrganizationID) (string, error)
}

// CreditCheck responde el crédito de un cliente en una fecha; nil: sin control de crédito.
type CreditCheck interface {
	Credit(ctx context.Context, company OrganizationID, customer PartyID, on vocab.Date) (Credit, error)
}
