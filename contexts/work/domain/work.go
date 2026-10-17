// Package domain es el modelo de Work: el trabajo que hace una empresa (proyectos, tareas,
// mantenimiento y órdenes de producción) con su plan, las personas asignadas, un estado que se mueve
// por reglas y el tiempo anotado en él. El WorkEffort del C# era una cabecera cuyo estado, historial
// y horas reales se tecleaban a mano y eran independientes entre sí, y sus anotaciones de tiempo no
// tenían fecha.
package domain

import (
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// WorkKind es el nombre estable del tipo de agregado.
const WorkKind = "work.work"

// Identidades del contexto.
type (
	// WorkID identifica un trabajo.
	WorkID struct{ fw.UUID }
	// TimeEntryID identifica una anotación de tiempo.
	TimeEntryID struct{ fw.UUID }
	// OrganizationID es la empresa que hace el trabajo, una organización interna de Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID es un cliente o una persona del contexto Parties.
	PartyID struct{ fw.UUID }
)

// Kind es qué es un trabajo (los tipos de work effort sembrados del C#).
type Kind string

// Tipos.
const (
	Project     Kind = "project"
	Task        Kind = "task"
	Maintenance Kind = "maintenance"
	Production  Kind = "production"
)

// Kinds lista los tipos válidos.
var Kinds = []Kind{Project, Task, Maintenance, Production}

// Purposes lista los propósitos válidos (los tipos de propósito sembrados del C#); un trabajo
// puede no tener ninguno.
var Purposes = []string{"improvement", "repair", "preventive-check", "audit", "training"}

// Status es dónde está un trabajo.
type Status string

// Estados (los tipos de estado sembrados del C#).
const (
	Created    Status = "created"
	Scheduled  Status = "scheduled"
	InProgress Status = "in-progress"
	OnHold     Status = "on-hold"
	Completed  Status = "completed"
	Cancelled  Status = "cancelled"
)

// Statuses lista los estados válidos.
var Statuses = []Status{Created, Scheduled, InProgress, OnHold, Completed, Cancelled}

// next es la regla del estado: adónde puede ir cada uno.
var next = map[Status][]Status{
	Created:    {Scheduled, InProgress, Cancelled},
	Scheduled:  {InProgress, Cancelled},
	InProgress: {OnHold, Completed, Cancelled},
	OnHold:     {InProgress, Cancelled},
}

// Step es un cambio de estado.
type Step struct {
	Status Status
	On     vocab.Date
}

// Assignment es una persona que trabaja en un trabajo, con la tarifa por hora de su tiempo.
type Assignment struct {
	Person PartyID
	Role   string
	Rate   vocab.Decimal // coste de una hora
	From   vocab.Date
	Until  vocab.Date // cero mientras está asignada
}

// Covers dice si la asignación está vigente en un día.
func (a Assignment) Covers(d vocab.Date) bool {
	return !d.Before(a.From) && (a.Until.IsZero() || !d.After(a.Until))
}

// Plan es lo que se puede editar mientras el trabajo está abierto.
type Plan struct {
	Name           string
	Description    string
	Purpose        string
	Customer       PartyID // para quién es el trabajo, si es para alguien
	Facility       fw.UUID // dónde (Facilities)
	Asset          fw.UUID // sobre qué (Assets)
	PlannedStart   vocab.Date
	PlannedEnd     vocab.Date
	EstimatedHours vocab.Decimal
	Budget         vocab.Decimal
}

// WorkState es el estado persistido de un trabajo.
type WorkState struct {
	Company OrganizationID
	Code    string
	Kind    Kind
	Parent  WorkID // el proyecto al que pertenece una tarea
	Plan
	Status       Status
	Started      vocab.Date
	Finished     vocab.Date
	CancelReason string
	Hours        vocab.Decimal // horas aprobadas al terminarse
	Cost         vocab.Decimal // su coste al terminarse
	Assignments  []Assignment
	History      []Step
	Audit        traits.AuditStamp
}

// Work es un trabajo de una empresa.
type Work struct {
	fw.BaseAggregateRoot[WorkID]
	traits.Audited
	s WorkState
}

func zero() vocab.Decimal { return vocab.DecimalFromInt(0) }

func validCode(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for _, r := range s {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '.' || r == '_') {
			return false
		}
	}
	return true
}

