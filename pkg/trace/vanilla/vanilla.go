// Package vanilla implementa trace.Tracer sólo con la biblioteca estándar: genera identificadores
// W3C, enlaza los hijos con sus padres, hace muestreo y entrega cada span terminado a un Exporter
// (una línea de log, o un Recorder en memoria para las pruebas). Aquí no hay exportación por red:
// ver los spans en un visor necesita el adaptador de OpenTelemetry, que está en un módulo aparte.
package vanilla

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"sync"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/trace"
)

// FinishedSpan es lo que recibe un Exporter cuando termina un span.
type FinishedSpan struct {
	Name       string
	Kind       trace.Kind
	Context    trace.SpanContext
	ParentID   [8]byte // cero para la raíz de una traza
	Start, End time.Time
	Attributes []any // pares clave/valor, en el orden en que se pusieron
	Err        error // lo pone RecordError
}

// Duration devuelve cuánto duró el span.
func (s FinishedSpan) Duration() time.Duration { return s.End.Sub(s.Start) }

// HasParent dice si el span tiene padre (local o remoto).
func (s FinishedSpan) HasParent() bool { return s.ParentID != [8]byte{} }

// Attr devuelve el último valor puesto para key, o nil.
func (s FinishedSpan) Attr(key string) any {
	var v any
	for i := 0; i+1 < len(s.Attributes); i += 2 {
		if k, ok := s.Attributes[i].(string); ok && k == key {
			v = s.Attributes[i+1]
		}
	}
	return v
}

// Exporter recibe cada span muestreado cuando termina. Export debe admitir uso concurrente y no
// debe bloquear.
type Exporter interface {
	Export(span FinishedSpan)
}

// Tracer es el trace.Tracer sin dependencias.
type Tracer struct {
	exporters []Exporter
	ratio     float64
	now       func() time.Time
}

var _ trace.Tracer = (*Tracer)(nil)

// Option configura un Tracer.
type Option func(*Tracer)

// WithExporter añade un destino para los spans terminados.
func WithExporter(e Exporter) Option {
	return func(t *Tracer) {
		if e != nil {
			t.exporters = append(t.exporters, e)
		}
	}
}

// WithSampleRatio fija la proporción de trazas nuevas que se registran, de 0 a 1 (por defecto 1:
// todas). Los hijos y los padres remotos mantienen la decisión de su traza. Los spans no
// muestreados reciben igualmente identificadores, para que las líneas de log de la petición
// compartan trace_id.
func WithSampleRatio(ratio float64) Option {
	return func(t *Tracer) { t.ratio = min(max(ratio, 0), 1) }
}

// WithClock sustituye el reloj (pruebas).
func WithClock(now func() time.Time) Option { return func(t *Tracer) { t.now = now } }

// New crea un Tracer. Sin exporters se siguen creando spans (identificadores para los logs),
// pero no van a ninguna parte.
func New(opts ...Option) *Tracer {
	t := &Tracer{ratio: 1, now: time.Now}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Start implementa trace.Tracer.
func (t *Tracer) Start(ctx context.Context, name string, opts ...trace.StartOption) (context.Context, trace.Span) {
	cfg := trace.NewStartConfig(opts...)
	parent := trace.SpanContext{}
	if !cfg.NewRoot {
		if cfg.RemoteParent.IsValid() {
			parent = cfg.RemoteParent
		} else {
			parent = trace.FromContext(ctx).Context()
		}
	}
	s := &span{tracer: t, name: name, kind: cfg.Kind, start: t.now()}
	if parent.IsValid() {
		s.sc.TraceID = parent.TraceID
		s.sc.Sampled = parent.Sampled
		s.parentID = parent.SpanID
	} else {
		randomID(s.sc.TraceID[:])
		s.sc.Sampled = t.sample(s.sc.TraceID)
	}
	randomID(s.sc.SpanID[:])
	return trace.ContextWithSpan(ctx, s), s
}

// sample decide a partir del id de traza, de modo que todos los procesos que usan la misma
// proporción coinciden.
func (t *Tracer) sample(traceID [16]byte) bool {
	if t.ratio >= 1 {
		return true
	}
	if t.ratio <= 0 {
		return false
	}
	return float64(binary.BigEndian.Uint64(traceID[8:])>>1) < t.ratio*float64(uint64(1)<<63)
}

// randomID llena b de bytes aleatorios que no son todos cero.
func randomID(b []byte) {
	for {
		_, _ = rand.Read(b) // crypto/rand.Read nunca falla
		for _, c := range b {
			if c != 0 {
				return
			}
		}
	}
}

type span struct {
	tracer   *Tracer
	sc       trace.SpanContext
	parentID [8]byte
	kind     trace.Kind
	start    time.Time

	mu    sync.Mutex
	name  string
	attrs []any
	err   error
	ended bool
}

func (s *span) Context() trace.SpanContext { return s.sc }

func (s *span) SetName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ended {
		s.name = name
	}
}

