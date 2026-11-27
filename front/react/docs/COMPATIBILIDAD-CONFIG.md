# Compatibilidad con la configuración de Angular

Qué hace `@karpo/react` con cada clave del JSON del runtime de Angular de Karpo C#
(`front/angular/config/entities/**`, A1 §3 del análisis). El motor está en `src/config`; este
documento y el aviso de claves ignoradas salen de **la misma tabla** (`src/config/compat.ts`), y
una prueba (`src/config/compat.test.ts`) comprueba que la lista de abajo contiene todas las claves
de A1 §3 y que el bloque generado está al día.

## Estados

| Estado | Significado |
| --- | --- |
| soportada | Se lee y se usa tal cual. |
| normalizada | Se lee y se convierte al modelo interno; la nota dice cómo. |
| ignorada | Se lee y se descarta, **con aviso** `info` en `EntityConfig.warnings` (la nota dice por qué). |
| ignorada (sin aviso) | Se descarta sin aviso: claves que Go declara en todos sus JSON y que no tienen efecto (`fieldMapping`, `metadata`, `showHistory`…). |
| desconocida | No está en la tabla: aviso `warn`. No es un error: la entidad se carga igualmente. |

Las claves de texto (`name`, `label`, `header`, `title`, `hint`…) aceptan además `<clave>Key`
(clave de i18n) y, como valor, un objeto por idioma (`{"es": "…", "en": "…"}`). Al pintar, el texto
se resuelve en este orden: clave traducida en el idioma activo, diccionario de la configuración
(`manifest.i18n[idioma]`), objeto por idioma y literal.

Los comentarios (`$schema`, `_nota`, `x-…`, `//`) se ignoran en silencio.

## Qué se ve en cada modo de carga

- **Manifiesto** (`loadManifest(url)`): `manifest.json` con entidades (`path` relativo al
  manifiesto o `config` en línea), `overlay` por entidad, menú, idiomas, tema, mapa y API.
- **Legado** (`loadLegacy(indexUrl, sidebarUrl)`): `index.json` + `sidebar-config.json` de Angular.
  Sólo se cargan las entidades que cita el menú (`all: true` carga todo el índice). Un `entityId`
  que no está en el índice se busca como `<id>.config.json` junto al índice (aviso `info`).
- **Descubrimiento** (`loadDiscovered(baseUrl, rutas)`): los 8 JSON de Go no están en ningún
  índice; el registro y el menú (agrupado por carpeta) se componen con las propias rutas.

## Superposición (`overlay`)

Se funde en profundidad sobre el JSON **antes** de normalizar, sin tocar los ficheros de
`front/angular`. Los objetos se funden clave a clave; los arrays de objetos con `id`, `field`,
`action` o `value` se funden por identidad (lo nuevo se añade al final); el resto de arrays se
sustituyen; un `null` borra la clave. Sirve para lo que el JSON de Angular no sabe decir: vistas `tree`
y `map`, claves ajenas con selector (`lookupEntity`), `help`, `permissions`, `serverSortFields`,
`saveMode`, `presentation` y las claves de texto (`*Key`).

`value` es la identidad de las opciones (`options`, `enumValues`): una superposición
`[{ "value": "land", "labelKey": "…" }]` pone la clave de texto a cada opción **sin sustituir** el array. El
valor puede ser una cadena, un número o un booleano (`true` y `"true"` son la misma identidad).

### Superposición en un fichero (`overlayPath`)

Una entrada del manifiesto puede llevar, además de `overlay` en línea, `overlayPath`: un JSON de
superposición relativo al manifiesto. Se funde **antes** que `overlay` (el `overlay` en línea manda). Sirve
para superposiciones generadas, como las de textos de la demo (`demo/config/overlays/<id>.i18n.json`, de
`npm run i18n:demo`). Si el fichero no se puede leer o no es un objeto, se avisa (`warn`, ruta
`entities.<id>.overlayPath`) y la entidad se carga sin él.

## Textos y convención de claves

Toda propiedad de texto acepta `<prop>Key` (`headerKey`, `labelKey`, `titleKey`, `hintKey`,
`placeholderKey`, `trueLabelKey`, `falseLabelKey`, `confirmMessageKey`, `emptyMessageKey`…). El normalizador
lee todas las que aparecen en la tabla de abajo (`textProps` de cada ámbito) y deja en el `Text` el literal y
la clave. Al pintar manda la clave del idioma activo, luego el diccionario de la configuración
(`manifest.i18n[idioma]`), luego el objeto por idioma y, al final, el literal.

Convención de claves de las superposiciones generadas para las 8 configuraciones de Go (D-R28; es lo que
TC-3 pide al pack `karpo-go` de Theros). Todas cuelgan de `entities.<id>.`, con `<id>` en camelCase
(`work-center` → `workCenter`, como el `nameKey` ya escrito a mano):

| Qué | Clave |
| --- | --- |
| Columna de la cuadrícula | `columns.<campo>.header` (y `.hint`, `.placeholder`, `.trueLabel`, `.falseLabel`) |
| Campo de `form` o `detail` | `fields.<campo>.label` (y `.hint`, `.placeholder`, `.trueLabel`, `.falseLabel`) |
| Opción (`badgeMap`, `enumValues`, `options`, de columnas, campos y filtros) | `options.<campo>.<valor>` |
| Sección | `sections.<id>.title` (y `.description`) |
| Títulos del formulario | `form.createTitle`, `form.editTitle` |
| Filtro | `filters.<campo>.label` (y `.hint`, `.placeholder`) |
| Acción de fila u orden | `commands.<id>.label` (y `.confirmMessage`, `.successMessage`…) |
| Hija del detalle | `childGrids.<id>.title`, `.emptyMessage`, `.columns.<campo>.header` |
| Estado vacío | `emptyState.title`, `emptyState.message` |

Si una misma clave tuviera textos distintos en dos ámbitos (por ejemplo el título de la sección `main` del
formulario y el del detalle), todas llevan el ámbito delante (`form.sections.main.title`,
`detail.sections.main.title`). Los trozos de la clave no llevan puntos (se sustituyen por `_`).

