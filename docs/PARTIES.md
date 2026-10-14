# Contexto Parties: mapa de contextos y port a Go

## 1. Punto de partida en C#

Parties (`ErpKernel.Domain/Subdominios/Parties` + `ErpDetail.Domain/Subdominios/Parties`) es hoy
**un único ensamblado, un esquema `parties` y un `DbContext`** con:

- 362 ficheros de dominio en ErpKernel y 150 en ErpDetail;
- 118 `DbSet` en la rebanada (90 del kernel y 28 de ErpDetail), y 104 tablas en la migración
  `ContextoDeParties` (76 y 28);
- 464 rutas HTTP activas en la rebanada (325 del kernel y 139 de ErpDetail).

Las cifras de esta sección están **medidas** el 2026-10-04 sobre el commit `6093f680` de Karpo
(`000-Docs/design/Karpo_Estudio_Convergencia_Modelos.md`, §2). Las de la primera versión de este
documento eran estimaciones, y dos estaban lejos: el núcleo no eran ~68 tablas y ~447 rutas, y las
extensiones de ErpDetail no eran 70 rutas (70 era el número de ficheros de endpoints).

Dentro conviven cinco cosas distintas:

| Grupo | Contenido | Tablas | Rutas |
|---|---|---|---|
| **Núcleo Parties** | Party (Person, Organization, LegalOrganization, Corporation), roles, relaciones, identificaciones, contactos, clasificaciones, características, cuentas bancarias, comunicaciones y casos | 57 | 260 |
| **Geografía** | GeographicBoundary (+ subtipos), jerarquía (`GeographicBoundaryAssociation`), tipos, códigos postales | 4 | 16 |
| **Instalaciones** | Facility (+ Warehouse, Plant, Building, Office, Floor, Room), tipos, `PartyFacility`, `FacilityContactMechanism` | 5 | 31 |
| **Datos de referencia** | CountryProfile, Currency (+ denominaciones), Language, TimeZone, StreetType, Country* | 9 | 18 |
| **Extensiones ErpDetail** | 28 agregados: convenios, contratos laborales, perfiles fiscales y comerciales, listas de precios, modelos AEAT… | 28 | 139 |

Del núcleo, 44 tablas y 197 rutas son lo que este port cubre (party, roles, relaciones,
identificaciones, contactos y clasificaciones); las otras 13 tablas y 63 rutas son
características, cuentas bancarias, comunicaciones y casos. Queda fuera de los cinco grupos una
tabla que no es de Parties (`work_effort_role_type`). En el árbol de trabajo había además dos
agregados de ErpDetail aún sin confirmar, con 11 rutas más.

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

- `Currency.Facial` y `CurrencyDenomination` son de cambio de divisa (FinancialDetail/Financial) y deben
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
  (`InventoryItem`, `StockBalance`), Shipments, WorkEffort, Orders, FinancialDetail e Import.
- **FinancialDetail consulta `ctx.Set<Facility>()` directamente** sobre el `DbContext` compartido: hay que
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
`facility-deactivated.v1`, para las cachés de nombres de Product, Shipments y FinancialDetail, más un
directorio `ResolveFacilities(ids)`.

### Mapa de contextos propuesto

