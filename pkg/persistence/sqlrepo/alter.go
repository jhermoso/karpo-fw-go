package sqlrepo

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

// AlterColumnToDecimal devuelve el Run de una migración (Migration.Run) que pasa una columna de
// texto con decimales escritos con punto ("12.50", "-3", ".5") a {dec:precision,scale}, sin
// perder ni redondear ningún valor. notNull dice si la columna es NOT NULL, y hay que darlo
// porque SQL Server, MySQL y Oracle redefinen la columna.
//
// Antes de cambiar nada lee la columna y falla, sin tocar los datos, si algún valor no nulo no es
// un decimal con punto (texto, vacío, espacios, «+», exponentes, NaN) o no cabe en (p,s) (más
// decimales que la escala o más cifras enteras que p-s). Así PostgreSQL no acepta «1e3» ni «NaN»,
// MySQL en modo no estricto no convierte «abc» en 0 y ningún motor redondea.
//
// Después, por motor: ALTER COLUMN ... TYPE ... USING en PostgreSQL, ALTER COLUMN en SQL Server,
// MODIFY COLUMN en MySQL (que pierde el DEFAULT, el COMMENT y los CHECK de la columna) y, en
// Oracle, que no cambia el tipo de una columna con datos (ORA-01439), una columna auxiliar
// (oracleDecimalSteps). En SQLite {dec} ya es TEXT: sólo reescribe los valores en su forma
// canónica ("002.50" pasa a "2.5"), para que la clave de orden que calcula SQLite coincida con
// la que calcula Go. La columna no debe formar parte de índices, claves ni restricciones: se
// quitan antes y se vuelven a crear después.
//
// En PostgreSQL, SQL Server y SQLite el Run va en la transacción de la migración y un fallo no
// deja nada a medias. En MySQL el cambio es una sola sentencia. En Oracle, si falla a medias, se
// vuelve a ejecutar (Migrator.Force y Migrate): cada paso mira el estado de la columna y retoma
// donde se quedó.
//
// En PostgreSQL con pgx, el proceso que migra abre su conexión con
// postgres.WithoutStatementCache: una sentencia preparada antes del cambio de tipo y reutilizada
// después fallaría con «cached plan must not change result type» (SQLSTATE 0A000).
func AlterColumnToDecimal(table, column string, precision, scale int, notNull bool) (func(ctx context.Context, db *DB) error, error) {
	for _, id := range []struct{ kind, v string }{{"table", table}, {"column", column}} {
		if err := checkIdent(id.kind, id.v); err != nil {
			return nil, err
		}
	}
	dc := Dec(precision, scale)
	if err := dc.validate(); err != nil {
		return nil, fmt.Errorf("sqlrepo: %s.%s: %w", table, column, err)
	}
	return func(ctx context.Context, db *DB) error {
		if db.d.Name() == "oracle" {
			return oracleDecimalSteps(ctx, db, table, column, dc, notNull)
		}
		values, err := decimalTexts(ctx, db, table, column, dc)
		if err != nil {
			return err
		}
		if db.d.Name() == "sqlite" {
			return canonicalizeTexts(ctx, db, table, column, values)
		}
		for _, s := range alterDecimalSQL(db.d, table, column, dc, notNull) {
			if _, err := db.ExecContext(ctx, s); err != nil {
				return fmt.Errorf("sqlrepo: %s.%s to %s: %w\n%s", table, column, dc, err, s)
			}
		}
		return nil
	}, nil
}

