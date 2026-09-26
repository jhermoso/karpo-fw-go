package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
)

// Idempotent returns the stored result for a request whose idempotency key
// (application.IdempotencyKeyed) was already processed successfully, instead of executing it
// again (equivalent to the C# IdempotencyCommandHandlerDecorator). Results are stored as JSON
// for ttl. Requests without a key (or with an empty key) pass through.
func Idempotent[In, Out any](store application.IdempotencyStore, ttl time.Duration) application.Middleware[In, Out] {
	return func(next application.Handler[In, Out]) application.Handler[In, Out] {
		return application.HandlerFunc[In, Out](func(ctx context.Context, in In) (Out, error) {
			keyed, ok := any(in).(application.IdempotencyKeyed)
			if !ok || keyed.IdempotencyKey() == "" {
				return next.Handle(ctx, in)
			}
			key := fmt.Sprintf("%T:%s", in, keyed.IdempotencyKey())

			var out Out
			if raw, found, err := store.Get(ctx, key); err != nil {
				return out, err
			} else if found {
				if err := json.Unmarshal(raw, &out); err != nil {
					return out, fmt.Errorf("idempotency: decoding stored result: %w", err)
				}
				return out, nil
			}

			out, err := next.Handle(ctx, in)
			if err != nil {
				return out, err
			}
			raw, err := json.Marshal(out)
			if err != nil {
				return out, fmt.Errorf("idempotency: encoding result: %w", err)
			}
			return out, store.Put(ctx, key, raw, ttl)
		})
	}
}
