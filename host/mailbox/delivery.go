// Package mailbox da a cada oyente del anfitrión un buzón propio: un mensaje que un oyente no
// puede recoger se le guarda a ese oyente y se vuelve a intentar más tarde, en vez de rechazarlo.
// Un rechazo haría que el publicador enviara el mensaje otra vez a todos y, al final, lo abandonara
// para todos. Con el transporte en proceso quien entrega es el anfitrión, así que los buzones son
// del anfitrión; un broker con una cola por consumidor hace lo mismo por su cuenta.
package mailbox

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// Kind es el nombre de tipo de agregado de una entrega.
const Kind = "host.delivery"

// ID identifica una entrega.
type ID struct{ fw.UUID }

// NewID devuelve una identidad nueva.
func NewID() ID { return ID{fw.NewUUID()} }

// ParseID interpreta una identidad en texto.
func ParseID(s string) (ID, error) { u, err := fw.ParseUUID(s); return ID{u}, err }

// Status dice en qué punto está una entrega.
type Status string

// Estados de una entrega. Una que llega a su oyente no se guarda: se quita.
const (
	Waiting   Status = "waiting"   // se volverá a intentar
	GivenUp   Status = "given-up"  // intentada demasiadas veces: espera a que alguien la mire
	Discarded Status = "discarded" // alguien decidió que el oyente no la recibirá
)

// Statuses lista los estados válidos.
var Statuses = []Status{Waiting, GivenUp, Discarded}

// BehindCode es por qué una entrega espera sin haber fallado: una anterior sobre lo mismo aún no ha
// llegado al oyente.
const BehindCode = "mailbox.behind"

// MaxAttempts es cuántas veces se intenta una entrega antes de abandonarla.
const MaxAttempts = 10

// State es el estado persistido de una entrega: el mensaje tal como llegó, para quién es y qué
// pasó cada vez que se intentó.
type State struct {
	Consumer      string
	EventType     string
	EnvelopeID    string
	Source        string
	Subject       string
	OccurredAt    time.Time
	CorrelationID string
	CausationID   string
	Data          string // el mensaje, como JSON
	Code          string
	Reason        string // lo que contestó el oyente la última vez
	ReceivedAt    time.Time
	Attempts      int
	NextAttempt   time.Time // cero cuando no se ha de intentar por su cuenta
	Status        Status
	ResolvedAt    time.Time
	Note          string // de quien la descartó
	Audit         traits.AuditStamp
}