`scripts/gen-demo-i18n.mjs` aplica la convención a las 8 configuraciones: escribe las superposiciones, el
diccionario español (los literales de Go, sin tocar) y el inglés (de la tabla revisada
`demo/config/i18n/en.go.json`, clave → inglés). `npm run i18n:demo -- --check` falla si falta una clave en
`en.go.json`, si sobra alguna o si lo generado no está al día. Lo escrito a mano en `es.json` y `en.json`
se conserva: el generador sólo reescribe las ramas `columns`, `fields`, `options`, `sections`, `form`,
`filters`, `commands`, `childGrids` y `emptyState` (y los ámbitos de los conflictos) de las 8 entidades.

## Iconos

Un `icon` (de la entidad, del menú, de las opciones, secciones, filtros, órdenes y el estado vacío) debe ser
un nombre de `ICON_NAMES` (`src/contracts/icons.ts`): un nombre de Phosphor en kebab (`buildings`,
`tree-structure`…) o un alias propio de la configuración heredada (`building`, `delete`…). Tras
normalizar, un recorrido del menú y de cada entidad avisa (`warn`) de todo nombre fuera de la lista, con la
ruta del JSON para el menú (`menu[3].children[1].icon`) y la del modelo normalizado para la entidad
(`columns[2].options[1].icon`, `commands[0].icon`, `form.sections[0].icon`…); el icono cae en un círculo
(`data-kx-icon="missing"`). Un ítem de menú que hereda el icono de su entidad no se avisa dos veces. Los
avisos de iconos se ven en `manifest.warnings` (menú) y en `entity.warnings` (entidad).

## Vistas de mapa: `skipSingle`

`views[].skipSingle` (sólo `kind: map`) dice si el mapa salta los niveles con una sola delimitación (Q-10,
D-R30). El normalizador lo rellena siempre: `true` por omisión y `false` si la configuración o la
superposición lo pone explícitamente (también como cadena `"false"`). Cualquier otro valor cuenta como
omitido.

## Herencia de pistas: del formulario al detalle

Las configuraciones de Go ponen `hint` sólo en `form.sections[].fields[]`; el detalle (lo que se ve al
leer y al editar) no las trae. Al normalizar, un campo de `detail` sin `hint`, `placeholder`, `maxLength`
o `help` los hereda del campo homónimo (mismo `field`) de `form`, y se avisa con `info`
(«el campo `type` hereda `hint` de `form`», en `detail.sections[0].fields[1]`). Lo que el detalle ya trae no
se toca. (Ninguna clave del JSON da hoy `help` a un campo; se hereda si el campo normalizado lo trae.)

## Prioridad de columna y ajuste al ancho (`priority`, ronda 3)

`grid.columns[].priority` es una extensión opcional de Karpo React (la configuración de Angular la
ignora): un entero; **mayor = más importante**. Con poco ancho la rejilla encoge primero las columnas
sin ancho del usuario hasta `max(mínimo por tipo, 0,7 del ancho)` y, si aun así no caben, **oculta por
`priority` ascendente**. Sin `priority` una columna cuenta como 0 (un valor positivo la hace más
importante que las demás y uno negativo, «ocultar antes»); con igualdad se oculta antes la de más a la
derecha, de modo que sin ninguna `priority` el resultado es «de derecha a izquierda». La primera columna
y las fijadas (`pinned`) no se ocultan nunca. Un valor no entero se descarta con un aviso («prioridad de
columna no entera»). Las columnas ocultas por el ajuste no tocan las preferencias del usuario
(`prefs.hidden`); el menú de columnas las marca «oculta por falta de espacio» y ofrece «Ajustar las
columnas al ancho disponible» (`prefs.autoFit`). El pie de la lista cuenta cuántas hay (`+n`).

## Valor vacío normalizado (`emptyValue`, ronda 3)

Una celda o un campo de lectura sin valor pinta siempre la misma marca, «—», con el texto «Sin valor» para
los lectores de pantalla (`fields:empty.noValue`). Si la columna o el campo trae `emptyValue`:

- hecho sólo de guiones (`-`, `–`, `—`, `‒`, `−`): se normaliza a «—»;
- cualquier otro texto («sin fecha», «·»): se respeta tal cual (y se pinta sin la marca).

**Cambio de comportamiento:** sin `emptyValue`, un valor vacío ya no pinta nada: pinta «—», en la rejilla
y en la ficha por igual.

## Recorrido de ayuda: fusión de pasos y ancla de reserva (ronda 3)

`help.steps` de una entidad ya **no sustituye** a las anclas de la pantalla: el recorrido es la fusión de
(1) los pasos de la configuración, en su orden, y (2) detrás, las anclas `data-kx-help` **visibles y con
texto** que ningún paso cubre, en orden de documento. Un ancla queda cubierta si un paso resalta su mismo
elemento (`target` como selector CSS o como ancla) o cita su nombre; un paso sin elemento visible o sin
texto se omite y no cubre nada. No hay interruptor (D-R36): quien quiera un guion exacto cita todas las
anclas que quiera ver, y el resto se añade al final. Sin pasos de configuración el resultado es el de
antes (sólo anclas).

El atributo `data-kx-help-fallback="<ancla o selector del elemento visible>"` va en un elemento efímero
y oculto (la barra de cambios por lotes, mientras no hay cambios): el recorrido enseña entonces el
elemento de reserva con los textos `help.<nombre>.idle.title|body` del espacio de nombres del ancla
oculta (`grid:help.batch-bar.idle.*`). Sin esos textos, o con la reserva no visible, el paso se omite;
con el ancla visible manda su texto normal. Un generador de pantallas (Theros) que emita anclas efímeras
puede usarlo igual.

## Otras tolerancias

- `serverSortFields` también admite una lista de campos (`["code", "name"]`): cada uno se ordena en el
  servidor con su propio nombre. Antes era un aviso de tipo y la lista se ignoraba.
- `availableViews: ["tree", "map"]` vale si `views[]` (o `hierarchy`, para `tree`) declara esa vista.
- `views[].label` y `views[].labelKey` se ignoran con aviso `info` (el conmutador usa `core:view.*`).
- Las órdenes aceptan `tone` y `confirmMessage` (antes avisaban de clave desconocida).

## Tipos de campo

