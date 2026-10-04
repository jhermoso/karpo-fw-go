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

Decisiones (**aprobadas por Javier el 2026-09-27**: las tres recomendaciones, y el contexto «Geografía y referencia»):

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

## 4. Fase 2 (hecha): identificaciones, contactos y clasificaciones

| Tema | C# | Go |
|---|---|---|
| Identificaciones | Agregado aparte; **solo marcaba** un «estado de formato» (válido, inválido, desconocido) después de guardar; sin control de duplicados entre parties; `RequiresIssuingAuthority` sin campo donde guardar la autoridad | Hijas de Party. `DocumentPolicy` (7 tipos + 31 reglas por país, mismos GUID) **rechaza** los números inválidos: longitud, patrón y dígito de control. Hay un único documento principal; la autoridad emisora y las fechas de expedición y caducidad se guardan; un documento identifica a una sola party (comprobación en la aplicación + índice único) |
| Dígitos de control | 11 algoritmos en la capa de aplicación | `vocab.ValidCheckDigit` (clave igual a la columna `ChecksumKey`): ES DNI, NIE y CIF, PT, IT, FR, DE, NL, BE, EL y HR, más NIF y NSS españoles. Un catálogo que nombre un algoritmo desconocido falla al cargarse |
| Forma canónica | Recorte y mayúsculas | Sin espacios, y sin separadores salvo que el formato del país los exija (el signo de siglo finlandés): `12345678-z` es `12345678Z` |
| Contactos | `ContactMechanism` (TPH) + `PartyContactMechanism` + `PartyContactMechanismPurpose` **sin enlazar entre sí**; tres modelos de teléfono coexistiendo | Contacto hijo de Party: correo electrónico, teléfono y fax (E.164), web y dirección postal como objeto valor con Ids de Geografía (decisión del mapa de contextos). Propósitos (`default`, `billing`, `shipping`, `home`, `work`) con un titular vigente por tipo; sin duplicados vigentes; vigencia; `nonSolicitation` |
| Clasificaciones | `party_type` mezclaba tipos heredados (Person, Organization, Corporation… = subclases o formas jurídicas; «Public Catalog» = visibilidad) con las familias de clasificación | Solo las 5 familias y 22 hojas (mismos GUID). Aplicabilidad por familia (el tamaño y el sector, solo a organizaciones); **familias exclusivas** (segmento, tamaño, AML): una hoja vigente a la vez; AML inactiva hasta que la active cumplimiento normativo. Los tipos 1–9 quedan fuera: la forma jurídica y la visibilidad se tratarán en la fase 3 |

Lenguaje publicado nuevo (v1): `party-identification-added` y `-removed`, `party-contact-added`,
`-purposes-changed` y `-ended`, `party-classified` y `party-classification-ended`.

Rutas:

- `POST` y `DELETE /api/parties/{id}/identifications`;
- `POST /api/parties/{id}/contacts`, `…/purposes` y `…/end`;
- `POST /api/parties/{id}/classifications` y `…/end`;
- `GET /api/catalogs/document-types?country=ES` y `GET /api/catalogs/party-classifications`;
- `GET /api/parties?document=…&classification=…`.

Hallazgo de motor: SQL Server ordena `UNIQUEIDENTIFIER` por grupos de bytes, así que `ORDER BY id`
no es cronológico con UUID v7. Parties ordena sus hijos en Go al hidratar, para que todos los motores
devuelvan lo mismo.

Pendiente de esta fase:

- Características (`PartyCharacteristic`): no hay tipos sembrados en C#.
- `ValidContactMechanismRole`.
- La validación del código postal contra Geografía, que llegará con ese contexto (hoy se guardan
  sus Ids).

## 5. Fase 3 (hecha): organizaciones internas y ámbito

