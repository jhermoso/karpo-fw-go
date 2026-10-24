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

// Deps son los puertos que necesitan los casos de uso; Recorder, Audit e Idempotency son opcionales.
type Deps struct {
	Parties       domain.PartyRepository
	Relationships domain.RelationshipRepository
	Catalogs      domain.Catalogs
	UoW           fw.UnitOfWork
	Recorder      app.EventRecorder
	Audit         app.AuditLog
	Idempotency   app.IdempotencyStore
	// Addresses valida las direcciones postales contra el contexto Geography (opcional).
	Addresses AddressChecker
	// FacilityDirectory resuelve instalaciones del contexto Facilities (opcional).
	FacilityDirectory FacilityDirectory
}

// AddressChecker valida la parte geográfica de una dirección postal y completa sus referencias a
// Geography (el puerto es de Parties; un adaptador lo implementa sobre los contratos de Geography).
type AddressChecker interface {
	CheckAddress(ctx context.Context, a domain.PostalAddress) (domain.PostalAddress, error)
}

// Service expone los casos de uso de Parties como manejadores decorados y de tipo estático. Cada
// manejador exige su permiso en el contexto de autorización de ctx, sea cual sea la entrada, y
// trabaja dentro del ámbito de organizaciones del llamante (véase scope).
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
	SetSelfEmployed       app.CommandHandler[SetSelfEmployedDetails, PartyDTO]
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
	Pairs                   app.QueryHandler[SearchPairs, []PairDTO]
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

// catalogMu deja pasar una sola alta en un catálogo cada vez. Que un nombre y un código estén
// libres se lee antes de escribir, y un catálogo con un código repetido no se puede cargar: dos
// altas a la vez con el mismo código cabrían las dos y dejarían fallando todos los casos de uso
// del contexto. Vale mientras haya un solo proceso (docs/ANFITRION.md); con varios hace falta un
// índice único (BACKLOG.md).
var catalogMu sync.Mutex

// catalogAdded deja en el registro de auditoría, en la unidad de trabajo de ctx, que alguien
// añadió una entrada a un catálogo: quién, cuándo y qué dice. Una entrada de catálogo no es un
// agregado, así que nadie más escribe este registro.
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

