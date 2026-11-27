// Tabla de compatibilidad con el JSON de Angular (A1 §3): qué hace React con cada clave.
// La usan el normalizador (avisos de claves ignoradas o desconocidas) y la prueba que comprueba
// que docs/COMPATIBILIDAD-CONFIG.md está al día (se genera con `renderCompatTable`).
//
// Estados:
//   supported   se lee y se usa tal cual
//   normalized  se lee y se convierte (la nota dice cómo)
//   ignored     se lee y se descarta, con aviso de severidad `info` (la nota dice por qué)
//   silent      se descarta sin aviso (declaradas en todos los JSON de Go y sin efecto)
// Una clave que no está aquí es desconocida: aviso `warn`.

export type KeyStatus = 'supported' | 'normalized' | 'ignored' | 'silent';
export type KeyEntry = readonly [key: string, status: KeyStatus, note: string];

export interface ScopeTable {
  scope: string;
  title: string;
  /** Propiedades de texto del ámbito: aceptan además `<prop>Key` (clave de i18n). */
  textProps?: readonly string[];
  keys: readonly KeyEntry[];
}

const T = 'Texto: literal, `<clave>Key` o objeto por idioma.';

export const COMPAT: readonly ScopeTable[] = [
  {
    scope: 'root',
    title: 'Raíz de la entidad (A1 §3.1)',
    textProps: ['name', 'namePlural', 'description'],
    keys: [
      ['id', 'supported', 'Identificador de la entidad; en un manifiesto manda el `id` del manifiesto.'],
      ['name', 'supported', T],
      ['namePlural', 'supported', `${T} Si falta, se usa \`name\`.`],
      ['description', 'supported', T],
      ['icon', 'supported', 'Nombre de icono; el registro de iconos (T3) lo traduce a Phosphor.'],
      ['color', 'normalized', 'Nombre de color -> tono (`green`->success, `danger`->error, `gray`->neutral, `blue`->info, `primary`->accent). Un hexadecimal se ignora con aviso y la entidad toma un tono por hash estable de su `id`.'],
      ['apiEndpoint', 'normalized', 'Pasa a `endpoint`. Sin versión si empieza por `~/` o `/api/` (rutas fuera de `/api/v1`) o si `apiVersioned` es `false`.'],
      ['apiBaseUrl', 'ignored', 'No la lee ni Angular; la base de la API sale del manifiesto (`api.baseUrl`).'],
      ['apiVersioned', 'supported', 'Extensión propuesta en `front/README.md`: `false` para rutas sin `/v1`.'],
      ['aggregateEndpoint', 'supported', 'El detalle carga este agregado (con `{id}`) en lugar de `{endpoint}/{id}`.'],
      ['editable', 'supported', '`false`: sin alta, edición ni borrado.'],
      ['editType', 'normalized', '`direct` -> `saveMode: optimistic`; `party` no se soporta (cambia a `form` con aviso). Lo manda `saveMode` si existe.'],
      ['saveMode', 'supported', 'Extensión de la superposición: `optimistic`, `batch` o `form`.'],
      ['presentation', 'supported', 'Extensión de la superposición: `panel` (por omisión) o `page`.'],
      ['permissions', 'supported', 'Extensión del manifiesto o la superposición: `{read, write}` con permisos `Contexto.Recurso.Acción`.'],
      ['primaryKey', 'supported', 'Por omisión `id`.'],
      ['displayField', 'supported', 'Por omisión, la primera columna de texto o `primaryKey`.'],
      ['companyScoped', 'supported', '`false`: sin filtro de empresa.'],
      ['suppressCompanyColumn', 'ignored', 'React no inyecta una columna «Empresa»; el ámbito va en `X-Organization-Scope`.'],
      ['createViewType', 'ignored', 'No hay vistas a medida de alta; el alta usa el formulario.'],
      ['availableViews', 'normalized', '`grid` -> vista de rejilla; `detail` va implícito; `tree` y `map` valen si `views[]` declara esa vista (la define `views[]`); `chart`, `history`, `kanban` y `calendar` no existen (aviso).'],
      ['defaultView', 'normalized', '`grid`, `tree` o `map`; `detail`/`chart`/... caen a `grid`.'],
      ['searchable', 'supported', 'Activa la búsqueda rápida.'],
      ['searchFields', 'supported', 'Campos de la búsqueda rápida (informativo: busca el servidor).'],
      ['useInfiniteScroll', 'ignored', 'Muerta en Angular; el desplazamiento infinito es el de `dataSource.paginationStrategy`.'],
      ['actions', 'ignored', 'Muerta en Angular (no tiene lector); las acciones salen de `grid.rowActions` y `commands`.'],
      ['relations', 'ignored', 'Sólo precargaba configuraciones hijas; React las carga bajo demanda.'],
      ['hierarchy', 'normalized', '`type: tree` con `parentField` -> vista `tree` (organigrama); el resto se ignora.'],
      ['emptyState', 'supported', 'Estado vacío: `{icon, title, message}` con textos por clave o literales.'],
      ['mobileView', 'ignored', 'La rejilla es adaptable por sí misma; no hay vista de tarjetas aparte.'],
      ['metadata', 'silent', 'Sólo informativa.'],
      ['childGridConfig', 'ignored', 'No la lee Angular; los hijos son `detail.childGrids`.'],
      ['dataSource', 'normalized', 'Ver «dataSource».'],
      ['grid', 'normalized', 'Ver «grid».'],
      ['filters', 'normalized', 'Ver «filters».'],
      ['form', 'normalized', 'Ver «form».'],
      ['detail', 'normalized', 'Ver «detail».'],
      ['charts', 'ignored', 'Las gráficas no forman parte de este front (aviso).'],
      ['commands', 'supported', 'Órdenes declarativas de `front/README.md` -> `CommandDef`.'],
      ['views', 'supported', 'Extensión de la superposición: vistas `tree` y `map`.'],
      ['help', 'supported', 'Extensión: pasos de la ayuda guiada (`[{target,title,body}]` o `{steps}`).'],
      ['serverSortFields', 'supported', 'Extensión de la superposición: `{campoDeColumna: campoDeOrdenDelServidor}` o una lista de campos que el servidor ordena con su propio nombre (`["code", "name"]`).'],
      ['$schema', 'silent', 'Referencia a un esquema JSON.'],
    ],
  },
  {
    scope: 'dataSource',
    title: 'dataSource (A1 §3.2)',
    keys: [
      ['type', 'normalized', '`single`; `aggregate` y `enriched` se tratan como `single` con aviso.'],
      ['paginationStrategy', 'normalized', '`server` o `client` -> `dataSource.pagination` (por omisión `client`).'],
      ['serverFiltering', 'normalized', 'Ver «serverFiltering».'],
      ['baseFilter', 'supported', 'Se funde en el `filter` de cada búsqueda.'],
      ['listMethod', 'supported', 'Extensión: `search` (POST `{ep}/search`) o `get`.'],
      ['listStyle', 'supported', 'Extensión: `maintenance` o `number-size` (por omisión según `apiVersioned`).'],
      ['fieldMapping', 'silent', 'Retirada del modelo de Angular (declaraba filtros que no filtraban).'],
      ['sources', 'ignored', 'Sólo `aggregate` (participant en C#).'],
      ['enrichment', 'ignored', 'Sólo `enriched` (C#).'],
      ['enrichedSearchEndpoint', 'ignored', 'Búsqueda enriquecida de C#; Go usa `/search`.'],
      ['include', 'ignored', 'Búsqueda enriquecida de C#.'],
      ['cursorSearchEndpoint', 'ignored', 'Paginación por cursor de C#.'],
      ['lookups', 'ignored', 'La resolución de claves ajenas es `lookupEntity` + `useLookupLabel`.'],
      ['pairSearch', 'normalized', 'Sólo `sortFields` (columna -> campo de orden de servidor).'],
      ['rowMapping', 'ignored', 'Renombrado de campos de C#; Go devuelve los nombres definitivos.'],
      ['chartSampleSize', 'ignored', 'Sólo gráficas.'],
    ],
  },
  {
    scope: 'serverFiltering',
    title: 'dataSource.serverFiltering',
    keys: [
      ['enabled', 'silent', 'Siempre `true` en Go.'],
      ['fieldMapping', 'silent', 'Retirada del modelo de Angular.'],
      ['clientOnlyFields', 'silent', 'Vacía en Go; React filtra en el servidor.'],
      ['companyFilterField', 'supported', 'Campo del `filter` que recibe las empresas seleccionadas.'],
    ],
  },
  {
    scope: 'pairSearch',
    title: 'dataSource.pairSearch',
    keys: [['sortFields', 'normalized', 'Se convierte en `serverSortField` de las columnas.']],
  },
  {
    scope: 'grid',
    title: 'grid (A1 §3.3)',
    keys: [
      ['columns', 'supported', 'Ver «grid.columns[]».'],
      ['defaultSort', 'supported', '`{field, direction}`.'],
      ['pageSize', 'supported', 'Tamaño de página (máximo 1000).'],
      ['pageSizeOptions', 'supported', 'Opciones del selector de tamaño.'],
      ['selectable', 'supported', ''],
      ['selectionMode', 'supported', '`single` o `multiple`.'],
      ['rowActions', 'normalized', 'Cada acción `view`/`edit`/`delete` (o el id de una orden) -> `CommandDef`.'],
      ['multiSort', 'ignored', 'Muerta en Angular.'],
      ['editable', 'ignored', 'Muerta en Angular; qué se edita sale del formulario y del detalle.'],
      ['showRowNumbers', 'ignored', 'Muerta en Angular.'],
      ['resizableColumns', 'ignored', 'Muerta en Angular; las columnas se redimensionan siempre.'],
      ['reorderableColumns', 'ignored', 'Muerta en Angular.'],
      ['exportable', 'ignored', 'Muerta en Angular; la exportación no está en esta versión.'],
      ['exportFormats', 'ignored', 'Muerta en Angular.'],
      ['height', 'ignored', 'Muerta en Angular; la rejilla ocupa el espacio disponible.'],
      ['virtualScroll', 'ignored', 'Muerta en Angular; la virtualización es siempre.'],
      ['emptyMessage', 'normalized', 'Pasa a `emptyState.message`.'],
    ],
  },
  {
    scope: 'column',
    title: 'grid.columns[] (A1 §3.3)',
    textProps: ['header', 'label', 'hint', 'tooltip', 'trueLabel', 'falseLabel'],
    keys: [
      ['field', 'supported', 'Alias `dataField`.'],
      ['dataField', 'normalized', 'Alias de `field`.'],
      ['header', 'normalized', 'Pasa a `label` (alias `label`).'],
      ['label', 'normalized', 'Alias de `header`.'],
      ['type', 'normalized', 'Ver la tabla de tipos de campo; un tipo desconocido se degrada a `text` con aviso.'],
      ['width', 'normalized', 'Píxeles -> `widthPx`.'],
      ['minWidth', 'ignored', 'La rejilla fija su mínimo.'],
      ['maxWidth', 'ignored', 'La rejilla fija su máximo.'],
      ['flex', 'ignored', 'No modelada en Angular.'],
      ['sortable', 'normalized', '`sortable`; `serverSortField` sólo si el servidor lo admite (lista blanca de A2 §2, `serverSortFields` o `pairSearch.sortFields`). Si no, se ordena en cliente la página cargada (aviso).'],
      ['filterable', 'ignored', 'Los filtros son los de `filters[]`.'],
      ['editable', 'supported', 'Por omisión sale del formulario o del detalle (campo editable y no inmutable).'],
      ['visible', 'normalized', '`false` -> `hidden`.'],
      ['sticky', 'normalized', '`true`/`left` -> `pinned: start`; `right` -> `pinned: end`.'],
      ['format', 'supported', ''],
      ['currency', 'supported', 'Código de divisa de `money`.'],
      ['currencyCode', 'normalized', 'Alias de `currency`.'],
      ['enumValues', 'normalized', 'Se funde con `badgeMap` en `options`.'],
      ['badgeMap', 'normalized', 'Claves en cadena (`"true"`); se funde con `enumValues`; comparación tolerante.'],
      ['options', 'supported', 'Opciones (cadenas u objetos).'],
      ['linkConfig', 'ignored', 'Las rutas de Angular no existen; `link` se trata como `url`.'],
      ['align', 'ignored', 'La alineación sale del tipo de campo y de `dir`.'],
      ['tooltip', 'normalized', 'Pasa a `hint` si no hay `hint`.'],
      ['tooltipField', 'ignored', 'No hay tooltip por fila.'],
      ['cssClass', 'ignored', 'Sin clases de aspecto: el aspecto sale del tema.'],
      ['template', 'ignored', 'Sin plantillas HTML en cadena.'],
      ['emptyValue', 'supported', 'Texto de la celda vacía. Ausente, o hecho sólo de guiones (`-`, `–`, `—`), se pinta como «—» (vacío normalizado, R3-3); cualquier otro texto se respeta.'],
      ['trueLabel', 'supported', ''],
      ['falseLabel', 'supported', ''],
      ['hint', 'supported', ''],
      ['progressConfig', 'ignored', 'Tipo `progress` no soportado.'],
      ['trendConfig', 'ignored', 'Tipo `trend` no soportado.'],
      ['ratingConfig', 'ignored', 'Tipo `rating` no soportado.'],
      ['sparklineConfig', 'ignored', 'Tipo `sparkline` no soportado.'],
      ['lookupEntity', 'supported', 'Convierte la columna en `lookup`.'],
      ['lookupDisplayField', 'supported', 'Por omisión `name`.'],
      ['lookupValueField', 'supported', 'Por omisión `id`.'],
      ['lookupFilter', 'supported', 'Filtro fijo de la búsqueda de la clave ajena.'],
      ['lookup', 'supported', 'Forma objeto de los tres anteriores: `{entity, valueField, displayField, filter}`.'],
      ['required', 'supported', ''],
      ['pinned', 'supported', 'Forma del modelo interno (`start`/`end`).'],
      ['priority', 'supported', 'Extensión opcional de Karpo React: entero, mayor = más importante; con poco ancho se ocultan primero las columnas de menor prioridad (sin ella, de derecha a izquierda). La configuración de Angular la ignora.'],
    ],
  },
  {
    scope: 'enumValue',
    title: 'columns[].enumValues[] y badgeMap[valor]',
    textProps: ['label'],
    keys: [
      ['value', 'normalized', 'Se guarda como cadena.'],
      ['label', 'supported', ''],
      ['color', 'normalized', 'Nombre de color -> `tone`; un hexadecimal se ignora con aviso.'],
      ['tone', 'supported', 'Forma del modelo interno.'],
      ['icon', 'supported', ''],
      ['cssClass', 'ignored', 'Sin clases de aspecto.'],
    ],
  },
  {
    scope: 'rowAction',
    title: 'grid.rowActions[] (A1 §3.3)',
    textProps: ['label', 'confirmMessage'],
    keys: [
      ['action', 'normalized', '`view`, `edit`, `delete` o el `id` de una orden de `commands`; otro valor se descarta con aviso.'],
      ['label', 'supported', ''],
      ['icon', 'supported', ''],
      ['color', 'normalized', 'Nombre de color -> `tone`.'],
      ['visibleWhen', 'normalized', 'Cadena o regla -> `VisibilityRule`.'],
      ['confirm', 'normalized', '`true` -> pregunta con `confirmMessage` o con la pregunta por omisión.'],
      ['confirmMessage', 'normalized', 'Texto de la pregunta de `confirm`.'],
    ],
  },
  {
    scope: 'filter',
    title: 'filters[] (A1 §3.4)',
    textProps: ['label', 'placeholder', 'hint'],
    keys: [
      ['field', 'supported', '`_search` o `search` = búsqueda rápida.'],
      ['serverField', 'supported', 'Por omisión `search` (búsqueda rápida) o `field`.'],
      ['label', 'supported', ''],
      ['type', 'normalized', '`checkbox` -> `boolean`; `multi-select` -> `multiselect`; `number` se soporta; `date-range`, `number-range` y `autocomplete` se degradan con aviso.'],
      ['placeholder', 'supported', ''],
      ['icon', 'supported', ''],
      ['width', 'supported', 'Columnas 1 a 12.'],
      ['options', 'supported', 'Cadenas u objetos `{value,label,color,icon}`.'],
      ['defaultValue', 'supported', ''],
      ['hint', 'supported', ''],
      ['visibleWhen', 'normalized', 'Cadena o regla.'],
      ['searchFields', 'ignored', 'Sólo filtrado en cliente de Angular; busca el servidor.'],
      ['optionsEndpoint', 'ignored', 'Opciones dinámicas no soportadas todavía.'],
      ['optionsValueField', 'ignored', 'Con `optionsEndpoint`.'],
      ['optionsLabelField', 'ignored', 'Con `optionsEndpoint`.'],
      ['required', 'ignored', 'Un filtro no es obligatorio.'],
      ['operator', 'ignored', 'Go no tiene operadores (A2 §2).'],
      ['dynamic', 'ignored', 'Opciones derivadas del conjunto cargado.'],
      ['dependsOn', 'ignored', 'Filtros en cascada no soportados.'],
      ['featureFlag', 'ignored', 'Regla legada sin equivalente en Go.'],
      ['serverValueFormat', 'ignored', 'Go no admite multivalor (A2 §2).'],
      ['headerButton', 'ignored', 'Todos los filtros van en la barra.'],
      ['queryParam', 'ignored', 'Parámetro de GET sin paginar de C#.'],
    ],
  },
  {
    scope: 'option',
    title: 'options[] de filtros y campos',
    textProps: ['label'],
    keys: [
      ['value', 'normalized', 'Se guarda como cadena.'],
      ['label', 'supported', 'Si falta, el valor.'],
      ['icon', 'supported', ''],
      ['color', 'normalized', 'Nombre de color -> `tone`.'],
      ['tone', 'supported', ''],
      ['disabled', 'ignored', 'No hay opciones deshabilitadas.'],
      ['count', 'ignored', 'No hay contadores.'],
      ['parentValue', 'ignored', 'Cascadas no soportadas.'],
    ],
  },
  {
    scope: 'form',
    title: 'form (A1 §3.5)',
    textProps: ['createTitle', 'editTitle'],
    keys: [
      ['sections', 'supported', ''],
      ['groups', 'normalized', 'Alias de `sections`.'],
      ['generated', 'normalized', 'Sin secciones: se genera el formulario con las columnas editables.'],
      ['create', 'supported', '`false`: sin alta.'],
      ['edit', 'supported', '`false`: sin edición por formulario.'],
      ['createTitle', 'supported', ''],
      ['editTitle', 'supported', ''],
      ['modalSize', 'ignored', 'El detalle es un panel o una página, no un modal.'],
      ['saveButtonText', 'ignored', 'Los textos de los botones salen de i18n.'],
      ['cancelButtonText', 'ignored', 'Los textos de los botones salen de i18n.'],
      ['showCancel', 'ignored', 'Siempre hay cancelar.'],
      ['createVisibleWhen', 'ignored', 'Usa `permissions.write`.'],
      ['createRouting', 'ignored', 'Alta por tipo de C#; no soportada.'],
      ['createRoute', 'ignored', 'Alta por ruta de C#; no soportada.'],
    ],
  },
  {
    scope: 'section',
    title: 'form.sections[] y detail.sections[] (A1 §3.5, §3.6)',
    textProps: ['title', 'label', 'description'],
    keys: [
      ['id', 'supported', 'Por omisión `section-N`.'],
      ['title', 'supported', 'Alias `label`.'],
      ['label', 'normalized', 'Alias de `title`.'],
      ['description', 'supported', ''],
      ['icon', 'supported', ''],
      ['columns', 'supported', 'Columnas de la sección, de 1 a 4 (Angular lo ignoraba en el formulario).'],
      ['collapsible', 'supported', ''],
      ['collapsed', 'supported', ''],
      ['fields', 'supported', ''],
      ['visibleWhen', 'normalized', 'Cadena o regla.'],
      ['apiEndpoint', 'ignored', 'Secciones con endpoint propio no soportadas (aviso).'],
      ['upsertMethod', 'ignored', 'Con `apiEndpoint` de sección.'],
      ['upsertParentKey', 'ignored', 'Con `apiEndpoint` de sección.'],
    ],
  },
  {
    scope: 'formField',
    title: 'form.sections[].fields[] (A1 §3.5)',
    textProps: ['label', 'hint', 'placeholder', 'trueLabel', 'falseLabel'],
    keys: [
      ['field', 'supported', 'Alias `dataField`.'],
      ['dataField', 'normalized', 'Alias de `field`.'],
      ['label', 'supported', ''],
      ['type', 'normalized', '`checkbox` y `switch` -> `boolean`; `multi-select` -> `multiselect`; `guid-select` y `autocomplete` con entidad -> `lookup`; `radio` -> `select`; desconocido -> `text` con aviso.'],
      ['placeholder', 'supported', ''],
      ['defaultValue', 'supported', 'Alias `default`.'],
      ['default', 'normalized', 'Alias de `defaultValue`.'],
      ['required', 'supported', ''],
      ['readonly', 'normalized', '`true` -> `editable: false`.'],
      ['disabled', 'normalized', '`true` -> `editable: false`.'],
      ['editable', 'supported', '`false` -> sólo lectura.'],
      ['hidden', 'normalized', '`true`: el campo se omite.'],
      ['readonlyOnEdit', 'normalized', '-> `immutable` (editable en el alta, de sólo lectura después).'],
      ['createOnly', 'normalized', '-> `immutable`.'],
      ['immutable', 'supported', ''],
      ['visibleWhen', 'normalized', 'Cadena (`campo == valor`) o regla.'],
      ['disabledWhen', 'ignored', 'Muerta en Angular.'],
      ['width', 'supported', 'Columnas 1 a 12 dentro de la sección.'],
      ['options', 'supported', 'Cadenas u objetos.'],
      ['optionsEndpoint', 'ignored', 'Usa `lookupEntity` para claves ajenas.'],
      ['optionsSearch', 'ignored', 'Usa `lookupEntity`.'],
      ['optionsValueField', 'ignored', 'Con `optionsEndpoint`.'],
      ['optionsLabelField', 'ignored', 'Con `optionsEndpoint`.'],
      ['optionsExtraQueryParams', 'ignored', 'Con `optionsEndpoint`.'],
      ['requiredWhenMultipleOptions', 'ignored', 'Regla de C# sobre opciones cargadas.'],
      ['validators', 'normalized', 'Los tipos `required`, `min`, `max`, `minLength`, `maxLength` y `pattern` pasan a sus propiedades planas (Angular no los leía).'],
      ['validation', 'ignored', 'Sin equivalente.'],
      ['hint', 'supported', 'Se pinta como ayuda del campo.'],
      ['prefix', 'ignored', 'Sin prefijos de campo.'],
      ['suffix', 'ignored', 'Sin sufijos de campo.'],
      ['rows', 'ignored', 'El `textarea` se ajusta solo.'],
      ['min', 'supported', ''],
      ['max', 'supported', ''],
      ['minLength', 'supported', ''],
      ['maxLength', 'supported', ''],
      ['pattern', 'supported', 'Expresión regular; si no compila se ignora con aviso.'],
      ['format', 'supported', ''],
      ['currency', 'supported', ''],
      ['currencyCode', 'normalized', 'Alias de `currency`.'],
      ['trueLabel', 'supported', ''],
      ['falseLabel', 'supported', ''],
      ['lookupEntity', 'supported', 'Convierte el campo en `lookup`: selector con búsqueda.'],
      ['lookupDisplayField', 'supported', 'Por omisión `name`.'],
      ['lookupValueField', 'supported', 'Por omisión `id`.'],
      ['lookupFilter', 'supported', ''],
      ['lookup', 'supported', 'Forma objeto de los cuatro anteriores.'],
    ],
  },
  {
    scope: 'detail',
    title: 'detail (A1 §3.6)',
    keys: [
      ['sections', 'supported', ''],
      ['tabs', 'normalized', 'Se aplanan: las secciones de cada pestaña pasan a `sections` y su `childGrid` a `childGrids` (aviso por `component`).'],
      ['childGrids', 'supported', ''],
      ['showHistory', 'silent', 'Declarada siempre en Go, sin efecto visible en Angular.'],
      ['showRelations', 'silent', 'Declarada siempre en Go, sin efecto visible en Angular.'],
      ['showTags', 'silent', 'Declarada siempre en Go, sin efecto visible en Angular.'],
      ['headerActions', 'ignored', 'No se pintaba en Angular; las órdenes de la cabecera son `commands` con la superficie `detail-header`.'],
      ['redirectToSource', 'ignored', 'Sin equivalente.'],
    ],
  },
  {
    scope: 'detailField',
    title: 'detail.sections[].fields[] (A1 §3.6)',
    textProps: ['label', 'hint', 'placeholder', 'trueLabel', 'falseLabel'],
    keys: [
      ['field', 'supported', 'Alias `dataField`.'],
      ['dataField', 'normalized', 'Alias de `field`.'],
      ['label', 'supported', ''],
      ['type', 'normalized', '`checkbox` -> `boolean`; `currency` -> `money`; `lookup`, `decimal` y `textarea` se soportan (Angular los trataba como texto); desconocido -> `text` con aviso.'],
      ['options', 'supported', ''],
      ['icon', 'ignored', 'Sin icono por campo.'],
      ['copyable', 'ignored', 'Sin botón de copiar.'],
      ['required', 'supported', ''],
      ['editable', 'supported', 'Por omisión `true`.'],
      ['readonly', 'normalized', '`true` -> `editable: false`.'],
      ['hidden', 'normalized', '`true`: el campo se omite.'],
      ['badgeType', 'ignored', 'El tono sale de `options`.'],
      ['placeholder', 'supported', ''],
      ['format', 'supported', ''],
      ['currency', 'supported', ''],
      ['currencyCode', 'normalized', 'Alias de `currency`.'],
      ['maxLength', 'supported', ''],
      ['minLength', 'supported', ''],
      ['min', 'supported', ''],
      ['max', 'supported', ''],
      ['pattern', 'supported', ''],
      ['hint', 'supported', ''],
      ['trueLabel', 'supported', ''],
      ['falseLabel', 'supported', ''],
      ['visibleWhen', 'normalized', 'Cadena o regla.'],
      ['validation', 'ignored', 'Sin equivalente.'],
      ['optionsEndpoint', 'ignored', 'Usa `lookupEntity`.'],
      ['defaultValue', 'supported', ''],
      ['immutable', 'supported', ''],
      ['readonlyOnEdit', 'normalized', '-> `immutable`.'],
      ['createOnly', 'normalized', '-> `immutable`.'],
      ['width', 'supported', ''],
      ['lookupEntity', 'supported', ''],
      ['lookupDisplayField', 'supported', ''],
      ['lookupValueField', 'supported', ''],
      ['lookupFilter', 'supported', ''],
      ['lookup', 'supported', ''],
      ['badgeMap', 'normalized', 'Igual que en columnas.'],
      ['enumValues', 'normalized', 'Igual que en columnas.'],
    ],
  },
  {
    scope: 'tab',
    title: 'detail.tabs[]',
    textProps: ['label'],
    keys: [
      ['id', 'supported', ''],
      ['label', 'supported', ''],
      ['icon', 'ignored', 'Sin icono de pestaña.'],
      ['sections', 'supported', ''],
      ['childGrid', 'supported', ''],
      ['component', 'ignored', 'Componentes a medida de Angular (aviso).'],
    ],
  },
  {
    scope: 'childGrid',
    title: 'detail.childGrids[] (A1 §3.6)',
    textProps: ['label', 'title', 'emptyMessage'],
    keys: [
      ['id', 'supported', ''],
      ['label', 'normalized', 'Pasa a `title` si no hay `title`.'],
      ['title', 'supported', ''],
      ['icon', 'ignored', 'Sin icono de pestaña.'],
      ['entityId', 'normalized', 'Sin `apiEndpoint`, el endpoint es `/{entityId}s` (aviso).'],
      ['apiEndpoint', 'normalized', 'Se sustituye `{id}` y los `{campo}` con la fila del padre; no se añade `?foreignKey=` si ya lleva `{id}`.'],
      ['foreignKey', 'normalized', 'Sólo si el endpoint no lleva `{id}`: se añade `?<foreignKey>={id}`.'],
      ['dataField', 'ignored', 'Datos en línea del registro padre: no soportado (aviso; el hijo se descarta).'],
      ['field', 'ignored', 'Datos en línea del registro padre: no soportado (aviso; el hijo se descarta).'],
      ['columns', 'supported', ''],
      ['emptyMessage', 'supported', ''],
      ['pageSize', 'ignored', 'Los hijos son tablas de sólo lectura sin paginar.'],
      ['visibleWhen', 'ignored', 'Sin regla por hijo.'],
      ['rowActions', 'ignored', 'Los hijos son de sólo lectura.'],
    ],
  },
  {
    scope: 'childColumn',
    title: 'detail.childGrids[].columns[]',
    textProps: ['header', 'label', 'trueLabel', 'falseLabel'],
    keys: [
      ['field', 'supported', ''],
      ['header', 'normalized', 'Pasa a `label`.'],
      ['label', 'normalized', 'Alias de `header`.'],
      ['type', 'normalized', 'Igual que en columnas.'],
      ['width', 'normalized', 'Píxeles -> `widthPx`.'],
      ['minWidth', 'ignored', ''],
      ['emptyValue', 'supported', ''],
      ['trueLabel', 'supported', ''],
      ['falseLabel', 'supported', ''],
      ['format', 'supported', ''],
    ],
  },
  {
    scope: 'command',
    title: 'commands[] (front/README.md)',
    textProps: ['label', 'confirm', 'confirmMessage', 'successMessage', 'acceptedMessage'],
    keys: [
      ['id', 'supported', ''],
      ['label', 'supported', ''],
      ['icon', 'supported', ''],
      ['color', 'normalized', 'Nombre de color -> `tone`.'],
      ['tone', 'supported', 'Tono explícito (`success`, `error`, `warning`, `info`, `neutral`, `accent`); manda sobre `color`.'],
      ['surfaces', 'supported', '`row`, `detail-header`, `toolbar`; por omisión `row` y `detail-header`.'],
      ['visibleWhen', 'normalized', 'Cláusula sobre un campo de la fila (`{field, equals}`) u otra regla.'],
      ['method', 'supported', '`POST` (por omisión), `PUT` o `DELETE`.'],
      ['endpoint', 'supported', 'Con `{id}` y `{campo}` sustituidos con la fila.'],
      ['confirm', 'normalized', 'Cadena, objeto por idioma o `true`.'],
      ['confirmMessage', 'supported', 'Texto de la pregunta cuando `confirm` es `true`.'],
      ['prompt', 'supported', 'Diálogo con un texto (motivo) que viaja en el cuerpo.'],
      ['sendVersion', 'supported', 'Añade `version` de la fila al cuerpo.'],
      ['successMessage', 'supported', ''],
      ['acceptedMessage', 'supported', 'Mensaje de «pendiente de aprobación» (202).'],
    ],
  },
  {
    scope: 'prompt',
    title: 'commands[].prompt',
    textProps: ['label'],
    keys: [
      ['field', 'supported', 'Nombre del campo del cuerpo (por ejemplo `reason`).'],
      ['label', 'supported', ''],
      ['required', 'supported', ''],
      ['maxLength', 'supported', ''],
    ],
  },
  {
    scope: 'hierarchy',
    title: 'hierarchy (A1 §3.1)',
    keys: [
      ['type', 'normalized', '`tree` -> vista de organigrama.'],
      ['parentField', 'normalized', 'Pasa a `parentField` de la vista.'],
      ['typeField', 'ignored', 'Sin tipo de nodo.'],
      ['multiSelect', 'ignored', 'Sin selección múltiple de nodos.'],
      ['showCounts', 'ignored', 'El contador de descendientes es siempre visible.'],
      ['levels', 'ignored', 'Sin niveles con nombre.'],
    ],
  },
  {
    scope: 'emptyState',
    title: 'emptyState',
    textProps: ['title', 'message'],
    keys: [
      ['icon', 'supported', ''],
      ['title', 'supported', ''],
      ['message', 'supported', ''],
    ],
  },
  {
    scope: 'viewTree',
    title: 'views[] con `kind: tree` (extensión)',
    keys: [
      ['kind', 'supported', ''],
      ['label', 'ignored', 'El conmutador de vista usa los textos de la interfaz (`core:view.*`), no uno por entidad.'],
      ['labelKey', 'ignored', 'Igual que `label`.'],
      ['parentField', 'supported', 'Obligatoria.'],
      ['labelField', 'supported', 'Obligatoria (por omisión `displayField`).'],
      ['subtitleField', 'supported', ''],
      ['badgeField', 'supported', ''],
      ['source', 'supported', '`{type: flat}` o `{type: chart, endpoint, rootsFrom, depth, childrenField, nodeField}`.'],
    ],
  },
  {
    scope: 'viewMap',
    title: 'views[] con `kind: map` (extensión)',
    keys: [
      ['kind', 'supported', ''],
      ['label', 'ignored', 'El conmutador de vista usa los textos de la interfaz (`core:view.*`), no uno por entidad.'],
      ['labelKey', 'ignored', 'Igual que `label`.'],
      ['boundaryField', 'supported', 'Obligatoria.'],
      ['levels', 'supported', 'Obligatoria: `[{id, label, boundaryLevel, codeProperty, source}]`.'],
      ['aggregate', 'supported', '`count` (por omisión) o `sum`.'],
      ['valueField', 'supported', 'Con `sum`.'],
      ['skipSingle', 'supported', 'Un nivel con una sola delimitación se salta al navegar (Q-10). Por omisión `true`; `false` conserva ese nivel.'],
    ],
  },
  {
    scope: 'mapLevel',
    title: 'views[].levels[] del mapa',
    textProps: ['label'],
    keys: [
      ['id', 'supported', '`world`, `country`, `region`, `province` o `city`.'],
      ['label', 'supported', ''],
      ['boundaryLevel', 'supported', ''],
      ['codeProperty', 'supported', ''],
      ['source', 'supported', 'Id en `manifest.map.sources`.'],
      ['defaultCode', 'supported', 'Delimitación que se abre por omisión en el nivel (`null`: la raíz).'],
    ],
  },
  {
    scope: 'helpStep',
    title: 'help[] (extensión)',
    textProps: ['title', 'body'],
    keys: [
      ['target', 'supported', 'Ancla `data-kx-help` o selector CSS.'],
      ['title', 'supported', ''],
      ['body', 'supported', ''],
    ],
  },
  {
    scope: 'manifest',
    title: 'manifest.json (PLAN §4.3.1)',
    keys: [
      ['schemaVersion', 'supported', 'Debe ser 1.'],
      ['api', 'supported', '`{baseUrl, apiVersion}`; por omisión `/api` y `v1`.'],
      ['locales', 'supported', 'Por omisión `["es","en"]`.'],
      ['defaultLocale', 'supported', 'Por omisión el primero de `locales`.'],
      ['i18n', 'supported', '`{idioma: url}` del diccionario de la configuración (relativa al manifiesto).'],
      ['theme', 'supported', '`{origin, default, available}`.'],
      ['map', 'supported', '`{sources: {id: {type, url, layer?, schematic?}}}`.'],
      ['entities', 'supported', 'Lista de `{id, path | config, permissions, overlay}`.'],
      ['menu', 'supported', 'Árbol de `{id, label | labelKey, icon, entity, children, visibleWhen}`.'],
      ['$schema', 'silent', ''],
    ],
  },
  {
    scope: 'manifestEntity',
    title: 'manifest.entities[]',
    keys: [
      ['id', 'supported', ''],
      ['path', 'supported', 'Relativa a la carpeta del manifiesto.'],
      ['config', 'supported', 'Configuración en línea, en lugar de `path`.'],
      ['permissions', 'supported', '`{read, write}`.'],
      ['overlay', 'supported', 'Se funde en profundidad ANTES de normalizar.'],
      ['overlayPath', 'supported', 'Superposición en un fichero JSON (relativo al manifiesto): se funde antes que `overlay` y sirve para las superposiciones generadas (textos con `*Key` de `npm run i18n:demo`). Si no se puede leer, aviso `warn` y la entidad se carga sin ella.'],
    ],
  },
  {
    scope: 'manifestMenu',
    title: 'manifest.menu[]',
    textProps: ['label'],
    keys: [
      ['id', 'supported', ''],
      ['label', 'supported', ''],
      ['icon', 'supported', ''],
      ['entity', 'supported', ''],
      ['children', 'supported', ''],
      ['visibleWhen', 'normalized', ''],
    ],
  },
  {
    scope: 'index',
    title: 'index.json de Angular (A1 §2.1)',
    keys: [
      ['version', 'silent', ''],
      ['description', 'silent', ''],
      ['entities', 'supported', '`{id: ruta}` relativa al índice.'],
      ['domains', 'ignored', 'Sólo servían a `getEntitiesByDomain()`.'],
    ],
  },
  {
    scope: 'sidebarContext',
    title: 'sidebar-config.json: contextos (A1 §3.9)',
    textProps: ['label'],
    keys: [
      ['id', 'supported', ''],
      ['label', 'supported', ''],
      ['icon', 'supported', ''],
      ['items', 'supported', ''],
    ],
  },
  {
    scope: 'sidebarItem',
    title: 'sidebar-config.json: ítems de navegación (A1 §3.9)',
    textProps: ['label'],
    keys: [
      ['id', 'supported', ''],
      ['label', 'supported', ''],
      ['icon', 'supported', ''],
      ['children', 'supported', ''],
      ['entityId', 'normalized', '-> `entity`.'],
      ['viewType', 'ignored', 'Las vistas se eligen en la entidad.'],
      ['type', 'normalized', '`folder` y `document` se tratan igual; `company` y `company-filter` no existen (el ámbito es un selector aparte).'],
      ['selectable', 'ignored', ''],
      ['requiresCompanySelection', 'ignored', ''],
      ['externalUrl', 'ignored', 'Los ítems con iframe externo se descartan (aviso).'],
      ['embedSupported', 'ignored', ''],
      ['badge', 'ignored', ''],
      ['roles', 'ignored', 'Regla legada sin equivalente en Go (aviso).'],
      ['sector', 'ignored', 'Regla legada sin equivalente en Go (aviso).'],
      ['capability', 'ignored', 'Regla legada sin equivalente en Go (aviso).'],
      ['featureFlag', 'ignored', 'Regla legada sin equivalente en Go (aviso).'],
      ['visibleWhen', 'normalized', 'Sólo se conservan `permission`, `field`/`equals`, `expr`, `allOf`, `anyOf`, `not`; las cláusulas legadas se descartan con aviso.'],
      ['quickAccess', 'ignored', ''],
    ],
  },
  {
    scope: 'visibleWhen',
    title: 'visibleWhen (A1 §3.8)',
    keys: [
      ['permission', 'supported', 'Con comodines `*.*.*`; fail-closed.'],
      ['expr', 'supported', '`campo == valor` o `campo != valor`.'],
      ['field', 'supported', 'Con `equals`; sin él, el campo ha de ser «verdadero».'],
      ['equals', 'supported', 'Comparación tolerante (`true` == `"true"`).'],
      ['allOf', 'supported', ''],
      ['anyOf', 'supported', ''],
      ['not', 'supported', ''],
      ['hasRole', 'ignored', 'Rol del registro: sin equivalente (la cláusula se descarta con aviso).'],
      ['capability', 'ignored', 'Capacidad del tenant: sin equivalente en Go.'],
      ['sector', 'ignored', 'Sector del tenant: sin equivalente en Go.'],
      ['userRole', 'ignored', 'Rol de usuario de C#: usa `permission`.'],
      ['requiredModule', 'ignored', 'Módulo activo: sin equivalente.'],
      ['featureFlag', 'ignored', 'Sin banderas de funcionalidad.'],
      ['roles', 'ignored', 'Regla legada.'],
      ['accessMode', 'ignored', 'Siempre `open`.'],
    ],
  },
  {
    scope: 'runtimeConfig',
    title: 'runtime-config.json de Angular (A1 §3.10)',
    keys: [
      ['apiUrl', 'ignored', 'La base de la API sale del manifiesto (`api.baseUrl`) o de `apiBaseUrl`.'],
      ['authUrl', 'ignored', 'Las rutas de autenticación cuelgan de `api.baseUrl`.'],
      ['apiVersion', 'ignored', 'Sale del manifiesto (`api.apiVersion`).'],
      ['featureFlags', 'ignored', 'Sin banderas de funcionalidad.'],
    ],
  },
];

