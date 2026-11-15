package purchases_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/purchases"
	uapp "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	uinfra "github.com/jhermoso/karpo-fw-go/contexts/purchases/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/approval"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	dapproval "github.com/jhermoso/karpo-fw-go/pkg/domain/approval"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// approvals recorre el bloqueo y el desbloqueo con aprobación (D-156 ampliada): la orden de
// bloquear responde 202 y no toca el perfil (ni su versión); una segunda solicitud igual es 409; el
// solicitante no aprueba ni lo hace quien no tiene el permiso; el rechazo exige comentario y no toca
// el perfil; el solicitante cancela; el cambio general con una solicitud pendiente se aplica entero;
// al aprobar, el perfil queda bloqueado con su motivo y una fila en el historial con la solicitud; y
// el desbloqueo, igual.
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
	history := func(id string) []uapp.BlockEventDTO {
		var out []uapp.BlockEventDTO
		h.must(h.do("GET", base+"/"+id+"/block-history", "clerk", nil, &out), 200, "history")
		return out
	}

	code, p, _ := h.raw("POST", base, "clerk", map[string]any{"company": acme, "supplier": fw.NewUUID().String()})
	if code != 201 {
		t.Fatalf("registration: %d %v", code, p)
	}
	id, v := p["id"].(string), p["version"]
	// Bloquear: 202 con la solicitud; el perfil, ni su versión, no cambian.
	code, p, _ = h.raw("POST", base+"/"+id+"/block", "clerk", map[string]any{"version": v, "reason": "impago"})
	if code != 202 || p["blocked"] != false || p["pendingApproval"] == nil || get(id)["version"] != v {
		t.Fatalf("block: %d %v", code, p)
	}
	first := p["pendingApproval"].(string)
	// La misma acción sobre el mismo perfil, otra vez: 409.
	h.must(status("clerk", "POST", base+"/"+id+"/block", map[string]any{"version": v, "reason": "otra"}), 409, "a second pending block")
	h.must(status("clerk", "DELETE", base+"/"+id+"?reason=otra", nil), 409, "a second pending block by DELETE")
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
	if res.Status != "rejected" || res.Comment != "no consta el impago" || get(id)["blocked"] != false || len(history(id)) != 0 {
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

	// Con un bloqueo pendiente, el cambio general se aplica entero y no toca el bloqueo.
	code, p, _ = h.raw("POST", base+"/"+id+"/block", "clerk", map[string]any{"version": v, "reason": "impago reiterado"})
	if code != 202 {
		t.Fatalf("block again: %d %v", code, p)
	}
	third := p["pendingApproval"].(string)
	code, p, _ = h.raw("PUT", base+"/"+id, "clerk", map[string]any{"paymentDays": 45, "version": v})
	if code != 200 || p["paymentDays"] != float64(45) || p["blocked"] != false || p["pendingApproval"] != nil {
		t.Fatalf("change with a pending block: %d %v", code, p)
	}
	// La aprobación lo bloquea con su motivo y deja la fila del historial; aprobar otra vez es 422.
	h.must(h.do("POST", apv+"/"+third+"/approve", "approver", map[string]any{"comment": "visto"}, &res), 200, "approve")
	if got := get(id); res.Status != "approved" || got["blocked"] != true || got["blockReason"] != "impago reiterado" || got["paymentDays"] != float64(45) {
		t.Fatalf("approved: %+v %v", res, got)
	}
	if hs := history(id); len(hs) != 1 || !hs[0].Blocked || hs[0].Reason != "impago reiterado" || hs[0].Approval != third || hs[0].UserName != "approver" {
		t.Fatalf("history after the approval: %+v", hs)
	}
	h.must(status("approver", "POST", apv+"/"+third+"/approve", nil), 422, "approve twice")

	// Quien pide no aprueba aunque tenga el permiso: 422; retira la suya.
	cur := get(id)
	code, p, _ = h.raw("POST", base+"/"+id+"/unblock", "approver", map[string]any{"reason": "pagado", "version": cur["version"]})
	if code != 202 {
		t.Fatalf("unblock by the approver: %d %v", code, p)
	}
	h.must(status("approver", "POST", apv+"/"+p["pendingApproval"].(string)+"/approve", nil), 422, "self approval")
	h.must(status("approver", "POST", apv+"/"+p["pendingApproval"].(string)+"/cancel", nil), 200, "cancel own")

	// Desbloquear: también pendiente hasta aprobarse.
	code, p, _ = h.raw("POST", base+"/"+id+"/unblock", "clerk", map[string]any{"reason": "pagado", "version": cur["version"]})
	if code != 202 || p["blocked"] != true || p["version"] != cur["version"] {
		t.Fatalf("unblock: %d %v", code, p)
	}
	h.must(status("approver", "POST", apv+"/"+p["pendingApproval"].(string)+"/approve", nil), 200, "approve the unblock")
	if got := get(id); got["blocked"] != false || got["unblockReason"] != "pagado" {
		t.Fatalf("unblocked: %v", got)
	}
	if hs := history(id); len(hs) != 2 || hs[1].Blocked || hs[1].Reason != "pagado" {
		t.Fatalf("history after the unblock: %+v", hs)
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

// Sin política, bloquear es inmediato, con su fila en el historial.
func TestPurchases_BlockWithoutPolicy_IsImmediate(t *testing.T) {
	h := compose(t)
	acme := fw.NewUUID().String()
	h.grant("clerk", acme)
	code, p, _ := h.raw("POST", purchases.MaintenanceBase, "clerk", map[string]any{"company": acme, "supplier": fw.NewUUID().String()})
	if code != 201 {
		t.Fatalf("%d %v", code, p)
	}
	code, p, _ = h.raw("POST", purchases.MaintenanceBase+"/"+p["id"].(string)+"/block", "clerk", map[string]any{"reason": "impago", "version": p["version"]})
	if code != 200 || p["blocked"] != true || p["pendingApproval"] != nil {
		t.Fatalf("%d %v", code, p)
	}
}

// En SQL la base también impide dos solicitudes pendientes para el mismo perfil y acción (el índice
// único de pending_key, migración 9), aunque el código no lo comprobara; resuelta una, cabe otra.
func TestPurchases_OnePendingApprovalInTheDatabase(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "pending.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
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
	repo, err := uinfra.ApprovalRepositoryFactory(db)
	if err != nil {
		t.Fatal(err)
	}
	company, by, target := fw.NewUUID(), fw.NewUUID(), dapproval.Target{Type: "purchases.supplier_profile", ID: fw.NewUUID().String()}
	request := func() *dapproval.Request {
		r, err := dapproval.New(dapproval.NewRequestID(), company, uapp.ActionBlockSupplier, target, nil, "impago", by)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r1 := request()
	if err := repo.Save(ctx, r1); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, request()); !errors.Is(err, fw.ErrConflict) {
		t.Fatalf("a second pending request: %v", err)
	}
	if err := r1.Cancel(by, ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, r1); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, request()); err != nil {
		t.Fatalf("a new request after the first is resolved: %v", err)
	}
}
