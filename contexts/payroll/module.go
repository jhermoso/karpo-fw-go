// Package payroll composes the Payroll bounded context on a hot-swap backend.
package payroll

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	papp "github.com/jhermoso/karpo-fw-go/contexts/payroll/application"
	"github.com/jhermoso/karpo-fw-go/contexts/payroll/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/payroll/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/payroll/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service           *papp.Service
	Remittance        contracts.Remittance
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
}

// Compose builds the context on sw. Payroll needs HR: employments are the port it owns.
func Compose(sw *hotswap.Switch, employments papp.Employments) *Module {
	payslips := hotswap.Repository(sw, infrastructure.PayslipRepositoryFactory)
	profiles := hotswap.Repository(sw, infrastructure.ProfileRepositoryFactory)
	accounts := hotswap.Repository(sw, infrastructure.AccountRepositoryFactory)
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	svc := papp.NewService(papp.Deps{
		Payslips: payslips, Profiles: profiles, Accounts: accounts, Catalogs: catalogs{hotswap.Bind(sw, infrastructure.CatalogsFor)},
		Employments: employments, UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			papp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	})
	return &Module{Service: svc, Remittance: papp.Remittance{Payslips: payslips, Profiles: profiles}, IntegrationOutbox: integration, Audit: audit}
}

// Relay forwards the Published Language to a transport.
func (m *Module) Relay(sender application.MessageSender, opts ...outbox.RelayOption) *outbox.Relay {
	return messaging.NewRelay(contracts.Source, m.IntegrationOutbox, sender, opts...)
}

type catalogs struct {
	b *hotswap.Binding[domain.Catalogs]
}

