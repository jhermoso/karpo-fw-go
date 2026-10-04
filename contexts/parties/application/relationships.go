package application

import (
	"context"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// MaxHierarchyDepth bounds every walk of an organization hierarchy (the C# 10 levels).
const MaxHierarchyDepth = 10

// NewAffiliation relates a party being registered to an internal organization, so it is born
// inside the caller's scope (the C# quick-create customer did the same with the customer
// relationship). The party gets the role its side of the relationship type requires.
type NewAffiliation struct {
	Organization     string `json:"organization"`
	RelationshipType string `json:"relationshipType"`
	RelationshipDetailsInput
}

// canRelate: global administrators relate anything; otherwise one side must be an organization
// the caller can write, or the caller must be able to write both parties.
func (s scope) canRelate(from, to *domain.Party) bool {
	now := fw.Now()
	return s.global || s.canWriteOrg(from.ID()) || s.canWriteOrg(to.ID()) || (s.canWrite(from, now) && s.canWrite(to, now))
}

// register creates a party, gives it its roles and, when requested, its first relationship. A
// caller who is not a global administrator must register the party inside its scope.
func (s service) register(ctx context.Context, build func(domain.PartyID) (*domain.Party, error), roles []string, aff *NewAffiliation) (PartyDTO, error) {
	sc := scopeOf(ctx)
	if aff == nil && !sc.global {
		var v fw.Validation
		v.Add("affiliation", "required", "register the party with an internal organization of your scope")
		return PartyDTO{}, v.Err()
	}
	n, err := s.names(ctx)
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
		if _, err := p.AssignRole(n.Roles, id, now); err != nil {
			return PartyDTO{}, err
		}
	}

	var org *domain.Party
	var rt domain.RelationshipType
	partyIsFrom := true
	if aff != nil {
		var v fw.Validation
		orgID, err := domain.ParsePartyID(aff.Organization)
		v.Require(err == nil, "affiliation.organization", "format", "organization must be a party id")
		typeID, err := domain.ParseRelationshipTypeID(aff.RelationshipType)
		v.Require(err == nil, "affiliation.relationshipType", "format", "relationship type must be an id")
		if err := v.Err(); err != nil {
			return PartyDTO{}, err
		}
		if org, err = sc.visible(s.Parties.Get(ctx, orgID)); err != nil {
			return PartyDTO{}, err
		}
		if !sc.canWriteOrg(orgID) {
			return PartyDTO{}, fw.ErrForbidden
		}
		types, err := s.relationshipTypes(ctx)
		if err != nil {
			return PartyDTO{}, err
		}
		var ok bool
		if rt, ok = types[typeID]; !ok {
			v.Add("affiliation.relationshipType", "unknown", "unknown relationship type")
			return PartyDTO{}, v.Err()
		}
		// The organization takes the side it plays; the new party the other one.
		switch {
		case org.PlaysAt(n.Roles, rt.ToRole, now):
			partyIsFrom = true
		case org.PlaysAt(n.Roles, rt.FromRole, now):
			partyIsFrom = false
		default:
			return PartyDTO{}, fw.Violation("parties.relationship_role_missing", "the organization does not play a role of "+rt.Name.String())
		}
		role := rt.FromRole
		if !partyIsFrom {
			role = rt.ToRole
		}
		if !p.PlaysAt(n.Roles, role, now) {
			if _, err := p.AssignRole(n.Roles, role, now); err != nil {
				return PartyDTO{}, err
			}
		}
	}
	if err := s.parties.Create(ctx, p); err != nil {
		return PartyDTO{}, err
	}
	if org != nil {
		from, to := p, org
		if !partyIsFrom {
			from, to = org, p
		}
		if _, err := s.establish(ctx, rt, from, to, now, "", aff.RelationshipDetailsInput); err != nil {
			return PartyDTO{}, err
		}
		if p, err = s.Parties.Get(ctx, p.ID()); err != nil {
			return PartyDTO{}, err
		}
	}
	return n.dto(p), nil
}

