// Package application holds the Facilities use cases (with permissions and organization scope),
// the translation to its Published Language and its directory.
package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/facilities/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permissions (the C# resource Parties/Facility).
var (
	PermRead   = authz.MustPermission("Facilities.Facility.Read")
	PermCreate = authz.MustPermission("Facilities.Facility.Create")
	PermUpdate = authz.MustPermission("Facilities.Facility.Update")
)

// MaxDepth bounds the facility hierarchy (room in floor in building in site...).
const MaxDepth = 10

// AddressChecker validates the geographic part of an address and completes its Geography
// references (the port Facilities owns; an adapter implements it over the Geography contracts).
type AddressChecker interface {
	CheckAddress(ctx context.Context, a domain.Address) (domain.Address, error)
}

// Deps are the ports the use cases need; Recorder, Audit and Addresses are optional.
type Deps struct {
	Facilities domain.Repository
	Catalogs   domain.Catalogs
	UoW        fw.UnitOfWork
	Recorder   app.EventRecorder
	Audit      app.AuditLog
	Addresses  AddressChecker
}

// LocationDTO is the transport form of a location.
type LocationDTO struct {
	StreetType    string `json:"streetType,omitempty"`
	Line1         string `json:"line1,omitempty"`
	Line2         string `json:"line2,omitempty"`
	PostalCode    string `json:"postalCode,omitempty"`
	Locality      string `json:"locality,omitempty"`
	Region        string `json:"region,omitempty"`
	Country       string `json:"country,omitempty"`
	GeoPostalCode string `json:"geoPostalCode,omitempty"`
	GeoBoundary   string `json:"geoBoundary,omitempty"`
	Phone         string `json:"phone,omitempty"`
	Email         string `json:"email,omitempty"`
}

// RegisterFacility registers a facility of an internal organization.
type RegisterFacility struct {
	Organization string       `json:"organization"`
	Type         string       `json:"type"`
	Name         string       `json:"name"`
	Description  string       `json:"description,omitempty"`
	PartOf       string       `json:"partOf,omitempty"`
	AreaM2       string       `json:"areaM2,omitempty"`
	Location     *LocationDTO `json:"location,omitempty"`
}

// RenameFacility changes the name.
type RenameFacility struct {
	ID   domain.FacilityID `json:"-"`
	Name string            `json:"name"`
}

// RelocateFacility replaces the location.
type RelocateFacility struct {
	ID       domain.FacilityID `json:"-"`
	Location LocationDTO       `json:"location"`
}

// MoveFacility makes a facility part of another one (empty PartOf detaches it).
type MoveFacility struct {
	ID     domain.FacilityID `json:"-"`
	PartOf string            `json:"partOf"`
}

// SetFacilityActive activates or deactivates a facility.
type SetFacilityActive struct {
	ID     domain.FacilityID `json:"-"`
	Active bool              `json:"active"`
}

// GetFacility loads a facility.
type GetFacility struct{ ID domain.FacilityID }

// SearchFacilities searches facilities of the caller's scope.
type SearchFacilities struct {
	Text, Type, Organization, PartOf string
	ActiveOnly                       bool
	Page, Size                       int
}

// ListFacilityTypes lists the facility type catalog.
type ListFacilityTypes struct{}

// FacilityDTO is the transport form of a facility.
type FacilityDTO struct {
	ID           string      `json:"id"`
	Organization string      `json:"organization"`
	Type         string      `json:"type"`
	TypeName     string      `json:"typeName,omitempty"`
	Name         string      `json:"name"`
	Description  string      `json:"description,omitempty"`
	PartOf       string      `json:"partOf,omitempty"`
	AreaM2       string      `json:"areaM2,omitempty"`
	Location     LocationDTO `json:"location"`
	Active       bool        `json:"active"`
	Version      int64       `json:"version"`
	ModifiedBy   string      `json:"modifiedBy,omitempty"`
}

// Service exposes the use cases.
type Service struct {
	Register  app.CommandHandler[RegisterFacility, FacilityDTO]
	Rename    app.CommandHandler[RenameFacility, FacilityDTO]
	Relocate  app.CommandHandler[RelocateFacility, FacilityDTO]
	Move      app.CommandHandler[MoveFacility, FacilityDTO]
	SetActive app.CommandHandler[SetFacilityActive, FacilityDTO]
	Get       app.QueryHandler[GetFacility, FacilityDTO]
	Search    app.QueryHandler[SearchFacilities, fw.Page[FacilityDTO]]
	Types     app.QueryHandler[ListFacilityTypes, []domain.FacilityType]
}

// scope: a facility belongs to its organization; it is visible inside that organization's scope
// (uniform 404 elsewhere) and writable with a Full grant on it.
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

func (s scope) spec() spec.Spec[*domain.Facility] {
	switch {
	case s.global:
		return spec.All[*domain.Facility]()
	case len(s.orgs) == 0:
		return spec.None[*domain.Facility]()
	}
	return domain.OwnedBy(s.orgs...)
}

func (s scope) sees(org domain.OrganizationID) bool { return s.global || slices.Contains(s.orgs, org) }

func (s scope) writes(org domain.OrganizationID) bool {
	return s.global || (s.ac != nil && s.ac.CanWrite(org.UUID))
}

func (s scope) check(f *domain.Facility, write bool) error {
	if !s.sees(f.Owner()) {
		return fw.NotFound(domain.FacilityKind, f.ID())
	}
	if write && !s.writes(f.Owner()) {
		return fmt.Errorf("%w: the facility is read-only in your organization scope", fw.ErrForbidden)
	}
	return nil
}

type service struct {
	Deps
	orch *orchestration.Orchestrator[domain.FacilityID, *domain.Facility]
}

func (s service) types(ctx context.Context) (map[domain.FacilityTypeID]domain.FacilityType, error) {
	ts, err := s.Catalogs.FacilityTypes(ctx)
	if err != nil {
		return nil, err
	}
	out := map[domain.FacilityTypeID]domain.FacilityType{}
	for _, t := range ts {
		out[t.ID] = t
	}
	return out, nil
}

func (s service) location(ctx context.Context, d *LocationDTO) (domain.Location, error) {
	if d == nil {
		return domain.Location{}, nil
	}
	var v fw.Validation
	l := domain.Location{Address: domain.Address{StreetType: d.StreetType, Line1: d.Line1, Line2: d.Line2, PostalCode: d.PostalCode,
		Locality: d.Locality, Region: d.Region}}
	if d.Country != "" {
		c, err := vocab.NewCountryCode(d.Country)
		v.Merge("location.address.country", err)
		l.Address.Country = c
	}
	for field, s := range map[string]string{"geoPostalCode": d.GeoPostalCode, "geoBoundary": d.GeoBoundary} {
		if s == "" {
			continue
		}
		u, err := fw.ParseUUID(s)
		v.Require(err == nil, "location.address."+field, "format", "must be a geography id")
		if field == "geoPostalCode" {
			l.Address.Geo.PostalCode = u
		} else {
			l.Address.Geo.Boundary = u
		}
	}
	if d.Phone != "" {
		p, err := vocab.NewPhone(d.Phone)
		v.Merge("location.phone", err)
		l.Phone = p
	}
	if d.Email != "" {
		e, err := vocab.NewEmail(d.Email)
		v.Merge("location.email", err)
		l.Email = e
	}
	if err := v.Err(); err != nil {
		return domain.Location{}, err
	}
	if s.Addresses != nil && !l.Address.IsZero() {
		a, err := s.Addresses.CheckAddress(ctx, l.Address)
		if err != nil {
			return domain.Location{}, err
		}
		l.Address = a
	}
	return l, nil
}

func toDTO(f *domain.Facility, types map[domain.FacilityTypeID]domain.FacilityType) FacilityDTO {
	l := f.Location()
	a := l.Address
	d := FacilityDTO{ID: f.ID().String(), Organization: f.Owner().String(), Type: f.Type().String(), TypeName: types[f.Type()].Name,
		Name: f.Name(), Description: f.Description(), Active: f.IsActive(), Version: f.Version(), ModifiedBy: f.ModifiedBy().Name,
		Location: LocationDTO{StreetType: a.StreetType, Line1: a.Line1, Line2: a.Line2, PostalCode: a.PostalCode, Locality: a.Locality,
			Region: a.Region, Country: a.Country.String(), Phone: l.Phone.String(), Email: l.Email.String()}}
	if !a.Geo.PostalCode.IsZero() {
		d.Location.GeoPostalCode = a.Geo.PostalCode.String()
	}
	if !a.Geo.Boundary.IsZero() {
		d.Location.GeoBoundary = a.Geo.Boundary.String()
	}
	if p := f.PartOf(); p != nil {
		d.PartOf = p.String()
	}
	if !f.Area().IsZero() {
		d.AreaM2 = f.Area().String()
	}
	return d
}

// parent loads the facility that will contain child, checking scope, ownership and cycles.
func (s service) parent(ctx context.Context, sc scope, child domain.FacilityID, owner domain.OrganizationID, id string) (*domain.Facility, error) {
	if id == "" {
		return nil, nil
	}
	pid, err := domain.ParseFacilityID(id)
	if err != nil {
		var v fw.Validation
		v.Add("partOf", "format", "partOf must be a facility id")
		return nil, v.Err()
	}
	p, err := s.Facilities.Get(ctx, pid)
	if err != nil {
		return nil, err
	}
	if err := sc.check(p, false); err != nil {
		return nil, err
	}
	if p.Owner() != owner {
		return nil, fw.Violation("facilities.other_organization", "a facility can only be part of a facility of the same organization")
	}
	for level, node := 0, p; ; level++ {
		if node.ID() == child {
			return nil, fw.Violation("facilities.hierarchy_cycle", "the parent is already inside the facility")
		}
		up := node.PartOf()
		if up == nil {
			break
		}
		if level >= MaxDepth {
			return nil, fw.Violation("facilities.hierarchy_too_deep", "the facility hierarchy would be too deep")
		}
		if node, err = s.Facilities.Get(ctx, *up); err != nil {
			return nil, err
		}
	}
	return p, nil
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
	s := service{Deps: d, orch: orchestration.New[domain.FacilityID, *domain.Facility](d.Facilities, d.UoW, opts...)}
	retry, backoff := 3, 10*time.Millisecond

	update := func(ctx context.Context, id domain.FacilityID, fn func(context.Context, *domain.Facility, map[domain.FacilityTypeID]domain.FacilityType) error) (FacilityDTO, error) {
		types, err := s.types(ctx)
		if err != nil {
			return FacilityDTO{}, err
		}
		sc := scopeOf(ctx)
		f, err := s.orch.Update(ctx, id, func(ctx context.Context, f *domain.Facility) error {
			if err := sc.check(f, true); err != nil {
				return err
			}
			return fn(ctx, f, types)
		})
		if err != nil {
			return FacilityDTO{}, err
		}
		return toDTO(f, types), nil
	}

	svc := &Service{}
	svc.Register = guard(PermCreate, func(ctx context.Context, c RegisterFacility) (FacilityDTO, error) {
		var v fw.Validation
		orgID, err := fw.ParseUUID(c.Organization)
		v.Require(err == nil, "organization", "format", "organization must be a party id")
		typeID, err := fw.ParseUUID(c.Type)
		v.Require(err == nil, "type", "format", "type must be a facility type id")
		var area vocab.Decimal
		if c.AreaM2 != "" {
			area, err = vocab.ParseDecimal(c.AreaM2)
			v.Require(err == nil, "areaM2", "format", "area must be a decimal number")
		}
		if err := v.Err(); err != nil {
			return FacilityDTO{}, err
		}
		owner := domain.OrganizationID{UUID: orgID}
		sc := scopeOf(ctx)
		if !sc.sees(owner) {
			return FacilityDTO{}, fw.NotFound("parties.party", owner)
		}
		if !sc.writes(owner) {
			return FacilityDTO{}, fw.ErrForbidden
		}
		types, err := s.types(ctx)
		if err != nil {
			return FacilityDTO{}, err
		}
		t, ok := types[domain.FacilityTypeID{UUID: typeID}]
		if !ok {
			v.Add("type", "unknown", "unknown facility type")
			return FacilityDTO{}, v.Err()
		}
		loc, err := s.location(ctx, c.Location)
		if err != nil {
			return FacilityDTO{}, err
		}
		id := domain.NewFacilityID()
		parent, err := s.parent(ctx, sc, id, owner, c.PartOf)
		if err != nil {
			return FacilityDTO{}, err
		}
		st := domain.FacilityState{Owner: owner, Name: c.Name, Description: c.Description, Area: area, Location: loc}
		if parent != nil {
			pid := parent.ID()
			st.PartOf = &pid
		}
		f, err := domain.Register(id, t, st)
		if err != nil {
			return FacilityDTO{}, err
		}
		if err := s.orch.Create(ctx, f); err != nil {
			return FacilityDTO{}, err
		}
		return toDTO(f, types), nil
	}, pipeline.Transactional[RegisterFacility, FacilityDTO](d.UoW))

	svc.Rename = guard(PermUpdate, func(ctx context.Context, c RenameFacility) (FacilityDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, f *domain.Facility, _ map[domain.FacilityTypeID]domain.FacilityType) error {
			return f.Rename(c.Name)
		})
	}, pipeline.RetryOnConflict[RenameFacility, FacilityDTO](retry, backoff))

	svc.Relocate = guard(PermUpdate, func(ctx context.Context, c RelocateFacility) (FacilityDTO, error) {
		loc, err := s.location(ctx, &c.Location)
		if err != nil {
			return FacilityDTO{}, err
		}
		return update(ctx, c.ID, func(_ context.Context, f *domain.Facility, types map[domain.FacilityTypeID]domain.FacilityType) error {
			return f.Relocate(types[f.Type()], loc)
		})
	}, pipeline.RetryOnConflict[RelocateFacility, FacilityDTO](retry, backoff))

	svc.Move = guard(PermUpdate, func(ctx context.Context, c MoveFacility) (FacilityDTO, error) {
		return update(ctx, c.ID, func(ctx context.Context, f *domain.Facility, _ map[domain.FacilityTypeID]domain.FacilityType) error {
			parent, err := s.parent(ctx, scopeOf(ctx), f.ID(), f.Owner(), c.PartOf)
			if err != nil {
				return err
			}
			return f.MoveUnder(parent)
		})
	}, pipeline.Transactional[MoveFacility, FacilityDTO](d.UoW))

	svc.SetActive = guard(PermUpdate, func(ctx context.Context, c SetFacilityActive) (FacilityDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, f *domain.Facility, _ map[domain.FacilityTypeID]domain.FacilityType) error {
			if c.Active {
				f.Activate()
			} else {
				f.Deactivate()
			}
			return nil
		})
	}, pipeline.RetryOnConflict[SetFacilityActive, FacilityDTO](retry, backoff))

	svc.Get = guard(PermRead, func(ctx context.Context, q GetFacility) (FacilityDTO, error) {
		types, err := s.types(ctx)
		if err != nil {
			return FacilityDTO{}, err
		}
		f, err := d.Facilities.Get(ctx, q.ID)
		if err != nil {
			return FacilityDTO{}, err
		}
		if err := scopeOf(ctx).check(f, false); err != nil {
			return FacilityDTO{}, err
		}
		return toDTO(f, types), nil
	})

	svc.Search = guard(PermRead, func(ctx context.Context, q SearchFacilities) (fw.Page[FacilityDTO], error) {
		types, err := s.types(ctx)
		if err != nil {
			return fw.Page[FacilityDTO]{}, err
		}
		parts := []spec.Specification[*domain.Facility]{scopeOf(ctx).spec()}
		if t := strings.TrimSpace(q.Text); t != "" {
			parts = append(parts, domain.FieldName.ContainsFold(t))
		}
		for field, s := range map[string]string{"type": q.Type, "organization": q.Organization, "partOf": q.PartOf} {
			if s == "" {
				continue
			}
			u, err := fw.ParseUUID(s)
			if err != nil {
				var v fw.Validation
				v.Add(field, "format", field+" must be an id")
				return fw.Page[FacilityDTO]{}, v.Err()
			}
			switch field {
			case "type":
				parts = append(parts, domain.FieldType.Eq(domain.FacilityTypeID{UUID: u}))
			case "organization":
				parts = append(parts, domain.OwnedBy(domain.OrganizationID{UUID: u}))
			default:
				parts = append(parts, domain.PartsOf(domain.FacilityID{UUID: u}))
			}
		}
		if q.ActiveOnly {
			parts = append(parts, domain.FieldActive.Eq(true))
		}
		page, err := d.Facilities.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.FieldName.Asc()))
		if err != nil {
			return fw.Page[FacilityDTO]{}, err
		}
		return fw.MapPage(page, func(f *domain.Facility) FacilityDTO { return toDTO(f, types) }), nil
	})

	svc.Types = guard(PermRead, func(ctx context.Context, _ ListFacilityTypes) ([]domain.FacilityType, error) {
		return d.Catalogs.FacilityTypes(ctx)
	})
	return svc
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

