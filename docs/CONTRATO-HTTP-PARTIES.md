# Parties: qué pide de verdad el front de Karpo, y qué le daría Go

> Escrito el 2026-10-13 para una decisión que Javier tiene pendiente desde el 11 de octubre: **cuál
> es el contrato HTTP común** para que el mismo front hable con el back de C# y con el de Go
> ([PARTIES-COMPARACION-CON-CSHARP.md](PARTIES-COMPARACION-CON-CSHARP.md) §4.3). Aquel documento
> comparaba 464 rutas de C# con 34 de Go. Este mide otra cosa: **las rutas que el front usa**. No
> decide nada; las opciones están en §5.
>
> El inventario del front lo hizo un agente de sólo lectura sobre
> `C:\Git\Paranoia\Karpo\010-Front\Angular`; yo comprobé una muestra (§6). Nadie ha ejecutado el
> front contra ningún back para esto.

## 0. Lo esencial

1. **El front no usa 464 rutas de Parties: usa unas 50**, y las siete pantallas principales
   (participantes, personas, organizaciones, clientes, empleados, organizaciones legales e
   internas) viven de **una docena**.
2. De esa docena, Go sirve hoy **dos tal cual** (los catálogos de tipos de rol y de relación). El
   resto existe en Go con otra ruta y otra forma, o no existe.
3. **El grueso del trabajo no son las rutas, es una sola: la búsqueda enriquecida**
   (`POST /parties/search/enriched`), con su filtro, su paginación por cursor y su fila con
   resúmenes de roles y relaciones. De ella cuelgan las siete pantallas.
4. Hay **cuatro cosas que el front hace y que ya decidiste que no se hacen**: borrar personas y
   roles, y el estado y la prioridad de las relaciones. Un contrato común tiene que decir qué ve
   el usuario en esos botones.
5. En el front hay **código muerto y configuración sin consumidor** que conviene no confundir con
   contrato: dos servicios enteros sin llamadas, y quince claves de configuración que nadie lee.

## 1. Cómo habla el front

**El front es genérico.** No tiene pantallas escritas a mano para Parties: cada entidad es un
fichero de configuración (41 para Parties) con su ruta base (`apiEndpoint`, por ejemplo
`/persons`) y sus campos. De esa ruta base el front deriva unas pocas llamadas fijas.

| Qué hace el usuario | Qué llama el front |
|---|---|
| Abrir una lista grande | `POST {ruta}/search` o, si la configuración lo dice, una búsqueda «enriquecida» propia |
| Abrir una lista pequeña | `GET {ruta}` sin parámetros, y filtra en el navegador |
| Abrir un detalle | `GET {ruta}/{id}` o una ruta «agregada» propia |
| Crear | `POST {ruta}` |
| Editar | `PUT {ruta}/{id}` con todos los campos del formulario |
| Borrar | `DELETE {ruta}/{id}` |

*Ejemplo:* la pantalla «Personas» tiene `apiEndpoint: "/persons"`, pero su lista no llama a
`/persons`: su configuración apunta a `parties/search/enriched` con el filtro fijo
`{ "partyTypes": ["Person"] }`.

**Prefijo.** Casi todo va a `/api/v1/…`; unas pocas llamadas, escritas a mano, van a `/api/…` sin
versión. Según un comentario de la configuración del front, la pasarela quita el `/v1`. Go sirve
`/api/…`.

**Cabeceras.** `Authorization: Bearer …`, `X-Organization-Scope` con las empresas elegidas (Go ya
la entiende) y `X-Client: web`.

## 2. Lo que piden las siete pantallas principales

| # | Ruta del front | Para qué | En Go hoy |
|---|---|---|---|
| 1 | `POST /parties/search/enriched` | La lista, con filtro, orden y página | **Otra forma**: `GET /api/parties?q=&kind=&role=…` |
| 2 | `POST /parties/search/enriched/cursor` | La misma lista, por bloques (paginación «por cursor»: en vez de «página 3», «lo que sigue a esta fila») | **No existe** |
| 3 | `POST /parties/search/stats` | Recuentos para los gráficos y para las opciones de los filtros | **No existe**. El front tolera un 404 aquí |
| 4 | `GET /parties/{id}/aggregate` | El detalle: la party con sus roles y relaciones | **Otra forma**: `GET /api/parties/{id}` |
| 5 | `POST /parties/rename` `{id, name}` | Cambiar el nombre | **Otra forma**: `PUT /api/parties/{id}/name` con nombre y apellidos por separado |
| 6 | `POST /parties/toggle` `{id, enable}` | Activar o desactivar | **Otra forma**: `PUT /api/parties/{id}/active` |
| 7 | `PUT /parties/{id}/test-flag` | Marcar como de pruebas | **No existe** como ruta (el dato sí) |
| 8 | `GET /parties/{id}/positions` | Los puestos de una persona | En otro contexto (`hr`), con otra ruta |
| 9 | `GET /catalogs/party-classifications/by-party/{id}` | Sus clasificaciones | Vienen dentro de `GET /api/parties/{id}` |
| 10 | `GET /catalogs/party-role-types` | Opciones de filtros y formularios | **Igual** |
| 11 | `GET /catalogs/party-relationship-types` | Ídem | **Igual** |
| 12 | `GET /catalogs/geographic-boundaries?type=Country` | Países para el filtro de documento | Otra ruta: `GET /api/reference/countries` |
| 13 | `POST /customers/quick-create` | Alta rápida de cliente (asistente de reservas) | **Otra forma**: `POST /api/persons` u `/organizations` con `affiliation` |
| 14 | `GET /internal-organizations` | Elegir empresa | **Igual la ruta**; la forma, sin comprobar |

