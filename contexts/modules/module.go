// Package modules composes the Modules (Módulos y capacidades: qué tiene activado cada empresa)
// bounded context on a hot-swap backend.
package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	mapp "github.com/jhermoso/karpo-fw-go/contexts/modules/application"
	"github.com/jhermoso/karpo-fw-go/contexts/modules/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/modules/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/modules/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service           *mapp.Service
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
	// Features is the port other contexts ask whether a company has something on.
	Features contracts.Features
	deps     mapp.Deps
}

// Option configures the composition.
type Option func(*mapp.Deps)

// WithDerivation tells which features a company has because of what it is (the financial
// capability of a financial institution) instead of because somebody switched them on.
func WithDerivation(d mapp.Derivation) Option { return func(deps *mapp.Deps) { deps.Derivation = d } }

// Compose builds the context on sw.
func Compose(sw *hotswap.Switch, opts ...Option) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	d := mapp.Deps{
		Features: hotswap.Repository(sw, infrastructure.FeatureRepositoryFactory), Activations: hotswap.Repository(sw, infrastructure.ActivationRepositoryFactory),
		UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			mapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	}
	for _, o := range opts {
		o(&d)
	}
	return &Module{Service: mapp.NewService(d), IntegrationOutbox: integration, Audit: audit,
		Features: mapp.Features{Activations: d.Activations, Derivation: d.Derivation}, deps: d}
}

// EnsureCatalog adds to the catalog the seed entries it lacks; the host calls it at start-up.
func (m *Module) EnsureCatalog(ctx context.Context) (int, error) {
	return mapp.EnsureCatalog(ctx, m.deps)
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

func post[In, Out any](status int, run func(context.Context, In) (Out, error)) http.HandlerFunc {
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

	mux.HandleFunc("GET /api/modules/current", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.Current.Handle(r.Context(), mapp.GetCurrent{})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/modules/catalog", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := svc.Catalog.Handle(r.Context(), mapp.ListCatalog{Kind: q.Get("kind"), Retired: q.Get("retired") == "true"})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/modules/catalog", post(http.StatusCreated, svc.Define.Handle))
	mux.HandleFunc("PUT /api/modules/catalog/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := domain.ParseFeatureID(r.PathValue("id"))
		if err != nil {
			distribution.WriteError(w, r, fmt.Errorf("%w: invalid id", fw.ErrValidation))
			return
		}
		var c mapp.ChangeFeature
		if err := decode(r, &c); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		c.ID = id
		out, err := svc.Change.Handle(r.Context(), c)
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/modules/activations", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.Of.Handle(r.Context(), mapp.ListActivations{Organization: r.URL.Query().Get("organization")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/modules/organizations", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := svc.Organizations.Handle(r.Context(), mapp.ListOrganizations{Kind: q.Get("kind"), Code: q.Get("code")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/modules/activate", post(http.StatusOK, svc.Activate.Handle))
	mux.HandleFunc("POST /api/modules/deactivate", post(http.StatusOK, svc.Deactivate.Handle))
}

var _ distribution.EndpointModule = (*Module)(nil)
