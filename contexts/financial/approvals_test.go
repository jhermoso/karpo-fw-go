package financial_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/financial"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/financial/application"
	"github.com/jhermoso/karpo-fw-go/contexts/financial/domain"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/financial/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/approval"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// approvals recorre el bloqueo y el desbloqueo de una cuenta con aprobación (D-156): la orden deja la
// solicitud (202) sin tocar la cuenta ni el historial; otra igual es 409; el solicitante no aprueba;
// el rechazo exige comentario; al aprobar, la cuenta queda bloqueada con su motivo y una fila en el
// historial; el desbloqueo, igual.
func (h *host) approvals() {
	t := h.t
	acme, globex, ana := fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String()
	for _, u := range []string{"opener", "officer", "approver", "viewer", "closer"} {
		h.grant(u, acme)
	}
	h.grant("outsider", globex)
	h.banks[domain.OrganizationID{UUID: fw.MustParseUUID(acme)}] = true
	apv := "/api/financial/approvals"
	var acc fapp.AccountDTO
	h.must(h.do("POST", "/api/financial/accounts", "opener", map[string]any{"company": acme, "number": "ES9121000418450200051332", "holder": ana}, &acc), 201, "open")
	path := "/api/financial/accounts/" + acc.ID
	get := func() fapp.AccountDTO {
		var a fapp.AccountDTO
		h.must(h.do("GET", path, "viewer", nil, &a), 200, "get")
		return a
	}
	history := func() int {
		var hist []fapp.BlockEventDTO
		h.must(h.do("GET", path+"/block-history", "viewer", nil, &hist), 200, "history")
		return len(hist)
	}

	h.must(h.do("POST", path+"/block", "officer", map[string]any{}, nil), 422, "no reason")
	var out fapp.AccountDTO
	h.must(h.do("POST", path+"/block", "officer", map[string]any{"reason": "Orden judicial"}, &out), 202, "block with approval")
	if out.PendingApproval == "" || get().Status != "active" || history() != 0 {
		t.Fatalf("pending: %+v", out)
	}
	first := out.PendingApproval
	h.must(h.do("POST", path+"/block", "officer", map[string]any{"reason": "otra"}, nil), 409, "a second pending block")
	var list []fapp.ApprovalDTO
	h.must(h.do("GET", apv, "approver", nil, &list), 200, "list")
	// El alias /api/v1 del motor de Angular responde lo mismo.
	var alias []fapp.ApprovalDTO
	h.must(h.do("GET", financial.ApprovalsBaseV1, "approver", nil, &alias), 200, "list by /api/v1")
	if !reflect.DeepEqual(alias, list) {
		h.t.Fatalf("/api/v1 list = %+v, want %+v", alias, list)
	}
	if len(list) != 1 || list[0].ID != first || list[0].Action != "financial.account.block" || list[0].TargetID != acc.ID {
		t.Fatalf("list = %+v", list)
	}
	h.must(h.do("GET", apv, "outsider", nil, &list), 200, "list outside")
	if len(list) != 0 {
		t.Fatalf("outsider sees %+v", list)
	}
	h.must(h.do("POST", apv+"/"+first+"/approve", "officer", nil, nil), 403, "without the permission")
	h.must(h.do("POST", apv+"/"+first+"/approve", "outsider", nil, nil), 404, "another organization")
	h.must(h.do("POST", apv+"/"+first+"/reject", "approver", map[string]any{}, nil), 422, "reject without a comment")
	var res fapp.ApprovalDTO
	h.must(h.do("POST", apv+"/"+first+"/reject", "approver", map[string]any{"comment": "no consta"}, &res), 200, "reject")
	if res.Status != "rejected" || get().Status != "active" {
		t.Fatalf("rejected: %+v", res)
	}
	h.must(h.do("POST", path+"/block", "officer", map[string]any{"reason": "Orden judicial"}, &out), 202, "block again")
	h.must(h.do("POST", apv+"/"+out.PendingApproval+"/approve", "approver", nil, &res), 200, "approve")
	if a := get(); res.Status != "approved" || a.Status != "blocked" || a.BlockReason != "Orden judicial" || history() != 1 {
		t.Fatalf("approved: %+v %+v", res, a)
	}
	h.must(h.do("POST", apv+"/"+out.PendingApproval+"/approve", "approver", nil, nil), 422, "approved twice")

	h.must(h.do("POST", path+"/unblock", "officer", map[string]any{"reason": "Levantada"}, &out), 202, "unblock with approval")
	h.must(h.do("POST", apv+"/"+out.PendingApproval+"/cancel", "approver", nil, nil), 422, "only the requester cancels")
	h.must(h.do("POST", financial.ApprovalsBaseV1+"/"+out.PendingApproval+"/approve", "approver", nil, &res), 200, "approve the unblock by /api/v1")
	if a := get(); a.Status != "active" || a.UnblockReason != "Levantada" || history() != 2 {
		t.Fatalf("unblocked: %+v", a)
	}

	// Si al aprobar ya no se puede aplicar (la cuenta se cerró entretanto), 422 y la solicitud sigue
	// pendiente: se rechaza o se cancela.
	h.must(h.do("POST", path+"/block", "officer", map[string]any{"reason": "Otra orden"}, &out), 202, "a block that will not apply")
	h.must(h.do("POST", path+"/close", "closer", map[string]any{"reason": "Fin"}, nil), 200, "closed meanwhile")
	h.must(h.do("POST", apv+"/"+out.PendingApproval+"/approve", "approver", nil, nil), 422, "no longer applicable")
	h.must(h.do("POST", apv+"/"+out.PendingApproval+"/cancel", "officer", nil, &res), 200, "the requester cancels it")
	if res.Status != "cancelled" || get().Status != "closed" || history() != 2 {
		t.Fatalf("not applicable: %+v", res)
	}
}

func TestFinancial_Approvals_MemoryThenSQLite(t *testing.T) {
	h := compose(t, financial.WithApprovalPolicy(approval.Fixed{fapp.ActionBlockAccount: true, fapp.ActionUnblockAccount: true}))
	h.approvals()
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "approvals.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{finfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.approvals()
}
