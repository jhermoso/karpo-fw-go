// Package financial composes the Financial (Cuentas de clientes) bounded context on a hot-swap
// backend.
package financial

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	fapp "github.com/jhermoso/karpo-fw-go/contexts/financial/application"
	"github.com/jhermoso/karpo-fw-go/contexts/financial/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/financial/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/financial/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service           *fapp.Service
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
	// Accounts answers other contexts which accounts a party has.
	Accounts contracts.Accounts
}

// Compose builds the context on sw. It is the finance sector of Karpo: institutions tells which
// companies are financial institutions, the only ones it exists for (nil: none).
func Compose(sw *hotswap.Switch, institutions domain.Institutions) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	accounts := hotswap.Repository(sw, infrastructure.AccountRepositoryFactory)
	svc := fapp.NewService(fapp.Deps{Accounts: accounts, Products: hotswap.Repository(sw, infrastructure.ProductRepositoryFactory),
		Agreements: hotswap.Repository(sw, infrastructure.AgreementRepositoryFactory), Institutions: institutions, UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			fapp.Publications(messaging.NewRecorder(contracts.Source, integration)))})
	return &Module{Service: svc, IntegrationOutbox: integration, Audit: audit, Accounts: fapp.NewDirectory(accounts)}
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

func on[In, Out any](set func(*In, domain.AccountID), run func(context.Context, In) (Out, error)) http.HandlerFunc {
	return at(domain.ParseAccountID, set, run)
}

