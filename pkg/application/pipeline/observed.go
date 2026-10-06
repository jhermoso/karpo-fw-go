package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/metrics"
	"github.com/jhermoso/karpo-fw-go/pkg/trace"
)

// Span attributes and log fields of a use case.
const (
	FieldRequest = "request"
	FieldOutcome = "outcome"
)

// Observed is the telemetry of a use case. For every execution it leaves:
//
//   - a span named after the request type, child of the span in the context (the HTTP request);
//   - one observation of karpo.usecase.duration (seconds) by request type and outcome, where the
//     outcome follows the domain error taxonomy (application.Outcome);
//   - a log line only when it fails: Error for an unexpected failure, Debug for a rejection the
//     taxonomy explains (validation, rule, not found...), which the request line already reports.
//
// Neither the request nor its fields are recorded: only its type. Place it first (outermost) so
// retries and the transaction are inside the span. Any of logger, tracer and meter may be nil.
func Observed[In, Out any](logger log.Logger, tracer trace.Tracer, meter metrics.Meter) application.Middleware[In, Out] {
	tracer = trace.OrNoop(tracer)
	duration := metrics.OrNoop(meter).Histogram(metrics.UseCaseDuration, metrics.UnitSeconds,
		"Duration of use case executions.")
	var zero In
	request := fmt.Sprintf("%T", zero)

	return func(next application.Handler[In, Out]) application.Handler[In, Out] {
		return application.HandlerFunc[In, Out](func(ctx context.Context, in In) (Out, error) {
			start := time.Now()
			ctx, span := tracer.Start(ctx, request)
			out, err := next.Handle(ctx, in)
			elapsed := time.Since(start)

			outcome := application.Outcome(err)
			duration.Record(ctx, elapsed.Seconds(), metrics.LabelRequest, request, metrics.LabelOutcome, outcome)
			span.SetAttributes(FieldRequest, request, FieldOutcome, outcome)
			if outcome == application.OutcomeError {
				span.RecordError(err)
			}
			span.End()

			if err != nil && logger != nil {
				args := []any{
					FieldRequest, request,
					FieldOutcome, outcome,
					"duration_ms", float64(elapsed.Microseconds()) / 1000,
					"error", err.Error(),
				}
				if id := application.CorrelationID(ctx); id != "" {
					args = append(args, "correlation_id", id)
				}
				args = append(args, trace.LogAttrs(ctx)...)
				if outcome == application.OutcomeError {
					logger.WithContext(ctx).Error("use case failed", args...)
				} else {
					logger.WithContext(ctx).Debug("use case rejected", args...)
				}
			}
			return out, err
		})
	}
}
