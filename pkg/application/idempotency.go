package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// IdempotencyStore remembers the result of already-processed requests
// (equivalent to the C# IIdempotencyStore).
type IdempotencyStore interface {
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	Put(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

// IdempotencyKeyed is implemented by commands carrying a client-supplied idempotency key.
type IdempotencyKeyed interface {
	IdempotencyKey() string
}

// Idempotent returns the stored result for a request whose idempotency key was already
// processed successfully, instead of executing it again (equivalent to the C#
// IdempotencyCommandHandlerDecorator). Results are stored as JSON for ttl.
// Requests without a key (or with an empty key) pass through.
func Idempotent[In, Out any](store IdempotencyStore, ttl time.Duration) Middleware[In, Out] {
	return func(next Handler[In, Out]) Handler[In, Out] {
		return HandlerFunc[In, Out](func(ctx context.Context, in In) (Out, error) {
			keyed, ok := any(in).(IdempotencyKeyed)
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
