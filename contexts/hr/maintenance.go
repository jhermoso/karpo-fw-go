package hr

// El contrato de mantenimiento de los centros de trabajo: las rutas, las formas y los errores que lee
// el motor de Angular de Karpo (el contrato `crud` del lenguaje de Theros, docs/RRHH.md «El contrato
// de mantenimiento»). Está escrito a mano sobre los casos de uso de los centros de trabajo: es otra
// puerta a los mismos centros, bajo /api/v1/work-centers.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	happ "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	"github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// MaintenanceBase es la ruta base de los centros de trabajo en el contrato de mantenimiento.
const MaintenanceBase = "/api/v1/work-centers"

// maxMaintenanceBody es lo más que se lee de un cuerpo (1 MiB): uno mayor es 413.
const maxMaintenanceBody = 1 << 20

// MaintainedWorkCenter es un centro de trabajo como viaja por el contrato.
type MaintainedWorkCenter struct {
	ID           string `json:"id"`
	Employer     string `json:"employer"`
	Facility     string `json:"facility"`
	Code         string `json:"code"`
	Headquarters bool   `json:"headquarters"`
	Opened       string `json:"opened"`
	Closed       string `json:"closed"`
	// Active dice si sigue abierto (DELETE lo cierra).
	Active  bool  `json:"active"`
	Version int64 `json:"version"`
}

func maintained(w happ.WorkCenterDTO) MaintainedWorkCenter {
	return MaintainedWorkCenter{ID: w.ID, Employer: w.Employer, Facility: w.Facility, Code: w.Code, Headquarters: w.Headquarters,
		Opened: w.Opened, Closed: w.Closed, Active: w.Closed == "", Version: w.Version}
}

// maintainedInput es el cuerpo de un alta o de un cambio. Lo que sobra se ignora (un front devuelve
// en el cambio lo que leyó, también active); lo que falta es un valor vacío. Version es obligatoria
// en el cambio.
type maintainedInput struct {
	Employer     string `json:"employer"`
	Facility     string `json:"facility"`
	Code         string `json:"code"`
	Headquarters bool   `json:"headquarters"`
	Opened       string `json:"opened"`
	Closed       string `json:"closed"`
	Version      *int64 `json:"version"`
}

