// Package trace es el contrato de trazas del framework: un Tracer abre spans, un Span es un tramo
// de una petición (HTTP -> caso de uso -> SQL) y un SpanContext lo identifica de un proceso a
// otro con la cabecera traceparent de W3C.
//
// El contrato sólo depende de la biblioteca estándar. Las implementaciones están en subpaquetes
// (trace/vanilla no tiene dependencias; un adaptador de OpenTelemetry va en un módulo aparte).
// Cada pieza instrumentada del framework recibe un Tracer como opción y, si no lo tiene, usa
// Noop, así que un servicio que no configura nada se comporta exactamente como antes.
package trace

import (
	"context"
	"encoding/hex"
	"strings"
)

// TraceParentHeader es la cabecera W3C Trace Context.
const TraceParentHeader = "traceparent"

// Claves de los atributos de log del span actual (véase LogAttrs).
const (
	TraceIDKey = "trace_id"
	SpanIDKey  = "span_id"
)

// SpanContext identifica un span: la traza a la que pertenece, su propio id y si se registra.
type SpanContext struct {
	TraceID [16]byte
	SpanID  [8]byte
	Sampled bool
}

// IsValid dice si están puestos los dos ids (W3C: todo ceros no es válido).
func (sc SpanContext) IsValid() bool {
	return sc.TraceID != [16]byte{} && sc.SpanID != [8]byte{}
}

// TraceIDString devuelve el id de traza como 32 caracteres hexadecimales en minúscula.
func (sc SpanContext) TraceIDString() string { return hex.EncodeToString(sc.TraceID[:]) }

// SpanIDString devuelve el id del span como 16 caracteres hexadecimales en minúscula.
func (sc SpanContext) SpanIDString() string { return hex.EncodeToString(sc.SpanID[:]) }

// TraceParent compone el valor de la cabecera traceparent de W3C, o "" para un contexto no válido.
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

// ParseTraceParent analiza una cabecera traceparent de W3C: version-traceid-parentid-flags, en
// hexadecimal en minúscula. Las versiones desconocidas se aceptan siempre que los cuatro campos
// conocidos estén bien formados (la especificación pide compatibilidad hacia delante); se rechazan
// la versión ff, el hexadecimal en mayúscula y los ids todo ceros.
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
	// La versión 00 tiene exactamente cuatro campos; las posteriores pueden añadir más tras un guion.
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

// Kind es el papel de un span en la traza.
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

// StartConfig es el resultado de aplicar las opciones de inicio. Las implementaciones lo leen con NewStartConfig.
type StartConfig struct {
	Kind         Kind
	RemoteParent SpanContext
	NewRoot      bool
}

// StartOption configura Tracer.Start.
type StartOption func(*StartConfig)

// WithKind fija el tipo del span (por defecto KindInternal).
func WithKind(k Kind) StartOption { return func(c *StartConfig) { c.Kind = k } }

// WithRemoteParent continúa la traza que llegó de otro proceso (p. ej. una cabecera
// traceparent). Un contexto no válido se ignora.
func WithRemoteParent(sc SpanContext) StartOption {
	return func(c *StartConfig) { c.RemoteParent = sc }
}

// WithNewRoot abre una traza nueva aunque ctx lleve un span.
func WithNewRoot() StartOption { return func(c *StartConfig) { c.NewRoot = true } }

// NewStartConfig aplica opts.
func NewStartConfig(opts ...StartOption) StartConfig {
	var c StartConfig
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

// Tracer abre spans.
type Tracer interface {
	// Start abre un span hijo del span de ctx (o del padre remoto, o como traza nueva) y
	// devuelve un contexto que lo lleva. Quien llama debe cerrar el span con End.
	Start(ctx context.Context, name string, opts ...StartOption) (context.Context, Span)
}

// Span es un tramo de una traza.
type Span interface {
	// Context devuelve los identificadores del span (el valor cero para un span no-op).
	Context() SpanContext
	// SetName sustituye el nombre del span (p. ej. cuando ya se conoce la ruta HTTP).
	SetName(name string)
	// SetAttributes añade pares clave/valor, como log.Logger. Nunca se pasan datos personales,
	// secretos, cuerpos de comandos ni argumentos SQL.
	SetAttributes(kv ...any)
	// RecordError marca el span como fallido con err; nil se ignora.
	RecordError(err error)
	// End termina el span. Las llamadas siguientes no tienen efecto.
	End()
}

type spanKey struct{}

// ContextWithSpan devuelve ctx con span. Las implementaciones de Tracer la llaman desde Start.
func ContextWithSpan(ctx context.Context, span Span) context.Context {
	return context.WithValue(ctx, spanKey{}, span)
}

// FromContext devuelve el span de ctx. Nunca devuelve nil: sin span devuelve uno no-op.
func FromContext(ctx context.Context) Span {
	if s, ok := ctx.Value(spanKey{}).(Span); ok && s != nil {
		return s
	}
	return noopSpan{}
}

// LogAttrs devuelve "trace_id" y "span_id" del span de ctx como argumentos de log, o nil cuando
// no hay un span válido.
func LogAttrs(ctx context.Context) []any {
	sc := FromContext(ctx).Context()
	if !sc.IsValid() {
		return nil
	}
	return []any{TraceIDKey, sc.TraceIDString(), SpanIDKey, sc.SpanIDString()}
}

// Noop devuelve un Tracer que no registra nada y deja el contexto intacto.
func Noop() Tracer { return noopTracer{} }

// OrNoop devuelve t, o Noop cuando t es nil.
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
