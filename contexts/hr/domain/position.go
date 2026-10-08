package domain

import (
	"slices"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// PositionKind is the stable aggregate type name.
const PositionKind = "hr.position"

// Fulfillment is a person holding the position during a period (child entity; the C#
// PositionFulfillment had no start date).
type Fulfillment struct {
	ID     fw.UUID
	Holder PersonID
	Period vocab.ValidPeriod
}

// ReportingLine is a supervisor position of this one during a period (child entity; the C#
// PositionReportingStructure could not set its primary flag or end).
type ReportingLine struct {
	ID         fw.UUID
	Supervisor PositionID
	Primary    bool
	Period     vocab.ValidPeriod
}

// Flags are the working conditions of a position.
type Flags struct {
	Salaried, Exempt, FullTime, Temporary bool
}

// Position is a job slot of an organization unit (a Parties organization: department, division
// or the internal organization itself), of a type, with its holders and reporting lines.
type Position struct {
	fw.BaseAggregateRoot[PositionID]
	traits.Audited
	unit         OrganizationID // department, division...
	organization OrganizationID // the internal organization the unit belongs to (scope)
	typ          PositionTypeID
	status       PositionStatusID
	planned      vocab.ValidPeriod // estimated from / thru
	flags        Flags
	holders      []Fulfillment
	reportsTo    []ReportingLine
}

// PositionState is the persisted state of a position.
type PositionState struct {
	Unit, Organization OrganizationID
	Type               PositionTypeID
	Status             PositionStatusID
	Planned            vocab.ValidPeriod
	Flags              Flags
	Holders            []Fulfillment
	ReportsTo          []ReportingLine
	Audit              traits.AuditStamp
}

// ReconstitutePosition rebuilds a position.
func ReconstitutePosition(id PositionID, s PositionState) (*Position, error) {
	base, err := fw.NewBaseAggregateRoot(PositionKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Unit.IsZero() && !s.Organization.IsZero(), "unit", "required", "a position belongs to an organization unit")
	v.Require(!s.Type.IsZero(), "type", "required", "a position needs a type")
	v.Require(!s.Status.IsZero(), "status", "required", "a position needs a status")
	v.Require(!s.Planned.IsZero(), "plannedFrom", "required", "a position needs its planned start")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Position{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), unit: s.Unit, organization: s.Organization,
		typ: s.Type, status: s.Status, planned: s.Planned, flags: s.Flags, holders: slices.Clone(s.Holders),
		reportsTo: slices.Clone(s.ReportsTo)}, nil
}

// OpenPosition creates a position (status Budgeted, Pending Approval or Active).
func OpenPosition(id PositionID, t PositionType, s PositionState) (*Position, error) {
	if !t.Active {
		var v fw.Validation
		v.Add("type", "inactive", "the position type is not active")
		return nil, v.Err()
	}
	if s.Status.IsZero() {
		s.Status = StatusActive
	}
	if s.Status != StatusActive && s.Status != StatusBudgeted && s.Status != StatusPendingApproval {
		return nil, fw.Violation("hr.position_status", "a new position is active, budgeted or pending approval")
	}
	s.Type, s.Holders, s.ReportsTo = t.ID, nil, nil
	p, err := ReconstitutePosition(id, s)
	if err != nil {
		return nil, err
	}
	p.Raise(PositionOpened{EventMeta: p.NewEventMeta(), Unit: s.Unit.String(), Organization: s.Organization.String(), Type: t.ID.String()})
	return p, nil
}

// Unit returns the organization unit.
func (p *Position) Unit() OrganizationID { return p.unit }

// Organization returns the internal organization (scope).
func (p *Position) Organization() OrganizationID { return p.organization }

// Type returns the position type.
func (p *Position) Type() PositionTypeID { return p.typ }

// Status returns the status (Vacant is never stored: see IsVacantAt).
func (p *Position) Status() PositionStatusID { return p.status }

// Planned returns the planned period.
func (p *Position) Planned() vocab.ValidPeriod { return p.planned }

// Flags returns the working conditions.
func (p *Position) Flags() Flags { return p.flags }

// Holders returns a copy of the fulfillments.
func (p *Position) Holders() []Fulfillment { return slices.Clone(p.holders) }

// ReportsTo returns a copy of the reporting lines.
func (p *Position) ReportsTo() []ReportingLine { return slices.Clone(p.reportsTo) }

