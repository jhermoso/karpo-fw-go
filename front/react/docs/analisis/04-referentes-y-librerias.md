# 04 - Referentes de producto y librerías

Análisis para el front React de Karpo Go (`@karpo/react`). Cubre cómo resuelven los mejores productos de gestión los problemas de interfaz que nos tocan, qué librerías npm podemos usar con licencia verificada, y una lista de ideas concretas para Karpo.

## 0. Método y límites

- **Licencias y versiones**: consultadas el 2026-11-27 con `npm view` contra el registro público. Además se instaló en un directorio temporal (fuera del repo, con `--ignore-scripts`) el conjunto candidato completo y se leyó el campo `license` de las 249 dependencias resultantes (apartado 3).
- **Referentes (Linear, Notion, Airtable, Retool, Stripe, Attio)**: esta pasada NO ha hecho consultas web en vivo; se apoya en el comportamiento público y documentado de los productos. Lo que sigue son patrones de interacción, no citas. Antes de copiar un detalle fino (atajos concretos, tiempos de animación) conviene contrastarlo con el producto real en una pasada de revisión.
- No se copia código ni activos de ningún referente: se toman ideas de interacción.
- Config existente examinada: `front/angular/config/entities/**`. Claves de entidad: `grid`, `form.sections`, `filters`, `detail.sections`, `availableViews`, `defaultView`, `searchFields`, `dataSource.serverFiltering`, `grid.rowActions`, `editType`, tipos de columna `text`/`badge`... El front React la leerá tal cual y la ampliará de forma retrocompatible (apartado 5).

## 1. Qué hacen bien los referentes

### 1.1 Cuadrículas editables

| Producto | Patrón que merece copiarse |
|---|---|
| **Airtable** | Selección de celda y de rango tipo hoja de cálculo; Enter/F2 edita, Esc cancela, Tab/Enter guardan y avanzan; copiar/pegar de rangos desde Excel con coerción por tipo de campo; relleno hacia abajo; deshacer/rehacer a nivel de tabla; expandir registro a un panel. Edición siempre optimista. |
| **Notion (bases de datos)** | La celda es el editor: no hay "modo edición" de fila; propiedades tipadas con selectores propios (select, fecha, relación). Cada vista guarda filtros, orden y columnas visibles. Abrir fila = peek lateral, con opción "abrir como página". |
| **Retool (Table)** | Edición en línea con cambios acumulados ("pending changes") y botón Guardar por lotes; indicadores visuales de celda modificada; validación por columna; acciones de fila configurables. Es el modelo más cercano al "guardado por lotes" que pide Javier. |
| **Linear** | Teclado primero: `j/k`, flechas, `x` selecciona, `Enter` abre, `Cmd+K` para todo; cambios de propiedad inmediatos (optimistas) con deshacer inmediato y toast "Deshacer". Cero espera percibida: la UI cambia antes de que responda el servidor. |
| **Stripe Dashboard** | Tablas densas con filtros como "chips" componibles (campo + operador + valor), columnas configurables, exportación, y detalle en página con pestañas. Cifras tabulares y alineación a la derecha de importes. |
| **Attio** | Modelo de "registro" universal: lista, detalle y ficha comparten los mismos campos tipados; vistas guardadas por usuario y por equipo; selectores de relación con búsqueda; orden por varias columnas. |

Síntesis para Karpo: la cuadrícula es un **editor de hoja de cálculo tipado** donde cada celda reutiliza el editor del mismo tipo de campo que usa el formulario (un único registro de tipos de campo en la config), con un **almacén de cambios pendientes** (modelo Retool) que admite los dos modos: guardado optimista por celda (Linear/Airtable) o por lote (Retool), elegible por entidad (`editType`).

### 1.2 Detalle lateral frente a página

- Linear, Notion y Attio abren **un panel lateral** (peek) al seleccionar y permiten "expandir" a página completa con URL propia. Stripe usa página completa con pestañas. Airtable expande en modal.
- Regla común: **el panel es navegable por URL** (compartible), conserva el contexto de la lista (flechas arriba/abajo para pasar al registro siguiente sin cerrarlo) y no destruye la selección ni el scroll.
- Decisión propuesta: tres presentaciones del mismo detalle, configurables por entidad (`detail.presentation`: `panel` | `page` | `modal`), con `panel` por defecto. La ruta es la fuente de verdad (`/:entidad/:id?modo=edicion`), de modo que el doble clic de Javier ("detalle en modo edición") es solo navegar a esa ruta.

