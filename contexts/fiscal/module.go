// Package fiscal composes the Fiscal bounded context on a hot-swap backend.
package fiscal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/jurisdictions/es"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service *fapp.Service
	Rates   contracts.Rates
	// TaxEngine calculates indirect taxes with the jurisdiction of the seller (Spain today).
	TaxEngine         contracts.TaxEngine
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
	// Consumer receives the Payroll and Purchases events that feed the withholding forms:
	// subscribe it to the transport.
	Consumer *messaging.Consumer
}

// Compose builds the context on sw. Fiscal needs the tax identities of Parties.
func Compose(sw *hotswap.Switch, identities domain.Identities) *Module {
	rates := hotswap.Repository(sw, infrastructure.TaxRateRepositoryFactory)
	withholdings := hotswap.Repository(sw, infrastructure.WithholdingRepositoryFactory)
	treatments := hotswap.Repository(sw, infrastructure.TreatmentRepositoryFactory)
	taxpayers := hotswap.Repository(sw, infrastructure.TaxpayerRepositoryFactory)
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	svc := fapp.NewService(fapp.Deps{
		Rates: rates, Treatments: treatments, Taxpayers: taxpayers, Filings: hotswap.Repository(sw, infrastructure.FilingRepositoryFactory),
		Counters: hotswap.Repository(sw, infrastructure.CounterRepositoryFactory), Withholdings: withholdings, Identities: identities,
		UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			fapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	})
	consumer := messaging.NewConsumer(contracts.Source, hotswap.Inbox(sw, infrastructure.InboxFactory), sw)
	fapp.Subscribe(consumer, withholdings)
	fapp.SubscribePurchases(consumer, withholdings)
	return &Module{Service: svc, Rates: fapp.RateLookup{Rates: rates}, TaxEngine: fapp.NewEngine(taxpayers, rates, treatments, es.Spain{}),
		IntegrationOutbox: integration, Audit: audit, Consumer: consumer}
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

// command handles POST/PUT /{id}/...: it parses the id, decodes the body and lets set place the id.
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
	rate, treat, tp, fil := domain.ParseTaxRateID, domain.ParseTreatmentID, domain.ParseTaxpayerID, domain.ParseFilingID

	// Catalogs.
	mux.HandleFunc("POST /api/fiscal/tax-rates", create(svc.CreateRate.Handle))
	mux.HandleFunc("GET /api/fiscal/tax-rates", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := svc.SearchRates.Handle(r.Context(), fapp.SearchRates{Type: q.Get("type"), Territory: q.Get("territory"), On: q.Get("on")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/fiscal/tax-rates/{id}/end", command(rate, func(c *fapp.EndRate, id domain.TaxRateID) { c.ID = id }, svc.EndRate.Handle))
	mux.HandleFunc("POST /api/fiscal/tax-treatments", create(svc.CreateTreatment.Handle))
	mux.HandleFunc("GET /api/fiscal/tax-treatments", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := svc.SearchTreatments.Handle(r.Context(), fapp.SearchTreatments{Territory: q.Get("territory"), ActiveOnly: q.Get("active") == "true"})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/fiscal/tax-treatments/{id}/retire", command(treat, func(c *fapp.RetireTreatment, id domain.TreatmentID) { c.ID = id }, svc.RetireTreatment.Handle))

	// Taxpayers.
	mux.HandleFunc("POST /api/fiscal/taxpayers", create(svc.RegisterTaxpayer.Handle))
	mux.HandleFunc("GET /api/fiscal/taxpayers", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.GetTaxpayer.Handle(r.Context(), fapp.GetTaxpayer{Organization: r.URL.Query().Get("organization")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/fiscal/taxpayers/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := tp(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.GetTaxpayer.Handle(r.Context(), fapp.GetTaxpayer{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("PUT /api/fiscal/taxpayers/{id}/terms", command(tp, func(c *fapp.SetTaxpayerTerms, id domain.TaxpayerID) { c.ID = id }, svc.SetTaxpayerTerms.Handle))
	mux.HandleFunc("POST /api/fiscal/taxpayers/{id}/activities", command(tp, func(c *fapp.AddActivity, id domain.TaxpayerID) { c.ID = id }, svc.AddActivity.Handle))
	mux.HandleFunc("POST /api/fiscal/taxpayers/{id}/activities/end", command(tp, func(c *fapp.EndActivity, id domain.TaxpayerID) { c.ID = id }, svc.EndActivity.Handle))
	mux.HandleFunc("POST /api/fiscal/taxpayers/{id}/obligations", command(tp, func(c *fapp.AddObligation, id domain.TaxpayerID) { c.ID = id }, svc.AddObligation.Handle))
	mux.HandleFunc("POST /api/fiscal/taxpayers/{id}/obligations/end", command(tp, func(c *fapp.EndObligation, id domain.TaxpayerID) { c.ID = id }, svc.EndObligation.Handle))

	// Filings.
	mux.HandleFunc("POST /api/fiscal/filings", create(svc.GenerateFiling.Handle))
	mux.HandleFunc("GET /api/fiscal/filings", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		page, err := svc.SearchFilings.Handle(r.Context(), fapp.SearchFilings{Organization: q.Get("organization"), Form: q.Get("form"),
			Status: q.Get("status"), Year: atoi("year"), Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, page, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/fiscal/filings/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := fil(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.GetFiling.Handle(r.Context(), fapp.GetFiling{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("DELETE /api/fiscal/filings/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := fil(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		if _, err := svc.DiscardFiling.Handle(r.Context(), fapp.DiscardFiling{ID: id}); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/fiscal/filings/{id}/submit", command(fil, func(c *fapp.SubmitFiling, id domain.FilingID) { c.ID = id }, svc.SubmitFiling.Handle))
	mux.HandleFunc("POST /api/fiscal/filings/{id}/revert", command(fil, func(c *fapp.RevertFiling, id domain.FilingID) { c.ID = id }, svc.RevertFiling.Handle))

	// Rate lookup for other contexts.
	mux.Handle("GET /api/fiscal/rate", distribution.RequirePermission(fapp.PermCatalogRead, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		ref, ok, err := m.Rates.RateOn(r.Context(), q.Get("type"), q.Get("territory"), q.Get("code"), q.Get("on"))
		if err == nil && !ok {
			err = fw.NotFound(domain.TaxRateKind, fwString(q.Get("code")))
		}
		distribution.Respond(w, r, ref, err, http.StatusOK)
	})))
}

type fwString string

func (s fwString) String() string { return string(s) }

var _ distribution.EndpointModule = (*Module)(nil)
