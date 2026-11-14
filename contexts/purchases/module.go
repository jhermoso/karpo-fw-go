// Package purchases compone el contexto delimitado Purchases (Compras) sobre un backend
// intercambiable en caliente.
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
	"github.com/jhermoso/karpo-fw-go/pkg/application/approval"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	fwapproval "github.com/jhermoso/karpo-fw-go/pkg/domain/approval"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module es el contexto compuesto.
type Module struct {
	Service *uapp.Service
	// UoW es la unidad de trabajo del contexto: el contrato de mantenimiento hace en una sola el
	// cambio entero de un proveedor.
	UoW               fw.UnitOfWork
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
	// Consumer recibe los proveedores que Parties traslada al fusionar fichas: hay que suscribirlo
	// al transporte.
	Consumer *messaging.Consumer
}

// Option configura la composición.
type Option func(*uapp.Deps)

// WithMergedParties dice qué parties están fusionadas en otra (Parties, en el anfitrión): los
// listados dejan fuera sus registros salvo que se pidan con includeMergedParties (D-100).
func WithMergedParties(m application.MergedParties) Option {
	return func(d *uapp.Deps) { d.Merged = m }
}

// WithApprovalPolicy dice qué empresas exigen aprobación para bloquear o desbloquear un proveedor
// (D-156; la de Parties, Module.ApprovalPolicy, en el anfitrión). Sin ella, ninguna.
func WithApprovalPolicy(p approval.Policy) Option {
	return func(d *uapp.Deps) { d.Policy = p }
}

// Compose construye el contexto sobre sw. Purchases necesita el motor de impuestos de Fiscal, un
// puerto propio.
func Compose(sw *hotswap.Switch, taxes domain.Taxes, opts ...Option) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	d := uapp.Deps{
		Invoices: hotswap.Repository(sw, infrastructure.InvoiceRepositoryFactory), Suppliers: hotswap.Repository(sw, infrastructure.SupplierRepositoryFactory),
		Counters: hotswap.Repository(sw, infrastructure.CounterRepositoryFactory), Taxes: taxes, UoW: sw, Audit: audit,
		Approvals: hotswap.Repository(sw, infrastructure.ApprovalRepositoryFactory), Policy: approval.Never{},
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			uapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	}
	for _, o := range opts {
		o(&d)
	}
	consumer := messaging.NewConsumer(contracts.Source, hotswap.Inbox(sw, infrastructure.InboxFactory), sw)
	uapp.Subscribe(consumer, d)
	return &Module{Service: uapp.NewService(d), UoW: sw, IntegrationOutbox: integration, Audit: audit, Consumer: consumer}
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

// RegisterRoutes implementa distribution.EndpointModule.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	svc := m.Service
	inv := domain.ParseInvoiceID

	mux.HandleFunc("POST /api/purchases/invoices", create(http.StatusCreated, svc.RegisterInvoice.Handle))
	mux.HandleFunc("GET /api/purchases/invoices", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := svc.SearchInvoices.Handle(r.Context(), uapp.SearchInvoices{Company: q.Get("company"), Supplier: q.Get("supplier"), From: q.Get("from"),
			To: q.Get("to"), IncludeMergedParties: q.Get("includeMergedParties") == "true", Page: atoi("page"), Size: atoi("size")})
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

	mux.HandleFunc("PUT /api/purchases/suppliers", func(w http.ResponseWriter, r *http.Request) {
		var c uapp.SetSupplier
		if err := decode(r, &c); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		out, err := svc.SetSupplier.Handle(r.Context(), c)
		distribution.Respond(w, r, out, err, supplierStatus(out, http.StatusOK))
	})
	mux.HandleFunc("GET /api/purchases/suppliers", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.SearchSuppliers.Handle(r.Context(), uapp.SearchSuppliers{Company: r.URL.Query().Get("company"),
			IncludeMergedParties: r.URL.Query().Get("includeMergedParties") == "true"})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	m.RegisterMaintenanceRoutes(mux) // el contrato de mantenimiento, bajo /api/v1/suppliers

	// Las solicitudes de aprobación del bloqueo y el desbloqueo (D-156, docs/COMPRAS.md).
	mux.HandleFunc("GET /api/purchases/approvals", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.SearchApprovals.Handle(r.Context(), uapp.SearchApprovals{Company: r.URL.Query().Get("company"), Status: r.URL.Query().Get("status")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	apv := func(s string) (fwapproval.RequestID, error) {
		u, err := fw.ParseUUID(s)
		return fwapproval.RequestID{UUID: u}, err
	}
	set := func(c *uapp.ResolveApproval, id fwapproval.RequestID) { c.ID = id }
	mux.HandleFunc("POST /api/purchases/approvals/{id}/approve", command(apv, set, svc.ApproveApproval.Handle))
	mux.HandleFunc("POST /api/purchases/approvals/{id}/reject", command(apv, set, svc.RejectApproval.Handle))
	mux.HandleFunc("POST /api/purchases/approvals/{id}/cancel", command(apv, set, svc.CancelApproval.Handle))
}

// supplierStatus es 202 si el cambio ha dejado una solicitud de aprobación pendiente.
func supplierStatus(s uapp.SupplierDTO, ok int) int {
	if s.PendingApproval != "" {
		return http.StatusAccepted
	}
	return ok
}

var _ distribution.EndpointModule = (*Module)(nil)
