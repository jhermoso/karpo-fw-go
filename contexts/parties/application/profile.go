package application

import (
	"context"
	"errors"
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

// SetInternalOrganizationProfile reemplaza el perfil de una empresa interna: un campo vacío,
// nulo o ausente lo limpia, y vuelve el comportamiento de siempre (D-133, D-144, D-148).
type SetInternalOrganizationProfile struct {
	ID                 domain.PartyID `json:"-"`
	TimeZone           string         `json:"timeZone"`
	LineRounding       string         `json:"lineRounding"`
	PriceDecimals      *int           `json:"priceDecimals"`
	InstallmentsTiming string         `json:"installmentsTiming"`
	DefaultPriceList   string         `json:"defaultPriceList"`
	AutomaticPricing   *bool          `json:"automaticPricing"`
}

// InternalOrganizationProfileDTO es la forma de transporte del perfil de una empresa interna.
// DefaultPriceList se omite si no hay tarifa general; AutomaticPricing es el valor que rige (sí,
// si no se guardó ninguno).
type InternalOrganizationProfileDTO struct {
	InternalOrganizationID string  `json:"internalOrganizationId"`
	TimeZone               *string `json:"timeZone"`
	LineRounding           *string `json:"lineRounding"`
	PriceDecimals          *int    `json:"priceDecimals"`
	InstallmentsTiming     *string `json:"installmentsTiming"`
	DefaultPriceList       *string `json:"defaultPriceList,omitempty"`
	AutomaticPricing       bool    `json:"automaticPricing"`
}

// GetInternalOrganizationCodePattern lee el patrón de un tipo de código de una empresa interna.
type GetInternalOrganizationCodePattern struct {
	ID   domain.PartyID `json:"-"`
	Kind string         `json:"-"`
}

// SetInternalOrganizationCodePattern fija el patrón de un tipo de código de una empresa interna;
// vacío o nulo, lo quita.
type SetInternalOrganizationCodePattern struct {
	ID      domain.PartyID `json:"-"`
	Kind    string         `json:"-"`
	Pattern string         `json:"pattern"`
}

// CodePatternDTO es la forma de transporte del patrón de un tipo de código.
type CodePatternDTO struct {
	InternalOrganizationID string  `json:"internalOrganizationId"`
	CodeKind               string  `json:"codeKind"`
	Pattern                *string `json:"pattern"`
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func profileDTO(p domain.InternalOrganizationProfile) InternalOrganizationProfileDTO {
	return InternalOrganizationProfileDTO{InternalOrganizationID: p.Organization.String(), TimeZone: optional(p.TimeZone),
		LineRounding: optional(p.LineRounding), PriceDecimals: p.PriceDecimals, InstallmentsTiming: optional(p.InstallmentsTiming),
		DefaultPriceList: optional(p.DefaultPriceList), AutomaticPricing: p.PricesAutomatically()}
}

// profileAudit son los campos del perfil tal como quedan en el registro de auditoría.
func profileAudit(p domain.InternalOrganizationProfile) map[string]any {
	var decimals any
	if p.PriceDecimals != nil {
		decimals = *p.PriceDecimals
	}
	var automatic any
	if p.AutomaticPricing != nil {
		automatic = *p.AutomaticPricing
	}
	return map[string]any{"timeZone": p.TimeZone, "lineRounding": p.LineRounding, "priceDecimals": decimals,
		"installmentsTiming": p.InstallmentsTiming, "defaultPriceList": p.DefaultPriceList, "automaticPricing": automatic}
}

// addProfile conecta los casos de uso del perfil de la empresa interna (D-133, D-144) y de sus
// patrones de código. Se lee con el
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
		p.SetAutomaticPricing(c.AutomaticPricing)
		if err := errors.Join(p.SetTimeZone(c.TimeZone), p.SetLineRounding(c.LineRounding), p.SetPriceDecimals(c.PriceDecimals),
			p.SetInstallmentsTiming(c.InstallmentsTiming), p.SetDefaultPriceList(c.DefaultPriceList)); err != nil {
			return InternalOrganizationProfileDTO{}, firstError(err)
		}
		if p.Equal(before) {
			return profileDTO(p), nil
		}
		if err := s.checkDefaultPriceList(ctx, p, before); err != nil {
			return InternalOrganizationProfileDTO{}, err
		}
		if err := s.Profiles.Save(ctx, p); err != nil {
			return InternalOrganizationProfileDTO{}, err
		}
		if err := s.profileChanged(ctx, c.ID, profileAudit(before), profileAudit(p)); err != nil {
			return InternalOrganizationProfileDTO{}, err
		}
		return profileDTO(p), nil
	}, // dos primeros guardados a la vez: el segundo choca y se repite entero (infrastructure.SQLProfiles.Save)
		pipeline.RetryOnConflict[SetInternalOrganizationProfile, InternalOrganizationProfileDTO](3, 10*time.Millisecond),
		pipeline.Transactional[SetInternalOrganizationProfile, InternalOrganizationProfileDTO](s.UoW))

	svc.InternalOrganizationCodePattern = chain(PermPartyRead, func(ctx context.Context, q GetInternalOrganizationCodePattern) (CodePatternDTO, error) {
		if err := s.internalOrganization(ctx, q.ID, false); err != nil {
			return CodePatternDTO{}, err
		}
		kind, err := domain.ParseCodeKind(q.Kind)
		if err != nil {
			return CodePatternDTO{}, err
		}
		pattern, err := s.Profiles.CodePattern(ctx, q.ID, kind)
		if err != nil {
			return CodePatternDTO{}, err
		}
		return CodePatternDTO{InternalOrganizationID: q.ID.String(), CodeKind: kind, Pattern: optional(pattern)}, nil
	})

	svc.SetInternalOrganizationCodePattern = chain(PermPartyUpdate, func(ctx context.Context, c SetInternalOrganizationCodePattern) (CodePatternDTO, error) {
		if err := s.internalOrganization(ctx, c.ID, true); err != nil {
			return CodePatternDTO{}, err
		}
		kind, err := domain.ParseCodeKind(c.Kind)
		if err != nil {
			return CodePatternDTO{}, err
		}
		pattern, err := domain.ParseCodePattern(c.Pattern)
		if err != nil {
			return CodePatternDTO{}, err
		}
		before, err := s.Profiles.CodePattern(ctx, c.ID, kind)
		if err != nil {
			return CodePatternDTO{}, err
		}
		out := CodePatternDTO{InternalOrganizationID: c.ID.String(), CodeKind: kind, Pattern: optional(pattern)}
		if pattern == before {
			return out, nil
		}
		if err := s.Profiles.SaveCodePattern(ctx, c.ID, kind, pattern); err != nil {
			return CodePatternDTO{}, err
		}
		field := "codePatterns." + kind
		if err := s.profileChanged(ctx, c.ID, map[string]any{field: before}, map[string]any{field: pattern}); err != nil {
			return CodePatternDTO{}, err
		}
		return out, nil
	}, pipeline.RetryOnConflict[SetInternalOrganizationCodePattern, CodePatternDTO](3, 10*time.Millisecond),
		pipeline.Transactional[SetInternalOrganizationCodePattern, CodePatternDTO](s.UoW))
}

