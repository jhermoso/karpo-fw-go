// Package domain es el modelo de Assets: el inmovilizado de una empresa con su coste, valor residual
// y vida útil, su amortización lineal mes a mes, y su baja. El FixedAsset del C# no tenía ningún
// importe (ni coste ni vida útil), la amortización era una «fórmula» de texto libre que nada
// interpretaba y una cabecera contable sin líneas, y los activos nunca se daban de baja.
package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// AssetKind es el nombre estable del tipo de agregado.
const AssetKind = "assets.asset"

// Identidades del contexto.
type (
	// AssetID identifica un elemento del inmovilizado.
	AssetID struct{ fw.UUID }
	// OrganizationID es la empresa propietaria, una organización interna del contexto Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID es el proveedor, una party del contexto Parties.
	PartyID struct{ fw.UUID }
	// FacilityID es donde está el activo, una instalación del contexto Facilities.
	FacilityID struct{ fw.UUID }
)

// Class es la clase de un elemento del inmovilizado (el C# tenía para ello cuatro tablas auxiliares vacías).
type Class string

// Clases. Los terrenos no se amortizan.
const (
	Land      Class = "land"
	Buildings Class = "buildings"
	Machinery Class = "machinery"
	Tooling   Class = "tooling"
	Furniture Class = "furniture"
	Computers Class = "computers"
	Vehicles  Class = "vehicles"
	Software  Class = "software"
	Other     Class = "other"
)

// Classes lista las clases válidas.
var Classes = []Class{Land, Buildings, Machinery, Tooling, Furniture, Computers, Vehicles, Software, Other}

// Period es un mes de un año.
type Period struct {
	Year  int
	Month int
}

// PeriodOf devuelve el mes de una fecha.
func PeriodOf(d vocab.Date) Period { return Period{d.Year(), int(d.Month())} }

func (p Period) index() int { return p.Year*12 + p.Month - 1 }

// Before dice si p es un mes anterior a o.
func (p Period) Before(o Period) bool { return p.index() < o.index() }

// Next devuelve el mes siguiente.
func (p Period) Next() Period {
	if p.Month == 12 {
		return Period{p.Year + 1, 1}
	}
	return Period{p.Year, p.Month + 1}
}

// End devuelve el último día del mes.
func (p Period) End() vocab.Date {
	n := p.Next()
	return vocab.MustDate(n.Year, time.Month(n.Month), 1).AddDays(-1)
}

// String lo escribe como YYYY-MM.
func (p Period) String() string { return fmt.Sprintf("%04d-%02d", p.Year, p.Month) }

// Valid dice si p es un mes real.
func (p Period) Valid() bool {
	return p.Year >= 1990 && p.Year <= 2200 && p.Month >= 1 && p.Month <= 12
}

// Charge es la amortización de un mes.
type Charge struct {
	Period Period
	Amount vocab.Decimal
}

// Tipos de baja.
const (
	Sale  = "sale"
	Scrap = "scrap"
)

// Details son los datos descriptivos de un activo, que pueden cambiar.
type Details struct {
	Name     string
	Serial   string
	Location FacilityID
}

// AssetState es el estado persistido de un activo.
type AssetState struct {
	Company OrganizationID
	Code    string
	Class   Class
	Details
	Supplier   PartyID
	Document   string // la factura del proveedor con la que se compró
	Acquired   vocab.Date
	InService  vocab.Date // la amortización empieza este día
	Cost       vocab.Decimal
	Residual   vocab.Decimal
	LifeMonths int // vida útil; cero para los terrenos
	Charges    []Charge
	Disposed   vocab.Date
	Disposal   string        // venta o desecho
	Proceeds   vocab.Decimal // lo que dio la venta
	Audit      traits.AuditStamp
}

// Asset es un elemento del inmovilizado de una empresa.
type Asset struct {
	fw.BaseAggregateRoot[AssetID]
	traits.Audited
	s AssetState
}

func zero() vocab.Decimal { return vocab.DecimalFromInt(0) }