// Directory implements contracts.Directory (one query per batch; it serves contexts, not users).
type Directory struct{ Facilities domain.Repository }

var _ contracts.Directory = Directory{}

// Resolve implements contracts.Directory.
func (d Directory) Resolve(ctx context.Context, ids []string) (map[string]contracts.FacilityRef, error) {
	if len(ids) > contracts.MaxBatch {
		return nil, fmt.Errorf("%w: at most %d ids per call", fw.ErrValidation, contracts.MaxBatch)
	}
	var want []domain.FacilityID
	for _, s := range ids {
		if id, err := domain.ParseFacilityID(s); err == nil && !id.IsZero() {
			want = append(want, id)
		}
	}
	out := map[string]contracts.FacilityRef{}
	if len(want) == 0 {
		return out, nil
	}
	fs, err := d.Facilities.Find(ctx, domain.WithIDs(want...))
	if err != nil {
		return nil, err
	}
	for _, f := range fs {
		out[f.ID().String()] = contracts.FacilityRef{ID: f.ID().String(), Name: f.Name(), Type: f.Type().String(),
			Organization: f.Owner().String(), Active: f.IsActive()}
	}
	return out, nil
}

// Publications translates the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	one := func(e app.IntegrationEvent) ([]app.IntegrationEvent, error) { return []app.IntegrationEvent{e}, nil }
	messaging.On(r, func(_ context.Context, e domain.FacilityRegistered) ([]app.IntegrationEvent, error) {
		return one(contracts.FacilityRegisteredV1{FacilityID: e.AggregateID, Organization: e.Organization, Type: e.Type, Name: e.Name, PartOf: e.PartOf})
	})
	messaging.On(r, func(_ context.Context, e domain.FacilityRenamed) ([]app.IntegrationEvent, error) {
		return one(contracts.FacilityRenamedV1{FacilityID: e.AggregateID, Name: e.Name})
	})
	messaging.On(r, func(_ context.Context, e domain.FacilityRelocated) ([]app.IntegrationEvent, error) {
		return one(contracts.FacilityRelocatedV1{FacilityID: e.AggregateID, Address: e.Address})
	})
	messaging.On(r, func(_ context.Context, e domain.FacilityActivationChanged) ([]app.IntegrationEvent, error) {
		return one(contracts.FacilityActivationChangedV1{FacilityID: e.AggregateID, Active: e.Active})
	})
	return r
}
