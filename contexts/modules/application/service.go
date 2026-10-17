// Package application contiene los casos de uso de Modules (con permisos y ámbito de empresa), el
// puerto al que preguntan otros contextos y la traducción al Published Language.
package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/modules/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/modules/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// FeatureRef nombra una funcionalidad: su clase y su código.
type FeatureRef struct {
	Kind domain.Kind
	Code string
}

// Derivation dice las funcionalidades que una empresa tiene por lo que es, no porque alguien las
// encendiera: la capacidad financiera de una empresa que es una entidad financiera. Lo deducido
// nunca se enciende ni se apaga a mano; una activación suya que se hubiera registrado no cuenta.
type Derivation interface {
	// Features lista lo que se deduce.
	Features() []FeatureRef
	// Of lista las funcionalidades deducidas que una empresa tiene ahora.
	Of(ctx context.Context, organization fw.UUID) ([]FeatureRef, error)
	// Holders lista las empresas que tienen ahora una funcionalidad deducida.
	Holders(ctx context.Context, f FeatureRef) ([]fw.UUID, error)
}

// derived dice si una funcionalidad es deducida.
func derived(d Derivation, kind domain.Kind, code string) bool {
	return d != nil && slices.Contains(d.Features(), FeatureRef{Kind: kind, Code: code})
}

// Deps son los puertos que necesitan los casos de uso; Derivation, Recorder y Audit son opcionales.
type Deps struct {
	Features    domain.FeatureRepository
	Activations domain.ActivationRepository
	Derivation  Derivation
	UoW         fw.UnitOfWork
	Recorder    app.EventRecorder
	Audit       app.AuditLog
}

// Service expone los casos de uso.
type Service struct {
	Define  app.CommandHandler[DefineFeature, FeatureDTO]
	Change  app.CommandHandler[ChangeFeature, FeatureDTO]
	Catalog app.QueryHandler[ListCatalog, []FeatureDTO]

	Activate      app.CommandHandler[SwitchFeature, ActivationDTO]
	Deactivate    app.CommandHandler[SwitchFeature, ActivationDTO]
	Of            app.QueryHandler[ListActivations, []ActivationDTO]
	Organizations app.QueryHandler[ListOrganizations, []string]
	// Current no necesita permiso: es lo que tienen encendido las propias empresas del que llama.
	Current app.QueryHandler[GetCurrent, CurrentDTO]
}

type service struct {
	Deps
	features    *orchestration.Orchestrator[domain.FeatureID, *domain.Feature]
	activations *orchestration.Orchestrator[domain.ActivationID, *domain.Activation]
}

func newService(d Deps) service {
	var opts []orchestration.Option
	if d.Recorder != nil {
		opts = append(opts, orchestration.WithOutbox(d.Recorder))
	}
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	return service{Deps: d, features: orchestration.New[domain.FeatureID, *domain.Feature](d.Features, d.UoW, opts...),
		activations: orchestration.New[domain.ActivationID, *domain.Activation](d.Activations, d.UoW, opts...)}
}

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

// check devuelve un 404 uniforme fuera del ámbito de la empresa y un 403 cuando es de sólo
// lectura.
func (s scope) check(org domain.OrganizationID, write bool) error {
	if !s.global && !slices.Contains(s.orgs, org) {
		return fw.NotFound("parties.party", org)
	}
	if write && !s.global && (s.ac == nil || !s.ac.CanWrite(org.UUID)) {
		return fmt.Errorf("%w: read-only in your organization scope", fw.ErrForbidden)
	}
	return nil
}

func (s scope) actor() string {
	if s.ac == nil {
		return ""
	}
	return s.ac.SubjectName
}

func parseID(v *fw.Validation, field, s string) fw.UUID {
	u, err := fw.ParseUUID(s)
	v.Require(err == nil && !u.IsZero(), field, "format", field+" must be an id")
	return u
}

