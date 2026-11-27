# Plan del front React de Karpo Go (`@karpo/react`)

Fecha: 2026-11-27. Rama `claude/front-react-7b8564`. Papel: planificador, ronda 1.

Insumos (leídos enteros): `docs/analisis/01-runtime-angular-y-configuracion.md` (A1),
`02-api-karpo-go.md` (A2), `03-contrato-temas-theros-front.md` (A3) y
`04-referentes-y-librerias.md` (A4). Las versiones y licencias de §3 se han vuelto a comprobar
con `npm view` el 2026-11-27.

Todo el trabajo vive en `front/react`. Nada fuera de esa carpeta se modifica: la configuración de
Go (`front/angular/config/entities/**`) se **lee**, Theros Front se **ejecuta** sólo para generar
temas en una carpeta nuestra, `cmd/karpo` se **ejecuta** sobre un SQLite temporal.

---

## 0. En una pantalla

1. **Un paquete npm, `@karpo/react`** (biblioteca de Vite, ESM y tipos, React como
   `peerDependency`, versión `0.1.0`, `npm pack --dry-run` en la batería y un `prepublishOnly` que
   impide publicar) y **una app de demostración** en `demo/` que no se publica.
2. **Motor de configuración**: lee los mismos `*.config.json` del runtime Angular (los 8 de Go tal
   cual, servidos desde `front/angular/config/entities`) y un `manifest.json` (formato que el pack
   karpo-go de Theros puede emitir) que registra entidades, menú, idiomas, tema, mapas y
   **superposiciones** (`overlay`) para lo que el JSON de Angular no sabe decir (vistas de árbol y
   mapa, selectores de clave ajena, ayuda). Valida con zod, normaliza las incoherencias de A1 §5 y
   avisa en desarrollo de las claves que ignora.
3. **Adaptador HTTP** del contrato de mantenimiento de Go (A2 §2) y de sus rutas auxiliares
   (catálogos PascalCase, páginas `number/size`, árbol de puestos, delimitaciones): sesión JWT con
   refresco único, `mustChangePassword`, permisos de `/api/auth/context`, `X-Organization-Scope`,
   `version` siempre en los cambios, problem+json en sus dos formas, 409 como conflicto.
4. **Temas de Theros Front sin valores fijos**: se consume `--th-*` directamente; lo que falta va
   en una sola capa `karpo.ext` (`--kx-*`, derivada con `color-mix()`/`calc()`); sonda
   `resolveThemeColor` para canvas/WebGL/driver.js; `manifest.json` del tema decide qué modos
   existen; prueba de contrato (ganchos, hash del diccionario, lint de literales).
5. **Pantallas**: cuadrícula editable (TanStack Table + Virtual, `role="grid"` propio, editor por
   tipo de campo, deshacer con parches de immer, guardado optimista o por lotes con `version`,
   teclado completo, **doble clic → detalle en edición**); detalle en panel/página con formulario
   generado; organigrama (lista `role="tree"` ⇄ diagrama @xyflow + dagre con zoom y plegado); mapa
   por niveles (lista ⇄ mapa MapLibre, mundo → país → región → ciudad, fuente GeoJSON o PMTiles
   propios con origen configurable, ninguna tesela de terceros).
6. **i18n** con i18next (es y en completos, estructura para más, textos de la configuración por
   clave o por diccionario con respaldo al literal), **RTL** por propiedades lógicas y `dir` desde
   el idioma, **ayuda guiada** con driver.js en todas las pantallas.
7. **Demo** con datos deterministas en dos modos: `demo` (backend falso con MSW, sin servidor) y
   `local` (contra `cmd/karpo` sobre SQLite temporal, sembrado por HTTP con los mismos datos).
8. **Capturas** generadas por script con playwright-core y el Edge/Chrome instalado: 9 escenas × claro,
   oscuro y RTL = 27 PNG en `docs/capturas/`. **Informe de licencias** generado por script desde
   `package-lock.json`.
9. **9 tareas en 4 olas** (§10): ola 0 = andamiaje con todas las dependencias y los contratos
   entre módulos; ola 1 = cimientos en paralelo (configuración/API/sesión, temas/i18n/ayuda,
   campos/primitivas/atajos, demo); ola 2 = pantallas en paralelo (cuadrícula, marco+detalle,
   organigrama+mapa); ola 3 = e2e, capturas y cierre.

---

## 1. Decisiones (técnicas y reversibles: tomadas)

| # | Decisión | Por qué / fuente |
|---|---|---|
| D-R1 | Nombre `@karpo/react`, carpeta `front/react`, versión `0.1.0` | Propuesta del encargo; sin publicar |
| D-R2 | Un solo `package.json` con biblioteca (`src/`) y demo (`demo/`); `files` sólo `dist/` | Ninguna tarea posterior toca `package.json` |
| D-R3 | TypeScript **6.0.3**, no 7.0.2 | `typescript-eslint` 8.74 exige `typescript <6.1.0` (comprobado) |
| D-R4 | Tipos con `tsc --emitDeclarationOnly`, sin `vite-plugin-dts` | Éste arrastra `@microsoft/api-extractor`; menos dependencias |
| D-R5 | Sin `eslint-plugin-jsx-a11y` | Depende de `axe-core` (MPL-2.0); accesibilidad con pruebas propias (A4 C-1, opción b) |
| D-R6 | Rejilla propia sobre `@tanstack/react-table` 9 + `@tanstack/react-virtual` | A1 §12.3, A4 §7; AG Grid Enterprise prohibido |
| D-R7 | Organigrama con `@xyflow/react` + `@dagrejs/dagre`; sin d3 directo ni ELK | ISC y EPL/GPL fuera de la lista (A4 C-4) |
| D-R8 | Mapa con `maplibre-gl` + `pmtiles`; capa 1 GeoJSON propio, capa 2 PMTiles propio; etiquetas como marcadores HTML (sin glifos) | A4 §2.3 |
| D-R9 | Iconos Phosphor (MIT); el peso (`regular`/`fill`) sigue al tema | A3 §6.5; Lucide es ISC |
| D-R10 | Sin fuentes empaquetadas; la familia sale de `--th-type-ui-family` | A4 C-3 |
| D-R11 | Rutas por URL (`react-router` 8): `/:entity`, `/:entity/:id?mode=edit`, `?view=tree|map&…` | A1 §11.11, A4 §1.2 |
| D-R12 | `style` sólo con variables de disposición (`--kx-col-width`, `--kx-row-index`, transformaciones) vía CSSOM; nunca color, fuente, radio, sombra ni espacio | A3 §7.6 (desviación consciente; consulta Q-1) |
| D-R13 | Paleta de datos provisional en `karpo.ext` (`--kx-data-seq-1..7`, `--kx-data-series-1..10`), derivada del tema, con prueba de contraste propia, marcada para sustituir por PT-5 | A3 §11.3 |
| D-R14 | No se reintenta nunca POST/PUT/PATCH/DELETE; los GET, hasta 2 veces ante red/503 con `Retry-After` | A1 §11.13 (409 espurios) |
| D-R15 | Guardado por lotes = secuencia de PUT no atómica, con informe por fila | A2 §2 (no hay lotes en Go) |
| D-R16 | RTL: `dir` sale del idioma (`ar`, `he`, `fa`, `ur` → `rtl`); la demo admite `?dir=rtl` para verificar con es/en | No hay traducción árabe revisada (consulta Q-5) |
| D-R17 | Las tareas no hacen commit; el orquestador hace uno por ola, en español | Tareas en paralelo en la misma carpeta |
| D-R18 | Pruebas e2e con **vitest como ejecutor** + `playwright-core` (sin `@playwright/test`) | Sólo playwright-core; ningún navegador descargado |

**Ronda 2.** Las decisiones D-R19 a D-R30 (iconos generados y Phosphor como dependencia de
desarrollo, vocabulario de iconos de Phosphor, `Tab` sale de la cuadrícula, nada de `:dir()`, un
solo control de vista, `karpo-map.css` aparte, `devEngines` en vez de `engines`, `zod/mini`,
aislamiento bidi en RTL, superposiciones de texto generadas, presupuesto de 250 kB gzip y salto del
nivel de un solo elemento) están en `docs/PLAN-RONDA-2.md` §2 y mandan sobre lo que aquí diga lo
contrario.

**Ronda 3.** Las decisiones D-R31 a D-R40 (avisos de terceros en `dist/THIRD-PARTY-NOTICES.md`,
un solo `h1` por pantalla con migas en la barra, «sin dato» discreto en el mapa, ajuste de
columnas por prioridad, vacío y cifras unificados, recorridos de ayuda que fusionan configuración y
anclas, iconos e idiomas por demanda con presupuesto de 215 kB gzip, árabe de demostración, entrada
por URL en niveles saltables y tarjetas «Registro» y «Relaciones» en el detalle) están en
`docs/PLAN-RONDA-3.md` §2 y mandan sobre lo que aquí diga lo contrario. El estado de todas las
consultas está en `docs/CONSULTAS-JAVIER.md`.

## 2. Consultas a Javier (con la opción por defecto que se aplica mientras tanto)

