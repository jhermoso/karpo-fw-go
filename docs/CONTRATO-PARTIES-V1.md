# Contrato HTTP común de Parties, versión 1 (propuesta)

> **Estado: propuesta, sin aprobar ni revisar.** Escrita el 2026-10-13 en la sesión de Karpo Go.
> Javier eligió ese día que el contrato HTTP común sea **uno nuevo y limpio, escrito una vez**, que
> sirvan el back de C# y el de Go, empezando por lo que usan las pantallas principales
> ([CONTRATO-HTTP-PARTIES.md](CONTRATO-HTTP-PARTIES.md), opción C). Falta que lo revisen las
> sesiones de Karpo C# y de Trench, como se hizo con el contrato de extensibilidad, y que
> Javier conteste las cinco decisiones de §7.
>
> Las tablas de cada back pueden ser distintas (decisión D-1 del modelo común). Lo que este
> documento fija es lo que se ve por HTTP.

## 0. Lo esencial

1. **Catorce rutas del front de hoy se quedan en quince del contrato**, casi todas las que Go ya
   tiene, más una búsqueda nueva que ninguno de los dos tiene con esta forma.
2. **Una sola forma de lista, una sola forma de error y ninguna barra final.** Hoy el front
   acepta cuatro nombres para el total y lee el error de cuatro sitios.
3. **Lo que el front hace y ya no se hace** (borrar, estado y prioridad de la relación) no está
   en el contrato. Qué pone en esos botones es la decisión 2 de §7.
4. El contrato **no** cubre las demás pantallas de Parties (cuentas bancarias, comunicaciones,
   perfiles…). Van después, con el mismo método.

## 1. Convenciones

| Tema | Regla | Hoy en el front |
|---|---|---|
| Base | `/api`. Si la pasarela antepone `/v1`, lo quita ella | `/api/v1` casi siempre, `/api` a veces |
| Barra final | Nunca | El detalle se pide con barra final en un camino |
| Identificadores | UUID en texto, en minúsculas | Igual |
| Fechas | Día: `yyyy-MM-dd`. Instante: ISO 8601 en UTC, con `Z` | Sin fijar |
| Importes y porcentajes | Texto decimal (`"30.00"`), nunca número de coma flotante | — |
| Empresa activa | Cabecera `X-Organization-Scope`: identificadores separados por comas | Igual |
| Campos vacíos | Se omiten; una lista vacía es `[]`, nunca falta | Mezclado |
| Campos desconocidos al escribir | Se rechazan (400) | Se ignoran |
| Versión del recurso | Lo que se puede cambiar lleva `version`; cambiarlo con una versión vieja es 409 | Sólo en C# en parte |

### 1.1 Listas

Una sola forma, en todas las rutas que devuelven varias filas:

```json
{ "items": [ … ], "total": 1234, "nextCursor": "…" }
```

- `total` es el número de filas que cumplen el filtro, no las de la página.
- `nextCursor` sólo aparece si hay más filas y la petición usó cursor.

### 1.2 Paginación

En el cuerpo de las búsquedas, una de las dos:

```json
{ "page": { "number": 1, "size": 25 } }
{ "page": { "size": 25, "after": "<nextCursor de la respuesta anterior>" } }
```

`number` empieza en 1. `size` va de 1 a 500; por defecto, 25. Un número de página fuera de rango
devuelve una lista vacía, no un error. El cursor es opaco: el cliente no lo interpreta.

### 1.3 Orden

```json
{ "sort": { "by": "name", "ascending": true } }
```

Cada búsqueda dice por qué campos se puede ordenar. Pedir otro es un 400 que los enumera.

### 1.4 Errores

`application/problem+json` (RFC 9457: un JSON normalizado de error) en todas las rutas:

```json
{ "title": "Validation Failed", "status": 400, "detail": "One or more fields are invalid",
  "code": "parties.role_overlap", "correlationId": "…",
  "errors": [ { "field": "address.floor", "code": "length", "message": "too long" } ] }
```

| Estado | Cuándo |
|---|---|
| 400 | La petición está mal formada o un campo no vale. `errors` dice cuáles |
| 401 | No hay sesión válida |
| 403 | Se ve el recurso pero no se puede hacer eso |
| 404 | No existe **o está fuera del ámbito de quien llama**. No se distingue |
| 409 | La versión enviada ya no es la actual |
| 422 | La petición es correcta pero rompe una regla de negocio. `code` dice cuál; los códigos son parte del contrato |

El texto de `detail` y de `message` no es contrato: puede cambiar y traducirse. El front decide
por `status`, `code` y `errors[].field`.

## 2. La party

### 2.1 Forma

Una party es una persona o una organización. Lo que es además —cliente, proveedor, empresa del
grupo— son **roles**, no tipos.

