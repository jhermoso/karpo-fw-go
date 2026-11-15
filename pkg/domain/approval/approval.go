// Package approval es el motor de aprobaciones del framework (D-156): una acción configurada no se
// ejecuta en el acto, sino que deja una solicitud pendiente que otra persona aprueba o rechaza.
//
// Es la traducción, limitada a aprobaciones, del motor de flujos de trabajo de Karpo C#
// (Paranoia.Karpo.Fw.Workflow): una solicitud es una instancia de un flujo de un solo paso humano,
// con cuatro estados fijos. Véase docs/APROBACIONES.md.
package approval

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// AggregateType es el tipo estable del agregado.
const AggregateType = "approval.request"

// RequestID identifica una solicitud de aprobación.
type RequestID struct{ domain.UUID }

// NewRequestID devuelve una identidad nueva.
func NewRequestID() RequestID { return RequestID{domain.NewUUID()} }

// Status es el estado de una solicitud.
type Status string

// Estados. StatusPending es el único no final.
const (
	StatusPending   Status = "pending"
	StatusApproved  Status = "approved"
	StatusRejected  Status = "rejected"
	StatusCancelled Status = "cancelled"
)

// Final dice si el estado ya no admite cambios.
func (s Status) Final() bool {
	return s == StatusApproved || s == StatusRejected || s == StatusCancelled
}

// Action es el nombre estable de la acción que se pide, en tres segmentos en minúsculas
// "{contexto}.{recurso}.{verbo}" (p. ej. "purchases.supplier.block"). Se persiste: no se renombra.
type Action string

// ParseAction valida un nombre de acción: tres segmentos de [a-z][a-z0-9_]*.
func ParseAction(s string) (Action, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("%w: action %q must be {context}.{resource}.{verb}", domain.ErrValidation, s)
	}
	for _, p := range parts {
		if !validSegment(p) {
			return "", fmt.Errorf("%w: action %q has an invalid segment %q", domain.ErrValidation, s, p)
		}
	}
	return Action(s), nil
}

