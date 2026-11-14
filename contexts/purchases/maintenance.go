package purchases

// El contrato de mantenimiento de los proveedores: las rutas, las formas y los errores que lee el
// motor de Angular de Karpo (el contrato `crud` del lenguaje de Theros, docs/COMPRAS.md «El contrato
// de mantenimiento»). Está escrito a mano sobre los casos de uso de los perfiles de proveedor: es
// otra puerta a los mismos perfiles, bajo /api/v1/suppliers.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	uapp "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	"github.com/jhermoso/karpo-fw-go/contexts/purchases/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// MaintenanceBase es la ruta base de los proveedores en el contrato de mantenimiento.
const MaintenanceBase = "/api/v1/suppliers"

// maxMaintenanceBody es lo más que se lee de un cuerpo (1 MiB): uno mayor es 413.
const maxMaintenanceBody = 1 << 20

// MaintainedSupplier es un perfil de proveedor como viaja por el contrato.
type MaintainedSupplier struct {
	ID              string `json:"id"`
	Company         string `json:"company"`
	Supplier        string `json:"supplier"`
	Category        string `json:"category"`
	WithholdingRate string `json:"withholdingRate"`
	PaymentDays     int    `json:"paymentDays"`
	IBAN            string `json:"iban"`
	Blocked         bool   `json:"blocked"`
	BlockReason     string `json:"blockReason"`
	Version         int64  `json:"version"`
}

func maintained(s uapp.SupplierDTO) MaintainedSupplier {
	return MaintainedSupplier{ID: s.ID, Company: s.Company, Supplier: s.Supplier, Category: s.Category, WithholdingRate: s.WithholdingRate,
		PaymentDays: s.PaymentDays, IBAN: s.IBAN, Blocked: s.Blocked, BlockReason: s.BlockReason, Version: s.Version}
}

// maintainedInput es el cuerpo de un alta o de un cambio. Lo que sobra se ignora (un front devuelve
// en el cambio lo que leyó); lo que falta es un valor vacío. Version es obligatoria en el cambio.
type maintainedInput struct {
	Company         string `json:"company"`
	Supplier        string `json:"supplier"`
	Category        string `json:"category"`
	WithholdingRate string `json:"withholdingRate"`
	PaymentDays     int    `json:"paymentDays"`
	IBAN            string `json:"iban"`
	Blocked         bool   `json:"blocked"`
	BlockReason     string `json:"blockReason"`
	Version         *int64 `json:"version"`
}

// maintenancePage es la página con los nombres que lee el motor (total y totalCount valen lo mismo).
type maintenancePage struct {
	Items      []MaintainedSupplier `json:"items"`
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
}

func (q listQuery) search() (uapp.PageSuppliers, error) {
	var v fw.Validation
	v.Require(q.page >= 1 && q.page <= 1_000_000, "page", "range", "a page from 1 to 1000000")
	v.Require(q.size >= 1 && q.size <= fw.MaxPageSize, "pageSize", "range", "a page size from 1 to 1000")
	v.Require(q.sortBy == "" || q.sortBy == "paymentDays", "sortBy", "enum", "paymentDays")
	v.Require(q.order == "" || q.order == "asc" || q.order == "desc", "sortOrder", "enum", "asc or desc")
	if err := v.Err(); err != nil {
		return uapp.PageSuppliers{}, err
	}
	// Los filtros de la lista; los demás (también la búsqueda de texto: el nombre del proveedor es de
	// Parties) se ignoran. isActive=true deja sólo los que no están bloqueados. La empresa sólo
	// estrecha el ámbito de quien llama: nunca enseña otra organización.
	return uapp.PageSuppliers{Company: q.filters["company"], Supplier: q.filters["supplier"], Category: q.filters["category"],
		ActiveOnly: q.filters["isActive"] == "true", IncludeMergedParties: q.filters["includeMergedParties"] == "true",
		Sort: q.sortBy, Desc: q.order == "desc", Page: q.page, Size: q.size}, nil
}

