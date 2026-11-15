package shipments_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/shipments"
	sapp "github.com/jhermoso/karpo-fw-go/contexts/shipments/application"
	sinfra "github.com/jhermoso/karpo-fw-go/contexts/shipments/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/approval"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// carrierApprovals recorre el bloqueo y el desbloqueo con aprobación (D-156): la orden deja la
// solicitud (202) sin tocar el transportista ni el historial; otra igual es 409; el solicitante no
// aprueba ni lo hace quien no tiene el permiso; el rechazo exige comentario; DELETE también pide; al
// aprobar, el transportista queda bloqueado con su motivo y una fila en el historial; el
// desbloqueo, igual.
func (h *host) carrierApprovals() {
	t := h.t
	acme, globex := fw.NewUUID().String(), fw.NewUUID().String()
	h.grant("planner", acme)
	h.grant("viewer", acme)
	h.grant("approver", acme)
	h.grant("outsider", globex)
	base, apv := shipments.MaintenanceBase, "/api/shipments/approvals"
	status := func(user, method, path string, body any) int { s, _, _ := h.raw(method, path, user, body); return s }
	get := func(id string) map[string]any {
		_, p, _ := h.raw("GET", base+"/"+id, "planner", nil)
		return p
	}
	history := func(id string) int {
		var hist []map[string]any
		h.must(h.do("GET", base+"/"+id+"/block-history", "planner", nil, &hist), 200, "history")
		return len(hist)
	}
	code, p, _ := h.raw("POST", base, "planner", map[string]any{"company": acme, "code": "SEUR", "name": "SEUR"})
	if code != 201 {
		t.Fatalf("register: %d %v", code, p)
	}
	id := p["id"].(string)

	// Sin motivo, 422 aunque pase por aprobación; con él, 202 y la solicitud.
	h.must(status("planner", "POST", base+"/"+id+"/block", map[string]any{}), 422, "no reason")
	code, p, _ = h.raw("POST", base+"/"+id+"/block", "planner", map[string]any{"reason": "Pierde paquetes"})
	if code != 202 || p["pendingApproval"] == nil || p["blocked"] != false {
		t.Fatalf("block with approval: %d %v", code, p)
	}
	first := p["pendingApproval"].(string)
	if get(id)["blocked"] != false || history(id) != 0 {
		t.Fatalf("pending changes nothing: %v", get(id))
	}
	h.must(status("planner", "DELETE", base+"/"+id+"?reason=otra", nil), 409, "a second pending block")
	var list []sapp.ApprovalDTO
	h.must(h.do("GET", apv, "approver", nil, &list), 200, "list")
	if len(list) != 1 || list[0].ID != first || list[0].Action != "shipments.carrier.block" || list[0].Reason != "Pierde paquetes" || list[0].TargetID != id {
		t.Fatalf("list = %+v", list)
	}
	h.must(h.do("GET", apv, "outsider", nil, &list), 200, "list outside")
	if len(list) != 0 {
		t.Fatalf("outsider sees %+v", list)
	}
	h.must(status("planner", "POST", apv+"/"+first+"/approve", nil), 403, "the requester has no approve permission")
	h.must(status("viewer", "POST", apv+"/"+first+"/approve", nil), 403, "without the permission")
	h.must(status("outsider", "POST", apv+"/"+first+"/approve", nil), 404, "another organization")
	h.must(status("approver", "POST", apv+"/"+first+"/reject", map[string]any{}), 422, "reject without a comment")
	var res sapp.ApprovalDTO
	h.must(h.do("POST", apv+"/"+first+"/reject", "approver", map[string]any{"comment": "no consta"}, &res), 200, "reject")
	if res.Status != "rejected" || get(id)["blocked"] != false {
		t.Fatalf("rejected: %+v", res)
	}

	// DELETE pide el bloqueo; el solicitante cancela; luego otra y se aprueba.
	code, p, _ = h.raw("DELETE", base+"/"+id, "planner", map[string]any{"reason": "Pierde paquetes"})
	if code != 202 || p["pendingApproval"] == nil {
		t.Fatalf("delete: %d %v", code, p)
	}
	second := p["pendingApproval"].(string)
	h.must(status("approver", "POST", apv+"/"+second+"/cancel", nil), 422, "only the requester cancels")
	h.must(h.do("POST", apv+"/"+second+"/cancel", "planner", nil, &res), 200, "cancel")
	_, p, _ = h.raw("POST", base+"/"+id+"/block", "planner", map[string]any{"reason": "Pierde paquetes"})
	third := p["pendingApproval"].(string)
	h.must(h.do("POST", apv+"/"+third+"/approve", "approver", nil, &res), 200, "approve")
	if c := get(id); res.Status != "approved" || c["blocked"] != true || c["blockReason"] != "Pierde paquetes" || history(id) != 1 {
		t.Fatalf("approved: %+v %v", res, c)
	}
	h.must(status("approver", "POST", apv+"/"+third+"/approve", nil), 422, "approved twice")

	// El desbloqueo, igual.
	code, p, _ = h.raw("POST", base+"/"+id+"/unblock", "planner", map[string]any{"reason": "Ya entrega"})
	if code != 202 || p["pendingApproval"] == nil || p["blocked"] != true {
		t.Fatalf("unblock with approval: %d %v", code, p)
	}
	h.must(h.do("POST", apv+"/"+p["pendingApproval"].(string)+"/approve", "approver", nil, &res), 200, "approve the unblock")
	if c := get(id); c["blocked"] != false || c["unblockReason"] != "Ya entrega" || history(id) != 2 {
		t.Fatalf("unblocked: %v", c)
	}
	h.must(h.do("GET", apv+"?status=all", "approver", nil, &list), 200, "all")
	if len(list) != 4 {
		t.Fatalf("all = %d", len(list))
	}
}

func TestShipments_CarrierApprovals_MemoryThenSQLite(t *testing.T) {
	h := compose(t, shipments.WithApprovalPolicy(approval.Fixed{sapp.ActionBlockCarrier: true, sapp.ActionUnblockCarrier: true}))
	h.carrierApprovals()
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "approvals.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{sinfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.carrierApprovals()
}
