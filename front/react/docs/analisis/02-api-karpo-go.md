# Análisis 02: la API HTTP de Karpo Go que consume el front

Fecha: 2026-11-27. Rama `claude/front-react-7b8564`. Todo lo de este documento se ha comprobado
arrancando `cmd/karpo` de verdad sobre un SQLite temporal y haciendo peticiones con `curl`; lo que
no se probó se dice. El proceso se paró al terminar (puerto 18741, base en el directorio temporal).

## 1. Arranque de `cmd/karpo` con SQLite

`cmd/karpo` es el anfitrión de desarrollo: los 25 contextos en un proceso, SQLite, mensajes entre
contextos en memoria y tareas periódicas con temporizador. Código: `cmd/karpo/main.go`,
`host/serve/serve.go`, `host/host.go`.

### Orden exacta

1. **Compilar** (21 s la primera vez): `go build -o karpo.exe ./cmd/karpo` (o `go run ./cmd/karpo`).
2. **Variables de entorno** (sin fichero de configuración):

   | Variable | Obligatoria | Qué es |
   |---|---|---|
   | `KARPO_SQLITE` | sí | ruta del fichero SQLite (se crea). En Windows, con barras normales |
   | `KARPO_JWT_SECRET` | sí | secreto que firma las sesiones, 32 caracteres o más |
   | `KARPO_ADDR` | no (`:8080`) | dónde escucha; conviene `127.0.0.1:<puerto>` |
   | `KARPO_BOOTSTRAP_ADMIN_USER` / `KARPO_BOOTSTRAP_ADMIN_PASSWORD` | no | primer administrador, sólo se crea mientras nadie administra |
   | `KARPO_EXPORTS_DIR` | no | por omisión, `exports` junto al fichero SQLite |
   | `KARPO_DELIVER_EVERY`, `KARPO_CHORES_EVERY` | no | `2s` y `1m` |
   | `KARPO_ALERT_WEBHOOK` | no | aviso de mensajes imposibles |

3. **`karpo migrate`**: aplica el esquema de los 25 contextos. Resultado real: 25 contextos,
   167 migraciones. Incluye la semilla de geografía (8.507 delimitaciones, 248 países,
   14.608 códigos postales), catálogos de Parties, RRHH, Instalaciones, divisas...
4. **`karpo serve`** (es el comando por omisión): **se niega a arrancar sobre un esquema sin
   migrar** y lista las migraciones pendientes. Si migra bien, registra los 168 permisos en
   Seguridad, completa el catálogo de módulos y crea el administrador de arranque.
5. Sondas: `GET /healthz` y `GET /readyz` (`{"status":"UP","checks":{"database":"OK"}}`), sin
   autenticación.

Ejemplo (bash; probado):

```bash
export KARPO_SQLITE="C:/tmp/karpo/karpo.db" KARPO_JWT_SECRET="un-secreto-de-al-menos-32-caracteres!!" \
       KARPO_ADDR="127.0.0.1:18741" KARPO_BOOTSTRAP_ADMIN_USER=admin KARPO_BOOTSTRAP_ADMIN_PASSWORD='<contraseña con política>'
./karpo.exe migrate && ./karpo.exe serve
```

Notas de entorno: usar un puerto fuera de los rangos reservados de Windows
(`netsh interface ipv4 show excludedportrange protocol=tcp`; el 18741 estaba libre).

### Datos de ejemplo

**No hay semillas de demostración** más allá de los catálogos y la geografía. Tras migrar hay:
catálogos de tipos de rol, de relación, de instalación, de puesto, de estado de puesto
(estructurales); 248 países ISO con 8.507 delimitaciones; ninguna empresa, instalación, puesto,
producto ni festivo. La demostración del front necesita un **sembrador propio** que cree datos por
HTTP (lo que se hizo a mano en este análisis, §7). Existe `host/smoke` (publicadora de humo,
`docs/PUBLICADORA-DE-HUMO.md`) que no se ha usado aquí; podría servir de base.

### Autenticación

