// Package domain is the Assets model: the fixed assets of a company with their cost, residual
// value and useful life, their straight-line depreciation month by month, and their disposal. The
// C# FixedAsset had no amount at all (no cost, no useful life), depreciation was a free-text
// "formula" nothing interpreted and an accounting header with no lines, and assets were never
// disposed of.
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

// AssetKind is the stable aggregate type name.
const AssetKind = "assets.asset"

// Identities of the context.
type (
	// AssetID identifies a fixed asset.
	AssetID struct{ fw.UUID }
	// OrganizationID is the owning company, an internal organization of the Parties context.
	OrganizationID struct{ fw.UUID }
	// PartyID is the supplier, a party of the Parties context.
	PartyID struct{ fw.UUID }
	// FacilityID is where the asset is, a facility of the Facilities context.
	FacilityID struct{ fw.UUID }
)

// Class is the kind of a fixed asset (the C# kept four empty side tables for it).
type Class string

// Classes. Land does not depreciate.
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

// Classes lists the valid classes.
var Classes = []Class{Land, Buildings, Machinery, Tooling, Furniture, Computers, Vehicles, Software, Other}

// Period is a month of a year.
type Period struct {
	Year  int
	Month int
}

// PeriodOf returns the month of a date.
func PeriodOf(d vocab.Date) Period { return Period{d.Year(), int(d.Month())} }

func (p Period) index() int { return p.Year*12 + p.Month - 1 }

// Before reports whether p is an earlier month than o.
func (p Period) Before(o Period) bool { return p.index() < o.index() }

// Next returns the following month.
func (p Period) Next() Period {
	if p.Month == 12 {
		return Period{p.Year + 1, 1}
	}
	return Period{p.Year, p.Month + 1}
}

// End returns the last day of the month.
func (p Period) End() vocab.Date {
	n := p.Next()
	return vocab.MustDate(n.Year, time.Month(n.Month), 1).AddDays(-1)
}

// String renders YYYY-MM.
func (p Period) String() string { return fmt.Sprintf("%04d-%02d", p.Year, p.Month) }

// Valid reports whether p is a real month.
func (p Period) Valid() bool {
	return p.Year >= 1990 && p.Year <= 2200 && p.Month >= 1 && p.Month <= 12
}

// Charge is the depreciation of a month.
type Charge struct {
	Period Period
	Amount vocab.Decimal
}

// Disposal kinds.
const (
	Sale  = "sale"
	Scrap = "scrap"
)

// Details are the descriptive data of an asset, which may change.
type Details struct {
	Name     string
	Serial   string
	Location FacilityID
}

// AssetState is the persisted state of an asset.
type AssetState struct {
	Company OrganizationID
	Code    string
	Class   Class
	Details
	Supplier   PartyID
	Document   string // the supplier's invoice it was bought with
	Acquired   vocab.Date
	InService  vocab.Date // depreciation starts on this day
	Cost       vocab.Decimal
	Residual   vocab.Decimal
	LifeMonths int // useful life; zero for land
	Charges    []Charge
	Disposed   vocab.Date
	Disposal   string        // sale or scrap
	Proceeds   vocab.Decimal // what the sale brought
	Audit      traits.AuditStamp
}

// Asset is a fixed asset of a company.
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

// ReconstituteAsset rebuilds an asset.
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

// RegisterAsset puts an asset in the register of a company.
func RegisterAsset(id AssetID, s AssetState) (*Asset, error) {
	s.Charges, s.Disposed, s.Disposal, s.Proceeds = nil, vocab.Date{}, "", zero()
	a, err := ReconstituteAsset(id, s)
	if err != nil {
		return nil, err
	}
	a.Raise(AssetRegistered{EventMeta: a.NewEventMeta(), Company: s.Company.String(), Code: a.s.Code, Class: string(s.Class), Cost: s.Cost.StringFixed(2)})
	return a, nil
}

// State returns the state (charges are a copy).
func (a *Asset) State() AssetState {
	s := a.s
	s.Charges = slices.Clone(s.Charges)
	return s
}

// Depreciable returns what is depreciated over the useful life: cost less residual value.
func (a *Asset) Depreciable() vocab.Decimal {
	if a.s.LifeMonths == 0 {
		return zero()
	}
	return a.s.Cost.Sub(a.s.Residual)
}

