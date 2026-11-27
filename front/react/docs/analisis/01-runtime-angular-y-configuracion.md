# 01 · El runtime Angular de Karpo C# y su configuración por entidad

Análisis previo al front React de Karpo Go. Fecha: 2026-11-27. Papel: analista (sólo lectura de los
repositorios de origen).

**Fuentes leídas**

- Runtime: `C:\Git\Paranoia\Karpo\010-Front\Angular` (Angular 20, standalone, signals, zoneless,
  AG Grid Community 34, ECharts 6). Se han leído los modelos de configuración
  (`src/app/shared/models/entity-config/*`), los servicios del núcleo (`core/`), la rejilla
  (`views/grid`), el detalle (`views/detalle`), los modales, los filtros, el patrón de comandos y
  `public/` (índice, sidebar, runtime-config).
- Configuración de Go: `front/angular/config/entities/**` de este repositorio (8 ficheros).
- Configuración completa de C#: `010-Front/Angular/public/config/entities/**` (148 configuraciones
  de entidad más `index.json`). Se ha hecho un censo de claves de los 149 JSON para saber qué se
  usa de verdad.
- Contrato del lado de Go: `docs/PRODUCTOS.md` («El contrato de mantenimiento»), `front/README.md`,
  `contexts/hr/maintenance.go`, `contexts/security/distribution/http.go`.

Convención: «verificado» = leído en el código; «inferido» = deducido sin ejecutarlo. No se ha
arrancado ningún proceso ni el runtime Angular.

---

## 0. Resumen ejecutivo

1. El runtime es una app **metadata-driven**: una pantalla = un `EntityConfig` JSON. Hay cuatro
   vistas por entidad (rejilla, detalle, gráficos, historial) más vistas a medida. El JSON declara
   columnas, filtros, formulario, detalle, hijos, gráficos y el contrato de datos.
2. La configuración **no tiene esquema formal**: son interfaces TypeScript a mano
   (`entity-config/*.ts`), sin JSON Schema. Y el modelo, el código y los JSON **divergen**: hay
   claves declaradas que nadie lee (`hierarchy`, `showCondition`, `fieldMapping`), claves que los
   JSON usan y el modelo no declara (`badgeMap`, `lookupEntity`, `commands`…) y tipos de campo que
   se degradan en silencio a texto (`checkbox`, `lookup`, `decimal`).
3. **Los 8 configs de Go chocan con el runtime en puntos concretos** (§5): los filtros `checkbox` y
   `number` no se pintan; `badgeMap` se ignora; los campos `checkbox` del detalle se muestran y
   editan como texto; las claves ajenas son texto libre. El login de Go no es el que el runtime
   espera (§7.4).
4. **Edición en rejilla**: el «modo edición» de la rejilla **no guarda nada en el servidor**
   (sólo confirma cambios en memoria). El doble clic abre el detalle en modo lectura. Es la mayor
   carencia frente a lo que pide Javier.
5. No hay **ayuda guiada**, ni **organigrama/árbol**, ni **mapas**, ni **i18n de configuración**
   (los textos del JSON son español literal; sólo la «cáscara» usa `$localize` en compilación).
   El historial es un placeholder. `hierarchy` está declarado y sin implementar.
6. Para React lo importante es: leer el mismo JSON (compatible hacia delante), **normalizar** las
   inconsistencias en una capa de carga con validación (JSON Schema/zod), y subir el listón en lo
   que el runtime hace mal (§11 y §12).

---

## 1. Arquitectura del runtime

| Capa | Dónde | Qué hace |
|---|---|---|
| Arranque | `app.config.ts`, `main.ts` | Zoneless, router, `HttpClient` con 5 interceptores, service worker (sólo producción), registro de comandos, precarga de organizaciones accesibles si hay sesión |
| Rutas | `app.routes.ts` | Sólo `/login`, `''` (guardado por `authGuard`) y `admin/capability-assignments`. **Las entidades no tienen ruta**: todo se abre en el «workarea» |
| Shell | `layout/` | `sidebar` (menú por contextos), `topbar`, `workarea` (pestañas + paneles divididos), `terminal` (consola de desarrollo), `login`, `register`, `splash` |
| Vistas | `views/` | `grid`, `detalle`, `grafico`, `historico` (placeholder), `external-tool` (iframe), `default`, y a medida (`reservation-*`, `database`, `admin-preferences`, `capability-admin`) |
| Núcleo | `core/` | Servicios de configuración, datos, auth, navegación, comandos, resiliencia, UI |
| Compartido | `shared/` | Modelos, modales, filtros, detalle, renderers, pipes, estilos |

**Estado de navegación = estado de aplicación, no URL.** `WorkareaService` mantiene paneles y
pestañas (máximo 8 por panel, arrastrables, vista dividida horizontal/vertical). `DeepLinkService`
sincroniza una parte con la URL: `/?entity=…&view=grid|grafico|detalle|historico&record=…&tab=…`.
No hay rutas tipo `/products/123`; los enlaces profundos son *query params*.

`VIEW_TYPES` (workarea): `default, grid, grafico, detalle, historico, external, database,
admin-preferences, capability-admin, reservation-wizard, reservation-dashboard, reservation-list`.
Una vista «a medida» se abre sin `entityId` (`isStandaloneView`).

Tecnología relevante para licencias: AG Grid **Community** (MIT), ECharts (Apache-2.0), `xlsx`
0.18.5 (Apache-2.0), jsPDF (MIT), `docx` (MIT, sólo scripts). React no puede usar AG Grid
Enterprise ni Handsontable; hay que construir (o elegir con licencia permitida) la rejilla.

---

## 2. Cómo se carga y se guarda la configuración

### 2.1 Ficheros y registro

- Raíz: `public/config/entities/` (servida estática; ruta fija `config/entities` en
  `EntityConfigService.configBasePath`, aunque `environment.entityConfigPath` existe).
- **`index.json`** es el registro: `{ version, description, entities: { <id>: "<carpeta>/<fichero>.config.json" }, domains: { <dominio>: [ids] } }`.
  Hoy mapea ~150 ids. Los `domains` sólo sirven a `getEntitiesByDomain()`.
- Si `index.json` no existe o el id no está, la ruta por defecto es `config/entities/<id>.config.json`
  **sin subcarpeta** (compatibilidad). Los ficheros de Go viven en subcarpetas
  (`assets/`, `shipments/`…): sin entrada en el índice **no se encuentran**.
- Otros ficheros de `public/`: `sidebar-config.json` (menú), `runtime-config.json`
  (`apiUrl`, `authUrl`, `apiVersion`, `featureFlags`), `theme-loader.js`, `manifest.webmanifest`,
  `empleados.json` (datos de prueba).

### 2.2 Carga

`EntityConfigService.getConfig(id)`:

1. Mira la caché (Map con TTL de **10 min**) y las cargas en vuelo (no duplica peticiones).
2. `GET config/entities/index.json` (una vez, perezoso) → ruta del id.
3. `GET config/entities/<ruta>`.
4. **Valida lo mínimo**: existe, `id`, `name`, `apiEndpoint`, `grid.columns` no vacío,
   `availableViews` no vacío. Nada más: ni tipos, ni valores admitidos, ni claves desconocidas.
5. Cachea. `preloadConfigs`, `registerConfig` (tests), `clearConfig`.

Un JSON inválido en otra parte (un `type` desconocido) **no se detecta**: se degrada en silencio en
el punto de uso (§4.3). No hay avisos de claves ignoradas.

### 2.3 Quién la consume

