package domain

import (
	"fmt"
	"slices"
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// ClassificationTypeID identifies a classification (same GUIDs as the C# party_type rows).
type ClassificationTypeID struct{ fw.UUID }

// ParseClassificationTypeID parses a textual identity.
func ParseClassificationTypeID(s string) (ClassificationTypeID, error) {
	u, err := fw.ParseUUID(s)
	return ClassificationTypeID{u}, err
}

// MustClassificationTypeID parses a well-known identity.
func MustClassificationTypeID(s string) ClassificationTypeID {
	return ClassificationTypeID{fw.MustParseUUID(s)}
}

// ClassificationID identifies a classification of a party (child entity).
type ClassificationID struct{ fw.UUID }

// NewClassificationID returns a new identity.
func NewClassificationID() ClassificationID { return ClassificationID{fw.NewUUID()} }

// ParseClassificationID parses a textual identity.
func ParseClassificationID(s string) (ClassificationID, error) {
	u, err := fw.ParseUUID(s)
	return ClassificationID{u}, err
}

// ClassificationType is an entry of the classification catalog: families (customer segment,
// activity sector, organization size, acquisition channel, AML risk) group the leaves that are
// assigned to parties. An inactive family (AML until compliance enables it) and its leaves
// cannot be assigned. In an exclusive family a party has one current leaf at a time.
type ClassificationType struct {
	ID          ClassificationTypeID
	Name        vocab.Name
	Description string
	Family      *ClassificationTypeID // nil for a family
	Active      bool
	AppliesTo   Applicability
	Exclusive   bool
}

// ClassificationCatalog answers questions about classifications.
type ClassificationCatalog struct {
	types map[ClassificationTypeID]ClassificationType
	order []ClassificationTypeID
}

// NewClassificationCatalog builds the catalog: two levels, families first.
func NewClassificationCatalog(types []ClassificationType) (*ClassificationCatalog, error) {
	c := &ClassificationCatalog{types: map[ClassificationTypeID]ClassificationType{}}
	for _, t := range types {
		c.types[t.ID] = t
		c.order = append(c.order, t.ID)
	}
	for _, t := range types {
		if t.Family == nil {
			continue
		}
		f, ok := c.types[*t.Family]
		if !ok || f.Family != nil {
			return nil, fmt.Errorf("%w: classification %s must belong to a known family", fw.ErrValidation, t.ID)
		}
	}
	return c, nil
}

// Type returns a classification type.
func (c *ClassificationCatalog) Type(id ClassificationTypeID) (ClassificationType, bool) {
	t, ok := c.types[id]
	return t, ok
}

// All returns every type in declaration order.
func (c *ClassificationCatalog) All() []ClassificationType {
	out := make([]ClassificationType, len(c.order))
	for i, id := range c.order {
		out[i] = c.types[id]
	}
	return out
}

// Classification is a classification of a party during a period (child entity of Party).
type Classification struct {
	ID     ClassificationID
	Type   ClassificationTypeID
	Period vocab.ValidPeriod
}

// Classify classifies the party from a moment on. Invariants: the type is an active leaf of an
// active family, suits the party kind, is not already current, and, in an exclusive family, no
// other leaf of the family is current in the period.
func (p *Party) Classify(c *ClassificationCatalog, typ ClassificationTypeID, from time.Time) (ClassificationID, error) {
	if err := p.requireActive("be classified"); err != nil {
		return ClassificationID{}, err
	}
	t, ok := c.Type(typ)
	if !ok {
		var v fw.Validation
		v.Add("classification", "unknown", "unknown classification")
		return ClassificationID{}, v.Err()
	}
	if t.Family == nil {
		return ClassificationID{}, fw.Violation("parties.classification_is_family", t.Name.String()+" is a family and cannot be assigned")
	}
	family, _ := c.Type(*t.Family)
	if !t.Active || !family.Active {
		return ClassificationID{}, fw.Violation("parties.classification_inactive", t.Name.String()+" is not active")
	}
	if a := family.AppliesTo; (a == AppliesToPerson && p.kind != KindPerson) || (a == AppliesToOrganization && p.kind != KindOrganization) {
		return ClassificationID{}, fw.Violation("parties.classification_incompatible", family.Name.String()+" does not apply to this kind of party")
	}
	period, err := vocab.OpenPeriodFrom(from)
	if err != nil {
		return ClassificationID{}, err
	}
	for _, x := range p.classifications {
		if !x.Period.Overlaps(period) {
			continue
		}
		if x.Type == typ {
			return ClassificationID{}, fw.Violation("parties.classification_overlap", "the party already has this classification in that period")
		}
		if other, ok := c.Type(x.Type); ok && family.Exclusive && other.Family != nil && *other.Family == family.ID {
			return ClassificationID{}, fw.Violation("parties.classification_exclusive",
				"the party already has a "+family.Name.String()+" in that period: end it first")
		}
	}
	cl := Classification{ID: NewClassificationID(), Type: typ, Period: period}
	p.classifications = append(slices.Clone(p.classifications), cl)
	p.Raise(PartyClassified{EventMeta: p.NewEventMeta(), ClassificationID: cl.ID.String(), Type: typ.String(), From: period.From()})
	return cl.ID, nil
}

// EndClassification ends a classification at a moment (not before it started).
func (p *Party) EndClassification(id ClassificationID, at time.Time) error {
	k := slices.IndexFunc(p.classifications, func(c Classification) bool { return c.ID == id })
	if k < 0 {
		return fw.NotFound("parties.classification", id)
	}
	c := p.classifications[k]
	if end, closed := c.Period.To(); closed && !at.Before(end) {
		return nil
	}
	period, err := vocab.NewValidPeriod(c.Period.From(), &at)
	if err != nil {
		return fw.Violation("parties.classification_end_before_start", "a classification cannot end before it starts")
	}
	p.classifications = slices.Clone(p.classifications)
	p.classifications[k].Period = period
	p.Raise(PartyClassificationEnded{EventMeta: p.NewEventMeta(), ClassificationID: id.String(), Type: c.Type.String(), At: at.UTC()})
	return nil
}

// Classifications returns a copy of the classifications, current or past.
func (p *Party) Classifications() []Classification { return slices.Clone(p.classifications) }
