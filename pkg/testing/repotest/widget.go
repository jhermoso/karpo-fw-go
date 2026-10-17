// Package repotest es la batería de conformidad del contrato domain.Repository. Cada
// implementación de repositorio (memoria, cada dialecto SQL, futuros almacenes de
// documentos...) ejecuta Run contra su propio almacén para demostrar que respeta la misma
// semántica: concurrencia optimista, rollback de la unidad de trabajo, colecciones hijas y,
// sobre todo, que cada especificación devuelve exactamente los agregados que la cumplen en
// memoria, aunque la ejecute el almacén.
//
// La batería usa su propio agregado pequeño, Widget, que ejercita todos los tipos de campo
// tipado.
package repotest

import (
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Kind es el nombre del tipo de agregado de Widget.
const Kind = "repotest.widget"

// WidgetID identifica un Widget.
type WidgetID struct{ domain.UUID }

// NewWidgetID devuelve una identidad nueva.
func NewWidgetID() WidgetID { return WidgetID{domain.NewUUID()} }

// Part es un objeto de valor hijo de Widget.
type Part struct {
	Name string
	Qty  int64
}

// Widget es el agregado que usa la batería de conformidad.
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

// WidgetCreated se lanza cuando se crea un Widget.
type WidgetCreated struct {
	domain.EventMeta
	Name string `json:"name"`
}

// EventType implementa domain.Event.
func (WidgetCreated) EventType() string { return "repotest.widget_created" }

// WidgetRenamed se lanza cuando se renombra un Widget.
type WidgetRenamed struct {
	domain.EventMeta
	From string `json:"from"`
	To   string `json:"to"`
}

// EventType implementa domain.Event.
func (WidgetRenamed) EventType() string { return "repotest.widget_renamed" }

// NewWidget crea un Widget nuevo y lanza WidgetCreated. El peso (un decimal) y la fecha de
// lanzamiento (una fecha civil) se derivan de forma determinista: price/8 y el día natural de
// createdAt, así que la batería de conformidad también ejercita decimales exactos y fechas en
// todos los almacenes.
func NewWidget(id WidgetID, name string, price int64, active bool, color *string, createdAt time.Time, parts ...Part) (*Widget, error) {
	weight := vocab.DecimalFromInt(price).Div(vocab.DecimalFromInt(8))
	w, err := Reconstitute(id, name, price, active, color, createdAt, weight, vocab.DateOf(createdAt.UTC()), parts)
	if err != nil {
		return nil, err
	}
	w.Raise(WidgetCreated{EventMeta: w.NewEventMeta(), Name: name})
	return w, nil
}

// Reconstitute reconstruye un Widget a partir del estado persistido sin lanzar eventos.
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

// Name devuelve el nombre.
func (w *Widget) Name() string { return w.name }

// Price devuelve el precio en céntimos.
func (w *Widget) Price() int64 { return w.price }

// Active dice si el widget está activo.
func (w *Widget) Active() bool { return w.active }

// Color devuelve el color opcional.
func (w *Widget) Color() *string { return w.color }

// CreatedAt devuelve el instante de creación.
func (w *Widget) CreatedAt() time.Time { return w.createdAt }

// Weight devuelve el peso (decimal exacto).
func (w *Widget) Weight() vocab.Decimal { return w.weight }

// Launch devuelve la fecha de lanzamiento (fecha civil).
func (w *Widget) Launch() vocab.Date { return w.launch }

// Parts devuelve las piezas.
func (w *Widget) Parts() []Part { return slices.Clone(w.parts) }

// Rename cambia el nombre y lanza WidgetRenamed.
func (w *Widget) Rename(name string) error {
	if name == "" {
		return domain.Violation("widget.name_required", "name is required")
	}
	old := w.name
	w.name = name
	w.Raise(WidgetRenamed{EventMeta: w.NewEventMeta(), From: old, To: name})
	return nil
}

// SetPrice cambia el precio.
func (w *Widget) SetPrice(p int64) { w.price = p }

// ReplaceParts sustituye la colección de piezas.
func (w *Widget) ReplaceParts(parts ...Part) { w.parts = slices.Clone(parts) }

// Clone devuelve una copia profunda (para la opción de aislamiento del repositorio en memoria).
func (w *Widget) Clone() *Widget {
	c := *w
	c.parts = slices.Clone(w.parts)
	if w.color != nil {
		col := *w.color
		c.color = &col
	}
	return &c
}

// Campos tipados de Widget que usan las especificaciones. Los adaptadores traducen los nombres
// lógicos al almacenamiento.
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

// PremiumName es el nombre de la especificación propia Premium.
const PremiumName = "repotest.premium"

// Premium es una especificación propia: widgets activos con precio de al menos minPrice.
// Cada adaptador debe dar su propia traducción de PremiumName.
func Premium(minPrice int64) spec.Spec[*Widget] {
	return spec.Custom(PremiumName, func(w *Widget) bool { return w.active && w.price >= minPrice }, minPrice)
}

// Unknown es una especificación propia que ningún adaptador traduce; los almacenes que no
// pueden evaluarla de forma nativa deben fallar con domain.ErrUnsupported.
func Unknown() spec.Spec[*Widget] {
	return spec.Custom("repotest.unknown", func(*Widget) bool { return true })
}