### 1.3 Paleta de comandos

- Linear, Notion, Attio y Stripe tienen `Cmd/Ctrl+K`: búsqueda difusa unificada (navegar, crear, buscar registros, cambiar tema/idioma, acciones contextuales de la selección).
- Buenas prácticas: resultados agrupados con cabecera, atajo visible junto a cada acción, comandos contextuales (distintos si hay filas seleccionadas), recientes primero, búsqueda de registros contra el servidor con debounce.
- Karpo: la paleta se alimenta **de la configuración**: cada entidad registra "Ir a lista de X", "Nuevo X" y sus `rowActions` como comandos cuando hay selección. Sin código por pantalla.

### 1.4 Vistas alternativas (tablero, organigrama, mapa)

- **Airtable/Notion/Attio/Linear**: selector de vista como pestañas o menú (Tabla, Tablero, Calendario, Galería, Cronograma); cada vista conserva sus propios filtros, orden y agrupación; el cambio de vista no recarga datos.
- `availableViews`/`defaultView` de la config ya modela esto. Se amplía con `tree` (organigrama), `map` (geografía) y `board`.
- **Organigrama/árbol** (lo hacen productos de RR. HH. y diagramación más que los seis referentes): nodos tarjeta con avatar y cargo, plegado por rama con contador de descendientes, "ajustar a pantalla", minimapa, búsqueda que centra y resalta el nodo, zoom con rueda y gestos, navegación con teclado (padre, hijos, hermanos).
- **Mapa por niveles**: patrón "drill-down" (mundo, país, región, ciudad): coropletas por nivel, migas de pan, clic para entrar, Esc para subir, tabla sincronizada con lo visible.

### 1.5 Recorridos de ayuda

- Linear/Notion/Attio usan listas de primeros pasos y tooltips contextuales más que tours largos. Los tours buenos son cortos (4 a 7 pasos), se pueden saltar y reanudar, y destacan el elemento real con un halo.
- Karpo: botón de ayuda fijo en la cabecera que lanza un recorrido **generado desde la configuración** (cada sección/columna/acción puede declarar `help: { title, body }` traducible); si no lo declara, se genera uno por defecto con los nombres traducidos de los elementos principales. driver.js (MIT) lo ejecuta.

### 1.6 Densidad y tipografía

- Linear y Stripe: densidad compacta por defecto (fila de 32 a 36 px, texto de 13 px), jerarquía por peso y color más que por tamaño, cifras tabulares, bordes de 1 px de baja opacidad.
- Notion/Attio: más aire en la ficha de detalle que en las listas.
- Karpo: **tres densidades** (compacta, normal, cómoda) como variable de tema derivada de los tokens de espaciado de Theros, conmutable por usuario. Todas las medidas salen de variables.

### 1.7 Microinteracciones y rendimiento percibido

- Actualización optimista con reversión limpia y mensaje; esqueletos con la forma real de la tabla en vez de spinners; "stale-while-revalidate" para que volver a una lista sea instantáneo; precarga del detalle al enfocar una fila; transiciones de 100 a 200 ms respetando `prefers-reduced-motion`; foco visible coherente; destello suave en la fila recién guardada y borde de error en la fallida.
- Virtualización de filas y columnas; paginación por cursor en servidor; scroll infinito opcional.
- Estado en la URL (filtros, orden, vista, selección) para compartir y recuperar con "atrás".

### 1.8 Accesibilidad

- Linear/Stripe cumplen contraste y foco AA; las cuadrículas son el punto débil común. Para Karpo: rol `grid` con `aria-rowindex`/`aria-colindex` (necesario con virtualización), foco por celda con roving tabindex, anuncios `aria-live` para guardado y errores, diálogos con trampa de foco, atajos documentados, objetivos táctiles de 24 px mínimo (WCAG 2.2), respeto de `prefers-reduced-motion` y `forced-colors`.
- **axe-core es MPL-2.0 y no puede ser dependencia directa.** `vitest-axe` y `jest-axe` (MIT) lo traen como dependencia. Ver consulta C-1.

