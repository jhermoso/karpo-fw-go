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

- **Solo lectura.** Los datos cambian mediante migraciones. Guardar un agregado devuelve
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
- Integración en PostgreSQL, SQL Server, Oracle (GUID RFC y .NET) y MySQL. Migración con carga
  completa de la semilla en 5–12 s por motor, y el mismo recorrido; repetible.

## Pendiente

- Mantenimiento de la geografía (altas y cambios de municipios) como casos de uso, cuando haga
  falta; hoy se hace por migración.
- Unificar los 9 tipos de organización supranacional en uno (cambio de datos, con migración).
- Validación de IBAN en las cuentas bancarias de Parties, cuando se porten.
