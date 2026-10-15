# Parties: por qué aquí son 14 tablas y en C# 109

> Escrito el 2026-10-11 para Javier. Contesta cuatro preguntas: de dónde sale la diferencia,
> si se puede igualar la funcionalidad, si un mismo front puede hablar con los dos backs, y qué
> debería querer decir «misma estructura». **No decide nada**: las opciones y su coste están en §5.
>
> Continúa [PARTIES.md](PARTIES.md) y [PARTIES-UDM.md](PARTIES-UDM.md). Se apoya en el estudio de
> convergencia de Karpo (`000-Docs/design/Karpo_Estudio_Convergencia_Modelos.md`), cuyas cifras
> he vuelto a contar aquí sobre el modelo actual.

## 0. Lo esencial

1. **La herencia (una tabla por subclase) explica 23 de las 95 tablas de diferencia: la cuarta
   parte.** Es real, pero no es la causa principal.
2. **La causa principal es la frontera, no el diseño: 48 tablas (la mitad) son cosas que en C#
   viven dentro del esquema `parties` y en Go viven en otro contexto** (geografía, instalaciones,
   RRHH, nóminas, fiscal, cobros, compras, tesorería). No faltan: están en otro sitio.
3. **14 tablas son funcionalidad que Go no tiene** (comunicaciones, casos, características,
   cuentas bancarias de terceros, estado y prioridad de la relación).
