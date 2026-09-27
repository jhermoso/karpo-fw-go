// Package repotest is the conformance suite of the domain.Repository contract. Every repository
// implementation (memory, each SQL dialect, future document stores...) runs Run against its own
// backend to prove it honours the same semantics: optimistic concurrency, unit-of-work rollback,
// child collections and, above all, that every specification returns exactly the aggregates
// that satisfy it in memory, while being executed by the store.
//
// The suite uses its own small aggregate, Widget, exercising every kind of typed field.
package repotest

import (
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Kind is the aggregate type name of Widget.
const Kind = "repotest.widget"

// WidgetID identifies a Widget.
type WidgetID struct{ domain.UUID }

// NewWidgetID returns a new identity.
func NewWidgetID() WidgetID { return WidgetID{domain.NewUUID()} }

// Part is a child value object of Widget.
type Part struct {
	Name string
	Qty  int64
}

// Widget is the aggregate used by the conformance suite.
type Widget struct {
	domain.BaseAggregateRoot[WidgetID]
	name      string
	price     int64
	active    bool
	color     *string
	createdAt time.Time
	weight    vocab.Decimal
	launch    vocab.Date
	parts     []Part
}

// WidgetCreated is raised when a Widget is created.
type WidgetCreated struct {
	domain.EventMeta
	Name string `json:"name"`
}

// EventType implements domain.Event.
func (WidgetCreated) EventType() string { return "repotest.widget_created" }

// WidgetRenamed is raised when a Widget is renamed.
type WidgetRenamed struct {
	domain.EventMeta
	From string `json:"from"`
	To   string `json:"to"`
}

// EventType implements domain.Event.
func (WidgetRenamed) EventType() string { return "repotest.widget_renamed" }

// NewWidget creates a new Widget and raises WidgetCreated. Weight (a decimal) and launch date (a
// civil date) are derived deterministically: price/8 and the calendar day of createdAt, so the
// conformance suite also exercises exact decimals and dates on every store.
func NewWidget(id WidgetID, name string, price int64, active bool, color *string, createdAt time.Time, parts ...Part) (*Widget, error) {
	weight := vocab.DecimalFromInt(price).Div(vocab.DecimalFromInt(8))
	w, err := Reconstitute(id, name, price, active, color, createdAt, weight, vocab.DateOf(createdAt.UTC()), parts)
	if err != nil {
		return nil, err
	}
	w.Raise(WidgetCreated{EventMeta: w.NewEventMeta(), Name: name})
	return w, nil
}

// Reconstitute rebuilds a Widget from persisted state without raising events.
func Reconstitute(id WidgetID, name string, price int64, active bool, color *string, createdAt time.Time,
	weight vocab.Decimal, launch vocab.Date, parts []Part) (*Widget, error) {
	base, err := domain.NewBaseAggregateRoot(Kind, id)
	if err != nil {
		return nil, err
	}
	var v domain.Validation
	v.Require(name != "", "name", "required", "name is required")
	v.Require(price >= 0, "price", "range", "price must not be negative")
	if err := v.Err(); err != nil {
		return nil, err
	}
	if color != nil {
		c := *color
		color = &c
	}
	return &Widget{
		BaseAggregateRoot: base,
		name:              name,
		price:             price,
		active:            active,
		color:             color,
		createdAt:         createdAt.UTC(),
		weight:            weight,
		launch:            launch,
		parts:             slices.Clone(parts),
	}, nil
}

// Name returns the name.
func (w *Widget) Name() string { return w.name }

// Price returns the price in cents.
func (w *Widget) Price() int64 { return w.price }

// Active reports whether the widget is active.
func (w *Widget) Active() bool { return w.active }

// Color returns the optional color.
func (w *Widget) Color() *string { return w.color }

// CreatedAt returns the creation instant.
func (w *Widget) CreatedAt() time.Time { return w.createdAt }

// Weight returns the weight (exact decimal).
func (w *Widget) Weight() vocab.Decimal { return w.weight }

// Launch returns the launch date (civil date).
func (w *Widget) Launch() vocab.Date { return w.launch }

// Parts returns the parts.
func (w *Widget) Parts() []Part { return slices.Clone(w.parts) }

// Rename changes the name and raises WidgetRenamed.
func (w *Widget) Rename(name string) error {
	if name == "" {
		return domain.Violation("widget.name_required", "name is required")
	}
	old := w.name
	w.name = name
	w.Raise(WidgetRenamed{EventMeta: w.NewEventMeta(), From: old, To: name})
	return nil
}

// SetPrice changes the price.
func (w *Widget) SetPrice(p int64) { w.price = p }

// ReplaceParts replaces the parts collection.
func (w *Widget) ReplaceParts(parts ...Part) { w.parts = slices.Clone(parts) }

// Clone returns a deep copy (for the memory repository's isolation option).
func (w *Widget) Clone() *Widget {
	c := *w
	c.parts = slices.Clone(w.parts)
	if w.color != nil {
		col := *w.color
		c.color = &col
	}
	return &c
}

// Typed fields of Widget used by specifications. Adapters map the logical names to storage.
var (
	FieldID        = spec.Comparable[*Widget, WidgetID]("id", (*Widget).ID)
	FieldName      = spec.Text[*Widget]("name", (*Widget).Name)
	FieldPrice     = spec.Ordered[*Widget, int64]("price", (*Widget).Price)
	FieldActive    = spec.Comparable[*Widget, bool]("active", (*Widget).Active)
	FieldColor     = spec.Optional[*Widget, string]("color", (*Widget).Color)
	FieldCreatedAt = spec.Time[*Widget]("created_at", (*Widget).CreatedAt)
	FieldParts     = spec.Collection[*Widget, Part]("parts", (*Widget).Parts)
	FieldWeight    = spec.OrderedBy[*Widget, vocab.Decimal]("weight", (*Widget).Weight, vocab.CompareDecimal)
	FieldLaunch    = spec.OrderedBy[*Widget, vocab.Date]("launch", (*Widget).Launch, vocab.CompareDates)

	PartName = spec.Text[Part]("name", func(p Part) string { return p.Name })
	PartQty  = spec.Ordered[Part, int64]("qty", func(p Part) int64 { return p.Qty })
)

// PremiumName is the name of the custom specification Premium.
const PremiumName = "repotest.premium"

// Premium is a custom specification: active widgets priced at least minPrice.
// Every adapter must provide its own translation of PremiumName.
func Premium(minPrice int64) spec.Spec[*Widget] {
	return spec.Custom(PremiumName, func(w *Widget) bool { return w.active && w.price >= minPrice }, minPrice)
}

// Unknown is a custom specification no adapter translates; stores that cannot evaluate it
// natively must fail with domain.ErrUnsupported.
func Unknown() spec.Spec[*Widget] {
	return spec.Custom("repotest.unknown", func(*Widget) bool { return true })
}