| Tipo del JSON | Tipo de React | Notas |
| --- | --- | --- |
| `text`, `string`, `guid`, (ausente) | `text` | |
| `textarea` | `textarea` | |
| `number`, `int`, `integer` | `number` | |
| `decimal` | `decimal` | |
| `currency`, `money` | `money` | `currency`/`currencyCode` guarda la divisa |
| `percentage`, `percent` | `percentage` | |
| `date` / `datetime`, `date-time` | `date` / `datetime` | |
| `boolean`, `bool`, `checkbox`, `switch` | `boolean` | en columnas, formulario, detalle y filtros |
| `select`, `radio` | `select` | `enum` en formulario y detalle |
| `enum`, `badge` | `badge` | en columnas; `badgeMap` y `enumValues` se funden en `options` |
| `multi-select`, `multiselect` | `multiselect` | |
| `lookup`, `guid-select`, `autocomplete` | `lookup` | sólo con `lookupEntity`; si no, `select` (con opciones) o `text`, con aviso |
| `email`, `phone`/`tel`, `url`/`link` | `email`, `phone`, `url` | |
| `text` + `lookupEntity` (o `select` + `lookupEntity`) | `lookup` | así se declaran las claves ajenas de Go |
| `image`, `icon`, `progress`, `trend`, `rating`, `sparkline`, `array`, `object`, `tag`, `avatar`, `password`, `file`, `color`, `rich-text`, `time`, `json` | `text` | con aviso: sin equivalente |
| cualquier otro | `text` | con aviso `warn`: tipo desconocido (Angular lo degradaba en silencio) |
| `actions` (columna) | — | la columna se descarta; las acciones salen de `rowActions` |

Filtros: `text`, `select`, `multi-select`, `date`, `number`, `boolean`/`checkbox`/`switch`;
`date-range` y `number-range` se degradan a `date` y `number`, y `autocomplete` a `select` (si hay
opciones) o `text`, con aviso.

## Ampliaciones del modelo

Los contratos de `src/contracts` no se tocan. Lo que el normalizador produce de más vive en
`src/config/types.ts` (`EntityConfigExt`, `FieldDefExt`, `FilterDefExt`, `SectionDefExt`) y es
estructuralmente compatible con ellos; `asExt(config)` devuelve la vista ampliada con valores por
omisión. Detalle en `docs/notas/T1.md`.

## Tabla de claves

<!-- compat:inicio (generado por src/config/compat.ts; no editar a mano) -->

### Raíz de la entidad (A1 §3.1)

