# Plan de la ronda 2 del front React (`@karpo/react`)

Fecha: 2026-11-27. Rama `claude/front-react-7b8564`, base `1490a0d`. Papel: planificador, ronda 2.

Insumos (leídos enteros): `docs/jurado/ronda-1.md` (5 bloqueantes, 19 mejoras, nota 7,0),
`docs/analisis/ronda-2.md` (causas medidas y enfoque) y `docs/PLAN.md` (plan de la ronda 1, que
sigue vigente salvo lo que cambia §2 de este documento).

Objetivo: resolver **B-1 a B-5**, el hallazgo nuevo de `:dir()` y las **19 mejoras** del jurado,
cada bloqueante con una prueba que hoy falla, sin romper nada de lo que ya funciona (las 1130
pruebas unitarias y de componentes, las 98 de contrato y las 58 e2e siguen en verde, salvo las que
este plan invierte a propósito, que se citan).

---

## 1. Cobertura: cada punto del jurado tiene dueño

| Punto | Qué se hace | Tarea |
|---|---|---|
| B-1 iconos | `ICON_NAMES` en contratos (nombres kebab de Phosphor + alias propios); registro generado y tipado `Record<IconName,…>`; `data-kx-icon="missing"` y aviso único en desarrollo; `ConfigWarning` al normalizar menú y entidades; prueba de contrato sobre manifiesto, 4 entidades de la demo y 8 de Go; e2e del menú | R0 (lista), R1 (registro, `Icon`, e2e), R2 (avisos, contrato) |
| B-2 (a) literales | `useText()` en vez de `.literal` en mapa, árbol y filtros; leyenda sin `toLowerCase()` | R3 (`map`, `tree`), R1 (`help/steps.ts`), R4 (`grid/FilterBar.tsx`), R5 (regla de eslint) |
| B-2 (b) insignias «Cubierto» | `useFieldText().resolve` delega en `useText`; los códecs reciben un resolvedor opcional; `findOption` admite la etiqueta traducida | R0 (firma), R1 |
| B-2 (c) contenido | `scripts/gen-demo-i18n.mjs`: superposiciones con `*Key` y diccionarios es/en para las 8 entidades de Go (~320 cadenas); falla si falta una clave en `en`; `value` en `IDENTITY_KEYS`; valores de catálogo por `Accept-Language` en el MSW | R2 |
| B-2 e2e | `e2e/ingles.e2e.ts`: ningún literal español de las 8 configuraciones con `lng=en` | R5 |
| B-3 tipografía del mapa | `font: inherit` sobre `.maplibregl-map` en la hoja nueva del mapa; prueba unitaria sobre el CSS; e2e de `font-family` | R0 (regla y prueba), R3 (e2e) |
| B-4 Tab | `Tab` fuera de `nextPosition`/`isNavigationKey`; sólo `afterCommit` (edición) avanza; pruebas invertidas | R4 |
| B-5 móvil | modo compacto por contenedor ≤ 40em con menú «Más»; `kbd` oculto en táctil; icono propio del modo sistema; detalle a página completa por debajo de 64em; e2e a 360 y 390 px con `hasTouch` | R0 (`openDemoPage`), R1 (`kbd`), R4 |
| Hallazgo `:dir()` | `[dir="rtl"]` en `ui.css`, `grid.css` y `shell.css`; regla de stylelint que prohíbe `:dir(`; prueba sobre el CSS compilado; e2e del espejo | R1, R4, R5 (regla y prueba) |
| M-1 organigrama | ajuste inicial con zoom mínimo 0,85 centrado en la raíz; «Ajustar» alterna «legible» y «todo»; zoom semántico; contador `≥14` con nombre accesible | R3 |
| M-2 coropletas | «sin dato» con trama (leyenda y `fill-pattern`); `seq-1` y `missing` separados; prueba de luminancia en los 4 temas y 2 modos | R1 (tokens y prueba), R3 (trama) |
| M-3 mapa país | rótulos medidos con `measureText`; nombre corto con el largo en `title`; polo de inaccesibilidad con cálculo propio; `fitBounds` con el relleno de la leyenda (lado lógico) | R3 |
| M-4 nivel de un elemento | se salta por omisión (`map.skipSingle`, Q-10) | R0 (contrato), R2 (normalizar), R3 |
| M-5 geometría real | consulta Q-8 a Javier; no se descarga nada; origen ya configurable | — (consulta) |
| M-6 un solo control de vista | control segmentado único «Tabla · Árbol · Diagrama · Mapa» en la cabecera de entidad; fuera `ViewSwitch` de la barra y `LayoutSwitch` de las vistas; la tabla del mapa pasa a «Ver como tabla» | R0 (`viewChoices`), R4 (cabecera), R3 (vistas) |
| M-7 «Nuevo» y contador | `EntityHeader` con título, recuento plural («248 instalaciones», «de 1.204» con filtro) y acción primaria «Nuevo» según permiso | R0 (`onStatus`), R4, R3 (las vistas informan) |
| M-8 detalle | `hint` heredado de `form` al normalizar; columna «Estado» no se corta; umbral de 2 columnas a 28em; pie de acciones fijo; catálogos traducidos | R2 (herencia, catálogos), R4 |
| M-9 barra de lote | barra flotante compacta; deshacer/rehacer con el mismo estilo; relleno inferior del viewport | R4 |
| M-10 textos pegados al borde | `padding-inline` con token en los estados del mapa | R3 |
| M-11 chevrons RTL | lo arregla el hallazgo `:dir()`; e2e | R1 |
| M-12 bidi | aislamiento Unicode (FSI/PDI) de las interpolaciones en idiomas RTL; `formatRange` en rangos; `<bdi>` en texto libre | R1 (formateador, `FieldView`), R3 (nombres y leyenda), R4 (celdas de cabecera de lote) |
| M-13 roles del diagrama | `role="group"` + nombre por nodo y enlace «Ver como lista»; prueba propia de roles | R3 |
| M-14 contraste automático | prueba de contrato de pares de tokens (texto ≥ 4,5:1, no texto ≥ 3:1) | R1 |
| M-15 rendimiento | iconos generados (sólo `regular` y `fill`); diferido del detalle, la paleta, los paneles y driver.js; `zod/mini`; presupuesto gzip ≤ 250 kB del trozo inicial | R0 (script), R1, R2, R4, R5 (lo hace obligatorio) |
| M-16 hoja de MapLibre | `dist/karpo-map.css` aparte (`@karpo/react/map.css`); la demo la carga con la vista | R0 (build y paquete), R3 (quitar el `@import`, carga diferida) |
| M-17 columnas no virtualizadas | límite anotado en `PLAN.md` §4.9 y README | R5 |
| M-18 `engines` | fuera `engines` del paquete; `devEngines` ≥ 24.15 con `onFail: warn`; README dice la verdad | R0, R5 |
| M-19 Q-6 | «por defecto mientras Javier no decida» en el generador de `LICENCIAS.md` y su prueba | R0 |
| Capturas | 27 regeneradas + `ingles` (9) + `movil` (5) = 41 PNG | R5 |

