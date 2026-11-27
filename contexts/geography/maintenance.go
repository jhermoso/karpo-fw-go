package geography

// El contrato de mantenimiento de Geografía: las rutas, las formas y los errores que lee el motor de
// Angular de Karpo (el contrato `crud` del lenguaje de Theros, docs/GEOGRAFIA.md «El contrato de
// mantenimiento»). Está escrito a mano sobre los casos de uso de siempre:
//
//   - /api/v1/countries: los países, sólo para leer. Son datos de referencia que llegan con las
//     migraciones (ISO 3166, formatos de IBAN y de código postal...); el alta, el cambio y la baja
//     responden 409 y dicen la vía.
//   - /api/v1/holidays: el calendario de festivos, lo único del contexto que se mantiene a mano.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Rutas base del contrato de mantenimiento.
const (
	CountriesBase = "/api/v1/countries"
	HolidaysBase  = "/api/v1/holidays"
)

// maxMaintenanceBody es lo más que se lee de un cuerpo (1 MiB): uno mayor es 413.
const maxMaintenanceBody = 1 << 20

// MaintainedCountry es un país como viaja por el contrato: su identidad es el código ISO alfa-2.
type MaintainedCountry struct {
	ID string `json:"id"`
	contracts.CountryInfo
}

// MaintainedCountryDetail es un país leído de uno en uno, con todos sus datos.
type MaintainedCountryDetail struct {
	ID string `json:"id"`
	gapp.CountryDTO
}

// holidayInput es el cuerpo de un alta o de un cambio de festivo. Lo que sobra se ignora.
type holidayInput struct {
	Boundary string `json:"boundary"`
	Date     string `json:"date"`
	Name     string `json:"name"`
	Version  *int64 `json:"version"`
}

// maintenancePage es la página con los nombres que lee el motor (total y totalCount valen lo mismo).
type maintenancePage[T any] struct {
	Items      []T   `json:"items"`
	Total      int64 `json:"total"`
	TotalCount int64 `json:"totalCount"`
	PageNumber int   `json:"pageNumber"`
	PageSize   int   `json:"pageSize"`
	TotalPages int   `json:"totalPages"`
}

// maintenanceProblem es el error del contrato: RFC 9457 con los errores de cada campo como
// diccionario (campo → mensajes) y sus códigos en errorCodes, en el mismo orden.
type maintenanceProblem struct {
	Title      string              `json:"title"`
	Status     int                 `json:"status"`
	Detail     string              `json:"detail,omitempty"`
	Instance   string              `json:"instance,omitempty"`
	Code       string              `json:"code,omitempty"`
	Errors     map[string][]string `json:"errors,omitempty"`
	ErrorCodes map[string][]string `json:"errorCodes,omitempty"`
}

func writeMaintenanceError(w http.ResponseWriter, r *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_ = json.NewEncoder(w).Encode(maintenanceProblem{Title: "Payload Too Large", Status: http.StatusRequestEntityTooLarge, Instance: r.URL.Path})
		return
	}
	p := distribution.Problem(r, err)
	distribution.RecordError(r, err)
	out := maintenanceProblem{Title: p.Title, Status: p.Status, Detail: p.Detail, Instance: p.Instance, Code: p.Code}
	for _, fe := range p.Errors {
		if out.Errors == nil {
			out.Errors, out.ErrorCodes = map[string][]string{}, map[string][]string{}
		}
		out.Errors[fe.Field] = append(out.Errors[fe.Field], fe.Message)
		out.ErrorCodes[fe.Field] = append(out.ErrorCodes[fe.Field], fe.Code)
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(out)
}

func respondMaintenance(w http.ResponseWriter, r *http.Request, v any, err error, status int) {
	if err != nil {
		writeMaintenanceError(w, r, err)
		return
	}
	distribution.WriteJSON(w, status, v)
}

// fieldError es un error de validación de un solo campo.
func fieldError(field, code, message string) error {
	var v fw.Validation
	v.Add(field, code, message)
	return v.Err()
}

// readBody lee un cuerpo JSON sin exigir que no sobre nada, con el tope de tamaño.
func readBody(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxMaintenanceBody)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return err
		}
		return fieldError("body", "json", "the body is not a JSON object of the expected shape")
	}
	return nil
}

// listQuery es una petición de la lista: la página, el orden y los filtros, por cualquiera de las
// dos rutas.
type listQuery struct {
	page, size    int
	sortBy, order string
	filters       map[string]string
	ids           []string // el filtro ids, tal como llega (docs/CONTRATO-MANTENIMIENTO.md)
	byIDs         bool
}

