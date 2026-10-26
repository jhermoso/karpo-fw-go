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

// OversizedCode es por qué se guarda descartado un hecho cuyo sobre no cabe: un tipo vacío o de más
// de 100 bytes, o una identidad vacía o de más de 64. Guardarlo como aparcado haría fallar la
// validación cada vez y el emisor lo enviaría una y otra vez; recortarlo cambiaría el hecho. Así se
// guarda una vez, no se intenta y quien lleva la contabilidad lo ve en la lista.
const OversizedCode = "accounting.oversized"

// MaxParkedWait es el tope de la espera entre dos intentos de un hecho aparcado.
const MaxParkedWait = time.Hour

// ParkedBackoff es cuánto espera un hecho tras n intentos que no lo contabilizaron: un minuto, dos,
// cuatro... hasta MaxParkedWait. No hay tope de intentos: un hecho aparcado se contabiliza solo en
// cuanto la empresa abre su libro o completa su perfil, y nadie recibe aviso de lo que espera.
func ParkedBackoff(n int) time.Duration {
	if n > 6 {
		return MaxParkedWait
	}
	return time.Minute << (max(n, 1) - 1)
}

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
	Attempts      int       // los intentos: el primero, cada reintento que no lo contabilizó y el que lo contabilizó
	NextAttempt   time.Time // cuándo le toca otro intento al planificador; cero cuando ya no espera (o cuando toca ya)
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

// Park guarda un hecho que no pudo contabilizarse, con la regla que lo rechazó. El que espera detrás
// de otro (WaitingCode) puede intentarse en cuanto pase el de delante; el rechazado, tras la primera
// espera. Un hecho cuyo tipo o identidad no caben se guarda descartado (OversizedCode).
func Park(id ParkedID, s ParkedState, code, reason string, now time.Time) (*ParkedFact, error) {
	s.Code, s.Reason, s.ReceivedAt, s.Attempts, s.Status = clip(code, 100), clip(reason, 500), now.UTC(), 1, Parked
	s.NextAttempt, s.ResolvedAt, s.Note = now.UTC().Add(ParkedBackoff(1)), time.Time{}, ""
	if s.Code == WaitingCode {
		s.NextAttempt = now.UTC()
	}
	s.Source, s.Subject, s.CorrelationID, s.CausationID = clip(s.Source, 50), clip(s.Subject, 100), clip(s.CorrelationID, 100), clip(s.CausationID, 100)
	if s.EventType == "" || len(s.EventType) > 100 || s.EnvelopeID == "" || len(s.EnvelopeID) > fw.MaxEnvelopeKey {
		note := "the fact does not fit: type " + s.EventType + " (" + strconv.Itoa(len(s.EventType)) + " bytes), identity " + s.EnvelopeID +
			" (" + strconv.Itoa(len(s.EnvelopeID)) + " bytes)"
		s.EventType, s.EnvelopeID = fw.ClipBytes(s.EventType, 100), fw.EnvelopeKey(s.EnvelopeID)
		if s.EventType == "" {
			s.EventType = "-"
		}
		s.Status, s.Code, s.NextAttempt, s.ResolvedAt, s.Note = Discarded, OversizedCode, time.Time{}, now.UTC(), clip(note, 500)
	}
	return ReconstituteParked(id, s)
}

// State devuelve el estado.
func (p *ParkedFact) State() ParkedState { return p.s }

// Waiting dice si el hecho todavía espera.
func (p *ParkedFact) Waiting() bool { return p.s.Status == Parked }

// Due dice si al planificador le toca intentarlo: espera y ya pasó su espera.
func (p *ParkedFact) Due(now time.Time) bool { return p.Waiting() && !p.s.NextAttempt.After(now) }

// Refused anota un intento que no lo contabilizó, y por qué esta vez, y fija cuándo se intenta otra
// vez: cada rechazo alarga la espera (ParkedBackoff), así que un hecho que tarda días en poder
// contabilizarse se escribe como mucho una vez por hora, y no en cada vuelta del planificador.
func (p *ParkedFact) Refused(code, reason string, now time.Time) {
	if p.Waiting() {
		p.s.Attempts++
		p.s.Code, p.s.Reason = clip(code, 100), clip(reason, 500)
		p.s.NextAttempt = now.UTC().Add(ParkedBackoff(p.s.Attempts))
	}
}

// MarkPosted anota que un intento posterior lo contabilizó.
func (p *ParkedFact) MarkPosted(now time.Time) {
	if p.Waiting() {
		p.s.Attempts++
		p.s.Status, p.s.ResolvedAt, p.s.NextAttempt, p.s.Code, p.s.Reason = Posted, now.UTC(), time.Time{}, "", ""
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
	p.s.Status, p.s.ResolvedAt, p.s.NextAttempt, p.s.Note = Discarded, now.UTC(), time.Time{}, clip(note, 500)
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
