package infrastructure

import "github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"

// decimalColumnsV4 son las columnas que la migración 4 pasa de texto a {dec:p,s} (D-94). Los
// importes del apunte son los de transaction_detail en Trench y Karpo C#. Tabla completa en
// docs/ESQUEMA-MIGRACIONES.md.
//
// La lista no se cambia nunca: el código de un Run no entra en el checksum de la migración, así que
// editarla haría divergir las bases nuevas de las ya migradas. Un cambio de (p,s) va en una
// migración nueva, con su propia lista, y se refleja en decimalColumns.
var decimalColumnsV4 = sqlrepo.DecimalColumns{
	{Table: "acc_entry_lines", Column: "debit", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "acc_entry_lines", Column: "credit", Type: sqlrepo.Dec(18, 4), NotNull: true},
}

// decimalColumns son las columnas decimales vigentes del contexto, las que declaran los mapeos:
// hoy, las de la migración 4.
var decimalColumns = decimalColumnsV4
