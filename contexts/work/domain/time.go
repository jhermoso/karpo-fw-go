package domain

import (
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// TimeEntryKind es el nombre estable del tipo de agregado.
const TimeEntryKind = "work.time_entry"

// MaxHoursADay es lo que una persona puede anotar en un día, sumando todos sus trabajos.
const MaxHoursADay = 24

// TimeEntryState es el estado persistido de una anotación de tiempo.
type TimeEntryState struct {
	Company  OrganizationID
	Work     WorkID
	Person   PartyID
	Date     vocab.Date
	Hours    vocab.Decimal
	Rate     vocab.Decimal // la tarifa por hora de la asignación ese día, fijada al anotarse
	Billable bool
	Comment  string
	Approved bool
	Audit    traits.AuditStamp
}

// TimeEntry es el tiempo que una persona trabajó en un trabajo en un día (el TimeEntry del C# tenía
// horas pero no día, y su Timesheet ni periodo ni estado). Un borrador se puede corregir o retirar;
// una vez aprobado es definitivo y cuenta en las horas y el coste del trabajo.
type TimeEntry struct {
	fw.BaseAggregateRoot[TimeEntryID]
	traits.Audited
	s TimeEntryState
}

func checkHours(v *fw.Validation, hours vocab.Decimal, comment string) {
	v.Require(hours.IsPositive() && !hours.GreaterThan(vocab.DecimalFromInt(MaxHoursADay)) && hours.Equal(hours.Round(2)), "hours", "range",
		"more than 0 and at most 24 hours, with two decimals")
	v.Require(utf8.RuneCountInString(comment) <= 500, "comment", "length", "at most 500 characters")
}

// ReconstituteTimeEntry reconstruye una anotación de tiempo.
func ReconstituteTimeEntry(id TimeEntryID, s TimeEntryState) (*TimeEntry, error) {
	base, err := fw.NewBaseAggregateRoot(TimeEntryKind, id)
	if err != nil {
		return nil, err
	}
	s.Comment = strings.TrimSpace(s.Comment)
	var v fw.Validation
	v.Require(!s.Company.IsZero() && !s.Work.IsZero() && !s.Person.IsZero(), "work", "required", "company, work and person are required")
	v.Require(!s.Date.IsZero(), "date", "required", "the day is required")
	v.Require(!s.Rate.IsNegative(), "rate", "range", "a rate that is not negative")
	checkHours(&v, s.Hours, s.Comment)
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &TimeEntry{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// RecordTime anota el tiempo de una persona en un trabajo, a la tarifa que da el trabajo.
func RecordTime(id TimeEntryID, w *Work, person PartyID, on vocab.Date, hours vocab.Decimal, billable bool, comment string) (*TimeEntry, error) {
	if on.IsZero() {
		return nil, fw.Violation("work.date", "the day is required")
	}
	rate, err := w.RateFor(person, on)
	if err != nil {
		return nil, err
	}
	return ReconstituteTimeEntry(id, TimeEntryState{Company: w.s.Company, Work: w.ID(), Person: person, Date: on, Hours: hours, Rate: rate,
		Billable: billable, Comment: comment})
}

// State devuelve el estado.
func (t *TimeEntry) State() TimeEntryState { return t.s }

// Cost devuelve las horas a la tarifa, en céntimos.
func (t *TimeEntry) Cost() vocab.Decimal { return t.s.Hours.Mul(t.s.Rate).Round(2) }

func (t *TimeEntry) draft() error {
	if t.s.Approved {
		return fw.Violation("work.time_approved", "approved time is final")
	}
	return nil
}

// Correct cambia las horas, el comentario y si un borrador es facturable.
func (t *TimeEntry) Correct(hours vocab.Decimal, billable bool, comment string) error {
	if err := t.draft(); err != nil {
		return err
	}
	comment = strings.TrimSpace(comment)
	var v fw.Validation
	checkHours(&v, hours, comment)
	if err := v.Err(); err != nil {
		return err
	}
	t.s.Hours, t.s.Billable, t.s.Comment = hours, billable, comment
	return nil
}

// Withdraw comprueba que la anotación se puede borrar: sólo un borrador.
func (t *TimeEntry) Withdraw() error { return t.draft() }

// Approve hace definitiva la anotación.
func (t *TimeEntry) Approve() error {
	if err := t.draft(); err != nil {
		return err
	}
	t.s.Approved = true
	t.Raise(TimeApproved{EventMeta: t.NewEventMeta(), Company: t.s.Company.String(), Work: t.s.Work.String(), Person: t.s.Person.String(),
		Date: t.s.Date.String(), Hours: t.s.Hours.StringFixed(2), Cost: t.Cost().StringFixed(2), Billable: t.s.Billable})
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (t *TimeEntry) AuditSnapshot() map[string]any {
	return map[string]any{"work": t.s.Work.String(), "person": t.s.Person.String(), "date": t.s.Date.String(), "hours": t.s.Hours.String(),
		"approved": t.s.Approved}
}

// Campos de la anotación de tiempo.
var (
	TimFieldCompany  = spec.Comparable("company", func(t *TimeEntry) OrganizationID { return t.s.Company })
	TimFieldWork     = spec.Comparable("work_id", func(t *TimeEntry) WorkID { return t.s.Work })
	TimFieldPerson   = spec.Comparable("person", func(t *TimeEntry) PartyID { return t.s.Person })
	TimFieldDate     = spec.OrderedBy("work_date", func(t *TimeEntry) vocab.Date { return t.s.Date }, vocab.CompareDates)
	TimFieldApproved = spec.Comparable("approved", func(t *TimeEntry) bool { return t.s.Approved })
)

// TimeApproved se lanza cuando una anotación de tiempo pasa a ser definitiva.
type TimeApproved struct {
	fw.EventMeta
	Company  string `json:"company"`
	Work     string `json:"work"`
	Person   string `json:"person"`
	Date     string `json:"date"`
	Hours    string `json:"hours"`
	Cost     string `json:"cost"`
	Billable bool   `json:"billable"`
}

// EventType implementa fw.Event.
func (TimeApproved) EventType() string { return "work.time_approved" }

// NewTimeEntryID devuelve una identidad nueva.
func NewTimeEntryID() TimeEntryID { return TimeEntryID{fw.NewUUID()} }

// ParseTimeEntryID analiza una identidad textual.
func ParseTimeEntryID(s string) (TimeEntryID, error) {
	u, err := fw.ParseUUID(s)
	return TimeEntryID{u}, err
}

// TimeEntryRepository guarda anotaciones de tiempo.
type TimeEntryRepository = fw.Repository[TimeEntryID, *TimeEntry]
