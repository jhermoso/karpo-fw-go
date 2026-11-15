package orders_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	iinfra "github.com/jhermoso/karpo-fw-go/contexts/inventory/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/orders"
	oapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	oinfra "github.com/jhermoso/karpo-fw-go/contexts/orders/infrastructure"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	dinfra "github.com/jhermoso/karpo-fw-go/contexts/products/infrastructure"
	rinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/approval"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// termsApprovals recorre el bloqueo y el desbloqueo de un cliente con aprobación (D-156): la orden
// deja la solicitud (202) sin tocar las condiciones; otra igual es 409; el solicitante no aprueba;
// el rechazo exige comentario; al aprobar, quedan bloqueadas las entregas con su motivo y el
// historial dice quién aprobó y qué ámbito cambió.
func (h *host) termsApprovals() {
	t := h.t
	acme := fw.NewUUID().String()
	h.grant("manager", acme)
	h.grant("approver", acme)
	var terms oapp.TermsDTO
	h.must(h.do("PUT", "/api/orders/terms", "manager", map[string]any{"company": acme, "customer": fw.NewUUID().String()}, &terms), 200, "terms")
	base, apv := "/api/orders/terms/"+terms.ID, "/api/orders/approvals"
	post := func(path, user string, body any, want int, what string) oapp.TermsDTO {
		t.Helper()
		var out oapp.TermsDTO
		h.must(h.do("POST", path, user, body, &out), want, what)
		return out
	}
	post(base+"/block", "manager", map[string]any{"scopes": []string{"delivery"}}, 422, "no reason, even with approval")
	got := post(base+"/block", "manager", map[string]any{"scopes": []string{"delivery"}, "reason": "sin almacén", "version": terms.Version}, 202, "block")
	if got.PendingApproval == "" || got.BlockDelivery {
		t.Fatalf("pending: %+v", got)
	}
	post(base+"/block", "manager", map[string]any{"scopes": []string{"delivery"}, "reason": "otra"}, 409, "a second pending block")
	post(base+"/block", "manager", map[string]any{"scopes": []string{"orders"}, "reason": "otra"}, 409, "one pending request per terms, any scope")
	var list []oapp.ApprovalDTO
	h.must(h.do("GET", apv, "approver", nil, &list), 200, "list")
	if len(list) != 1 || list[0].Action != "orders.customer_terms.block" || list[0].TargetID != terms.ID {
		t.Fatalf("list = %+v", list)
	}
	h.must(h.do("GET", apv, "outsider", nil, &list), 200, "list outside")
	if len(list) != 0 {
		t.Fatalf("outsider sees %+v", list)
	}
	h.must(h.do("POST", apv+"/"+got.PendingApproval+"/approve", "manager", nil, nil), 403, "the requester does not approve")
	h.must(h.do("POST", apv+"/"+got.PendingApproval+"/reject", "approver", map[string]any{}, nil), 422, "reject without a comment")
	h.must(h.do("POST", apv+"/"+got.PendingApproval+"/reject", "approver", map[string]any{"comment": "no consta"}, nil), 200, "reject")
	got = post(base+"/block", "manager", map[string]any{"scopes": []string{"delivery"}, "reason": "sin almacén"}, 202, "block again")
	var res oapp.ApprovalDTO
	h.must(h.do("POST", apv+"/"+got.PendingApproval+"/approve", "approver", nil, &res), 200, "approve")
	var all []oapp.TermsDTO
	h.must(h.do("GET", "/api/orders/terms?company="+acme, "manager", nil, &all), 200, "terms")
	var hist []oapp.BlockEventDTO
	h.must(h.do("GET", base+"/block-history", "manager", nil, &hist), 200, "history")
	if len(all) != 1 || !all[0].BlockDelivery || all[0].BlockOrders || all[0].BlockReason != "sin almacén" || len(hist) != 1 ||
		hist[0].ByName != "approver" || hist[0].Approval != res.ID || hist[0].Scopes[0] != "delivery" {
		t.Fatalf("approved: %+v %+v", all, hist)
	}
	got = post(base+"/unblock", "manager", map[string]any{"reason": "repuesto"}, 202, "unblock with approval")
	post(base+"/block", "manager", map[string]any{"scopes": []string{"orders"}, "reason": "otra"}, 409, "no block while an unblock is pending")
	h.must(h.do("POST", apv+"/"+got.PendingApproval+"/approve", "approver", nil, nil), 200, "approve the unblock")
	h.must(h.do("GET", base+"/block-history", "manager", nil, &hist), 200, "history")
	if len(hist) != 2 || hist[1].Blocked || hist[1].Reason != "repuesto" {
		t.Fatalf("unblocked: %+v", hist)
	}
	h.must(h.do("GET", apv+"?status=all", "approver", nil, &list), 200, "all")
	if len(list) != 3 {
		t.Fatalf("all = %d", len(list))
	}
}

func TestOrders_TermsApprovals_MemoryThenSQLite(t *testing.T) {
	h := compose(t, orders.WithApprovalPolicy(approval.Fixed{oapp.ActionBlockTerms: true, oapp.ActionUnblockTerms: true}))
	h.termsApprovals()
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "approvals.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), dinfra.Migrations(), iinfra.Migrations(), rinfra.Migrations(),
		oinfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.termsApprovals()
}
