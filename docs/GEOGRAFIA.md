# Contexto Geografía y referencia

Decisión aprobada por Javier el 2026-09-27 (ver [PARTIES.md](PARTIES.md), sección 2): la geografía y
los datos de referencia salen de Parties a un contexto propio. Es un catálogo de lectura que otros
contextos referencian por identidad (los mismos GUID que Karpo) o por código ISO.

## Contenido y origen

Semilla de Karpo (`040-Data/schema/sqlserver/02-semillas.sql`, esquema `parties`) extraída a CSV
comprimidos embebidos en el binario (`contexts/geography/infrastructure/seed`, 639 KB).

| Datos | Filas | Nota |
|---|---|---|
| Tipos de delimitación | 27 | Continente, país, comunidad autónoma, provincia, municipio… y agrupaciones supranacionales |
| Delimitaciones | 8.497 | 8.132 municipios, 248 países, 52 provincias, 26 regiones, 22 subcontinentes, 6 continentes y agrupaciones |
| Vínculos | 8.870 | 8.480 de contención (un padre como máximo) y 390 de pertenencia (país → UE) |
| Códigos postales | 14.608 | 11.051 códigos distintos; un código puede cubrir hasta 10 municipios |
| Perfiles de país | 248 | ISO 3166 (alfa-2, alfa-3 y numérico), prefijo telefónico, IBAN, formato postal, UE, EEE, eurozona, SEPA |
| Monedas / idiomas / zonas horarias / tipos de vía | 182 / 183 / 5 / 40 | Con sus vínculos por país (71 / 4 / 2) |

## Evaluación del C#

| # | Pieza C# | Decisión → Go |
|---|---|---|
| 1 | `GeographicBoundary` (TPH: Country, Municipality…) + `GeographicBoundaryAssociation` | Se mantiene. `Boundary` (tipo como dato, sin subclases) con sus vínculos como hijos. Invariante: **un padre de contención como máximo**, que la semilla cumple |
| 2 | Discriminador TPH (`Country`, `Municipality`, `GeographicBoundary`) | Se retira: duplica el tipo (`BoundaryTypeId`), y 117 filas tenían el genérico |
| 3 | `AllowsAllLegalRepresentatives` en la delimitación | Se retira: es política de representantes legales, no geografía (todas las filas son NULL) |
| 4 | Un tipo de delimitación por organización supranacional (OTAN, ONU, G7, G20, OPEP…: 9 tipos de una sola fila) | Se conserva como dato. **Defecto:** debería ser un único tipo «organización supranacional» con varias instancias |
| 5 | `PostalCode` (código ↔ municipio, muchos a muchos) | Se mantiene. `PostalCode` es una entrada por municipio; el formato por país lo valida el perfil del país |
| 6 | `CountryProfile` + `CountryCurrency` / `Language` / `TimeZone` | Se mantienen, como un agregado `Country` con sus tres colecciones. Formatos de IBAN y código postal compilados y validados al cargar |
| 7 | `Currency.Facial`, `CurrencyDenomination` | Se retiran de aquí: pertenecen al cambio de divisa (contexto Financial) |
| 8 | `CountryDocumentRule` | Se queda en Parties (política de documentos de identidad), con el país por código ISO |
| 9 | `PostalAddressBoundary` | Se queda en Parties: la dirección guarda los Ids de Geografía (`GeoRef`) |
| 10 | `GetAddressCountryIdByPartyIdsAsync` (un repositorio de geografía que leía contactos de Party) | Se retira: invertía la dependencia. `CountryOf` resuelve el país de cualquier delimitación |
| 11 | Validación «el código postal pertenece al municipio» en `ContactMechanismApplicationService` | Pasa a ser `AddressChecker` (Open Host Service). Parties la consume a través de **su propio puerto** y un adaptador (ACL) |
| 12 | `CountyCity` (sin configuración EF ni `DbSet`) | Se retira (código muerto) |

## Diseño

