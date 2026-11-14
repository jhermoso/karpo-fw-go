package approvals_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/approval"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/approvals"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

const table = "pur_approvals"

func TestDDL_RendersInEveryDialect(t *testing.T) {
	for _, d := range sqlrepo.Dialects {
		for _, s := range approvals.DDL(table) {
			if _, err := sqlrepo.RenderDDLChecked(d, s); err != nil {
				t.Errorf("%s: %v", d, err)
			}
		}
	}
}

func TestLongTableName_Panics(t *testing.T) {
	for name, fn := range map[string]func(){
		"ddl":     func() { approvals.DDL("purchases_approval_requests") },
		"mapping": func() { approvals.Mapping("purchases_approval_requests") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: expected panic", name)
				}
			}()
			fn()
		}()
	}
	approvals.DDL("abcdefghijklmnopqrstuvw") // 23: vale
}

func TestRoundTrip_SQLite(t *testing.T) {
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	db := sqlite.Open(raw)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	for _, s := range approvals.DDL(table) {
		if _, err := db.ExecContext(ctx, sqlrepo.RenderDDL("sqlite", s)); err != nil {
			t.Fatal(err)
		}
	}
	repo := sqlrepo.MustRepository(db, approvals.Mapping(table))
	requester, approver := domain.NewUUID(), domain.NewUUID()
	r, err := approval.New(approval.NewRequestID(), domain.NewUUID(), "purchases.supplier.block",
		approval.Target{Type: "purchases.supplier", ID: "42"}, []byte(`{"reason":"impago"}`), "impago", requester)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, r); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, r.ID())
	if err != nil {
		t.Fatal(err)
	}
	if s := got.State(); s.Status != approval.StatusPending || !s.ResolvedBy.IsZero() || string(s.Payload) != `{"reason":"impago"}` {
		t.Fatalf("state = %#v", s)
	}
	if err := got.Approve(approver, "ok"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, got); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Get(ctx, r.ID())
	if s := got.State(); s.Status != approval.StatusApproved || s.ResolvedBy != approver || s.Comment != "ok" || got.Version() != 2 {
		t.Fatalf("state = %#v v%d", s, got.Version())
	}
}