## 2. Librerías: licencia real y uso propuesto

Licencias verificadas con `npm view <paquete> license version` el 2026-11-27.

### 2.1 Aceptables como dependencias directas (MIT, Apache-2.0, BSD)

| Función | Paquete | Versión | Licencia | Notas |
|---|---|---|---|---|
| Framework UI | `react`, `react-dom` | 19.3.1 | MIT | |
| Compilación | `vite`, `@vitejs/plugin-react` | 8.4.0 / 6.3.0 | MIT | Vite trae `lightningcss` MPL-2.0 como transitiva (apartado 3). |
| Lenguaje | `typescript` | 7.0.2 | Apache-2.0 | |
| Enrutado | `react-router` | 8.5.3 | MIT | Alternativa: `@tanstack/react-router` 1.170 (MIT), con estado de búsqueda tipado en la URL. Decisión de arquitectura. |
| Cuadrícula (sin DOM) | `@tanstack/react-table` | 9.3.1 | MIT | Ordenación, filtros, agrupación, fijado y redimensionado. Depende de `@tanstack/react-store` (MIT). |
| Virtualización | `@tanstack/react-virtual` | 3.14.28 | MIT | Filas y columnas; necesaria para `aria-rowindex`. |
| Estado de servidor | `@tanstack/react-query` | 5.108.4 | MIT | Mutaciones optimistas con reversión, caché, `prefetchQuery`. |
| Formularios | `react-hook-form` | 7.92.0 | MIT | Alternativa: `@tanstack/react-form` 1.33 (MIT). |
| Validación | `zod` | 4.6.7 | MIT | Esquema generado desde la config (`required`, `maxLength`, `pattern`...). Alternativa ligera: `valibot` 1.5 (MIT). Puente `@hookform/resolvers` 7.0.9 (MIT). |
| i18n | `i18next` + `react-i18next` | 27.2.9 / 18.0.3 | MIT | Espacios de nombres por entidad, carga diferida. Alternativas: `@formatjs/intl` 6.2 (MIT) / `react-intl` 12.2 (BSD-3); `@lingui/core` 6.12 (MIT). Fechas, números y monedas con `Intl` nativo. |
| Árbol/organigrama | `@xyflow/react` | 13.0.3 | MIT | Lienzo con zoom, paneo, minimapa, nodos React. Depende de `zustand`, `classcat`, `@xyflow/system`; sus `d3-*` (ISC) son transitivas. |
| Disposición de árbol | `@dagrejs/dagre` | 3.1.4 | MIT | Disposición jerárquica. |
| Mapas | `maplibre-gl` | 6.19.0 | BSD-3-Clause | WebGL, teselas vectoriales. |
| Teselas alojadas | `pmtiles` | 4.5.0 | BSD-3-Clause | Protocolo `pmtiles://` en MapLibre: un fichero estático con peticiones de rango; origen configurable. Depende de `fflate` (MIT). |
| Estilos de mapa | `@protomaps/basemaps` | 5.7.2 | BSD-3-Clause | Solo capas de estilo, no datos (ver 2.3). |
| Recorridos | `driver.js` | 1.14.0 | MIT | Sin dependencias. Shepherd.js prohibido. |
| Paleta de comandos | `cmdk` | 1.1.1 | MIT | Sin estilos propios, accesible. Alternativa: `@ariakit/react` 0.4 (MIT) o `react-aria-components` 1.22 (Apache-2.0). |
| Primitivas accesibles | `@radix-ui/react-*` | 1.x | MIT | Diálogo, popover, menú, tooltip, pestañas. Alternativa más completa en cuadrículas y fechas: `react-aria-components` (Apache-2.0). |
| Posicionamiento | `@floating-ui/react` | 0.27.20 | MIT | Popovers de celda y menús. |
| Iconos | `@phosphor-icons/react` 2.1.10 o `@tabler/icons-react` 3.53 | | MIT | **`lucide-react` es ISC: no permitido como directa** (C-2). Los iconos de la config (`package`, `view`...) se mapean por nombre. |
| Animación | `motion` | 14.14.0 | MIT | Solo si CSS no basta. |
| Paneles | `react-resizable-panels` | 4.24.3 | MIT | Panel de detalle redimensionable. |
| Atajos | `tinykeys` 4.0.1 / `react-hotkeys-hook` 5.4.0 / `@tanstack/hotkeys` 0.36 | | MIT | Preferible un gestor propio sobre `keydown` con ámbitos (cuadrícula, panel, global). |
| Búsqueda difusa | `fuse.js` | 7.5.0 | Apache-2.0 | Paleta en cliente. |
| Arrastrar y soltar | `@dnd-kit/core` | 6.3.1 | MIT | Tablero y reordenación de columnas. |
| Deshacer | `immer` | 11.2.13 | MIT | `produceWithPatches` da parches y parches inversos: base de la pila deshacer/rehacer. |
| Estado de UI | `zustand` | 5.1.0 | MIT | Cambios pendientes, selección, preferencias. |
| Pruebas | `vitest` 5.1.2, `@testing-library/react` 16.3.3, `@testing-library/user-event`, `jsdom` | | MIT | |
| Mocks de red | `msw` | 3.0.8 | MIT | Falsear la API Go en pruebas y demo. |
| e2e y capturas | `playwright-core` | 1.65.1 | Apache-2.0 | Con `channel: 'msedge'` o `'chrome'`; no descarga navegadores. |

