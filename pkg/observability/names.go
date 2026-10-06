package observability

// Names shared with the C# framework (Paranoia.Karpo.Fw.Infrastructure.Observability). A log
// line, a span or a metric means the same in both languages only if these strings match: change
// them in both places or in neither. HTTP names follow the OpenTelemetry semantic conventions,
// which is also what ASP.NET Core emits natively; framework names carry the karpo. prefix.

// Log fields.
const (
	FieldCorrelationID = "correlation_id"
	FieldCausationID   = "causation_id"
	FieldTraceID       = "trace_id"
	FieldSpanID        = "span_id"
	FieldError         = "error"
	FieldErrorType     = "error.type"
	FieldDurationMs    = "duration_ms"
	FieldService       = "service.name"
)

// Attributes (span attributes, metric labels and log fields).
const (
	AttrHTTPMethod     = "http.request.method"
	AttrHTTPRoute      = "http.route"
	AttrHTTPStatusCode = "http.response.status_code"
	AttrUseCase        = "karpo.use_case"
	AttrOutcome        = "karpo.outcome"
	AttrEventType      = "karpo.event_type"
)

// Outcome values of AttrOutcome.
const (
	OutcomeOK    = "ok"
	OutcomeError = "error"
)

// RouteUnknown is the http.route value when the request matched no route pattern. The raw path
// is never used: it would make the metric cardinality unbounded and may carry personal data.
const RouteUnknown = "unknown"

// Metric names.
const (
	// MetricHTTPServerDuration is the duration of inbound HTTP requests, in seconds, labelled by
	// method, route and status code (its count is the request count).
	MetricHTTPServerDuration = "http.server.request.duration"
	// MetricUseCaseDuration is the duration of application use cases, in seconds, labelled by
	// use case and outcome.
	MetricUseCaseDuration = "karpo.use_case.duration"
	// MetricOutboxDelivered counts outbox messages delivered by a relay, by event type.
	MetricOutboxDelivered = "karpo.outbox.delivered"
	// MetricOutboxFailed counts failed outbox deliveries (each attempt), by event type.
	MetricOutboxFailed = "karpo.outbox.failed"
)

// DurationBuckets are the histogram bounds, in seconds, the OpenTelemetry semantic conventions
// recommend for http.server.request.duration (the same ASP.NET Core uses).
var DurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}
