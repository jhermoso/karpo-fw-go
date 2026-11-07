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

// LineRounding es cómo se redondea el neto de una línea con descuento (D-128, D-144). Los dos modos
// redondean al céntimo alejándose de cero en el medio.
type LineRounding string

// Formas de redondear una línea.
const (
	// RoundOnce redondea una sola vez, al final: cantidad × precio − descuento.
	RoundOnce LineRounding = "once"
	// RoundGrossThenDiscount redondea primero el bruto (cantidad × precio) y después el neto, ya
	// con el descuento, como la ficha de factura de Trench.
	RoundGrossThenDiscount LineRounding = "gross_then_discount"
)

// Decimales del precio de una línea: como mucho 6 (la escala de bil_invoice_lines.unit_price); sin
// ajuste de la empresa, 4.
const (
	MaxPriceDecimals     = 6
	DefaultPriceDecimals = 4
)

// Calculation es cómo se calculan las líneas de una factura: la forma de redondear y los decimales
// del precio. Se toma de los ajustes de la empresa vendedora al preparar el borrador y se queda en
// la factura: no cambia aunque cambien los ajustes, ni antes ni después de emitirla (D-144).
type Calculation struct {
	Rounding      LineRounding
	PriceDecimals int
}

// DefaultCalculation es el cálculo de siempre: una sola vez al final y el precio con 4 decimales.
func DefaultCalculation() Calculation {
	return Calculation{Rounding: RoundOnce, PriceDecimals: DefaultPriceDecimals}
}

func (c Calculation) valid() bool {
	return (c.Rounding == RoundOnce || c.Rounding == RoundGrossThenDiscount) && c.PriceDecimals >= 0 && c.PriceDecimals <= MaxPriceDecimals
}

// Net es el importe neto de una línea: cantidad × precio menos el descuento (un porcentaje),
// redondeado como diga el cálculo. Ejemplo: 1 × 0.125 con un 50 % da 0.06 redondeando una vez
// (0.0625) y 0.07 redondeando el bruto antes (0.13, y 0.065).
func (c Calculation) Net(quantity, unitPrice, discount vocab.Decimal) vocab.Decimal {
	gross := quantity.Mul(unitPrice)
	if c.Rounding == RoundGrossThenDiscount {
		gross = gross.Round(2)
	}
	return gross.Sub(gross.Mul(discount).Div(hundred)).Round(2)
}

// Line es una línea de una factura. Su importe neto es cantidad × precio unitario menos el
// descuento, redondeado al céntimo alejándose de cero en el medio, una vez al final o el bruto
// antes, según el cálculo de la factura (el C# guardaba el producto sin redondear).
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
	Address Address // domicilio fiscal (plan Sage T1, art. 6 del reglamento de facturación); vacío si Parties no lo tiene
}

// Address es un domicilio tal como se congela en la factura.
type Address struct {
	Street     string
	PostalCode string
	Locality   string
	Region     string
	Country    string
}

// Complete dice si el domicilio basta para una factura completa: al menos la calle y el país
// (D-147). La localidad todavía no se exige.
func (a Address) Complete() bool {
	return strings.TrimSpace(a.Street) != "" && strings.TrimSpace(a.Country) != ""
}