// Delivery es un mensaje guardado para un oyente que no pudo recogerlo.
type Delivery struct {
	fw.BaseAggregateRoot[ID]
	traits.Audited
	s State
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// Reconstitute reconstruye una entrega.
func Reconstitute(id ID, s State) (*Delivery, error) {
	base, err := fw.NewBaseAggregateRoot(Kind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(s.Consumer != "" && len(s.Consumer) <= 100, "consumer", "required", "whom the message is for")
	v.Require(s.EventType != "" && len(s.EventType) <= 100, "eventType", "required", "the type of the message")
	v.Require(s.EnvelopeID != "" && len(s.EnvelopeID) <= 64, "envelopeId", "required", "the identity of the message")
	v.Require(s.Data != "", "data", "required", "the message")
	v.Require(!s.ReceivedAt.IsZero() && s.Attempts >= 0, "receivedAt", "required", "when it arrived and how many times it was tried")
	v.Require(slices.Contains(Statuses, s.Status), "status", "enum", "a status")
	v.Require((s.Status == Discarded) == !s.ResolvedAt.IsZero(), "resolvedAt", "state", "only what was discarded has the moment it was")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Delivery{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// backoff es cuánto espera una entrega tras fallar n veces: un minuto, dos, cuatro... hasta una hora.
func backoff(n int) time.Duration {
	if n > 6 {
		return time.Hour
	}
	return time.Minute << (max(n, 1) - 1)
}

// Keep guarda un mensaje que rechazó un oyente: se intentó una vez y se volverá a intentar.
func Keep(id ID, s State, code, reason string, now time.Time) (*Delivery, error) {
	s.Code, s.Reason, s.ReceivedAt, s.Attempts, s.Status = clip(code, 100), clip(reason, 500), now.UTC(), 1, Waiting
	s.NextAttempt, s.ResolvedAt, s.Note = now.UTC().Add(backoff(1)), time.Time{}, ""
	return normalized(id, s)
}

// Queue guarda un mensaje que no se intentó: va antes uno anterior sobre lo mismo.
func Queue(id ID, s State, now time.Time) (*Delivery, error) {
	s.Code, s.Reason, s.ReceivedAt, s.Attempts, s.Status = BehindCode, "an earlier message about the same thing has not been delivered yet", now.UTC(), 0, Waiting
	s.NextAttempt, s.ResolvedAt, s.Note = now.UTC(), time.Time{}, ""
	return normalized(id, s)
}

func normalized(id ID, s State) (*Delivery, error) {
	s.Source, s.Subject, s.CorrelationID, s.CausationID = clip(s.Source, 50), clip(s.Subject, 100), clip(s.CorrelationID, 100), clip(s.CausationID, 100)
	return Reconstitute(id, s)
}

// State devuelve el estado.
func (d *Delivery) State() State { return d.s }

// Open dice si el oyente aún puede recibir el mensaje.
func (d *Delivery) Open() bool { return d.s.Status == Waiting || d.s.Status == GivenUp }

// Refused anota un intento que rechazó el oyente. Tras MaxAttempts la entrega se abandona: no se
// vuelve a intentar hasta que alguien lo pida.
func (d *Delivery) Refused(code, reason string, now time.Time) {
	if !d.Open() {
		return
	}
	d.s.Attempts++
	d.s.Code, d.s.Reason = clip(code, 100), clip(reason, 500)
	if d.s.Attempts >= MaxAttempts {
		d.s.Status, d.s.NextAttempt = GivenUp, time.Time{}
		return
	}
	d.s.Status, d.s.NextAttempt = Waiting, now.UTC().Add(backoff(d.s.Attempts))
}

// Again devuelve una entrega abandonada a las que se intentan.
func (d *Delivery) Again(now time.Time) {
	if d.s.Status == GivenUp {
		d.s.Status, d.s.Attempts, d.s.NextAttempt = Waiting, 0, now.UTC()
	}
}

// Discard abandona una entrega para siempre: el oyente no la recibirá. Quien lo hace dice por qué.
func (d *Delivery) Discard(note string, now time.Time) error {
	if !d.Open() {
		return fw.Violation("mailbox.not_open", "the delivery is already "+string(d.s.Status))
	}
	if strings.TrimSpace(note) == "" {
		return fw.Violation("mailbox.discard_note", "say why the listener will not get the message")
	}
	d.s.Status, d.s.ResolvedAt, d.s.NextAttempt, d.s.Note = Discarded, now.UTC(), time.Time{}, clip(note, 500)
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (d *Delivery) AuditSnapshot() map[string]any {
	return map[string]any{"consumer": d.s.Consumer, "eventType": d.s.EventType, "status": string(d.s.Status), "attempts": d.s.Attempts, "note": d.s.Note}
}

// Campos de Delivery.
var (
	FieldConsumer = spec.Comparable("consumer", func(d *Delivery) string { return d.s.Consumer })
	FieldStatus   = spec.Comparable("status", func(d *Delivery) string { return string(d.s.Status) })
	FieldEnvelope = spec.Comparable("envelope_id", func(d *Delivery) string { return d.s.EnvelopeID })
	FieldSubject  = spec.Comparable("subject_id", func(d *Delivery) string { return d.s.Subject })
	FieldReceived = spec.OrderedBy("received_at", func(d *Delivery) time.Time { return d.s.ReceivedAt }, func(a, b time.Time) int { return a.Compare(b) })
)

// Repository guarda entregas.
type Repository = fw.Repository[ID, *Delivery]
