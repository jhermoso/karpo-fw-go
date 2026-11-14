package facilities

// El contrato de mantenimiento de las instalaciones: las rutas, las formas y los errores que lee el
// motor de Angular de Karpo (el contrato `crud` del lenguaje de Theros, docs/INSTALACIONES.md «El
// contrato de mantenimiento»). Está escrito a mano sobre los casos de uso de siempre, que no cambian:
// es otra puerta a las mismas instalaciones, bajo /api/v1/facilities.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	fapp "github.com/jhermoso/karpo-fw-go/contexts/facilities/application"
	"github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// MaintenanceBase es la ruta base de las instalaciones en el contrato de mantenimiento.
const MaintenanceBase = "/api/v1/facilities"

// maxMaintenanceBody es lo más que se lee de un cuerpo (1 MiB): uno mayor es 413.
const maxMaintenanceBody = 1 << 20

// MaintainedFacility es una instalación como viaja por el contrato.
type MaintainedFacility struct {
	ID           string           `json:"id"`
	Organization string           `json:"organization"`
	Type         string           `json:"type"`
	TypeName     string           `json:"typeName"`
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	PartOf       string           `json:"partOf"`
	AreaM2       string           `json:"areaM2"`
	Location     fapp.LocationDTO `json:"location"`
	// Active dice si está en uso (DELETE la desactiva).
	Active  bool  `json:"active"`
	Version int64 `json:"version"`
}

func maintained(f fapp.FacilityDTO) MaintainedFacility {
	return MaintainedFacility{ID: f.ID, Organization: f.Organization, Type: f.Type, TypeName: f.TypeName, Name: f.Name,
		Description: f.Description, PartOf: f.PartOf, AreaM2: f.AreaM2, Location: f.Location, Active: f.Active, Version: f.Version}
}

// maintainedInput es el cuerpo de un alta o de un cambio. Lo que sobra se ignora (un front devuelve
// en el cambio lo que leyó); lo que falta es un valor vacío. Version es obligatoria en el cambio.
type maintainedInput struct {
	Organization string           `json:"organization"`
	Type         string           `json:"type"`
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	PartOf       string           `json:"partOf"`
	AreaM2       string           `json:"areaM2"`
	Location     fapp.LocationDTO `json:"location"`
	Active       *bool            `json:"active"`
	Version      *int64           `json:"version"`
}

// maintenancePage es la página con los nombres que lee el motor (total y totalCount valen lo mismo).
type maintenancePage struct {
	Items      []MaintainedFacility `json:"items"`
	Total      int64                `json:"total"`
	TotalCount int64                `json:"totalCount"`
	PageNumber int                  `json:"pageNumber"`
	PageSize   int                  `json:"pageSize"`
	TotalPages int                  `json:"totalPages"`
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
}

func (q listQuery) search() (fapp.SearchFacilities, error) {
	var v fw.Validation
	v.Require(q.page >= 1 && q.page <= 1_000_000, "page", "range", "a page from 1 to 1000000")
	v.Require(q.size >= 1 && q.size <= fw.MaxPageSize, "pageSize", "range", "a page size from 1 to 1000")
	v.Require(q.sortBy == "" || q.sortBy == "name", "sortBy", "enum", "name")
	v.Require(q.order == "" || q.order == "asc" || q.order == "desc", "sortOrder", "enum", "asc or desc")
	if err := v.Err(); err != nil {
		return fapp.SearchFacilities{}, err
	}
	// Los filtros de la lista; los demás se ignoran. La búsqueda global y el nombre buscan en el
	// nombre; isActive=true deja sólo las activas (las que DELETE no ha dado de baja).
	text := q.filters["search"]
	if text == "" {
		text = q.filters["name"]
	}
	return fapp.SearchFacilities{Text: text, Type: q.filters["type"], Organization: q.filters["organization"], PartOf: q.filters["partOf"],
		ActiveOnly: q.filters["isActive"] == "true", Sort: q.sortBy, Desc: q.order == "desc", Page: q.page, Size: q.size}, nil
}

