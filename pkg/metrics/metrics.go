// Package metrics es el contrato de métricas del framework: números que se acumulan (cuántas
// peticiones, cuánto tardan) y se leen en agregado.
//
// El contrato sólo depende de la biblioteca estándar. Las implementaciones están en subpaquetes
// (metrics/vanilla no tiene dependencias y expone el formato de texto de Prometheus). Cada pieza
// instrumentada del framework recibe un Meter como opción y, si no lo tiene, usa Noop.
//
// Las etiquetas se pasan como pares clave/valor ("method", "GET", "status", "200") y deben salir
// de un conjunto pequeño y cerrado: método, patrón de ruta, código de estado, tipo de petición,
// resultado, motor, operación, tipo de evento, consumidor. Nunca identificadores, organizaciones
// ni correlaciones: cada valor distinto es una serie nueva.
package metrics

import "context"

// Unidades que usan las métricas del framework.
const (
	UnitSeconds = "s"
	UnitNone    = ""
)

// Nombres de las métricas que emite el framework. C# (Paranoia.Karpo.Fw) usa los mismos nombres.
const (
	HTTPServerRequestDuration = "http.server.request.duration"    // histograma (s): method, route, status
	UseCaseDuration           = "karpo.usecase.duration"          // histograma (s): request, outcome
	DBOperationDuration       = "db.client.operation.duration"    // histograma (s): engine, operation, outcome
	DBConnections             = "db.client.connections"           // gauge: engine, state
	OutboxRelayed             = "karpo.outbox.relayed"            // contador: event_type, outcome
	OutboxOldestPendingAge    = "karpo.outbox.oldest_pending_age" // gauge (s): outbox
	MessagingHandled          = "karpo.messaging.handled"         // contador: consumer, event_type, outcome
	MessagingLag              = "karpo.messaging.lag"             // histograma (s): consumer, event_type
)

// Claves de etiqueta.
const (
	LabelMethod    = "method"
	LabelRoute     = "route"
	LabelStatus    = "status"
	LabelRequest   = "request"
	LabelOutcome   = "outcome"
	LabelEngine    = "engine"
	LabelOperation = "operation"
	LabelState     = "state"
	LabelEventType = "event_type"
	LabelConsumer  = "consumer"
	LabelOutbox    = "outbox"
)

// Meter crea instrumentos. Pedir dos veces el mismo nombre devuelve el mismo instrumento.
type Meter interface {
	// Counter devuelve una suma que sólo crece.
	Counter(name, unit, help string) Counter
	// Histogram devuelve una distribución de valores observados. Sin buckets, la implementación
	// usa los suyos por defecto (duraciones en segundos).
	Histogram(name, unit, help string, buckets ...float64) Histogram
	// Gauge registra un valor que se lee cuando se exportan las métricas. labels son los pares
	// clave/valor constantes de esa serie. Registrar otra vez el mismo nombre y las mismas
	// etiquetas sustituye al lector. La función devuelta vuelve a quitar la serie (p. ej. cuando
	// se cierra el pool que lee); no hace nada si un registro posterior ya sustituyó al lector.
	Gauge(name, unit, help string, read func() float64, labels ...string) (unregister func())
}

// Counter acumula una suma.
type Counter interface {
	Add(ctx context.Context, n float64, labels ...string)
}

// Histogram registra observaciones.
type Histogram interface {
	Record(ctx context.Context, v float64, labels ...string)
}

// Noop devuelve un Meter que no registra nada.
func Noop() Meter { return noop{} }

// OrNoop devuelve m, o Noop cuando m es nil.
func OrNoop(m Meter) Meter {
	if m == nil {
		return noop{}
	}
	return m
}

type noop struct{}

func (noop) Counter(string, string, string) Counter                         { return noop{} }
func (noop) Histogram(string, string, string, ...float64) Histogram         { return noop{} }
func (noop) Gauge(string, string, string, func() float64, ...string) func() { return func() {} }
func (noop) Add(context.Context, float64, ...string)                        {}
func (noop) Record(context.Context, float64, ...string)                     {}
