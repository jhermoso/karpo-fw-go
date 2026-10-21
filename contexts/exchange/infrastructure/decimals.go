package infrastructure

import "github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"

// decimalColumnsV3 son las columnas que la migración 3 pasa de texto a {dec:p,s} (D-94). Siguen a
// currency, currency_exchange_margin y currency_reservation_item de Trench y Karpo C#; el tipo de
// la divisa (que allí no se guarda en currency) tiene los seis decimales del dominio, como
// QuotedRate, y lo entregado y el total de la reserva son importes al céntimo, como lo pedido.
// Tabla completa en docs/ESQUEMA-MIGRACIONES.md.
//
// La lista no se cambia nunca: el código de un Run no entra en el checksum de la migración, así que
// editarla haría divergir las bases nuevas de las ya migradas. Un cambio de (p,s) va en una
// migración nueva, con su propia lista, y se refleja en decimalColumns.
var decimalColumnsV3 = sqlrepo.DecimalColumns{
	{Table: "exg_currencies", Column: "rate", Type: sqlrepo.Dec(18, 6), NotNull: true},
	{Table: "exg_currencies", Column: "facial", Type: sqlrepo.Dec(18, 4), NotNull: true},
	{Table: "exg_margins", Column: "level1", Type: sqlrepo.Dec(18, 6), NotNull: true},
	{Table: "exg_margins", Column: "level2", Type: sqlrepo.Dec(18, 6), NotNull: true},
	{Table: "exg_margins", Column: "level3", Type: sqlrepo.Dec(18, 6), NotNull: true},
	{Table: "exg_reservations", Column: "total", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "exg_reservation_lines", Column: "requested", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "exg_reservation_lines", Column: "delivered", Type: sqlrepo.Dec(18, 2), NotNull: true},
	{Table: "exg_reservation_lines", Column: "base_rate", Type: sqlrepo.Dec(18, 6), NotNull: true},
	{Table: "exg_reservation_lines", Column: "offered_rate", Type: sqlrepo.Dec(18, 6), NotNull: true},
	{Table: "exg_reservation_lines", Column: "margin_value", Type: sqlrepo.Dec(18, 6), NotNull: true},
	{Table: "exg_reservation_lines", Column: "eur", Type: sqlrepo.Dec(18, 2), NotNull: true},
}

// decimalColumns son las columnas decimales vigentes del contexto, las que declaran los mapeos:
// hoy, las de la migración 3.
var decimalColumns = decimalColumnsV3