func validSegment(p string) bool {
	if p == "" || p[0] < 'a' || p[0] > 'z' {
		return false
	}
	for _, c := range p {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

// Segments devuelve el contexto, el recurso y el verbo.
func (a Action) Segments() (context, resource, verb string) {
	parts := strings.SplitN(string(a), ".", 3)
	if len(parts) != 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}

// Target es aquello sobre lo que actúa la acción (p. ej. Type "purchases.supplier" e ID el de la party).
type Target struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Códigos de las reglas que se incumplen.
const (
	CodeAlreadyResolved = "approval.already_resolved"
	CodeSelfResolution  = "approval.self_resolution"
	CodeCommentRequired = "approval.comment_required"
	CodeNotRequester    = "approval.not_requester"
)

// Request es una solicitud de aprobación: quién pide qué acción, sobre qué y con qué datos, y cómo
// se resolvió. Reglas: sólo se resuelve una vez; quien la pidió no la aprueba ni la rechaza (sí
// puede cancelarla, y sólo él); rechazar exige un comentario.
type Request struct {
	domain.BaseAggregateRoot[RequestID]
	s State
}

// State es la foto de una solicitud, para reconstituirla y para persistirla.
type State struct {
	Company     domain.UUID     // empresa en la que se pide
	Action      Action          // acción pedida
	Target      Target          // objetivo
	Payload     json.RawMessage // lo pedido, en JSON; la acción lo interpreta al aplicarse
	Reason      string          // motivo del solicitante
	RequestedBy domain.UUID     // party del solicitante
	RequestedAt time.Time
	Status      Status
	ResolvedBy  domain.UUID // party de quien resolvió (o canceló)
	ResolvedAt  time.Time
	Comment     string // comentario de la resolución
	// Group agrupa las acciones que no pueden estar pendientes a la vez sobre el mismo objetivo
	// (p. ej. GroupBlock para bloquear y desbloquear). Vacío vale la propia acción: una pendiente
	// por acción y objetivo. Sólo cuenta mientras la solicitud está pendiente.
	Group string
}

// GroupBlock es el grupo de bloquear y desbloquear: no conviven dos pendientes sobre el mismo
// objetivo, sea cual sea el sentido.
const GroupBlock = "block"

// ErrPending es el conflicto de pedir una acción cuando ya hay una solicitud pendiente del mismo
// grupo sobre el mismo objetivo. Lo devuelven Service.Request y el índice único de pending_key
// (pkg/persistence/approvals); cada contexto lo traduce a su código con errors.Is.
var ErrPending = fmt.Errorf("%w: approval.pending: there is already a pending request for this target", domain.ErrConflict)

// Option ajusta una solicitud nueva.
type Option func(*State)

// InGroup pone la solicitud en el grupo g ([a-z][a-z0-9_]*; véase State.Group).
func InGroup(g string) Option { return func(s *State) { s.Group = g } }

// PendingKeyFor es la clave de unicidad de una solicitud pendiente: "grupo:tipo:id" del objetivo.
func PendingKeyFor(group string, t Target) string { return group + ":" + t.Type + ":" + t.ID }

// PendingKey es la clave de unicidad de la solicitud: PendingKeyFor mientras está pendiente y su
// propio id después, que no choca con nada (nunca vacía, para que un índice único normal valga
// también en SQL Server).
func (r *Request) PendingKey() string {
	if r.s.Status != StatusPending {
		return r.ID().String()
	}
	return PendingKeyFor(r.Group(), r.s.Target)
}

// Group devuelve el grupo de la solicitud (la acción si no se dio otro).
func (r *Request) Group() string {
	if r.s.Group == "" {
		return string(r.s.Action)
	}
	return r.s.Group
}

// New crea una solicitud pendiente y lanza Requested. opts admite InGroup.
func New(id RequestID, company domain.UUID, action Action, target Target, payload json.RawMessage, reason string, by domain.UUID, opts ...Option) (*Request, error) {
	var o State
	for _, opt := range opts {
		opt(&o)
	}
	var v domain.Validation
	v.Require(o.Group == "" || validSegment(o.Group), "group", "invalid", "group must match [a-z][a-z0-9_]*")
	v.Require(!strings.Contains(target.Type, ":"), "target", "invalid", "target type cannot contain ':'")
	v.Require(!company.IsZero(), "company", "required", "company is required")
	if _, err := ParseAction(string(action)); err != nil {
		v.Add("action", "invalid", err.Error())
	}
	v.Require(strings.TrimSpace(target.Type) != "" && strings.TrimSpace(target.ID) != "", "target", "required", "target type and id are required")
	v.Require(len(payload) == 0 || json.Valid(payload), "payload", "invalid", "payload must be valid JSON")
	v.Require(!by.IsZero(), "requestedBy", "required", "requester is required")
	if err := v.Err(); err != nil {
		return nil, err
	}
	base, err := domain.NewBaseAggregateRoot(AggregateType, id)
	if err != nil {
		return nil, err
	}
	r := &Request{BaseAggregateRoot: base, s: State{
		Company: company, Action: action, Target: target, Payload: append(json.RawMessage(nil), payload...),
		Reason: strings.TrimSpace(reason), RequestedBy: by, RequestedAt: domain.Now(), Status: StatusPending, Group: o.Group,
	}}
	r.Raise(Requested{EventMeta: r.NewEventMeta(), Company: company, Action: action, Target: target,
		Payload: r.s.Payload, Reason: r.s.Reason, RequestedBy: by})
	return r, nil
}

// Reconstitute rehace una solicitud persistida sin lanzar eventos (la versión la fija el repositorio).
func Reconstitute(id RequestID, s State) (*Request, error) {
	base, err := domain.NewBaseAggregateRoot(AggregateType, id)
	if err != nil {
		return nil, err
	}
	return &Request{BaseAggregateRoot: base, s: s}, nil
}

// State devuelve una copia de la foto de la solicitud.
func (r *Request) State() State {
	s := r.s
	s.Payload = append(json.RawMessage(nil), r.s.Payload...)
	return s
}

// Status devuelve el estado.
func (r *Request) Status() Status { return r.s.Status }

// Action devuelve la acción pedida.
func (r *Request) Action() Action { return r.s.Action }

// Company devuelve la empresa.
func (r *Request) Company() domain.UUID { return r.s.Company }

// RequestedBy devuelve el solicitante.
func (r *Request) RequestedBy() domain.UUID { return r.s.RequestedBy }

func (r *Request) resolve(by domain.UUID, comment string, to Status) error {
	if r.s.Status.Final() {
		return domain.Violation(CodeAlreadyResolved, fmt.Sprintf("approval request is already %s", r.s.Status))
	}
	if by.IsZero() {
		return &domain.ValidationError{Errors: []domain.FieldError{{Field: "by", Code: "required", Message: "resolver is required"}}}
	}
	r.s.Status, r.s.ResolvedBy, r.s.ResolvedAt, r.s.Comment = to, by, domain.Now(), strings.TrimSpace(comment)
	return nil
}

// Approve aprueba la solicitud. Quien la pidió no puede aprobarla.
func (r *Request) Approve(by domain.UUID, comment string) error {
	if by == r.s.RequestedBy && !r.s.Status.Final() {
		return domain.Violation(CodeSelfResolution, "the requester cannot approve their own request")
	}
	if err := r.resolve(by, comment, StatusApproved); err != nil {
		return err
	}
	r.Raise(Approved{EventMeta: r.NewEventMeta(), Resolution: r.resolution()})
	return nil
}

// Reject rechaza la solicitud con un comentario obligatorio. Quien la pidió no puede rechazarla.
func (r *Request) Reject(by domain.UUID, comment string) error {
	if !r.s.Status.Final() {
		if by == r.s.RequestedBy {
			return domain.Violation(CodeSelfResolution, "the requester cannot reject their own request")
		}
		if strings.TrimSpace(comment) == "" {
			return domain.Violation(CodeCommentRequired, "a rejection needs a comment")
		}
	}
	if err := r.resolve(by, comment, StatusRejected); err != nil {
		return err
	}
	r.Raise(Rejected{EventMeta: r.NewEventMeta(), Resolution: r.resolution()})
	return nil
}

// Cancel retira la solicitud. Sólo la cancela quien la pidió.
func (r *Request) Cancel(by domain.UUID, comment string) error {
	if !r.s.Status.Final() && by != r.s.RequestedBy {
		return domain.Violation(CodeNotRequester, "only the requester can cancel the request")
	}
	if err := r.resolve(by, comment, StatusCancelled); err != nil {
		return err
	}
	r.Raise(Cancelled{EventMeta: r.NewEventMeta(), Resolution: r.resolution()})
	return nil
}

func (r *Request) resolution() Resolution {
	return Resolution{Company: r.s.Company, Action: r.s.Action, Target: r.s.Target, Payload: r.s.Payload,
		RequestedBy: r.s.RequestedBy, ResolvedBy: r.s.ResolvedBy, Comment: r.s.Comment}
}