func parseKind(v *fw.Validation, s string) domain.Kind {
	k := domain.Kind(strings.TrimSpace(s))
	v.Require(slices.Contains(domain.Kinds, k), "kind", "enum", "module, capability or sector")
	return k
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func changing[In, Out any](uow fw.UnitOfWork, p authz.Permission, fn func(context.Context, In) (Out, error)) app.Handler[In, Out] {
	return guard(p, fn, pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond), pipeline.Transactional[In, Out](uow))
}

// DefineFeature añade una entrada al catálogo.
type DefineFeature struct {
	Kind        string `json:"kind"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ChangeFeature renombra una entrada del catálogo, o la retira o la restaura.
type ChangeFeature struct {
	ID          domain.FeatureID `json:"-"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Retired     bool             `json:"retired,omitempty"`
}

// ListCatalog lista el catálogo.
type ListCatalog struct {
	Kind    string
	Retired bool // también las entradas retiradas
}

// SwitchFeature enciende o apaga una funcionalidad de una empresa.
type SwitchFeature struct {
	Organization string `json:"organization"`
	Kind         string `json:"kind"`
	Code         string `json:"code"`
	Notes        string `json:"notes,omitempty"`
}

// ListActivations lista lo que una empresa tiene, o tuvo, encendido.
type ListActivations struct{ Organization string }

// ListOrganizations lista las empresas del ámbito del que llama con una funcionalidad encendida.
type ListOrganizations struct{ Kind, Code string }

// GetCurrent pregunta qué tienen encendido las empresas del que llama.
type GetCurrent struct{}

// FeatureDTO es la forma de transporte de una entrada del catálogo.
type FeatureDTO struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Retired     bool   `json:"retired"`
	Version     int64  `json:"version"`
}

func featureDTO(f *domain.Feature) FeatureDTO {
	s := f.State()
	return FeatureDTO{ID: f.ID().String(), Kind: string(s.Kind), Code: s.Code, Name: s.Name, Description: s.Description, Retired: s.Retired,
		Version: f.Version()}
}

// ActivationDTO es la forma de transporte de una activación.
type ActivationDTO struct {
	Organization  string `json:"organization"`
	Kind          string `json:"kind"`
	Code          string `json:"code"`
	Active        bool   `json:"active"`
	Derived       bool   `json:"derived,omitempty"` // la tiene por lo que es: nadie la encendió
	ActivatedAt   string `json:"activatedAt,omitempty"`
	ActivatedBy   string `json:"activatedBy,omitempty"`
	DeactivatedAt string `json:"deactivatedAt,omitempty"`
	DeactivatedBy string `json:"deactivatedBy,omitempty"`
	Notes         string `json:"notes,omitempty"`
	Version       int64  `json:"version"`
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func activationDTO(a *domain.Activation) ActivationDTO {
	s := a.State()
	return ActivationDTO{Organization: s.Organization.String(), Kind: string(s.Kind), Code: s.Code, Active: s.Active, ActivatedAt: stamp(s.ActivatedAt),
		ActivatedBy: s.ActivatedBy, DeactivatedAt: stamp(s.DeactivatedAt), DeactivatedBy: s.DeactivatedBy, Notes: s.Notes, Version: a.Version()}
}

// CurrentDTO es lo que tienen encendido las empresas del que llama: la unión de todas, por clase.
type CurrentDTO struct {
	Modules      []string `json:"modules"`
	Capabilities []string `json:"capabilities"`
	Sectors      []string `json:"sectors"`
}

func (s service) feature(ctx context.Context, kind domain.Kind, code string) (*domain.Feature, error) {
	found, err := s.Features.Find(ctx, spec.And(domain.FtFieldKind.Eq(string(kind)), domain.FtFieldCode.Eq(code)))
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fw.Violation("modules.unknown_feature", "the catalog has no "+string(kind)+" "+code)
	}
	return found[0], nil
}

func (s service) activation(ctx context.Context, org domain.OrganizationID, kind domain.Kind, code string) (*domain.Activation, error) {
	found, err := s.Activations.Find(ctx, spec.And(domain.ActFieldOrganization.Eq(org), domain.ActFieldKind.Eq(string(kind)), domain.ActFieldCode.Eq(code)))
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return found[0], nil
}

