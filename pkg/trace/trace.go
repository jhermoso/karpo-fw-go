// Package trace is the tracing contract of the framework: a Tracer starts spans, a Span is one
// leg of a request (HTTP -> use case -> SQL) and a SpanContext identifies it across processes
// with the W3C traceparent header.
//
// The contract depends on the standard library only. Implementations live in subpackages
// (trace/vanilla has no dependencies; an OpenTelemetry adapter belongs to a separate module).
// Every instrumented piece of the framework takes a Tracer as an option and falls back to Noop,
// so a service that configures nothing behaves exactly as before.
package trace

import (
	"context"
	"encoding/hex"
	"strings"
)

// TraceParentHeader is the W3C Trace Context header.
const TraceParentHeader = "traceparent"

// Log attribute keys of the current span (see LogAttrs).
const (
	TraceIDKey = "trace_id"
	SpanIDKey  = "span_id"
)

// SpanContext identifies a span: the trace it belongs to, its own id and whether it is recorded.
type SpanContext struct {
	TraceID [16]byte
	SpanID  [8]byte
	Sampled bool
}

// IsValid reports whether both ids are set (W3C: all zeros is invalid).
func (sc SpanContext) IsValid() bool {
	return sc.TraceID != [16]byte{} && sc.SpanID != [8]byte{}
}

// TraceIDString returns the trace id as 32 lowercase hex characters.
func (sc SpanContext) TraceIDString() string { return hex.EncodeToString(sc.TraceID[:]) }

// SpanIDString returns the span id as 16 lowercase hex characters.
func (sc SpanContext) SpanIDString() string { return hex.EncodeToString(sc.SpanID[:]) }

// TraceParent renders the W3C traceparent header value, or "" for an invalid context.
func (sc SpanContext) TraceParent() string {
	if !sc.IsValid() {
		return ""
	}
	flags := "00"
	if sc.Sampled {
		flags = "01"
	}
	return "00-" + sc.TraceIDString() + "-" + sc.SpanIDString() + "-" + flags
}

// ParseTraceParent parses a W3C traceparent header: version-traceid-parentid-flags, lowercase
// hex. Unknown versions are accepted as long as the four known fields are well formed (the
// specification asks for forward compatibility); version ff, uppercase hex and all-zero ids are
// rejected.
func ParseTraceParent(header string) (SpanContext, bool) {
	var sc SpanContext
	h := strings.TrimSpace(header)
	if len(h) < 55 || h[2] != '-' || h[35] != '-' || h[52] != '-' {
		return sc, false
	}
	version, ok := lowerHex(h[0:2])
	if !ok || h[0:2] == "ff" {
		return sc, false
	}
	// Version 00 has exactly four fields; later versions may append more after a dash.
	if len(h) > 55 && (version[0] == 0 || h[55] != '-') {
		return sc, false
	}
	traceID, ok1 := lowerHex(h[3:35])
	spanID, ok2 := lowerHex(h[36:52])
	flags, ok3 := lowerHex(h[53:55])
	if !ok1 || !ok2 || !ok3 {
		return sc, false
	}
	copy(sc.TraceID[:], traceID)
	copy(sc.SpanID[:], spanID)
	sc.Sampled = flags[0]&1 == 1
	if !sc.IsValid() {
		return SpanContext{}, false
	}
	return sc, true
}

func lowerHex(s string) ([]byte, bool) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return nil, false
		}
	}
	b, err := hex.DecodeString(s)
	return b, err == nil
}

// Kind is the role of a span in the trace.
type Kind int

const (
	KindInternal Kind = iota
	KindServer
	KindClient
	KindProducer
	KindConsumer
)

func (k Kind) String() string {
	switch k {
	case KindServer:
		return "server"
	case KindClient:
		return "client"
	case KindProducer:
		return "producer"
	case KindConsumer:
		return "consumer"
	default:
		return "internal"
	}
}

// StartConfig is what the start options resolve to. Implementations read it with NewStartConfig.
type StartConfig struct {
	Kind         Kind
	RemoteParent SpanContext
	NewRoot      bool
}

// StartOption configures Tracer.Start.
type StartOption func(*StartConfig)

// WithKind sets the span kind (default KindInternal).
func WithKind(k Kind) StartOption { return func(c *StartConfig) { c.Kind = k } }

// WithRemoteParent continues the trace that arrived from another process (e.g. a traceparent
// header). An invalid context is ignored.
func WithRemoteParent(sc SpanContext) StartOption {
	return func(c *StartConfig) { c.RemoteParent = sc }
}

// WithNewRoot starts a new trace even if ctx carries a span.
func WithNewRoot() StartOption { return func(c *StartConfig) { c.NewRoot = true } }

// NewStartConfig applies opts.
func NewStartConfig(opts ...StartOption) StartConfig {
	var c StartConfig
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

// Tracer starts spans.
type Tracer interface {
	// Start begins a span as a child of the span in ctx (or of the remote parent, or as a new
	// trace) and returns a context carrying it. The caller must End the span.
	Start(ctx context.Context, name string, opts ...StartOption) (context.Context, Span)
}

// Span is one leg of a trace.
type Span interface {
	// Context returns the identifiers of the span (the zero value for a no-op span).
	Context() SpanContext
	// SetName replaces the span name (e.g. once the HTTP route is known).
	SetName(name string)
	// SetAttributes adds key/value pairs, like log.Logger. Never pass personal data, secrets,
	// command bodies or SQL arguments.
	SetAttributes(kv ...any)
	// RecordError marks the span as failed with err; nil is ignored.
	RecordError(err error)
	// End finishes the span. Further calls have no effect.
	End()
}

type spanKey struct{}

// ContextWithSpan returns ctx carrying span. Tracer implementations call it from Start.
func ContextWithSpan(ctx context.Context, span Span) context.Context {
	return context.WithValue(ctx, spanKey{}, span)
}

// FromContext returns the span in ctx. It never returns nil: without a span it returns a no-op.
func FromContext(ctx context.Context) Span {
	if s, ok := ctx.Value(spanKey{}).(Span); ok && s != nil {
		return s
	}
	return noopSpan{}
}

// LogAttrs returns "trace_id" and "span_id" of the span in ctx as log arguments, or nil when
// there is no valid span.
func LogAttrs(ctx context.Context) []any {
	sc := FromContext(ctx).Context()
	if !sc.IsValid() {
		return nil
	}
	return []any{TraceIDKey, sc.TraceIDString(), SpanIDKey, sc.SpanIDString()}
}

// Noop returns a Tracer that records nothing and leaves the context untouched.
func Noop() Tracer { return noopTracer{} }

// OrNoop returns t, or Noop when t is nil.
func OrNoop(t Tracer) Tracer {
	if t == nil {
		return noopTracer{}
	}
	return t
}

type noopTracer struct{}

func (noopTracer) Start(ctx context.Context, _ string, _ ...StartOption) (context.Context, Span) {
	return ctx, noopSpan{}
}

type noopSpan struct{}

func (noopSpan) Context() SpanContext { return SpanContext{} }
func (noopSpan) SetName(string)       {}
func (noopSpan) SetAttributes(...any) {}
func (noopSpan) RecordError(error)    {}
func (noopSpan) End()                 {}