// RegisterMaintenanceRoutes monta el contrato de mantenimiento de las instalaciones bajo MaintenanceBase.
func (m *Module) RegisterMaintenanceRoutes(mux *http.ServeMux) {
	svc := m.Service
	parseID := func(w http.ResponseWriter, r *http.Request) (domain.FacilityID, bool) {
		id, err := domain.ParseFacilityID(r.PathValue("id"))
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
		page, err := svc.Search.Handle(r.Context(), in)
		if err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		out := maintenancePage{Items: make([]MaintainedFacility, 0, len(page.Items)), Total: page.Total, TotalCount: page.Total,
			PageNumber: page.Number, PageSize: page.Size, TotalPages: page.TotalPages}
		for _, f := range page.Items {
			out.Items = append(out.Items, maintained(f))
		}
		distribution.WriteJSON(w, http.StatusOK, out)
	}
	mux.HandleFunc("GET "+MaintenanceBase, func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		q := listQuery{page: 1, size: fw.DefaultPageSize, sortBy: values.Get("sortBy"), order: values.Get("sortOrder"), filters: map[string]string{}}
		for k := range values {
			q.filters[k] = values.Get(k)
		}
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
		q := listQuery{page: 1, size: body.Pagination.PageSize, sortBy: body.Pagination.OrderBy,
			filters: map[string]string{}}
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
		f, err := svc.Get.Handle(r.Context(), fapp.GetFacility{ID: id})
		respondMaintenance(w, r, maintained(f), err, http.StatusOK)
	})
	mux.HandleFunc("POST "+MaintenanceBase, func(w http.ResponseWriter, r *http.Request) {
		var in maintainedInput
		if err := readBody(w, r, &in); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		// Una instalación nace activa: darla de alta inactiva no es un alta, y se rechaza.
		if in.Active != nil && !*in.Active {
			writeMaintenanceError(w, r, fieldError("active", "unsupported", "a facility is registered active; deactivate it afterwards"))
			return
		}
		var loc *fapp.LocationDTO
		if in.Location != (fapp.LocationDTO{}) {
			loc = &in.Location
		}
		f, err := svc.Register.Handle(r.Context(), fapp.RegisterFacility{Organization: in.Organization, Type: in.Type, Name: in.Name,
			Description: in.Description, PartOf: in.PartOf, AreaM2: in.AreaM2, Location: loc})
		if err == nil {
			w.Header().Set("Location", MaintenanceBase+"/"+f.ID)
		}
		respondMaintenance(w, r, maintained(f), err, http.StatusCreated)
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
		var f fapp.FacilityDTO
		err := m.UoW.Do(r.Context(), func(ctx context.Context) (err error) {
			f, err = m.change(ctx, id, body)
			return err
		})
		respondMaintenance(w, r, maintained(f), err, http.StatusOK)
	})
	// Dar de baja una instalación es desactivarla: no se borra, porque otros contextos la citan.
	// Desactivar la que ya lo está no cambia nada, pero exige el mismo permiso.
	mux.HandleFunc("DELETE "+MaintenanceBase+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r)
		if !ok {
			return
		}
		if _, err := svc.SetActive.Handle(r.Context(), fapp.SetFacilityActive{ID: id, Active: false}); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// sameID compara dos identidades sin distinguir mayúsculas.
func sameID(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }

// sameDecimal compara dos cantidades por su valor ("12000.5" y "12000.50" son la misma); vacío es cero.
func sameDecimal(a, b string) bool {
	parse := func(s string) (vocab.Decimal, bool) {
		if strings.TrimSpace(s) == "" {
			return vocab.DecimalFromInt(0), true
		}
		d, err := vocab.ParseDecimal(s)
		return d, err == nil
	}
	x, okA := parse(a)
	y, okB := parse(b)
	return okA && okB && vocab.CompareDecimal(x, y) == 0
}

