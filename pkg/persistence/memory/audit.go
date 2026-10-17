package memory

import (
	"context"
	"slices"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
)

// AuditLog es un application.AuditLog en memoria ligado a la unidad de trabajo de un Store.
type AuditLog struct {
	store   *Store
	mu      sync.Mutex
	records []application.AuditRecord
}

// NewAuditLog crea un registro de auditoría sobre store.
func NewAuditLog(store *Store) *AuditLog { return &AuditLog{store: store} }

// Append añade registros; se vuelven a quitar si la unidad de trabajo hace rollback.
func (l *AuditLog) Append(ctx context.Context, records ...application.AuditRecord) error {
	if l.store.closed.Load() {
		return ErrClosed
	}
	l.mu.Lock()
	n := len(l.records)
	l.records = append(l.records, records...)
	l.mu.Unlock()
	l.store.onRollback(ctx, func() {
		l.mu.Lock()
		l.records = l.records[:n]
		l.mu.Unlock()
	})
	return nil
}

// Trail devuelve los registros de un agregado en el orden en que se insertaron.
func (l *AuditLog) Trail(_ context.Context, aggregateType, aggregateID string) ([]application.AuditRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.DeleteFunc(slices.Clone(l.records), func(r application.AuditRecord) bool {
		return r.AggregateType != aggregateType || r.AggregateID != aggregateID
	}), nil
}

var _ application.AuditLog = (*AuditLog)(nil)