func cents(d vocab.Decimal) bool { return d.Equal(d.Round(2)) }

func validCode(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for _, r := range s {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '.' || r == '_') {
			return false
		}
	}
	return true
}

func checkDetails(v *fw.Validation, d *Details) {
	d.Name, d.Serial = strings.TrimSpace(d.Name), strings.TrimSpace(d.Serial)
	v.Require(d.Name != "" && utf8.RuneCountInString(d.Name) <= 120, "name", "length", "a name of 1 to 120 characters")
	v.Require(utf8.RuneCountInString(d.Serial) <= 60, "serial", "length", "at most 60 characters")
}

// ReconstituteAsset reconstruye un activo.
func ReconstituteAsset(id AssetID, s AssetState) (*Asset, error) {
	base, err := fw.NewBaseAggregateRoot(AssetKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero(), "company", "required", "the company is required")
	s.Code = strings.ToUpper(strings.TrimSpace(s.Code))
	v.Require(validCode(s.Code), "code", "format", "1 to 20 letters, digits, dots, dashes or underscores")
	v.Require(slices.Contains(Classes, s.Class), "class", "enum", "an asset class")
	checkDetails(&v, &s.Details)
	s.Document = strings.TrimSpace(s.Document)
	v.Require(utf8.RuneCountInString(s.Document) <= 60, "document", "length", "at most 60 characters")
	v.Require(!s.Acquired.IsZero() && !s.InService.IsZero() && !s.InService.Before(s.Acquired), "inService", "range",
		"an acquisition date and a start of service not before it")
	v.Require(s.Cost.IsPositive() && cents(s.Cost), "cost", "range", "a positive cost in cents")
	v.Require(!s.Residual.IsNegative() && cents(s.Residual) && s.Residual.LessThan(s.Cost), "residual", "range", "a residual value under the cost, in cents")
	if s.Class == Land {
		v.Require(s.LifeMonths == 0, "lifeMonths", "land", "land does not depreciate")
	} else {
		v.Require(s.LifeMonths >= 1 && s.LifeMonths <= 1200, "lifeMonths", "range", "a useful life of 1 to 1200 months")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Charges = slices.Clone(s.Charges)
	return &Asset{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// RegisterAsset da de alta un activo en el registro de una empresa.
func RegisterAsset(id AssetID, s AssetState) (*Asset, error) {
	s.Charges, s.Disposed, s.Disposal, s.Proceeds = nil, vocab.Date{}, "", zero()
	a, err := ReconstituteAsset(id, s)
	if err != nil {
		return nil, err
	}
	a.Raise(AssetRegistered{EventMeta: a.NewEventMeta(), Company: s.Company.String(), Code: a.s.Code, Class: string(s.Class), Cost: s.Cost.StringFixed(2)})
	return a, nil
}

// State devuelve el estado (los cargos son una copia).
func (a *Asset) State() AssetState {
	s := a.s
	s.Charges = slices.Clone(s.Charges)
	return s
}

// Depreciable devuelve lo que se amortiza a lo largo de la vida útil: el coste menos el valor residual.
func (a *Asset) Depreciable() vocab.Decimal {
	if a.s.LifeMonths == 0 {
		return zero()
	}
	return a.s.Cost.Sub(a.s.Residual)
}

// Accumulated devuelve la amortización cargada hasta ahora.
func (a *Asset) Accumulated() vocab.Decimal {
	t := zero()
	for _, c := range a.s.Charges {
		t = t.Add(c.Amount)
	}
	return t
}

// NetBookValue devuelve el coste menos la amortización acumulada.
func (a *Asset) NetBookValue() vocab.Decimal { return a.s.Cost.Sub(a.Accumulated()) }

// Monthly devuelve el cargo de un mes completo.
func (a *Asset) Monthly() vocab.Decimal {
	if a.s.LifeMonths == 0 {
		return zero()
	}
	return a.Depreciable().Div(vocab.DecimalFromInt(int64(a.s.LifeMonths))).Round(2)
}

// FullyDepreciated dice si no queda nada por amortizar.
func (a *Asset) FullyDepreciated() bool { return !a.Depreciable().GreaterThan(a.Accumulated()) }

// next devuelve el mes del próximo cargo.
func (a *Asset) next() Period {
	if n := len(a.s.Charges); n > 0 {
		return a.s.Charges[n-1].Period.Next()
	}
	return PeriodOf(a.s.InService)
}

// chargeOf calcula el cargo del mes siguiente: el importe mensual, los días en servicio del primer
// mes, y nunca más de lo que queda (así el último cargo absorbe el redondeo).
func (a *Asset) chargeOf(p Period) vocab.Decimal {
	amount := a.Monthly()
	if p == PeriodOf(a.s.InService) {
		days := p.End().Day()
		amount = amount.Mul(vocab.DecimalFromInt(int64(days - a.s.InService.Day() + 1))).Div(vocab.DecimalFromInt(int64(days))).Round(2)
	}
	if left := a.Depreciable().Sub(a.Accumulated()); amount.GreaterThan(left) {
		amount = left
	}
	return amount
}

// DepreciateThrough carga cada mes aún no cargado hasta uno dado, en orden, y devuelve los cargos
// nuevos (ninguno cuando el activo está al día, totalmente amortizado, dado de baja o es un
// terreno). Los meses se cargan una vez y nunca se saltan.
func (a *Asset) DepreciateThrough(until Period) ([]Charge, error) {
	if !until.Valid() {
		return nil, fw.Violation("assets.period", "a month of a year is required")
	}
	if !a.s.Disposed.IsZero() {
		return nil, nil
	}
	var out []Charge
	for p := a.next(); !until.Before(p) && !a.FullyDepreciated(); p = a.next() {
		amount := a.chargeOf(p)
		if !amount.IsPositive() {
			break
		}
		c := Charge{Period: p, Amount: amount}
		a.s.Charges = append(slices.Clone(a.s.Charges), c)
		out = append(out, c)
		a.Raise(DepreciationCharged{EventMeta: a.NewEventMeta(), Company: a.s.Company.String(), Code: a.s.Code, Class: string(a.s.Class),
			Period: p.String(), Date: p.End().String(), Amount: amount.StringFixed(2), Accumulated: a.Accumulated().StringFixed(2)})
	}
	return out, nil
}

// Change sustituye los datos descriptivos. El coste, el valor residual y la vida útil nunca cambian:
// son con lo que se calcularon los cargos ya hechos.
func (a *Asset) Change(d Details) error {
	var v fw.Validation
	checkDetails(&v, &d)
	if err := v.Err(); err != nil {
		return err
	}
	a.s.Details = d
	return nil
}

// Dispose saca el activo del registro en una fecha: vendido por un importe, o desechado sin él.
// Antes se amortiza hasta el mes anterior, así que el resultado es lo obtenido menos el valor neto
// contable de ese día.
func (a *Asset) Dispose(on vocab.Date, kind string, proceeds vocab.Decimal) error {
	if !a.s.Disposed.IsZero() {
		return fw.Violation("assets.disposed", "the asset is already disposed of")
	}
	if on.IsZero() || on.Before(a.s.InService) {
		return fw.Violation("assets.disposal_date", "an asset is disposed of after entering service")
	}
	if kind != Sale && kind != Scrap {
		return fw.Violation("assets.disposal_kind", "sale or scrap")
	}
	if proceeds.IsNegative() || !cents(proceeds) || (kind == Scrap && !proceeds.IsZero()) || (kind == Sale && !proceeds.IsPositive()) {
		return fw.Violation("assets.proceeds", "a sale has positive proceeds in cents and a scrapping none")
	}
	if n := len(a.s.Charges); n > 0 && !a.s.Charges[n-1].Period.Before(PeriodOf(on)) {
		return fw.Violation("assets.disposal_date", "the asset is already depreciated through that month")
	}
	prev := PeriodOf(on.AddMonths(-1))
	if !prev.Before(PeriodOf(a.s.InService)) {
		if _, err := a.DepreciateThrough(prev); err != nil {
			return err
		}
	}
	nbv := a.NetBookValue()
	a.s.Disposed, a.s.Disposal, a.s.Proceeds = on, kind, proceeds
	a.Raise(AssetDisposed{EventMeta: a.NewEventMeta(), Company: a.s.Company.String(), Code: a.s.Code, Class: string(a.s.Class), Kind: kind,
		Date: on.String(), Cost: a.s.Cost.StringFixed(2), Accumulated: a.Accumulated().StringFixed(2), Proceeds: proceeds.StringFixed(2),
		Result: proceeds.Sub(nbv).StringFixed(2)})
	return nil
}

// Result devuelve el beneficio (positivo) o la pérdida de la baja.
func (a *Asset) Result() vocab.Decimal {
	if a.s.Disposed.IsZero() {
		return zero()
	}
	return a.s.Proceeds.Sub(a.NetBookValue())
}

// Status devuelve in-service, depreciated o disposed.
func (a *Asset) Status() string {
	switch {
	case !a.s.Disposed.IsZero():
		return "disposed"
	case a.s.LifeMonths > 0 && a.FullyDepreciated():
		return "depreciated"
	}
	return "in-service"
}

// AuditSnapshot implementa traits.Snapshotter.
func (a *Asset) AuditSnapshot() map[string]any {
	return map[string]any{"code": a.s.Code, "cost": a.s.Cost.String(), "accumulated": a.Accumulated().String(), "status": a.Status()}
}

// Campos del activo.
var (
	AstFieldCompany  = spec.Comparable("company", func(a *Asset) OrganizationID { return a.s.Company })
	AstFieldCode     = spec.Ordered("code", func(a *Asset) string { return a.s.Code })
	AstFieldClass    = spec.Comparable("asset_class", func(a *Asset) string { return string(a.s.Class) })
	AstFieldDisposed = spec.Comparable("disposed", func(a *Asset) bool { return !a.s.Disposed.IsZero() })
)

// Eventos del contexto.
type (
	// AssetRegistered se lanza cuando un activo entra en el registro.
	AssetRegistered struct {
		fw.EventMeta
		Company string `json:"company"`
		Code    string `json:"code"`
		Class   string `json:"class"`
		Cost    string `json:"cost"`
	}
	// DepreciationCharged se lanza por cada mes cargado.
	DepreciationCharged struct {
		fw.EventMeta
		Company     string `json:"company"`
		Code        string `json:"code"`
		Class       string `json:"class"`
		Period      string `json:"period"`
		Date        string `json:"date"`
		Amount      string `json:"amount"`
		Accumulated string `json:"accumulated"`
	}
	// AssetDisposed se lanza cuando un activo se vende o se desecha.
	AssetDisposed struct {
		fw.EventMeta
		Company     string `json:"company"`
		Code        string `json:"code"`
		Class       string `json:"class"`
		Kind        string `json:"kind"`
		Date        string `json:"date"`
		Cost        string `json:"cost"`
		Accumulated string `json:"accumulated"`
		Proceeds    string `json:"proceeds"`
		Result      string `json:"result"`
	}
)

// Implementaciones de EventType.
func (AssetRegistered) EventType() string     { return "assets.asset_registered" }
func (DepreciationCharged) EventType() string { return "assets.depreciation_charged" }
func (AssetDisposed) EventType() string       { return "assets.asset_disposed" }

// NewAssetID devuelve una identidad nueva.
func NewAssetID() AssetID { return AssetID{fw.NewUUID()} }

// ParseAssetID interpreta una identidad en texto.
func ParseAssetID(s string) (AssetID, error) { u, err := fw.ParseUUID(s); return AssetID{u}, err }

// AssetRepository guarda los activos.
type AssetRepository = fw.Repository[AssetID, *Asset]