- El **menú** (`sidebar-config.json`) referencia entidades por `entityId`.
- Al abrir un ítem se crea una pestaña (`viewType: 'grid'` por defecto) y `GridViewComponent`
  pide la config. El doble clic crea/cambia la pestaña de detalle pasando `entityConfig`,
  `recordId` y `recordData`.
- Modales de alta/edición (`ConfigDrivenFormService`), filtros (`FilterContainerComponent`),
  gráficos (`grafico/`), hijos (`detail-child-grid`) leen de la misma config.

---

## 3. Esquema completo de `EntityConfig`

Fuente: `shared/models/entity-config/*.ts`. Los campos marcados «†» **no se leen en ningún sitio**
(declarados y muertos, verificado por búsqueda). Los marcados «‡» aparecen en los JSON pero **no**
en el modelo.

### 3.1 Raíz

| Clave | Tipo | Significado |
|---|---|---|
| `id` * | string | Identificador único. Es el de `index.json` y el de `entityId` en menú |
| `name` * / `namePlural` | string | Singular / plural, textos literales (sin clave i18n) |
| `description` | string | |
| `icon` | string | Nombre del catálogo interno de iconos (`shared/pipes/icons.config.ts`, ~250 SVG en línea) |
| `color` | hex | Color propio de la entidad (valor fijo) |
| `apiEndpoint` * | string | Ruta bajo `apiUrl` + `/<apiVersion>` (`/products` → `/api/v1/products`). Si empieza por `http(s)://` va tal cual |
| `apiBaseUrl` ‡ | string | Aparece en 12 configs; no se lee |
| `aggregateEndpoint` | string | Con `{id}`: el detalle carga este agregado en vez de `apiEndpoint/{id}` |
| `editable` | bool | `false` = sin alta, edición ni borrado en la UI. Ausente = editable |
| `editType` | `'party'`\|`'direct'` | Cómo se guarda: `party` → `POST /parties/rename` y `/parties/toggle`; `direct` → `PUT apiEndpoint/{id}` con el registro entero |
| `primaryKey`, `displayField` | string | Clave y campo-título (por defecto `id`) |
| `companyScoped` | bool | `false` = sin columna/filtro de empresa |
| `suppressCompanyColumn` | bool | Oculta sólo la columna «Empresa» inyectada |
| `createViewType` | string | El «Nuevo» abre una vista a medida en vez del modal |
| `availableViews` * | `('grid'\|'chart'\|'detail'\|'history'\|'kanban'\|'calendar')[]` | `kanban` y `calendar` están en el tipo **sin implementar**; en los JSON de Go sólo `grid`,`detail` |
| `defaultView` | ViewType | |
| `searchable`, `searchFields` | bool, string[] | Búsqueda rápida; los campos se usan en filtrado **cliente** |
| `useInfiniteScroll` | bool † | Sustituido por `dataSource.paginationStrategy` |
| `actions` | `ActionConfig[]` † | `{action,label,icon,enabled,permissions,color,confirm,confirmMessage,bulk}`; no hay lector |
| `relations` | `RelationConfig[]` | `{name,entityId,type,localField,foreignField,label,icon}`; sólo se usa para precargar configs hijas |
| `hierarchy` | `HierarchyConfig` † | `{type:'tree'\|'flat',parentField,typeField,multiSelect,showCounts,levels[]}`: **sin vista**. El «organigrama» de C# es una rejilla plana de `organization-rollup` |
| `emptyState` | `{icon,title,message}` | Estado vacío de la rejilla |
| `mobileView` | `{avatarField,avatarIconMap,avatarClassMap,primaryField,secondaryField,tagFields}` | Tarjetas en móvil |
| `metadata` | `{version,author,lastModified,tags}` | Sólo informativo |
| `childGridConfig` ‡ | | 6 configs; no se lee |

### 3.2 `dataSource` (contrato de datos)

| Clave | Significado |
|---|---|
| `type` | `single` (por defecto, 129), `aggregate` (varias fuentes, sólo `participant`), `enriched` (14: base + colecciones) |
| `sources[]` | Para `aggregate`: `{id,endpoint,type,subType,icon,color}` |
| `enrichment` | Para `enriched`: `{collections[{endpoint,foreignKey,additionalForeignKeys,displayField,labelField,countField,labelResolve}],aggregateEndpoint,cacheTtl}` |
| `paginationStrategy` | `'server'` (Infinite Row Model, `POST …/search`) o `'client'` (carga todo). **Los 8 de Go son `server`** |
| `serverFiltering` | `{enabled, clientOnlyFields[], companyFilterField}`. `companyFilterField` inyecta en el `filter` las empresas seleccionadas |
| `fieldMapping` ‡ | `{}` en los 8 de Go. **Retirado del modelo** (nota «D-FILTROS-DECLARADOS-QUE-NO-FILTRAN»): se declaraba y no filtraba |
| `baseFilter` | Objeto fusionado en cada búsqueda (p. ej. `{ partyTypes:["Person"] }`) |
| `enrichedSearchEndpoint`, `include[]`, `cursorSearchEndpoint` | Variantes de búsqueda de C# (enriquecida, paginación por cursor) |
| `lookups[]` | `{field,targetField,catalogEndpoint,labelField}`: resuelve FK→nombre cargando un catálogo |
| `pairSearch` | `{ sortFields: {columna→campo servidor} }`: usa el contrato de **Karpo Go** `{filter,page:{number,size},sort:{by,ascending}}` → `{items,total}` (2 configs: `customer-relationship`, `supplier-relationship`) |
| `rowMapping` | Renombra campos de la respuesta fila a fila |
| `chartSampleSize` | Filas para gráficos cuando no hay estadísticas de servidor |

### 3.3 `grid`

```jsonc
"grid": {
  "columns": [ ColumnConfig ],
  "defaultSort": { "field": "code", "direction": "asc" },
  "pageSize": 25, "pageSizeOptions": [10,25,50,100],
  "selectable": true, "selectionMode": "multiple",
  "rowActions": [ RowActionConfig ]
  // declarados y sin lector real: multiSort, editable, showRowNumbers, resizableColumns,
  // reorderableColumns, exportable, exportFormats, height, virtualScroll, emptyMessage
}
```

**`ColumnConfig`**: `field`*, `header`*, `type`*, `width/minWidth/maxWidth`, `sortable`,
`filterable`, `editable`, `visible`, `sticky`, `format`, `currency`/`currencyCode`, `enumValues[]`
(`{value,label,color,icon,cssClass}`), `linkConfig` (`{route,paramField,newTab,external}`), `align`,
`tooltip`, `cssClass`, `template`, `emptyValue`, `tooltipField`, `trueLabel`/`falseLabel`,
`progressConfig`, `trendConfig`, `ratingConfig`, `sparklineConfig`. Extras de JSON no modelados:
`badgeMap` ‡ (ignorado), `flex`.

**`type` de columna**: `text, number, date, datetime, boolean, enum, link, image, badge, icon,
currency, percentage, actions, progress, trend, rating, sparkline, array, object`. En el
constructor de columnas (`grid-column-builder`) sólo tienen tratamiento propio `date, datetime,
currency, number, boolean, badge/enum, link, progress, trend, rating, sparkline, array/object`;
`image`, `icon`, `percentage`, `actions` se quedan como texto/columna fija.

**Renderizado de `badge`/`enum`**: busca `enumValues` con igualdad estricta (`===`) y pinta
`<span class="badge …">${label}</span>` como **HTML en cadena** (riesgo de inyección si el texto
viniera de datos; hoy viene de la config). El color se traduce a clase CSS fija
(`badge-success`…).

