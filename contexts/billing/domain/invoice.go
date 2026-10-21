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

// InvoiceKind es el nombre estable del tipo de agregado.
const InvoiceKind = "billing.invoice"

// MaxLines limita las líneas de una factura.
const MaxLines = 500

var hundred = vocab.DecimalFromInt(100)

// places dice si d tiene como mucho n decimales.
func places(d vocab.Decimal, n int32) bool { return d.Equal(d.Round(n)) }

// Kind de factura: ordinaria o rectificativa. El C# modelaba la exención como un tipo de factura
// ("Exenta") y los abonos como una cabecera aparte sin líneas.
type Kind int

// Tipos de factura.
const (
	Ordinary Kind = iota + 1
	Corrective
)

var kinds = map[Kind]string{Ordinary: "ordinary", Corrective: "corrective"}

// String devuelve el nombre estable.
func (k Kind) String() string { return kinds[k] }

// Status de una factura: un borrador cambia y puede descartarse; una factura emitida es inmutable y
// se corrige con una factura rectificativa (el C# dejaba cambiar y borrar las facturas emitidas).
type Status int

// Estados.
const (
	Draft Status = iota + 1
	Issued
)

var statuses = map[Status]string{Draft: "draft", Issued: "issued"}

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

// CorrectionReasons son las causas de una factura rectificativa (R1: error fundado en derecho y art.
// 80 Uno, Dos y Seis LIVA; R2: concurso, art. 80 Tres; R3: créditos incobrables, art. 80 Cuatro; R4:
// resto de causas; R5: facturas simplificadas).
var CorrectionReasons = []string{"R1", "R2", "R3", "R4", "R5"}

// Line es una línea de una factura. Su importe neto es cantidad × precio unitario menos el
// descuento, redondeado al céntimo alejándose de cero en el medio (el C# guardaba el producto sin
// redondear).
type Line struct {
	ID          LineID
	Description string
	Quantity    vocab.Decimal
	UnitPrice   vocab.Decimal
	Discount    vocab.Decimal // porcentaje
	TaxCode     string
	Treatment   string
	Net         vocab.Decimal
}

// LineInput es una línea nueva.
type LineInput struct {
	Description string
	Quantity    vocab.Decimal
	UnitPrice   vocab.Decimal
	Discount    vocab.Decimal
	TaxCode     string
	Treatment   string
}

// Identity es la identificación fiscal de una party en una factura, congelada al emitirla.
type Identity struct {
	NIF     string
	Name    string
	Country string
}

// TaxLine es una línea del desglose de impuestos congelado al emitir.
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

// Breakdown es el desglose de impuestos de una factura.
type Breakdown struct {
	Country   string
	Lines     []TaxLine
	Net       vocab.Decimal
	Tax       vocab.Decimal
	Surcharge vocab.Decimal
}

// Total devuelve base + impuestos + recargo.
func (b Breakdown) Total() vocab.Decimal { return b.Net.Add(b.Tax).Add(b.Surcharge) }

// Source es el documento del que se preparó una factura (un albarán de Orders); vacío cuando se
// preparó a mano.
type Source struct {
	Type string
	ID   string
	Ref  string // su número, para mostrarlo
}

// Invoice es una factura de venta de un vendedor a un cliente, con sus líneas.
type Invoice struct {
	fw.BaseAggregateRoot[InvoiceID]
	traits.Audited
	s InvoiceState
}

// InvoiceState es el estado persistido de una factura.
type InvoiceState struct {
	Seller               OrganizationID
	Customer             PartyID
	Kind                 Kind
	Status               Status
	Currency             vocab.CurrencyCode
	Description          string
	OperationDate        vocab.Date // cero: la fecha de emisión
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
	Source               Source
	Audit                traits.AuditStamp
}

// Details son los campos editables de la cabecera de un borrador.
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

