package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

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
	ID RoleTypeID
	// Code is the stable, readable key of the type ("customer"); optional and unique. It is what
	// a model, a seed or an extension names the type by, instead of its identifier.
	Code        string
	Name        vocab.Name
	Description string
	Parent      *RoleTypeID
	Category    bool
}

// MaxTypeCode is the length of the longest code of a catalog type.
const MaxTypeCode = 40

var typeCode = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// CodeOf derives a code from a name: "Bill-To Customer" is "bill-to-customer".
func CodeOf(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
			continue
		}
		dash = true
	}
	return b.String()
}

// NewRoleType builds a role type somebody adds to the catalog: a role that can be assigned,
// placed in the hierarchy under its parent. It carries no data of its own (a type that needs
// some is an extension, docs/EXTENSIBILIDAD.md). Whether the parent exists, and whether the name
// and the code are free, is checked against the catalog (Catalog.Admits).
func NewRoleType(id RoleTypeID, name, description, code string, parent RoleTypeID) (RoleType, error) {
	var v fw.Validation
	n, err := vocab.NewName(name)
	v.Merge("", err)
	description = strings.Join(strings.Fields(description), " ")
	v.Require(len([]rune(description)) <= 500, "description", "length", "description must have at most 500 characters")
	code = strings.TrimSpace(code)
	v.Require(code == "" || (len(code) <= MaxTypeCode && typeCode.MatchString(code)), "code", "format",
		"code is written in lower case letters, digits and hyphens, starts with a letter and has at most 40 characters")
	v.Require(!id.IsZero(), "id", "required", "id is required")
	v.Require(!parent.IsZero(), "parent", "required", "a role type hangs from a parent in the hierarchy")
	if err := v.Err(); err != nil {
		return RoleType{}, err
	}
	return RoleType{ID: id, Code: code, Name: n, Description: description, Parent: &parent}, nil
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
	codes      map[string]RoleTypeID
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
	c := &Catalog{types: map[RoleTypeID]RoleType{}, codes: map[string]RoleTypeID{}, ancestors: map[RoleTypeID][]RoleTypeID{},
		appliesTo: map[RoleTypeID]Applicability{}, personCat: personCategory, orgCat: organizationCategory}
	for _, t := range types {
		if t.ID.IsZero() {
			return nil, fmt.Errorf("%w: role type without id", fw.ErrValidation)
		}
		if _, dup := c.types[t.ID]; dup {
			return nil, fmt.Errorf("%w: role type %s declared twice", fw.ErrValidation, t.ID)
		}
		if other, dup := c.codes[t.Code]; dup && t.Code != "" {
			return nil, fmt.Errorf("%w: role types %s and %s share the code %q", fw.ErrValidation, other, t.ID, t.Code)
		}
		if t.Code != "" {
			c.codes[t.Code] = t.ID
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

// ByCode returns the role type with a code.
func (c *Catalog) ByCode(code string) (RoleType, bool) {
	id, ok := c.codes[code]
	return c.types[id], ok && code != ""
}

// Admits checks that a new role type fits the catalog: its parent exists, and nobody has its
// name (whatever the case) or its code.
func (c *Catalog) Admits(t RoleType) error {
	if t.Parent == nil {
		return fw.Violation("parties.role_type_without_parent", "a role type hangs from a parent in the hierarchy")
	}
	if _, ok := c.types[*t.Parent]; !ok {
		var v fw.Validation
		v.Add("parent", "unknown", "unknown role type "+t.Parent.String())
		return v.Err()
	}
	if _, dup := c.types[t.ID]; dup {
		return fw.Violation("parties.role_type_exists", "the role type "+t.ID.String()+" already exists")
	}
	for _, other := range c.types {
		if strings.EqualFold(other.Name.String(), t.Name.String()) {
			return fw.Violation("parties.role_type_name_taken", "there is already a role type called "+other.Name.String())
		}
	}
	if _, dup := c.codes[t.Code]; dup && t.Code != "" {
		return fw.Violation("parties.role_type_code_taken", "there is already a role type with the code "+t.Code)
	}
	return nil
}

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
