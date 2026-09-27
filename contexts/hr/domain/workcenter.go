package domain

import (
	"context"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// WorkCenterKind is the stable aggregate type name.
const WorkCenterKind = "hr.work_center"

// WorkCenter is a facility registered as work center of an employer (the code of the labor
// authorities, headquarters flag, opening and closing). Its address is the facility's location
// in the Facilities context (approved decision: no more address strings copied here).
type WorkCenter struct {
	fw.BaseAggregateRoot[WorkCenterID]
	traits.Audited
	employer     OrganizationID
	facility     FacilityID
	code         string
	headquarters bool
	opened       vocab.Date
	closed       vocab.Date
}

// WorkCenterState is the persisted state of a work center.
type WorkCenterState struct {
	Employer     OrganizationID
	Facility     FacilityID
	Code         string
	Headquarters bool
	Opened       vocab.Date
	Closed       vocab.Date
	Audit        traits.AuditStamp
}

// ReconstituteWorkCenter rebuilds a work center.
func ReconstituteWorkCenter(id WorkCenterID, s WorkCenterState) (*WorkCenter, error) {
	base, err := fw.NewBaseAggregateRoot(WorkCenterKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	code := strings.ToUpper(strings.TrimSpace(s.Code))
	v.Require(code != "" && utf8.RuneCountInString(code) <= 60, "code", "length", "a code of 1 to 60 characters is required")
	v.Require(!s.Employer.IsZero(), "employer", "required", "a work center belongs to an employer")
	v.Require(!s.Facility.IsZero(), "facility", "required", "a work center is a facility")
	v.Require(!s.Opened.IsZero(), "opened", "required", "the opening date is required")
	v.Require(s.Closed.IsZero() || !s.Closed.Before(s.Opened), "closed", "order", "closing cannot precede opening")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &WorkCenter{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), employer: s.Employer, facility: s.Facility,
		code: code, headquarters: s.Headquarters, opened: s.Opened, closed: s.Closed}, nil
}

// OpenWorkCenter registers a work center.
func OpenWorkCenter(id WorkCenterID, s WorkCenterState) (*WorkCenter, error) {
	s.Closed = vocab.Date{}
	w, err := ReconstituteWorkCenter(id, s)
	if err != nil {
		return nil, err
	}
	w.Raise(WorkCenterOpened{EventMeta: w.NewEventMeta(), Employer: s.Employer.String(), Facility: s.Facility.String(), Code: w.code})
	return w, nil
}

// Employer returns the employer.
func (w *WorkCenter) Employer() OrganizationID { return w.employer }

// Facility returns the facility.
func (w *WorkCenter) Facility() FacilityID { return w.facility }

// Code returns the code.
func (w *WorkCenter) Code() string { return w.code }

// IsHeadquarters reports whether it is the employer's headquarters.
func (w *WorkCenter) IsHeadquarters() bool { return w.headquarters }

// Opened returns the opening date.
func (w *WorkCenter) Opened() vocab.Date { return w.opened }

// Closed returns the closing date (zero while open).
func (w *WorkCenter) Closed() vocab.Date { return w.closed }

// IsOpen reports whether the work center has not been closed.
func (w *WorkCenter) IsOpen() bool { return w.closed.IsZero() }

// MarkHeadquarters sets or clears the headquarters flag. One open headquarters per employer is
// checked by the application (it touches several work centers).
func (w *WorkCenter) MarkHeadquarters(hq bool) error {
	if hq && !w.IsOpen() {
		return fw.Violation("hr.work_center_closed", "a closed work center cannot be the headquarters")
	}
	w.headquarters = hq
	return nil
}

// Close closes the work center on a date.
func (w *WorkCenter) Close(on vocab.Date) error {
	if !w.IsOpen() {
		return nil
	}
	if on.Before(w.opened) {
		return fw.Violation("hr.work_center_close_before_open", "closing cannot precede opening")
	}
	w.closed, w.headquarters = on, false
	w.Raise(WorkCenterClosed{EventMeta: w.NewEventMeta(), Closed: on.String()})
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (w *WorkCenter) AuditSnapshot() map[string]any {
	return map[string]any{"code": w.code, "facility": w.facility.String(), "headquarters": w.headquarters,
		"opened": w.opened.String(), "closed": w.closed.String()}
}

// Work center fields.
var (
	WCFieldEmployer = spec.Comparable("employer", (*WorkCenter).Employer)
	WCFieldCode     = spec.Ordered("code", (*WorkCenter).Code)
	WCFieldHQ       = spec.Comparable("headquarters", (*WorkCenter).IsHeadquarters)
	WCFieldFacility = spec.Comparable("facility", (*WorkCenter).Facility)
	WCFieldID       = spec.Comparable("id", func(w *WorkCenter) WorkCenterID { return w.ID() })
	WCFieldOpen     = spec.Comparable("open_flag", (*WorkCenter).IsOpen)
)

// Repositories and catalogs of the context.
type (
	PositionRepository   = fw.Repository[PositionID, *Position]
	EmploymentRepository = fw.Repository[EmploymentID, *Employment]
	WorkCenterRepository = fw.Repository[WorkCenterID, *WorkCenter]
)

// Catalogs reads the reference lists of the context.
type Catalogs interface {
	PositionStatuses(ctx context.Context) ([]PositionStatus, error)
	PositionClasses(ctx context.Context) ([]PositionClass, error)
	PositionTypes(ctx context.Context) ([]PositionType, error)
	Agreements(ctx context.Context) ([]Agreement, error)
}
