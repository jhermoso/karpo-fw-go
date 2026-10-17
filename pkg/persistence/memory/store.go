// Package memory ofrece adaptadores en memoria que cumplen los mismos contratos que los
// adaptadores de base de datos: repositorios con concurrencia optimista y soporte de
// especificaciones, una unidad de trabajo con rollback, un almacén de outbox y un almacén de
// idempotencia. Sirve para pruebas, prototipos y como backend intercambiable en caliente.
//
// Las especificaciones se evalúan con IsSatisfiedBy: para este adaptador la memoria *es* el
// almacén, así que ésa es la ejecución nativa, no un recurso de reserva.
package memory

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// ErrClosed se devuelve al usar un Store cerrado.
var ErrClosed = errors.New("memory: store closed")

// Store es una base de datos en memoria: ofrece la unidad de trabajo que comparten los
// repositorios y el outbox creados sobre él. Las unidades de trabajo van en serie (una cada vez)
// y, cuando fallan, deshacen todos los cambios hechos a través de los adaptadores de este almacén.
type Store struct {
	name   string
	txMu   sync.Mutex
	closed atomic.Bool
}

type storeKey struct{ s *Store }

type memTx struct {
	undo []func()
}

// NewStore crea un almacén vacío.
func NewStore(name string) *Store {
	if name == "" {
		name = "memory"
	}
	return &Store{name: name}
}

// Name identifica el almacén (lo usan los diagnósticos del cambio en caliente).
func (s *Store) Name() string { return s.name }

// Close marca el almacén como cerrado; las unidades de trabajo posteriores fallan con ErrClosed.
func (s *Store) Close() error {
	s.closed.Store(true)
	return nil
}

// Do ejecuta fn como unidad de trabajo. Las llamadas anidadas se unen a la unidad de trabajo exterior.
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

// onRollback registra undo para la unidad de trabajo de ctx (no hace nada fuera de una unidad de
// trabajo).
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
