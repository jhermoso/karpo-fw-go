package infrastructure

import "github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"

// decimalColumnsV3 son las columnas que la migración 3 pasa de texto a {dec:p,s} (D-94). El límite
// de crédito sigue a customer.CreditLimit de Trench y Karpo C#; los cobros, vencimientos y
// aplicaciones no tienen equivalente: son importes al céntimo, como los exige el dominio. Tabla
// completa en docs/ESQUEMA-MIGRACIONES.md.
//
// La lista no se cambia nunca: el código de un Run no entra en el checksum de la migración, así que
// editarla haría divergir las bases nuevas de las ya migradas. Un cambio de (p,s) va en una
// migración nueva, con su propia lista, y se refleja en decimalColumns.
var decimalColumnsV3 = sqlrepo.DecimalColumns{
	{Table: "rec_credit_profiles", Column: "credit_limit", Type: sqlrepo.Dec(18, 2)},
	{Table: "rec_receivables", Column: "total", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "rec_receivables", Column: "open_amount", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "rec_installments", Column: "amount", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "rec_installments", Column: "collected", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "rec_collections", Column: "amount", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "rec_allocations", Column: "amount", Type: sqlrepo.Dec(18, 2), NotNull: true},
}

// decimalColumns son las columnas decimales vigentes del contexto, las que declaran los mapeos:
// hoy, las de la migración 3.
var decimalColumns = decimalColumnsV3