| # | Pregunta | Por defecto |
|---|---|---|
| Q-1 | ¿Se acepta `style` con variables de disposición (D-R12)? | Sí, sólo disposición, vía CSSOM |
| Q-2 | ¿Se incluye en el paquete una copia generada de `modern-flat` como tema por defecto? (licencia de lo generado por Theros Front sin declarar) | No se incluye en `files`: la demo lo sirve desde `demo/public/themes/`; el paquete pide `themeOrigin` |
| Q-3 | Licencia del paquete `@karpo/react` | `GPL-2.0-only`, la del repositorio (`LICENSE`); cambiarla es una línea |
| Q-4 | Geometrías reales: ¿de dónde? (A4 C-5) | Demo con **geometría esquemática generada** (rotulada como tal) y origen configurable para un `.pmtiles` real; anotado en peticiones |
| Q-5 | ¿Se añade un idioma RTL real (árabe) con traducción revisada? | Desde la ronda 3 (D-R38): árabe de demostración **sin revisión nativa**; las capturas `rtl` lo usan. Queda abierta la revisión |
| Q-6 | `lightningcss` (MPL-2.0) como transitiva de Vite y otras transitivas ISC/BlueOak/CC0 | **Por defecto**, mientras Javier no decida, se usan y se listan en el informe (A4 C-6); `lightningcss` no se distribuye. Alternativa si la rechaza: `cssMinify: 'esbuild'` |
| Q-7 | Paleta de datos provisional mientras no llegue PT-5 | Provisional en `karpo.ext` (D-R13) |
| Q-8 | Geometría real de la demo: Natural Earth (dominio público) a PMTiles propio; requiere que Javier aporte el fichero o autorice la descarga (`world-atlas`, ISC, sólo con su visto bueno) | Geometría esquemática rotulada (Q-4) y origen configurable |
| Q-9 | `polylabel` (ISC) como directa para colocar rótulos | No: cálculo propio |
| Q-10 | Nivel del mapa con un solo elemento: saltarlo o conservarlo con zoom limitado | Saltarlo (`skipSingle: true`, configurable) |
| Q-11 | Atribución de `@xyflow/react` en el diagrama (MIT permite ocultarla; el fabricante la pide por cortesía) | Oculta, con su aviso en `THIRD-PARTY-NOTICES.md`; alternativas: visible y configurable, o suscripción |
| Q-12 | ¿Cantidades de dinero en monoespaciada? | No: `tabular-nums` en la familia del cuerpo para todas las cifras (D-R35) |

Formato i18n en la configuración (A1 §12.3): se aceptan **los dos** y se recomienda a Theros el de
claves (§5.4). Iconos (A4 C-2) y ELK (C-4) quedan resueltos por D-R9 y D-R7.

---

## 3. Paquetes npm (versiones exactas, licencia comprobada con `npm view` el 2026-11-27)

Todas las directas son MIT, Apache-2.0 o BSD. Se fijan **sin `^`**. `.npmrc` con
`ignore-scripts=true` y `save-exact=true`.

### 3.1 `dependencies` (las que trae quien instala el paquete)

| Paquete | Versión | Licencia | Para qué |
|---|---|---|---|
| `react-router` | 8.5.3 | MIT | Rutas y estado en la URL (D-R11) |
| `@tanstack/react-table` | 9.3.1 | MIT | Modelo de la cuadrícula (orden, columnas, fijado, tamaño) |
| `@tanstack/react-virtual` | 3.14.28 | MIT | Virtualización de filas y columnas |
| `@tanstack/react-query` | 5.108.4 | MIT | Caché de servidor, mutaciones optimistas con reversión, precarga |
| `zustand` | 5.1.0 | MIT | Cambios pendientes, selección, preferencias |
| `immer` | 11.2.13 | MIT | `produceWithPatches`: pila deshacer/rehacer |
| `react-hook-form` | 7.92.0 | MIT | Formularios del detalle y altas |
| `zod` | 4.6.7 | MIT | Esquema de la configuración y validación generada |
| `@hookform/resolvers` | 7.0.9 | MIT | Puente zod ↔ react-hook-form |
| `i18next` | 27.2.9 | MIT | Traducciones en ejecución, espacios de nombres |
| `react-i18next` | 18.0.3 | MIT | Enlace con React |
| `@xyflow/react` | 13.0.3 | MIT | Lienzo del organigrama (zoom, paneo, minimapa) |
| `@dagrejs/dagre` | 3.1.4 | MIT | Disposición jerárquica |
| `maplibre-gl` | 6.19.0 | BSD-3-Clause | Motor del mapa (WebGL) |
| `pmtiles` | 4.5.0 | BSD-3-Clause | Protocolo `pmtiles://` contra origen propio |
| `driver.js` | 1.14.0 | MIT | Recorridos de ayuda (Shepherd prohibido) |
| `cmdk` | 1.1.1 | MIT | Paleta de órdenes Ctrl+K y selector de clave ajena |
| `@radix-ui/react-dialog` | 1.3.1 | MIT | Diálogos accesibles (conflicto, confirmación, motivo) |
| `@radix-ui/react-popover` | 1.3.1 | MIT | Popovers de celda y filtros |
| `@radix-ui/react-dropdown-menu` | 2.2.1 | MIT | Menús (tema, idioma, acciones de fila) |
| `@radix-ui/react-tooltip` | 1.4.1 | MIT | Ayudas (`hint`) |
| `@radix-ui/react-tabs` | 1.2.1 | MIT | Pestañas del detalle |
| `@floating-ui/react` | 0.27.20 | MIT | Posición del editor de celda y del error |
| `@phosphor-icons/react` | 2.1.10 | MIT | Iconos (D-R9). Ronda 2 (D-R19): pasa a `devDependencies`; sólo lo usa `scripts/gen-icons.mjs` |
| `react-resizable-panels` | 4.24.3 | MIT | Panel de detalle redimensionable |
| `fuse.js` | 7.5.0 | Apache-2.0 | Búsqueda difusa en la paleta y el menú |

### 3.2 `peerDependencies`

`react` y `react-dom` `>=19.2.7` (exigencia de `react-router` 8); en desarrollo 19.3.1 (MIT).

### 3.3 `devDependencies`

| Paquete | Versión | Licencia | Para qué |
|---|---|---|---|
| `react`, `react-dom` | 19.3.1 | MIT | Demo y pruebas |
| `@types/react` / `@types/react-dom` / `@types/node` | 19.3.3 / 19.3.0 / 26.6.11 | MIT | Tipos |
| `typescript` | 6.0.3 | Apache-2.0 | D-R3 |
| `vite` | 8.4.0 | MIT | Servidor de desarrollo, demo y biblioteca |
| `@vitejs/plugin-react` | 6.3.0 | MIT | JSX y refresco |
| `vitest` / `@vitest/coverage-v8` | 5.1.2 | MIT | Unitarias, componentes, contrato, e2e (D-R18) |
| `jsdom` | 31.0.1 | MIT | DOM de las pruebas de componentes |
| `@testing-library/react` / `dom` / `user-event` / `jest-dom` | 16.3.3 / 10.4.2 / 14.6.13 / 8.0.0 | MIT | Pruebas de componentes por rol y nombre accesible |
| `msw` | 3.0.8 | MIT | Backend falso en la demo y en pruebas |
| `playwright-core` | 1.65.1 | Apache-2.0 | e2e y capturas con `channel: 'msedge'` o `'chrome'` |
| `eslint` / `@eslint/js` | 10.15.0 / 10.0.1 | MIT | Lint |
| `typescript-eslint` | 8.74.0 | MIT | Lint de TS |
| `eslint-plugin-react-hooks` | 7.1.1 | MIT | Reglas de hooks |
| `globals` | 17.14.0 | MIT | Globales del lint |
| `stylelint` / `stylelint-config-standard` | 18.1.0 / 41.1.0 | MIT | Lint de CSS y **abstinencia** (sin literales) |

Si al instalar una versión no casa con otra (pares), la tarea 0 baja a la última compatible,
la comprueba con `npm view` y la anota en `docs/LICENCIAS.md`; nunca cambia a una licencia
distinta.

### 3.4 Descartadas como directas

`elkjs` (EPL/GPL), `axe-core` y `eslint-plugin-jsx-a11y` (MPL por transitiva), `d3-*`,
`lucide-react`, `topojson-client`, `world-atlas` (ISC), `@fontsource/*` (OFL), `world-countries`
(sin licencia), `vite-plugin-dts` (D-R4), `@playwright/test`/`playwright` (no hacen falta),
AG Grid Enterprise, Handsontable, Shepherd.js.

### 3.5 Informe de licencias

`scripts/licencias.mjs` (sin dependencias) lee `package-lock.json` y el `package.json` de cada
paquete en `node_modules` y escribe `docs/LICENCIAS.md`: directas con licencia y si son de
ejecución o de desarrollo; transitivas agrupadas por licencia; excepciones aceptadas con su motivo
(`lightningcss*` MPL-2.0, herramienta de compilación; ISC, 0BSD, CC0, BlueOak, MIT-0, CC-BY-4.0 de
`caniuse-lite` si aparece). Con `--check` **falla** si una directa no es MIT/Apache-2.0/BSD-2/BSD-3,
si alguna es GPL/AGPL/LGPL/SSPL/EUPL/EPL/comercial/sin licencia, o si aparece una MPL nueva fuera de
la lista de excepciones.

---

## 4. Arquitectura

### 4.1 Estructura de carpetas

```
front/react/
  package.json  package-lock.json  .npmrc  .gitignore  README.md
  tsconfig*.json  vite.config.ts  vite.lib.config.ts  vitest.config.ts  vitest.e2e.config.ts
  eslint.config.js  stylelint.config.js
  build/                     plugins de Vite (servir la config de Go, copiarla al construir)
  src/
    index.ts                 exportaciones públicas del paquete
    contracts/               tipos compartidos entre módulos (§4.2); sólo los escribe la tarea 0
    config/                  esquema zod, normalizador, cargador, manifiesto, reglas de visibilidad, textos
    api/                     cliente HTTP, adaptador Go, errores, consultas (react-query)
    auth/                    sesión, login, cambio de contraseña, contexto de permisos, ámbito
    theme/                   ThemeProvider, sonda de color, capas CSS, karpo.ext, contrato
    i18n/                    arranque de i18next, idiomas, dirección, formatos Intl
    shell/                   KarpoApp, rutas, menú, barra superior, paleta, atajos, páginas de error
    fields/                  registro de tipos de campo: visor, editor, analizador, validador
    ui/                      primitivas con ganchos de Theros (acción, diálogo, aviso, chip, kbd…)
    help/                    ayuda guiada (driver.js), anclas, botón
    grid/                    cuadrícula editable
    detail/                  detalle, formulario, conflicto, rejillas hijas, órdenes
    tree/                    organigrama: lista role=tree y diagrama
    map/                     mapa por niveles
    locales/{es,en}/<ns>.json   un fichero por espacio de nombres y tarea
  demo/
    index.html  main.tsx      app de demostración (no se publica)
    config/                  manifest.json, configs nuevas (puesto, delimitación), textos por idioma
    data/                    generador determinista (ESM .mjs, compartido con el sembrador)
    mocks/                   MSW: navegador y nodo; handlers/*.ts (uno por área)
    public/                  themes/<tema>/, geo/, mockServiceWorker.js
  scripts/                   licencias, temas, geodatos, pmtiles, sembrador, local, capturas
  e2e/                       pruebas de extremo a extremo (vitest + playwright-core)
  docs/                      PLAN, análisis, peticiones, LICENCIAS, capturas, notas por tarea
```