// RequireAddresses comprueba que las dos partes de una factura ordinaria tienen domicilio fiscal
// (art. 6 del RD 1619/2012, D-147). A la rectificativa no se le exige: lleva el de la factura que
// rectifica o, si aquélla no lo tenía, el que haya hoy, y una factura antigua se tiene que poder
// corregir aunque no haya ninguno.
func RequireAddresses(kind Kind, seller, customer Identity) error {
	if kind != Ordinary {
		return nil
	}
	if !seller.Address.Complete() {
		return fw.Violation("billing.seller_address_required", "the seller has no fiscal address")
	}
	if !customer.Address.Complete() {
		return fw.Violation("billing.customer_address_required", "the customer has no fiscal address")
	}
	return nil
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

// Source es un documento del que se preparó una factura (un albarán de Orders). Una factura hecha
// a mano no tiene ninguno; una de varios albaranes, uno por albarán (D-146).
type Source struct {
	Type string
	ID   string
	Ref  string // su número, para mostrarlo
}

// MaxSources limita los orígenes de una factura: como mucho uno por línea.
const MaxSources = MaxLines

func checkSources(v *fw.Validation, ss []Source) {
	seen := map[[2]string]bool{}
	ok := len(ss) <= MaxSources
	for _, s := range ss {
		k := [2]string{s.Type, s.ID}
		ok = ok && s.Type != "" && s.ID != "" && len(s.Type) <= 80 && len(s.ID) <= 80 && len(s.Ref) <= 40 && !seen[k]
		seen[k] = true
	}
	v.Require(ok, "sources", "length", "each source once, with a type and an id within limits")
}

// refs devuelve los números de los orígenes, separados por comas.
func refs(ss []Source) string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.Ref)
	}
	return strings.Join(out, ", ")
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
	Sources              []Source
	// Calculation es cómo se calculan las líneas; vacío (sin Rounding), DefaultCalculation.
	Calculation Calculation
	Audit       traits.AuditStamp
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
	checkSources(&v, s.Sources)
	if s.Calculation.Rounding == "" {
		s.Calculation = DefaultCalculation()
	}
	v.Require(s.Calculation.valid(), "calculation", "enum", "a line rounding once or gross_then_discount and 0 to 6 price decimals")
	d := Details{Description: s.Description, OperationDate: s.OperationDate, DueDate: s.DueDate, EquivalenceSurcharge: s.EquivalenceSurcharge}
	checkDetails(&v, &d)
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Description = d.Description
	s.Lines, s.Sources = slices.Clone(s.Lines), slices.Clone(s.Sources)
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
	s.Sources = slices.Clone(s.Sources)
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
	// Las escalas de la cantidad y del descuento son las de sus columnas (bil_invoice_lines: (18,4) y
	// (7,4)); la del precio, los decimales del cálculo de la factura (como mucho los 6 de su columna).
	// Un valor con más decimales es un error del campo, no de la base de datos.
	calc := i.s.Calculation
	v.Require(!in.Quantity.IsZero() && (i.s.Kind == Corrective || in.Quantity.IsPositive()) && places(in.Quantity, 4), "quantity", "range",
		"a positive quantity of up to 4 decimals (negative only in corrective invoices)")
	v.Require(!in.UnitPrice.IsNegative() && places(in.UnitPrice, int32(calc.PriceDecimals)), "unitPrice", "range",
		fmt.Sprintf("a non-negative unit price of up to %d decimals", calc.PriceDecimals))
	v.Require(!in.Discount.IsNegative() && in.Discount.LessThanOrEqual(hundred) && places(in.Discount, 4), "discount", "range",
		"a discount from 0 to 100 with up to 4 decimals")
	code, treat := strings.ToUpper(strings.TrimSpace(in.TaxCode)), strings.ToUpper(strings.TrimSpace(in.Treatment))
	v.Require(code != "" || treat != "", "taxCode", "required", "a tax code or an exempt or not subject treatment")
	v.Require(utf8.RuneCountInString(code) <= 5 && utf8.RuneCountInString(treat) <= 5, "taxCode", "length", "codes of at most 5 characters")
	if err := v.Err(); err != nil {
		return LineID{}, err
	}
	net := calc.Net(in.Quantity, in.UnitPrice, in.Discount)
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
// impuestos. Invariantes: una descripción de la operación (la exige el SII, plan Sage T1); al menos una línea; una factura ordinaria tiene un neto positivo y una
// rectificativa un neto distinto de cero; el desglose cubre exactamente el neto de las líneas; el
// vendedor tiene un NIF español válido y el cliente un número fiscal; en una factura ordinaria, las
// dos partes tienen domicilio fiscal (D-147); el vencimiento no precede a la emisión.
func (i *Invoice) Issue(x Issuance) error {
	if err := i.mustBeDraft(); err != nil {
		return err
	}
	net := i.Net()
	switch {
	case i.s.Description == "":
		return fw.Violation("billing.description_required", "an invoice describes its operation before it is issued")
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
	if err := RequireAddresses(i.s.Kind, x.Seller, x.Customer); err != nil {
		return err
	}
	i.s.Status, i.s.Series, i.s.Number, i.s.IssueDate = Issued, x.Series, x.Number, x.Date
	i.s.SellerIdentity, i.s.CustomerIdentity, i.s.Taxes = x.Seller, x.Customer, x.Taxes
	i.s.Taxes.Lines = slices.Clone(x.Taxes.Lines)
	i.Raise(InvoiceIssued{EventMeta: i.NewEventMeta(), Invoice: i.State()})
	return nil
}

// Discard comprueba que un borrador puede borrarse (una factura emitida, nunca). Un borrador hecho a
// partir de un albarán tampoco se descarta: lo entregado se factura, y un error se corrige después
// de emitirlo. Sólo desaparece al unirse a otro (Merge).
func (i *Invoice) Discard() error {
	if err := i.mustBeDraft(); err != nil {
		return err
	}
	if len(i.s.Sources) > 0 {
		return fw.Violation("billing.sourced_draft", "the draft comes from "+refs(i.s.Sources)+": it is issued, not discarded")
	}
	return nil
}

// Merge une a este borrador otros borradores de documentos (albaranes) del mismo vendedor y cliente
// (D-146): se queda con las líneas y los orígenes de todos, y la descripción que se le da. La fecha
// de la operación es la última de todas, el final del periodo que cubre. Los demás desaparecen:
// la aplicación los borra en la misma unidad de trabajo. Todos tienen que ser borradores
// ordinarios con algún origen y calcularse igual (D-144: el mismo redondeo y los mismos
// decimales), con o sin recargo de equivalencia todos a la vez.
func (i *Invoice) Merge(others []*Invoice, description string) error {
	if len(others) == 0 {
		return fw.Violation("billing.merge_too_few", "at least two drafts are merged")
	}
	all := append([]*Invoice{i}, others...)
	lines, sources, date := 0, []Source{}, vocab.Date{}
	for _, o := range all {
		if err := o.mustBeDraft(); err != nil {
			return err
		}
		os := o.s
		switch {
		case os.Kind != Ordinary || len(os.Sources) == 0:
			return fw.Violation("billing.merge_unsourced", "only drafts prepared from delivery notes are merged")
		case os.Seller != i.s.Seller || os.Customer != i.s.Customer:
			return fw.Violation("billing.merge_other_customer", "the drafts are of the same seller and customer")
		case os.Calculation != i.s.Calculation:
			return fw.Violation("billing.merge_calculation", "the drafts are calculated alike: the same line rounding and price decimals")
		case os.EquivalenceSurcharge != i.s.EquivalenceSurcharge:
			return fw.Violation("billing.merge_surcharge", "the drafts all have the equivalence surcharge, or none has it")
		}
		lines += len(os.Lines)
		sources = append(sources, os.Sources...)
		if os.OperationDate.After(date) {
			date = os.OperationDate
		}
	}
	if lines > MaxLines {
		return fw.Violation("billing.too_many_lines", "the invoice has too many lines")
	}
	var v fw.Validation
	checkSources(&v, sources)
	d := Details{Description: description, OperationDate: date, DueDate: i.s.DueDate, EquivalenceSurcharge: i.s.EquivalenceSurcharge}
	checkDetails(&v, &d)
	if err := v.Err(); err != nil {
		return err
	}
	merged := slices.Clone(i.s.Lines)
	for _, o := range others {
		merged = append(merged, o.s.Lines...)
	}
	i.s.Lines, i.s.Sources, i.s.Description, i.s.OperationDate = merged, sources, d.Description, d.OperationDate
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
	InvFieldSources  = spec.Collection("sources", func(i *Invoice) []Source { return i.s.Sources })
	SrcFieldType     = spec.Comparable("source_type", func(s Source) string { return s.Type })
	SrcFieldID       = spec.Comparable("source_id", func(s Source) string { return s.ID })
)

// FromSource selecciona las facturas, borradores o emitidas, que tienen ese documento entre sus orígenes.
func FromSource(typ, id string) spec.Spec[*Invoice] {
	return InvFieldSources.Any(spec.And(SrcFieldType.Eq(typ), SrcFieldID.Eq(id)))
}

// Repositorios y puertos del contexto.
type (
	SeriesRepository          = fw.Repository[SeriesID, *Series]
	InvoiceRepository         = fw.Repository[InvoiceID, *Invoice]
	PendingDeliveryRepository = fw.Repository[PendingDeliveryID, *PendingDelivery]
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
