package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// FacilityInfo is what Parties needs of a facility of the Facilities context.
type FacilityInfo struct {
	Organization fw.UUID
	Name         string
	Active       bool
}

// FacilityDirectory resolves facilities (the port Parties owns; an adapter implements it over the
// Facilities contracts).
type FacilityDirectory interface {
	Facilities(ctx context.Context, ids []fw.UUID) (map[fw.UUID]FacilityInfo, error)
}

// AssignFacilityRole makes a party play a role at a facility (an employee at the work center, the
// headquarters of an organization...).
type AssignFacilityRole struct {
	PartyID  domain.PartyID `json:"-"`
	Facility string         `json:"facility"`
	RoleType string         `json:"roleType"`
	From     *time.Time     `json:"from,omitempty"`
}

// EndFacilityRole ends a facility role (now when At is nil).
type EndFacilityRole struct {
	PartyID domain.PartyID        `json:"-"`
	RoleID  domain.FacilityRoleID `json:"-"`
	At      *time.Time            `json:"at,omitempty"`
}

// ListFacilityRoleTypes lists the facility role catalog.
type ListFacilityRoleTypes struct{}

// FacilityRoleDTO is a facility role of a party.
type FacilityRoleDTO struct {
	ID           string     `json:"id"`
	Facility     string     `json:"facility"`
	FacilityName string     `json:"facilityName,omitempty"`
	RoleType     string     `json:"roleType"`
	From         time.Time  `json:"from"`
	Until        *time.Time `json:"until,omitempty"`
	Active       bool       `json:"active"`
}

func facilityRoleDTOs(p *domain.Party) []FacilityRoleDTO {
	out := []FacilityRoleDTO{}
	now := fw.Now()
	for _, r := range p.FacilityRoles() {
		d := FacilityRoleDTO{ID: r.ID.String(), Facility: r.Facility.String(), RoleType: r.RoleType.String(), From: r.Period.From(),
			Active: r.Period.IsActiveAt(now)}
		if t, ok := r.Period.To(); ok {
			d.Until = &t
		}
		out = append(out, d)
	}
	return out
}

// addFacilityRoles wires the facility role use cases.
func addFacilityRoles(svc *Service, s service) {
	retry, backoff := 3, 10*time.Millisecond
	svc.AssignFacilityRole = chain(PermPartyUpdate, func(ctx context.Context, c AssignFacilityRole) (PartyDTO, error) {
		if s.FacilityDirectory == nil {
			return PartyDTO{}, fmt.Errorf("%w: no facility directory configured", fw.ErrUnsupported)
		}
		var v fw.Validation
		facility, err := fw.ParseUUID(c.Facility)
		v.Require(err == nil, "facility", "format", "facility must be a facility id")
		role, err := domain.ParseFacilityRoleTypeID(c.RoleType)
		v.Require(err == nil, "roleType", "format", "roleType must be a facility role type id")
		if err := v.Err(); err != nil {
			return PartyDTO{}, err
		}
		found, err := s.FacilityDirectory.Facilities(ctx, []fw.UUID{facility})
		if err != nil {
			return PartyDTO{}, err
		}
		info, ok := found[facility]
		// A facility of another organization does not exist for the caller (uniform 404).
		if sc := scopeOf(ctx); !ok || (!sc.global && !slices.Contains(sc.orgs, domain.PartyID{UUID: info.Organization})) {
			return PartyDTO{}, fw.NotFound("facilities.facility", facility)
		}
		if !info.Active {
			return PartyDTO{}, fw.Violation("parties.facility_inactive", "the facility "+info.Name+" is inactive")
		}
		types, err := s.Catalogs.FacilityRoleTypes(ctx)
		if err != nil {
			return PartyDTO{}, err
		}
		return s.updateParty(ctx, c.PartyID, func(p *domain.Party, _ *domain.Catalog) error {
			_, err := p.AssignFacilityRole(types, facility, role, nowOr(c.From))
			return err
		})
	}, pipeline.RetryOnConflict[AssignFacilityRole, PartyDTO](retry, backoff))

	svc.EndFacilityRole = chain(PermPartyUpdate, func(ctx context.Context, c EndFacilityRole) (PartyDTO, error) {
		return s.updateParty(ctx, c.PartyID, func(p *domain.Party, _ *domain.Catalog) error { return p.EndFacilityRole(c.RoleID, nowOr(c.At)) })
	}, pipeline.RetryOnConflict[EndFacilityRole, PartyDTO](retry, backoff))

	svc.ListFacilityRoleTypes = chain(PermPartyRead, func(ctx context.Context, _ ListFacilityRoleTypes) ([]domain.FacilityRoleType, error) {
		return s.Catalogs.FacilityRoleTypes(ctx)
	})
}

// facilitySpec filters parties with a role at a facility now.
func facilitySpec(id string) (spec.Spec[*domain.Party], error) {
	u, err := fw.ParseUUID(id)
	if err != nil {
		var v fw.Validation
		v.Add("facility", "format", "facility must be a facility id")
		return spec.Spec[*domain.Party]{}, v.Err()
	}
	return domain.AtFacility(fw.Now(), u), nil
}
