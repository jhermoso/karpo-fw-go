package sqlrepo

import (
	"context"
	"fmt"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// DefaultInboxTable es el nombre por defecto de la tabla del inbox. Cada paquete de dialecto
// aporta el DDL (InboxDDL) de su motor.
const DefaultInboxTable = "inbox_messages"

// Inbox es la implementación SQL de application.InboxStore: una fila por (consumidor, id de
// mensaje), insertada en la unidad de trabajo del consumidor.
type Inbox struct {
	db    *DB
	table string
}

var _ application.InboxStore = (*Inbox)(nil)

// NewInbox crea un inbox sobre db con la tabla table (DefaultInboxTable si está vacía).
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

// Claim implementa application.InboxStore. Comprueba antes de insertar en lugar de fiarse de la
// restricción, porque en PostgreSQL una sentencia fallida aborta la transacción entera. Dos
// entregas simultáneas del mismo mensaje siguen chocando en la clave primaria: la que pierde
// recibe domain.ErrConflict, hace rollback, y su reentrega se salta después como duplicada.
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
