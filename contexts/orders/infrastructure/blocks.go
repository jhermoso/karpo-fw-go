package infrastructure

// El bloqueo de las condiciones de un cliente (D-156): el historial de bloqueos y desbloqueos y las
// solicitudes de aprobación, con una sola pendiente por condiciones y acción (pending_key y su
// índice único, como en Compras).

import (
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/approval"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/approvals"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// Tablas del bloqueo.
const (
	// TableApprovals guarda las solicitudes de aprobación del bloqueo y el desbloqueo.
	TableApprovals = "ord_approvals"
	// TableTermsBlockEvents es el historial de bloqueos y desbloqueos de las condiciones.
	TableTermsBlockEvents = "ord_terms_block_events"
)

// uxPending es el índice único de la solicitud pendiente.
const uxPending = "ux_ord_approvals_pending"

// blockDDL es la migración 11: el motivo del último desbloqueo, el historial (una fila por bloqueo
// o desbloqueo efectivo, con los ámbitos que cambiaron, quién, cuándo y la solicitud aprobada, si
// la hubo) y las solicitudes de aprobación con su clave de unicidad. Los motivos nuevos miden 800
// porque Oracle cuenta bytes.
var blockDDL = slices.Concat([]string{
	`ALTER TABLE ord_terms {add:unblock_reason} {str:800}{addEnd}`,
	`CREATE TABLE ` + TableTermsBlockEvents + ` (terms_id {uuid} NOT NULL, line_no {int} NOT NULL, blocked {bool} NOT NULL,
	scopes {str:30} NOT NULL, reason {str:800} NOT NULL, changed_at {ts} NOT NULL, changed_by_id {str:64}, changed_by_name {str:200},
	approval_id {uuid}, PRIMARY KEY (terms_id, line_no), FOREIGN KEY (terms_id) REFERENCES ord_terms (id))`,
}, approvals.DDL(TableApprovals), []string{
	`ALTER TABLE ` + TableApprovals + ` {add:pending_key} {str:200}{addEnd}`,
	`CREATE UNIQUE INDEX ` + uxPending + ` ON ` + TableApprovals + ` (pending_key)`,
})

// ApprovalRepositoryFactory construye el repositorio de las solicitudes de aprobación.
func ApprovalRepositoryFactory(b hotswap.Backend) (approval.Repository, error) {
	return repository(b, approvals.Mapping(TableApprovals))
}

// blockEventsChild mapea el historial de bloqueos de las condiciones.
var blockEventsChild = sqlrepo.Child[*domain.Terms]{
	Name: "blocks", Table: TableTermsBlockEvents, ForeignKey: "terms_id", OrderBy: []string{"line_no"},
	Columns: []string{"line_no", "blocked", "scopes", "reason", "changed_at", "changed_by_id", "changed_by_name", "approval_id"},
	Dehydrate: func(t *domain.Terms) ([]sqlrepo.Values, error) {
		out := []sqlrepo.Values{}
		for k, e := range t.State().BlockEvents {
			out = append(out, sqlrepo.Values{"line_no": int64(k + 1), "blocked": e.Blocked, "scopes": strings.Join(e.Scope.Texts(), ","),
				"reason": e.Reason, "changed_at": e.At, "changed_by_id": nullString(idText(e.By.PartyID)), "changed_by_name": nullString(e.By.Name),
				"approval_id": optUUID(e.Approval)})
		}
		return out, nil
	},
}

// blockEvents lee el historial de bloqueos de las condiciones.
func blockEvents(rows []*sqlrepo.Row) ([]domain.BlockEvent, error) {
	var out []domain.BlockEvent
	for _, c := range byNo(rows) {
		scope, err := domain.ParseBlockScope(strings.Split(c.String("scopes"), ","))
		if err != nil {
			return nil, err
		}
		e := domain.BlockEvent{Blocked: c.Bool("blocked"), Scope: scope, Reason: c.String("reason"), At: c.Time("changed_at"),
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