**`RowActionConfig`**: `{action,label,icon,color,visibleWhen,confirm,confirmMessage}`. `action` es el
id de un **comando registrado** en código: sólo `view`, `edit`, `delete` existen; otro id se pinta
pero su clic cae al despacho antiguo (no hace nada útil). `edit` se oculta si la entidad no ofrece
formulario (`offersFormEdit`); `delete` siempre se pinta si está en la config.

### 3.4 `filters[]`

`FilterConfig`: `field`*, `label`*, `type`*, `placeholder`, `icon`, `options[]`
(`{value,label,icon,disabled,color,count,parentValue}`), `optionsEndpoint`, `optionsValueField`,
`optionsLabelField`, `defaultValue`, `required`, `width` (1-12, columnas del grid de filtros),
`operator`, `dynamic` (opciones derivadas del dataset cargado), `searchFields`, `dependsOn`
(cascada), `featureFlag` (legado), `visibleWhen`, `serverField`, `serverValueFormat:'array'`,
`headerButton` (toggle en la cabecera del panel), `hint` (tooltip), `queryParam` (viaja como
`?serverField=valor` en listados GET no paginados).

Tipos del modelo: `text, select, multi-select, date, date-range, number-range, boolean,
autocomplete`. **El contenedor de filtros sólo pinta `select, multi-select, boolean, text, date`**
(verificado en `filter-container.component.html`): `date-range`, `number-range`, `autocomplete`,
**`checkbox` y `number` (que usa Go) no producen control**. Convención: el filtro con `field:"_search"`
(o `search`) es la búsqueda rápida; el valor viaja por `serverField` o, por defecto, como
`nameContains`.

### 3.5 `form` (alta y edición por modal)

`FormConfig`: `groups[]` **o** `sections[]` (equivalentes; Go usa `sections`), `generated` (declara
válido el formulario autogenerado de las columnas), `create:false`, `edit:false`,
`createVisibleWhen`, `createRouting` (`{field, routes{valor→{endpoint,generateId,fixedValues}}}`),
`createRoute`, `modalSize` (`sm..full`), `createTitle`, `editTitle`, `saveButtonText`,
`cancelButtonText`, `showCancel`.

Reglas de oferta (`form.models.ts`): se ofrece **Nuevo** si `editable!==false` y hay `createViewType`
o formulario con algún campo (o `generated:true`) y `create!==false`; **Editar** por modal si
`editable!==false`, hay formulario y `edit!==false`.

`FormGroupConfig`: `id,title,description,icon,collapsed,collapsible,fields[]` (+ `columns` ‡ en los
JSON, que **no se lee**: el modal sólo conoce ancho `full`/`half`; `width>=7` → full).

`FormFieldConfig`: `field`*, `label`*, `type`*, `placeholder`, `defaultValue`, `required`,
`readonly`, `disabled`, `disabledWhen`†, `visibleWhen`, `width` (1-12), `options[]`,
`optionsEndpoint`, `optionsSearch` (`{endpoint,filter}` → POST), `requiredWhenMultipleOptions`,
`createOnly`, `validators[]`† (`{type,value,message}` **no se lee**; sí `min/max/minLength/maxLength/pattern`
planos), `hint`† (**no se pinta en el modal**, verificado), `prefix/suffix`†, `min/max`,
`minLength/maxLength`, `pattern`, `rows`†. JSON extras ‡: `editable`, `hidden`, `readonlyOnEdit`,
`optionsValueField/LabelField/ExtraQueryParams`, `lookupEntity/DisplayField/ValueField/Filter`
(75 usos), `trueLabel/falseLabel`, `default`.

Tipos del modelo: `text, textarea, number, email, password, date, datetime, select, multi-select,
checkbox, radio, switch, autocomplete, file, image, color, rich-text`. El modal sólo materializa
`text, textarea, number, email, date (también datetime), select (también multi-select, radio,
autocomplete), checkbox (también switch), guid-select`; el resto cae a texto. Validación del
modal: `required`, `email`, `min`, `max`, `minLength`, `maxLength`, `pattern` con **mensajes
fijos** en español; no se usan los `message` de la config.

### 3.6 `detail`

```jsonc
"detail": {
  "showHistory": false, "showRelations": false, "showTags": false,   // se declaran siempre; sin efecto visible
  "sections": [ DetailSectionConfig ],      // o bien "tabs": [ { id,label,icon,sections,component,childGrid } ]
  "childGrids": [ ChildGridConfig ],
  "headerActions": [ { id,label,icon,type } ],    // declarado y NO pintado
  "redirectToSource": false
}
```

`DetailSectionConfig`: `id,title,icon,collapsible,collapsed,columns (1-4),fields[],apiEndpoint`
(sección con endpoint propio, `{id}`), `upsertMethod:'PUT'|'POST'`, `upsertParentKey`,
`visibleWhen`.

`DetailFieldConfig`: `field`, `label`, `type` (`text, number, date, datetime, boolean, email,
phone, url, badge, tag, avatar, currency, select`), `options`, `icon`, `copyable`, `required`,
`editable`, `badgeType`, `placeholder`, `format`, `maxLength`, `minLength`, `visibleWhen`; JSON
extras ‡: `hint`, `min`, `max`, `trueLabel/falseLabel`, `hidden`, `lookup*`, `optionsEndpoint`,
`validation`. Cualquier otro `type` (`checkbox`, `lookup`, `decimal`, `textarea`, `guid-select`)
**se trata como `text`** (`mapFieldType`).

`ChildGridConfig`: `id,label,title,icon,entityId,apiEndpoint (admite {id}),foreignKey,dataField|field`
(datos en línea del registro padre), `columns[]` (`field,header,type,width,minWidth,emptyValue,trueLabel,falseLabel,format`),
`emptyMessage`, `pageSize`, `visibleWhen`; JSON extra: `rowActions`.

### 3.7 `charts` (vista gráfica)

`{ title, subtitle, showFooter, stats[], charts[] }`. `stats[]`: `{id,label,type:count|countWhere|sum|avg|min|max,field,value,icon,color,showPercentage,format,threshold{ok,warning,danger,inverse},tooltip}`.
`charts[]`: `{id,chartType:pie|donut|bar|horizontalBar|line|area|radar|scatter|treemap,field,secondaryField,title,subtitle,size:half|full,limit,colors[],showLegend,showLabels,labelMap,options}`.
Colores fijos en el JSON (hex): choca con el requisito de temas por variables.

### 3.8 Reglas de visibilidad (`visibleWhen`)

Unifica seis mecanismos antiguos. `VisibilityRule` (todas las cláusulas presentes en AND; ausente =
visible, *fail-open*): `hasRole` (rol **del registro**), `capability` (del tenant, multi),
`sector` (del tenant, único), `userRole` (`super|admin|user|customer`), `permission` (de
`/api/auth/context`, con comodines `*.*.*`), `requiredModule`, `featureFlag`, `expr`
(`"campo == valor"` / `!=`, sólo formularios y filtros) y composición `allOf/anyOf/not`. Un string
es atajo de `{expr}`. El modo `accessMode:'allowlist'` invierte a *deny-by-default* (hoy siempre
`open`). Ojo: en C# hay **dos** `visibleWhen` distintos según el sitio (regla completa en
detalle/filas/menú; expresión sobre otros campos en formularios y filtros).

### 3.9 `sidebar-config.json`

```jsonc
{ "contexts": [ { "id","label","icon","items": [ NavigationItem ] } ] }
```

