// Package hotswap permite a un servicio en marcha cambiar su backend de persistencia (p. ej. SQL
// Server -> PostgreSQL, o de un servidor a otro) sin reiniciar y sin romper el trabajo en curso.
//
// Cómo funciona:
//   - un Switch guarda el Backend actual (cualquier cosa que ofrezca una unidad de trabajo y se
//     pueda cerrar: *sqlrepo.DB, *memory.Store, ...);
//   - cada operación toma prestado el backend actual mientras dura; una unidad de trabajo fija su
//     backend en el contexto, de modo que todas las llamadas a repositorios dentro de una
//     transacción van a la misma base de datos aunque entretanto haya un cambio;
//   - Swap dirige las operaciones nuevas al backend nuevo de inmediato, espera a que terminen los
//     préstamos del antiguo y después lo cierra;
//   - Bind / Repository / Outbox construyen los adaptadores de forma perezosa por backend mediante
//     una factoría, de modo que el mismo valor domain.Repository sigue funcionando tras los cambios.
//
// Mover los datos en sí (replicación, escrituras dobles, relleno) es un asunto operativo fuera de
// este paquete: Swap cambia las conexiones de forma atómica y segura.
package hotswap

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Backend es un backend de persistencia intercambiable.
type Backend interface {
	domain.UnitOfWork
	Name() string
	Close() error
}

// ErrClosed se devuelve después de cerrar el Switch.
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

// Switch dirige las operaciones al backend actual y cambia de backend con seguridad.
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

// New crea un Switch que sirve initial.
func New(initial Backend) *Switch {
	s := &Switch{}
	s.seq++
	s.current = &generation{id: s.seq, backend: initial, drained: make(chan struct{})}
	return s
}

// Current devuelve el backend al que se dirigen las operaciones nuevas.
func (s *Switch) Current() Backend {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.backend
}

// Name devuelve el nombre del backend actual.
func (s *Switch) Name() string { return s.Current().Name() }

// OnSwap registra una función que se llama después de cada cambio de destino con éxito.
func (s *Switch) OnSwap(fn func(old, next Backend)) {
	s.hooksMu.Lock()
	s.onSwap = append(s.onSwap, fn)
	s.hooksMu.Unlock()
}

// acquire toma prestado el backend fijado en ctx, o el actual.
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

// Do ejecuta fn como unidad de trabajo en el backend actual, y fija ese backend para todas las
// operaciones que se hagan con el contexto que recibe fn.
func (s *Switch) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	g, err := s.acquire(ctx)
	if err != nil {
		return err
	}
	defer g.release()
	ctx = context.WithValue(ctx, pinKey{s}, g)
	return g.backend.Do(ctx, fn)
}

// Swap dirige las operaciones nuevas a next, espera (con el límite de ctx) a que terminen las
// operaciones que tienen prestado el backend anterior, y lo cierra. Si ctx vence antes, Swap
// devuelve ctx.Err(), pero el cambio de destino sigue en vigor y el backend anterior se cierra en
// cuanto se vacía.
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

// Close vacía y cierra el backend actual; las operaciones posteriores fallan con ErrClosed.
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

// Binding construye de forma perezosa un adaptador X por generación de backend (repositorios,
// outboxes, modelos de lectura...) y ejecuta las operaciones con el que corresponde.
type Binding[X any] struct {
	s       *Switch
	factory func(Backend) (X, error)
	mu      sync.Mutex
	cache   map[uint64]X
}

// Bind crea un Binding; factory se llama una vez por backend (normalmente un type switch sobre el
// backend concreto: *sqlrepo.DB, *memory.Store, ...).
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

// With toma prestado el backend de ctx (el fijado o el actual), obtiene su adaptador y ejecuta fn.
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
