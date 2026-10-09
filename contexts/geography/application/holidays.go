package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permissions of the calendar of holidays.
var (
	PermHolidayRead   = authz.MustPermission("Geography.Holiday.Read")
	PermHolidayUpdate = authz.MustPermission("Geography.Holiday.Update")
)

// MaxHolidaysPerCall bounds a declaration: the holidays of a place for a few years.
const MaxHolidaysPerCall = 200

// Holidays keeps the calendar of holidays and answers whether a day is one in a place. It is the
// one part of the context that is written to; the rest is a catalog.
type Holidays struct {
	holidays domain.HolidayRepository
	ports    Ports
	orch     *orchestration.Orchestrator[domain.HolidayID, *domain.Holiday]
}

// NewHolidays builds the calendar.
func NewHolidays(repo domain.HolidayRepository, ports Ports, uow fw.UnitOfWork) *Holidays {
	return &Holidays{holidays: repo, ports: ports, orch: orchestration.New[domain.HolidayID, *domain.Holiday](repo, uow)}
}

var _ contracts.Calendar = (*Holidays)(nil)

// Commands, queries and DTOs.
type (
	// DayInput is a holiday to declare.
	DayInput struct {
		Date vocab.Date `json:"date"`
		Name string     `json:"name"`
	}
	// DeclareHolidays declares holidays in a boundary. A day the boundary already has is left as
	// it is, so the calendar of a year can be sent again.
	DeclareHolidays struct {
		Boundary string     `json:"boundary"`
		Days     []DayInput `json:"days"`
	}
	// RemoveHoliday removes a holiday declared by mistake.
	RemoveHoliday struct{ ID domain.HolidayID }
	// SearchHolidays lists the holidays of a boundary in a year: its own and, with Inherited,
	// those of the boundaries that contain it.
	SearchHolidays struct {
		Boundary  string
		Year      int
		Inherited bool
	}
	// HolidayDTO is the transport form of a holiday.
	HolidayDTO struct {
		ID           string `json:"id"`
		Boundary     string `json:"boundary"`
		BoundaryName string `json:"boundaryName,omitempty"`
		Date         string `json:"date"`
		Name         string `json:"name"`
	}
)

func require(ctx context.Context, p authz.Permission) error { return authz.Require(ctx, p) }

// boundary parses the identity of a boundary that exists.
func (h *Holidays) boundary(ctx context.Context, id string) (domain.BoundaryID, error) {
	bid, err := domain.ParseBoundaryID(id)
	if err != nil || bid.IsZero() {
		var v fw.Validation
		v.Add("boundary", "format", "the identity of a boundary")
		return domain.BoundaryID{}, v.Err()
	}
	if _, err := h.ports.Boundaries.Get(ctx, bid); err != nil {
		return domain.BoundaryID{}, err
	}
	return bid, nil
}

// chain returns a boundary and those that contain it, nearest first, with their names.
func (h *Holidays) chain(ctx context.Context, id domain.BoundaryID) ([]domain.BoundaryID, map[string]string, error) {
	up, err := h.ports.Ancestors(ctx, id.String())
	if err != nil {
		return nil, nil, err
	}
	ids, names := []domain.BoundaryID{id}, map[string]string{}
	for _, a := range up {
		if aid, err := domain.ParseBoundaryID(a.ID); err == nil {
			ids = append(ids, aid)
			names[a.ID] = a.Name
		}
	}
	return ids, names, nil
}

