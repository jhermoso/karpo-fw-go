package sqlrepo

import (
	"context"
	"fmt"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// DefaultInboxTable is the default inbox table name. Each dialect package provides the DDL
// (InboxDDL) for its engine.
const DefaultInboxTable = "inbox_messages"

// Inbox is the SQL implementation of application.InboxStore: one row per (consumer, message id),
// inserted in the consumer's unit of work.
type Inbox struct {
	db    *DB
	table string
}

var _ application.InboxStore = (*Inbox)(nil)

// NewInbox creates an inbox on db using table (DefaultInboxTable when empty).
func NewInbox(db *DB, table string) (*Inbox, error) {
	if table == "" {
		table = DefaultInboxTable
	}
	if err := checkIdent("table", table); err != nil {
		return nil, err
	}
	return &Inbox{db: db, table: table}, nil
}

func (i *Inbox) q(s string) string { return i.db.d.Quote(s) }

// Claim implements application.InboxStore. It checks before inserting instead of relying on
// the constraint, because a failed statement aborts the whole transaction in PostgreSQL. Two
// concurrent deliveries of the same message still collide on the primary key: the loser gets
// domain.ErrConflict, rolls back, and its redelivery is then skipped as a duplicate.
func (i *Inbox) Claim(ctx context.Context, consumer, messageID string) (first bool, err error) {
	err = i.db.Do(ctx, func(ctx context.Context) error {
		b := &Builder{d: i.db.d}
		cp, _ := b.Arg(consumer)
		mp, _ := b.Arg(messageID)
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = %s AND %s = %s",
			i.q(i.table), i.q("consumer"), cp, i.q("message_id"), mp)
		var n int64
		if err := i.db.executor(ctx).QueryRowContext(ctx, query, b.args...).Scan(&n); err != nil {
			return fmt.Errorf("sqlrepo: inbox lookup: %w", err)
		}
		if n > 0 {
			return nil
		}
		b = &Builder{d: i.db.d}
		cp, _ = b.Arg(consumer)
		mp, _ = b.Arg(messageID)
		tp, err := b.Arg(domain.Now())
		if err != nil {
			return err
		}
		insert := fmt.Sprintf("INSERT INTO %s (%s, %s, %s) VALUES (%s, %s, %s)", i.q(i.table),
			i.q("consumer"), i.q("message_id"), i.q("processed_at"), cp, mp, tp)
		if _, err := i.db.executor(ctx).ExecContext(ctx, insert, b.args...); err != nil {
			if i.db.d.IsUniqueViolation(err) {
				return fmt.Errorf("%w: message %s claimed concurrently by %s", domain.ErrConflict, messageID, consumer)
			}
			return fmt.Errorf("sqlrepo: inbox claim: %w", err)
		}
		first = true
		return nil
	})
	return first, err
}
