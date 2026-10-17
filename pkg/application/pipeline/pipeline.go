// Package pipeline implementa el middleware estándar de los casos de uso (la contrapartida en Go de
// los pipeline behaviors de MediatR / los decoradores de manejadores de C#): validación, transacciones,
// reintentos por concurrencia optimista, idempotencia y log. Se componen con application.Chain; el
// primer middleware es el más exterior.
//
//	h := application.Chain[RegisterParty, PartyDTO](handler,
//		pipeline.Idempotent[RegisterParty, PartyDTO](store, 24*time.Hour),
//		pipeline.Validating[RegisterParty, PartyDTO](),
//		pipeline.RetryOnConflict[RegisterParty, PartyDTO](3, 10*time.Millisecond),
//		pipeline.Transactional[RegisterParty, PartyDTO](uow),
//	)
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
)

// Validating rechaza, antes de llegar al manejador, las peticiones cuyo método Validate
// (application.Validatable) falla.
func Validating[In, Out any]() application.Middleware[In, Out] {
	return func(next application.Handler[In, Out]) application.Handler[In, Out] {
		return application.HandlerFunc[In, Out](func(ctx context.Context, in In) (Out, error) {
			if v, ok := any(in).(application.Validatable); ok {
				if err := v.Validate(); err != nil {
					var zero Out
					if !errors.Is(err, domain.ErrValidation) {
						err = &domain.ValidationError{Errors: []domain.FieldError{{Field: "", Code: "invalid", Message: err.Error()}}}
					}
					return zero, err
				}
			}
			return next.Handle(ctx, in)
		})
	}
}

// Transactional ejecuta el manejador dentro de uow: todo lo que hace mediante los repositorios y el
// outbox se confirma o se deshace junto.
func Transactional[In, Out any](uow domain.UnitOfWork) application.Middleware[In, Out] {
	return func(next application.Handler[In, Out]) application.Handler[In, Out] {
		return application.HandlerFunc[In, Out](func(ctx context.Context, in In) (Out, error) {
			var out Out
			err := uow.Do(ctx, func(ctx context.Context) error {
				var err error
				out, err = next.Handle(ctx, in)
				return err
			})
			return out, err
		})
	}
}

// RetryOnConflict vuelve a ejecutar el manejador cuando falla con domain.ErrConflict (concurrencia
// optimista), hasta attempts veces en total, esperando backoff*intento entre uno y otro.
// Se coloca fuera de Transactional para que cada intento se ejecute en una transacción nueva.
func RetryOnConflict[In, Out any](attempts int, backoff time.Duration) application.Middleware[In, Out] {
	if attempts < 1 {
		attempts = 1
	}
	return func(next application.Handler[In, Out]) application.Handler[In, Out] {
		return application.HandlerFunc[In, Out](func(ctx context.Context, in In) (Out, error) {
			var (
				out Out
				err error
			)
			for i := 1; i <= attempts; i++ {
				out, err = next.Handle(ctx, in)
				if err == nil || !errors.Is(err, domain.ErrConflict) || i == attempts {
					return out, err
				}
				select {
				case <-ctx.Done():
					return out, ctx.Err()
				case <-time.After(backoff * time.Duration(i)):
				}
			}
			return out, err
		})
	}
}

// Logging registra cada ejecución con su tipo de petición, duración, desenlace e id de correlación.
// El id de correlación se escribe explícitamente para que todo log.Logger lo lleve, no sólo los que
// leen el contexto (log/vanilla lo hace, y no repite una clave que la línea ya tiene).
//
// Es preferible Observed, que además traza y mide el caso de uso y sólo registra los fallos.
func Logging[In, Out any](logger log.Logger) application.Middleware[In, Out] {
	return func(next application.Handler[In, Out]) application.Handler[In, Out] {
		return application.HandlerFunc[In, Out](func(ctx context.Context, in In) (Out, error) {
			start := time.Now()
			out, err := next.Handle(ctx, in)
			args := []any{
				"request", fmt.Sprintf("%T", in),
				"duration_ms", time.Since(start).Milliseconds(),
			}
			if id := application.CorrelationID(ctx); id != "" {
				args = append(args, "correlation_id", id)
			}
			if err != nil {
				logger.WithContext(ctx).Warn("use case failed", append(args, "error", err)...)
			} else {
				logger.WithContext(ctx).Info("use case executed", args...)
			}
			return out, err
		})
	}
}
