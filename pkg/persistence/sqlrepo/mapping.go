package sqlrepo

import (
	"fmt"
	"regexp"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Values holds column values of one row, keyed by column name.
type Values map[string]any

// CustomSQL translates a custom specification (spec.Custom) to a SQL boolean expression.
// Use b.Column to reference mapped fields of the current table, b.Arg to bind parameters and
// b.Dialect to branch on engine-specific syntax.
type CustomSQL func(b *Builder, args []any) (string, error)

// Mapping maps an aggregate root to tables. It is defined once per aggregate in the bounded
// context's infrastructure layer and works with every Dialect.
type Mapping[ID domain.Identifier, T domain.AggregateRoot[ID]] struct {
	// Table is the root table.
	Table string
	// IDColumn is the primary key column (default "id"). It is exposed to specifications as
	// the logical field "id".
	IDColumn string
	// VersionColumn stores the optimistic concurrency version (default "version").
	VersionColumn string
	// Columns are the data columns of the root table (excluding id and version).
	Columns []string
	// Fields maps logical specification field names to columns when they differ.
	// Columns are always addressable by their own name.
	Fields map[string]string
	// Dehydrate returns the value of every column in Columns.
	Dehydrate func(agg T) (Values, error)
	// Hydrate rebuilds the whole aggregate from its root row (id, version and Columns) and the
	// already loaded rows of its child collections, typically through a Reconstitute
	// constructor of the domain. It must not raise domain events.
	Hydrate func(row *Row, children ChildRows) (T, error)
	// Children maps child entity collections stored in their own tables.
	Children []Child[T]
	// Custom translates custom specifications (spec.Custom) by name.
	Custom map[string]CustomSQL
}

// ChildRows holds the loaded rows of each child collection of one aggregate, by child Name.
type ChildRows map[string][]*Row

// Of returns the rows of the named child collection (in Child.OrderBy order).
func (c ChildRows) Of(name string) []*Row { return c[name] }

// Child maps a collection of child entities or value objects of the aggregate to a table.
// Children are saved with a replace strategy (delete + insert) inside the aggregate's
// transaction, and loaded in batches before hydration (no N+1).
type Child[T any] struct {
	// Name is the logical collection name used by spec.Collection(...).Any.
	Name string
	// Table is the child table.
	Table string
	// ForeignKey is the column referencing the root identity.
	ForeignKey string
	// Columns are the data columns (excluding ForeignKey).
	Columns []string
	// Fields maps logical field names to columns when they differ.
	Fields map[string]string
	// OrderBy lists columns that define the order children are loaded in.
	OrderBy []string
	// Dehydrate returns one Values per child element.
	Dehydrate func(root T) ([]Values, error)
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func checkIdent(kind, s string) error {
	if !identRe.MatchString(s) {
		return fmt.Errorf("sqlrepo: invalid %s identifier %q", kind, s)
	}
	return nil
}

// table is the normalized, validated form of a mapped table used by the translator.
type table struct {
	name     string
	key      string // id column (root) or foreign key (child)
	columns  []string
	fields   map[string]string
	children map[string]*childTable
}

type childTable struct {
	table
	orderBy []string
}

func buildFields(columns []string, extra map[string]string) (map[string]string, error) {
	fields := make(map[string]string, len(columns)+len(extra))
	for _, c := range columns {
		if err := checkIdent("column", c); err != nil {
			return nil, err
		}
		fields[c] = c
	}
	for f, c := range extra {
		if err := checkIdent("column", c); err != nil {
			return nil, err
		}
		fields[f] = c
	}
	return fields, nil
}

func (m *Mapping[ID, T]) normalize() (*table, error) {
	if m.IDColumn == "" {
		m.IDColumn = "id"
	}
	if m.VersionColumn == "" {
		m.VersionColumn = "version"
	}
	if m.Dehydrate == nil || m.Hydrate == nil {
		return nil, fmt.Errorf("sqlrepo: mapping for %q needs Dehydrate and Hydrate", m.Table)
	}
	for _, id := range []struct{ kind, v string }{{"table", m.Table}, {"id column", m.IDColumn}, {"version column", m.VersionColumn}} {
		if err := checkIdent(id.kind, id.v); err != nil {
			return nil, err
		}
	}
	fields, err := buildFields(m.Columns, m.Fields)
	if err != nil {
		return nil, err
	}
	if _, ok := fields["id"]; !ok {
		fields["id"] = m.IDColumn
	}
	fields[m.IDColumn] = m.IDColumn
	fields[m.VersionColumn] = m.VersionColumn

	root := &table{name: m.Table, key: m.IDColumn, columns: m.Columns, fields: fields, children: map[string]*childTable{}}
	for _, c := range m.Children {
		if c.Dehydrate == nil {
			return nil, fmt.Errorf("sqlrepo: child %q needs Dehydrate", c.Name)
		}
		if c.Name == "" {
			return nil, fmt.Errorf("sqlrepo: child of %q without Name", m.Table)
		}
		if err := checkIdent("table", c.Table); err != nil {
			return nil, err
		}
		if err := checkIdent("foreign key", c.ForeignKey); err != nil {
			return nil, err
		}
		cf, err := buildFields(c.Columns, c.Fields)
		if err != nil {
			return nil, err
		}
		for _, o := range c.OrderBy {
			if err := checkIdent("column", o); err != nil {
				return nil, err
			}
		}
		root.children[c.Name] = &childTable{
			table:   table{name: c.Table, key: c.ForeignKey, columns: c.Columns, fields: cf},
			orderBy: c.OrderBy,
		}
	}
	return root, nil
}
