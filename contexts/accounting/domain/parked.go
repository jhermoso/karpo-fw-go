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

// ParkedKind es el nombre del tipo de agregado de un hecho aparcado.
const ParkedKind = "accounting.parked_fact"

// ParkedID identifica un hecho aparcado.
type ParkedID struct{ fw.UUID }

// NewParkedID devuelve una identidad nueva.
func NewParkedID() ParkedID { return ParkedID{fw.NewUUID()} }

// ParseParkedID interpreta una identidad en texto.
func ParseParkedID(s string) (ParkedID, error) { u, err := fw.ParseUUID(s); return ParkedID{u}, err }

// ParkedStatus es en qué quedó un hecho aparcado.
type ParkedStatus string

// Estados de un hecho aparcado.
const (
	Parked    ParkedStatus = "parked"    // espera: aún no pudo contabilizarse
	Posted    ParkedStatus = "posted"    // se contabilizó en un intento posterior
	Discarded ParkedStatus = "discarded" // alguien decidió que no se contabilizará
)

// ParkedStatuses lista los estados válidos.
var ParkedStatuses = []ParkedStatus{Parked, Posted, Discarded}

// WaitingCode es por qué un hecho espera detrás de otro: no le pasa nada.
const WaitingCode = "accounting.waiting"

// FailedCode es por qué espera un hecho cuando lo que impidió contabilizarlo en un intento
// posterior no fue una regla (una validación, un fallo): el motivo dice qué.
const FailedCode = "accounting.failed"

// ParkedState es el estado persistido de un hecho aparcado: el mensaje tal como llegó y por qué espera.
type ParkedState struct {
	Company       OrganizationID // a qué libros va; cero cuando el hecho no lo dice
	EventType     string
	EnvelopeID    string
	Source        string
	Subject       string
	OccurredAt    time.Time
	CorrelationID string
	CausationID   string
	Data          string // el hecho, en JSON
	Code          string // de la regla que impidió contabilizarlo
	Reason        string
	ReceivedAt    time.Time
	Attempts      int // los intentos que cambiaron algo: ver Refused
	Status        ParkedStatus
	ResolvedAt    time.Time
	Note          string // de quien lo descartó
	Audit         traits.AuditStamp
}

// ParkedFact es un hecho de otro contexto que Accounting no pudo contabilizar cuando llegó (la
// empresa aún no tiene contabilidad, al perfil de contabilización le falta una cuenta, el periodo
// está cerrado) y que guarda para contabilizarlo después. Rechazarlo retendría a su publicador para
// todos los oyentes.
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

// ReconstituteParked reconstruye un hecho aparcado.
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

// Park guarda un hecho que no pudo contabilizarse, con la regla que lo rechazó.
func Park(id ParkedID, s ParkedState, code, reason string, now time.Time) (*ParkedFact, error) {
	s.Code, s.Reason, s.ReceivedAt, s.Attempts, s.Status = clip(code, 100), clip(reason, 500), now.UTC(), 1, Parked
	s.ResolvedAt, s.Note = time.Time{}, ""
	s.Source, s.Subject, s.CorrelationID, s.CausationID = clip(s.Source, 50), clip(s.Subject, 100), clip(s.CorrelationID, 100), clip(s.CausationID, 100)
	return ReconstituteParked(id, s)
}

// State devuelve el estado.
func (p *ParkedFact) State() ParkedState { return p.s }

// Waiting dice si el hecho todavía espera.
func (p *ParkedFact) Waiting() bool { return p.s.Status == Parked }

// SameRefusal dice si el hecho ya espera por este código y este motivo: un intento que acaba como
// el anterior no tiene nada nuevo que anotar, y no se escribe.
func (p *ParkedFact) SameRefusal(code, reason string) bool {
	return p.Waiting() && p.s.Code == clip(code, 100) && p.s.Reason == clip(reason, 500)
}

// Refused anota un intento que no lo contabilizó, y por qué esta vez. Quien reintenta sólo lo
// llama cuando cambió el código o el motivo (SameRefusal), así que Attempts no cuenta todos los
// intentos: cuenta el primero, cada uno que se rechazó por otra cosa, y el que lo contabilizó.
func (p *ParkedFact) Refused(code, reason string) {
	if p.Waiting() {
		p.s.Attempts++
		p.s.Code, p.s.Reason = clip(code, 100), clip(reason, 500)
	}
}

// MarkPosted anota que un intento posterior lo contabilizó.
func (p *ParkedFact) MarkPosted(now time.Time) {
	if p.Waiting() {
		p.s.Attempts++
		p.s.Status, p.s.ResolvedAt, p.s.Code, p.s.Reason = Posted, now.UTC(), "", ""
	}
}

// Discard renuncia a un hecho que espera: no se contabilizará. Quien lo hace dice por qué.
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

// AuditSnapshot implementa traits.Snapshotter.
func (p *ParkedFact) AuditSnapshot() map[string]any {
	return map[string]any{"eventType": p.s.EventType, "status": string(p.s.Status), "attempts": p.s.Attempts, "code": p.s.Code, "note": p.s.Note}
}

// Campos del hecho aparcado.
var (
	ParFieldCompany  = spec.Comparable("company", func(p *ParkedFact) OrganizationID { return p.s.Company })
	ParFieldStatus   = spec.Comparable("status", func(p *ParkedFact) string { return string(p.s.Status) })
	ParFieldEnvelope = spec.Comparable("envelope_id", func(p *ParkedFact) string { return p.s.EnvelopeID })
	ParFieldSubject  = spec.Comparable("subject_id", func(p *ParkedFact) string { return p.s.Subject })
	ParFieldReceived = spec.OrderedBy("received_at", func(p *ParkedFact) time.Time { return p.s.ReceivedAt }, func(a, b time.Time) int { return a.Compare(b) })
)

// ParkedRepository guarda los hechos aparcados.
type ParkedRepository = fw.Repository[ParkedID, *ParkedFact]
