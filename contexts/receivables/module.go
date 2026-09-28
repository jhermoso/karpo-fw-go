// Package receivables composes the Receivables (Cobros) bounded context on a hot-swap backend.
package receivables

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	rapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service           *rapp.Service
	Credit            contracts.Credit
	Collectable       contracts.Collectable
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
	// Consumer receives billing.invoice-issued.v1 and the direct debit events of Treasury: subscribe
	// it to the transport.
	Consumer *messaging.Consumer
}

// Compose builds the context on sw. cal may be nil (no holidays).
func Compose(sw *hotswap.Switch, cal domain.Calendar) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	d := rapp.Deps{
		Terms: hotswap.Repository(sw, infrastructure.TermsRepositoryFactory), Receivables: hotswap.Repository(sw, infrastructure.ReceivableRepositoryFactory),
		Collections: hotswap.Repository(sw, infrastructure.CollectionRepositoryFactory), Credit: hotswap.Repository(sw, infrastructure.CreditRepositoryFactory),
		Calendar: cal, UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			rapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	}
	consumer := messaging.NewConsumer(contracts.Source, hotswap.Inbox(sw, infrastructure.InboxFactory), sw)
	rapp.Subscribe(consumer, d)
	rapp.SubscribeTreasury(consumer, d)
	return &Module{Service: rapp.NewService(d), Credit: rapp.CreditPort{Receivables: d.Receivables, Credit: d.Credit},
		Collectable:       rapp.DueItemsPort{Receivables: d.Receivables},
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
	col := domain.ParseCollectionID
	query := func(r *http.Request) (func(string) string, func(string) int) {
		q := r.URL.Query()
		return q.Get, func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
	}

	mux.HandleFunc("POST /api/receivables/terms", create(http.StatusCreated, svc.CreateTerms.Handle))
	mux.HandleFunc("GET /api/receivables/terms", func(w http.ResponseWriter, r *http.Request) {
		s, _ := query(r)
		out, err := svc.SearchTerms.Handle(r.Context(), rapp.SearchTerms{Seller: s("seller"), ActiveOnly: s("active") == "true"})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/receivables/terms/{id}/schedule", func(w http.ResponseWriter, r *http.Request) {
		id, err := domain.ParseTermsID(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		s, _ := query(r)
		out, err := svc.Preview.Handle(r.Context(), rapp.PreviewSchedule{ID: id, Issued: s("issued"), Amount: s("amount")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/receivables/terms/{id}/retire", command(domain.ParseTermsID, func(c *rapp.RetireTerms, id domain.TermsID) { c.ID = id }, svc.RetireTerms.Handle))

	mux.HandleFunc("PUT /api/receivables/credit", create(http.StatusOK, svc.SetCredit.Handle))
	mux.HandleFunc("GET /api/receivables/credit/exposure", func(w http.ResponseWriter, r *http.Request) {
		s, _ := query(r)
		out, err := svc.GetExposure.Handle(r.Context(), rapp.GetExposure{Seller: s("seller"), Customer: s("customer"), On: s("on")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})

	mux.HandleFunc("GET /api/receivables/invoices", func(w http.ResponseWriter, r *http.Request) {
		s, n := query(r)
		out, err := svc.SearchReceivables.Handle(r.Context(), rapp.SearchReceivables{Seller: s("seller"), Customer: s("customer"),
			OpenOnly: s("open") == "true", Page: n("page"), Size: n("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/receivables/invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := domain.ParseReceivableID(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.GetReceivable.Handle(r.Context(), rapp.GetReceivable{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})

	mux.HandleFunc("POST /api/receivables/collections", create(http.StatusCreated, svc.RegisterCollection.Handle))
	mux.HandleFunc("POST /api/receivables/offsets", create(http.StatusCreated, svc.Offset.Handle))
	mux.HandleFunc("GET /api/receivables/collections", func(w http.ResponseWriter, r *http.Request) {
		s, n := query(r)
		out, err := svc.SearchCollections.Handle(r.Context(), rapp.SearchCollections{Seller: s("seller"), Payer: s("payer"), Page: n("page"), Size: n("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/receivables/collections/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := col(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.GetCollection.Handle(r.Context(), rapp.GetCollection{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/receivables/collections/{id}/allocations", command(col, func(c *rapp.Allocate, id domain.CollectionID) { c.ID = id }, svc.Allocate.Handle))
	mux.HandleFunc("POST /api/receivables/collections/{id}/allocations/reverse", command(col, func(c *rapp.Deallocate, id domain.CollectionID) { c.ID = id }, svc.Deallocate.Handle))
	mux.HandleFunc("POST /api/receivables/collections/{id}/cancel", command(col, func(c *rapp.CancelCollection, id domain.CollectionID) { c.ID = id }, svc.CancelCollection.Handle))
}

var _ distribution.EndpointModule = (*Module)(nil)
