package domain

import (
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// HolidayKind is the aggregate type name of a holiday.
const HolidayKind = "geography.holiday"

// HolidayID identifies a holiday.
type HolidayID struct{ fw.UUID }

// NewHolidayID returns a fresh identity.
func NewHolidayID() HolidayID { return HolidayID{fw.NewUUID()} }

// ParseHolidayID parses a textual identity.
func ParseHolidayID(s string) (HolidayID, error) { u, err := fw.ParseUUID(s); return HolidayID{u}, err }

// HolidayState is the persisted state of a holiday.
type HolidayState struct {
	Boundary BoundaryID // where it is kept: a country, a region, a municipality...
	Date     vocab.Date
	Name     string
	Audit    traits.AuditStamp
}

// Holiday is a day nobody works in a place: a day of a year, in a boundary. It holds for the
// boundary and everything inside it, so a national holiday is declared once, on the country. It
// is the one thing of this context that is kept by hand: holidays are published every year, and
// the local ones town by town. (The C# had no calendar.)
type Holiday struct {
	fw.BaseAggregateRoot[HolidayID]
	traits.Audited
	s HolidayState
}

// ReconstituteHoliday rebuilds a holiday.
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

// State returns the state.
func (h *Holiday) State() HolidayState { return h.s }

// AuditSnapshot implements traits.Snapshotter.
func (h *Holiday) AuditSnapshot() map[string]any {
	return map[string]any{"boundary": h.s.Boundary.String(), "date": h.s.Date.String(), "name": h.s.Name}
}

// Holiday fields.
var (
	HolidayFieldBoundary = spec.Comparable("boundary", func(h *Holiday) BoundaryID { return h.s.Boundary })
	HolidayFieldDate     = spec.OrderedBy("holiday_date", func(h *Holiday) vocab.Date { return h.s.Date }, vocab.CompareDates)
)

// HolidayRepository stores holidays.
type HolidayRepository = fw.Repository[HolidayID, *Holiday]