### 4.2 Contratos entre módulos (`src/contracts/`, escritos por la tarea 0)

Las tareas de las olas 1 y 2 trabajan en paralelo; por eso las interfaces que se cruzan se fijan
aquí y la tarea 0 las escribe tal cual en `src/contracts/*.ts`. Una tarea **no cambia** un
contrato: si le falta algo, lo amplía en su módulo y lo anota en su `docs/notas/<tarea>.md`.

```ts
// contracts/config.ts — modelo interno normalizado (lo que sale del normalizador)
export type Text = { literal: string; key?: string; byLang?: Record<string, string> };
export type Tone = 'success' | 'error' | 'warning' | 'info' | 'neutral' | 'accent';
export type FieldKind = 'text' | 'textarea' | 'number' | 'decimal' | 'money' | 'percentage'
  | 'date' | 'datetime' | 'boolean' | 'select' | 'multiselect' | 'badge' | 'lookup'
  | 'email' | 'phone' | 'url';
export interface OptionDef { value: string; label: Text; tone?: Tone; icon?: string }
export interface LookupDef { entity: string; valueField: string; displayField: string;
  filter?: Record<string, string | number | boolean> }
export type VisibilityRule = { expr?: string; permission?: string; field?: string; equals?: unknown;
  allOf?: VisibilityRule[]; anyOf?: VisibilityRule[]; not?: VisibilityRule };
export interface FieldDef { field: string; label: Text; kind: FieldKind; required: boolean;
  editable: boolean; immutable?: boolean; min?: number; max?: number; minLength?: number;
  maxLength?: number; pattern?: string; options?: OptionDef[]; lookup?: LookupDef; hint?: Text;
  width?: number /* 1..12 */; format?: string; emptyValue?: string; visibleWhen?: VisibilityRule;
  trueLabel?: Text; falseLabel?: Text }
export interface ColumnDef extends FieldDef { widthPx?: number; sortable: boolean;
  serverSortField?: string; pinned?: 'start' | 'end'; hidden?: boolean }
export interface FilterDef { field: string; serverField: string; label: Text;
  kind: 'text' | 'boolean' | 'number' | 'select' | 'multiselect' | 'date';
  quickSearch: boolean; options?: OptionDef[]; defaultValue?: unknown; hint?: Text }
export interface CommandDef { id: string; label: Text; icon?: string; tone?: Tone;
  surfaces: ('row' | 'detail-header' | 'toolbar')[]; kind: 'view' | 'edit' | 'delete' | 'http';
  method?: 'POST' | 'PUT' | 'DELETE'; endpoint?: string; confirm?: Text;
  prompt?: { field: string; label: Text; required: boolean; maxLength?: number };
  sendVersion?: boolean; successMessage?: Text; acceptedMessage?: Text; visibleWhen?: VisibilityRule }
export interface SectionDef { id: string; title: Text; columns: number; collapsible: boolean;
  collapsed: boolean; fields: FieldDef[]; visibleWhen?: VisibilityRule }
export interface ChildGridDef { id: string; title: Text; endpoint: string; columns: ColumnDef[];
  emptyMessage?: Text }
export type ViewDef =
  | { kind: 'grid' }
  | { kind: 'tree'; parentField: string; labelField: string; subtitleField?: string;
      badgeField?: string; source: { type: 'flat' } | { type: 'chart'; endpoint: string;
      rootsFrom: 'list'; depth: number; childrenField: string; nodeField: string } }
  | { kind: 'map'; boundaryField: string; levels: MapLevelDef[]; aggregate: 'count' | 'sum';
      valueField?: string };
export interface MapLevelDef { id: 'world' | 'country' | 'region' | 'province' | 'city';
  label: Text; boundaryLevel: string; codeProperty: string; source: string /* id en manifest.map.sources */ }
export interface HelpStepDef { target: string /* ancla data-kx-help o selector */;
  title: Text; body: Text }
export interface EntityConfig {
  id: string; name: Text; namePlural: Text; description?: Text; icon?: string; tone?: Tone;
  endpoint: string; apiVersioned: boolean; primaryKey: string; displayField: string;
  editable: boolean; saveMode: 'optimistic' | 'batch' | 'form'; presentation: 'panel' | 'page';
  permissions?: { read?: string; write?: string };
  dataSource: { pagination: 'server' | 'client'; listStyle: 'maintenance' | 'number-size';
    baseFilter: Record<string, unknown>; companyFilterField?: string };
  columns: ColumnDef[]; filters: FilterDef[];
  form: { sections: SectionDef[]; create: boolean; edit: boolean } | null;
  detail: { sections: SectionDef[]; childGrids: ChildGridDef[] };
  views: ViewDef[]; defaultView: 'grid' | 'tree' | 'map';
  commands: CommandDef[]; help: HelpStepDef[]; warnings: ConfigWarning[] }
export interface ConfigWarning { path: string; message: string; severity: 'info' | 'warn' }
export interface MenuNode { id: string; label: Text; icon?: string; entity?: string;
  children?: MenuNode[]; visibleWhen?: VisibilityRule }
export interface AppManifest { schemaVersion: 1; entities: Record<string, EntityConfig>;
  menu: MenuNode[]; locales: string[]; defaultLocale: string;
  theme: { origin: string; default: string; available: string[] };
  map: { sources: Record<string, MapSourceDef> }; api: { baseUrl: string; apiVersion: string } }
export type MapSourceDef = { type: 'geojson'; url: string } | { type: 'pmtiles'; url: string; layer: string };

// contracts/api.ts
export type Row = Record<string, unknown> & { id: string; version?: number };
export interface ListQuery { page: number; pageSize: number;
  sort?: { field: string; direction: 'asc' | 'desc' }; filter: Record<string, string | number | boolean> }
export interface Page<T = Row> { items: T[]; total: number; page: number; pageSize: number }
export type ApiErrorKind = 'validation' | 'conflict' | 'forbidden' | 'unauthorized' | 'notfound'
  | 'business' | 'too-large' | 'network' | 'server';
export interface ApiErrorShape { kind: ApiErrorKind; status: number; title: string; detail?: string;
  code?: string; fieldErrors: Record<string, { code: string; message: string }[]> }
export interface CommandResult { status: 'done' | 'pending'; row?: Row }
export interface EntityApi {
  list(cfg: EntityConfig, q: ListQuery, signal?: AbortSignal): Promise<Page>;
  get(cfg: EntityConfig, id: string, signal?: AbortSignal): Promise<Row>;
  create(cfg: EntityConfig, body: Record<string, unknown>): Promise<Row>;
  update(cfg: EntityConfig, id: string, patch: Record<string, unknown>, version: number): Promise<Row>;
  remove(cfg: EntityConfig, id: string, body?: Record<string, unknown>): Promise<CommandResult>;
  command(cfg: EntityConfig, cmd: CommandDef, row: Row, body?: Record<string, unknown>): Promise<CommandResult>;
  childList(endpoint: string, row: Row, signal?: AbortSignal): Promise<Row[]>;
  raw<T>(path: string, init?: RequestInit): Promise<T> }
export const queryKeys = {
  list: (entity: string, q: ListQuery) => ['entity', entity, 'list', q] as const,
  record: (entity: string, id: string) => ['entity', entity, 'record', id] as const,
  label: (entity: string, id: string) => ['entity', entity, 'label', id] as const };

// contracts/auth.ts
export interface AuthContext { subjectId: string; subjectName: string; isGlobalAdmin: boolean;
  permissions: string[]; effectiveOrganizationIds: string[] }
export interface Session { status: 'anonymous' | 'must-change-password' | 'authenticated';
  context?: AuthContext; can(permission: string): boolean }

// contracts/theme.ts
export type ThemeMode = 'light' | 'dark' | 'system';
export interface ThemeInfo { name: string; modes: ThemeMode[]; highContrast: boolean;
  iconStyle: 'outline' | 'filled'; direction?: 'ltr' | 'rtl'; dictionaryHash: string }
export interface ThemeApi { info: ThemeInfo; mode: ThemeMode; setMode(m: ThemeMode): void;
  setTheme(name: string): Promise<void>; available: string[];
  resolveColor(token: string, el?: Element): string; onChange(cb: () => void): () => void }

// contracts/fields.ts
export interface FieldTypeDef { kind: FieldKind; align: 'start' | 'end' | 'center';
  format(value: unknown, def: FieldDef, locale: string): string;
  parse(text: string, def: FieldDef, locale: string): { ok: true; value: unknown } | { ok: false; code: string };
  validate(value: unknown, def: FieldDef): string[] /* códigos: required, maxLength, min… */;
  Viewer: React.ComponentType<{ value: unknown; def: FieldDef; row?: Row }>;
  Editor: React.ComponentType<{ value: unknown; def: FieldDef; onChange(v: unknown): void;
    onCommit(move?: 'next' | 'prev' | 'down' | 'up'): void; onCancel(): void; autoFocus?: boolean;
    invalid?: boolean; describedBy?: string }> }

// contracts/views.ts
export interface EntityRoute { entity: string; id?: string; mode?: 'read' | 'edit';
  view?: 'grid' | 'tree' | 'map'; params?: Record<string, string> }
export interface EntityViewProps { config: EntityConfig; route: EntityRoute;
  navigate(to: EntityRoute): void }
// Cada módulo de vista exporta un componente con este nombre exacto:
//   grid → GridView, detail → DetailView, tree → TreeView, map → MapView.

// contracts/help.ts
export interface HelpApi { register(screen: string, steps: HelpStepDef[]): () => void;
  start(screen?: string): void }
// Ancla: atributo data-kx-help="<ns>.<nombre>"; textos en el espacio <ns>, claves
// help.<nombre>.title y help.<nombre>.body. Un paso sin elemento visible se omite.

// contracts/shortcuts.ts
export interface ShortcutDef { id: string; scope: 'global' | 'grid' | 'detail' | 'tree' | 'map';
  keys: string /* p. ej. "Mod+K", "Shift+?" */; labelKey: string; run(): void }
// useShortcut(def) (src/ui, tarea 3) lo registra; el panel de atajos (Shift+?) lista el registro.

// contracts/contexts.ts — los contextos React se crean aquí (tarea 0) para que productores y
// consumidores de olas distintas o paralelas no dependan del orden de escritura.
export const ManifestContext = createContext<AppManifest | null>(null);
export const ApiContext = createContext<EntityApi | null>(null);
export const SessionContext = createContext<Session | null>(null);
export const ThemeContext = createContext<ThemeApi | null>(null);   // sin proveedor: iconos 'outline'
export const HelpContext = createContext<HelpApi | null>(null);     // sin proveedor: sin botón
export interface KarpoAppProps { manifestUrl?: string; manifest?: AppManifest; apiBaseUrl?: string;
  themeOrigin?: string; locale?: string; router?: 'browser' | 'hash' | 'memory'; basename?: string;
  initialPath?: string }
```

