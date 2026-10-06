// Package metrics is the metrics contract of the framework: numbers that accumulate (how many
// requests, how long they take) and are read in aggregate.
//
// The contract depends on the standard library only. Implementations live in subpackages
// (metrics/vanilla has no dependencies and exposes the Prometheus text format). Every
// instrumented piece of the framework takes a Meter as an option and falls back to Noop.
//
// Labels are passed as key/value pairs ("method", "GET", "status", "200") and must come from a
// small closed set: method, route pattern, status code, request type, outcome, engine, operation,
// event type, consumer. Never identifiers, organizations or correlations: every distinct value
// is a new series.
package metrics

import "context"

// Units used by the framework metrics.
const (
	UnitSeconds = "s"
	UnitNone    = ""
)

// Names of the metrics the framework emits. C# (Paranoia.Karpo.Fw) uses the same names.
const (
	HTTPServerRequestDuration = "http.server.request.duration"    // histogram (s): method, route, status
	UseCaseDuration           = "karpo.usecase.duration"          // histogram (s): request, outcome
	DBOperationDuration       = "db.client.operation.duration"    // histogram (s): engine, operation, outcome
	DBConnections             = "db.client.connections"           // gauge: engine, state
	OutboxRelayed             = "karpo.outbox.relayed"            // counter: event_type, outcome
	OutboxOldestPendingAge    = "karpo.outbox.oldest_pending_age" // gauge (s): outbox
	MessagingHandled          = "karpo.messaging.handled"         // counter: consumer, event_type, outcome
	MessagingLag              = "karpo.messaging.lag"             // histogram (s): consumer, event_type
)

// Label keys.
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

// Meter creates instruments. Asking twice for the same name returns the same instrument.
type Meter interface {
	// Counter returns a monotonically increasing sum.
	Counter(name, unit, help string) Counter
	// Histogram returns a distribution of observed values. Without buckets the implementation
	// uses its defaults (durations in seconds).
	Histogram(name, unit, help string, buckets ...float64) Histogram
	// Gauge registers a value that is read when the metrics are exported. labels are the
	// constant key/value pairs of that series.
	Gauge(name, unit, help string, read func() float64, labels ...string)
}

// Counter accumulates a sum.
type Counter interface {
	Add(ctx context.Context, n float64, labels ...string)
}

// Histogram records observations.
type Histogram interface {
	Record(ctx context.Context, v float64, labels ...string)
}

// Noop returns a Meter that records nothing.
func Noop() Meter { return noop{} }

// OrNoop returns m, or Noop when m is nil.
func OrNoop(m Meter) Meter {
	if m == nil {
		return noop{}
	}
	return m
}

type noop struct{}

func (noop) Counter(string, string, string) Counter                  { return noop{} }
func (noop) Histogram(string, string, string, ...float64) Histogram  { return noop{} }
func (noop) Gauge(string, string, string, func() float64, ...string) {}
func (noop) Add(context.Context, float64, ...string)                 {}
func (noop) Record(context.Context, float64, ...string)              {}