---

## 2. Decisiones nuevas (técnicas y reversibles; también en `PLAN.md` §1)

| # | Decisión | Por qué |
|---|---|---|
| D-R19 | Iconos generados por script (`scripts/gen-icons.mjs`) desde `@phosphor-icons/react/dist/defs` con `react-dom/server`, sólo pesos `regular` y `fill`, a `src/ui/icon-data.ts` (confirmado en git). `@phosphor-icons/react` pasa a `devDependencies` | −60/−80 kB gzip; sin dependencia nueva; quien instala el paquete ya no trae Phosphor |
| D-R20 | Vocabulario de iconos = nombres kebab de Phosphor (`buildings`, `tree-structure`…) más los alias propios como sinónimos; lista única en `src/contracts/icons.ts` | B-1; los manifiestos de Theros traerán nombres de Phosphor |
| D-R21 | En la cuadrícula `Tab`/`Shift+Tab` salen de la rejilla (una parada, patrón APG); sólo al editar `Tab` confirma y avanza | B-4, WCAG 2.1.1/2.1.2/2.4.3. Sustituye la línea «Tab/Shift+Tab» de `PLAN.md` §4.9 |
| D-R22 | Nada de `:dir()` en el CSS: `[dir="rtl"]`/`:where([dir="rtl"])`, impuesto por stylelint | lightningcss lo compila a `:lang(ar,he,…)` y no funciona con `?dir=rtl` |
| D-R23 | Un único control de vista en la cabecera de entidad (`viewChoices` en contratos); la tabla del mapa es una alternativa accesible dentro del mapa, no una vista | M-6 |
| D-R24 | La hoja de MapLibre va en `dist/karpo-map.css` (`@karpo/react/map.css`), con `font: inherit`; `dist/karpo.css` ya no la trae | M-16, B-3 |
| D-R25 | El paquete no declara `engines` (es de navegador); el desarrollo declara `devEngines.runtime` `>=24.15` con `onFail: "warn"` | M-18: el aviso `EBADENGINE` viene de jsdom y stylelint |
| D-R26 | `zod/mini` en `config/schema.ts` y `detail/form-model.ts` (mismo paquete, misma licencia); se mantiene `@hookform/resolvers` | M-15 |
| D-R27 | En idiomas con `dir=rtl` toda interpolación de i18next se envuelve en FSI/PDI; los rangos usan `Intl.NumberFormat#formatRange` | M-12 |
| D-R28 | Las superposiciones de texto de la demo para las 8 entidades de Go se **generan** (`scripts/gen-demo-i18n.mjs`) con la convención de claves `entities.<id>.columns.<campo>.header`, `.sections.<id>.title`, `.fields.<campo>.label|hint`, `.options.<campo>.<valor>`, `.filters.<id>.label`, `.commands.<id>.label`; la tabla inglesa se revisa a mano | B-2 (c); prueba de concepto de TC-3 |
| D-R29 | Presupuesto de bytes: trozo inicial de la demo (entrada + sus importaciones estáticas, según `manifest.json` de Vite) ≤ 250 kB gzip; obligatorio desde R5 | M-15 |
| D-R30 | Un nivel del mapa con una sola delimitación se salta por omisión (Q-10); la miga lo refleja | M-4 |

