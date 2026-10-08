// Package treasury composes the Treasury bounded context on a hot-swap backend.
package treasury

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	tapp "github.com/jhermoso/karpo-fw-go/contexts/treasury/application"
	"github.com/jhermoso/karpo-fw-go/contexts/treasury/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/treasury/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/treasury/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service           *tapp.Service
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
}

// Compose builds the context on sw. Treasury needs the due items of Receivables, the payables of
// Payments (nil: no transfer orders) and the identities of Parties: all are ports it owns.
func Compose(sw *hotswap.Switch, receivables domain.Receivables, payables domain.Payables, identities domain.Identities) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	svc := tapp.NewService(tapp.Deps{
		Accounts: hotswap.Repository(sw, infrastructure.AccountRepositoryFactory), Mandates: hotswap.Repository(sw, infrastructure.MandateRepositoryFactory),
		Remittances: hotswap.Repository(sw, infrastructure.RemittanceRepositoryFactory), Transfers: hotswap.Repository(sw, infrastructure.TransferOrderRepositoryFactory),
		Statements: hotswap.Repository(sw, infrastructure.StatementRepositoryFactory), Receivables: receivables, Payables: payables, Identities: identities,
		UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			tapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
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
	acc, man, rem := domain.ParseAccountID, domain.ParseMandateID, domain.ParseRemittanceID

	mux.HandleFunc("POST /api/treasury/accounts", create(svc.OpenAccount.Handle))
	mux.HandleFunc("GET /api/treasury/accounts", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.SearchAccounts.Handle(r.Context(), tapp.SearchAccounts{Owner: r.URL.Query().Get("owner")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/treasury/accounts/{id}/close", command(acc, func(c *tapp.CloseAccount, id domain.AccountID) { c.ID = id }, svc.CloseAccount.Handle))

	mux.HandleFunc("POST /api/treasury/mandates", create(svc.RegisterMandate.Handle))
	mux.HandleFunc("GET /api/treasury/mandates", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := svc.SearchMandates.Handle(r.Context(), tapp.SearchMandates{Creditor: q.Get("creditor"), Debtor: q.Get("debtor")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/treasury/mandates/{id}/revoke", command(man, func(c *tapp.RevokeMandate, id domain.MandateID) { c.ID = id }, svc.RevokeMandate.Handle))

	mux.HandleFunc("POST /api/treasury/remittances", create(svc.Propose.Handle))
	mux.HandleFunc("GET /api/treasury/remittances", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := svc.Search.Handle(r.Context(), tapp.SearchRemittances{Creditor: q.Get("creditor"), Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/treasury/remittances/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := rem(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.Get.Handle(r.Context(), tapp.GetRemittance{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/treasury/remittances/{id}/pain008", func(w http.ResponseWriter, r *http.Request) {
		id, err := rem(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		file, err := svc.File.Handle(r.Context(), tapp.GetFile{ID: id})
		if err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+id.String()+`.xml"`)
		_, _ = w.Write(file)
	})
	mux.HandleFunc("POST /api/treasury/remittances/{id}/items/remove", command(rem, func(c *tapp.RemoveItem, id domain.RemittanceID) { c.ID = id }, svc.RemoveItem.Handle))
	mux.HandleFunc("POST /api/treasury/remittances/{id}/generate", command(rem, func(c *tapp.GenerateRemittance, id domain.RemittanceID) { c.ID = id }, svc.Generate.Handle))
	mux.HandleFunc("POST /api/treasury/remittances/{id}/settle", command(rem, func(c *tapp.SettleRemittance, id domain.RemittanceID) { c.ID = id }, svc.Settle.Handle))
	mux.HandleFunc("POST /api/treasury/remittances/{id}/returns", command(rem, func(c *tapp.ReturnDebit, id domain.RemittanceID) { c.ID = id }, svc.Return.Handle))
	mux.HandleFunc("POST /api/treasury/remittances/{id}/cancel", command(rem, func(c *tapp.CancelRemittance, id domain.RemittanceID) { c.ID = id }, svc.Cancel.Handle))

	trf := domain.ParseTransferOrderID
	mux.HandleFunc("POST /api/treasury/transfers", create(svc.ProposeTransfers.Handle))
	mux.HandleFunc("GET /api/treasury/transfers", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := svc.SearchTransferOrders.Handle(r.Context(), tapp.SearchTransferOrders{Debtor: q.Get("debtor"), Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/treasury/transfers/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := trf(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.GetTransferOrder.Handle(r.Context(), tapp.GetTransferOrder{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/treasury/transfers/{id}/pain001", func(w http.ResponseWriter, r *http.Request) {
		id, err := trf(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		file, err := svc.TransferFile.Handle(r.Context(), tapp.GetTransferFile{ID: id})
		if err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+id.String()+`.xml"`)
		_, _ = w.Write(file)
	})
	mux.HandleFunc("POST /api/treasury/transfers/{id}/remove", command(trf, func(c *tapp.RemoveTransfer, id domain.TransferOrderID) { c.ID = id }, svc.RemoveTransfer.Handle))
	mux.HandleFunc("POST /api/treasury/transfers/{id}/generate", command(trf, func(c *tapp.GenerateTransfers, id domain.TransferOrderID) { c.ID = id }, svc.GenerateTransfers.Handle))
	mux.HandleFunc("POST /api/treasury/transfers/{id}/settle", command(trf, func(c *tapp.SettleTransfers, id domain.TransferOrderID) { c.ID = id }, svc.SettleTransfers.Handle))
	mux.HandleFunc("POST /api/treasury/transfers/{id}/rejections", command(trf, func(c *tapp.RejectTransfer, id domain.TransferOrderID) { c.ID = id }, svc.RejectTransfer.Handle))
	mux.HandleFunc("POST /api/treasury/transfers/{id}/cancel", command(trf, func(c *tapp.CancelTransfers, id domain.TransferOrderID) { c.ID = id }, svc.CancelTransfers.Handle))

	stm := domain.ParseStatementID
	mux.HandleFunc("POST /api/treasury/statements", create(svc.ImportStatement.Handle))
	mux.HandleFunc("POST /api/treasury/statements/norma43", create(svc.ImportNorma43.Handle))
	mux.HandleFunc("GET /api/treasury/statements", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := svc.SearchStatements.Handle(r.Context(), tapp.SearchStatements{Owner: q.Get("owner"), Account: q.Get("account"),
			Pending: q.Get("pending") == "true", Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/treasury/statements/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := stm(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.GetStatement.Handle(r.Context(), tapp.GetStatement{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/treasury/statements/{id}/reconcile", command(stm, func(c *tapp.ReconcileLine, id domain.StatementID) { c.ID = id }, svc.Reconcile.Handle))
	mux.HandleFunc("POST /api/treasury/statements/{id}/release", command(stm, func(c *tapp.ReleaseLine, id domain.StatementID) { c.ID = id }, svc.Release.Handle))
	mux.HandleFunc("POST /api/treasury/statements/{id}/auto-reconcile", command(stm, func(c *tapp.AutoReconcile, id domain.StatementID) { c.ID = id }, svc.AutoReconcile.Handle))
}

var _ distribution.EndpointModule = (*Module)(nil)