Quién implementa cada exportación pública (los esqueletos de la tarea 0 se sustituyen):

| Módulo | Exporta (como mínimo) | Tarea |
|---|---|---|
| `config` | `loadManifest`, `loadLegacy`, `normalizeEntityConfig`, `evaluateRule`, `useText`, `resolveText` | T1 |
| `api` | `createHttpClient`, `createEntityApi`, `ApiError`, `useEntityList`, `useEntityRecord`, `useLookupLabel` | T1 |
| `auth` | `SessionProvider`, `useSession`, `LoginPage`, `ChangePasswordPage`, `OrganizationScopePicker` | T1 |
| `theme` | `ThemeProvider`, `useTheme`, `resolveThemeColor`, `assertTherosHooks` (pruebas) | T2 |
| `i18n` | `createI18n`, `I18nProvider`, `directionFor`, `useFormatters` | T2 |
| `help` | `HelpProvider`, `HelpButton`, `useHelp` | T2 |
| `fields` | `fieldTypes`, `getFieldType`, `registerFieldType`, `FieldView`, `FieldEditor` | T3 |
| `ui` | `Action`, `Dialog`, `ConfirmDialog`, `PromptDialog`, `Feedback`, `ToastProvider`, `useToast`, `Badge`, `Chip`, `Kbd`, `Skeleton`, `Spinner`, `Icon`, `Toolbar`, `Field`, `ShortcutProvider`, `useShortcut`, `useShortcutRegistry` | T3 |
| `grid` | `GridView` | T5 |
| `shell`, `detail` | `KarpoApp`, `DetailView` | T6 |
| `tree`, `map` | `TreeView`, `MapView` | T7 |

### 4.3 Motor de configuración (`src/config`)

- **Entrada.** `loadManifest(url)` lee `manifest.json` (formato de §4.3.1). Modo legado:
  `loadLegacy(indexUrl, sidebarUrl)` acepta `index.json` + `sidebar-config.json` del runtime
  Angular. Modo descubrimiento: una lista de rutas de configs sin índice (los 8 de Go), con el
  menú agrupado por la carpeta.
- **Validación**: esquema zod **permisivo** del JSON de Angular (todas las claves de A1 §3, incluidas
  las ‡ que usan los JSON: `badgeMap`, `lookupEntity*`, `sections[].columns`, `hint`, `trueLabel`…);
  una clave desconocida o muerta (A1 §3 †, `fieldMapping`) da un `ConfigWarning`, no un error.
  Un `type` desconocido se degrada a `text` **con aviso** (no en silencio, como Angular).
- **Normalización** (A1 §12.1): `groups`≡`sections`; `field`≡`dataField`; `checkbox`/`switch`≡
  `boolean`; filtro `checkbox`≡`boolean`, `number`≡`number`; `badgeMap`⊕`enumValues` con claves en
  cadena y comparación tolerante (`true`≡`"true"`); `visibleWhen` texto≡`{expr}`; claves legadas
  (`roles`, `sector`, `capability`, `featureFlag`) → regla única; `color` hexadecimal → se ignora
  (aviso) y la entidad toma tono por hash estable del `id`; `color` con nombre → tono
  (`green→success`, `danger→error`, `warning→warning`, `gray→neutral`, `blue→info`,
  `primary→accent`); `rowActions` `view/edit/delete` y `commands[]` (forma de `front/README.md`) →
  `CommandDef`; `editType` → `saveMode` (`direct`→`optimistic`, salvo `overlay.saveMode`);
  `sortable` → `serverSortField` sólo si el manifiesto lo declara o coincide con la lista blanca
  conocida de A2 §2 (si no, orden sólo en cliente de la página cargada y aviso); hijos con `{id}` no
  añaden `?foreignKey=`.
- **Superposición** (`overlay`) por entidad en el manifiesto: se fusiona en profundidad sobre el
  JSON original **antes** de normalizar (para no tocar los ficheros de `front/angular`). Sirve para
  `views` (árbol, mapa), `lookup` de claves ajenas, `help`, `permissions`, `serverSortFields`,
  `saveMode`, `presentation`.
- **Textos**: `Text` admite literal, `labelKey`/`titleKey`/`headerKey` (clave de i18n) y objeto por
  idioma (`{"es":"…","en":"…"}`). `useText()` resuelve: clave en el idioma activo → diccionario →
  literal. Los diccionarios de la configuración se cargan desde `manifest.i18n[lng]` (URL).
- **Visibilidad**: `evaluateRule(rule, {session, row, form})` con `permission` (comodines
  `*.*.*`), `field/equals`, `expr` (`==`, `!=`), `allOf/anyOf/not`; *fail-open* como Angular salvo
  `permission` (*fail-closed*).
- **Informe de compatibilidad**: `docs/COMPATIBILIDAD-CONFIG.md` (tarea 1) dice clave a clave qué
  se soporta, qué se normaliza y qué se ignora nunca.

#### 4.3.1 `manifest.json` (propuesta para el pack karpo-go de Theros)

```jsonc
{
  "schemaVersion": 1,
  "api": { "baseUrl": "/api", "apiVersion": "v1" },
  "locales": ["es", "en"], "defaultLocale": "es",
  "i18n": { "es": "i18n/es.json", "en": "i18n/en.json" },
  "theme": { "origin": "/themes/", "default": "modern-flat",
             "available": ["modern-flat", "classic-1995", "glossy-2003", "souq"] },
  "map": { "sources": {
      "demo-geojson": { "type": "geojson", "url": "/geo/{level}.geojson" },
      "demo-pmtiles": { "type": "pmtiles", "url": "/geo/karpo-demo.pmtiles", "layer": "boundaries" } } },
  "entities": [
    { "id": "facility", "path": "go/facilities/facility.config.json",
      "permissions": { "read": "Facilities.Facility.Read", "write": "Facilities.Facility.Update" },
      "overlay": { "views": [ { "kind": "tree", "parentField": "partOf", "labelField": "name" },
                              { "kind": "map", "boundaryField": "location.geoBoundary", "aggregate": "count",
                                "levels": [ "…" ] } ],
                   "form": { "sections": [ { "id": "main", "fields": [ { "field": "partOf",
                     "lookupEntity": "facility", "lookupDisplayField": "name" } ] } ] } } }
  ],
  "menu": [ { "id": "ops", "labelKey": "menu.operations", "children": [ { "entity": "facility" } ] } ]
}
```

Las rutas `path` son relativas a la carpeta del manifiesto; `go/` la sirve un plugin de Vite desde
`../angular/config/entities` en desarrollo y la copia a `dist` al construir (tarea 0).

### 4.4 Adaptador HTTP y sesión (`src/api`, `src/auth`)

- `createHttpClient({ baseUrl, getToken, refresh, getScope, onUnauthorized })` sobre `fetch`:
  `Authorization`, `X-Client: web`, `X-Organization-Scope` (si hay selección), `Accept-Language`
  (aunque Go no lo lea, A2 §1), tiempo máximo 30 s salvo exportaciones, D-R14 en reintentos.
- **Refresco único**: un 401 lanza un solo `POST /api/auth/refresh`; las demás peticiones esperan y se
  reintentan una vez; un segundo 401 cierra la sesión. El token se renueva antes de `expiresAt`
  (15 min, A2 §1). Sesión en `sessionStorage` (envuelto en try/catch).
- **Login de Go** (A2 §1): `{accessToken, refreshToken, expiresAt, userId, username,
  mustChangePassword}`; sin `success`. Con `mustChangePassword` la app sólo ofrece la pantalla de
  cambio (`POST /api/auth/change-password` → sesión nueva). Identidad y permisos de
  `GET /api/auth/context` (se recarga tras login, refresco, cambio de ámbito y un 403).