Consultas nuevas para Javier (con la opción por defecto que se aplica mientras tanto):

| # | Pregunta | Por defecto |
|---|---|---|
| Q-6 (reescrita) | `lightningcss` (MPL-2.0) como transitiva de desarrollo de Vite | Se usa **por defecto mientras Javier no decida**; no se distribuye. Alternativa: `cssMinify: 'esbuild'` |
| Q-8 | Geometría real para la demo (M-5): Natural Earth (dominio público) convertida a PMTiles propio. Hace falta que Javier aporte el fichero o autorice la descarga; `world-atlas` (ISC) por npm sólo con su visto bueno | Geometría esquemática rotulada (Q-4) y origen configurable |
| Q-9 | `polylabel` (ISC) para colocar rótulos | No: cálculo propio del polo de inaccesibilidad (~40 líneas) |
| Q-10 | Nivel con un solo elemento: saltarlo o conservarlo con zoom limitado | Saltarlo (`map.skipSingle: true`), configurable por entidad |

---

## 3. Reglas de esta ronda

- **Olas.** Ola 0: R0 sola (andamiaje y contratos). Ola 1: R1 a R4 en paralelo en la misma
  carpeta. Ola 2: R5 sola (integración, pruebas transversales, capturas y cierre).
- **Propiedad de ficheros.** Cada fichero pertenece a una sola tarea (campo `files`). Ninguna tarea
  salvo R0 toca `package.json`, `package-lock.json`, `vite*.config.ts`, `vitest*.config.ts`,
  `tsconfig*.json` ni `src/contracts/`. `eslint.config.js`, `stylelint.config.js` y
  `build/lint-config.test.ts` son de R5 (las reglas nuevas entran cuando todo el código ya las
  cumple). Excepción heredada de la ronda 1: R0 crea como **esqueleto** `scripts/gen-icons.mjs` y
  `scripts/gen-demo-i18n.mjs` (con `--check` que pasa sin hacer nada) para que `npm run check` no
  falle entre olas; pasan a R1 y R2. R5, sola en su ola, puede hacer arreglos mínimos de integración
  en ficheros de otras tareas y los anota en `docs/notas/INTEGRACION.md`.
- **Textos.** Sólo por clave, cada espacio de nombres en `src/locales/{es,en}/<ns>.json` de una sola
  tarea: `ui`, `fields`, `help` → R1; `errors`, `auth` → R2; `tree`, `map` → R3; `core`, `grid`,
  `detail` → R4. Claves iguales en es y en (prueba existente).
