package infrastructure

import "github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"

// decimalColumnsV5 son las columnas que la migración 5 pasa de texto a {dec:p,s} (D-94). La
// precisión y la escala son las de Trench y Karpo C# (order_item, quote_item, perfiles comerciales
// del cliente); reserved y delivered, que no tienen equivalente, son cantidades como quantity.
// Tabla completa en docs/ESQUEMA-MIGRACIONES.md.
//
// La lista no se cambia nunca: el código de un Run no entra en el checksum de la migración, así que
// editarla haría divergir las bases nuevas de las ya migradas. Un cambio de (p,s) va en una
// migración nueva, con su propia lista, y se refleja en decimalColumns.
var decimalColumnsV5 = sqlrepo.DecimalColumns{
	{Table: "ord_terms", Column: "discount", Type: sqlrepo.Dec(5, 2), NotNull: true},
	{Table: "ord_orders", Column: "customer_discount", Type: sqlrepo.Dec(5, 2), NotNull: true},
	{Table: "ord_order_lines", Column: "quantity", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "ord_order_lines", Column: "unit_price", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "ord_order_lines", Column: "discount", Type: sqlrepo.Dec(7, 4), NotNull: true},
	{Table: "ord_order_lines", Column: "net_price", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "ord_order_lines", Column: "amount", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "ord_order_lines", Column: "reserved", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "ord_order_lines", Column: "delivered", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "ord_delivery_lines", Column: "quantity", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "ord_delivery_lines", Column: "net_price", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "ord_delivery_lines", Column: "amount", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "ord_quotes", Column: "customer_discount", Type: sqlrepo.Dec(5, 2), NotNull: true},
	{Table: "ord_quote_lines", Column: "quantity", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "ord_quote_lines", Column: "unit_price", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "ord_quote_lines", Column: "discount", Type: sqlrepo.Dec(7, 4), NotNull: true},
	{Table: "ord_quote_lines", Column: "net_price", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "ord_quote_lines", Column: "amount", Type: sqlrepo.Dec(18, 4), NotNull: true},
}

// decimalColumns son las columnas decimales vigentes del contexto, las que declaran los mapeos:
// hoy, las de la migración 5.
var decimalColumns = decimalColumnsV5
