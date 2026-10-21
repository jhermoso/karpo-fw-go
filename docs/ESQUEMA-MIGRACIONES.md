# Evaluación de la gestión de esquema y migraciones (Fw C# → Go)

Evaluación de `IDatabaseSchemaManager`, de `IDatabaseOperationRunner`, de los proyectos
`*.Infrastructure.Migrations.*` y de `040-Data/schema`. Se usa el mismo método que en
[LENGUAJE-UBICUO.md](LENGUAJE-UBICUO.md) y en los demás informes de evaluación.

## Método

| Criterio | Peso |
|---|---|
| **U** Utilidad (uso real en contextos, sin Fw ni tests) | 40 % |
| **C** Corrección de la implementación | 25 % |
| **D** Diseño de la firma | 20 % |
| **G** Encaje en Go | 15 % |

Puntuación de 1 a 5 por criterio; nota ponderada sobre 100. **Mantener** ≥ 70 · **Modificar** 45–69 ·
**Retirar** < 45 · **Sustituido**.

## Cómo se gestiona hoy el esquema en C#

| Pieza | Uso medido |
|---|---|
| `IDatabaseSchemaManager` (`CanConnect`, `HasSchema`, `EnsureSchemaCreated`, `ExecuteRawSql`) en `Fw.Domain.Contracts` | 3 archivos fuera del Fw |
| `IDatabaseOperationRunner` (reset, drop, seed, status, snapshot y restore invocando PowerShell desde la aplicación) | 5 archivos (endpoints de administración) |
| Proyectos de migraciones EF | 20 (12 Npgsql por contexto; SQL Server y Oracle por «peldaño»: 217, 253 o 283 tablas) |
| Aplicación de migraciones desde el código (`MigrateAsync`, `GetPendingMigrations`) | **0**: solo `dotnet ef database update` a mano |
| `040-Data/schema` | fotografías de referencia de SQL Server y Oracle, y **semillas de catálogos sin otra fuente** (≈100.000 líneas) |

## Resultados

| # | Pieza C# | U | C | D | G | Nota | Decisión → Go |
|---|---|---|---|---|---|---|---|
| 1 | Migraciones EF derivadas del modelo, por motor | 5 | 3 | 3 | 2 | **70** | Mantener la idea, sustituir el mecanismo → `sqlrepo.Migration` / `MigrationSet` por contexto, con SQL explícito por dialecto |
| 2 | Semillas de catálogos (`02-semillas.sql`) | 5 | 3 | 2 | 3 | **71** | Mantener → migraciones de datos, versionadas junto al esquema del contexto que las posee |
| 3 | Trinquete `extraer-esquema-y-semillas.ps1 -Comprobar` | 4 | 4 | 3 | 3 | **73** | Mantener la idea → `Migrator.Verify` (checksum de lo aplicado frente al código) |
| 4 | `IDatabaseSchemaManager.CanConnectAsync` | 3 | 5 | 4 | 4 | **78** | Sustituido → `sqlrepo.DB.Ping` / health check de `distribution` |
| 5 | `IDatabaseSchemaManager.HasSchemaAsync` | 3 | 1 | 2 | 3 | **47** | Sustituido → `Migrator.Status` (estado por migración y contexto) |
| 6 | `IDatabaseSchemaManager.EnsureSchemaCreatedAsync` (`EnsureCreated`) | 3 | 1 | 2 | 2 | **44** | Retirar → `Migrator.Migrate` |
| 7 | `IDatabaseSchemaManager.ExecuteRawSqlAsync` | 2 | 2 | 1 | 2 | **38** | Retirar (el SQL crudo vive en las migraciones o en el adaptador) |
| 8 | Contrato del esquema en `Domain.Contracts` | — | — | 1 | — | — | Mover → `application.SchemaMigrator` (el esquema no es un concepto del dominio) |
| 9 | «Peldaños» de migraciones no incrementales (se aplica UNO) | 3 | 2 | 1 | 1 | **50** | Modificar → un `MigrationSet` por contexto; un despliegue combina los contextos que aloja |
| 10 | `IDatabaseOperationRunner` (PowerShell desde la aplicación) | 2 | 2 | 2 | 1 | **39** | Retirar del Fw: reset, snapshot y restore son operación de plataforma (scripts, jobs, `docker`), no un puerto de la aplicación |