// establish creates a relationship and projects it into the parties: a party related to an
// internal organization becomes affiliated with it (visibility, membership). Hierarchical types
// keep one parent per child and no cycles. Everything happens in the caller's unit of work.
func (s service) establish(ctx context.Context, rt domain.RelationshipType, from, to *domain.Party, since time.Time, remark string,
	details RelationshipDetailsInput) (*domain.Relationship, error) {
	cat, err := s.roleCatalog(ctx)
	if err != nil {
		return nil, err
	}
	if rt.Hierarchical {
		if err := s.checkHierarchy(ctx, rt, from.ID(), to.ID(), since); err != nil {
			return nil, err
		}
	}
	if dup, err := s.Relationships.Exists(ctx, domain.SameRelationship(rt, from.ID(), to.ID(), since)); err != nil {
		return nil, err
	} else if dup {
		return nil, fw.Violation("parties.duplicate_relationship", "the parties already have this relationship in that period")
	}
	r, err := domain.Establish(domain.NewRelationshipID(), rt, from, to, cat, since, remark)
	if err != nil {
		return nil, err
	}
	if err := details.apply(r, rt); err != nil {
		return nil, err
	}
	if err := s.relationships.Create(ctx, r); err != nil {
		return nil, err
	}
	for _, side := range [][2]*domain.Party{{from, to}, {to, from}} {
		member, org := side[0], side[1]
		if !org.PlaysAt(cat, domain.RoleInternalOrganization, since) {
			continue
		}
		if _, err := s.parties.Update(ctx, member.ID(), func(_ context.Context, p *domain.Party) error {
			return p.Affiliate(org.ID(), r.ID(), since)
		}); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// checkHierarchy refuses a second current parent and cycles (the parent's ancestors must not
// include the child).
func (s service) checkHierarchy(ctx context.Context, rt domain.RelationshipType, child, parent domain.PartyID, since time.Time) error {
	current := domain.RelFieldType.Eq(rt.ID).And(domain.RelFieldUntil.IsNull().Or(domain.RelFieldUntil.After(since)))
	seen := []domain.PartyID{parent}
	for level, node := 0, parent; ; level++ {
		if node == child {
			return fw.Violation("parties.hierarchy_cycle", "the parent is already below the child in the hierarchy")
		}
		if level >= MaxHierarchyDepth {
			return fw.Violation("parties.hierarchy_too_deep", "the hierarchy would be deeper than the allowed levels")
		}
		ups, err := s.Relationships.Find(ctx, current.And(domain.RelFieldFrom.Eq(node)))
		if err != nil {
			return err
		}
		if len(ups) == 0 {
			break
		}
		node = ups[0].To()
		if slices.Contains(seen, node) {
			break // an existing cycle is not this change's fault; stop walking
		}
		seen = append(seen, node)
	}
	if has, err := s.Relationships.Exists(ctx, spec.And(current, domain.RelFieldFrom.Eq(child))); err != nil {
		return err
	} else if has {
		return fw.Violation("parties.hierarchy_one_parent", "the child already has a parent: end that relationship first")
	}
	return nil
}

// writableRelationship loads a relationship the caller is about to change, with its parties: 404
// when it is out of scope, 403 when the caller may not relate its parties.
func (s service) writableRelationship(ctx context.Context, id domain.RelationshipID) (r *domain.Relationship, from, to *domain.Party, err error) {
	sc := scopeOf(ctx)
	if r, err = s.Relationships.Get(ctx, id); err != nil {
		return nil, nil, nil, err
	}
	if !sc.relationships().IsSatisfiedBy(r) {
		return nil, nil, nil, fw.NotFound(domain.RelationshipKind, id)
	}
	if from, err = s.Parties.Get(ctx, r.From()); err != nil {
		return nil, nil, nil, err
	}
	if to, err = s.Parties.Get(ctx, r.To()); err != nil {
		return nil, nil, nil, err
	}
	if !sc.canRelate(from, to) {
		return nil, nil, nil, fw.ErrForbidden
	}
	return r, from, to, nil
}

// terminate ends a relationship and the affiliations it produced. The caller needs to see the
// relationship and be allowed to relate its parties.
func (s service) terminate(ctx context.Context, id domain.RelationshipID, at time.Time) (*domain.Relationship, error) {
	_, from, to, err := s.writableRelationship(ctx, id)
	if err != nil {
		return nil, err
	}
	r, err := s.relationships.Update(ctx, id, func(_ context.Context, r *domain.Relationship) error { return r.Terminate(at) })
	if err != nil {
		return nil, err
	}
	for _, p := range []*domain.Party{from, to} {
		if !slices.ContainsFunc(p.Affiliations(), func(a domain.Affiliation) bool { return a.Relationship == id }) {
			continue // nothing to end: do not bump the version of a party that did not change
		}
		if _, err := s.parties.Update(ctx, p.ID(), func(_ context.Context, p *domain.Party) error { return p.EndAffiliation(id, at) }); err != nil {
			return nil, err
		}
	}
	return r, nil
}