```
contexts/geography/
├── domain/          # Boundary (+ links), PostalCode, Country (+ monedas/idiomas/zonas), catálogos
├── contracts/       # Gazetteer, AddressChecker, Reference (Open Host Service)
├── application/     # consultas con permiso (Geography.Boundary.Read, Geography.Reference.Read) + Ports
├── infrastructure/  # semilla embebida, esquema geo_* de 5 motores, mapeos de solo lectura, fábricas
└── distribution/    # /api/geography/... y /api/reference/...
```

- **Solo lectura**, salvo el calendario de festivos (ver «Calendario de festivos»). Los datos cambian mediante migraciones. Guardar un agregado devuelve
  `ErrUnsupported`.
- **Puertos para otros contextos:**
  - `Resolve`, `Ancestors`, `Descendants` y `CountryOf` recorren la jerarquía con una consulta por
    nivel. Las listas IN se trocean de 500 en 500, porque toda España son más de 8.000
    identificadores y SQL Server admite 2.100 parámetros por sentencia.
  - `CheckPostalAddress` y `Country` completan los puertos de consulta.
  - `CheckIBAN` combina `vocab.IBAN` (ISO 13616, mod 97) con la longitud y el formato del país.
- **Rutas:**
  - `GET /api/geography/boundaries?q&type&parent&grouping` y `GET /api/geography/boundaries/{id}`
    (con ancestros y agrupaciones);
  - `GET /api/geography/postal-codes?country&code` y `POST /api/geography/address-check`;
  - `GET /api/reference/countries[/{alpha2}]` y
    `GET /api/reference/{currencies|languages|time-zones|street-types|boundary-types}`.
- **Integración con Parties:**
  - Parties define `application.AddressChecker`, y `infrastructure.GeographyAddresses` lo
    implementa sobre `contracts.AddressChecker`;
  - `parties.Compose(..., parties.WithAddressChecker(...))` activa la comprobación;
  - una dirección con el código postal de otro municipio se rechaza, y la válida guarda los Ids
    del código y del municipio.
- **Regla de arquitectura nueva:** un contexto solo importa de otro su paquete `contracts`, y nunca
  desde su dominio (`archtest`, regla 7).

## Calendario de festivos

Añadido el 2026-10-09. C# no tenía calendario: las condiciones de pago hablaban de festivos y
nadie sabía cuáles eran.

- **Un festivo es un día en una delimitación**: un país, una comunidad, una provincia, un
  municipio. Vale para la delimitación y para todo lo que contiene, así que el festivo nacional
  se declara una vez, en el país, y el local solo en su municipio.
- **Es lo único del contexto que se mantiene a mano.** El resto sigue siendo un catálogo de solo
  lectura que cambia por migración. Los festivos se publican cada año y los locales, pueblo a
  pueblo: no pueden ir en una semilla.
- **Rutas:**
  - `GET /api/geography/holidays?boundary=&year=&inherited=true` — los de una delimitación en un
    año; con `inherited`, también los de las que la contienen, por orden de fecha.
  - `POST /api/geography/holidays` con `{"boundary":…, "days":[{"date":…,"name":…}]}` — hasta 200
    días por llamada. Un día que la delimitación ya tiene se deja como está, así que el
    calendario de un año se puede enviar dos veces.
  - `DELETE /api/geography/holidays/{id}` — el que se declaró por error.
- **Permisos:** `Geography.Holiday.Read` y `Geography.Holiday.Update`.
- **Para otros contextos:** `contracts.Calendar.IsHoliday(delimitación, día)`. Los fines de semana
  no son festivos: que cuenten o no lo decide quien pregunta.
- **Tabla:** `geo_holidays` (migración 3), única por delimitación y día.
- **`Locate`** encuentra una delimitación por su código oficial (`ES`, `ES-MD`, `28`, `28079`):
  es como los calendarios nombran los lugares, y lo usa la importación.

### Quién lo usa: los vencimientos de Cobros

El puerto `Calendar` de Cobros recibe ahora **el vendedor** además del día. El anfitrión lo
conecta con `SellerCalendar` (`host/calendar.go`):

- sábados y domingos no se cobra;
- los demás días, se pregunta a Geografía por el lugar del vendedor: el municipio de su dirección
  postal en vigor en Parties o, si la dirección no nombra municipio, su país;
- un vendedor sin dirección solo tiene fines de semana.

