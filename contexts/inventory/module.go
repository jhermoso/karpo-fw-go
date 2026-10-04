// Package inventory composes the Inventory (Inventario) bounded context on a hot-swap backend.
package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	iapp "github.com/jhermoso/karpo-fw-go/contexts/inventory/application"
	"github.com/jhermoso/karpo-fw-go/contexts/inventory/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/inventory/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/inventory/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service           *iapp.Service
	Availability      contracts.Availability
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
	// Consumer receives the stock requests, deliveries and closures of Orders: subscribe it to the
	// transport.
	Consumer *messaging.Consumer
}

// Compose builds the context on sw. Inventory needs the catalog of Products, a port it owns.
func Compose(sw *hotswap.Switch, catalog domain.Catalog) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	levels := hotswap.Repository(sw, infrastructure.LevelRepositoryFactory)
	d := iapp.Deps{
		Warehouses: hotswap.Repository(sw, infrastructure.WarehouseRepositoryFactory), Levels: levels,
		Movements: hotswap.Repository(sw, infrastructure.MovementRepositoryFactory), Reservations: hotswap.Repository(sw, infrastructure.ReservationRepositoryFactory),
		Catalog: catalog, UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			iapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	}
	consumer := messaging.NewConsumer(contracts.Source, hotswap.Inbox(sw, infrastructure.InboxFactory), sw)
	iapp.Subscribe(consumer, d)
	return &Module{Service: iapp.NewService(d), Availability: iapp.AvailabilityPort{Levels: levels}, IntegrationOutbox: integration, Audit: audit,
		Consumer: consumer}
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

func command[ID, In, Out any](parse func(string) (ID, error), set func(*In, ID), run func(context.Context, In) (Out, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parse(r.PathValue("id"))
		if err != nil {
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

func create[In, Out any](status int, run func(context.Context, In) (Out, error)) http.HandlerFunc {
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

// RegisterRoutes implements distribution.EndpointModule.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	svc := m.Service
	wh, res := domain.ParseWarehouseID, domain.ParseReservationID

	mux.HandleFunc("POST /api/inventory/warehouses", create(http.StatusCreated, svc.CreateWarehouse.Handle))
	mux.HandleFunc("GET /api/inventory/warehouses", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.SearchWarehouses.Handle(r.Context(), iapp.SearchWarehouses{Company: r.URL.Query().Get("company")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/inventory/warehouses/{id}/close", command(wh, func(c *iapp.CloseWarehouse, id domain.WarehouseID) { c.ID = id }, svc.CloseWarehouse.Handle))

	mux.HandleFunc("POST /api/inventory/receipts", create(http.StatusCreated, svc.Receive.Handle))
	mux.HandleFunc("POST /api/inventory/issues", create(http.StatusCreated, svc.Issue.Handle))
	mux.HandleFunc("POST /api/inventory/counts", create(http.StatusOK, svc.Adjust.Handle))
	mux.HandleFunc("POST /api/inventory/transfers", create(http.StatusCreated, svc.Transfer.Handle))
	mux.HandleFunc("PUT /api/inventory/reorder-points", create(http.StatusOK, svc.SetReorderPoint.Handle))
	mux.HandleFunc("POST /api/inventory/reservations", create(http.StatusCreated, svc.Reserve.Handle))
	mux.HandleFunc("POST /api/inventory/reservations/{id}/release", command(res, func(c *iapp.Release, id domain.ReservationID) { c.ID = id }, svc.Release.Handle))
	mux.HandleFunc("GET /api/inventory/reservations", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := svc.Reservations.Handle(r.Context(), iapp.SearchReservations{Company: q.Get("company"), OpenOnly: q.Get("open") == "true"})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})

	mux.HandleFunc("GET /api/inventory/stock", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := svc.Stock.Handle(r.Context(), iapp.GetStock{Company: q.Get("company"), Warehouse: q.Get("warehouse"), Product: q.Get("product"),
			BelowReorder: q.Get("belowReorder") == "true"})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/inventory/movements", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := svc.Ledger.Handle(r.Context(), iapp.GetLedger{Company: q.Get("company"), Warehouse: q.Get("warehouse"), Product: q.Get("product"),
			From: q.Get("from"), To: q.Get("to"), Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/inventory/valuation", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := svc.Valuation.Handle(r.Context(), iapp.GetValuation{Company: q.Get("company"), Warehouse: q.Get("warehouse")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
}

var _ distribution.EndpointModule = (*Module)(nil)
