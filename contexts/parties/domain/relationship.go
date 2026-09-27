package domain

import (
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// RelationshipKind is the stable aggregate type name of Relationship.
const RelationshipKind = "parties.relationship"

// RelationshipType is an entry of the relationship type catalog: the role each side must play
// ("Employment" relates an Employee to an Internal Organization).
type RelationshipType struct {
	ID          RelationshipTypeID
	Name        vocab.Name
	Description string
	FromRole    RoleTypeID
	ToRole      RoleTypeID
}

// Relationship links two parties playing the roles its type requires, during a period
// (UDM PARTY RELATIONSHIP). It is an aggregate of its own: a party can have thousands of
// relationships, and each one changes independently.
type Relationship struct {
	fw.BaseAggregateRoot[RelationshipID]
	traits.Audited
	relType  RelationshipTypeID
	from, to PartyID
	fromRole RoleTypeID
	toRole   RoleTypeID
	period   vocab.ValidPeriod
	remark   string
}

// Establish creates a relationship between two parties, checking what the C# left to stubs:
// the parties differ, are active, and play the roles required by the type (or roles below them
// in the hierarchy) when the relationship starts. The parties are read, never modified.
func Establish(id RelationshipID, t RelationshipType, from, to *Party, c RoleCatalog, since time.Time, remark string) (*Relationship, error) {
	if from.ID() == to.ID() {
		return nil, fw.Violation("parties.self_relationship", "a party cannot be related to itself")
	}
	for _, s := range []struct {
		p    *Party
		role RoleTypeID
		side string
	}{{from, t.FromRole, "origin"}, {to, t.ToRole, "destination"}} {
		if !s.p.IsActive() {
			return nil, fw.Violation("parties.inactive", "the "+s.side+" party is inactive")
		}
		if !s.p.PlaysAt(c, s.role, since) {
			name := s.role.String()
			if rt, ok := c.RoleType(s.role); ok {
				name = rt.Name.String()
			}
			return nil, fw.Violation("parties.relationship_role_missing",
				"the "+s.side+" party does not play the role "+name+" required by "+t.Name.String())
		}
	}
	period, err := vocab.OpenPeriodFrom(since)
	if err != nil {
		return nil, err
	}
	r, err := ReconstituteRelationship(id, RelationshipState{
		Type: t.ID, From: from.ID(), To: to.ID(), FromRole: t.FromRole, ToRole: t.ToRole, Period: period, Remark: remark,
	})
	if err != nil {
		return nil, err
	}
	r.Raise(RelationshipEstablished{EventMeta: r.NewEventMeta(), Type: t.ID.String(), From: from.ID().String(),
		To: to.ID().String(), FromRole: t.FromRole.String(), ToRole: t.ToRole.String(), Since: period.From()})
	return r, nil
}

// RelationshipState is the persisted state of a relationship.
type RelationshipState struct {
	Type             RelationshipTypeID
	From, To         PartyID
	FromRole, ToRole RoleTypeID
	Period           vocab.ValidPeriod
	Remark           string
	Audit            traits.AuditStamp
}

// ReconstituteRelationship rebuilds a relationship from persisted state, without events.
func ReconstituteRelationship(id RelationshipID, s RelationshipState) (*Relationship, error) {
	base, err := fw.NewBaseAggregateRoot(RelationshipKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Type.IsZero(), "type", "required", "relationship type is required")
	v.Require(!s.From.IsZero() && !s.To.IsZero(), "parties", "required", "both parties are required")
	v.Require(!s.Period.IsZero(), "since", "required", "start is required")
	v.Require(len([]rune(s.Remark)) <= 1000, "remark", "length", "at most 1000 characters")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Relationship{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), relType: s.Type,
		from: s.From, to: s.To, fromRole: s.FromRole, toRole: s.ToRole, period: s.Period, remark: s.Remark}, nil
}

// Type returns the relationship type.
func (r *Relationship) Type() RelationshipTypeID { return r.relType }

// From returns the origin party.
func (r *Relationship) From() PartyID { return r.from }

// To returns the destination party.
func (r *Relationship) To() PartyID { return r.to }

// FromRole returns the role of the origin party.
func (r *Relationship) FromRole() RoleTypeID { return r.fromRole }

// ToRole returns the role of the destination party.
func (r *Relationship) ToRole() RoleTypeID { return r.toRole }

// Period returns the validity of the relationship.
func (r *Relationship) Period() vocab.ValidPeriod { return r.period }

// Remark returns the free comment.
func (r *Relationship) Remark() string { return r.remark }

// Since returns when the relationship started.
func (r *Relationship) Since() time.Time { return r.period.From() }

// Until returns when the relationship ended (nil while active).
func (r *Relationship) Until() *time.Time {
	if t, ok := r.period.To(); ok {
		return &t
	}
	return nil
}

// Involves reports whether the party is one of the sides.
func (r *Relationship) Involves(p PartyID) bool { return r.from == p || r.to == p }

// Terminate ends the relationship at a moment (not before it started). Terminating an ended
// relationship earlier than its end moves the end back; later is a no-op.
func (r *Relationship) Terminate(at time.Time) error {
	if end, closed := r.period.To(); closed && !at.Before(end) {
		return nil
	}
	p, err := vocab.NewValidPeriod(r.period.From(), &at)
	if err != nil {
		return fw.Violation("parties.relationship_end_before_start", "a relationship cannot end before it starts")
	}
	r.period = p
	r.Raise(RelationshipTerminated{EventMeta: r.NewEventMeta(), At: at.UTC()})
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (r *Relationship) AuditSnapshot() map[string]any {
	until := ""
	if t := r.Until(); t != nil {
		until = t.Format(time.RFC3339)
	}
	return map[string]any{"type": r.relType.String(), "from": r.from.String(), "to": r.to.String(),
		"since": r.Since().Format(time.RFC3339), "until": until, "remark": r.remark}
}
