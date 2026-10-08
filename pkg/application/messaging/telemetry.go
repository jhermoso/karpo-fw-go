package messaging

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/metrics"
	"github.com/jhermoso/karpo-fw-go/pkg/trace"
)

// Outcomes of a consumed message, besides application.OutcomeOK and application.OutcomeError.
const (
	OutcomeDuplicate = "duplicate" // already claimed in the inbox: skipped
	OutcomeIgnored   = "ignored"   // no handler for its type: acknowledged without effect
)

type consumerTelemetry struct {
	logger  log.Logger
	tracer  trace.Tracer
	handled metrics.Counter
	lag     metrics.Histogram
}

// WithTelemetry instruments the consumer and returns it: a span per consumed message (a new
// trace that carries the correlation and causation ids of the envelope), the counter
// karpo.messaging.handled by consumer, event type and outcome (ok, duplicate, error, ignored),
// the histogram karpo.messaging.lag (seconds between the fact and its effect) and a log line
// with the correlation id when a handler fails. Any argument may be nil.
//
// Ignored messages are counted under the event type "other": their types are not a closed set.
func (c *Consumer) WithTelemetry(logger log.Logger, tracer trace.Tracer, meter metrics.Meter) *Consumer {
	m := metrics.OrNoop(meter)
	tel := &consumerTelemetry{
		logger:  logger,
		tracer:  trace.OrNoop(tracer),
		handled: m.Counter(metrics.MessagingHandled, metrics.UnitNone, "Integration messages handled by a consumer, by outcome."),
		lag: m.Histogram(metrics.MessagingLag, metrics.UnitSeconds, "Time between an integration event and its handling.",
			0.01, 0.05, 0.1, 0.5, 1, 5, 15, 60, 300, 1800, 3600),
	}
	c.mu.Lock()
	c.tel = tel
	c.mu.Unlock()
	return c
}

func (t *consumerTelemetry) ignored(ctx context.Context, consumer string) {
	if t == nil {
		return
	}
	t.handled.Add(ctx, 1, metrics.LabelConsumer, consumer, metrics.LabelEventType, "other", metrics.LabelOutcome, OutcomeIgnored)
}

// start opens the span of a message; the returned function closes it and records the outcome.
func (t *consumerTelemetry) start(ctx context.Context, consumer string, env application.Envelope) (context.Context, func(duplicate bool, err error)) {
	if t == nil {
		return ctx, func(bool, error) {}
	}
	ctx, span := t.tracer.Start(ctx, "consume "+env.Type, trace.WithKind(trace.KindConsumer), trace.WithNewRoot())
	span.SetAttributes("consumer", consumer, "event_type", env.Type, "message_id", env.ID)
	if env.CorrelationID != "" {
		span.SetAttributes("correlation_id", env.CorrelationID)
	}
	if env.CausationID != "" {
		span.SetAttributes("causation_id", env.CausationID)
	}
	return ctx, func(duplicate bool, err error) {
		outcome := application.OutcomeOK
		switch {
		case err != nil:
			outcome = application.OutcomeError
			span.RecordError(err)
		case duplicate:
			outcome = OutcomeDuplicate
		}
		span.SetAttributes("outcome", outcome)
		span.End()
		t.handled.Add(ctx, 1, metrics.LabelConsumer, consumer, metrics.LabelEventType, env.Type, metrics.LabelOutcome, outcome)
		if outcome == application.OutcomeOK && !env.OccurredAt.IsZero() {
			t.lag.Record(ctx, max(Lag(env).Seconds(), 0), metrics.LabelConsumer, consumer, metrics.LabelEventType, env.Type)
		}
		if err != nil && t.logger != nil {
			args := []any{"consumer", consumer, "event_type", env.Type, "message_id", env.ID,
				"correlation_id", env.CorrelationID, "error", err.Error()}
			t.logger.WithContext(ctx).Warn("integration message failed", append(args, trace.LogAttrs(ctx)...)...)
		}
	}
}