## Defectos encontrados (justifican la columna C)

- **Ninguna aplicación comprueba al arrancar que su esquema está al día.** No hay `MigrateAsync`
  ni `GetPendingMigrations` en el código: un servicio arranca contra una base desfasada y falla
  más tarde, en la primera consulta. Es lo que pasó con el discriminador de `party` en SQL Server
  y Oracle (documentado en `040-Data/schema/README.md`). En Go, `Verify` se ejecuta al arrancar
  y antes de un cambio de base en caliente.
- **`HasSchemaAsync` usa `HasTablesAsync`**, que responde «sí» si existe **cualquier** tabla. En
  una base compartida por varios contextos, el esquema de otro contexto hace que
  `EnsureSchemaCreated` se salte el propio.
- **`EnsureCreated` y las migraciones son incompatibles** en EF: una base creada con
  `EnsureCreated` no tiene historial y no admite migraciones posteriores. El contrato ofrece el
  camino que cierra el otro.
- **`ExecuteRawSqlAsync` en `Domain.Contracts`** filtra SQL al dominio y deja abierta una
  superficie de inyección, porque recibe una cadena ya compuesta.
- **`IDatabaseOperationRunner` lanza PowerShell desde el proceso de la aplicación**, con
  contraseñas por defecto en los parámetros de los scripts (`aplicar-esquema.ps1`). Es una
  operación destructiva de plataforma expuesta como puerto de aplicación.
- **Los peldaños no son incrementales**: cada uno crea todo lo suyo y lo de abajo. Pasar de
  ErpKernel a ErpDetail en una base existente no es una migración, es otra base.

## Diseño en Go

```go
// Cada contexto declara sus migraciones (infraestructura del contexto).
func Migrations() sqlrepo.MigrationSet {
	return sqlrepo.MigrationSet{Context: "parties", Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "initial schema", Up: map[string][]string{"postgres": {...}, "oracle": {...}, ...}},
		{Version: 2, Name: "party roles", Up: sqlrepo.Portable(`CREATE TABLE party_roles (...)`)},
	}}
}

// Punto de composición: el despliegue combina los contextos que aloja.
m, _ := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{parties.Migrations(), orders.Migrations()})
m.Migrate(ctx) // en el job de despliegue
m.Verify(ctx)  // al arrancar y antes de hotswap.Switch.Swap: nunca sobre un esquema desfasado
```

| Pieza | Qué hace |
|---|---|
| `application.SchemaMigrator` | Contrato: `Status`, `Migrate` y `Verify`, con estados `pending`, `applied`, `dirty`, `modified` y `unknown` y errores `ErrSchemaOutdated` y `ErrSchemaDirty` |
| `sqlrepo.Migration`, `MigrationSet`, `Portable` | Migración versionada por contexto, con sentencias por dialecto o comunes |
| `sqlrepo.Migrator` | Historial `schema_migrations` (contexto, versión, nombre, checksum, dirty, fecha) y bloqueo `schema_migrations_lock` |
| `Migrator.Force` | Marca como aplicada una migración `dirty` después de repararla a mano |

**Garantías:**

- **Transaccional donde el motor lo permite.** En SQLite, PostgreSQL y SQL Server cada migración
  y su registro van en una transacción: si falla, no queda rastro. En Oracle y MySQL el DDL hace
  commit implícito, así que la migración se registra como `dirty` antes de ejecutarse y como
  limpia al terminar. Si falla a medias, `Migrate` y `Verify` se niegan hasta que alguien repare
  la base y llame a `Force`.
- **Inmutabilidad.** Si cambian las sentencias de una migración ya aplicada, pasa a `modified` y
  `Migrate` se niega.
