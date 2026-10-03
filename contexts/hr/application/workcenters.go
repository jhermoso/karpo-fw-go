package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// OpenWorkCenter registers a facility of the employer as work center.
type OpenWorkCenter struct {
	Employer     string     `json:"employer"`
	Facility     string     `json:"facility"`
	Code         string     `json:"code"`
	Headquarters bool       `json:"headquarters"`
	Opened       vocab.Date `json:"opened"`
}

// SetHeadquarters makes a work center the employer's headquarters.
type SetHeadquarters struct {
	ID domain.WorkCenterID `json:"-"`
}

// CloseWorkCenter closes a work center without contracts in force after the date.
type CloseWorkCenter struct {
	ID domain.WorkCenterID `json:"-"`
	On vocab.Date          `json:"on"`
}

// GetWorkCenter loads a work center.
type GetWorkCenter struct{ ID domain.WorkCenterID }

// SearchWorkCenters searches work centers of the caller's scope.
type SearchWorkCenters struct {
	Employer   string
	OpenOnly   bool
	Page, Size int
}

// WorkCenterDTO is the transport form of a work center.
type WorkCenterDTO struct {
	ID           string `json:"id"`
	Employer     string `json:"employer"`
	Facility     string `json:"facility"`
	Code         string `json:"code"`
	Headquarters bool   `json:"headquarters"`
	Opened       string `json:"opened"`
	Closed       string `json:"closed,omitempty"`
	Version      int64  `json:"version"`
}

func workCenterDTO(w *domain.WorkCenter) WorkCenterDTO {
	return WorkCenterDTO{ID: w.ID().String(), Employer: w.Employer().String(), Facility: w.Facility().String(), Code: w.Code(),
		Headquarters: w.IsHeadquarters(), Opened: w.Opened().String(), Closed: dateText(w.Closed()), Version: w.Version()}
}

// unsetHeadquarters clears the flag of the other open headquarters of the employer.
func (s service) unsetHeadquarters(ctx context.Context, employer domain.OrganizationID, keep domain.WorkCenterID) error {
	hqs, err := s.WorkCenters.Find(ctx, domain.WCFieldEmployer.Eq(employer).And(domain.WCFieldHQ.Eq(true)))
	if err != nil {
		return err
	}
	for _, w := range hqs {
		if w.ID() == keep {
			continue
		}
		if _, err := s.workCenters.Update(ctx, w.ID(), func(_ context.Context, w *domain.WorkCenter) error { return w.MarkHeadquarters(false) }); err != nil {
			return err
		}
	}
	return nil
}

