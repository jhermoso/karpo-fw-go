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

// FacilityInfo es lo que Parties necesita de una instalación del contexto Facilities.
type FacilityInfo struct {
	Organization fw.UUID
	Name         string
	Active       bool
}

// FacilityDirectory resuelve instalaciones (el puerto es de Parties; un adaptador lo implementa
// sobre los contratos de Facilities).
type FacilityDirectory interface {
	Facilities(ctx context.Context, ids []fw.UUID) (map[fw.UUID]FacilityInfo, error)
}

// AssignFacilityRole hace que una party desempeñe un rol en una instalación (un empleado en el
// centro de trabajo, la sede de una organización...).
type AssignFacilityRole struct {
	PartyID  domain.PartyID `json:"-"`
	Facility string         `json:"facility"`
	RoleType string         `json:"roleType"`
	From     *time.Time     `json:"from,omitempty"`
}

// EndFacilityRole termina un rol en una instalación (ahora si At es nil).
type EndFacilityRole struct {
	PartyID domain.PartyID        `json:"-"`
	RoleID  domain.FacilityRoleID `json:"-"`
	At      *time.Time            `json:"at,omitempty"`
}

// ListFacilityRoleTypes lista el catálogo de roles en instalaciones.
type ListFacilityRoleTypes struct{}

// FacilityRoleDTO es un rol de una party en una instalación.
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

// addFacilityRoles conecta los casos de uso de los roles en instalaciones.
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
		// Una instalación de otra organización no existe para el llamante (404 uniforme).
		if sc := s.scope(ctx); !ok || (!sc.global && !slices.Contains(sc.orgs, domain.PartyID{UUID: info.Organization})) {
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

// facilitySpec filtra las parties con un rol en una instalación ahora.
func facilitySpec(id string) (spec.Spec[*domain.Party], error) {
	u, err := fw.ParseUUID(id)
	if err != nil {
		var v fw.Validation
		v.Add("facility", "format", "facility must be a facility id")
		return spec.Spec[*domain.Party]{}, v.Err()
	}
	return domain.AtFacility(fw.Now(), u), nil
}
