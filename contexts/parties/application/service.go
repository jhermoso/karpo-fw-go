package application

import (
	"context"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Deps are the ports the use cases need; Recorder, Audit and Idempotency are optional.
type Deps struct {
	Parties       domain.PartyRepository
	Relationships domain.RelationshipRepository
	Catalogs      domain.Catalogs
	UoW           fw.UnitOfWork
	Recorder      app.EventRecorder
	Audit         app.AuditLog
	Idempotency   app.IdempotencyStore
}

// Service exposes the Parties use cases as decorated, statically typed handlers. Every handler
// requires its permission in the authorization context of ctx, whatever the entry point.
type Service struct {
	RegisterPerson        app.CommandHandler[RegisterPerson, PartyDTO]
	RegisterOrganization  app.CommandHandler[RegisterOrganization, PartyDTO]
	Rename                app.CommandHandler[RenameParty, PartyDTO]
	SetActive             app.CommandHandler[SetPartyActive, PartyDTO]
	AssignRole            app.CommandHandler[AssignRole, PartyDTO]
	EndRole               app.CommandHandler[EndRole, PartyDTO]
	EstablishRelationship app.CommandHandler[EstablishRelationship, RelationshipDTO]
	TerminateRelationship app.CommandHandler[TerminateRelationship, RelationshipDTO]

	Get                   app.QueryHandler[GetParty, PartyDTO]
	Search                app.QueryHandler[SearchParties, fw.Page[PartyDTO]]
	Relationships         app.QueryHandler[PartyRelationships, []RelationshipDTO]
	ListRoleTypes         app.QueryHandler[ListRoleTypes, []RoleTypeDTO]
	ListRelationshipTypes app.QueryHandler[ListRelationshipTypes, []RelationshipTypeDTO]
}

type service struct{ Deps }

func (s service) roleCatalog(ctx context.Context) (*domain.Catalog, error) {
	types, err := s.Catalogs.RoleTypes(ctx)
	if err != nil {
		return nil, err
	}
	return domain.NewRoleCatalog(types, domain.RolePersonCategory, domain.RoleOrganizationCategory)
}

func (s service) relationshipTypes(ctx context.Context) (map[domain.RelationshipTypeID]domain.RelationshipType, error) {
	types, err := s.Catalogs.RelationshipTypes(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[domain.RelationshipTypeID]domain.RelationshipType, len(types))
	for _, t := range types {
		out[t.ID] = t
	}
	return out, nil
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
	parties := orchestration.New[domain.PartyID, *domain.Party](d.Parties, d.UoW, opts...)
	relationships := orchestration.New[domain.RelationshipID, *domain.Relationship](d.Relationships, d.UoW, opts...)
	s := service{d}

	register := func(ctx context.Context, build func(domain.PartyID) (*domain.Party, error), roles []string) (PartyDTO, error) {
		cat, err := s.roleCatalog(ctx)
		if err != nil {
			return PartyDTO{}, err
		}
		p, err := build(domain.NewPartyID())
		if err != nil {
			return PartyDTO{}, err
		}
		now := fw.Now()
		for _, r := range roles {
			id, err := parseRoleType(r)
			if err != nil {
				return PartyDTO{}, err
			}
			if _, err := p.AssignRole(cat, id, now); err != nil {
				return PartyDTO{}, err
			}
		}
		if err := parties.Create(ctx, p); err != nil {
			return PartyDTO{}, err
		}
		return ToDTO(p, cat), nil
	}

	updateParty := func(ctx context.Context, id domain.PartyID, fn func(*domain.Party, *domain.Catalog) error) (PartyDTO, error) {
		cat, err := s.roleCatalog(ctx)
		if err != nil {
			return PartyDTO{}, err
		}
		p, err := parties.Update(ctx, id, func(_ context.Context, p *domain.Party) error { return fn(p, cat) })
		if err != nil {
			return PartyDTO{}, err
		}
		return ToDTO(p, cat), nil
	}

	svc := &Service{}
	retry := 3
	backoff := 10 * time.Millisecond

	svc.RegisterPerson = chain(PermPartyCreate, func(ctx context.Context, c RegisterPerson) (PartyDTO, error) {
		details, err := c.details()
		if err != nil {
			return PartyDTO{}, err
		}
		return register(ctx, func(id domain.PartyID) (*domain.Party, error) { return domain.RegisterPerson(id, details) }, c.Roles)
	}, idempotent[RegisterPerson, PartyDTO](d.Idempotency), pipeline.Transactional[RegisterPerson, PartyDTO](d.UoW))

	svc.RegisterOrganization = chain(PermPartyCreate, func(ctx context.Context, c RegisterOrganization) (PartyDTO, error) {
		name, err := domain.NewOrganizationName(c.LegalName, c.TradeName)
		if err != nil {
			return PartyDTO{}, err
		}
		return register(ctx, func(id domain.PartyID) (*domain.Party, error) {
			return domain.RegisterOrganization(id, domain.OrganizationDetails{Name: name})
		}, c.Roles)
	}, idempotent[RegisterOrganization, PartyDTO](d.Idempotency), pipeline.Transactional[RegisterOrganization, PartyDTO](d.UoW))

	svc.Rename = chain(PermPartyUpdate, func(ctx context.Context, c RenameParty) (PartyDTO, error) {
		return updateParty(ctx, c.ID, func(p *domain.Party, _ *domain.Catalog) error {
			if p.Kind() == domain.KindPerson {
				n, err := domain.NewPersonalName(c.GivenName, c.FirstSurname, c.SecondSurname)
				if err != nil {
					return err
				}
				return p.RenamePerson(n)
			}
			n, err := domain.NewOrganizationName(c.LegalName, c.TradeName)
			if err != nil {
				return err
			}
			return p.RenameOrganization(n)
		})
	}, pipeline.RetryOnConflict[RenameParty, PartyDTO](retry, backoff))

	svc.SetActive = chain(PermPartyUpdate, func(ctx context.Context, c SetPartyActive) (PartyDTO, error) {
		return updateParty(ctx, c.ID, func(p *domain.Party, _ *domain.Catalog) error {
			if c.Active {
				p.Activate()
			} else {
				p.Deactivate()
			}
			return nil
		})
	}, pipeline.RetryOnConflict[SetPartyActive, PartyDTO](retry, backoff))

	svc.AssignRole = chain(PermRoleAssign, func(ctx context.Context, c AssignRole) (PartyDTO, error) {
		role, err := parseRoleType(c.RoleType)
		if err != nil {
			return PartyDTO{}, err
		}
		return updateParty(ctx, c.PartyID, func(p *domain.Party, cat *domain.Catalog) error {
			_, err := p.AssignRole(cat, role, nowOr(c.From))
			return err
		})
	}, pipeline.RetryOnConflict[AssignRole, PartyDTO](retry, backoff))

	svc.EndRole = chain(PermRoleAssign, func(ctx context.Context, c EndRole) (PartyDTO, error) {
		return updateParty(ctx, c.PartyID, func(p *domain.Party, _ *domain.Catalog) error {
			return p.EndRole(c.RoleID, nowOr(c.At))
		})
	}, pipeline.RetryOnConflict[EndRole, PartyDTO](retry, backoff))

	svc.EstablishRelationship = chain(PermRelationshipCreate, func(ctx context.Context, c EstablishRelationship) (RelationshipDTO, error) {
		var v fw.Validation
		typeID, err := domain.ParseRelationshipTypeID(c.Type)
		v.Require(err == nil, "type", "format", "type must be a relationship type id")
		fromID, err := domain.ParsePartyID(c.From)
		v.Require(err == nil, "fromParty", "format", "fromParty must be a party id")
		toID, err := domain.ParsePartyID(c.To)
		v.Require(err == nil, "toParty", "format", "toParty must be a party id")
		if err := v.Err(); err != nil {
			return RelationshipDTO{}, err
		}
		cat, err := s.roleCatalog(ctx)
		if err != nil {
			return RelationshipDTO{}, err
		}
		types, err := s.relationshipTypes(ctx)
		if err != nil {
			return RelationshipDTO{}, err
		}
		rt, ok := types[typeID]
		if !ok {
			v.Add("type", "unknown", "unknown relationship type")
			return RelationshipDTO{}, v.Err()
		}
		from, err := d.Parties.Get(ctx, fromID)
		if err != nil {
			return RelationshipDTO{}, err
		}
		to, err := d.Parties.Get(ctx, toID)
		if err != nil {
			return RelationshipDTO{}, err
		}
		since := nowOr(c.Since)
		if dup, err := d.Relationships.Exists(ctx, domain.SameRelationship(rt, fromID, toID, since)); err != nil {
			return RelationshipDTO{}, err
		} else if dup {
			return RelationshipDTO{}, fw.Violation("parties.duplicate_relationship", "the parties already have this relationship in that period")
		}
		r, err := domain.Establish(domain.NewRelationshipID(), rt, from, to, cat, since, strings.TrimSpace(c.Remark))
		if err != nil {
			return RelationshipDTO{}, err
		}
		if err := relationships.Create(ctx, r); err != nil {
			return RelationshipDTO{}, err
		}
		return RelationshipToDTO(r, types), nil
	}, pipeline.Transactional[EstablishRelationship, RelationshipDTO](d.UoW))

	svc.TerminateRelationship = chain(PermRelationshipEnd, func(ctx context.Context, c TerminateRelationship) (RelationshipDTO, error) {
		types, err := s.relationshipTypes(ctx)
		if err != nil {
			return RelationshipDTO{}, err
		}
		r, err := relationships.Update(ctx, c.ID, func(_ context.Context, r *domain.Relationship) error { return r.Terminate(nowOr(c.At)) })
		if err != nil {
			return RelationshipDTO{}, err
		}
		return RelationshipToDTO(r, types), nil
	}, pipeline.RetryOnConflict[TerminateRelationship, RelationshipDTO](retry, backoff))

	svc.Get = chain(PermPartyRead, func(ctx context.Context, q GetParty) (PartyDTO, error) {
		cat, err := s.roleCatalog(ctx)
		if err != nil {
			return PartyDTO{}, err
		}
		p, err := d.Parties.Get(ctx, q.ID)
		if err != nil {
			return PartyDTO{}, err
		}
		return ToDTO(p, cat), nil
	})

	svc.Search = chain(PermPartyRead, func(ctx context.Context, q SearchParties) (fw.Page[PartyDTO], error) {
		cat, err := s.roleCatalog(ctx)
		if err != nil {
			return fw.Page[PartyDTO]{}, err
		}
		parts := []spec.Specification[*domain.Party]{}
		if t := strings.TrimSpace(q.Text); t != "" {
			parts = append(parts, domain.NameContains(t))
		}
		if q.Kind != "" {
			parts = append(parts, domain.OfKind(domain.Kind(q.Kind)))
		}
		if q.ActiveOnly {
			parts = append(parts, domain.Active())
		}
		if q.Role != "" {
			role, err := parseRoleType(q.Role)
			if err != nil {
				return fw.Page[PartyDTO]{}, err
			}
			parts = append(parts, domain.PlaysAt(fw.Now(), cat.Descendants(role)...))
		}
		page, err := d.Parties.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.FieldName.Asc()))
		if err != nil {
			return fw.Page[PartyDTO]{}, err
		}
		return fw.MapPage(page, func(p *domain.Party) PartyDTO { return ToDTO(p, cat) }), nil
	})

	svc.Relationships = chain(PermRelationshipRead, func(ctx context.Context, q PartyRelationships) ([]RelationshipDTO, error) {
		types, err := s.relationshipTypes(ctx)
		if err != nil {
			return nil, err
		}
		where := domain.Involving(q.PartyID)
		if q.ActiveOnly {
			where = where.And(domain.ActiveAt(fw.Now()))
		}
		rs, err := d.Relationships.Find(ctx, where, domain.RelFieldSince.Asc())
		if err != nil {
			return nil, err
		}
		out := make([]RelationshipDTO, len(rs))
		for i, r := range rs {
			out[i] = RelationshipToDTO(r, types)
		}
		return out, nil
	})

	svc.ListRoleTypes = chain(PermPartyRead, func(ctx context.Context, _ ListRoleTypes) ([]RoleTypeDTO, error) {
		cat, err := s.roleCatalog(ctx)
		if err != nil {
			return nil, err
		}
		out := []RoleTypeDTO{}
		for _, t := range cat.All() {
			dto := RoleTypeDTO{ID: t.ID.String(), Name: t.Name.String(), Description: t.Description, Category: t.Category,
				Applicability: string(cat.Applicability(t.ID))}
			if t.Parent != nil {
				dto.Parent = t.Parent.String()
			}
			out = append(out, dto)
		}
		return out, nil
	})

	svc.ListRelationshipTypes = chain(PermPartyRead, func(ctx context.Context, _ ListRelationshipTypes) ([]RelationshipTypeDTO, error) {
		types, err := d.Catalogs.RelationshipTypes(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]RelationshipTypeDTO, len(types))
		for i, t := range types {
			out[i] = RelationshipTypeDTO{ID: t.ID.String(), Name: t.Name.String(), Description: t.Description,
				FromRole: t.FromRole.String(), ToRole: t.ToRole.String()}
		}
		return out, nil
	})
	return svc
}

// chain decorates a handler: permission first, then the given middleware.
func chain[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	all := append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)
	var nonNil []app.Middleware[In, Out]
	for _, m := range all {
		if m != nil {
			nonNil = append(nonNil, m)
		}
	}
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), nonNil...)
}

func idempotent[In, Out any](store app.IdempotencyStore) app.Middleware[In, Out] {
	if store == nil {
		return nil
	}
	return pipeline.Idempotent[In, Out](store, 24*time.Hour)
}

func parseRoleType(s string) (domain.RoleTypeID, error) {
	id, err := domain.ParseRoleTypeID(s)
	if err != nil {
		var v fw.Validation
		v.Add("roleType", "format", "role type must be a role type id")
		return domain.RoleTypeID{}, v.Err()
	}
	return id, nil
}
