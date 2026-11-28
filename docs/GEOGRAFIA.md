# Contexto Geografía y referencia

Decisión aprobada por Javier el 2026-09-27 (ver [PARTIES.md](PARTIES.md), sección 2): la geografía y
los datos de referencia salen de Parties a un contexto propio. Es un catálogo de lectura que otros
contextos referencian por identidad (los mismos GUID que Karpo) o por código ISO.

## Contenido y origen

Semilla de Karpo (`040-Data/schema/sqlserver/02-semillas.sql`, esquema `parties`) extraída a CSV
comprimidos embebidos en el binario (`contexts/geography/infrastructure/seed`, 639 KB).

| Datos | Filas | Nota |
|---|---|---|
| Tipos de delimitación | 28 | Continente, país, comunidad autónoma, provincia, municipio… y agrupaciones supranacionales |
| Delimitaciones | 8.507 | 8.132 municipios, 248 países, 52 provincias, 10 islas, 19 comunidades y ciudades autónomas, 7 agrupaciones de comunidades, 22 subcontinentes, 6 continentes y agrupaciones. Cada una con su país y su nivel: ver [GEOGRAFIA-NUTS.md](GEOGRAFIA-NUTS.md) |
| Vínculos | 8.880 | 8.490 de contención (un padre como máximo; los municipios insulares, su isla) y 390 de pertenencia (país → UE) |
| Códigos postales | 14.608 | 11.051 códigos distintos; un código puede cubrir hasta 10 municipios |
| Perfiles de país | 248 | ISO 3166 (alfa-2, alfa-3 y numérico), prefijo telefónico, IBAN, formato postal, UE, EEE, eurozona, SEPA |
| Monedas / idiomas / zonas horarias / tipos de vía | 182 / 183 / 5 / 40 | Con sus vínculos por país (71 / 4 / 2) |

### Nombres con la codificación rota

La semilla traía un nombre con *mojibake* (UTF-8 leído como Latin-1 y vuelto a codificar): la
provincia de código geográfico `15` se llamaba «CoruÃ±a, A» en lugar de «Coruña, A». Revisados
todos los textos de los doce ficheros (secuencias `Ã`, `Â`, `â€` y el carácter de sustitución
U+FFFD), era el único caso: los municipios, los países, las monedas, los idiomas y los tipos de
vía estaban bien.

- **Semilla:** corregido `boundaries.csv.gz`. Los backends en memoria y las bases de datos nuevas
  reciben ya el nombre correcto.
- **Bases de datos ya migradas:** la migración 4 («boundary names with broken encoding») corrige
  la fila en `geo_boundaries` por su identificador. En una base nueva no cambia nada.
- **Prueba:** `TestSeed_NoTextHasABrokenEncoding` recorre todas las columnas de todos los ficheros
  de la semilla y falla si aparece alguna de esas secuencias.

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
    `GET /api/reference/{currencies|languages|time-zones|street-types|boundary-types|boundary-levels}`;
  - `GET /api/geography/boundaries` admite además `country` y `level` (`country`, `1`, `2`, `3`, `local`).
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
   `holidays` de Importación. **Hoy sólo por la ruta:** el contexto de importación se retiró
   después de este repositorio.
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

## El contrato de mantenimiento (front; añadido el 2026-11-15)

Países y festivos se ven también por el **contrato de mantenimiento**, el que lee el motor de
Angular de Karpo C# (el contrato `crud` del lenguaje de Theros), escrito a mano en
`contexts/geography/maintenance.go` sobre los casos de uso de siempre. La configuración de las
pantallas está en `front/angular/config/entities/geography/` (ver front/README.md). Es el mismo
contrato que el de Instalaciones (docs/INSTALACIONES.md).

**Qué entidades.** Sólo dos. Los países, porque son lo que un usuario busca y consulta; los
festivos, porque son lo único del contexto que se mantiene a mano. Las delimitaciones, los códigos
postales y los catálogos pequeños (monedas, idiomas, husos, tipos de vía) son datos de la semilla
y siguen en `/api/geography` y `/api/reference`: no tienen alta ni cambio.

**Países (`/api/v1/countries`): sólo lectura.** Son ISO 3166 con sus formatos (IBAN, código postal)
y llegan con las migraciones; ningún caso de uso escribe en ellos. La identidad del contrato es el
código alfa-2 (`ES`), la que usan las demás rutas y los demás contextos. `POST`, `PUT` y `DELETE`
responden **409** con la vía («vienen con las migraciones»), después de exigir
`Geography.Reference.Read` (403 sin él): un 409 con `detail` explica por qué no se puede; un 405
sin cuerpo, no.