// EnsureCatalog añade las entradas de la semilla que le faltan al catálogo (las que existen se
// dejan como están). Dice cuántas se añadieron.
func EnsureCatalog(ctx context.Context, d Deps) (int, error) {
	s, added := newService(d), 0
	err := d.UoW.Do(ctx, func(ctx context.Context) error {
		added = 0
		for _, st := range domain.Seed {
			exists, err := d.Features.Exists(ctx, spec.And(domain.FtFieldKind.Eq(string(st.Kind)), domain.FtFieldCode.Eq(st.Code)))
			if err != nil {
				return err
			}
			if exists {
				continue
			}
			f, err := domain.ReconstituteFeature(domain.NewFeatureID(), st)
			if err != nil {
				return err
			}
			if err := s.Features.Save(ctx, f); err != nil {
				return err
			}
			added++
		}
		return nil
	})
	return added, err
}

// NewService monta los casos de uso.
func NewService(d Deps) *Service {
	s := newService(d)
	svc := &Service{}

	// El catálogo es de todos: sólo lo cambia un administrador global.
	global := func(ctx context.Context) error {
		if !scopeOf(ctx).global {
			return fmt.Errorf("%w: the catalog is changed by a global administrator", fw.ErrForbidden)
		}
		return nil
	}
	svc.Define = changing(d.UoW, PermCatalogUpdate, func(ctx context.Context, c DefineFeature) (FeatureDTO, error) {
		if err := global(ctx); err != nil {
			return FeatureDTO{}, err
		}
		f, err := domain.ReconstituteFeature(domain.NewFeatureID(), domain.FeatureState{Kind: domain.Kind(strings.TrimSpace(c.Kind)), Code: c.Code,
			Name: c.Name, Description: c.Description})
		if err != nil {
			return FeatureDTO{}, err
		}
		dup, err := d.Features.Exists(ctx, spec.And(domain.FtFieldKind.Eq(string(f.State().Kind)), domain.FtFieldCode.Eq(f.State().Code)))
		if err != nil {
			return FeatureDTO{}, err
		}
		if dup {
			return FeatureDTO{}, fw.Violation("modules.duplicate_feature", "the catalog already has that "+string(f.State().Kind))
		}
		if err := s.features.Create(ctx, f); err != nil {
			return FeatureDTO{}, err
		}
		return featureDTO(f), nil
	})
	svc.Change = changing(d.UoW, PermCatalogUpdate, func(ctx context.Context, c ChangeFeature) (FeatureDTO, error) {
		if err := global(ctx); err != nil {
			return FeatureDTO{}, err
		}
		f, err := s.features.Update(ctx, c.ID, func(_ context.Context, f *domain.Feature) error { return f.Change(c.Name, c.Description, c.Retired) })
		if err != nil {
			return FeatureDTO{}, err
		}
		return featureDTO(f), nil
	})
	svc.Catalog = guard(PermCatalogRead, func(ctx context.Context, q ListCatalog) ([]FeatureDTO, error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Feature]{}
		if q.Kind != "" {
			parts = append(parts, domain.FtFieldKind.Eq(string(parseKind(&v, q.Kind))))
		}
		if !q.Retired {
			parts = append(parts, domain.FtFieldRetired.Eq(false))
		}
		if err := v.Err(); err != nil {
			return nil, err
		}
		fs, err := d.Features.Find(ctx, spec.And(parts...), domain.FtFieldKind.Asc(), domain.FtFieldCode.Asc())
		if err != nil {
			return nil, err
		}
		out := []FeatureDTO{}
		for _, f := range fs {
			out = append(out, featureDTO(f))
		}
		return out, nil
	})

	// Encender: la funcionalidad debe ofrecerse; una empresa tiene un solo sector, así que el que
	// tenía se apaga.
	svc.Activate = changing(d.UoW, PermActivationUpdate, func(ctx context.Context, c SwitchFeature) (ActivationDTO, error) {
		var v fw.Validation
		org, kind := domain.OrganizationID{UUID: parseID(&v, "organization", c.Organization)}, parseKind(&v, c.Kind)
		if err := v.Err(); err != nil {
			return ActivationDTO{}, err
		}
		sc := scopeOf(ctx)
		if err := sc.check(org, true); err != nil {
			return ActivationDTO{}, err
		}
		if derived(d.Derivation, kind, domain.NormalizeCode(c.Code)) {
			return ActivationDTO{}, fw.Violation("modules.derived", "a company has that "+string(kind)+" because of what it is: it is not switched by hand")
		}
		f, err := s.feature(ctx, kind, domain.NormalizeCode(c.Code))
		if err != nil {
			return ActivationDTO{}, err
		}
		now := fw.Now()
		if kind.Exclusive() {
			others, err := d.Activations.Find(ctx, spec.And(domain.ActFieldOrganization.Eq(org), domain.ActFieldKind.Eq(string(kind)), domain.ActFieldActive.Eq(true)))
			if err != nil {
				return ActivationDTO{}, err
			}
			for _, o := range others {
				if o.State().Code == f.State().Code {
					continue
				}
				if _, err := s.activations.Update(ctx, o.ID(), func(_ context.Context, a *domain.Activation) error {
					_, err := a.Deactivate(sc.actor(), now, "")
					return err
				}); err != nil {
					return ActivationDTO{}, err
				}
			}
		}
		act, err := s.activation(ctx, org, kind, f.State().Code)
		if err != nil {
			return ActivationDTO{}, err
		}
		if act == nil {
			if f.State().Retired {
				return ActivationDTO{}, fw.Violation("modules.retired_feature", "the "+string(kind)+" is no longer offered")
			}
			if act, err = domain.NewActivation(domain.NewActivationID(), org, f); err != nil {
				return ActivationDTO{}, err
			}
			if _, err := act.Activate(sc.actor(), now, c.Notes); err != nil {
				return ActivationDTO{}, err
			}
			if err := s.activations.Create(ctx, act); err != nil {
				return ActivationDTO{}, err
			}
			return activationDTO(act), nil
		}
		if !act.State().Active && f.State().Retired {
			return ActivationDTO{}, fw.Violation("modules.retired_feature", "the "+string(kind)+" is no longer offered")
		}
		if notes := strings.TrimSpace(c.Notes); act.State().Active && (notes == "" || notes == act.State().Notes) {
			return activationDTO(act), nil // ya está encendida
		}
		act, err = s.activations.Update(ctx, act.ID(), func(_ context.Context, a *domain.Activation) error {
			_, err := a.Activate(sc.actor(), now, c.Notes)
			return err
		})
		if err != nil {
			return ActivationDTO{}, err
		}
		return activationDTO(act), nil
	})
	svc.Deactivate = changing(d.UoW, PermActivationUpdate, func(ctx context.Context, c SwitchFeature) (ActivationDTO, error) {
		var v fw.Validation
		org, kind := domain.OrganizationID{UUID: parseID(&v, "organization", c.Organization)}, parseKind(&v, c.Kind)
		if err := v.Err(); err != nil {
			return ActivationDTO{}, err
		}
		sc := scopeOf(ctx)
		if err := sc.check(org, true); err != nil {
			return ActivationDTO{}, err
		}
		if derived(d.Derivation, kind, domain.NormalizeCode(c.Code)) {
			return ActivationDTO{}, fw.Violation("modules.derived", "a company has that "+string(kind)+" because of what it is: it is not switched by hand")
		}
		act, err := s.activation(ctx, org, kind, domain.NormalizeCode(c.Code))
		if err != nil {
			return ActivationDTO{}, err
		}
		if act == nil {
			return ActivationDTO{}, fw.Violation("modules.not_active", "the company never had that "+string(kind))
		}
		if !act.State().Active {
			return activationDTO(act), nil // ya está apagada
		}
		act, err = s.activations.Update(ctx, act.ID(), func(_ context.Context, a *domain.Activation) error {
			_, err := a.Deactivate(sc.actor(), fw.Now(), c.Notes)
			return err
		})
		if err != nil {
			return ActivationDTO{}, err
		}
		return activationDTO(act), nil
	})

	svc.Of = guard(PermActivationRead, func(ctx context.Context, q ListActivations) ([]ActivationDTO, error) {
		var v fw.Validation
		org := domain.OrganizationID{UUID: parseID(&v, "organization", q.Organization)}
		if err := v.Err(); err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check(org, false); err != nil {
			return nil, err
		}
		as, err := d.Activations.Find(ctx, domain.ActFieldOrganization.Eq(org), domain.ActFieldKind.Asc(), domain.ActFieldCode.Asc())
		if err != nil {
			return nil, err
		}
		out := []ActivationDTO{}
		for _, a := range as {
			if !derived(d.Derivation, a.State().Kind, a.State().Code) { // lo deducido se dice más abajo, tal como está ahora
				out = append(out, activationDTO(a))
			}
		}
		if d.Derivation != nil {
			refs, err := d.Derivation.Of(ctx, org.UUID)
			if err != nil {
				return nil, err
			}
			for _, r := range refs {
				out = append(out, ActivationDTO{Organization: org.String(), Kind: string(r.Kind), Code: r.Code, Active: true, Derived: true})
			}
			slices.SortFunc(out, func(a, b ActivationDTO) int { return strings.Compare(a.Kind+"|"+a.Code, b.Kind+"|"+b.Code) })
		}
		return out, nil
	})
	svc.Organizations = guard(PermActivationRead, func(ctx context.Context, q ListOrganizations) ([]string, error) {
		var v fw.Validation
		kind := parseKind(&v, q.Kind)
		v.Require(domain.ValidCode(domain.NormalizeCode(q.Code)), "code", "format", "a feature code")
		if err := v.Err(); err != nil {
			return nil, err
		}
		sc := scopeOf(ctx)
		if derived(d.Derivation, kind, domain.NormalizeCode(q.Code)) {
			holders, err := d.Derivation.Holders(ctx, FeatureRef{Kind: kind, Code: domain.NormalizeCode(q.Code)})
			if err != nil {
				return nil, err
			}
			out := []string{}
			for _, h := range holders {
				if sc.global || slices.Contains(sc.orgs, domain.OrganizationID{UUID: h}) {
					out = append(out, h.String())
				}
			}
			slices.Sort(out)
			return out, nil
		}
		parts := []spec.Specification[*domain.Activation]{domain.ActFieldKind.Eq(string(kind)), domain.ActFieldCode.Eq(domain.NormalizeCode(q.Code)),
			domain.ActFieldActive.Eq(true)}
		switch {
		case sc.global:
		case len(sc.orgs) == 0:
			return []string{}, nil
		default:
			parts = append(parts, domain.ActFieldOrganization.In(sc.orgs...))
		}
		as, err := d.Activations.Find(ctx, spec.And(parts...))
		if err != nil {
			return nil, err
		}
		out := []string{}
		for _, a := range as {
			out = append(out, a.State().Organization.String())
		}
		slices.Sort(out)
		return out, nil
	})

	// Lo que ve el que llama: un administrador global, todo lo que se ofrece; cualquier otro, lo que
	// tienen encendido sus empresas, y nada si no tiene empresas (el C# caía en todo).
	svc.Current = app.HandlerFunc[GetCurrent, CurrentDTO](func(ctx context.Context, _ GetCurrent) (CurrentDTO, error) {
		out := CurrentDTO{Modules: []string{}, Capabilities: []string{}, Sectors: []string{}}
		add := func(kind domain.Kind, code string) {
			list := map[domain.Kind]*[]string{domain.Module: &out.Modules, domain.Capability: &out.Capabilities, domain.Sector: &out.Sectors}[kind]
			if !slices.Contains(*list, code) {
				*list = append(*list, code)
			}
		}
		sc := scopeOf(ctx)
		switch {
		case sc.global:
			fs, err := d.Features.Find(ctx, domain.FtFieldRetired.Eq(false))
			if err != nil {
				return CurrentDTO{}, err
			}
			for _, f := range fs {
				add(f.State().Kind, f.State().Code)
			}
		case len(sc.orgs) > 0:
			as, err := d.Activations.Find(ctx, spec.And(domain.ActFieldOrganization.In(sc.orgs...), domain.ActFieldActive.Eq(true)))
			if err != nil {
				return CurrentDTO{}, err
			}
			for _, a := range as {
				if !derived(d.Derivation, a.State().Kind, a.State().Code) {
					add(a.State().Kind, a.State().Code)
				}
			}
			if d.Derivation != nil {
				for _, org := range sc.orgs {
					refs, err := d.Derivation.Of(ctx, org.UUID)
					if err != nil {
						return CurrentDTO{}, err
					}
					for _, r := range refs {
						add(r.Kind, r.Code)
					}
				}
			}
		}
		slices.Sort(out.Modules)
		slices.Sort(out.Capabilities)
		slices.Sort(out.Sectors)
		return out, nil
	})
	return svc
}

