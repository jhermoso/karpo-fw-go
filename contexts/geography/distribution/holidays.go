package distribution

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// HolidaysModule serves the calendar of holidays.
type HolidaysModule struct{ holidays *gapp.Holidays }

// NewHolidaysModule builds the routes of the calendar.
func NewHolidaysModule(h *gapp.Holidays) *HolidaysModule { return &HolidaysModule{holidays: h} }

var _ distribution.EndpointModule = (*HolidaysModule)(nil)

// RegisterRoutes implements distribution.EndpointModule.
func (m *HolidaysModule) RegisterRoutes(mux *http.ServeMux) {
	h := m.holidays
	mux.HandleFunc("GET /api/geography/holidays", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		year, _ := strconv.Atoi(q.Get("year"))
		out, err := h.Search(r.Context(), gapp.SearchHolidays{Boundary: q.Get("boundary"), Year: year, Inherited: q.Get("inherited") == "true"})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/geography/holidays", func(w http.ResponseWriter, r *http.Request) {
		var c gapp.DeclareHolidays
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			var v fw.Validation
			v.Add("body", "json", err.Error())
			distribution.WriteError(w, r, v.Err())
			return
		}
		out, err := h.Declare(r.Context(), c)
		distribution.Respond(w, r, out, err, http.StatusCreated)
	})
	mux.HandleFunc("DELETE /api/geography/holidays/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := domain.ParseHolidayID(r.PathValue("id"))
		if err != nil {
			distribution.WriteError(w, r, fmt.Errorf("%w: invalid id", fw.ErrValidation))
			return
		}
		distribution.Respond(w, r, struct{}{}, h.Remove(r.Context(), gapp.RemoveHoliday{ID: id}), http.StatusOK)
	})
}
