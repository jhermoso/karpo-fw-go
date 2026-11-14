package purchases_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/purchases"
	uapp "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	uinfra "github.com/jhermoso/karpo-fw-go/contexts/purchases/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/approval"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// approvals recorre el bloqueo y el desbloqueo con aprobación (D-156): el alta bloqueada nace sin
// bloquear y con la solicitud; una segunda solicitud igual es 409; el solicitante no aprueba ni lo
// hace quien no tiene el permiso; el rechazo exige comentario y no toca el perfil; el solicitante
// cancela; un cambio que mezcla otros campos los aplica y deja el bloqueo pendiente; al aprobar, el
// perfil queda bloqueado con su motivo; y el desbloqueo, igual.
func (h *host) approvals() {
	t := h.t
	acme, globex := fw.NewUUID().String(), fw.NewUUID().String()
	h.grant("clerk", acme)
	h.grant("viewer", acme)
	h.grant("approver", acme)
	h.grant("outsider", globex)
	base, apv := purchases.MaintenanceBase, "/api/purchases/approvals"
	status := func(user, method, path string, body any) int { s, _, _ := h.raw(method, path, user, body); return s }
	get := func(id string) map[string]any {
		_, p, _ := h.raw("GET", base+"/"+id, "clerk", nil)
		return p
	}

	// Alta bloqueada: nace sin bloquear, 202 con la solicitud.
	code, p, _ := h.raw("POST", base, "clerk", map[string]any{"company": acme, "supplier": fw.NewUUID().String(), "blocked": true,
		"blockReason": "impago"})
	if code != 202 || p["blocked"] != false || p["pendingApproval"] == nil {
		t.Fatalf("blocked registration: %d %v", code, p)
	}
	id, first := p["id"].(string), p["pendingApproval"].(string)
	// La misma acción sobre el mismo perfil, otra vez: 409.
	h.must(status("clerk", "DELETE", base+"/"+id+"?reason=otra", nil), 409, "a second pending block")
	// Las pendientes del ámbito.
	var list []uapp.ApprovalDTO
	h.must(h.do("GET", apv, "approver", nil, &list), 200, "list")
	if len(list) != 1 || list[0].ID != first || list[0].Action != "purchases.supplier.block" || list[0].Reason != "impago" || list[0].TargetID != id {
		t.Fatalf("list = %+v", list)
	}
	h.must(h.do("GET", apv, "outsider", nil, &list), 200, "list outside")
	if len(list) != 0 {
		t.Fatalf("outsider sees %+v", list)
	}
	// Ni el solicitante ni quien no tiene el permiso aprueban; fuera del ámbito, 404.
	h.must(status("clerk", "POST", apv+"/"+first+"/approve", nil), 403, "the requester has no approve permission")
	h.must(status("viewer", "POST", apv+"/"+first+"/approve", nil), 403, "without the permission")
	h.must(status("outsider", "POST", apv+"/"+first+"/approve", nil), 404, "another organization")
	// Rechazo: con comentario; el perfil no cambia.
	h.must(status("approver", "POST", apv+"/"+first+"/reject", map[string]any{}), 422, "reject without a comment")
	var res uapp.ApprovalDTO
	h.must(h.do("POST", apv+"/"+first+"/reject", "approver", map[string]any{"comment": "no consta el impago"}, &res), 200, "reject")
	if res.Status != "rejected" || res.Comment != "no consta el impago" || get(id)["blocked"] != false {
		t.Fatalf("rejected: %+v %v", res, get(id))
	}
	h.must(status("approver", "POST", apv+"/"+first+"/approve", nil), 422, "approve a rejected request")

	// DELETE pide el bloqueo; el solicitante lo cancela; otro no.
	code, p, _ = h.raw("DELETE", base+"/"+id+"?reason=impago", "clerk", nil)
	if code != 202 || p["pendingApproval"] == nil {
		t.Fatalf("delete: %d %v", code, p)
	}
	second := p["pendingApproval"].(string)
	h.must(status("approver", "POST", apv+"/"+second+"/cancel", nil), 422, "only the requester cancels")
	h.must(h.do("POST", apv+"/"+second+"/cancel", "clerk", nil, &res), 200, "cancel")
	if res.Status != "cancelled" || get(id)["blocked"] != false {
		t.Fatalf("cancelled: %+v", res)
	}

	// Un cambio que mezcla otros campos: se aplican y el bloqueo queda pendiente.
	cur := get(id)
	code, p, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"paymentDays": 45, "blocked": true, "blockReason": "impago reiterado",
		"version": cur["version"]})
	if code != 202 || p["paymentDays"] != float64(45) || p["blocked"] != false || p["pendingApproval"] == nil {
		t.Fatalf("mixed change: %d %v", code, p)
	}
	third := p["pendingApproval"].(string)
	// La aprobación lo bloquea con su motivo; aprobar otra vez es 422.
	h.must(h.do("POST", apv+"/"+third+"/approve", "approver", map[string]any{"comment": "visto"}, &res), 200, "approve")
	if got := get(id); res.Status != "approved" || got["blocked"] != true || got["blockReason"] != "impago reiterado" || got["paymentDays"] != float64(45) {
		t.Fatalf("approved: %+v %v", res, got)
	}
	h.must(status("approver", "POST", apv+"/"+third+"/approve", nil), 422, "approve twice")

	// Quien pide no aprueba aunque tenga el permiso: 422; retira la suya.
	cur = get(id)
	code, p, _ = h.raw("PUT", base+"/"+id, "approver", map[string]any{"blocked": false, "unblockReason": "pagado", "version": cur["version"]})
	if code != 202 {
		t.Fatalf("unblock by the approver: %d %v", code, p)
	}
	h.must(status("approver", "POST", apv+"/"+p["pendingApproval"].(string)+"/approve", nil), 422, "self approval")
	h.must(status("approver", "POST", apv+"/"+p["pendingApproval"].(string)+"/cancel", nil), 200, "cancel own")

	// Desbloquear: también pendiente hasta aprobarse.
	cur = get(id)
	code, p, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"blocked": false, "unblockReason": "pagado", "version": cur["version"]})
	if code != 202 || p["blocked"] != true {
		t.Fatalf("unblock: %d %v", code, p)
	}
	h.must(status("approver", "POST", apv+"/"+p["pendingApproval"].(string)+"/approve", nil), 200, "approve the unblock")
	if got := get(id); got["blocked"] != false || got["unblockReason"] != "pagado" {
		t.Fatalf("unblocked: %v", got)
	}
	// La lista de las resueltas.
	h.must(h.do("GET", apv+"?status=all&company="+acme, "approver", nil, &list), 200, "list all")
	if len(list) != 5 {
		t.Fatalf("all = %d", len(list))
	}
	h.must(status("approver", "GET", apv+"?status=nope", nil), 400, "a bad status")
}

func TestPurchases_BlockApprovals_MemoryThenSQLite(t *testing.T) {
	h := compose(t, purchases.WithApprovalPolicy(approval.Fixed{uapp.ActionBlockSupplier: true, uapp.ActionUnblockSupplier: true}))
	h.approvals()
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "approvals.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{uinfra.Migrations()})
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

// Sin política, bloquear es inmediato, como siempre.
func TestPurchases_BlockWithoutPolicy_IsImmediate(t *testing.T) {
	h := compose(t)
	acme := fw.NewUUID().String()
	h.grant("clerk", acme)
	code, p, _ := h.raw("POST", purchases.MaintenanceBase, "clerk", map[string]any{"company": acme, "supplier": fw.NewUUID().String(),
		"blocked": true, "blockReason": "impago"})
	if code != 201 || p["blocked"] != true || p["pendingApproval"] != nil {
		t.Fatalf("%d %v", code, p)
	}
}