```json
{ "id": "…", "kind": "organization", "name": "Talleres Pérez",
  "organization": { "legalName": "Talleres Pérez, S.L.", "tradeName": "Talleres Pérez", "legalForm": "corporation" },
  "active": true, "test": false, "shared": false,
  "organizations": ["<empresas del grupo a las que pertenece hoy>"],
  "roles":           [ { "id": "…", "roleType": "…", "code": "customer", "name": "Customer", "from": "…", "until": null, "active": true } ],
  "identifications": [ { "id": "…", "documentType": "…", "code": "TXID", "country": "ES", "number": "B12345674", "primary": true } ],
  "contacts":        [ { "id": "…", "kind": "postal", "address": { … }, "purposes": ["billing"], "active": true } ],
  "classifications": [ { "id": "…", "type": "…", "name": "Corporate", "active": true } ],
  "documentIssue": { "number": "B12345678", "reason": "…" },
  "version": 7 }
```

Una persona lleva `person: { givenName, firstSurname, secondSurname, gender, birthDate, maritalStatus }`
en lugar de `organization`. Es la forma que Go devuelve hoy; `code` en cada rol es nuevo.

| Campo del front de hoy | En el contrato |
|---|---|
| `partyType`: `Person`, `Organization`, `LegalOrganization`, `Corporation`, `InternalOrganization` | `kind` (dos valores), `organization.legalForm` y el rol `internal-organization`. Ver decisión 3 de §7 |
| `isActive`, `isTest` | `active`, `test` |
| `taxIdentity` | La identificación fiscal principal: `identifications[]` con `primary`, y en la búsqueda, `taxId` |
| `rolesDisplay`, `relationshipsDisplay`, `displayName` | No vienen: el front ya los calcula él |

### 2.2 Rutas

| # | Ruta | Qué hace | Sustituye en el front de hoy a |
|---|---|---|---|
| 1 | `POST /api/parties/search` | Buscar (§3) | `POST /parties/search/enriched` y `…/cursor`, y los `…/search` de personas, organizaciones, clientes, empleados |
| 2 | `POST /api/parties/search/stats` | Recuentos (§3.4) | `POST /parties/search/stats` |
| 3 | `GET /api/parties/{id}` | La party entera (§2.1) | `GET /parties/{id}/aggregate`, con y sin barra, y `GET /catalogs/party-classifications/by-party/{id}` |
| 4 | `POST /api/persons` · `POST /api/organizations` | Alta, con su relación con una empresa del grupo en la misma petición | `POST /persons`, `/organizations`, `/customers/quick-create` |
| 5 | `PUT /api/parties/{id}/name` | Cambiar el nombre: `{ givenName, firstSurname, secondSurname }` o `{ legalName, tradeName }` | `POST /parties/rename` `{id, name}` |
| 6 | `PUT /api/parties/{id}/active` | `{ "active": false }` | `POST /parties/toggle` `{id, enable}` |
| 7 | `PUT /api/parties/{id}/test` | `{ "test": true }` | `PUT /parties/{id}/test-flag` |
| 8 | `POST /api/parties/{id}/roles` | Asignar un rol: `{ roleType, from? }` | `POST /party-roles` |
| 9 | `POST /api/parties/{id}/roles/{roleId}/end` | Terminarlo: `{ at? }` | `PUT /party-roles/{id}` con fecha de fin, y `DELETE /party-roles/{id}` |
| 10 | `GET /api/parties/{id}/relationships?active=true` | Sus relaciones, en los dos sentidos | Las listas hijas de roles y relaciones del detalle |
| 11 | `POST /api/party-relationships` | Establecer una relación | Igual |
| 12 | `POST /api/party-relationships/{id}/terminate` | Terminarla | `DELETE /party-relationships/{id}` |
| 13 | `GET /api/catalogs/party-role-types` · `…/party-relationship-types` · `…/party-classifications` · `…/document-types?country=ES` | Catálogos | Los dos primeros, igual; los otros, `GET /catalogs/party-types` y los tipos de documento escritos a mano en el front |
| 14 | `GET /api/internal-organizations` | Las empresas del grupo que ve quien llama | Igual |
| 14 bis | `GET /api/internal-organizations/names` | Identificador y nombre de todas las empresas del grupo de la instalación (D-101) | Sólo en Go por ahora |
| 15 | `GET /api/reference/countries` | Países | `GET /catalogs/geographic-boundaries?type=Country` |

Todas menos la 1, la 2 y la 7 existen ya en Go con esta forma.

## 3. La búsqueda

Es la ruta de la que viven las siete pantallas principales.

### 3.1 Petición

```json
{ "filter": { "kinds": ["person"], "text": "ana", "active": true,
              "rolesAny": ["customer"], "organizations": ["…"] },
  "page": { "number": 1, "size": 25 },
  "sort": { "by": "name", "ascending": true },
  "include": ["relationships"] }
```

