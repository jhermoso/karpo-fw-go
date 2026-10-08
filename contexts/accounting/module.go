// Package accounting composes the Accounting bounded context on a hot-swap backend.
package accounting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	aapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	"github.com/jhermoso/karpo-fw-go/contexts/accounting/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/accounting/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Source is the name of the bounded context (its inbox partition).
const Source = "accounting"

// Module is the composed context.
type Module struct {
	Service *aapp.Service
	Audit   application.AuditLog
	// Consumer receives the facts Accounting posts (invoices, collections, direct debits,
	// payslips): subscribe it to the transport.
	Consumer *messaging.Consumer
	// Parking stands before Consumer: what a rule keeps from being posted (a company without a
	// ledger yet) is kept and posted later instead of refused. Subscribe it in place of Consumer
	// wherever the publishers must not be held back.
	Parking *aapp.Parking
}

// Compose builds the context on sw.
func Compose(sw *hotswap.Switch) *Module {
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	d := aapp.Deps{
		Accounts: hotswap.Repository(sw, infrastructure.AccountRepositoryFactory), Ledgers: hotswap.Repository(sw, infrastructure.LedgerRepositoryFactory),
		Entries: hotswap.Repository(sw, infrastructure.EntryRepositoryFactory), Counters: hotswap.Repository(sw, infrastructure.CounterRepositoryFactory),
		Parked: hotswap.Repository(sw, infrastructure.ParkedRepositoryFactory),
		UoW:    sw, Audit: audit, Recorder: outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
	}
	consumer := messaging.NewConsumer(Source, hotswap.Inbox(sw, infrastructure.InboxFactory), sw)
	aapp.Subscribe(consumer, d)
	d.Parking = aapp.NewParking(consumer, d)
	return &Module{Service: aapp.NewService(d), Audit: audit, Consumer: consumer, Parking: d.Parking}
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
	acc, led, ent := domain.ParseAccountID, domain.ParseLedgerID, domain.ParseEntryID

	mux.HandleFunc("POST /api/accounting/accounts", create(svc.CreateAccount.Handle))
	mux.HandleFunc("GET /api/accounting/accounts", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := svc.SearchAccounts.Handle(r.Context(), aapp.SearchAccounts{Company: q.Get("company"), Prefix: q.Get("prefix")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("PUT /api/accounting/accounts/{id}/name", command(acc, func(c *aapp.RenameAccount, id domain.AccountID) { c.ID = id }, svc.RenameAccount.Handle))
	mux.HandleFunc("POST /api/accounting/accounts/{id}/deactivate", command(acc, func(c *aapp.DeactivateAccount, id domain.AccountID) { c.ID = id }, svc.DeactivateAccount.Handle))

	mux.HandleFunc("POST /api/accounting/ledgers", create(svc.OpenLedger.Handle))
	mux.HandleFunc("GET /api/accounting/ledgers", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.GetLedger.Handle(r.Context(), aapp.GetLedger{Company: r.URL.Query().Get("company")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("PUT /api/accounting/ledgers/{id}/profile", command(led, func(c *aapp.SetProfile, id domain.LedgerID) { c.ID = id }, svc.SetProfile.Handle))
	mux.HandleFunc("POST /api/accounting/ledgers/{id}/periods/close", command(led, func(c *aapp.ChangePeriod, id domain.LedgerID) { c.ID = id }, svc.ClosePeriod.Handle))
	mux.HandleFunc("POST /api/accounting/ledgers/{id}/periods/reopen", command(led, func(c *aapp.ChangePeriod, id domain.LedgerID) { c.ID = id }, svc.ReopenPeriod.Handle))

	mux.HandleFunc("POST /api/accounting/entries", create(svc.PostEntry.Handle))
	mux.HandleFunc("GET /api/accounting/entries", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := svc.SearchEntries.Handle(r.Context(), aapp.SearchEntries{Company: q.Get("company"), From: q.Get("from"), To: q.Get("to"),
			Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/accounting/entries/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := ent(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.GetEntry.Handle(r.Context(), aapp.GetEntry{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/accounting/entries/{id}/reverse", command(ent, func(c *aapp.ReverseEntry, id domain.EntryID) { c.ID = id }, svc.ReverseEntry.Handle))
	mux.HandleFunc("GET /api/accounting/trial-balance", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := svc.TrialBalance.Handle(r.Context(), aapp.TrialBalance{Company: q.Get("company"), From: q.Get("from"), To: q.Get("to")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/accounting/ledger", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := svc.AccountLedger.Handle(r.Context(), aapp.AccountLedger{Company: q.Get("company"), Account: q.Get("account"), From: q.Get("from"), To: q.Get("to")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})

	mux.HandleFunc("GET /api/accounting/parked", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := svc.SearchParked.Handle(r.Context(), aapp.SearchParked{Company: q.Get("company"), Status: q.Get("status"), Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/accounting/parked/retry", func(w http.ResponseWriter, r *http.Request) {
		var c aapp.RetryParked
		if r.ContentLength != 0 {
			if err := decode(r, &c); err != nil {
				distribution.WriteError(w, r, err)
				return
			}
		}
		out, err := svc.RetryParked.Handle(r.Context(), c)
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/accounting/parked/{id}/discard", command(domain.ParseParkedID, func(c *aapp.DiscardParked, id domain.ParkedID) { c.ID = id },
		svc.DiscardParked.Handle))
}

var _ distribution.EndpointModule = (*Module)(nil)
