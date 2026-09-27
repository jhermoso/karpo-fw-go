// Package distribution exposes the Geography and reference data context over HTTP.
package distribution

import (
	"encoding/json"
	"net/http"
	"strconv"

	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Module is the HTTP endpoint module of the context.
type Module struct{ svc *gapp.Service }

// NewModule creates the endpoint module.
func NewModule(svc *gapp.Service) *Module { return &Module{svc: svc} }

var _ distribution.EndpointModule = (*Module)(nil)

// RegisterRoutes implements distribution.EndpointModule.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/geography/boundaries", m.searchBoundaries)
	mux.HandleFunc("GET /api/geography/boundaries/{id}", m.getBoundary)
	mux.HandleFunc("GET /api/geography/postal-codes", m.lookupPostalCode)
	mux.HandleFunc("POST /api/geography/address-check", m.checkAddress)
	mux.HandleFunc("GET /api/reference/countries", m.countries)
	mux.HandleFunc("GET /api/reference/countries/{alpha2}", m.country)
	mux.HandleFunc("GET /api/reference/{catalog}", m.catalog)
}

func (m *Module) searchBoundaries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
	page, err := m.svc.SearchBoundaries.Handle(r.Context(), gapp.SearchBoundaries{Text: q.Get("q"), Type: q.Get("type"),
		Parent: q.Get("parent"), Grouping: q.Get("grouping"), Page: atoi("page"), Size: atoi("size")})
	distribution.Respond(w, r, page, err, http.StatusOK)
}

func (m *Module) getBoundary(w http.ResponseWriter, r *http.Request) {
	b, err := m.svc.GetBoundary.Handle(r.Context(), gapp.GetBoundary{ID: r.PathValue("id")})
	distribution.Respond(w, r, b, err, http.StatusOK)
}

func (m *Module) lookupPostalCode(w http.ResponseWriter, r *http.Request) {
	out, err := m.svc.LookupPostalCode.Handle(r.Context(), gapp.LookupPostalCode{Country: r.URL.Query().Get("country"), Code: r.URL.Query().Get("code")})
	distribution.Respond(w, r, out, err, http.StatusOK)
}

func (m *Module) checkAddress(w http.ResponseWriter, r *http.Request) {
	var q gapp.CheckPostalAddress
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&q); err != nil {
		var v fw.Validation
		v.Add("body", "json", err.Error())
		distribution.WriteError(w, r, v.Err())
		return
	}
	out, err := m.svc.CheckPostalAddress.Handle(r.Context(), q)
	distribution.Respond(w, r, out, err, http.StatusOK)
}

func (m *Module) countries(w http.ResponseWriter, r *http.Request) {
	out, err := m.svc.ListCountries.Handle(r.Context(), gapp.ListCountries{})
	distribution.Respond(w, r, out, err, http.StatusOK)
}

func (m *Module) country(w http.ResponseWriter, r *http.Request) {
	out, err := m.svc.GetCountry.Handle(r.Context(), gapp.GetCountry{Alpha2: r.PathValue("alpha2")})
	distribution.Respond(w, r, out, err, http.StatusOK)
}

func (m *Module) catalog(w http.ResponseWriter, r *http.Request) {
	out, err := m.svc.ListCatalog.Handle(r.Context(), gapp.ListCatalog{Name: r.PathValue("catalog"), Country: r.URL.Query().Get("country")})
	distribution.Respond(w, r, out, err, http.StatusOK)
}
