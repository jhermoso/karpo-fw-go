// Package application holds the Modules use cases (with permissions and company scope), the port
// other contexts ask, and the translation to the Published Language.
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

// Deps are the ports the use cases need; Recorder and Audit are optional.
type Deps struct {
	Features    domain.FeatureRepository
	Activations domain.ActivationRepository
	UoW         fw.UnitOfWork
	Recorder    app.EventRecorder
	Audit       app.AuditLog
}

// Service exposes the use cases.
type Service struct {
	Define  app.CommandHandler[DefineFeature, FeatureDTO]
	Change  app.CommandHandler[ChangeFeature, FeatureDTO]
	Catalog app.QueryHandler[ListCatalog, []FeatureDTO]

	Activate      app.CommandHandler[SwitchFeature, ActivationDTO]
	Deactivate    app.CommandHandler[SwitchFeature, ActivationDTO]
	Of            app.QueryHandler[ListActivations, []ActivationDTO]
	Organizations app.QueryHandler[ListOrganizations, []string]
	// Current needs no permission: it is what the caller's own companies have on.
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

// check returns a uniform 404 outside the company's scope and 403 when it is read-only.
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

// DefineFeature adds an entry to the catalog.
type DefineFeature struct {
	Kind        string `json:"kind"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ChangeFeature renames an entry of the catalog, or retires or restores it.
type ChangeFeature struct {
	ID          domain.FeatureID `json:"-"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Retired     bool             `json:"retired,omitempty"`
}

// ListCatalog lists the catalog.
type ListCatalog struct {
	Kind    string
	Retired bool // also the retired entries
}

// SwitchFeature switches a feature of a company on or off.
type SwitchFeature struct {
	Organization string `json:"organization"`
	Kind         string `json:"kind"`
	Code         string `json:"code"`
	Notes        string `json:"notes,omitempty"`
}

// ListActivations lists what a company has, or had, on.
type ListActivations struct{ Organization string }

// ListOrganizations lists the companies of the caller's scope with a feature on.
type ListOrganizations struct{ Kind, Code string }

// GetCurrent asks what the caller's companies have on.
type GetCurrent struct{}

// FeatureDTO is the transport form of a catalog entry.
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

// ActivationDTO is the transport form of an activation.
type ActivationDTO struct {
	Organization  string `json:"organization"`
	Kind          string `json:"kind"`
	Code          string `json:"code"`
	Active        bool   `json:"active"`
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

// CurrentDTO is what the caller's companies have on: the union over them, by kind.
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

// EnsureCatalog adds the seed entries the catalog lacks (existing ones are left as they are). It
// reports how many were added.
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

// NewService wires the use cases.
func NewService(d Deps) *Service {
	s := newService(d)
	svc := &Service{}

	// The catalog is everybody's: only a global administrator changes it.
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

	// Switching on: the feature must be offered; a company has one sector, so the one it had goes off.
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
			return activationDTO(act), nil // on already
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
		act, err := s.activation(ctx, org, kind, domain.NormalizeCode(c.Code))
		if err != nil {
			return ActivationDTO{}, err
		}
		if act == nil {
			return ActivationDTO{}, fw.Violation("modules.not_active", "the company never had that "+string(kind))
		}
		if !act.State().Active {
			return activationDTO(act), nil // off already
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
			out = append(out, activationDTO(a))
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

	// What the caller sees: a global administrator, everything offered; anyone else, what their
	// companies have on, and nothing without companies (the C# fell back to everything).
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
				add(a.State().Kind, a.State().Code)
			}
		}
		slices.Sort(out.Modules)
		slices.Sort(out.Capabilities)
		slices.Sort(out.Sectors)
		return out, nil
	})
	return svc
}

// Features implements contracts.Features on the repository: the port other contexts ask.
type Features struct{ Activations domain.ActivationRepository }

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

// Has implements contracts.Features.
func (f Features) Has(ctx context.Context, organization, kind, code string) (bool, error) {
	as, err := f.on(ctx, organization, kind, domain.ActFieldCode.Eq(domain.NormalizeCode(code)))
	return len(as) > 0, err
}

// Of implements contracts.Features.
func (f Features) Of(ctx context.Context, organization, kind string) ([]string, error) {
	as, err := f.on(ctx, organization, kind)
	out := []string{}
	for _, a := range as {
		out = append(out, a.State().Code)
	}
	return out, err
}

// Publications translates the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.FeatureActivated) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.FeatureActivatedV1{Organization: e.Organization, Kind: e.Kind, Code: e.Code}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.FeatureDeactivated) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.FeatureDeactivatedV1{Organization: e.Organization, Kind: e.Kind, Code: e.Code}}, nil
	})
	return r
}
