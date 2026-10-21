package infrastructure

import "github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"

// decimalColumnsV5 son las columnas que la migración 5 pasa de texto a {dec:p,s} (D-94). Los
// movimientos y saldos del extracto siguen a bank_statement_line (Amount y Balance) de Trench y
// Karpo C#; los adeudos y transferencias no tienen equivalente: son importes al céntimo, como los
// exige el dominio. Tabla completa en docs/ESQUEMA-MIGRACIONES.md.
//
// La lista no se cambia nunca: el código de un Run no entra en el checksum de la migración, así que
// editarla haría divergir las bases nuevas de las ya migradas. Un cambio de (p,s) va en una
// migración nueva, con su propia lista, y se refleja en decimalColumns.
var decimalColumnsV5 = sqlrepo.DecimalColumns{
	{Table: "trs_remittance_items", Column: "amount", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "trs_transfers", Column: "amount", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "trs_statements", Column: "opening", Type: sqlrepo.Dec(19, 4), NotNull: true},
	{Table: "trs_statements", Column: "closing", Type: sqlrepo.Dec(19, 4), NotNull: true},
	{Table: "trs_statement_lines", Column: "amount", Type: sqlrepo.Dec(19, 4), NotNull: true},
}

// decimalColumns son las columnas decimales vigentes del contexto, las que declaran los mapeos:
// hoy, las de la migración 5.
var decimalColumns = decimalColumnsV5
