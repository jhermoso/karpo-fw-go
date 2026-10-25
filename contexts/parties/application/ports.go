package application

import (
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Organizations implementa los puertos de organización que usan otros contextos (pertenencia,
// jerarquía, catálogo de organizaciones internas). Como Directory, sirve a contextos, no a
// usuarios: lo que se autoriza es el caso de uso del llamante.
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

// InternalOrganizations implementa contracts.Membership a partir de las afiliaciones de las
// parties (una consulta por lote).
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

// Descendants implementa contracts.OrganizationHierarchy: una consulta por nivel, como mucho
// MaxHierarchyDepth niveles, y un nodo visitado nunca se recorre dos veces.
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

// InternalOrganizationOf implementa contracts.OrganizationHierarchy: sube por los
// agrupamientos, un nivel por consulta, hasta una party que desempeñe Internal Organization.
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
	current := map[domain.PartyID]domain.PartyID{} // unidad -> nodo que se examina
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

// All implementa contracts.InternalOrganizationCatalog: toda party que desempeña Internal
// Organization ahora.
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

// MergedParties implementa contracts.MergedParties sobre la marca de duplicado de las parties
// (una consulta; sirve a contextos, no a usuarios: no mira los permisos de quien llama, D-103).
type MergedParties struct{ Parties domain.PartyRepository }

var _ contracts.MergedParties = MergedParties{}

// MergedParties implementa contracts.MergedParties.
func (m MergedParties) MergedParties(ctx context.Context) ([]string, error) {
	ps, err := m.Parties.Find(ctx, domain.FieldDuplicate.Eq(true))
	if err != nil {
		return nil, err
	}
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.ID().String()
	}
	return out, nil
}

// TaxIdentities implementa contracts.TaxIdentities sobre las identificaciones y los contactos
// postales de las parties (una consulta por lote; sirve a contextos, no a usuarios).
type TaxIdentities struct{ Parties domain.PartyRepository }

var _ contracts.TaxIdentities = TaxIdentities{}

// taxDocuments son los documentos que identifican a una party a efectos fiscales, el más
// específico primero.
var taxDocuments = []struct {
	id   domain.DocumentTypeID
	code string
}{
	{domain.MustDocumentTypeID("c0000000-0004-0000-0000-000000000003"), "TXID"},
	{domain.MustDocumentTypeID("c0000000-0004-0000-0000-000000000002"), "NIDN"},
	{domain.MustDocumentTypeID("c0000000-0004-0000-0000-000000000006"), "ARNU"},
}

// TaxIdentities implementa contracts.TaxIdentities: el documento fiscal principal si lo hay; si
// no, TXID, luego NIDN, luego ARNU; la provincia del contacto postal español vigente, primero el
// de facturación.
func (t TaxIdentities) TaxIdentities(ctx context.Context, partyIDs []string) (map[string]contracts.TaxIdentity, error) {
	if len(partyIDs) > contracts.MaxDirectoryBatch {
		return nil, fmt.Errorf("%w: at most %d ids per call", fw.ErrValidation, contracts.MaxDirectoryBatch)
	}
	out := map[string]contracts.TaxIdentity{}
	ids := parseIDs(partyIDs)
	if len(ids) == 0 {
		return out, nil
	}
	ps, err := t.Parties.Find(ctx, domain.WithIDs(ids...))
	if err != nil {
		return nil, err
	}
	now := fw.Now()
	for _, p := range ps {
		ti := contracts.TaxIdentity{PartyID: p.ID().String(), Name: p.Name(), Blocked: p.IsDuplicate()}
		rank := func(i domain.Identification) int {
			for k, d := range taxDocuments {
				if d.id == i.Type {
					if i.Primary {
						return k
					}
					return k + len(taxDocuments)
				}
			}
			return -1
		}
		best := -1
		for _, i := range p.Identifications() {
			if r := rank(i); r >= 0 && (best < 0 || r < best) {
				best = r
				ti.Country, ti.Number, ti.DocumentType = i.Country.String(), i.Number, taxDocuments[r%len(taxDocuments)].code
			}
		}
		for _, purpose := range []domain.Purpose{domain.PurposeBilling, domain.PurposeDefault, ""} {
			if ti.Province != "" {
				break
			}
			for _, c := range p.Contacts() {
				a := c.Address
				if c.Kind == domain.ContactPostal && c.IsActiveAt(now) && a.Country.String() == "ES" && len(a.PostalCode) == 5 &&
					(purpose == "" || c.Has(purpose)) {
					ti.Province = a.PostalCode[:2]
					break
				}
			}
		}
		out[ti.PartyID] = ti
	}
	return out, nil
}