const BY_SCOPE = new Map(COMPAT.map((t) => [t.scope, t]));
const LOOKUP_CACHE = new Map<string, ReadonlyMap<string, KeyEntry>>();

function entriesFor(scope: string): ReadonlyMap<string, KeyEntry> {
  const cached = LOOKUP_CACHE.get(scope);
  if (cached) return cached;
  const table = BY_SCOPE.get(scope);
  const map = new Map<string, KeyEntry>();
  if (table) {
    for (const entry of table.keys) map.set(entry[0], entry);
    for (const prop of table.textProps ?? []) {
      const key = `${prop}Key`;
      if (!map.has(key)) map.set(key, [key, 'supported', `Clave de i18n de \`${prop}\`.`]);
    }
  }
  LOOKUP_CACHE.set(scope, map);
  return map;
}

/** Estado de una clave en un ámbito, o `undefined` si es desconocida. */
export function keyStatus(scope: string, key: string): KeyEntry | undefined {
  return entriesFor(scope).get(key);
}

/** Claves con nombre de comentario o de herramienta, que se ignoran en silencio. */
const COMMENT_KEY = /^(\$|_|x-|\/\/)/;

export interface KeyWarning {
  key: string;
  severity: 'info' | 'warn';
  message: string;
}

/** Revisa las claves de un objeto del ámbito dado y devuelve los avisos (ignoradas y desconocidas). */
export function checkKeys(scope: string, obj: Record<string, unknown>): KeyWarning[] {
  const out: KeyWarning[] = [];
  const known = entriesFor(scope);
  for (const key of Object.keys(obj)) {
    if (COMMENT_KEY.test(key) && !known.has(key)) continue;
    const entry = known.get(key);
    if (!entry) {
      out.push({ key, severity: 'warn', message: `clave desconocida \`${key}\`: se ignora` });
    } else if (entry[1] === 'ignored') {
      out.push({
        key,
        severity: 'info',
        message: `clave \`${key}\` ignorada${entry[2] ? `: ${entry[2]}` : ''}`,
      });
    }
  }
  return out;
}

const STATUS_LABEL: Record<KeyStatus, string> = {
  supported: 'soportada',
  normalized: 'normalizada',
  ignored: 'ignorada',
  silent: 'ignorada (sin aviso)',
};

function cell(text: string): string {
  return text.replace(/\|/g, '\\|');
}

/** Tabla Markdown de compatibilidad (la que figura entre los marcadores de COMPATIBILIDAD-CONFIG.md). */
export function renderCompatTable(): string {
  const lines: string[] = [];
  for (const table of COMPAT) {
    lines.push(`### ${table.title}`, '');
    lines.push(`Ámbito \`${table.scope}\`.`, '');
    lines.push('| Clave | Estado | Cómo se trata |', '| --- | --- | --- |');
    for (const [key, status, note] of table.keys) {
      lines.push(`| \`${key}\` | ${STATUS_LABEL[status]} | ${cell(note) || '—'} |`);
    }
    if (table.textProps?.length) {
      const keys = table.textProps.map((p) => `\`${p}Key\``).join(', ');
      lines.push(`| ${keys} | soportada | Clave de i18n de la propiedad de texto correspondiente. |`);
    }
    lines.push('');
  }
  return lines.join('\n').trimEnd();
}