// change aplica un PUT: mezcla el cuerpo sobre lo que hay y llama sólo a los casos de uso de los
// campos que cambian, cada uno con la versión que dejó el anterior. Corre dentro de la unidad de
// trabajo de quien llama.
func (m *Module) change(ctx context.Context, id domain.FacilityID, body map[string]json.RawMessage) (fapp.FacilityDTO, error) {
	svc := m.Service
	// Lo que el cuerpo no trae se queda como está: el modal de la rejilla del motor manda sólo
	// los campos de su formulario. La ubicación viaja entera: si viene, sustituye a la que había;
	// si viene vacía o null, se queda la que había (como en el alta, vacía es «no viene»).
	current, err := svc.Get.Handle(ctx, fapp.GetFacility{ID: id})
	if err != nil {
		return fapp.FacilityDTO{}, err
	}
	was := maintained(current)
	merged := map[string]json.RawMessage{}
	raw, _ := json.Marshal(was)
	_ = json.Unmarshal(raw, &merged)
	delete(merged, "version") // la versión la dice quien llama, o falta
	for k, v := range body {
		merged[k] = v
	}
	raw, _ = json.Marshal(merged)
	var in maintainedInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return fapp.FacilityDTO{}, fieldError("body", "json", "the body is not a JSON object of the expected shape")
	}
	if in.Version == nil {
		return fapp.FacilityDTO{}, fieldError("version", "required", "the version that was read is required")
	}
	if *in.Version != current.Version {
		return fapp.FacilityDTO{}, fw.Conflict(domain.FacilityKind, id, *in.Version, "the facility changed after it was read: read it again")
	}
	if in.Location == (fapp.LocationDTO{}) {
		in.Location = was.Location
	}
	active := was.Active
	if in.Active != nil {
		active = *in.Active
	}
	// Lo que ningún caso de uso cambia no se puede cambiar por aquí. Se compara por su valor: las
	// identidades sin distinguir mayúsculas y la superficie como número.
	var v fw.Validation
	v.Require(sameID(in.Organization, was.Organization), "organization", "immutable", "the organization does not change after registration")
	v.Require(sameID(in.Type, was.Type), "type", "immutable", "the type does not change after registration")
	v.Require(strings.TrimSpace(in.Description) == strings.TrimSpace(was.Description), "description", "immutable",
		"the description does not change after registration")
	v.Require(sameDecimal(in.AreaM2, was.AreaM2), "areaM2", "immutable", "the area does not change after registration")
	if err := v.Err(); err != nil {
		return fapp.FacilityDTO{}, err
	}
	// Una instalación inactiva no admite cambios: se activa antes y se desactiva después. Con la
	// versión, cada caso de uso falla sin reintentar si alguien se ha adelantado.
	f := current
	steps := []struct {
		when bool
		run  func(version int64) (fapp.FacilityDTO, error)
	}{
		{active && !was.Active, func(v int64) (fapp.FacilityDTO, error) {
			return svc.SetActive.Handle(ctx, fapp.SetFacilityActive{ID: id, Active: true, Version: v})
		}},
		{in.Name != was.Name, func(v int64) (fapp.FacilityDTO, error) {
			return svc.Rename.Handle(ctx, fapp.RenameFacility{ID: id, Name: in.Name, Version: v})
		}},
		{in.Location != was.Location, func(v int64) (fapp.FacilityDTO, error) {
			return svc.Relocate.Handle(ctx, fapp.RelocateFacility{ID: id, Location: in.Location, Version: v})
		}},
		{!sameID(in.PartOf, was.PartOf), func(v int64) (fapp.FacilityDTO, error) {
			return svc.Move.Handle(ctx, fapp.MoveFacility{ID: id, PartOf: in.PartOf, Version: v})
		}},
		{!active && was.Active, func(v int64) (fapp.FacilityDTO, error) {
			return svc.SetActive.Handle(ctx, fapp.SetFacilityActive{ID: id, Active: false, Version: v})
		}},
	}
	for _, s := range steps {
		if !s.when {
			continue
		}
		if f, err = s.run(f.Version); err != nil {
			return fapp.FacilityDTO{}, err
		}
	}
	return f, nil
}