// maintenancePage es la página con los nombres que lee el motor (total y totalCount valen lo mismo).
type maintenancePage struct {
	Items      []MaintainedWorkCenter `json:"items"`
	Total      int64                  `json:"total"`
	TotalCount int64                  `json:"totalCount"`
	PageNumber int                    `json:"pageNumber"`
	PageSize   int                    `json:"pageSize"`
	TotalPages int                    `json:"totalPages"`
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

func writeProblem(w http.ResponseWriter, p maintenanceProblem) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

func writeMaintenanceError(w http.ResponseWriter, r *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeProblem(w, maintenanceProblem{Title: "Payload Too Large", Status: http.StatusRequestEntityTooLarge, Instance: r.URL.Path})
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
	writeProblem(w, out)
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

func (q listQuery) search() (happ.SearchWorkCenters, error) {
	var v fw.Validation
	v.Require(q.page >= 1 && q.page <= 1_000_000, "page", "range", "a page from 1 to 1000000")
	v.Require(q.size >= 1 && q.size <= fw.MaxPageSize, "pageSize", "range", "a page size from 1 to 1000")
	v.Require(q.sortBy == "" || q.sortBy == "code", "sortBy", "enum", "code")
	v.Require(q.order == "" || q.order == "asc" || q.order == "desc", "sortOrder", "enum", "asc or desc")
	if err := v.Err(); err != nil {
		return happ.SearchWorkCenters{}, err
	}
	// Los filtros de la lista; los demás se ignoran. isActive=true deja sólo los abiertos. El
	// empleador (también como company, el filtro de empresa del motor) sólo estrecha el ámbito de
	// quien llama: nunca enseña otra organización.
	employer := q.filters["employer"]
	if employer == "" {
		employer = q.filters["company"]
	}
	return happ.SearchWorkCenters{IDs: q.ids, ByIDs: q.byIDs, Employer: employer, OpenOnly: q.filters["isActive"] == "true", Desc: q.order == "desc",
		Page: q.page, Size: q.size}, nil
}

// RegisterMaintenanceRoutes monta el contrato de mantenimiento de los centros de trabajo bajo
// MaintenanceBase.
func (m *Module) RegisterMaintenanceRoutes(mux *http.ServeMux) {
	svc := m.Service
	parseID := func(w http.ResponseWriter, r *http.Request) (domain.WorkCenterID, bool) {
		id, err := domain.ParseWorkCenterID(r.PathValue("id"))
		if err != nil {
			writeMaintenanceError(w, r, fieldError("id", "format", "the identity is not valid"))
			return id, false
		}
		return id, true
	}
	list := func(w http.ResponseWriter, r *http.Request, q listQuery) {
		in, err := q.search()
		if err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		page, err := svc.SearchWorkCenters.Handle(r.Context(), in)
		if err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		out := maintenancePage{Items: make([]MaintainedWorkCenter, 0, len(page.Items)), Total: page.Total, TotalCount: page.Total,
			PageNumber: page.Number, PageSize: page.Size, TotalPages: page.TotalPages}
		for _, c := range page.Items {
			out.Items = append(out.Items, maintained(c))
		}
		distribution.WriteJSON(w, http.StatusOK, out)
	}
	mux.HandleFunc("GET "+MaintenanceBase, func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("POST "+MaintenanceBase+"/search", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("GET "+MaintenanceBase+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r)
		if !ok {
			return
		}
		c, err := svc.GetWorkCenter.Handle(r.Context(), happ.GetWorkCenter{ID: id})
		respondMaintenance(w, r, maintained(c), err, http.StatusOK)
	})
	mux.HandleFunc("POST "+MaintenanceBase, func(w http.ResponseWriter, r *http.Request) {
		var in maintainedInput
		if err := readBody(w, r, &in); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		// Un centro de trabajo nace abierto: darlo de alta cerrado no es un alta, y se rechaza.
		if strings.TrimSpace(in.Closed) != "" {
			writeMaintenanceError(w, r, fieldError("closed", "unsupported", "a work center is opened; close it afterwards"))
			return
		}
		opened, err := parseDate("opened", in.Opened)
		if err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		c, err := svc.OpenWorkCenter.Handle(r.Context(), happ.OpenWorkCenter{Employer: strings.TrimSpace(in.Employer),
			Facility: strings.TrimSpace(in.Facility), Code: in.Code, Headquarters: in.Headquarters, Opened: opened})
		if err == nil {
			w.Header().Set("Location", MaintenanceBase+"/"+c.ID)
		}
		respondMaintenance(w, r, maintained(c), err, http.StatusCreated)
	})
	mux.HandleFunc("PUT "+MaintenanceBase+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r)
		if !ok {
			return
		}
		var body map[string]json.RawMessage
		if err := readBody(w, r, &body); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		// Todo el cambio es una sola unidad de trabajo: si un paso falla, no queda hecho ninguno.
		var c happ.WorkCenterDTO
		err := m.UoW.Do(r.Context(), func(ctx context.Context) (err error) {
			c, err = m.change(ctx, id, body)
			return err
		})
		respondMaintenance(w, r, maintained(c), err, http.StatusOK)
	})
	// Un centro de trabajo no se borra (los contratos lo citan): DELETE lo cierra, que es su baja en
	// el dominio. Cierra hoy (el hoy del empleador) o en la fecha de ?on=; 422
	// hr.work_center_in_use si tiene contratos en vigor después. Sobre uno ya cerrado es 204 sin
	// versión nueva, así que se puede repetir; siempre exige el permiso y el ámbito de escritura.
	mux.HandleFunc("DELETE "+MaintenanceBase+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r)
		if !ok {
			return
		}
		on, err := parseDate("on", r.URL.Query().Get("on"))
		if err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		if _, err := svc.CloseWorkCenter.Handle(r.Context(), happ.CloseWorkCenter{ID: id, On: on}); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// parseDate lee una fecha AAAA-MM-DD; vacía es la fecha cero.
func parseDate(field, s string) (vocab.Date, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return vocab.Date{}, nil
	}
	d, err := vocab.ParseDate(s)
	if err != nil {
		return vocab.Date{}, fieldError(field, "format", "a date as YYYY-MM-DD")
	}
	return d, nil
}