- **Base por delante del código.** Una migración aplicada que el código no declara queda como
  `unknown`: `Verify` falla, y un binario antiguo no arranca contra una base más nueva.
- **Orden.** Una migración pendiente más antigua que la última aplicada se rechaza. La solución
  es darle una versión nueva, no reordenar lo ya aplicado.
- **Varias instancias.** Un bloqueo con arrendamiento (10 minutos por defecto) serializa los
  `Migrate` concurrentes. Si el titular muere, otra instancia lo retoma al caducar el plazo.
- **Contextos independientes.** Cada contexto lleva su propia secuencia de versiones sobre una
  base compartida.
- **Solo lectura.** `Status` y `Verify` no escriben: sin tabla de historial, todo está pendiente.

## Cambios en el renderizado del DDL portable

El checksum de una migración se calcula sobre sus sentencias **ya renderizadas** para el dialecto
(`RenderDDL`). Por eso, un cambio en cómo se renderiza un tipo lógico cambia el checksum de todas
las migraciones que lo usan, aunque nadie haya tocado su texto: en las bases donde ya estaban
aplicadas pasan a `modified`, y `Migrate` y `Verify` se niegan. Es el comportamiento deseado,
porque el esquema de esa base ya no es el que el código crearía.

**`{str:N}` en Oracle: de `VARCHAR2(N)` a `VARCHAR2(N CHAR)`.** `VARCHAR2(N)` cuenta bytes (con
el `NLS_LENGTH_SEMANTICS` por defecto) y los contextos validan las longitudes en caracteres, así
que un texto con acentos cerca de su límite fallaba con `ORA-12899` solo en Oracle. El cambio
alcanza también a las tablas de outbox, auditoría, inbox e historial de migraciones de Oracle,
que estaban escritas a mano con `VARCHAR2(N)`. Los demás motores no cambian.

- Una base Oracle **nueva** nace con semántica de caracteres. No hay nada que hacer.
- Una base Oracle **migrada antes del cambio** conserva sus columnas en bytes y sus migraciones
  quedan como `modified`. Se repara una vez:
  1. Pasar las columnas a caracteres. Es un cambio de metadatos que nunca recorta datos (la
     columna solo crece). Esta consulta genera las sentencias del esquema:

     ```sql
     SELECT 'ALTER TABLE ' || table_name || ' MODIFY (' || column_name || ' VARCHAR2(' || char_length || ' CHAR))'
       FROM user_tab_columns
      WHERE data_type = 'VARCHAR2' AND char_used = 'B'
      ORDER BY table_name, column_id;
     ```

  2. Llamar a `Migrator.Force(ctx, contexto, versión)` por cada migración `modified` de
     `Status`, para registrar el checksum nuevo.

  La alternativa, si la base no tiene datos que conservar, es recrearla.
- **Límites que siguen existiendo.** Un `VARCHAR2` de Oracle no guarda más de 4000 bytes
  (`MAX_STRING_SIZE=STANDARD`), se declare como se declare: `{str:N}` solo garantiza N caracteres
  cualesquiera hasta N = 1000 (4 bytes por carácter). Para textos más largos, `{text}`. En SQL
  Server, `NVARCHAR(N)` cuenta unidades UTF-16, así que los caracteres de fuera del plano básico
  (emoji) ocupan dos.
- Una columna declarada más ancha para esquivar el problema (por ejemplo `{str:4000}` para 1000
  caracteres) solo puede volver a su tamaño con una migración nueva, no editando la aplicada.

## Decimales: `{dec:p,s}` y el paso de texto a decimal (D-94)

