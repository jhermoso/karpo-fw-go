// Package domain is the Work model: the work a company does (projects, tasks, maintenance and
// production jobs) with its plan, the people assigned to it, a status that moves by rules, and
// the time recorded against it. The C# WorkEffort was a header whose status, history and actual
// hours were typed by hand and independent of each other, and its time entries had no date.
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

// WorkKind is the stable aggregate type name.
const WorkKind = "work.work"

// Identities of the context.
type (
	// WorkID identifies a piece of work.
	WorkID struct{ fw.UUID }
	// TimeEntryID identifies a time entry.
	TimeEntryID struct{ fw.UUID }
	// OrganizationID is the company doing the work, an internal organization of Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID is a customer or a person of the Parties context.
	PartyID struct{ fw.UUID }
)

// Kind is what a piece of work is (the seeded C# work effort types).
type Kind string

// Kinds.
const (
	Project     Kind = "project"
	Task        Kind = "task"
	Maintenance Kind = "maintenance"
	Production  Kind = "production"
)

// Kinds lists the valid kinds.
var Kinds = []Kind{Project, Task, Maintenance, Production}

// Purposes lists the valid purposes (the seeded C# purpose types); a piece of work may have none.
var Purposes = []string{"improvement", "repair", "preventive-check", "audit", "training"}

// Status is where a piece of work is.
type Status string

// Statuses (the seeded C# status types).
const (
	Created    Status = "created"
	Scheduled  Status = "scheduled"
	InProgress Status = "in-progress"
	OnHold     Status = "on-hold"
	Completed  Status = "completed"
	Cancelled  Status = "cancelled"
)

// Statuses lists the valid statuses.
var Statuses = []Status{Created, Scheduled, InProgress, OnHold, Completed, Cancelled}

// next is the rule of the status: where each one may go.
var next = map[Status][]Status{
	Created:    {Scheduled, InProgress, Cancelled},
	Scheduled:  {InProgress, Cancelled},
	InProgress: {OnHold, Completed, Cancelled},
	OnHold:     {InProgress, Cancelled},
}

// Step is a change of status.
type Step struct {
	Status Status
	On     vocab.Date
}

// Assignment is a person working on a piece of work, with the hourly rate of their time.
type Assignment struct {
	Person PartyID
	Role   string
	Rate   vocab.Decimal // cost of an hour
	From   vocab.Date
	Until  vocab.Date // zero while assigned
}

// Covers reports whether the assignment is in force on a day.
func (a Assignment) Covers(d vocab.Date) bool {
	return !d.Before(a.From) && (a.Until.IsZero() || !d.After(a.Until))
}

// Plan is what may be edited while the work is open.
type Plan struct {
	Name           string
	Description    string
	Purpose        string
	Customer       PartyID // who the work is for, if anyone
	Facility       fw.UUID // where (Facilities)
	Asset          fw.UUID // on what (Assets)
	PlannedStart   vocab.Date
	PlannedEnd     vocab.Date
	EstimatedHours vocab.Decimal
	Budget         vocab.Decimal
}

// WorkState is the persisted state of a piece of work.
type WorkState struct {
	Company OrganizationID
	Code    string
	Kind    Kind
	Parent  WorkID // the project a task belongs to
	Plan
	Status       Status
	Started      vocab.Date
	Finished     vocab.Date
	CancelReason string
	Hours        vocab.Decimal // approved hours when completed
	Cost         vocab.Decimal // their cost when completed
	Assignments  []Assignment
	History      []Step
	Audit        traits.AuditStamp
}

// Work is a piece of work of a company.
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

// ReconstituteWork rebuilds a piece of work.
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

// OpenWork creates a piece of work on a day.
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

// State returns the state (slices are copies).
func (w *Work) State() WorkState {
	s := w.s
	s.Assignments, s.History = slices.Clone(s.Assignments), slices.Clone(s.History)
	return s
}

// Open reports whether the work is neither completed nor cancelled.
func (w *Work) Open() bool { return w.s.Status != Completed && w.s.Status != Cancelled }