Solo afecta a las condiciones de pago con «controlar festivos» activado: el vencimiento que cae
en día no hábil retrocede hasta `BackwardDays` días o, si no encuentra hábil, avanza.

### Decisiones (aprobadas por Javier el 2026-10-09)

1. **Los festivos son de Geografía**, colgados de una delimitación y heredados hacia abajo.
   Sugerencia: sí; es un hecho del lugar, y RRHH o Pagos podrán usar el mismo calendario.
2. **Geografía deja de ser solo lectura en esto**, con dos permisos nuevos. No se toca el resto.
   Sugerencia: sí.
3. **No hay semilla de festivos**: se cargan por la ruta o, desde el mismo día, con la fuente
   `holidays` de Importación (ver [IMPORTACION.md](IMPORTACION.md), «Fuente del calendario de
   festivos»).
4. **Sábados y domingos cuentan como no hábiles para cobrar**, y eso lo decide el anfitrión, no
   Geografía. Sugerencia: sí; es el uso bancario en España. Otro sector u otro país lo cambia en
   el adaptador.
5. **El lugar del vendedor es el de su dirección postal en Parties** (la primera en vigor con
   municipio; si no, el país). No el del cliente ni el del banco. Sugerencia: sí; era lo que
   apuntaba C# con «festivos» en la condición de pago del vendedor.
6. **Un vendedor sin dirección no falla**: solo se le aplican fines de semana. Sugerencia: sí.
7. **El lugar de cada empresa se recuerda cinco minutos** para no preguntar a Parties día a día.
   Un cambio de dirección tarda como mucho eso en notarse. Sugerencia: sí.
8. **Los vencimientos ya calculados no se recalculan** al declarar un festivo después.
   Sugerencia: sí; un vencimiento emitido es un compromiso con el cliente.
9. **El calendario es de la instalación, no de cada empresa**: quien tenga el permiso lo mantiene
   para todas. Sugerencia: sí; los festivos no dependen de quién pregunte.

## Aportaciones al framework

- `vocab.IBAN`.
- `sqlrepo.DB.InsertMany`: carga por lotes con `VALUES` de varias filas, `INSERT ALL` en Oracle
  (válido también antes de 23ai) y lotes limitados a 2.000 parámetros.
- `sqlrepo.RenderDDL` / `RenderDDLAll`: DDL con tipos lógicos (`{uuid}`, `{str:N}`, `{bool}`,
  `{ts}`, `{add:col}`…). Salió de Parties y ahora lo comparten los contextos.

## Validación

- Semilla: tamaños exactos y todas las invariantes del dominio sobre las ~32.500 filas.
- Extremo a extremo HTTP en memoria y en SQLite migrada:
  - Madrid y sus ancestros;
  - 28013 pertenece a Madrid y no a Barcelona;
  - 27 miembros de la UE;
  - toda España recorrida;
  - IBAN, países y catálogos;
  - 403 sin permiso.
- Entre contextos: Parties valida direcciones con Geografía sobre el mismo backend.
- Calendario de festivos, por HTTP en memoria y en SQLite: declarar los de España y los de Madrid,
  repetir sin duplicar, validaciones, el calendario propio y el heredado en orden, otro año vacío,
  el puerto por lugar (municipio, país, delimitación desconocida), borrar, y 403 sin cada permiso.
- En el anfitrión (memoria, SQLite y los cuatro motores): treinta días desde un viernes caen en
  domingo; una empresa de Madrid salta el domingo, el lunes festivo local y el martes festivo
  nacional; con días hacia atrás va al viernes anterior; una empresa sin dirección solo salta el
  fin de semana; unas condiciones que no controlan festivos se quedan en el domingo.
- Integración en PostgreSQL, SQL Server, Oracle (GUID RFC y .NET) y MySQL. Migración con carga
  completa de la semilla en 5–12 s por motor, y el mismo recorrido; repetible.

## Pendiente

- Mantenimiento de la geografía (altas y cambios de municipios) como casos de uso, cuando haga
  falta; hoy se hace por migración.
- Unificar los 9 tipos de organización supranacional en uno (cambio de datos, con migración).
- Validación de IBAN en las cuentas bancarias de Parties, cuando se porten.
