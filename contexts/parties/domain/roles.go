package domain

import (
	"fmt"
	"slices"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Applicability tells which kind of party may play a role.
type Applicability string

// Applicabilities, derived from the position of the role in the hierarchy (under the Person or
// Organization category, or elsewhere).
const (
	AppliesToAny          Applicability = "any"
	AppliesToPerson       Applicability = "person"
	AppliesToOrganization Applicability = "organization"
)

// RoleType is an entry of the role type catalog. Categories group roles and cannot be assigned
// (the C# IsTag). A role type may have one parent; a party playing a role also plays every
// ancestor role (virtual inheritance: ancestors are derived, never stored).
type RoleType struct {
	ID          RoleTypeID
	Name        vocab.Name
	Description string
	Parent      *RoleTypeID
	Category    bool
}

// RoleCatalog answers questions about role types. It is a domain service built from the
// catalog data (see NewRoleCatalog); the aggregates receive it instead of reading ambient state.
type RoleCatalog interface {
	RoleType(id RoleTypeID) (RoleType, bool)
	// IsA reports whether role is ancestor or equal to the ancestor role.
	IsA(role, ancestor RoleTypeID) bool
	// Applicability returns which kind of party may play role.
	Applicability(role RoleTypeID) Applicability
}

// Catalog is the in-memory RoleCatalog. It validates the hierarchy when built: known parents,
// no cycles.
type Catalog struct {
	types      map[RoleTypeID]RoleType
	ancestors  map[RoleTypeID][]RoleTypeID
	appliesTo  map[RoleTypeID]Applicability
	personCat  RoleTypeID
	orgCat     RoleTypeID
	typesOrder []RoleTypeID
}

var _ RoleCatalog = (*Catalog)(nil)

// NewRoleCatalog builds a catalog. personCategory and organizationCategory are the categories
// whose descendants may only be played by people or by organizations.
func NewRoleCatalog(types []RoleType, personCategory, organizationCategory RoleTypeID) (*Catalog, error) {
	c := &Catalog{types: map[RoleTypeID]RoleType{}, ancestors: map[RoleTypeID][]RoleTypeID{},
		appliesTo: map[RoleTypeID]Applicability{}, personCat: personCategory, orgCat: organizationCategory}
	for _, t := range types {
		if t.ID.IsZero() {
			return nil, fmt.Errorf("%w: role type without id", fw.ErrValidation)
		}
		if _, dup := c.types[t.ID]; dup {
			return nil, fmt.Errorf("%w: role type %s declared twice", fw.ErrValidation, t.ID)
		}
		c.types[t.ID] = t
		c.typesOrder = append(c.typesOrder, t.ID)
	}
	for _, t := range types {
		seen := map[RoleTypeID]bool{t.ID: true}
		for p := t.Parent; p != nil; {
			parent, ok := c.types[*p]
			if !ok {
				return nil, fmt.Errorf("%w: role type %s has unknown parent %s", fw.ErrValidation, t.ID, *p)
			}
			if seen[parent.ID] {
				return nil, fmt.Errorf("%w: role type hierarchy has a cycle at %s", fw.ErrValidation, t.ID)
			}
			seen[parent.ID] = true
			c.ancestors[t.ID] = append(c.ancestors[t.ID], parent.ID)
			p = parent.Parent
		}
		c.appliesTo[t.ID] = AppliesToAny
		for _, a := range append([]RoleTypeID{t.ID}, c.ancestors[t.ID]...) {
			switch a {
			case personCategory:
				c.appliesTo[t.ID] = AppliesToPerson
			case organizationCategory:
				c.appliesTo[t.ID] = AppliesToOrganization
			}
		}
	}
	return c, nil
}

// RoleType implements RoleCatalog.
func (c *Catalog) RoleType(id RoleTypeID) (RoleType, bool) { t, ok := c.types[id]; return t, ok }

// IsA implements RoleCatalog.
func (c *Catalog) IsA(role, ancestor RoleTypeID) bool {
	return role == ancestor || slices.Contains(c.ancestors[role], ancestor)
}

// Applicability implements RoleCatalog.
func (c *Catalog) Applicability(role RoleTypeID) Applicability { return c.appliesTo[role] }

// Ancestors returns the ancestors of role, nearest first.
func (c *Catalog) Ancestors(role RoleTypeID) []RoleTypeID { return slices.Clone(c.ancestors[role]) }

// Descendants returns role and every role below it (to search parties "playing role X").
func (c *Catalog) Descendants(role RoleTypeID) []RoleTypeID {
	out := []RoleTypeID{}
	for _, id := range c.typesOrder {
		if c.IsA(id, role) {
			out = append(out, id)
		}
	}
	return out
}

// All returns every role type in declaration order.
func (c *Catalog) All() []RoleType {
	out := make([]RoleType, len(c.typesOrder))
	for i, id := range c.typesOrder {
		out[i] = c.types[id]
	}
	return out
}

// CanPlay checks that a party of kind may be given role: known, not a category, compatible.
func CanPlay(c RoleCatalog, kind Kind, role RoleTypeID) error {
	t, ok := c.RoleType(role)
	if !ok {
		var v fw.Validation
		v.Add("roleType", "unknown", "unknown role type "+role.String())
		return v.Err()
	}
	if t.Category {
		return fw.Violation("parties.role_is_category", t.Name.String()+" is a category and cannot be assigned")
	}
	switch a := c.Applicability(role); {
	case a == AppliesToPerson && kind != KindPerson:
		return fw.Violation("parties.role_incompatible", t.Name.String()+" can only be played by a person")
	case a == AppliesToOrganization && kind != KindOrganization:
		return fw.Violation("parties.role_incompatible", t.Name.String()+" can only be played by an organization")
	}
	return nil
}
