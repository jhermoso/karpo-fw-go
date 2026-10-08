// Package application holds the Human Resources use cases (with permissions and organization
// scope), the ports HR consumes from Parties and Facilities, its query ports for other contexts
// and the translation to its Published Language.
package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Permissions (the C# resources RRHH/Position, RRHH/Employee, RRHH/WorkCenter and catalogs).
var (
	PermPositionRead     = authz.MustPermission("HR.Position.Read")
	PermPositionCreate   = authz.MustPermission("HR.Position.Create")
	PermPositionUpdate   = authz.MustPermission("HR.Position.Update")
	PermEmploymentRead   = authz.MustPermission("HR.Employment.Read")
	PermEmploymentCreate = authz.MustPermission("HR.Employment.Create")
	PermEmploymentUpdate = authz.MustPermission("HR.Employment.Update")
	PermWorkCenterRead   = authz.MustPermission("HR.WorkCenter.Read")
	PermWorkCenterCreate = authz.MustPermission("HR.WorkCenter.Create")
	PermWorkCenterUpdate = authz.MustPermission("HR.WorkCenter.Update")
	PermCatalogRead      = authz.MustPermission("HR.Catalog.Read")
)

// MaxDepth bounds the reporting chains walked (cycle checks and org charts).
const MaxDepth = 20

// chunk is the size of the IN lists of the level-by-level queries.
const chunk = 500

// Organizations is what HR needs of Parties (the port HR owns; an adapter implements it over the
// Parties contracts).
type Organizations interface {
	// InternalOrganizationOf returns the internal organization each unit (department, division...)
	// belongs to; an internal organization maps to itself, unknown units are absent.
	InternalOrganizationOf(ctx context.Context, units []domain.OrganizationID) (map[domain.OrganizationID]domain.OrganizationID, error)
	// Affiliated reports whether the person is affiliated with the organization now.
	Affiliated(ctx context.Context, person domain.PersonID, org domain.OrganizationID) (bool, error)
}

// FacilityInfo is what HR needs of a facility.
type FacilityInfo struct {
	Name   string
	Owner  domain.OrganizationID
	Active bool
}

// Facilities is what HR needs of the Facilities context.
type Facilities interface {
	Facility(ctx context.Context, id domain.FacilityID) (FacilityInfo, bool, error)
}

// Deps are the ports the use cases need. Recorder and Audit are optional; without Organizations
// a unit is its own organization and affiliation is not checked, without Facilities the
// facility of a work center is not checked (standalone use).
type Deps struct {
	Positions     domain.PositionRepository
	Employments   domain.EmploymentRepository
	WorkCenters   domain.WorkCenterRepository
	Catalogs      domain.Catalogs
	UoW           fw.UnitOfWork
	Recorder      app.EventRecorder
	Audit         app.AuditLog
	Organizations Organizations
	Facilities    Facilities
}

// Service exposes the use cases.
type Service struct {
	OpenPosition      app.CommandHandler[OpenPosition, PositionDTO]
	SetPositionStatus app.CommandHandler[SetPositionStatus, PositionDTO]
	FillPosition      app.CommandHandler[FillPosition, PositionDTO]
	VacatePosition    app.CommandHandler[VacatePosition, PositionDTO]
	ReportTo          app.CommandHandler[ReportTo, PositionDTO]
	EndReporting      app.CommandHandler[EndReporting, PositionDTO]
	ClosePosition     app.CommandHandler[ClosePosition, PositionDTO]
	GetPosition       app.QueryHandler[GetPosition, PositionDTO]
	SearchPositions   app.QueryHandler[SearchPositions, fw.Page[PositionDTO]]
	OrgChart          app.QueryHandler[GetOrgChart, ChartNode]

	Hire              app.CommandHandler[Hire, EmploymentDTO]
	AddContract       app.CommandHandler[AddContract, EmploymentDTO]
	EndContract       app.CommandHandler[EndContract, EmploymentDTO]
	Terminate         app.CommandHandler[Terminate, EmploymentDTO]
	SetClassification app.CommandHandler[SetClassification, EmploymentDTO]
	GetEmployment     app.QueryHandler[GetEmployment, EmploymentDTO]
	SearchEmployments app.QueryHandler[SearchEmployments, fw.Page[EmploymentDTO]]

	OpenWorkCenter    app.CommandHandler[OpenWorkCenter, WorkCenterDTO]
	SetHeadquarters   app.CommandHandler[SetHeadquarters, WorkCenterDTO]
	CloseWorkCenter   app.CommandHandler[CloseWorkCenter, WorkCenterDTO]
	GetWorkCenter     app.QueryHandler[GetWorkCenter, WorkCenterDTO]
	SearchWorkCenters app.QueryHandler[SearchWorkCenters, fw.Page[WorkCenterDTO]]

	PositionTypes    app.QueryHandler[ListPositionTypes, []domain.PositionType]
	PositionStatuses app.QueryHandler[ListPositionStatuses, []domain.PositionStatus]
	PositionClasses  app.QueryHandler[ListPositionClasses, []domain.PositionClass]
	Agreements       app.QueryHandler[ListAgreements, []AgreementDTO]
}

// scope: positions belong to their internal organization, employments and work centers to their
// employer; they are visible inside that organization's scope (uniform 404 elsewhere) and
// writable with a Full grant on it.
type scope struct {
	global bool
	ac     *authz.Context
	orgs   []domain.OrganizationID
}

func scopeOf(ctx context.Context) scope {
	ac, ok := authz.FromContext(ctx)
	if !ok {
		return scope{}
	}
	s := scope{global: ac.GlobalAdmin, ac: ac}
	for _, u := range ac.EffectiveOrganizations {
		s.orgs = append(s.orgs, domain.OrganizationID{UUID: u})
	}
	return s
}

func (s scope) sees(org domain.OrganizationID) bool { return s.global || slices.Contains(s.orgs, org) }

func (s scope) writes(org domain.OrganizationID) bool {
	return s.global || (s.ac != nil && s.ac.CanWrite(org.UUID))
}

// check returns a uniform 404 outside the scope and 403 when the scope is read-only.
func (s scope) check(kind string, id fmt.Stringer, org domain.OrganizationID, write bool) error {
	if !s.sees(org) {
		return fw.NotFound(kind, id)
	}
	if write && !s.writes(org) {
		return fmt.Errorf("%w: read-only in your organization scope", fw.ErrForbidden)
	}
	return nil
}

// within restricts a query to the scope through the organization field of an aggregate.
func within[T any](s scope, field spec.Field[T, domain.OrganizationID]) spec.Spec[T] {
	switch {
	case s.global:
		return spec.All[T]()
	case len(s.orgs) == 0:
		return spec.None[T]()
	}
	return field.In(s.orgs...)
}

type service struct {
	Deps
	positions   *orchestration.Orchestrator[domain.PositionID, *domain.Position]
	employments *orchestration.Orchestrator[domain.EmploymentID, *domain.Employment]
	workCenters *orchestration.Orchestrator[domain.WorkCenterID, *domain.WorkCenter]
}

func parseID(v *fw.Validation, field, s string) fw.UUID {
	u, err := fw.ParseUUID(s)
	v.Require(err == nil && !u.IsZero(), field, "format", field+" must be an id")
	return u
}

// at returns the moment of a command (now when absent).
func at(t *time.Time) time.Time {
	if t == nil || t.IsZero() {
		return fw.Now()
	}
	return t.UTC()
}

// NewService wires the use cases.
func NewService(d Deps) *Service {
	var opts []orchestration.Option
	if d.Recorder != nil {
		opts = append(opts, orchestration.WithOutbox(d.Recorder))
	}
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	s := service{Deps: d,
		positions:   orchestration.New[domain.PositionID, *domain.Position](d.Positions, d.UoW, opts...),
		employments: orchestration.New[domain.EmploymentID, *domain.Employment](d.Employments, d.UoW, opts...),
		workCenters: orchestration.New[domain.WorkCenterID, *domain.WorkCenter](d.WorkCenters, d.UoW, opts...),
	}
	svc := &Service{}
	s.positionUseCases(svc)
	s.employmentUseCases(svc)
	s.workCenterUseCases(svc)
	s.catalogUseCases(svc)
	return svc
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func retry[In, Out any]() app.Middleware[In, Out] {
	return pipeline.RetryOnConflict[In, Out](3, 10*time.Millisecond)
}

func (s service) tx() fw.UnitOfWork { return s.UoW }
