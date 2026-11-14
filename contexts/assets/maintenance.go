package assets

// El contrato de mantenimiento de los activos: las rutas, las formas y los errores que lee el motor
// de Angular de Karpo (el contrato `crud` del lenguaje de Theros, docs/ACTIVOS.md «El contrato de
// mantenimiento»). Está escrito a mano sobre los casos de uso de siempre, que no cambian: es otra
// puerta al mismo registro de inmovilizado, bajo /api/v1/assets.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	uapp "github.com/jhermoso/karpo-fw-go/contexts/assets/application"
	"github.com/jhermoso/karpo-fw-go/contexts/assets/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// MaintenanceBase es la ruta base de los activos en el contrato de mantenimiento.
const MaintenanceBase = "/api/v1/assets"

// maxMaintenanceBody es lo más que se lee de un cuerpo (1 MiB): uno mayor es 413.
const maxMaintenanceBody = 1 << 20

// CodeDisposalRequiresData es el código del 409 de un DELETE sobre un activo en servicio: darlo de
// baja exige una fecha, venta o desecho y lo obtenido, que un DELETE no lleva.
const CodeDisposalRequiresData = "assets.disposal_requires_data"

// MaintainedAsset es un activo como viaja por el contrato. Los importes calculados (cuota, acumulado,
// valor neto, estado, resultado) sólo se leen: en un cambio se ignoran.
type MaintainedAsset struct {
	ID           string `json:"id"`
	Company      string `json:"company"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	Class        string `json:"class"`
	Serial       string `json:"serial"`
	Location     string `json:"location"`
	Supplier     string `json:"supplier"`
	Document     string `json:"document"`
	Acquired     string `json:"acquired"`
	InService    string `json:"inService"`
	Cost         string `json:"cost"`
	Residual     string `json:"residual"`
	LifeMonths   int    `json:"lifeMonths"`
	Monthly      string `json:"monthly"`
	Accumulated  string `json:"accumulated"`
	NetBookValue string `json:"netBookValue"`
	Status       string `json:"status"`
	Disposed     string `json:"disposed"`
	Disposal     string `json:"disposal"`
	Proceeds     string `json:"proceeds"`
	Result       string `json:"result"`
	Version      int64  `json:"version"`
}

func maintained(a uapp.AssetDTO) MaintainedAsset {
	return MaintainedAsset{ID: a.ID, Company: a.Company, Code: a.Code, Name: a.Name, Class: a.Class, Serial: a.Serial, Location: a.Location,
		Supplier: a.Supplier, Document: a.Document, Acquired: a.Acquired, InService: a.InService, Cost: a.Cost, Residual: a.Residual,
		LifeMonths: a.LifeMonths, Monthly: a.Monthly, Accumulated: a.Accumulated, NetBookValue: a.NetBookValue, Status: a.Status,
		Disposed: a.Disposed, Disposal: a.Disposal, Proceeds: a.Proceeds, Result: a.Result, Version: a.Version}
}

// maintainedInput es el cuerpo de un alta o de un cambio. Lo que sobra se ignora (un front devuelve
// en el cambio lo que leyó); lo que falta es un valor vacío. Version es obligatoria en el cambio.
type maintainedInput struct {
	Company    string `json:"company"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	Class      string `json:"class"`
	Serial     string `json:"serial"`
	Location   string `json:"location"`
	Supplier   string `json:"supplier"`
	Document   string `json:"document"`
	Acquired   string `json:"acquired"`
	InService  string `json:"inService"`
	Cost       string `json:"cost"`
	Residual   string `json:"residual"`
	LifeMonths int    `json:"lifeMonths"`
	Disposed   string `json:"disposed"`
	Disposal   string `json:"disposal"`
	Proceeds   string `json:"proceeds"`
	Version    *int64 `json:"version"`
}