`NavigationItem`: `id,label,icon,children[],entityId,viewType,type ('company'|'company-filter'|'folder'|'document'),
selectable,requiresCompanySelection,externalUrl,embedSupported,badge,roles[] (legado),sector,capability,
visibleWhen,quickAccess`. Contextos: «Aplicación» (seguridad, telemetría, rendimiento con iframes de
Grafana/Prometheus, etc.), «Mi empresa» (selector de empresas + PRM, Productos…). Quién ve qué
contexto: sólo el administrador global ve todos; el resto ve únicamente `empresas`. El rol
`customer` va en *allowlist* (sólo ítems con `roles:['customer']`).
**Los 8 ítems de Go no están en este fichero** (ni en `index.json`).

### 3.10 `runtime-config.json` y entornos

`{ apiUrl, authUrl, apiVersion, featureFlags{…} }` sobrescribe `environment.*` en arranque
(`RuntimeConfigService`, `fetch('runtime-config.json')`; si falla, valores por defecto).
`environment`: `apiUrl:'/api'`, `apiVersion:'v1'`, `apiTimeout:30000`, `authUrl:'/api/auth'`,
`useMockData`, `allowOfflineLogin`, flags (`dynamicEntities, aiTerminal, dataExport, dataImport,
darkMode, i18n`).

---

## 4. Cómo interpreta la configuración el runtime

### 4.1 Rejilla (`views/grid`, ~1.850 líneas en `grid.ts`)

Un único componente que decide estrategia de carga según config:

1. `dataSource.type==='aggregate'` → carga múltiples endpoints y los une (sólo `participant`).
2. `editType==='party'` → carga enriquecida (varias peticiones + cruce por id case-insensitive).
3. `paginationStrategy==='server'` → **Infinite Row Model** de AG Grid con `GridInfiniteDatasourceService`.
4. Resto → cliente: `getAllUnpaginated` (GET con `pageSize=2000`; si el total supera lo recibido
   reintenta por `POST /search` con tope de 10.000) y filtrado/ordenación **en el navegador**
   (con Web Worker).

Búsqueda de servidor (el caso de Go): `POST {apiEndpoint}/search` con

```json
{ "filter": { "<serverField|field>": valor, … } | null,
  "pagination": { "pageNumber": n, "pageSize": ≤500, "orderBy": "<colId>", "ascending": true } }
```

(`pageNumber = floor(startRow/pageSize)+1`; el orden es el `field` de la columna ordenada, no un
campo de servidor). Con `pairSearch` el cuerpo es `{filter,page:{number,size},sort:{by,ascending}}`
y `isActive`→`active`. La respuesta se normaliza a ojo: filas en `items|data|results|records` o
array directo; total en `totalCount|totalItems|total|totalElements|count`. Si falla la ruta
enriquecida/cursor hay cadena de reintentos (cursor → offset → `/search` básico). Si todo falla se
enseña el `detail` del ProblemDetails en un panel.

Filtros → `filter` del cuerpo; la búsqueda rápida sale por el filtro `_search` (`serverField`) o
`nameContains`. Las empresas seleccionadas viajan **además** como cabecera `X-Organization-Scope`
y, si `companyFilterField`, dentro del `filter`.

Funciones de la rejilla (verificado): selección simple/múltiple, ordenación, redimensionar y
reordenar columnas, estado de columnas en `localStorage` (`grid_<entityId>_state`), búsqueda rápida
con *debounce* de 200 ms, panel de filtros con chips y filtros guardados (localStorage), exportar a
CSV/Excel/PDF (cliente: `xlsx`, `jsPDF`), vista de tarjetas en móvil, columna de acciones por fila,
columna «Empresa» inyectada cuando hay varias empresas, atajos de «Spotlight».

**Doble clic**: `onCellDoubleClicked` → `openDetailForRecord` → abre/reemplaza la pestaña de detalle
**en modo lectura**. No entra en edición.

**Modo edición de rejilla** (`activateEditMode`): hace editables las columnas salvo `id`,
`companyLabel`, `company`, acciones y `badge/enum`; `trackChange` apunta las filas tocadas;
«Guardar» (`saveChangesAndExit`) **sólo llama a `commitChanges` (copia local) y a `saveGridState`**.
Verificado: no hay llamada HTTP. «Descartar» restaura la copia. No hay validación por celda, ni
deshacer paso a paso, ni guardado optimista, ni gestión de conflictos.

### 4.2 Alta, edición por modal y borrado

- **Alta** (`EntityCreationService`, `ConfigDrivenFormService.buildCreateModal`): construye un
  `FormModalConfig` desde `form`, carga opciones (`optionsEndpoint` por GET con caché por endpoint;
  `optionsSearch` por POST sin caché), aplica `requiredWhenMultipleOptions`, y `POST apiEndpoint`
  con el cuerpo del formulario (o `createRouting`/`createRoute`). Los valores de los campos ocultos
  se vacían y deshabilitan.
- **Edición por modal**: `PUT apiEndpoint/{id}` (mismo formulario, sin los `createOnly`).
- **Borrado** (`delete` en `rowActions`): confirmación (`confirm`/`confirmMessage` si los hay) y
  `DELETE apiEndpoint/{id}`; el mensaje de éxito es fijo («Registro eliminado correctamente»).
  `deleteMany` intenta `POST /bulk-delete` y cae a borrado secuencial.
- Tras la escritura, el interceptor de calidad de datos (D-92) muestra un aviso «Guardado con
  avisos» si el cuerpo trae `warnings[{field,code,message}]`.

### 4.3 Detalle (`views/detalle`, ~1.300 líneas)

1. Recibe `recordData` de la fila y **vuelve a pedir** el registro: `aggregateEndpoint` (`{id}`) y,
   si falla, `GET apiEndpoint/{id}`.
2. Secciones con `apiEndpoint` propio: `GET` y fusión en el registro; guardan con `PUT`/`POST`.
3. Pestañas = secciones (o `detail.tabs`) + una por `childGrids[]`; todo con `visibleWhen`.
4. **Edición** (botón «Editar» en cabecera): formulario reactivo con los campos `editable`
   (por defecto editable) → `saveChanges`: calcula **sólo los cambios**, pero `saveDirectUpdate`
   envía `PUT apiEndpoint/{id}` con `{...registroActual, ...cambios}` (así viaja `version` si el
   registro la trae: concurrencia optimista implícita). Valida `required`, `minLength/maxLength`
   (límite **por defecto de 100 caracteres** si no hay `maxLength`, verificado en la plantilla).
5. Cabecera: «Actualizar», «Activar/Desactivar» (si el registro trae `isActive`; `POST
   /parties/toggle` o `PUT` directo), «Marcar test» (si trae `isTest`), «Eliminar».
   `headerActions` del JSON **no se pinta**.
6. Hijos: `field`/`dataField` (datos en el registro; avisa si hay `<campo>TotalCount` mayor) o API:
   `apiEndpoint` (con `{id}` sustituido) o `/{entityId}s`, con paginación por `getAll`; **añade
   además `?<foreignKey>=<id>` y `page`/`pageSize`** aunque el endpoint ya sea específico.

Tipos de campo en **edición del detalle**: sólo `date, datetime, number, boolean, email, phone, url,
select` y texto por defecto. Faltan `checkbox`, `lookup`, `textarea`, `decimal`, `min/max`
(el propio código lo anota: «se portarán en la sesión de sync»).

### 4.4 Gráficos

