package infrastructure

import "github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"

// decimalColumnsV3 son las columnas que la migración 3 pasa de texto a {dec:p,s} (D-94). Siguen a
// work_effort, work_effort_assignment_rate y time_entry de Trench y Karpo C#, todas (18,4). Tabla
// completa en docs/ESQUEMA-MIGRACIONES.md.
//
// La lista no se cambia nunca: el código de un Run no entra en el checksum de la migración, así que
// editarla haría divergir las bases nuevas de las ya migradas. Un cambio de (p,s) va en una
// migración nueva, con su propia lista, y se refleja en decimalColumns.
var decimalColumnsV3 = sqlrepo.DecimalColumns{
	{Table: "wrk_works", Column: "estimated_hours", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "wrk_works", Column: "budget", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "wrk_works", Column: "hours", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "wrk_works", Column: "cost", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "wrk_work_assignments", Column: "rate", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "wrk_time_entries", Column: "hours", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "wrk_time_entries", Column: "rate", Type: sqlrepo.Dec(18, 4), NotNull: true},
}

// decimalColumns son las columnas decimales vigentes del contexto, las que declaran los mapeos:
// hoy, las de la migración 3.
var decimalColumns = decimalColumnsV3
