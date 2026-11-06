package application

import (
	"context"
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// GetInternalOrganizationProfile lee el perfil de una empresa interna.
type GetInternalOrganizationProfile struct {
	ID domain.PartyID `json:"-"`
}

// SetInternalOrganizationProfile reemplaza el perfil de una empresa interna. Una zona vacía o
// nula la limpia.
type SetInternalOrganizationProfile struct {
	ID       domain.PartyID `json:"-"`
	TimeZone string         `json:"timeZone"`
}

// InternalOrganizationProfileDTO es la forma de transporte del perfil de una empresa interna.
type InternalOrganizationProfileDTO struct {
	InternalOrganizationID string  `json:"internalOrganizationId"`
	TimeZone               *string `json:"timeZone"`
}

func profileDTO(p domain.InternalOrganizationProfile) InternalOrganizationProfileDTO {
	out := InternalOrganizationProfileDTO{InternalOrganizationID: p.Organization.String()}
	if p.TimeZone != "" {
		tz := p.TimeZone
		out.TimeZone = &tz
	}
	return out
}

// addProfile conecta los casos de uso del perfil de la empresa interna (D-133). Se lee con el
// permiso de leer parties y se cambia con el de modificarlas, dentro del ámbito: fuera de él, o si
// la party no es ahora una organización interna, no existe (404); escribirlo es escribir la
// empresa, así que sólo pueden sus propios usuarios con acceso Full y el administrador global.
func addProfile(svc *Service, s service) {
	svc.InternalOrganizationProfile = chain(PermPartyRead, func(ctx context.Context, q GetInternalOrganizationProfile) (InternalOrganizationProfileDTO, error) {
		if err := s.internalOrganization(ctx, q.ID, false); err != nil {
			return InternalOrganizationProfileDTO{}, err
		}
		p, err := s.Profiles.Get(ctx, q.ID)
		if err != nil {
			return InternalOrganizationProfileDTO{}, err
		}
		return profileDTO(p), nil
	})

	svc.SetInternalOrganizationProfile = chain(PermPartyUpdate, func(ctx context.Context, c SetInternalOrganizationProfile) (InternalOrganizationProfileDTO, error) {
		if err := s.internalOrganization(ctx, c.ID, true); err != nil {
			return InternalOrganizationProfileDTO{}, err
		}
		p, err := s.Profiles.Get(ctx, c.ID)
		if err != nil {
			return InternalOrganizationProfileDTO{}, err
		}
		before := p
		if err := p.SetTimeZone(c.TimeZone); err != nil {
			return InternalOrganizationProfileDTO{}, err
		}
		if p == before {
			return profileDTO(p), nil
		}
		if err := s.Profiles.Save(ctx, p); err != nil {
			return InternalOrganizationProfileDTO{}, err
		}
		if err := s.profileChanged(ctx, before, p); err != nil {
			return InternalOrganizationProfileDTO{}, err
		}
		return profileDTO(p), nil
	}, // dos primeros guardados a la vez: el segundo choca y se repite entero (infrastructure.SQLProfiles.Save)
		pipeline.RetryOnConflict[SetInternalOrganizationProfile, InternalOrganizationProfileDTO](3, 10*time.Millisecond),
		pipeline.Transactional[SetInternalOrganizationProfile, InternalOrganizationProfileDTO](s.UoW))
}

// internalOrganization comprueba que la party existe en el ámbito del llamante (y que puede
// escribirla, si write) y que es ahora una organización interna.
func (s service) internalOrganization(ctx context.Context, id domain.PartyID, write bool) error {
	if s.Profiles == nil {
		return fmt.Errorf("%w: internal organization profiles are not configured", fw.ErrUnsupported)
	}
	sc := s.scope(ctx)
	p, err := sc.visible(s.Parties.Get(ctx, id))
	if err != nil {
		return err
	}
	if !isInternalOrganization(p, fw.Now()) {
		return fw.NotFound(domain.InternalOrganizationProfileKind, id)
	}
	if write {
		return sc.writable(p)
	}
	return nil
}

// profileChanged deja en el registro de auditoría el cambio del perfil, en la unidad de trabajo de
// ctx. El perfil no es un agregado: su versión es el número de cambios registrados.
func (s service) profileChanged(ctx context.Context, before, after domain.InternalOrganizationProfile) error {
	if s.Audit == nil {
		return nil
	}
	id := after.Organization.String()
	trail, err := s.Audit.Trail(ctx, domain.InternalOrganizationProfileKind, id)
	if err != nil {
		return err
	}
	op := app.AuditUpdated
	if len(trail) == 0 {
		op = app.AuditCreated
	}
	return s.Audit.Append(ctx, app.AuditRecord{ID: fw.NewUUID().String(), AggregateType: domain.InternalOrganizationProfileKind,
		AggregateID: id, AggregateVersion: int64(len(trail) + 1), Operation: op, Actor: app.ActorFrom(ctx), Channel: app.Channel(ctx),
		CorrelationID: app.CorrelationID(ctx), At: fw.Now(),
		Changes: traits.Diff(map[string]any{"timeZone": before.TimeZone}, map[string]any{"timeZone": after.TimeZone})})
}

// CompanyTimeZones implementa contracts.CompanyTimeZones sobre los perfiles. Sirve a otros
// contextos, así que no exige un permiso de Parties (como Directory).
type CompanyTimeZones struct {
	Profiles domain.InternalOrganizationProfiles
}

var _ contracts.CompanyTimeZones = CompanyTimeZones{}

// TimeZone implementa contracts.CompanyTimeZones.
func (c CompanyTimeZones) TimeZone(ctx context.Context, organization string) (string, error) {
	id, err := domain.ParsePartyID(organization)
	if err != nil || id.IsZero() || c.Profiles == nil {
		return "", nil
	}
	p, err := c.Profiles.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return p.TimeZone, nil
}
