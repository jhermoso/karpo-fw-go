// Package exchange composes the Exchange (Cambio de divisas: cotización y reservas) bounded
// context on a hot-swap backend.
package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	xapp "github.com/jhermoso/karpo-fw-go/contexts/exchange/application"
	"github.com/jhermoso/karpo-fw-go/contexts/exchange/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/exchange/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/exchange/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service           *xapp.Service
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
}

// Compose builds the context on sw. collaborators resolves promotion codes (nil: no code can be
// validated).
func Compose(sw *hotswap.Switch, collaborators domain.Collaborators) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	svc := xapp.NewService(xapp.Deps{
		Currencies: hotswap.Repository(sw, infrastructure.CurrencyRepositoryFactory), Margins: hotswap.Repository(sw, infrastructure.MarginRepositoryFactory),
		Settings: hotswap.Repository(sw, infrastructure.SettingsRepositoryFactory), Reservations: hotswap.Repository(sw, infrastructure.ReservationRepositoryFactory),
		Collaborators: collaborators, UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			xapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	})
	return &Module{Service: svc, IntegrationOutbox: integration, Audit: audit}
}

// Relay forwards the Published Language to a transport.
func (m *Module) Relay(sender application.MessageSender, opts ...outbox.RelayOption) *outbox.Relay {
	return messaging.NewRelay(contracts.Source, m.IntegrationOutbox, sender, opts...)
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var val fw.Validation
		val.Add("body", "json", err.Error())
		return val.Err()
	}
	return nil
}

func badID(w http.ResponseWriter, r *http.Request) {
	distribution.WriteError(w, r, fmt.Errorf("%w: invalid id", fw.ErrValidation))
}

func body[In, Out any](status int, run func(context.Context, In) (Out, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var c In
		if err := decode(r, &c); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		out, err := run(r.Context(), c)
		distribution.Respond(w, r, out, err, status)
	}
}

func onReservation[In, Out any](set func(*In, domain.ReservationID), run func(context.Context, In) (Out, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := domain.ParseReservationID(r.PathValue("id"))
		if err != nil || id.IsZero() {
			badID(w, r)
			return
		}
		var c In
		if r.ContentLength != 0 {
			if err := decode(r, &c); err != nil {
				distribution.WriteError(w, r, err)
				return
			}
		}
		set(&c, id)
		out, err := run(r.Context(), c)
		distribution.Respond(w, r, out, err, http.StatusOK)
	}
}

// RegisterRoutes implements distribution.EndpointModule.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	svc := m.Service
	query := func(run func(r *http.Request) (any, error)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			out, err := run(r)
			distribution.Respond(w, r, out, err, http.StatusOK)
		}
	}

	mux.HandleFunc("PUT /api/exchange/currencies", body(http.StatusOK, svc.SetCurrency.Handle))
	mux.HandleFunc("GET /api/exchange/currencies", query(func(r *http.Request) (any, error) {
		return svc.ListCurrencies.Handle(r.Context(), xapp.ListCurrencies{Company: r.URL.Query().Get("company")})
	}))
	mux.HandleFunc("POST /api/exchange/rates", body(http.StatusOK, svc.SetRate.Handle))
	mux.HandleFunc("PUT /api/exchange/margins", body(http.StatusOK, svc.SetMargin.Handle))
	mux.HandleFunc("GET /api/exchange/margins", query(func(r *http.Request) (any, error) {
		return svc.ListMargins.Handle(r.Context(), xapp.ListMargins{Company: r.URL.Query().Get("company")})
	}))
	mux.HandleFunc("GET /api/exchange/settings", query(func(r *http.Request) (any, error) {
		return svc.GetSettings.Handle(r.Context(), xapp.GetSettings{Company: r.URL.Query().Get("company")})
	}))
	mux.HandleFunc("PUT /api/exchange/settings", body(http.StatusOK, svc.SetSettings.Handle))
	mux.HandleFunc("GET /api/exchange/quote", query(func(r *http.Request) (any, error) {
		q := r.URL.Query()
		return svc.Quote.Handle(r.Context(), xapp.GetQuote{Company: q.Get("company"), Currency: q.Get("currency"), Segment: q.Get("segment"), Amount: q.Get("amount")})
	}))

	mux.HandleFunc("POST /api/exchange/reservations", body(http.StatusCreated, svc.Reserve.Handle))
	mux.HandleFunc("POST /api/exchange/reservations/expire-due", body(http.StatusOK, svc.ExpireDue.Handle))
	mux.HandleFunc("GET /api/exchange/reservations", query(func(r *http.Request) (any, error) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		return svc.Search.Handle(r.Context(), xapp.SearchReservations{Company: q.Get("company"), Status: q.Get("status"), Customer: q.Get("customer"),
			Facility: q.Get("facility"), Currency: q.Get("currency"), CreatedFrom: q.Get("createdFrom"), CreatedTo: q.Get("createdTo"),
			PickupFrom: q.Get("pickupFrom"), PickupTo: q.Get("pickupTo"), Page: atoi("page"), Size: atoi("size")})
	}))
	mux.HandleFunc("GET /api/exchange/reservations/by-reference/{reference}", query(func(r *http.Request) (any, error) {
		return svc.Get.Handle(r.Context(), xapp.GetReservation{Reference: r.PathValue("reference")})
	}))
	mux.HandleFunc("GET /api/exchange/reservations/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := domain.ParseReservationID(r.PathValue("id"))
		if err != nil || id.IsZero() {
			badID(w, r)
			return
		}
		out, err := svc.Get.Handle(r.Context(), xapp.GetReservation{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/exchange/reservations/{id}/advance", onReservation(func(c *xapp.AdvanceReservation, id domain.ReservationID) { c.ID = id }, svc.Advance.Handle))
	mux.HandleFunc("POST /api/exchange/reservations/{id}/cancel", onReservation(func(c *xapp.CancelReservation, id domain.ReservationID) { c.ID = id }, svc.Cancel.Handle))
	mux.HandleFunc("GET /api/exchange/dashboard", query(func(r *http.Request) (any, error) {
		q := r.URL.Query()
		return svc.Dashboard.Handle(r.Context(), xapp.GetDashboard{Company: q.Get("company"), From: q.Get("from"), To: q.Get("to")})
	}))
}

var _ distribution.EndpointModule = (*Module)(nil)
