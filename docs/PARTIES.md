# Contexto Parties: mapa de contextos y port a Go

## 1. Punto de partida en C#

Parties (`ErpKernel.Domain/Subdominios/Parties` + `ErpDetail.Domain/Subdominios/Parties`) es hoy
**un único ensamblado, un esquema `parties` y un `DbContext`** con:

- 362 ficheros de dominio en ErpKernel y 150 en ErpDetail;
- 91 `DbSet` en la rebanada, y ~105 tablas en la migración `ContextoDeParties`;
- ~517 rutas HTTP.

Dentro conviven cinco cosas distintas:

| Grupo | Contenido | Tablas | Rutas |
|---|---|---|---|
| **Núcleo Parties** | Party (Person, Organization, LegalOrganization, Corporation), roles, relaciones, identificaciones, contactos, clasificaciones, características, cuentas bancarias, comunicaciones y casos | ~68 | ~447 |
| **Geografía** | GeographicBoundary (+ subtipos), jerarquía (`GeographicBoundaryAssociation`), tipos, códigos postales | 5 | 16 |
| **Instalaciones** | Facility (+ Warehouse, Plant, Building, Office, Floor, Room), tipos, `PartyFacility`, `FacilityContactMechanism` | 5 | 31 |
| **Datos de referencia** | CountryProfile, Currency (+ denominaciones), Language, TimeZone, StreetType, Country* | 10 | 23 |
| **Extensiones ErpDetail** | 30 agregados: convenios, contratos laborales, perfiles fiscales y comerciales, listas de precios, modelos AEAT… | 28 | 70 |

## 2. ¿Separar Geografía e Instalaciones? Evaluación

### Geografía → contexto propio: **sí, corte limpio**

- **No depende de nada.** `GeographicBoundary`, sus tipos, la jerarquía y los códigos postales
  no referencian Party, ni ninguna otra cosa.
- **Todo lo que entra es por identificador.** Llegan referencias desde:
  - `PartyIdentification.IssuingCountry` y `PersonFiscalProfile.CountryOfResidence`;
  - `PostalAddress.PostalCodeId`;
  - Orders (`SalesTaxLookup`, `AgreementGeographicalApplicability`).
- **Es un catálogo de lectura** con ~32.000 filas sembradas (8.497 delimitaciones, 8.870
  asociaciones, 14.608 códigos postales), sin eventos y sin reglas.
- **Qué hay que arreglar al cortar:**
  - `IGeographicBoundaryReadRepository.GetAddressCountryIdByPartyIdsAsync` consulta
    `PartyContactMechanisms`: un repositorio de geografía leyendo datos de Party. Se muda a
    Parties, que pedirá a Geografía los ancestros y descendientes de una delimitación a través
    de un puerto.
  - `Product.IGeo : IGeographicBoundary` hereda un contrato de otro contexto y debe pasar a ser
    una referencia por Id.
  - Las ~6 FK de la base que cruzan (`party_identification`, `person_fiscal_profile`,
    `contact_mechanism.PostalCodeId`, `postal_address_boundary`) pasan a referencias por
    identidad, validadas en la aplicación.
- **Qué se queda en Parties:** `PostalAddressBoundary` (la dirección señala una delimitación: es
  de quien posee la dirección) y `CountryDocumentRule`, que es política de validación de
  documentos de identidad de Parties.

### Datos de referencia: **agruparlos con Geografía («Geografía y referencia»)**

`CountryProfile`, `CountryCurrency`, `CountryLanguage`, `CountryTimeZone`, `Currency`, `Language`,
`TimeZoneRef` y `StreetType` solo dependen de la Id del país, tienen el mismo ciclo de vida
(sembrados, de lectura) y el mismo consumidor. Separarlos en un tercer contexto no aporta nada hoy;
el corte interno queda preparado.

Excepciones:

- `Currency.Facial` y `CurrencyDenomination` son de cambio de divisa (Maccorp/Financial) y deben
  ir allí.
- `CountryReferenceApplicationService` mezcla referencia con documentos de identidad y hay que
  partirlo.

### Instalaciones → contexto propio: **sí, pero antes hay que tomar tres decisiones**

