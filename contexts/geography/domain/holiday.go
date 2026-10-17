package domain

import (
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// HolidayKind es el nombre de tipo de agregado de un festivo.
const HolidayKind = "geography.holiday"

// HolidayID identifica un festivo.
type HolidayID struct{ fw.UUID }

// NewHolidayID devuelve una identidad nueva.
func NewHolidayID() HolidayID { return HolidayID{fw.NewUUID()} }

// ParseHolidayID interpreta una identidad en texto.
func ParseHolidayID(s string) (HolidayID, error) { u, err := fw.ParseUUID(s); return HolidayID{u}, err }

// HolidayState es el estado persistido de un festivo.
type HolidayState struct {
	Boundary BoundaryID // dónde se celebra: un país, una región, un municipio...
	Date     vocab.Date
	Name     string
	Audit    traits.AuditStamp
}

// Holiday es un día en que nadie trabaja en un lugar: un día de un año, en un límite territorial.
// Vale para el límite y todo lo que hay dentro, así que un festivo nacional se declara una vez, en
// el país. Es lo único de este contexto que se mantiene a mano: los festivos se publican cada año,
// y los locales pueblo a pueblo. (El C# no tenía calendario.)
type Holiday struct {
	fw.BaseAggregateRoot[HolidayID]
	traits.Audited
	s HolidayState
}

// ReconstituteHoliday reconstruye un festivo.
func ReconstituteHoliday(id HolidayID, s HolidayState) (*Holiday, error) {
	base, err := fw.NewBaseAggregateRoot(HolidayKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	s.Name = strings.Join(strings.Fields(s.Name), " ")
	v.Require(!s.Boundary.IsZero(), "boundary", "required", "where the holiday is kept")
	v.Require(!s.Date.IsZero(), "date", "required", "the day")
	v.Require(s.Name != "" && utf8.RuneCountInString(s.Name) <= 120, "name", "length", "a name of 1 to 120 characters")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Holiday{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State devuelve el estado.
func (h *Holiday) State() HolidayState { return h.s }

// AuditSnapshot implementa traits.Snapshotter.
func (h *Holiday) AuditSnapshot() map[string]any {
	return map[string]any{"boundary": h.s.Boundary.String(), "date": h.s.Date.String(), "name": h.s.Name}
}

// Campos de los festivos.
var (
	HolidayFieldBoundary = spec.Comparable("boundary", func(h *Holiday) BoundaryID { return h.s.Boundary })
	HolidayFieldDate     = spec.OrderedBy("holiday_date", func(h *Holiday) vocab.Date { return h.s.Date }, vocab.CompareDates)
)

// HolidayRepository guarda los festivos.
type HolidayRepository = fw.Repository[HolidayID, *Holiday]