| Filtro | Tipo | Encuentra | Sustituye a |
|---|---|---|---|
| `text` | texto | Nombre que contiene el texto, sin distinguir mayúsculas | `nameContains` |
| `kinds` | lista de `person`, `organization` | De alguna de esas clases | `partyTypes` |
| `legalForms` | lista | Organizaciones con alguna de esas formas jurídicas | `partyTypes` (`LegalOrganization`, `Corporation`) |
| `active` | sí/no | Activas o inactivas. Si falta, todas | `isActive` |
| `test` | sí/no | De pruebas o no | `isTest` |
| `rolesAny` | lista de **códigos** de tipo de rol | Juegan hoy alguno de esos roles, o uno por debajo en la jerarquía | `roleTypeIdsAny` |
| `rolesAll` | lista de códigos | Juegan hoy todos | `roleTypeIdsAll` |
| `classificationsAny` | lista de identificadores | Clasificadas hoy con alguna | `classificationTypeIdsAny` |
| `organizations` | lista de identificadores | Pertenecen hoy a alguna de esas empresas del grupo | `internalOrganizationIds` |
| `document` | texto | Tienen ese número de documento | `identificationValue` |
| `documentCountry` | código ISO de dos letras | Tienen un documento de ese país | `issuingCountryBoundaryId` |
| `documentIssue` | sí/no | Marcadas con un documento erróneo | (nuevo) |
| `bornOn` | `{ from?, to? }` | Personas nacidas en ese intervalo | `birthDate` |

- Varios filtros se combinan con «y». Un filtro desconocido es un 400.
- Los roles se piden por **código** (`customer`), no por identificador: es lo que hace portable una
  pantalla entre instalaciones. Go ya tiene los códigos; C# tendría que dárselos (decisión D-7 del
  contrato de extensibilidad de Trench).
- **No está** «sólo documentos duplicados» (`onlyDuplicatedIdentifications`): el modelo común dice
  que un documento identifica a una sola party, así que no puede haber duplicados que buscar.

### 3.1 bis Filtrar por roles con «y», «o» y «no»: la expresión de Trench

> Añadido el 2026-10-13, después de escribir lo anterior. Trench tiene en su rama principal un
> formato para esto, `FilterExpressionDto`, y Javier ha pedido a los demás proyectos que lo tomen
> como ejemplo. Conviene que sea **idéntico** en los tres, así que este contrato lo adopta en
> lugar de inventar otro. Lo he leído en su código (`main`, `e6d157c9`); no lo he ejecutado.

`rolesAny` y `rolesAll` sólo saben decir «alguno» o «todos». La expresión dice cualquier
combinación, y viaja en `filter.expression`:

```json
{ "version": 1,
  "root": { "op": "or", "args": [
      { "op": "and", "args": [
          { "op": "leaf", "kind": "roleType", "value": "10000000-0000-0000-0001-000000000002" },
          { "op": "leaf", "kind": "roleType", "value": "10000000-0000-0000-0001-000000000003" } ] },
      { "op": "leaf", "kind": "roleType", "value": "10000000-0000-0000-0001-000000000010" } ] } }
```

*Se lee:* «(cliente **y** proveedor) **o** empleado».

| Regla | Valor |
|---|---|
| Nodos | `and` y `or` (uno o más hijos en `args`), `not` (exactamente uno) y `leaf` |
| Hoja de hoy | `kind: "roleType"`, con el identificador del tipo de rol en `value` |
| Límites | Profundidad 8 y 16 hojas; más es un 400 |
| Hoja desconocida | 400. Un servidor nunca la ignora |
| Versión | Añadir un tipo de hoja no la sube; cambiar la forma del árbol, sí |

Con esto, `rolesAny` y `rolesAll` de §3.1 sobran: se quedan como atajo sólo si alguien los pide.

Dos cosas por aclarar con Trench antes de darlo por igual en los tres:

1. **¿«Tiene el rol X» incluye los roles que están por debajo de X?** En Go sí: quien es «cliente
   de facturación» es «cliente». No he leído qué hace Trench.
2. **El valor es el identificador del tipo de rol, no su código.** Para los tipos del programa da
   igual, porque los identificadores son los mismos en los tres. Para un tipo que añada un
   administrador, el identificador es de esa instalación. Una hoja `roleTypeCode` lo haría
   portable; es añadir un tipo de hoja, que el formato ya prevé.

### 3.2 Orden

`name` (por defecto), `birthDate`, `createdAt`. Nada más en la versión 1: ordenar por una columna
calculada (número de roles, país) obliga a los dos backs a calcularla igual.

### 3.3 Respuesta

Cada fila es un **resumen**, no la party entera:

