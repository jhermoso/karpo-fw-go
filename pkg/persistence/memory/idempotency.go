package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

type ledgerKey struct {
	tenant  string
	subject domain.UUID
	key     string
}

// IdempotencyLedger es un application.IdempotencyLedger en memoria ligado a la unidad de trabajo de
// un Store: lo reservado en una unidad de trabajo que hace rollback se olvida.
type IdempotencyLedger struct {
	store *Store
	mu    sync.Mutex
	items map[ledgerKey]application.IdempotencyEntry
}

var _ application.IdempotencyLedger = (*IdempotencyLedger)(nil)

// NewIdempotencyLedger crea un registro sobre store.
func NewIdempotencyLedger(store *Store) *IdempotencyLedger {
	return &IdempotencyLedger{store: store, items: map[ledgerKey]application.IdempotencyEntry{}}
}

// Find implementa application.IdempotencyLedger.
func (l *IdempotencyLedger) Find(_ context.Context, tenant string, subject domain.UUID, key string) (application.IdempotencyEntry, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.items[ledgerKey{tenant, subject, key}]
	return e, ok, nil
}

// Reserve implementa application.IdempotencyLedger.
func (l *IdempotencyLedger) Reserve(ctx context.Context, e application.IdempotencyEntry) error {
	k := ledgerKey{e.Tenant, e.Subject, e.Key}
	l.mu.Lock()
	_, taken := l.items[k]
	if !taken {
		l.items[k] = e
	}
	l.mu.Unlock()
	if taken {
		return fmt.Errorf("%w: idempotency key %s reserved concurrently", domain.ErrConflict, e.Key)
	}
	l.store.onRollback(ctx, func() {
		l.mu.Lock()
		delete(l.items, k)
		l.mu.Unlock()
	})
	return nil
}

// Remove implementa application.IdempotencyLedger.
func (l *IdempotencyLedger) Remove(ctx context.Context, tenant string, subject domain.UUID, key string) error {
	k := ledgerKey{tenant, subject, key}
	l.mu.Lock()
	e, ok := l.items[k]
	delete(l.items, k)
	l.mu.Unlock()
	if ok {
		l.store.onRollback(ctx, func() {
			l.mu.Lock()
			l.items[k] = e
			l.mu.Unlock()
		})
	}
	return nil
}

// Purge implementa application.IdempotencyLedger.
func (l *IdempotencyLedger) Purge(_ context.Context, before time.Time) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var n int64
	for k, e := range l.items {
		if e.ExpiresAt.Before(before) {
			delete(l.items, k)
			n++
		}
	}
	return n, nil
}
