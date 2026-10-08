// Package hr composes the Human Resources bounded context on a hot-swap backend.
package hr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	happ "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	"github.com/jhermoso/karpo-fw-go/contexts/hr/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/hr/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service           *happ.Service
	Staff             contracts.Staff
	Positions         contracts.Positions
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
}

// Option configures the composition.
type Option func(*happ.Deps)

// WithOrganizations resolves units and affiliations with the Parties context.
func WithOrganizations(o happ.Organizations) Option {
	return func(d *happ.Deps) { d.Organizations = o }
}

// WithFacilities checks the facilities of work centers with the Facilities context.
func WithFacilities(f happ.Facilities) Option { return func(d *happ.Deps) { d.Facilities = f } }

// Compose builds the context on sw.
func Compose(sw *hotswap.Switch, opts ...Option) *Module {
	positions := hotswap.Repository(sw, infrastructure.PositionRepositoryFactory)
	employments := hotswap.Repository(sw, infrastructure.EmploymentRepositoryFactory)
	workCenters := hotswap.Repository(sw, infrastructure.WorkCenterRepositoryFactory)
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	deps := happ.Deps{
		Positions: positions, Employments: employments, WorkCenters: workCenters,
		Catalogs: catalogs{hotswap.Bind(sw, infrastructure.CatalogsFor)}, UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			happ.Publications(messaging.NewRecorder(contracts.Source, integration))),
	}
	for _, o := range opts {
		o(&deps)
	}
	return &Module{Service: happ.NewService(deps), Staff: happ.Staff{Employments: employments},
		Positions: happ.PositionDirectory{Positions: positions}, IntegrationOutbox: integration, Audit: audit}
}

// Relay forwards the Published Language to a transport.
func (m *Module) Relay(sender application.MessageSender, opts ...outbox.RelayOption) *outbox.Relay {
	return messaging.NewRelay(contracts.Source, m.IntegrationOutbox, sender, opts...)
}

type catalogs struct {
	b *hotswap.Binding[domain.Catalogs]
}

func with[T any](c catalogs, ctx context.Context, fn func(domain.Catalogs, context.Context) (T, error)) (out T, err error) {
	err = c.b.With(ctx, func(ctx context.Context, x domain.Catalogs) error { out, err = fn(x, ctx); return err })
	return out, err
}

func (c catalogs) PositionStatuses(ctx context.Context) ([]domain.PositionStatus, error) {
	return with(c, ctx, domain.Catalogs.PositionStatuses)
}

func (c catalogs) PositionClasses(ctx context.Context) ([]domain.PositionClass, error) {
	return with(c, ctx, domain.Catalogs.PositionClasses)
}

func (c catalogs) PositionTypes(ctx context.Context) ([]domain.PositionType, error) {
	return with(c, ctx, domain.Catalogs.PositionTypes)
}

