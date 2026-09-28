// Package payments composes the Payments (Pagos) bounded context on a hot-swap backend.
package payments

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	papp "github.com/jhermoso/karpo-fw-go/contexts/payments/application"
	"github.com/jhermoso/karpo-fw-go/contexts/payments/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/payments/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/payments/infrastructure"
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
	Payable           contracts.Payable
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
	// Consumer receives the payslips of Payroll, the tax forms of Fiscal and the transfers of
	// Treasury: subscribe it to the transport.
	Consumer *messaging.Consumer
}

// Compose builds the context on sw. netPay may be nil (payroll payables without accounts).
func Compose(sw *hotswap.Switch, netPay domain.NetPaySplits) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	d := papp.Deps{
		Payables: hotswap.Repository(sw, infrastructure.PayableRepositoryFactory), Payments: hotswap.Repository(sw, infrastructure.PaymentRepositoryFactory),
		NetPay: netPay, UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			papp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	}
	consumer := messaging.NewConsumer(contracts.Source, hotswap.Inbox(sw, infrastructure.InboxFactory), sw)
	papp.Subscribe(consumer, d)
	return &Module{Service: papp.NewService(d), Payable: papp.PayablePort{Payables: d.Payables}, IntegrationOutbox: integration, Audit: audit,
		Consumer: consumer}
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
	pay, pmt := domain.ParsePayableID, domain.ParsePaymentID
	query := func(r *http.Request) (func(string) string, func(string) int) {
		q := r.URL.Query()
		return q.Get, func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
	}

	mux.HandleFunc("POST /api/payments/supplier-invoices", create(http.StatusCreated, svc.RegisterSupplierInvoice.Handle))
	mux.HandleFunc("GET /api/payments/payables", func(w http.ResponseWriter, r *http.Request) {
		s, n := query(r)
		out, err := svc.SearchPayables.Handle(r.Context(), papp.SearchPayables{Company: s("company"), Payee: s("payee"), Kind: s("kind"), DueTo: s("dueTo"),
			OpenOnly: s("open") == "true", Page: n("page"), Size: n("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/payments/payables/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := pay(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.GetPayable.Handle(r.Context(), papp.GetPayable{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("PUT /api/payments/payables/{id}/pay-to", command(pay, func(c *papp.SetPayTo, id domain.PayableID) { c.ID = id }, svc.SetPayTo.Handle))
	mux.HandleFunc("POST /api/payments/payables/{id}/cancel", command(pay, func(c *papp.CancelPayable, id domain.PayableID) { c.ID = id }, svc.CancelPayable.Handle))

	mux.HandleFunc("POST /api/payments/payments", create(http.StatusCreated, svc.RegisterPayment.Handle))
	mux.HandleFunc("GET /api/payments/payments", func(w http.ResponseWriter, r *http.Request) {
		s, n := query(r)
		out, err := svc.SearchPayments.Handle(r.Context(), papp.SearchPayments{Company: s("company"), Payee: s("payee"), Page: n("page"), Size: n("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/payments/payments/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := pmt(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.GetPayment.Handle(r.Context(), papp.GetPayment{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/payments/payments/{id}/allocations", command(pmt, func(c *papp.Allocate, id domain.PaymentID) { c.ID = id }, svc.Allocate.Handle))
	mux.HandleFunc("POST /api/payments/payments/{id}/allocations/reverse", command(pmt, func(c *papp.Deallocate, id domain.PaymentID) { c.ID = id }, svc.Deallocate.Handle))
	mux.HandleFunc("POST /api/payments/payments/{id}/cancel", command(pmt, func(c *papp.CancelPayment, id domain.PaymentID) { c.ID = id }, svc.CancelPayment.Handle))
}

var _ distribution.EndpointModule = (*Module)(nil)
