package infrastructure

import "github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"

// decimalColumnsV3 son las columnas que la migración 3 pasa de texto a {dec:p,s} (D-94). Trench y
// Karpo C# no tienen factura recibida: los importes son al céntimo, como los escribe el dominio;
// los tipos de retención siguen a supplier_relationship_commercial_profile.WithholdingTaxPercent y
// los de IVA, a vat_group. Tabla completa en docs/ESQUEMA-MIGRACIONES.md.
//
// La lista no se cambia nunca: el código de un Run no entra en el checksum de la migración, así que
// editarla haría divergir las bases nuevas de las ya migradas. Un cambio de (p,s) va en una
// migración nueva, con su propia lista, y se refleja en decimalColumns.
var decimalColumnsV3 = sqlrepo.DecimalColumns{
	{Table: "pur_invoices", Column: "withholding_rate", Type: sqlrepo.Dec(5, 2), NotNull: true},
	{Table: "pur_invoices", Column: "withholding", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "pur_invoices", Column: "net", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "pur_invoices", Column: "tax", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "pur_invoices", Column: "declared_total", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "pur_invoice_lines", Column: "base", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "pur_invoice_taxes", Column: "rate", Type: sqlrepo.Dec(5, 2), NotNull: true},
	{Table: "pur_invoice_taxes", Column: "base", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "pur_invoice_taxes", Column: "amount", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "pur_invoice_accounts", Column: "amount", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "pur_suppliers", Column: "withholding_rate", Type: sqlrepo.Dec(5, 2), NotNull: true},
}

// decimalColumns son las columnas decimales vigentes del contexto, las que declaran los mapeos:
// hoy, las de la migración 3.
var decimalColumns = decimalColumnsV3