func checkPlan(v *fw.Validation, p *Plan) {
	p.Name, p.Description, p.Purpose = strings.TrimSpace(p.Name), strings.TrimSpace(p.Description), strings.TrimSpace(p.Purpose)
	v.Require(p.Name != "" && utf8.RuneCountInString(p.Name) <= 200, "name", "length", "a name of 1 to 200 characters")
	v.Require(utf8.RuneCountInString(p.Description) <= 1000, "description", "length", "at most 1000 characters")
	v.Require(p.Purpose == "" || slices.Contains(Purposes, p.Purpose), "purpose", "enum", "a purpose")
	v.Require(p.PlannedEnd.IsZero() || (!p.PlannedStart.IsZero() && !p.PlannedEnd.Before(p.PlannedStart)), "plannedEnd", "range",
		"a planned end needs a planned start not after it")
	v.Require(!p.EstimatedHours.IsNegative() && p.EstimatedHours.Equal(p.EstimatedHours.Round(2)), "estimatedHours", "range", "hours with two decimals")
	v.Require(!p.Budget.IsNegative() && p.Budget.Equal(p.Budget.Round(2)), "budget", "range", "a budget in cents")
}

// ReconstituteWork reconstruye un trabajo.
func ReconstituteWork(id WorkID, s WorkState) (*Work, error) {
	base, err := fw.NewBaseAggregateRoot(WorkKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero(), "company", "required", "the company is required")
	s.Code = strings.ToUpper(strings.TrimSpace(s.Code))
	v.Require(validCode(s.Code), "code", "format", "1 to 20 letters, digits, dots, dashes or underscores")
	v.Require(slices.Contains(Kinds, s.Kind), "kind", "enum", "project, task, maintenance or production")
	v.Require(slices.Contains(Statuses, s.Status), "status", "enum", "a status")
	v.Require(s.Parent.IsZero() || s.Parent != id, "parent", "self", "a piece of work is not part of itself")
	checkPlan(&v, &s.Plan)
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Assignments, s.History = slices.Clone(s.Assignments), slices.Clone(s.History)
	return &Work{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// OpenWork crea un trabajo en un día.
func OpenWork(id WorkID, company OrganizationID, code string, kind Kind, parent WorkID, plan Plan, on vocab.Date) (*Work, error) {
	if on.IsZero() {
		return nil, fw.Violation("work.date", "the day is required")
	}
	w, err := ReconstituteWork(id, WorkState{Company: company, Code: code, Kind: kind, Parent: parent, Plan: plan, Status: Created,
		Hours: zero(), Cost: zero(), History: []Step{{Status: Created, On: on}}})
	if err != nil {
		return nil, err
	}
	w.Raise(WorkOpened{EventMeta: w.NewEventMeta(), Company: company.String(), Code: w.s.Code, Kind: string(kind)})
	return w, nil
}

// State devuelve el estado (los slices son copias).
func (w *Work) State() WorkState {
	s := w.s
	s.Assignments, s.History = slices.Clone(s.Assignments), slices.Clone(s.History)
	return s
}

// Open dice si el trabajo no está ni terminado ni anulado.
func (w *Work) Open() bool { return w.s.Status != Completed && w.s.Status != Cancelled }

func (w *Work) closed() error {
	if !w.Open() {
		return fw.Violation("work.closed", "the work is "+string(w.s.Status))
	}
	return nil
}

// Change sustituye el plan de un trabajo abierto.
func (w *Work) Change(p Plan) error {
	if err := w.closed(); err != nil {
		return err
	}
	var v fw.Validation
	checkPlan(&v, &p)
	if err := v.Err(); err != nil {
		return err
	}
	if w.s.Status == Scheduled && (p.PlannedStart.IsZero() || p.PlannedEnd.IsZero()) {
		return fw.Violation("work.plan_required", "scheduled work keeps its planned dates")
	}
	w.s.Plan = p
	return nil
}

// move cambia el estado según la regla, en un día no anterior al último cambio.
func (w *Work) move(to Status, on vocab.Date) error {
	if on.IsZero() {
		return fw.Violation("work.date", "the day is required")
	}
	if !slices.Contains(next[w.s.Status], to) {
		return fw.Violation("work.transition", "work "+string(w.s.Status)+" cannot become "+string(to))
	}
	if n := len(w.s.History); n > 0 && on.Before(w.s.History[n-1].On) {
		return fw.Violation("work.date", "a change of status is not before the previous one")
	}
	from := w.s.Status
	w.s.Status = to
	w.s.History = append(slices.Clone(w.s.History), Step{Status: to, On: on})
	w.Raise(WorkStatusChanged{EventMeta: w.NewEventMeta(), Company: w.s.Company.String(), Code: w.s.Code, From: string(from), To: string(to), On: on.String()})
	return nil
}

// Schedule fija las fechas planificadas.
func (w *Work) Schedule(on vocab.Date) error {
	if w.s.PlannedStart.IsZero() || w.s.PlannedEnd.IsZero() {
		return fw.Violation("work.plan_required", "scheduling needs the planned start and end")
	}
	return w.move(Scheduled, on)
}

// Start empieza el trabajo, o lo reanuda cuando está en pausa.
func (w *Work) Start(on vocab.Date) error {
	if err := w.move(InProgress, on); err != nil {
		return err
	}
	if w.s.Started.IsZero() {
		w.s.Started = on
	}
	return nil
}

// Hold pone en pausa el trabajo.
func (w *Work) Hold(on vocab.Date) error { return w.move(OnHold, on) }

// Complete termina el trabajo con las horas aprobadas anotadas en él y su coste.
func (w *Work) Complete(on vocab.Date, hours, cost vocab.Decimal) error {
	if err := w.move(Completed, on); err != nil {
		return err
	}
	w.s.Finished, w.s.Hours, w.s.Cost = on, hours, cost
	w.Raise(WorkCompleted{EventMeta: w.NewEventMeta(), Company: w.s.Company.String(), Code: w.s.Code, Kind: string(w.s.Kind),
		Customer: optional(w.s.Customer.UUID), Started: w.s.Started.String(), Finished: on.String(), Hours: hours.StringFixed(2), Cost: cost.StringFixed(2)})
	return nil
}

// Cancel abandona el trabajo, con un motivo.
func (w *Work) Cancel(on vocab.Date, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > 200 {
		return fw.Violation("work.cancel_reason", "a reason of 1 to 200 characters")
	}
	if err := w.move(Cancelled, on); err != nil {
		return err
	}
	w.s.Finished, w.s.CancelReason = on, reason
	return nil
}

// Assign pone a una persona en el trabajo desde un día con una tarifa por hora, o cambia el papel y
// la tarifa de una persona que ya está en él.
func (w *Work) Assign(person PartyID, role string, rate vocab.Decimal, from vocab.Date) error {
	if err := w.closed(); err != nil {
		return err
	}
	role = strings.TrimSpace(role)
	var v fw.Validation
	v.Require(!person.IsZero(), "person", "required", "the person is required")
	v.Require(utf8.RuneCountInString(role) <= 60, "role", "length", "at most 60 characters")
	v.Require(!rate.IsNegative() && rate.Equal(rate.Round(2)), "rate", "range", "an hourly rate in cents")
	v.Require(!from.IsZero(), "from", "required", "the first day is required")
	if err := v.Err(); err != nil {
		return err
	}
	as := slices.Clone(w.s.Assignments)
	if i := slices.IndexFunc(as, func(a Assignment) bool { return a.Person == person }); i >= 0 {
		as[i] = Assignment{Person: person, Role: role, Rate: rate, From: from}
	} else {
		as = append(as, Assignment{Person: person, Role: role, Rate: rate, From: from})
	}
	w.s.Assignments = as
	return nil
}

// Release saca a una persona del trabajo después de un día.
func (w *Work) Release(person PartyID, until vocab.Date) error {
	if err := w.closed(); err != nil {
		return err
	}
	i := slices.IndexFunc(w.s.Assignments, func(a Assignment) bool { return a.Person == person })
	if i < 0 {
		return fw.Violation("work.not_assigned", "the person is not assigned to the work")
	}
	if until.IsZero() || until.Before(w.s.Assignments[i].From) {
		return fw.Violation("work.date", "the last day is not before the first")
	}
	as := slices.Clone(w.s.Assignments)
	as[i].Until = until
	w.s.Assignments = as
	return nil
}

// RateFor devuelve la tarifa por hora de una persona en un día: sólo anota tiempo quien está
// asignado ese día, en un trabajo en curso que ya había empezado para entonces.
func (w *Work) RateFor(person PartyID, on vocab.Date) (vocab.Decimal, error) {
	if w.s.Status != InProgress {
		return zero(), fw.Violation("work.not_in_progress", "time is recorded on work in progress")
	}
	if on.Before(w.s.Started) {
		return zero(), fw.Violation("work.time_before_start", "time is not recorded before the work started")
	}
	for _, a := range w.s.Assignments {
		if a.Person == person && a.Covers(on) {
			return a.Rate, nil
		}
	}
	return zero(), fw.Violation("work.not_assigned", "the person is not assigned to the work that day")
}

func optional(u fw.UUID) string {
	if u.IsZero() {
		return ""
	}
	return u.String()
}

// AuditSnapshot implementa traits.Snapshotter.
func (w *Work) AuditSnapshot() map[string]any {
	return map[string]any{"code": w.s.Code, "name": w.s.Name, "status": string(w.s.Status), "assignments": len(w.s.Assignments)}
}

// Campos del trabajo.
var (
	WrkFieldCompany  = spec.Comparable("company", func(w *Work) OrganizationID { return w.s.Company })
	WrkFieldCode     = spec.Ordered("code", func(w *Work) string { return w.s.Code })
	WrkFieldKind     = spec.Comparable("kind", func(w *Work) string { return string(w.s.Kind) })
	WrkFieldStatus   = spec.Comparable("status", func(w *Work) string { return string(w.s.Status) })
	WrkFieldParent   = spec.Comparable("parent", func(w *Work) WorkID { return w.s.Parent })
	WrkFieldCustomer = spec.Comparable("customer", func(w *Work) PartyID { return w.s.Customer })
)

// Eventos del trabajo.
type (
	// WorkOpened se lanza cuando se crea un trabajo.
	WorkOpened struct {
		fw.EventMeta
		Company string `json:"company"`
		Code    string `json:"code"`
		Kind    string `json:"kind"`
	}
	// WorkStatusChanged se lanza en cada cambio de estado.
	WorkStatusChanged struct {
		fw.EventMeta
		Company string `json:"company"`
		Code    string `json:"code"`
		From    string `json:"from"`
		To      string `json:"to"`
		On      string `json:"on"`
	}
	// WorkCompleted se lanza cuando se termina un trabajo.
	WorkCompleted struct {
		fw.EventMeta
		Company  string `json:"company"`
		Code     string `json:"code"`
		Kind     string `json:"kind"`
		Customer string `json:"customer,omitempty"`
		Started  string `json:"started"`
		Finished string `json:"finished"`
		Hours    string `json:"hours"`
		Cost     string `json:"cost"`
	}
)

// Implementaciones de EventType.
func (WorkOpened) EventType() string        { return "work.work_opened" }
func (WorkStatusChanged) EventType() string { return "work.status_changed" }
func (WorkCompleted) EventType() string     { return "work.work_completed" }

// NewWorkID devuelve una identidad nueva.
func NewWorkID() WorkID { return WorkID{fw.NewUUID()} }

// ParseWorkID analiza una identidad textual.
func ParseWorkID(s string) (WorkID, error) { u, err := fw.ParseUUID(s); return WorkID{u}, err }

// WorkRepository guarda trabajos.
type WorkRepository = fw.Repository[WorkID, *Work]