// RegisterMaintenanceRoutes monta el contrato de mantenimiento de los proveedores bajo
// MaintenanceBase.
func (m *Module) RegisterMaintenanceRoutes(mux *http.ServeMux) {
	svc := m.Service
	parseID := func(w http.ResponseWriter, r *http.Request) (domain.SupplierID, bool) {
		id, err := domain.ParseSupplierID(r.PathValue("id"))
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
		page, err := svc.PageSuppliers.Handle(r.Context(), in)
		if err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		out := maintenancePage{Items: make([]MaintainedSupplier, 0, len(page.Items)), Total: page.Total, TotalCount: page.Total,
			PageNumber: page.Number, PageSize: page.Size, TotalPages: page.TotalPages}
		for _, s := range page.Items {
			out.Items = append(out.Items, maintained(s))
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
		s, err := svc.GetSupplier.Handle(r.Context(), uapp.GetSupplier{ID: id})
		respondMaintenance(w, r, maintained(s), err, http.StatusOK)
	})
	mux.HandleFunc("POST "+MaintenanceBase, func(w http.ResponseWriter, r *http.Request) {
		var in maintainedInput
		if err := readBody(w, r, &in); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		s, err := svc.RegisterSupplier.Handle(r.Context(), uapp.SetSupplier{Company: strings.TrimSpace(in.Company), Supplier: strings.TrimSpace(in.Supplier),
			Category: strings.TrimSpace(in.Category), WithholdingRate: strings.TrimSpace(in.WithholdingRate), PaymentDays: in.PaymentDays,
			IBAN: strings.TrimSpace(in.IBAN), Blocked: in.Blocked, BlockReason: strings.TrimSpace(in.BlockReason)})
		if err == nil {
			w.Header().Set("Location", MaintenanceBase+"/"+s.ID)
		}
		respondMaintenance(w, r, maintained(s), err, http.StatusCreated)
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
		var s uapp.SupplierDTO
		err := m.UoW.Do(r.Context(), func(ctx context.Context) (err error) {
			s, err = m.change(ctx, id, body)
			return err
		})
		respondMaintenance(w, r, maintained(s), err, http.StatusOK)
	})
	// Un perfil de proveedor no se borra (las facturas registradas nombran al proveedor y el perfil
	// guarda sus valores por defecto): DELETE lo bloquea, que es su baja en el dominio; a un proveedor
	// bloqueado no se le registran facturas nuevas. Sobre uno ya bloqueado es 204 sin versión nueva,
	// así que se puede repetir. Siempre exige el permiso y el ámbito de escritura.
	mux.HandleFunc("DELETE "+MaintenanceBase+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r)
		if !ok {
			return
		}
		err := m.UoW.Do(r.Context(), func(ctx context.Context) error {
			if _, err := svc.SupplierChangeable.Handle(ctx, uapp.GetSupplier{ID: id}); err != nil {
				return err
			}
			s, err := svc.GetSupplier.Handle(ctx, uapp.GetSupplier{ID: id})
			if err != nil || s.Blocked {
				return err
			}
			_, err = svc.ChangeSupplier.Handle(ctx, uapp.ChangeSupplier{ID: id, Category: s.Category, WithholdingRate: s.WithholdingRate,
				PaymentDays: s.PaymentDays, IBAN: s.IBAN, Blocked: true, Version: s.Version})
			return err
		})
		if err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// sameID compara dos identidades sin distinguir mayúsculas.
func sameID(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }

// sameRate compara dos retenciones por su valor («15» y «15.00» son la misma); si alguna no es un
// número, por su texto.
func sameRate(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" {
		a = "0"
	}
	if b == "" {
		b = "0"
	}
	x, errA := vocab.ParseDecimal(a)
	y, errB := vocab.ParseDecimal(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return x.Equal(y)
}

// sameIBAN compara dos cuentas sin los espacios ni las mayúsculas con que se escriban.
func sameIBAN(a, b string) bool {
	norm := func(s string) string { return strings.ToUpper(strings.Join(strings.Fields(s), "")) }
	return norm(a) == norm(b)
}

// change aplica un PUT: mezcla el cuerpo sobre lo que hay y, si algo cambia, llama al caso de uso de
// siempre con la versión leída. Corre dentro de la unidad de trabajo de quien llama.
func (m *Module) change(ctx context.Context, id domain.SupplierID, body map[string]json.RawMessage) (uapp.SupplierDTO, error) {
	svc := m.Service
	// Cambiar exige el permiso y el ámbito de escritura aunque el cambio no cambie nada.
	if _, err := svc.SupplierChangeable.Handle(ctx, uapp.GetSupplier{ID: id}); err != nil {
		return uapp.SupplierDTO{}, err
	}
	// Lo que el cuerpo no trae, o trae como null, se queda como está: el modal de la rejilla del
	// motor manda sólo los campos de su formulario.
	current, err := svc.GetSupplier.Handle(ctx, uapp.GetSupplier{ID: id})
	if err != nil {
		return uapp.SupplierDTO{}, err
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
		return uapp.SupplierDTO{}, fieldError("body", "json", "the body is not a JSON object of the expected shape")
	}
	if in.Version == nil {
		return uapp.SupplierDTO{}, fieldError("version", "required", "the version that was read is required")
	}
	if *in.Version != current.Version {
		return uapp.SupplierDTO{}, fw.Conflict(domain.SupplierKind, id, *in.Version, "the supplier changed after it was read: read it again")
	}
	// Lo que ningún caso de uso cambia no se puede cambiar por aquí: la empresa y el proveedor. Se
	// comparan por su valor: la identidad sin distinguir mayúsculas ni los espacios de los extremos.
	var v fw.Validation
	v.Require(sameID(in.Company, was.Company), "company", "immutable", "the company does not change after registration")
	v.Require(sameID(in.Supplier, was.Supplier), "supplier", "immutable", "the supplier does not change after registration")
	if err := v.Err(); err != nil {
		return uapp.SupplierDTO{}, err
	}
	// Sin bloqueo no hay motivo: el dominio lo borra al desbloquear.
	reason := strings.TrimSpace(in.BlockReason)
	if !in.Blocked {
		reason = ""
	}
	if strings.TrimSpace(in.Category) == was.Category && sameRate(in.WithholdingRate, was.WithholdingRate) && in.PaymentDays == was.PaymentDays &&
		sameIBAN(in.IBAN, was.IBAN) && in.Blocked == was.Blocked && reason == was.BlockReason {
		return current, nil
	}
	// Con la versión, el caso de uso falla sin reintentar si alguien se ha adelantado.
	return svc.ChangeSupplier.Handle(ctx, uapp.ChangeSupplier{ID: id, Category: strings.TrimSpace(in.Category),
		WithholdingRate: strings.TrimSpace(in.WithholdingRate), PaymentDays: in.PaymentDays, IBAN: strings.TrimSpace(in.IBAN), Blocked: in.Blocked,
		BlockReason: reason, Version: current.Version})
}
