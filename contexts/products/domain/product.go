// Package domain es el modelo de Products: el catálogo de lo que una empresa vende, compra y
// almacena (bienes y servicios con su unidad, código fiscal, códigos de barras, componentes de kit y
// proveedores), sus categorías y sus listas de precios. El C# tenía un catálogo UDM amplio en el que
// todo Validate() devolvía éxito, y precio, coste, códigos de barras, proveedor y umbrales de stock
// estaban modelados dos o tres veces cada uno sin relación.
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

// ProductKind es el nombre estable del tipo de agregado.
const ProductKind = "products.product"

// Kind distingue bienes de servicios (el C# lo guardaba a la vez como cadena libre y como subtipo TPT).
type Kind int

// Tipos.
const (
	Good Kind = iota + 1
	Service
)

var kinds = map[Kind]string{Good: "good", Service: "service"}

// String devuelve el nombre estable.
func (k Kind) String() string { return kinds[k] }

// ParseKind analiza el nombre de un tipo.
func ParseKind(s string) (Kind, bool) {
	for k, n := range kinds {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// Tracking es cómo se identifica el stock de un bien (el TrackingMode del C# era una cadena libre).
type Tracking int

// Modos de trazabilidad.
const (
	TrackNone Tracking = iota + 1
	TrackLot
	TrackSerial
)

var trackings = map[Tracking]string{TrackNone: "none", TrackLot: "lot", TrackSerial: "serial"}

// String devuelve el nombre estable.
func (t Tracking) String() string { return trackings[t] }

// ParseTracking analiza el nombre de un modo de trazabilidad.
func ParseTracking(s string) (Tracking, bool) {
	for k, n := range trackings {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// Barcode es un código de identificación de un producto.
type Barcode struct {
	Type  string // ean-13 | ean-8 | upc-a | internal
	Value string
}

// barcodeDigits son las longitudes de los tipos GTIN, cuyo último dígito es de control.
var barcodeDigits = map[string]int{"ean-13": 13, "ean-8": 8, "upc-a": 12}

// ValidGTIN dice si digits es un GTIN con el dígito de control correcto (pesos 3 y 1 desde la
// derecha, módulo 10).
func ValidGTIN(digits string) bool {
	if len(digits) < 8 {
		return false
	}
	sum := 0
	for i := 0; i < len(digits); i++ {
		c := digits[len(digits)-1-i]
		if c < '0' || c > '9' {
			return false
		}
		if i%2 == 1 {
			sum += int(c-'0') * 3
		} else {
			sum += int(c - '0')
		}
	}
	return sum%10 == 0
}

func validBarcode(b Barcode) bool {
	if n, ok := barcodeDigits[b.Type]; ok {
		return len(b.Value) == n && ValidGTIN(b.Value)
	}
	return b.Type == "internal" && b.Value != "" && len(b.Value) <= 30 && !strings.ContainsAny(b.Value, " \t\n")
}

// Component es un producto del que está hecho un kit.
type Component struct {
	Product  ProductID
	Quantity vocab.Decimal
}

// SupplierItem es cómo nombra y entrega el producto un proveedor.
type SupplierItem struct {
	Supplier  PartyID
	Code      string // la referencia del proveedor
	LeadDays  int
	Preferred bool
}

// Details son los datos editables de un producto.
type Details struct {
	Name            string
	Description     string
	Kind            Kind
	UoM             string
	Category        CategoryID
	TaxCode         string        // código fiscal del catálogo de Fiscal (G21, R10…)
	ExpenseCategory string        // categoría de gasto de Purchases cuando se compra (mercaderías, suministros…)
	BasePrice       vocab.Decimal // precio de venta sin lista de precios, hasta 4 decimales
	StandardCost    vocab.Decimal
	ForSale         bool
	ForPurchase     bool
	Stocked         bool
	Tracking        Tracking
	BlockedSales    bool
	BlockedPurchase bool
	Barcodes        []Barcode
	Components      []Component
	Suppliers       []SupplierItem
}

// ProductState es el estado persistido de un producto.
type ProductState struct {
	Company OrganizationID
	SKU     string
	Details
	Discontinued vocab.Date
	Audit        traits.AuditStamp
}

// Product es algo que una empresa vende, compra o almacena.
type Product struct {
	fw.BaseAggregateRoot[ProductID]
	traits.Audited
	s ProductState
}

// MaxComponents limita los componentes de un kit.
const MaxComponents = 200

// NormalizeSKU devuelve la forma canónica de un SKU: sin espacios en los extremos y en mayúsculas.
func NormalizeSKU(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

func validSKU(s string) bool {
	if s == "" || len(s) > 30 {
		return false
	}
	for _, r := range s {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '.' || r == '_') {
			return false
		}
	}
	return true
}

func price(d vocab.Decimal) bool { return !d.IsNegative() && d.Equal(d.Round(4)) }

func checkDetails(v *fw.Validation, self ProductID, d *Details) {
	d.Name, d.Description = strings.TrimSpace(d.Name), strings.TrimSpace(d.Description)
	v.Require(d.Name != "" && utf8.RuneCountInString(d.Name) <= 120, "name", "length", "a name of 1 to 120 characters")
	v.Require(utf8.RuneCountInString(d.Description) <= 500, "description", "length", "at most 500 characters")
	_, ok := kinds[d.Kind]
	v.Require(ok, "kind", "enum", "good or service")
	_, ok = UnitOf(d.UoM)
	v.Require(ok, "uom", "enum", "an unknown unit of measure")
	d.TaxCode = strings.ToUpper(strings.TrimSpace(d.TaxCode))
	v.Require(len(d.TaxCode) <= 10, "taxCode", "length", "at most 10 characters")
	v.Require(len(d.ExpenseCategory) <= 30, "expenseCategory", "length", "at most 30 characters")
	v.Require(price(d.BasePrice) && price(d.StandardCost), "basePrice", "range", "non-negative amounts of up to 4 decimals")
	if d.Tracking == 0 {
		d.Tracking = TrackNone
	}
	_, ok = trackings[d.Tracking]
	v.Require(ok, "tracking", "enum", "none, lot or serial")
	if d.Kind == Service {
		v.Require(!d.Stocked && d.Tracking == TrackNone && len(d.Components) == 0, "kind", "service", "a service is not stocked, tracked nor a kit")
	}
	v.Require(d.Stocked || d.Tracking == TrackNone, "tracking", "stocked", "only a stocked good is tracked by lot or serial")
	for i, b := range d.Barcodes {
		v.Require(validBarcode(b), fmt.Sprintf("barcodes[%d]", i), "format", "an ean-13, ean-8 or upc-a with its check digit, or an internal code")
		v.Require(!slices.ContainsFunc(d.Barcodes[:i], func(x Barcode) bool { return x.Value == b.Value }), fmt.Sprintf("barcodes[%d]", i), "duplicate",
			"each code once")
	}
	v.Require(len(d.Barcodes) <= 20, "barcodes", "count", "at most 20 codes")
	v.Require(len(d.Components) <= MaxComponents, "components", "count", "too many components")
	for i, c := range d.Components {
		f := fmt.Sprintf("components[%d]", i)
		v.Require(!c.Product.IsZero() && c.Product != self, f, "self", "a component is another product")
		v.Require(c.Quantity.IsPositive() && c.Quantity.Equal(c.Quantity.Round(4)), f, "range", "a positive quantity of up to 4 decimals")
		v.Require(!slices.ContainsFunc(d.Components[:i], func(x Component) bool { return x.Product == c.Product }), f, "duplicate", "each component once")
	}
	preferred := 0
	for i, s := range d.Suppliers {
		f := fmt.Sprintf("suppliers[%d]", i)
		v.Require(!s.Supplier.IsZero() && len(s.Code) <= 50 && s.LeadDays >= 0 && s.LeadDays <= 730, f, "range", "a supplier, a code of up to 50 and 0 to 730 days")
		v.Require(!slices.ContainsFunc(d.Suppliers[:i], func(x SupplierItem) bool { return x.Supplier == s.Supplier }), f, "duplicate", "each supplier once")
		if s.Preferred {
			preferred++
		}
	}
	v.Require(preferred <= 1 && len(d.Suppliers) <= 50, "suppliers", "preferred", "at most one preferred supplier and 50 suppliers")
}

// ReconstituteProduct reconstruye un producto.
func ReconstituteProduct(id ProductID, s ProductState) (*Product, error) {
	base, err := fw.NewBaseAggregateRoot(ProductKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero(), "company", "required", "the company is required")
	s.SKU = NormalizeSKU(s.SKU)
	v.Require(validSKU(s.SKU), "sku", "format", "1 to 30 letters, digits, dots, dashes or underscores")
	checkDetails(&v, id, &s.Details)
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Barcodes, s.Components, s.Suppliers = slices.Clone(s.Barcodes), slices.Clone(s.Components), slices.Clone(s.Suppliers)
	return &Product{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// RegisterProduct añade un producto al catálogo de una empresa.
func RegisterProduct(id ProductID, company OrganizationID, sku string, d Details) (*Product, error) {
	p, err := ReconstituteProduct(id, ProductState{Company: company, SKU: sku, Details: d})
	if err != nil {
		return nil, err
	}
	p.Raise(ProductRegistered{EventMeta: p.NewEventMeta(), Company: company.String(), SKU: p.s.SKU, Name: p.s.Name, Kind: p.s.Kind.String()})
	return p, nil
}

// State devuelve el estado (los slices son copias).
func (p *Product) State() ProductState {
	s := p.s
	s.Barcodes, s.Components, s.Suppliers = slices.Clone(s.Barcodes), slices.Clone(s.Components), slices.Clone(s.Suppliers)
	return s
}

// Active dice si el producto no está retirado en una fecha.
func (p *Product) Active(on vocab.Date) bool {
	return p.s.Discontinued.IsZero() || on.Before(p.s.Discontinued)
}

// Change sustituye los datos. Un producto retirado no cambia; el tipo de un producto tampoco (un
// bien en stock no puede pasar a ser un servicio).
func (p *Product) Change(d Details) error {
	if !p.s.Discontinued.IsZero() {
		return fw.Violation("products.discontinued", "a discontinued product does not change")
	}
	if d.Kind != p.s.Kind {
		return fw.Violation("products.kind_fixed", "the kind of a product does not change")
	}
	var v fw.Validation
	checkDetails(&v, p.ID(), &d)
	if err := v.Err(); err != nil {
		return err
	}
	d.Barcodes, d.Components, d.Suppliers = slices.Clone(d.Barcodes), slices.Clone(d.Components), slices.Clone(d.Suppliers)
	p.s.Details = d
	return nil
}

// Discontinue retira el producto desde una fecha (nunca se borra: los documentos siguen apuntando a él).
func (p *Product) Discontinue(on vocab.Date) error {
	if !p.s.Discontinued.IsZero() {
		return fw.Violation("products.discontinued", "the product is already discontinued")
	}
	if on.IsZero() {
		return fw.Violation("products.discontinue_date", "the date is required")
	}
	p.s.Discontinued = on
	p.Raise(ProductDiscontinued{EventMeta: p.NewEventMeta(), Company: p.s.Company.String(), SKU: p.s.SKU, On: on.String()})
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (p *Product) AuditSnapshot() map[string]any {
	return map[string]any{"sku": p.s.SKU, "name": p.s.Name, "basePrice": p.s.BasePrice.String(), "discontinued": p.s.Discontinued.String()}
}

// Campos del producto.
var (
	ProdFieldID       = spec.Comparable("id", func(p *Product) ProductID { return p.ID() })
	ProdFieldCompany  = spec.Comparable("company", func(p *Product) OrganizationID { return p.s.Company })
	ProdFieldSKU      = spec.Ordered("sku", func(p *Product) string { return p.s.SKU })
	ProdFieldName     = spec.Text("name", func(p *Product) string { return p.s.Name })
	ProdFieldKind     = spec.Comparable("kind", func(p *Product) int { return int(p.s.Kind) })
	ProdFieldCategory = spec.Comparable("category_id", func(p *Product) CategoryID { return p.s.Category })
	ProdFieldBarcodes = spec.Collection("barcodes", func(p *Product) []Barcode { return p.s.Barcodes })
	BarcodeFieldValue = spec.Comparable("code_value", func(b Barcode) string { return b.Value })
)