**Marcador.** `{dec:p,s}` es `NUMERIC(p,s)` en PostgreSQL, `DECIMAL(p,s)` en SQL Server y MySQL,
`NUMBER(p,s)` en Oracle y `TEXT` en SQLite (su `NUMERIC` pierde cifras). Exige 1 ≤ p ≤ 28 y
0 ≤ s ≤ p; un marcador mal formado (`{dec}`, `{dec:18}`, `{dec:29,2}`, `{dec:5,6}`) hace que
`RenderDDL` y `RenderDDLAll` entren en pánico, como `regexp.MustCompile`, porque el DDL es una
constante del programa; `RenderDDLChecked` devuelve el error. Cómo se escriben, leen y consultan
los decimales: [ARQUITECTURA.md](ARQUITECTURA.md) §4, «Decimales».

**Pasar una columna de texto a decimal.** El DDL portable no cambiaba tipos de columna.
`sqlrepo.AlterColumnToDecimal(tabla, columna, p, s, notNull)` devuelve el `Run` de una migración
(`Migration{Version: n, Name: "...", Run: run}`) que pasa una columna de texto con decimales
escritos con punto a `{dec:p,s}` sin perder ni redondear ningún valor. Es Go, y no sentencias de
`Up`, porque tiene que validar los datos antes de tocar nada y, en Oracle, mirar el estado de la
columna.

**Validación previa, en los cinco motores.** Lee los valores no nulos y falla, sin cambiar
nada, si alguno no es un decimal con punto (`^-?([0-9]+(\.[0-9]+)?|\.[0-9]+)$`: ni texto, ni
vacío, ni espacios, ni `+`, ni exponentes, ni `NaN`, ni `1.`) o no cabe en (p,s): más decimales
que s o más cifras enteras que p-s. Así PostgreSQL no acepta `1e3` ni `NaN`, MySQL en modo no
estricto no convierte `abc` en 0 y ningún motor redondea. `.5`, `-.5`, `002.50` y `-0` son
válidos.

| Motor | Después de validar |
|---|---|
| PostgreSQL | `ALTER TABLE … ALTER COLUMN … TYPE NUMERIC(p,s) USING CAST(… AS NUMERIC(p,s))` |
| SQL Server | `ALTER TABLE … ALTER COLUMN … DECIMAL(p,s) [NOT] NULL` |
| MySQL | `ALTER TABLE … MODIFY COLUMN … DECIMAL(p,s) [NOT] NULL`. Redefine la columna: pierde su `DEFAULT`, su `COMMENT` y sus `CHECK` |
| Oracle | no cambia el tipo de una columna con datos (ORA-01439). Con una columna auxiliar `karpo_dec_tmp`: se añade, se copia en ella cada valor con `TO_NUMBER` y `NLS_NUMERIC_CHARACTERS` explícito, la columna se deja admitir nulos, se vacía, se cambia a `NUMBER(p,s)`, se copia de vuelta, se borra la auxiliar y, si `notNull`, vuelve a `NOT NULL`. Conserva el nombre y la posición de la columna; puede perder su `DEFAULT` y sus `CHECK` (un `CHECK` que no admita NULL impide vaciarla) |
| SQLite | `{dec}` ya es `TEXT`: reescribe cada valor en su forma canónica (`002.50` → `2.5`), para que la clave de orden que calcula SQLite coincida con la de Go |

**Si falla a medias.** En PostgreSQL, SQL Server y SQLite todo va en la transacción de la
migración y un fallo no deja nada hecho. En MySQL el cambio es una sola sentencia. En Oracle cada
DDL hace commit, así que un fallo puede dejar la tabla a medias, pero los valores están siempre en
la columna o en la auxiliar, y el `Run` se puede repetir: mira el tipo de la columna y si existe la
auxiliar, y retoma donde se quedó (sólo copia las filas cuyo texto sigue en la columna). Para
repetirlo, `Migrator.Force` sobre la migración sucia y `Migrate`. Para deshacerlo a mano antes de
vaciar la columna, basta con borrar la auxiliar.

`notNull` hay que darlo porque SQL Server, MySQL y Oracle redefinen la columna. La columna no debe
estar en índices, claves ni restricciones: se quitan antes y se vuelven a crear después. Tras la
migración, la columna se declara en `Mapping.Decimals` con su (p,s) y el mapeo escribe el
`vocab.Decimal`, no su texto.