- JWT HS256 (emisor `erp-security`, audiencia `erp-api`), **15 minutos** de vida (`exp - iat = 900 s`).
  El token lleva sólo quién es (`sub`, `username`, `partyId`); los permisos se resuelven en cada
  petición, sin caché.
- Rutas públicas (sin `Authorization`):
  - `POST /api/auth/login` `{username, password}` → `{accessToken, refreshToken, expiresAt, userId, username, mustChangePassword}`.
  - `POST /api/auth/refresh` `{refreshToken}` → mismo cuerpo (probado).
  - `POST /api/auth/logout` `{refreshToken}` → 204.
- **Primer acceso del administrador de arranque: `mustChangePassword: true` y todo lo demás
  responde 403 hasta cambiarla.** `POST /api/auth/change-password` `{currentPassword, newPassword}`
  (autenticado) devuelve **una sesión nueva** (tokens nuevos, `mustChangePassword:false`). Lo mismo
  les pasa a los usuarios creados con contraseña inicial (probado con un usuario de lectura).
- `GET /api/auth/context` (autenticado, funciona aunque haya que cambiar la contraseña) devuelve la
  resolución de autorización con la que el front puede pintar menús:

  ```json
  {"outcome":"allow","context":{"subjectId":"…","subjectName":"admin","actorKind":"human",
   "actorPartyId":"…","isGlobalAdmin":true,"grants":[{"organizationId":"…","level":"ReadOnly","includeSubsidiaries":false}],
   "permissions":["*.*.*"],"effectiveOrganizationIds":["…"],"policyVersion":"…","correlationId":"…"}}
  ```

  Los permisos son `Contexto.Recurso.Acción` (`Facilities.Facility.Read`, `*.*.*` el comodín).
  Niveles de acceso a organización: `Full`, `ReadOnly`, `Restricted`.
- Cabecera opcional `X-Organization-Scope: <uuid>,<uuid>`: restringe el ámbito de la petición a esas
  organizaciones (intersección con las concesiones); un valor mal formado da 400.
- `Idempotency-Key` se lee en las altas de Parties (`/api/persons`, `/api/organizations`).
- Errores de autenticación: 401 con `WWW-Authenticate: Bearer`; 403 sin permiso; 503 con
  `Retry-After: 5` si la autorización no se puede determinar.

### Lo que NO hace el servidor (importa al front)

- **Sin CORS**: un `OPTIONS` de preflight responde 401 y ninguna respuesta lleva
  `Access-Control-*`. El front debe servirse del **mismo origen** (proxy inverso o `server.proxy`
  de Vite en desarrollo). Petición a backend: PETICIONES-BACKEND.md.
- **No sirve ficheros estáticos** ni expone **OpenAPI/Swagger**; no hay endpoint de metadatos de
  pantallas: la configuración de entidades vive en `front/angular/config/entities/**`, fuera de la API.
- **Sin `ETag`/`If-Match`**; ninguna respuesta lleva `Cache-Control` de datos (las de seguridad
  llevan `no-store`).
- **No lee `Accept-Language`**: los textos del servidor van en inglés (países, tipos de
  delimitación, errores) salvo catálogos sembrados en español (tipos de puesto de RRHH).

## 2. El contrato de mantenimiento (`/api/v1/<entidad>`)

Es lo que el motor de Angular lee y lo que consumirá el front React. Escrito a mano en
`contexts/<contexto>/maintenance.go`; cada contexto repite el mismo patrón. Entidades hoy:

| Ruta | Contexto | Escritura | Config Angular |
|---|---|---|---|
| `/api/v1/countries` | Geografía | **sólo lectura** (POST/PUT/DELETE → 409 con la vía) | `geography/country.config.json` |
| `/api/v1/holidays` | Geografía | alta, cambio del nombre, baja (borra) | `geography/holiday.config.json` |
| `/api/v1/facilities` | Instalaciones | alta, cambio, baja = desactivar | `facilities/facility.config.json` |
| `/api/v1/work-centers` | RRHH | alta, cambio (sede, cierre), baja = cerrar | `hr/work-center.config.json` |
| `/api/v1/products` | Productos | alta, cambio, baja = retirar | `products/product.config.json` |
| `/api/v1/assets` | Activos | alta, cambio, baja por el cambio | `assets/asset.config.json` |
| `/api/v1/carriers` | Envíos | alta, cambio, bloquear/desbloquear | `shipments/carrier.config.json` |
| `/api/v1/suppliers` | Compras | alta, cambio, bloquear/desbloquear | `purchases/supplier.config.json` |