Ámbito `root`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `id` | soportada | Identificador de la entidad; en un manifiesto manda el `id` del manifiesto. |
| `name` | soportada | Texto: literal, `<clave>Key` o objeto por idioma. |
| `namePlural` | soportada | Texto: literal, `<clave>Key` o objeto por idioma. Si falta, se usa `name`. |
| `description` | soportada | Texto: literal, `<clave>Key` o objeto por idioma. |
| `icon` | soportada | Nombre de icono; el registro de iconos (T3) lo traduce a Phosphor. |
| `color` | normalizada | Nombre de color -> tono (`green`->success, `danger`->error, `gray`->neutral, `blue`->info, `primary`->accent). Un hexadecimal se ignora con aviso y la entidad toma un tono por hash estable de su `id`. |
| `apiEndpoint` | normalizada | Pasa a `endpoint`. Sin versión si empieza por `~/` o `/api/` (rutas fuera de `/api/v1`) o si `apiVersioned` es `false`. |
| `apiBaseUrl` | ignorada | No la lee ni Angular; la base de la API sale del manifiesto (`api.baseUrl`). |
| `apiVersioned` | soportada | Extensión propuesta en `front/README.md`: `false` para rutas sin `/v1`. |
| `aggregateEndpoint` | soportada | El detalle carga este agregado (con `{id}`) en lugar de `{endpoint}/{id}`. |
| `editable` | soportada | `false`: sin alta, edición ni borrado. |
| `editType` | normalizada | `direct` -> `saveMode: optimistic`; `party` no se soporta (cambia a `form` con aviso). Lo manda `saveMode` si existe. |
| `saveMode` | soportada | Extensión de la superposición: `optimistic`, `batch` o `form`. |
| `presentation` | soportada | Extensión de la superposición: `panel` (por omisión) o `page`. |
| `permissions` | soportada | Extensión del manifiesto o la superposición: `{read, write}` con permisos `Contexto.Recurso.Acción`. |
| `primaryKey` | soportada | Por omisión `id`. |
| `displayField` | soportada | Por omisión, la primera columna de texto o `primaryKey`. |
| `companyScoped` | soportada | `false`: sin filtro de empresa. |
| `suppressCompanyColumn` | ignorada | React no inyecta una columna «Empresa»; el ámbito va en `X-Organization-Scope`. |
| `createViewType` | ignorada | No hay vistas a medida de alta; el alta usa el formulario. |
| `availableViews` | normalizada | `grid` -> vista de rejilla; `detail` va implícito; `tree` y `map` valen si `views[]` declara esa vista (la define `views[]`); `chart`, `history`, `kanban` y `calendar` no existen (aviso). |
| `defaultView` | normalizada | `grid`, `tree` o `map`; `detail`/`chart`/... caen a `grid`. |
| `searchable` | soportada | Activa la búsqueda rápida. |
| `searchFields` | soportada | Campos de la búsqueda rápida (informativo: busca el servidor). |
| `useInfiniteScroll` | ignorada | Muerta en Angular; el desplazamiento infinito es el de `dataSource.paginationStrategy`. |
| `actions` | ignorada | Muerta en Angular (no tiene lector); las acciones salen de `grid.rowActions` y `commands`. |
| `relations` | ignorada | Sólo precargaba configuraciones hijas; React las carga bajo demanda. |
| `hierarchy` | normalizada | `type: tree` con `parentField` -> vista `tree` (organigrama); el resto se ignora. |
| `emptyState` | soportada | Estado vacío: `{icon, title, message}` con textos por clave o literales. |
| `mobileView` | ignorada | La rejilla es adaptable por sí misma; no hay vista de tarjetas aparte. |
| `metadata` | ignorada (sin aviso) | Sólo informativa. |
| `childGridConfig` | ignorada | No la lee Angular; los hijos son `detail.childGrids`. |
| `dataSource` | normalizada | Ver «dataSource». |
| `grid` | normalizada | Ver «grid». |
| `filters` | normalizada | Ver «filters». |
| `form` | normalizada | Ver «form». |
| `detail` | normalizada | Ver «detail». |
| `charts` | ignorada | Las gráficas no forman parte de este front (aviso). |
| `commands` | soportada | Órdenes declarativas de `front/README.md` -> `CommandDef`. |
| `views` | soportada | Extensión de la superposición: vistas `tree` y `map`. |
| `help` | soportada | Extensión: pasos de la ayuda guiada (`[{target,title,body}]` o `{steps}`). |
| `serverSortFields` | soportada | Extensión de la superposición: `{campoDeColumna: campoDeOrdenDelServidor}` o una lista de campos que el servidor ordena con su propio nombre (`["code", "name"]`). |
| `$schema` | ignorada (sin aviso) | Referencia a un esquema JSON. |
| `nameKey`, `namePluralKey`, `descriptionKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### dataSource (A1 §3.2)

Ámbito `dataSource`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `type` | normalizada | `single`; `aggregate` y `enriched` se tratan como `single` con aviso. |
| `paginationStrategy` | normalizada | `server` o `client` -> `dataSource.pagination` (por omisión `client`). |
| `serverFiltering` | normalizada | Ver «serverFiltering». |
| `baseFilter` | soportada | Se funde en el `filter` de cada búsqueda. |
| `listMethod` | soportada | Extensión: `search` (POST `{ep}/search`) o `get`. |
| `listStyle` | soportada | Extensión: `maintenance` o `number-size` (por omisión según `apiVersioned`). |
| `fieldMapping` | ignorada (sin aviso) | Retirada del modelo de Angular (declaraba filtros que no filtraban). |
| `sources` | ignorada | Sólo `aggregate` (participant en C#). |
| `enrichment` | ignorada | Sólo `enriched` (C#). |
| `enrichedSearchEndpoint` | ignorada | Búsqueda enriquecida de C#; Go usa `/search`. |
| `include` | ignorada | Búsqueda enriquecida de C#. |
| `cursorSearchEndpoint` | ignorada | Paginación por cursor de C#. |
| `lookups` | ignorada | La resolución de claves ajenas es `lookupEntity` + `useLookupLabel`. |
| `pairSearch` | normalizada | Sólo `sortFields` (columna -> campo de orden de servidor). |
| `rowMapping` | ignorada | Renombrado de campos de C#; Go devuelve los nombres definitivos. |
| `chartSampleSize` | ignorada | Sólo gráficas. |

### dataSource.serverFiltering

Ámbito `serverFiltering`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `enabled` | ignorada (sin aviso) | Siempre `true` en Go. |
| `fieldMapping` | ignorada (sin aviso) | Retirada del modelo de Angular. |
| `clientOnlyFields` | ignorada (sin aviso) | Vacía en Go; React filtra en el servidor. |
| `companyFilterField` | soportada | Campo del `filter` que recibe las empresas seleccionadas. |

### dataSource.pairSearch

Ámbito `pairSearch`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `sortFields` | normalizada | Se convierte en `serverSortField` de las columnas. |

### grid (A1 §3.3)

Ámbito `grid`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `columns` | soportada | Ver «grid.columns[]». |
| `defaultSort` | soportada | `{field, direction}`. |
| `pageSize` | soportada | Tamaño de página (máximo 1000). |
| `pageSizeOptions` | soportada | Opciones del selector de tamaño. |
| `selectable` | soportada | — |
| `selectionMode` | soportada | `single` o `multiple`. |
| `rowActions` | normalizada | Cada acción `view`/`edit`/`delete` (o el id de una orden) -> `CommandDef`. |
| `multiSort` | ignorada | Muerta en Angular. |
| `editable` | ignorada | Muerta en Angular; qué se edita sale del formulario y del detalle. |
| `showRowNumbers` | ignorada | Muerta en Angular. |
| `resizableColumns` | ignorada | Muerta en Angular; las columnas se redimensionan siempre. |
| `reorderableColumns` | ignorada | Muerta en Angular. |
| `exportable` | ignorada | Muerta en Angular; la exportación no está en esta versión. |
| `exportFormats` | ignorada | Muerta en Angular. |
| `height` | ignorada | Muerta en Angular; la rejilla ocupa el espacio disponible. |
| `virtualScroll` | ignorada | Muerta en Angular; la virtualización es siempre. |
| `emptyMessage` | normalizada | Pasa a `emptyState.message`. |

### grid.columns[] (A1 §3.3)

Ámbito `column`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `field` | soportada | Alias `dataField`. |
| `dataField` | normalizada | Alias de `field`. |
| `header` | normalizada | Pasa a `label` (alias `label`). |
| `label` | normalizada | Alias de `header`. |
| `type` | normalizada | Ver la tabla de tipos de campo; un tipo desconocido se degrada a `text` con aviso. |
| `width` | normalizada | Píxeles -> `widthPx`. |
| `minWidth` | ignorada | La rejilla fija su mínimo. |
| `maxWidth` | ignorada | La rejilla fija su máximo. |
| `flex` | ignorada | No modelada en Angular. |
| `sortable` | normalizada | `sortable`; `serverSortField` sólo si el servidor lo admite (lista blanca de A2 §2, `serverSortFields` o `pairSearch.sortFields`). Si no, se ordena en cliente la página cargada (aviso). |
| `filterable` | ignorada | Los filtros son los de `filters[]`. |
| `editable` | soportada | Por omisión sale del formulario o del detalle (campo editable y no inmutable). |
| `visible` | normalizada | `false` -> `hidden`. |
| `sticky` | normalizada | `true`/`left` -> `pinned: start`; `right` -> `pinned: end`. |
| `format` | soportada | — |
| `currency` | soportada | Código de divisa de `money`. |
| `currencyCode` | normalizada | Alias de `currency`. |
| `enumValues` | normalizada | Se funde con `badgeMap` en `options`. |
| `badgeMap` | normalizada | Claves en cadena (`"true"`); se funde con `enumValues`; comparación tolerante. |
| `options` | soportada | Opciones (cadenas u objetos). |
| `linkConfig` | ignorada | Las rutas de Angular no existen; `link` se trata como `url`. |
| `align` | ignorada | La alineación sale del tipo de campo y de `dir`. |
| `tooltip` | normalizada | Pasa a `hint` si no hay `hint`. |
| `tooltipField` | ignorada | No hay tooltip por fila. |
| `cssClass` | ignorada | Sin clases de aspecto: el aspecto sale del tema. |
| `template` | ignorada | Sin plantillas HTML en cadena. |
| `emptyValue` | soportada | Texto de la celda vacía. Ausente, o hecho sólo de guiones (`-`, `–`, `—`), se pinta como «—» (vacío normalizado, R3-3); cualquier otro texto se respeta. |
| `trueLabel` | soportada | — |
| `falseLabel` | soportada | — |
| `hint` | soportada | — |
| `progressConfig` | ignorada | Tipo `progress` no soportado. |
| `trendConfig` | ignorada | Tipo `trend` no soportado. |
| `ratingConfig` | ignorada | Tipo `rating` no soportado. |
| `sparklineConfig` | ignorada | Tipo `sparkline` no soportado. |
| `lookupEntity` | soportada | Convierte la columna en `lookup`. |
| `lookupDisplayField` | soportada | Por omisión `name`. |
| `lookupValueField` | soportada | Por omisión `id`. |
| `lookupFilter` | soportada | Filtro fijo de la búsqueda de la clave ajena. |
| `lookup` | soportada | Forma objeto de los tres anteriores: `{entity, valueField, displayField, filter}`. |
| `required` | soportada | — |
| `pinned` | soportada | Forma del modelo interno (`start`/`end`). |
| `priority` | soportada | Extensión opcional de Karpo React: entero, mayor = más importante; con poco ancho se ocultan primero las columnas de menor prioridad (sin ella, de derecha a izquierda). La configuración de Angular la ignora. |
| `headerKey`, `labelKey`, `hintKey`, `tooltipKey`, `trueLabelKey`, `falseLabelKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### columns[].enumValues[] y badgeMap[valor]