- **Facility está ligada a Party, no a la geografía.**
  - `PartyFacility` + `FacilityRoleType` modelan propietario, operador, sede, centro de trabajo…
  - El ámbito por organización pasa por `PartyFacility`.
  - La dirección y el teléfono se toman prestados del `ContactMechanism` de Party. Facility no
    tiene ningún campo geográfico: llega a la geografía a través de la dirección.
- **Es la entidad más referenciada desde fuera** (≈45 ficheros, todos por Id): Product
  (`InventoryItem`, `StockBalance`), Shipments, WorkEffort, Orders, Maccorp e Import.
- **Maccorp consulta `ctx.Set<Facility>()` directamente** sobre el `DbContext` compartido: hay que
  sustituirlo por un puerto.
- **RRHH `WorkCenter` duplica la instalación**, con la dirección guardada como texto.

Decisiones pendientes (recomendación entre paréntesis):

1. **¿De quién es `PartyFacility`?** (Recomiendo **Parties**, que referencia `FacilityID`, igual
   que `PartyContactMechanism`. Así la comprobación de ámbito por organización no se mueve.)
2. **¿Cómo se modela la dirección y el teléfono de una instalación?** (Recomiendo **un objeto
   valor de ubicación propio de Instalaciones**: dirección con Ids de Geografía y teléfono. Nada
   de `ContactMechanism` prestado.)
3. **¿Qué pasa con `WorkCenter` de RRHH?** (Recomiendo que referencie `FacilityID` en lugar de
   duplicar la dirección.)

Lenguaje publicado de Instalaciones: `facility-registered.v1`, `facility-renamed.v1` y
`facility-deactivated.v1`, para las cachés de nombres de Product, Shipments y Maccorp, más un
directorio `ResolveFacilities(ids)`.

### Mapa de contextos propuesto

```text
                 Geografía y referencia  (OHS + lenguaje publicado; catálogo de lectura)
                   ▲ Ids de país / delimitación / código postal
     ┌─────────────┼──────────────────────────┐
     │             │                          │
  Parties  ◄── Id ── Instalaciones ◄── Id ── Product · Shipments · WorkEffort · Maccorp
  (OHS: directorio, eventos v1)      (OHS: directorio, eventos v1)
     ▲ Id de party / rol
  Security · RRHH · Orders · Accounting · … (consumidores del directorio y de los eventos)
```

Otros candidatos detectados en el análisis (fuera de esta decisión):

- **Comunicaciones y casos** (`CommunicationEvent`, `Case`): encajan como contexto de
  interacciones o CRM.
- **Extensiones ErpDetail:** la mayoría pertenecen a RRHH o Nómina (contrato laboral, convenio,
  cuentas de cotización), a Ventas o Precios (listas de precios, perfiles comerciales), o a
  Fiscal/AEAT.

## 3. Port a Go: fase 1 (hecha)

Ubicación: `contexts/parties/`, con las mismas capas que el ejemplo del framework:

```
contexts/parties/
├── domain/          # Party, PartyRole, RoleType + Catalog, Relationship, eventos, especificaciones
├── contracts/       # Lenguaje publicado v1 + Directory (Open Host Service)
├── application/     # casos de uso con permisos, DTOs, traducción al lenguaje publicado, directorio
├── infrastructure/  # mapeos SQL, migraciones de los 5 motores + semillas, catálogos, fábricas hot swap
├── distribution/    # HTTP (/api/persons, /api/organizations, /api/parties, /api/party-relationships…)
└── module.go        # composición del contexto
```

### Decisiones de modelo (y defectos del C# que corrigen)