### 2.2 No utilizables como dependencia directa

| Paquete | Licencia | Motivo / alternativa |
|---|---|---|
| `elkjs` 0.12.2 | EPL-2.0 OR GPL-3.0-or-later | Copyleft (EPL por archivo, la alternativa GPL). Usar `@dagrejs/dagre`. Consulta C-4. |
| `axe-core` 4.14.1 | MPL-2.0 | Ver C-1. |
| `d3-hierarchy`, `d3-zoom`, `topojson-client`, `world-atlas` | ISC | ISC no está en la lista de directas. `@xyflow/react` ya cubre zoom y paneo. |
| `lucide-react` 1.81.0 | ISC | Usar Phosphor o Tabler. |
| `@fontsource/*` | OFL-1.1 | No está en la lista (C-3). |
| `lightningcss` | MPL-2.0 | Solo transitiva de herramientas de compilación. |
| AG Grid Enterprise, Handsontable, Shepherd.js | comercial | Prohibidas por requisito. |
| `world-countries` | sin campo `license` en el registro | No usar sin revisar el repositorio. |

`@turf/turf` 7.4.0 es MIT, pero grande; si hace falta, usar paquetes `@turf/*` sueltos.

### 2.3 Datos geográficos para las vistas de mapa (el punto más delicado)

Requisito: sin teselas de terceros en ejecución y sin descargar nada fuera de npm.

- **Hallazgo**: los paquetes permitidos aportan el motor (`maplibre-gl`, `pmtiles`) y el estilo (`@protomaps/basemaps`), pero **no un fichero `.pmtiles` del mundo**. Bajar un extracto de OSM/Protomaps es una descarga fuera de npm y queda descartada.
- Paquetes de datos comprobados: `world-atlas` y `topojson-client` (ISC), `world-countries` (sin licencia declarada en el registro), `i18n-iso-countries` (MIT, solo nombres sin geometría). Ninguno es MIT/Apache/BSD con geometría.
- **Propuesta en capas**, de menos a más fidelidad:
  1. **Demo y modo sin teselas**: geometrías GeoJSON simplificadas como datos del despliegue (la geografía de Karpo ya tiene países, regiones NUTS, ver `docs/GEOGRAFIA-NUTS.md`, y ciudades en su API), servidas desde un origen configurable, dibujadas por MapLibre como fuente `geojson` (no requiere ninguna tesela) con coropletas por nivel (mundo, país, región, ciudad). Etiquetas como marcadores HTML para no necesitar glifos.
  2. **Producción**: MapLibre + `pmtiles://` contra un origen configurable (`map.tilesUrl`, `map.glyphsUrl`, `map.spriteUrl`) alojado por el propio despliegue. Se documenta cómo producir el `.pmtiles` (herramienta externa a este repo) y dónde colocarlo.
  3. **Glifos y sprites**: las etiquetas vectoriales necesitan glifos PBF, otro activo alojado; en la capa 1 se evitan.