// ReconstituteInvoice reconstruye una factura.
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
	v.Require((s.Source.Type == "") == (s.Source.ID == "") && len(s.Source.Type) <= 80 && len(s.Source.ID) <= 80 && len(s.Source.Ref) <= 40, "source",
		"length", "a source type and id within limits")
	d := Details{Description: s.Description, OperationDate: s.OperationDate, DueDate: s.DueDate, EquivalenceSurcharge: s.EquivalenceSurcharge}
	checkDetails(&v, &d)
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Description = d.Description
	s.Lines = slices.Clone(s.Lines)
	return &Invoice{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// DraftInvoice crea un borrador. Un borrador rectificativo copia el cliente de la original, de la
// que la aplicación comprueba que está emitida y es del mismo vendedor.
func DraftInvoice(id InvoiceID, s InvoiceState) (*Invoice, error) {
	s.Status, s.Lines, s.Series, s.Number, s.IssueDate = Draft, nil, SeriesID{}, "", vocab.Date{}
	s.SellerIdentity, s.CustomerIdentity, s.Taxes = Identity{}, Identity{}, Breakdown{}
	if s.Currency.IsZero() {
		s.Currency = vocab.MustCurrencyCode("EUR")
	}
	return ReconstituteInvoice(id, s)
}

// State devuelve el estado (las líneas y los impuestos son copias).
func (i *Invoice) State() InvoiceState {
	s := i.s
	s.Lines = slices.Clone(s.Lines)
	s.Taxes.Lines = slices.Clone(s.Taxes.Lines)
	return s
}

// Net devuelve la suma de los netos de las líneas.
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

// AddLine añade una línea (sólo borradores). Una factura ordinaria tiene cantidades positivas y
// precios no negativos; una rectificativa (por diferencias) puede tener cantidades negativas.
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
	// Las escalas son las de las columnas (bil_invoice_lines: cantidad y precio (18,4), descuento
	// (7,4)): un valor con más decimales es un error del campo, no de la base de datos.
	v.Require(!in.Quantity.IsZero() && (i.s.Kind == Corrective || in.Quantity.IsPositive()) && places(in.Quantity, 4), "quantity", "range",
		"a positive quantity of up to 4 decimals (negative only in corrective invoices)")
	v.Require(!in.UnitPrice.IsNegative() && places(in.UnitPrice, 4), "unitPrice", "range", "a non-negative unit price of up to 4 decimals")
	v.Require(!in.Discount.IsNegative() && in.Discount.LessThanOrEqual(hundred) && places(in.Discount, 4), "discount", "range",
		"a discount from 0 to 100 with up to 4 decimals")
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

// RemoveLine quita una línea (sólo borradores).
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

// SetDetails sustituye los campos editables de la cabecera (sólo borradores).
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

// AccrualDate devuelve la fecha de devengo de los impuestos: la fecha de la operación, o la de emisión.
func (i *Invoice) AccrualDate(issue vocab.Date) vocab.Date {
	if !i.s.OperationDate.IsZero() {
		return i.s.OperationDate
	}
	return issue
}

// Issuance es lo que la aplicación reúne para emitir una factura.
type Issuance struct {
	Series   SeriesID
	Number   string
	Date     vocab.Date
	Seller   Identity
	Customer Identity
	Taxes    Breakdown
}

// Issue congela la factura con su número, las identidades de las partes y el desglose de
// impuestos. Invariantes: al menos una línea; una factura ordinaria tiene un neto positivo y una
// rectificativa un neto distinto de cero; el desglose cubre exactamente el neto de las líneas; el
// vendedor tiene un NIF español válido y el cliente un número fiscal; el vencimiento no precede a la emisión.
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

// Discard comprueba que un borrador puede borrarse (una factura emitida, nunca). Un borrador hecho a
// partir de un albarán tampoco se descarta: lo entregado se factura, y un error se corrige después
// de emitirlo.
func (i *Invoice) Discard() error {
	if err := i.mustBeDraft(); err != nil {
		return err
	}
	if i.s.Source.Type != "" {
		return fw.Violation("billing.sourced_draft", "the draft comes from "+i.s.Source.Ref+": it is issued, not discarded")
	}
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (i *Invoice) AuditSnapshot() map[string]any {
	return map[string]any{"status": i.s.Status.String(), "number": i.s.Number, "lines": len(i.s.Lines), "net": i.Net().String()}
}

// Campos de la factura.
var (
	InvFieldID       = spec.Comparable("id", func(i *Invoice) InvoiceID { return i.ID() })
	InvFieldSeller   = spec.Comparable("seller", func(i *Invoice) OrganizationID { return i.s.Seller })
	InvFieldCustomer = spec.Comparable("customer", func(i *Invoice) PartyID { return i.s.Customer })
	InvFieldStatus   = spec.Comparable("status", func(i *Invoice) int { return int(i.s.Status) })
	InvFieldCorrects = spec.Comparable("corrects", func(i *Invoice) InvoiceID { return i.s.Corrects })
	InvFieldIssued   = spec.OrderedBy("issue_date", func(i *Invoice) vocab.Date { return i.s.IssueDate }, vocab.CompareDates)
	InvFieldNumber   = spec.Ordered("invoice_number", func(i *Invoice) string { return i.s.Number })
	InvFieldSrcType  = spec.Comparable("source_type", func(i *Invoice) string { return i.s.Source.Type })
	InvFieldSrcID    = spec.Comparable("source_id", func(i *Invoice) string { return i.s.Source.ID })
)

// Repositorios y puertos del contexto.
type (
	SeriesRepository  = fw.Repository[SeriesID, *Series]
	InvoiceRepository = fw.Repository[InvoiceID, *Invoice]
)

// TaxableLine es una línea que se envía al motor de impuestos.
type TaxableLine struct {
	Ref       string
	Base      vocab.Decimal
	TaxCode   string
	Treatment string
}

// Taxes calcula el desglose de impuestos de una factura (un puerto propio de Billing; un adaptador
// lo implementa sobre el TaxEngine de Fiscal, que elige la jurisdicción del vendedor).
type Taxes interface {
	Calculate(ctx context.Context, seller OrganizationID, accrual vocab.Date, surcharge bool, lines []TaxableLine) (Breakdown, error)
}

// Identities resuelve las identidades fiscales de las parties (un puerto propio de Billing; un
// adaptador lo implementa sobre el contrato TaxIdentities de Parties).
type Identities interface {
	Identities(ctx context.Context, parties []PartyID) (map[PartyID]Identity, error)
}

// Blocks responde si una party no participa en nada salvo en consultas (el duplicado de otra, en
// Parties): no se le prepara ni se le emite una factura ordinaria (docs/DECISIONES-COMUNES.md,
// D-70). Una rectificativa sí: corregir lo facturado es una obligación.
type Blocks interface {
	Blocked(ctx context.Context, party PartyID) (bool, error)
}

// TaxableLines devuelve las líneas de la factura tal como las necesita el motor de impuestos.
func (i *Invoice) TaxableLines() []TaxableLine {
	out := make([]TaxableLine, 0, len(i.s.Lines))
	for _, l := range i.s.Lines {
		out = append(out, TaxableLine{Ref: l.ID.String(), Base: l.Net, TaxCode: l.TaxCode, Treatment: l.Treatment})
	}
	return out
}