```json
{ "items": [
    { "id": "…", "kind": "person", "name": "Ana García López", "active": true, "test": false,
      "birthDate": "1990-04-12", "taxId": "12345678Z", "country": "ES",
      "roles": [ { "code": "customer", "name": "Customer" } ], "roleCount": 1,
      "relationships": [ { "typeCode": "customer", "typeName": "Customer Relationship",
                           "otherParty": "…", "otherPartyName": "Acme", "direction": "from" } ],
      "relationshipCount": 1,
      "organizations": ["…"], "documentIssue": false } ],
  "total": 1 }
```

| Campo | Regla |
|---|---|
| `roles` | Los que juega hoy, por nombre; como mucho diez, y `roleCount` dice cuántos son |
| `relationships` | Sólo si se pide en `include`; las vigentes, como mucho cinco, y `relationshipCount` dice cuántas son |
| `taxId` | El número de su identificación fiscal principal, si tiene |
| `country` | El país de su dirección postal vigente (la de facturación primero), si tiene |

### 3.4 Recuentos

`POST /api/parties/search/stats` con `{ "filter": { … }, "groupBy": ["kind", "active"] }` devuelve

```json
{ "total": 1234, "groups": { "kind": { "person": 900, "organization": 334 }, "active": { "true": 1200, "false": 34 } } }
```

Se agrupa por `kind`, `active`, `test`, `legalForm` y `role` (código). Es obligatoria en el
contrato; hoy el front tolera que falte.

## 4. Lo que no está en el contrato, a propósito

| Qué hace el front hoy | Por qué no está |
|---|---|
| Borrar una persona, una organización, un cliente, un rol o una relación | Decidido: se termina o se desactiva, y los errores de alta se anulan (D-4, D-8). El comando de anular está sin diseñar; cuando lo esté, entra |
| Estado y prioridad de una relación, y sus catálogos | Decidido: fuera del modelo (D-11) |
| Editar un rol entero con `PUT` | Un rol sólo tiene tipo y periodo: se asigna y se termina |
| `GET /parties/{id}/positions` | Los puestos son de RRHH; irán en su contrato |
| «Clientes» y «empleados» como recursos (`/customers`, `/employees`) | Son parties con un rol: `rolesAny: ["customer"]`. El empleo, con sus fechas, es de RRHH |
| Validar el formato de un documento sin guardarlo (`…/validate-format`) | Lo usa el registro público. Va con el contrato de Security |
| Exportar | Tiene su propio contexto y su contrato |

## 5. Qué tendría que hacer cada uno

| Quién | Qué | Tamaño (estimación) |
|---|---|---|
| **Go** | La búsqueda con filtros de varios valores, orden, cursor y fila resumida; los recuentos; la ruta de «de pruebas»; `code` en los roles de la party; filtrar por código de rol | Medio |
| **Karpo C# y Trench** | Las quince rutas junto a las suyas (no quitan nada), con la forma de party de §2.1, la lista y el error únicos, y códigos en los tipos de rol | Medio. La forma de la party es lo delicado: hoy la persona y la organización salen de clases distintas |
| **Front** | Siete configuraciones de pantalla; que el motor genérico lea campos anidados (`person.birthDate`), mande el filtro y la página con esta forma y lea el error de un solo sitio; y lo que se decida de los botones de borrar | Medio |

Una **batería de pruebas de contrato** —peticiones HTTP y respuestas esperadas, sin saber qué back
hay detrás— es lo que dice si los dos lo cumplen. La de Go existe en parte
(`contexts/parties/parties_test.go`); habría que sacarla a un formato que también pueda ejecutarse
contra C#.

## 6. Cómo se ha llegado a cada elección

| Elección | Por qué | Alternativa |
|---|---|---|
| Partir de las rutas de Go | Trece de quince ya existen así y expresan intención (`…/end`, `…/terminate`) en vez de un `PUT` genérico | Partir de las del front: hereda `rename`/`toggle` con el identificador en el cuerpo |
| Hijos bajo la party (`/parties/{id}/roles`) | Un rol no existe sin su party, y el ámbito se comprueba una vez | Recursos de primer nivel (`/party-roles`), como hoy el front |
| Una búsqueda para todas las pantallas | Persona, cliente o empleado son filtros, no recursos | Una búsqueda por pantalla, como hoy C# |
| Filtros con nombre propio, no por columna | Cada filtro es una regla que los dos backs implementan igual | Filtro genérico por cualquier campo: cada back lo traduciría a su esquema de forma distinta |
| Página o cursor, a elegir | El front usa los dos hoy | Sólo cursor: más simple de servir, obliga a cambiar el modo «ir a la página N» |
| Roles por código en el filtro | Portabilidad entre instalaciones | Por identificador, como hoy |

## 7. Decisiones que tocan a Javier

1. **¿Vale partir de la forma de Go** (§2, §6), o prefieres conservar las rutas que el front usa
   hoy y limpiar sólo listas y errores? Lo segundo deja el front casi intacto y obliga a Go a
   servir recursos que no tiene (`/customers`, `/party-roles`).
