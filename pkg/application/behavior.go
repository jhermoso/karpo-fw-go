package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
)

// Validatable is implemented by commands/queries that can check their own input.
// Return a *domain.ValidationError (see domain.Validation) to get field-level details.
type Validatable interface {
	Validate() error
}

// Validating rejects requests whose Validate method fails, before reaching the handler.
func Validating[In, Out any]() Middleware[In, Out] {
	return func(next Handler[In, Out]) Handler[In, Out] {
		return HandlerFunc[In, Out](func(ctx context.Context, in In) (Out, error) {
			if v, ok := any(in).(Validatable); ok {
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

// Transactional runs the handler inside uow: everything it does through repositories and the
// outbox commits or rolls back together.
func Transactional[In, Out any](uow domain.UnitOfWork) Middleware[In, Out] {
	return func(next Handler[In, Out]) Handler[In, Out] {
		return HandlerFunc[In, Out](func(ctx context.Context, in In) (Out, error) {
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

// RetryOnConflict re-executes the handler when it fails with domain.ErrConflict (optimistic
// concurrency), up to attempts times in total, waiting backoff*attempt between tries.
// Place it outside Transactional so every attempt runs in a fresh transaction.
func RetryOnConflict[In, Out any](attempts int, backoff time.Duration) Middleware[In, Out] {
	if attempts < 1 {
		attempts = 1
	}
	return func(next Handler[In, Out]) Handler[In, Out] {
		return HandlerFunc[In, Out](func(ctx context.Context, in In) (Out, error) {
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

// Logging logs every execution with its request type, duration and outcome.
func Logging[In, Out any](logger log.Logger) Middleware[In, Out] {
	return func(next Handler[In, Out]) Handler[In, Out] {
		return HandlerFunc[In, Out](func(ctx context.Context, in In) (Out, error) {
			start := time.Now()
			out, err := next.Handle(ctx, in)
			args := []any{
				"request", fmt.Sprintf("%T", in),
				"duration_ms", time.Since(start).Milliseconds(),
				"correlation_id", CorrelationID(ctx),
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
