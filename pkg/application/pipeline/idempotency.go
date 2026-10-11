package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// CodeIdempotencyKeyReused is the rule code returned when a caller repeats an idempotency key
// with a different request.
const CodeIdempotencyKeyReused = "idempotency.key_reused"

// idempotentRecord is what the store keeps for a processed request: the fingerprint of the
// request that produced the result, and the result.
type idempotentRecord struct {
	Request string          `json:"request"`
	Result  json.RawMessage `json:"result"`
}

// Idempotent returns the stored result for a request whose idempotency key
// (application.IdempotencyKeyed) was already processed successfully, instead of executing it
// again (equivalent to the C# IdempotencyCommandHandlerDecorator). Results are stored as JSON
// for ttl. Requests without a key (or with an empty key) pass through.
//
// A key belongs to the caller that used it: the same key sent by another caller is a different
// request, so nobody receives somebody else's result. Repeating a key with a different request
// is a rule violation (CodeIdempotencyKeyReused) rather than a replay of the first result.
//
// The result is stored after next returns, so it is not part of the transaction of the use
// case: a process that stops between the commit and the store executes the request again on
// retry, and two simultaneous requests with the same key may both execute. Closing that window
// needs a store that writes in the unit of work (BACKLOG.md, "Idempotencia persistente").
func Idempotent[In, Out any](store application.IdempotencyStore, ttl time.Duration) application.Middleware[In, Out] {
	return func(next application.Handler[In, Out]) application.Handler[In, Out] {
		return application.HandlerFunc[In, Out](func(ctx context.Context, in In) (Out, error) {
			keyed, ok := any(in).(application.IdempotencyKeyed)
			if !ok || keyed.IdempotencyKey() == "" {
				return next.Handle(ctx, in)
			}
			key := fmt.Sprintf("%T:%q:%s", in, idempotencyCaller(ctx), keyed.IdempotencyKey())

			var out Out
			body, err := json.Marshal(in)
			if err != nil {
				return out, fmt.Errorf("idempotency: encoding request: %w", err)
			}
			sum := sha256.Sum256(body)
			fingerprint := hex.EncodeToString(sum[:])

			if raw, found, err := store.Get(ctx, key); err != nil {
				return out, err
			} else if found {
				var rec idempotentRecord
				if err := json.Unmarshal(raw, &rec); err != nil {
					return out, fmt.Errorf("idempotency: decoding stored record: %w", err)
				}
				if rec.Request != fingerprint {
					return out, domain.Violation(CodeIdempotencyKeyReused,
						"the idempotency key was already used with a different request")
				}
				if err := json.Unmarshal(rec.Result, &out); err != nil {
					return out, fmt.Errorf("idempotency: decoding stored result: %w", err)
				}
				return out, nil
			}

			out, err = next.Handle(ctx, in)
			if err != nil {
				return out, err
			}
			result, err := json.Marshal(out)
			if err != nil {
				return out, fmt.Errorf("idempotency: encoding result: %w", err)
			}
			raw, err := json.Marshal(idempotentRecord{Request: fingerprint, Result: result})
			if err != nil {
				return out, fmt.Errorf("idempotency: encoding record: %w", err)
			}
			return out, store.Put(ctx, key, raw, ttl)
		})
	}
}

// idempotencyCaller identifies who is making the request: the authenticated subject when there
// is an authorization context, the actor of the request otherwise.
func idempotencyCaller(ctx context.Context) string {
	if c, ok := authz.FromContext(ctx); ok && !c.Subject.IsZero() {
		return c.TenantID + "/" + c.Subject.String()
	}
	a := application.ActorFrom(ctx)
	if !a.PartyID.IsZero() {
		return a.PartyID.String()
	}
	return a.Name
}
