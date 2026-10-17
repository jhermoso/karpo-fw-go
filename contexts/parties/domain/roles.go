package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Applicability dice qué clase de party puede desempeñar un rol.
type Applicability string

// Aplicabilidades, deducidas de la posición del rol en la jerarquía (bajo la categoría Person u
// Organization, o en otro sitio).
const (
	AppliesToAny          Applicability = "any"
	AppliesToPerson       Applicability = "person"
	AppliesToOrganization Applicability = "organization"
)

// RoleTypeKind y RelationshipTypeKind nombran las entradas del catálogo en el registro de auditoría.
const (
	RoleTypeKind         = "parties.role_type"
	RelationshipTypeKind = "parties.relationship_type"
)

// RoleType es una entrada del catálogo de tipos de rol. Las categorías agrupan roles y no se
// pueden asignar (el IsTag de C#). Un tipo de rol puede tener un padre; una party que desempeña un
// rol desempeña también todos sus roles antepasados (herencia virtual: los antepasados se deducen,
// nunca se guardan).
type RoleType struct {
	ID RoleTypeID
	// Code es la clave estable y legible del tipo ("customer"); opcional y única. Es el nombre con
	// el que un modelo, una semilla o una extensión se refieren al tipo, en lugar de su identificador.
	Code        string
	Name        vocab.Name
	Description string
	Parent      *RoleTypeID
	Category    bool
}

// MaxTypeCode es la longitud del código más largo de un tipo del catálogo.
const MaxTypeCode = 40

var typeCode = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// CodeOf deduce un código a partir de un nombre: "Bill-To Customer" es "bill-to-customer".
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

// NewRoleType construye un tipo de rol que alguien añade al catálogo: un rol que se puede asignar,
// colocado en la jerarquía bajo su padre. No lleva datos propios (un tipo que los necesite es una
// extensión, docs/EXTENSIBILIDAD.md). Que el padre exista, y que el nombre y el código estén
// libres, se comprueba contra el catálogo (Catalog.Admits).
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

// RoleCatalog responde a preguntas sobre los tipos de rol. Es un servicio de dominio construido a
// partir de los datos del catálogo (ver NewRoleCatalog); los agregados lo reciben en lugar de leer
// estado ambiental.
type RoleCatalog interface {
	RoleType(id RoleTypeID) (RoleType, bool)
	// IsA dice si role es ancestor o desciende de él.
	IsA(role, ancestor RoleTypeID) bool
	// Applicability devuelve qué clase de party puede desempeñar role.
	Applicability(role RoleTypeID) Applicability
}

// Catalog es el RoleCatalog en memoria. Valida la jerarquía al construirse: padres conocidos, sin
// ciclos.
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

// NewRoleCatalog construye un catálogo. personCategory y organizationCategory son las categorías
// cuyos descendientes sólo pueden desempeñar personas u organizaciones, respectivamente.
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

// RoleType implementa RoleCatalog.
func (c *Catalog) RoleType(id RoleTypeID) (RoleType, bool) { t, ok := c.types[id]; return t, ok }

// ByCode devuelve el tipo de rol que tiene un código.
func (c *Catalog) ByCode(code string) (RoleType, bool) {
	id, ok := c.codes[code]
	return c.types[id], ok && code != ""
}

// Admits comprueba que un tipo de rol nuevo encaja en el catálogo: su padre existe y nadie tiene
// su nombre (sin distinguir mayúsculas) ni su código.
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

// IsA implementa RoleCatalog.
func (c *Catalog) IsA(role, ancestor RoleTypeID) bool {
	return role == ancestor || slices.Contains(c.ancestors[role], ancestor)
}

// Applicability implementa RoleCatalog.
func (c *Catalog) Applicability(role RoleTypeID) Applicability { return c.appliesTo[role] }

// Ancestors devuelve los antepasados de role, primero el más cercano.
func (c *Catalog) Ancestors(role RoleTypeID) []RoleTypeID { return slices.Clone(c.ancestors[role]) }

// Descendants devuelve role y todos los roles por debajo de él (para buscar parties «que
// desempeñan el rol X»).
func (c *Catalog) Descendants(role RoleTypeID) []RoleTypeID {
	out := []RoleTypeID{}
	for _, id := range c.typesOrder {
		if c.IsA(id, role) {
			out = append(out, id)
		}
	}
	return out
}

// All devuelve todos los tipos de rol en el orden en que se declararon.
func (c *Catalog) All() []RoleType {
	out := make([]RoleType, len(c.typesOrder))
	for i, id := range c.typesOrder {
		out[i] = c.types[id]
	}
	return out
}

// CanPlay comprueba que a una party de clase kind se le puede dar role: conocido, no es una
// categoría, compatible.
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