// HolderAt returns the person holding the position at t.
func (p *Position) HolderAt(t time.Time) (PersonID, bool) {
	for _, h := range p.holders {
		if h.Period.IsActiveAt(t) {
			return h.Holder, true
		}
	}
	return PersonID{}, false
}

// IsVacantAt reports whether an active position has no holder at t (the C# stored Vacant as a
// status that nobody kept up to date).
func (p *Position) IsVacantAt(t time.Time) bool {
	_, held := p.HolderAt(t)
	return p.status == StatusActive && !held
}

// PrimarySupervisorAt returns the primary supervisor position at t.
func (p *Position) PrimarySupervisorAt(t time.Time) (PositionID, bool) {
	for _, l := range p.reportsTo {
		if l.Primary && l.Period.IsActiveAt(t) {
			return l.Supervisor, true
		}
	}
	return PositionID{}, false
}

// SetStatus changes the status. Vacant is derived and cannot be set; an inactive position is
// closed (see Close).
func (p *Position) SetStatus(s PositionStatusID) error {
	switch s {
	case StatusVacant:
		return fw.Violation("hr.position_status", "vacancy is derived from the holders, not set")
	case StatusInactive:
		return fw.Violation("hr.position_status", "close the position instead")
	}
	if p.status == StatusInactive {
		return fw.Violation("hr.position_closed", "a closed position does not change")
	}
	if s != p.status {
		p.status = s
		p.Raise(PositionStatusChanged{EventMeta: p.NewEventMeta(), Status: s.String()})
	}
	return nil
}

// Fill gives the position to a person from a moment on. Invariants: the position is active, and
// it has one holder at a time (the C# did not check it). The application checks the person is
// an employee of the organization.
func (p *Position) Fill(holder PersonID, from time.Time) error {
	if p.status != StatusActive {
		return fw.Violation("hr.position_not_active", "only an active position can be filled")
	}
	period, err := vocab.OpenPeriodFrom(from)
	if err != nil {
		return err
	}
	for _, h := range p.holders {
		if h.Period.Overlaps(period) {
			return fw.Violation("hr.position_filled", "the position already has a holder in that period")
		}
	}
	p.holders = append(slices.Clone(p.holders), Fulfillment{ID: fw.NewUUID(), Holder: holder, Period: period})
	p.Raise(PositionFilled{EventMeta: p.NewEventMeta(), Holder: holder.String(), From: period.From()})
	return nil
}

// Vacate ends the current holding at a moment (the C# Terminate was a no-op).
func (p *Position) Vacate(at time.Time) error {
	for i, h := range p.holders {
		if !h.Period.IsOpenEnded() || at.Before(h.Period.From()) {
			continue
		}
		period, err := vocab.NewValidPeriod(h.Period.From(), &at)
		if err != nil {
			return err
		}
		p.holders = slices.Clone(p.holders)
		p.holders[i].Period = period
		p.Raise(PositionVacated{EventMeta: p.NewEventMeta(), Holder: h.Holder.String(), At: at.UTC()})
		return nil
	}
	return fw.Violation("hr.position_not_filled", "the position has no current holder")
}

// ReportTo adds a supervisor position. Invariants: not itself, not twice at a time, and one primary
// supervisor at a time (a new primary replaces the previous one, which becomes secondary).
// The application checks there is no cycle through primary lines.
func (p *Position) ReportTo(supervisor PositionID, primary bool, from time.Time) error {
	if supervisor == p.ID() {
		return fw.Violation("hr.reporting_self", "a position cannot report to itself")
	}
	period, err := vocab.OpenPeriodFrom(from)
	if err != nil {
		return err
	}
	lines := slices.Clone(p.reportsTo)
	for i, l := range lines {
		if !l.Period.Overlaps(period) {
			continue
		}
		if l.Supervisor == supervisor {
			return fw.Violation("hr.reporting_duplicate", "the position already reports to that supervisor")
		}
		if primary && l.Primary {
			lines[i].Primary = false
		}
	}
	p.reportsTo = append(lines, ReportingLine{ID: fw.NewUUID(), Supervisor: supervisor, Primary: primary, Period: period})
	p.Raise(ReportingChanged{EventMeta: p.NewEventMeta(), Supervisor: supervisor.String(), Primary: primary})
	return nil
}

