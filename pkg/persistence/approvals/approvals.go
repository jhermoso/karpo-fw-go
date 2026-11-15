// Package approvals guarda las solicitudes de aprobación (pkg/domain/approval) con sqlrepo, en
// cualquier motor. La tabla es de cada contexto que use aprobaciones, como su outbox: el contexto
// elige el nombre (convención <prefijo-del-contexto>_approvals, p. ej. "pur_approvals"), añade DDL(tabla) a sus migraciones y
// abre el repositorio con sqlrepo.NewRepository(db, Mapping(tabla)). En memoria basta
// memory.NewRepository[approval.RequestID, *approval.Request].
//
// Una sola solicitud pendiente por grupo y objetivo (approval.Request.PendingKey): la columna
// pending_key, con su índice único, vale «grupo:tipo:id» mientras la solicitud está pendiente y su
// id después, nunca NULL. Una tabla nueva lleva DDL y PendingKeyDDL; una que ya existía sin la
// columna, PendingKeyColumnDDL con FillPendingKeys en Run y, en la migración siguiente,
// PendingKeyIndexDDL. Un choque del índice es approval.ErrPending (409).
package approvals

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/approval"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// MaxTableLen es la longitud máxima del nombre de la tabla: el índice "ux_<tabla>_pending" no puede
// pasar de 30 caracteres en Oracle (ORA-00972).
const MaxTableLen = 19

// checkTable entra en pánico si el nombre de la tabla es demasiado largo (error de composición).
func checkTable(table string) {
	if table == "" || len(table) > MaxTableLen {
		panic(fmt.Sprintf("approvals: table name %q must have 1 to %d characters", table, MaxTableLen))
	}
}

// DDL devuelve la tabla y su índice (sin pending_key: va aparte, PendingKeyDDL, para no cambiar las
// migraciones ya aplicadas), con los marcadores de dialecto de sqlrepo.RenderDDL. Entra en
// pánico si table tiene más de MaxTableLen caracteres.
func DDL(table string) []string {
	checkTable(table)
	return []string{
		`CREATE TABLE ` + table + ` (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL,
	action {str:120} NOT NULL, target_type {str:120} NOT NULL, target_id {str:64} NOT NULL, payload {text}, reason {str:1000},
	requested_by {uuid} NOT NULL, requested_at {ts} NOT NULL, status {str:20} NOT NULL, resolved_by {uuid}, resolved_at {ts},
	resolution_comment {str:1000})`,
		`CREATE INDEX ix_` + table + `_target ON ` + table + ` (company, target_type, target_id, status)`,
	}
}

// PendingKeyColumnDDL añade la columna pending_key a una tabla creada con DDL.
func PendingKeyColumnDDL(table string) string {
	checkTable(table)
	return `ALTER TABLE ` + table + ` {add:pending_key} {str:200}{addEnd}`
}

// PendingKeyIndexDDL crea el índice único de pending_key, "ux_<tabla>_pending".
func PendingKeyIndexDDL(table string) string {
	return `CREATE UNIQUE INDEX ` + pendingIndex(table) + ` ON ` + table + ` (pending_key)`
}

// PendingKeyDDL es la columna y su índice, para una tabla nueva (sin filas que rellenar).
func PendingKeyDDL(table string) []string {
	return []string{PendingKeyColumnDDL(table), PendingKeyIndexDDL(table)}
}

func pendingIndex(table string) string {
	checkTable(table)
	return "ux_" + table + "_pending"
}

// FillPendingKeys devuelve un Run de migración que recalcula pending_key en todas las filas de
// table con el grupo group (las solicitudes que ya hubiera). Si dos pendientes chocan, la más
// antigua se queda la clave y las demás su id: siguen pendientes y se pueden resolver, pero no
// impiden nada.
func FillPendingKeys(table, group string) func(context.Context, *sqlrepo.DB) error {
	checkTable(table)
	return func(ctx context.Context, db *sqlrepo.DB) error {
		rows, err := db.Select(ctx, table, []string{"id", "target_type", "target_id", "status", "requested_at"})
		if err != nil {
			return err
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Time("requested_at").Before(rows[j].Time("requested_at")) })
		used := map[string]bool{}
		for _, r := range rows {
			id := r.UUID("id")
			key := id.String()
			if approval.Status(r.String("status")) == approval.StatusPending {
				if k := approval.PendingKeyFor(group, approval.Target{Type: r.String("target_type"), ID: r.String("target_id")}); !used[k] {
					key, used[k] = k, true
				}
			}
			if err := r.Err(); err != nil {
				return err
			}
			if _, err := db.Update(ctx, table, sqlrepo.Values{"pending_key": key}, sqlrepo.Values{"id": id}); err != nil {
				return err
			}
		}
		return nil
	}
}

// Mapping devuelve el mapeo de las solicitudes sobre table. Entra en pánico si table tiene más de
// MaxTableLen caracteres.
func Mapping(table string) sqlrepo.Mapping[approval.RequestID, *approval.Request] {
	checkTable(table)
	return sqlrepo.Mapping[approval.RequestID, *approval.Request]{
		Table: table,
		Columns: []string{"company", "action", "target_type", "target_id", "payload", "reason", "requested_by",
			"requested_at", "status", "resolved_by", "resolved_at", "resolution_comment", "pending_key"},
		Unique: []sqlrepo.UniqueIndex{{Name: pendingIndex(table), Columns: []string{"pending_key"}, Err: approval.ErrPending}},
		Dehydrate: func(r *approval.Request) (sqlrepo.Values, error) {
			s := r.State()
			return sqlrepo.Values{"company": s.Company, "action": string(s.Action), "target_type": s.Target.Type,
				"target_id": s.Target.ID, "payload": opt(string(s.Payload)), "reason": opt(s.Reason),
				"requested_by": s.RequestedBy, "requested_at": s.RequestedAt, "status": string(s.Status),
				"resolved_by": optUUID(s.ResolvedBy), "resolved_at": optTime(s.ResolvedAt), "resolution_comment": opt(s.Comment),
				"pending_key": r.PendingKey()}, nil
		},
		Hydrate: func(row *sqlrepo.Row, _ sqlrepo.ChildRows) (*approval.Request, error) {
			s := approval.State{Company: row.UUID("company"), Action: approval.Action(row.String("action")),
				Target:  approval.Target{Type: row.String("target_type"), ID: row.String("target_id")},
				Payload: json.RawMessage(row.String("payload")), Reason: row.String("reason"),
				RequestedBy: row.UUID("requested_by"), RequestedAt: row.Time("requested_at"),
				Status: approval.Status(row.String("status")), ResolvedBy: row.UUID("resolved_by"),
				ResolvedAt: row.Time("resolved_at"), Comment: row.String("resolution_comment")}
			if s.Status == approval.StatusPending {
				// El grupo sólo cuenta mientras está pendiente y va al principio de la clave.
				if g, _, ok := strings.Cut(row.String("pending_key"), ":"); ok && g != string(s.Action) {
					s.Group = g
				}
			}
			if len(s.Payload) == 0 {
				s.Payload = nil
			}
			if err := row.Err(); err != nil {
				return nil, err
			}
			return approval.Reconstitute(approval.RequestID{UUID: row.UUID("id")}, s)
		},
	}
}

func opt(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func optUUID(u domain.UUID) any {
	if u.IsZero() {
		return nil
	}
	return u
}

func optTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
