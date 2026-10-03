package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// ListPositionTypes lists the position type catalog.
type ListPositionTypes struct{ ActiveOnly bool }

// ListPositionStatuses lists the position status catalog.
type ListPositionStatuses struct{}

// ListPositionClasses lists the position classification catalog.
type ListPositionClasses struct{}

// ListAgreements lists the collective agreements in force for an organization (the global ones
// and its company agreements) or, without organization, those of the caller's scope.
type ListAgreements struct {
	Organization string
	InForceOn    string // civil date; empty: all
}

// AgreementDTO is the transport form of a collective agreement.
type AgreementDTO struct {
	ID              string `json:"id"`
	Code            string `json:"code"`
	Name            string `json:"name"`
	Scope           string `json:"scope"`
	Organization    string `json:"organization,omitempty"`
	TerritorialCode string `json:"territorialCode,omitempty"`
	SectoralCode    string `json:"sectoralCode,omitempty"`
	Start           string `json:"start"`
	End             string `json:"end,omitempty"`
	Active          bool   `json:"active"`
}

var scopeNames = map[domain.AgreementScope]string{domain.ScopeNational: "national", domain.ScopeRegional: "regional",
	domain.ScopeProvincial: "provincial", domain.ScopeCompany: "company"}

func agreementDTO(a domain.Agreement) AgreementDTO {
	d := AgreementDTO{ID: a.ID.String(), Code: a.Code, Name: a.Name, Scope: scopeNames[a.Scope], TerritorialCode: a.TerritorialCode,
		SectoralCode: a.SectoralCode, Start: a.Start.String(), Active: a.Active}
	if !a.Organization.IsZero() {
		d.Organization = a.Organization.String()
	}
	if !a.End.IsZero() {
		d.End = a.End.String()
	}
	return d
}

func (s service) positionTypes(ctx context.Context) (map[domain.PositionTypeID]domain.PositionType, error) {
	ts, err := s.Catalogs.PositionTypes(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[domain.PositionTypeID]domain.PositionType, len(ts))
	for _, t := range ts {
		out[t.ID] = t
	}
	return out, nil
}

func (s service) positionStatuses(ctx context.Context) (map[domain.PositionStatusID]domain.PositionStatus, error) {
	ss, err := s.Catalogs.PositionStatuses(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[domain.PositionStatusID]domain.PositionStatus, len(ss))
	for _, x := range ss {
		out[x.ID] = x
	}
	return out, nil
}

func (s service) agreement(ctx context.Context, id domain.AgreementID) (domain.Agreement, bool, error) {
	as, err := s.Catalogs.Agreements(ctx)
	if err != nil {
		return domain.Agreement{}, false, err
	}
	for _, a := range as {
		if a.ID == id {
			return a, true, nil
		}
	}
	return domain.Agreement{}, false, nil
}

func (s service) catalogUseCases(svc *Service) {
	svc.PositionTypes = guard(PermCatalogRead, func(ctx context.Context, q ListPositionTypes) ([]domain.PositionType, error) {
		ts, err := s.Catalogs.PositionTypes(ctx)
		if err != nil || !q.ActiveOnly {
			return ts, err
		}
		out := ts[:0:0]
		for _, t := range ts {
			if t.Active {
				out = append(out, t)
			}
		}
		return out, nil
	})
	svc.PositionStatuses = guard(PermCatalogRead, func(ctx context.Context, _ ListPositionStatuses) ([]domain.PositionStatus, error) {
		return s.Catalogs.PositionStatuses(ctx)
	})
	svc.PositionClasses = guard(PermCatalogRead, func(ctx context.Context, _ ListPositionClasses) ([]domain.PositionClass, error) {
		return s.Catalogs.PositionClasses(ctx)
	})
	svc.Agreements = guard(PermCatalogRead, func(ctx context.Context, q ListAgreements) ([]AgreementDTO, error) {
		var v fw.Validation
		var org domain.OrganizationID
		if q.Organization != "" {
			org = domain.OrganizationID{UUID: parseID(&v, "organization", q.Organization)}
		}
		var on domain.Agreement
		if q.InForceOn != "" {
			d, err := parseDate(q.InForceOn)
			v.Require(err == nil, "inForceOn", "format", "a date YYYY-MM-DD is required")
			on.Start = d
		}
		if err := v.Err(); err != nil {
			return nil, err
		}
		sc := scopeOf(ctx)
		if !org.IsZero() && !sc.sees(org) {
			return nil, fw.NotFound("parties.party", org)
		}
		as, err := s.Catalogs.Agreements(ctx)
		if err != nil {
			return nil, err
		}
		out := []AgreementDTO{}
		for _, a := range as {
			if a.Scope == domain.ScopeCompany && ((!org.IsZero() && a.Organization != org) || !sc.sees(a.Organization)) {
				continue
			}
			if !on.Start.IsZero() && !a.InForceOn(on.Start) {
				continue
			}
			out = append(out, agreementDTO(a))
		}
		return out, nil
	})
}
