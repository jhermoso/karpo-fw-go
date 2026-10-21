package infrastructure

import "github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"

// decimalColumnsV3 son las columnas que la migración 3 pasa de texto a {dec:p,s} (D-94). El
// producto y la tarifa siguen a product_commercial_profile y price_list_line de Karpo C#, que
// Trench no tiene (allí price_component.Price y estimated_product_cost.Cost son (18,2), menos que
// los cuatro decimales del dominio); el escandallo, a product_component.QuantityUsed. Tabla
// completa en docs/ESQUEMA-MIGRACIONES.md.
//
// La lista no se cambia nunca: el código de un Run no entra en el checksum de la migración, así que
// editarla haría divergir las bases nuevas de las ya migradas. Un cambio de (p,s) va en una
// migración nueva, con su propia lista, y se refleja en decimalColumns.
var decimalColumnsV3 = sqlrepo.DecimalColumns{
	{Table: "prd_products", Column: "base_price", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "prd_products", Column: "standard_cost", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "prd_components", Column: "quantity", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "prd_price_lines", Column: "min_quantity", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "prd_price_lines", Column: "unit_price", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "prd_price_lines", Column: "discount", Type: sqlrepo.Dec(5, 2), NotNull: true},
}

// decimalColumns son las columnas decimales vigentes del contexto, las que declaran los mapeos:
// hoy, las de la migración 3.
var decimalColumns = decimalColumnsV3
