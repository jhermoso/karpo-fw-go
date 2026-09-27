// Package facilities composes the Facilities bounded context on a hot-swap backend.
package facilities

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	fapp "github.com/jhermoso/karpo-fw-go/contexts/facilities/application"
	"github.com/jhermoso/karpo-fw-go/contexts/facilities/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/facilities/infrastructure"
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
	Directory         contracts.Directory
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
}

// Option configures the composition.
type Option func(*fapp.Deps)

// WithAddressChecker validates locations with the Geography context.
func WithAddressChecker(c fapp.AddressChecker) Option { return func(d *fapp.Deps) { d.Addresses = c } }

// Compose builds the context on sw.
func Compose(sw *hotswap.Switch, opts ...Option) *Module {
	repo := hotswap.Repository(sw, infrastructure.RepositoryFactory)
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	deps := fapp.Deps{
		Facilities: repo, Catalogs: catalogs{hotswap.Bind(sw, infrastructure.CatalogsFor)}, UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			fapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	}
	for _, o := range opts {
		o(&deps)
	}
	return &Module{Service: fapp.NewService(deps), Directory: fapp.Directory{Facilities: repo}, IntegrationOutbox: integration, Audit: audit}
}

// Relay forwards the Published Language to a transport.
func (m *Module) Relay(sender application.MessageSender, opts ...outbox.RelayOption) *outbox.Relay {
	return messaging.NewRelay(contracts.Source, m.IntegrationOutbox, sender, opts...)
}

type catalogs struct {
	b *hotswap.Binding[domain.Catalogs]
}

func (c catalogs) FacilityTypes(ctx context.Context) (out []domain.FacilityType, err error) {
	err = c.b.With(ctx, func(ctx context.Context, x domain.Catalogs) error { out, err = x.FacilityTypes(ctx); return err })
	return out, err
}

// RegisterRoutes implements distribution.EndpointModule.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	svc := m.Service
	id := func(r *http.Request) (domain.FacilityID, error) {
		f, err := domain.ParseFacilityID(r.PathValue("id"))
		if err != nil {
			return f, fmt.Errorf("%w: invalid facility id", fw.ErrValidation)
		}
		return f, nil
	}
	decode := func(r *http.Request, v any) error {
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(v); err != nil {
			var val fw.Validation
			val.Add("body", "json", err.Error())
			return val.Err()
		}
		return nil
	}
	update := func(fn func(r *http.Request, id domain.FacilityID) (fapp.FacilityDTO, error)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			fid, err := id(r)
			if err != nil {
				distribution.WriteError(w, r, err)
				return
			}
			dto, err := fn(r, fid)
			distribution.Respond(w, r, dto, err, http.StatusOK)
		}
	}
	mux.HandleFunc("POST /api/facilities", func(w http.ResponseWriter, r *http.Request) {
		var c fapp.RegisterFacility
		if err := decode(r, &c); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		dto, err := svc.Register.Handle(r.Context(), c)
		distribution.Respond(w, r, dto, err, http.StatusCreated)
	})
	mux.HandleFunc("GET /api/facilities", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		page, err := svc.Search.Handle(r.Context(), fapp.SearchFacilities{Text: q.Get("q"), Type: q.Get("type"),
			Organization: q.Get("organization"), PartOf: q.Get("partOf"), ActiveOnly: q.Get("active") == "true", Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, page, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/facilities/{id}", update(func(r *http.Request, id domain.FacilityID) (fapp.FacilityDTO, error) {
		return svc.Get.Handle(r.Context(), fapp.GetFacility{ID: id})
	}))
	mux.HandleFunc("PUT /api/facilities/{id}/name", update(func(r *http.Request, id domain.FacilityID) (fapp.FacilityDTO, error) {
		c := fapp.RenameFacility{}
		if err := decode(r, &c); err != nil {
			return fapp.FacilityDTO{}, err
		}
		c.ID = id
		return svc.Rename.Handle(r.Context(), c)
	}))
	mux.HandleFunc("PUT /api/facilities/{id}/location", update(func(r *http.Request, id domain.FacilityID) (fapp.FacilityDTO, error) {
		c := fapp.RelocateFacility{}
		if err := decode(r, &c); err != nil {
			return fapp.FacilityDTO{}, err
		}
		c.ID = id
		return svc.Relocate.Handle(r.Context(), c)
	}))
	mux.HandleFunc("PUT /api/facilities/{id}/parent", update(func(r *http.Request, id domain.FacilityID) (fapp.FacilityDTO, error) {
		c := fapp.MoveFacility{}
		if err := decode(r, &c); err != nil {
			return fapp.FacilityDTO{}, err
		}
		c.ID = id
		return svc.Move.Handle(r.Context(), c)
	}))
	mux.HandleFunc("PUT /api/facilities/{id}/active", update(func(r *http.Request, id domain.FacilityID) (fapp.FacilityDTO, error) {
		c := fapp.SetFacilityActive{}
		if err := decode(r, &c); err != nil {
			return fapp.FacilityDTO{}, err
		}
		c.ID = id
		return svc.SetActive.Handle(r.Context(), c)
	}))
	mux.HandleFunc("GET /api/catalogs/facility-types", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.Types.Handle(r.Context(), fapp.ListFacilityTypes{})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.Handle("POST /api/facilities/directory/resolve", distribution.RequirePermission(fapp.PermRead, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				FacilityIDs []string `json:"facilityIds"`
			}
			if err := decode(r, &body); err != nil {
				distribution.WriteError(w, r, err)
				return
			}
			out, err := m.Directory.Resolve(r.Context(), body.FacilityIDs)
			distribution.Respond(w, r, out, err, http.StatusOK)
		})))
}

var _ distribution.EndpointModule = (*Module)(nil)