- **CSS.** Dentro de `@layer karpo.components`, sin literales (stylelint), sin `:dir(`.
- **Notas.** Lo que haga falta de otra tarea o fuera de la carpeta se anota en
  `docs/notas/R2-<tarea>.md` (fichero propio); R5 lo consolida en `PETICIONES-*.md`.
- **Al terminar cada tarea:** `npm run typecheck`, `npm run lint`, `npm run test`,
  `npm run test:contract` en verde y sus e2e propias en verde (las que dependen de otra tarea de la
  misma ola se marcan en su criterio y las cierra R5). Sin commit (D-R17). Procesos parados; nada en
  el puerto 55432; nada descargado fuera de npm.
- **Pruebas que se invierten a propósito:** `src/grid/keyboard.test.ts` (Tab recorre celdas) y
  `e2e/rejilla.e2e.ts:195-199`; las e2e que buscan «Listado» en `organigrama.e2e.ts` y
  `mapa.e2e.ts`. Ninguna otra prueba existente se borra ni se debilita.

---

## 4. Tareas

### R0 · Andamiaje y contratos (ola 0)

No hay dependencias npm nuevas. Cambios:

1. `package.json`:
   - `@phosphor-icons/react` pasa a `devDependencies` (D-R19).
   - Fuera `engines`; `devEngines.runtime` `{ "name": "node", "version": ">=24.15", "onFail": "warn" }`.
   - `exports["./map.css"] = "./dist/karpo-map.css"`.
   - Órdenes: `icons:gen`, `i18n:demo`, `presupuesto`.
   - `check` añade `icons:gen -- --check`, `i18n:demo -- --check` y `presupuesto` tras `build`.
   - `package-lock.json` regenerado con `npm install --package-lock-only` y comprobado con `npm ci`.
2. `vite.lib.config.ts` (o una segunda configuración `vite.map-css.config.ts` invocada desde
   `build:lib`): emite `dist/karpo-map.css` desde `src/map/maplibre.css`.
3. `src/map/maplibre.css` (nuevo, de R0): `@import` de `maplibre-gl.css` en
   `@layer karpo.components.maplibre` y, en `@layer karpo.components`,
   `[data-kx-part="map"].maplibregl-map, .maplibregl-map { font: inherit; }` (B-3).
4. `vite.config.ts`: `build.manifest: true` en el modo demo (lo lee el presupuesto).
5. `scripts/presupuesto.mjs` (+ prueba): suma gzip del trozo de entrada y sus importaciones
   estáticas; también `dist/karpo-react.js`; límite y obligatoriedad desde
   `scripts/presupuesto.json` (si no existe: 250 kB, sólo aviso).
6. `scripts/pack-check.mjs` (+ prueba): exige `dist/karpo-map.css` con `.maplibregl-map` y
   `font:inherit`.
7. `scripts/licencias.mjs` (+ prueba) y `docs/LICENCIAS.md` regenerado: Q-6 «por defecto mientras
   Javier no decida»; nota de que los `targets` de lightningcss cambian el CSS publicado.
8. `src/contracts/`:
   - `icons.ts`: `ICON_NAMES` (`as const`, datos puros) con los alias actuales de `src/ui/icons.ts`,
     el nombre kebab de Phosphor de cada uno, los cinco del manifiesto (`buildings`,
     `tree-structure`, `globe-hemisphere-west`, `map-trifold`, `calendar-dots`) y los que piden las
     tareas (`monitor`, `palette`, `dots-three`, `plus`, `table`, `graph`, `list-bullets`,
     `arrows-in`, `arrows-out`); `IconName`, `isIconName()` y `DIRECTIONAL_ICONS`.
   - `views.ts`: `ViewChoice`, `viewChoices(config)` (Tabla, Árbol, Diagrama, Mapa a partir de
     `config.views`, con `view`, `layout` y `icon`), `ViewStatus { total?, matched?, loading? }` y
     `EntityViewProps.onStatus?(s)`.
   - `fields.ts`: `format(value, def, locale, resolve?)` y `parse(…, resolve?)` con
     `resolve?: (text: Text) => string` opcional (compatible hacia atrás).
   - `config.ts`: `skipSingle?: boolean` en la vista `map`.
   - Prueba de contrato de `viewChoices` e `isIconName`.
