package memory

import (
	"context"
	"slices"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
)

// AuditLog is an in-memory application.AuditLog bound to a Store's unit of work.
type AuditLog struct {
	store   *Store
	mu      sync.Mutex
	records []application.AuditRecord
}

// NewAuditLog creates an audit log on store.
func NewAuditLog(store *Store) *AuditLog { return &AuditLog{store: store} }

// Append adds records; they are removed again if the unit of work rolls back.
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

// Trail returns the records of one aggregate in insertion order.
func (l *AuditLog) Trail(_ context.Context, aggregateType, aggregateID string) ([]application.AuditRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.DeleteFunc(slices.Clone(l.records), func(r application.AuditRecord) bool {
		return r.AggregateType != aggregateType || r.AggregateID != aggregateID
	}), nil
}

var _ application.AuditLog = (*AuditLog)(nil)
