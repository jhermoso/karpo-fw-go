package application

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// ---------------------------------------------------------------------------------------------
// Eventos de integración (estratégico: comunicación entre contextos delimitados)
// ---------------------------------------------------------------------------------------------
//
// Los eventos de dominio se quedan dentro de su contexto delimitado. Lo que cruza la frontera es un
// evento de integración: un contrato explícito y versionado del contexto proveedor (su Published
// Language), producido traduciendo eventos de dominio, registrado en un outbox de integración en la
// misma unidad de trabajo, retransmitido a un transporte y consumido aguas abajo mediante un inbox
// que descarta los duplicados. Renombrar un campo de un evento de dominio nunca rompe otro contexto.

// IntegrationEvent es un hecho que un contexto delimitado publica para los demás. El nombre del tipo
// forma parte del contrato y lleva su versión, "{context}.{fact}.v{n}" (p. ej. "parties.party-registered.v1").
// Las implementaciones son structs simples con etiquetas JSON, propiedad del contexto que publica.
type IntegrationEvent interface {
	IntegrationEventType() string
}

// Envelope es un evento de integración en el cable (atributos al estilo CloudEvents más los datos JSON).
type Envelope struct {
	ID            string          `json:"id"`      // id único del mensaje: la clave del inbox
	Type          string          `json:"type"`    // IntegrationEventType
	Source        string          `json:"source"`  // contexto delimitado que publica
	Subject       string          `json:"subject"` // id del agregado al que se refiere el hecho (clave de orden)
	OccurredAt    time.Time       `json:"time"`
	CorrelationID string          `json:"correlationId,omitempty"`
	CausationID   string          `json:"causationId,omitempty"` // id del evento de dominio que lo causó
	Data          json.RawMessage `json:"data"`
}

// Translator traduce un evento de dominio de su contexto delimitado a los eventos de integración que
// publica (ninguno cuando el hecho es interno). Es el lado de salida de la capa anticorrupción.
type Translator interface {
	Translate(ctx context.Context, evt domain.Event) ([]IntegrationEvent, error)
}

// TranslatorFunc adapta una función a Translator.
type TranslatorFunc func(ctx context.Context, evt domain.Event) ([]IntegrationEvent, error)

// Translate llama a fn.
func (fn TranslatorFunc) Translate(ctx context.Context, evt domain.Event) ([]IntegrationEvent, error) {
	return fn(ctx, evt)
}

// MessageSender entrega sobres a un transporte (adaptador de un broker de mensajería, broker en
// proceso, webhook HTTP...). Sólo vuelve cuando el transporte los ha aceptado.
type MessageSender interface {
	Send(ctx context.Context, envelopes ...Envelope) error
}

// MessageHandler consume un sobre en un contexto delimitado aguas abajo.
type MessageHandler interface {
	HandleMessage(ctx context.Context, env Envelope) error
}

// MessageHandlerFunc adapta una función a MessageHandler.
type MessageHandlerFunc func(ctx context.Context, env Envelope) error

// HandleMessage llama a fn.
func (fn MessageHandlerFunc) HandleMessage(ctx context.Context, env Envelope) error {
	return fn(ctx, env)
}

// InboxStore recuerda los mensajes que un consumidor ya ha procesado. Claim debe unirse a la unidad
// de trabajo de ctx: la reclamación se confirma con los cambios del consumidor o se deshace con ellos,
// lo que convierte la entrega al menos una vez en un efecto exactamente una vez (implementaciones:
// persistence/memory, persistence/sqlrepo, persistence/hotswap).
type InboxStore interface {
	// Claim registra (consumer, messageID) y dice si es la primera vez.
	Claim(ctx context.Context, consumer, messageID string) (first bool, err error)
}
