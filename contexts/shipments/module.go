// Package shipments composes the Shipments (Envíos) bounded context on a hot-swap backend.
package shipments

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	sapp "github.com/jhermoso/karpo-fw-go/contexts/shipments/application"
	"github.com/jhermoso/karpo-fw-go/contexts/shipments/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/shipments/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/shipments/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service           *sapp.Service
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
	// Consumer receives the delivery notes of Orders to ship: subscribe it to the transport.
	Consumer *messaging.Consumer
}

// Compose builds the context on sw.
func Compose(sw *hotswap.Switch) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	d := sapp.Deps{
		Shipments: hotswap.Repository(sw, infrastructure.ShipmentRepositoryFactory), Carriers: hotswap.Repository(sw, infrastructure.CarrierRepositoryFactory),
		UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			sapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	}
	consumer := messaging.NewConsumer(contracts.Source, hotswap.Inbox(sw, infrastructure.InboxFactory), sw)
	sapp.Subscribe(consumer, d)
	return &Module{Service: sapp.NewService(d), IntegrationOutbox: integration, Audit: audit, Consumer: consumer}
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
		if err := decode(r, &c); err != nil {
			distribution.WriteError(w, r, err)
			return
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
	sid, cid := domain.ParseShipmentID, domain.ParseCarrierID

	mux.HandleFunc("POST /api/shipments", create(http.StatusCreated, svc.Schedule.Handle))
	mux.HandleFunc("GET /api/shipments", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := svc.SearchShipments.Handle(r.Context(), sapp.SearchShipments{Company: q.Get("company"), Customer: q.Get("customer"),
			Status: q.Get("status"), Carrier: q.Get("carrier"), Tracking: q.Get("tracking"), Delivery: q.Get("delivery"), Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/shipments/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := sid(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.GetShipment.Handle(r.Context(), sapp.GetShipment{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("PUT /api/shipments/{id}", command(sid, func(c *sapp.ChangeShipment, id domain.ShipmentID) { c.ID = id }, svc.Change.Handle))
	mux.HandleFunc("POST /api/shipments/{id}/move", command(sid, func(c *sapp.MoveShipment, id domain.ShipmentID) { c.ID = id }, svc.Move.Handle))

	mux.HandleFunc("POST /api/carriers", create(http.StatusCreated, svc.RegisterCarrier.Handle))
	mux.HandleFunc("GET /api/carriers", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.SearchCarriers.Handle(r.Context(), sapp.SearchCarriers{Company: r.URL.Query().Get("company")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("PUT /api/carriers/{id}", command(cid, func(c *sapp.ChangeCarrier, id domain.CarrierID) { c.ID = id }, svc.ChangeCarrier.Handle))
}

var _ distribution.EndpointModule = (*Module)(nil)
