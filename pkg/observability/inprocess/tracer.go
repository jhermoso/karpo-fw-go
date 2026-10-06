// Package inprocess implements the observability contract inside the process, with the standard
// library only: a Tracer that creates W3C trace and span ids (so logs and responses carry a
// trace_id even without a collector) and keeps the last finished spans, and a Registry of
// metrics that can be read back (Snapshot) or exposed (see observability/prometheus).
package inprocess

import (
	"context"
	"crypto/rand"
	"sync"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/observability"
)

// FinishedSpan is a span that has ended.
type FinishedSpan struct {
	Name        string
	Kind        observability.SpanKind
	Context     observability.SpanContext
	Parent      observability.SpanContext // invalid for a root span
	Attrs       []observability.Attr
	Errors      []string
	Status      observability.StatusCode
	Description string
	Start, End  time.Time
}

// Attr returns the value of the attribute key, or "".
func (s FinishedSpan) Attr(key string) string {
	for i := len(s.Attrs) - 1; i >= 0; i-- {
		if s.Attrs[i].Key == key {
			return s.Attrs[i].Value
		}
	}
	return ""
}

// Tracer is an observability.Tracer that keeps the last finished spans in a bounded buffer.
type Tracer struct {
	mu       sync.Mutex
	capacity int
	finished []FinishedSpan
	now      func() time.Time
}

// NewTracer creates a tracer that keeps up to capacity finished spans (0 keeps none: the spans
// only provide identities for logs and propagation).
func NewTracer(capacity int) *Tracer {
	return &Tracer{capacity: capacity, now: time.Now}
}

var _ observability.Tracer = (*Tracer)(nil)

// Start implements observability.Tracer. A child inherits the trace id and the sampled flag of
// its parent; a root span is sampled.
func (t *Tracer) Start(ctx context.Context, name string, opts ...observability.SpanOption) (context.Context, observability.Span) {
	cfg := observability.NewSpanConfig(opts...)
	s := &span{tracer: t, name: name, kind: cfg.Kind, attrs: cfg.Attrs, start: t.now()}
	if parent, ok := observability.ParentFromContext(ctx); ok {
		s.parent = parent
		s.sc.TraceID = parent.TraceID
		s.sc.Sampled = parent.Sampled
	} else {
		_, _ = rand.Read(s.sc.TraceID[:])
		s.sc.Sampled = true
	}
	_, _ = rand.Read(s.sc.SpanID[:])
	return observability.ContextWithSpan(ctx, s), s
}

// Finished returns a copy of the finished spans, oldest first.
func (t *Tracer) Finished() []FinishedSpan {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]FinishedSpan(nil), t.finished...)
}

func (t *Tracer) record(fs FinishedSpan) {
	if t.capacity <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.finished) == t.capacity {
		copy(t.finished, t.finished[1:])
		t.finished = t.finished[:len(t.finished)-1]
	}
	t.finished = append(t.finished, fs)
}

type span struct {
	tracer *Tracer
	name   string
	kind   observability.SpanKind
	sc     observability.SpanContext
	parent observability.SpanContext

	mu     sync.Mutex
	attrs  []observability.Attr
	errs   []string
	status observability.StatusCode
	desc   string
	start  time.Time
	ended  bool
}

func (s *span) Context() observability.SpanContext { return s.sc }

func (s *span) SetName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ended {
		s.name = name
	}
}

func (s *span) SetAttributes(attrs ...observability.Attr) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ended {
		s.attrs = append(s.attrs, attrs...)
	}
}

func (s *span) RecordError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ended {
		s.errs = append(s.errs, err.Error())
	}
}

func (s *span) SetStatus(code observability.StatusCode, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ended {
		s.status, s.desc = code, description
	}
}

func (s *span) End() {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	fs := FinishedSpan{Name: s.name, Kind: s.kind, Context: s.sc, Parent: s.parent,
		Attrs: append([]observability.Attr(nil), s.attrs...), Errors: append([]string(nil), s.errs...),
		Status: s.status, Description: s.desc, Start: s.start, End: s.tracer.now()}
	s.mu.Unlock()
	s.tracer.record(fs)
}