// Accumulated returns the depreciation charged so far.
func (a *Asset) Accumulated() vocab.Decimal {
	t := zero()
	for _, c := range a.s.Charges {
		t = t.Add(c.Amount)
	}
	return t
}

// NetBookValue returns the cost less the accumulated depreciation.
func (a *Asset) NetBookValue() vocab.Decimal { return a.s.Cost.Sub(a.Accumulated()) }

// Monthly returns the charge of a full month.
func (a *Asset) Monthly() vocab.Decimal {
	if a.s.LifeMonths == 0 {
		return zero()
	}
	return a.Depreciable().Div(vocab.DecimalFromInt(int64(a.s.LifeMonths))).Round(2)
}

// FullyDepreciated reports whether nothing remains to depreciate.
func (a *Asset) FullyDepreciated() bool { return !a.Depreciable().GreaterThan(a.Accumulated()) }

// next returns the month of the next charge.
func (a *Asset) next() Period {
	if n := len(a.s.Charges); n > 0 {
		return a.s.Charges[n-1].Period.Next()
	}
	return PeriodOf(a.s.InService)
}

// chargeOf computes the charge of the next month: the monthly amount, the days in service of the
// first month, and never more than what remains (so the last charge absorbs the rounding).
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

// DepreciateThrough charges every month not yet charged up to one, in order, and returns the new
// charges (none when the asset is up to date, fully depreciated, disposed of or land). Months are
// charged once and never skipped.
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

// Change replaces the descriptive data. Cost, residual value and useful life never change: they
// are what the charges already made were computed with.
func (a *Asset) Change(d Details) error {
	var v fw.Validation
	checkDetails(&v, &d)
	if err := v.Err(); err != nil {
		return err
	}
	a.s.Details = d
	return nil
}

// Dispose removes the asset from the register on a date: sold for some proceeds, or scrapped for
// none. It is depreciated first through the month before, so the result is the proceeds less the
// net book value of that day.
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

// Result returns the gain (positive) or loss of the disposal.
func (a *Asset) Result() vocab.Decimal {
	if a.s.Disposed.IsZero() {
		return zero()
	}
	return a.s.Proceeds.Sub(a.NetBookValue())
}

// Status returns in-service, depreciated or disposed.
func (a *Asset) Status() string {
	switch {
	case !a.s.Disposed.IsZero():
		return "disposed"
	case a.s.LifeMonths > 0 && a.FullyDepreciated():
		return "depreciated"
	}
	return "in-service"
}

// AuditSnapshot implements traits.Snapshotter.
func (a *Asset) AuditSnapshot() map[string]any {
	return map[string]any{"code": a.s.Code, "cost": a.s.Cost.String(), "accumulated": a.Accumulated().String(), "status": a.Status()}
}

// Asset fields.
var (
	AstFieldCompany  = spec.Comparable("company", func(a *Asset) OrganizationID { return a.s.Company })
	AstFieldCode     = spec.Ordered("code", func(a *Asset) string { return a.s.Code })
	AstFieldClass    = spec.Comparable("asset_class", func(a *Asset) string { return string(a.s.Class) })
	AstFieldDisposed = spec.Comparable("disposed", func(a *Asset) bool { return !a.s.Disposed.IsZero() })
)

// Events of the context.
type (
	// AssetRegistered is raised when an asset enters the register.
	AssetRegistered struct {
		fw.EventMeta
		Company string `json:"company"`
		Code    string `json:"code"`
		Class   string `json:"class"`
		Cost    string `json:"cost"`
	}
	// DepreciationCharged is raised for each month charged.
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
	// AssetDisposed is raised when an asset is sold or scrapped.
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

// EventType implementations.
func (AssetRegistered) EventType() string     { return "assets.asset_registered" }
func (DepreciationCharged) EventType() string { return "assets.depreciation_charged" }
func (AssetDisposed) EventType() string       { return "assets.asset_disposed" }

// NewAssetID returns a new identity.
func NewAssetID() AssetID { return AssetID{fw.NewUUID()} }

// ParseAssetID parses a textual identity.
func ParseAssetID(s string) (AssetID, error) { u, err := fw.ParseUUID(s); return AssetID{u}, err }

// AssetRepository stores assets.
type AssetRepository = fw.Repository[AssetID, *Asset]