- Anotar en `PETICIONES-BACKEND.md`: endpoint de geometrías simplificadas por nivel (`/api/v1/geography/geometries?level=country|region|city`) o empaquetado de un `.pmtiles` por el despliegue.
- **Consulta C-5**: de dónde salen las geometrías de demo (ver apartado 4).

## 3. Informe de licencias (muestra real del conjunto candidato)

Instalación de prueba de: react, react-dom, @tanstack/react-table, react-virtual, react-query, @xyflow/react, @dagrejs/dagre, maplibre-gl, pmtiles, driver.js, i18next, react-i18next, react-hook-form, zod, @hookform/resolvers, react-router, cmdk, vitest, @testing-library/react, user-event, playwright-core, @radix-ui/react-dialog, popover, @floating-ui/react, @phosphor-icons/react, @protomaps/basemaps, vite, @vitejs/plugin-react, typescript, jsdom, msw. Resultado: **249 paquetes**.

| Licencia | Nº | Veredicto |
|---|---|---|
| MIT | 192 | Permitida |
| Apache-2.0 | 7 | Permitida |
| BSD-3-Clause / BSD-2-Clause | 8 / 4 | Permitida |
| (MIT OR Apache-2.0) | 1 (`@maplibre/mlt`) | Permitida |
| ISC | 25 | Transitivas aceptables (d3-*, kdbush, earcut, picocolors, yargs...) |
| MIT-0 | 2 (`@csstools/*`) | Permisiva |
| 0BSD | 1 (`tslib`) | Permisiva |
| CC0-1.0 | 1 (`mdn-data`) | Dominio público |
| BlueOak-1.0.0 | 1 (`lru-cache`) | Permisiva |
| (MIT OR CC0-1.0) | 1 (`type-fest`) | Permisiva |
| **MPL-2.0** | **2** (`lightningcss`, `lightningcss-win32-x64-msvc`) | **Copyleft débil por archivo; transitiva de la herramienta de compilación (vite), no se distribuye en el paquete publicado. Debe constar en el informe final (C-6).** |

Ninguna GPL, AGPL, LGPL ni comercial. El informe definitivo se regenerará con el `package-lock.json` real del proyecto (script que lee `node_modules/**/package.json`, sin dependencias nuevas). CC-BY (`caniuse-lite`) no apareció en esta muestra; puede surgir con `browserslist`.

Descargas: `playwright-core` no baja navegadores (solo `playwright` / `@playwright/test` con su postinstall lo harían); instalar siempre con `--ignore-scripts` donde no haga falta un script.

## 4. Consultas abiertas a Javier

- **C-1 Accesibilidad automática**: `axe-core` (MPL-2.0) es el motor de referencia. Opciones: (a) permitirlo solo como `devDependency` de pruebas, nunca empaquetada ni distribuida; (b) sustituirlo por comprobaciones propias con Testing Library (`getByRole` ya exige nombre accesible) y revisión manual. Recomendación: (b) por defecto, (a) con tu aprobación.
- **C-2 Iconos**: Lucide es ISC; se propone Phosphor (MIT).
- **C-3 Fuentes**: OFL-1.1 no está en la lista. Propuesta: no empaquetar fuentes; usar la variable de fuente del tema de Theros con pila del sistema como respaldo.
- **C-4 ELK**: mejor para árboles enormes, pero EPL/GPL. Propuesta: dagre más plegado por ramas; ELK descartado.
- **C-5 Geometrías de demo**: (a) que Karpo Go las sirva o las genere (más soberano); (b) un `.pmtiles` que deposites tú en el despliegue; (c) un paquete npm permitido que aún no hemos localizado. Mientras tanto, origen configurable y datos de muestra mínimos generados.
- **C-6 `lightningcss` (MPL-2.0)**: transitiva de Vite en desarrollo. ¿Se acepta en el informe, o se intenta evitar con el transformador PostCSS?

## 5. Ideas y mejoras concretas para Karpo

Todas parten de la config. Las marcadas (config+) piden ampliar el JSON con campos opcionales y retrocompatibles.

### 5.1 Cuadrícula