9. `e2e/lib/` (`demo.mjs`, `demo.d.mts`, `suite.ts`): `openDemoPage` admite `viewport`,
   `isMobile`, `hasTouch`; `signIn` funciona con el menú como cajón (`state: 'attached'`).
10. Esqueletos `scripts/gen-icons.mjs` y `scripts/gen-demo-i18n.mjs` (pasan a R1 y R2).

Criterios: ver el plan estructurado (§5).

### R1 · Cimientos: iconos, campos, i18n, temas y ayuda (ola 1)

- **B-1 y M-15.1.** `scripts/gen-icons.mjs` renderiza los `defs` de Phosphor a
  `src/ui/icon-data.ts` (sólo `regular` y `fill`), tipado `Record<IconName, IconData>`;
  `--check` falla si el fichero no está al día. `icons.ts` y `Icon.tsx` usan esos datos y no
  importan `@phosphor-icons/react` en ejecución. Nombre desconocido: círculo con
  `data-kx-icon="missing"` y `console.warn` una vez por nombre en desarrollo.
- **B-2 (b).** `useFieldText().resolve` delega en `useText()` (`config/text.ts`); códecs con
  `resolve` opcional (booleanos `trueLabel/falseLabel`, `select`, `multiselect`, `badge`);
  `findOption` acepta la etiqueta traducida (pegar «Filled»). `help/steps.ts` sin `.literal`.
- **Hallazgo `:dir()` (parte) y M-11.** `ui.css` con `[dir="rtl"]`.
- **B-5 (parte).** `kbd` oculto con `@media (hover: none), (pointer: coarse)`.
- **M-12.** Formateador en `src/i18n` que, con `dir=rtl`, envuelve cada interpolación en FSI/PDI;
  `formatNumberRange(locale, a, b)` con `Intl.NumberFormat#formatRange` exportado desde
  `src/i18n`; `FieldView` pinta el texto libre en `<bdi>`.
- **M-2 y M-14.** `kx-ext.css`: `--kx-data-seq-1` al 18-22 %, `--kx-data-missing` neutro fuera de
  la escala y nuevo `--kx-data-missing-hatch`; `src/theme/contrast.contract.test.ts` sobre los 4
  temas × modos: pares de texto ≥ 4,5:1, no texto ≥ 3:1, ΔL(`missing`, `seq-1`) ≥ 0,04 y escala
  monótona en el mismo sentido en ambos modos.
- **M-15.2 (parte).** `driver.js` y su hoja con `import()` dentro de `startTour`.
- E2E: `menu.e2e.ts` (nuevo: las 7 entradas sin `data-kx-icon="missing"`), `rtl.e2e.ts` (espejo con
  `dir=rtl`, ninguno con `ltr`; isolates en las 5 cadenas del jurado), `ayuda.e2e.ts`,
  `temas.e2e.ts`.

### R2 · Configuración, API y demo (ola 1)

- **B-1 (avisos).** Tras normalizar, un recorrido del `MenuNode[]` y de cada `EntityConfig` emite
  `ConfigWarning` `warn` con ruta (`menu[3].children[1].icon`) para todo icono fuera de
  `ICON_NAMES`. Prueba de contrato: manifiesto de la demo, sus 4 entidades y las 8 de Go sin avisos
  de icono.
- **B-2 (c).** `scripts/gen-demo-i18n.mjs` (D-R28) escribe las superposiciones en el manifiesto de
  la demo y los diccionarios `demo/config/i18n/{es,en}.json` desde `demo/config/i18n/en.go.json`
  (tabla inglesa revisada); `--check` falla si falta una clave en `en` o si lo generado no está al
  día. `merge.ts`: `value` en `IDENTITY_KEYS` (con prueba). Comprobar que `normalize.ts` lee todas las
  `*Key` usadas. Prueba unitaria: normalizar las 8 de Go con la demo en `lng=en` no deja ningún
  literal español en cabeceras, secciones, etiquetas, pistas, filtros, órdenes ni opciones.
- **Catálogos (M-8).** `src/api` envía `Accept-Language`; el MSW devuelve `typeName` y demás
  nombres de catálogo según el idioma.