2. **Los botones de borrar.** Propongo que el front los cambie por «desactivar» en la party y
   «terminar» en roles y relaciones, y que «eliminar» vuelva, sólo para errores de alta, cuando
   exista el comando de anular.
3. **Los tipos de party.** Propongo `kind` con dos valores, forma jurídica y roles (§2.1). La
   alternativa es conservar los cinco tipos del front, que mezclan qué es (persona u organización),
   qué forma tiene (sociedad) y qué papel juega (empresa del grupo).
4. **La forma del error** (§1.4). Propongo la de Go, que es el estándar RFC 9457. El contrato de
   extensibilidad de Trench dejó esto pendiente de esta decisión.
5. **¿Página y cursor, o sólo cursor?** Propongo los dos.

## 7 bis. Roces con lo que Trench acaba de publicar (2026-10-13)

La sesión de Trench leyó este contrato y señaló tres puntos donde responde distinto que lo que
entró ese día en su rama principal («Relaciones de clientes / proveedores»), que Javier ha pedido
tomar como ejemplo. Los tres van a Javier; aquí queda cómo están.

| # | Roce | Estado |
|---|---|---|
| 1 | **Filtro de roles**: este contrato tenía dos listas; Trench, un árbol «y» / «o» / «no» | **Resuelto**: el contrato adopta su expresión tal cual (§3.1 bis) |
| 2 | **Clientes y proveedores**: aquí son parties con un rol; en Trench la pantalla es **una fila por pareja parte–empresa**, con los datos del perfil comercial de la pareja (código, número, límite, bloqueo) y un alta de la relación en un solo paso | **Abierto.** No se contradicen: son dos vistas. A este contrato le falta la segunda: una búsqueda de **relaciones** por tipo, con los datos de las dos partes y los de la pareja. Encaja con lo ya decidido (límite y número de cliente son de la pareja). En Go esos datos están repartidos: el número, en Parties; el límite y el bloqueo, en `receivables`; el perfil de proveedor, en `purchases` |
| 3 | **Documentos duplicados**: aquí «un documento identifica a una sola party», y por eso no hay filtro de duplicados. En los datos reales de Trench **sí hay documentos repetidos** entre parties activas, existe la fusión de parties, y Javier ha pedido allí un filtro de duplicados | **Abierto, y es de modelo, no de rutas.** En Go la regla es de la fase 2 (aprobada el 2026-09-27) y la sostiene un índice único: una importación de esos datos rechazaría el documento del segundo. Hay que decidir si la regla se mantiene, y entonces los duplicados se resuelven antes de entrar, o si se admite el duplicado marcado y se añade la fusión |

Decisiones que esto añade a las de §7:

6. **¿Entra en la versión 1 la vista por pareja** (roce 2), o va en una segunda tanda?
7. **¿Un documento puede estar en dos parties?** (roce 3). Si sí, cambia una regla aprobada de Go y
   hace falta la fusión de duplicados; si no, la importación de datos reales necesita un paso
   previo de limpieza.

## 7 ter. Revisión de Karpo C# (2026-10-13)

La sesión de Karpo C# revisó el contrato contra su código (`main`, `6c36c40f`) y contra el front,
sólo leyendo. Su detalle, con fichero y línea, está en su repositorio:
`000-Docs/design/Karpo_Respuesta_Contrato_Parties_V1.md`. **Esta sección manda sobre las
anteriores donde difieran.** Nada de esto está aprobado por Javier.

### Lo que se cambia en el contrato (aceptado aquí; es técnico)

| # | Cambio | Por qué |
|---|---|---|
| 1 | **La fila de búsqueda no lleva relaciones con nombre.** `relationships` pasa a ser `[{ typeCode, typeName, count }]`; las relaciones con el nombre de la otra parte van en el detalle y en la ruta 10 | Una empresa del grupo tiene miles de relaciones; C# quitó ese dato de la fila en agosto por eso |
| 2 | **Se puede ordenar también por `active`** (§3.2) | El front ordena hoy por ese campo |
| 3 | **Faltaban las rutas para escribir lo que §2.1 devuelve.** Entran: `POST /api/parties/{id}/identifications` y `DELETE …/{identId}`; `POST /api/parties/{id}/contacts`, `PUT …/{contactId}/purposes` y `POST …/{contactId}/end`; `POST /api/parties/{id}/classifications` y `POST …/{classId}/end` | Existen ya en Go con esa forma; el contrato las había dejado fuera sin decirlo |
| 4 | **`organization.legalForm` tiene seis valores:** `corporation`, `government-agency`, `non-profit`, `partnership`, `sole-proprietorship`, `team` | C# pidió la lista |
| 5 | **`taxId` es la identificación fiscal principal**, no la principal a secas; `country` es el código ISO de la dirección postal vigente | En C# hoy salen de otros sitios y `country` es un nombre en inglés |
| 6 | **Roles y relaciones de la fila y del filtro son los vigentes hoy** | En C# hoy no se filtra por vigencia; es el mismo roce que con Trench |
| 7 | **El contrato tiene que decir permisos y ámbito** (ver abajo) | No decía nada |

