package memory

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
)

type outboxRow struct {
	msg       application.OutboxMessage
	processed bool
}

// Outbox is an in-memory application.OutboxStore bound to a Store's unit of work.
type Outbox struct {
	store *Store
	mu    sync.Mutex
	rows  map[string]*outboxRow
}

// NewOutbox creates an outbox on store.
func NewOutbox(store *Store) *Outbox {
	return &Outbox{store: store, rows: make(map[string]*outboxRow)}
}

// Append adds messages; they are removed again if the unit of work rolls back.
func (o *Outbox) Append(ctx context.Context, msgs ...application.OutboxMessage) error {
	if o.store.closed.Load() {
		return ErrClosed
	}
	o.mu.Lock()
	for _, m := range msgs {
		o.rows[m.ID] = &outboxRow{msg: m}
	}
	o.mu.Unlock()
	o.store.onRollback(ctx, func() {
		o.mu.Lock()
		for _, m := range msgs {
			delete(o.rows, m.ID)
		}
		o.mu.Unlock()
	})
	return nil
}

// Pending returns unprocessed messages below maxAttempts, oldest first.
func (o *Outbox) Pending(_ context.Context, limit, maxAttempts int) ([]application.OutboxMessage, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]application.OutboxMessage, 0)
	for _, r := range o.rows {
		if !r.processed && r.msg.Attempts < maxAttempts {
			out = append(out, r.msg)
		}
	}
	slices.SortFunc(out, func(a, b application.OutboxMessage) int {
		if c := a.OccurredAt.Compare(b.OccurredAt); c != 0 {
			return c
		}
		if a.ID < b.ID {
			return -1
		}
		return 1
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// MarkProcessed flags a message as delivered.
func (o *Outbox) MarkProcessed(_ context.Context, id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if r, ok := o.rows[id]; ok {
		r.processed = true
	}
	return nil
}

// MarkFailed records a failed delivery attempt.
func (o *Outbox) MarkFailed(_ context.Context, id string, cause error) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if r, ok := o.rows[id]; ok {
		r.msg.Attempts++
		if cause != nil {
			r.msg.LastError = cause.Error()
		}
	}
	return nil
}

// IdempotencyStore is an in-memory application.IdempotencyStore with expiry.
type IdempotencyStore struct {
	mu    sync.Mutex
	now   func() time.Time
	items map[string]idemItem
}

type idemItem struct {
	value   []byte
	expires time.Time
}

// NewIdempotencyStore creates an empty store.
func NewIdempotencyStore() *IdempotencyStore {
	return &IdempotencyStore{now: time.Now, items: make(map[string]idemItem)}
}

// Get returns a stored, non-expired value.
func (s *IdempotencyStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[key]
	if !ok {
		return nil, false, nil
	}
	if !it.expires.IsZero() && s.now().After(it.expires) {
		delete(s.items, key)
		return nil, false, nil
	}
	return it.value, true, nil
}

// Put stores value for ttl (ttl <= 0: no expiry).
func (s *IdempotencyStore) Put(_ context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var exp time.Time
	if ttl > 0 {
		exp = s.now().Add(ttl)
	}
	s.items[key] = idemItem{value: append([]byte(nil), value...), expires: exp}
	return nil
}

var (
	_ application.OutboxStore      = (*Outbox)(nil)
	_ application.IdempotencyStore = (*IdempotencyStore)(nil)
)
