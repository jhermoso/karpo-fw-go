package products

// El contrato de mantenimiento de los productos: las rutas, las formas y los errores que lee el motor
// de Angular de Karpo (el contrato `crud` del lenguaje de Theros, docs/PRODUCTOS.md «El contrato de
// mantenimiento»). Está escrito a mano sobre los casos de uso de siempre, que no cambian: es otra
// puerta a los mismos productos, bajo /api/v1/products.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	papp "github.com/jhermoso/karpo-fw-go/contexts/products/application"
	"github.com/jhermoso/karpo-fw-go/contexts/products/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// MaintenanceBase es la ruta base de los productos en el contrato de mantenimiento.
const MaintenanceBase = "/api/v1/products"

// maxMaintenanceBody es lo más que se lee de un cuerpo (1 MiB): uno mayor es 413.
const maxMaintenanceBody = 1 << 20

// MaintainedProduct es un producto como viaja por el contrato: los datos de la ficha, sin sus partes
// (códigos de barras, componentes y proveedores), que se mantienen por la API de Productos y que un
// cambio por el contrato conserva.
type MaintainedProduct struct {
	ID                    string `json:"id"`
	Company               string `json:"company"`
	SKU                   string `json:"sku"`
	Name                  string `json:"name"`
	Description           string `json:"description"`
	Kind                  string `json:"kind"`
	UoM                   string `json:"uom"`
	Category              string `json:"category"`
	TaxCode               string `json:"taxCode"`
	ExpenseCategory       string `json:"expenseCategory"`
	BasePrice             string `json:"basePrice"`
	StandardCost          string `json:"standardCost"`
	ForSale               bool   `json:"forSale"`
	ForPurchase           bool   `json:"forPurchase"`
	Stocked               bool   `json:"stocked"`
	Tracking              string `json:"tracking"`
	BlockedSales          bool   `json:"blockedSales"`
	BlockedPurchase       bool   `json:"blockedPurchase"`
	ServiceFulfilmentMode string `json:"serviceFulfilmentMode"`
	// Discontinued es el día desde el que está retirado (DELETE lo retira hoy); vacío, no lo está.
	Discontinued string `json:"discontinued"`
	Version      int64  `json:"version"`
}

func maintained(p papp.ProductDTO) MaintainedProduct {
	return MaintainedProduct{ID: p.ID, Company: p.Company, SKU: p.SKU, Name: p.Name, Description: p.Description, Kind: p.Kind, UoM: p.UoM,
		Category: p.Category, TaxCode: p.TaxCode, ExpenseCategory: p.ExpenseCategory, BasePrice: p.BasePrice, StandardCost: p.StandardCost,
		ForSale: p.ForSale, ForPurchase: p.ForPurchase, Stocked: p.Stocked, Tracking: p.Tracking, BlockedSales: p.BlockedSales,
		BlockedPurchase: p.BlockedPurchase, ServiceFulfilmentMode: p.ServiceFulfilmentMode, Discontinued: p.Discontinued, Version: p.Version}
}