// checkDefaultPriceList comprueba una tarifa general nueva o cambiada con el puerto de Products, si
// lo hay: una que no existe, es de otra empresa o está retirada da 422. La que ya estaba no se
// vuelve a mirar, para que retirarla después no impida cambiar el resto del perfil (Products se la
// salta al cotizar).
func (s service) checkDefaultPriceList(ctx context.Context, p, before domain.InternalOrganizationProfile) error {
	if s.PriceLists == nil || p.DefaultPriceList == "" || p.DefaultPriceList == before.DefaultPriceList {
		return nil
	}
	ok, err := s.PriceLists.UsableDefaultPriceList(ctx, p.Organization.String(), p.DefaultPriceList)
	if err != nil {
		return err
	}
	if !ok {
		return fw.Violation(domain.ErrUnknownDefaultPriceList, "the default price list does not exist, belongs to another company or is retired")
	}
	return nil
}

// firstError devuelve la primera violación de varias juntas (errors.Join): la respuesta lleva un
// solo código.
func firstError(err error) error {
	if j, ok := err.(interface{ Unwrap() []error }); ok && len(j.Unwrap()) > 0 {
		return j.Unwrap()[0]
	}
	return err
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

// profileChanged deja en el registro de auditoría el cambio del perfil o de uno de sus patrones,
// en la unidad de trabajo de ctx. El perfil no es un agregado: su versión es el número de cambios
// registrados.
func (s service) profileChanged(ctx context.Context, org domain.PartyID, before, after map[string]any) error {
	if s.Audit == nil {
		return nil
	}
	id := org.String()
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
		Changes: traits.Diff(before, after)})
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

// CompanyProfiles implementa contracts.CompanyProfiles sobre los perfiles: los ajustes de
// facturación de cada empresa interna (D-144). Sirve a otros contextos, sin los permisos del
// usuario, como CompanyTimeZones.
type CompanyProfiles struct {
	Profiles domain.InternalOrganizationProfiles
}

var _ contracts.CompanyProfiles = CompanyProfiles{}

// Settings implementa contracts.CompanyProfiles.
func (c CompanyProfiles) Settings(ctx context.Context, organization string) (contracts.CompanySettings, error) {
	id, err := domain.ParsePartyID(organization)
	if err != nil || id.IsZero() || c.Profiles == nil {
		return contracts.CompanySettings{}, nil
	}
	p, err := c.Profiles.Get(ctx, id)
	if err != nil {
		return contracts.CompanySettings{}, err
	}
	return contracts.CompanySettings{LineRounding: p.LineRounding, PriceDecimals: p.PriceDecimals, InstallmentsTiming: p.InstallmentsTiming,
		DefaultPriceList: p.DefaultPriceList, ManualPricing: !p.PricesAutomatically()}, nil
}

// CodePattern implementa contracts.CompanyProfiles.
func (c CompanyProfiles) CodePattern(ctx context.Context, organization, codeKind string) (string, error) {
	id, err := domain.ParsePartyID(organization)
	if err != nil || id.IsZero() || c.Profiles == nil {
		return "", nil
	}
	return c.Profiles.CodePattern(ctx, id, codeKind)
}