| Tema | C# | Go |
|---|---|---|
| Visibilidad (P1) | Filtros por `IOrganizationScopeProvider` repartidos en 159 ficheros; la pertenencia se calculaba al consultar, recorriendo relaciones con un mapa fijo de «lado empresa» (solo 5 tipos) | **Afiliaciones**: cuando una relación toca una organización interna, la otra party queda afiliada a ella en la misma transacción. «Visible para mi ámbito» es una especificación sobre un solo agregado (`VisibleTo`, que se traduce a `EXISTS` en SQL): la propia organización, las parties compartidas o las afiliadas vigentes. Se aplica a cualquier tipo de relación, como dice P1 |
| Fuera de ámbito | — | **404 uniforme** en lectura, escritura y compartición; 403 cuando la party es visible pero el acceso es de solo lectura. Para escribir hace falta acceso completo (`Full`) en la propia organización o en una de sus afiliaciones |
| Alta | Una party nueva sin relación era invisible para quien la creaba | El alta lleva una **afiliación**: organización y tipo de relación, obligatoria salvo para el administrador global. La party recibe el rol de su lado del tipo, y la relación y la afiliación se crean en la misma transacción (como el alta rápida de cliente del C#) |
| «Public Catalog» | Tipo de party 9 | `Party.Share`: visible para todas las organizaciones y editable solo por el administrador global |
| Formas jurídicas | Tipos de party 3 a 8 | `OrganizationDetails.LegalForm`: `corporation`, `government-agency`, `non-profit`, `partnership`, `sole-proprietorship` o `team` |
| Jerarquía | `OrganizationRollup` con el catálogo Department → Division, mientras `IOrganizationHierarchy` bajaba desde las organizaciones legales; se filtraba por `IsActive` y no por la vigencia; se cargaban **todas** las organizaciones legales para hacer una intersección | Tipos de relación **jerárquicos**. Rollup = unidad organizativa → organización, con un padre vigente, sin ciclos y un máximo de 10 niveles. Los recorridos hacen una consulta por nivel y usan la vigencia |
| Puertos | `IPartyOrganizationMembership`, `IOrganizationHierarchy`, `IInternalOrganizationCatalog` | `contracts.Membership`, `contracts.OrganizationHierarchy` (`Descendants`, `InternalOrganizationOf`) y `contracts.InternalOrganizationCatalog`, implementados por `application.Organizations` |

Lenguaje publicado nuevo: `party-affiliated.v1` y `party-affiliation-ended.v1`.

Rutas:

- `PUT /api/parties/{id}/legal-form` y `PUT /api/parties/{id}/shared`;
- `GET /api/internal-organizations`;
- `GET /api/parties?organization=…`;
- el alta acepta `affiliation`.

Framework: `sqlrepo.DB.Update` para las migraciones de datos; `ALTER TABLE … ADD` con la sintaxis de
cada motor, comprobado en los cinco.

Validado:

- en el test HTTP de extremo a extremo (memoria y SQLite), con cinco perfiles: administrador global,
  lector, acceso completo, solo lectura con permisos de escritura, y ajeno;
- en la integración de los cinco motores: búsqueda con ámbito, 404 fuera de ámbito, alta con
  afiliación, ciclo rechazado, forma jurídica, descendientes, organización interna de una unidad y
  pertenencia.

Pendiente:

- `IncludeSubsidiaries` sigue sin ampliar el ámbito (P2 v1): cuando se decida, `Descendants` ya lo
  permite.
- Las afiliaciones dependen de que la contraparte juegue Internal Organization **al establecer** la
  relación: si una organización pasa a ser interna más tarde, hay que recalcular (tarea de
  mantenimiento pendiente).
- ~~P3 (`OrganizationAdmin` con ámbito) pertenece al contexto Security~~: hecho en
  [SEGURIDAD.md](SEGURIDAD.md). La prueba de extremo a extremo de Parties corre también sobre el
  directorio real de Security.

Reacciones a otros contextos (primer consumidor de Parties, decisión 4 de [RRHH.md](RRHH.md)):
`hr.employee-terminated.v1` termina la relación `Employment` y la afiliación, a través de la
bandeja de entrada `parties_inbox` (migración 10) y de `Module.Consumer`.

Contrato `TaxIdentities` (para Fiscal): documento fiscal (el principal, o TXID > NIDN > ARNU) y
provincia del contacto postal español vigente (facturación primero), por lotes. Ver
[FISCAL.md](FISCAL.md).

## 6. Siguientes fases

| Fase | Contenido |
|---|---|
| ~~2~~ | ✅ Identificaciones, contactos y clasificaciones (sección 4) |
| ~~3~~ | ✅ Organización interna y ámbito (sección 5) |
| ~~4~~ | ✅ Contexto Geografía y referencia ([GEOGRAFIA.md](GEOGRAFIA.md)) (si se aprueba la separación) con su semilla de 32.000 filas desde `040-Data/schema` |
| ~~5~~ | ✅ Contexto Instalaciones ([INSTALACIONES.md](INSTALACIONES.md)) |
| ~~6~~ | ✅ Contexto RRHH ([RRHH.md](RRHH.md)): empleados (`Employment`), contratos, puestos y centros de trabajo; Parties conserva el rol `Employee` y la afiliación |
| — | Clientes y el resto de extensiones de ErpDetail, cada uno en el contexto que le corresponda (Ventas, Nómina, Fiscal), no en Parties |
