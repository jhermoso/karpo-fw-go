package infrastructure

import "github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"

// decimalColumnsV3 son las columnas que la migración 3 pasa de texto a {dec:p,s} (D-94). Trench y
// Karpo C# no guardan el coste, el residual, la venta ni la dotación de un activo: son importes al
// céntimo, como los redondea el dominio. Tabla completa en docs/ESQUEMA-MIGRACIONES.md.
//
// La lista no se cambia nunca: el código de un Run no entra en el checksum de la migración, así que
// editarla haría divergir las bases nuevas de las ya migradas. Un cambio de (p,s) va en una
// migración nueva, con su propia lista, y se refleja en decimalColumns.
var decimalColumnsV3 = sqlrepo.DecimalColumns{
	{Table: "ast_assets", Column: "cost", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "ast_assets", Column: "residual", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "ast_assets", Column: "proceeds", Type: sqlrepo.Dec(18, 2)},
	{Table: "ast_asset_charges", Column: "amount", Type: sqlrepo.Dec(18, 2), NotNull: true},
}

// decimalColumns son las columnas decimales vigentes del contexto, las que declaran los mapeos:
// hoy, las de la migración 3.
var decimalColumns = decimalColumnsV3
