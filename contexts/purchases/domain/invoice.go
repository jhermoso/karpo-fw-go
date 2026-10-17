// Package domain es el modelo de Purchases de la fase 1: las facturas que una empresa recibe de sus
// proveedores (registradas con su desglose de impuestos y su retención profesional, numeradas en el
// registro de facturas recibidas) y el perfil de compra de cada proveedor. El C# tenía una sola
// fila de siembra «Purchase Invoice»: sin número del proveedor, sin IVA soportado, sin retención, y
// toda factura perdía sus partes facturadas al guardarse.
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

// InvoiceKind es el nombre estable del tipo de agregado.
const InvoiceKind = "purchases.received_invoice"

// MaxLines limita las líneas de una factura.
const MaxLines = 500

// Category es la naturaleza de lo comprado: Accounting contabiliza cada una en su cuenta de gasto
// (los subgrupos 60 y 62 del PGC).
type Category string

// Categorías.
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
	FixedAsset           Category = "fixed-asset"           // 21x: una inversión, no un gasto
)

// Categories lista las categorías válidas.
var Categories = []Category{Goods, Rent, Repairs, ProfessionalServices, Transport, Insurance, Advertising, Supplies, OtherServices, FixedAsset}

// ValidCategory dice si c es una categoría.
func ValidCategory(c Category) bool { return slices.Contains(Categories, c) }

// ProfessionalKey es la clave de percepción del Modelo 190 de las actividades profesionales, la única
// retención de la fase 1 (los alquileres van al Modelo 115, fase 2).
const ProfessionalKey = "G"

// Line es una línea de una factura recibida.
type Line struct {
	No          int
	Description string
	Category    Category
	Base        vocab.Decimal // negativa en una factura rectificativa
	TaxCode     string
	Treatment   string // exenta o no sujeta, en lugar de un código fiscal
}

// TaxableLine es una línea tal como la necesita el motor de impuestos.
type TaxableLine struct {
	Ref       string
	Base      vocab.Decimal
	TaxCode   string
	Treatment string
}

// TaxLine es una línea del desglose de impuestos.
type TaxLine struct {
	TaxType       string
	TaxCode       string
	Treatment     string
	TreatmentKind string
	Rate          vocab.Decimal
	Base          vocab.Decimal
	Amount        vocab.Decimal
}

// Breakdown es el desglose de impuestos de una factura.
type Breakdown struct {
	Country string
	Lines   []TaxLine
	Net     vocab.Decimal
	Tax     vocab.Decimal
}

// Taxes calcula el desglose (un puerto propio de Purchases sobre el TaxEngine de Fiscal, con la
// jurisdicción de la empresa compradora).
type Taxes interface {
	Calculate(ctx context.Context, company OrganizationID, accrual vocab.Date, lines []TaxableLine) (Breakdown, error)
}

// PayTo es una cuenta a la que se paga la factura.
type PayTo struct {
	IBAN   vocab.IBAN
	Amount vocab.Decimal
}

// Draft es lo que se registra: la factura tal como la emitió el proveedor.
type Draft struct {
	Company         OrganizationID
	Supplier        PartyID
	SupplierNumber  string
	Issued          vocab.Date // fecha de la factura del proveedor (devengo)
	Received        vocab.Date // fecha en que se registra
	Due             vocab.Date
	Lines           []Line
	NonDeductible   bool          // el impuesto soportado es un coste (no deducible)
	WithholdingRate vocab.Decimal // porcentaje sobre el neto
	DeclaredTotal   vocab.Decimal // el total impreso en la factura, que se comprueba con el desglose
	PayTo           []PayTo
	Corrects        InvoiceID // la factura que rectifica una rectificativa
}

// InvoiceState es el estado persistido de una factura recibida.
type InvoiceState struct {
	Draft
	Register     string // número en el registro de facturas recibidas
	Year         int
	Breakdown    Breakdown
	Withholding  vocab.Decimal
	Cancelled    bool
	CancelReason string
	Audit        traits.AuditStamp
}

// Invoice es una factura recibida de un proveedor, anotada en el registro.
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

// ReconstituteInvoice reconstruye una factura.
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

// RegisterNumber compone el número del registro de facturas recibidas.
func RegisterNumber(year int, n int64) string { return fmt.Sprintf("FR-%d-%06d", year, n) }

// Register registra una factura recibida con su desglose y su número de registro. Invariantes
// (ninguna existía en el C#): el desglose cubre las líneas, el total calculado es igual al total
// impreso por el proveedor, la retención es el tipo sobre el neto, las cuentas suman lo que se paga
// al proveedor (total menos retención) y una factura rectificativa es negativa.
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

// State devuelve el estado (los slices son copias).
func (i *Invoice) State() InvoiceState {
	s := i.s
	s.Lines, s.PayTo, s.Breakdown.Lines = slices.Clone(s.Lines), slices.Clone(s.PayTo), slices.Clone(s.Breakdown.Lines)
	return s
}

// Total devuelve el neto más los impuestos.
func (i *Invoice) Total() vocab.Decimal { return i.s.Breakdown.Net.Add(i.s.Breakdown.Tax) }

// Payable devuelve lo que se paga al proveedor: el total menos la retención.
func (i *Invoice) Payable() vocab.Decimal { return i.Total().Sub(i.s.Withholding) }

// Expense es el coste de una categoría: sus bases más su parte del impuesto cuando no es deducible.
type Expense struct {
	Category Category
	Amount   vocab.Decimal
}

// Expenses devuelve el coste por categoría (en el orden en que aparecen por primera vez) y el
// impuesto deducible. Un impuesto no deducible se reparte entre las categorías en proporción a sus
// bases, y la última se queda con el redondeo.
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

// Cancel anula una factura registrada por error (una corrección de verdad es una factura
// rectificativa del proveedor).
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

// AuditSnapshot implementa traits.Snapshotter.
func (i *Invoice) AuditSnapshot() map[string]any {
	return map[string]any{"register": i.s.Register, "supplierNumber": i.s.SupplierNumber, "total": i.Total().String(), "cancelled": i.s.Cancelled}
}

// Campos de la factura.
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

// Counter numera el registro de facturas recibidas de una empresa y año.
type Counter struct {
	fw.BaseAggregateRoot[CounterID]
	company OrganizationID
	year    int
	last    int64
}

// ReconstituteCounter reconstruye un contador.
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

// Next toma el siguiente número.
func (c *Counter) Next() int64 { c.last++; return c.last }

// Values devuelve los valores persistidos.
func (c *Counter) Values() (OrganizationID, int, int64) { return c.company, c.year, c.last }

// Campos del contador.
var (
	CntFieldCompany = spec.Comparable("company", func(c *Counter) OrganizationID { return c.company })
	CntFieldYear    = spec.Comparable("fiscal_year", func(c *Counter) int { return c.year })
)

// CounterKind es el nombre estable del tipo de agregado.
const CounterKind = "purchases.register_counter"