4. **11 tablas son rediseño de verdad**, y casi todo es una sola decisión: los contactos (10
   tablas en C#, 1 en Go).
5. **Sí se puede alcanzar la misma funcionalidad** en los dos sentidos. Lo que Go corrigió se
   lleva a C# casi todo sin tocar tablas (son reglas). Lo que a Go le falta es trabajo aditivo.
6. **Sí puede el mismo front hablar con los dos**, y para eso las tablas dan igual. Lo que tiene
   que coincidir es el contrato HTTP, y hoy no coincide en casi nada: 34 rutas en Go frente a 464
   en C#, con otros nombres de campo, otra paginación y otra forma de anidar.

## 1. De dónde sale cada tabla de la diferencia

### 1.1 Qué he contado y cómo

| Lado | Fuente | Tablas |
|---|---|---|
| Go | `contexts/parties/infrastructure/schema.go` (14 migraciones) | **14** de negocio, más 4 técnicas (dos bandejas de salida, auditoría, bandeja de entrada) |
| Karpo C# | `…ErpDetail.Parties.Infrastructure.Migrations.Npgsql/Migrations/ErpDetailDbContextModelSnapshot.cs`, commit `8c8f7244` | **109** en el esquema `parties`, más 6 en `transversal` |
| Trench | `050DBs/subdomain/postgres/*.sql` (cifra del análisis de partida, §3.3; no la he recontado) | Las 115 de Karpo y **9 más** (colaboradores, capacidades, módulos) |

El «modelo snapshot» es el fichero donde Entity Framework (la biblioteca con la que C# habla con
la base) anota la forma exacta de cada tabla; es la fuente más fiable del lado de C#.

Trench y Karpo C# son **el mismo esquema** con seis semanas de separación: mismas tablas,
mismos nombres, mismas claves. Todo lo que sigue sobre C# vale para los dos.

### 1.2 Las 109 tablas de C#, por bloques

| Bloque | Tablas en C# | En Go |
|---|---:|---|
| **A. Terreno común**: party, roles, relaciones, identificaciones, contactos, clasificaciones | 44 | 12 de las 14 tablas de `parties` |
| **B. Comunicaciones, casos, características, cuentas bancarias de terceros** | 14 | Nada |
| **C. Geografía, instalaciones y datos de referencia** | 20 | 2 en `parties` (roles de instalación); el resto en los contextos `geography` (13 tablas) y `facilities` (2) |
| **D. Una tabla que no es de Parties** (`work_effort_role_type`) | 1 | Nada (es de Trabajos) |
| **E. Extensiones de ErpDetail**: contrato laboral, convenio, perfiles fiscales y comerciales, modelos AEAT, tarifas… | 30 | En `hr`, `payroll`, `fiscal`, `receivables`, `purchases`, `treasury`; una parte, en ningún sitio (§3.2) |
| **Total** | **109** | |

### 1.3 Las 95 tablas de diferencia, por causa

109 − 14 = 95. Cada tabla de C# está en una sola fila.

| Causa | Tablas | % | Ejemplos |
|---|---:|---:|---|
| **Frontera de contexto**: en Go existe, en otro contexto | 48 | 50 % | `geographic_boundary`, `postal_code`, `currency` → `geography`. `facility` → `facilities`. `labor_contract`, `collective_agreement` → `hr`. `vat_group`, `official_form_submission` → `fiscal`. `customer_relationship_commercial_profile` → `receivables` |
| **Herencia, una tabla por subclase** | 23 | 24 % | `person`, `organization`, `legal_organization`, `corporation`; `customer`, `supplier`, `employee`…; `customer_relationship`, `stockholding`… |
| **Funcionalidad que Go no tiene** | 14 | 15 % | `communication_event` y 5 más, `case` y 2 más, `party_characteristic`, `party_bank_account`, `status_type`, `priority_type` |
| **Rediseño** | 11 | 12 % | Contactos: 10 tablas → 1. Jerarquía de roles: tabla → columna. Catálogo de clasificación: 3 tablas → 2 |
| Tabla que Go tiene y C# no (`party_affiliations`) | −1 | | |
| **Total** | **95** | | |

Dicho de otro modo: si mañana C# sacara de su esquema `parties` todo lo que no es de Parties
(bloques C, D y E), quedarían 58 tablas frente a 14, y la comparación justa es **44 frente a 12**
en el terreno que los dos cubren.

### 1.4 La herencia: qué es, cuánto pesa y qué guarda

Hay dos formas corrientes de guardar una jerarquía de clases:

- **Una tabla por tipo (TPT).** Una tabla para la clase base y otra por cada subclase, unidas por
  la misma clave. *Ejemplo:* una sociedad anónima es una fila en `party`, otra en `organization`,
  otra en `legal_organization` y otra en `corporation`. Leerla son cuatro tablas unidas.
- **Una tabla para toda la jerarquía (TPH).** Una sola tabla con una columna que dice de qué
  subclase es cada fila, y las columnas de las subclases vacías donde no tocan. *Ejemplo:* en C#
  un teléfono y una dirección postal están los dos en `contact_mechanism`, con una columna
  `Discriminator` que vale `TelephoneContact` o `PostalAddress`.

**C# usa las dos**, y eso es lo primero que hay que saber:

| Jerarquía en C# | Forma | Subclases | Tablas que añade |
|---|---|---:|---:|
| `Party` → `Person`, `Organization` → `LegalOrganization` → `Corporation` | Una tabla por tipo | 4 | 4 |
| `PartyRole` → `Customer`, `Supplier`, `Employee`, `InternalOrganization`, `FinancialInstitution`, `Carrier`, `Shareholder` y tres de cliente | Una tabla por tipo | 10 | 10 |
| `PartyRelationship` → `CustomerRelationship`, `SupplierRelationship`, `OrganizationRollup`, `Partnership`, `Stockholding` | Una tabla por tipo | 5 | 5 |
| `RoleType` → `PartyRoleType`, `CaseRoleType`, `CommunicationEventRoleType`, `WorkEffortRoleType` | Una tabla por tipo | 4 | 4 |
| `ContactMechanism`, `CommunicationEvent`, `Facility`, `GeographicBoundary`, `PartyClassification`, `StatusType` | Una tabla para todas | 29 | 0 |

Las 23 tablas de subclase guardan muy poco. **Doce de las 23 tienen una sola columna, `Id`**: no
guardan ningún dato, sólo dicen «esta fila es de esta subclase». Así está escrita una de ellas en
el script de Trench (`050DBs/subdomain/postgres/02-parties-schema.sql:827`):

```sql
CREATE TABLE IF NOT EXISTS "parties"."bill_to_customer" (
    "Id" UUID NOT NULL,
    CONSTRAINT "PK_bill_to_customer" PRIMARY KEY ("Id"),
    CONSTRAINT "fk_bill_to_customer_party_role" FOREIGN KEY ("Id") REFERENCES "parties"."party_role"("Id") ON DELETE CASCADE
);
```

Las otras once suman **28 columnas de datos entre todas**:

| Tabla | Columnas propias |
|---|---|
| `person` | `BirthDate`, `PersonGender` |
| `organization` | (ninguna) |
| `legal_organization` | `TaxIdentity` |
| `corporation` | `Description`, `EffectiveFrom`, `ExpirationDate`, `RegulationSource` |
| `customer` | `CustomerNumber`, `CreditLimit`, `BlockReason` |
| `employee` | `EmployeeNumber`, `HireDate`, `TerminationDate`, `JobCategoryCode`, `WageGroup` |
| `financial_institution` | `IsSignificant` |
| `supplier`, `internal_organization`, `carrier`, `shareholder`, `bill_to_customer`, `ship_to_customer`, `end_user_customer` | (ninguna) |
| `customer_relationship` | `CustomerSegment`, `CreditLimit`, `LifetimeValue` |
| `supplier_relationship` | `CommercialTerms`, `SupplierRating` |
| `organization_rollup` | `ThruDate`, `HierarchyLevel`, `ControlPercentage`, `IsActiveForReporting` |
| `partnership` | `PartnershipType`, `ParticipationPercentage` |
| `stockholding` | `Percentage` |
| `party_role_type`, `case_role_type`, `communication_event_role_type`, `work_effort_role_type` | (ninguna) |

**Qué hizo Go con eso.** No «usa TPH en vez de TPT»: en Go no hay herencia. Hizo tres cosas
distintas según el caso:

- **Persona y organización: un tipo con variantes.** Una tabla `parties` con una columna `kind`
  (`person` u `organization`) y los datos de cada una en columnas que quedan vacías para la otra.
  La forma jurídica (sociedad, asociación, administración…) deja de ser subclase y pasa a ser un
  valor (`legal_form`). Además Go guarda lo que C# declara y no guarda: nombre y dos apellidos y
  estado civil (`schema.go:46-49`).
- **Subtipos de rol y de relación: filas de catálogo.** «Cliente» no es una clase ni una tabla:
  es una fila de `role_types`. Asignar el rol es una fila en `party_roles` con el identificador
  de ese tipo. Añadir un tipo de rol nuevo es añadir una fila, no una tabla ni una clase. Es lo
  que la pregunta llamaba «filas con un tipo de catálogo en vez de una tabla por tipo de rol», y
  es correcto.
- **Los datos que llevaban esas subclases: cada uno a su dueño.** El límite de crédito está en
  cobros, por pareja empresa–cliente (un cliente puede tener un límite distinto con cada empresa
  del grupo; en C# sólo uno). El alta y la baja del empleado, en RRHH. El porcentaje de
  participación, que en C# está en tres sitios (`party_relationship`, `partnership` y
  `stockholding`), es una columna de `party_relationships` (`share_percent`). El razonamiento,
  dato a dato, está en [PARTIES-UDM.md](PARTIES-UDM.md) §3 y §5.

**Franqueza sobre este punto:** la herencia por tabla no es en sí un error. Es la traducción
literal del libro de modelos de datos del que sale Parties (UDM), donde cada subtipo es una
entidad. Lo que sí es un coste medible es que la mitad de esas tablas no guardan nada, y que el
propio C# tuvo un fallo de inserción al retirar la columna discriminadora de `party` (7 de
septiembre; `PARTIES.md` §3).

