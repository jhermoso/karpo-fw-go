// Package imports composes the Imports (Importación de datos) bounded context on a hot-swap
// backend.
package imports

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	iapp "github.com/jhermoso/karpo-fw-go/contexts/imports/application"
	"github.com/jhermoso/karpo-fw-go/contexts/imports/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/imports/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/imports/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// MaxBody bounds a request with files: their 5 MB and the JSON around them.
const MaxBody = domain.MaxFileBytes + 1<<20

// Module is the composed context.
type Module struct {
	Service           *iapp.Service
	Registry          *iapp.Registry
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
}

// Compose builds the context on sw, with the sources it ships (Personio). The host adds its own
// with Offer and says who writes each kind of record with Load.
func Compose(sw *hotswap.Switch) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	registry := iapp.NewRegistry()
	registry.Offer(domain.Personio{})
	svc := iapp.NewService(iapp.Deps{
		Runs: hotswap.Repository(sw, infrastructure.RunRepositoryFactory), References: hotswap.Repository(sw, infrastructure.ReferenceRepositoryFactory),
		Registry: registry, UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			iapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	})
	return &Module{Service: svc, Registry: registry, IntegrationOutbox: integration, Audit: audit}
}

// Offer adds sources of the host.
func (m *Module) Offer(sources ...domain.Source) *Module {
	m.Registry.Offer(sources...)
	return m
}

// Load says who writes each kind of record.
func (m *Module) Load(loaders ...domain.Loader) *Module {
	m.Registry.Load(loaders...)
	return m
}

// Relay forwards the Published Language to a transport.
func (m *Module) Relay(sender application.MessageSender, opts ...outbox.RelayOption) *outbox.Relay {
	return messaging.NewRelay(contracts.Source, m.IntegrationOutbox, sender, opts...)
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var val fw.Validation
		val.Add("body", "json", err.Error())
		return val.Err()
	}
	return nil
}

// RegisterRoutes implements distribution.EndpointModule.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	svc := m.Service
	query := func(run func(r *http.Request) (any, error)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			out, err := run(r)
			distribution.Respond(w, r, out, err, http.StatusOK)
		}
	}
	mux.HandleFunc("GET /api/imports/sources", query(func(r *http.Request) (any, error) {
		return svc.Sources.Handle(r.Context(), iapp.ListSources{})
	}))
	mux.HandleFunc("POST /api/imports/preview", func(w http.ResponseWriter, r *http.Request) {
		var c iapp.RunImport
		if err := decode(w, r, &c); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		out, err := svc.Preview.Handle(r.Context(), c)
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/imports/runs", func(w http.ResponseWriter, r *http.Request) {
		var c iapp.RunImport
		if err := decode(w, r, &c); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		out, err := svc.Execute.Handle(r.Context(), c)
		distribution.Respond(w, r, out, err, http.StatusCreated)
	})
	mux.HandleFunc("POST /api/imports/runs/close-stale", func(w http.ResponseWriter, r *http.Request) {
		var c iapp.CloseStale
		if r.ContentLength != 0 {
			if err := decode(w, r, &c); err != nil {
				distribution.WriteError(w, r, err)
				return
			}
		}
		out, err := svc.CloseStale.Handle(r.Context(), c)
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/imports/runs", query(func(r *http.Request) (any, error) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		return svc.SearchRuns.Handle(r.Context(), iapp.SearchRuns{Source: q.Get("source"), Status: q.Get("status"), Page: atoi("page"), Size: atoi("size")})
	}))
	mux.HandleFunc("GET /api/imports/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := domain.ParseRunID(r.PathValue("id"))
		if err != nil || id.IsZero() {
			distribution.WriteError(w, r, fmt.Errorf("%w: invalid id", fw.ErrValidation))
			return
		}
		out, err := svc.GetRun.Handle(r.Context(), iapp.GetRun{ID: id})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/imports/references", query(func(r *http.Request) (any, error) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		return svc.References.Handle(r.Context(), iapp.SearchReferences{Source: q.Get("source"), Kind: q.Get("kind"), Scope: q.Get("scope"), Key: q.Get("key"),
			Entity: q.Get("entity"), Page: atoi("page"), Size: atoi("size")})
	}))
}

var _ distribution.EndpointModule = (*Module)(nil)
