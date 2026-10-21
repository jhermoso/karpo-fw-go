package infrastructure

import "github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"

// decimalColumnsV3 son las columnas que la migración 3 pasa de texto a {dec:p,s} (D-94). El coste y
// la cantidad siguen a shipment y shipment_item de Trench y Karpo C#; el peso del envío no tiene
// equivalente y lleva los tres decimales que exige el dominio. Tabla completa en
// docs/ESQUEMA-MIGRACIONES.md.
//
// La lista no se cambia nunca: el código de un Run no entra en el checksum de la migración, así que
// editarla haría divergir las bases nuevas de las ya migradas. Un cambio de (p,s) va en una
// migración nueva, con su propia lista, y se refleja en decimalColumns.
var decimalColumnsV3 = sqlrepo.DecimalColumns{
	{Table: "shp_shipments", Column: "weight_kg", Type: sqlrepo.Dec(18, 3), NotNull: true},
	{Table: "shp_shipments", Column: "cost", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "shp_shipment_lines", Column: "quantity", Type: sqlrepo.Dec(18, 4), NotNull: true},
}

// decimalColumns son las columnas decimales vigentes del contexto, las que declaran los mapeos:
// hoy, las de la migración 3.
var decimalColumns = decimalColumnsV3
