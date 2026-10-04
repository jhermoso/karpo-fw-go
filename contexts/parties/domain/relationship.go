package domain

import (
	"fmt"
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
	// Hierarchical types relate a child (From) to its parent (To): a party has one current
	// parent per type and the hierarchy has no cycles (organization rollups).
	Hierarchical bool
	// Code is the stable, readable key of the type ("prospect"); optional and unique. It is what
	// tells which details a relationship of the type carries (the UDM subtypes of PARTY
	// RELATIONSHIP): a type without code, or with a code the domain does not know, carries none,
	// so adding a type without data of its own stays a catalog row.
	Code string
}

// IndexRelationshipTypes indexes the catalog by id, refusing two types with the same code.
func IndexRelationshipTypes(types []RelationshipType) (map[RelationshipTypeID]RelationshipType, error) {
	out := make(map[RelationshipTypeID]RelationshipType, len(types))
	codes := map[string]RelationshipTypeID{}
	for _, t := range types {
		if other, dup := codes[t.Code]; dup && t.Code != "" && other != t.ID {
			return nil, fmt.Errorf("%w: relationship types %s and %s share the code %q", fw.ErrValidation, other, t.ID, t.Code)
		}
		codes[t.Code] = t.ID
		out[t.ID] = t
	}
	return out, nil
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
	details  RelationshipDetails
}

// RelationshipDetails are the data a relationship carries because of its type (the UDM subtypes
// of PARTY RELATIONSHIP). As with PersonDetails and OrganizationDetails in Party, the type is
// data and the details of each type are value objects; the ones of the other types stay zero.
type RelationshipDetails struct {
	Prospect  ProspectDetails  // types with code CodeProspect
	Ownership OwnershipDetails // types with code CodeOwnership
}

// OwnershipDetails are the data of an ownership relationship: the stake of the shareholder.
type OwnershipDetails struct {
	// Share is the direct stake in the organization, in points (30 = 30 %); nil when unknown.
	Share *vocab.Percentage
}

// ProspectDetails are the data of a prospect relationship: the free trial the internal
// organization grants to the prospect.
type ProspectDetails struct {
	// TrialUntil is when the trial ends (nil: no trial granted). The trial starts with the
	// relationship; its default length is commercial policy and belongs to whoever grants it.
	TrialUntil *time.Time
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
	Details          RelationshipDetails
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
		from: s.From, to: s.To, fromRole: s.FromRole, toRole: s.ToRole, period: s.Period, remark: s.Remark, details: s.Details}, nil
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

// Details returns the data the relationship carries because of its type.
func (r *Relationship) Details() RelationshipDetails { return r.details }

// TrialUntil returns when the trial of a prospect relationship ends (nil without trial).
func (r *Relationship) TrialUntil() *time.Time { return r.details.Prospect.TrialUntil }

// InTrialAt reports whether the trial is in force at t: the relationship is, and t is before
// the end of the trial.
func (r *Relationship) InTrialAt(t time.Time) bool {
	until := r.details.Prospect.TrialUntil
	return until != nil && r.period.IsActiveAt(t) && t.Before(*until)
}

// SetTrial grants, extends, shortens or (with nil) withdraws the trial of a prospect
// relationship. t is the type of the relationship: only the types with code CodeProspect carry
// a trial. The trial ends after the relationship starts, and an ended relationship keeps the
// trial it had.
func (r *Relationship) SetTrial(t RelationshipType, until *time.Time) error {
	if t.ID != r.relType || t.Code != CodeProspect {
		return fw.Violation("parties.not_a_prospect_relationship", "only a prospect relationship has a trial")
	}
	if err := r.requireOpen(); err != nil {
		return err
	}
	if until != nil {
		u := until.UTC()
		if !u.After(r.Since()) {
			var v fw.Validation
			v.Add("trialUntil", "range", "the trial must end after the relationship starts")
			return v.Err()
		}
		until = &u
	}
	if old := r.details.Prospect.TrialUntil; (old == nil && until == nil) || (old != nil && until != nil && old.Equal(*until)) {
		return nil
	}
	r.details.Prospect.TrialUntil = until
	r.Raise(ProspectTrialChanged{EventMeta: r.NewEventMeta(), Prospect: r.from.String(), Organization: r.to.String(), TrialUntil: until})
	return nil
}

// requireOpen refuses to change the details of a relationship that has ended.
func (r *Relationship) requireOpen() error {
	if end, closed := r.period.To(); closed && !end.After(fw.Now()) {
		return fw.Violation("parties.relationship_ended", "the details of an ended relationship cannot change")
	}
	return nil
}

// OwnershipShare returns the stake of the shareholder (nil when unknown or not an ownership).
func (r *Relationship) OwnershipShare() *vocab.Percentage { return r.details.Ownership.Share }

var maxShare = vocab.DecimalFromInt(100)

// SetOwnershipShare records, corrects or (with nil) clears the direct stake of the shareholder in
// an ownership relationship: above 0 and up to 100 %, with two decimals at most. t is the type of
// the relationship: only the types with code CodeOwnership carry a stake.
func (r *Relationship) SetOwnershipShare(t RelationshipType, share *vocab.Percentage) error {
	if t.ID != r.relType || t.Code != CodeOwnership {
		return fw.Violation("parties.not_an_ownership_relationship", "only an ownership relationship has a share")
	}
	if err := r.requireOpen(); err != nil {
		return err
	}
	points := ""
	if share != nil {
		p := share.Points()
		if !p.IsPositive() || p.GreaterThan(maxShare) || !p.Equal(p.Round(2)) {
			var v fw.Validation
			v.Add("share", "range", "the share must be above 0 and up to 100, with two decimals at most")
			return v.Err()
		}
		points = p.StringFixed(2)
	}
	if old := r.details.Ownership.Share; (old == nil && share == nil) || (old != nil && share != nil && old.Equal(*share)) {
		return nil
	}
	r.details.Ownership.Share = share
	r.Raise(OwnershipShareChanged{EventMeta: r.NewEventMeta(), Shareholder: r.from.String(), Organization: r.to.String(), Share: points})
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (r *Relationship) AuditSnapshot() map[string]any {
	format := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.Format(time.RFC3339)
	}
	return map[string]any{"type": r.relType.String(), "from": r.from.String(), "to": r.to.String(),
		"since": r.Since().Format(time.RFC3339), "until": format(r.Until()), "remark": r.remark,
		"trialUntil": format(r.TrialUntil()), "share": shareText(r.OwnershipShare())}
}

// shareText renders a stake with two decimals ("" for nil).
func shareText(p *vocab.Percentage) string {
	if p == nil {
		return ""
	}
	return p.Points().StringFixed(2)
}
