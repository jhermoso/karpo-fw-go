// Package approvals guarda las solicitudes de aprobación (pkg/domain/approval) con sqlrepo, en
// cualquier motor. La tabla es de cada contexto que use aprobaciones, como su outbox: el contexto
// elige el nombre (convención <prefijo-del-contexto>_approvals, p. ej. "pur_approvals"), añade DDL(tabla) a sus migraciones y
// abre el repositorio con sqlrepo.NewRepository(db, Mapping(tabla)). En memoria basta
// memory.NewRepository[approval.RequestID, *approval.Request].
package approvals

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/approval"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// MaxTableLen es la longitud máxima del nombre de la tabla: el índice "ix_<tabla>_target" no puede
// pasar de 30 caracteres en Oracle (ORA-00972).
const MaxTableLen = 23

// checkTable entra en pánico si el nombre de la tabla es demasiado largo (error de composición).
func checkTable(table string) {
	if table == "" || len(table) > MaxTableLen {
		panic(fmt.Sprintf("approvals: table name %q must have 1 to %d characters", table, MaxTableLen))
	}
}

// DDL devuelve la tabla y su índice, con los marcadores de dialecto de sqlrepo.RenderDDL. Entra en
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

// Mapping devuelve el mapeo de las solicitudes sobre table. Entra en pánico si table tiene más de
// MaxTableLen caracteres.
func Mapping(table string) sqlrepo.Mapping[approval.RequestID, *approval.Request] {
	checkTable(table)
	return sqlrepo.Mapping[approval.RequestID, *approval.Request]{
		Table: table,
		Columns: []string{"company", "action", "target_type", "target_id", "payload", "reason", "requested_by",
			"requested_at", "status", "resolved_by", "resolved_at", "resolution_comment"},
		Dehydrate: func(r *approval.Request) (sqlrepo.Values, error) {
			s := r.State()
			return sqlrepo.Values{"company": s.Company, "action": string(s.Action), "target_type": s.Target.Type,
				"target_id": s.Target.ID, "payload": opt(string(s.Payload)), "reason": opt(s.Reason),
				"requested_by": s.RequestedBy, "requested_at": s.RequestedAt, "status": string(s.Status),
				"resolved_by": optUUID(s.ResolvedBy), "resolved_at": optTime(s.ResolvedAt), "resolution_comment": opt(s.Comment)}, nil
		},
		Hydrate: func(row *sqlrepo.Row, _ sqlrepo.ChildRows) (*approval.Request, error) {
			s := approval.State{Company: row.UUID("company"), Action: approval.Action(row.String("action")),
				Target:  approval.Target{Type: row.String("target_type"), ID: row.String("target_id")},
				Payload: json.RawMessage(row.String("payload")), Reason: row.String("reason"),
				RequestedBy: row.UUID("requested_by"), RequestedAt: row.Time("requested_at"),
				Status: approval.Status(row.String("status")), ResolvedBy: row.UUID("resolved_by"),
				ResolvedAt: row.Time("resolved_at"), Comment: row.String("resolution_comment")}
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
