// Package assets composes the Assets (Activos, inmovilizado) bounded context on a hot-swap backend.
package assets

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	uapp "github.com/jhermoso/karpo-fw-go/contexts/assets/application"
	"github.com/jhermoso/karpo-fw-go/contexts/assets/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/assets/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/assets/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service           *uapp.Service
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
}

// Compose builds the context on sw.
func Compose(sw *hotswap.Switch) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	svc := uapp.NewService(uapp.Deps{
		Assets: hotswap.Repository(sw, infrastructure.AssetRepositoryFactory), UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			uapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
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

func command[In, Out any](set func(*In, domain.AssetID), run func(context.Context, In) (Out, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := domain.ParseAssetID(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		var c In
		if err := decode(r, &c); err != nil {
			distribution.WriteError(w, r, err)
			return
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

	mux.HandleFunc("POST /api/assets", create(http.StatusCreated, svc.Register.Handle))
	mux.HandleFunc("GET /api/assets", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := svc.SearchAssets.Handle(r.Context(), uapp.SearchAssets{Company: q.Get("company"), Class: q.Get("class"),
			InService: q.Get("inService") == "true", Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/assets/book", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.Book.Handle(r.Context(), uapp.GetBook{Company: r.URL.Query().Get("company")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/assets/depreciation", create(http.StatusOK, svc.Depreciate.Handle))
	mux.HandleFunc("GET /api/assets/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := domain.ParseAssetID(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := svc.GetAsset.Handle(r.Context(), uapp.GetAsset{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("PUT /api/assets/{id}", command(func(c *uapp.ChangeAsset, id domain.AssetID) { c.ID = id }, svc.Change.Handle))
	mux.HandleFunc("POST /api/assets/{id}/dispose", command(func(c *uapp.DisposeAsset, id domain.AssetID) { c.ID = id }, svc.Dispose.Handle))
}

var _ distribution.EndpointModule = (*Module)(nil)