// check valida la página y el orden contra la lista blanca de campos.
func (q listQuery) check(sortable ...string) error {
	var v fw.Validation
	v.Require(q.page >= 1 && q.page <= 1_000_000, "page", "range", "a page from 1 to 1000000")
	v.Require(q.size >= 1 && q.size <= fw.MaxPageSize, "pageSize", "range", "a page size from 1 to 1000")
	v.Require(q.sortBy == "" || slices.Contains(sortable, q.sortBy), "sortBy", "enum", strings.Join(sortable, ", "))
	v.Require(q.order == "" || q.order == "asc" || q.order == "desc", "sortOrder", "enum", "asc or desc")
	return v.Err()
}

// text es la búsqueda global o, si no viene, la del nombre.
func (q listQuery) text() string {
	if t := q.filters["search"]; t != "" {
		return t
	}
	return q.filters["name"]
}

// registerList monta la lista por GET y por POST …/search.
func registerList(mux *http.ServeMux, base string, list func(w http.ResponseWriter, r *http.Request, q listQuery)) {
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		q := listQuery{page: 1, size: fw.DefaultPageSize, sortBy: values.Get("sortBy"), order: values.Get("sortOrder"), filters: map[string]string{}}
		for k := range values {
			q.filters[k] = values.Get(k)
		}
		q.ids, q.byIDs = distribution.IDsFromQuery(values)
		delete(q.filters, "ids")
		var err error
		if s := values.Get("page"); s != "" {
			if q.page, err = strconv.Atoi(s); err != nil {
				q.page = 0
			}
		}
		if s := values.Get("pageSize"); s != "" {
			if q.size, err = strconv.Atoi(s); err != nil {
				q.size = 0
			}
		}
		list(w, r, q)
	})
	mux.HandleFunc("POST "+base+"/search", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Filter     map[string]any `json:"filter"`
			Pagination struct {
				PageNumber *int   `json:"pageNumber"`
				PageSize   int    `json:"pageSize"`
				OrderBy    string `json:"orderBy"`
				Ascending  *bool  `json:"ascending"`
			} `json:"pagination"`
		}
		if err := readBody(w, r, &body); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		q := listQuery{page: 1, size: body.Pagination.PageSize, sortBy: body.Pagination.OrderBy, filters: map[string]string{}}
		// Sin número de página se pide la primera; uno fuera de rango da 400, como en GET.
		if body.Pagination.PageNumber != nil {
			q.page = *body.Pagination.PageNumber
		}
		if q.size == 0 {
			q.size = fw.DefaultPageSize
		}
		if body.Pagination.Ascending != nil && !*body.Pagination.Ascending {
			q.order = "desc"
		}
		ids, byIDs, err := distribution.IDsFromFilter(body.Filter)
		if err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		q.ids, q.byIDs = ids, byIDs
		for k, v := range body.Filter {
			switch x := v.(type) {
			case nil:
			case string:
				q.filters[k] = x
			case float64, bool:
				q.filters[k] = fmt.Sprint(x)
			default:
				writeMaintenanceError(w, r, fieldError(k, "type", "a filter is a single value"))
				return
			}
		}
		list(w, r, q)
	})
}

// RegisterMaintenanceRoutes monta el contrato de mantenimiento de los países y los festivos.
func (m *Module) RegisterMaintenanceRoutes(mux *http.ServeMux) {
	m.countryRoutes(mux)
	m.holidayRoutes(mux)
}