- **Adaptador del contrato de mantenimiento**: lista por `POST {ep}/search` con
  `{filter, pagination:{pageNumber,pageSize≤1000,orderBy,ascending}}` (sólo con campos de orden
  admitidos) o `GET` con `page/pageSize/sortBy/sortOrder`; respuesta normalizada
  (`items|data|results`, `total|totalCount`); estilo `number-size` para rutas sin `v1`
  (`{items,total,number,size}`, parámetro `size`). Detalle `GET {ep}/{id}`; alta `POST` (201 con
  `Location`); cambio `PUT` con `{...patch, version}` (Go mezcla como PATCH, A2 §2); baja `DELETE`
  con cuerpo opcional (`{reason}`) y 204; órdenes `POST {ep}/{id}/{orden}` con `{version, …}`;
  202 o `pendingApproval` → `CommandResult.status = 'pending'`.
- **Errores**: problem+json con `errors` diccionario y `errorCodes` (contrato `v1`) **y** con
  `errors` lista `[{field,code,message}]` (rutas sin `v1`) → `ApiErrorShape`. 409 → `conflict`;
  422 con `code` → `business`; 400 con `errors` → `validation`. Mensaje al usuario por
  `errors:<code>` traducido; el inglés del servidor sólo como reserva.
- **Normalizadores auxiliares**: catálogos PascalCase → camelCase (`/api/catalogs/facility-types`,
  `/api/hr/catalogs/*`, `/api/reference/*`); árbol `GET /api/hr/positions/{id}/chart?depth=` →
  nodos; `GET /api/geography/boundaries?parent&level&size` → delimitaciones.
- **Etiquetas de claves ajenas**: `useLookupLabel(entity, id)` con caché `queryKeys.label` y
  agrupación de peticiones en el mismo fotograma.

### 4.5 Temas (`src/theme`)

- **Capas**: `@layer th.tokens, th.modes, th.base, th.domain, th.a11y, karpo.ext,
  karpo.components;` declarado antes que el tema; nunca `!important`.
- **`ThemeProvider`**: inserta `<link rel="stylesheet" href="{origin}{tema}/index.css">`, lee
  `{origin}{tema}/manifest.json` (modos, `culture`, densidad, hash del diccionario, estilo de iconos
  si consta; si no, se mide un `svg[data-th-icon]` sonda), expone `ThemeApi`. Cambio de tema =
  cambiar el `href` y esperar a los cinco `@import`; cambio de modo = `data-th-mode` en `<html>`
  (sólo los que el tema declare; el resto se oculta en el menú). Observa `prefers-color-scheme`,
  `prefers-contrast`, `forced-colors` y el atributo, y avisa a los suscriptores (`onChange`).
- **`resolveColor(token, el)`**: pinta `color: var(token)` en un elemento sonda dentro de `el` y
  devuelve el `rgb()` calculado (A3 §7.1). Lo usan MapLibre, driver.js y cualquier canvas.
- **`karpo.ext`** (`src/theme/kx-ext.css`): sólo `var(--th-*)`, `color-mix()` y `calc()`:
  estados de celda (`--kx-cell-*`, PT-2), de guardado (`--kx-save-*`, PT-3), tono marca
  (`--kx-accent-*`, PT-4), paleta de datos provisional (`--kx-data-seq-1..7`,
  `--kx-data-series-1..10`, `--kx-data-missing`, PT-5), esqueleto (PT-6), velo del recorrido.
  `src/theme/kx-structure.css` es **el único fichero con literales numéricos**: escala `z-index`
  (`--kx-z-sticky` … `--kx-z-tour`, PT-7) y nada más (los puntos de corte van en `@container` con `em`).
- **Ganchos primero**: los componentes emiten `data-th`, `data-th-kind`, `data-th-tone`,
  `data-th-state`, `role`, `aria-*` (A3 §2.3); lo propio va con `data-kx-*`. Celdas hijas directas
  del registro, `role="row"`, cebra por índice de datos, sin `outline: none` (A3 §7.5, §7.7).
- **Temas de la demo**: `scripts/sync-themes.mjs` ejecuta
  `go run ./cmd/theros-front build <tema> -out <tmp>` con `cwd` en `C:\Git\Paranoia\Theros.Front`
  (sólo lectura: escribe en una carpeta temporal nuestra) para `modern-flat`, `classic-1995`,
  `glossy-2003` y `souq`, y copia los seis CSS y `manifest.json` a `demo/public/themes/<tema>/`
  con un `THEMES.md` que anota commit y hash del diccionario. Los ficheros generados se versionan
  en el repositorio (la demo no depende de tener Theros.Front).
- **Prueba de contrato** (`src/theme/contract.test.ts` y `scripts/`): (a) el hash del diccionario
  de cada `manifest.json` es el esperado (`sha256:2276b820…`); (b) un recorrido del DOM de cada
  componente montado comprueba que todo `data-th*`/`role`/`aria-*` pertenece a la lista de A3 §2.3;
  (c) stylelint prohíbe en `src/**/*.css` colores literales (hex, `rgb()`, `hsl()`, nombres),
  unidades de longitud (`px`, `rem`, `em` salvo en `@container`), `font-family` y `font-weight`
  numérico, salvo en `kx-structure.css`; (d) un lint de TSX prohíbe `style={{…}}` con claves que no
  empiecen por `--kx-` o no sean `transform`/`inset`/`width`/`height` de disposición.

### 4.6 i18n y RTL (`src/i18n`)

- i18next con espacios de nombres por módulo y **un fichero por idioma y espacio**:
  `src/locales/{es,en}/<ns>.json` con `errors` y `auth` (T1), `help` (T2), `fields` y `ui` (T3),
  `grid` (T5), `core` y `detail` (T6), `tree` y `map` (T7), cargados con
  `import.meta.glob` (añadir un fichero no toca código ajeno). `es` es el idioma de referencia;
  prueba «sin claves sin traducir» (mismas claves en `es` y `en`) y pseudolocalización `xx`
  (`[ÉŚ…]`) en desarrollo para cazar textos escritos a mano.
- Fechas, números e importes con `Intl` (importes en cadena de Go → `decimal` sin pérdida: se
  formatean como texto con `Intl.NumberFormat` sólo para mostrar; el valor viaja como cadena).
- `dir` y `lang` en `<html>` desde el idioma (D-R16); `?dir=rtl` fuerza la dirección en la demo.
  Sólo propiedades lógicas en CSS; iconos direccionales con `data-kx-mirror` y regla propia
  (A3 §7.8, PT-11); organigrama y migas siguen `dir`.

### 4.7 Marco de la app (`src/shell`, tarea 6; el registro de atajos y los avisos son de `src/ui`, tarea 3)

- `<KarpoApp manifestUrl | manifest, apiBaseUrl, themeOrigin, locale, router='browser'|'memory'|'hash', basename />`.
- Rutas: `/login`, `/cambiar-clave` (traducida por clave), `/:entity` (vista por defecto o
  `?view=`), `/:entity/:id` (detalle; `?mode=edit`), `/403`, `*`. Con `presentation: 'panel'` el
  detalle se abre como panel junto a la lista (redimensionable) sin perder selección ni scroll.
- Menú lateral desde `manifest.menu` filtrado por permisos; buscador del menú; selector de
  organización (`/api/internal-organizations/names` → `X-Organization-Scope`).
- Barra superior: título, interruptor de vista (si la entidad tiene más de una), botón de ayuda
  (`data-kx-help`), tema, modo, idioma, usuario.
- Paleta `Mod+K` (cmdk + fuse.js): ir a entidad, crear, órdenes de la selección, tema, idioma, modo.
- Registro de atajos con ámbitos (§4.2) y panel `Shift+?` generado del mismo registro.
- Avisos (toast) con `[data-th=feedback]` y `role=status|alert`, con acción «Deshacer».

### 4.8 Campos y primitivas (`src/fields`, `src/ui`, tarea 3) y ayuda (`src/help`, tarea 2)

- **Registro único de tipos de campo** (A4 §5.1.1): `text, textarea, number, decimal, money,
  percentage, date, datetime, boolean, select, multiselect, badge, lookup, email, phone, url`. Cada
  uno con visor, editor, `format`/`parse` por idioma (coma decimal, fechas locales), `validate` por
  código (`required`, `maxLength`, `minLength`, `min`, `max`, `pattern`, `format`), alineación y
  analizador de pegado. Lo usan cuadrícula, detalle, formularios, filtros y edición masiva.
  `lookup` = combobox con búsqueda en servidor (`EntityApi.list` con `search`) y etiqueta resuelta.
- **Primitivas** con ganchos de Theros: `Action` (`data-th=action`, `data-th-kind`,
  `aria-disabled` + `data-th-reason`), `Dialog` (Radix sobre `dialog`/`role=dialog`), `Feedback`,
  `Badge`/`Chip`, `Kbd`, `Skeleton`, `Spinner`, `Icon` (Phosphor, peso según `ThemeInfo.iconStyle`,
  `svg[data-th-icon]`, espejo RTL), `Toolbar` (`data-th=toolbar`, nunca `role=toolbar`), `Field`
  (`data-th=field`, `label`, `data-th=hint`, `data-th=field-error`, `aria-describedby`),
  `ConfirmDialog`, `PromptDialog` (motivo de un bloqueo).
- **Ayuda guiada**: `HelpProvider` + `HelpButton` + `useHelp(screen, steps)`. Los pasos salen de
  `config.help` (traducidos por `Text`) y, si no hay, de las anclas `[data-kx-help]` visibles en el
  orden del DOM, con textos `t('<ns>:help.<nombre>.title|body')`. driver.js sin su hoja por
  defecto: hoja propia `src/help/help.css` con `surface.popover.*`, `text.*` y acciones del tema;
  velo con `resolveColor('--th-surface-scrim')`; `animate`/`smoothScroll` sólo con
  `prefers-reduced-motion: no-preference`; foco devuelto al botón al cerrar; teclado (flechas,
  Esc); recorrido «visto» por pantalla en `localStorage` (try/catch).

### 4.9 Cuadrícula (`src/grid`)