**PostgreSQL: sentencias preparadas.** pgx prepara por defecto cada sentencia una vez por
conexión y la guarda con sus tipos (modo `cache_statement`). Una sentencia preparada antes de un
cambio de tipo y reutilizada después falla con «cached plan must not change result type»
(SQLSTATE 0A000) si cambia una columna leída, o con un error de tipos si cambia un parámetro.
Dentro de la transacción de una migración no se puede reintentar. Por eso el proceso que migra
(`karpo-postgres migrate`) abre su conexión con `postgres.WithoutStatementCache`, que añade
`default_query_exec_mode=describe_exec` a la cadena de conexión: cada sentencia se describe al
ejecutarla y no se guarda nada, a cambio de un viaje más por sentencia. Los procesos que sirven no
migran (verifican el esquema) y arrancan después, con un pool nuevo, así que conservan la caché.
Se descartaron reintentar ante 0A000 (no sirve dentro de una transacción, y la otra conexión del
pool puede tener la misma sentencia vieja) y vaciar la caché tras la migración (database/sql no da
acceso a la caché de pgx sin importar el driver en el dialecto).

## Columnas decimales de los contextos (D-94)

Las columnas decimales que los contextos guardaban como texto (`{str:N}`) pasan a `{dec:p,s}` con
una migración «decimal columns» por contexto, la siguiente a la última. El DDL de las migraciones
anteriores no cambia: una migración aplicada es inmutable.

**La lista.** Cada contexto la lleva en `contexts/<contexto>/infrastructure/decimals.go` como un
`sqlrepo.DecimalColumns`. `decimalColumnsVn` es la de la migración n, y su `Run` es el de la
migración. `decimalColumns` es la vigente, de la que salen las `Mapping.Decimals` con `.Of(tabla)`.
Hoy las dos son la misma lista. La de una migración no se edita nunca: el código de un `Run` no
entra en el checksum, así que cambiar una (p,s) en ella haría divergir, sin que el migrador lo
notara, las bases nuevas de las ya migradas. Un cambio de (p,s) va en una migración nueva, con su
propia lista, y se refleja en la vigente.

**El orden.** `DecimalColumns.Run` valida primero los datos de todas las columnas
(`sqlrepo.ValidateDecimalColumn`) y sólo después las cambia una a una con
`AlterColumnToDecimal`. En MySQL y en Oracle cada `ALTER` confirma la transacción: si la validación
de una columna posterior fallara después de cambiar las anteriores, la migración quedaría a medias.
Si aun así queda a medias (un error del motor), se repite entera con `Migrator.Force` y `Migrate`.

**Escritura.** Los mapeos escriben el `vocab.Decimal`. Donde antes se escribía `StringFixed(n)`,
ahora se escribe `Round(n)`, que redondea igual; los tipos de retención de Purchases, que el dominio
ya valida con dos decimales, se escriben tal cual. Ninguna columna está en un índice, una clave ni
una restricción. Las semillas de migraciones anteriores (HR, migración 4) siguen escribiendo texto,
porque corren antes del cambio de tipo.

**Antes de migrar una base existente.**

- Parar los servidores de la versión anterior. En PostgreSQL, una conexión que preparó una
  sentencia con la columna de texto falla después con «cached plan must not change result type»
  (SQLSTATE 0A000). Los servidores nuevos arrancan después de migrar, con conexiones nuevas.
- Comprobar si hay valores que no caben. La migración los rechaza sin tocar nada, pero conviene
  saberlo antes. Para cada columna de la tabla, con su (p,s), esta consulta da las filas que la
  migración rechazaría (en PostgreSQL; en los otros motores, la misma idea con su función de
  expresiones regulares: `REGEXP_LIKE` en Oracle y MySQL, y `LIKE` con patrones en SQL Server):

  ```sql
  -- ord_order_lines.unit_price, {dec:18,4}: un decimal con punto con como mucho 14 (p-s) cifras
  -- enteras significativas y 4 decimales significativos (los ceros finales no cuentan).
  SELECT order_id, line_no, unit_price FROM ord_order_lines
  WHERE unit_price IS NOT NULL
    AND unit_price !~ '^-?(0*[0-9]{1,14}([.][0-9]{1,4}0*)?|[.][0-9]{1,4}0*)$';
  ```

  Ninguna fila quiere decir que la columna pasa: es la misma regla que aplica la migración.

