package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permisos del calendario de festivos.
var (
	PermHolidayRead   = authz.MustPermission("Geography.Holiday.Read")
	PermHolidayUpdate = authz.MustPermission("Geography.Holiday.Update")
)

// MaxHolidaysPerCall acota una declaración: los festivos de un lugar para unos pocos años.
const MaxHolidaysPerCall = 200

// Holidays lleva el calendario de festivos y responde si un día lo es en un lugar. Es la única
// parte del contexto en la que se escribe; el resto es un catálogo.
type Holidays struct {
	holidays domain.HolidayRepository
	ports    Ports
	orch     *orchestration.Orchestrator[domain.HolidayID, *domain.Holiday]
}

// NewHolidays construye el calendario.
func NewHolidays(repo domain.HolidayRepository, ports Ports, uow fw.UnitOfWork) *Holidays {
	return &Holidays{holidays: repo, ports: ports, orch: orchestration.New[domain.HolidayID, *domain.Holiday](repo, uow)}
}

var _ contracts.Calendar = (*Holidays)(nil)

// Comandos, consultas y DTO.
type (
	// DayInput es un festivo que declarar.
	DayInput struct {
		Date vocab.Date `json:"date"`
		Name string     `json:"name"`
	}
	// DeclareHolidays declara festivos en un límite. Un día que el límite ya tiene se deja como
	// está, así que el calendario de un año se puede volver a enviar.
	DeclareHolidays struct {
		Boundary string     `json:"boundary"`
		Days     []DayInput `json:"days"`
	}
	// RemoveHoliday quita un festivo declarado por error.
	RemoveHoliday struct{ ID domain.HolidayID }
	// SearchHolidays lista los festivos de un límite en un año: los suyos y, con Inherited, los de
	// los límites que lo contienen.
	SearchHolidays struct {
		Boundary  string
		Year      int
		Inherited bool
	}
	// HolidayDTO es la forma de transporte de un festivo.
	HolidayDTO struct {
		ID           string `json:"id"`
		Boundary     string `json:"boundary"`
		BoundaryName string `json:"boundaryName,omitempty"`
		Date         string `json:"date"`
		Name         string `json:"name"`
	}
)

func require(ctx context.Context, p authz.Permission) error { return authz.Require(ctx, p) }

// boundary interpreta la identidad de un límite que existe.
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

// chain devuelve un límite y los que lo contienen, el más cercano primero, con sus nombres.
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

// Locate encuentra el límite que un calendario nombra por su código, como hacen los calendarios
// oficiales:
//
//	ES      un país, por su código ISO 3166-1
//	ES-MD   una región de un país, por su código ISO 3166-2
//	28      una provincia, por su código del INE
//	28079   un municipio, por su código del INE
//
// Sirve a otros contextos, como IsHoliday.
func (h *Holidays) Locate(ctx context.Context, place string) (contracts.BoundaryRef, error) {
	place = strings.ToUpper(strings.TrimSpace(place))
	unknown := fw.Violation("geography.unknown_place", "no boundary has the code "+place+
		": a country is written ES, a region ES-MD, a province 28 and a municipality 28079")
	digits := place != "" && strings.Trim(place, "0123456789") == ""
	var found []*domain.Boundary
	var err error
	switch {
	case len(place) == 2 && !digits:
		c, cerr := h.ports.Country(ctx, place)
		if cerr != nil {
			return contracts.BoundaryRef{}, unknown
		}
		refs, rerr := h.ports.Resolve(ctx, []string{c.Boundary})
		if ref, ok := refs[c.Boundary]; rerr == nil && ok {
			return ref, nil
		}
		return contracts.BoundaryRef{}, errors.Join(unknown, rerr)
	case len(place) > 3 && place[2] == '-':
		// Las regiones de un país son sus niveles 1 y 2, los llame como los llame cada país.
		found, err = h.ports.Boundaries.Find(ctx, spec.And(domain.FieldCountry.Eq(place[:2]),
			domain.FieldLevel.In(string(domain.Level1), string(domain.Level2)), domain.FieldAbbreviation.Eq(place[3:])))
	case digits && len(place) == 2:
		found, err = h.ports.Boundaries.Find(ctx, spec.And(domain.FieldBoundaryType.Eq(domain.TypeProvince), domain.FieldGeoCode.Eq(place)))
	case digits && len(place) == 5:
		found, err = h.ports.Boundaries.Find(ctx, spec.And(domain.FieldBoundaryType.Eq(domain.TypeMunicipality), domain.FieldGeoCode.Eq(place)))
	}
	if err != nil {
		return contracts.BoundaryRef{}, err
	}
	if len(found) != 1 {
		return contracts.BoundaryRef{}, unknown
	}
	id := found[0].ID().String()
	refs, err := h.ports.Resolve(ctx, []string{id})
	if err != nil {
		return contracts.BoundaryRef{}, err
	}
	return refs[id], nil
}

// Declare declara festivos en un límite y devuelve los que eran nuevos.
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

// Remove quita un festivo.
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

// Search lista los festivos de un límite en un año, en el orden del calendario.
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

// IsHoliday implementa contracts.Calendar: si el día es festivo en el límite o en uno que lo
// contiene. Sirve a otros contextos, no a usuarios: lo que se autoriza es el caso de uso del que
// llama. Un límite que nadie conoce no tiene festivos.
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