- Modelo con `@tanstack/react-table` (columnas de `config.columns`, orden, tamaño, fijado,
  visibilidad) y filas virtualizadas con `@tanstack/react-virtual` (las columnas no se virtualizan:
  límite conocido de ≤ ~40 columnas, anotado en la ronda 2); render propio
  `div[data-th=collection][role=grid]` con `aria-rowcount/colcount`, filas `role=row` y
  `aria-rowindex`, celdas `role=gridcell` hijas directas y `aria-colindex`; cabeceras
  `role=columnheader` con `aria-sort`. Altura de fila **medida** con una fila sonda (y vuelta a
  medir en `ThemeApi.onChange`), nunca fija.
- **Datos**: página de servidor con scroll infinito (`useInfiniteQuery`, `pageSize` de la config,
  hasta 1000), filtros como chips (los `filters` de la config) + búsqueda rápida (`_search`), orden
  sólo por campos de servidor admitidos.
- **Edición** (almacén zustand + immer por entidad): capas valor del servidor → borrador →
  pendiente; estado por celda `data-kx-cell="active|editing|dirty|invalid|saving|error|readonly"` y
  por fila `data-th-state="dirty|new"` + `data-kx-state="saving|saved|error|conflict|pending"`.
  Validación por `FieldTypeDef.validate` al confirmar; errores del servidor (`fieldErrors`) a la
  celda; `aria-live` con el resumen.
- **Guardado**: `optimistic` = al salir de la fila (o de la celda si así se configura) un `PUT` con
  `version`, reversión y aviso «Deshacer» si falla; `batch` = barra «N cambios · Guardar (Mod+S) ·
  Descartar», PUT en secuencia (D-R15) con informe por fila; 409 → fila en `conflict` con acción
  «Recargar y reaplicar» (vuelve a leer, reaplica el parche si no toca los mismos campos, si no
  pregunta).
- **Deshacer/rehacer**: `Mod+Z`/`Mod+Shift+Z` con parches inversos (también pegado y relleno); un
  cambio ya guardado se deshace con otro `PUT`.
- **Teclado** (ronda 2, D-R21: `Tab`/`Shift+Tab` salen de la rejilla, que es una sola parada de
  tabulación; al editar, `Tab` confirma y pasa a la celda siguiente): flechas, Inicio/Fin, Mod+Inicio/Fin, RePág/AvPág, Enter/F2 editar,
  escribir sobrescribe, Esc cancela (dos veces: la fila), Supr limpia, Mod+C/Mod+V (TSV con vista
  previa de errores), Mod+D rellena hacia abajo, Espacio selecciona fila, Shift+flechas rango,
  `/` filtro, **Mod+Enter o doble clic en una fila → `navigate({id, mode:'edit'})`**.
- Acciones de fila y masivas desde `config.commands` (`view`, `edit`, `delete` con `confirm`/
  `prompt`, órdenes HTTP), con 202 → aviso «pendiente de aprobación».
- Estado de columnas y vista guardados por usuario en `localStorage` (try/catch); la URL lleva
  filtros, orden y vista.

### 4.10 Detalle (`src/detail`)

- `DetailView`: panel (por defecto) o página; cabecera con título (`displayField`), estado,
  órdenes (`detail-header`), botón Editar/Guardar/Cancelar; pestañas = secciones + rejillas hijas.
- `?mode=edit` abre en edición con el foco en el primer campo editable; `Mod+Enter` guarda; `Esc`
  cancela (con aviso si hay cambios); aviso al salir con cambios sin guardar.
- Formulario react-hook-form + esquema zod **generado** de `FieldDef` (mismos códigos que el
  registro de campos); `hint` pintado; secciones con `columns` y `width` 1..12; campos `immutable`
  en sólo lectura tras el alta.
- Guardado `PUT` con los cambios + `version` del registro leído; respuesta = registro nuevo (se
  escribe en la caché de lista y de registro sin releer). 409 → diálogo de conflicto con las
  diferencias (mío / servidor) y «Recargar» o «Reaplicar mis cambios». 400/422 → errores en cada
  campo por `errorCodes`; los que no casan con ningún campo, en un aviso de la ficha.
- Alta (`form.create`) en el mismo componente con `mode=new` (`/:entity/nuevo`): `POST`, 201 →
  navega al registro creado.
- Navegación al registro anterior/siguiente de la lista (`Alt+↑/↓`) con la caché de la lista.
- Rejillas hijas (`childGrids`): tabla de sólo lectura (`role=table`) sin `?foreignKey=` si el
  endpoint lleva `{id}`.

### 4.11 Organigrama (`src/tree`)

- Vista `tree` declarada en la superposición; fuente `flat` (lista paginada entera, padre por
  `parentField`, raíces = padre vacío o ausente) o `chart` (`/api/hr/positions/{raíz}/chart?depth=`,
  carga perezosa de más profundidad al desplegar; raíces de la lista, A2 §4).
- **Listado**: `role=tree` con `treeitem`, `aria-expanded`, `aria-level`, `aria-setsize`,
  `aria-posinset`; teclado WAI-ARIA (flechas, Inicio/Fin, `*` expande hermanos, letras para saltar);
  contador de descendientes; búsqueda que despliega y resalta.
- **Diagrama**: @xyflow/react con nodos tarjeta (visor del registro de campos: etiqueta, subtítulo,
  insignia), disposición dagre (arriba-abajo; en RTL, hermanos en espejo), plegado por rama con
  contador, zoom con rueda y botones, «ajustar», minimapa, búsqueda que centra el nodo, teclado
  entre padre/hijos/hermanos, doble clic → detalle en edición. Colores por `var(--th-*)`/`--kx-*`
  en CSS (SVG/DOM, sin sonda).
- Interruptor **listado ⇄ diagrama** en la barra (`?view=tree&layout=list|diagram`), conservando
  selección y plegado.

### 4.12 Mapa por niveles (`src/map`)

- Vista `map` declarada en la superposición: niveles `world` (países ISO alfa-2),
  `country` (comunidades, `nutsCode` NUTS 2), `region` (provincias, NUTS 3) y `city` (municipios,
  `lauCode`/`ineCode`); la escalera de Go (A2 §5) sólo tiene detalle para España: el resto de
  países no se puede abrir (se indica).
- MapLibre **sin estilo remoto**: estilo construido en ejecución con fondo, relleno coroplético
  (`--kx-data-seq-*` resueltos con la sonda), bordes (`--th-edge-strong`), elegido/pasado
  (`--th-state-selected-*`); se reconstruye en `ThemeApi.onChange`. Etiquetas como marcadores HTML
  (sin glifos ni sprites). Ninguna URL de terceros: la prueba e2e registra las peticiones y falla
  si alguna sale del origen.
- Fuente por nivel según `manifest.map.sources`: `geojson` (`/geo/{level}.geojson`, filtrado por
  padre) o `pmtiles` (`pmtiles://{url}` con el protocolo de `pmtiles` registrado en MapLibre). El
  origen es configurable en el manifiesto.
- Valor por delimitación = agregado en cliente (`count`/`sum`) de las filas de la entidad por
  `boundaryField`, subido por la cadena de antecesores (`/api/geography/boundaries/{id}` →
  `ancestors[]`, con caché). Leyenda accesible.
- Navegación: clic entra en el nivel siguiente (`fitBounds`), migas (Mundo › España › Comunidad de
  Madrid › Madrid), Esc sube, URL `?view=map&level=region&code=ES30`; `flyTo` sólo con
  `prefers-reduced-motion: no-preference`.
- Interruptor **listado ⇄ mapa**: la lista es una tabla del nivel actual (delimitación, valor) que
  también navega; es la alternativa accesible.
- Rótulo «Geometría esquemática de demostración» cuando la fuente es la de la demo (Q-4).

### 4.13 Geodatos y PMTiles de la demo

- `scripts/gen-geo.mjs` (tarea 4) genera **geometría esquemática determinista** (no cartográfica,
  sin copiar ficheros de terceros): para cada nivel un `FeatureCollection` con propiedades
  `code`, `name`, `level`, `parentCode` y los códigos de Go (`alpha2`, `nutsCode`, `lauCode`).
  Mundo: un polígono simple por país en su posición aproximada (centroides escritos en el script,
  tamaño por clase de superficie). España: comunidades y provincias como teselas de un cartograma
  en rejilla en su posición relativa; municipios de dos provincias (Madrid y Barcelona, recortados a
  una muestra) por subdivisión regular del polígono de su provincia. Salida en
  `demo/public/geo/{world,country-ES,region-<nuts2>,city-<nuts3>}.geojson` y un índice.
- `scripts/build-pmtiles.mjs` (tarea 7) escribe `demo/public/geo/karpo-demo.pmtiles` (PMTiles v3,
  compresión `none`) con un codificador MVT propio mínimo (polígonos, propiedades de texto) y
  zooms 0..8, sin dependencias nuevas (`node:zlib` si hiciera falta).

### 4.14 Demo y modo local

- **Datos deterministas** (`demo/data/dataset.mjs`, ESM puro, semilla fija `20261127`, PRNG
  mulberry32, «hoy» fijo 2026-11-27 sólo en los datos): 3 organizaciones; 30 instalaciones en árbol
  (edificio → planta → sala) con dirección y municipio; 12 centros de trabajo; 40 festivos; 120
  productos; 60 activos; 25 proveedores (alguno bloqueado, con historial); 10 transportistas;
  45 puestos en 5 niveles con titulares; los 248 países con su `boundary`; delimitaciones de España
  (7 + 19 + 59 + municipios de la muestra) con los mismos códigos que los geodatos.
