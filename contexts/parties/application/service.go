package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
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
	UpdatePerson          app.CommandHandler[UpdatePerson, PartyDTO]
	SetActive             app.CommandHandler[SetPartyActive, PartyDTO]
	SetLegalForm          app.CommandHandler[SetLegalForm, PartyDTO]
	FlagDocumentIssue     app.CommandHandler[FlagDocumentIssue, PartyDTO]
	ClearDocumentIssue    app.CommandHandler[ClearDocumentIssue, PartyDTO]
	SetShared             app.CommandHandler[SetShared, PartyDTO]
	AssignRole            app.CommandHandler[AssignRole, PartyDTO]
	EndRole               app.CommandHandler[EndRole, PartyDTO]
	AnnulRole             app.CommandHandler[AnnulRole, PartyDTO]
	EstablishRelationship app.CommandHandler[EstablishRelationship, RelationshipDTO]
	TerminateRelationship app.CommandHandler[TerminateRelationship, RelationshipDTO]
	SetProspectTrial      app.CommandHandler[SetProspectTrial, RelationshipDTO]
	SetOwnershipShare     app.CommandHandler[SetOwnershipShare, RelationshipDTO]
	SetPromotionCode      app.CommandHandler[SetPromotionCode, RelationshipDTO]
	SetCustomerNumber     app.CommandHandler[SetCustomerNumber, RelationshipDTO]
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
	DefineRoleType          app.CommandHandler[DefineRoleType, RoleTypeDTO]
	DefineRelationshipType  app.CommandHandler[DefineRelationshipType, RelationshipTypeDTO]
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

// catalogMu lets one addition to a catalog in at a time. That a name and a code are free is
// read before writing, and a catalog with a code twice cannot be loaded: two additions at once
// with the same code would both fit and leave every use case of the context failing. It holds
// while there is one process (docs/ANFITRION.md); several need a unique index (BACKLOG.md).
var catalogMu sync.Mutex

// catalogAdded leaves in the audit log, in the unit of work of ctx, that somebody added an entry
// to a catalog: who, when and what it says. A catalog entry is not an aggregate, so nobody else
// writes this record.
func (s service) catalogAdded(ctx context.Context, kind, id string, fields map[string]any) error {
	if s.Audit == nil {
		return nil
	}
	return s.Audit.Append(ctx, app.AuditRecord{ID: fw.NewUUID().String(), AggregateType: kind, AggregateID: id, AggregateVersion: 1,
		Operation: app.AuditCreated, Actor: app.ActorFrom(ctx), Channel: app.Channel(ctx), CorrelationID: app.CorrelationID(ctx),
		At: fw.Now(), Changes: traits.Diff(nil, fields)})
}

func (s service) relationshipTypes(ctx context.Context) (map[domain.RelationshipTypeID]domain.RelationshipType, error) {
	types, err := s.Catalogs.RelationshipTypes(ctx)
	if err != nil {
		return nil, err
	}
	return domain.IndexRelationshipTypes(types)
}

// updateParty loads a party in scope, checks the caller may write it, applies fn and saves.
func (s service) updateParty(ctx context.Context, id domain.PartyID, fn func(*domain.Party, *domain.Catalog) error) (PartyDTO, error) {
	n, err := s.names(ctx)
	if err != nil {
		return PartyDTO{}, err
	}
	p, err := s.parties.Update(ctx, id, func(ctx context.Context, p *domain.Party) error {
		// The scope reads the audit log inside the unit of work it is in (D-74).
		if err := s.scope(ctx).writable(p); err != nil {
			return err
		}
		if err := operable(p); err != nil {
			return err
		}
		return fn(p, n.Roles)
	})
	if err != nil {
		return PartyDTO{}, err
	}
	return n.dto(p), nil
}

// scope is the scope of the caller, able to tell from the audit log which parties an
// organization registered.
func (s service) scope(ctx context.Context) scope {
	sc := scopeOf(ctx)
	sc.registered = func(p *domain.Party, a domain.Affiliation) bool { return s.registeredWith(ctx, p, a) }
	return sc
}

