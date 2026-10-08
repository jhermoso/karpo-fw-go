package traits

import (
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// AuditStamp records who created and last modified an aggregate, and when.
type AuditStamp struct {
	CreatedAt  time.Time   `json:"createdAt"`
	CreatedBy  vocab.Actor `json:"createdBy"`
	ModifiedAt time.Time   `json:"modifiedAt,omitzero"`
	ModifiedBy vocab.Actor `json:"modifiedBy,omitzero"`
}

// Auditable is implemented by aggregates that embed Audited. The interface is sealed: the stamp
// can only be written through Stamp, which the application layer calls with the actor of the
// request, so aggregates cannot forge it (in C# the entity stamped itself and always recorded
// Actor.System).
type Auditable interface {
	AuditStamp() AuditStamp
	stamp(actor vocab.Actor, at time.Time)
}

// Audited is the embeddable audit stamp. The change history ("audit trail") is not kept inside
// the aggregate: the application layer records it in an audit log together with the state
// change (see application.AuditLog), like the C# EfUnitOfWork did.
type Audited struct {
	s AuditStamp
}

// RestoredAudit rebuilds the stamp from persistence.
func RestoredAudit(s AuditStamp) Audited { return Audited{s: s} }

// AuditStamp returns the stamp.
func (a Audited) AuditStamp() AuditStamp { return a.s }

// CreatedAt returns the creation instant (zero until first saved).
func (a Audited) CreatedAt() time.Time { return a.s.CreatedAt }

// CreatedBy returns the creator.
func (a Audited) CreatedBy() vocab.Actor { return a.s.CreatedBy }

// ModifiedAt returns the last modification instant (zero if never modified).
func (a Audited) ModifiedAt() time.Time { return a.s.ModifiedAt }

// ModifiedBy returns the last modifier.
func (a Audited) ModifiedBy() vocab.Actor { return a.s.ModifiedBy }

func (a *Audited) stamp(actor vocab.Actor, at time.Time) {
	if a.s.CreatedAt.IsZero() {
		a.s.CreatedAt, a.s.CreatedBy = at, actor
		return
	}
	a.s.ModifiedAt, a.s.ModifiedBy = at, actor
}

// Stamp records that actor created (first time) or modified the aggregate at t.
// Reserved for the application layer (orchestration.Orchestrator calls it before saving).
func Stamp(x Auditable, actor vocab.Actor, at time.Time) { x.stamp(actor, at.UTC()) }

// Snapshotter is implemented by aggregates that expose the fields to audit as a flat map of
// comparable values (strings, numbers, booleans, identifiers...). The application layer diffs
// the snapshots taken before and after a command to record field-level changes, the Go
// counterpart of the C# change-tracker based audit.
type Snapshotter interface {
	AuditSnapshot() map[string]any
}

// FieldChange is one audited field change.
type FieldChange struct {
	Field string `json:"field"`
	Old   any    `json:"old,omitempty"`
	New   any    `json:"new,omitempty"`
}

// Diff compares two snapshots and returns the changed fields sorted by name.
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
