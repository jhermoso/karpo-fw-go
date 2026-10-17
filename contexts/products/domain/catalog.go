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

// Unit es una unidad de medida. Las unidades de la misma dimensión con factor se convierten entre
// sí a través de la unidad base de la dimensión; los embalajes (caja, palé) no, su contenido depende
// del producto. El C# sembraba 14 unidades y una tabla de conversión cuyo factor no podía fijarse nunca.
type Unit struct {
	Code      string
	Name      string
	Dimension string        // count | mass | volume | length | area | time | package
	Factor    vocab.Decimal // unidades de la base de su dimensión; cero cuando no se convierte
}

func unit(code, name, dim, factor string) Unit {
	return Unit{Code: code, Name: name, Dimension: dim, Factor: vocab.MustDecimal(factor)}
}

// Units es el catálogo de unidades de medida (los códigos de la siembra del C#).
var Units = []Unit{
	unit("ea", "Unidad", "count", "1"), unit("pza", "Pieza", "count", "1"),
	unit("kg", "Kilogramo", "mass", "1"), unit("g", "Gramo", "mass", "0.001"),
	unit("l", "Litro", "volume", "1"), unit("ml", "Mililitro", "volume", "0.001"), unit("m3", "Metro cúbico", "volume", "1000"),
	unit("m", "Metro", "length", "1"), unit("cm", "Centímetro", "length", "0.01"),
	unit("m2", "Metro cuadrado", "area", "1"),
	unit("h", "Hora", "time", "1"), unit("min", "Minuto", "time", "0.0166666667"),
	unit("caja", "Caja", "package", "0"), unit("pallet", "Palé", "package", "0"),
}

// UnitOf devuelve la unidad de un código.
func UnitOf(code string) (Unit, bool) {
	k := slices.IndexFunc(Units, func(u Unit) bool { return u.Code == code })
	if k < 0 {
		return Unit{}, false
	}
	return Units[k], true
}

// Convert convierte una cantidad entre dos unidades de la misma dimensión.
func Convert(q vocab.Decimal, from, to string) (vocab.Decimal, error) {
	f, ok1 := UnitOf(from)
	t, ok2 := UnitOf(to)
	if !ok1 || !ok2 || f.Dimension != t.Dimension || f.Factor.IsZero() || t.Factor.IsZero() {
		return vocab.Decimal{}, fw.Violation("products.uom_conversion", "no conversion from "+from+" to "+to)
	}
	if from == to {
		return q, nil
	}
	if from == "min" && to == "h" { // exacto, en lugar del factor redondeado
		return q.Div(vocab.DecimalFromInt(60)).Round(6), nil
	}
	if from == "h" && to == "min" {
		return q.Mul(vocab.DecimalFromInt(60)), nil
	}
	return q.Mul(f.Factor).Div(t.Factor).Round(6), nil
}

// CategoryKind es el nombre estable del tipo de agregado.
const CategoryKind = "products.category"

// CategoryState es el estado persistido de una categoría.
type CategoryState struct {
	Company OrganizationID
	Code    string
	Name    string
	Parent  CategoryID
	Audit   traits.AuditStamp
}

// Category clasifica en un árbol los productos de una empresa (el C# sembraba ocho categorías sin
// nombre y tres de la vertical de cambio de divisas).
type Category struct {
	fw.BaseAggregateRoot[CategoryID]
	traits.Audited
	s CategoryState
}