// ValidateDecimalColumn comprueba, sin cambiar nada, que AlterColumnToDecimal puede pasar la columna
// a {dec:precision,scale}: que cada valor no nulo es un decimal con punto que cabe sin redondear.
// Devuelve el mismo error que daría AlterColumnToDecimal (un valor que no cabe es
// ErrDecimalDoesNotFit; uno que no es un decimal, domain.ErrValidation). Una columna que ya es
// decimal también se valida (sus valores caben), salvo en Oracle, donde una columna NUMBER ya está
// cambiada o a medio cambiar y AlterColumnToDecimal retoma sin leer su texto.
func ValidateDecimalColumn(ctx context.Context, db *DB, table, column string, precision, scale int) error {
	for _, id := range []struct{ kind, v string }{{"table", table}, {"column", column}} {
		if err := checkIdent(id.kind, id.v); err != nil {
			return err
		}
	}
	dc := Dec(precision, scale)
	if err := dc.validate(); err != nil {
		return fmt.Errorf("sqlrepo: %s.%s: %w", table, column, err)
	}
	if db.d.Name() == "oracle" {
		typ, _, err := oracleColumn(ctx, db, table, column)
		if err != nil {
			return err
		}
		if typ == "" {
			return fmt.Errorf("sqlrepo: %s.%s does not exist", table, column)
		}
		if typ == "NUMBER" {
			return nil
		}
	}
	_, err := decimalTexts(ctx, db, table, column, dc)
	return err
}

