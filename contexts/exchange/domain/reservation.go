package domain

import (
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// MaxLines bounds the currencies of a reservation.
const MaxLines = 20

// Status is where a reservation is.
type Status string

// Statuses (those of the C#: Registrado, EmailVerificado, Notificado, Completado, Cancelado and
// Expired).
const (
	Registered    Status = "registered"
	EmailVerified Status = "email-verified"
	Notified      Status = "notified" // the customer was told the cash is ready
	Completed     Status = "completed"
	Cancelled     Status = "cancelled"
	Expired       Status = "expired"
)

// Statuses lists the valid statuses.
var Statuses = []Status{Registered, EmailVerified, Notified, Completed, Cancelled, Expired}

// forward is the only way a reservation advances.
var forward = map[Status]Status{Registered: EmailVerified, EmailVerified: Notified, Notified: Completed}

// Channels a reservation arrives through.
var Channels = []string{"web", "phone", "office"}

// Line is an amount of a currency reserved, at the rate quoted when it was registered.
type Line struct {
	No            int
	Currency      string
	Requested     vocab.Decimal
	Delivered     vocab.Decimal // in whole notes
	BaseRate      vocab.Decimal
	OfferedRate   vocab.Decimal
	MarginKind    MarginKind
	MarginValue   vocab.Decimal
	Eur           vocab.Decimal
	Collector     bool // a collector's piece, not cash to spend
	CollectorNote string
}

// Step is a change of status.
type Step struct {
	Status Status
	At     time.Time
}

// ReservationState is the persisted state of a reservation.
type ReservationState struct {
	Company       OrganizationID
	Customer      PartyID
	Reference     string
	Channel       string
	Segment       string
	Pickup        time.Time
	TimeSlot      string
	ExpiresAt     time.Time
	Facility      fw.UUID // the office where it is collected
	PromotionCode string
	Collaborator  PartyID // whose promotion code it carries
	Lines         []Line
	Status        Status
	Reason        string // of the cancellation
	History       []Step
	Audit         traits.AuditStamp
}

// Reservation is foreign cash a customer will collect at an office, at the rates quoted when it
// was registered.
type Reservation struct {
	fw.BaseAggregateRoot[ReservationID]
	traits.Audited
	s ReservationState
}

// ReferenceOf builds the reference customers quote: RES, the year and eleven characters of the
// identity (the C# format).
func ReferenceOf(id ReservationID, year int) string {
	hex := strings.ToUpper(strings.ReplaceAll(id.String(), "-", ""))
	return "RES-" + strconv.Itoa(year) + "-" + hex[len(hex)-11:]
}

// ReconstituteReservation rebuilds a reservation.
func ReconstituteReservation(id ReservationID, s ReservationState) (*Reservation, error) {
	base, err := fw.NewBaseAggregateRoot(ReservationKind, id)
	if err != nil {
		return nil, err
	}
	s.Channel, s.TimeSlot, s.PromotionCode = strings.ToLower(strings.TrimSpace(s.Channel)), strings.TrimSpace(s.TimeSlot), strings.TrimSpace(s.PromotionCode)
	s.Segment = NormalizeCode(s.Segment)
	var v fw.Validation
	v.Require(!s.Company.IsZero() && !s.Customer.IsZero(), "customer", "required", "the company and the customer are required")
	v.Require(s.Reference != "" && len(s.Reference) <= 40, "reference", "length", "a reference of 1 to 40 characters")
	v.Require(slices.Contains(Channels, s.Channel), "channel", "enum", "web, phone or office")
	v.Require(validCode(s.Segment, 20), "segment", "format", "a segment code")
	v.Require(!s.Pickup.IsZero() && s.ExpiresAt.After(s.Pickup), "pickup", "range", "a pickup time, and an expiry after it")
	v.Require(utf8.RuneCountInString(s.TimeSlot) <= 50, "timeSlot", "length", "at most 50 characters")
	v.Require(utf8.RuneCountInString(s.PromotionCode) <= 15, "promotionCode", "length", "at most 15 characters")
	v.Require(slices.Contains(Statuses, s.Status), "status", "enum", "a status")
	v.Require(len(s.History) >= 1, "history", "required", "a reservation has the moment it was registered")
	v.Require(len(s.Lines) >= 1 && len(s.Lines) <= MaxLines, "lines", "range", "from 1 to 20 currencies")
	seen := map[string]bool{}
	s.Lines = slices.Clone(s.Lines)
	for i := range s.Lines {
		l := &s.Lines[i]
		l.CollectorNote = strings.TrimSpace(l.CollectorNote)
		v.Require(l.No == i+1, "lines", "order", "lines are numbered from 1")
		v.Require(validCode(l.Currency, 10) && !seen[l.Currency], "lines", "currency", "each currency once")
		seen[l.Currency] = true
		v.Require(l.Requested.IsPositive() && l.Delivered.IsPositive() && !l.Delivered.GreaterThan(l.Requested), "lines", "amount",
			"a positive amount, and what is delivered not above it")
		v.Require(l.OfferedRate.IsPositive() && !l.Eur.IsNegative(), "lines", "rate", "a positive rate")
		v.Require(!l.Collector || l.CollectorNote != "", "lines", "collector", "a collector's piece needs a note saying which")
		v.Require(utf8.RuneCountInString(l.CollectorNote) <= 500, "lines", "note", "a note of at most 500 characters")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.History = slices.Clone(s.History)
	return &Reservation{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// Request is what a customer asks for: each amount already quoted.
type Request struct {
	Company       OrganizationID
	Customer      PartyID
	Channel       string
	Segment       string
	Pickup        time.Time
	TimeSlot      string
	Facility      fw.UUID
	PromotionCode string
	Collaborator  PartyID
	Lines         []RequestLine
}

// RequestLine is an amount of a currency with its quotation.
type RequestLine struct {
	Quotation
	Collector     bool
	CollectorNote string
}

// RegisterReservation registers a reservation at a moment: the pickup is later, and the
// reservation waits for it the hours the company sets.
func RegisterReservation(id ReservationID, r Request, expiryHours int, now time.Time) (*Reservation, error) {
	if !r.Pickup.After(now) {
		return nil, fw.Violation("exchange.pickup_past", "the pickup time is still to come")
	}
	s := ReservationState{Company: r.Company, Customer: r.Customer, Reference: ReferenceOf(id, now.UTC().Year()), Channel: r.Channel, Segment: r.Segment,
		Pickup: r.Pickup.UTC(), TimeSlot: r.TimeSlot, ExpiresAt: r.Pickup.UTC().Add(time.Duration(expiryHours) * time.Hour), Facility: r.Facility,
		PromotionCode: r.PromotionCode, Collaborator: r.Collaborator, Status: Registered, History: []Step{{Status: Registered, At: now.UTC()}}}
	for i, l := range r.Lines {
		s.Lines = append(s.Lines, Line{No: i + 1, Currency: l.Currency, Requested: l.Amount, Delivered: l.Delivered, BaseRate: l.BaseRate,
			OfferedRate: l.OfferedRate, MarginKind: l.MarginKind, MarginValue: l.MarginValue, Eur: l.Eur, Collector: l.Collector, CollectorNote: l.CollectorNote})
	}
	res, err := ReconstituteReservation(id, s)
	if err != nil {
		return nil, err
	}
	res.Raise(ReservationRegistered{EventMeta: res.NewEventMeta(), Company: s.Company.String(), Customer: s.Customer.String(), Reference: s.Reference,
		Channel: res.s.Channel, Pickup: s.Pickup, ExpiresAt: s.ExpiresAt, Lines: len(s.Lines), Total: res.Total().StringFixed(2)})
	return res, nil
}

// State returns the state (slices are copies).
func (r *Reservation) State() ReservationState {
	s := r.s
	s.Lines, s.History = slices.Clone(s.Lines), slices.Clone(s.History)
	return s
}

// Total returns what the customer pays, in euros.
func (r *Reservation) Total() vocab.Decimal {
	t := zero()
	for _, l := range r.s.Lines {
		t = t.Add(l.Eur)
	}
	return t
}

// Active reports whether the reservation is still on its way to being collected.
func (r *Reservation) Active() bool {
	return r.s.Status == Registered || r.s.Status == EmailVerified || r.s.Status == Notified
}

// Due reports whether an active reservation has waited long enough.
func (r *Reservation) Due(now time.Time) bool { return r.Active() && !now.Before(r.s.ExpiresAt) }

func (r *Reservation) set(to Status, now time.Time) {
	from := r.s.Status
	r.s.Status = to
	r.s.History = append(slices.Clone(r.s.History), Step{Status: to, At: now.UTC()})
	r.Raise(ReservationStatusChanged{EventMeta: r.NewEventMeta(), Company: r.s.Company.String(), Customer: r.s.Customer.String(), Reference: r.s.Reference,
		From: string(from), To: string(to), At: now.UTC(), Total: r.Total().StringFixed(2)})
}

// Advance moves the reservation one step forward: email verified, customer notified, collected.
// Asking for the status it already has changes nothing; it reports whether anything changed.
func (r *Reservation) Advance(to Status, now time.Time) (bool, error) {
	if r.s.Status == to {
		return false, nil
	}
	if r.Due(now) {
		return false, fw.Violation("exchange.expired", "the reservation waited until "+r.s.ExpiresAt.Format(time.RFC3339))
	}
	if next, ok := forward[r.s.Status]; !ok || next != to {
		return false, fw.Violation("exchange.transition", "a reservation "+string(r.s.Status)+" cannot become "+string(to))
	}
	r.set(to, now)
	return true, nil
}

// Cancel abandons an active reservation. Cancelling a cancelled one changes nothing.
func (r *Reservation) Cancel(now time.Time, reason string) (bool, error) {
	if r.s.Status == Cancelled {
		return false, nil
	}
	if !r.Active() {
		return false, fw.Violation("exchange.transition", "a reservation "+string(r.s.Status)+" cannot be cancelled")
	}
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > 200 {
		return false, fw.Violation("exchange.reason", "a reason of at most 200 characters")
	}
	r.s.Reason = reason
	r.set(Cancelled, now)
	return true, nil
}

// Expire closes an active reservation nobody collected in time. It reports whether it did.
func (r *Reservation) Expire(now time.Time) bool {
	if !r.Due(now) {
		return false
	}
	r.set(Expired, now)
	return true
}

// AuditSnapshot implements traits.Snapshotter.
func (r *Reservation) AuditSnapshot() map[string]any {
	return map[string]any{"reference": r.s.Reference, "status": string(r.s.Status), "total": r.Total().String(), "lines": len(r.s.Lines)}
}

// Reservation fields.
var (
	ResFieldCompany   = spec.Comparable("company", func(r *Reservation) OrganizationID { return r.s.Company })
	ResFieldCustomer  = spec.Comparable("customer", func(r *Reservation) PartyID { return r.s.Customer })
	ResFieldReference = spec.Ordered("reference", func(r *Reservation) string { return r.s.Reference })
	ResFieldStatus    = spec.Comparable("status", func(r *Reservation) string { return string(r.s.Status) })
	ResFieldFacility  = spec.Comparable("facility", func(r *Reservation) fw.UUID { return r.s.Facility })
	ResFieldCreated   = spec.OrderedBy("created_on", func(r *Reservation) vocab.Date { return vocab.DateOf(r.s.History[0].At) }, vocab.CompareDates)
	ResFieldPickup    = spec.OrderedBy("pickup_on", func(r *Reservation) vocab.Date { return vocab.DateOf(r.s.Pickup) }, vocab.CompareDates)
	ResFieldLines     = spec.Collection("lines", func(r *Reservation) []Line { return r.s.Lines })
	LineFieldCurrency = spec.Comparable("currency", func(l Line) string { return l.Currency })
)

// Events of a reservation.
type (
	// ReservationRegistered is raised when a customer reserves.
	ReservationRegistered struct {
		fw.EventMeta
		Company   string    `json:"company"`
		Customer  string    `json:"customer"`
		Reference string    `json:"reference"`
		Channel   string    `json:"channel"`
		Pickup    time.Time `json:"pickup"`
		ExpiresAt time.Time `json:"expiresAt"`
		Lines     int       `json:"lines"`
		Total     string    `json:"total"`
	}
	// ReservationStatusChanged is raised on each change of status.
	ReservationStatusChanged struct {
		fw.EventMeta
		Company   string    `json:"company"`
		Customer  string    `json:"customer"`
		Reference string    `json:"reference"`
		From      string    `json:"from"`
		To        string    `json:"to"`
		At        time.Time `json:"at"`
		Total     string    `json:"total"`
	}
)

// EventType implementations.
func (ReservationRegistered) EventType() string    { return "exchange.reservation_registered" }
func (ReservationStatusChanged) EventType() string { return "exchange.reservation_status_changed" }

// ParseReservationID parses a textual identity.
func ParseReservationID(s string) (ReservationID, error) {
	u, err := fw.ParseUUID(s)
	return ReservationID{u}, err
}