Ámbito `enumValue`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `value` | normalizada | Se guarda como cadena. |
| `label` | soportada | — |
| `color` | normalizada | Nombre de color -> `tone`; un hexadecimal se ignora con aviso. |
| `tone` | soportada | Forma del modelo interno. |
| `icon` | soportada | — |
| `cssClass` | ignorada | Sin clases de aspecto. |
| `labelKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### grid.rowActions[] (A1 §3.3)

Ámbito `rowAction`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `action` | normalizada | `view`, `edit`, `delete` o el `id` de una orden de `commands`; otro valor se descarta con aviso. |
| `label` | soportada | — |
| `icon` | soportada | — |
| `color` | normalizada | Nombre de color -> `tone`. |
| `visibleWhen` | normalizada | Cadena o regla -> `VisibilityRule`. |
| `confirm` | normalizada | `true` -> pregunta con `confirmMessage` o con la pregunta por omisión. |
| `confirmMessage` | normalizada | Texto de la pregunta de `confirm`. |
| `labelKey`, `confirmMessageKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### filters[] (A1 §3.4)

Ámbito `filter`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `field` | soportada | `_search` o `search` = búsqueda rápida. |
| `serverField` | soportada | Por omisión `search` (búsqueda rápida) o `field`. |
| `label` | soportada | — |
| `type` | normalizada | `checkbox` -> `boolean`; `multi-select` -> `multiselect`; `number` se soporta; `date-range`, `number-range` y `autocomplete` se degradan con aviso. |
| `placeholder` | soportada | — |
| `icon` | soportada | — |
| `width` | soportada | Columnas 1 a 12. |
| `options` | soportada | Cadenas u objetos `{value,label,color,icon}`. |
| `defaultValue` | soportada | — |
| `hint` | soportada | — |
| `visibleWhen` | normalizada | Cadena o regla. |
| `searchFields` | ignorada | Sólo filtrado en cliente de Angular; busca el servidor. |
| `optionsEndpoint` | ignorada | Opciones dinámicas no soportadas todavía. |
| `optionsValueField` | ignorada | Con `optionsEndpoint`. |
| `optionsLabelField` | ignorada | Con `optionsEndpoint`. |
| `required` | ignorada | Un filtro no es obligatorio. |
| `operator` | ignorada | Go no tiene operadores (A2 §2). |
| `dynamic` | ignorada | Opciones derivadas del conjunto cargado. |
| `dependsOn` | ignorada | Filtros en cascada no soportados. |
| `featureFlag` | ignorada | Regla legada sin equivalente en Go. |
| `serverValueFormat` | ignorada | Go no admite multivalor (A2 §2). |
| `headerButton` | ignorada | Todos los filtros van en la barra. |
| `queryParam` | ignorada | Parámetro de GET sin paginar de C#. |
| `labelKey`, `placeholderKey`, `hintKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### options[] de filtros y campos

Ámbito `option`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `value` | normalizada | Se guarda como cadena. |
| `label` | soportada | Si falta, el valor. |
| `icon` | soportada | — |
| `color` | normalizada | Nombre de color -> `tone`. |
| `tone` | soportada | — |
| `disabled` | ignorada | No hay opciones deshabilitadas. |
| `count` | ignorada | No hay contadores. |
| `parentValue` | ignorada | Cascadas no soportadas. |
| `labelKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### form (A1 §3.5)

Ámbito `form`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `sections` | soportada | — |
| `groups` | normalizada | Alias de `sections`. |
| `generated` | normalizada | Sin secciones: se genera el formulario con las columnas editables. |
| `create` | soportada | `false`: sin alta. |
| `edit` | soportada | `false`: sin edición por formulario. |
| `createTitle` | soportada | — |
| `editTitle` | soportada | — |
| `modalSize` | ignorada | El detalle es un panel o una página, no un modal. |
| `saveButtonText` | ignorada | Los textos de los botones salen de i18n. |
| `cancelButtonText` | ignorada | Los textos de los botones salen de i18n. |
| `showCancel` | ignorada | Siempre hay cancelar. |
| `createVisibleWhen` | ignorada | Usa `permissions.write`. |
| `createRouting` | ignorada | Alta por tipo de C#; no soportada. |
| `createRoute` | ignorada | Alta por ruta de C#; no soportada. |
| `createTitleKey`, `editTitleKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### form.sections[] y detail.sections[] (A1 §3.5, §3.6)

Ámbito `section`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `id` | soportada | Por omisión `section-N`. |
| `title` | soportada | Alias `label`. |
| `label` | normalizada | Alias de `title`. |
| `description` | soportada | — |
| `icon` | soportada | — |
| `columns` | soportada | Columnas de la sección, de 1 a 4 (Angular lo ignoraba en el formulario). |
| `collapsible` | soportada | — |
| `collapsed` | soportada | — |
| `fields` | soportada | — |
| `visibleWhen` | normalizada | Cadena o regla. |
| `apiEndpoint` | ignorada | Secciones con endpoint propio no soportadas (aviso). |
| `upsertMethod` | ignorada | Con `apiEndpoint` de sección. |
| `upsertParentKey` | ignorada | Con `apiEndpoint` de sección. |
| `titleKey`, `labelKey`, `descriptionKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### form.sections[].fields[] (A1 §3.5)

