// Package billing compone el contexto delimitado Billing sobre un backend intercambiable en caliente.
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

// Module es el contexto compuesto.
type Module struct {
	Service           *bapp.Service
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
	// Consumer recibe los albaranes de Orders que hay que facturar: hay que suscribirlo al transporte.
	Consumer *messaging.Consumer
}

// Option configura la composición.
type Option func(*bapp.Deps)

// WithMergedParties dice qué parties están fusionadas en otra (Parties, en el anfitrión): los
// listados dejan fuera sus registros salvo que se pidan con includeMergedParties (D-100).
func WithMergedParties(m application.MergedParties) Option {
	return func(d *bapp.Deps) { d.Merged = m }
}

// WithZones da la zona horaria de cada empresa: la fecha de emisión por defecto es la de hoy
// allí, no en UTC.
func WithZones(z bapp.Zones) Option {
	return func(d *bapp.Deps) { d.Zones = z }
}

// WithSettings da los ajustes de facturación de cada empresa (D-144): cómo se calculan sus líneas,
// el patrón de sus series y cuándo nacen sus vencimientos.
func WithSettings(s bapp.Settings) Option {
	return func(d *bapp.Deps) { d.Settings = s }
}

// WithInstallmentsInTransaction da quién recibe billing.invoice-issued.v1 en la transacción de la
// emisión cuando la empresa quiere los vencimientos con la factura (with_issue, D-130), y sobre qué
// backend trabaja: en el anfitrión, el consumidor de Receivables. Si su backend no es el de
// Billing, no puede ser una sola transacción: Compose lo descarta, lo deja en el log y los
// vencimientos nacen después, como con after_issue.
func WithInstallmentsInTransaction(h application.MessageHandler, backend fw.UnitOfWork) Option {
	return func(d *bapp.Deps) { d.InTransaction, d.InTransactionUoW = h, backend }
}

// Compose construye el contexto sobre sw. Billing necesita el motor de impuestos de Fiscal y las
// identidades de Parties: los dos son puertos propios.
func Compose(sw *hotswap.Switch, taxes domain.Taxes, identities domain.Identities, opts ...Option) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	d := bapp.Deps{
		Series: hotswap.Repository(sw, infrastructure.SeriesRepositoryFactory), Invoices: hotswap.Repository(sw, infrastructure.InvoiceRepositoryFactory),
		Taxes: taxes, Identities: identities, UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			bapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	}
	if b, ok := identities.(domain.Blocks); ok { // Parties responde a los dos
		d.Blocks = b
	}
	for _, o := range opts {
		o(&d)
	}
	if d.InTransaction != nil && d.InTransactionUoW != fw.UnitOfWork(sw) {
		slog.Warn("billing: the installments handler is on another backend; installments are created after the issue (after_issue)")
		d.InTransaction, d.InTransactionUoW = nil, nil
	}
	consumer := messaging.NewConsumer(contracts.Source, hotswap.Inbox(sw, infrastructure.InboxFactory), sw)
	bapp.Subscribe(consumer, d)
	return &Module{Service: bapp.NewService(d), IntegrationOutbox: integration, Audit: audit, Consumer: consumer}
}

// Relay reenvía el Published Language a un transporte.
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

// RegisterRoutes implementa distribution.EndpointModule.
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
			Status: q.Get("status"), From: q.Get("from"), To: q.Get("to"), IncludeMergedParties: q.Get("includeMergedParties") == "true",
			Page: atoi("page"), Size: atoi("size")})
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