// alterDecimalSQL es el cambio de tipo en PostgreSQL, SQL Server y MySQL.
func alterDecimalSQL(d Dialect, table, column string, dc DecimalColumn, notNull bool) []string {
	t, c := d.Quote(table), d.Quote(column)
	ps := strconv.Itoa(dc.Precision) + "," + strconv.Itoa(dc.Scale)
	null := " NULL"
	if notNull {
		null = " NOT NULL"
	}
	switch d.Name() {
	case "postgres":
		return []string{fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE NUMERIC(%s) USING CAST(%s AS NUMERIC(%s))", t, c, ps, c, ps)}
	case "sqlserver":
		return []string{fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DECIMAL(%s)%s", t, c, ps, null)}
	case "mysql":
		return []string{fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s DECIMAL(%s)%s", t, c, ps, null)}
	}
	return nil
}

// decimalTexts lee los valores no nulos de la columna, comprueba que todos son decimales con
// punto que caben en dc y devuelve cada texto distinto con su forma canónica.
func decimalTexts(ctx context.Context, db *DB, table, column string, dc DecimalColumn) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %s WHERE %s IS NOT NULL",
		db.d.Quote(column), db.d.Quote(table), db.d.Quote(column)))
	if err != nil {
		return nil, fmt.Errorf("sqlrepo: read %s.%s: %w", table, column, err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var raw any
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var s string
		switch x := raw.(type) {
		case string, []byte:
			s = stringOf(x)
		case int64:
			s = strconv.FormatInt(x, 10)
		default:
			return nil, fmt.Errorf("sqlrepo: %s.%s holds a %T, not text", table, column, raw)
		}
		if _, seen := out[s]; seen {
			continue
		}
		canon, err := dc.canonical(s)
		if err != nil {
			return nil, fmt.Errorf("sqlrepo: %s.%s cannot become %s, nothing changed: %w", table, column, dc, err)
		}
		out[s] = canon
	}
	return out, rows.Err()
}

// canonicalizeTexts reescribe en su forma canónica los textos que no lo están (SQLite).
func canonicalizeTexts(ctx context.Context, db *DB, table, column string, values map[string]string) error {
	for orig, canon := range values {
		if orig == canon {
			continue
		}
		if _, err := db.Update(ctx, table, Values{column: canon}, Values{column: orig}); err != nil {
			return fmt.Errorf("sqlrepo: canonicalize %s.%s: %w", table, column, err)
		}
	}
	return nil
}

// oracleDecimalSteps pasa la columna a NUMBER(p,s) conservando su nombre y su posición, con una
// columna auxiliar karpo_dec_tmp:
//
//  1. si la columna aún es texto: se validan los valores, se añade la auxiliar (si no está), se
//     copia cada valor en ella con TO_NUMBER y NLS_NUMERIC_CHARACTERS explícito, la columna se
//     deja admitir nulos, se vacía y se cambia a NUMBER(p,s) (Oracle sólo cambia el tipo de una
//     columna vacía);
//  2. si la auxiliar existe: se copia de vuelta a la columna y se borra;
//  3. si notNull: la columna vuelve a ser NOT NULL.
//
// Cada sentencia DDL hace commit en Oracle, así que un fallo puede dejar la tabla a medias; los
// valores siempre están en la columna o en la auxiliar, y volver a ejecutar el Run retoma donde
// se quedó: el paso 1 sólo copia las filas cuyo texto sigue en la columna y el 2 sólo corre si la
// auxiliar existe. Para deshacerlo a mano a mitad del paso 1, basta con borrar la auxiliar
// mientras la columna conserve su texto; después de vaciarla, los valores están en la auxiliar.
// Cambiar el tipo puede perder el DEFAULT y los CHECK de la columna: se vuelven a crear después.
func oracleDecimalSteps(ctx context.Context, db *DB, table, column string, dc DecimalColumn, notNull bool) error {
	const tmp = "karpo_dec_tmp"
	t, c, x := db.d.Quote(table), db.d.Quote(column), db.d.Quote(tmp)
	exec := func(q string, args ...any) error {
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			return fmt.Errorf("sqlrepo: %s.%s to %s: %w\n%s", table, column, dc, err, q)
		}
		return nil
	}
	ps := strconv.Itoa(dc.Precision) + "," + strconv.Itoa(dc.Scale)

	typ, nullable, err := oracleColumn(ctx, db, table, column)
	if err != nil {
		return err
	}
	tmpType, _, err := oracleColumn(ctx, db, table, tmp)
	if err != nil {
		return err
	}
	if typ == "" {
		return fmt.Errorf("sqlrepo: %s.%s does not exist", table, column)
	}
	if typ != "NUMBER" {
		values, err := decimalTexts(ctx, db, table, column, dc)
		if err != nil {
			return err
		}
		if tmpType == "" {
			if err := exec(fmt.Sprintf("ALTER TABLE %s ADD (%s NUMBER(%s))", t, x, ps)); err != nil {
				return err
			}
			tmpType = "NUMBER"
		}
		for orig, canon := range values {
			b := &Builder{d: db.d}
			num, err := b.Arg(decimal.RequireFromString(canon)) // TO_NUMBER con NLS explícito
			if err != nil {
				return err
			}
			where := db.d.Placeholder(len(b.args) + 1)
			if err := exec(fmt.Sprintf("UPDATE %s SET %s = %s WHERE %s = %s", t, x, num, c, where), append(b.args, orig)...); err != nil {
				return err
			}
		}
		if nullable == "N" {
			if err := exec(fmt.Sprintf("ALTER TABLE %s MODIFY (%s NULL)", t, c)); err != nil {
				return err
			}
		}
		if err := exec(fmt.Sprintf("UPDATE %s SET %s = NULL", t, c)); err != nil {
			return err
		}
		if err := exec(fmt.Sprintf("ALTER TABLE %s MODIFY (%s NUMBER(%s))", t, c, ps)); err != nil {
			return err
		}
	}
	if tmpType != "" {
		if err := exec(fmt.Sprintf("UPDATE %s SET %s = %s", t, c, x)); err != nil {
			return err
		}
		if err := exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", t, x)); err != nil {
			return err
		}
	}
	if notNull {
		if _, nullable, err = oracleColumn(ctx, db, table, column); err != nil {
			return err
		}
		if nullable == "Y" {
			return exec(fmt.Sprintf("ALTER TABLE %s MODIFY (%s NOT NULL)", t, c))
		}
	}
	return nil
}

// oracleColumn devuelve el tipo y si admite nulos ("Y"/"N") de una columna ("" si no existe).
func oracleColumn(ctx context.Context, db *DB, table, column string) (typ, nullable string, err error) {
	rows, err := db.QueryContext(ctx, "SELECT data_type, nullable FROM user_tab_columns WHERE table_name = :1 AND column_name = :2",
		strings.ToUpper(table), strings.ToUpper(column))
	if err != nil {
		return "", "", err
	}
	defer rows.Close()
	if rows.Next() {
		var t, n any
		if err := rows.Scan(&t, &n); err != nil {
			return "", "", err
		}
		typ, nullable = stringOf(t), stringOf(n)
	}
	return typ, nullable, rows.Err()
}