### 2.1 La búsqueda enriquecida, que es lo caro

Lo que el front manda:

```json
{ "filter": { "partyTypes": ["Person"], "nameContains": "ana", "roleTypeIdsAny": ["…"], "isActive": true },
  "pagination": { "pageNumber": 1, "pageSize": 25, "orderBy": "name", "ascending": true },
  "include": ["roleSummary", "relationshipSummary"] }
```

Y lo que espera en cada fila: `id`, `name`, `partyType`, `isActive`, `roleNames[]`, `roleCount`,
`relationshipSummaries[]`, `relationshipCount`, y lo que nombre cada columna (`country`,
`birthDate`, `taxIdentity`, `isTest`, `companyLabel`).

Lo que Go tiene: filtros por texto, tipo, rol, documento, empresa, instalación y clasificación,
uno de cada; orden fijo por nombre; páginas numeradas; y la party entera en cada fila.

| Clave de filtro del front | En Go |
|---|---|
| `nameContains`, `isActive`, `identificationValue` | Hay equivalente (`q`, `active`, `document`) |
| `partyTypes` | `kind`, pero el front manda `Person`, `Organization`, `LegalOrganization`, `Corporation`, `InternalOrganization`, y Go sólo distingue persona y organización |
| `roleTypeIdsAny` | `role`, de uno en uno |
| `roleTypeIdsAll`, `classificationTypeIdsAny` | No: varios a la vez |
| `internalOrganizationIds` | `organization`, de una en una |
| `issuingCountryBoundaryId` | No. Go guarda el país del documento como código de dos letras, no como identificador |
| `onlyDuplicatedIdentifications` | No, y en Go no puede haber duplicados: lo impide un índice |
| `isTest`, `birthDate` | No |
| Orden por cualquier columna | No: sólo por nombre |

## 3. Lo que piden las demás pantallas de Parties

| Grupo | Rutas del front | En Go |
|---|---|---|
| **Roles** (`/party-roles`) | Lista (`POST …/search`), crear, editar entero (`PUT`), **borrar** | Asignar y terminar bajo la party (`POST /api/parties/{id}/roles`, `…/end`). No hay lista de roles ni borrado |
| **Relaciones** (`/party-relationships`) | Búsqueda enriquecida, por cursor, recuentos, crear, editar, **borrar**, y «las que llegan a» o «salen de» una party | Crear (misma ruta), terminar, y las de una party. No hay búsqueda de relaciones ni borrado |
| **Relación de cliente y de proveedor** | Listas propias y sus perfiles comerciales | No existen como recurso: son relaciones con tipo. Los perfiles están en `receivables` y `purchases` |
| **Organización interna**: representantes, accionistas, filiales, actividades, contadores, módulos, cuentas de cotización | Ocho listas hijas | Repartido en `fiscal`, `payroll`, `modules` y relaciones con tipo |
| **Catálogos de estado y prioridad** de la relación | `GET /catalogs/status-types`, `/priority-types` | **No existen: decidiste dejarlos fuera** (D-11) |
| **Tipos de party** | `GET /catalogs/party-types` | No existe: Go tiene forma jurídica y familias de clasificación |
| **Cuentas bancarias de terceros, consentimiento, registro** | CRUD | Sin portar |
| **Comunicaciones, casos, contactos sueltos, instalaciones, jerarquía, accionistas** | Listas de sólo lectura | Sin portar, o en otro contexto con otra ruta |
| **Contrato laboral, cuenta de nómina, actividad fiscal, modelos** | CRUD | En `hr`, `payroll` y `fiscal`, con otro diseño |
| **Exportación** | `POST /exports/start`, estado, cancelar, descargar | Existe con otras rutas (`POST /api/exports`…) |

## 4. Lo que no es contrato aunque esté en el front

