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

// MaxLines limita las monedas de una reserva.
const MaxLines = 20

// Status es dónde está una reserva.
type Status string

// Estados (los del C#: Registrado, EmailVerificado, Notificado, Completado, Cancelado y Expired).
const (
	Registered    Status = "registered"
	EmailVerified Status = "email-verified"
	Notified      Status = "notified" // se avisó al cliente de que el efectivo está preparado
	Completed     Status = "completed"
	Cancelled     Status = "cancelled"
	Expired       Status = "expired"
)

// Statuses lista los estados válidos.
var Statuses = []Status{Registered, EmailVerified, Notified, Completed, Cancelled, Expired}

// forward es el único camino por el que avanza una reserva.
var forward = map[Status]Status{Registered: EmailVerified, EmailVerified: Notified, Notified: Completed}

// Channels por los que llega una reserva.
var Channels = []string{"web", "phone", "office"}

// Line es un importe reservado de una moneda, al cambio cotizado cuando se registró.
type Line struct {
	No            int
	Currency      string
	Requested     vocab.Decimal
	Delivered     vocab.Decimal // en billetes enteros
	BaseRate      vocab.Decimal
	OfferedRate   vocab.Decimal
	MarginKind    MarginKind
	MarginValue   vocab.Decimal
	Eur           vocab.Decimal
	Collector     bool // una pieza de coleccionista, no efectivo para gastar
	CollectorNote string
}

// Step es un cambio de estado.
type Step struct {
	Status Status
	At     time.Time
}

// ReservationState es el estado persistido de una reserva.
type ReservationState struct {
	Company       OrganizationID
	Customer      PartyID
	Reference     string
	Channel       string
	Segment       string
	Pickup        time.Time
	TimeSlot      string
	ExpiresAt     time.Time
	Facility      fw.UUID // la oficina donde se recoge
	PromotionCode string
	Collaborator  PartyID // de quién es el código de promoción que lleva
	Lines         []Line
	Status        Status
	Reason        string // de la cancelación
	History       []Step
	Audit         traits.AuditStamp
}

// Reservation es efectivo en divisa que un cliente recogerá en una oficina, a los cambios cotizados
// cuando se registró.
type Reservation struct {
	fw.BaseAggregateRoot[ReservationID]
	traits.Audited
	s ReservationState
}

// ReferenceOf construye la referencia que dan los clientes: RES, el año y once caracteres de la
// identidad (el formato del C#).
func ReferenceOf(id ReservationID, year int) string {
	hex := strings.ToUpper(strings.ReplaceAll(id.String(), "-", ""))
	return "RES-" + strconv.Itoa(year) + "-" + hex[len(hex)-11:]
}

// ReconstituteReservation reconstruye una reserva.
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

// Request es lo que pide un cliente: cada importe ya cotizado.
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

// RequestLine es un importe de una moneda con su cotización.
type RequestLine struct {
	Quotation
	Collector     bool
	CollectorNote string
}

// RegisterReservation registra una reserva en un momento: la recogida es posterior, y la reserva la
// espera las horas que fija la empresa.
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

// State devuelve el estado (los slices son copias).
func (r *Reservation) State() ReservationState {
	s := r.s
	s.Lines, s.History = slices.Clone(s.Lines), slices.Clone(s.History)
	return s
}

// Total devuelve lo que paga el cliente, en euros.
func (r *Reservation) Total() vocab.Decimal {
	t := zero()
	for _, l := range r.s.Lines {
		t = t.Add(l.Eur)
	}
	return t
}

// Active dice si la reserva sigue en camino de recogerse.
func (r *Reservation) Active() bool {
	return r.s.Status == Registered || r.s.Status == EmailVerified || r.s.Status == Notified
}

// Due dice si una reserva activa ha esperado lo suficiente.
func (r *Reservation) Due(now time.Time) bool { return r.Active() && !now.Before(r.s.ExpiresAt) }

func (r *Reservation) set(to Status, now time.Time) {
	from := r.s.Status
	r.s.Status = to
	r.s.History = append(slices.Clone(r.s.History), Step{Status: to, At: now.UTC()})
	r.Raise(ReservationStatusChanged{EventMeta: r.NewEventMeta(), Company: r.s.Company.String(), Customer: r.s.Customer.String(), Reference: r.s.Reference,
		From: string(from), To: string(to), At: now.UTC(), Total: r.Total().StringFixed(2)})
}

// Advance hace avanzar la reserva un paso: correo verificado, cliente avisado, recogida. Pedir el
// estado que ya tiene no cambia nada; dice si algo cambió.
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

// Cancel abandona una reserva activa. Cancelar una ya cancelada no cambia nada.
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

// Expire cierra una reserva activa que nadie recogió a tiempo. Dice si lo hizo.
func (r *Reservation) Expire(now time.Time) bool {
	if !r.Due(now) {
		return false
	}
	r.set(Expired, now)
	return true
}

// AuditSnapshot implementa traits.Snapshotter.
func (r *Reservation) AuditSnapshot() map[string]any {
	return map[string]any{"reference": r.s.Reference, "status": string(r.s.Status), "total": r.Total().String(), "lines": len(r.s.Lines)}
}

// Campos de la reserva.
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

// Eventos de una reserva.
type (
	// ReservationRegistered se lanza cuando un cliente reserva.
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
	// ReservationStatusChanged se lanza en cada cambio de estado.
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

// Implementaciones de EventType.
func (ReservationRegistered) EventType() string    { return "exchange.reservation_registered" }
func (ReservationStatusChanged) EventType() string { return "exchange.reservation_status_changed" }

// ParseReservationID interpreta una identidad en texto.
func ParseReservationID(s string) (ReservationID, error) {
	u, err := fw.ParseUUID(s)
	return ReservationID{u}, err
}