Los 8 responden 200/401 correctamente en la prueba (con listas vacías salvo países, instalaciones,
centros de trabajo y festivos, que se poblaron).

### Operaciones

| Operación | Ruta | Respuesta |
|---|---|---|
| Lista | `GET /api/v1/<e>?page&pageSize&sortBy&sortOrder&<filtros>` | 200 página (abajo) |
| Lista (alterna) | `POST /api/v1/<e>/search` `{filter:{...}, pagination:{pageNumber, pageSize, orderBy, ascending}}` | 200 igual; mismos filtros y mismas reglas |
| Detalle | `GET /api/v1/<e>/{id}` | 200 objeto; 404 si no existe; 400 si el id no es válido |
| Alta | `POST /api/v1/<e>` | **201** con el objeto y cabecera `Location: /api/v1/<e>/<id>` |
| Cambio | `PUT /api/v1/<e>/{id}` | 200 con el objeto (`version` nueva) |
| Baja | `DELETE /api/v1/<e>/{id}` | **204** sin cuerpo (según entidad desactiva, cierra, retira, bloquea o borra) |
| Bloqueo | `POST /api/v1/<carriers|suppliers>/{id}/block` y `/unblock` con `{version, reason}` | 200; con aprobación activa, **202** y `pendingApproval` |
| Historial de bloqueos | `GET /api/v1/<carriers|suppliers>/{id}/block-history` | lista plana |

### Forma de la lista

```json
{"items":[…],"total":27,"totalCount":27,"pageNumber":1,"pageSize":20,"totalPages":2}
```

- `total` y `totalCount` valen lo mismo (nombres que lee el motor). `pageNumber` desde **1**.
- Por omisión `pageSize` = 20; máximo **1000**; `page` entre 1 y 1.000.000. Fuera de rango: **400**.
- **Orden**: `sortBy` con **lista blanca por entidad** (países: `alpha2|alpha3|name`; festivos:
  `date|name`; instalaciones: `name`; centros: `code`; productos: `sku|name`; activos:
  `code|name`; proveedores: `paymentDays`; transportistas: `code|name`); otro valor → 400 con
  `errors.sortBy`. `sortOrder` = `asc|desc`. El empate lo deshace siempre la identidad.
- **Filtros** (cualquier otro parámetro se **ignora en silencio**):
  - comunes: `search` (texto global) y `name`;
  - `isActive=true` (instalaciones, centros, activos, proveedores, transportistas);
  - `company` / `employer` / `organization` (empresa; sólo estrecha el ámbito del llamante);
  - países: `eu`, `eurozone`, `sepa` (`true|false`, otro → 400);
  - festivos: `boundary` (UUID de delimitación), `year` (1900–2200);
  - instalaciones: `type`, `partOf` (hijos de una instalación → base de un árbol);
  - productos: `category`, `kind`; activos: `class`, `inService`; proveedores: `category`, `supplier`,
    `includeMergedParties`.
  - En `POST …/search` un filtro sólo admite valor simple (texto, número, booleano); un objeto → 400.
  - **No hay filtros por rango, múltiples valores ni operadores**: el front no puede hacer
    «filtros de columna» ricos sin filtrar en cliente.
- **Los países se filtran, ordenan y paginan en memoria** en el servidor (248 filas; el listado
  completo con `pageSize=1000` pesa 55 KB). El resto va a SQL.

### Forma de los objetos (ejemplos reales)

- Valores monetarios y decimales viajan como **cadena** (`"areaM2":"1200"`, `cost`, `basePrice`,
  `withholdingRate`...). Fechas como `YYYY-MM-DD` (`opened`, `date`); instantes en RFC 3339. Una
  fecha o un identificador vacío viaja como `""`, no como `null` (ver `closed`, `partOf`).
