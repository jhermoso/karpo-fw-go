package infrastructure

import (
	"context"
	"slices"

	fac "github.com/jhermoso/karpo-fw-go/contexts/facilities/contracts"
	happ "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	"github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	parties "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// PartiesOrganizations adapts the Parties contracts to the HR Organizations port (ACL).
type PartiesOrganizations struct {
	Hierarchy  parties.OrganizationHierarchy
	Membership parties.Membership
}

var _ happ.Organizations = PartiesOrganizations{}

// InternalOrganizationOf implements happ.Organizations.
func (p PartiesOrganizations) InternalOrganizationOf(ctx context.Context, units []domain.OrganizationID) (map[domain.OrganizationID]domain.OrganizationID, error) {
	ids := make([]string, len(units))
	for i, u := range units {
		ids[i] = u.String()
	}
	refs, err := p.Hierarchy.InternalOrganizationOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[domain.OrganizationID]domain.OrganizationID, len(refs))
	for unit, ref := range refs {
		u, err1 := fw.ParseUUID(unit)
		o, err2 := fw.ParseUUID(ref.ID)
		if err1 == nil && err2 == nil {
			out[domain.OrganizationID{UUID: u}] = domain.OrganizationID{UUID: o}
		}
	}
	return out, nil
}

// Affiliated implements happ.Organizations.
func (p PartiesOrganizations) Affiliated(ctx context.Context, person domain.PersonID, org domain.OrganizationID) (bool, error) {
	m, err := p.Membership.InternalOrganizations(ctx, []string{person.String()})
	if err != nil {
		return false, err
	}
	return slices.Contains(m[person.String()], org.String()), nil
}

// FacilitiesDirectory adapts the Facilities directory to the HR Facilities port (ACL).
type FacilitiesDirectory struct{ Directory fac.Directory }

var _ happ.Facilities = FacilitiesDirectory{}

// Facility implements happ.Facilities.
func (f FacilitiesDirectory) Facility(ctx context.Context, id domain.FacilityID) (happ.FacilityInfo, bool, error) {
	refs, err := f.Directory.Resolve(ctx, []string{id.String()})
	if err != nil {
		return happ.FacilityInfo{}, false, err
	}
	ref, ok := refs[id.String()]
	if !ok {
		return happ.FacilityInfo{}, false, nil
	}
	owner, err := fw.ParseUUID(ref.Organization)
	if err != nil {
		return happ.FacilityInfo{}, false, err
	}
	return happ.FacilityInfo{Name: ref.Name, Owner: domain.OrganizationID{UUID: owner}, Active: ref.Active}, true, nil
}
