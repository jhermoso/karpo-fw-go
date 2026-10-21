package infrastructure

import "github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"

// decimalColumnsV26 son las columnas que la migración 26 pasa de texto a {dec:p,s} (D-94). La
// participación sigue a party_relationship.DirectOwnershipPercent de Trench y Karpo C#. Tabla
// completa en docs/ESQUEMA-MIGRACIONES.md.
//
// La lista no se cambia nunca: el código de un Run no entra en el checksum de la migración, así que
// editarla haría divergir las bases nuevas de las ya migradas. Un cambio de (p,s) va en una
// migración nueva, con su propia lista, y se refleja en decimalColumns.
var decimalColumnsV26 = sqlrepo.DecimalColumns{
	{Table: "party_relationships", Column: "share_percent", Type: sqlrepo.Dec(5, 2)},
}

// decimalColumns son las columnas decimales vigentes del contexto, las que declaran los mapeos:
// hoy, las de la migración 26.
var decimalColumns = decimalColumnsV26