- Los booleanos de estado son derivados en el servidor: `active` (centros: `closed == ""`).
- `id` es un UUID v7 en todos los casos salvo países (el código ISO alfa-2: `ES`).
- Instalación:
  `{"id","organization","type","typeName","name","description","partOf","areaM2","location":{line1,number,postalCode,locality,region,country,geoPostalCode,geoBoundary,phone,email…},"active","version"}`.
  `geoBoundary` y `geoPostalCode` los **resuelve el servidor** a partir de `postalCode`+`country`.
- País (lista): `{id, alpha2, alpha3, name, boundary, callingCode, eu, eurozone, sepa, currencies:null, languages:null, timeZones:null}`;
  (detalle): además `numeric, eea, ibanLength, ibanPattern, postalCodePattern, postalCodeRequired, requiresSubdivision, currencies[], languages[], timeZones[]`.
  `boundary` es el UUID de la delimitación del país, la llave para unirse a §5.
- Festivo: `{id, boundary, boundaryName, date, name, version}`.
- Centro de trabajo: `{id, employer, facility, code, headquarters, opened, closed, active, version}`.

### Alta y cambio: concurrencia y validación

- **Concurrencia optimista por `version` en el cuerpo**, no por ETag. Un cambio sin `version` →
  **400** `errors.version:["the version that was read is required"]`, código `required`. Con una
  versión que ya no es la actual → **409** `detail:"the facility changed after it was read: read it again"`.
  Probado. Cada cambio válido sube la versión en 1 (en los bloqueos y cierres también) y devuelve
  el objeto entero, con lo que el front puede actualizar la fila sin releerla.
- **`PUT` se comporta como `PATCH`**: se mezcla el cuerpo sobre lo que hay (probado con
  `{"name":"Planta Uno","version":1}`); lo que sobra se ignora (se puede devolver lo que se leyó,
  incluido `active`), lo que falta se conserva. Algunos campos son **inmutables** tras el alta y
  fallan con código `immutable` (festivos: `boundary` y `date`; centros: `employer`, `facility`,
  `code`).
- **No hay lotes**: ni endpoint de lote ni transacción entre filas. El «guardado por lotes» del
  front sólo puede ser una **secuencia de `PUT` independientes** (cada uno atómico por sí mismo;
  si el tercero falla, los dos primeros quedan guardados). Ver PETICIONES-BACKEND.md.
- **Borrado = baja lógica salvo festivos** (que se borran de verdad): instalaciones se desactivan
  (`active:false`, versión +1; el registro sigue en el GET), centros se cierran (`DELETE ?on=YYYY-MM-DD`
  opcional), productos se retiran, proveedores y transportistas se **bloquean y exigen motivo**
  (D-156), así que el `DELETE` sin motivo da 422 (según `front/README.md`; no probado). Repetir un `DELETE`
  sobre algo ya dado de baja es 204.
- **Errores**: `application/problem+json` (RFC 9457) en estas rutas con los errores de campo como
  **diccionario** `campo → [mensajes]` y `errorCodes` con los códigos en el mismo orden:

  ```json
  {"title":"Validation Failed","status":400,"detail":"One or more fields are invalid","instance":"/api/v1/facilities",
   "errors":{"type":["type must be a facility type id"]},"errorCodes":{"type":["format"]}}
  ```

  | Estado | Cuándo |
  |---|---|
  | 400 | validación de campo, página/orden fuera de rango, cuerpo que no es JSON (`errors.body`), id mal formado (`errors.id`) |
  | 401 / 403 | sin sesión o sin permiso (`title` y `status`, sin `detail`) |
  | 404 | no existe (o fuera del ámbito del llamante) |
  | 409 | versión caduca; país como dato de referencia |
  | 413 | cuerpo mayor de 1 MiB |
  | 422 | regla de negocio (`code` tipo `hr.work_center_in_use`) |
  | 202 | orden aceptada pero pendiente de aprobación (`pendingApproval`) |

  Los mensajes están en **inglés** y son para humanos técnicos, no para pantalla: el front debe
  traducir por `errorCodes` (`required`, `format`, `range`, `enum`, `immutable`, `unsupported`...)
  y poner el mensaje del servidor sólo como reserva. Hay una observación: la validación puede
  cortar en el primer fallo de formato (una fecha mal escrita impidió ver los otros campos
  inválidos del mismo alta), de modo que el servidor no siempre devuelve **todos** los errores a la vez.
