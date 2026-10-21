package infrastructure

import "github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"

// decimalColumnsV5 son las columnas que la migración 5 pasa de texto a {dec:p,s} (D-94). La factura
// y sus líneas siguen a invoicing_invoice e invoicing_invoice_item de Trench y Karpo C#; el
// descuento de línea, al de order_item; los tipos, a vat_group. La base y las cuotas del desglose
// no tienen equivalente: son importes al céntimo, como los calcula Fiscal. Tabla completa en
// docs/ESQUEMA-MIGRACIONES.md.
//
// La lista no se cambia nunca: el código de un Run no entra en el checksum de la migración, así que
// editarla haría divergir las bases nuevas de las ya migradas. Un cambio de (p,s) va en una
// migración nueva, con su propia lista, y se refleja en decimalColumns.
var decimalColumnsV5 = sqlrepo.DecimalColumns{
	{Table: "bil_invoices", Column: "net", Type: sqlrepo.Dec(18, 4)},
	{Table: "bil_invoices", Column: "tax_amount", Type: sqlrepo.Dec(18, 4)},
	{Table: "bil_invoices", Column: "surcharge", Type: sqlrepo.Dec(18, 4)},
	{Table: "bil_invoice_lines", Column: "quantity", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "bil_invoice_lines", Column: "unit_price", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "bil_invoice_lines", Column: "discount", Type: sqlrepo.Dec(7, 4)},
	{Table: "bil_invoice_lines", Column: "net", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "bil_invoice_taxes", Column: "rate", Type: sqlrepo.Dec(5, 2), NotNull: true},
	{Table: "bil_invoice_taxes", Column: "base", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "bil_invoice_taxes", Column: "amount", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "bil_invoice_taxes", Column: "surcharge_rate", Type: sqlrepo.Dec(5, 2)},
	{Table: "bil_invoice_taxes", Column: "surcharge_amount", Type: sqlrepo.Dec(18, 2)},
}

// decimalColumns son las columnas decimales vigentes del contexto, las que declaran los mapeos:
// hoy, las de la migración 5.
var decimalColumns = decimalColumnsV5