func (w *Work) closed() error {
	if !w.Open() {
		return fw.Violation("work.closed", "the work is "+string(w.s.Status))
	}
	return nil
}

// Change replaces the plan of an open piece of work.
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

// move changes the status by the rule, on a day not before the last change.
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

// Schedule commits the planned dates.
func (w *Work) Schedule(on vocab.Date) error {
	if w.s.PlannedStart.IsZero() || w.s.PlannedEnd.IsZero() {
		return fw.Violation("work.plan_required", "scheduling needs the planned start and end")
	}
	return w.move(Scheduled, on)
}

// Start begins the work, or resumes it when on hold.
func (w *Work) Start(on vocab.Date) error {
	if err := w.move(InProgress, on); err != nil {
		return err
	}
	if w.s.Started.IsZero() {
		w.s.Started = on
	}
	return nil
}

// Hold pauses the work.
func (w *Work) Hold(on vocab.Date) error { return w.move(OnHold, on) }

// Complete finishes the work with the approved hours recorded against it and their cost.
func (w *Work) Complete(on vocab.Date, hours, cost vocab.Decimal) error {
	if err := w.move(Completed, on); err != nil {
		return err
	}
	w.s.Finished, w.s.Hours, w.s.Cost = on, hours, cost
	w.Raise(WorkCompleted{EventMeta: w.NewEventMeta(), Company: w.s.Company.String(), Code: w.s.Code, Kind: string(w.s.Kind),
		Customer: optional(w.s.Customer.UUID), Started: w.s.Started.String(), Finished: on.String(), Hours: hours.StringFixed(2), Cost: cost.StringFixed(2)})
	return nil
}

// Cancel abandons the work, with a reason.
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

// Assign puts a person on the work from a day at an hourly rate, or changes the role and rate of
// a person already on it.
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

// Release takes a person off the work after a day.
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

// RateFor returns the hourly rate of a person on a day: time is recorded only by who is assigned
// that day, on work in progress that had started by then.
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

// AuditSnapshot implements traits.Snapshotter.
func (w *Work) AuditSnapshot() map[string]any {
	return map[string]any{"code": w.s.Code, "name": w.s.Name, "status": string(w.s.Status), "assignments": len(w.s.Assignments)}
}

// Work fields.
var (
	WrkFieldCompany  = spec.Comparable("company", func(w *Work) OrganizationID { return w.s.Company })
	WrkFieldCode     = spec.Ordered("code", func(w *Work) string { return w.s.Code })
	WrkFieldKind     = spec.Comparable("kind", func(w *Work) string { return string(w.s.Kind) })
	WrkFieldStatus   = spec.Comparable("status", func(w *Work) string { return string(w.s.Status) })
	WrkFieldParent   = spec.Comparable("parent", func(w *Work) WorkID { return w.s.Parent })
	WrkFieldCustomer = spec.Comparable("customer", func(w *Work) PartyID { return w.s.Customer })
)

// Events of the work.
type (
	// WorkOpened is raised when a piece of work is created.
	WorkOpened struct {
		fw.EventMeta
		Company string `json:"company"`
		Code    string `json:"code"`
		Kind    string `json:"kind"`
	}
	// WorkStatusChanged is raised on each change of status.
	WorkStatusChanged struct {
		fw.EventMeta
		Company string `json:"company"`
		Code    string `json:"code"`
		From    string `json:"from"`
		To      string `json:"to"`
		On      string `json:"on"`
	}
	// WorkCompleted is raised when a piece of work is finished.
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

// EventType implementations.
func (WorkOpened) EventType() string        { return "work.work_opened" }
func (WorkStatusChanged) EventType() string { return "work.status_changed" }
func (WorkCompleted) EventType() string     { return "work.work_completed" }

// NewWorkID returns a new identity.
func NewWorkID() WorkID { return WorkID{fw.NewUUID()} }

// ParseWorkID parses a textual identity.
func ParseWorkID(s string) (WorkID, error) { u, err := fw.ParseUUID(s); return WorkID{u}, err }

// WorkRepository stores work.
type WorkRepository = fw.Repository[WorkID, *Work]