// Features implementa contracts.Features sobre el repositorio: el puerto al que preguntan otros
// contextos. Lo deducido lo responde la derivación, se haya registrado lo que se haya registrado a
// mano.
type Features struct {
	Activations domain.ActivationRepository
	Derivation  Derivation
}

func (f Features) derivedOf(ctx context.Context, organization, kind string) ([]string, error) {
	org, err := fw.ParseUUID(organization)
	if f.Derivation == nil || err != nil {
		return nil, nil
	}
	refs, err := f.Derivation.Of(ctx, org)
	out := []string{}
	for _, r := range refs {
		if string(r.Kind) == kind {
			out = append(out, r.Code)
		}
	}
	return out, err
}

var _ contracts.Features = Features{}

func (f Features) on(ctx context.Context, organization, kind string, more ...spec.Specification[*domain.Activation]) ([]*domain.Activation, error) {
	org, err := fw.ParseUUID(organization)
	if err != nil || !slices.Contains(domain.Kinds, domain.Kind(kind)) {
		return nil, nil
	}
	parts := append([]spec.Specification[*domain.Activation]{domain.ActFieldOrganization.Eq(domain.OrganizationID{UUID: org}),
		domain.ActFieldKind.Eq(kind), domain.ActFieldActive.Eq(true)}, more...)
	return f.Activations.Find(ctx, spec.And(parts...), domain.ActFieldCode.Asc())
}