// ReconstituteCategory reconstruye una categoría.
func ReconstituteCategory(id CategoryID, s CategoryState) (*Category, error) {
	base, err := fw.NewBaseAggregateRoot(CategoryKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	s.Code, s.Name = NormalizeSKU(s.Code), strings.TrimSpace(s.Name)
	v.Require(!s.Company.IsZero(), "company", "required", "the company is required")
	v.Require(validSKU(s.Code) && len(s.Code) <= 20, "code", "format", "1 to 20 letters, digits, dots, dashes or underscores")
	v.Require(s.Name != "" && utf8.RuneCountInString(s.Name) <= 100, "name", "length", "a name of 1 to 100 characters")
	v.Require(s.Parent != id, "parent", "self", "a category is not its own parent")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Category{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State devuelve el estado.
func (c *Category) State() CategoryState { return c.s }

// Rename cambia el nombre de la categoría.
func (c *Category) Rename(name string) error {
	s := c.s
	s.Name = name
	if _, err := ReconstituteCategory(c.ID(), s); err != nil {
		return err
	}
	c.s.Name = strings.TrimSpace(name)
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (c *Category) AuditSnapshot() map[string]any {
	return map[string]any{"code": c.s.Code, "name": c.s.Name}
}

// Campos de la categoría.
var (
	CatFieldCompany = spec.Comparable("company", func(c *Category) OrganizationID { return c.s.Company })
	CatFieldCode    = spec.Ordered("code", func(c *Category) string { return c.s.Code })
)

// PriceListKind es el nombre estable del tipo de agregado.
const PriceListKind = "products.price_list"

// MaxPriceLines limita las líneas de una lista de precios.
const MaxPriceLines = 5000

// PriceLine es el precio de un producto en una lista a partir de una cantidad, dentro de una vigencia.
type PriceLine struct {
	Product     ProductID
	MinQuantity vocab.Decimal
	UnitPrice   vocab.Decimal
	Discount    vocab.Decimal // porcentaje sobre el precio unitario
	From        vocab.Date
	To          vocab.Date // cero: abierta
}

func (l PriceLine) validOn(d vocab.Date) bool {
	return !d.Before(l.From) && (l.To.IsZero() || !d.After(l.To))
}

func (l PriceLine) overlaps(o PriceLine) bool {
	return (o.To.IsZero() || !l.From.After(o.To)) && (l.To.IsZero() || !o.From.After(l.To))
}

// PriceListState es el estado persistido de una lista de precios.
type PriceListState struct {
	Company  OrganizationID
	Code     string
	Name     string
	Currency vocab.CurrencyCode
	Active   bool
	Lines    []PriceLine
	Audit    traits.AuditStamp
}

// PriceList es una tarifa de una empresa (el C# tenía las listas de precios en Parties, leía su
// vigencia en un solo sitio y su moneda nunca).
type PriceList struct {
	fw.BaseAggregateRoot[PriceListID]
	traits.Audited
	s PriceListState
}

func checkLine(v *fw.Validation, f string, l PriceLine) {
	v.Require(!l.Product.IsZero(), f+".product", "required", "the product is required")
	v.Require(!l.MinQuantity.IsNegative() && l.MinQuantity.Equal(l.MinQuantity.Round(4)), f+".minQuantity", "range", "a quantity of up to 4 decimals")
	v.Require(price(l.UnitPrice), f+".unitPrice", "range", "a non-negative price of up to 4 decimals")
	v.Require(!l.Discount.IsNegative() && !l.Discount.GreaterThan(vocab.DecimalFromInt(100)) && l.Discount.Equal(l.Discount.Round(2)), f+".discount",
		"range", "a percentage from 0 to 100")
	v.Require(!l.From.IsZero() && (l.To.IsZero() || !l.To.Before(l.From)), f+".from", "range", "a start date and an end not before it")
}

// ReconstitutePriceList reconstruye una lista de precios.
func ReconstitutePriceList(id PriceListID, s PriceListState) (*PriceList, error) {
	base, err := fw.NewBaseAggregateRoot(PriceListKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	s.Code, s.Name = NormalizeSKU(s.Code), strings.TrimSpace(s.Name)
	v.Require(!s.Company.IsZero(), "company", "required", "the company is required")
	v.Require(validSKU(s.Code) && len(s.Code) <= 20, "code", "format", "1 to 20 letters, digits, dots, dashes or underscores")
	v.Require(s.Name != "" && utf8.RuneCountInString(s.Name) <= 100, "name", "length", "a name of 1 to 100 characters")
	v.Require(s.Currency.String() == "EUR", "currency", "supported", "only euros for now")
	v.Require(len(s.Lines) <= MaxPriceLines, "lines", "count", "too many lines")
	for i, l := range s.Lines {
		checkLine(&v, fmt.Sprintf("lines[%d]", i), l)
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Lines = slices.Clone(s.Lines)
	return &PriceList{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State devuelve el estado (las líneas son una copia).
func (p *PriceList) State() PriceListState {
	s := p.s
	s.Lines = slices.Clone(s.Lines)
	return s
}

// SetPrice añade un precio. Invariante: dos precios de un producto desde la misma cantidad no se
// solapan en el tiempo (el C# cogía la primera fila que devolvía la base de datos).
func (p *PriceList) SetPrice(l PriceLine) error {
	var v fw.Validation
	checkLine(&v, "line", l)
	if err := v.Err(); err != nil {
		return err
	}
	if slices.ContainsFunc(p.s.Lines, func(x PriceLine) bool {
		return x.Product == l.Product && x.MinQuantity.Equal(l.MinQuantity) && x.overlaps(l)
	}) {
		return fw.Violation("products.price_overlap", "the product already has a price from that quantity in those dates")
	}
	if len(p.s.Lines) >= MaxPriceLines {
		return fw.Violation("products.price_list_full", "the price list is full")
	}
	p.s.Lines = append(slices.Clone(p.s.Lines), l)
	return nil
}

// RemovePrice quita el precio de un producto desde una cantidad, a partir de una fecha.
func (p *PriceList) RemovePrice(product ProductID, minQuantity vocab.Decimal, from vocab.Date) error {
	k := slices.IndexFunc(p.s.Lines, func(x PriceLine) bool {
		return x.Product == product && x.MinQuantity.Equal(minQuantity) && x.From == from
	})
	if k < 0 {
		return fw.NotFound("products.price_line", product)
	}
	p.s.Lines = slices.Delete(slices.Clone(p.s.Lines), k, k+1)
	return nil
}

// SetActive activa o retira la lista.
func (p *PriceList) SetActive(active bool) { p.s.Active = active }

// PriceFor devuelve el precio de un producto para una cantidad en una fecha: la línea vigente con
// la mayor cantidad mínima que no supere la cantidad.
func (p *PriceList) PriceFor(product ProductID, quantity vocab.Decimal, on vocab.Date) (PriceLine, bool) {
	var best PriceLine
	found := false
	for _, l := range p.s.Lines {
		if l.Product != product || !l.validOn(on) || l.MinQuantity.GreaterThan(quantity) {
			continue
		}
		if !found || l.MinQuantity.GreaterThan(best.MinQuantity) {
			best, found = l, true
		}
	}
	return best, found
}

// AuditSnapshot implementa traits.Snapshotter.
func (p *PriceList) AuditSnapshot() map[string]any {
	return map[string]any{"code": p.s.Code, "lines": len(p.s.Lines), "active": p.s.Active}
}

// Campos de la lista de precios.
var (
	PlFieldCompany = spec.Comparable("company", func(p *PriceList) OrganizationID { return p.s.Company })
	PlFieldCode    = spec.Ordered("code", func(p *PriceList) string { return p.s.Code })
)

// Quote es el precio de un producto para una cantidad en una fecha.
type Quote struct {
	UnitPrice vocab.Decimal // antes del descuento
	Discount  vocab.Decimal // porcentaje
	Net       vocab.Decimal // precio unitario tras el descuento, 4 decimales
	Source    string        // "list" o "base"
}

// QuoteOf pone precio a un producto: la línea de la lista cuando la tiene; si no, su precio base.
func QuoteOf(p *Product, list *PriceList, quantity vocab.Decimal, on vocab.Date) Quote {
	if list != nil && list.s.Active {
		if l, ok := list.PriceFor(p.ID(), quantity, on); ok {
			net := l.UnitPrice.Mul(vocab.DecimalFromInt(100).Sub(l.Discount)).Div(vocab.DecimalFromInt(100)).Round(4)
			return Quote{UnitPrice: l.UnitPrice, Discount: l.Discount, Net: net, Source: "list"}
		}
	}
	return Quote{UnitPrice: p.s.BasePrice, Discount: vocab.DecimalFromInt(0), Net: p.s.BasePrice, Source: "base"}
}

// Identidades del contexto.
type (
	// ProductID identifica un producto.
	ProductID struct{ fw.UUID }
	// CategoryID identifica una categoría.
	CategoryID struct{ fw.UUID }
	// PriceListID identifica una lista de precios.
	PriceListID struct{ fw.UUID }
	// OrganizationID es la empresa, una organización interna del contexto Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID es un proveedor, una party del contexto Parties.
	PartyID struct{ fw.UUID }
)

// Eventos del contexto.
type (
	// ProductRegistered se lanza cuando un producto entra en el catálogo.
	ProductRegistered struct {
		fw.EventMeta
		Company string `json:"company"`
		SKU     string `json:"sku"`
		Name    string `json:"name"`
		Kind    string `json:"kind"`
	}
	// ProductDiscontinued se lanza cuando un producto se retira.
	ProductDiscontinued struct {
		fw.EventMeta
		Company string `json:"company"`
		SKU     string `json:"sku"`
		On      string `json:"on"`
	}
)

// Implementaciones de EventType.
func (ProductRegistered) EventType() string   { return "products.product_registered" }
func (ProductDiscontinued) EventType() string { return "products.product_discontinued" }

// Identidades nuevas y análisis.
func NewProductID() ProductID     { return ProductID{fw.NewUUID()} }
func NewCategoryID() CategoryID   { return CategoryID{fw.NewUUID()} }
func NewPriceListID() PriceListID { return PriceListID{fw.NewUUID()} }

// ParseProductID analiza una identidad textual.
func ParseProductID(s string) (ProductID, error) { u, err := fw.ParseUUID(s); return ProductID{u}, err }

// ParseCategoryID analiza una identidad textual.
func ParseCategoryID(s string) (CategoryID, error) {
	u, err := fw.ParseUUID(s)
	return CategoryID{u}, err
}

// ParsePriceListID analiza una identidad textual.
func ParsePriceListID(s string) (PriceListID, error) {
	u, err := fw.ParseUUID(s)
	return PriceListID{u}, err
}

// Repositorios del contexto.
type (
	ProductRepository   = fw.Repository[ProductID, *Product]
	CategoryRepository  = fw.Repository[CategoryID, *Category]
	PriceListRepository = fw.Repository[PriceListID, *PriceList]
)
