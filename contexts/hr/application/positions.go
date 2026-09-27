package application

import (
	"context"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// OpenPosition creates a position in an organization unit.
type OpenPosition struct {
	Unit        string     `json:"unit"`
	Type        string     `json:"type"`
	Status      string     `json:"status,omitempty"` // Active (default), Budgeted or Pending Approval
	PlannedFrom *time.Time `json:"plannedFrom,omitempty"`
	PlannedThru *time.Time `json:"plannedThru,omitempty"`
	Salaried    bool       `json:"salaried"`
	Exempt      bool       `json:"exempt"`
	FullTime    bool       `json:"fullTime"`
	Temporary   bool       `json:"temporary"`
}

// SetPositionStatus changes the status of a position.
type SetPositionStatus struct {
	ID     domain.PositionID `json:"-"`
	Status string            `json:"status"`
}

// FillPosition gives a position to an employee of its organization.
type FillPosition struct {
	ID     domain.PositionID `json:"-"`
	Person string            `json:"person"`
	From   *time.Time        `json:"from,omitempty"`
}

// VacatePosition ends the current holding.
type VacatePosition struct {
	ID domain.PositionID `json:"-"`
	At *time.Time        `json:"at,omitempty"`
}

// ReportTo adds a supervisor position.
type ReportTo struct {
	ID         domain.PositionID `json:"-"`
	Supervisor string            `json:"supervisor"`
	Primary    bool              `json:"primary"`
	From       *time.Time        `json:"from,omitempty"`
}

// EndReporting ends the line to a supervisor position.
type EndReporting struct {
	ID         domain.PositionID `json:"-"`
	Supervisor string            `json:"supervisor"`
	At         *time.Time        `json:"at,omitempty"`
}

// ClosePosition closes a position (its holder leaves it).
type ClosePosition struct {
	ID domain.PositionID `json:"-"`
	At *time.Time        `json:"at,omitempty"`
}

// GetPosition loads a position.
type GetPosition struct{ ID domain.PositionID }

// SearchPositions searches positions of the caller's scope.
type SearchPositions struct {
	Unit, Organization, Type, Status, Holder string
	VacantOnly                               bool
	Page, Size                               int
}

// GetOrgChart returns the positions reporting (primary line, now) to a root position.
type GetOrgChart struct {
	Root  domain.PositionID
	Depth int // 1..MaxDepth, default 3
}

// FulfillmentDTO is a holding of a position.
type FulfillmentDTO struct {
	Person string     `json:"person"`
	From   time.Time  `json:"from"`
	Thru   *time.Time `json:"thru,omitempty"`
}

// ReportingDTO is a reporting line.
type ReportingDTO struct {
	Supervisor string     `json:"supervisor"`
	Primary    bool       `json:"primary"`
	From       time.Time  `json:"from"`
	Thru       *time.Time `json:"thru,omitempty"`
}

// PositionDTO is the transport form of a position.
type PositionDTO struct {
	ID           string           `json:"id"`
	Unit         string           `json:"unit"`
	Organization string           `json:"organization"`
	Type         string           `json:"type"`
	TypeTitle    string           `json:"typeTitle,omitempty"`
	Status       string           `json:"status"`
	StatusName   string           `json:"statusName,omitempty"`
	Vacant       bool             `json:"vacant"`
	Holder       string           `json:"holder,omitempty"`
	ReportsTo    string           `json:"reportsTo,omitempty"`
	PlannedFrom  time.Time        `json:"plannedFrom"`
	PlannedThru  *time.Time       `json:"plannedThru,omitempty"`
	Flags        domain.Flags     `json:"flags"`
	Holders      []FulfillmentDTO `json:"holders"`
	Lines        []ReportingDTO   `json:"lines"`
	Version      int64            `json:"version"`
	ModifiedBy   string           `json:"modifiedBy,omitempty"`
}

// ChartNode is a position of an org chart with its direct reports.
type ChartNode struct {
	Position PositionDTO `json:"position"`
	Reports  []ChartNode `json:"reports"`
}

type positionNames struct {
	types    map[domain.PositionTypeID]domain.PositionType
	statuses map[domain.PositionStatusID]domain.PositionStatus
}

func (s service) positionNames(ctx context.Context) (positionNames, error) {
	types, err := s.positionTypes(ctx)
	if err != nil {
		return positionNames{}, err
	}
	statuses, err := s.positionStatuses(ctx)
	return positionNames{types: types, statuses: statuses}, err
}

func thru(p vocab.ValidPeriod) *time.Time {
	if t, ok := p.To(); ok {
		return &t
	}
	return nil
}

func (n positionNames) dto(p *domain.Position) PositionDTO {
	now := fw.Now()
	d := PositionDTO{ID: p.ID().String(), Unit: p.Unit().String(), Organization: p.Organization().String(), Type: p.Type().String(),
		TypeTitle: n.types[p.Type()].Title, Status: p.Status().String(), StatusName: n.statuses[p.Status()].Name,
		Vacant: p.IsVacantAt(now), PlannedFrom: p.Planned().From(), PlannedThru: thru(p.Planned()), Flags: p.Flags(),
		Holders: []FulfillmentDTO{}, Lines: []ReportingDTO{}, Version: p.Version(), ModifiedBy: p.ModifiedBy().Name}
	if h, ok := p.HolderAt(now); ok {
		d.Holder = h.String()
	}
	if sup, ok := p.PrimarySupervisorAt(now); ok {
		d.ReportsTo = sup.String()
	}
	for _, h := range p.Holders() {
		d.Holders = append(d.Holders, FulfillmentDTO{Person: h.Holder.String(), From: h.Period.From(), Thru: thru(h.Period)})
	}
	for _, l := range p.ReportsTo() {
		d.Lines = append(d.Lines, ReportingDTO{Supervisor: l.Supervisor.String(), Primary: l.Primary, From: l.Period.From(), Thru: thru(l.Period)})
	}
	return d
}

// organizationOf returns the internal organization of a unit.
func (s service) organizationOf(ctx context.Context, unit domain.OrganizationID) (domain.OrganizationID, error) {
	if s.Organizations == nil {
		return unit, nil
	}
	m, err := s.Organizations.InternalOrganizationOf(ctx, []domain.OrganizationID{unit})
	if err != nil {
		return domain.OrganizationID{}, err
	}
	org, ok := m[unit]
	if !ok {
		return domain.OrganizationID{}, fw.NotFound("parties.party", unit)
	}
	return org, nil
}

// employedBy reports whether the person has an employment with the organization on a date.
func (s service) employedBy(ctx context.Context, person domain.PersonID, org domain.OrganizationID, on vocab.Date) (bool, error) {
	es, err := s.Employments.Find(ctx, domain.CurrentEmploymentsOf(person).And(domain.EmpFieldEmployer.Eq(org)))
	if err != nil {
		return false, err
	}
	for _, e := range es {
		if e.ActiveOn(on) {
			return true, nil
		}
	}
	return false, nil
}

// checkNoCycle walks up the primary supervisors of sup: the position must not be among them.
func (s service) checkNoCycle(ctx context.Context, position, sup domain.PositionID, now time.Time) error {
	node := sup
	for level := 0; ; level++ {
		if node == position {
			return fw.Violation("hr.reporting_cycle", "the supervisor already reports to the position")
		}
		if level >= MaxDepth {
			return fw.Violation("hr.reporting_too_deep", "the reporting chain would be too deep")
		}
		p, err := s.Positions.Get(ctx, node)
		if err != nil {
			return err
		}
		up, ok := p.PrimarySupervisorAt(now)
		if !ok {
			return nil
		}
		node = up
	}
}

func (s service) positionUseCases(svc *Service) {
	update := func(ctx context.Context, id domain.PositionID, fn func(context.Context, *domain.Position) error) (PositionDTO, error) {
		names, err := s.positionNames(ctx)
		if err != nil {
			return PositionDTO{}, err
		}
		sc := scopeOf(ctx)
		p, err := s.positions.Update(ctx, id, func(ctx context.Context, p *domain.Position) error {
			if err := sc.check(domain.PositionKind, p.ID(), p.Organization(), true); err != nil {
				return err
			}
			return fn(ctx, p)
		})
		if err != nil {
			return PositionDTO{}, err
		}
		return names.dto(p), nil
	}

	svc.OpenPosition = guard(PermPositionCreate, func(ctx context.Context, c OpenPosition) (PositionDTO, error) {
		var v fw.Validation
		unit := domain.OrganizationID{UUID: parseID(&v, "unit", c.Unit)}
		typeID := domain.PositionTypeID{UUID: parseID(&v, "type", c.Type)}
		var status domain.PositionStatusID
		if c.Status != "" {
			status = domain.PositionStatusID{UUID: parseID(&v, "status", c.Status)}
		}
		if err := v.Err(); err != nil {
			return PositionDTO{}, err
		}
		org, err := s.organizationOf(ctx, unit)
		if err != nil {
			return PositionDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", unit, org, true); err != nil {
			return PositionDTO{}, err
		}
		names, err := s.positionNames(ctx)
		if err != nil {
			return PositionDTO{}, err
		}
		t, ok := names.types[typeID]
		if !ok {
			v.Add("type", "unknown", "unknown position type")
		}
		if _, ok := names.statuses[status]; !ok && !status.IsZero() {
			v.Add("status", "unknown", "unknown position status")
		}
		planned, err := vocab.NewValidPeriod(at(c.PlannedFrom), c.PlannedThru)
		v.Merge("plannedThru", err)
		if err := v.Err(); err != nil {
			return PositionDTO{}, err
		}
		p, err := domain.OpenPosition(domain.NewPositionID(), t, domain.PositionState{Unit: unit, Organization: org, Status: status,
			Planned: planned, Flags: domain.Flags{Salaried: c.Salaried, Exempt: c.Exempt, FullTime: c.FullTime, Temporary: c.Temporary}})
		if err != nil {
			return PositionDTO{}, err
		}
		if err := s.positions.Create(ctx, p); err != nil {
			return PositionDTO{}, err
		}
		return names.dto(p), nil
	})

	svc.SetPositionStatus = guard(PermPositionUpdate, func(ctx context.Context, c SetPositionStatus) (PositionDTO, error) {
		var v fw.Validation
		status := domain.PositionStatusID{UUID: parseID(&v, "status", c.Status)}
		if err := v.Err(); err != nil {
			return PositionDTO{}, err
		}
		statuses, err := s.positionStatuses(ctx)
		if err != nil {
			return PositionDTO{}, err
		}
		if st, ok := statuses[status]; !ok || !st.Active {
			v.Add("status", "unknown", "unknown or inactive position status")
			return PositionDTO{}, v.Err()
		}
		return update(ctx, c.ID, func(_ context.Context, p *domain.Position) error { return p.SetStatus(status) })
	}, retry[SetPositionStatus, PositionDTO]())

	svc.FillPosition = guard(PermPositionUpdate, func(ctx context.Context, c FillPosition) (PositionDTO, error) {
		var v fw.Validation
		person := domain.PersonID{UUID: parseID(&v, "person", c.Person)}
		if err := v.Err(); err != nil {
			return PositionDTO{}, err
		}
		from := at(c.From)
		return update(ctx, c.ID, func(ctx context.Context, p *domain.Position) error {
			ok, err := s.employedBy(ctx, person, p.Organization(), vocab.DateOf(from))
			if err != nil {
				return err
			}
			if !ok {
				return fw.Violation("hr.not_employed", "the person is not an employee of the position's organization on that date")
			}
			return p.Fill(person, from)
		})
	}, pipeline.Transactional[FillPosition, PositionDTO](s.tx()))

	svc.VacatePosition = guard(PermPositionUpdate, func(ctx context.Context, c VacatePosition) (PositionDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, p *domain.Position) error { return p.Vacate(at(c.At)) })
	}, retry[VacatePosition, PositionDTO]())

	svc.ReportTo = guard(PermPositionUpdate, func(ctx context.Context, c ReportTo) (PositionDTO, error) {
		var v fw.Validation
		sup := domain.PositionID{UUID: parseID(&v, "supervisor", c.Supervisor)}
		if err := v.Err(); err != nil {
			return PositionDTO{}, err
		}
		from := at(c.From)
		return update(ctx, c.ID, func(ctx context.Context, p *domain.Position) error {
			if sup != p.ID() {
				supervisor, err := s.Positions.Get(ctx, sup)
				if err != nil {
					return err
				}
				if err := scopeOf(ctx).check(domain.PositionKind, sup, supervisor.Organization(), false); err != nil {
					return err
				}
				if supervisor.Status() == domain.StatusInactive {
					return fw.Violation("hr.position_closed", "a closed position supervises nobody")
				}
				if c.Primary {
					if err := s.checkNoCycle(ctx, p.ID(), sup, from); err != nil {
						return err
					}
				}
			}
			return p.ReportTo(sup, c.Primary, from)
		})
	}, pipeline.Transactional[ReportTo, PositionDTO](s.tx()))

	svc.EndReporting = guard(PermPositionUpdate, func(ctx context.Context, c EndReporting) (PositionDTO, error) {
		var v fw.Validation
		sup := domain.PositionID{UUID: parseID(&v, "supervisor", c.Supervisor)}
		if err := v.Err(); err != nil {
			return PositionDTO{}, err
		}
		return update(ctx, c.ID, func(_ context.Context, p *domain.Position) error { return p.EndReporting(sup, at(c.At)) })
	}, retry[EndReporting, PositionDTO]())

	svc.ClosePosition = guard(PermPositionUpdate, func(ctx context.Context, c ClosePosition) (PositionDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, p *domain.Position) error { return p.Close(at(c.At)) })
	}, retry[ClosePosition, PositionDTO]())

	svc.GetPosition = guard(PermPositionRead, func(ctx context.Context, q GetPosition) (PositionDTO, error) {
		names, err := s.positionNames(ctx)
		if err != nil {
			return PositionDTO{}, err
		}
		p, err := s.Positions.Get(ctx, q.ID)
		if err != nil {
			return PositionDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.PositionKind, p.ID(), p.Organization(), false); err != nil {
			return PositionDTO{}, err
		}
		return names.dto(p), nil
	})

	svc.SearchPositions = guard(PermPositionRead, func(ctx context.Context, q SearchPositions) (fw.Page[PositionDTO], error) {
		names, err := s.positionNames(ctx)
		if err != nil {
			return fw.Page[PositionDTO]{}, err
		}
		now := fw.Now()
		var v fw.Validation
		parts := []spec.Specification[*domain.Position]{within(scopeOf(ctx), domain.PosFieldOrganization)}
		if q.Unit != "" {
			parts = append(parts, domain.PosFieldUnit.Eq(domain.OrganizationID{UUID: parseID(&v, "unit", q.Unit)}))
		}
		if q.Organization != "" {
			parts = append(parts, domain.PosFieldOrganization.Eq(domain.OrganizationID{UUID: parseID(&v, "organization", q.Organization)}))
		}
		if q.Type != "" {
			parts = append(parts, domain.PosFieldType.Eq(domain.PositionTypeID{UUID: parseID(&v, "type", q.Type)}))
		}
		if q.Status != "" {
			parts = append(parts, domain.PosFieldStatus.Eq(domain.PositionStatusID{UUID: parseID(&v, "status", q.Status)}))
		}
		if q.Holder != "" {
			parts = append(parts, domain.HeldBy(domain.PersonID{UUID: parseID(&v, "holder", q.Holder)}, now))
		}
		if q.VacantOnly {
			parts = append(parts, domain.VacantAt(now))
		}
		if err := v.Err(); err != nil {
			return fw.Page[PositionDTO]{}, err
		}
		page, err := s.Positions.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.PosFieldPlannedFrom.Asc()))
		if err != nil {
			return fw.Page[PositionDTO]{}, err
		}
		return fw.MapPage(page, names.dto), nil
	})

	svc.OrgChart = guard(PermPositionRead, func(ctx context.Context, q GetOrgChart) (ChartNode, error) {
		depth := q.Depth
		if depth <= 0 {
			depth = 3
		}
		depth = min(depth, MaxDepth)
		names, err := s.positionNames(ctx)
		if err != nil {
			return ChartNode{}, err
		}
		sc := scopeOf(ctx)
		root, err := s.Positions.Get(ctx, q.Root)
		if err != nil {
			return ChartNode{}, err
		}
		if err := sc.check(domain.PositionKind, root.ID(), root.Organization(), false); err != nil {
			return ChartNode{}, err
		}
		// One query per level (IN lists in chunks); a position is placed once.
		now := fw.Now()
		nodes := map[domain.PositionID]*ChartNode{root.ID(): {Position: names.dto(root)}}
		children := map[domain.PositionID][]domain.PositionID{}
		level := []domain.PositionID{root.ID()}
		for d := 0; d < depth && len(level) > 0; d++ {
			var next []domain.PositionID
			for i := 0; i < len(level); i += chunk {
				ps, err := s.Positions.Find(ctx, spec.And(within(sc, domain.PosFieldOrganization), domain.DirectReportsOf(level[i:min(i+chunk, len(level))]...)),
					domain.PosFieldPlannedFrom.Asc())
				if err != nil {
					return ChartNode{}, err
				}
				for _, p := range ps {
					sup, ok := p.PrimarySupervisorAt(now)
					if _, seen := nodes[p.ID()]; seen || !ok {
						continue
					}
					nodes[p.ID()] = &ChartNode{Position: names.dto(p)}
					children[sup] = append(children[sup], p.ID())
					next = append(next, p.ID())
				}
			}
			level = next
		}
		var build func(id domain.PositionID) ChartNode
		build = func(id domain.PositionID) ChartNode {
			n := *nodes[id]
			n.Reports = []ChartNode{}
			for _, c := range children[id] {
				n.Reports = append(n.Reports, build(c))
			}
			return n
		}
		return build(root.ID()), nil
	})
}
