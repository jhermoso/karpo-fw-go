// Package mailbox gives every listener of the host a mailbox of its own: a message a listener
// cannot take is kept for that listener and tried again later, instead of being refused. A
// refusal would make the publisher send the message again to everybody and, in the end, give it
// up for everybody. With the in-process transport the host is who delivers, so the mailboxes are
// the host's; a broker with a queue per consumer does the same on its own.
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

// Kind is the aggregate type name of a delivery.
const Kind = "host.delivery"

// ID identifies a delivery.
type ID struct{ fw.UUID }

// NewID returns a fresh identity.
func NewID() ID { return ID{fw.NewUUID()} }

// ParseID parses a textual identity.
func ParseID(s string) (ID, error) { u, err := fw.ParseUUID(s); return ID{u}, err }

// Status is where a delivery is.
type Status string

// Statuses of a delivery. One that reaches its listener is not kept: it is removed.
const (
	Waiting   Status = "waiting"   // it will be tried again
	GivenUp   Status = "given-up"  // tried too many times: it waits for somebody to look at it
	Discarded Status = "discarded" // somebody decided the listener will not get it
)

// Statuses lists the valid statuses.
var Statuses = []Status{Waiting, GivenUp, Discarded}

// BehindCode is why a delivery waits without having failed: an earlier one about the same thing
// has not reached the listener yet.
const BehindCode = "mailbox.behind"

// MaxAttempts is how many times a delivery is tried before it is given up.
const MaxAttempts = 10

// State is the persisted state of a delivery: the message as it came, whom it is for and what
// happened each time it was tried.
type State struct {
	Consumer      string
	EventType     string
	EnvelopeID    string
	Source        string
	Subject       string
	OccurredAt    time.Time
	CorrelationID string
	CausationID   string
	Data          string // the message, as JSON
	Code          string
	Reason        string // what the listener answered the last time
	ReceivedAt    time.Time
	Attempts      int
	NextAttempt   time.Time // zero when it is not to be tried on its own
	Status        Status
	ResolvedAt    time.Time
	Note          string // of who discarded it
	Audit         traits.AuditStamp
}

// Delivery is a message kept for a listener that could not take it.
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

// Reconstitute rebuilds a delivery.
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

// backoff is how long a delivery waits after it failed n times: a minute, two, four... up to an hour.
func backoff(n int) time.Duration {
	if n > 6 {
		return time.Hour
	}
	return time.Minute << (max(n, 1) - 1)
}

// Keep keeps a message a listener refused: it was tried once and will be tried again.
func Keep(id ID, s State, code, reason string, now time.Time) (*Delivery, error) {
	s.Code, s.Reason, s.ReceivedAt, s.Attempts, s.Status = clip(code, 100), clip(reason, 500), now.UTC(), 1, Waiting
	s.NextAttempt, s.ResolvedAt, s.Note = now.UTC().Add(backoff(1)), time.Time{}, ""
	return normalized(id, s)
}

// Queue keeps a message that was not tried: an earlier one about the same thing goes first.
func Queue(id ID, s State, now time.Time) (*Delivery, error) {
	s.Code, s.Reason, s.ReceivedAt, s.Attempts, s.Status = BehindCode, "an earlier message about the same thing has not been delivered yet", now.UTC(), 0, Waiting
	s.NextAttempt, s.ResolvedAt, s.Note = now.UTC(), time.Time{}, ""
	return normalized(id, s)
}

func normalized(id ID, s State) (*Delivery, error) {
	s.Source, s.Subject, s.CorrelationID, s.CausationID = clip(s.Source, 50), clip(s.Subject, 100), clip(s.CorrelationID, 100), clip(s.CausationID, 100)
	return Reconstitute(id, s)
}

// State returns the state.
func (d *Delivery) State() State { return d.s }

// Open reports whether the listener may still get the message.
func (d *Delivery) Open() bool { return d.s.Status == Waiting || d.s.Status == GivenUp }

// Refused notes a try the listener refused. After MaxAttempts the delivery is given up: it is
// not tried again until somebody asks.
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

// Again puts a delivery that was given up back among those that are tried.
func (d *Delivery) Again(now time.Time) {
	if d.s.Status == GivenUp {
		d.s.Status, d.s.Attempts, d.s.NextAttempt = Waiting, 0, now.UTC()
	}
}

// Discard gives up a delivery for good: the listener will not get it. Whoever does it says why.
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

// AuditSnapshot implements traits.Snapshotter.
func (d *Delivery) AuditSnapshot() map[string]any {
	return map[string]any{"consumer": d.s.Consumer, "eventType": d.s.EventType, "status": string(d.s.Status), "attempts": d.s.Attempts, "note": d.s.Note}
}

// Delivery fields.
var (
	FieldConsumer = spec.Comparable("consumer", func(d *Delivery) string { return d.s.Consumer })
	FieldStatus   = spec.Comparable("status", func(d *Delivery) string { return string(d.s.Status) })
	FieldEnvelope = spec.Comparable("envelope_id", func(d *Delivery) string { return d.s.EnvelopeID })
	FieldSubject  = spec.Comparable("subject_id", func(d *Delivery) string { return d.s.Subject })
	FieldReceived = spec.OrderedBy("received_at", func(d *Delivery) time.Time { return d.s.ReceivedAt }, func(a, b time.Time) int { return a.Compare(b) })
)

// Repository stores deliveries.
type Repository = fw.Repository[ID, *Delivery]