| Tema | C# | Go |
|---|---|---|
| Person/Organization | Subclases de Party mapeadas TPT (4 tablas); la retirada del discriminador rompió los inserts (error 23502) | Una sola `Party` con `Kind` y detalles como objetos valor. Una tabla. |
| Datos de la persona | Estado civil, DNI, pasaporte y `PersonalName` («JOHN DOE» por defecto) **no se persistían** | `PersonalName` con nombre y dos apellidos, género, fecha de nacimiento (no futura) y estado civil, todo persistido |
| Roles | `PartyRoleApplicationService` creaba un `Party("Temp")` falso y guardaba el rol directamente: **nunca se comprobaba el solapamiento** | Los roles son hijos del agregado. `AssignRole` exige tipo existente, no categoría, compatible con persona u organización, party activa y sin solapamiento del mismo rol |
| Jerarquía de roles | Codificada en `WellKnownCatalog` **y** en la tabla `role_type_hierarchy` (dos fuentes de verdad) | La tabla `role_types.parent_id` es la única fuente; se siembra desde el mismo catálogo con **los mismos GUID**. `Catalog` valida que no haya ciclos y deriva a qué tipo de party aplica cada rol |
| Herencia de roles | Virtual al leer (`LoadRolesSectionAsync`) | Igual: `PlaysAt` y `Catalog.Descendants`. En la búsqueda se traduce a `EXISTS` con la vigencia en SQL |
| Relaciones | Partes «PartyFrom»/«PartyTo» falsas; `RelationshipTypeAllowedSpec` devolvía siempre `true`; sin control de origen distinto del destino | `Establish` exige partes distintas y activas que **jueguen los roles del tipo** (o uno por debajo) en la fecha de inicio. Además rechaza duplicados solapados, en ambos sentidos si el tipo es simétrico |
| Vigencia | `ExpirationDate` sin setter en PartyRelationship; `ThruDate` duplicado en OrganizationRollup | `vocab.ValidPeriod` semiabierto con `Terminate`/`EndRole` |
| Catálogo | Roles de pedido (C0–C8) dentro del catálogo de roles de party; `Salesperson`/`Collaborator` usados en tipos de relación pero fuera de la jerarquía; `DepartmentAssignment` declarado sin fila | Los roles de pedido son de Orders; `Salesperson` cuelga de Persona y `Collaborator` de la raíz; `DepartmentAssignment` queda pendiente |
| Autorización | Política en el endpoint | `pipeline.RequirePermission` en cada caso de uso (`Parties.Party.Create`, `Parties.PartyRole.Assign`, `Parties.Relationship.Create`…) |
| Integración | `IPartyDirectory` + outbox sin consumidores | `contracts.Directory` (resolución por lotes de 900) + 8 eventos v1 en el outbox de integración |

### Aportaciones al framework en esta fase

- `spec.OptionalTime`: fechas anulables (fin de vigencia abierto) con comparaciones que nunca
  casan con NULL, igual en memoria y en SQL.
- `sqlrepo.Migration.Run`: migraciones en Go para semillas que necesitan el formato de valores
  del dialecto (UUID en `RAW(16)` de Oracle, orden .NET…).
- `sqlrepo.DB.Insert` y `sqlrepo.DB.Select`: inserción parametrizada y lectura de catálogos.

### Validación

- **Dominio:** nombres, catálogo (aplicabilidad, herencia, ciclos, padres desconocidos, coherencia
  con los tipos de relación), reglas de roles y relaciones, y especificaciones en memoria.
- **Extremo a extremo HTTP** (`contexts/parties/parties_test.go`):
  - JWT y autorización con un usuario administrador y otro de solo lectura;
  - el escenario completo en memoria y, tras migrar y verificar, en SQLite cambiada en caliente;
  - problemas 401, 403 y 422;
  - traza de auditoría con el actor y el lenguaje publicado consumido por un CRM con inbox.
- **Integración** (`integration/parties_context_test.go`) en PostgreSQL, SQL Server, Oracle (GUID
  RFC y .NET) y MySQL:
  - migraciones y semillas;
  - roles con herencia y vigencia traducidos a SQL;
  - ida y vuelta de fechas civiles;
  - relación duplicada y terminada;
  - directorio, auditoría y outbox de integración.

## 4. Siguientes fases

| Fase | Contenido |
|---|---|
| 2 | Identificaciones (tipos de documento + `CountryDocumentRule` con `vocab.Identification`), contactos (correo electrónico, teléfono, dirección postal con Ids de Geografía) y clasificaciones y características |
| 3 | Organización interna y ámbito: `IPartyOrganizationMembership`, `IOrganizationHierarchy`, `IInternalOrganizationCatalog`, rollups; visibilidad P1 (`PartyRelationship` + permiso) |
| 4 | Contexto Geografía y referencia (si se aprueba la separación) con su semilla de 32.000 filas desde `040-Data/schema` |
| 5 | Contexto Instalaciones (tras las tres decisiones de la sección 2) |
| — | Clientes, empleados y extensiones de ErpDetail, cada uno en el contexto que le corresponda (Ventas, RRHH, Nómina, Fiscal), no en Parties |
