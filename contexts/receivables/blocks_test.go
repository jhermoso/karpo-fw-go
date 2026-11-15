package receivables_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables"
	rapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	rinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/approval"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// toSQLite pasa el anfitrión a una base SQLite nueva con el esquema de los cuatro contextos.
func (h *host) toSQLite() {
	t, ctx := h.t, context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "blocks.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	h.ok(err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), binfra.Migrations(), rinfra.Migrations()})
	h.ok(err)
	_, err = m.Migrate(ctx)
	h.ok(err)
	h.ok(h.sw.Swap(ctx, db))
}

// profile da de alta el perfil de crédito de un cliente nuevo de seller.
func (h *host) profile(seller string) rapp.CreditDTO {
	var p rapp.CreditDTO
	h.must(h.do("PUT", "/api/receivables/credit", "clerk", map[string]any{"seller": seller, "customer": fw.NewUUID().String()}, &p), 200, "credit profile")
	return p
}

// creditBlocks recorre el bloqueo sin aprobación (D-156): el alta y el cambio general no bloquean
// (422), bloquear y desbloquear exigen motivo y versión, y de bloqueado no se sale salvo por
// desbloquear con motivo; cada cambio queda en el historial.
func (h *host) creditBlocks() {
	t := h.t
	acme := fw.NewUUID().String()
	h.grant("clerk", acme)
	h.must(h.do("PUT", "/api/receivables/credit", "clerk", map[string]any{"seller": acme, "customer": fw.NewUUID().String(), "blocked": true}, nil),
		422, "a new profile is not born blocked")
	p := h.profile(acme)
	base := "/api/receivables/credit/" + p.ID
	h.must(h.do("POST", base+"/block", "clerk", map[string]any{}, nil), 422, "blocking needs a reason")
	h.must(h.do("POST", base+"/block", "clerk", map[string]any{"reason": "impagos", "version": p.Version + 1}, nil), 409, "a stale version")
	h.must(h.do("POST", base+"/block", "clerk", map[string]any{"reason": "impagos", "version": p.Version}, &p), 200, "block")
	if !p.Blocked || p.BlockReason != "impagos" {
		t.Fatalf("blocked: %+v", p)
	}
	// De bloqueado no se sale con el cambio general ni sin motivo.
	h.must(h.do("PUT", "/api/receivables/credit", "clerk", map[string]any{"seller": acme, "customer": p.Customer, "blocked": false}, nil), 422, "the general change")
	h.must(h.do("PUT", "/api/receivables/credit", "clerk", map[string]any{"seller": acme, "customer": p.Customer, "blockReason": "otro"}, nil), 422, "another reason")
	h.must(h.do("POST", base+"/unblock", "clerk", map[string]any{"reason": " "}, nil), 422, "unblocking needs a reason")
	var same rapp.CreditDTO
	h.must(h.do("PUT", "/api/receivables/credit", "clerk", map[string]any{"seller": acme, "customer": p.Customer, "blocked": true, "limit": "100"}, &same), 200,
		"the same block is accepted")
	if !same.Blocked || same.Limit != "100.00" {
		t.Fatalf("the general change keeps the block: %+v", same)
	}
	p = rapp.CreditDTO{}
	h.must(h.do("POST", base+"/unblock", "clerk", map[string]any{"reason": "pagado"}, &p), 200, "unblock")
	if p.Blocked || p.BlockReason != "" || p.UnblockReason != "pagado" {
		t.Fatalf("unblocked: %+v", p)
	}
	var hist []rapp.BlockEventDTO
	h.must(h.do("GET", base+"/block-history", "clerk", nil, &hist), 200, "history")
	if len(hist) != 2 || !hist[0].Blocked || hist[0].Reason != "impagos" || hist[1].Blocked || hist[1].ByName != "clerk" {
		t.Fatalf("history: %+v", hist)
	}
	h.must(h.do("GET", base+"/block-history", "outsider", nil, nil), 404, "history outside the scope")
}

