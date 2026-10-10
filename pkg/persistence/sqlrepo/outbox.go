package sqlrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// DefaultOutboxTable is the default outbox table name. Each dialect package provides the DDL
// (OutboxDDL) for its engine.
const DefaultOutboxTable = "outbox_messages"

var outboxColumns = []string{
	"id", "event_type", "aggregate_type", "aggregate_id", "aggregate_version",
	"payload", "occurred_at", "correlation_id", "causation_id", "attempts", "last_error",
}

// Outbox is the SQL implementation of application.OutboxStore. Append joins the unit of work in
// ctx, so events commit atomically with the aggregate state.
type Outbox struct {
	db    *DB
	table string
}

// NewOutbox creates an outbox on db using table (DefaultOutboxTable when empty).
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

// Append inserts messages in the current unit of work.
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

// Pending returns unprocessed messages below maxAttempts, oldest first.
func (o *Outbox) Pending(ctx context.Context, limit, maxAttempts int) ([]application.OutboxMessage, error) {
	if limit <= 0 {
		limit = 100
	}
	b := &Builder{d: o.db.d}
	query := fmt.Sprintf("SELECT %s FROM %s o WHERE %s ORDER BY %s %s",
		o.selectList(), o.q(o.table), o.pending(b, "o", maxAttempts), o.order("o"), o.db.d.LimitOffset(limit, 0))
	return o.query(ctx, "pending", query, b.args)
}

func (o *Outbox) selectList() string {
	sel := make([]string, len(outboxColumns))
	for i, c := range outboxColumns {
		sel[i] = "o." + o.q(c)
	}
	return strings.Join(sel, ", ")
}

// pending is the condition of a message waiting to be relayed, for the table alias a.
func (o *Outbox) pending(b *Builder, a string, maxAttempts int) string {
	ph, _ := b.Arg(int64(maxAttempts))
	return fmt.Sprintf("%s.%s IS NULL AND %s.%s < %s", a, o.q("processed_at"), a, o.q("attempts"), ph)
}

func (o *Outbox) order(a string) string {
	return fmt.Sprintf("%s.%s, %s.%s", a, o.q("occurred_at"), a, o.q("id"))
}

// Claim implements application.OutboxClaimer with the row locks of each engine: FOR UPDATE SKIP
// LOCKED on PostgreSQL, MySQL and Oracle, and the UPDLOCK and READPAST hints on SQL Server.
// The locks last until the unit of work in ctx ends, also when the relay dies: nothing has to
// expire. The messages it does not return stay locked until then, which only makes the other
// relays skip them. SQLite has a single writer and no row locks, so there Claim is Pending.
func (o *Outbox) Claim(ctx context.Context, limit, maxAttempts int) ([]application.OutboxMessage, error) {
	if !o.db.InTx(ctx) {
		return nil, fmt.Errorf("sqlrepo: outbox claim outside a unit of work")
	}
	if limit <= 0 {
		limit = 100
	}
	// The read hint and the lock of each engine. NOLOCK because a plain read on SQL Server would
	// wait for the rows another relay already marked.
	read, hold, lock := "", "", " FOR UPDATE SKIP LOCKED"
	switch o.db.d.Name() {
	case "postgres", "oracle":
	case "mysql":
		// Through any other index (or a table scan) InnoDB locks the gaps it reads too, and the
		// publishers wait for the relay.
		hold = " FORCE INDEX (PRIMARY)"
	case "sqlserver":
		read, hold, lock = " WITH (NOLOCK)", " WITH (UPDLOCK, READPAST, ROWLOCK)", ""
	default:
		return o.Pending(ctx, limit, maxAttempts)
	}
	table := o.q(o.table)

	// 1. The oldest pending messages, whoever holds them, read without locking.
	b := &Builder{d: o.db.d}
	rows, err := o.db.executor(ctx).QueryContext(ctx, fmt.Sprintf("SELECT o.%s FROM %s o%s WHERE %s ORDER BY %s %s",
		o.q("id"), table, read, o.pending(b, "o", maxAttempts), o.order("o"), o.db.d.LimitOffset(limit, 0)), b.args...)
	if err != nil {
		return nil, fmt.Errorf("sqlrepo: outbox claim: %w", err)
	}
	var oldest []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("sqlrepo: outbox claim: %w", err)
		}
		oldest = append(oldest, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, fmt.Errorf("sqlrepo: outbox claim: %w", err)
	}
	if len(oldest) == 0 {
		return nil, nil
	}

	// 2. Lock them by primary key (a range scan would also lock the gaps on MySQL and hold the
	//    publishers up), skipping the ones another relay holds.
	b = &Builder{d: o.db.d}
	in := make([]string, len(oldest))
	for i, id := range oldest {
		in[i], _ = b.Arg(id)
	}
	msgs, err := o.query(ctx, "claim", fmt.Sprintf("SELECT %s FROM %s o%s WHERE o.%s IN (%s) AND %s ORDER BY %s%s",
		o.selectList(), table, hold, o.q("id"), strings.Join(in, ", "), o.pending(b, "o", maxAttempts), o.order("o"), lock), b.args)
	if err != nil {
		return nil, err
	}

	// 3. Keep what continues the outbox without a gap. Two relays claiming at once get the
	//    messages interleaved, and sending past a message the other one holds would overtake
	//    it. The relay that holds the oldest message always keeps it, so there is progress.
	n := 0
	for n < len(msgs) && msgs[n].ID == oldest[n] {
		n++
	}
	return msgs[:n], nil
}

func (o *Outbox) query(ctx context.Context, op, query string, args []any) ([]application.OutboxMessage, error) {
	rows, err := o.db.executor(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlrepo: outbox %s: %w", op, err)
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

// MarkProcessed flags a message as delivered.
func (o *Outbox) MarkProcessed(ctx context.Context, id string) error {
	b := &Builder{d: o.db.d}
	tsPh, _ := b.Arg(domain.Now())
	idPh, _ := b.Arg(id)
	query := fmt.Sprintf("UPDATE %s SET %s = %s WHERE %s = %s", o.q(o.table), o.q("processed_at"), tsPh, o.q("id"), idPh)
	_, err := o.db.executor(ctx).ExecContext(ctx, query, b.args...)
	return err
}

// MarkFailed increments the attempt counter and stores the error.
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

var (
	_ application.OutboxStore   = (*Outbox)(nil)
	_ application.OutboxClaimer = (*Outbox)(nil)
)