// EndReporting ends the line to a supervisor at a moment.
func (p *Position) EndReporting(supervisor PositionID, at time.Time) error {
	for i, l := range p.reportsTo {
		if l.Supervisor != supervisor || !l.Period.IsOpenEnded() {
			continue
		}
		period, err := vocab.NewValidPeriod(l.Period.From(), &at)
		if err != nil {
			return fw.Violation("hr.reporting_end_before_start", "a reporting line cannot end before it starts")
		}
		p.reportsTo = slices.Clone(p.reportsTo)
		p.reportsTo[i].Period = period
		p.Raise(ReportingChanged{EventMeta: p.NewEventMeta(), Supervisor: supervisor.String(), Ended: true})
		return nil
	}
	return fw.NotFound("hr.reporting_line", supervisor)
}

// Close closes the position at a moment: it becomes inactive and its holder leaves it.
func (p *Position) Close(at time.Time) error {
	if p.status == StatusInactive {
		return nil
	}
	if _, held := p.HolderAt(at); held {
		if err := p.Vacate(at); err != nil {
			return err
		}
	}
	for i, l := range p.reportsTo {
		if l.Period.IsOpenEnded() && !at.Before(l.Period.From()) {
			period, _ := vocab.NewValidPeriod(l.Period.From(), &at)
			p.reportsTo = slices.Clone(p.reportsTo)
			p.reportsTo[i].Period = period
		}
	}
	p.status = StatusInactive
	p.Raise(PositionClosed{EventMeta: p.NewEventMeta(), At: at.UTC()})
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (p *Position) AuditSnapshot() map[string]any {
	now := fw.Now()
	holder, _ := p.HolderAt(now)
	sup, _ := p.PrimarySupervisorAt(now)
	return map[string]any{"unit": p.unit.String(), "type": p.typ.String(), "status": p.status.String(),
		"holder": holder.String(), "reportsTo": sup.String()}
}

func until(p vocab.ValidPeriod) *time.Time {
	if t, ok := p.To(); ok {
		return &t
	}
	return nil
}

// Position fields and specifications.
var (
	PosFieldID           = spec.Comparable("id", func(p *Position) PositionID { return p.ID() })
	PosFieldUnit         = spec.Comparable("unit", (*Position).Unit)
	PosFieldOrganization = spec.Comparable("organization", (*Position).Organization)
	PosFieldType         = spec.Comparable("position_type", (*Position).Type)
	PosFieldStatus       = spec.Comparable("status", (*Position).Status)
	PosFieldPlannedFrom  = spec.Time("planned_from", func(p *Position) time.Time { return p.planned.From() })
	PosFieldHolders      = spec.Collection("holders", (*Position).Holders)
	HolderFieldPerson    = spec.Comparable("holder", func(f Fulfillment) PersonID { return f.Holder })
	HolderFieldFrom      = spec.Time("valid_from", func(f Fulfillment) time.Time { return f.Period.From() })
	HolderFieldUntil     = spec.OptionalTime("valid_to", func(f Fulfillment) *time.Time { return until(f.Period) })
	PosFieldReports      = spec.Collection("reports_to", (*Position).ReportsTo)
	ReportFieldSup       = spec.Comparable("supervisor", func(l ReportingLine) PositionID { return l.Supervisor })
	ReportFieldPrimary   = spec.Comparable("is_primary", func(l ReportingLine) bool { return l.Primary })
	ReportFieldUntil     = spec.OptionalTime("valid_to", func(l ReportingLine) *time.Time { return until(l.Period) })
)

// HeldBy matches positions the person holds at t.
func HeldBy(person PersonID, t time.Time) spec.Spec[*Position] {
	return PosFieldHolders.Any(spec.And(HolderFieldPerson.Eq(person), HolderFieldFrom.AtOrBefore(t),
		HolderFieldUntil.IsNull().Or(HolderFieldUntil.After(t))))
}

// DirectReportsOf matches positions whose current primary supervisor is one of the positions.
func DirectReportsOf(supervisors ...PositionID) spec.Spec[*Position] {
	return PosFieldReports.Any(spec.And(ReportFieldSup.In(supervisors...), ReportFieldPrimary.Eq(true), ReportFieldUntil.IsNull()))
}

// VacantAt matches active positions without a holder at t.
func VacantAt(t time.Time) spec.Spec[*Position] {
	current := spec.And(HolderFieldFrom.AtOrBefore(t), HolderFieldUntil.IsNull().Or(HolderFieldUntil.After(t)))
	return PosFieldStatus.Eq(StatusActive).And(PosFieldHolders.Any(current).Not())
}