- **Modo `demo`** (`npm run dev`): MSW en el navegador implementa el contrato de A2 (lista GET y
  `/search`, filtros admitidos y los ignorados, lista blanca de orden con 400, páginas, `version` con
  400/409, PUT como PATCH, 201/204/202, problem+json, login con `mustChangePassword` desactivado
  para el usuario de demo, `/api/auth/context`, catálogos PascalCase, árbol de puestos,
  delimitaciones). Latencia simulada determinista pequeña; `?fallo=409` y similares fuerzan
  errores para pruebas y capturas. El usuario de demostración y su contraseña están en
  `demo/data/credenciales-demo.json` (valores de prueba de la propia demo).
- **Modo `local`** (`npm run karpo:local`): `scripts/karpo-local.mjs` compila `cmd/karpo` (raíz del
  repositorio, sólo lectura), crea un SQLite en una carpeta temporal, genera `KARPO_JWT_SECRET` y la
  contraseña del administrador de arranque aleatorios, busca **puertos libres** (evitando los rangos
  reservados de Windows: prueba de `listen`), ejecuta `migrate` y `serve`, espera a `/readyz`, lanza
  `scripts/seed-karpo.mjs` (cambia la contraseña, crea por HTTP los mismos datos que el dataset;
  resuelve delimitaciones por código porque los UUID de Go son otros; tolera e informa los fallos
  de las altas no probadas en A2 §9), escribe las credenciales en `.karpo-local/credenciales.json`
  (ignorado por git) y arranca Vite en modo `local` con proxy de `/api`, `/healthz` y `/readyz` al
  puerto de Go (Go no tiene CORS, A2 §1). Ctrl+C para ambos; `npm run karpo:stop` limpia procesos
  huérfanos registrados en `.karpo-local/pids.json`. Nunca el puerto 55432 ni una base viva.

---

## 5. Peticiones fuera de esta carpeta

Primera versión escrita con este plan, a partir de A1 §13, A2 §8 y A3 §9:

- `docs/PETICIONES-BACKEND.md` (backend Go): B-1 a B-14.
- `docs/PETICIONES-THEROS-FRONT.md` (contrato de temas): PT-1 a PT-14, y TC-1 a TC-6 para el
  pack karpo-go de Theros (configuración).

Durante las olas 1 y 2 cada tarea anota lo nuevo en `docs/notas/<tarea>.md` (fichero suyo); la
tarea 8 lo consolida en los dos documentos.

---

## 6. Estrategia de pruebas

| Nivel | Herramienta | Qué cubre | Dónde |
|---|---|---|---|
| Unitarias | vitest (entorno `node`) | normalizador con los 8 JSON de Go reales y casos de A1 §5; reglas de visibilidad; cliente HTTP (refresco único, sin reintento de PUT, errores en sus dos formas); tipos de campo (`parse`/`format`/`validate` en es y en); almacén de edición y deshacer; agregados del mapa; geodatos y PMTiles (lectura con `pmtiles`) | `src/**/*.test.ts` |
| Componentes | vitest + jsdom + Testing Library + MSW (nodo) | cada vista montada contra el backend falso; consultas por **rol y nombre accesible** (sustituye a axe, D-R5); teclado con `user-event`; RTL (`dir`) | `src/**/*.test.tsx` |
| Contrato | vitest (`project: contract`) | ganchos Theros válidos, hash del diccionario, claves i18n iguales en es/en, informe de licencias `--check`, `npm pack --dry-run` sólo con `dist/` | `src/**/*.contract.test.ts`, `scripts/*.test.mjs` |
| Lint | eslint, stylelint, `tsc --noEmit` | tipos estrictos, hooks, abstinencia de literales (§4.5) | `npm run lint`, `npm run typecheck` |
| e2e | vitest + playwright-core con `channel: 'msedge'` (si no, `'chrome'`) | contra `vite preview` del modo demo en un puerto libre: doble clic → detalle en edición; editar celdas, deshacer, guardar por lotes, 409; teclado completo de la cuadrícula; organigrama lista ⇄ diagrama, plegar, zoom; mapa en 4 niveles con migas y Esc; recorrido de ayuda; tema claro/oscuro/`classic-1995`/`glossy-2003`; RTL; ninguna petición fuera del origen | `e2e/*.e2e.ts` |
| Humo local | `npm run karpo:local` + `e2e/local.e2e.ts` (opcional, `KARPO_LOCAL=1`) | login real, lista de instalaciones, cambio con `version`, árbol de puestos | manual o a petición |

Cobertura de líneas exigida en `config`, `api`, `fields` y el almacén de edición: **≥ 80 %**.
Ninguna prueba descarga nada; si no hay Edge ni Chrome, las e2e se saltan con un mensaje claro.

**La batería** (`npm run check`): `typecheck && lint && test && test:contract && licencias --check
&& build && pack:check`. Las e2e y las capturas, aparte (`npm run test:e2e`, `npm run capturas`).

## 7. Script de capturas

`npm run capturas` → `scripts/capturas.mjs` (tarea 8):

1. `vite build --mode demo` y `vite preview` en un puerto libre (parado al terminar, también si falla).
2. `chromium.launch({ channel: 'msedge' })` y, si no existe, `'chrome'`; con
   `--enable-unsafe-swiftshader` para WebGL sin GPU. Ventana 1440×900, `deviceScaleFactor: 1`,
   animaciones desactivadas (`reducedMotion: 'reduce'`), fuentes del sistema.
3. Para cada escena y variante espera a un indicador de «listo» (`data-kx-ready`) y guarda
   `docs/capturas/<escena>-<variante>.png`:

| Escena | URL de la demo (resumen) | Estado preparado |
|---|---|---|
| `cuadricula-editable` | `/product` | 3 celdas sucias, 1 inválida con su error, una en edición, barra de lote visible |
| `detalle-edicion` | `/facility/<id>?mode=edit` (doble clic real desde la lista) | un campo cambiado y un error de campo |
| `organigrama-listado` | `/position?view=tree&layout=list` | dos ramas desplegadas, una plegada, búsqueda |
| `organigrama-diagrama` | `/position?view=tree&layout=diagram` | ajustado, una rama plegada con contador, minimapa |
| `mapa-mundo` | `/facility?view=map&level=world` | coropletas por país |
| `mapa-pais` | `…&level=country&code=ES` | comunidades |
| `mapa-region` | `…&level=region&code=ES30` | provincias |
| `mapa-ciudad` | `…&level=city&code=ES300` | municipios y migas completas |
| `ayuda-recorrido` | `/product` + botón de ayuda | segundo paso del recorrido abierto |

Variantes: `claro` (`modern-flat`, `mode=light`, `lng=es`), `oscuro` (`mode=dark`, `lng=es`),
`rtl` (`mode=light`, `lng=en`, `dir=rtl`). Total: **27 PNG**. El script escribe además
`docs/capturas/README.md` con la tabla de ficheros, fecha, navegador y versión, y falla si falta
alguna o si alguna petición salió del origen.

## 8. Empaquetado

- `vite.lib.config.ts`: entrada `src/index.ts`, formato ES, `dist/karpo-react.js` + `dist/karpo.css`
  (capas `karpo.ext` y `karpo.components`); todas las `dependencies` y `peerDependencies` externas.
- `package.json`: `"type": "module"`, `exports` (`"."` con `types` e `import`, `"./karpo.css"`),
  `sideEffects: ["**/*.css"]`, `files: ["dist"]`, sin `engines` y con `devEngines.runtime >=24.15`
  (ronda 2, D-R25), `"./map.css"` → `dist/karpo-map.css` con la hoja de MapLibre (D-R24), `license` (Q-3),
  `prepublishOnly: node scripts/no-publish.mjs` (falla siempre con el motivo).
- `npm run pack:check`: `npm pack --dry-run --json` y comprueba que sólo hay `dist/**`,
  `package.json`, `README.md` y `LICENSE`.

## 9. Riesgos

| Riesgo | Mitigación |
|---|---|
| `role=grid` con virtualización (accesibilidad y foco) | Prototipo en la tarea 5 primero; pruebas por rol con `aria-rowindex` |
| TanStack Table 9 es mayor reciente | Adaptador fino en `grid/model.ts`; versión fija |
| WebGL en Edge sin GPU para capturas/e2e | SwiftShader; si falla, la e2e del mapa comprueba la lista y el estilo, y la captura lo anota |
| Las altas no probadas en Go (productos, activos, proveedores) | El sembrador informa y sigue; peticiones B-n |
| Paralelismo en una carpeta | Propiedad de ficheros exclusiva (§10); contratos fijos (§4.2); sin `package.json` fuera de la ola 0 |
| Theros.Front cambia (`carril/margenes`) | `gap` en contenedores, nada de márgenes del campo (A3 §8.1); prueba de contrato |

---

## 10. Tareas y olas

Reglas comunes a todas las tareas:

- Sólo se escribe en los ficheros y carpetas del campo `files` de la tarea. Nadie salvo la tarea 0
  toca `package.json`, `package-lock.json`, configuraciones de herramientas o `src/contracts/`.
  `src/<módulo>/index.ts` lo crea la tarea 0 como esqueleto y pasa a ser de la tarea del módulo.
- CSS de cada módulo en su carpeta, dentro de `@layer karpo.components`; sin literales (§4.5).
- Textos de interfaz sólo por clave, en `src/locales/{es,en}/<ns>.json` de la propia tarea.
- Lo que haga falta de otra tarea y no esté en §4.2 se anota en `docs/notas/<tarea>.md`.
- Al terminar: `npm run typecheck`, `npm run lint` y las pruebas del propio módulo en verde; no
  romper las de otros. Sin commit (D-R17). Procesos arrancados, parados.

