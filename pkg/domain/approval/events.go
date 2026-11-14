package approval

import (
	"encoding/json"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Nombres estables de los eventos. El contexto dueño de la acción se suscribe a Approved y la
// aplica (paso 3 de D-156); los demás sirven para avisar y para el historial.
const (
	EventRequested = "approval.requested"
	EventApproved  = "approval.approved"
	EventRejected  = "approval.rejected"
	EventCancelled = "approval.cancelled"
)

// Requested: se ha pedido una acción que exige aprobación.
type Requested struct {
	domain.EventMeta
	Company     domain.UUID     `json:"company"`
	Action      Action          `json:"action"`
	Target      Target          `json:"target"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Reason      string          `json:"reason,omitempty"`
	RequestedBy domain.UUID     `json:"requestedBy"`
}

// EventType devuelve el nombre estable.
func (Requested) EventType() string { return EventRequested }

// Resolution es la carga común de los eventos de resolución: lleva la acción, el objetivo y lo
// pedido para que el suscriptor pueda aplicarla sin volver a leer la solicitud.
type Resolution struct {
	Company     domain.UUID     `json:"company"`
	Action      Action          `json:"action"`
	Target      Target          `json:"target"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	RequestedBy domain.UUID     `json:"requestedBy"`
	ResolvedBy  domain.UUID     `json:"resolvedBy"`
	Comment     string          `json:"comment,omitempty"`
}

// Approved: la solicitud se ha aprobado; el contexto dueño de la acción debe aplicarla.
type Approved struct {
	domain.EventMeta
	Resolution
}

// EventType devuelve el nombre estable.
func (Approved) EventType() string { return EventApproved }

// Rejected: la solicitud se ha rechazado con un comentario.
type Rejected struct {
	domain.EventMeta
	Resolution
}

// EventType devuelve el nombre estable.
func (Rejected) EventType() string { return EventRejected }

// Cancelled: el solicitante ha retirado la solicitud.
type Cancelled struct {
	domain.EventMeta
	Resolution
}

// EventType devuelve el nombre estable.
func (Cancelled) EventType() string { return EventCancelled }
