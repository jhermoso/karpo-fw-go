package infrastructure

import "github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"

// decimalColumnsV5 son las columnas que la migración 5 pasa de texto a {dec:p,s} (D-94). La
// superficie es la de facility.SquareFootage en Trench y Karpo C#. Tabla completa en
// docs/ESQUEMA-MIGRACIONES.md.
//
// La lista no se cambia nunca: el código de un Run no entra en el checksum de la migración, así que
// editarla haría divergir las bases nuevas de las ya migradas. Un cambio de (p,s) va en una
// migración nueva, con su propia lista, y se refleja en decimalColumns.
var decimalColumnsV5 = sqlrepo.DecimalColumns{
	{Table: "fac_facilities", Column: "area_m2", Type: sqlrepo.Dec(18, 2)},
}

// decimalColumns son las columnas decimales vigentes del contexto, las que declaran los mapeos:
// hoy, las de la migración 5.
var decimalColumns = decimalColumnsV5
