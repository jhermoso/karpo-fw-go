// Package domain is the Products model: the catalog of what a company sells, buys and stocks
// (goods and services with their unit, tax code, barcodes, kit components and suppliers), its
// categories and its price lists. The C# had a wide UDM catalog where every Validate() returned
// success and price, cost, barcodes, supplier and stock thresholds were each modelled two or
// three times without a link.
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

// ProductKind is the stable aggregate type name.
const ProductKind = "products.product"

// Kind tells goods from services (the C# kept it as a free string and a TPT subtype at once).
type Kind int

// Kinds.
const (
	Good Kind = iota + 1
	Service
)

var kinds = map[Kind]string{Good: "good", Service: "service"}

// String returns the stable name.
func (k Kind) String() string { return kinds[k] }

// ParseKind parses a kind name.
func ParseKind(s string) (Kind, bool) {
	for k, n := range kinds {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// Tracking is how the stock of a good is identified (the C# TrackingMode was a free string).
type Tracking int

// Tracking modes.
const (
	TrackNone Tracking = iota + 1
	TrackLot
	TrackSerial
)

var trackings = map[Tracking]string{TrackNone: "none", TrackLot: "lot", TrackSerial: "serial"}

// String returns the stable name.
func (t Tracking) String() string { return trackings[t] }

// ParseTracking parses a tracking mode name.
func ParseTracking(s string) (Tracking, bool) {
	for k, n := range trackings {
		if n == s {
			return k, true
		}
	}
	return 0, false
}

// Barcode is an identification code of a product.
type Barcode struct {
	Type  string // ean-13 | ean-8 | upc-a | internal
	Value string
}

// barcodeDigits are the lengths of the GTIN types, whose last digit is a check digit.
var barcodeDigits = map[string]int{"ean-13": 13, "ean-8": 8, "upc-a": 12}

// ValidGTIN reports whether digits is a GTIN with a correct check digit (weights 3 and 1 from the
// right, modulo 10).
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

// Component is a product a kit is made of.
type Component struct {
	Product  ProductID
	Quantity vocab.Decimal
}

// SupplierItem is how a supplier names and delivers the product.
type SupplierItem struct {
	Supplier  PartyID
	Code      string // the supplier's reference
	LeadDays  int
	Preferred bool
}

// Details are the editable data of a product.
type Details struct {
	Name            string
	Description     string
	Kind            Kind
	UoM             string
	Category        CategoryID
	TaxCode         string        // tax code of the Fiscal catalog (G21, R10…)
	ExpenseCategory string        // expense category of Purchases when bought (goods, supplies…)
	BasePrice       vocab.Decimal // sales price without a price list, up to 4 decimals
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

// ProductState is the persisted state of a product.
type ProductState struct {
	Company OrganizationID
	SKU     string
	Details
	Discontinued vocab.Date
	Audit        traits.AuditStamp
}

// Product is something a company sells, buys or stocks.
type Product struct {
	fw.BaseAggregateRoot[ProductID]
	traits.Audited
	s ProductState
}

// MaxComponents bounds the components of a kit.
const MaxComponents = 200

// NormalizeSKU returns the canonical form of a SKU: trimmed, upper case.
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

// ReconstituteProduct rebuilds a product.
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

// RegisterProduct adds a product to the catalog of a company.
func RegisterProduct(id ProductID, company OrganizationID, sku string, d Details) (*Product, error) {
	p, err := ReconstituteProduct(id, ProductState{Company: company, SKU: sku, Details: d})
	if err != nil {
		return nil, err
	}
	p.Raise(ProductRegistered{EventMeta: p.NewEventMeta(), Company: company.String(), SKU: p.s.SKU, Name: p.s.Name, Kind: p.s.Kind.String()})
	return p, nil
}

// State returns the state (slices are copies).
func (p *Product) State() ProductState {
	s := p.s
	s.Barcodes, s.Components, s.Suppliers = slices.Clone(s.Barcodes), slices.Clone(s.Components), slices.Clone(s.Suppliers)
	return s
}

// Active reports whether the product is not discontinued on a date.
func (p *Product) Active(on vocab.Date) bool {
	return p.s.Discontinued.IsZero() || on.Before(p.s.Discontinued)
}

// Change replaces the details. A discontinued product does not change; the kind of a product does
// not change either (a good in stock cannot become a service).
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

// Discontinue retires the product from a date (it is never deleted: documents keep pointing to it).
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

// AuditSnapshot implements traits.Snapshotter.
func (p *Product) AuditSnapshot() map[string]any {
	return map[string]any{"sku": p.s.SKU, "name": p.s.Name, "basePrice": p.s.BasePrice.String(), "discontinued": p.s.Discontinued.String()}
}

// Product fields.
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