func (s *span) SetAttributes(kv ...any) {
	if !s.sc.Sampled {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ended {
		s.attrs = append(s.attrs, kv[:len(kv)&^1]...)
	}
}

func (s *span) RecordError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ended {
		s.err = err
	}
}

func (s *span) End() {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	fs := FinishedSpan{Name: s.name, Kind: s.kind, Context: s.sc, ParentID: s.parentID,
		Start: s.start, End: s.tracer.now(), Attributes: s.attrs, Err: s.err}
	s.mu.Unlock()
	if !s.sc.Sampled {
		return
	}
	for _, e := range s.tracer.exporters {
		e.Export(fs)
	}
}

// Recorder es un Exporter que guarda en memoria los últimos spans, para pruebas y diagnóstico.
type Recorder struct {
	mu    sync.Mutex
	limit int
	spans []FinishedSpan
}

// NewRecorder crea un Recorder que guarda como mucho limit spans (<= 0: 1024) y descarta los
// más antiguos.
func NewRecorder(limit int) *Recorder {
	if limit <= 0 {
		limit = 1024
	}
	return &Recorder{limit: limit}
}

// Export implementa Exporter.
func (r *Recorder) Export(s FinishedSpan) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.spans) >= r.limit {
		r.spans = append(r.spans[:0], r.spans[1:]...)
	}
	r.spans = append(r.spans, s)
}

// Spans devuelve una copia de los spans registrados, en el orden en que terminaron.
func (r *Recorder) Spans() []FinishedSpan {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]FinishedSpan(nil), r.spans...)
}

// Named devuelve los spans registrados que se llaman name.
func (r *Recorder) Named(name string) []FinishedSpan {
	var out []FinishedSpan
	for _, s := range r.Spans() {
		if s.Name == name {
			out = append(out, s)
		}
	}
	return out
}

// Reset olvida los spans registrados.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = nil
}

// LogExporter escribe en logger una línea por span terminado, con nivel Debug: mientras el destino
// de las trazas sea el log, las líneas de span no deben multiplicar la salida Info (la línea de la
// petición y las de error ya llevan trace_id). Para verlas hay que subir el logger a Debug.
func LogExporter(logger log.Logger) Exporter { return logExporter{logger} }

type logExporter struct{ logger log.Logger }

func (e logExporter) Export(s FinishedSpan) {
	args := make([]any, 0, len(s.Attributes)+14)
	args = append(args,
		"span", s.Name,
		"kind", s.Kind.String(),
		trace.TraceIDKey, s.Context.TraceIDString(),
		trace.SpanIDKey, s.Context.SpanIDString(),
		"duration_ms", float64(s.Duration().Microseconds())/1000,
	)
	if s.HasParent() {
		args = append(args, "parent_span_id", trace.SpanContext{SpanID: s.ParentID}.SpanIDString())
	}
	if s.Err != nil {
		args = append(args, "error", s.Err.Error())
	}
	e.logger.Debug("span", append(args, s.Attributes...)...)
}
