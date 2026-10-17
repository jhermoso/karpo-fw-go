package traits

import (
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// AuditStamp registra quién creó y quién modificó por última vez un agregado, y cuándo.
type AuditStamp struct {
	CreatedAt  time.Time   `json:"createdAt"`
	CreatedBy  vocab.Actor `json:"createdBy"`
	ModifiedAt time.Time   `json:"modifiedAt,omitzero"`
	ModifiedBy vocab.Actor `json:"modifiedBy,omitzero"`
}

// Auditable lo implementan los agregados que incrustan Audited. La interfaz está sellada: el sello
// sólo se escribe mediante Stamp, que la capa de aplicación llama con el actor de la petición, así
// que los agregados no pueden falsificarlo (en C# la entidad se sellaba a sí misma y siempre
// registraba Actor.System).
type Auditable interface {
	AuditStamp() AuditStamp
	stamp(actor vocab.Actor, at time.Time)
}

// Audited es el sello de auditoría incrustable. El historial de cambios («rastro de auditoría») no
// se guarda dentro del agregado: la capa de aplicación lo registra en un log de auditoría junto con
// el cambio de estado (véase application.AuditLog), como hacía el EfUnitOfWork de C#.
type Audited struct {
	s AuditStamp
}

// RestoredAudit reconstruye el sello desde la persistencia.
func RestoredAudit(s AuditStamp) Audited { return Audited{s: s} }

// AuditStamp devuelve el sello.
func (a Audited) AuditStamp() AuditStamp { return a.s }

// CreatedAt devuelve el instante de creación (cero hasta que se guarda por primera vez).
func (a Audited) CreatedAt() time.Time { return a.s.CreatedAt }

// CreatedBy devuelve quién lo creó.
func (a Audited) CreatedBy() vocab.Actor { return a.s.CreatedBy }

// ModifiedAt devuelve el instante de la última modificación (cero si nunca se ha modificado).
func (a Audited) ModifiedAt() time.Time { return a.s.ModifiedAt }

// ModifiedBy devuelve quién lo modificó por última vez.
func (a Audited) ModifiedBy() vocab.Actor { return a.s.ModifiedBy }

func (a *Audited) stamp(actor vocab.Actor, at time.Time) {
	if a.s.CreatedAt.IsZero() {
		a.s.CreatedAt, a.s.CreatedBy = at, actor
		return
	}
	a.s.ModifiedAt, a.s.ModifiedBy = at, actor
}

// Stamp registra que actor creó (la primera vez) o modificó el agregado en t.
// Reservada a la capa de aplicación (orchestration.Orchestrator la llama antes de guardar).
func Stamp(x Auditable, actor vocab.Actor, at time.Time) { x.stamp(actor, at.UTC()) }

// Snapshotter lo implementan los agregados que exponen los campos a auditar como un mapa plano de
// valores comparables (cadenas, números, booleanos, identificadores...). La capa de aplicación compara
// las instantáneas tomadas antes y después de una orden para registrar los cambios campo a campo:
// la contrapartida en Go de la auditoría de C# basada en el change tracker.
type Snapshotter interface {
	AuditSnapshot() map[string]any
}

// FieldChange es un cambio auditado de un campo.
type FieldChange struct {
	Field string `json:"field"`
	Old   any    `json:"old,omitempty"`
	New   any    `json:"new,omitempty"`
}

// Diff compara dos instantáneas y devuelve los campos cambiados ordenados por nombre.
func Diff(before, after map[string]any) []FieldChange {
	var out []FieldChange
	seen := map[string]bool{}
	for k, nv := range after {
		seen[k] = true
		if ov, ok := before[k]; !ok || !equalValue(ov, nv) {
			out = append(out, FieldChange{Field: k, Old: before[k], New: nv})
		}
	}
	for k, ov := range before {
		if !seen[k] {
			out = append(out, FieldChange{Field: k, Old: ov})
		}
	}
	sortChanges(out)
	return out
}