// countryRoutes: los países se leen; no se dan de alta, ni se cambian, ni se borran por aquí.
func (m *Module) countryRoutes(mux *http.ServeMux) {
	svc := m.Service
	registerList(mux, CountriesBase, func(w http.ResponseWriter, r *http.Request, q listQuery) {
		if err := q.check("alpha2", "alpha3", "name"); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		var v fw.Validation
		flags := map[string]func(contracts.CountryInfo) bool{
			"eu":       func(c contracts.CountryInfo) bool { return c.EU },
			"eurozone": func(c contracts.CountryInfo) bool { return c.Eurozone },
			"sepa":     func(c contracts.CountryInfo) bool { return c.SEPA },
		}
		for f := range flags {
			s := q.filters[f]
			v.Require(s == "" || s == "true" || s == "false", f, "enum", "true or false")
		}
		// ids filtra por alfa-2: un código bien formado que no existe no es un error, sólo no está.
		var wanted map[string]bool
		if q.byIDs {
			wanted = map[string]bool{}
			for _, c := range app.ParseIDs(&v, q.ids, canonAlpha2) {
				wanted[c] = true
			}
		}
		if err := v.Err(); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		// Los países son unos 250: se leen todos y se filtran, ordenan y paginan aquí.
		all, err := svc.ListCountries.Handle(r.Context(), gapp.ListCountries{})
		if err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		text := strings.ToLower(strings.TrimSpace(q.text()))
		var found []contracts.CountryInfo
		for _, c := range all {
			if q.byIDs && !wanted[c.Alpha2] {
				continue
			}
			if text != "" && !strings.Contains(strings.ToLower(c.Name), text) && !strings.EqualFold(c.Alpha2, text) && !strings.EqualFold(c.Alpha3, text) {
				continue
			}
			keep := true
			for f, get := range flags {
				if s := q.filters[f]; s != "" && get(c) != (s == "true") {
					keep = false
				}
			}
			if keep {
				found = append(found, c)
			}
		}
		key := func(c contracts.CountryInfo) string {
			switch q.sortBy {
			case "alpha3":
				return c.Alpha3
			case "name":
				return strings.ToLower(c.Name)
			}
			return c.Alpha2
		}
		slices.SortStableFunc(found, func(a, b contracts.CountryInfo) int {
			c := strings.Compare(key(a), key(b))
			if q.order == "desc" {
				c = -c
			}
			if c == 0 {
				c = strings.Compare(a.Alpha2, b.Alpha2) // el desempate, por la identidad
			}
			return c
		})
		total := len(found)
		from, to := min((q.page-1)*q.size, total), min(q.page*q.size, total)
		out := maintenancePage[MaintainedCountry]{Items: make([]MaintainedCountry, 0, to-from), Total: int64(total), TotalCount: int64(total),
			PageNumber: q.page, PageSize: q.size, TotalPages: (total + q.size - 1) / q.size}
		for _, c := range found[from:to] {
			out.Items = append(out.Items, MaintainedCountry{ID: c.Alpha2, CountryInfo: c})
		}
		distribution.WriteJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("GET "+CountriesBase+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		c, err := svc.GetCountry.Handle(r.Context(), gapp.GetCountry{Alpha2: r.PathValue("id")})
		respondMaintenance(w, r, MaintainedCountryDetail{ID: c.Alpha2, CountryDTO: c}, err, http.StatusOK)
	})
	// Alta, cambio y baja: 409, con la vía. Antes se exige el permiso de leer los datos de
	// referencia, para que quien no los ve no sepa más de ellos que un 403.
	readOnly := func(w http.ResponseWriter, r *http.Request) {
		if err := authz.Require(r.Context(), gapp.PermReferenceRead); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		writeMaintenanceError(w, r, fw.Conflict(domain.CountryKind, stringID(r.PathValue("id")), 0,
			"countries are reference data (ISO 3166) that come with the schema migrations: they are not registered, changed or deleted here"))
	}
	mux.HandleFunc("POST "+CountriesBase, readOnly)
	mux.HandleFunc("PUT "+CountriesBase+"/{id}", readOnly)
	mux.HandleFunc("DELETE "+CountriesBase+"/{id}", readOnly)
}

// canonAlpha2 acepta exactamente dos letras ASCII y las devuelve en mayúsculas.
func canonAlpha2(s string) (string, error) {
	if len(s) != 2 {
		return "", errors.New("not an alpha-2 code")
	}
	b := []byte(strings.ToUpper(s))
	for _, c := range b {
		if c < 'A' || c > 'Z' {
			return "", errors.New("not an alpha-2 code")
		}
	}
	return string(b), nil
}

type stringID string

func (s stringID) String() string { return string(s) }

// holidayRoutes: el calendario de festivos, de uno en uno.
func (m *Module) holidayRoutes(mux *http.ServeMux) {
	h := m.Holidays
	parseID := func(w http.ResponseWriter, r *http.Request) (domain.HolidayID, bool) {
		id, err := domain.ParseHolidayID(r.PathValue("id"))
		if err != nil {
			writeMaintenanceError(w, r, fieldError("id", "format", "the identity is not valid"))
			return id, false
		}
		return id, true
	}
	registerList(mux, HolidaysBase, func(w http.ResponseWriter, r *http.Request, q listQuery) {
		if err := q.check("date", "name"); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		year := 0
		if s := q.filters["year"]; s != "" {
			var err error
			if year, err = strconv.Atoi(s); err != nil {
				writeMaintenanceError(w, r, fieldError("year", "range", "a year from 1900 to 2200"))
				return
			}
		}
		page, err := h.Page(r.Context(), gapp.PageHolidays{IDs: q.ids, ByIDs: q.byIDs, Boundary: q.filters["boundary"], Text: q.text(), Year: year,
			ByName: q.sortBy == "name", Desc: q.order == "desc", Page: q.page, Size: q.size})
		if err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		items := page.Items
		if items == nil {
			items = []gapp.HolidayDTO{}
		}
		distribution.WriteJSON(w, http.StatusOK, maintenancePage[gapp.HolidayDTO]{Items: items, Total: page.Total, TotalCount: page.Total,
			PageNumber: page.Number, PageSize: page.Size, TotalPages: page.TotalPages})
	})
	mux.HandleFunc("GET "+HolidaysBase+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r)
		if !ok {
			return
		}
		x, err := h.Get(r.Context(), id)
		respondMaintenance(w, r, x, err, http.StatusOK)
	})
	mux.HandleFunc("POST "+HolidaysBase, func(w http.ResponseWriter, r *http.Request) {
		var in holidayInput
		if err := readBody(w, r, &in); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		x, err := h.Register(r.Context(), gapp.RegisterHoliday{Boundary: strings.TrimSpace(in.Boundary), Date: in.Date, Name: in.Name})
		if err == nil {
			w.Header().Set("Location", HolidaysBase+"/"+x.ID)
		}
		respondMaintenance(w, r, x, err, http.StatusCreated)
	})
	mux.HandleFunc("PUT "+HolidaysBase+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r)
		if !ok {
			return
		}
		var body map[string]json.RawMessage
		if err := readBody(w, r, &body); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		var x gapp.HolidayDTO
		err := m.UoW.Do(r.Context(), func(ctx context.Context) (err error) {
			x, err = m.changeHoliday(ctx, id, body)
			return err
		})
		respondMaintenance(w, r, x, err, http.StatusOK)
	})
	// Quitar un festivo es borrarlo: nadie lo cita, y declarado por error no tiene historia que
	// guardar. Quitar uno que ya no está no es un error, pero exige el mismo permiso.
	mux.HandleFunc("DELETE "+HolidaysBase+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r)
		if !ok {
			return
		}
		if err := h.Remove(r.Context(), gapp.RemoveHoliday{ID: id}); err != nil && !errors.Is(err, fw.ErrNotFound) {
			writeMaintenanceError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// changeHoliday aplica un PUT: mezcla el cuerpo sobre lo que hay; el lugar y el día no cambian (se
// comparan por su valor) y el nombre, sí. Corre dentro de la unidad de trabajo de quien llama.
func (m *Module) changeHoliday(ctx context.Context, id domain.HolidayID, body map[string]json.RawMessage) (gapp.HolidayDTO, error) {
	current, err := m.Holidays.Get(ctx, id)
	if err != nil {
		return gapp.HolidayDTO{}, err
	}
	merged := map[string]json.RawMessage{}
	raw, _ := json.Marshal(current)
	_ = json.Unmarshal(raw, &merged)
	delete(merged, "version") // la versión la dice quien llama, o falta
	for k, v := range body {
		merged[k] = v
	}
	raw, _ = json.Marshal(merged)
	var in holidayInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return gapp.HolidayDTO{}, fieldError("body", "json", "the body is not a JSON object of the expected shape")
	}
	if in.Version == nil {
		return gapp.HolidayDTO{}, fieldError("version", "required", "the version that was read is required")
	}
	if *in.Version != current.Version {
		return gapp.HolidayDTO{}, fw.Conflict(domain.HolidayKind, id, *in.Version, "the holiday changed after it was read: read it again")
	}
	sameDay := func(a, b string) bool {
		x, errA := vocab.ParseDate(strings.TrimSpace(a))
		y, errB := vocab.ParseDate(strings.TrimSpace(b))
		return errA == nil && errB == nil && vocab.CompareDates(x, y) == 0
	}
	var v fw.Validation
	v.Require(strings.EqualFold(strings.TrimSpace(in.Boundary), current.Boundary), "boundary", "immutable",
		"the place does not change: remove the holiday and declare it where it belongs")
	v.Require(sameDay(in.Date, current.Date), "date", "immutable", "the day does not change: remove the holiday and declare the right day")
	if err := v.Err(); err != nil {
		return gapp.HolidayDTO{}, err
	}
	return m.Holidays.Rename(ctx, gapp.RenameHoliday{ID: id, Name: in.Name, Version: *in.Version})
}