- **M-8 `hint`.** El normalizador hereda `hint`, `placeholder`, `maxLength` y `help` del campo
  homónimo de `form` cuando el de `detail` no los trae (aviso `info`).
- **M-4.** Normaliza `skipSingle` (por omisión `true`, Q-10).
- **M-15.3.** `config/schema.ts` con `zod/mini`.

### R3 · Organigrama y mapa (ola 1)

- **B-2 (a).** `useText()` en `MapView`, `TreeView`, `TreeDiagram`, `TreeList`; leyenda sin
  sustantivo en minúsculas («Instalaciones por delimitación»); `plainFieldText` pasa el resolvedor
  a los códecs (búsqueda «Filled» en inglés).
- **B-3.** E2E: `font-family` de `.kx-map-label` igual a la de `body`. **M-16:** `map.css` sin el
  `@import`; `engine.ts` carga `maplibre.css` con `import()` junto a `maplibre-gl`.
- **M-1.** `FIT_MIN_ZOOM = 0.85` y centrado en la raíz; «Ajustar» alterna legible/todo; zoom
  semántico (`data-kx-zoom="far"` por debajo de 0,6: sólo título y contador, ≥ 11 px efectivos);
  contador `≥14` en `<bdi>` con `aria-label` «al menos 14 descendientes; hay ramas sin cargar».
- **M-2 (a).** Trama de «sin dato» en la leyenda (gradiente con tokens) y en el mapa
  (`fill-pattern` desde un `<canvas>` con colores de la sonda, regenerado al cambiar tema o modo).
- **M-3.** `measureText` con la fuente calculada (caché por texto y fuente), nombre corto
  (`shortName` o código) con el largo en `title` y en la tabla; polo de inaccesibilidad propio
  (Q-9); `fitBounds` con relleno por lados que suma la leyenda (`ResizeObserver`, lado lógico en
  RTL); geodatos con `shortName` (`gen-geo.mjs`).
- **M-4.** `nav.ts` salta los niveles de un solo elemento si `skipSingle`; migas coherentes.
- **M-6.** Fuera los `LayoutSwitch` de `TreeView` y `MapView`; la disposición sale de la URL que
  pone la cabecera; en el mapa, botón «Ver como tabla»/«Ver como mapa»; anclas de ayuda nuevas.
- **M-7.** Las vistas informan con `onStatus` (total y coincidencias).
- **M-10.** `padding-inline` con token y `overflow-wrap: anywhere` en los estados del mapa y en
  `search-status` del árbol.
- **M-12.** Rangos de la leyenda con `formatNumberRange`; nombres de nodos y delimitaciones en
  `<bdi>`/`dir="auto"`.
- **M-13.** `role="group"` + `aria-roledescription` + nombre en cada nodo; enlace «Ver como lista»;
  prueba propia de roles y nombres accesibles de todos los controles del diagrama.
- E2E: `mapa.e2e.ts` y `organigrama.e2e.ts` adaptadas al control único, con zoom legible, rótulos
  y relleno de leyenda.

### R4 · Marco, cuadrícula y detalle (ola 1)

- **B-4.** `keyboard.ts`: `Tab` fuera de `nextPosition`/`isNavigationKey`; `stepCell(pos, dir,
  dims)` sólo desde `afterCommit`; `GridView` no intercepta `Tab` en navegación; última celda
  editable: el foco vuelve a la celda; `MultiSelectEditor` sigue confirmando con `Tab`; atajos y
  textos (`grid.json`, ayuda de la rejilla) reescritos.
- **B-5.** `useCompact()` con `ResizeObserver` sobre `[data-kx-shell]` (≤ 40em): título primero
  (`min-inline-size: 6ch`, elipsis), menú, paleta, ayuda y un menú «Más» (`dots-three`) con tema,
  modo, idioma, atajos, cambiar clave y salir; sin `kbd` en compacto; icono `monitor` para «sistema»
  y `palette` para el tema; el detalle a página completa por debajo de 64em.
- **M-6 y M-7.** `EntityHeader` encima de los paneles: título, recuento plural (`onStatus`),
  control segmentado único (`viewChoices`, `role=radiogroup`) y «Nuevo» (`primary`, `plus`) si
  `config.form?.create`, `config.editable` y permiso de escritura; atajo `N`. `ViewSwitch` sale de
  `TopBar`. «Añadir fila» se queda en la rejilla como secundaria.
