// Package hotswap lets a running service switch its persistence backend (e.g. SQL Server ->
// PostgreSQL, or one server to another) without restarting and without breaking in-flight work.
//
// How it works:
//   - a Switch holds the current Backend (anything that provides a unit of work and can be closed:
//     *sqlrepo.DB, *memory.Store, ...);
//   - every operation leases the current backend for its duration; a unit of work pins its
//     backend in the context, so all repository calls inside one transaction hit the same
//     database even if a swap happens meanwhile;
//   - Swap routes new operations to the new backend immediately, waits for the leases on the old
//     one to drain and then closes it;
//   - Bind / Repository / Outbox build adapters lazily per backend through a factory, so the
//     same domain.Repository value keeps working across swaps.
//
// Moving the data itself (replication, dual writes, backfill) is an operational concern outside
// this package: Swap switches connections atomically and safely.
package hotswap

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Backend is a swappable persistence backend.
type Backend interface {
	domain.UnitOfWork
	Name() string
	Close() error
}

// ErrClosed is returned after the Switch is closed.
var ErrClosed = errors.New("hotswap: switch closed")

type generation struct {
	id      uint64
	backend Backend

	mu       sync.Mutex
	refs     int
	draining bool
	drained  chan struct{}
}

func (g *generation) release() {
	g.mu.Lock()
	g.refs--
	done := g.draining && g.refs == 0
	g.mu.Unlock()
	if done {
		close(g.drained)
	}
}

// Switch routes operations to the current backend and swaps backends safely.
type Switch struct {
	mu      sync.RWMutex
	current *generation
	seq     uint64
	closed  bool

	hooksMu  sync.Mutex
	onRetire []func(genID uint64)
	onSwap   []func(old, next Backend)
}

type pinKey struct{ s *Switch }

// New creates a Switch serving initial.
func New(initial Backend) *Switch {
	s := &Switch{}
	s.seq++
	s.current = &generation{id: s.seq, backend: initial, drained: make(chan struct{})}
	return s
}

// Current returns the backend new operations are routed to.
func (s *Switch) Current() Backend {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.backend
}

// Name returns the name of the current backend.
func (s *Switch) Name() string { return s.Current().Name() }

// OnSwap registers a callback invoked after each successful routing change.
func (s *Switch) OnSwap(fn func(old, next Backend)) {
	s.hooksMu.Lock()
	s.onSwap = append(s.onSwap, fn)
	s.hooksMu.Unlock()
}

// acquire leases the backend pinned in ctx, or the current one.
func (s *Switch) acquire(ctx context.Context) (*generation, error) {
	if g, ok := ctx.Value(pinKey{s}).(*generation); ok {
		g.mu.Lock()
		g.refs++
		g.mu.Unlock()
		return g, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, ErrClosed
	}
	g := s.current
	g.mu.Lock()
	g.refs++
	g.mu.Unlock()
	return g, nil
}

// Do runs fn as a unit of work on the current backend, pinning that backend for every
// operation performed with the context passed to fn.
func (s *Switch) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	g, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer g.release()
	ctx = context.WithValue(ctx, pinKey{s}, g)
	return g.backend.Do(ctx, fn)
}

// Swap routes new operations to next, waits (bounded by ctx) until operations leased on the
// previous backend finish, and closes it. If ctx expires first, Swap returns ctx.Err() but the
// routing change stays in effect and the previous backend is closed as soon as it drains.
func (s *Switch) Swap(ctx context.Context, next Backend) error {
	if next == nil {
		return fmt.Errorf("hotswap: nil backend")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	if s.current.backend == next {
		s.mu.Unlock()
		return fmt.Errorf("hotswap: backend %q is already current", next.Name())
	}
	old := s.current
	s.seq++
	s.current = &generation{id: s.seq, backend: next, drained: make(chan struct{})}
	s.mu.Unlock()

	s.hooksMu.Lock()
	hooks := slices.Clone(s.onSwap)
	s.hooksMu.Unlock()
	for _, h := range hooks {
		h(old.backend, next)
	}

	return s.retire(ctx, old)
}

func (s *Switch) retire(ctx context.Context, g *generation) error {
	g.mu.Lock()
	g.draining = true
	if g.refs == 0 {
		close(g.drained)
	}
	g.mu.Unlock()

	finish := func() error {
		err := g.backend.Close()
		s.hooksMu.Lock()
		hooks := slices.Clone(s.onRetire)
		s.hooksMu.Unlock()
		for _, h := range hooks {
			h(g.id)
		}
		return err
	}

	select {
	case <-g.drained:
		return finish()
	case <-ctx.Done():
		go func() {
			<-g.drained
			_ = finish()
		}()
		return ctx.Err()
	}
}

// Close drains and closes the current backend; further operations fail with ErrClosed.
func (s *Switch) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	g := s.current
	s.mu.Unlock()
	return s.retire(ctx, g)
}

// Binding lazily builds one adapter X per backend generation (repositories, outboxes, read
// models...) and runs operations with the right one.
type Binding[X any] struct {
	s       *Switch
	factory func(Backend) (X, error)
	mu      sync.Mutex
	cache   map[uint64]X
}

// Bind creates a Binding; factory is called once per backend (typically a type switch on the
// concrete backend: *sqlrepo.DB, *memory.Store, ...).
func Bind[X any](s *Switch, factory func(Backend) (X, error)) *Binding[X] {
	b := &Binding[X]{s: s, factory: factory, cache: make(map[uint64]X)}
	s.hooksMu.Lock()
	s.onRetire = append(s.onRetire, func(id uint64) {
		b.mu.Lock()
		delete(b.cache, id)
		b.mu.Unlock()
	})
	s.hooksMu.Unlock()
	return b
}

// With leases the backend for ctx (pinned or current), resolves its adapter and runs fn.
func (b *Binding[X]) With(ctx context.Context, fn func(ctx context.Context, x X) error) error {
	g, err := b.s.acquire(ctx)
	if err != nil {
		return err
	}
	defer g.release()

	b.mu.Lock()
	x, ok := b.cache[g.id]
	if !ok {
		if x, err = b.factory(g.backend); err != nil {
			b.mu.Unlock()
			return fmt.Errorf("hotswap: building adapter for backend %q: %w", g.backend.Name(), err)
		}
		b.cache[g.id] = x
	}
	b.mu.Unlock()

	return fn(context.WithValue(ctx, pinKey{b.s}, g), x)
}
