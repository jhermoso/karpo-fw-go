package observability

import (
	"context"
	"encoding/hex"
	"strings"
)

// TraceID is a W3C Trace Context trace id (16 bytes).
type TraceID [16]byte

// SpanID is a W3C Trace Context span id (8 bytes).
type SpanID [8]byte

// IsValid reports whether the id is not all zeros.
func (t TraceID) IsValid() bool { return t != TraceID{} }

// String returns the id as 32 lower-case hex digits.
func (t TraceID) String() string { return hex.EncodeToString(t[:]) }

// IsValid reports whether the id is not all zeros.
func (s SpanID) IsValid() bool { return s != SpanID{} }

// String returns the id as 16 lower-case hex digits.
func (s SpanID) String() string { return hex.EncodeToString(s[:]) }

// SpanContext identifies a span across process boundaries.
type SpanContext struct {
	TraceID TraceID
	SpanID  SpanID
	Sampled bool
	Remote  bool // received from another process (traceparent), not created here
}

// IsValid reports whether both ids are set.
func (sc SpanContext) IsValid() bool { return sc.TraceID.IsValid() && sc.SpanID.IsValid() }

// TraceparentHeader is the W3C Trace Context header.
const TraceparentHeader = "traceparent"

// Traceparent formats sc as a W3C traceparent value (version 00), or "" when sc is not valid.
func (sc SpanContext) Traceparent() string {
	if !sc.IsValid() {
		return ""
	}
	flags := "00"
	if sc.Sampled {
		flags = "01"
	}
	return "00-" + sc.TraceID.String() + "-" + sc.SpanID.String() + "-" + flags
}

// ParseTraceparent parses a W3C traceparent value. It accepts only the exact 55-character form
// with lower-case hex, rejects the all-zero ids and the forbidden version ff, and returns ok=false
// for anything else: a malformed header from a client is ignored, never trusted in part.
func ParseTraceparent(v string) (SpanContext, bool) {
	if len(v) != 55 || v[2] != '-' || v[35] != '-' || v[52] != '-' {
		return SpanContext{}, false
	}
	if !isLowerHex(v[0:2]) || v[0:2] == "ff" || !isLowerHex(v[3:35]) || !isLowerHex(v[36:52]) || !isLowerHex(v[53:55]) {
		return SpanContext{}, false
	}
	var sc SpanContext
	_, _ = hex.Decode(sc.TraceID[:], []byte(v[3:35]))
	_, _ = hex.Decode(sc.SpanID[:], []byte(v[36:52]))
	if !sc.IsValid() {
		return SpanContext{}, false
	}
	var flags [1]byte
	_, _ = hex.Decode(flags[:], []byte(v[53:55]))
	sc.Sampled = flags[0]&1 == 1
	sc.Remote = true
	return sc, true
}

func isLowerHex(s string) bool {
	return strings.Trim(s, "0123456789abcdef") == ""
}

// SpanKind is the role of a span in a trace.
type SpanKind int

const (
	SpanKindInternal SpanKind = iota
	SpanKindServer
	SpanKindClient
	SpanKindProducer
	SpanKindConsumer
)

// StatusCode is the outcome of a span.
type StatusCode int

const (
	StatusUnset StatusCode = iota
	StatusOK
	StatusError
)

// Span is one timed operation of a trace.
type Span interface {
	// Context returns the identity of the span (invalid for the no-op span).
	Context() SpanContext
	// SetName renames the span (e.g. a server span once the route is known).
	SetName(name string)
	SetAttributes(attrs ...Attr)
	// RecordError attaches err to the span; it does not change the status.
	RecordError(err error)
	SetStatus(code StatusCode, description string)
	// End finishes the span; calls after the first are ignored.
	End()
}

// SpanConfig is the configuration a Tracer receives from the SpanOptions.
type SpanConfig struct {
	Kind  SpanKind
	Attrs []Attr
}

// SpanOption configures a span at start.
type SpanOption func(*SpanConfig)

// WithKind sets the span kind.
func WithKind(k SpanKind) SpanOption { return func(c *SpanConfig) { c.Kind = k } }

// WithAttributes sets attributes at start.
func WithAttributes(attrs ...Attr) SpanOption {
	return func(c *SpanConfig) { c.Attrs = append(c.Attrs, attrs...) }
}

// NewSpanConfig applies opts (for Tracer implementations).
func NewSpanConfig(opts ...SpanOption) SpanConfig {
	var c SpanConfig
	for _, o := range opts {
		o(&c)
	}
	return c
}

// Tracer starts spans. The parent is the span in ctx or, failing that, the remote span context
// stored with ContextWithRemoteSpanContext.
type Tracer interface {
	Start(ctx context.Context, name string, opts ...SpanOption) (context.Context, Span)
}

// NoopTracer returns a tracer that creates no spans: it returns ctx unchanged and a span that
// ignores every call.
func NoopTracer() Tracer { return noopTracer{} }

type noopTracer struct{}

func (noopTracer) Start(ctx context.Context, _ string, _ ...SpanOption) (context.Context, Span) {
	return ctx, noopSpan{}
}

type noopSpan struct{}

func (noopSpan) Context() SpanContext         { return SpanContext{} }
func (noopSpan) SetName(string)               {}
func (noopSpan) SetAttributes(...Attr)        {}
func (noopSpan) RecordError(error)            {}
func (noopSpan) SetStatus(StatusCode, string) {}
func (noopSpan) End()                         {}

type ctxKey int

const (
	spanKey ctxKey = iota
	remoteKey
)

// ContextWithSpan returns ctx carrying span as the current span.
func ContextWithSpan(ctx context.Context, span Span) context.Context {
	return context.WithValue(ctx, spanKey, span)
}

// SpanFromContext returns the current span, or a no-op span when there is none.
func SpanFromContext(ctx context.Context) Span {
	if s, ok := ctx.Value(spanKey).(Span); ok && s != nil {
		return s
	}
	return noopSpan{}
}

// ContextWithRemoteSpanContext stores a span context received from another process (e.g. the
// traceparent of an HTTP request) so that the next span started on ctx continues that trace.
func ContextWithRemoteSpanContext(ctx context.Context, sc SpanContext) context.Context {
	if !sc.IsValid() {
		return ctx
	}
	sc.Remote = true
	return context.WithValue(ctx, remoteKey, sc)
}

// ParentFromContext returns the span context a new span on ctx must use as parent: the current
// span's, or the remote one. ok is false when there is none (the new span starts a trace).
func ParentFromContext(ctx context.Context) (SpanContext, bool) {
	if s, ok := ctx.Value(spanKey).(Span); ok && s != nil {
		if sc := s.Context(); sc.IsValid() {
			return sc, true
		}
	}
	if sc, ok := ctx.Value(remoteKey).(SpanContext); ok && sc.IsValid() {
		return sc, true
	}
	return SpanContext{}, false
}

// TraceIDs returns the trace and span ids of the current span as hex strings, or "" when there
// is no valid span. Loggers use it to stamp trace_id and span_id.
func TraceIDs(ctx context.Context) (traceID, spanID string) {
	if s, ok := ctx.Value(spanKey).(Span); ok && s != nil {
		if sc := s.Context(); sc.IsValid() {
			return sc.TraceID.String(), sc.SpanID.String()
		}
	}
	return "", ""
}