### Permisos y ámbito (sección nueva, propuesta por C#)

- Cada ruta exige una acción con nombre. Leer: `Parties.Party.Read`, `Parties.Relationship.Read`.
  Crear: `Parties.Party.Create`. Cambiar: `Parties.Party.Update`. Roles:
  `Parties.PartyRole.Assign`. Relaciones: `Parties.Relationship.Create`, `…Terminate`,
  `…Update`. Fusionar (marcar «Duplicate Of» o deshacerlo): `Parties.Party.Merge`, y poder cambiar
  las dos parties (D-95, D-96). (Son los nombres de Go; C# propone los suyos y hay que casarlos:
  allí es `party.merge`.)
- La cabecera `X-Organization-Scope` **nunca amplía** lo que quien llama puede ver; sólo lo
  reduce.
- Fuera de ámbito, **404**; dentro pero sin poder escribir, **403**. Escribir exige acceso
  completo.
- El alta con su relación comprueba la empresa antes de crear, y es todo o nada.
- **Quién ve a una party** se escribe una sola vez, para los dos backs, y **la regla es la de
  D-15**: la visibilidad no depende de la vigencia de la relación, sino de que la party esté
  activada. C# había propuesto aquí «relación vigente» y **lo ha retirado** (2026-10-13). D-15
  tiene tres preguntas sin contestar (`DECISIONES-COMUNES.md` §2); hasta entonces la regla no se
  puede escribir entera. Ninguno de los tres la cumple hoy.

### Lo que C# no puede cumplir todavía

| Qué | Detalle |
|---|---|
| **Los datos de persona y organización** | C# guarda un solo nombre de 120 caracteres: no tiene nombre y apellidos por separado, estado civil, nombre comercial ni forma jurídica. Sin un cambio de esquema previo (el paso P-30/P-31 de su estudio), cumpliría devolviendo vacíos |
| **Códigos de tipo de rol y de relación** | No los tiene |
| **Terminar una relación** | Sólo tiene borrar |
| **`version`, rechazo de campos desconocidos, `code`, `correlationId` y `errors` como lista** | No por HTTP hoy |
| **422 para una regla de negocio** | Devuelve 409. Lo cambiaría sólo en las rutas nuevas |

### Lo que cambia mi inventario del front

- **Son unas 85 plantillas de ruta, no «unas 50»**, contando como yo las 41 configuraciones. La
  conclusión se mantiene: las siete pantallas viven de una docena.
- **`POST /customers/quick-create` no existe en el back de C#**, ni las rutas para borrar
  personas, organizaciones o clientes: parte de lo que el contrato quita ya no funciona hoy.
- **Cambios recientes que el inventario no conocía:** el alta de participante va ya a
  `/persons` u `/organizations` con `{ name }`, y en organizaciones **el cliente genera el
  identificador**.
- **El front construye la petición de búsqueda en seis sitios y traduce la fila en cuatro.** Hay
  que juntarlos antes de adaptarlo. Tamaño en el front: medio-grande, no medio.
- **No se adapta sin rediseñar:** editar con `PUT {ruta}/{id}`, el nombre en un solo campo, el
  alta de rol y de relación desde las pestañas, «añadir relación» (su formulario exige estado y
  prioridad) y los recuentos por número de roles o de relaciones.
- Configuraciones a tocar: **ocho** (las siete pantallas y la de pedidos).

### Decisiones que esto añade para Javier

8. **Prefijo de convivencia.** En C#, diez de estas rutas chocan en método y plantilla con las
   actuales, con otra forma, y `/api/v1` no sirve porque todo lo recorta a `/api`. C# propone un
   segmento propio, por ejemplo `/api/c1/…`, fijado en el contrato. A Go le da igual servir un
   prefijo u otro.
9. **¿El cliente puede mandar el identificador al crear?** El front ya lo hace en organizaciones.
   C# recomienda que sí. En Go hoy lo genera el servidor.
10. **La regulación de la corporación** (fuente, vigencia, descripción) está en C# y no en §2.1.
    ¿Entra?
11. **Hacer antes en C# el cambio de esquema de persona y organización**, para que la forma de
    §2.1 no salga vacía.
12. **409 o 422** para una regla de negocio rota. Go usa 422; C#, 409.

## 7 quater. Lo que cambian las decisiones del 13 y 14 de octubre

Decisiones de Javier posteriores a este contrato (números del registro,
`DECISIONES-COMUNES.md`). El cuerpo del documento **no está reescrito todavía**; esta sección dice
qué cambia y qué falta para poder reescribirlo.

