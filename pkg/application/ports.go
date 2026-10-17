package application

import (
	"context"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Validatable lo implementan las órdenes/consultas que pueden comprobar su propia entrada.
// Se devuelve un *domain.ValidationError (véase domain.Validation) para dar el detalle por campo.
type Validatable interface {
	Validate() error
}

// ---------------------------------------------------------------------------------------------
// Eventos de dominio
// ---------------------------------------------------------------------------------------------

// WildcardEventType suscribe un manejador a todos los tipos de evento.
const WildcardEventType = "*"

// Publisher entrega un evento de dominio a sus suscriptores.
type Publisher interface {
	Publish(ctx context.Context, evt domain.Event) error
}

// EventHandler reacciona a un evento de dominio.
type EventHandler interface {
	Handle(ctx context.Context, evt domain.Event) error
}

// EventHandlerFunc adapta una función a EventHandler.
type EventHandlerFunc func(ctx context.Context, evt domain.Event) error

// Handle llama a fn.
func (fn EventHandlerFunc) Handle(ctx context.Context, evt domain.Event) error { return fn(ctx, evt) }

// Dispatcher publica eventos y gestiona las suscripciones (implementaciones: events/inprocess,
// adaptadores de brokers de mensajería).
type Dispatcher interface {
	Publisher
	// Subscribe registra un manejador para un tipo de evento (o WildcardEventType) y devuelve una
	// función para darse de baja.
	Subscribe(eventType string, h EventHandler) (unsubscribe func())
}

// EventRecorder registra de forma duradera los eventos de dominio dentro de la unidad de trabajo
// en curso (normalmente el outbox transaccional).
type EventRecorder interface {
	Record(ctx context.Context, events []domain.Event) error
}

// EventDecoder reconstruye los eventos a partir de su forma serializada (implementación: events.Registry).
type EventDecoder interface {
	Decode(eventType string, payload []byte) (domain.Event, error)
}

// ---------------------------------------------------------------------------------------------
// Outbox transaccional
// ---------------------------------------------------------------------------------------------

// OutboxMessage es un evento de dominio serializado que espera a ser retransmitido.
type OutboxMessage struct {
	ID               string
	EventType        string
	AggregateType    string
	AggregateID      string
	AggregateVersion int64
	Payload          []byte
	OccurredAt       time.Time
	CorrelationID    string
	CausationID      string
	Attempts         int
	LastError        string
}

// OutboxStore persiste los mensajes del outbox. Append debe unirse a la unidad de trabajo de ctx
// para que los mensajes se confirmen de forma atómica con el estado del agregado (implementaciones:
// persistence/sqlrepo, persistence/memory, persistence/hotswap).
type OutboxStore interface {
	Append(ctx context.Context, msgs ...OutboxMessage) error
	// Pending devuelve los mensajes sin procesar con menos de maxAttempts intentos, los más antiguos primero.
	Pending(ctx context.Context, limit, maxAttempts int) ([]OutboxMessage, error)
	MarkProcessed(ctx context.Context, id string) error
	MarkFailed(ctx context.Context, id string, cause error) error
}

// ---------------------------------------------------------------------------------------------
// Idempotencia
// ---------------------------------------------------------------------------------------------

// IdempotencyStore recuerda el resultado de las peticiones ya procesadas
// (equivale a IIdempotencyStore de C#).
type IdempotencyStore interface {
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	Put(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

// IdempotencyKeyed lo implementan las órdenes que llevan una clave de idempotencia aportada por el
// cliente (equivale a IIdempotentCommand de C#).
type IdempotencyKeyed interface {
	IdempotencyKey() string
}

// ---------------------------------------------------------------------------------------------
// Log de auditoría
// ---------------------------------------------------------------------------------------------

// Operaciones de auditoría.
const (
	AuditCreated = "created"
	AuditUpdated = "updated"
	AuditDeleted = "deleted"
)

// AuditRecord es una entrada del log de auditoría: quién cambió qué agregado, cómo, cuándo, por qué
// canal o ejecución de importación, qué campos cambiaron y qué eventos lanzó.
type AuditRecord struct {
	ID               string               `json:"id"`
	AggregateType    string               `json:"aggregateType"`
	AggregateID      string               `json:"aggregateId"`
	AggregateVersion int64                `json:"aggregateVersion"`
	Operation        string               `json:"operation"`
	Actor            vocab.Actor          `json:"actor"`
	Channel          string               `json:"channel,omitempty"`
	Import           *ImportProvenance    `json:"import,omitempty"`
	CorrelationID    string               `json:"correlationId,omitempty"`
	At               time.Time            `json:"at"`
	Changes          []traits.FieldChange `json:"changes,omitempty"`
	Events           []string             `json:"events,omitempty"`
}

// AuditLog guarda los registros de auditoría. Append debe unirse a la unidad de trabajo de ctx,
// para que el registro se confirme de forma atómica con el cambio de estado (implementaciones:
// persistence/sqlrepo, persistence/memory).
type AuditLog interface {
	Append(ctx context.Context, records ...AuditRecord) error
	// Trail devuelve los registros de un agregado, los más antiguos primero.
	Trail(ctx context.Context, aggregateType, aggregateID string) ([]AuditRecord, error)
}