func (c catalogs) Concepts(ctx context.Context) (out []domain.Concept, err error) {
	err = c.b.With(ctx, func(ctx context.Context, x domain.Catalogs) error { out, err = x.Concepts(ctx); return err })
	return out, err
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

func get[ID, Out any](parse func(string) (ID, error), run func(context.Context, ID) (Out, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parse(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := run(r.Context(), id)
		distribution.Respond(w, r, out, err, http.StatusOK)
	}
}

// RegisterRoutes implements distribution.EndpointModule.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	svc := m.Service
	pay, prof, acc := domain.ParsePayslipID, domain.ParseProfileID, domain.ParseEmployerAccountID
	query := func(r *http.Request) (func(string) string, func(string) int) {
		q := r.URL.Query()
		return q.Get, func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
	}

	// Payslips.
	mux.HandleFunc("POST /api/payroll/payslips", create(svc.DraftPayslip.Handle))
	mux.HandleFunc("GET /api/payroll/payslips", func(w http.ResponseWriter, r *http.Request) {
		s, n := query(r)
		page, err := svc.SearchPayslips.Handle(r.Context(), papp.SearchPayslips{Employer: s("employer"), Person: s("person"), Status: s("status"),
			From: s("from"), To: s("to"), Page: n("page"), Size: n("size")})
		distribution.Respond(w, r, page, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/payroll/payslips/{id}", get(pay, func(ctx context.Context, id domain.PayslipID) (papp.PayslipDTO, error) {
		return svc.GetPayslip.Handle(ctx, papp.GetPayslip{ID: id})
	}))
	mux.HandleFunc("DELETE /api/payroll/payslips/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := pay(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		if _, err := svc.Discard.Handle(r.Context(), papp.DiscardPayslip{ID: id}); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/payroll/payslips/{id}/lines", command(pay, func(c *papp.AddLine, id domain.PayslipID) { c.ID = id }, svc.AddLine.Handle))
	mux.HandleFunc("POST /api/payroll/payslips/{id}/lines/remove", command(pay, func(c *papp.RemoveLine, id domain.PayslipID) { c.ID = id }, svc.RemoveLine.Handle))
	mux.HandleFunc("PUT /api/payroll/payslips/{id}/payment-date", command(pay, func(c *papp.Reschedule, id domain.PayslipID) { c.ID = id }, svc.Reschedule.Handle))
	mux.HandleFunc("POST /api/payroll/payslips/{id}/approve", command(pay, func(c *papp.ApprovePayslip, id domain.PayslipID) { c.ID = id }, svc.Approve.Handle))
	mux.HandleFunc("POST /api/payroll/payslips/{id}/cancel", command(pay, func(c *papp.CancelPayslip, id domain.PayslipID) { c.ID = id }, svc.Cancel.Handle))

	// Profiles.
	mux.HandleFunc("POST /api/payroll/profiles", create(svc.OpenProfile.Handle))
	mux.HandleFunc("GET /api/payroll/profiles", func(w http.ResponseWriter, r *http.Request) {
		s, n := query(r)
		page, err := svc.SearchProfiles.Handle(r.Context(), papp.SearchProfiles{Employer: s("employer"), Person: s("person"), Page: n("page"), Size: n("size")})
		distribution.Respond(w, r, page, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/payroll/profiles/{id}", get(prof, func(ctx context.Context, id domain.ProfileID) (papp.ProfileDTO, error) {
		return svc.GetProfile.Handle(ctx, papp.GetProfile{ID: id})
	}))
	mux.HandleFunc("PUT /api/payroll/profiles/{id}/terms", command(prof, func(c *papp.SetTerms, id domain.ProfileID) { c.ID = id }, svc.SetTerms.Handle))
	mux.HandleFunc("POST /api/payroll/profiles/{id}/splits", command(prof, func(c *papp.AddSplit, id domain.ProfileID) { c.ID = id }, svc.AddSplit.Handle))
	mux.HandleFunc("POST /api/payroll/profiles/{id}/splits/end", command(prof, func(c *papp.EndSplit, id domain.ProfileID) { c.ID = id }, svc.EndSplit.Handle))

	// Employer accounts (CCC).
	mux.HandleFunc("POST /api/payroll/employer-accounts", create(svc.RegisterAccount.Handle))
	mux.HandleFunc("GET /api/payroll/employer-accounts", func(w http.ResponseWriter, r *http.Request) {
		s, n := query(r)
		page, err := svc.SearchAccounts.Handle(r.Context(), papp.SearchAccounts{Employer: s("employer"), ActiveOnly: s("active") == "true",
			Page: n("page"), Size: n("size")})
		distribution.Respond(w, r, page, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/payroll/employer-accounts/{id}", get(acc, func(ctx context.Context, id domain.EmployerAccountID) (papp.AccountDTO, error) {
		return svc.GetAccount.Handle(ctx, papp.GetAccount{ID: id})
	}))
	mux.HandleFunc("PUT /api/payroll/employer-accounts/{id}/payment", command(acc, func(c *papp.SetAccountPayment, id domain.EmployerAccountID) { c.ID = id }, svc.SetAccountPayment.Handle))
	mux.HandleFunc("POST /api/payroll/employer-accounts/{id}/deactivate", command(acc, func(c *papp.DeactivateAccount, id domain.EmployerAccountID) { c.ID = id }, svc.DeactivateAccount.Handle))

	// Catalog and the Remittance port (for Treasury).
	mux.HandleFunc("GET /api/payroll/catalogs/concepts", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.Concepts.Handle(r.Context(), papp.ListConcepts{Kind: r.URL.Query().Get("kind")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.Handle("POST /api/payroll/remittance/net-payments", distribution.RequirePermission(papp.PermRemittanceRead, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				PayslipIDs []string `json:"payslipIds"`
			}
			if err := decode(r, &body); err != nil {
				distribution.WriteError(w, r, err)
				return
			}
			out, err := m.Remittance.NetPayments(r.Context(), body.PayslipIDs)
			distribution.Respond(w, r, out, err, http.StatusOK)
		})))
}

var _ distribution.EndpointModule = (*Module)(nil)