Ámbito `formField`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `field` | soportada | Alias `dataField`. |
| `dataField` | normalizada | Alias de `field`. |
| `label` | soportada | — |
| `type` | normalizada | `checkbox` y `switch` -> `boolean`; `multi-select` -> `multiselect`; `guid-select` y `autocomplete` con entidad -> `lookup`; `radio` -> `select`; desconocido -> `text` con aviso. |
| `placeholder` | soportada | — |
| `defaultValue` | soportada | Alias `default`. |
| `default` | normalizada | Alias de `defaultValue`. |
| `required` | soportada | — |
| `readonly` | normalizada | `true` -> `editable: false`. |
| `disabled` | normalizada | `true` -> `editable: false`. |
| `editable` | soportada | `false` -> sólo lectura. |
| `hidden` | normalizada | `true`: el campo se omite. |
| `readonlyOnEdit` | normalizada | -> `immutable` (editable en el alta, de sólo lectura después). |
| `createOnly` | normalizada | -> `immutable`. |
| `immutable` | soportada | — |
| `visibleWhen` | normalizada | Cadena (`campo == valor`) o regla. |
| `disabledWhen` | ignorada | Muerta en Angular. |
| `width` | soportada | Columnas 1 a 12 dentro de la sección. |
| `options` | soportada | Cadenas u objetos. |
| `optionsEndpoint` | ignorada | Usa `lookupEntity` para claves ajenas. |
| `optionsSearch` | ignorada | Usa `lookupEntity`. |
| `optionsValueField` | ignorada | Con `optionsEndpoint`. |
| `optionsLabelField` | ignorada | Con `optionsEndpoint`. |
| `optionsExtraQueryParams` | ignorada | Con `optionsEndpoint`. |
| `requiredWhenMultipleOptions` | ignorada | Regla de C# sobre opciones cargadas. |
| `validators` | normalizada | Los tipos `required`, `min`, `max`, `minLength`, `maxLength` y `pattern` pasan a sus propiedades planas (Angular no los leía). |
| `validation` | ignorada | Sin equivalente. |
| `hint` | soportada | Se pinta como ayuda del campo. |
| `prefix` | ignorada | Sin prefijos de campo. |
| `suffix` | ignorada | Sin sufijos de campo. |
| `rows` | ignorada | El `textarea` se ajusta solo. |
| `min` | soportada | — |
| `max` | soportada | — |
| `minLength` | soportada | — |
| `maxLength` | soportada | — |
| `pattern` | soportada | Expresión regular; si no compila se ignora con aviso. |
| `format` | soportada | — |
| `currency` | soportada | — |
| `currencyCode` | normalizada | Alias de `currency`. |
| `trueLabel` | soportada | — |
| `falseLabel` | soportada | — |
| `lookupEntity` | soportada | Convierte el campo en `lookup`: selector con búsqueda. |
| `lookupDisplayField` | soportada | Por omisión `name`. |
| `lookupValueField` | soportada | Por omisión `id`. |
| `lookupFilter` | soportada | — |
| `lookup` | soportada | Forma objeto de los cuatro anteriores. |
| `labelKey`, `hintKey`, `placeholderKey`, `trueLabelKey`, `falseLabelKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### detail (A1 §3.6)

Ámbito `detail`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `sections` | soportada | — |
| `tabs` | normalizada | Se aplanan: las secciones de cada pestaña pasan a `sections` y su `childGrid` a `childGrids` (aviso por `component`). |
| `childGrids` | soportada | — |
| `showHistory` | ignorada (sin aviso) | Declarada siempre en Go, sin efecto visible en Angular. |
| `showRelations` | ignorada (sin aviso) | Declarada siempre en Go, sin efecto visible en Angular. |
| `showTags` | ignorada (sin aviso) | Declarada siempre en Go, sin efecto visible en Angular. |
| `headerActions` | ignorada | No se pintaba en Angular; las órdenes de la cabecera son `commands` con la superficie `detail-header`. |
| `redirectToSource` | ignorada | Sin equivalente. |

### detail.sections[].fields[] (A1 §3.6)

Ámbito `detailField`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `field` | soportada | Alias `dataField`. |
| `dataField` | normalizada | Alias de `field`. |
| `label` | soportada | — |
| `type` | normalizada | `checkbox` -> `boolean`; `currency` -> `money`; `lookup`, `decimal` y `textarea` se soportan (Angular los trataba como texto); desconocido -> `text` con aviso. |
| `options` | soportada | — |
| `icon` | ignorada | Sin icono por campo. |
| `copyable` | ignorada | Sin botón de copiar. |
| `required` | soportada | — |
| `editable` | soportada | Por omisión `true`. |
| `readonly` | normalizada | `true` -> `editable: false`. |
| `hidden` | normalizada | `true`: el campo se omite. |
| `badgeType` | ignorada | El tono sale de `options`. |
| `placeholder` | soportada | — |
| `format` | soportada | — |
| `currency` | soportada | — |
| `currencyCode` | normalizada | Alias de `currency`. |
| `maxLength` | soportada | — |
| `minLength` | soportada | — |
| `min` | soportada | — |
| `max` | soportada | — |
| `pattern` | soportada | — |
| `hint` | soportada | — |
| `trueLabel` | soportada | — |
| `falseLabel` | soportada | — |
| `visibleWhen` | normalizada | Cadena o regla. |
| `validation` | ignorada | Sin equivalente. |
| `optionsEndpoint` | ignorada | Usa `lookupEntity`. |
| `defaultValue` | soportada | — |
| `immutable` | soportada | — |
| `readonlyOnEdit` | normalizada | -> `immutable`. |
| `createOnly` | normalizada | -> `immutable`. |
| `width` | soportada | — |
| `lookupEntity` | soportada | — |
| `lookupDisplayField` | soportada | — |
| `lookupValueField` | soportada | — |
| `lookupFilter` | soportada | — |
| `lookup` | soportada | — |
| `badgeMap` | normalizada | Igual que en columnas. |
| `enumValues` | normalizada | Igual que en columnas. |
| `labelKey`, `hintKey`, `placeholderKey`, `trueLabelKey`, `falseLabelKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### detail.tabs[]

