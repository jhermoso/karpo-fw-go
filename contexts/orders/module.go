// Package orders composes the Orders (Pedidos) bounded context on a hot-swap backend.
package orders

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	oapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	"github.com/jhermoso/karpo-fw-go/contexts/orders/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/orders/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service           *oapp.Service
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
	// Consumer receives the stock Inventory holds for the orders: subscribe it to the transport.
	Consumer *messaging.Consumer
}

// Compose builds the context on sw. Orders needs the catalog and prices of Products and, when
// credit is controlled, the exposure of Receivables (nil: no credit control): ports it owns.
func Compose(sw *hotswap.Switch, catalog domain.Catalog, credit domain.CreditCheck) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	d := oapp.Deps{
		Orders: hotswap.Repository(sw, infrastructure.OrderRepositoryFactory), Deliveries: hotswap.Repository(sw, infrastructure.DeliveryRepositoryFactory),
		Terms: hotswap.Repository(sw, infrastructure.TermsRepositoryFactory), Counters: hotswap.Repository(sw, infrastructure.CounterRepositoryFactory),
		Catalog: catalog, Credit: credit, UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			oapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	}
	consumer := messaging.NewConsumer(contracts.Source, hotswap.Inbox(sw, infrastructure.InboxFactory), sw)
	oapp.Subscribe(consumer, d)
	return &Module{Service: oapp.NewService(d), IntegrationOutbox: integration, Audit: audit, Consumer: consumer}
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

func command[ID, In, Out any](status int, parse func(string) (ID, error), set func(*In, ID), run func(context.Context, In) (Out, error)) http.HandlerFunc {
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
		distribution.Respond(w, r, out, err, status)
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
	ord := domain.ParseOrderID
	query := func(r *http.Request) (func(string) string, func(string) int) {
		q := r.URL.Query()
		return q.Get, func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
	}

	mux.HandleFunc("PUT /api/orders/terms", create(http.StatusOK, svc.SetTerms.Handle))
	mux.HandleFunc("GET /api/orders/terms", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.SearchTerms.Handle(r.Context(), oapp.SearchTerms{Company: r.URL.Query().Get("company")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})

	mux.HandleFunc("POST /api/orders/orders", create(http.StatusCreated, svc.DraftOrder.Handle))
	mux.HandleFunc("GET /api/orders/orders", func(w http.ResponseWriter, r *http.Request) {
		s, n := query(r)
		out, err := svc.SearchOrders.Handle(r.Context(), oapp.SearchOrders{Company: s("company"), Customer: s("customer"), Status: s("status"),
			Page: n("page"), Size: n("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/orders/orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := ord(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.GetOrder.Handle(r.Context(), oapp.GetOrder{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	ok := http.StatusOK
	mux.HandleFunc("POST /api/orders/orders/{id}/lines", command(ok, ord, func(c *oapp.AddLine, id domain.OrderID) { c.ID = id }, svc.AddLine.Handle))
	mux.HandleFunc("POST /api/orders/orders/{id}/lines/remove", command(ok, ord, func(c *oapp.RemoveLine, id domain.OrderID) { c.ID = id }, svc.RemoveLine.Handle))
	mux.HandleFunc("POST /api/orders/orders/{id}/confirm", command(ok, ord, func(c *oapp.ConfirmOrder, id domain.OrderID) { c.ID = id }, svc.Confirm.Handle))
	mux.HandleFunc("POST /api/orders/orders/{id}/request-stock", command(ok, ord, func(c *oapp.RequestStock, id domain.OrderID) { c.ID = id }, svc.RequestStock.Handle))
	mux.HandleFunc("POST /api/orders/orders/{id}/deliveries", command(http.StatusCreated, ord, func(c *oapp.Deliver, id domain.OrderID) { c.ID = id }, svc.Deliver.Handle))
	mux.HandleFunc("POST /api/orders/orders/{id}/cancel", command(ok, ord, func(c *oapp.EndOrder, id domain.OrderID) { c.ID = id }, svc.Cancel.Handle))
	mux.HandleFunc("POST /api/orders/orders/{id}/close", command(ok, ord, func(c *oapp.EndOrder, id domain.OrderID) { c.ID = id }, svc.Close.Handle))

	mux.HandleFunc("GET /api/orders/deliveries", func(w http.ResponseWriter, r *http.Request) {
		s, n := query(r)
		out, err := svc.SearchDeliveries.Handle(r.Context(), oapp.SearchDeliveries{Company: s("company"), Customer: s("customer"), Order: s("order"),
			Page: n("page"), Size: n("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/orders/deliveries/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := domain.ParseDeliveryID(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.GetDelivery.Handle(r.Context(), oapp.GetDelivery{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
}

var _ distribution.EndpointModule = (*Module)(nil)
