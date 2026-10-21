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