| Operación | Ruta | Qué hace |
|---|---|---|
| Lista | `GET /api/v1/countries?page&pageSize&sortBy&sortOrder&search&name&eu&eurozone&sepa&ids`, o `POST /api/v1/countries/search` | la página del motor. Orden por `alpha2` (por omisión), `alpha3` o `name`, con `alpha2` para deshacer empates; otro, 400. `search` busca en el nombre o es un código alfa-2 o alfa-3 exacto; `eu`, `eurozone` y `sepa` valen `true` o `false` (otro valor, 400). La distribución sólo traduce: la consulta `SearchCountries` de la aplicación mira el permiso, valida después (sin permiso, 403 aunque `ids` o `eu` estén mal), filtra `ids`, `eu`, `eurozone` y `sepa` con una especificación sobre `geo_countries`, y busca el texto, ordena y pagina en memoria, porque el nombre está en el límite (`geo_boundaries`) y los países son unos 250. `ids` pide varios por su alfa-2 (dos letras, sin distinguir mayúsculas; `ESP` o `1A`, 400 `ids/format`; uno bien formado que no existe, como `XX`, sólo no está). Ver [el filtro ids](CONTRATO-MANTENIMIENTO.md) |
| Lectura | `GET /api/v1/countries/{alfa-2}` | el país con todos sus datos (los de `/api/reference/countries/{alpha2}` más `id`) |
| Alta, cambio, baja | `POST`, `PUT …/{id}`, `DELETE …/{id}` | 409 con la vía |

**Festivos (`/api/v1/holidays`).**

| Operación | Ruta | Qué hace |
|---|---|---|
| Lista | `GET /api/v1/holidays?page&pageSize&sortBy&sortOrder&search&name&boundary&year&ids`, o `POST /api/v1/holidays/search` | la página del motor. Orden por `date` (por omisión) o `name`, con la identidad para deshacer empates; otro, 400. `boundary` deja los declarados en esa delimitación (sólo los suyos; los heredados siguen en `/api/geography/holidays?inherited=true`); `year`, los de un año (1900–2200; otro, 400); `search` y `name` buscan en el nombre. `ids` pide varias identidades a la vez, para resolver etiquetas (ver [el filtro ids](CONTRATO-MANTENIMIENTO.md)). Los festivos no tienen ámbito de organización: basta el permiso de leerlos |
| Lectura | `GET /api/v1/holidays/{id}` | el festivo, con `boundaryName` |
| Alta | `POST /api/v1/holidays` con `{boundary, date, name}` | 201 con `Location`; un solo día (la declaración por lotes sigue en `/api/geography/holidays`). Si el lugar ya tiene festivo ese día, 409 (el lote, en cambio, lo deja como está) |
| Cambio | `PUT /api/v1/holidays/{id}` con lo leído y su `version` | 200; lo que el cuerpo no trae se queda. Sin `version`, 400; desfasada, 409, sin reintento. Sólo cambia el **nombre** (recortado, 1–120). `boundary` y `date` no cambian: se comparan por su valor (identidad sin mayúsculas, día como fecha) y, si difieren, 400; un festivo en otro sitio u otro día es otro festivo. Un cambio sin cambios no gasta versión, pero exige `Geography.Holiday.Update`. Todo en una unidad de trabajo |
| Baja | `DELETE /api/v1/holidays/{id}` | 204: **borra** el festivo. Nadie lo cita (los vencimientos preguntan a `Calendar` cada vez) y uno declarado por error no tiene historia que guardar; la auditoría deja constancia. Repetirla da 204, pero exige `Geography.Holiday.Update` (403 sin él) |

- **El objeto:** `id`, `boundary`, `boundaryName`, `date` (YYYY-MM-DD), `name`, `version`. La
  `version` aparece también en las respuestas de `/api/geography/holidays`.
- **Casos de uso nuevos** en `Holidays`: `Get`, `Page`, `Register` (uno) y `Rename` (versión
  obligatoria); en el dominio, `Holiday.Rename` y los campos `HolidayFieldName` y
  `HolidayFieldIDOrder`. Permisos, los de siempre.
- **Consultas nuevas en `sqlrepo`:** la página de `geo_holidays` (`FindPage` con filtro por
  `boundary`, rango de `holiday_date` y `name` sin mayúsculas; orden por `holiday_date` o `name` y
  después `id`) y el `UPDATE` del nombre. Probadas en memoria y SQLite; falta pasarlas por los
  motores de `integration/`.
- **El error:** RFC 9457 con `errors` como diccionario (campo → mensajes) y `errorCodes`; cuerpo de
  más de 1 MiB, 413.
- **Prueba:** `TestGeography_MemoryThenSQLite` (escenario `maintenance`, en memoria y en SQLite).

## El mapa: claves y teselas (B-2 y B-26; añadido el 2026-11-28)