- **Fuera del contrato de mantenimiento** (rutas `/api/...` sin `v1`) los errores de validación
  usan otra forma: **lista** `errors:[{field,code,message}]` más `correlationId`. El cliente HTTP
  del front tiene que normalizar las dos.

## 3. Identificadores, referencias y búsquedas auxiliares

Un campo de relación en el formulario (`organization`, `type`, `partOf`, `facility`, `employer`)
viaja como el **UUID** de la otra entidad; no hay `{id, label}` ni expansión. Para pintar nombres y
rellenar desplegables el front necesita llamadas auxiliares, que están **fuera** de `/api/v1` y con
formas distintas:

| Qué | Ruta | Forma |
|---|---|---|
| Organizaciones internas | `GET /api/internal-organizations`, `…/names` | lista de `Party` completa / `[{id,name}]` |
| Tipos de instalación | `GET /api/catalogs/facility-types` | `[{ID,Name,Description,RequiresContact,Active}]` **PascalCase** |
| Tipos de rol / relación | `GET /api/catalogs/party-role-types`, `party-relationship-types` | camelCase |
| Tipos de puesto / estados | `GET /api/hr/catalogs/position-types`, `position-statuses`, `position-classes` | **PascalCase** |
| Catálogos de referencia | `GET /api/reference/{currencies|languages|time-zones|street-types|boundary-types|boundary-levels}` | PascalCase salvo `boundary-levels` (camelCase) |
| Perfil de la empresa | `GET /api/profiles/internal-organizations/{id}` | — |
| Módulos activos | `GET /api/modules/current`, `/catalog`, `/activations` | no probados |

Incoherencias de formato a absorber en el cliente: catálogos en PascalCase (sin etiquetas
`json`) frente al resto en camelCase; páginas con `{items,total,number,size,totalPages}` frente a
`{items,total,totalCount,pageNumber,pageSize,totalPages}`; parámetro `size` frente a `pageSize`.

## 4. Jerarquías (organigramas)

Hay cuatro jerarquías con datos, y **ninguna tiene un endpoint de árbol uniforme**:

| Jerarquía | Cómo se modela | Cómo se lee hoy | Probado |
|---|---|---|---|
| **Puestos de trabajo** (organigrama de RRHH) | `Position` con líneas `reports-to` (principal única, con periodo, sin ciclos, máx. 20 niveles) | `GET /api/hr/positions/{raíz}/chart?depth=N` → árbol anidado `{position:{…}, reports:[…]}`; `depth` 1..20, **3 por omisión**. Lista plana: `GET /api/hr/positions?unit&organization&type&status&holder&vacant&page&size` | sí: tres niveles, y un ciclo da **422** `hr.reporting_cycle` |
| **Unidades organizativas** (empresa → división → departamento) | `Party` organización + relación `organization-rollup` (tipo `10000000-0000-0000-0002-000000000005`, hacia el padre, jerárquica, un padre vigente) | **no hay endpoint de árbol**: sólo `GET /api/parties/{id}/relationships` (por party) y puertos internos (`Descendants`) sin ruta HTTP | sí: alta de «Ventas» como departamento bajo la empresa por `affiliation` |
| **Instalaciones** (edificio → planta → sala) | `partOf` (un padre, sin ciclos, misma organización) | `GET /api/v1/facilities?partOf=<id>` devuelve hijos directos; la **raíz** no se puede pedir (no hay filtro «sin padre»): hay que listar todo y filtrar | sí |
| **Delimitaciones geográficas** | `parent` único de contención | `GET /api/geography/boundaries?parent=<id>` | sí (§5) |

Faltan para el organigrama:

1. **Raíces de puestos**: no hay `GET /api/hr/positions?root=true` (sin `reportsTo`); el front tiene
   que listar y deducir (`reportsTo` vacío en el DTO).