// maintainedInput es el cuerpo de un alta o de un cambio. Lo que sobra se ignora (un front devuelve
// en el cambio lo que leyó); lo que falta es un valor vacío. Version es obligatoria en el cambio.
type maintainedInput struct {
	Company         string `json:"company"`
	SKU             string `json:"sku"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Kind            string `json:"kind"`
	UoM             string `json:"uom"`
	Category        string `json:"category"`
	TaxCode         string `json:"taxCode"`
	ExpenseCategory string `json:"expenseCategory"`
	BasePrice       string `json:"basePrice"`
	StandardCost    string `json:"standardCost"`
	ForSale         bool   `json:"forSale"`
	ForPurchase     bool   `json:"forPurchase"`
	Stocked         bool   `json:"stocked"`
	Tracking        string `json:"tracking"`
	BlockedSales    bool   `json:"blockedSales"`
	BlockedPurchase bool   `json:"blockedPurchase"`
	Version         *int64 `json:"version"`
	// ServiceFulfilmentMode es delivery_note o direct_invoice (D-155); vacío, sin ajuste.
	ServiceFulfilmentMode string `json:"serviceFulfilmentMode"`
}

func (in maintainedInput) details() papp.DetailsInput {
	return papp.DetailsInput{Name: in.Name, Description: in.Description, Kind: in.Kind, UoM: in.UoM, Category: in.Category, TaxCode: in.TaxCode,
		ExpenseCategory: in.ExpenseCategory, BasePrice: in.BasePrice, StandardCost: in.StandardCost, ForSale: in.ForSale, ForPurchase: in.ForPurchase,
		Stocked: in.Stocked, Tracking: in.Tracking, BlockedSales: in.BlockedSales, BlockedPurchase: in.BlockedPurchase,
		ServiceFulfilmentMode: in.ServiceFulfilmentMode}
}

// maintenancePage es la página con los nombres que lee el motor (total y totalCount valen lo mismo).
type maintenancePage struct {
	Items      []MaintainedProduct `json:"items"`
	Total      int64               `json:"total"`
	TotalCount int64               `json:"totalCount"`
	PageNumber int                 `json:"pageNumber"`
	PageSize   int                 `json:"pageSize"`
	TotalPages int                 `json:"totalPages"`
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

func (q listQuery) search() (papp.SearchProducts, error) {
	var v fw.Validation
	v.Require(q.page >= 1 && q.page <= 1_000_000, "page", "range", "a page from 1 to 1000000")
	v.Require(q.size >= 1 && q.size <= fw.MaxPageSize, "pageSize", "range", "a page size from 1 to 1000")
	v.Require(q.sortBy == "" || q.sortBy == "sku" || q.sortBy == "name", "sortBy", "enum", "sku or name")
	v.Require(q.order == "" || q.order == "asc" || q.order == "desc", "sortOrder", "enum", "asc or desc")
	if err := v.Err(); err != nil {
		return papp.SearchProducts{}, err
	}
	// Los filtros de la lista; los demás (el motor manda isActive a toda rejilla) se ignoran. La
	// búsqueda global, el nombre y la referencia buscan en el nombre y en la referencia.
	text := q.filters["search"]
	for _, k := range []string{"name", "sku"} {
		if text == "" {
			text = q.filters[k]
		}
	}
	return papp.SearchProducts{Company: q.filters["company"], Text: text, Kind: q.filters["kind"], Category: q.filters["category"],
		Sort: q.sortBy, Desc: q.order == "desc", Page: q.page, Size: q.size}, nil
}

// RegisterMaintenanceRoutes monta el contrato de mantenimiento de los productos bajo MaintenanceBase.
func (m *Module) RegisterMaintenanceRoutes(mux *http.ServeMux) {
	svc := m.Service
	list := func(w http.ResponseWriter, r *http.Request, q listQuery) {
		in, err := q.search()
		if err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		page, err := svc.SearchProducts.Handle(r.Context(), in)
		if err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		out := maintenancePage{Items: make([]MaintainedProduct, 0, len(page.Items)), Total: page.Total, TotalCount: page.Total,
			PageNumber: page.Number, PageSize: page.Size, TotalPages: page.TotalPages}
		for _, p := range page.Items {
			out.Items = append(out.Items, maintained(p))
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
		id, err := domain.ParseProductID(r.PathValue("id"))
		if err != nil {
			writeMaintenanceError(w, r, fieldError("id", "format", "the identity is not valid"))
			return
		}
		p, err := svc.GetProduct.Handle(r.Context(), papp.GetProduct{ID: id})
		respondMaintenance(w, r, maintained(p), err, http.StatusOK)
	})
	mux.HandleFunc("POST "+MaintenanceBase, func(w http.ResponseWriter, r *http.Request) {
		var in maintainedInput
		if err := readBody(w, r, &in); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		p, err := svc.RegisterProduct.Handle(r.Context(), papp.RegisterProduct{Company: in.Company, SKU: in.SKU, DetailsInput: in.details()})
		if err == nil {
			w.Header().Set("Location", MaintenanceBase+"/"+p.ID)
		}
		respondMaintenance(w, r, maintained(p), err, http.StatusCreated)
	})
	mux.HandleFunc("PUT "+MaintenanceBase+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := domain.ParseProductID(r.PathValue("id"))
		if err != nil {
			writeMaintenanceError(w, r, fieldError("id", "format", "the identity is not valid"))
			return
		}
		var body map[string]json.RawMessage
		if err := readBody(w, r, &body); err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		// Lo que el cuerpo no trae se queda como está: el modal de la rejilla del motor manda sólo
		// los campos de su formulario. Las partes no viajan por el contrato: se conservan.
		current, err := svc.GetProduct.Handle(r.Context(), papp.GetProduct{ID: id})
		if err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		merged := map[string]json.RawMessage{}
		raw, _ := json.Marshal(maintained(current))
		_ = json.Unmarshal(raw, &merged)
		delete(merged, "version") // la versión la dice quien llama, o falta
		for k, v := range body {
			merged[k] = v
		}
		raw, _ = json.Marshal(merged)
		var in maintainedInput
		if err := json.Unmarshal(raw, &in); err != nil {
			writeMaintenanceError(w, r, fieldError("body", "json", "the body is not a JSON object of the expected shape"))
			return
		}
		if in.Version == nil {
			writeMaintenanceError(w, r, fieldError("version", "required", "the version that was read is required"))
			return
		}
		if *in.Version < 1 { // ningún producto está en esa versión
			writeMaintenanceError(w, r, fw.Conflict(domain.ProductKind, id, *in.Version, "the product changed after it was read: read it again"))
			return
		}
		d := in.details()
		d.Barcodes, d.Components, d.Suppliers = current.Barcodes, current.Components, current.Suppliers
		p, err := svc.ChangeProduct.Handle(r.Context(), papp.ChangeProduct{ID: id, Version: *in.Version, DetailsInput: d})
		respondMaintenance(w, r, maintained(p), err, http.StatusOK)
	})
	// Dar de baja un producto es retirarlo hoy: no se borra, porque pedidos y facturas lo citan.
	mux.HandleFunc("DELETE "+MaintenanceBase+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := domain.ParseProductID(r.PathValue("id"))
		if err != nil {
			writeMaintenanceError(w, r, fieldError("id", "format", "the identity is not valid"))
			return
		}
		_, err = svc.DiscontinueProduct.Handle(r.Context(), papp.DiscontinueProduct{ID: id})
		var rv *fw.RuleViolationError
		if errors.As(err, &rv) && rv.Code == "products.discontinued" {
			err = nil // ya estaba retirado: repetir la baja no es un error
		}
		if err != nil {
			writeMaintenanceError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