### 1.5 El rediseño de verdad: 11 tablas

| Qué | C# | Go | Qué se gana y qué se pierde |
|---|---|---|---|
| **Contactos** | 10 tablas: `contact_mechanism`, `contact_mechanism_type`, `contact_mechanism_purpose_type`, `contact_mechanism_link`, `electronic_address`, `telecommunications_number`, `party_contact_mechanism`, `party_contact_mechanism_purpose`, `postal_address_boundary`, `valid_contact_mechanism_role` | 1 tabla: `party_contacts`, con la dirección dentro y los propósitos en una columna | Se gana sencillez y que el propósito apunte al contacto (en C# no apunta). **Se pierde el contacto compartido**: en C# un domicilio puede ser de dos personas y se corrige una vez; en Go son dos copias. Es la pregunta CV-7, sin contestar |
| **Jerarquía de roles** | `role_type_hierarchy` (un rol puede tener varios padres) | `role_types.parent_id` (un padre) | Una sola fuente de verdad. Se pierde poder dar dos padres a un rol; hoy nadie lo usa |
| **Catálogo de clasificación** | `party_type` (mezcla subclases, formas jurídicas, visibilidad y familias) + `person_classification` | `classification_types` con familia, aplicabilidad y exclusividad | Reglas que C# no tiene (una hoja vigente por familia exclusiva) |

### 1.6 Lo que no es diferencia de modelo: nombres y claves

«Ningún nombre de columna en común» es cierto y no significa nada sobre el modelo: es convención.

| | C# | Go |
|---|---|---|
| Tablas | Singular: `party_role` | Plural: `party_roles` |
| Columnas | `PartyFromId`, `InitDate`, `ExpirationDate` | `from_party`, `valid_from`, `valid_to` |
| Versión | `row_version integer` | `version bigint` |
| Clave de los hijos | `Id` propio | Compuesta: `(party_id, id)` |
| País | `IssuingCountryId uuid` (apunta a geografía) | `country` de dos letras (ISO) |

De esta lista, **sólo la clave compuesta y el país son decisiones**; el resto lo decide quien
genera el código (en Theros, el «pack»: el juego de plantillas de cada lenguaje). Cambiarlo en un
lado no cambia ni una regla.

## 2. El mismo dato en los dos lados: tres ejemplos

**«Talleres Pérez, S.L.» es cliente de Acme desde enero, con 10.000 € de crédito.**

| | C# | Go |
|---|---|---|
| La organización | 4 filas: `party`, `organization`, `legal_organization`, `corporation` | 1 fila en `parties` (`kind='organization'`, `legal_form='corporation'`) |
| Que es cliente | 2 filas: `party_role` y `customer` | 1 fila en `party_roles` |
| La relación con Acme | 2 filas: `party_relationship` y `customer_relationship` | 1 fila en `party_relationships` y 1 en `party_affiliations` |
| El límite de crédito | En `customer.CreditLimit`, **y** en `customer_relationship.CreditLimit`, **y** como riesgo máximo en `customer_relationship_commercial_profile.MaxRisk` | 1 fila en el contexto `receivables`, por pareja Acme–Talleres Pérez |
| **Total** | 8 filas en 8 tablas más el perfil, con el límite en tres sitios | 4 filas en 4 tablas de `parties` y 1 en `receivables` |

**El teléfono de facturación de Ana.**

| C# | Go |
|---|---|
| `contact_mechanism` (el teléfono, con `Discriminator`), `party_contact_mechanism` (Ana lo usa), `party_contact_mechanism_purpose` (para facturación; enlazado por Ana y el mecanismo, no por la fila anterior) | 1 fila en `party_contacts` con `kind='phone'`, `contact_value='+34…'`, `purposes='billing'` |

**Que Ana pueda ver a Talleres Pérez** (porque Ana trabaja en Acme y Talleres Pérez es cliente de
Acme).

| C# | Go |
|---|---|
| No se guarda: se calcula en cada consulta recorriendo relaciones | Se guarda: `party_affiliations` dice a qué organización interna pertenece cada party, y se mantiene en la misma transacción que la relación |

## 3. ¿Se puede alcanzar la misma funcionalidad?

Sí, en los dos sentidos. Nada de lo que hace uno es imposible en el otro; lo que cambia es el
precio. Los tamaños son **estimaciones mías, no mediciones**: «pequeño» es un día de sesión con
sus pruebas, «medio» varios días, «grande» semanas.

### 3.1 Lo que Go hace y C# no (lo que Go corrigió)

De las 22 diferencias que inventarió el estudio de convergencia (§3), 13 se llevan a C# sin
romper nada, y 9 de esas 13 sin tocar una sola tabla.

| Qué | Qué pasa hoy en C# | Llevarlo a C# | Tamaño |
|---|---|---|---|
| Un rol no se asigna dos veces a la vez | La ruta que usa el front guardaba el rol sin pasar por la party | **Hecho** (P-10, 7 de octubre) | — |
| Una relación exige partes distintas que jueguen el rol de su lado | Partes falsas y una regla que siempre decía «sí» | **Hecho** en la ruta principal (P-11) | — |
| Jerarquía de roles con una sola fuente; jerarquía organizativa por vigencia | Dos fuentes que se contradecían | **Hecho** (P-12, P-13) | — |
| Familias de clasificación exclusivas; alta rápida de cliente atómica | No hay regla; el alta puede quedar a medias | Sólo código (P-14, P-15) | Pequeño |
| Datos de la persona guardados (dos apellidos, estado civil) | La clase los tiene y no se guardan; el nombre vale «JOHN DOE» por defecto | Columnas nuevas en `person` | Pequeño |
| Terminar una relación | No se puede: sólo borrar | Una ruta nueva; la columna ya existe | Pequeño |
| Documento de identidad: rechazar el inválido, autoridad emisora, índice único | Lo guarda marcado «formato inválido»; sin índice único | Una columna y un índice (antes hay que buscar duplicados). Rechazar o marcar es la pregunta CV-4 | Pequeño |
| **Afiliaciones** (visibilidad guardada) | Se calcula en cada consulta, en 95 ficheros | Tabla nueva, relleno inicial y cambiar el filtro | Medio |
| 18 eventos publicados a otros contextos | La bandeja de salida existe y nadie la consume | Sólo código | Medio |
| Permiso en cada caso de uso | En el endpoint, y muchos no tienen ninguno | Cambia el comportamiento: quien hoy entra sin permiso recibirá un 403 | Medio |
| **Lo que rompe**: una tabla para party, contactos como hijos, subtipos de rol y de relación como catálogo, geografía fuera | — | Rompe esquema, rutas y front: 5 + 10 + 10 + 5 tablas, ~80 rutas, ~65 configuraciones del front. Cada cambio de esquema en C# toca 9 proyectos de migraciones | **Grande**; el estudio propone no hacerlo en el C# escrito a mano |

### 3.2 Lo que C# hace y Go no

| Qué | Tamaño en C# | Llevarlo a Go | Tamaño |
|---|---|---|---|
| **Cuentas bancarias de terceros** | 1 tabla, 8 rutas | Hijo de `Party`, con el IBAN validado como en `treasury` | Pequeño |
| **Consentimiento de protección de datos** | 1 tabla | Hijo de `Party` con su evento | Pequeño |
| **Registro mercantil** (tomo, folio, hoja) | 1 tabla, 5 rutas | Hijo de `Party` | Pequeño |
| **Número de cliente** | Columna en `customer` y otra en el perfil comercial | Detalle de la relación de cliente (ya decidido en PU-2; sin hacer) | Pequeño |
| **Estado y prioridad de la relación**, título del cargo, administrador único | 2 catálogos, 4 columnas, 10 rutas | Se decidió **no** hacerlo (PU-8): el estado se deriva de las fechas. Si se quiere: dos catálogos y dos columnas | Pequeño |
| **Características** de la party | 2 tablas, 8 rutas, **ningún tipo sembrado** | O se porta, o se retira de C# (CV-8) | Pequeño |
| **Mantenimiento de catálogos por HTTP** (crear un tipo de rol o de relación desde la aplicación) | ~50 rutas | En Go los catálogos se siembran y sólo se leen. Hacerlos editables es una decisión (CV-9), porque hay reglas colgando de ellos | Medio |
| **Lecturas para la pantalla**: búsqueda con filtros por campo, estadísticas, vista agregada, exportación directa, búsqueda de relaciones | ~10 rutas de las que depende el front | Go tiene una búsqueda con filtros fijos y la lectura por identificador | Medio |
| **Borrar** (party, rol, relación, contacto) | En casi todos los recursos | Go termina, no borra; sólo se borran identificaciones. Es la pregunta CV-10 | Pequeño si se decide |
| **Contacto compartido** entre parties | El modelo entero de contactos | Cambiar el modelo de contactos de Go (CV-7) | Medio |
| **Comunicaciones y casos** | 11 tablas, ~53 rutas | Contexto nuevo de interacciones (CV-14); no es de Parties | Grande |
| **Ventas y precios** (tarifas, zonas, rutas, comisionistas, grupos de precio) | 7 agregados, ~36 rutas | Contexto nuevo (CV-14); no es de Parties | Grande |
| **Callejero** (vías y tramos del INE) e **IAE** | 3 tablas, 2,4 millones de filas | `geography` y `fiscal` | Medio |
| Lo que Trench tiene y Karpo no: fusión de duplicados (`MergedIntoPartyId`), discapacidad y personas a cargo, idioma preferido, planta y puerta en la dirección, colaboradores (4 tablas) | ~45 columnas y 9 tablas | Primero tiene que decidirse si entra en Karpo C# | Sin estimar |

**Lectura.** A Go le faltan, de Parties propiamente dicho, seis piezas pequeñas y dos medias. Lo
grande (interacciones, ventas y precios) no es de Parties en ninguno de los dos diseños: en C#
está en el esquema `parties` por historia.

## 4. ¿Puede el mismo front hablar con los dos?

**Sí.** Un front sólo ve HTTP: no sabe cuántas tablas hay detrás, ni en qué lenguaje está
escrito el servidor. El motor de Angular de Karpo lo confirma: no tiene ninguna vista escrita a
mano para Parties; se dirige por ficheros de configuración (`public/config/entities/**`), y cada
uno fija cuatro cosas: la ruta (`"apiEndpoint": "/persons"`), los nombres de los campos
(`"field": "isActive"`), los filtros y la clave del hijo.

**Pero hoy no puede**, porque el contrato HTTP de Go es otro. No es un problema de tablas.

### 4.1 Qué hay que igualar

| Qué | Front de Karpo + C# | Go hoy | ¿Coincide? |
|---|---|---|---|
| **Rutas** | Recursos de primer nivel: `/persons`, `/organizations`, `/party-roles`, `/party-relationships`, `/party-identifications`, `/customers`, `/employees`… 464 rutas | Hijos anidados bajo la party: `POST /api/parties/{id}/roles`, `…/contacts`, `…/identifications`. 34 rutas | Sólo `/persons`, `/organizations`, `/parties`, `/party-relationships` y `/internal-organizations`, y no en todos los verbos |
| **Verbos** | `PUT` y `DELETE` genéricos por recurso; borrado en bloque | Una ruta por intención: `PUT …/name`, `PUT …/person`, `POST …/end`, `POST …/terminate`. Casi nada se borra | No |
| **Nombres de campo** | Planos: `isActive`, `birthDate`, `partyType`, `rolesDisplay` | Anidados por variante: `active`, `person.birthDate`, `kind`, `roles[]` | No |
| **Tipo de party** | `'Person' \| 'Organization' \| 'LegalOrganization' \| 'Corporation'` | `kind: person \| organization` más `legalForm` | No |
| **Paginación, petición** | `?page=&pageSize=&sortBy=&sortOrder=&search=`; además `POST /search` con `{ pagination: { pageNumber, pageSize, orderBy } }` | `?page=&size=&q=` y filtros fijos (`kind`, `role`, `document`, `classification`, `organization`) | Sólo `page` |
| **Paginación, respuesta** | El front acepta `data`, `items` o `results`, y `total`, `totalItems` o `totalCount` | `{ items, total, number, size, totalPages }` | **Sí, de casualidad**: el front es tolerante |
| **Errores** | El front enseña `error.message` del cuerpo | RFC 9457 («problem details»: un JSON normalizado de error con `title`, `status`, `detail`, `code` y la lista de campos) | No he comprobado qué manda C#; el front no lee `detail` |
| **Lecturas auxiliares** | `GET /{id}/aggregate`, `POST /search/stats`, `GET /count`, `GET /export`, `$expand` | Ninguna | No |
| **Identificadores de catálogo** | GUID fijos copiados en `well-known-role-types.ts` | **Los mismos GUID** | **Sí** |
| **Entrada** | JWT y contexto de autorización | JWT y `GET /api/auth/context` | Parecido; sin comprobar campo a campo |
| **Semántica** | Borrar; guardar un documento inválido marcado; contacto compartido | Terminar; rechazarlo; contacto propio | No, y esto no lo arregla un cambio de nombres |

### 4.2 Qué no hace falta igualar

- Tablas, columnas, claves, índices y motor de base de datos.
- Lenguaje, capas internas, fronteras de agregado (que el rol sea hijo de la party o agregado
  propio no se ve desde fuera si la ruta es la misma).
- En qué contexto vive cada dato, siempre que la ruta que lo sirve sea la misma.
- Eventos de integración: los consumen otros servicios, no el front.

### 4.3 Tres formas de conseguirlo

| Forma | Qué es | Coste | Riesgo |
|---|---|---|---|
| **(1) Go imita a C#** | Una capa de rutas en Go que hable como C# (`/party-roles`, campos planos, `POST /search`) | Grande: son cientos de rutas, y varias exigen funcionalidad que Go no tiene o decidió no tener (borrar, catálogos editables, subtipos como recursos: `/customers`) | Copia a Go la forma que Go corrigió |
| **(2) C# añade las rutas de Go** (CV-11, paso P-36) | C# sirve las 34 rutas de Go **junto a** las suyas. El front sigue con las antiguas mientras quiera | Medio en C#. Aditivo: no rompe nada | Dos contratos vivos en C# durante un tiempo |
| **(3) El front cambia de contrato** | Las ~65 configuraciones de Parties pasan a apuntar al contrato nuevo, y el motor aprende campos anidados y rutas por intención | Medio-grande en el front, y sólo tiene sentido después de (2) | El front de Karpo lleva pruebas en rojo sin vigilar |

Una vez hecho (2), **una misma batería de pruebas HTTP mide a los dos backs** (manda peticiones y
comprueba respuestas, sin saber qué servidor hay detrás). Es lo que convierte «son equivalentes»
en un número.

## 5. Qué puede querer decir «misma estructura»

Los `CLAUDE.md` nuevos dicen que «la estructura de las bases de datos debe mantenerse igual a la
de Trench y Karpo C# siempre que sea posible». El estudio de convergencia (CV-1) propone lo
contrario, y Karpo Go se hizo con esquema propio a sabiendas. **Los dos textos no pueden ser
ciertos a la vez**; uno de los dos hay que corregirlo.

### Opción A — Mismas tablas

Go y C# con el mismo esquema, tabla a tabla.

- **A1, Go adopta las tablas de C#.** Coste: reescribir `parties`, `geography` y `facilities` de
  Go y deshacer decisiones aprobadas el 27 de septiembre y el 4 de octubre. Go heredaría el
  límite de crédito en tres sitios, las doce tablas sin datos y la visibilidad calculada. Contradice
  «lo que Go corrigió debería llevarse a C#».
- **A2, C# adopta las tablas de Go.** Coste: la fase 4 del estudio. Unas 30 tablas rehechas, ~80
  rutas, ~65 configuraciones del front, 9 proyectos de migraciones por cambio, y migrar los datos
  de las bases de SQL Server y Oracle, que ya no coinciden con su propio modelo.
- **Qué se gana:** un mismo script de datos sirve a los dos; se pueden comparar bases con una
  herramienta.
- **Qué no se gana:** no hace falta para compartir front (§4) ni para compartir modelo.

### Opción B — Mismo modelo, mismas reglas, mismas rutas

Lo que coincide es: qué agregados hay y qué partes tienen, qué reglas cumplen, qué rutas HTTP
sirven y con qué forma, y qué eventos publican. Cómo se guarda lo decide cada implementación.

- **Coste:** terminar la fase 1 del estudio en C# (reglas; va por la mitad), la fase 3 (aditiva:
  afiliaciones, datos de persona, terminar relación, eventos, y las rutas de Go junto a las
  suyas), y en Go las piezas de §3.2. Nada de esto rompe a nadie.