2. **Árbol de unidades**: sin ruta HTTP; sólo alcanzable con N+1 llamadas a `relationships`.
3. **Un árbol que mezcle** empresa → unidades → puestos → personas obliga a unir tres APIs.
4. El nodo del organigrama trae el `PositionDTO` entero (titular como UUID de persona, sin
   nombre): para mostrar el nombre del titular hace falta otra llamada a `/api/parties/{id}`.
   Las banderas del puesto (`flags.Salaried`...) viajan en PascalCase.

## 5. Datos geográficos

Rutas de Geografía **no versionadas** y distintas del contrato de mantenimiento (permisos
`Geography.Boundary.Read` y `Geography.Reference.Read`):

- `GET /api/geography/boundaries?q&type&parent&grouping&country&level&page&size` → `{items,total,number,size,totalPages}`;
  `size` máximo **1000** (5000 se recorta a 1000); `level` ∈ `country|1|2|3|local`.
- `GET /api/geography/boundaries/{id}` → la delimitación con `ancestors[]` (cadena hasta el continente)
  y `groupings[]` (UE, etc.).
- `GET /api/geography/postal-codes?country&code`, `POST /api/geography/address-check`.
- `GET /api/reference/countries[/{alpha2}]`, además de `/api/v1/countries` (§2).

Delimitación: `{id, name, type, typeName, geoCode, abbreviation, nutsCode, ineCode, lauCode, country, level, parent, active}`.

Contenido real medido tras migrar:

| Qué | Cantidad |
|---|---|
| Delimitaciones | 8.507 |
| Países (con perfil) | 248 |
| España: país / NUTS 1 / NUTS 2 / NUTS 3 / municipios | 1 / 7 / 19 / 59 / 8.132 |
| Otros países | **sólo el país** (`FR`: 1) |
| Códigos postales | 14.608 filas |

La escalera es la de `GEOGRAFIA-NUTS.md`: Continente → subcontinente → País → NUTS 1 (agrupación,
no administrativa) → NUTS 2 (comunidad) → NUTS 3 (provincia/isla) → municipio. Niveles con
nombre por país en `/api/reference/boundary-levels` (para España: «Agrupación de comunidades
autónomas», «Comunidad autónoma», «Provincia o isla», «Municipio»). **Mundo → país → región → ciudad
sólo existe para España**; el resto del mundo no tiene nivel 1–3 ni ciudades.

**No hay ninguna coordenada ni geometría en ningún sitio de la API ni de la base**: ni latitud y
longitud, ni centroides, ni cajas, ni GeoJSON (la búsqueda de `latitude|longitude|centroid|geojson`
en `contexts/` sólo encuentra falsos positivos). Tampoco las instalaciones: su `location` es una
dirección postal. Consecuencias para el mapa navegable por niveles:

- La **identidad** para unir con teselas vectoriales alojadas por nosotros son los códigos:
  país ISO alfa-2/alfa-3/numérico, `nutsCode` (NUTS 1–3 de España), `lauCode`/`ineCode` (municipios,
  5 dígitos INE) y el UUID de la delimitación. Las geometrías (PMTiles) tienen que venir de otra
  fuente con licencia permitida y llevar esos códigos como propiedades; el front une por código.
- Para pintar métricas sobre el mapa (clientes por provincia, por ejemplo) habría que agregar por
  código en el cliente o pedirlo al backend: **no hay agregados por delimitación**.
- Para ubicar instalaciones como puntos haría falta geocodificar `postalCode` (hay 14.608 filas de
  código postal ↔ municipio, sin coordenadas) o añadir coordenadas al modelo.

## 6. Permisos y ámbito (resumen operativo)

- Un usuario sin el permiso de la entidad recibe **403 en la lista** (probado: un usuario con
  `Facilities.Facility.Read` y `Geography.Reference.Read` ve países e instalaciones, y recibe 403
  en centros de trabajo, festivos y en cualquier escritura). La pantalla debe ocultarse con
  `/api/auth/context` y manejar el 403 igualmente.
