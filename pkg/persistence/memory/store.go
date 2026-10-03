// Package memory provides in-memory adapters that honour the same contracts as the database
// adapters: repositories with optimistic concurrency and specification support, a unit of work
// with rollback, an outbox store and an idempotency store. Use it for tests, prototypes and as a
// hot-swappable backend.
//
// Specifications are evaluated with IsSatisfiedBy: for this adapter memory *is* the store, so
// that is the native execution, not a fallback.
package memory

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// ErrClosed is returned when using a closed Store.
var ErrClosed = errors.New("memory: store closed")

// Store is an in-memory database: it provides the unit of work shared by the repositories and
// outbox created on it. Units of work are serialized (one at a time) and roll back every change
// made through this store's adapters when they fail.
type Store struct {
	name   string
	txMu   sync.Mutex
	closed atomic.Bool
}

type storeKey struct{ s *Store }

type memTx struct {
	undo []func()
}

// NewStore creates an empty store.
func NewStore(name string) *Store {
	if name == "" {
		name = "memory"
	}
	return &Store{name: name}
}

// Name identifies the store (used by hot-swap diagnostics).
func (s *Store) Name() string { return s.name }

// Close marks the store closed; further units of work fail with ErrClosed.
func (s *Store) Close() error {
	s.closed.Store(true)
	return nil
}

// Do runs fn as a unit of work. Nested calls join the outer unit of work.
func (s *Store) Do(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if s.closed.Load() {
		return ErrClosed
	}
	if s.tx(ctx) != nil {
		return fn(ctx)
	}
	s.txMu.Lock()
	defer s.txMu.Unlock()

	tx := &memTx{}
	ctx = context.WithValue(ctx, storeKey{s}, tx)
	defer func() {
		if r := recover(); r != nil {
			tx.rollback()
			panic(r)
		}
	}()
	if err = fn(ctx); err != nil {
		tx.rollback()
	}
	return err
}

func (s *Store) tx(ctx context.Context) *memTx {
	tx, _ := ctx.Value(storeKey{s}).(*memTx)
	return tx
}

// onRollback registers undo for the unit of work in ctx (no-op outside a unit of work).
func (s *Store) onRollback(ctx context.Context, undo func()) {
	if tx := s.tx(ctx); tx != nil {
		tx.undo = append(tx.undo, undo)
	}
}

func (t *memTx) rollback() {
	for i := len(t.undo) - 1; i >= 0; i-- {
		t.undo[i]()
	}
	t.undo = nil
}
