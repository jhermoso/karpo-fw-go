// Package billing composes the Billing bounded context on a hot-swap backend.
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	"github.com/jhermoso/karpo-fw-go/contexts/billing/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service           *bapp.Service
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
	// Consumer receives the delivery notes of Orders to invoice: subscribe it to the transport.
	Consumer *messaging.Consumer
}

// Compose builds the context on sw. Billing needs the tax engine of Fiscal and the identities of
// Parties: both are ports it owns.
func Compose(sw *hotswap.Switch, taxes domain.Taxes, identities domain.Identities) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	d := bapp.Deps{
		Series: hotswap.Repository(sw, infrastructure.SeriesRepositoryFactory), Invoices: hotswap.Repository(sw, infrastructure.InvoiceRepositoryFactory),
		Taxes: taxes, Identities: identities, UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			bapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	}
	consumer := messaging.NewConsumer(contracts.Source, hotswap.Inbox(sw, infrastructure.InboxFactory), sw)
	bapp.Subscribe(consumer, d)
	return &Module{Service: bapp.NewService(d), IntegrationOutbox: integration, Audit: audit, Consumer: consumer}
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

func create[In, Out any](run func(context.Context, In) (Out, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var c In
		if err := decode(r, &c); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		out, err := run(r.Context(), c)
		distribution.Respond(w, r, out, err, http.StatusCreated)
	}
}

// RegisterRoutes implements distribution.EndpointModule.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	svc := m.Service
	ser, inv := domain.ParseSeriesID, domain.ParseInvoiceID

	mux.HandleFunc("POST /api/billing/series", create(svc.OpenSeries.Handle))
	mux.HandleFunc("GET /api/billing/series", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		year, _ := strconv.Atoi(q.Get("year"))
		out, err := svc.SearchSeries.Handle(r.Context(), bapp.SearchSeries{Seller: q.Get("seller"), Year: year})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/billing/series/{id}/close", command(ser, func(c *bapp.CloseSeries, id domain.SeriesID) { c.ID = id }, svc.CloseSeries.Handle))

	mux.HandleFunc("POST /api/billing/invoices", create(svc.DraftInvoice.Handle))
	mux.HandleFunc("GET /api/billing/invoices", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		page, err := svc.SearchInvoices.Handle(r.Context(), bapp.SearchInvoices{Seller: q.Get("seller"), Customer: q.Get("customer"),
			Status: q.Get("status"), From: q.Get("from"), To: q.Get("to"), Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, page, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/billing/invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := inv(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.GetInvoice.Handle(r.Context(), bapp.GetInvoice{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/billing/invoices/{id}/taxes", func(w http.ResponseWriter, r *http.Request) {
		id, err := inv(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.PreviewTaxes.Handle(r.Context(), bapp.PreviewTaxes{ID: id, On: r.URL.Query().Get("on")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("DELETE /api/billing/invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := inv(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		if _, err := svc.Discard.Handle(r.Context(), bapp.DiscardInvoice{ID: id}); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/billing/invoices/{id}/lines", command(inv, func(c *bapp.AddLine, id domain.InvoiceID) { c.ID = id }, svc.AddLine.Handle))
	mux.HandleFunc("POST /api/billing/invoices/{id}/lines/remove", command(inv, func(c *bapp.RemoveLine, id domain.InvoiceID) { c.ID = id }, svc.RemoveLine.Handle))
	mux.HandleFunc("PUT /api/billing/invoices/{id}/details", command(inv, func(c *bapp.SetDetails, id domain.InvoiceID) { c.ID = id }, svc.SetDetails.Handle))
	mux.HandleFunc("POST /api/billing/invoices/{id}/issue", command(inv, func(c *bapp.IssueInvoice, id domain.InvoiceID) { c.ID = id }, svc.Issue.Handle))
}

var _ distribution.EndpointModule = (*Module)(nil)
