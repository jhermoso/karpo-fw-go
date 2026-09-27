package sqlconformance

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/mysql"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/oracle"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlserver"
)

// InboxDDL returns the inbox DDL of a dialect name.
func InboxDDL(dialect string) []string {
	switch dialect {
	case "sqlite":
		return sqlite.InboxDDL("")
	case "postgres":
		return postgres.InboxDDL("")
	case "sqlserver":
		return sqlserver.InboxDDL("")
	case "oracle":
		return oracle.InboxDDL("")
	case "mysql":
		return mysql.InboxDDL("")
	}
	return nil
}

// RunInbox checks the SQL inbox on db: first claim, duplicate, per-consumer partitioning,
// and rollback together with the unit of work.
func RunInbox(t *testing.T, db *sqlrepo.DB) {
	ctx := context.Background()
	_, _ = db.ExecContext(ctx, "DROP TABLE "+sqlrepo.DefaultInboxTable)
	for _, s := range InboxDDL(db.Dialect().Name()) {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("inbox ddl: %v\n%s", err, s)
		}
	}
	inbox, err := sqlrepo.NewInbox(db, "")
	if err != nil {
		t.Fatal(err)
	}
	msg := domain.NewUUID().String()
	claim := func(ctx context.Context, consumer string) bool {
		t.Helper()
		first, err := inbox.Claim(ctx, consumer, msg)
		if err != nil {
			t.Fatal(err)
		}
		return first
	}

	boom := errors.New("boom")
	if err := db.Do(ctx, func(ctx context.Context) error {
		if !claim(ctx, "billing") {
			t.Fatal("first claim")
		}
		return boom
	}); !errors.Is(err, boom) {
		t.Fatalf("expected rollback, got %v", err)
	}
	if !claim(ctx, "billing") {
		t.Fatal("a rolled back claim must be forgotten")
	}
	if claim(ctx, "billing") {
		t.Fatal("a committed claim must reject the duplicate")
	}
	if !claim(ctx, "crm") {
		t.Fatal("each consumer has its own inbox")
	}

}