Estadísticas del servidor (`POST {entidad}/search/stats` con `groupBy`) y, si no hay, muestra de
`chartSampleSize` filas (500 por defecto). ECharts; colores del JSON; arrastrar-soltar para
reordenar tarjetas; preferencias en `dashboard-preferences.service` (localStorage). Los 8 de Go no
tienen `charts` (y `availableViews` sin `chart`).

### 4.5 Comandos y visibilidad (D-FRONT-COMMAND-PATTERN)

`CommandRegistry` con `UiCommand{id,surfaces,label,icon,kind,isVisible,execute}`; superficies:
`sidebar, detail-header, detail-tab, detail-field, grid-row, grid-toolbar, child-grid-row, filter`.
Comandos hoy: `view`, `edit`, `delete`, `grid.add`, `record.refresh`, `record.toggle-status`,
`record.toggle-test`, `nav.*`. **Los comandos se registran en código**; el JSON sólo los referencia
por id. No existe forma declarativa de definir una orden nueva (`POST …/{id}/block`) – es la
petición pendiente que `front/README.md` ya documenta («Órdenes declarativas por fila…»).

---

## 5. Los 8 configs de Go frente al runtime Angular

Censo (8 ficheros, 6 contextos): `dataSource.type:"single"` y `paginationStrategy:"server"` en los 8;
`editType:"direct"`; `availableViews:["grid","detail"]`; 73 campos de detalle, 40 de formulario, 12
filtros, 19 acciones de fila (`view` 8, `edit` 7, `delete` 4), 2 con `childGrids` (Bloqueos).

Tipos usados — columnas: `text 22, number 5, badge 8, date 3, boolean 1`; filtros: `text 7, number 1,
checkbox 3, select 1`; formulario: `text 23, number 5, date 4, checkbox 4, select 3, textarea 1`;
detalle: `text 39, number 13, checkbox 12, date 7, select 2`.

| # | Hallazgo (verificado en código C#) | Efecto hoy en Angular | Qué debe hacer React |
|---|---|---|---|
| 1 | Filtros `type:"checkbox"` («Sólo activos») | **No se pinta ningún control** | Soportar `checkbox` como alias de `boolean` |
| 2 | Filtro `type:"number"` (año de festivos) | No se pinta | Soportarlo |
| 3 | Columnas `badge` con `badgeMap` (`{"true":{label,color}}`) | `badgeMap` no se lee; el badge sólo mira `enumValues` → muestra el valor crudo | Soportar `badgeMap` **y** `enumValues`, con claves en cadena (`"true"`) y comparación tolerante (`true`≡`"true"`) |
| 4 | Detalle `type:"checkbox"` (12) | Se trata como texto, y se edita como `<input type=text>` | Alias de `boolean` |
| 5 | Formulario `sections[].columns` | Ignorado; sólo full/half | Respetar `columns` y `width` (1-12) |
| 6 | `hint` en formularios (24) y detalle (7) | No se pinta (verificado en el modal) | Pintarlo como ayuda del campo |
| 7 | Claves ajenas (`employer`, `facility`, `supplier`…) son **texto libre** con un hint «identidad de…» | El usuario pega GUIDs | Soportar `lookupEntity`/`optionsEndpoint`/`optionsSearch` como selector con búsqueda; proponer a Theros emitirlo |
| 8 | `dataSource.serverFiltering.fieldMapping:{}` | Muerto (retirado del modelo) | Ignorar sin avisar |
| 9 | Los ficheros no están en `index.json` ni en `sidebar-config.json` | Inalcanzables (README: «copiar el fichero» no basta) | React debe poder **componer** el registro y el menú desde las propias configs (`metadata`/`menu` por entidad) o aceptar un índice emitido por Theros |
| 10 | `rowActions[].delete` con `confirm`/`confirmMessage` | Funciona, pero Go retira/cierra/bloquea, no borra | Etiqueta y mensaje por acción; mantener `confirm` |
| 11 | Columnas `sortable` | Cualquier columna ordenable manda `orderBy=<field>`; Go admite pocos (producto: `sku`,`name`; otro → 400) | Enviar sólo lo declarado ordenable por servidor (necesita `serverSortable`/mapa; hoy sólo existe `pairSearch.sortFields`) |
| 12 | Hijo «Bloqueos» `apiEndpoint:"/suppliers/{id}/block-history"` + `foreignKey` | Funciona, pero añade `?supplierId=…&page&pageSize` que Go ignora | No añadir el parámetro cuando el endpoint ya lleva `{id}` |
| 13 | `form.sections[].fields[].maxLength` | Sí se aplica en el modal | Igual |
| 14 | `version` en el cuerpo (PUT exige `version`; 409 si cambió) | El detalle manda el registro entero (incluye `version`); el modal **sólo manda su formulario** → «sin `version`, 400» | Siempre fusionar con el registro original (o enviar `version`); tratar 409 como conflicto con opción de recargar |
| 15 | Errores RFC 9457 con `errors{campo:[msgs]}` y `errorCodes` | `extractErrorMessage` los **concatena en un toast**; no marca el campo | Mapear `errors` a los campos del formulario; usar `errorCodes` para i18n |

Más allá de la config, para que el runtime hable con Go faltan o difieren (verificado con grep en Go):
`GET /api/v1/security/my-accessible-organizations`, `/api/capabilities/current`,
`/api/module-activation/current` y `/api/sector/current` **no existen** en Go (el runtime cae a
`GET /internal-organizations` para el selector; el comportamiento ante el 404 de capabilities/sector
no se ha verificado). Para el login, ver §7.4.

---

## 6. Navegación, menú y shell

- **Sidebar**: contextos → ítems → hijos (árbol de profundidad libre), colapsable, redimensionable
  (ancho en señal), menú móvil, buscador del menú (`sidebar-search`). El ítem `type:'company-filter'`
  convierte el menú en **selector de empresas** (casillas; umbral en línea de 10; colores rotados);
  la selección se guarda en `localStorage` (`selected-companies`) y cambia el ámbito de todas las
  peticiones.
- **Contexto guardado**: `localStorage['sidebar-selected-context']`.
- **Workarea**: pestañas por panel (máx. 8), arrastrar para reordenar o mover entre paneles, división
  horizontal/vertical con tamaño en %, cierre, «+» nueva pestaña. Aviso «Sin conexión».
- **Spotlight** (buscador global): `Ctrl+Shift+K`; ejecuta acciones (exportar, refrescar, abrir
  entidad). No hay otros atajos globales documentados; el resto es `Enter`/`Escape` locales.
- **Topbar**: usuario, tema, idioma, ayuda de salud del backend (`health-banner`), consola.
- Banners: sin conexión, salud del backend (`/health`), expiración por inactividad (30 min + 5 min
  de aviso).
- **Deep links**: `?entity=&view=&record=&tab=`; `Router` con restauración de scroll.
- **Terminal** de desarrollo: consola con IA, desacoplada («Vistas de IA retiradas 2026-08-29»).

---

## 7. Llamadas HTTP, cabeceras y errores

### 7.1 Base y versión

`{apiUrl}{/apiVersion}{endpoint}`; por defecto `/api/v1`. `authUrl` = `/api/auth` (sin versión). El
servidor de desarrollo proxifica `/api` y `/health` al *gateway* de la pila de contenedores
(`proxy.conf.js`); en producción lo sirve el mismo proxy inverso. React en desarrollo contra Go
necesitará su propio proxy a `cmd/karpo`.

### 7.2 Catálogo de llamadas