1. **Registro único de tipos de campo** (`text`, `number`, `money`, `date`, `select`, `badge`, `relation`, `boolean`...). Cada tipo declara visor, editor, validador, alineación y analizador de pegado. Lista, formulario, detalle, filtros y edición masiva usan el mismo registro.
2. **Modelo de edición por capas**: valor del servidor, borrador, cambios pendientes. Estado visual por celda: sin cambios, modificada, inválida, guardando, error del servidor. Esc revierte la celda; Esc dos veces, la fila.
3. **Guardado** según `editType`: `direct` (optimista por celda), `batch` (barra "N cambios: Guardar / Descartar", Ctrl+S) o `form` (solo formulario) (config+). Reversión automática y toast "Deshacer" de unos segundos.
4. **Deshacer/rehacer** (Ctrl+Z / Ctrl+Shift+Z) con parches inversos de `immer`, incluidos pegados y relleno, y menú "Historial de cambios".
5. **Teclado completo**: flechas, Tab/Shift+Tab, Inicio/Fin, Ctrl+flecha, Re Pág/Av Pág, Enter/F2 edita, Esc cancela, Espacio abre peek, Shift+flecha amplía rango, Ctrl+C/V, Supr limpia, Ctrl+D rellena, `/` enfoca el filtro, Ctrl+K paleta, `?` panel de atajos. Doble clic en fila, o Ctrl+Enter con la fila enfocada, abre el detalle en edición.
6. **Pegado inteligente desde Excel**: TSV a rango con coerción por tipo (fechas por locale, decimales con coma, `select` por etiqueta o clave) y vista previa con errores marcados celda a celda.
7. **Validación** derivada de la config (`required`, `maxLength`, `pattern`, `min`, `max`, unicidad consultada al servidor con debounce). Errores del servidor enlazados al campo por ruta; resumen en `aria-live`.
8. **Columnas**: redimensionar, reordenar, fijar, ocultar, autoajustar con doble clic en el borde; persistencia por usuario (config+ `pinned`, `hidden`).
9. **Vistas guardadas** (filtros, orden, columnas, densidad), personales y compartidas; la URL representa la vista actual.
10. **Filtros como chips** con operador adaptado al tipo; `filters` define los campos y `serverFiltering.fieldMapping` el traspaso a la API.
11. **Selección y acciones masivas**: "seleccionar las N del filtro", barra flotante con edición masiva y vista previa.
12. **Agrupación y subtotales** en tipos numéricos, con cifras tabulares.
13. **Exportación** a CSV/XLSX respetando filtros y columnas (ver `docs/EXPORTACION.md` del backend; verificar qué hace el servidor).
14. **Rendimiento**: virtualización de filas y columnas, `content-visibility`, paginación por cursor con prefetch de la siguiente página, esqueleto con las columnas reales.

### 5.2 Detalle y formularios

15. Detalle en panel redimensionable y fijable, navegación arriba/abajo entre registros, "abrir como página" y modo lectura/edición conmutable. Historial (`detail.showHistory`) y relaciones (`showRelations`) como pestañas.
16. Formularios por secciones de la config (`form.sections`, `columns`, `width` en 12 columnas), secciones plegables, Ctrl+Enter para guardar, aviso de cambios sin guardar, y guardado por campo en el detalle (modelo Notion).
17. Campos de relación con selector buscable que muestra `displayField` y creación rápida en modal anidado.

### 5.3 Vistas alternativas

18. **Interruptor listado/diagrama** persistente en la URL (`?vista=arbol|mapa|tablero`). La config declara el mapeo por vista (config+): `tree: { parentField, labelField, subtitleField }`, `map: { levels, geoField, valueField }`, `board: { groupField }`.
19. **Organigrama**: `@xyflow/react` + dagre; nodos tarjeta con el mismo registro de tipos; plegado por rama con contador; minimapa; ajustar a pantalla; búsqueda que centra el nodo; teclado entre padre, hijos y hermanos; **alternativa accesible en lista anidada `role="tree"`**; carga bajo demanda por nivel en árboles grandes.
20. **Mapa por niveles**: migas (Mundo > España > Cataluña > Barcelona), coropletas por valor agregado, selección sincronizada con la tabla, Esc sube un nivel, tabla de datos alternativa por accesibilidad, degradación sin teselas (polígonos propios).

