package messaging

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/metrics"
	"github.com/jhermoso/karpo-fw-go/pkg/trace"
)

// Desenlaces de un mensaje consumido, además de application.OutcomeOK y application.OutcomeError.
const (
	OutcomeDuplicate = "duplicate" // ya reclamado en el inbox: se salta
	OutcomeIgnored   = "ignored"   // sin manejador para su tipo: se confirma sin efecto
)

type consumerTelemetry struct {
	logger  log.Logger
	tracer  trace.Tracer
	handled metrics.Counter
	lag     metrics.Histogram
}

// WithTelemetry instrumenta el consumidor y lo devuelve: un span por mensaje consumido (una traza
// nueva que lleva los ids de correlación y de causalidad del sobre), el contador
// karpo.messaging.handled por consumidor, tipo de evento y desenlace (ok, duplicate, error, ignored),
// el histograma karpo.messaging.lag (segundos entre el hecho y su efecto) y una línea de log con el
// id de correlación cuando un manejador falla. Cualquier argumento puede ser nil.
//
// Los mensajes ignorados se cuentan bajo el tipo de evento "other": sus tipos no son un conjunto cerrado.
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

// start abre el span de un mensaje; la función devuelta lo cierra y registra el desenlace.
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
