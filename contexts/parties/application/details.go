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

// addRelationshipDetails wires the use cases that change the details a relationship carries
// because of its type (docs/PARTIES-UDM.md).
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

	svc.SetPromotionCode = chain(PermRelationshipUpdate, func(ctx context.Context, c SetPromotionCode) (RelationshipDTO, error) {
		code := domain.NormalizePromotionCode(c.PromotionCode)
		if code != "" {
			// A code tells one collaborator of the organization from the others. Asked before
			// the change, with its own read: who may not see the relationship is told so below.
			if r, err := s.relationships.Repository().Get(ctx, c.ID); err == nil {
				same, err := s.relationships.Repository().Find(ctx, spec.And(domain.RelFieldType.Eq(r.Type()), domain.RelFieldTo.Eq(r.To()),
					domain.RelFieldPromotionCode.Eq(code), domain.ActiveAt(fw.Now())))
				if err != nil {
					return RelationshipDTO{}, err
				}
				for _, o := range same {
					if o.ID() != r.ID() {
						if _, _, _, err := s.writableRelationship(ctx, c.ID); err != nil {
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

// Collaborators implements contracts.Collaborators over the collaborator relationships. Like
// Directory, it serves contexts, not users: the caller's own use case is what is authorized.
type Collaborators struct {
	Relationships domain.RelationshipRepository
	Catalogs      domain.Catalogs
}

var _ contracts.Collaborators = Collaborators{}

// ByPromotionCode implements contracts.Collaborators.
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

// changeDetails loads a relationship the caller may change, applies fn with its type and saves.
func (s service) changeDetails(ctx context.Context, id domain.RelationshipID, fn func(*domain.Relationship, domain.RelationshipType) error) (RelationshipDTO, error) {
	types, err := s.relationshipTypes(ctx)
	if err != nil {
		return RelationshipDTO{}, err
	}
	if _, _, _, err := s.writableRelationship(ctx, id); err != nil {
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

// Trials implements contracts.Trials over the prospect relationships (one query per batch).
// Like Directory, it serves contexts, not users: the caller's own use case is what is authorized.
type Trials struct {
	Relationships domain.RelationshipRepository
	Catalogs      domain.Catalogs
}

var _ contracts.Trials = Trials{}

// Trials implements contracts.Trials.
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