// at serves a command on the aggregate the path names.
func at[ID interface{ IsZero() bool }, In, Out any](parse func(string) (ID, error), set func(*In, ID), run func(context.Context, In) (Out, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parse(r.PathValue("id"))
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
	mux.HandleFunc("POST /api/financial/accounts", func(w http.ResponseWriter, r *http.Request) {
		var c fapp.OpenAccount
		if err := decode(r, &c); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		out, err := svc.Open.Handle(r.Context(), c)
		distribution.Respond(w, r, out, err, http.StatusCreated)
	})
	mux.HandleFunc("GET /api/financial/accounts", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := svc.Search.Handle(r.Context(), fapp.SearchAccounts{Company: q.Get("company"), Status: q.Get("status"), Party: q.Get("party"),
			Use: q.Get("use"), Currency: q.Get("currency"), Name: q.Get("name"), Demo: q.Get("demo"), Product: q.Get("product"),
			Agreement: q.Get("agreement"), Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/financial/accounts/stats", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.Stats.Handle(r.Context(), fapp.GetStats{Company: r.URL.Query().Get("company")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/financial/accounts/by-number/{number}", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.Get.Handle(r.Context(), fapp.GetAccount{Company: r.URL.Query().Get("company"), Number: r.PathValue("number")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/financial/accounts/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := domain.ParseAccountID(r.PathValue("id"))
		if err != nil || id.IsZero() {
			badID(w, r)
			return
		}
		out, err := svc.Get.Handle(r.Context(), fapp.GetAccount{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("PUT /api/financial/accounts/{id}", on(func(c *fapp.DescribeAccount, id domain.AccountID) { c.ID = id }, svc.Describe.Handle))
	mux.HandleFunc("POST /api/financial/accounts/{id}/holders", on(func(c *fapp.RelateParty, id domain.AccountID) { c.ID = id }, svc.Relate.Handle))
	mux.HandleFunc("POST /api/financial/accounts/{id}/holders/end", on(func(c *fapp.UnrelateParty, id domain.AccountID) { c.ID = id }, svc.Unrelate.Handle))
	mux.HandleFunc("POST /api/financial/accounts/{id}/holders/primary", on(func(c *fapp.FileUnder, id domain.AccountID) { c.ID = id }, svc.FileUnder.Handle))
	mux.HandleFunc("POST /api/financial/accounts/{id}/uses", on(func(c *fapp.ChangeUse, id domain.AccountID) { c.ID = id }, svc.Assign.Handle))
	mux.HandleFunc("POST /api/financial/accounts/{id}/uses/end", on(func(c *fapp.ChangeUse, id domain.AccountID) { c.ID = id }, svc.Withdraw.Handle))
	status := func(c *fapp.ChangeStatus, id domain.AccountID) { c.ID = id }
	mux.HandleFunc("POST /api/financial/accounts/{id}/block", on(status, svc.Block.Handle))
	mux.HandleFunc("POST /api/financial/accounts/{id}/abandon", on(status, svc.Abandon.Handle))
	mux.HandleFunc("POST /api/financial/accounts/{id}/release", on(status, svc.Release.Handle))
	mux.HandleFunc("POST /api/financial/accounts/{id}/close", on(status, svc.Close.Handle))

	create := func(w http.ResponseWriter, r *http.Request, run func(*http.Request) (any, error)) {
		out, err := run(r)
		distribution.Respond(w, r, out, err, http.StatusCreated)
	}
	mux.HandleFunc("POST /api/financial/products", func(w http.ResponseWriter, r *http.Request) {
		create(w, r, func(r *http.Request) (any, error) {
			var c fapp.DefineProduct
			if err := decode(r, &c); err != nil {
				return nil, err
			}
			return svc.DefineProduct.Handle(r.Context(), c)
		})
	})
	mux.HandleFunc("GET /api/financial/products", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := svc.SearchProducts.Handle(r.Context(), fapp.SearchProducts{Company: q.Get("company"), Family: q.Get("family"), Offered: q.Get("offered"),
			Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	pro := domain.ParseProductID
	mux.HandleFunc("GET /api/financial/products/{id}", at(pro, func(c *fapp.GetProduct, id domain.ProductID) { c.ID = id }, svc.GetProduct.Handle))
	mux.HandleFunc("PUT /api/financial/products/{id}", at(pro, func(c *fapp.ChangeProduct, id domain.ProductID) { c.ID = id }, svc.ChangeProduct.Handle))
	mux.HandleFunc("POST /api/financial/products/{id}/discontinue", at(pro, func(c *fapp.DiscontinueProduct, id domain.ProductID) { c.ID = id },
		svc.DiscontinueProduct.Handle))
	mux.HandleFunc("POST /api/financial/products/{id}/reinstate", at(pro, func(c *fapp.DiscontinueProduct, id domain.ProductID) { c.ID, c.Reinstate = id, true },
		svc.DiscontinueProduct.Handle))

	mux.HandleFunc("POST /api/financial/agreements", func(w http.ResponseWriter, r *http.Request) {
		create(w, r, func(r *http.Request) (any, error) {
			var c fapp.SignAgreement
			if err := decode(r, &c); err != nil {
				return nil, err
			}
			return svc.SignAgreement.Handle(r.Context(), c)
		})
	})
	mux.HandleFunc("GET /api/financial/agreements", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := svc.SearchAgreements.Handle(r.Context(), fapp.SearchAgreements{Company: q.Get("company"), Customer: q.Get("customer"),
			Product: q.Get("product"), Status: q.Get("status"), Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	agr := domain.ParseAgreementID
	mux.HandleFunc("GET /api/financial/agreements/{id}", at(agr, func(c *fapp.GetAgreement, id domain.AgreementID) { c.ID = id }, svc.GetAgreement.Handle))
	mux.HandleFunc("PUT /api/financial/agreements/{id}", at(agr, func(c *fapp.ChangeAgreement, id domain.AgreementID) { c.ID = id }, svc.ChangeAgreement.Handle))
	mux.HandleFunc("POST /api/financial/agreements/{id}/terminate", at(agr, func(c *fapp.TerminateAgreement, id domain.AgreementID) { c.ID = id },
		svc.TerminateAgreement.Handle))
}

var _ distribution.EndpointModule = (*Module)(nil)