Ámbito `tab`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `id` | soportada | — |
| `label` | soportada | — |
| `icon` | ignorada | Sin icono de pestaña. |
| `sections` | soportada | — |
| `childGrid` | soportada | — |
| `component` | ignorada | Componentes a medida de Angular (aviso). |
| `labelKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### detail.childGrids[] (A1 §3.6)

Ámbito `childGrid`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `id` | soportada | — |
| `label` | normalizada | Pasa a `title` si no hay `title`. |
| `title` | soportada | — |
| `icon` | ignorada | Sin icono de pestaña. |
| `entityId` | normalizada | Sin `apiEndpoint`, el endpoint es `/{entityId}s` (aviso). |
| `apiEndpoint` | normalizada | Se sustituye `{id}` y los `{campo}` con la fila del padre; no se añade `?foreignKey=` si ya lleva `{id}`. |
| `foreignKey` | normalizada | Sólo si el endpoint no lleva `{id}`: se añade `?<foreignKey>={id}`. |
| `dataField` | ignorada | Datos en línea del registro padre: no soportado (aviso; el hijo se descarta). |
| `field` | ignorada | Datos en línea del registro padre: no soportado (aviso; el hijo se descarta). |
| `columns` | soportada | — |
| `emptyMessage` | soportada | — |
| `pageSize` | ignorada | Los hijos son tablas de sólo lectura sin paginar. |
| `visibleWhen` | ignorada | Sin regla por hijo. |
| `rowActions` | ignorada | Los hijos son de sólo lectura. |
| `labelKey`, `titleKey`, `emptyMessageKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### detail.childGrids[].columns[]

Ámbito `childColumn`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `field` | soportada | — |
| `header` | normalizada | Pasa a `label`. |
| `label` | normalizada | Alias de `header`. |
| `type` | normalizada | Igual que en columnas. |
| `width` | normalizada | Píxeles -> `widthPx`. |
| `minWidth` | ignorada | — |
| `emptyValue` | soportada | — |
| `trueLabel` | soportada | — |
| `falseLabel` | soportada | — |
| `format` | soportada | — |
| `headerKey`, `labelKey`, `trueLabelKey`, `falseLabelKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### commands[] (front/README.md)

Ámbito `command`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `id` | soportada | — |
| `label` | soportada | — |
| `icon` | soportada | — |
| `color` | normalizada | Nombre de color -> `tone`. |
| `tone` | soportada | Tono explícito (`success`, `error`, `warning`, `info`, `neutral`, `accent`); manda sobre `color`. |
| `surfaces` | soportada | `row`, `detail-header`, `toolbar`; por omisión `row` y `detail-header`. |
| `visibleWhen` | normalizada | Cláusula sobre un campo de la fila (`{field, equals}`) u otra regla. |
| `method` | soportada | `POST` (por omisión), `PUT` o `DELETE`. |
| `endpoint` | soportada | Con `{id}` y `{campo}` sustituidos con la fila. |
| `confirm` | normalizada | Cadena, objeto por idioma o `true`. |
| `confirmMessage` | soportada | Texto de la pregunta cuando `confirm` es `true`. |
| `prompt` | soportada | Diálogo con un texto (motivo) que viaja en el cuerpo. |
| `sendVersion` | soportada | Añade `version` de la fila al cuerpo. |
| `successMessage` | soportada | — |
| `acceptedMessage` | soportada | Mensaje de «pendiente de aprobación» (202). |
| `labelKey`, `confirmKey`, `confirmMessageKey`, `successMessageKey`, `acceptedMessageKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### commands[].prompt

Ámbito `prompt`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `field` | soportada | Nombre del campo del cuerpo (por ejemplo `reason`). |
| `label` | soportada | — |
| `required` | soportada | — |
| `maxLength` | soportada | — |
| `labelKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### hierarchy (A1 §3.1)

Ámbito `hierarchy`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `type` | normalizada | `tree` -> vista de organigrama. |
| `parentField` | normalizada | Pasa a `parentField` de la vista. |
| `typeField` | ignorada | Sin tipo de nodo. |
| `multiSelect` | ignorada | Sin selección múltiple de nodos. |
| `showCounts` | ignorada | El contador de descendientes es siempre visible. |
| `levels` | ignorada | Sin niveles con nombre. |

### emptyState

Ámbito `emptyState`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `icon` | soportada | — |
| `title` | soportada | — |
| `message` | soportada | — |
| `titleKey`, `messageKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### views[] con `kind: tree` (extensión)

Ámbito `viewTree`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `kind` | soportada | — |
| `label` | ignorada | El conmutador de vista usa los textos de la interfaz (`core:view.*`), no uno por entidad. |
| `labelKey` | ignorada | Igual que `label`. |
| `parentField` | soportada | Obligatoria. |
| `labelField` | soportada | Obligatoria (por omisión `displayField`). |
| `subtitleField` | soportada | — |
| `badgeField` | soportada | — |
| `source` | soportada | `{type: flat}` o `{type: chart, endpoint, rootsFrom, depth, childrenField, nodeField}`. |

### views[] con `kind: map` (extensión)

Ámbito `viewMap`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `kind` | soportada | — |
| `label` | ignorada | El conmutador de vista usa los textos de la interfaz (`core:view.*`), no uno por entidad. |
| `labelKey` | ignorada | Igual que `label`. |
| `boundaryField` | soportada | Obligatoria. |
| `levels` | soportada | Obligatoria: `[{id, label, boundaryLevel, codeProperty, source}]`. |
| `aggregate` | soportada | `count` (por omisión) o `sum`. |
| `valueField` | soportada | Con `sum`. |
| `skipSingle` | soportada | Un nivel con una sola delimitación se salta al navegar (Q-10). Por omisión `true`; `false` conserva ese nivel. |

### views[].levels[] del mapa

Ámbito `mapLevel`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `id` | soportada | `world`, `country`, `region`, `province` o `city`. |
| `label` | soportada | — |
| `boundaryLevel` | soportada | — |
| `codeProperty` | soportada | — |
| `source` | soportada | Id en `manifest.map.sources`. |
| `defaultCode` | soportada | Delimitación que se abre por omisión en el nivel (`null`: la raíz). |
| `labelKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### help[] (extensión)

Ámbito `helpStep`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `target` | soportada | Ancla `data-kx-help` o selector CSS. |
| `title` | soportada | — |
| `body` | soportada | — |
| `titleKey`, `bodyKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### manifest.json (PLAN §4.3.1)