func (s service) workCenterUseCases(svc *Service) {
	svc.OpenWorkCenter = guard(PermWorkCenterCreate, func(ctx context.Context, c OpenWorkCenter) (WorkCenterDTO, error) {
		var v fw.Validation
		employer := domain.OrganizationID{UUID: parseID(&v, "employer", c.Employer)}
		facility := domain.FacilityID{UUID: parseID(&v, "facility", c.Facility)}
		if err := v.Err(); err != nil {
			return WorkCenterDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", employer, employer, true); err != nil {
			return WorkCenterDTO{}, err
		}
		if s.Facilities != nil {
			f, ok, err := s.Facilities.Facility(ctx, facility)
			if err != nil {
				return WorkCenterDTO{}, err
			}
			if !ok || f.Owner != employer {
				// Another organization's facility does not exist for this employer.
				return WorkCenterDTO{}, fw.NotFound("facilities.facility", facility)
			}
			if !f.Active {
				return WorkCenterDTO{}, fw.Violation("hr.facility_inactive", "the facility is not active")
			}
		}
		w, err := domain.OpenWorkCenter(domain.NewWorkCenterID(), domain.WorkCenterState{Employer: employer, Facility: facility,
			Code: c.Code, Headquarters: c.Headquarters, Opened: c.Opened})
		if err != nil {
			return WorkCenterDTO{}, err
		}
		others, err := s.WorkCenters.Find(ctx, domain.WCFieldEmployer.Eq(employer).And(
			domain.WCFieldCode.Eq(w.Code()).Or(domain.WCFieldFacility.Eq(facility).And(domain.WCFieldOpen.Eq(true)))))
		if err != nil {
			return WorkCenterDTO{}, err
		}
		for _, o := range others {
			if o.Code() == w.Code() {
				return WorkCenterDTO{}, fw.Violation("hr.duplicate_work_center_code", "the employer already has a work center with that code")
			}
			return WorkCenterDTO{}, fw.Violation("hr.facility_already_work_center", "the facility is already an open work center of the employer")
		}
		if w.IsHeadquarters() {
			if err := s.unsetHeadquarters(ctx, employer, w.ID()); err != nil {
				return WorkCenterDTO{}, err
			}
		}
		if err := s.workCenters.Create(ctx, w); err != nil {
			return WorkCenterDTO{}, err
		}
		return workCenterDTO(w), nil
	}, pipeline.Transactional[OpenWorkCenter, WorkCenterDTO](s.tx()))

	svc.SetHeadquarters = guard(PermWorkCenterUpdate, func(ctx context.Context, c SetHeadquarters) (WorkCenterDTO, error) {
		sc := scopeOf(ctx)
		w, err := s.workCenters.Update(ctx, c.ID, func(_ context.Context, w *domain.WorkCenter) error {
			if err := sc.check(domain.WorkCenterKind, w.ID(), w.Employer(), true); err != nil {
				return err
			}
			return w.MarkHeadquarters(true)
		})
		if err != nil {
			return WorkCenterDTO{}, err
		}
		if err := s.unsetHeadquarters(ctx, w.Employer(), w.ID()); err != nil {
			return WorkCenterDTO{}, err
		}
		return workCenterDTO(w), nil
	}, pipeline.Transactional[SetHeadquarters, WorkCenterDTO](s.tx()))

	svc.CloseWorkCenter = guard(PermWorkCenterUpdate, func(ctx context.Context, c CloseWorkCenter) (WorkCenterDTO, error) {
		sc := scopeOf(ctx)
		w, err := s.workCenters.Update(ctx, c.ID, func(ctx context.Context, w *domain.WorkCenter) error {
			if err := sc.check(domain.WorkCenterKind, w.ID(), w.Employer(), true); err != nil {
				return err
			}
			es, err := s.Employments.Find(ctx, domain.EmpFieldEmployer.Eq(w.Employer()).And(domain.EmpFieldEnded.Eq(false),
				domain.EmpFieldContracts.Any(domain.ContractFieldWorkCenter.Eq(w.ID()))))
			if err != nil {
				return err
			}
			for _, e := range es {
				for _, k := range e.Contracts() {
					if k.WorkCenter == w.ID() && (k.End.IsZero() || k.End.After(c.On)) {
						return fw.Violation("hr.work_center_in_use", "the work center has contracts in force after that date")
					}
				}
			}
			return w.Close(c.On)
		})
		if err != nil {
			return WorkCenterDTO{}, err
		}
		return workCenterDTO(w), nil
	}, pipeline.Transactional[CloseWorkCenter, WorkCenterDTO](s.tx()))

	svc.GetWorkCenter = guard(PermWorkCenterRead, func(ctx context.Context, q GetWorkCenter) (WorkCenterDTO, error) {
		w, err := s.WorkCenters.Get(ctx, q.ID)
		if err != nil {
			return WorkCenterDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.WorkCenterKind, w.ID(), w.Employer(), false); err != nil {
			return WorkCenterDTO{}, err
		}
		return workCenterDTO(w), nil
	})

	svc.SearchWorkCenters = guard(PermWorkCenterRead, func(ctx context.Context, q SearchWorkCenters) (fw.Page[WorkCenterDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.WorkCenter]{within(scopeOf(ctx), domain.WCFieldEmployer)}
		if q.Employer != "" {
			parts = append(parts, domain.WCFieldEmployer.Eq(domain.OrganizationID{UUID: parseID(&v, "employer", q.Employer)}))
		}
		if q.OpenOnly {
			parts = append(parts, domain.WCFieldOpen.Eq(true))
		}
		if err := v.Err(); err != nil {
			return fw.Page[WorkCenterDTO]{}, err
		}
		page, err := s.WorkCenters.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.WCFieldCode.Asc()))
		if err != nil {
			return fw.Page[WorkCenterDTO]{}, err
		}
		return fw.MapPage(page, workCenterDTO), nil
	})
}
