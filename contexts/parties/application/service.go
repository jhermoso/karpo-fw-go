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
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
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
	// Addresses validates postal addresses against the Geography context (optional).
	Addresses AddressChecker
	// FacilityDirectory resolves facilities of the Facilities context (optional).
	FacilityDirectory FacilityDirectory
}

// AddressChecker validates the geographic part of a postal address and completes its Geography
// references (the port Parties owns; an adapter implements it over the Geography contracts).
type AddressChecker interface {
	CheckAddress(ctx context.Context, a domain.PostalAddress) (domain.PostalAddress, error)
}

// Service exposes the Parties use cases as decorated, statically typed handlers. Every handler
// requires its permission in the authorization context of ctx, whatever the entry point, and
// works inside the caller's organization scope (see scope).
type Service struct {
	RegisterPerson        app.CommandHandler[RegisterPerson, PartyDTO]
	RegisterOrganization  app.CommandHandler[RegisterOrganization, PartyDTO]
	Rename                app.CommandHandler[RenameParty, PartyDTO]
	SetActive             app.CommandHandler[SetPartyActive, PartyDTO]
	SetLegalForm          app.CommandHandler[SetLegalForm, PartyDTO]
	SetShared             app.CommandHandler[SetShared, PartyDTO]
	AssignRole            app.CommandHandler[AssignRole, PartyDTO]
	EndRole               app.CommandHandler[EndRole, PartyDTO]
	EstablishRelationship app.CommandHandler[EstablishRelationship, RelationshipDTO]
	TerminateRelationship app.CommandHandler[TerminateRelationship, RelationshipDTO]
	AddIdentification     app.CommandHandler[AddIdentification, PartyDTO]
	RemoveIdentification  app.CommandHandler[RemoveIdentification, PartyDTO]
	AddContact            app.CommandHandler[AddContact, PartyDTO]
	SetContactPurposes    app.CommandHandler[SetContactPurposes, PartyDTO]
	EndContact            app.CommandHandler[EndContact, PartyDTO]
	Classify              app.CommandHandler[Classify, PartyDTO]
	EndClassification     app.CommandHandler[EndClassification, PartyDTO]
	AssignFacilityRole    app.CommandHandler[AssignFacilityRole, PartyDTO]
	EndFacilityRole       app.CommandHandler[EndFacilityRole, PartyDTO]

	Get                     app.QueryHandler[GetParty, PartyDTO]
	Search                  app.QueryHandler[SearchParties, fw.Page[PartyDTO]]
	Relationships           app.QueryHandler[PartyRelationships, []RelationshipDTO]
	InternalOrganizations   app.QueryHandler[ListInternalOrganizations, []PartyDTO]
	ListRoleTypes           app.QueryHandler[ListRoleTypes, []RoleTypeDTO]
	ListRelationshipTypes   app.QueryHandler[ListRelationshipTypes, []RelationshipTypeDTO]
	DocumentOptions         app.QueryHandler[DocumentOptions, []DocumentOptionDTO]
	ListClassificationTypes app.QueryHandler[ListClassificationTypes, []ClassificationTypeDTO]
	ListFacilityRoleTypes   app.QueryHandler[ListFacilityRoleTypes, []domain.FacilityRoleType]
}

type (
	partyOrchestrator        = orchestration.Orchestrator[domain.PartyID, *domain.Party]
	relationshipOrchestrator = orchestration.Orchestrator[domain.RelationshipID, *domain.Relationship]
)

type service struct {
	Deps
	parties       *partyOrchestrator
	relationships *relationshipOrchestrator
}

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