Ámbito `manifest`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `schemaVersion` | soportada | Debe ser 1. |
| `api` | soportada | `{baseUrl, apiVersion}`; por omisión `/api` y `v1`. |
| `locales` | soportada | Por omisión `["es","en"]`. |
| `defaultLocale` | soportada | Por omisión el primero de `locales`. |
| `i18n` | soportada | `{idioma: url}` del diccionario de la configuración (relativa al manifiesto). |
| `theme` | soportada | `{origin, default, available}`. |
| `map` | soportada | `{sources: {id: {type, url, layer?, schematic?}}}`. |
| `entities` | soportada | Lista de `{id, path \| config, permissions, overlay}`. |
| `menu` | soportada | Árbol de `{id, label \| labelKey, icon, entity, children, visibleWhen}`. |
| `$schema` | ignorada (sin aviso) | — |

### manifest.entities[]

Ámbito `manifestEntity`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `id` | soportada | — |
| `path` | soportada | Relativa a la carpeta del manifiesto. |
| `config` | soportada | Configuración en línea, en lugar de `path`. |
| `permissions` | soportada | `{read, write}`. |
| `overlay` | soportada | Se funde en profundidad ANTES de normalizar. |
| `overlayPath` | soportada | Superposición en un fichero JSON (relativo al manifiesto): se funde antes que `overlay` y sirve para las superposiciones generadas (textos con `*Key` de `npm run i18n:demo`). Si no se puede leer, aviso `warn` y la entidad se carga sin ella. |

### manifest.menu[]

Ámbito `manifestMenu`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `id` | soportada | — |
| `label` | soportada | — |
| `icon` | soportada | — |
| `entity` | soportada | — |
| `children` | soportada | — |
| `visibleWhen` | normalizada | — |
| `labelKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### index.json de Angular (A1 §2.1)

Ámbito `index`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `version` | ignorada (sin aviso) | — |
| `description` | ignorada (sin aviso) | — |
| `entities` | soportada | `{id: ruta}` relativa al índice. |
| `domains` | ignorada | Sólo servían a `getEntitiesByDomain()`. |

### sidebar-config.json: contextos (A1 §3.9)

Ámbito `sidebarContext`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `id` | soportada | — |
| `label` | soportada | — |
| `icon` | soportada | — |
| `items` | soportada | — |
| `labelKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### sidebar-config.json: ítems de navegación (A1 §3.9)

Ámbito `sidebarItem`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `id` | soportada | — |
| `label` | soportada | — |
| `icon` | soportada | — |
| `children` | soportada | — |
| `entityId` | normalizada | -> `entity`. |
| `viewType` | ignorada | Las vistas se eligen en la entidad. |
| `type` | normalizada | `folder` y `document` se tratan igual; `company` y `company-filter` no existen (el ámbito es un selector aparte). |
| `selectable` | ignorada | — |
| `requiresCompanySelection` | ignorada | — |
| `externalUrl` | ignorada | Los ítems con iframe externo se descartan (aviso). |
| `embedSupported` | ignorada | — |
| `badge` | ignorada | — |
| `roles` | ignorada | Regla legada sin equivalente en Go (aviso). |
| `sector` | ignorada | Regla legada sin equivalente en Go (aviso). |
| `capability` | ignorada | Regla legada sin equivalente en Go (aviso). |
| `featureFlag` | ignorada | Regla legada sin equivalente en Go (aviso). |
| `visibleWhen` | normalizada | Sólo se conservan `permission`, `field`/`equals`, `expr`, `allOf`, `anyOf`, `not`; las cláusulas legadas se descartan con aviso. |
| `quickAccess` | ignorada | — |
| `labelKey` | soportada | Clave de i18n de la propiedad de texto correspondiente. |

### visibleWhen (A1 §3.8)

Ámbito `visibleWhen`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `permission` | soportada | Con comodines `*.*.*`; fail-closed. |
| `expr` | soportada | `campo == valor` o `campo != valor`. |
| `field` | soportada | Con `equals`; sin él, el campo ha de ser «verdadero». |
| `equals` | soportada | Comparación tolerante (`true` == `"true"`). |
| `allOf` | soportada | — |
| `anyOf` | soportada | — |
| `not` | soportada | — |
| `hasRole` | ignorada | Rol del registro: sin equivalente (la cláusula se descarta con aviso). |
| `capability` | ignorada | Capacidad del tenant: sin equivalente en Go. |
| `sector` | ignorada | Sector del tenant: sin equivalente en Go. |
| `userRole` | ignorada | Rol de usuario de C#: usa `permission`. |
| `requiredModule` | ignorada | Módulo activo: sin equivalente. |
| `featureFlag` | ignorada | Sin banderas de funcionalidad. |
| `roles` | ignorada | Regla legada. |
| `accessMode` | ignorada | Siempre `open`. |

### runtime-config.json de Angular (A1 §3.10)

Ámbito `runtimeConfig`.

| Clave | Estado | Cómo se trata |
| --- | --- | --- |
| `apiUrl` | ignorada | La base de la API sale del manifiesto (`api.baseUrl`) o de `apiBaseUrl`. |
| `authUrl` | ignorada | Las rutas de autenticación cuelgan de `api.baseUrl`. |
| `apiVersion` | ignorada | Sale del manifiesto (`api.apiVersion`). |
| `featureFlags` | ignorada | Sin banderas de funcionalidad. |

<!-- compat:fin -->

## Sin soporte (fuera de alcance)

- **Gráficas** (`charts`, `availableViews: chart`), **historial** y vistas **kanban** y **calendario**:
  Go no las usa y el análisis las deja fuera.
- **`dataSource` `aggregate` y `enriched`**, búsqueda por cursor y enriquecida: son de C#; Go sólo usa
  `single` con `paginationStrategy: server` (`POST {ep}/search`) o `client`.
- **`editType: party`** (`POST /parties/rename` y `/toggle`): se guarda por formulario con `PUT`.
- **Secciones del detalle con `apiEndpoint` propio**, hijos con datos en línea (`dataField`) y
  componentes a medida de pestañas: se avisa y se omiten.
- **Colores fijos** (`color` hexadecimal, `colors[]`): el color sale de los tonos del tema.
