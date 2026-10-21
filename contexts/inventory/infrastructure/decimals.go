package infrastructure

import "github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"

// decimalColumnsV4 son las columnas que la migración 4 pasa de texto a {dec:p,s} (D-94). Las
// cantidades y el coste siguen a stock_balance, product_commercial_profile (sólo en Karpo C#) e
// inventory_item_variance (en los dos); el valor del movimiento es un importe al céntimo, como lo
// redondea el dominio. Tabla completa en docs/ESQUEMA-MIGRACIONES.md.
//
// La lista no se cambia nunca: el código de un Run no entra en el checksum de la migración, así que
// editarla haría divergir las bases nuevas de las ya migradas. Un cambio de (p,s) va en una
// migración nueva, con su propia lista, y se refleja en decimalColumns.
var decimalColumnsV4 = sqlrepo.DecimalColumns{
	{Table: "inv_levels", Column: "on_hand", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "inv_levels", Column: "reserved", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "inv_levels", Column: "average_cost", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "inv_levels", Column: "reorder_point", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "inv_movements", Column: "quantity", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "inv_movements", Column: "unit_cost", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "inv_movements", Column: "move_value", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "inv_movements", Column: "balance", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "inv_reservations", Column: "quantity", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "inv_reservations", Column: "open_quantity", Type: sqlrepo.Dec(18, 4), NotNull: true},
}

// decimalColumns son las columnas decimales vigentes del contexto, las que declaran los mapeos:
// hoy, las de la migración 4.
var decimalColumns = decimalColumnsV4