// maintenancePage es la página con los nombres que lee el motor (total y totalCount valen lo mismo).
type maintenancePage struct {
	Items      []MaintainedAsset `json:"items"`
	Total      int64             `json:"total"`
	TotalCount int64             `json:"totalCount"`
	PageNumber int               `json:"pageNumber"`
	PageSize   int               `json:"pageSize"`
	TotalPages int               `json:"totalPages"`
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

// parseDate interpreta una fecha opcional (vacía es la fecha cero).
func parseDate(v *fw.Validation, field, s string) vocab.Date {
	if strings.TrimSpace(s) == "" {
		return vocab.Date{}
	}
	d, err := vocab.ParseDate(strings.TrimSpace(s))
	v.Require(err == nil, field, "format", field+" must be a date (YYYY-MM-DD)")
	return d
}

// listQuery es una petición de la lista: la página, el orden y los filtros, por cualquiera de las
// dos rutas.
type listQuery struct {
	page, size    int
	sortBy, order string
	filters       map[string]string
}

func (q listQuery) search() (uapp.SearchAssets, error) {
	var v fw.Validation
	v.Require(q.page >= 1 && q.page <= 1_000_000, "page", "range", "a page from 1 to 1000000")
	v.Require(q.size >= 1 && q.size <= fw.MaxPageSize, "pageSize", "range", "a page size from 1 to 1000")
	v.Require(q.sortBy == "" || q.sortBy == "code" || q.sortBy == "name", "sortBy", "enum", "code or name")
	v.Require(q.order == "" || q.order == "asc" || q.order == "desc", "sortOrder", "enum", "asc or desc")
	if err := v.Err(); err != nil {
		return uapp.SearchAssets{}, err
	}
	// Los filtros de la lista; los demás se ignoran. La búsqueda global y el nombre buscan en el
	// nombre; inService=true (o isActive=true) deja sólo los que no están dados de baja. La empresa
	// sólo estrecha el ámbito de quien llama: nunca enseña otra organización.
	text := q.filters["search"]
	if text == "" {
		text = q.filters["name"]
	}
	return uapp.SearchAssets{Company: q.filters["company"], Class: q.filters["class"], Text: text,
		InService: q.filters["inService"] == "true" || q.filters["isActive"] == "true",
		Sort:      q.sortBy, Desc: q.order == "desc", Page: q.page, Size: q.size}, nil
}

// RegisterMaintenanceRoutes monta el contrato de mantenimiento de los activos bajo MaintenanceBase.
func (m *Module) RegisterMaintenanceRoutes(mux *http.ServeMux) {
	svc := m.Service
	parseID := func(w http.ResponseWriter, r *http.Request) (domain.AssetID, bool) {
		id, err := domain.ParseAssetID(r.PathValue("id"))
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
		page, err := svc.SearchAssets.Handle(r.Context(), in)
		if err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		out := maintenancePage{Items: make([]MaintainedAsset, 0, len(page.Items)), Total: page.Total, TotalCount: page.Total,
			PageNumber: page.Number, PageSize: page.Size, TotalPages: page.TotalPages}
		for _, a := range page.Items {
			out.Items = append(out.Items, maintained(a))
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
		a, err := svc.GetAsset.Handle(r.Context(), uapp.GetAsset{ID: id})
		respondMaintenance(w, r, maintained(a), err, http.StatusOK)
	})
	mux.HandleFunc("POST "+MaintenanceBase, func(w http.ResponseWriter, r *http.Request) {
		var in maintainedInput
		if err := readBody(w, r, &in); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		var v fw.Validation
		acquired, inService := parseDate(&v, "acquired", in.Acquired), parseDate(&v, "inService", in.InService)
		// Un activo entra en el registro en servicio: darlo de alta ya dado de baja no es un alta.
		v.Require(in.Disposed == "" && in.Disposal == "", "disposed", "unsupported", "an asset is registered in service; dispose of it afterwards")
		if err := v.Err(); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		a, err := svc.Register.Handle(r.Context(), uapp.RegisterAsset{Company: in.Company, Code: in.Code, Name: in.Name, Class: in.Class,
			Serial: in.Serial, Location: in.Location, Supplier: in.Supplier, Document: in.Document, Acquired: acquired, InService: inService,
			Cost: in.Cost, Residual: in.Residual, LifeMonths: in.LifeMonths})
		if err == nil {
			w.Header().Set("Location", MaintenanceBase+"/"+a.ID)
		}
		respondMaintenance(w, r, maintained(a), err, http.StatusCreated)
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
		var a uapp.AssetDTO
		err := m.UoW.Do(r.Context(), func(ctx context.Context) (err error) {
			a, err = m.change(ctx, id, body)
			return err
		})
		respondMaintenance(w, r, maintained(a), err, http.StatusOK)
	})
	// Un activo no se borra: sale del registro dándolo de baja, y la baja exige una fecha, venta o
	// desecho y lo obtenido, que un DELETE no lleva. Así, DELETE sobre uno ya dado de baja es 204 (y
	// se puede repetir), y sobre uno en servicio es 409 con CodeDisposalRequiresData. En los dos
	// casos exige el permiso y el ámbito de escritura de la baja.
	mux.HandleFunc("DELETE "+MaintenanceBase+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r)
		if !ok {
			return
		}
		disposed, err := svc.Disposed.Handle(r.Context(), uapp.GetAsset{ID: id})
		if err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		if !disposed {
			writeProblem(w, maintenanceProblem{Title: "Conflict", Status: http.StatusConflict, Instance: r.URL.Path, Code: CodeDisposalRequiresData,
				Detail: "an asset in service is disposed of with a date, sale or scrap and its proceeds: send them with PUT (disposed, disposal, proceeds)"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// sameID compara dos identidades sin distinguir mayúsculas.
func sameID(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }

// sameText compara dos textos sin los espacios de los extremos y sin distinguir mayúsculas.
func sameText(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }

// sameDecimal compara dos importes por su valor ("12000" y "12000.00" son el mismo); vacío es cero.
func sameDecimal(a, b string) bool {
	parse := func(s string) (vocab.Decimal, bool) {
		if strings.TrimSpace(s) == "" {
			return vocab.DecimalFromInt(0), true
		}
		d, err := vocab.ParseDecimal(strings.TrimSpace(s))
		return d, err == nil
	}
	x, okA := parse(a)
	y, okB := parse(b)
	return okA && okB && vocab.CompareDecimal(x, y) == 0
}

// change aplica un PUT: mezcla el cuerpo sobre lo que hay y llama sólo a los casos de uso de lo que
// cambia (cambiar los datos descriptivos y, después, dar de baja), cada uno con la versión que dejó
// el anterior. Corre dentro de la unidad de trabajo de quien llama.
func (m *Module) change(ctx context.Context, id domain.AssetID, body map[string]json.RawMessage) (uapp.AssetDTO, error) {
	svc := m.Service
	// Cambiar exige el permiso y el ámbito de escritura aunque el cambio no cambie nada.
	if _, err := svc.Changeable.Handle(ctx, uapp.GetAsset{ID: id}); err != nil {
		return uapp.AssetDTO{}, err
	}
	// Lo que el cuerpo no trae, o trae como null, se queda como está: el modal de la rejilla del
	// motor manda sólo los campos de su formulario.
	current, err := svc.GetAsset.Handle(ctx, uapp.GetAsset{ID: id})
	if err != nil {
		return uapp.AssetDTO{}, err
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
		return uapp.AssetDTO{}, fieldError("body", "json", "the body is not a JSON object of the expected shape")
	}
	if in.Version == nil {
		return uapp.AssetDTO{}, fieldError("version", "required", "the version that was read is required")
	}
	if *in.Version != current.Version {
		return uapp.AssetDTO{}, fw.Conflict(domain.AssetKind, id, *in.Version, "the asset changed after it was read: read it again")
	}
	// Lo que ningún caso de uso cambia no se puede cambiar por aquí: coste, valor residual y vida
	// útil son con lo que se calcularon los cargos hechos. Se compara por su valor: identidades,
	// código y clase sin distinguir mayúsculas, importes como números.
	var v fw.Validation
	immutable := func(same bool, field string) {
		v.Require(same, field, "immutable", "the "+field+" does not change after registration")
	}
	immutable(sameID(in.Company, was.Company), "company")
	immutable(strings.TrimSpace(in.Code) == strings.TrimSpace(was.Code), "code")
	immutable(sameText(in.Class, was.Class), "class")
	immutable(sameID(in.Supplier, was.Supplier), "supplier")
	immutable(strings.TrimSpace(in.Document) == strings.TrimSpace(was.Document), "document")
	sameDate := func(s, wasText, field string) bool {
		d := parseDate(&v, field, s)
		w, _ := vocab.ParseDate(wasText)
		return d == w
	}
	immutable(sameDate(in.Acquired, was.Acquired, "acquired"), "acquired")
	immutable(sameDate(in.InService, was.InService, "inService"), "inService")
	immutable(sameDecimal(in.Cost, was.Cost), "cost")
	immutable(sameDecimal(in.Residual, was.Residual), "residual")
	immutable(in.LifeMonths == was.LifeMonths, "lifeMonths")
	disposing := was.Disposed == "" && (strings.TrimSpace(in.Disposed) != "" || strings.TrimSpace(in.Disposal) != "")
	if was.Disposed != "" {
		// Una baja no se deshace ni se rehace.
		immutable(sameDate(in.Disposed, was.Disposed, "disposed"), "disposed")
		immutable(sameText(in.Disposal, was.Disposal), "disposal")
		immutable(sameDecimal(in.Proceeds, was.Proceeds), "proceeds")
	}
	if was.Disposed == "" && !disposing && !sameDecimal(in.Proceeds, "") {
		v.Add("proceeds", "required", "proceeds come with a disposal date and kind")
	}
	disposedOn := parseDate(&v, "disposed", in.Disposed)
	if err := v.Err(); err != nil {
		return uapp.AssetDTO{}, err
	}
	// Con la versión, cada caso de uso falla sin reintentar si alguien se ha adelantado.
	a := current
	steps := []struct {
		when bool
		run  func(version int64) (uapp.AssetDTO, error)
	}{
		{strings.TrimSpace(in.Name) != strings.TrimSpace(was.Name) || strings.TrimSpace(in.Serial) != strings.TrimSpace(was.Serial) || !sameID(in.Location, was.Location), func(ver int64) (uapp.AssetDTO, error) {
			return svc.Change.Handle(ctx, uapp.ChangeAsset{ID: id, Name: strings.TrimSpace(in.Name), Serial: strings.TrimSpace(in.Serial), Location: in.Location, Version: ver})
		}},
		{disposing, func(ver int64) (uapp.AssetDTO, error) {
			return svc.Dispose.Handle(ctx, uapp.DisposeAsset{ID: id, Date: disposedOn, Kind: in.Disposal, Proceeds: in.Proceeds, Version: ver})
		}},
	}
	for _, s := range steps {
		if !s.when {
			continue
		}
		if a, err = s.run(a.Version); err != nil {
			return uapp.AssetDTO{}, err
		}
	}
	return a, nil
}