**Regla única:** el nivel de la tesela es el `level` de la semilla. Está en el mapa todo límite
activo con nivel `country`, `2`, `3` o `local` cuyo tipo tenga regla de clave
(`(*Boundary).MapCode()`, en `domain/mapcode.go`). La API (`code`, `parentCode`) y la tesela
(`cmd/geokeys` → `keys.csv` → `tools/geo`) salen de esa misma función, así que casan por
construcción.

**Límites inactivos:** la API da `code` y `parentCode` también a un límite inactivo (la clave es
su identidad y sirve para leer datos antiguos); la tesela sólo lleva los activos
(`infrastructure.MapKeys`). El front sólo pinta lo que hay en la tesela.

| Tipo | Nº | `level` | `kind` | `code` | `parentCode` | Geometría |
|---|---|---|---|---|---|---|
| País | 248 | `country` | `country` | alfa-2 (`ES`) | — | Natural Earth 10m |
| Agrupación NUTS 1 | 7 | `1` | — | fuera del mapa | — | — |
| Comunidad o ciudad autónoma | 19 | `2` | `region` | ISO 3166-2 (`ES-MD`) | `ES` | BDDAE (IGN) |
| Provincia con nivel | 49 | `3` | `province` | INE (`28`) | su comunidad | BDDAE |
| Provincia insular (07, 35, 38) | 3 | — | — | fuera del mapa | — | — (la cubren sus islas) |
| Isla | 10 | `3` | `island` | **NUTS 3** (`ES532`) | `ES-IB` o `ES-CN` | sus municipios, disueltos |
| Municipio | 8.132 | `local` | `municipality` | INE (`28079`, = `lauCode`) | su provincia o su isla | BDDAE |

- `parentCode` es la clave del primer antecesor que está en el mapa: 07040 → `ES532` → `ES-IB` →
  `ES`. Se calcula por rondas (`domain.MapParents`), una consulta por ronda y por tramos de 500,
  nunca una por límite. Un país no sube.
- **Excepción documentada (D-c, provisional):** la isla se nombra por su NUTS 3 porque no tiene
  código ISO ni INE. La alternativa (las 52 provincias INE, sin islas) exige cambiar la semilla.
- **Defecto conocido:** Navarra sale `ES-NA` (ISO dice `ES-NC`) en la API, en la tesela y en los
  festivos. Se corrige en la semilla en una tarea aparte, común con Trench y Karpo C#.
- Una prueba de ida y vuelta comprueba que `Locate` (festivos) resuelve cada clave del mapa salvo
  las islas, que `Locate` no conoce.

**La tesela** (`karpo.pmtiles`, artefacto de despliegue, fuera del repositorio): capa `boundaries`
(polígonos) y capa `labels` (un punto dentro de cada polígono), con `code`, `name`, `level`,
`kind`, `hasChildren`, `parentCode` (salvo países) y `lauCode` (municipios). Se genera con
`tools/geo/build.sh` (fuentes y licencias en `tools/geo/SOURCES.md`) y la sirve el anfitrión con
`KARPO_PMTILES` (ver `docs/ANFITRION.md`), público, con rangos y ETag.

- **Zoom 0–8.** El front fija `maxzoom: 8` en la fuente y amplía las teselas por encima (su
  cámara no pasa de z10); a z8 una unidad de tesela mide unos 29 m. El `maxzoom` de la cabecera
  del fichero y el del front deben coincidir: con menos, el front pediría teselas que no existen.
  Lo comprueba `pmtiles_geo_test.go`. Generarla tarda unos 16 minutos (z12 tardaba horas).
- **D-b:** a z0 están los 8.132 municipios como rótulo (`--drop-rate=1`); su polígono aparece en
  cuanto tiene área a ese zoom (a z0 una unidad mide unos 10 km). El tamaño medido de z0 está en
  `tools/geo/SOURCES.md`.
- **Procedencia:** `build.sh` escribe `karpo.provenance.txt` junto al fichero: huella y tamaño del
  `.pmtiles`, fecha de descarga y número de unidades de la BDDAE, huella de cada página y de
  Natural Earth, e imágenes usadas. La atribución lleva la fecha de descarga, no la de generación.
- **Si falla tras una actualización del IGN:** `join` se detiene cuando la BDDAE trae un municipio
  nuevo (fusión, segregación) o pierde uno de la semilla, porque lo que no enlaza es una lista
  cerrada (`tools/geo/expected-unmatched.txt`). Es lo deseado: se revisa la semilla y la lista, y
  se vuelve a generar. Para descargar otra vez, borra `ign/` en `GEO_WORK`.

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
- Pasar por los motores de `integration/` la página y el cambio de nombre de `geo_holidays` del
  contrato de mantenimiento (hoy, memoria y SQLite).
- Unificar los 9 tipos de organización supranacional en uno (cambio de datos, con migración).
- Validación de IBAN en las cuentas bancarias de Parties, cuando se porten.