| Llamada | Cuándo | Notas |
|---|---|---|
| `GET config/entities/index.json`, `GET config/entities/<ruta>`, `GET sidebar-config.json`, `GET runtime-config.json` | Arranque / bajo demanda | Estáticos, **sin token**, sin timeout |
| `POST /api/auth/login` `{username,password}` | Login | Reintenta 3× en 0/408/5xx (1-2-4 s); 401/403 no |
| `POST /api/auth/refresh` `{refreshToken}` | 401 de cualquier petición | Coordinador único; las demás esperan (máx. 30 s) |
| `GET /api/auth/context` | Tras login, refresco, cambio de empresa, 403 | Permisos reales (D-123). Fallo = en cerrado |
| `GET /api/auth/register/capabilities`, `POST /api/auth/register` | Registro público | Anónimo |
| `GET /api/v1/security/my-accessible-organizations` | Selector de empresas | Con `USE_FULL_ACL` (sin cabecera de ámbito); fallback `GET /internal-organizations` |
| `GET /api/capabilities/current`, `/api/sector/current`, `/api/module-activation/current` | Menú por tenant | Ejes `capability`, `sector`, `requiredModule` |
| `POST {ep}/search` (o `enrichedSearchEndpoint`, `cursorSearchEndpoint`) | Rejilla servidor | §4.1 |
| `GET {ep}?page&pageSize&sortBy&sortOrder&search&fields&$expand&<filtros>` | Rejilla cliente, hijos, opciones | Los filtros-objeto se aplanan `clave.sub=valor`; arrays con coma |
| `POST {ep}/search/stats` `{filter,groupBy,numericFields}` | Gráficos | 404 tolerado |
| `GET {ep}/count`, `GET {ep}/{id}?$expand=` | Contadores, detalle | |
| `POST {ep}` / `PUT {ep}/{id}` / `PATCH {ep}/{id}` / `DELETE {ep}/{id}` | Alta, edición, borrado | `PATCH` existe en el servicio pero el detalle usa `PUT` |
| `POST {ep}/bulk-delete` `{ids}`; `GET {ep}/export`; `POST {ep}/import`; `POST {ep}/{id}/{acción}` | Servicio genérico | `export` real: la rejilla exporta en cliente |
| `POST /api/v1/parties/rename`, `/parties/toggle`, `PUT /parties/{id}/test-flag` | `editType:'party'` | Específico de parties |
| `GET {aggregateEndpoint}` | Detalle | `{id}` sustituido |
| `GET /health…` | Banner de salud | Fuera de `/api` |

Modo `useMockData`: sirve datos simulados desde JSON local (sólo desarrollo).

### 7.3 Interceptores (orden)

1. `clientChannelInterceptor`: `X-Client: web` en **todas** las peticiones.
2. `authInterceptor`: `Authorization: Bearer <token>` (salvo rutas públicas: login, register,
   refresh, forgot-password, `validate-format`, `/assets/`, `/config/`, cualquier `.json`) y
   `X-Organization-Scope: <ids de empresas seleccionadas separados por coma>` (salvo
   `USE_FULL_ACL`). 401 → refresco y reintento; 403 → recarga el contexto una vez.
3. `timeoutInterceptor`: 30 s (excluye assets, config, `/export`) → error sintético 408.
4. `errorInterceptor`: *circuit breaker* por URL (503 sintético si abierto); **reintentos** hasta 3
   con retroceso exponencial + *jitter* (500 ms base, tope 10 s) en 0/408/429/500/502/503/504,
   respetando `Retry-After`; sólo métodos seguros (GET, HEAD, OPTIONS, **PUT, DELETE**); POST/PATCH
   sólo en 0 y 429. Muestra *toasts* por código (red, 403, 422 de validación, 429, 5xx); el 422
   con `errorCode` (regla de negocio) **no** muestra aviso global, lo trata el componente.
5. `dataQualityWarningsInterceptor`: aviso tras POST/PUT/PATCH si el cuerpo trae `warnings`.
6. XSRF: `XSRF-TOKEN` → `X-XSRF-TOKEN`.

### 7.4 Discrepancias de contrato con Go (verificadas leyendo código de ambos lados)

- **Login**: el runtime exige `{ success: true, accessToken, refreshToken, errorMessage }` y
  descodifica el JWT esperando claims `sub`, `username`, `roles`, `exp` (+ opcionales `partyId`,
  `name`, `displayName`, `position`). Go responde `{ accessToken, refreshToken, expiresAt, userId,
  username, mustChangePassword }` **sin `success`** (`contexts/security/application/auth.go`): el
  runtime lo interpretaría como credenciales incorrectas. El rol se deduce del claim `roles` y, de
  ahí, del contexto (`isGlobalAdmin`→`super`). React debe leer el contrato de Go tal cual y tomar
  identidad/permisos de `GET /api/auth/context` (que sí coincide: `authz.Resolution`).
- `mustChangePassword` y `POST /api/auth/change-password`, `logout`: el runtime no los usa.
- Paginación: Go devuelve `{items,total,totalCount,pageNumber,pageSize,totalPages}`; el runtime
  entiende `items`+`totalCount`/`total`.

---

## 8. Autenticación y permisos

- **Sesión**: en `sessionStorage` (`session`, `token`, `refresh token`); se pierde al cerrar la
  pestaña. Guardias: `authGuard` (→ `/login?returnUrl=`), `guestGuard`, `roleGuard`, `authMatchGuard`.
- **Roles**: `super | admin | user | customer`. `super` sólo si el contexto dice `isGlobalAdmin`;
  `admin` por claim `admin`/`organizationadmin`; `customer` por claim. Hay **login simulado**
  (`allowOfflineLogin`, usuarios `super/admin/user`, contraseña `123`) sólo en desarrollo.
- **Contexto de autorización** (`AuthContextService`, D-123): `{tenantId, subjectId, subjectName,
  actorKind, actorPartyId, isGlobalAdmin, grants[{organizationId,level,includeSubsidiaries}],
  permissions[], requestedOrganizationIds, effectiveOrganizationIds, policyVersion}`.
  `permissionMatches` compara por segmentos con comodín (`*.*.*`). Falla en cerrado: sin contexto no
  hay menú ni permisos y se avisa con un toast.
- **Qué se controla en UI**: ítems de menú y contextos, pestañas del detalle, acciones de fila,
  campos, filtros, alta (`createVisibleWhen`). **No se controla por permiso funcional** la lectura
  ni la escritura de cada entidad (`Products.Product.Read/Update`): la UI sólo oculta lo que el
  `visibleWhen` declare; el servidor decide con 403.
- **Ámbito por empresa**: el usuario elige empresas en el menú; viaja en `X-Organization-Scope`;
  el servidor lo intersecta con el ACL del token (no es un bypass).
- **Inactividad**: aviso a los 30 min, cierre a los 35.
- Extras: *offline queue* de mutaciones en IndexedDB (`karpo-offline-queue`) con reenvío al volver
  la red; registro de auditoría de cliente (`audit-trail`); vitales web; service worker
  (`ngsw-worker.js`, sólo producción).

---

## 9. Textos, i18n, temas, RTL y ayuda

- **i18n**: dos capas desconectadas.
  - *Cáscara* (menús del shell, errores, mensajes): `$localize` y `i18n=` de Angular,
    **resueltos en compilación**: un *build* por idioma (`--localize`; en `angular.json` sólo `en` y
    `fr` además del español de origen aunque haya 25 ficheros `messages.*.xlf`). No hay cambio de
    idioma en caliente.
  - *Configuración* (`name`, `header`, `label`, `hint`, `confirmMessage`, `sidebar-config`): **texto
    literal en español**. No hay claves ni diccionarios por idioma. Verificado: ningún JSON usa
    `labelKey`/`i18nKey`/`help`.