| Decisión | Qué cambia en el contrato | Estado en Go |
|---|---|---|
| D-48 | La búsqueda lleva `includeEndedRoles` (booleano, hermano de `expression`). Ausente o `false`: sólo cuentan los roles vigentes. Afecta al filtro plano por rol y a la hoja `roleType` de la expresión | Hecho en `GET /api/parties`; la expresión no está implementada |
| D-52 | La interfaz no ofrece paréntesis: construye siempre una «o» de grupos «y». El árbol de §3.1 bis no cambia | Nada que hacer en el back |
| D-49 | Crear una relación ya no devuelve 422 porque a un lado le falte el rol: se le asigna. Se sigue rechazando para «Internal Organization», para un rol que la party no puede jugar y para una party que quien llama no puede escribir | Hecho |
| D-51 | La vista por pareja («Relaciones de clientes», «Relaciones de proveedores») **entra en el contrato común**. Cierra la decisión 6 de §7 bis. Faltan sus rutas y su fila | Sin hacer: Go no tiene búsqueda de relaciones |
| D-53 | Una persona puede ser proveedor y organización interna: el selector de empresa y la lista de proveedores no pueden dar por hecho `kind: organization` | Hecho en el catálogo |
| D-4 | `PUT /api/parties/{id}/active` con `false` devuelve 422 (`parties.has_relationships`) si la party tiene relaciones sin terminar. El front debe ofrecer terminarlas, no un «Eliminar» | Hecho |
| D-8 | Ruta nueva `POST /api/parties/{id}/roles/{roleId}/annul`; el rol lleva `annulled` | Hecho para roles |
| D-62 | El catálogo de tipos es el mismo en todos: los desplegables de rol y de relación pueden fiarse de los códigos (`customer`, `supplier`, `team`…) | Hecho, salvo «Duplicate Of» |
| D-85 | Ruta nueva `PUT /api/parties/{id}/roles/{roleId}/self-employed` con `{tradeName, activityCode, registeredOn}`; el rol lleva `selfEmployed`. Añadir un documento que no es de la clase de la ficha responde 422 (`parties.document_not_for_kind`) | Hecho (`PARTIES.md` §19) |

**Lo que sigue impidiendo reescribir la búsqueda** (§3): la primera pregunta de Theros (¿Talleres
Pérez, que compra a Acme y a Acme Levante, sale una vez o dos en «Clientes»?) y las tres de D-15
sobre visibilidad. Con D-51 decidida, la respuesta natural es «dos, en la vista por pareja; una, en
la de parties», pero no la ha dado Javier.

## 7 quinquies. La vista por pareja (borrador, D-51)

Javier quiere que «Relaciones de clientes» y «Relaciones de proveedores» sean comunes a todos.
Este borrador parte de lo que Trench sirve hoy (su sesión me pasó la forma exacta el
2026-10-14) y lo ajusta a las convenciones de este contrato (§1). **No lo ha revisado nadie.**

*Qué es:* una fila por cada pareja empresa–tercero. Talleres Pérez, que compra a Acme y a Acme
Levante, sale **dos veces**, cada una con su número de cliente, su límite y su bloqueo.

### Rutas

Dos familias con el mismo molde, una por tipo de relación: `customer-relationships` y
`supplier-relationships`.

| Ruta | Qué hace |
|---|---|
| `POST /api/customer-relationships/search` | Lista de parejas, paginada. Cuerpo: `filter`, `sort`, `page` como en §3 |
| `GET /api/customer-relationships/{id}` | Una pareja |
| `POST /api/customer-relationships` | Alta de la pareja, todo o nada |

Las fichas fusionadas (duplicados, D-64) quedan fuera salvo `includeMergedParties=true`, igual que en
la búsqueda de parties.

Trench las tiene hoy como `…/search/enriched` y añade `…/search/top-role-types` (los roles más
usados, para los botones); aquí el recuento va por §3.4.

### Filtro

El mismo de la búsqueda de parties (§3.1: texto, tipo, documento, roles, `expression`,
`includeEndedRoles`), **aplicado al tercero**, más `internalOrganizationIds`, aplicado a la
relación. Sólo relaciones vigentes, salvo que se pida otra cosa (por decidir el nombre del campo).

### Fila

| Campo | Qué es |
|---|---|
| `id` | El de la relación |
| `partyId`, `name`, `kind`, `taxId`, `country`, `active`, `test` | El tercero, como en la fila resumida de §3.3 |
| `roles` | Sus roles vigentes, si se piden |
| `internalOrganizationId`, `internalOrganizationName` | La empresa del grupo |
| Sólo cliente | `customerNumber`, `externalCode` (el código en la contabilidad de origen), `creditLimit` (nulo: sin límite), `blockOrders`, `blockDelivery`, `blockReason` |
| Sólo proveedor | `externalCode`, `branch`, `blockOrders`, `blockReceipts`, `blockReason` |

