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

## Validación

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