// updateParty carga una party del ámbito, comprueba que el llamante puede escribirla, le aplica fn
// y la guarda.
func (s service) updateParty(ctx context.Context, id domain.PartyID, fn func(*domain.Party, *domain.Catalog) error) (PartyDTO, error) {
	n, err := s.names(ctx)
	if err != nil {
		return PartyDTO{}, err
	}
	p, err := s.parties.Update(ctx, id, func(ctx context.Context, p *domain.Party) error {
		// El ámbito lee el registro de auditoría dentro de la unidad de trabajo en la que está (D-74).
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

// scope es el ámbito del llamante, capaz de saber por el registro de auditoría qué parties dio de
// alta una organización.
func (s service) scope(ctx context.Context) scope {
	sc := scopeOf(ctx)
	sc.registered = func(p *domain.Party, a domain.Affiliation) bool { return s.registeredWith(ctx, p, a) }
	return sc
}

// registeredWith dice si la party se dio de alta junto con la relación que hay detrás de la
// afiliación: el primer registro de auditoría de cada una, en una sola operación (la misma
// correlación). Así se sabe que «la organización la dio de alta» sin guardarlo en ningún otro
// sitio (docs/DECISIONES-COMUNES.md, D-74). Sin registro de auditoría, o en caso de duda, dice que
// no.
//
// Una party dada de alta antes de que existiera la regla no cuenta, aunque el registro de
// auditoría pudiera decirlo (D-77): sigue siendo de sólo lectura para los usuarios estándar de la
// organización.
func (s service) registeredWith(ctx context.Context, p *domain.Party, a domain.Affiliation) bool {
	if s.Audit == nil {
		return false
	}
	first := func(kind, id string) (app.AuditRecord, bool) {
		trail, err := s.Audit.Trail(ctx, kind, id)
		if err != nil || len(trail) == 0 {
			return app.AuditRecord{}, false
		}
		return trail[0], true
	}
	party, ok := first(domain.PartyKind, p.ID().String())
	if !ok || party.CorrelationID == "" || party.At.Before(RegistrationRuleSince) {
		return false
	}
	// Una relación que volvió al deshacer una fusión continúa a otra (D-93): si la de la afiliación
	// no nació con la party, se sigue la cadena hacia atrás hasta la de origen. De qué relación viene
	// cada una lo dice su propio primer registro, así que la cadena no cuesta lecturas de relaciones.
	id := a.Relationship.String()
	for range maxTransferChain {
		rel, ok := first(domain.RelationshipKind, id)
		if !ok {
			return false
		}
		if rel.CorrelationID == party.CorrelationID {
			return true
		}
		if id = transferredFromOf(rel); id == "" {
			return false
		}
	}
	return false
}

// transferredFromOf devuelve, del registro de auditoría del alta de una relación, la relación a la
// que continúa ("" si ninguna).
func transferredFromOf(rec app.AuditRecord) string {
	for _, c := range rec.Changes {
		if c.Field == "transferredFrom" {
			if s, ok := c.New.(string); ok {
				return s
			}
		}
	}
	return ""
}

// RegistrationRuleSince es el momento en que entró en vigor «la organización que dio de alta una party la
// cambia» (docs/DECISIONES-COMUNES.md, D-65 y D-77): las parties dadas de alta antes sólo están
// relacionadas.
var RegistrationRuleSince = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)

// mayGive se niega a dar un rol que sólo da el administrador global: Internal Organization, o un
// rol por debajo, hace de una party una empresa del grupo (docs/DECISIONES-COMUNES.md, D-78).
func mayGive(sc scope, cat *domain.Catalog, role domain.RoleTypeID) error {
	if !sc.global && cat.IsA(role, domain.RoleInternalOrganization) {
		return fmt.Errorf("%w: only the global administrator makes a party an internal organization", fw.ErrForbidden)
	}
	return nil
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
		// Una party de la que depende algo en vigor no se desactiva: antes se terminan sus
		// relaciones (docs/DECISIONES-COMUNES.md, D-4). La orden pregunta antes de hacer nada.
		if !c.Active {
			p, err := d.Parties.Get(ctx, c.ID)
			if err != nil {
				return PartyDTO{}, err
			}
			if err := s.scope(ctx).writable(p); err != nil { // antes de contar nada de sus relaciones
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

	// Sólo los administradores globales deciden qué parties ven todas las organizaciones.
	svc.SetShared = chain(PermPartyUpdate, func(ctx context.Context, c SetShared) (PartyDTO, error) {
		if sc := s.scope(ctx); !sc.global {
			if _, err := sc.visible(d.Parties.Get(ctx, c.ID)); err != nil {
				return PartyDTO{}, err // fuera del ámbito: 404 uniforme
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
			if err := mayGive(s.scope(ctx), cat, role); err != nil {
				return err
			}
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
		// Lo que depende de un rol son las relaciones que la party tiene, o tuvo, con él. La orden
		// mira antes de hacer nada (D-8): si no hay ninguna, el rol se borra; si hay alguna, se anula.
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
			// Internal Organization nunca se borra: permisos y afiliaciones pueden apuntar a la
			// empresa, y merece la pena conservar que alguna vez lo fue.
			depended := cat.IsA(role, domain.RoleInternalOrganization) || slices.ContainsFunc(rels, func(r *domain.Relationship) bool {
				return (r.From() == p.ID() && cat.IsA(role, r.FromRole())) || (r.To() == p.ID() && cat.IsA(role, r.ToRole()))
			})
			_, err := p.AnnulRole(c.RoleID, fw.Now(), depended)
			return err
		})
	}, pipeline.RetryOnConflict[AnnulRole, PartyDTO](retry, backoff))

	// El perfil del autónomo es un dato de la party: lo cambia quien puede modificarla (D-85).
	svc.SetSelfEmployed = chain(PermPartyUpdate, func(ctx context.Context, c SetSelfEmployedDetails) (PartyDTO, error) {
		var registered vocab.Date
		if c.RegisteredOn != "" {
			var err error
			if registered, err = vocab.ParseDate(c.RegisteredOn); err != nil {
				var v fw.Validation
				v.Add("registeredOn", "format", "registeredOn must be YYYY-MM-DD")
				return PartyDTO{}, v.Err()
			}
		}
		details, err := domain.NewSelfEmployedDetails(c.TradeName, c.ActivityCode, registered)
		if err != nil {
			return PartyDTO{}, err
		}
		return s.updateParty(ctx, c.PartyID, func(p *domain.Party, cat *domain.Catalog) error {
			return p.SetSelfEmployedDetails(cat, c.RoleID, details)
		})
	}, pipeline.RetryOnConflict[SetSelfEmployedDetails, PartyDTO](retry, backoff))

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
		// Una Duplicate Of pide el permiso de fusionar y poder cambiar las dos parties (lo
		// comprueba establish, D-95 y D-96), no poder relacionarlas.
		if rt.ID != domain.RelDuplicateOf && !sc.canRelate(from, to) {
			return RelationshipDTO{}, fw.ErrForbidden
		}
		r, moment, err := s.establish(ctx, rt, from, to, nowOr(c.Since), strings.TrimSpace(c.Remark), c.RelationshipDetailsInput)
		if err != nil {
			return RelationshipDTO{}, err
		}
		out := RelationshipToDTO(r, types)
		if rt.ID == domain.RelDuplicateOf {
			// En el instante en que se movieron las relaciones, no en r.Since(): las que pasaron
			// empiezan en ese instante, y en r.Since(), si el reloj avanzó entre uno y otro, todavía
			// no habrían empezado y las que cedieron seguirían vigentes.
			if out.Merge, err = s.mergeOf(ctx, r.From(), r.To(), moment); err != nil {
				return RelationshipDTO{}, err
			}
		}
		return out, nil
	}, pipeline.Transactional[EstablishRelationship, RelationshipDTO](d.UoW))

	svc.TerminateRelationship = chain(PermRelationshipEnd, func(ctx context.Context, c TerminateRelationship) (RelationshipDTO, error) {
		types, err := s.relationshipTypes(ctx)
		if err != nil {
			return RelationshipDTO{}, err
		}
		r, merge, err := s.terminate(ctx, c.ID, nowOr(c.At))
		if err != nil {
			return RelationshipDTO{}, err
		}
		out := RelationshipToDTO(r, types)
		out.Merge = merge
		return out, nil
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
		where, err := s.partyFilter(ctx, n, q)
		if err != nil {
			return fw.Page[PartyDTO]{}, err
		}
		page, err := d.Parties.FindPage(ctx, where, fw.NewPageRequest(q.Page, q.Size, domain.FieldName.Asc()))
		if err != nil {
			return fw.Page[PartyDTO]{}, err
		}
		return fw.MapPage(page, n.dto), nil
	})

	svc.Pairs = chain(PermRelationshipRead, func(ctx context.Context, q SearchPairs) ([]PairDTO, error) {
		return s.pairs(ctx, q)
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

// names carga los catálogos que nombran las partes de una party en su DTO.
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

// chain decora un manejador: primero el permiso, después el middleware que se le da.
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

// partyFilter es el filtro de la búsqueda de parties, dentro del ámbito de quien llama. Lo
// comparten la búsqueda de parties y la de parejas, que lo aplica al tercero.
func (s service) partyFilter(ctx context.Context, n Names, q SearchParties) (spec.Spec[*domain.Party], error) {
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
			return spec.Spec[*domain.Party]{}, err
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
			return spec.Spec[*domain.Party]{}, v.Err()
		}
		parts = append(parts, domain.ClassifiedAt(now, typ))
	}
	if q.Facility != "" {
		at, err := facilitySpec(q.Facility)
		if err != nil {
			return spec.Spec[*domain.Party]{}, err
		}
		parts = append(parts, at)
	}
	if q.Organization != "" {
		org, err := domain.ParsePartyID(q.Organization)
		if err != nil {
			var v fw.Validation
			v.Add("organization", "format", "organization must be a party id")
			return spec.Spec[*domain.Party]{}, v.Err()
		}
		parts = append(parts, domain.AffiliatedAt(now, org))
	}
	return spec.And(parts...), nil
}