- **Temas**: nueve temas (`dark-on-light` «Claro Karpo» por defecto, `green-on-dark`, `light-on-dark`,
  `amber-on-dark`, `nord`, `dracula`, `solarized-dark`, `one-dark`, `tokyo-night`), definidos **en
  TypeScript y duplicados en `theme-loader.js`** (hex fijos y paleta `--primary-50..900`);
  persistidos en `localStorage['app-theme']`; tema de AG Grid propio (`ag-grid-theme.scss`). Los
  colores de entidad, gráficos y *badges* son valores fijos en JSON/SCSS. **No se lee ningún
  contrato de Theros Front.**
- **RTL**: `_rtl.scss` bajo `[dir='rtl']` y detección para árabe/hebreo en `theme-loader.js`.
  Cobertura parcial (inferido: la rejilla de AG Grid y los *popups* necesitan más).
- **Ayuda**: **no existe** (ni recorridos, ni `help` en config, ni librería de *tours*). Sólo hay
  `hint` (no pintado en el modal), `tooltip` de columna y `title` en filtros.
- **Accesibilidad**: roles/`aria-label` en pestañas y botones del shell; las celdas se generan
  como HTML en cadena (renderers de AG Grid); no hay auditoría de contraste ni gestión de foco en
  modales confirmada (inferido, no probado).

---

## 10. Pruebas del runtime

Karma + Jasmine (≈400+ tests unitarios; cobertura declarada ~23 %), Playwright (`e2e/`,
`e2e/humo`). Ficheros útiles como referencia de comportamiento: `entity-create-offer.spec.ts`,
`grid-pair-search*.spec.ts`, `visibility-rule.spec.ts`, `sales-price-configs.spec.ts`.

---

## 11. Carencias de UX y de producto (para superarlas)

Ordenadas por impacto frente al requisito de Javier.

1. **Edición en cuadrícula inexistente en la práctica**: «guardar» no persiste; sin validación por
   celda, sin deshacer/rehacer, sin guardado por lotes ni optimista, sin conflictos de versión.
   El doble clic abre detalle en lectura (hay que pulsar «Editar» después).
2. **Validación pobre**: mensajes fijos, no se usan los `message` de la config; `required` y poco
   más; errores de servidor (`errors` por campo) se vuelcan en un toast sin marcar el campo; en el
   detalle el límite por defecto de 100 caracteres es arbitrario.
3. **Tipos de campo que se degradan en silencio** (`checkbox`, `lookup`, `decimal`, `textarea`,
   `guid-select` en detalle; `date-range`, `number-range`, `autocomplete`, `checkbox`, `number`
   en filtros). Sin aviso de desarrollo ni validación de la config.
4. **Claves ajenas sin selector** en Go (texto libre).
5. **Sin vistas alternativas**: `hierarchy`, `kanban`, `calendar` declarados y sin implementar; no hay
   organigrama, árbol, mapa ni diagrama; el historial es un placeholder («se rellenará más tarde»).
6. **Sin ayuda contextual**; `hint` no se muestra.
7. **i18n inflexible**: un *build* por idioma, config sólo en español, sin claves.
8. **Temas rígidos**: nueve temas en TS duplicados con el cargador; colores fijos en configs;
   ningún contrato externo.
9. **Rejilla**: HTML en cadena en celdas (sin componentes, sin foco por teclado dentro de la celda),
   sin edición de celdas de `badge/boolean` con control propio, sin agrupación, sin columnas
   fijas configurables (`sticky` no se lee), sin selección por rango/portapapeles, sin virtualización
   de columnas propia más allá de AG Grid; el filtrado en cliente de 10.000 filas.
10. **Estado en memoria y `localStorage`** para estado de rejilla, filtros guardados, selección de
    empresa y preferencias: no sincroniza entre dispositivos ni se asocia al usuario.
11. **Navegación sin URL de entidad**: sin rutas `/<entidad>/<id>`; los *deep links* son *query
    params*; el «atrás» del navegador sólo recorre lo que el servicio sincroniza.
12. **Acciones sólo registradas en código**: no hay órdenes declarativas (bloquear, aprobar…), ni
    `prompt` de motivo en el borrado, ni tratamiento del 202 «pendiente de aprobación»
    (`front/README.md` ya lo documenta); `delete` no distingue borrar/retirar/cerrar.
13. **Reintentos de `PUT`/`DELETE` automáticos**: al ser «seguros» se reenvían hasta 3 veces ante
    5xx; con `version` obligatoria puede dar 409 espurios (inferido).
14. **Dos `visibleWhen` distintos** y claves legadas (`roles`, `sector`, `capability`,
    `featureFlag`) que conviven con la regla nueva.
15. **Calidad de la configuración**: sin esquema, claves muertas y duplicadas (`groups`/`sections`,
    `alias field/dataField`), `fieldMapping` retirado pero aún emitido, dominios de `index.json`
    desfasados.

---

## 12. Implicaciones para el front React

### 12.1 Requisitos de compatibilidad (leer la misma config)

1. **Capa de normalización con validación** (JSON Schema + función de normalizado) que reciba el
   JSON tal cual y entregue un modelo interno estable: `groups`≡`sections`, `field`≡`dataField`,
   `checkbox`≡`boolean`, `badgeMap`⊕`enumValues`, `visibleWhen` string≡`{expr}`, `legacy roles/sector/
   capability/featureFlag` → regla única; claves desconocidas = **aviso en desarrollo**, no error.
2. **Resolución del registro y del menú**: `index.json` (`entities`, `domains`) y `sidebar-config.json`
   **más** descubrimiento de configs sin índice (Go). Propuesta: aceptar un único `manifest.json`
   emitido por Theros con `{entities, menu, i18n, help, theme}` y mantener el modo legado.
3. **Contrato de datos**: soportar `single`+`server` (`POST /search` con `pagination`),
   `single`+`client`, `pairSearch`; `aggregate`/`enriched` y cursores en una segunda fase (el
   ERP de Go no los usa). Normalizar respuestas (`items|data|results`, `total|totalCount`).
4. **Cabeceras**: `Authorization`, `X-Organization-Scope`, `X-Client`, `X-XSRF-TOKEN` si hay cookie.
5. **Errores**: RFC 9457 (`title`, `detail`, `errors{campo:[]}`, `errorCodes`, `code`) mapeados a
   campos; 409 de versión con acción «recargar»; 422 de regla de negocio con su mensaje; 202/`pendingApproval`.
6. **Auth de Go**: `POST /api/auth/login|refresh|logout|change-password`, identidad y permisos de
   `GET /api/auth/context`, control de `mustChangePassword`. No depender de `success` ni de claims
   de rol en el JWT.
7. **Hijos**: `{id}` en `apiEndpoint`; no añadir `?foreignKey=` si ya lleva `{id}`.

### 12.2 Lo que React debe hacer mejor (mapeo carencia → solución)