// Has implementa contracts.Features.
func (f Features) Has(ctx context.Context, organization, kind, code string) (bool, error) {
	if derived(f.Derivation, domain.Kind(kind), domain.NormalizeCode(code)) {
		codes, err := f.derivedOf(ctx, organization, kind)
		return slices.Contains(codes, domain.NormalizeCode(code)), err
	}
	as, err := f.on(ctx, organization, kind, domain.ActFieldCode.Eq(domain.NormalizeCode(code)))
	return len(as) > 0, err
}

// Of implementa contracts.Features.
func (f Features) Of(ctx context.Context, organization, kind string) ([]string, error) {
	as, err := f.on(ctx, organization, kind)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, a := range as {
		if !derived(f.Derivation, a.State().Kind, a.State().Code) {
			out = append(out, a.State().Code)
		}
	}
	more, err := f.derivedOf(ctx, organization, kind)
	out = append(out, more...)
	slices.Sort(out)
	return out, err
}

// Publications traduce los eventos de dominio al Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.FeatureActivated) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.FeatureActivatedV1{Organization: e.Organization, Kind: e.Kind, Code: e.Code}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.FeatureDeactivated) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.FeatureDeactivatedV1{Organization: e.Organization, Kind: e.Kind, Code: e.Code}}, nil
	})
	return r
}
