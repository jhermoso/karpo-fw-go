// Package vanilla implements trace.Tracer with the standard library only: it generates W3C
// identifiers, links children to their parents, samples, and hands every finished span to an
// Exporter (a log line, or an in-memory Recorder for tests). There is no network export here:
// seeing spans in a viewer needs the OpenTelemetry adapter, which lives in a separate module.
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

// FinishedSpan is what an Exporter receives when a span ends.
type FinishedSpan struct {
	Name       string
	Kind       trace.Kind
	Context    trace.SpanContext
	ParentID   [8]byte // zero for the root of a trace
	Start, End time.Time
	Attributes []any // key/value pairs, in the order they were set
	Err        error // set by RecordError
}

// Duration returns how long the span lasted.
func (s FinishedSpan) Duration() time.Duration { return s.End.Sub(s.Start) }

// HasParent reports whether the span has a parent (local or remote).
func (s FinishedSpan) HasParent() bool { return s.ParentID != [8]byte{} }

// Attr returns the last value set for key, or nil.
func (s FinishedSpan) Attr(key string) any {
	var v any
	for i := 0; i+1 < len(s.Attributes); i += 2 {
		if k, ok := s.Attributes[i].(string); ok && k == key {
			v = s.Attributes[i+1]
		}
	}
	return v
}

// Exporter receives every sampled span when it ends. Export must be safe for concurrent use and
// must not block.
type Exporter interface {
	Export(span FinishedSpan)
}

// Tracer is the dependency-free trace.Tracer.
type Tracer struct {
	exporters []Exporter
	ratio     float64
	now       func() time.Time
}

var _ trace.Tracer = (*Tracer)(nil)

// Option configures a Tracer.
type Option func(*Tracer)

// WithExporter adds a destination for finished spans.
func WithExporter(e Exporter) Option {
	return func(t *Tracer) {
		if e != nil {
			t.exporters = append(t.exporters, e)
		}
	}
}

// WithSampleRatio sets the share of new traces that are recorded, from 0 to 1 (default 1: every
// trace). Children and remote parents keep the decision of their trace. Unsampled spans still
// get identifiers, so log lines of the request share a trace_id.
func WithSampleRatio(ratio float64) Option {
	return func(t *Tracer) { t.ratio = min(max(ratio, 0), 1) }
}

// WithClock replaces the clock (tests).
func WithClock(now func() time.Time) Option { return func(t *Tracer) { t.now = now } }

// New creates a Tracer. Without exporters spans are still created (identifiers for the logs)
// but go nowhere.
func New(opts ...Option) *Tracer {
	t := &Tracer{ratio: 1, now: time.Now}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Start implements trace.Tracer.
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

// sample decides from the trace id, so every process that uses the same ratio agrees.
func (t *Tracer) sample(traceID [16]byte) bool {
	if t.ratio >= 1 {
		return true
	}
	if t.ratio <= 0 {
		return false
	}
	return float64(binary.BigEndian.Uint64(traceID[8:])>>1) < t.ratio*float64(uint64(1)<<63)
}

// randomID fills b with random bytes that are not all zero.
func randomID(b []byte) {
	for {
		_, _ = rand.Read(b) // crypto/rand.Read never fails
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

// Recorder is an Exporter that keeps the last spans in memory, for tests and diagnostics.
type Recorder struct {
	mu    sync.Mutex
	limit int
	spans []FinishedSpan
}

// NewRecorder creates a Recorder that keeps at most limit spans (<= 0: 1024), dropping the
// oldest ones.
func NewRecorder(limit int) *Recorder {
	if limit <= 0 {
		limit = 1024
	}
	return &Recorder{limit: limit}
}

// Export implements Exporter.
func (r *Recorder) Export(s FinishedSpan) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.spans) >= r.limit {
		r.spans = append(r.spans[:0], r.spans[1:]...)
	}
	r.spans = append(r.spans, s)
}

// Spans returns a copy of the recorded spans, in the order they ended.
func (r *Recorder) Spans() []FinishedSpan {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]FinishedSpan(nil), r.spans...)
}

// Named returns the recorded spans called name.
func (r *Recorder) Named(name string) []FinishedSpan {
	var out []FinishedSpan
	for _, s := range r.Spans() {
		if s.Name == name {
			out = append(out, s)
		}
	}
	return out
}

// Reset forgets the recorded spans.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = nil
}

// LogExporter writes one line per finished span to logger, at Debug level: while the destination
// of the traces is the log, span lines must not multiply the Info output (the request line and
// the error lines already carry trace_id). Raise the logger to Debug to see them.
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