| Ola | Tarea | Resumen |
|---|---|---|
| 0 | T0 Andamiaje | dependencias, configuración de herramientas, contratos y contextos, esqueletos, licencias, utilidades de procesos y navegador |
| 1 | T1 Configuración, API y sesión | `src/config`, `src/api`, `src/auth` (con las páginas de entrada y cambio de clave) |
| 1 | T2 Temas, i18n, RTL y ayuda | `src/theme`, `src/i18n`, `src/help`, temas de la demo |
| 1 | T3 Campos, primitivas y atajos | `src/fields`, `src/ui` (avisos, diálogos, iconos, registro de atajos) |
| 1 | T4 Demo y datos | `demo/`, MSW, dataset, geodatos, sembrador, modo local |
| 2 | T5 Cuadrícula | `src/grid` |
| 2 | T6 Marco de la app y detalle | `src/shell`, `src/detail` |
| 2 | T7 Organigrama y mapa | `src/tree`, `src/map`, PMTiles |
| 3 | T8 e2e, capturas y cierre | `e2e/`, capturas, README, peticiones, verificación final |

En la ola 2 las vistas se prueban montadas directamente (cada tarea con su propio arnés de
pruebas en su carpeta, sobre `demo/mocks/node.ts` de T4); la integración en el marco la cierra T8.
§11 resume ficheros y criterios de cada tarea; el detalle completo es el del plan estructurado.

---

## 11. Ficheros y criterios por tarea

Rutas relativas a `front/react`. «Pasa» = sale con código 0.

### T0 · Andamiaje (ola 0)

Ficheros: `package.json`, `package-lock.json`, `.npmrc`, `.gitignore`, `LICENSE`, `tsconfig.json`,
`tsconfig.lib.json`, `tsconfig.node.json`, `vite.config.ts`, `vite.lib.config.ts`,
`vitest.config.ts`, `vitest.e2e.config.ts`, `eslint.config.js`, `stylelint.config.js`, `build/`,
`src/index.ts`, `src/vite-env.d.ts`, `src/test/`, `src/contracts/`, los esqueletos
`src/<módulo>/index.ts` (pasan a su tarea), `scripts/licencias.mjs`, `scripts/no-publish.mjs`,
`scripts/pack-check.mjs`, `scripts/lib/`, `docs/LICENCIAS.md`.

Criterios: `npm ci` reproduce el árbol desde el lock; versiones directas = §3 (o desviación anotada
con motivo en `docs/LICENCIAS.md`); `npm run licencias -- --check`, `typecheck`, `lint`, `test`
(sin pruebas aún, pasa), `build:lib` y `pack:check` pasan; `npm run prepublishOnly` falla; el plugin
sirve `/config/go/facilities/facility.config.json` idéntico al de `front/angular` (prueba); no se
ha descargado ningún navegador; `git status` sólo muestra cambios en `front/react`.

### T1 · Configuración, API y sesión (ola 1)

Ficheros: `src/config/`, `src/api/`, `src/auth/`, `src/locales/{es,en}/errors.json`,
`src/locales/{es,en}/auth.json`, `docs/COMPATIBILIDAD-CONFIG.md`, `docs/notas/T1.md`.

Criterios: los 8 JSON reales de Go se normalizan sin error y cumplen los 15 puntos de A1 §5 (una
prueba por punto); tipo desconocido → `text` + aviso; manifiesto con superposición, modo legado y
textos (clave, por idioma, literal); reglas de visibilidad con comodines; cliente HTTP: un solo
refresco con 401 concurrentes, ningún reintento de PUT/POST/DELETE, GET reintentado ≤ 2 con
`Retry-After`, cabeceras, problem+json en sus dos formas, 409/422/202/413; adaptador: cuerpo de
`/search` exacto, `pageSize` ≤ 1000, estilo `number-size`, catálogos PascalCase, árbol de puestos;
sesión: `mustChangePassword`, contexto, `can()`; páginas de entrada y cambio de clave probadas por
rol; cobertura ≥ 80 % en `config` y `api`; `docs/COMPATIBILIDAD-CONFIG.md` cubre todas las claves de A1 §3.

### T2 · Temas, i18n, RTL y ayuda (ola 1)

Ficheros: `src/theme/`, `src/i18n/`, `src/help/`, `src/locales/{es,en}/help.json`,
`scripts/sync-themes.mjs`, `demo/public/themes/`, `docs/notas/T2.md`.

Criterios: `npm run themes:sync` deja los 4 temas (6 CSS + `manifest.json`) y `THEMES.md` con
commit y hash; `ThemeProvider` con origen configurable, modos según manifiesto (`classic-1995` sólo
claro), `data-th-mode`, `onChange`, aviso de hash distinto; `resolveThemeColor` con sonda;
`kx-ext.css` sólo con `var(--th-*)`/`color-mix`/`calc` y stylelint en verde; prueba de contrato
(hash de los 4 manifiestos, `assertTherosHooks`); i18n con espacios por `import.meta.glob`, prueba
de claves iguales en es/en sobre **todo** `src/locales`, `dir`/`lang` en `<html>`, `?dir=rtl`;
ayuda: pasos de la configuración o de las anclas visibles, se omiten las que no existen, textos
traducidos, velo con la sonda, foco devuelto, sin hoja de driver.js por defecto.

### T3 · Campos, primitivas y atajos (ola 1)

Ficheros: `src/fields/`, `src/ui/`, `src/locales/{es,en}/fields.json`,
`src/locales/{es,en}/ui.json`, `docs/notas/T3.md`.

Criterios: los 16 tipos de campo con `format`/`parse`/`validate` probados en es y en (coma decimal,
fechas locales, importes en cadena sin pérdida, booleano tolerante, `select` por etiqueta o
clave); `lookup` con búsqueda y etiqueta resuelta (contra un `EntityApi` falso); primitivas con
ganchos de Theros comprobados (`data-th=action` + `aria-disabled` + `data-th-reason`, `Toolbar` sin
`role=toolbar`, `Field` con `aria-describedby`); `Icon` con peso según `ThemeContext` y espejo RTL;
avisos con acción «Deshacer»; registro de atajos con ámbitos y conflicto detectado; cobertura
≥ 80 % en `fields`.

### T4 · Demo y datos (ola 1)

Ficheros: `demo/index.html`, `demo/main.tsx`, `demo/config/`, `demo/data/`, `demo/mocks/`,
`demo/public/mockServiceWorker.js`, `demo/public/geo/*.geojson`, `demo/public/geo/index.json`,
`scripts/gen-geo.mjs`, `scripts/seed-karpo.mjs`, `scripts/karpo-local.mjs`,
`scripts/karpo-stop.mjs`, `docs/notas/T4.md`.

Criterios: el dataset es idéntico en dos ejecuciones (prueba con hash); manifiesto de la demo con
los 8 de Go (sin copiarlos) + puesto y delimitación, superposiciones y textos es/en; handlers de
MSW que cumplen A2 (pruebas en nodo: 400 de orden, filtro ignorado, 400 sin `version`, 409, 201 con
`Location`, 204, 202, problem+json); geodatos de los 4 niveles con códigos que casan con el dataset;
`npm run karpo:local` arranca Go en puertos libres, siembra, imprime la URL y para todo con Ctrl+C
(comprobado una vez, informe en `docs/notas/T4.md`); nada en el puerto 55432.

### T5 · Cuadrícula (ola 2)

Ficheros: `src/grid/`, `src/locales/{es,en}/grid.json`, `docs/notas/T5.md`.

Criterios: `role=grid` con índices ARIA y celdas hijas directas; doble clic y Mod+Enter llaman a
`navigate({id, mode:'edit'})`; edición con validación por celda, deshacer/rehacer, guardado
optimista y por lotes con `version`, 409 con «Recargar y reaplicar»; teclado de §4.9 probado con
`user-event`; pegado TSV con vista previa; altura de fila medida; filtros como chips y orden sólo
por campos de servidor; cobertura ≥ 80 % en el almacén de edición.

### T6 · Marco de la app y detalle (ola 2)

Ficheros: `src/shell/`, `src/detail/`, `src/locales/{es,en}/core.json`,
`src/locales/{es,en}/detail.json`, `docs/notas/T6.md`.

Criterios: `KarpoApp` monta proveedores, rutas, menú filtrado por permisos, barra con interruptor de
vista, ayuda, tema, modo, idioma; paleta Mod+K; panel de atajos Shift+?; `?mode=edit` abre en
edición con foco en el primer campo editable; formulario generado con `hint`, `columns`, `width`;
PUT con `version`; 409 con diálogo de diferencias; errores por campo; alta y navegación
anterior/siguiente; rejillas hijas sin `?foreignKey=`; `npm run dev` muestra la demo completa.

### T7 · Organigrama y mapa (ola 2)

Ficheros: `src/tree/`, `src/map/`, `src/locales/{es,en}/tree.json`, `src/locales/{es,en}/map.json`,
`scripts/build-pmtiles.mjs`, `demo/public/geo/karpo-demo.pmtiles`, `docs/notas/T7.md`.

Criterios: lista `role=tree` con teclado WAI-ARIA; diagrama con zoom, ajustar, minimapa, plegado
con contador y búsqueda; interruptor listado ⇄ diagrama en la URL; fuentes `flat` y `chart`; mapa
con 4 niveles, migas, Esc, URL, coropletas resueltas con la sonda y reconstruidas al cambiar de
tema; fuentes GeoJSON y PMTiles (el `.pmtiles` generado se lee con `pmtiles` en una prueba); lista
⇄ mapa; ninguna URL de terceros (prueba que intercepta `fetch`).

### T8 · e2e, capturas y cierre (ola 3)

Ficheros: `e2e/`, `scripts/capturas.mjs`, `docs/capturas/`, `README.md`,
`docs/PETICIONES-BACKEND.md`, `docs/PETICIONES-THEROS-FRONT.md`, `docs/notas/T8.md`.

Criterios: `npm run check` pasa; `npm run test:e2e` pasa con Edge o Chrome locales; `npm run
capturas` deja los 27 PNG y su `README.md`; `README.md` con las órdenes de §4.14 probadas;
peticiones consolidadas desde `docs/notas/*.md`; `npm run licencias -- --check` sin cambios en
`docs/LICENCIAS.md`; ningún proceso arrancado queda vivo.