### 5.4 Temas, i18n, RTL

21. **Capa de tokens**: la app consume solo variables semánticas (`--color-surface`, `--space-*`, `--radius-*`, `--font-*`) y un único fichero de mapeo las enlaza con las variables que emite Theros Front. Lo que el contrato no cubra (altura de fila, colores de estado de celda, sombra de popover) va a `PETICIONES-THEROS-FRONT.md`. Sin `#hex` ni `px` fijos: comprobable con una prueba que busque literales en el CSS.
22. **Claro/oscuro/sistema** con `prefers-color-scheme` y `data-theme`; **RTL** con propiedades lógicas CSS desde el primer día, iconos direccionales espejados, pruebas e2e con un idioma RTL.
23. **i18n**: claves por espacio de nombres (`core`, `entity.<id>`, `help.<id>`), es y en completos, carga diferida; textos de la config con claves o con objeto por idioma; prueba automática de "sin claves sin traducir" y pseudo-localización para cazar cadenas escritas a mano.

### 5.5 Ayuda guiada

24. Botón `?` en cada pantalla; recorrido con `driver.js` desde pasos `{ selector, title, body }` de la config (`help.steps`, config+) y, si faltan, generado: cabecera, buscador, filtros, columnas, acciones de fila, interruptor de vista, paleta. Los pasos cuyo elemento no existe se omiten. Persistencia de "visto". Traducible, accesible con teclado, con `prefers-reduced-motion`.
25. **Panel de atajos** generado del mismo registro de atajos que el comportamiento: documentación y código no pueden divergir.

### 5.6 Calidad y empaquetado

26. **Paquete `@karpo/react`** en modo biblioteca de Vite: exporta `<KarpoApp config api theme locale />`, hooks y registro extensible de tipos de campo; `exports` ESM, tipos, `sideEffects` explícito, React como `peerDependency`, versión semántica, `files` mínimo, `npm pack --dry-run` en las pruebas, sin publicar.
27. **Pruebas**: Vitest + Testing Library por componente; MSW como falso backend; Playwright con Edge/Chrome para teclado, deshacer, doble clic, RTL y capturas de regresión visual por tema e idioma; los mismos datos de muestra alimentan demo y pruebas.
28. **Cliente de API tipado** a partir de `apiEndpoint` y `dataSource.serverFiltering`; si faltan `ETag`/versión de fila para concurrencia optimista al guardar celdas, anotarlo en `PETICIONES-BACKEND.md`.

## 6. Riesgos y huecos

- **Datos geográficos**: choque entre soberanía y "solo npm"; solución escalonada en 2.3 y consulta C-5.
- **TanStack Table 9** es una versión mayor reciente (almacén propio); fijar versión exacta y escribir un adaptador fino.
- **`role="grid"` con virtualización** es lo más costoso en accesibilidad; hacer un prototipo temprano probado con teclado y lector de pantalla.
- **Contrato de Theros**: no se ha validado aquí la cobertura de tokens (fuera de este análisis); la capa de mapeo es el punto único de adaptación.
- **Referentes**: sin contraste en vivo (apartado 0).

## 7. Decisiones propuestas (insumo para la arquitectura)

| Tema | Propuesta |
|---|---|
| Cuadrícula | TanStack Table 9 + TanStack Virtual, render propio con `role="grid"` |
| Servidor | TanStack Query con mutaciones optimistas y reversión |
| Edición | Registro único de tipos de campo; `immer` (parches) para deshacer; Zustand para cambios pendientes |
| Formularios | react-hook-form + zod generado desde la config |
| Organigrama | @xyflow/react + @dagrejs/dagre (ELK descartado) |
| Mapa | maplibre-gl + pmtiles con origen configurable; modo GeoJSON propio sin teselas |
| Ayuda | driver.js con pasos desde la config |
| i18n | i18next / react-i18next (es, en; estructura para más) |
| Primitivas UI | Radix o react-aria-components (a decidir) |
| Iconos | Phosphor (MIT) |
| Pruebas | Vitest, Testing Library, MSW, playwright-core (Edge/Chrome) |
| Pendiente de Javier | C-1 a C-6 |
