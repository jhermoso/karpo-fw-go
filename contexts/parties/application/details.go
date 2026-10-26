package application

import (
	"context"
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// addRelationshipDetails conecta los casos de uso que cambian los datos que una relación lleva por
// su tipo (docs/PARTIES-UDM.md).
func addRelationshipDetails(svc *Service, s service) {
	svc.SetProspectTrial = chain(PermRelationshipSetTrial, func(ctx context.Context, c SetProspectTrial) (RelationshipDTO, error) {
		return s.changeDetails(ctx, c.ID, func(r *domain.Relationship, rt domain.RelationshipType) error {
			return r.SetTrial(rt, c.TrialUntil)
		})
	}, pipeline.RetryOnConflict[SetProspectTrial, RelationshipDTO](3, 10*time.Millisecond))

	svc.SetOwnershipShare = chain(PermRelationshipUpdate, func(ctx context.Context, c SetOwnershipShare) (RelationshipDTO, error) {
		share, err := parseShare(c.Share)
		if err != nil {
			return RelationshipDTO{}, err
		}
		return s.changeDetails(ctx, c.ID, func(r *domain.Relationship, rt domain.RelationshipType) error {
			return r.SetOwnershipShare(rt, share)
		})
	}, pipeline.RetryOnConflict[SetOwnershipShare, RelationshipDTO](3, 10*time.Millisecond))

	svc.SetCustomerNumber = chain(PermRelationshipUpdate, func(ctx context.Context, c SetCustomerNumber) (RelationshipDTO, error) {
		number := domain.NormalizeCustomerNumber(c.CustomerNumber)
		// El número es de la numeración de la organización (el lado que vende): quien lo da tiene
		// que poder escribir en esa organización, no sólo en una de las dos parties. Se pregunta
		// primero, para que nadie sepa qué números están cogidos sin tener permiso para dar uno.
		r, _, _, err := s.operableRelationship(ctx, c.ID)
		if err != nil {
			return RelationshipDTO{}, err
		}
		if sc := s.scope(ctx); !sc.global && !sc.canWriteOrg(r.To()) {
			return RelationshipDTO{}, fmt.Errorf("%w: the customer number is given by the organization that numbers its customers", fw.ErrForbidden)
		}
		if number != "" {
			// Un número distingue a un cliente de la organización de los demás: ningún otro cliente
			// lo recibe mientras una relación del primero, en vigor o terminada, lo lleve, y el
			// mismo cliente lo conserva si vuelve.
			same, err := s.relationships.Repository().Find(ctx, spec.And(domain.RelFieldType.Eq(r.Type()), domain.RelFieldTo.Eq(r.To()),
				domain.RelFieldCustomerNumber.Eq(number)))
			if err != nil {
				return RelationshipDTO{}, err
			}
			for _, o := range same {
				if o.From() != r.From() {
					return RelationshipDTO{}, fw.Violation("parties.duplicate_customer_number",
						"another customer of the organization has, or had, that customer number")
				}
			}
		}
		return s.changeDetails(ctx, c.ID, func(r *domain.Relationship, rt domain.RelationshipType) error {
			return r.SetCustomerNumber(rt, number)
		})
	}, pipeline.RetryOnConflict[SetCustomerNumber, RelationshipDTO](3, 10*time.Millisecond))

	svc.SetPromotionCode = chain(PermRelationshipUpdate, func(ctx context.Context, c SetPromotionCode) (RelationshipDTO, error) {
		code := domain.NormalizePromotionCode(c.PromotionCode)
		if code != "" {
			// Un código distingue a un colaborador de la organización de los demás. Se pregunta
			// antes del cambio, con su propia lectura: a quien no puede ver la relación se le dice
			// más abajo. Dos cambios a la vez que pasen los dos esta lectura los detiene en SQL el
			// índice único de la migración 29, con la misma violación (docs/PARTIES.md §22).
			if r, err := s.relationships.Repository().Get(ctx, c.ID); err == nil {
				same, err := s.relationships.Repository().Find(ctx, spec.And(domain.RelFieldType.Eq(r.Type()), domain.RelFieldTo.Eq(r.To()),
					domain.RelFieldPromotionCode.Eq(code), domain.ActiveAt(fw.Now())))
				if err != nil {
					return RelationshipDTO{}, err
				}
				for _, o := range same {
					if o.ID() != r.ID() {
						if _, _, _, err := s.operableRelationship(ctx, c.ID); err != nil {
							return RelationshipDTO{}, err
						}
						return RelationshipDTO{}, fw.Violation("parties.duplicate_promotion_code",
							"another collaborator of the organization already has that promotion code")
					}
				}
			}
		}
		return s.changeDetails(ctx, c.ID, func(r *domain.Relationship, rt domain.RelationshipType) error {
			return r.SetPromotionCode(rt, code)
		})
	}, pipeline.RetryOnConflict[SetPromotionCode, RelationshipDTO](3, 10*time.Millisecond))
}

// Collaborators implementa contracts.Collaborators sobre las relaciones de colaborador. Como
// Directory, sirve a contextos, no a usuarios: lo que se autoriza es el caso de uso del llamante.
type Collaborators struct {
	Relationships domain.RelationshipRepository
	Catalogs      domain.Catalogs
}

var _ contracts.Collaborators = Collaborators{}

// ByPromotionCode implementa contracts.Collaborators.
func (c Collaborators) ByPromotionCode(ctx context.Context, organization, code string) (contracts.Collaborator, bool, error) {
	org, err := domain.ParsePartyID(organization)
	if err != nil {
		return contracts.Collaborator{}, false, fmt.Errorf("%w: organization must be a party id", fw.ErrValidation)
	}
	code = domain.NormalizePromotionCode(code)
	if code == "" {
		return contracts.Collaborator{}, false, nil
	}
	types, err := c.Catalogs.RelationshipTypes(ctx)
	if err != nil {
		return contracts.Collaborator{}, false, err
	}
	var collaborator []domain.RelationshipTypeID
	for _, rt := range types {
		if rt.Code == domain.CodeCollaborator {
			collaborator = append(collaborator, rt.ID)
		}
	}
	if len(collaborator) == 0 {
		return contracts.Collaborator{}, false, nil
	}
	rs, err := c.Relationships.Find(ctx, spec.And(domain.RelFieldType.In(collaborator...), domain.RelFieldTo.Eq(org),
		domain.RelFieldPromotionCode.Eq(code), domain.ActiveAt(fw.Now())))
	if err != nil || len(rs) == 0 {
		return contracts.Collaborator{}, false, err
	}
	return contracts.Collaborator{PartyID: rs[0].From().String(), RelationshipID: rs[0].ID().String(), PromotionCode: code}, true, nil
}

// changeDetails carga una relación que el llamante puede cambiar, le aplica fn con su tipo y la
// guarda.
func (s service) changeDetails(ctx context.Context, id domain.RelationshipID, fn func(*domain.Relationship, domain.RelationshipType) error) (RelationshipDTO, error) {
	types, err := s.relationshipTypes(ctx)
	if err != nil {
		return RelationshipDTO{}, err
	}
	if _, _, _, err := s.operableRelationship(ctx, id); err != nil {
		return RelationshipDTO{}, err
	}
	r, err := s.relationships.Update(ctx, id, func(_ context.Context, r *domain.Relationship) error {
		return fn(r, types[r.Type()])
	})
	if err != nil {
		return RelationshipDTO{}, err
	}
	return RelationshipToDTO(r, types), nil
}

// Trials implementa contracts.Trials sobre las relaciones de prospecto (una consulta por lote).
// Como Directory, sirve a contextos, no a usuarios: lo que se autoriza es el caso de uso del llamante.
type Trials struct {
	Relationships domain.RelationshipRepository
	Catalogs      domain.Catalogs
}

var _ contracts.Trials = Trials{}

// Trials implementa contracts.Trials.
func (t Trials) Trials(ctx context.Context, organization string, partyIDs []string) (map[string]contracts.Trial, error) {
	if len(partyIDs) > contracts.MaxDirectoryBatch {
		return nil, fmt.Errorf("%w: at most %d ids per call", fw.ErrValidation, contracts.MaxDirectoryBatch)
	}
	org, err := domain.ParsePartyID(organization)
	if err != nil {
		return nil, fmt.Errorf("%w: organization must be a party id", fw.ErrValidation)
	}
	out := map[string]contracts.Trial{}
	ids := parseIDs(partyIDs)
	if len(ids) == 0 {
		return out, nil
	}
	types, err := t.Catalogs.RelationshipTypes(ctx)
	if err != nil {
		return nil, err
	}
	var prospect []domain.RelationshipTypeID
	for _, rt := range types {
		if rt.Code == domain.CodeProspect {
			prospect = append(prospect, rt.ID)
		}
	}
	if len(prospect) == 0 {
		return out, nil
	}
	now := fw.Now()
	rs, err := t.Relationships.Find(ctx, spec.And(domain.RelFieldType.In(prospect...), domain.RelFieldTo.Eq(org),
		domain.RelFieldFrom.In(ids...), domain.ActiveAt(now)))
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		out[r.From().String()] = contracts.Trial{PartyID: r.From().String(), RelationshipID: r.ID().String(), Since: r.Since(),
			Until: r.TrialUntil(), InForce: r.InTrialAt(now)}
	}
	return out, nil
}