- **Qué se gana:** un mismo front, una misma batería de pruebas, y el modelo agnóstico que
  necesita Theros: «`Party` tiene dos variantes» es una línea de modelo, y que se pinte en una
  tabla o en cinco lo decide la plantilla de cada lenguaje.
- **Qué no se gana:** las bases siguen siendo distintas. Un volcado de una no entra en la otra.

### Opción C — B ahora, y las mismas tablas sólo en lo generado

Es B, más una regla para el futuro: cuando Theros genere C#, su plantilla pinta las tablas con la
forma de Go (una tabla para party, catálogo de tipos, contactos como hijos). El C# escrito a mano
no se toca; se sustituye contexto a contexto cuando el generado pase la misma batería.

- **Coste añadido sobre B:** una migración de datos por contexto el día que se sustituya, y
  mantener en Theros una sola forma de persistencia en vez de una por lenguaje (que es menos
  trabajo, no más).
- **Qué se gana:** al final hay mismas tablas, sin haber reescrito nunca el C# a mano.
- **Riesgo:** durante la transición conviven en C# dos formas de tabla.

### Comparación

| | A1 | A2 | B | C |
|---|---|---|---|---|
| Mismo front | Sí | Sí | Sí | Sí |
| Misma batería de pruebas HTTP | Sí | Sí | Sí | Sí |
| Modelo agnóstico del lenguaje | No lo exige ni lo da | No lo exige ni lo da | Sí | Sí |
| Lo que Go corrigió llega a C# | No: se pierde | Sí | Las reglas sí; la estructura no | Sí, al generar |
| Mismas tablas | Sí | Sí | No | Al final |
| Rompe lo que hay | Go entero | Esquema, rutas y front de C# | Nada | Nada hasta sustituir |
| Tamaño | Grande | Muy grande | Medio | Medio, más una migración por contexto |

