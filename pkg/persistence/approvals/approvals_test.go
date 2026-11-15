package approvals_test

import (
	"context"
	"database/sql"
	"errors"
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
		for _, s := range append(approvals.DDL(table), approvals.PendingKeyDDL(table)...) {
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
	approvals.DDL("abcdefghijklmnopqrs") // 19: vale; "ux_<tabla>_pending" mide 30
}

func openSQLite(t *testing.T, ddl ...string) *sqlrepo.DB {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	db := sqlite.Open(raw)
	t.Cleanup(func() { _ = db.Close() })
	for _, s := range ddl {
		if _, err := db.ExecContext(context.Background(), sqlrepo.RenderDDL("sqlite", s)); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func newRequest(t *testing.T, action approval.Action, target string, by domain.UUID, opts ...approval.Option) *approval.Request {
	t.Helper()
	r, err := approval.New(approval.NewRequestID(), domain.NewUUID(), action,
		approval.Target{Type: "purchases.supplier", ID: target}, nil, "motivo", by, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Bloquear y desbloquear en el grupo "block" no conviven pendientes sobre el mismo objetivo; al
// resolverse una, la clave pasa a su id y se puede pedir otra.
func TestPendingKey_UniquePerGroupAndTarget_SQLite(t *testing.T) {
	db := openSQLite(t, append(approvals.DDL(table), approvals.PendingKeyDDL(table)...)...)
	repo := sqlrepo.MustRepository(db, approvals.Mapping(table))
	ctx := context.Background()
	requester := domain.NewUUID()
	block := newRequest(t, "purchases.supplier.block", "42", requester, approval.InGroup(approval.GroupBlock))
	if err := repo.Save(ctx, block); err != nil {
		t.Fatal(err)
	}
	unblock := newRequest(t, "purchases.supplier.unblock", "42", requester, approval.InGroup(approval.GroupBlock))
	if err := repo.Save(ctx, unblock); !errors.Is(err, approval.ErrPending) || !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second pending in group = %v, want ErrPending", err)
	}
	other := newRequest(t, "purchases.supplier.unblock", "43", requester, approval.InGroup(approval.GroupBlock))
	if err := repo.Save(ctx, other); err != nil {
		t.Fatalf("other target: %v", err)
	}
	got, err := repo.Get(ctx, block.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.Group() != approval.GroupBlock || got.PendingKey() != "block:purchases.supplier:42" {
		t.Fatalf("group %q key %q", got.Group(), got.PendingKey())
	}
	if err := got.Cancel(requester, ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, got); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, unblock); err != nil {
		t.Fatalf("after cancelling: %v", err)
	}
}

// FillPendingKeys pasa las filas que ya había al formato común; si dos pendientes chocan, la más
// antigua se queda la clave.
func TestFillPendingKeys_SQLite(t *testing.T) {
	db := openSQLite(t, append(approvals.DDL(table), approvals.PendingKeyColumnDDL(table))...)
	repo := sqlrepo.MustRepository(db, approvals.Mapping(table))
	ctx := context.Background()
	requester := domain.NewUUID()
	first := newRequest(t, "purchases.supplier.block", "42", requester)
	second := newRequest(t, "purchases.supplier.unblock", "42", requester)
	done := newRequest(t, "purchases.supplier.block", "43", requester)
	_ = done.Cancel(requester, "")
	for _, r := range []*approval.Request{first, second, done} {
		if err := repo.Save(ctx, r); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Update(ctx, table, sqlrepo.Values{"pending_key": "old"}, sqlrepo.Values{"id": r.ID().UUID}); err != nil {
			t.Fatal(err)
		}
	}
	if err := approvals.FillPendingKeys(table, approval.GroupBlock)(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, sqlrepo.RenderDDL("sqlite", approvals.PendingKeyIndexDDL(table))); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Select(ctx, table, []string{"id", "pending_key"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[domain.UUID]string{first.ID().UUID: "block:purchases.supplier:42", second.ID().UUID: second.ID().String(),
		done.ID().UUID: done.ID().String()}
	for _, r := range rows {
		if k := r.String("pending_key"); k != want[r.UUID("id")] {
			t.Errorf("%s: key %q, want %q", r.UUID("id"), k, want[r.UUID("id")])
		}
	}
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
	for _, s := range append(approvals.DDL(table), approvals.PendingKeyDDL(table)...) {
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
