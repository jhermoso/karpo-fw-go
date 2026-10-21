package sqlrepo

import (
	"context"
	"fmt"
)

// DecimalChange es una columna de texto con decimales que una migración pasa a {dec:p,s}: su
// tabla, su nombre, su precisión y escala y si es NOT NULL (lo necesita AlterColumnToDecimal,
// porque SQL Server, MySQL y Oracle redefinen la columna).
type DecimalChange struct {
	Table, Column string
	Type          DecimalColumn
	NotNull       bool
}

// DecimalColumns es la lista de columnas decimales de un contexto. La misma lista da el Run de la
// migración que las pasa de texto a decimal y las Mapping.Decimals que las declaran.
//
// La lista de una migración no cambia nunca: el código de un Run no entra en su checksum, así que
// cambiar una (p,s) en ella haría que las bases nuevas y las ya migradas divergieran sin que el
// migrador lo notara. Un cambio de (p,s) va en una migración nueva, con su propia lista.
type DecimalColumns []DecimalChange

// Of devuelve las columnas de table como Mapping.Decimals (o Child.Decimals).
func (cs DecimalColumns) Of(table string) map[string]DecimalColumn {
	out := map[string]DecimalColumn{}
	for _, c := range cs {
		if c.Table == table {
			out[c.Column] = c.Type
		}
	}
	return out
}

// Run es el Run de una migración (Migration{..., Run: cols.Run}) que pasa las columnas, en orden,
// de texto a decimal con AlterColumnToDecimal. Antes de cambiar ninguna valida los datos de todas
// (ValidateDecimalColumn): en MySQL y en Oracle cada ALTER confirma la transacción, así que, si la
// validación de una columna posterior fallara después de cambiar las anteriores, la migración
// quedaría a medias. Si aun así falla a medias (un error del motor), se repite entera
// (Migrator.Force y Migrate): una columna que ya es decimal vuelve a validarse y a cambiarse sin
// perder nada, y en Oracle cada columna se retoma donde se quedó.
func (cs DecimalColumns) Run(ctx context.Context, db *DB) error {
	for _, c := range cs {
		if err := ValidateDecimalColumn(ctx, db, c.Table, c.Column, c.Type.Precision, c.Type.Scale); err != nil {
			return err
		}
	}
	for _, c := range cs {
		run, err := AlterColumnToDecimal(c.Table, c.Column, c.Type.Precision, c.Type.Scale, c.NotNull)
		if err != nil {
			return fmt.Errorf("sqlrepo: %s.%s: %w", c.Table, c.Column, err)
		}
		if err := run(ctx, db); err != nil {
			return err
		}
	}
	return nil
}