### Mi recomendación

**C**, que es B con un destino. Las razones, para que se puedan discutir:

1. Las tablas no son lo que comparten un front ni un modelo; son lo que comparten dos programas
   que escriben en la misma base, y eso no está previsto.
2. A1 contradice lo que Javier ya ha dicho. A2 es el camino más caro para llegar a donde C llega
   sin romper.
3. El 75 % de la diferencia de tablas (frontera de contexto y herencia) lo resuelve una
   plantilla, no una decisión de negocio.

Lo que **no** es de plantilla y hay que decidir de verdad, porque cambia lo que el usuario puede
hacer, son cuatro preguntas del estudio que siguen abiertas: contacto compartido o propio (CV-7),
rechazar o marcar un documento inválido (CV-4), borrar o terminar (CV-10) y catálogos editables o
sembrados (CV-9).

## 6. Decisiones que tocan a Javier

1. **Qué significa «misma estructura»** (opciones A1, A2, B o C de §5), y con ello qué frase se
   corrige: la de los `CLAUDE.md` o la del estudio.
2. **Cuál es el contrato HTTP común** para el front: el de C# de hoy (forma 1 de §4.3) o el de Go
   añadido a C# (formas 2 y 3).
3. Las cuatro preguntas de comportamiento de arriba (CV-4, CV-7, CV-9, CV-10) pueden esperar a
   que se conteste la 1, pero ninguna plantilla las va a contestar.

