package sqlrepo

import (
	"fmt"
	"regexp"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Values guarda los valores de las columnas de una fila, por nombre de columna.
type Values map[string]any

// CustomSQL traduce una especificación propia (spec.Custom) a una expresión booleana SQL.
// Con b.Column se referencian los campos mapeados de la tabla en curso, con b.Arg se enlazan
// parámetros y con b.Dialect se distingue la sintaxis propia de cada motor.
type CustomSQL func(b *Builder, args []any) (string, error)

// Mapping mapea una raíz de agregado a tablas. Se define una vez por agregado en la capa de
// infraestructura del contexto delimitado y funciona con todos los Dialect.
type Mapping[ID domain.Identifier, T domain.AggregateRoot[ID]] struct {
	// Table es la tabla raíz.
	Table string
	// IDColumn es la columna de clave primaria (por defecto "id"). Se expone a las
	// especificaciones como el campo lógico "id".
	IDColumn string
	// VersionColumn guarda la versión de concurrencia optimista (por defecto "version").
	VersionColumn string
	// Columns son las columnas de datos de la tabla raíz (sin id ni version).
	Columns []string
	// Fields mapea nombres de campo lógicos de las especificaciones a columnas cuando difieren.
	// Las columnas siempre se pueden referenciar por su propio nombre.
	Fields map[string]string
	// Decimals declara las columnas de Columns que son {dec:p,s}, con su precisión y su escala.
	// Sólo aceptan vocab.Decimal, *vocab.Decimal o nil, y un valor que no cabe en (p,s) es un
	// error, no se redondea; se comparan (con valores vocab.Decimal) y se ordenan por valor
	// numérico en todos los motores, también en SQLite, que las guarda como texto. Un
	// vocab.Decimal en una columna que no está aquí es domain.ErrUnsupported, al escribir y al
	// comparar.
	Decimals map[string]DecimalColumn
	// Dehydrate devuelve el valor de cada columna de Columns.
	Dehydrate func(agg T) (Values, error)
	// Hydrate reconstruye el agregado entero a partir de su fila raíz (id, version y Columns) y
	// las filas ya cargadas de sus colecciones hijas, normalmente mediante un constructor
	// Reconstitute del dominio. No debe lanzar eventos de dominio.
	Hydrate func(row *Row, children ChildRows) (T, error)
	// Children mapea las colecciones de entidades hijas guardadas en sus propias tablas.
	Children []Child[T]
	// Custom traduce las especificaciones propias (spec.Custom) por nombre.
	Custom map[string]CustomSQL
}

// ChildRows guarda las filas cargadas de cada colección hija de un agregado, por Name del hijo.
type ChildRows map[string][]*Row

// Of devuelve las filas de la colección hija indicada (en el orden de Child.OrderBy).
func (c ChildRows) Of(name string) []*Row { return c[name] }

// Child mapea a una tabla una colección de entidades hijas u objetos de valor del agregado.
// Los hijos se guardan reemplazándolos (borrar + insertar) dentro de la transacción del
// agregado, y se cargan por lotes antes de la hidratación (sin N+1).
type Child[T any] struct {
	// Name es el nombre lógico de la colección que usa spec.Collection(...).Any.
	Name string
	// Table es la tabla hija.
	Table string
	// ForeignKey es la columna que referencia la identidad de la raíz.
	ForeignKey string
	// Columns son las columnas de datos (sin ForeignKey).
	Columns []string
	// Fields mapea nombres de campo lógicos a columnas cuando difieren.
	Fields map[string]string
	// Decimals declara las columnas {dec:p,s} de Columns (como Mapping.Decimals).
	Decimals map[string]DecimalColumn
	// OrderBy enumera las columnas que fijan el orden en que se cargan los hijos.
	OrderBy []string
	// Dehydrate devuelve un Values por cada elemento hijo.
	Dehydrate func(root T) ([]Values, error)
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func checkIdent(kind, s string) error {
	if !identRe.MatchString(s) {
		return fmt.Errorf("sqlrepo: invalid %s identifier %q", kind, s)
	}
	return nil
}

// table es la forma normalizada y validada de una tabla mapeada que usa el traductor.
type table struct {
	name     string
	key      string // columna id (raíz) o clave ajena (hijo)
	columns  []string
	fields   map[string]string
	decimals map[string]DecimalColumn // columnas {dec:p,s}
	children map[string]*childTable
}

// DecimalColumn es la precisión (cifras) y la escala (decimales) de una columna {dec:p,s}.
type DecimalColumn struct {
	Precision, Scale int
}

// Dec devuelve la DecimalColumn de un {dec:precision,scale}.
func Dec(precision, scale int) DecimalColumn {
	return DecimalColumn{Precision: precision, Scale: scale}
}

func (c DecimalColumn) String() string { return fmt.Sprintf("{dec:%d,%d}", c.Precision, c.Scale) }

func (c DecimalColumn) validate() error {
	_, _, err := parseDecimalMarker(fmt.Sprintf("%d,%d", c.Precision, c.Scale))
	return err
}

// decimalSet valida las columnas decimales de un mapeo: deben estar entre sus columnas y tener una
// precisión y una escala válidas.
func decimalSet(tableName string, columns []string, decimals map[string]DecimalColumn) (map[string]DecimalColumn, error) {
	set := make(map[string]DecimalColumn, len(decimals))
	for c, dc := range decimals {
		if !contains(columns, c) {
			return nil, fmt.Errorf("sqlrepo: decimal column %q is not a column of %q", c, tableName)
		}
		if err := dc.validate(); err != nil {
			return nil, fmt.Errorf("sqlrepo: decimal column %s.%s: %w", tableName, c, err)
		}
		set[c] = dc
	}
	return set, nil
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

	decimals, err := decimalSet(m.Table, m.Columns, m.Decimals)
	if err != nil {
		return nil, err
	}
	root := &table{name: m.Table, key: m.IDColumn, columns: m.Columns, fields: fields, decimals: decimals, children: map[string]*childTable{}}
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
		cd, err := decimalSet(c.Table, c.Columns, c.Decimals)
		if err != nil {
			return nil, err
		}
		root.children[c.Name] = &childTable{
			table:   table{name: c.Table, key: c.ForeignKey, columns: c.Columns, fields: cf, decimals: cd},
			orderBy: c.OrderBy,
		}
	}
	return root, nil
}