| Requisito de Javier | Base en el runtime | Qué falta/añadir en React |
|---|---|---|
| Doble clic abre detalle **en edición** | Doble clic abre lectura | Modo del detalle por parámetro (`?mode=edit`), foco al primer campo editable |
| Edición en línea con validación, deshacer, lote/optimista | Modo edición local sin guardar | Editor de celdas por tipo, validación por esquema de campo, pila deshacer/rehacer, cola de cambios con PUT por fila (con `version`), optimista con retroceso y marcado de errores por celda |
| Teclado completo | Sólo AG Grid por defecto y `Ctrl+Shift+K` | Navegación por celdas (flechas, Enter, F2, Esc, Tab), atajos globales documentados, foco visible, paleta de comandos |
| Listado/diagrama | `hierarchy` sin vista | Interruptor de vista por entidad; configuración nueva y opcional (`views.diagram`/`views.map`) |
| Organigrama navegable | — | Árbol con zoom, plegado, búsqueda; necesita `parentField`, `displayField`, `typeField` (ya existen en `hierarchy`) |
| Mapas por niveles | — | MapLibre + PMTiles propios, origen configurable; niveles mundo/país/región/ciudad (datos NUTS de Go: `docs/GEOGRAFIA-NUTS.md`) |
| Temas de Theros | 9 temas fijos | Todo por variables CSS; sin hex en componentes; claro/oscuro; RTL con propiedades lógicas |
| Ayuda guiada | no existe | driver.js; pasos desde la config (`help.steps[{target,title,body}]`) con claves i18n |
| i18n `es`/`en` + estructura | compilación | Diccionarios en ejecución con claves (`labelKey`) y *fallback* al literal |
| Paquete npm | app | Librería `@karpo/react` (componentes + hooks + cliente de config) y app de demostración |

### 12.3 Decisiones y consultas pendientes (no bloquean el análisis)

1. **Rejilla**: AG Grid Community es MIT, pero sus funciones de edición por rango, pegado y lote
   son Enterprise. Opciones MIT/Apache/BSD: TanStack Table (MIT, sin UI) + TanStack Virtual;
   `react-data-grid` (MIT). Se propone construir sobre TanStack Table y escribir el editor/teclado.
   (Valorar en el análisis de librerías.)
2. **Dependencias ISC** (d3-*, p. ej. para el árbol): no están en la lista de licencias directas
   permitidas; se evitan o se consulta.
3. **Formato de i18n en config**: ¿claves (`"labelKey"`) o diccionario por idioma dentro del
   mismo JSON? Hay que acordarlo con Theros (§13).
4. **Hacer pública la compatibilidad**: los campos del JSON que **React no soportará nunca**
   (`apiBaseUrl`, `childGridConfig`, `useInfiniteScroll`, `actions`…) deberían figurar en el
   informe de compatibilidad.

---

## 13. Peticiones derivadas (para anotar en los ficheros de peticiones)

A anotar en `docs/PETICIONES-THEROS-FRONT.md` (contrato de Theros Front, la parte de temas y
configuración) y `docs/PETICIONES-BACKEND.md` por quien construya el front; aquí sólo el borrador
del análisis.

**Backend Go**

1. `GET /api/v1/security/my-accessible-organizations` (o equivalente en `/api/auth/context`):
   lista de empresas accesibles para el selector.
2. Documentar o unificar el **login** con el runtime/React (`success`, claims, `mustChangePassword`).
3. Lista de campos ordenables por servidor en cada entidad (hoy un orden no admitido da 400).
4. `GET /api/v1/<entidad>` y `POST …/search` coherentes en las 8 entidades; confirmar que las 8
   tienen `…/search` con `{filter,pagination}`.
5. Opciones para claves ajenas: `GET /api/v1/<catálogo>?q=` o `POST …/search` ligero (lookups).
6. `409`/`422` con `errors` por campo en todas las entidades (ya en el contrato de mantenimiento).
7. Datos geográficos jerárquicos con identificadores estables para el mapa por niveles.

**Theros (config y tema)**

1. Emitir `manifest.json` (entidades + menú + versión de esquema) y las configs con **esquema JSON
   publicado** (hoy sólo TypeScript).
2. Emitir `checkbox`→`boolean`, `badgeMap` unificado con `enumValues`, `lookup` con `lookupEntity`.
3. Textos con claves i18n y un diccionario por idioma, y bloque `help` por pantalla.
4. Contrato de **temas**: lista de variables CSS (colores, tipografías, espaciados, radios, sombras,
   estados) y modo claro/oscuro/RTL; sin colores fijos en `color`/`colors[]` de la config (usar
   *tokens* semánticos, p. ej. `"color": "token:accent"`).
5. Órdenes declarativas (`commands[]`) con `prompt`, `sendVersion`, `acceptedMessage`
   (ya propuestas en `front/README.md`).
6. Declaración de vistas alternativas: `views.diagram` (árbol/organigrama: `parentField`, `rootField`),
   `views.map` (`levelField`, `geometry`).

---

## Anexo A · Referencia rápida de los ficheros leídos

| Tema | Ruta (Angular, `src/app/`) |
|---|---|
| Modelos de config | `shared/models/entity-config/{base.types,entity.models,grid.models,filter.models,form.models,detail.models,chart.models,hierarchy.models,action.models}.ts` |
| Carga de config | `core/services/api/entity-config.service.ts` |
| Datos genéricos | `core/services/api/generic-entity.service.ts` |
| Búsqueda servidor | `views/grid/services/grid-infinite-datasource.service.ts`, `grid-data-orchestration.service.ts` |
| Columnas | `views/grid/services/grid-column-builder.service.ts` |
| Edición de rejilla | `views/grid/services/grid-edit.service.ts`, `views/grid/grid.ts` (modo edición ~L1270-1340) |
| Detalle y guardado | `views/detalle/detalle.ts`, `views/detalle/services/detail-data.service.ts`, `detail-field-config.service.ts` |
| Formularios | `core/services/ui/config-driven-form.service.ts`, `shared/components/modals/form-modal/*` |
| Filtros | `shared/components/filters/filter-container/*` |
| Comandos / visibilidad | `core/commands/*` |
| Auth | `core/services/auth.service.ts`, `auth/auth-context.service.ts`, `core/interceptors/*`, `core/guards/auth.guard.ts` |
| Navegación | `core/services/navigation/{navigation,workarea,deep-link}.service.ts`, `public/sidebar-config.json` |
| Tema | `core/services/theme.service.ts`, `public/theme-loader.js`, `shared/styles/*` |

## Anexo B · Censo de claves de los configs de C# (148 entidades)

Útil para priorizar qué soportar primero (nº de configs que usan la clave):
`apiEndpoint 148`, `detail 142`, `filters 145`, `dataSource.serverFiltering 143`, `form 45`
(`groups 30`, `sections 14`), `charts 12`, `dataSource.paginationStrategy 27`,
`dataSource.enrichedSearchEndpoint 15`, `dataSource.cursorSearchEndpoint 9`, `dataSource.pairSearch 2`,
`aggregateEndpoint 8`, `detail.childGrids 11`, `detail.tabs 1`, `detail.headerActions 4`,
`filters[].optionsEndpoint 25`, `filters[].queryParam 13`, `filters[].headerButton 17`,
`form.createRouting 1`, `form.createRoute 2`, `form.createVisibleWhen 1`, `createViewType 1`.
Tipos de campo en los configs: `text 1098`, `date 250`, `boolean 233`, `number 165`, `select 103`,
`lookup 74`, `guid-select 33`, `textarea 32`, `decimal 27`, `multi-select 14`, `datetime 10`,
`checkbox 7`. Los 8 de Go usan sólo `text, number, date, select, checkbox, textarea, badge, boolean`.
Conclusión: **`lookup`, `guid-select` y `decimal` son los tipos de C# que React debe cubrir** aunque
Go aún no los emita, porque un pack karpo-go de Theros los emitirá en cuanto haya claves ajenas con
selector.
