// Package application contiene los casos de uso de Payroll (con permisos y ámbito de empleador),
// el puerto que Payroll consume de HR, su puerto Remittance y la traducción a su Published
// Language.
package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/payroll/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permisos (los RRHH.Payslip.* y RRHH.PayslipLine.* de C#; los endpoints de nómina del lado de
// Parties no tenían ninguno). Aprobar es un permiso propio, aparte de preparar.
var (
	PermPayslipRead    = authz.MustPermission("Payroll.Payslip.Read")
	PermPayslipCreate  = authz.MustPermission("Payroll.Payslip.Create")
	PermPayslipUpdate  = authz.MustPermission("Payroll.Payslip.Update")
	PermPayslipApprove = authz.MustPermission("Payroll.Payslip.Approve")
	PermProfileRead    = authz.MustPermission("Payroll.Profile.Read")
	PermProfileUpdate  = authz.MustPermission("Payroll.Profile.Update")
	PermAccountRead    = authz.MustPermission("Payroll.EmployerAccount.Read")
	PermAccountCreate  = authz.MustPermission("Payroll.EmployerAccount.Create")
	PermAccountUpdate  = authz.MustPermission("Payroll.EmployerAccount.Update")
	PermCatalogRead    = authz.MustPermission("Payroll.Catalog.Read")
	PermRemittanceRead = authz.MustPermission("Payroll.Remittance.Read")
)

// EmploymentInfo es lo que Payroll necesita de una relación laboral de HR.
type EmploymentInfo struct {
	ID         domain.EmploymentID
	Number     string
	Agreement  domain.AgreementID
	WorkCenter domain.WorkCenterID
}

// Employments es lo que Payroll necesita de HR (el puerto propio de Payroll; un adaptador lo
// implementa sobre el contrato Staff de HR).
type Employments interface {
	// EmploymentOn devuelve la relación laboral de la persona con el empleador activa en la fecha,
	// con su contrato principal en esa fecha.
	EmploymentOn(ctx context.Context, person domain.PersonID, employer domain.OrganizationID, on vocab.Date) (EmploymentInfo, bool, error)
}

// Deps son los puertos que necesitan los casos de uso; Recorder y Audit son opcionales.
type Deps struct {
	Payslips    domain.PayslipRepository
	Profiles    domain.ProfileRepository
	Accounts    domain.EmployerAccountRepository
	Catalogs    domain.Catalogs
	Employments Employments
	UoW         fw.UnitOfWork
	Recorder    app.EventRecorder
	Audit       app.AuditLog
	Merged      app.MergedParties // opcional: las parties fusionadas, que los listados dejan fuera (D-100)
}

// withoutMerged deja fuera de un listado los registros cuya party (empleado) es una party fusionada,
// salvo que se pidan con includeMergedParties (docs/DECISIONES-COMUNES.md, D-100).
func withoutMerged[T any](ctx context.Context, m app.MergedParties, include bool, f spec.Field[T, domain.PersonID]) ([]spec.Specification[T], error) {
	return app.WithoutMerged(ctx, m, include, f, func(u fw.UUID) domain.PersonID { return domain.PersonID{UUID: u} })
}

// Service expone los casos de uso.
type Service struct {
	DraftPayslip   app.CommandHandler[DraftPayslip, PayslipDTO]
	AddLine        app.CommandHandler[AddLine, PayslipDTO]
	RemoveLine     app.CommandHandler[RemoveLine, PayslipDTO]
	Reschedule     app.CommandHandler[Reschedule, PayslipDTO]
	Approve        app.CommandHandler[ApprovePayslip, PayslipDTO]
	Cancel         app.CommandHandler[CancelPayslip, PayslipDTO]
	Discard        app.CommandHandler[DiscardPayslip, struct{}]
	GetPayslip     app.QueryHandler[GetPayslip, PayslipDTO]
	SearchPayslips app.QueryHandler[SearchPayslips, fw.Page[PayslipDTO]]

	OpenProfile    app.CommandHandler[OpenProfile, ProfileDTO]
	SetTerms       app.CommandHandler[SetTerms, ProfileDTO]
	AddSplit       app.CommandHandler[AddSplit, ProfileDTO]
	EndSplit       app.CommandHandler[EndSplit, ProfileDTO]
	GetProfile     app.QueryHandler[GetProfile, ProfileDTO]
	SearchProfiles app.QueryHandler[SearchProfiles, fw.Page[ProfileDTO]]

	RegisterAccount   app.CommandHandler[RegisterAccount, AccountDTO]
	SetAccountPayment app.CommandHandler[SetAccountPayment, AccountDTO]
	DeactivateAccount app.CommandHandler[DeactivateAccount, AccountDTO]
	GetAccount        app.QueryHandler[GetAccount, AccountDTO]
	SearchAccounts    app.QueryHandler[SearchAccounts, fw.Page[AccountDTO]]

	Concepts app.QueryHandler[ListConcepts, []ConceptDTO]
}

// scope: todo pertenece a su empleador; visible dentro del ámbito del empleador (404 uniforme en
// otro caso) y modificable con un permiso Full sobre él.
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

func (s scope) check(kind string, id fmt.Stringer, org domain.OrganizationID, write bool) error {
	if !s.sees(org) {
		return fw.NotFound(kind, id)
	}
	if write && !s.global && (s.ac == nil || !s.ac.CanWrite(org.UUID)) {
		return fmt.Errorf("%w: read-only in your organization scope", fw.ErrForbidden)
	}
	return nil
}

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
	payslips *orchestration.Orchestrator[domain.PayslipID, *domain.Payslip]
	profiles *orchestration.Orchestrator[domain.ProfileID, *domain.Profile]
	accounts *orchestration.Orchestrator[domain.EmployerAccountID, *domain.EmployerAccount]
}

