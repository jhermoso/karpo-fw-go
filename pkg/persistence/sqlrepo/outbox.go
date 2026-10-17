package sqlrepo

import (
	"context"
	"fmt"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// DefaultOutboxTable es el nombre por defecto de la tabla del outbox. Cada paquete de dialecto
// aporta el DDL (OutboxDDL) de su motor.
const DefaultOutboxTable = "outbox_messages"

var outboxColumns = []string{
	"id", "event_type", "aggregate_type", "aggregate_id", "aggregate_version",
	"payload", "occurred_at", "correlation_id", "causation_id", "attempts", "last_error",
}

// Outbox es la implementación SQL de application.OutboxStore. Append se une a la unidad de
// trabajo de ctx, así que los eventos hacen commit de forma atómica con el estado del agregado.
type Outbox struct {
	db    *DB
	table string
}

// NewOutbox crea un outbox sobre db con la tabla table (DefaultOutboxTable si está vacía).
func NewOutbox(db *DB, table string) (*Outbox, error) {
	if table == "" {
		table = DefaultOutboxTable
	}
	if err := checkIdent("table", table); err != nil {
		return nil, err
	}
	return &Outbox{db: db, table: table}, nil
}

func (o *Outbox) q(s string) string { return o.db.d.Quote(s) }

// Append inserta los mensajes en la unidad de trabajo en curso.
func (o *Outbox) Append(ctx context.Context, msgs ...application.OutboxMessage) error {
	if len(msgs) == 0 {
		return nil
	}
	cols := make([]string, len(outboxColumns))
	for i, c := range outboxColumns {
		cols[i] = o.q(c)
	}
	return o.db.Do(ctx, func(ctx context.Context) error {
		for _, m := range msgs {
			b := &Builder{d: o.db.d}
			vals := []any{
				m.ID, m.EventType, m.AggregateType, m.AggregateID, m.AggregateVersion,
				string(m.Payload), m.OccurredAt, m.CorrelationID, m.CausationID, int64(m.Attempts), m.LastError,
			}
			phs := make([]string, len(vals))
			for i, v := range vals {
				ph, err := b.Arg(v)
				if err != nil {
					return err
				}
				phs[i] = ph
			}
			query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", o.q(o.table), strings.Join(cols, ", "), strings.Join(phs, ", "))
			if _, err := o.db.executor(ctx).ExecContext(ctx, query, b.args...); err != nil {
				return fmt.Errorf("sqlrepo: outbox append: %w", err)
			}
		}
		return nil
	})
}

// Pending devuelve los mensajes sin procesar por debajo de maxAttempts, del más antiguo al más
// reciente.
func (o *Outbox) Pending(ctx context.Context, limit, maxAttempts int) ([]application.OutboxMessage, error) {
	if limit <= 0 {
		limit = 100
	}
	b := &Builder{d: o.db.d}
	ph, _ := b.Arg(int64(maxAttempts))
	sel := make([]string, len(outboxColumns))
	for i, c := range outboxColumns {
		sel[i] = "o." + o.q(c)
	}
	query := fmt.Sprintf("SELECT %s FROM %s o WHERE o.%s IS NULL AND o.%s < %s ORDER BY o.%s, o.%s %s",
		strings.Join(sel, ", "), o.q(o.table), o.q("processed_at"), o.q("attempts"), ph,
		o.q("occurred_at"), o.q("id"), o.db.d.LimitOffset(limit, 0))
	rows, err := o.db.executor(ctx).QueryContext(ctx, query, b.args...)
	if err != nil {
		return nil, fmt.Errorf("sqlrepo: outbox pending: %w", err)
	}
	scanned, err := scanAll(o.db.d, rows, outboxColumns)
	if err != nil {
		return nil, fmt.Errorf("sqlrepo: outbox scan: %w", err)
	}
	out := make([]application.OutboxMessage, 0, len(scanned))
	for _, r := range scanned {
		m := application.OutboxMessage{
			ID:               r.String("id"),
			EventType:        r.String("event_type"),
			AggregateType:    r.String("aggregate_type"),
			AggregateID:      r.String("aggregate_id"),
			AggregateVersion: r.Int64("aggregate_version"),
			Payload:          r.Bytes("payload"),
			OccurredAt:       r.Time("occurred_at"),
			CorrelationID:    r.String("correlation_id"),
			CausationID:      r.String("causation_id"),
			Attempts:         int(r.Int64("attempts")),
			LastError:        r.String("last_error"),
		}
		if err := r.Err(); err != nil {
			return nil, fmt.Errorf("sqlrepo: outbox scan: %w", err)
		}
		out = append(out, m)
	}
	return out, nil
}

// MarkProcessed marca un mensaje como entregado.
func (o *Outbox) MarkProcessed(ctx context.Context, id string) error {
	b := &Builder{d: o.db.d}
	tsPh, _ := b.Arg(domain.Now())
	idPh, _ := b.Arg(id)
	query := fmt.Sprintf("UPDATE %s SET %s = %s WHERE %s = %s", o.q(o.table), o.q("processed_at"), tsPh, o.q("id"), idPh)
	_, err := o.db.executor(ctx).ExecContext(ctx, query, b.args...)
	return err
}

// MarkFailed incrementa el contador de intentos y guarda el error.
func (o *Outbox) MarkFailed(ctx context.Context, id string, cause error) error {
	msg := ""
	if cause != nil {
		msg = cause.Error()
		if len(msg) > 1000 {
			msg = msg[:1000]
		}
	}
	b := &Builder{d: o.db.d}
	errPh, _ := b.Arg(msg)
	idPh, _ := b.Arg(id)
	query := fmt.Sprintf("UPDATE %s SET %s = %s + 1, %s = %s WHERE %s = %s",
		o.q(o.table), o.q("attempts"), o.q("attempts"), o.q("last_error"), errPh, o.q("id"), idPh)
	_, err := o.db.executor(ctx).ExecContext(ctx, query, b.args...)
	return err
}

var _ application.OutboxStore = (*Outbox)(nil)
