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

// Atributos de span y campos de log de un caso de uso.
const (
	FieldRequest = "request"
	FieldOutcome = "outcome"
)

// Observed es la telemetría de un caso de uso. De cada ejecución deja:
//
//   - un span con el nombre del tipo de la petición, hijo del span del contexto (la petición HTTP);
//   - una observación de karpo.usecase.duration (segundos) por tipo de petición y desenlace, donde el
//     desenlace sigue la taxonomía de errores del dominio (application.Outcome);
//   - una línea de log sólo cuando falla: Error para un fallo inesperado, Debug para un rechazo que la
//     taxonomía explica (validación, regla, no encontrado...), del que ya informa la línea de la petición.
//
// No se registran ni la petición ni sus campos: sólo su tipo. Se coloca el primero (el más exterior)
// para que los reintentos y la transacción queden dentro del span. Cualquiera de logger, tracer y meter puede ser nil.
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