## 7. Límites de este documento

- No me he conectado a ninguna base. Todo sale de ficheros: `schema.go`, el modelo de Entity
  Framework de PostgreSQL y un script de Trench.
- Del lado de C# sólo he leído el modelo de PostgreSQL. El análisis de partida avisa de que las
  bases de SQL Server y Oracle tienen más columnas que su modelo.
- Las tablas de Trench (9 más que Karpo) y sus ~45 columnas de más las tomo del análisis de
  partida sin recontarlas.
- El reparto de las 30 extensiones de ErpDetail entre contextos de Go es el del estudio de
  convergencia (§4.2); no he comprobado tabla a tabla que cada una tenga su equivalente.
- No he comprobado qué cuerpo de error devuelve C#, ni la entrada (JWT) campo a campo.
- Los tamaños de §3 y §5 son estimaciones.

## 8. Cómo comprobar las cifras

Desde la raíz de este repositorio:

```bash
grep -c 'CREATE TABLE' contexts/parties/infrastructure/schema.go
```

Debe salir `14`.

```bash
grep -c 'ToTable("[a-z0-9_]*", "parties")' /c/Git/Paranoia/Karpo/020-Back/020-Source/Paranoia.Karpo.ErpDetail.Parties.Infrastructure.Migrations.Npgsql/Migrations/ErpDetailDbContextModelSnapshot.cs
```

Debe salir `109`.

```bash
grep -c 'HasBaseType' /c/Git/Paranoia/Karpo/020-Back/020-Source/Paranoia.Karpo.ErpDetail.Parties.Infrastructure.Migrations.Npgsql/Migrations/ErpDetailDbContextModelSnapshot.cs
```

Debe salir `52`: las 23 subclases con tabla propia más las 29 que comparten tabla.