// Declare declares holidays in a boundary and returns those that were new.
func (h *Holidays) Declare(ctx context.Context, c DeclareHolidays) ([]HolidayDTO, error) {
	if err := require(ctx, PermHolidayUpdate); err != nil {
		return nil, err
	}
	bid, err := h.boundary(ctx, c.Boundary)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(len(c.Days) > 0 && len(c.Days) <= MaxHolidaysPerCall, "days", "length", "1 to "+strconv.Itoa(MaxHolidaysPerCall)+" days")
	if err := v.Err(); err != nil {
		return nil, err
	}
	out := []HolidayDTO{}
	seen := map[vocab.Date]bool{}
	for _, d := range c.Days {
		hol, err := domain.ReconstituteHoliday(domain.NewHolidayID(), domain.HolidayState{Boundary: bid, Date: d.Date, Name: d.Name})
		if err != nil {
			return nil, err
		}
		if seen[d.Date] {
			continue
		}
		seen[d.Date] = true
		known, err := h.holidays.Exists(ctx, spec.And(domain.HolidayFieldBoundary.Eq(bid), domain.HolidayFieldDate.Eq(d.Date)))
		if err != nil {
			return nil, err
		}
		if known {
			continue
		}
		if err := h.orch.Create(ctx, hol); err != nil {
			return nil, err
		}
		out = append(out, holidayDTO(hol, nil))
	}
	return out, nil
}

// Remove removes a holiday.
func (h *Holidays) Remove(ctx context.Context, c RemoveHoliday) error {
	if err := require(ctx, PermHolidayUpdate); err != nil {
		return err
	}
	return h.orch.Delete(ctx, c.ID, func(context.Context, *domain.Holiday) error { return nil })
}

func holidayDTO(h *domain.Holiday, names map[string]string) HolidayDTO {
	s := h.State()
	return HolidayDTO{ID: h.ID().String(), Boundary: s.Boundary.String(), BoundaryName: names[s.Boundary.String()], Date: s.Date.String(), Name: s.Name}
}

// Search lists the holidays of a boundary in a year, in the order of the calendar.
func (h *Holidays) Search(ctx context.Context, q SearchHolidays) ([]HolidayDTO, error) {
	if err := require(ctx, PermHolidayRead); err != nil {
		return nil, err
	}
	bid, err := h.boundary(ctx, q.Boundary)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(q.Year >= 1900 && q.Year <= 2200, "year", "range", "a year")
	if err := v.Err(); err != nil {
		return nil, err
	}
	ids, names := []domain.BoundaryID{bid}, map[string]string{}
	if q.Inherited {
		if ids, names, err = h.chain(ctx, bid); err != nil {
			return nil, err
		}
	}
	found, err := h.holidays.Find(ctx, spec.And(domain.HolidayFieldBoundary.In(ids...),
		domain.HolidayFieldDate.Between(vocab.MustDate(q.Year, 1, 1), vocab.MustDate(q.Year, 12, 31))))
	if err != nil {
		return nil, err
	}
	slices.SortFunc(found, func(a, b *domain.Holiday) int {
		if c := vocab.CompareDates(a.State().Date, b.State().Date); c != 0 {
			return c
		}
		return slices.Index(ids, a.State().Boundary) - slices.Index(ids, b.State().Boundary)
	})
	out := make([]HolidayDTO, 0, len(found))
	for _, f := range found {
		out = append(out, holidayDTO(f, names))
	}
	return out, nil
}

// IsHoliday implements contracts.Calendar: whether the day is a holiday in the boundary or in
// one that contains it. It serves other contexts, not users: the caller's own use case is what
// is authorized. A boundary nobody knows has no holidays.
func (h *Holidays) IsHoliday(ctx context.Context, boundary, day string) (bool, error) {
	date, err := vocab.ParseDate(day)
	if err != nil {
		return false, fmt.Errorf("%w: a day is written YYYY-MM-DD", fw.ErrValidation)
	}
	bid, err := domain.ParseBoundaryID(boundary)
	if err != nil || bid.IsZero() {
		return false, nil
	}
	ids, _, err := h.chain(ctx, bid)
	if errors.Is(err, fw.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return h.holidays.Exists(ctx, spec.And(domain.HolidayFieldBoundary.In(ids...), domain.HolidayFieldDate.Eq(date)))
}