- Países exigen `Geography.Reference.Read` también para obtener el 409 de escritura.
- El ámbito por organización filtra silenciosamente las listas: un `ReadOnly` o `Restricted` ve
  sólo su organización, y una escritura fuera de su ámbito falla (según el código; no probado).
- La gestión de usuarios y roles está en `/api/security/...` (`users`, `roles`, `permissions`);
  se usó para crear el usuario de prueba (`PUT /api/security/users/{id}/roles/{rol}` → 200).
  Un rol propio se define con `POST /api/security/roles {name, permissions[]}`.

## 7. Cómo se sembraron los datos de la prueba (reproducible)

Todo por HTTP con el administrador (tras cambiar su contraseña):

1. `POST /api/organizations` `{legalName, tradeName, roles:["10000000-0000-0000-0001-000000000001"]}`
   (rol *internal-organization*; los roles viajan como **UUID de tipo de rol**, no como código).
2. Unidad bajo la empresa: `POST /api/organizations` con `roles:[<department>]` y
   `affiliation:{organization:<id>, relationshipType:"10000000-0000-0000-0002-000000000005"}`.
3. Instalaciones: `POST /api/v1/facilities` con `type` de `/api/catalogs/facility-types`
   (`Building`, `Floor`...) y `partOf` para el árbol. El código postal 28001 resolvió a Madrid.
4. Centro de trabajo: `POST /api/v1/work-centers` (`code` se guarda en mayúsculas).
5. Puestos: `POST /api/hr/positions {unit, type}` y `POST /api/hr/positions/{id}/reports-to {supervisor, primary:true}`.
6. Festivos: `POST /api/v1/holidays {boundary:<UUID>, date, name}`. **`boundary` debe ser el UUID**; el
   código `ES` o `ES-MD` da 400 `format`.
7. Persona: `POST /api/persons {givenName, firstSurname}` (con `Idempotency-Key`).

## 8. Lo que falta o estorba para el front (resumen)

Ver los detalles y el formato de petición en `PETICIONES-BACKEND.md`. Orden aproximado de impacto:

1. **CORS / mismo origen** (sin CORS no hay front en otro origen). Salvable con proxy; el
   despliegue debe prever el proxy o que Go sirva el front.
2. **Guardado por lotes transaccional** (no existe; hoy PUT fila a fila).
3. **Coordenadas / geometrías** (inexistentes); definir de dónde salen las teselas y cómo se unen.
4. **Árbol de unidades** y **raíces de puestos** sin endpoint; organigrama de puestos sólo desde una raíz conocida.
5. **Filtros de columna** limitados a los de cada entidad; sin rangos ni multivalor; los desconocidos se ignoran sin avisar (riesgo: el front cree que filtra y no filtra).
6. **Metadatos de las entidades** no servidos por la API (la configuración JSON vive fuera).
7. **Referencias sin etiqueta** (UUID sin nombre) y catálogos con formatos distintos.
8. **Mensajes y catálogos en inglés**, sin `Accept-Language`.
9. **Datos de demostración** inexistentes; geografía sólo detallada para España.
10. **Token de 15 minutos** con refresco: el cliente necesita renovar de forma transparente y
    reintentar la petición que falló con 401.
11. **Bloquear con motivo y 202 de aprobación**: el contrato lo tiene y la configuración
    declarativa de Angular no (`front/README.md`); el front React puede resolverlo con una acción de
    orden declarada en la configuración.
12. Sin `ETag`: la concurrencia depende de que el cliente reenvíe `version`.

## 9. Pendiente de comprobar (no se probó)

- Altas y cambios de productos, activos, proveedores y transportistas (sólo se comprobó la lista
  vacía y los campos del código fuente), y las órdenes `block`/`unblock` con aprobación.
- `/api/modules/*` (módulos activos), `/api/security/users` en lista, exportaciones, y el resto de
  contextos sin contrato de mantenimiento (pedidos, facturación, tesorería, contabilidad, nóminas...).
- Comportamiento con varias organizaciones y `X-Organization-Scope` válido.
- PostgreSQL (`cmd/karpo-postgres`): las formas HTTP son las mismas; no se levantó.
