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

// WorkCenterKind es el nombre estable de tipo de agregado.
const WorkCenterKind = "hr.work_center"

// WorkCenter es una instalación dada de alta como centro de trabajo de un empleador (el código de
// la autoridad laboral, la marca de sede, la apertura y el cierre). Su dirección es la ubicación de
// la instalación en el contexto Facilities (decisión aprobada: no se copian aquí más cadenas de
// dirección).
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

// WorkCenterState es el estado persistido de un centro de trabajo.
type WorkCenterState struct {
	Employer     OrganizationID
	Facility     FacilityID
	Code         string
	Headquarters bool
	Opened       vocab.Date
	Closed       vocab.Date
	Audit        traits.AuditStamp
}

// ReconstituteWorkCenter reconstruye un centro de trabajo.
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

// OpenWorkCenter da de alta un centro de trabajo.
func OpenWorkCenter(id WorkCenterID, s WorkCenterState) (*WorkCenter, error) {
	s.Closed = vocab.Date{}
	w, err := ReconstituteWorkCenter(id, s)
	if err != nil {
		return nil, err
	}
	w.Raise(WorkCenterOpened{EventMeta: w.NewEventMeta(), Employer: s.Employer.String(), Facility: s.Facility.String(), Code: w.code})
	return w, nil
}

// Employer devuelve el empleador.
func (w *WorkCenter) Employer() OrganizationID { return w.employer }

// Facility devuelve la instalación.
func (w *WorkCenter) Facility() FacilityID { return w.facility }

// Code devuelve el código.
func (w *WorkCenter) Code() string { return w.code }

// IsHeadquarters dice si es la sede del empleador.
func (w *WorkCenter) IsHeadquarters() bool { return w.headquarters }

// Opened devuelve la fecha de apertura.
func (w *WorkCenter) Opened() vocab.Date { return w.opened }

// Closed devuelve la fecha de cierre (cero mientras está abierto).
func (w *WorkCenter) Closed() vocab.Date { return w.closed }

// IsOpen dice si el centro de trabajo no se ha cerrado.
func (w *WorkCenter) IsOpen() bool { return w.closed.IsZero() }

// MarkHeadquarters pone o quita la marca de sede. Que haya una sola sede abierta por empleador lo
// comprueba la aplicación (afecta a varios centros de trabajo).
func (w *WorkCenter) MarkHeadquarters(hq bool) error {
	if hq && !w.IsOpen() {
		return fw.Violation("hr.work_center_closed", "a closed work center cannot be the headquarters")
	}
	w.headquarters = hq
	return nil
}

// Close cierra el centro de trabajo en una fecha.
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

// AuditSnapshot implementa traits.Snapshotter.
func (w *WorkCenter) AuditSnapshot() map[string]any {
	return map[string]any{"code": w.code, "facility": w.facility.String(), "headquarters": w.headquarters,
		"opened": w.opened.String(), "closed": w.closed.String()}
}

// Campos de los centros de trabajo.
var (
	WCFieldEmployer = spec.Comparable("employer", (*WorkCenter).Employer)
	WCFieldCode     = spec.Ordered("code", (*WorkCenter).Code)
	WCFieldHQ       = spec.Comparable("headquarters", (*WorkCenter).IsHeadquarters)
	WCFieldFacility = spec.Comparable("facility", (*WorkCenter).Facility)
	WCFieldID       = spec.Comparable("id", func(w *WorkCenter) WorkCenterID { return w.ID() })
	WCFieldOpen     = spec.Comparable("open_flag", (*WorkCenter).IsOpen)
)

// Repositorios y catálogos del contexto.
type (
	PositionRepository   = fw.Repository[PositionID, *Position]
	EmploymentRepository = fw.Repository[EmploymentID, *Employment]
	WorkCenterRepository = fw.Repository[WorkCenterID, *WorkCenter]
)

// Catalogs lee las listas de referencia del contexto.
type Catalogs interface {
	PositionStatuses(ctx context.Context) ([]PositionStatus, error)
	PositionClasses(ctx context.Context) ([]PositionClass, error)
	PositionTypes(ctx context.Context) ([]PositionType, error)
	Agreements(ctx context.Context) ([]Agreement, error)
}