La precisión y la escala siguen a la columna equivalente de Trench (DDL de `050DBs/unified` y
configuraciones de EF) y de Karpo C# (`040-Data/schema` y EF), que coinciden en todo lo que tienen
en común. Donde no hay equivalente («dominio»), es la escala que impone el agregado con 18 cifras:
importes al céntimo (18,2), cantidades (18,4). Nunca es menor que la escala del dominio.

| Contexto | Columnas | (p,s) | De dónde |
|---|---|---|---|
| Accounting (v4) | `acc_entry_lines.debit`, `credit` | 18,4 | transaction_detail.Amount |
| Assets (v3) | `ast_assets.cost`, `residual`, `proceeds`; `ast_asset_charges.amount` | 18,2 | dominio (Trench y Karpo C# no guardan importes del activo) |
| Billing (v5) | `bil_invoices.net`, `tax_amount`, `surcharge` | 18,4 | invoicing_invoice.TotalAmount |
| | `bil_invoice_lines.quantity`, `unit_price`, `net` | 18,4 | invoicing_invoice_item (Quantity, UnitPrice, LineTotal) |
| | `bil_invoice_lines.discount` | 7,4 | order_item.LineDiscountPercent (la línea de factura no tiene descuento) |
| | `bil_invoice_taxes.rate`, `surcharge_rate` | 5,2 | vat_group.Rate, EquivalenceSurchargeRate |
| | `bil_invoice_taxes.base`, `amount`, `surcharge_amount` | 18,2 | dominio (Fiscal calcula al céntimo) |
| Documents (v3) | `doc_documents.total` | 18,2 | dominio |
| Exchange (v3) | `exg_currencies.rate` | 18,6 | dominio (seis decimales), como QuotedRate |
| | `exg_currencies.facial` | 18,4 | currency.Facial |
| | `exg_margins.level1`, `level2`, `level3`; `exg_reservation_lines.margin_value` | 18,6 | currency_exchange_margin.MarginValuePvp1..3 |
| | `exg_reservation_lines.base_rate`, `offered_rate` | 18,6 | currency_reservation_item.QuotedRate |
| | `exg_reservation_lines.requested`, `eur` | 18,2 | currency_reservation_item (RequestedAmount, EurCounterValue) |
| | `exg_reservation_lines.delivered`; `exg_reservations.total` | 18,2 | dominio, como lo pedido |
| Facilities (v5) | `fac_facilities.area_m2` | 18,2 | facility.SquareFootage |
| Fiscal (v3) | `fis_tax_rates.rate`, `surcharge` | 5,2 | vat_group |
| | `fis_taxpayers.general_prorata` | 5,2 | internal_organization_profile.GeneralProRata |
| | `fis_withholdings.perceptions`, `withheld`; `fis_filing_lines.perceptions`, `withheld` | 18,2 | mod190_perceptor_line, official_form_submission |
| HR (v5) | `hr_contracts.weekly_hours`; `hr_position_type_classes.standard_weekly_hours` | 5,2 | employee_profile.WeeklyHours, position_type_class.StandardHoursPerWeek |
| Inventory (v4) | `inv_levels.on_hand`, `reserved`, `reorder_point`, `average_cost` | 18,4 | stock_balance y product_commercial_profile (sólo Karpo C#) |
| | `inv_movements.quantity`, `unit_cost`, `balance` | 18,4 | inventory_item_variance.Quantity; coste y saldo como la cantidad |
| | `inv_movements.move_value` | 18,2 | dominio |
| | `inv_reservations.quantity`, `open_quantity` | 18,4 | stock_balance.QuantityReserved |
| Orders (v5) | `ord_terms.discount`; `ord_orders.customer_discount`; `ord_quotes.customer_discount` | 5,2 | perfiles comerciales del cliente (EarlyPaymentDiscountPercent, BaseDiscountPercent) |
| | `ord_order_lines`, `ord_quote_lines`: `quantity`, `unit_price`, `net_price`, `amount`; `ord_delivery_lines.quantity`, `net_price`, `amount` | 18,4 | order_item, quote_item, shipment_item |
| | `ord_order_lines.reserved`, `delivered` | 18,4 | dominio, como la cantidad |
| | `ord_order_lines.discount`, `ord_quote_lines.discount` | 7,4 | order_item.LineDiscountPercent |
| Parties (v26) | `party_relationships.share_percent` | 5,2 | party_relationship.DirectOwnershipPercent |
| Payments (v3) | `pay_payables.amount`, `paid`; `pay_payable_accounts.amount` | 18,2 | dominio |
| | `pay_payments.amount`; `pay_allocations.amount` | 19,4 | payment_header.Amount, payment_application.AmountApplied |
| Payroll (v4) | `pay_profiles.income_tax_rate`; `pay_payslips.income_tax_rate` | 5,2 | employee_profile.IrpfWithholdingPercent |
| | `pay_profiles.salary_amount`; `pay_profile_splits.split_amount` | 18,2 | employee_profile.BaseSalary, employee_payroll_account.SplitAmount |
| | `pay_profile_splits.split_percent` | 5,2 | employee_payroll_account.SplitPercentage |
| | `pay_payslips.gross`, `net` | 19,4 | cabecera de la nómina (GrossAmount, NetAmount) |
| | `pay_payslip_lines.quantity`, `unit_amount`, `base_amount` | 18,4 | payslip_line, DDL (EF dice 19,4 en los importes) |
| | `pay_payslip_lines.rate_percent` | 5,2 | payslip_line.WithholdingPercent |
| | `pay_payslip_lines.amount` | 18,2 | payslip_line, DDL (EF dice 19,4) |
| Products (v3) | `prd_products.base_price`, `standard_cost` | 18,4 | product_commercial_profile (Karpo C#; Trench: price_component y estimated_product_cost, 18,2) |
| | `prd_components.quantity` | 18,4 | product_component.QuantityUsed |
| | `prd_price_lines.unit_price`, `min_quantity` | 18,4 | price_list_line (sólo Karpo C#) |
| | `prd_price_lines.discount` | 5,2 | price_list_line.DiscountPercent (Trench: price_component.Percent, 9,4) |
| Purchases (v3) | `pur_invoices.withholding_rate`; `pur_suppliers.withholding_rate` | 5,2 | supplier_relationship_commercial_profile.WithholdingTaxPercent |
| | `pur_invoice_taxes.rate` | 5,2 | vat_group.Rate |
| | `pur_invoices.withholding`, `net`, `tax`, `declared_total`; `pur_invoice_lines.base`; `pur_invoice_taxes.base`, `amount`; `pur_invoice_accounts.amount` | 18,2 | dominio (no hay factura recibida en Trench ni en Karpo C#) |
| Receivables (v3) | `rec_credit_profiles.credit_limit` | 18,2 | customer.CreditLimit |
| | `rec_receivables.total`, `open_amount`; `rec_installments.amount`, `collected`; `rec_collections.amount`; `rec_allocations.amount` | 18,2 | dominio |
| Shipments (v3) | `shp_shipments.cost` | 18,4 | shipment.EstimatedShipCost, ActualShipCost |
| | `shp_shipment_lines.quantity` | 18,4 | shipment_item.Quantity |
| | `shp_shipments.weight_kg` | 18,3 | dominio (tres decimales) |
| Treasury (v5) | `trs_statements.opening`, `closing`; `trs_statement_lines.amount` | 19,4 | bank_statement_line (Balance, Amount) |
| | `trs_remittance_items.amount`; `trs_transfers.amount` | 18,2 | dominio |
| Work (v3) | `wrk_works.estimated_hours`, `budget`, `hours`, `cost`; `wrk_work_assignments.rate`; `wrk_time_entries.hours`, `rate` | 18,4 | work_effort, work_effort_assignment_rate, time_entry |

**La escala la valida el dominio.** Cada agregado rechaza, con un error de validación del campo
(400), un valor con más decimales que su columna, así que el usuario nunca llega al error de la
base. Donde el dominio no lo hacía, ahora lo hace:

- factura emitida: cantidad, precio y descuento, con 4 decimales;
- línea de nómina: cantidad, importe unitario y base con 4, porcentaje con 2;
- porcentaje de reparto del pago, horas semanales, superficie de la instalación y tipos de
  retención de Purchases: 2 decimales;
- línea de envío: cantidad con 4.

Si aun así un valor no cabe, el repositorio devuelve `sqlrepo.ErrDecimalDoesNotFit`. No es
`domain.ErrValidation`, porque ya sólo puede ser un fallo del programa: sale como error interno
(500, sin detalle), no como un 400 con el nombre de la tabla y la columna.

**Pruebas.**

- `integration/decimals_context_test.go`, en los cuatro motores servidores. Escribe con el esquema
  anterior, como texto y como lo escribía el código de entonces (`.String()` o `StringFixed(n)`):
  - un pedido de Orders;
  - una factura de Billing y su rectificativa, con cantidad e importe negativos;
  - un perfil de Payroll.

  Después migra y comprueba en el catálogo del motor que cada columna es numérica, con su (p,s), y
  que admite nulos igual que antes. Por último relee los agregados iguales, los vuelve a guardar y
  los relee otra vez.
- `contexts/orders/infrastructure/decimals_test.go`, en SQLite, dentro de `go test ./...`. Comprueba
  que la migración reescribe en forma canónica el texto de antes (`5.00` pasa a `5`, `12.5000` a
  `12.5`), y que el pedido se relee igual, se guarda y se relee otra vez.

## Validación

- `sqlconformance.RunStrings` en los cinco motores: una columna `{str:1000}` guarda y devuelve
  1000 caracteres de 2, 3 y 4 bytes (los de 4, salvo en SQL Server).
- `sqlconformance.RunDecimals` en los cinco motores: columnas `{dec:28,8}` y `{dec:28,28}`, y el
  paso de `{str:40}` a `{dec:18,4}` con `AlterColumnToDecimal`: con un valor no válido o que
  desborda falla y los datos siguen intactos; con valores válidos (`.5`, `-.5`, NULL, columna
  `NOT NULL` y otra que admite nulos) los conserva, se puede repetir y la columna se ordena por
  valor.

- `sqlconformance.RunMigrations` en SQLite, PostgreSQL, SQL Server, Oracle y MySQL:
  - base vacía pendiente y `Verify` fallando;
  - aplicación, idempotencia e incremento;
  - rechazo por orden;
  - `modified` y `unknown`;
  - migración fallida: rollback del DDL en los motores transaccionales; `dirty` + rechazo +
    `Force` en Oracle y MySQL;
  - bloqueo retenido que bloquea y bloqueo caducado que se retoma;
  - versiones no crecientes rechazadas.
- Parties declara su esquema como la migración 1 de su contexto. En los tests de integración, cada
  motor pasa por `Verify` fallando → `Migrate` → `Verify` correcto. El e2e de Parties exige
  `Verify` antes del cambio de base en caliente.

## Pendiente

- Migraciones escritas en Go (transformaciones de datos que no caben en SQL).
- Generar el `MigrationSet` inicial de cada contexto a partir de los scripts de `040-Data/schema`
  cuando se porte cada contexto (las semillas de catálogos, como migraciones de datos).
- Un comando `karpo migrate` (status, up, force) para el job de despliegue.