func parseID(v *fw.Validation, field, s string) fw.UUID {
	u, err := fw.ParseUUID(s)
	v.Require(err == nil && !u.IsZero(), field, "format", field+" must be an id")
	return u
}

func parseDecimal(v *fw.Validation, field, s string) vocab.Decimal {
	if s == "" {
		return vocab.DecimalFromInt(0)
	}
	d, err := vocab.ParseDecimal(s)
	v.Require(err == nil, field, "format", field+" must be a decimal number")
	return d
}

func parseIBAN(v *fw.Validation, field, s string) vocab.IBAN {
	if s == "" {
		return vocab.IBAN{}
	}
	i, err := vocab.NewIBAN(s)
	v.Merge(field, err)
	return i
}

func decimalText(d vocab.Decimal) string {
	if d.IsZero() {
		return ""
	}
	return d.StringFixed(2)
}

func dateText(d vocab.Date) string {
	if d.IsZero() {
		return ""
	}
	return d.String()
}

// NewService conecta los casos de uso.
func NewService(d Deps) *Service {
	var opts []orchestration.Option
	if d.Recorder != nil {
		opts = append(opts, orchestration.WithOutbox(d.Recorder))
	}
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	s := service{Deps: d,
		payslips: orchestration.New[domain.PayslipID, *domain.Payslip](d.Payslips, d.UoW, opts...),
		profiles: orchestration.New[domain.ProfileID, *domain.Profile](d.Profiles, d.UoW, opts...),
		accounts: orchestration.New[domain.EmployerAccountID, *domain.EmployerAccount](d.Accounts, d.UoW, opts...),
	}
	svc := &Service{}
	s.payslipUseCases(svc)
	s.profileUseCases(svc)
	s.accountUseCases(svc)
	svc.Concepts = guard(PermCatalogRead, func(ctx context.Context, q ListConcepts) ([]ConceptDTO, error) {
		cs, err := d.Catalogs.Concepts(ctx)
		if err != nil {
			return nil, err
		}
		out := []ConceptDTO{}
		for _, c := range cs {
			if q.Kind != "" && c.Kind.String() != q.Kind {
				continue
			}
			out = append(out, conceptDTO(c))
		}
		return out, nil
	})
	return svc
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func retry[In, Out any]() app.Middleware[In, Out] {
	return pipeline.RetryOnConflict[In, Out](3, 10*time.Millisecond)
}

// ListConcepts enumera el catálogo de conceptos retributivos, de una sola clase si se pide.
type ListConcepts struct{ Kind string }

// ConceptDTO es la forma de transporte de un concepto.
type ConceptDTO struct {
	ID            string `json:"id"`
	Code          string `json:"code"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Contributable bool   `json:"contributable"`
	Taxable       bool   `json:"taxable"`
	PerceptionKey string `json:"perceptionKey,omitempty"`
	Active        bool   `json:"active"`
}

func conceptDTO(c domain.Concept) ConceptDTO {
	return ConceptDTO{ID: c.ID.String(), Code: c.Code, Name: c.Name, Kind: c.Kind.String(), Contributable: c.Contributable,
		Taxable: c.Taxable, PerceptionKey: c.PerceptionKey, Active: c.Active}
}