// sameID compara dos identidades sin distinguir mayúsculas.
func sameID(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }

// sameDate compara dos fechas por su valor; si alguna no es una fecha, por su texto.
func sameDate(a, b string) bool {
	x, errA := parseDate("", a)
	y, errB := parseDate("", b)
	if errA != nil || errB != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return x == y
}

// change aplica un PUT: mezcla el cuerpo sobre lo que hay y llama sólo a los casos de uso de lo que
// cambia, cada uno con la versión que dejó el anterior. Corre dentro de la unidad de trabajo de
// quien llama. Sólo cambian dos cosas: la sede (se hace sede; dejar de serlo es hacer sede a otro) y
// el cierre (se cierra en una fecha; un centro cerrado no se reabre).
func (m *Module) change(ctx context.Context, id domain.WorkCenterID, body map[string]json.RawMessage) (happ.WorkCenterDTO, error) {
	svc := m.Service
	// Cambiar exige el permiso y el ámbito de escritura aunque el cambio no cambie nada.
	if _, err := svc.WorkCenterChangeable.Handle(ctx, happ.GetWorkCenter{ID: id}); err != nil {
		return happ.WorkCenterDTO{}, err
	}
	// Lo que el cuerpo no trae, o trae como null, se queda como está: el modal de la rejilla del
	// motor manda sólo los campos de su formulario.
	current, err := svc.GetWorkCenter.Handle(ctx, happ.GetWorkCenter{ID: id})
	if err != nil {
		return happ.WorkCenterDTO{}, err
	}
	was := maintained(current)
	merged := map[string]json.RawMessage{}
	raw, _ := json.Marshal(was)
	_ = json.Unmarshal(raw, &merged)
	delete(merged, "version") // la versión la dice quien llama, o falta
	for k, v := range body {
		if strings.TrimSpace(string(v)) == "null" {
			continue
		}
		merged[k] = v
	}
	raw, _ = json.Marshal(merged)
	var in maintainedInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return happ.WorkCenterDTO{}, fieldError("body", "json", "the body is not a JSON object of the expected shape")
	}
	if in.Version == nil {
		return happ.WorkCenterDTO{}, fieldError("version", "required", "the version that was read is required")
	}
	if *in.Version != current.Version {
		return happ.WorkCenterDTO{}, fw.Conflict(domain.WorkCenterKind, id, *in.Version, "the work center changed after it was read: read it again")
	}
	// Lo que ningún caso de uso cambia no se puede cambiar por aquí. Se compara por su valor: las
	// identidades sin distinguir mayúsculas, el código como lo guarda el dominio y las fechas como
	// fechas.
	var v fw.Validation
	v.Require(sameID(in.Employer, was.Employer), "employer", "immutable", "the employer does not change after opening")
	v.Require(sameID(in.Facility, was.Facility), "facility", "immutable", "the facility does not change after opening")
	v.Require(strings.ToUpper(strings.TrimSpace(in.Code)) == was.Code, "code", "immutable", "the code does not change after opening")
	v.Require(sameDate(in.Opened, was.Opened), "opened", "immutable", "the opening date does not change")
	v.Require(!was.Headquarters || in.Headquarters, "headquarters", "unsupported", "make another work center the headquarters")
	v.Require(was.Closed == "" || sameDate(in.Closed, was.Closed), "closed", "immutable", "a closed work center does not reopen")
	if err := v.Err(); err != nil {
		return happ.WorkCenterDTO{}, err
	}
	closed, err := parseDate("closed", in.Closed)
	if err != nil {
		return happ.WorkCenterDTO{}, err
	}
	// Con la versión, cada caso de uso falla sin reintentar si alguien se ha adelantado.
	c := current
	if in.Headquarters && !was.Headquarters {
		if c, err = svc.SetHeadquarters.Handle(ctx, happ.SetHeadquarters{ID: id, Version: c.Version}); err != nil {
			return happ.WorkCenterDTO{}, err
		}
	}
	if was.Closed == "" && !closed.IsZero() {
		if c, err = svc.CloseWorkCenter.Handle(ctx, happ.CloseWorkCenter{ID: id, On: closed, Version: c.Version}); err != nil {
			return happ.WorkCenterDTO{}, err
		}
	}
	return c, nil
}