- **M-8.** La columna «Estado» no se corta con la de acciones fijada (relleno `--kx-trail`, ancho
  mínimo por contenido, sombra con scroll); `@container kx-sheet` a 28em para 2 columnas (3 desde
  44em); pie de acciones fijo; `hint` con `aria-describedby` (prueba de componente).
- **M-9.** Barra de lote flotante (`position: sticky`, `inline-size: fit-content`, radio y
  elevación del tema, punto de color «sucio»), deshacer/rehacer `secondary` en un `role=group`,
  relleno inferior del viewport igual a su alto.
- **Hallazgo `:dir()`.** `grid.css` y `shell.css` con `[dir="rtl"]`.
- **M-15.2/3/4.** `DetailView`, `CommandPalette` (con `cmdk` y `fuse.js`) y
  `react-resizable-panels` fuera del trozo inicial (`lazy`, precarga al pasar o enfocar);
  `form-model.ts` con `zod/mini`; `react-router` importado sin barril.
- E2E: `rejilla.e2e.ts` (Tab ×3 sale de `[role=grid]`, Shift+Tab vuelve, Tab en edición avanza),
  `detalle.e2e.ts`, `movil.e2e.ts` (nuevo, 360 y 390 px con `hasTouch`: `scrollWidth <=
  innerWidth`, `h1` con ancho y texto, sin `kbd` visible, «Más» abre y cierra) y
  `utilidades.e2e.ts`.

### R5 · Integración, pruebas transversales, capturas y cierre (ola 2)

- `e2e/ingles.e2e.ts` (B-2): con `lng=en`, ninguna pantalla de `SCREENS` muestra un literal español
  de las 8 configuraciones de Go (salvo los iguales en inglés); afirmaciones concretas de leyenda,
  insignias y cabeceras de `/product`. `i18n.e2e.ts` y `local.e2e.ts` al día.
- Reglas nuevas: stylelint `selector-pseudo-class-disallowed-list: ['dir']`; eslint que prohíbe
  `.literal` fuera de `src/config` y `src/fields/values.ts`; `build/css-dir.test.ts` compila
  `src/**/*.css` con los `targets` de Vite y falla si aparece `:lang(ar`.
- `scripts/presupuesto.json` con `{ "limitKb": 250, "enforce": true }` (D-R29).
- Capturas: variantes `ingles` (9 escenas) y `movil` (5 escenas: cuadrícula, detalle, organigrama
  listado, mapa mundo, ayuda) más las 27 de siempre = 41 PNG y su README.
- README (Node 24.15 para desarrollar y por qué; `@karpo/react/map.css`; columnas no virtualizadas,
  ≤ ~40 columnas), `PETICIONES-THEROS-FRONT.md` (TC-3 con la convención de D-R28, PT-5 con ΔL,
  nombres de Phosphor en `look.icons`, `:dir()` frente a lightningcss) y `PETICIONES-BACKEND.md`
  (catálogos traducibles, total de la lista, servicio de teselas propio), consolidando
  `docs/notas/R2-*.md`.
- `npm run check` y `npm run test:e2e` en verde; ningún proceso vivo.

---

## 5. Orden y dependencias

- R0 antes que todo: contratos (`ICON_NAMES`, `viewChoices`, `onStatus`, `resolve`, `skipSingle`),
  hoja del mapa, presupuesto y `openDemoPage` móvil.
- En la ola 1 no hay dependencias bloqueantes entre tareas: todas programan contra los contratos de
  R0. Dependencias suaves, que cierra R5: los avisos de icono de R2 se ven en verde cuando R1 tiene
  el registro (ambos leen `ICON_NAMES`); la e2e del menú (R1) necesita la barra lateral de R4 tal
  como está hoy; `rtl.e2e.ts` (R1) comprueba isolates en cadenas de R4 y R3; `mapa.e2e.ts` y
  `organigrama.e2e.ts` (R3) usan la cabecera de R4 para cambiar de disposición (pueden navegar por
  URL mientras tanto).
- R5 al final: reglas de lint nuevas, presupuesto obligatorio, e2e en inglés y capturas.
