package infrastructure

import "github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"

// decimalColumnsV3 son las columnas que la migración 3 pasa de texto a {dec:p,s} (D-94). Los tipos
// y la prorrata siguen a vat_group e internal_organization_profile de Trench y Karpo C#; los
// importes de retenciones y modelos, a mod190_perceptor_line y official_form_submission. Tabla
// completa en docs/ESQUEMA-MIGRACIONES.md.
//
// La lista no se cambia nunca: el código de un Run no entra en el checksum de la migración, así que
// editarla haría divergir las bases nuevas de las ya migradas. Un cambio de (p,s) va en una
// migración nueva, con su propia lista, y se refleja en decimalColumns.
var decimalColumnsV3 = sqlrepo.DecimalColumns{
	{Table: "fis_tax_rates", Column: "rate", Type: sqlrepo.Dec(5, 2), NotNull: true},
	{Table: "fis_tax_rates", Column: "surcharge", Type: sqlrepo.Dec(5, 2)},
	{Table: "fis_taxpayers", Column: "general_prorata", Type: sqlrepo.Dec(5, 2)},
	{Table: "fis_filing_lines", Column: "perceptions", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "fis_filing_lines", Column: "withheld", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "fis_withholdings", Column: "perceptions", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "fis_withholdings", Column: "withheld", Type: sqlrepo.Dec(18, 2), NotNull: true},
}

// decimalColumns son las columnas decimales vigentes del contexto, las que declaran los mapeos:
// hoy, las de la migración 3.
var decimalColumns = decimalColumnsV3