| Qué | Detalle |
|---|---|
| Dos servicios sin llamadas | `party-role-api.service.ts` no lo usa nadie; `party-relationship-api.service.ts` sólo lo usa una función sin llamadas. Son 21 rutas que no hace falta servir |
| Funciones del servicio genérico sin llamadas | Contar, borrar en bloque, exportar e importar por la ruta de la entidad, `PATCH`, `$expand`, y los parámetros `sortBy`, `sortOrder`, `search` y `fields` |
| Quince claves de configuración sin consumidor | Entre ellas `lookupEntity`, `extraQueryParams`, `childGridConfig`, `form.submitAction`. Los campos de tipo «lookup» se pintan como texto |
| Un error que deja ver el diseño | «Representantes» y «Accionistas» de una organización interna hacen **la misma petición**: el filtro por tipo de relación está en la configuración y no se envía |
| Un detalle que rompe | El detalle se pide a `/parties/{id}/aggregate/` **con barra final** en un camino y sin ella en otro |

El front acepta varias formas de respuesta en las listas (un vector, o las filas en `items` o
`data` y el total en `totalCount`, `totalItems`, `total` o `totalElements`), pero **no la misma
en todas partes**: la forma de Go (`items` + `total`) vale en las listas principales y **no** en
las listas hijas de un detalle, que piden `data` + `total`, o `items` + `totalCount`.

Los errores se leen de `message`, `errors`, `detail` o `title` según el sitio. El alta, la
edición y el borrado genéricos enseñan un texto fijo y no el del servidor, por un fallo del
propio front.

## 5. Opciones, con el tamaño que da este inventario

Los tamaños son estimaciones mías. «Pequeño» es un día de sesión con sus pruebas; «medio»,
varios; «grande», semanas.

| Opción | Qué es | Tamaño | Qué deja fuera |
|---|---|---|---|
| **A. Go sirve lo que el front pide** | Una capa de rutas en Go con las 14 de §2, en la forma del front. La búsqueda enriquecida es casi todo el trabajo: filtros por varios valores, orden por columna, cursor y fila resumida | **Medio**, no «cientos de rutas» como estimé el día 11 | Las pantallas de §3. Y obliga a decidir qué responde Go a «borrar persona» |
| **B. C# añade las rutas de Go** y el front cambia después | Lo que proponía el estudio de convergencia | Medio en C#, medio-grande en el front (41 configuraciones) | Nada, pero es el camino largo |
| **C. Un contrato nuevo, escrito una vez**, que sirven los dos | Se parte de lo que el front usa (este inventario), se limpia (sin barra final, una sola forma de lista, un solo sobre de error) y se escribe como se ha hecho con la extensibilidad | Medio en cada back, medio en el front | Nada; es el único que deja un contrato que Theros pueda generar |

**Mi recomendación: C, empezando por las 14 rutas de §2.** Razones:

1. El inventario demuestra que el contrato real del front es pequeño y está sucio. Copiarlo tal
   cual en Go (A) copia la barra final, las cuatro formas de total y la petición repetida.
2. Ya hay un precedente que ha funcionado: el contrato de extensibilidad lo escribió Trench, lo
   revisó Go y quedó uno solo en un día.
3. Con C, la búsqueda enriquecida se diseña una vez con lo que los dos pueden servir, en vez de
   heredar filtros como «sólo documentos duplicados», que en Go no tienen sentido.

## 6. Decisiones que tocan a Javier

1. **A, B o C.**
2. **Los botones de borrar.** El front borra personas, organizaciones, clientes, roles y
   relaciones. Decidiste terminar y desactivar, y «anular» para los errores (D-4, D-8). ¿El botón
   pasa a llamarse «desactivar» o «terminar», o se queda como «eliminar» y hace eso por debajo?
3. **Los tipos de party del front** (`LegalOrganization`, `Corporation`, `InternalOrganization`).
   En Go una organización tiene forma jurídica, y «interna» es un rol. ¿El contrato común habla
   como el front (cinco tipos) o como Go (dos tipos, forma jurídica y roles)?

## 7. Límites

- El inventario lo hizo un agente; comprobé a mano esto y coincide: la configuración de
  «Personas» (búsqueda enriquecida, cursor, agregado), las rutas de renombrar, activar y marcar
  de pruebas, que el 404 de recuentos se tolera, los nombres de total y de cursor de la lista
  principal, las rutas de alta rápida y de validar formato, y que la búsqueda enriquecida existe
  en C#. **No comprobé** el resto, en particular las tablas de §3 y §4.
- No se ha leído la tabla de rutas de la pasarela: que quita `/v1` sale de un comentario.
- La columna «En Go» de §2 y §3 es de leer las rutas de Go, no de probarlas con el front.
- El agente no determinó el formato exacto de los filtros de fecha ni de selección múltiple.
- Sólo es Parties. Los otros contextos tendrán su propio inventario.
