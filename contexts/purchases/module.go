// Package purchases composes the Purchases (Compras) bounded context on a hot-swap backend.
package purchases

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	uapp "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	"github.com/jhermoso/karpo-fw-go/contexts/purchases/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/purchases/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/purchases/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service           *uapp.Service
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
}

// Compose builds the context on sw. Purchases needs the tax engine of Fiscal, a port it owns.
func Compose(sw *hotswap.Switch, taxes domain.Taxes) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	svc := uapp.NewService(uapp.Deps{
		Invoices: hotswap.Repository(sw, infrastructure.InvoiceRepositoryFactory), Suppliers: hotswap.Repository(sw, infrastructure.SupplierRepositoryFactory),
		Counters: hotswap.Repository(sw, infrastructure.CounterRepositoryFactory), Taxes: taxes, UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			uapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
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
	inv := domain.ParseInvoiceID

	mux.HandleFunc("POST /api/purchases/invoices", create(http.StatusCreated, svc.RegisterInvoice.Handle))
	mux.HandleFunc("GET /api/purchases/invoices", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := svc.SearchInvoices.Handle(r.Context(), uapp.SearchInvoices{Company: q.Get("company"), Supplier: q.Get("supplier"), From: q.Get("from"),
			To: q.Get("to"), Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/purchases/invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := inv(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.GetInvoice.Handle(r.Context(), uapp.GetInvoice{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/purchases/invoices/{id}/cancel", command(inv, func(c *uapp.CancelInvoice, id domain.InvoiceID) { c.ID = id }, svc.CancelInvoice.Handle))

	mux.HandleFunc("PUT /api/purchases/suppliers", create(http.StatusOK, svc.SetSupplier.Handle))
	mux.HandleFunc("GET /api/purchases/suppliers", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.SearchSuppliers.Handle(r.Context(), uapp.SearchSuppliers{Company: r.URL.Query().Get("company")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
}

var _ distribution.EndpointModule = (*Module)(nil)
