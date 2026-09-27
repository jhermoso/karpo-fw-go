package sqlconformance

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/mysql"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/oracle"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlserver"
)

// AuditDDL returns the audit log DDL of a dialect name.
func AuditDDL(dialect string) []string {
	switch dialect {
	case "sqlite":
		return sqlite.AuditDDL("")
	case "postgres":
		return postgres.AuditDDL("")
	case "sqlserver":
		return sqlserver.AuditDDL("")
	case "oracle":
		return oracle.AuditDDL("")
	case "mysql":
		return mysql.AuditDDL("")
	}
	return nil
}

// RunAuditLog checks the SQL audit log on db: round trip of every field, ordering, and
// rollback together with the unit of work.
func RunAuditLog(t *testing.T, db *sqlrepo.DB) {
	ctx := context.Background()
	_, _ = db.ExecContext(ctx, "DROP TABLE "+sqlrepo.DefaultAuditTable)
	for _, s := range AuditDDL(db.Dialect().Name()) {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("audit ddl: %v\n%s", err, s)
		}
	}
	log, err := sqlrepo.NewAuditLog(db, "")
	if err != nil {
		t.Fatal(err)
	}

	ana, _ := vocab.NewActor(domain.NewUUID(), "Ana")
	id := domain.NewUUID().String()
	t0 := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)
	first := application.AuditRecord{
		ID: domain.NewUUID().String(), AggregateType: "test.account", AggregateID: id, AggregateVersion: 1,
		Operation: application.AuditCreated, Actor: ana, Channel: application.ChannelWeb, CorrelationID: "corr-1", At: t0,
		Changes: []traits.FieldChange{{Field: "limit", New: "1000"}, {Field: "name", New: "Main"}},
		Events:  []string{"test.account_opened"},
	}
	second := application.AuditRecord{
		ID: domain.NewUUID().String(), AggregateType: "test.account", AggregateID: id, AggregateVersion: 2,
		Operation: application.AuditUpdated, Actor: vocab.SystemActor, At: t0.Add(time.Minute),
		Import: &application.ImportProvenance{SourceKey: "legacy-crm", RunID: domain.NewUUID(), SourceFile: "a.csv"},
	}

	boom := errors.New("boom")
	if err := db.Do(ctx, func(ctx context.Context) error {
		if err := log.Append(ctx, first); err != nil {
			return err
		}
		return boom
	}); !errors.Is(err, boom) {
		t.Fatalf("expected rollback, got %v", err)
	}
	if trail, _ := log.Trail(ctx, "test.account", id); len(trail) != 0 {
		t.Fatalf("rolled back records must not persist: %d", len(trail))
	}

	if err := log.Append(ctx, second, first); err != nil {
		t.Fatal(err)
	}
	trail, err := log.Trail(ctx, "test.account", id)
	if err != nil {
		t.Fatal(err)
	}
	if len(trail) != 2 || trail[0].ID != first.ID || trail[1].ID != second.ID {
		t.Fatalf("trail must be ordered by time: %+v", trail)
	}
	got := trail[0]
	if got.Actor != ana || got.Channel != "web" || got.CorrelationID != "corr-1" || !got.At.Equal(t0) ||
		got.AggregateVersion != 1 || len(got.Changes) != 2 || got.Changes[0].Field != "limit" ||
		fmt.Sprint(got.Changes[0].New) != "1000" || len(got.Events) != 1 || got.Import != nil {
		t.Fatalf("round trip of the first record: %+v", got)
	}
	imp := trail[1]
	if imp.Actor != vocab.SystemActor || imp.Import == nil || imp.Import.SourceKey != "legacy-crm" ||
		imp.Import.RunID != second.Import.RunID || imp.Import.SourceFile != "a.csv" || len(imp.Changes) != 0 || imp.Channel != "" {
		t.Fatalf("round trip of the import record: %+v", imp)
	}
}
