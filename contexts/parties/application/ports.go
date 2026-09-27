package application

import (
	"context"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Organizations implements the organization ports other contexts use (membership, hierarchy,
// internal organization catalog). Like Directory, it serves contexts, not users: the caller's
// own use case is what is authorized.
type Organizations struct {
	Parties       domain.PartyRepository
	Relationships domain.RelationshipRepository
	Catalogs      domain.Catalogs
}

var (
	_ contracts.Membership                  = Organizations{}
	_ contracts.OrganizationHierarchy       = Organizations{}
	_ contracts.InternalOrganizationCatalog = Organizations{}
)

func parseIDs(ids []string) []domain.PartyID {
	out := make([]domain.PartyID, 0, len(ids))
	for _, s := range ids {
		if id, err := domain.ParsePartyID(s); err == nil && !id.IsZero() && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func idsOf(ids []domain.PartyID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// InternalOrganizations implements contracts.Membership from the affiliations of the parties
// (one query per batch).
func (o Organizations) InternalOrganizations(ctx context.Context, partyIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(partyIDs))
	for _, s := range partyIDs {
		out[s] = []string{}
	}
	ids := parseIDs(partyIDs)
	if len(ids) == 0 {
		return out, nil
	}
	ps, err := o.Parties.Find(ctx, domain.WithIDs(ids...))
	if err != nil {
		return nil, err
	}
	now := fw.Now()
	for _, p := range ps {
		orgs := []string{}
		for _, org := range p.OrganizationsAt(now) {
			orgs = append(orgs, org.String())
		}
		out[p.ID().String()] = orgs
	}
	return out, nil
}

func (o Organizations) hierarchicalTypes(ctx context.Context) ([]domain.RelationshipTypeID, error) {
	types, err := o.Catalogs.RelationshipTypes(ctx)
	if err != nil {
		return nil, err
	}
	var out []domain.RelationshipTypeID
	for _, t := range types {
		if t.Hierarchical {
			out = append(out, t.ID)
		}
	}
	return out, nil
}

// Descendants implements contracts.OrganizationHierarchy: one query per level, at most
// MaxHierarchyDepth levels, a visited node is never walked twice.
func (o Organizations) Descendants(ctx context.Context, organizationIDs []string) ([]string, error) {
	seeds := parseIDs(organizationIDs)
	types, err := o.hierarchicalTypes(ctx)
	if err != nil || len(seeds) == 0 || len(types) == 0 {
		return idsOf(seeds), err
	}
	owned := slices.Clone(seeds)
	level := seeds
	now := fw.Now()
	for depth := 0; depth < MaxHierarchyDepth && len(level) > 0; depth++ {
		rs, err := o.Relationships.Find(ctx, spec.And(domain.RelFieldType.In(types...), domain.RelFieldTo.In(level...), domain.ActiveAt(now)))
		if err != nil {
			return nil, err
		}
		level = nil
		for _, r := range rs {
			if !slices.Contains(owned, r.From()) {
				owned = append(owned, r.From())
				level = append(level, r.From())
			}
		}
	}
	return idsOf(owned), nil
}

// InternalOrganizationOf implements contracts.OrganizationHierarchy: it walks up the rollups,
// one level per query, until a party playing Internal Organization.
func (o Organizations) InternalOrganizationOf(ctx context.Context, unitIDs []string) (map[string]contracts.PartyRef, error) {
	out := map[string]contracts.PartyRef{}
	units := parseIDs(unitIDs)
	types, err := o.hierarchicalTypes(ctx)
	if err != nil || len(units) == 0 {
		return out, err
	}
	roleTypes, err := o.Catalogs.RoleTypes(ctx)
	if err != nil {
		return nil, err
	}
	cat, err := domain.NewRoleCatalog(roleTypes, domain.RolePersonCategory, domain.RoleOrganizationCategory)
	if err != nil {
		return nil, err
	}
	now := fw.Now()
	current := map[domain.PartyID]domain.PartyID{} // unit -> node being examined
	for _, u := range units {
		current[u] = u
	}
	for depth := 0; depth <= MaxHierarchyDepth && len(current) > 0; depth++ {
		var nodes []domain.PartyID
		for _, n := range current {
			if !slices.Contains(nodes, n) {
				nodes = append(nodes, n)
			}
		}
		ps, err := o.Parties.Find(ctx, domain.WithIDs(nodes...))
		if err != nil {
			return nil, err
		}
		internal := map[domain.PartyID]*domain.Party{}
		for _, p := range ps {
			if p.PlaysAt(cat, domain.RoleInternalOrganization, now) {
				internal[p.ID()] = p
			}
		}
		parent := map[domain.PartyID]domain.PartyID{}
		if len(types) > 0 {
			rs, err := o.Relationships.Find(ctx, spec.And(domain.RelFieldType.In(types...), domain.RelFieldFrom.In(nodes...), domain.ActiveAt(now)))
			if err != nil {
				return nil, err
			}
			for _, r := range rs {
				parent[r.From()] = r.To()
			}
		}
		for unit, node := range current {
			if p, ok := internal[node]; ok {
				out[unit.String()] = contracts.PartyRef{ID: p.ID().String(), Name: p.Name(), Active: p.IsActive()}
				delete(current, unit)
			} else if up, ok := parent[node]; ok {
				current[unit] = up
			} else {
				delete(current, unit)
			}
		}
	}
	return out, nil
}

// All implements contracts.InternalOrganizationCatalog: every party playing Internal
// Organization now.
func (o Organizations) All(ctx context.Context) ([]contracts.PartyRef, error) {
	ps, err := o.Parties.Find(ctx, domain.PlaysAt(fw.Now(), domain.RoleInternalOrganization), domain.FieldName.Asc())
	if err != nil {
		return nil, err
	}
	out := make([]contracts.PartyRef, len(ps))
	for i, p := range ps {
		out[i] = contracts.PartyRef{ID: p.ID().String(), Name: p.Name(), Active: p.IsActive()}
	}
	return out, nil
}