// creditApprovals recorre el bloqueo y el desbloqueo con aprobación: la orden deja la solicitud
// (202) sin tocar el perfil; otra igual es 409; el solicitante no aprueba; el rechazo exige
// comentario; al aprobar, el perfil queda bloqueado con su motivo y el historial dice quién aprobó.
func (h *host) creditApprovals() {
	t := h.t
	acme := fw.NewUUID().String()
	h.grant("clerk", acme)
	h.grant("approver", acme)
	p := h.profile(acme)
	base, apv := "/api/receivables/credit/"+p.ID, "/api/receivables/approvals"
	h.must(h.do("POST", base+"/block", "clerk", map[string]any{}, nil), 422, "no reason, even with approval")
	var got rapp.CreditDTO
	h.must(h.do("POST", base+"/block", "clerk", map[string]any{"reason": "impagos"}, &got), 202, "block with approval")
	if got.PendingApproval == "" || got.Blocked {
		t.Fatalf("pending: %+v", got)
	}
	h.must(h.do("POST", base+"/block", "clerk", map[string]any{"reason": "otra"}, nil), 409, "a second pending block")
	var list []rapp.ApprovalDTO
	h.must(h.do("GET", apv, "approver", nil, &list), 200, "list")
	if len(list) != 1 || list[0].Action != "receivables.credit.block" || list[0].TargetID != p.ID {
		t.Fatalf("list = %+v", list)
	}
	h.must(h.do("GET", apv, "outsider", nil, &list), 200, "list outside")
	if len(list) != 0 {
		t.Fatalf("outsider sees %+v", list)
	}
	h.must(h.do("POST", apv+"/"+got.PendingApproval+"/approve", "clerk", nil, nil), 403, "the requester does not approve")
	h.must(h.do("POST", apv+"/"+got.PendingApproval+"/reject", "approver", map[string]any{}, nil), 422, "reject without a comment")
	h.must(h.do("POST", apv+"/"+got.PendingApproval+"/reject", "approver", map[string]any{"comment": "no consta"}, nil), 200, "reject")
	h.must(h.do("POST", base+"/block", "clerk", map[string]any{"reason": "impagos"}, &got), 202, "block again")
	var res rapp.ApprovalDTO
	h.must(h.do("POST", apv+"/"+got.PendingApproval+"/approve", "approver", nil, &res), 200, "approve")
	var hist []rapp.BlockEventDTO
	h.must(h.do("GET", base+"/block-history", "clerk", nil, &hist), 200, "history")
	if res.Status != "approved" || len(hist) != 1 || !hist[0].Blocked || hist[0].ByName != "approver" || hist[0].Approval != res.ID {
		t.Fatalf("approved: %+v %+v", res, hist)
	}
	h.must(h.do("POST", base+"/unblock", "clerk", map[string]any{"reason": "pagado"}, &got), 202, "unblock with approval")
	h.must(h.do("POST", apv+"/"+got.PendingApproval+"/approve", "approver", nil, nil), 200, "approve the unblock")
	h.must(h.do("GET", base+"/block-history", "clerk", nil, &hist), 200, "history")
	if len(hist) != 2 || hist[1].Blocked || hist[1].Reason != "pagado" {
		t.Fatalf("unblocked: %+v", hist)
	}
	h.must(h.do("GET", apv+"?status=all", "approver", nil, &list), 200, "all")
	if len(list) != 3 {
		t.Fatalf("all = %d", len(list))
	}
}

func TestReceivables_CreditBlocks_MemoryThenSQLite(t *testing.T) {
	h := compose(t)
	h.creditBlocks()
	h.toSQLite()
	h.creditBlocks()
}

func TestReceivables_CreditApprovals_MemoryThenSQLite(t *testing.T) {
	h := compose(t, receivables.WithApprovalPolicy(approval.Fixed{rapp.ActionBlockCredit: true, rapp.ActionUnblockCredit: true}))
	h.creditApprovals()
	h.toSQLite()
	h.creditApprovals()
}