func (c catalogs) Agreements(ctx context.Context) ([]domain.Agreement, error) {
	return with(c, ctx, domain.Catalogs.Agreements)
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

// command handles PUT/POST /{id}/...: it parses the path id, decodes the body into In and lets
// set place the id.
func command[ID any, In any, Out any](parse func(string) (ID, error), set func(*In, ID), run func(context.Context, In) (Out, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parse(r.PathValue("id"))
		if err != nil {
			distribution.WriteError(w, r, fmt.Errorf("%w: invalid id", fw.ErrValidation))
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

func create[In any, Out any](run func(context.Context, In) (Out, error)) http.HandlerFunc {
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

func get[ID any, Out any](parse func(string) (ID, error), run func(context.Context, ID) (Out, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parse(r.PathValue("id"))
		if err != nil {
			distribution.WriteError(w, r, fmt.Errorf("%w: invalid id", fw.ErrValidation))
			return
		}
		out, err := run(r.Context(), id)
		distribution.Respond(w, r, out, err, http.StatusOK)
	}
}

// RegisterRoutes implements distribution.EndpointModule.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	svc := m.Service
	pos, emp, wc := domain.ParsePositionID, domain.ParseEmploymentID, domain.ParseWorkCenterID
	query := func(r *http.Request) (func(string) string, func(string) int) {
		q := r.URL.Query()
		return q.Get, func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
	}

	// Positions.
	mux.HandleFunc("POST /api/hr/positions", create(svc.OpenPosition.Handle))
	mux.HandleFunc("GET /api/hr/positions", func(w http.ResponseWriter, r *http.Request) {
		s, n := query(r)
		page, err := svc.SearchPositions.Handle(r.Context(), happ.SearchPositions{Unit: s("unit"), Organization: s("organization"),
			Type: s("type"), Status: s("status"), Holder: s("holder"), VacantOnly: s("vacant") == "true", Page: n("page"), Size: n("size")})
		distribution.Respond(w, r, page, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/hr/positions/{id}", get(pos, func(ctx context.Context, id domain.PositionID) (happ.PositionDTO, error) {
		return svc.GetPosition.Handle(ctx, happ.GetPosition{ID: id})
	}))
	mux.HandleFunc("GET /api/hr/positions/{id}/chart", func(w http.ResponseWriter, r *http.Request) {
		id, err := pos(r.PathValue("id"))
		if err != nil {
			distribution.WriteError(w, r, fmt.Errorf("%w: invalid id", fw.ErrValidation))
			return
		}
		_, n := query(r)
		out, err := svc.OrgChart.Handle(r.Context(), happ.GetOrgChart{Root: id, Depth: n("depth")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("PUT /api/hr/positions/{id}/status", command(pos, func(c *happ.SetPositionStatus, id domain.PositionID) { c.ID = id }, svc.SetPositionStatus.Handle))
	mux.HandleFunc("POST /api/hr/positions/{id}/fill", command(pos, func(c *happ.FillPosition, id domain.PositionID) { c.ID = id }, svc.FillPosition.Handle))
	mux.HandleFunc("POST /api/hr/positions/{id}/vacate", command(pos, func(c *happ.VacatePosition, id domain.PositionID) { c.ID = id }, svc.VacatePosition.Handle))
	mux.HandleFunc("POST /api/hr/positions/{id}/reports-to", command(pos, func(c *happ.ReportTo, id domain.PositionID) { c.ID = id }, svc.ReportTo.Handle))
	mux.HandleFunc("POST /api/hr/positions/{id}/reports-to/end", command(pos, func(c *happ.EndReporting, id domain.PositionID) { c.ID = id }, svc.EndReporting.Handle))
	mux.HandleFunc("POST /api/hr/positions/{id}/close", command(pos, func(c *happ.ClosePosition, id domain.PositionID) { c.ID = id }, svc.ClosePosition.Handle))

	// Employments.
	mux.HandleFunc("POST /api/hr/employments", create(svc.Hire.Handle))
	mux.HandleFunc("GET /api/hr/employments", func(w http.ResponseWriter, r *http.Request) {
		s, n := query(r)
		page, err := svc.SearchEmployments.Handle(r.Context(), happ.SearchEmployments{Employer: s("employer"), Person: s("person"),
			Number: s("number"), ActiveOnly: s("active") == "true", Page: n("page"), Size: n("size")})
		distribution.Respond(w, r, page, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/hr/employments/{id}", get(emp, func(ctx context.Context, id domain.EmploymentID) (happ.EmploymentDTO, error) {
		return svc.GetEmployment.Handle(ctx, happ.GetEmployment{ID: id})
	}))
	mux.HandleFunc("POST /api/hr/employments/{id}/contracts", command(emp, func(c *happ.AddContract, id domain.EmploymentID) { c.ID = id }, svc.AddContract.Handle))
	mux.HandleFunc("POST /api/hr/employments/{id}/contracts/end", command(emp, func(c *happ.EndContract, id domain.EmploymentID) { c.ID = id }, svc.EndContract.Handle))
	mux.HandleFunc("POST /api/hr/employments/{id}/terminate", command(emp, func(c *happ.Terminate, id domain.EmploymentID) { c.ID = id }, svc.Terminate.Handle))
	mux.HandleFunc("PUT /api/hr/employments/{id}/classification", command(emp, func(c *happ.SetClassification, id domain.EmploymentID) { c.ID = id }, svc.SetClassification.Handle))

	// Work centers.
	mux.HandleFunc("POST /api/hr/work-centers", create(svc.OpenWorkCenter.Handle))
	mux.HandleFunc("GET /api/hr/work-centers", func(w http.ResponseWriter, r *http.Request) {
		s, n := query(r)
		page, err := svc.SearchWorkCenters.Handle(r.Context(), happ.SearchWorkCenters{Employer: s("employer"), OpenOnly: s("open") == "true",
			Page: n("page"), Size: n("size")})
		distribution.Respond(w, r, page, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/hr/work-centers/{id}", get(wc, func(ctx context.Context, id domain.WorkCenterID) (happ.WorkCenterDTO, error) {
		return svc.GetWorkCenter.Handle(ctx, happ.GetWorkCenter{ID: id})
	}))
	mux.HandleFunc("POST /api/hr/work-centers/{id}/headquarters", command(wc, func(c *happ.SetHeadquarters, id domain.WorkCenterID) { c.ID = id }, svc.SetHeadquarters.Handle))
	mux.HandleFunc("POST /api/hr/work-centers/{id}/close", command(wc, func(c *happ.CloseWorkCenter, id domain.WorkCenterID) { c.ID = id }, svc.CloseWorkCenter.Handle))

	// Catalogs.
	mux.HandleFunc("GET /api/hr/catalogs/position-types", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.PositionTypes.Handle(r.Context(), happ.ListPositionTypes{ActiveOnly: r.URL.Query().Get("active") == "true"})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/hr/catalogs/position-statuses", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.PositionStatuses.Handle(r.Context(), happ.ListPositionStatuses{})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/hr/catalogs/position-classes", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.PositionClasses.Handle(r.Context(), happ.ListPositionClasses{})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/hr/catalogs/agreements", func(w http.ResponseWriter, r *http.Request) {
		s, _ := query(r)
		out, err := svc.Agreements.Handle(r.Context(), happ.ListAgreements{Organization: s("organization"), InForceOn: s("on")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})

	// Query ports for other contexts (they serve services, not users).
	mux.Handle("POST /api/hr/staff/employments", distribution.RequirePermission(happ.PermEmploymentRead, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				PersonIDs []string `json:"personIds"`
				Date      string   `json:"date"`
			}
			if err := decode(r, &body); err != nil {
				distribution.WriteError(w, r, err)
				return
			}
			out, err := m.Staff.EmploymentsOn(r.Context(), body.PersonIDs, body.Date)
			distribution.Respond(w, r, out, err, http.StatusOK)
		})))
	mux.Handle("POST /api/hr/positions/directory/resolve", distribution.RequirePermission(happ.PermPositionRead, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				PositionIDs []string `json:"positionIds"`
			}
			if err := decode(r, &body); err != nil {
				distribution.WriteError(w, r, err)
				return
			}
			out, err := m.Positions.Resolve(r.Context(), body.PositionIDs)
			distribution.Respond(w, r, out, err, http.StatusOK)
		})))
}

var _ distribution.EndpointModule = (*Module)(nil)
