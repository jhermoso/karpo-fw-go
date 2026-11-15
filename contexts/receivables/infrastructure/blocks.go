package infrastructure

// El bloqueo del crédito (D-156): el historial de bloqueos y desbloqueos y las solicitudes de
// aprobación, con una sola pendiente por perfil y acción (pending_key y su índice único, como en
// Compras).

import (
	"fmt"
	"slices"
	"sort"

	"github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/approval"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	fwapproval "github.com/jhermoso/karpo-fw-go/pkg/domain/approval"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/approvals"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// Tablas del bloqueo.
const (
	// TableApprovals guarda las solicitudes de aprobación del bloqueo y el desbloqueo.
	TableApprovals = "rec_approvals"
	// TableCreditBlockEvents es el historial de bloqueos y desbloqueos del crédito.
	TableCreditBlockEvents = "rec_credit_block_events"
)

// uxPending es el índice único de la solicitud pendiente.
const uxPending = "ux_rec_approvals_pending"

// blockDDL es la migración 5: el motivo del último desbloqueo, el historial (una fila por bloqueo
// o desbloqueo efectivo, con quién, cuándo y la solicitud aprobada, si la hubo) y las solicitudes
// de aprobación con su clave de unicidad. Los motivos nuevos miden 800 porque Oracle cuenta bytes.
var blockDDL = slices.Concat([]string{
	`ALTER TABLE rec_credit_profiles {add:unblock_reason} {str:800}{addEnd}`,
	`CREATE TABLE ` + TableCreditBlockEvents + ` (profile_id {uuid} NOT NULL, line_no {int} NOT NULL, blocked {bool} NOT NULL,
	reason {str:800} NOT NULL, changed_at {ts} NOT NULL, changed_by_id {str:64}, changed_by_name {str:200}, approval_id {uuid},
	PRIMARY KEY (profile_id, line_no), FOREIGN KEY (profile_id) REFERENCES rec_credit_profiles (id))`,
}, approvals.DDL(TableApprovals), []string{
	`ALTER TABLE ` + TableApprovals + ` {add:pending_key} {str:200}{addEnd}`,
	`CREATE UNIQUE INDEX ` + uxPending + ` ON ` + TableApprovals + ` (pending_key)`,
})

// pendingKey es la clave de unicidad de una solicitud: el destino mientras está pendiente (una sola
// pendiente por destino, sea bloquear o desbloquear);
// después, su id, que no choca con nada.
func pendingKey(status, _, target, id string) string {
	if status == string(fwapproval.StatusPending) {
		return "block:" + target
	}
	return id
}

// ApprovalMapping es el mapeo del motor (pkg/persistence/approvals) con la clave pending_key y su
// índice único: dos solicitudes pendientes para el mismo perfil y acción chocan (409).
func ApprovalMapping() sqlrepo.Mapping[fwapproval.RequestID, *fwapproval.Request] {
	m := approvals.Mapping(TableApprovals)
	m.Columns = append(slices.Clone(m.Columns), "pending_key")
	dehydrate := m.Dehydrate
	m.Dehydrate = func(r *fwapproval.Request) (sqlrepo.Values, error) {
		v, err := dehydrate(r)
		if err != nil {
			return nil, err
		}
		st := r.State()
		v["pending_key"] = pendingKey(string(st.Status), string(st.Action), st.Target.ID, r.ID().String())
		return v, nil
	}
	m.Unique = append(m.Unique, sqlrepo.UniqueIndex{Name: uxPending, Columns: []string{"pending_key"},
		Err: fmt.Errorf("%w: receivables.approval_pending: there is already a pending request for this credit profile", fw.ErrConflict)})
	return m
}

// ApprovalRepositoryFactory construye el repositorio de las solicitudes de aprobación.
func ApprovalRepositoryFactory(b hotswap.Backend) (approval.Repository, error) {
	return repository(b, ApprovalMapping())
}

// blockEventsChild mapea el historial de bloqueos del perfil.
var blockEventsChild = sqlrepo.Child[*domain.CreditProfile]{
	Name: "blocks", Table: TableCreditBlockEvents, ForeignKey: "profile_id", OrderBy: []string{"line_no"},
	Columns: []string{"line_no", "blocked", "reason", "changed_at", "changed_by_id", "changed_by_name", "approval_id"},
	Dehydrate: func(p *domain.CreditProfile) ([]sqlrepo.Values, error) {
		out := []sqlrepo.Values{}
		for k, e := range p.State().BlockEvents {
			out = append(out, sqlrepo.Values{"line_no": int64(k + 1), "blocked": e.Blocked, "reason": e.Reason, "changed_at": e.At,
				"changed_by_id": nullString(idText(e.By.PartyID)), "changed_by_name": nullString(e.By.Name), "approval_id": nullUUID(e.Approval)})
		}
		return out, nil
	},
}

// blockEvents lee el historial de bloqueos del perfil.
func blockEvents(rows []*sqlrepo.Row) ([]domain.BlockEvent, error) {
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Int64("line_no") < rows[j].Int64("line_no") })
	var out []domain.BlockEvent
	for _, c := range rows {
		e := domain.BlockEvent{Blocked: c.Bool("blocked"), Reason: c.String("reason"), At: c.Time("changed_at"),
			By: vocab.Actor{Name: c.String("changed_by_name")}, Approval: c.UUID("approval_id")}
		if id := c.String("changed_by_id"); id != "" {
			e.By.PartyID, _ = fw.ParseUUID(id)
		}
		if err := c.Err(); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func idText(u fw.UUID) string {
	if u.IsZero() {
		return ""
	}
	return u.String()
}

func nullUUID(u fw.UUID) any {
	if u.IsZero() {
		return nil
	}
	return u
}
