// Package application contiene los casos de uso de Recursos Humanos (con permisos y ámbito de
// organización), los puertos que RRHH consume de Parties y Facilities, sus puertos de consulta
// para otros contextos y la traducción a su Published Language.
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

// Permisos (los recursos RRHH/Position, RRHH/Employee, RRHH/WorkCenter y catálogos del C#).
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

// MaxDepth acota las cadenas de dependencia que se recorren (comprobación de ciclos y
// organigramas).
const MaxDepth = 20

// chunk es el tamaño de las listas IN de las consultas nivel a nivel.
const chunk = 500

// Organizations es lo que RRHH necesita de Parties (el puerto es de RRHH; un adaptador lo
// implementa sobre los contratos de Parties).
type Organizations interface {
	// InternalOrganizationOf devuelve la organización interna a la que pertenece cada unidad
	// (departamento, división...); una organización interna se corresponde consigo misma, las
	// unidades desconocidas no aparecen.
	InternalOrganizationOf(ctx context.Context, units []domain.OrganizationID) (map[domain.OrganizationID]domain.OrganizationID, error)
	// Affiliated dice si la persona está afiliada ahora a la organización.
	Affiliated(ctx context.Context, person domain.PersonID, org domain.OrganizationID) (bool, error)
}

// FacilityInfo es lo que RRHH necesita de una instalación.
type FacilityInfo struct {
	Name   string
	Owner  domain.OrganizationID
	Active bool
}

// Facilities es lo que RRHH necesita del contexto Facilities.
type Facilities interface {
	Facility(ctx context.Context, id domain.FacilityID) (FacilityInfo, bool, error)
}

// Deps son los puertos que necesitan los casos de uso. Recorder y Audit son opcionales; sin
// Organizations una unidad es su propia organización y no se comprueba la afiliación, sin
// Facilities no se comprueba la instalación de un centro de trabajo (uso independiente).
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

// Service expone los casos de uso.
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

// scope: los puestos pertenecen a su organización interna, las relaciones laborales y los centros
// de trabajo a su empleador; son visibles dentro del ámbito de esa organización (un 404 uniforme
// fuera de él) y se pueden escribir con una concesión Full sobre ella.
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

// check devuelve un 404 uniforme fuera del ámbito y un 403 cuando el ámbito es de sólo lectura.
func (s scope) check(kind string, id fmt.Stringer, org domain.OrganizationID, write bool) error {
	if !s.sees(org) {
		return fw.NotFound(kind, id)
	}
	if write && !s.writes(org) {
		return fmt.Errorf("%w: read-only in your organization scope", fw.ErrForbidden)
	}
	return nil
}

// within restringe una consulta al ámbito mediante el campo de organización de un agregado.
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

// at devuelve el momento de un comando (ahora, si no lo trae).
func at(t *time.Time) time.Time {
	if t == nil || t.IsZero() {
		return fw.Now()
	}
	return t.UTC()
}

// NewService monta los casos de uso.
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
