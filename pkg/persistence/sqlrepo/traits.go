package sqlrepo

import (
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// AuditColumns are the conventional columns of a traits.AuditStamp. Append them to
// Mapping.Columns and use AuditStampValues / Row.AuditStamp in Dehydrate / Hydrate.
var AuditColumns = []string{"created_at", "created_by_id", "created_by_name", "modified_at", "modified_by_id", "modified_by_name"}

// WithAuditColumns returns columns followed by AuditColumns.
func WithAuditColumns(columns ...string) []string {
	return append(append([]string{}, columns...), AuditColumns...)
}

func actorID(a vocab.Actor) any {
	if a.IsZero() || a.IsSystem() {
		return nil
	}
	return a.PartyID.String()
}

func actorName(a vocab.Actor) any {
	if a.IsZero() {
		return nil
	}
	return a.Name
}

// AuditStampValues writes a stamp into vals (creating the map when nil) and returns it.
func AuditStampValues(vals Values, s traits.AuditStamp) Values {
	if vals == nil {
		vals = Values{}
	}
	vals["created_at"] = nil // an unstamped aggregate stores NULL, never Go's zero time
	if !s.CreatedAt.IsZero() {
		vals["created_at"] = s.CreatedAt
	}
	vals["created_by_id"] = actorID(s.CreatedBy)
	vals["created_by_name"] = actorName(s.CreatedBy)
	vals["modified_at"] = nil
	if !s.ModifiedAt.IsZero() {
		vals["modified_at"] = s.ModifiedAt
	}
	vals["modified_by_id"] = actorID(s.ModifiedBy)
	vals["modified_by_name"] = actorName(s.ModifiedBy)
	return vals
}

func (r *Row) actor(idCol, nameCol string) vocab.Actor {
	name := r.String(nameCol)
	if name == "" {
		return vocab.Actor{}
	}
	id := r.String(idCol)
	if id == "" {
		return vocab.Actor{Name: name}
	}
	u, err := domain.ParseUUID(id)
	if err != nil {
		r.fail(idCol, err)
	}
	return vocab.Actor{PartyID: u, Name: name}
}

// AuditStamp reads the AuditColumns of the row.
func (r *Row) AuditStamp() traits.AuditStamp {
	s := traits.AuditStamp{
		CreatedAt:  r.Time("created_at"),
		CreatedBy:  r.actor("created_by_id", "created_by_name"),
		ModifiedBy: r.actor("modified_by_id", "modified_by_name"),
	}
	if t := r.NullTime("modified_at"); t != nil {
		s.ModifiedAt = *t
	}
	return s
}