// updateParty loads a party in scope, checks the caller may write it, applies fn and saves.
func (s service) updateParty(ctx context.Context, id domain.PartyID, fn func(*domain.Party, *domain.Catalog) error) (PartyDTO, error) {
	n, err := s.names(ctx)
	if err != nil {
		return PartyDTO{}, err
	}
	sc := scopeOf(ctx)
	p, err := s.parties.Update(ctx, id, func(_ context.Context, p *domain.Party) error {
		if err := sc.writable(p); err != nil {
			return err
		}
		return fn(p, n.Roles)
	})
	if err != nil {
		return PartyDTO{}, err
	}
	return n.dto(p), nil
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
		parties:       orchestration.New[domain.PartyID, *domain.Party](d.Parties, d.UoW, opts...),
		relationships: orchestration.New[domain.RelationshipID, *domain.Relationship](d.Relationships, d.UoW, opts...)}

	svc := &Service{}
	retry, backoff := 3, 10*time.Millisecond

	svc.RegisterPerson = chain(PermPartyCreate, func(ctx context.Context, c RegisterPerson) (PartyDTO, error) {
		details, err := c.details()
		if err != nil {
			return PartyDTO{}, err
		}
		return s.register(ctx, func(id domain.PartyID) (*domain.Party, error) { return domain.RegisterPerson(id, details) }, c.Roles, c.Affiliation)
	}, idempotent[RegisterPerson, PartyDTO](d.Idempotency), pipeline.Transactional[RegisterPerson, PartyDTO](d.UoW))

	svc.RegisterOrganization = chain(PermPartyCreate, func(ctx context.Context, c RegisterOrganization) (PartyDTO, error) {
		name, err := domain.NewOrganizationName(c.LegalName, c.TradeName)
		if err != nil {
			return PartyDTO{}, err
		}
		form, err := domain.ParseLegalForm(c.LegalForm)
		if err != nil {
			return PartyDTO{}, err
		}
		return s.register(ctx, func(id domain.PartyID) (*domain.Party, error) {
			return domain.RegisterOrganization(id, domain.OrganizationDetails{Name: name, LegalForm: form})
		}, c.Roles, c.Affiliation)
	}, idempotent[RegisterOrganization, PartyDTO](d.Idempotency), pipeline.Transactional[RegisterOrganization, PartyDTO](d.UoW))

	svc.Rename = chain(PermPartyUpdate, func(ctx context.Context, c RenameParty) (PartyDTO, error) {
		return s.updateParty(ctx, c.ID, func(p *domain.Party, _ *domain.Catalog) error {
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
		return s.updateParty(ctx, c.ID, func(p *domain.Party, _ *domain.Catalog) error {
			if c.Active {
				p.Activate()
			} else {
				p.Deactivate()
			}
			return nil
		})
	}, pipeline.RetryOnConflict[SetPartyActive, PartyDTO](retry, backoff))

	svc.SetLegalForm = chain(PermPartyUpdate, func(ctx context.Context, c SetLegalForm) (PartyDTO, error) {
		f, err := domain.ParseLegalForm(c.LegalForm)
		if err != nil {
			return PartyDTO{}, err
		}
		return s.updateParty(ctx, c.ID, func(p *domain.Party, _ *domain.Catalog) error { return p.SetLegalForm(f) })
	}, pipeline.RetryOnConflict[SetLegalForm, PartyDTO](retry, backoff))

	// Only global administrators decide which parties every organization sees.
	svc.SetShared = chain(PermPartyUpdate, func(ctx context.Context, c SetShared) (PartyDTO, error) {
		if sc := scopeOf(ctx); !sc.global {
			if _, err := sc.visible(d.Parties.Get(ctx, c.ID)); err != nil {
				return PartyDTO{}, err // out of scope: uniform 404
			}
			return PartyDTO{}, fw.ErrForbidden
		}
		return s.updateParty(ctx, c.ID, func(p *domain.Party, _ *domain.Catalog) error { p.Share(c.Shared); return nil })
	}, pipeline.RetryOnConflict[SetShared, PartyDTO](retry, backoff))

	svc.AssignRole = chain(PermRoleAssign, func(ctx context.Context, c AssignRole) (PartyDTO, error) {
		role, err := parseRoleType(c.RoleType)
		if err != nil {
			return PartyDTO{}, err
		}
		return s.updateParty(ctx, c.PartyID, func(p *domain.Party, cat *domain.Catalog) error {
			_, err := p.AssignRole(cat, role, nowOr(c.From))
			return err
		})
	}, pipeline.RetryOnConflict[AssignRole, PartyDTO](retry, backoff))

	svc.EndRole = chain(PermRoleAssign, func(ctx context.Context, c EndRole) (PartyDTO, error) {
		return s.updateParty(ctx, c.PartyID, func(p *domain.Party, _ *domain.Catalog) error {
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
		types, err := s.relationshipTypes(ctx)
		if err != nil {
			return RelationshipDTO{}, err
		}
		rt, ok := types[typeID]
		if !ok {
			v.Add("type", "unknown", "unknown relationship type")
			return RelationshipDTO{}, v.Err()
		}
		sc := scopeOf(ctx)
		from, err := sc.visible(d.Parties.Get(ctx, fromID))
		if err != nil {
			return RelationshipDTO{}, err
		}
		to, err := sc.visible(d.Parties.Get(ctx, toID))
		if err != nil {
			return RelationshipDTO{}, err
		}
		if !sc.canRelate(from, to) {
			return RelationshipDTO{}, fw.ErrForbidden
		}
		r, err := s.establish(ctx, rt, from, to, nowOr(c.Since), strings.TrimSpace(c.Remark))
		if err != nil {
			return RelationshipDTO{}, err
		}
		return RelationshipToDTO(r, types), nil
	}, pipeline.Transactional[EstablishRelationship, RelationshipDTO](d.UoW))

	svc.TerminateRelationship = chain(PermRelationshipEnd, func(ctx context.Context, c TerminateRelationship) (RelationshipDTO, error) {
		types, err := s.relationshipTypes(ctx)
		if err != nil {
			return RelationshipDTO{}, err
		}
		r, err := s.terminate(ctx, c.ID, nowOr(c.At))
		if err != nil {
			return RelationshipDTO{}, err
		}
		return RelationshipToDTO(r, types), nil
	}, pipeline.Transactional[TerminateRelationship, RelationshipDTO](d.UoW))

	svc.Get = chain(PermPartyRead, func(ctx context.Context, q GetParty) (PartyDTO, error) {
		n, err := s.names(ctx)
		if err != nil {
			return PartyDTO{}, err
		}
		p, err := scopeOf(ctx).visible(d.Parties.Get(ctx, q.ID))
		if err != nil {
			return PartyDTO{}, err
		}
		return n.dto(p), nil
	})

	svc.Search = chain(PermPartyRead, func(ctx context.Context, q SearchParties) (fw.Page[PartyDTO], error) {
		n, err := s.names(ctx)
		if err != nil {
			return fw.Page[PartyDTO]{}, err
		}
		now := fw.Now()
		parts := []spec.Specification[*domain.Party]{scopeOf(ctx).parties(now)}
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
			parts = append(parts, domain.PlaysAt(now, n.Roles.Descendants(role)...))
		}
		if doc := strings.TrimSpace(q.Document); doc != "" {
			parts = append(parts, domain.WithDocumentNumber(vocab.NormalizeDocumentNumber(doc)).Or(
				domain.WithDocumentNumber(strings.ToUpper(strings.Join(strings.Fields(doc), "")))))
		}
		if q.Classification != "" {
			typ, err := domain.ParseClassificationTypeID(q.Classification)
			if err != nil {
				var v fw.Validation
				v.Add("classification", "format", "classification must be a classification id")
				return fw.Page[PartyDTO]{}, v.Err()
			}
			parts = append(parts, domain.ClassifiedAt(now, typ))
		}
		if q.Facility != "" {
			at, err := facilitySpec(q.Facility)
			if err != nil {
				return fw.Page[PartyDTO]{}, err
			}
			parts = append(parts, at)
		}
		if q.Organization != "" {
			org, err := domain.ParsePartyID(q.Organization)
			if err != nil {
				var v fw.Validation
				v.Add("organization", "format", "organization must be a party id")
				return fw.Page[PartyDTO]{}, v.Err()
			}
			parts = append(parts, domain.AffiliatedAt(now, org))
		}
		page, err := d.Parties.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.FieldName.Asc()))
		if err != nil {
			return fw.Page[PartyDTO]{}, err
		}
		return fw.MapPage(page, n.dto), nil
	})

	svc.Relationships = chain(PermRelationshipRead, func(ctx context.Context, q PartyRelationships) ([]RelationshipDTO, error) {
		types, err := s.relationshipTypes(ctx)
		if err != nil {
			return nil, err
		}
		sc := scopeOf(ctx)
		if _, err := sc.visible(d.Parties.Get(ctx, q.PartyID)); err != nil {
			return nil, err
		}
		where := domain.Involving(q.PartyID).And(sc.relationships())
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

	svc.InternalOrganizations = chain(PermPartyRead, func(ctx context.Context, _ ListInternalOrganizations) ([]PartyDTO, error) {
		n, err := s.names(ctx)
		if err != nil {
			return nil, err
		}
		now := fw.Now()
		sc := scopeOf(ctx)
		where := domain.PlaysAt(now, domain.RoleInternalOrganization)
		if !sc.global {
			where = where.And(domain.WithIDs(sc.orgs...))
		}
		ps, err := d.Parties.Find(ctx, where, domain.FieldName.Asc())
		if err != nil {
			return nil, err
		}
		out := make([]PartyDTO, len(ps))
		for i, p := range ps {
			out[i] = n.dto(p)
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
				FromRole: t.FromRole.String(), ToRole: t.ToRole.String(), Hierarchical: t.Hierarchical}
		}
		return out, nil
	})

	addPhase2(svc, s)
	addFacilityRoles(svc, s)
	return svc
}

// names loads the catalogs that name the parts of a party in its DTO.
func (s service) names(ctx context.Context) (Names, error) {
	cat, err := s.roleCatalog(ctx)
	if err != nil {
		return Names{}, err
	}
	docs, err := s.documentPolicy(ctx)
	if err != nil {
		return Names{}, err
	}
	classes, err := s.classificationCatalog(ctx)
	if err != nil {
		return Names{}, err
	}
	return Names{Roles: cat, Documents: docs, Classifications: classes}, nil
}

func (n Names) dto(p *domain.Party) PartyDTO { return ToDTO(p, n) }

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
