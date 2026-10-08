package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// ParkedKind is the aggregate type name of a parked fact.
const ParkedKind = "accounting.parked_fact"

// ParkedID identifies a parked fact.
type ParkedID struct{ fw.UUID }

// NewParkedID returns a fresh identity.
func NewParkedID() ParkedID { return ParkedID{fw.NewUUID()} }

// ParseParkedID parses a textual identity.
func ParseParkedID(s string) (ParkedID, error) { u, err := fw.ParseUUID(s); return ParkedID{u}, err }

// ParkedStatus is what became of a parked fact.
type ParkedStatus string

// Statuses of a parked fact.
const (
	Parked    ParkedStatus = "parked"    // it waits: it could not be posted yet
	Posted    ParkedStatus = "posted"    // it was posted on a later try
	Discarded ParkedStatus = "discarded" // somebody decided it will not be posted
)

// ParkedStatuses lists the valid statuses.
var ParkedStatuses = []ParkedStatus{Parked, Posted, Discarded}

// WaitingCode is why a fact waits behind another: nothing is wrong with it.
const WaitingCode = "accounting.waiting"

// ParkedState is the persisted state of a parked fact: the message as it came and why it waits.
type ParkedState struct {
	Company       OrganizationID // whose books it goes to; zero when the fact does not say
	EventType     string
	EnvelopeID    string
	Source        string
	Subject       string
	OccurredAt    time.Time
	CorrelationID string
	CausationID   string
	Data          string // the fact, as JSON
	Code          string // of the rule that kept it from being posted
	Reason        string
	ReceivedAt    time.Time
	Attempts      int
	Status        ParkedStatus
	ResolvedAt    time.Time
	Note          string // of who discarded it
	Audit         traits.AuditStamp
}

// ParkedFact is a fact of another context that Accounting could not post when it arrived (the
// company has no ledger yet, the posting profile lacks an account, the period is closed) and
// keeps to post later. Refusing it instead would hold its publisher back for every listener.
type ParkedFact struct {
	fw.BaseAggregateRoot[ParkedID]
	traits.Audited
	s ParkedState
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// ReconstituteParked rebuilds a parked fact.
func ReconstituteParked(id ParkedID, s ParkedState) (*ParkedFact, error) {
	base, err := fw.NewBaseAggregateRoot(ParkedKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(s.EventType != "" && len(s.EventType) <= 100, "eventType", "required", "the type of the fact")
	v.Require(s.EnvelopeID != "" && len(s.EnvelopeID) <= 64, "envelopeId", "required", "the identity of the message")
	v.Require(s.Data != "", "data", "required", "the fact")
	v.Require(!s.ReceivedAt.IsZero() && s.Attempts >= 1, "receivedAt", "required", "when it arrived and how many times it was tried")
	v.Require(slices.Contains(ParkedStatuses, s.Status), "status", "enum", "a status")
	v.Require((s.Status == Parked) == s.ResolvedAt.IsZero(), "resolvedAt", "state", "only what no longer waits has the moment it stopped")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &ParkedFact{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// Park keeps a fact that could not be posted, with the rule that refused it.
func Park(id ParkedID, s ParkedState, code, reason string, now time.Time) (*ParkedFact, error) {
	s.Code, s.Reason, s.ReceivedAt, s.Attempts, s.Status = clip(code, 100), clip(reason, 500), now.UTC(), 1, Parked
	s.ResolvedAt, s.Note = time.Time{}, ""
	s.Source, s.Subject, s.CorrelationID, s.CausationID = clip(s.Source, 50), clip(s.Subject, 100), clip(s.CorrelationID, 100), clip(s.CausationID, 100)
	return ReconstituteParked(id, s)
}

// State returns the state.
func (p *ParkedFact) State() ParkedState { return p.s }

// Waiting reports whether the fact still waits.
func (p *ParkedFact) Waiting() bool { return p.s.Status == Parked }

// Refused notes another try that did not post it, and why this time.
func (p *ParkedFact) Refused(code, reason string) {
	if p.Waiting() {
		p.s.Attempts++
		p.s.Code, p.s.Reason = clip(code, 100), clip(reason, 500)
	}
}

// MarkPosted notes that a later try posted it.
func (p *ParkedFact) MarkPosted(now time.Time) {
	if p.Waiting() {
		p.s.Attempts++
		p.s.Status, p.s.ResolvedAt, p.s.Code, p.s.Reason = Posted, now.UTC(), "", ""
	}
}

// Discard gives up a fact that waits: it will not be posted. Whoever does it says why.
func (p *ParkedFact) Discard(note string, now time.Time) error {
	if !p.Waiting() {
		return fw.Violation("accounting.not_parked", "the fact no longer waits: it is "+string(p.s.Status))
	}
	if strings.TrimSpace(note) == "" {
		return fw.Violation("accounting.discard_note", "say why it will not be posted")
	}
	p.s.Status, p.s.ResolvedAt, p.s.Note = Discarded, now.UTC(), clip(note, 500)
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (p *ParkedFact) AuditSnapshot() map[string]any {
	return map[string]any{"eventType": p.s.EventType, "status": string(p.s.Status), "attempts": p.s.Attempts, "code": p.s.Code, "note": p.s.Note}
}

// Parked fact fields.
var (
	ParFieldCompany  = spec.Comparable("company", func(p *ParkedFact) OrganizationID { return p.s.Company })
	ParFieldStatus   = spec.Comparable("status", func(p *ParkedFact) string { return string(p.s.Status) })
	ParFieldEnvelope = spec.Comparable("envelope_id", func(p *ParkedFact) string { return p.s.EnvelopeID })
	ParFieldSubject  = spec.Comparable("subject_id", func(p *ParkedFact) string { return p.s.Subject })
	ParFieldReceived = spec.OrderedBy("received_at", func(p *ParkedFact) time.Time { return p.s.ReceivedAt }, func(a, b time.Time) int { return a.Compare(b) })
)

// ParkedRepository stores parked facts.
type ParkedRepository = fw.Repository[ParkedID, *ParkedFact]