Nombres de Trench que cambian aquí, por coherencia con §2.1: `partyType` → `kind`,
`taxIdentity` → `taxId`, `isActive` → `active`, `isTest` → `test`, `maxRisk` → `creditLimit`,
`customerCodeInTenant` → `externalCode`. Son propuestas: si es más barato que Go adopte los de
Trench, se hace al revés.

### Alta

`internalOrganizationId` (obligatoria) y, o bien `partyId` de un tercero que existe, o bien los
datos de uno nuevo (tipo, nombre y, si se quiere, documento). El cliente admite además los datos
de la pareja.

| Regla | Respuesta |
|---|---|
| La relación asigna al tercero el rol que le falte (D-49) | — |
| Nunca asigna «Internal Organization»: la empresa tiene que serlo ya | 422 (Trench: 400) |
| La empresa no existe o está fuera del ámbito (se comprueba antes, para no revelar si es interna) | 404 |
| La pareja ya tiene una relación vigente de ese tipo | 422 con el identificador de la existente (Trench: 409) |
| El documento ya es de otra party (D-50) | 422 (Trench: 409) |

Los códigos distintos son la decisión 12 de §7 ter (409 o 422), todavía abierta.

### Lo que hay en Go (2026-10-17)

Hecho, en el anfitrión (`host/pairs.go`), la lectura: `POST /api/customer-relationships/search`,
`GET /api/customer-relationships/{id}` y lo mismo para `supplier-relationships`. El alta no está
hecha: hoy una pareja se crea con el alta de la party con su afiliación o con
`POST /api/party-relationships`, y sus datos en Cobros, Pedidos y Compras con sus rutas.

| Qué | En Go |
|---|---|
| Filtro | `text`, `kinds`, `document`, `role` (identificador), `includeEndedRoles`, `includeMergedParties`, `active`, `internalOrganizationIds`, `includeEnded` (relaciones terminadas), `customerNumber`, `supplierNumber` (D-122), `blocked`. Uno desconocido, 400 |
| Orden | `name` (por defecto), `customerNumber`, `supplierNumber`, `creditLimit`, `blocked`, `internalOrganization`, `since`. Otro, 400 que los enumera |
| Fila común | `id` (la relación), `partyId`, `name`, `kind`, `taxId`, `country`, `active`, `test`, `roles` (nombres de los vigentes), `internalOrganizationId`, `internalOrganizationName`, `since`, `until` |
| De cliente | `customerNumber` (Parties); `creditLimit` (Cobros; nulo: sin límite); `blockOrders` (bloqueo por crédito de Cobros o de pedidos de Pedidos); `blockDelivery` (Pedidos); `sageCode` y `apiscoreCode` (Parties, D-29); `blockReason` (D-122: los motivos de los bloqueos en vigor de Cobros y de Pedidos que quien llama puede leer, separados por «; ») |
| De proveedor | `supplierNumber`, `sageCode` y `apiscoreCode` (Parties, D-122 y D-29); `blockInvoices` (Compras: no se registran facturas nuevas) y su `blockReason` |
| Sin datos | `branch` no existe en Go; `externalCode` es `sageCode` (desde D-122, 2026-10-30) |
| Permisos | Leer relaciones de Parties. Lo que quien llama no puede leer de Cobros, Pedidos o Compras queda sin rellenar, sin error |

Diferencias con el borrador de arriba: el filtro por rol va por identificador y no por código
todavía; el proveedor lleva `blockInvoices` en lugar de `blockOrders` y `blockReceipts`, que en Go
no existen.

### Lo que esto pide a Go

Los datos de la pareja viven en tres contextos: el número de cliente en Parties, el límite y el
bloqueo en `receivables`, el perfil de proveedor en `purchases`. La fila es una **lectura
compuesta** que arma el anfitrión, como ya hace con otras; el contrato no tiene por qué notarlo.

**Decidido por Javier (D-73, 2026-10-14):** en la versión 1 se filtra y se ordena también por los
campos de la pareja (número de cliente, bloqueo). En Go eso pide una lectura compuesta de Parties,
Cobros y Compras, o una proyección de lectura que junte la pareja; está sin implementar.

## 8. Límites

- Nadie de C# ni de Trench ha revisado esto todavía.
- No he comprobado que C# pueda servir la fila resumida de §3.3 con sus datos de hoy (por ejemplo,
  `country` y `taxId` salen en C# de otra parte).
- `GET /api/internal-organizations` existe en los dos con la misma ruta; no he comparado la forma.
- El contrato sale del inventario de [CONTRATO-HTTP-PARTIES.md](CONTRATO-HTTP-PARTIES.md), que
  comprobé sólo en parte.
- Los tamaños de §5 son estimaciones mías.
