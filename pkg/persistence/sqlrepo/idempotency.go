package sqlrepo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// idempotencyColumns son las columnas de un registro de idempotencia, iguales en los tres
// proyectos (docs/DECISIONES-COMUNES.md, D-125).
var idempotencyColumns = []string{"tenant_id", "subject_id", "idempotency_key", "request_hash", "resource_id",
	"response_status", "created_at", "expires_at"}

// IdempotencyLedger es la implementación SQL de application.IdempotencyLedger, sobre la tabla de un
// contexto.
type IdempotencyLedger struct {
	db    *DB
	table string
}

var _ application.IdempotencyLedger = (*IdempotencyLedger)(nil)

// NewIdempotencyLedger crea un registro sobre db con la tabla table.
func NewIdempotencyLedger(db *DB, table string) (*IdempotencyLedger, error) {
	if err := checkIdent("table", table); err != nil {
		return nil, err
	}
	return &IdempotencyLedger{db: db, table: table}, nil
}

// Find implementa application.IdempotencyLedger.
func (l *IdempotencyLedger) Find(ctx context.Context, tenant string, subject domain.UUID, key string) (application.IdempotencyEntry, bool, error) {
	var out application.IdempotencyEntry
	b := &Builder{d: l.db.d}
	tp, err := b.Arg(tenant)
	if err != nil {
		return out, false, err
	}
	sp, err := b.Arg(subject)
	if err != nil {
		return out, false, err
	}
	kp, err := b.Arg(key)
	if err != nil {
		return out, false, err
	}
	cols := make([]string, len(idempotencyColumns))
	for i, c := range idempotencyColumns {
		cols[i] = l.db.d.Quote(c)
	}
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s AND %s = %s AND %s = %s", strings.Join(cols, ", "), l.db.d.Quote(l.table),
		l.db.d.Quote("tenant_id"), tp, l.db.d.Quote("subject_id"), sp, l.db.d.Quote("idempotency_key"), kp)
	rows, err := l.db.executor(ctx).QueryContext(ctx, query, b.args...)
	if err != nil {
		return out, false, fmt.Errorf("sqlrepo: idempotency lookup: %w", err)
	}
	found, err := scanAll(l.db.d, rows, idempotencyColumns)
	if err != nil || len(found) == 0 {
		return out, false, err
	}
	r := found[0]
	out = application.IdempotencyEntry{Tenant: r.String("tenant_id"), Subject: r.UUID("subject_id"), Key: r.String("idempotency_key"),
		RequestHash: r.String("request_hash"), ResourceID: r.UUID("resource_id"), Status: int(r.Int64("response_status")),
		CreatedAt: r.Time("created_at"), ExpiresAt: r.Time("expires_at")}
	return out, true, r.Err()
}

// Reserve implementa application.IdempotencyLedger.
func (l *IdempotencyLedger) Reserve(ctx context.Context, e application.IdempotencyEntry) error {
	err := l.db.Insert(ctx, l.table, Values{"id": domain.NewUUID(), "tenant_id": e.Tenant, "subject_id": e.Subject,
		"idempotency_key": e.Key, "request_hash": e.RequestHash, "resource_id": e.ResourceID,
		"response_status": int64(e.Status), "created_at": e.CreatedAt, "expires_at": e.ExpiresAt})
	if l.db.d.IsUniqueViolation(err) {
		return fmt.Errorf("%w: idempotency key %s reserved concurrently", domain.ErrConflict, e.Key)
	}
	if err != nil {
		return fmt.Errorf("sqlrepo: idempotency reserve: %w", err)
	}
	return nil
}

// Remove implementa application.IdempotencyLedger.
func (l *IdempotencyLedger) Remove(ctx context.Context, tenant string, subject domain.UUID, key string) error {
	_, err := l.db.Delete(ctx, l.table, Values{"tenant_id": tenant, "subject_id": subject, "idempotency_key": key})
	return err
}

// Purge implementa application.IdempotencyLedger.
func (l *IdempotencyLedger) Purge(ctx context.Context, before time.Time) (int64, error) {
	b := &Builder{d: l.db.d}
	bp, err := b.Arg(before)
	if err != nil {
		return 0, err
	}
	res, err := l.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s < %s", l.db.d.Quote(l.table), l.db.d.Quote("expires_at"), bp), b.args...)
	if err != nil {
		return 0, fmt.Errorf("sqlrepo: idempotency purge: %w", err)
	}
	return res.RowsAffected()
}