// registeredWith reports whether the party was registered together with the relationship behind
// the affiliation: the first audit record of each, in one operation (the same correlation). It
// is how "the organization registered it" is known without keeping it anywhere else
// (docs/DECISIONES-COMUNES.md, D-74). Without an audit log, or when in doubt, it says no.
func (s service) registeredWith(ctx context.Context, p *domain.Party, a domain.Affiliation) bool {
	if s.Audit == nil {
		return false
	}
	first := func(kind, id string) string {
		trail, err := s.Audit.Trail(ctx, kind, id)
		if err != nil || len(trail) == 0 {
			return ""
		}
		return trail[0].CorrelationID
	}
	c := first(domain.PartyKind, p.ID().String())
	return c != "" && c == first(domain.RelationshipKind, a.Relationship.String())
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

	svc.UpdatePerson = chain(PermPartyUpdate, func(ctx context.Context, c UpdatePerson) (PartyDTO, error) {
		var v fw.Validation
		g, err := domain.ParseGender(c.Gender)
		v.Merge("", err)
		m, err := domain.ParseMaritalStatus(c.MaritalStatus)
		v.Merge("", err)
		var birth vocab.Date
		if c.BirthDate != "" {
			if birth, err = vocab.ParseDate(c.BirthDate); err != nil {
				v.Add("birthDate", "format", "birth date must be YYYY-MM-DD")
			}
		}
		if err := v.Err(); err != nil {
			return PartyDTO{}, err
		}
		return s.updateParty(ctx, c.ID, func(p *domain.Party, _ *domain.Catalog) error { return p.UpdatePersonDetails(g, birth, m) })
	}, pipeline.RetryOnConflict[UpdatePerson, PartyDTO](retry, backoff))

	svc.SetActive = chain(PermPartyUpdate, func(ctx context.Context, c SetPartyActive) (PartyDTO, error) {
		// A party something in force depends on is not deactivated: its relationships are ended
		// first (docs/DECISIONES-COMUNES.md, D-4). The command asks before it does anything.
		if !c.Active {
			p, err := d.Parties.Get(ctx, c.ID)
			if err != nil {
				return PartyDTO{}, err
			}
			if err := s.scope(ctx).writable(p); err != nil { // before telling anything about its relationships
				return PartyDTO{}, err
			}
			related, err := d.Relationships.Exists(ctx, spec.And(domain.Involving(c.ID), domain.NotEndedAt(fw.Now())))
			if err != nil {
				return PartyDTO{}, err
			}
			if related {
				return PartyDTO{}, fw.Violation("parties.has_relationships", "the party has relationships that have not ended; end them first")
			}
		}
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

	svc.FlagDocumentIssue = chain(PermPartyUpdate, func(ctx context.Context, c FlagDocumentIssue) (PartyDTO, error) {
		return s.updateParty(ctx, c.ID, func(p *domain.Party, _ *domain.Catalog) error { return p.FlagDocumentIssue(c.Number, c.Reason) })
	}, pipeline.RetryOnConflict[FlagDocumentIssue, PartyDTO](retry, backoff))

	svc.ClearDocumentIssue = chain(PermPartyUpdate, func(ctx context.Context, c ClearDocumentIssue) (PartyDTO, error) {
		return s.updateParty(ctx, c.ID, func(p *domain.Party, _ *domain.Catalog) error { p.ClearDocumentIssue(); return nil })
	}, pipeline.RetryOnConflict[ClearDocumentIssue, PartyDTO](retry, backoff))

	// Only global administrators decide which parties every organization sees.
	svc.SetShared = chain(PermPartyUpdate, func(ctx context.Context, c SetShared) (PartyDTO, error) {
		if sc := s.scope(ctx); !sc.global {
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

	svc.AnnulRole = chain(PermRoleAnnul, func(ctx context.Context, c AnnulRole) (PartyDTO, error) {
		// What depends on a role are the relationships the party has, or had, in it. The command
		// looks before it does anything (D-8): with none, the role is removed; with any, annulled.
		rels, err := d.Relationships.Find(ctx, domain.Involving(c.PartyID))
		if err != nil {
			return PartyDTO{}, err
		}
		return s.updateParty(ctx, c.PartyID, func(p *domain.Party, cat *domain.Catalog) error {
			i := slices.IndexFunc(p.Roles(), func(r domain.PartyRole) bool { return r.ID == c.RoleID })
			if i < 0 {
				return fw.NotFound("parties.party_role", c.RoleID)
			}
			role := p.Roles()[i].RoleType
			// Internal Organization is never removed: grants and affiliations may point at the
			// company, and that it once was one is worth keeping.
			depended := cat.IsA(role, domain.RoleInternalOrganization) || slices.ContainsFunc(rels, func(r *domain.Relationship) bool {
				return (r.From() == p.ID() && cat.IsA(role, r.FromRole())) || (r.To() == p.ID() && cat.IsA(role, r.ToRole()))
			})
			_, err := p.AnnulRole(c.RoleID, fw.Now(), depended)
			return err
		})
	}, pipeline.RetryOnConflict[AnnulRole, PartyDTO](retry, backoff))

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
		sc := s.scope(ctx)
		from, err := sc.visible(d.Parties.Get(ctx, fromID))
		if err != nil {
			return RelationshipDTO{}, err
		}
		to, err := sc.visible(d.Parties.Get(ctx, toID))
		if err != nil {
			return RelationshipDTO{}, err
		}
		// A Duplicate Of asks to be able to change the duplicate only (establish checks it); the
		// survivor is just pointed at.
		if rt.ID != domain.RelDuplicateOf && !sc.canRelate(from, to) {
			return RelationshipDTO{}, fw.ErrForbidden
		}
		r, err := s.establish(ctx, rt, from, to, nowOr(c.Since), strings.TrimSpace(c.Remark), c.RelationshipDetailsInput)
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
		p, err := s.scope(ctx).visible(d.Parties.Get(ctx, q.ID))
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
		parts := []spec.Specification[*domain.Party]{s.scope(ctx).parties(now)}
		if t := strings.TrimSpace(q.Text); t != "" {
			parts = append(parts, domain.NameContains(t))
		}
		if q.Kind != "" {
			parts = append(parts, domain.OfKind(domain.Kind(q.Kind)))
		}
		if q.ActiveOnly {
			parts = append(parts, domain.Active())
		}
		if q.DocumentIssue {
			parts = append(parts, domain.FieldDocumentIssue.Eq(true))
		}
		if !q.IncludeMergedParties {
			parts = append(parts, domain.FieldDuplicate.Eq(false))
		}
		if q.Role != "" {
			role, err := parseRoleType(q.Role)
			if err != nil {
				return fw.Page[PartyDTO]{}, err
			}
			if q.IncludeEndedRoles {
				parts = append(parts, domain.HasPlayed(n.Roles.Descendants(role)...))
			} else {
				parts = append(parts, domain.PlaysAt(now, n.Roles.Descendants(role)...))
			}
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
		sc := s.scope(ctx)
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
		sc := s.scope(ctx)
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
			out = append(out, roleTypeDTO(cat, t))
		}
		return out, nil
	})

	svc.DefineRoleType = chain(PermRoleTypeDefine, func(ctx context.Context, c DefineRoleType) (RoleTypeDTO, error) {
		if !s.scope(ctx).global {
			return RoleTypeDTO{}, fmt.Errorf("%w: the role catalog belongs to the whole installation", fw.ErrForbidden)
		}
		catalogMu.Lock()
		defer catalogMu.Unlock()
		parent, err := parseRoleType(c.Parent)
		if err != nil {
			var v fw.Validation
			v.Add("parent", "format", "parent must be a role type id")
			return RoleTypeDTO{}, v.Err()
		}
		t, err := domain.NewRoleType(domain.RoleTypeID{UUID: fw.NewUUID()}, c.Name, c.Description, c.Code, parent)
		if err != nil {
			return RoleTypeDTO{}, err
		}
		cat, err := s.roleCatalog(ctx)
		if err != nil {
			return RoleTypeDTO{}, err
		}
		if err := cat.Admits(t); err != nil {
			return RoleTypeDTO{}, err
		}
		err = s.UoW.Do(ctx, func(ctx context.Context) error {
			if err := s.Catalogs.AddRoleType(ctx, t); err != nil {
				return err
			}
			return s.catalogAdded(ctx, domain.RoleTypeKind, t.ID.String(), map[string]any{"name": t.Name.String(), "code": t.Code,
				"description": t.Description, "parent": t.Parent.String()})
		})
		if err != nil {
			return RoleTypeDTO{}, err
		}
		if cat, err = s.roleCatalog(ctx); err != nil {
			return RoleTypeDTO{}, err
		}
		return roleTypeDTO(cat, t), nil
	})

	svc.DefineRelationshipType = chain(PermRelationshipTypeDefine, func(ctx context.Context, c DefineRelationshipType) (RelationshipTypeDTO, error) {
		if !s.scope(ctx).global {
			return RelationshipTypeDTO{}, fmt.Errorf("%w: the relationship catalog belongs to the whole installation", fw.ErrForbidden)
		}
		catalogMu.Lock()
		defer catalogMu.Unlock()
		var v fw.Validation
		from, err := parseRoleType(c.FromRole)
		v.Require(err == nil, "fromRole", "format", "fromRole must be a role type id")
		to, err := parseRoleType(c.ToRole)
		v.Require(err == nil, "toRole", "format", "toRole must be a role type id")
		if err := v.Err(); err != nil {
			return RelationshipTypeDTO{}, err
		}
		t, err := domain.NewRelationshipType(domain.RelationshipTypeID{UUID: fw.NewUUID()}, c.Name, c.Description, c.Code, from, to)
		if err != nil {
			return RelationshipTypeDTO{}, err
		}
		roles, err := s.roleCatalog(ctx)
		if err != nil {
			return RelationshipTypeDTO{}, err
		}
		types, err := s.relationshipTypes(ctx)
		if err != nil {
			return RelationshipTypeDTO{}, err
		}
		if err := domain.AdmitsRelationshipType(roles, types, t); err != nil {
			return RelationshipTypeDTO{}, err
		}
		err = s.UoW.Do(ctx, func(ctx context.Context) error {
			if err := s.Catalogs.AddRelationshipType(ctx, t); err != nil {
				return err
			}
			return s.catalogAdded(ctx, domain.RelationshipTypeKind, t.ID.String(), map[string]any{"name": t.Name.String(), "code": t.Code,
				"description": t.Description, "fromRole": t.FromRole.String(), "toRole": t.ToRole.String()})
		})
		if err != nil {
			return RelationshipTypeDTO{}, err
		}
		return relationshipTypeDTO(t), nil
	})

	svc.ListRelationshipTypes = chain(PermPartyRead, func(ctx context.Context, _ ListRelationshipTypes) ([]RelationshipTypeDTO, error) {
		types, err := d.Catalogs.RelationshipTypes(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]RelationshipTypeDTO, len(types))
		for i, t := range types {
			out[i] = relationshipTypeDTO(t)
		}
		return out, nil
	})

	addPhase2(svc, s)
	addFacilityRoles(svc, s)
	addRelationshipDetails(svc, s)
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