```text
                 Geografía y referencia  (OHS + lenguaje publicado; catálogo de lectura)
                   ▲ Ids de país / delimitación / código postal
     ┌─────────────┼──────────────────────────┐
     │             │                          │
  Parties  ◄── Id ── Instalaciones ◄── Id ── Product · Shipments · WorkEffort · FinancialDetail
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
| Relaciones | Partes «PartyFrom»/«PartyTo» falsas; `RelationshipTypeAllowedSpec` devolvía siempre `true`; sin control de origen distinto del destino | `Establish` exige partes distintas y activas que **jueguen los roles del tipo** (o uno por debajo) en la fecha de inicio; desde el 2026-10-14 el caso de uso **asigna antes el rol a quien le falte** (§11). Además rechaza duplicados solapados, en ambos sentidos si el tipo es simétrico |
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
| Visibilidad (P1) | Filtros por `IOrganizationScopeProvider` repartidos en 95 ficheros; la pertenencia se calculaba al consultar, recorriendo relaciones con un mapa fijo de «lado empresa» (solo 5 tipos) | **Afiliaciones**: cuando una relación toca una organización interna, la otra party queda afiliada a ella en la misma transacción. «Visible para mi ámbito» es una especificación sobre un solo agregado (`VisibleTo`, que se traduce a `EXISTS` en SQL): la propia organización, las parties compartidas o las afiliadas vigentes. Se aplica a cualquier tipo de relación, como dice P1 |
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
| ~~7~~ | ✅ Conceptos básicos de UDM 1 que faltaban ([PARTIES-UDM.md](PARTIES-UDM.md), decisiones aprobadas el 2026-10-04): datos propios por tipo de relación (código estable en el catálogo, relación de cliente potencial con su tiempo de prueba, participación en el capital) y edición de persona |

## 7. Dirección postal: las piezas españolas (2026-10-12)

Decidido por Javier el 2026-10-12: la dirección lleva los campos que los formularios españoles
piden por separado. Hasta ahora todo iba en dos líneas de texto libre, y de un texto libre no se
puede sacar la planta para una casilla.

*Ejemplo:* «Calle Mayor 5 bis, bloque 2, portal A, escalera B, 3.º izquierda».

| Campo (HTTP) | Columna | Ejemplo | Qué es |
|---|---|---|---|
| `streetType` | `street_type` | `CL` | Ya existía: tipo de vía del catálogo |
| `line1` | `line1` | `Mayor` | Ya existía: el nombre de la vía. Sigue siendo el único obligatorio |
| `number` | `addr_number` | `5`, `5-7`, `s/n` | Número |
| `numberQualifier` | `addr_qualifier` | `bis` | Lo que sigue al número |
| `block` | `addr_block` | `2` | Bloque |
| `portal` | `addr_portal` | `A` | Portal |
| `staircase` | `addr_staircase` | `B` | Escalera |
| `floor` | `addr_floor` | `3` | Planta |
| `door` | `addr_door` | `izquierda`, `B` | Puerta o letra |

- **Todos opcionales, de diez caracteres como mucho.** Una dirección extranjera, o una española
  escrita como antes («Mayor 1» en `line1`), sigue valiendo: no se intenta partir el texto.
- **Aditivo.** Migración 15 (siete columnas en `party_contacts`). Ninguna ruta ni evento `v1`
  cambia; el evento `party-contact-added` y la auditoría llevan la dirección en una línea, que
  ahora incluye las piezas.
- **Decisiones técnicas tomadas** (reversibles): las piezas son las de los formularios de la
  Agencia Tributaria (número, calificador, bloque, portal, escalera, planta, puerta) y no las
  cinco columnas que Trench tiene sin usar en `contact_mechanism` (`StreetNumber2`,
  `Staircase`, `Floor`, `Door`, `Letter`). La correspondencia: escalera, planta y puerta, igual;
  `Letter` va en `door`; `StreetNumber2` no sé qué guarda (ningún código de Trench la lee) y
  la he tomado por el calificador. Trench no tiene bloque ni portal.
- **Las mismas piezas en las instalaciones.** Son vocabulario común (`vocab.Premises`, en
  `pkg/domain/vocab`): la dirección de una instalación las lleva con los mismos nombres
  ([INSTALACIONES.md](INSTALACIONES.md)).
- **Pendiente:** la región de la dirección sigue siendo texto libre aunque la dirección lleve
  los identificadores de Geografía (ver [GEOGRAFIA-NUTS.md](GEOGRAFIA-NUTS.md)).

## 8. Tipos de rol y de relación: código estable y alta desde la aplicación (2026-10-12)

Primer paso de la extensibilidad en Go ([EXTENSIBILIDAD.md](EXTENSIBILIDAD.md), X-3), y el primer
caso de la decisión sobre catálogos: un tipo de rol **sin datos propios** se añade como dato,
diciendo quién es su padre.

*Ejemplo:* un administrador añade «Guarantor» (avalista) debajo de «Person». Desde ese momento se
asigna como cualquier otro rol, y sólo a personas, porque cuelga de «Person».

```
POST /api/catalogs/party-role-types
{ "name": "Guarantor", "code": "guarantor", "parent": "<id de Person>" }
```

- **Código estable.** Cada tipo de rol tiene un `code` opcional y único, como ya lo tenían los
  de relación. Los 37 del programa llevan el suyo, que es su nombre en minúsculas y con guiones
  (`customer`, `internal-organization`, `bill-to-customer`). Es por lo que un modelo, una semilla
  o una extensión nombran el tipo sin usar su identificador. Migración 16.
- **Qué se comprueba al añadir:** que el padre existe, y que nadie tiene ya ese nombre (sin
  distinguir mayúsculas) ni ese código. El tipo nuevo siempre se puede asignar: no se crean
  categorías. A quién se puede asignar sale de su sitio en la jerarquía.
- **Quién.** Permiso propio, `Parties.RoleType.Define`, y además hay que ser administrador
  global: el catálogo es de toda la instalación. La acción no se llama «Create» a propósito,
  porque la regla estándar de Security reparte «Create» a cualquier usuario.
- **`wellKnown`** distingue en el listado los tipos del programa de los añadidos.
- **Queda en el historial.** Quién añadió el tipo, cuándo y con qué datos se anota en la
  auditoría, en la misma transacción que el alta (tipos `parties.role_type` y
  `parties.relationship_type`).
- **Los tipos de relación, igual.** `POST /api/catalogs/party-relationship-types` con nombre,
  código opcional y el rol que juega cada lado (`fromRole`, `toRole`); permiso
  `Parties.RelationshipType.Define` y administrador global. Se comprueba que los dos roles
  existen y que el nombre y el código están libres. Un tipo añadido no es jerárquico ni lleva
  datos propios. *Ejemplo:* «Guarantee», de «Guarantor» a «Customer»; desde entonces relaciona a
  un avalista con un cliente, y se rechaza con los lados al revés.
- **Lo que no hay todavía:** cambiar o retirar un tipo añadido; y los tipos **con** datos
  propios, que son el resto de la extensibilidad.
- **Decisiones técnicas tomadas** (reversibles): la unicidad del nombre y del código se comprueba
  antes de escribir y no con un índice único (un índice único sobre una columna que admite vacíos
  no se comporta igual en todos los motores). Para que dos altas simultáneas con el mismo código
  no entren las dos —un catálogo con un código repetido no se puede cargar, y dejaría fallando
  a todo Parties— las altas de catálogo pasan **de una en una** dentro del proceso. Vale mientras
  haya un solo proceso; con varios hará falta el índice único (`BACKLOG.md` §2d). En el almacén
  de memoria los tipos añadidos no sobreviven a un cambio de almacén.

## 9. Número de cliente (2026-10-13)

Decidido por Javier el 2026-10-11 ([PARTIES-MODELO-COMUN.md](PARTIES-MODELO-COMUN.md), D-10): el
número de cliente es de la pareja empresa–cliente, porque cada empresa numera a los suyos. En C#
estaba en el rol de cliente (uno para todas las empresas) y otra vez en el perfil comercial.

*Ejemplo:* Talleres Pérez es el cliente `4301` de Acme y el `0017` de Acme Levante.

```
PUT /api/party-relationships/{id}/customer-number
{ "customerNumber": "C-4300001" }
```

- Es un **detalle de la relación de cliente**, con el mismo mecanismo que la prueba del cliente
  potencial o el código de promoción ([PARTIES-UDM.md](PARTIES-UDM.md)): columna
  `party_relationships.customer_number` (migración 17), y la relación lo devuelve en
  `customer: { number }`. Pedirlo a una relación de otro tipo es un 422.
- Hasta 20 letras, cifras, guiones, puntos, barras y guiones bajos; se guarda en mayúsculas. Un
  número vacío lo retira. Permiso `Parties.Relationship.Update`, y además **poder escribir en la
  empresa que numera** (el lado que vende): el número es de su numeración, no del cliente.
- **Decisión técnica tomada** (reversible): un número no se da a otro cliente **mientras alguna
  relación del primero lo lleve, vigente o terminada**, y el mismo cliente lo recupera si vuelve.
  (El código de promoción hace lo contrario: queda libre al terminar la relación.) Una relación
  terminada ya no se puede cambiar, así que el número de un cliente que se fue es suyo para
  siempre.
- **Lo que esa regla no cubre:** si a un cliente **vigente** se le cambia o se le retira el
  número, el antiguo queda libre y otro cliente puede recibirlo, aunque aparezca en facturas ya
  emitidas. Impedirlo exige guardar el historial de números; no está hecho.
- **Lo que no hay:** numeración automática (el número lo da quien da de alta); que el alta de la
  party o de la relación lo traiga ya puesto; un evento cuando cambia; y un puerto para que
  Facturación o Pedidos lo lean. Tampoco se comprueba con un índice único, como el código de
  promoción (`BACKLOG.md` §2d).

## 10. Marca de documento identificativo erróneo (2026-10-13)

Decidido por Javier el 2026-10-11 ([PARTIES-MODELO-COMUN.md](PARTIES-MODELO-COMUN.md), D-5 y D-9):
un documento de identidad inválido se rechaza; en una importación la party entra igualmente,
**marcada** con el error, hay un filtro para encontrar las marcadas y queda en el informe.

*Ejemplo:* de 400 clientes de Sage, 12 traen el NIF con la letra mal. Los 12 entran, cada uno
con la marca «the CIF does not pass its check digits» y el número tal como vino, y
`GET /api/parties?documentIssue=true` los lista para que alguien los corrija.

| Qué | Cómo |
|---|---|
| La marca | `documentIssue: { number, reason }` en la party. El número es el que vino, para quien corrige; **no identifica a nadie** |
| Ponerla | `PUT /api/parties/{id}/document-issue` con `reason` (obligatorio) y `number`. La pone la importación; también puede ponerla una persona |
| Buscar las marcadas | `GET /api/parties?documentIssue=true` |
| Quitarla | Se quita sola al añadir a la party un documento nacional, fiscal o de extranjero que pasa sus comprobaciones (un DNI, un NIF, un NIE). Un pasaporte o un «otro documento» no la quitan: no son el documento que estaba mal. También a mano, con `DELETE /api/parties/{id}/document-issue` |
| Permiso | `Parties.Party.Update`, y el ámbito de siempre |
| Esquema | Migración 18: `parties.doc_issue`, `doc_issue_number` y `doc_issue_reason` |

- **Lo que hace la importación** ([IMPORTACION.md](IMPORTACION.md), fuentes de Sage y de Apiscore):
  un documento español (DNI, NIF, CIF, NIE) con el dígito de control mal se sigue guardando como
  «otro documento», marca a la party, y añade un aviso al informe de la ejecución
  (`sage.wrong_document`, `apiscore.wrong_document`).
- **Confirmado por Javier el 2026-10-13, y por qué no es exactamente «no se guarda»:** el número erróneo se
  **conserva como «otro documento»** además de la marca. La importación encuentra a quien ya
  existe por su número de documento; si el número erróneo no se guardara, cada reimportación
  crearía otra vez al mismo cliente. «Otro documento» no se valida ni cuenta como identificación
  fiscal. Si se prefiere no guardarlo, hay que dar a la importación otra forma de reconocer a
  quien ya cargó.
- Un pasaporte o un documento extranjero también se guardan como «otro documento», pero **no
  marcan**: lo son por lo que son, no por estar mal. Tampoco marca un número que no está escrito
  como un documento español (un NIF francés, o uno con `ES` delante).
- **Quien ya estaba cargado también se marca.** Si la importación encuentra a alguien que ya
  existe y su registro trae un documento erróneo, lo marca si no lo estaba. Consecuencia: una
  marca quitada a mano vuelve en la siguiente importación mientras el documento no se corrija.
- **Lo que no hay:** la marca no bloquea nada (se puede facturar a una party marcada); eso sería
  una regla de Facturación.

## 11. La relación asigna los roles que falten

Decisión de Javier (D-49 de [DECISIONES-COMUNES.md](DECISIONES-COMUNES.md), confirmada el
2026-10-14). Antes, dar de alta «Talleres Pérez es cliente de Acme» se rechazaba con un 422 si
Talleres Pérez no tenía ya el rol de cliente. Ahora la relación se crea y Talleres Pérez recibe el
rol, con la misma fecha de inicio que la relación.

| Qué | Cómo |
|---|---|
| Dónde | `POST /api/party-relationships` y el alta de una party con afiliación. La regla del dominio no cambia (`Establish` sigue exigiendo los roles); el caso de uso los asigna antes |
| Qué rol se asigna | El que pide el tipo de relación para ese lado, si la party no juega ya ése o uno por debajo |
| Lo que se sigue rechazando (422) | Un rol que la party no puede jugar (una organización no pasa a ser empleada); una categoría, que no se asigna a nadie; **«Internal Organization»** o un rol que cuelgue de él; y cualquier rol de una party que quien llama **no puede escribir** (una compartida, una que sólo ve) |
| Auditoría y eventos | La asignación deja su entrada y publica `PartyRoleAssigned`, como una asignación a mano, en la misma transacción que la relación |
| Permiso | El de crear relaciones (`Parties.Relationship.Create`); no pide además el de asignar roles |

- **Por qué «Internal Organization» no se asigna así:** ese rol decide quién ve y quién escribe
  qué (las afiliaciones cuelgan de él). Si una relación lo diera, cualquiera con permiso para crear
  relaciones podría convertir a un tercero en empresa del grupo. Se sigue dando a propósito.
  Excepción confirmada por Javier el 2026-10-14.
- **Terminar la relación no quita el rol.** Quien fue cliente por una relación sigue teniendo el
  rol de cliente cuando la relación termina, hasta que alguien se lo cierre.
- **El rol es de la party, no de la pareja**, y lo ven todas las empresas que la ven. Por eso la
  relación sólo lo asigna a una party que quien llama podría modificar a mano. Para relacionarse
  con un banco compartido que no tiene el rol, un administrador global se lo da antes (revisión
  independiente del 2026-10-14).

## 12. Una persona puede ser proveedor, y organización interna

Decisión de Javier (D-53, confirmada el 2026-10-14): un autónomo puede ser proveedor, y una
persona puede ser organización interna y comportarse como cualquier empresa del grupo (emite
facturas, tiene plan contable y empleados).

| Qué | Cómo |
|---|---|
| Catálogo | «Supplier» e «Internal Organization» dejan la rama «Organization → Legal» y cuelgan de la raíz, como «Customer»: los puede jugar cualquier party |
| Esquema | Migración 20, de datos: cambia el padre de los dos tipos. Los identificadores y los códigos no cambian |
| Tipos que piden «una organización» | El «Organization Rollup» (una unidad bajo su organización) pide en el destino la categoría «Organization». Como una empresa del grupo ya no la hereda de su rol, esa categoría se cumple por **lo que la party es**: cualquier organización vale, juegue los roles que juegue |
| Los demás contextos | Fiscal, Contabilidad, Facturación, Recursos Humanos y el resto guardan a la empresa por su identificador y no miran si es persona u organización: no cambian |

- **Lo que una persona empresa no puede todavía:** tener unidades organizativas debajo (el
  «Organization Rollup» pide una organización), ni forma jurídica o nombre comercial, que son datos
  de organización. Su nombre es el de la persona.
- **Buscar por la categoría «Organization» o «Legal»** ya no devuelve a quien sólo juega
  «Supplier» o «Internal Organization»; para eso está el tipo de party (`kind`).
- **Sin decidir:** con qué documento se identifica un autónomo (su NIF de persona, D-53 en §2 del
  registro), y que la importación de proveedores cargue personas.

## 13. El catálogo común de tipos

Decisión de Javier (D-62, 2026-10-14): los cuatro proyectos siembran los mismos tipos de rol y de
relación, con los mismos identificadores y códigos; y «Team» cuelga de «Informal».

| Qué | Tipos | De dónde salen |
|---|---|---|
| Tipos de rol nuevos | «Club» (`…0001-…0038`, `club`) y «Group» (`…0001-…0039`, `group`), bajo «Informal» | Semilla de Trench |
| «Team» | Pasa de «Organization Unit» a «Informal» | D-62 |
| Tipos de relación nuevos | «Subsidiary» (`…0002-…0010`, `subsidiary`: organización interna → filial); «Legal Representation» (`…0011`, `legal-representation`: representante autorizado → organización interna); «Department Assignment» (`…0012`, `department-assignment`: empleado → departamento); «Collaborator Contact» (`…0013`, `collaborator-contact`: contacto → colaborador) | Semilla de Trench |
| Esquema | Migración 21, de datos: añade a una base ya sembrada los tipos que le faltan y mueve «Team». No toca los que ya están. Si alguien añadió por su cuenta un tipo con uno de estos códigos (`club`, `subsidiary`…), la migración se detiene y dice cuál: hay que darle otro código y migrar de nuevo |

Lo que **no** ha entrado, y por qué:

- ~~«Duplicate Of»~~: entró después. Su identificador en Trench, `…0002-…0016`, era aquí el de
  «Prospect Relationship». Javier decidió el 2026-10-14 (D-63) que se mueva el de Go: «Prospect
  Relationship» pasa a `…0017` y «Duplicate Of» ocupa `…0016` (Party → Party, código
  `duplicate-of`). La migración 23 copia el tipo al identificador nuevo, mueve sus relaciones y
  borra la fila antigua; la 21 hace lo mismo en una base que llegue a ella sin haberla pasado.
  Sólo se siembra: Go no tiene todavía fusión de parties.
- **«App User»**: en la semilla de Trench está comentado; no se siembra allí.
- **Caller, Callee, Sender, Recipient, Worker, Project Manager, Reporter, Owner, Resolver** y los de
  pedidos: en Trench no son tipos de rol *de party*, sino de un evento de comunicación, de un
  esfuerzo de trabajo, de un caso o de un pedido (otra clase de tipo de rol, en la misma tabla).
  Aquí `role_types` es sólo el catálogo de lo que una party puede ser; entrarán con el contexto
  que los use.
- Los tipos nuevos sólo se siembran: ningún caso de uso hace todavía nada especial con ellos
  (una «Department Assignment» no da ámbito, como sí hace en Trench).

## 14. Buscar por rol: sólo los vigentes, salvo que se pida

Decisión de Javier (D-48). `GET /api/parties?role=<tipo>` devuelve a quien juega ese rol (o uno
por debajo) **ahora**: empezó ya y no ha terminado. Con `includeEndedRoles=true` entran también los
roles terminados y los que aún no han empezado. El nombre y la definición son los de Trench
(Trench PR 34), para que el mismo front sirva a los dos.

- *Ejemplo:* Luis fue empleado hasta diciembre y hoy sólo es cliente. `?role=Employee` no lo
  devuelve; `?role=Employee&includeEndedRoles=true`, sí.
- El parámetro sólo afecta al filtro por rol; no cambia el de organización ni el de instalación.

## 15. No se desactiva a quien tiene relaciones sin terminar

Decisión de Javier (D-4): «borrar» es desactivar, y no se puede desactivar algo de lo que dependen
otras cosas vigentes; lo comprueba el comando antes de ejecutarse.

| Qué | Cómo |
|---|---|
| Dónde | `PUT /api/parties/{id}/active` con `active: false` |
| Qué bloquea | Cualquier relación de la party, en cualquiera de los dos lados, que no haya terminado: las vigentes y las que aún no han empezado |
| Respuesta | 422, `parties.has_relationships`, sólo a quien puede modificar la party (a quien sólo la ve, 403, sin decirle nada de sus relaciones). Primero se terminan las relaciones (`POST /api/party-relationships/{id}/terminate`) |
| Reactivar | No comprueba nada |

- *Ejemplo:* Talleres Pérez es cliente de Acme. Desactivarlo se rechaza; se termina la relación
  de cliente y entonces sí se puede.
- **Los roles no bloquean.** Son parte de la party (D-13: su ciclo de vida depende de ella) y se
  quedan como están; una party desactivada ya no cuenta para nada aunque conserve roles vigentes.
- **Lo de otros contextos no bloquea todavía:** una factura pendiente o un pedido abierto no lo
  sabe Parties. Queda como estaba propuesto: cada contexto escucha `PartyDeactivated` y avisa.
- **D-8, el comando «anular»:** hecho para los roles (§16); falta para lo demás.

## 16. Anular un rol dado por error

Decisión de Javier (D-8): un alta equivocada se quita con un comando «anular», que mira qué
depende del dato. Si nada depende, lo borra; si algo depende, lo deja marcado como anulado.

| Qué | Cómo |
|---|---|
| Ruta | `POST /api/parties/{id}/roles/{roleId}/annul`, sin cuerpo |
| Qué cuenta como «depende» | Una relación de la party, de cualquier fecha, en el lado que pide ese rol (o uno por encima de él) |
| Si nada depende | El rol **se borra**, como si nunca se hubiera dado |
| Si algo depende | El rol **termina en ese momento** y queda con `annulled: true` |
| Permiso | `Parties.PartyRole.Annul`, propio: la regla estándar de Seguridad no lo reparte, así que de entrada sólo lo tiene el administrador |
| Hacia fuera | Se publica `parties.party-role-ended.v1` en los dos casos: quien oyó que el rol se dio, oye que ya no se juega |
| Esquema | Migración 22: `party_roles.annulled` |

- *Ejemplo:* a Ana se le da «Proveedor» por error y se corrige al minuto: se borra. Si ya se
  hubiera creado su relación de proveedor con Acme, el rol queda terminado y anulado.
- **Un rol anulado no cuenta para nada, tampoco para los días que duró:** ni al buscar por rol
  (ni con `includeEndedRoles=true`), ni para establecer una relación con fecha pasada, ni impide
  volver a dar el mismo rol desde antes.
- **«Internal Organization» nunca se borra**, aunque nada dependa: queda terminado y anulado, para
  conservar que esa party fue empresa del grupo.
- **La relación que dependía del rol no se toca.** Sigue donde estaba; si también fue un error,
  se termina aparte. Anular relaciones, identificaciones o parties enteras no está hecho.
- Decidido aquí, sin respuesta de Javier a las dos preguntas abiertas de D-8: el permiso propio, y
  que «depende» son las relaciones.

## 17. El duplicado queda bloqueado

Decisión de Javier (D-64): una party que es duplicado de otra se puede seguir encontrando y
consultando, pero por norma se excluye al buscar y no puede intervenir en ninguna operación que no
sea de consulta. Sus relaciones se quedan donde están; fusionar (pasarlas a la buena y retirar una)
se verá caso por caso.

| Qué | Cómo |
|---|---|
| Marcar | Crear la relación «Duplicate Of» (`…0002-…0016`) del duplicado a la que sobrevive: `POST /api/party-relationships`. El duplicado queda con `duplicate: true` |
| Deshacer | Terminar esa relación |
| Buscar | `GET /api/parties` lo deja fuera; con `includeMergedParties=true`, entra (el nombre es el de Trench, su PR 45). Leerlo por su identificador funciona siempre |
| Lo que se rechaza (422, `parties.duplicate`) | Cualquier cambio en la party (nombre, roles, documentos, contactos, activar o desactivar…), crearle relaciones y cambiar los datos de las que tiene |
| Lo que se permite | Leerla y terminar sus relaciones, la de duplicado incluida |
| Esquema | Migración 24: `parties.duplicate` |

- **Quién marca y desmarca:** quien puede modificar el duplicado (§18); la superviviente sólo
  tiene que verse. Una empresa interna sólo la marca el administrador global. Desmarcar pide lo
  mismo, aunque la relación sea entre dos terceros.
- Un duplicado lo es de una sola party, y una party marcada no puede ser la superviviente de otra.
- **No es desactivar:** la party sigue activa y conserva sus relaciones, así que no choca con D-4.
- **Facturación y Pedidos también lo bloquean** (D-70): no se le hace un presupuesto ni un pedido,
  ni se confirma uno que ya tuviera; no se le prepara ni se le emite una factura ordinaria. Sí una
  rectificativa: corregir lo facturado es una obligación. Lo saben preguntando a Parties por el
  contrato de identidades fiscales, que lleva `blocked`. Los demás contextos (Compras,
  Cobros, Tesorería) no lo miran todavía.

## 18. Quién modifica a un tercero

Decisiones de Javier D-65 a D-67 (tomadas en la sesión de Karpo C# y confirmadas aquí el
2026-10-14). Antes, cualquier relación de un tercero con tu empresa te dejaba verlo **y
modificarlo**. Ahora:

| Caso | Quién lo modifica |
|---|---|
| Se dio de alta desde tu empresa (la party y su relación con tu empresa se crearon en la misma operación, según la auditoría) | Los usuarios de tu empresa con acceso completo |
| Ya existía y tu empresa sólo se relaciona con él | Sólo quien tenga además `Parties.Party.UpdateRelated`: de entrada el administrador de organización, no el usuario estándar; el administrador global se lo puede retirar |
| Es empresa interna de otros | Nadie de tu empresa, tenga el permiso que tenga. Sí sus propios usuarios, o quien reciba acceso a esa empresa |
| Compartida (catálogo común) | Sólo el administrador global, como antes |

- **Verlo no cambia:** cualquier relación con tu empresa te deja verlo, salvo las de familia: una
  relación de familia no hace visible a nadie (D-67, pensado para la persona que es empresa).
- **Cómo se sabe que la dio de alta tu empresa** (D-74): sin dato propio. La auditoría guarda,
  para cada cambio, el identificador de la operación en que se hizo. Si el primer registro de la
  party y el de la relación que la afilia a tu empresa comparten ese identificador, nacieron
  juntas. Un alta que llega sin identificador de operación recibe uno. Sin auditoría configurada,
  ninguna party cuenta como dada de alta por nadie: sólo modifica quien tenga el permiso.
- **Sin hacer de D-67:** la marca por campo con la que una persona autoriza qué datos suyos se ven
  como party pública. Necesita diseño.
- Dar de alta una party con una afiliación de familia o de duplicado se rechaza (400): no
  afiliarían a nadie y quien la crea no la vería.
- **Una salvedad:** «empresa interna» se mira por el tipo de rol exacto; un tipo de rol añadido
  colgando de «Internal Organization» no cuenta como tal.
- Esquema: nada nuevo.

