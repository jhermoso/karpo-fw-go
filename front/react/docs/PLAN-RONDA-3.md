# Plan de la ronda 3 del front React (`@karpo/react`)

Fecha: 2026-11-27. Rama `claude/front-react-7b8564`, base `fe40aa0`. Papel: planificador, ronda 3.

Insumos (leídos enteros): `docs/jurado/ronda-2.md` (1 bloqueante, 14 mejoras, nota 8,2),
`docs/analisis/ronda-3.md` (causas medidas y enfoque de B-1 y M-1 a M-12), `docs/PLAN.md` y
`docs/PLAN-RONDA-2.md`, que siguen vigentes salvo lo que cambia §2 de este documento.

Objetivo: resolver **B-1** y **las 14 mejoras** del jurado sin romper lo que ya funciona (las 1417
pruebas unitarias y de componentes, las 154 de contrato y las 138 e2e siguen en verde, salvo las
que este plan cambia a propósito, que se citan en §3). No hay **ninguna dependencia nueva**: no hay
ola 0 y **nadie toca `package.json` ni `package-lock.json`**.

---

## 1. Cobertura: cada punto del jurado tiene dueño

La numeración del jurado (1 a 14) y la del análisis (M-1 a M-12) no coinciden; se citan las dos.

| Jurado | Análisis | Qué se hace | Tarea |
|---|---|---|---|
| B-1 avisos de terceros | B-1 | `dist/THIRD-PARTY-NOTICES.md` generado en el build desde los `LICENSE` de `node_modules` (Phosphor, MapLibre GL JS, xyflow); cabecera `/*! … */` en `karpo.css` y `karpo-map.css`; `pack:check` falla si falta el fichero o un origen, con detección automática (`@import` de paquetes en `src/**/*.css`, fuentes `node_modules/` en `dist/*.map`, generadores declarados); README lo enlaza | R3-1 |
| 1 y 11 título duplicado | M-1 | Un único `h1` por pantalla, pintado por el contenido; la barra superior pinta migas (sección › entidad); la ficha como página usa `h1`, en panel `h2`; prueba de componentes y e2e que cuentan los `h1` | R3-2 |
| 2 «sin dato» domina el mapa | M-2 | Token suave `--kx-data-missing-hatch-map` para el mapa; tesela de 12 px con raya de 1 px; sin datos en el nivel, sin capa de trama; la leyenda conserva la trama marcada; ΔL(missing, seq-1) intacta | R3-4 |
| 3 columna «Estado» cortada | M-3 | Ajuste de columnas al ancho: se encogen y, si no basta, se ocultan por `priority` (o de derecha a izquierda) sin tocar las preferencias; contador `+n` en el pie y nota en el menú de columnas; panel de detalle 60/40 por omisión | R3-3 (rejilla), R3-2 (`Panels.tsx`) |
| 4 espacio muerto en el detalle | M-4 | Tarjetas «Registro» (id con copiar, `version` y, si llegan, creado/modificado) y «Relaciones» (campos `lookup` con enlace; «Dentro de» en entidades con árbol); petición B-27 (sello de auditoría) | R3-2; petición R3-6 |
| 5 nivel región de un elemento | M-5 | Entrar por URL en un nivel saltable redirige con `replace` (sólo fuentes GeoJSON); escena de capturas `level=region&code=ES` | R3-4; escena R3-6 |
| 6 rótulos mezclados | M-6 | Criterio único por nivel: la variante más corta de las elegidas (`full > short > code`) para todos; atributo `data-kx-kind` | R3-4 |
| 7 pulido | M-7 | (1) vacío único «—»; (2) cifras con `tabular-nums` en la familia del cuerpo; (3) nodos del diagrama sin truncado con hueco libre, dos líneas; (4) contador del árbol junto a su etiqueta; (5) pie «50 de 120 filas» con relleno | R3-3 (1, 2, 5), R3-4 (2 en `MapTable`, 3, 4) |
| 8 recorridos cortos | M-8 | Pasos de configuración **más** anclas visibles no cubiertas; la barra de lote oculta se explica sobre su ancla de reserva con texto «aparece al editar»; ancla nueva `grid.undo`; cierre con icono SVG ≥ 28 px | R3-5 (motor), R3-3 (anclas y textos de la rejilla) |
| 9 casillas táctiles | M-9 | En `(pointer: coarse)` toda la celda de selección es el objetivo (≥ 24 px; casilla de `--th-control-height-small`); `accent-color` del tema | R3-3 |
| 10 RTL sin idioma real | M-10 | Idioma `ar` completo en `src/locales/ar` y en la configuración de la demo que sale en capturas; reserva `ar → en`; cifras latinas; capturas `rtl` con `lng=ar`; e2e propia | R3-6 |
| 12 presupuesto justo | M-11 | Iconos partidos en núcleo (estático) y resto (diferido); idiomas y espacios de nombres por demanda; acceso y cambio de contraseña diferidos; límite obligatorio 215 kB y objetivo 200 kB; `presupuesto --detalle` | R3-1 |
| 13 columnas no virtualizadas | — | Sin cambio: sigue anotado (≤ ~40 columnas, `PLAN.md` §4.9) | — |
| 14 consultas abiertas | M-12 | `docs/CONSULTAS-JAVIER.md`: Q-1 a Q-12 con recomendación, qué pasa si no responde y coste de cambiar; nuevas Q-11 (atribución de xyflow) y Q-12 (`amount` monoespaciado); enlazado desde el README | R3-1 |
| Peticiones y compatibilidad | — | `PETICIONES-THEROS-FRONT.md` (`amount` sin mono o `figure`, `priority` de columna, ayuda por anclas y `data-kx-help-fallback`, token `--kx-data-missing-hatch-map`), `PETICIONES-BACKEND.md` (B-27), `COMPATIBILIDAD-CONFIG.md` (`priority`, vacío normalizado, fusión de pasos de ayuda) | R3-6 (consolida las notas de cada tarea) |
| Capturas | — | Las 41 PNG regeneradas una sola vez al final, con la variante `rtl` en árabe | R3-6 |

---

## 2. Decisiones nuevas (técnicas y reversibles; también en `PLAN.md` §1)

| # | Decisión | Por qué |
|---|---|---|
| D-R31 | Los avisos de lo incrustado en `dist/` van en `dist/THIRD-PARTY-NOTICES.md`, generado por un plugin del build de biblioteca (`vite.lib.config.ts`) con el registro `scripts/avisos.mjs` y el texto de `node_modules/<paquete>/LICENSE*` (no se copia a mano). `files: ["dist"]` ya lo incluye: **no se toca `package.json`**. Se regenera a mano con `npm run licencias -- --avisos` | B-1; D-R2 (nadie toca `package.json` salvo el andamiaje) |
| D-R32 | Cada pantalla tiene **un solo `h1`** y lo pinta su contenido (cabecera de entidad, ficha como página, inicio, 403, 404, acceso). La barra superior pinta migas (`nav` «Ruta»: sección del menú › entidad) en texto secundario; en compacto, la miga es `aria-hidden` | Jurado 1 y 11; WCAG 1.3.1 y 2.4.6 |
| D-R33 | «Sin dato» en el mapa: relleno casi del lienzo, trama suave (`--kx-data-missing-hatch-map`, tesela 12 px, raya 1 px) y ninguna trama si el nivel no tiene ningún dato; la leyenda conserva la trama de contraste ≥ 3:1 | Jurado 2 |
| D-R34 | La rejilla **nunca** pinta una columna a medias: encoge (hasta 0,7 del ancho, con mínimo por tipo) y oculta por `priority` (extensión opcional de la columna, mayor = más importante) o de derecha a izquierda. La primera columna y las fijadas no se ocultan. Las ocultas por ajuste son estado derivado (no tocan `prefs.hidden`); `prefs.autoFit` (por omisión `true`) lo desactiva. Panel de detalle por omisión 60/40 con mínimo 40 % para la lista | Jurado 3 |
| D-R35 | Vacío único «—» (`EMPTY_MARK`): un `emptyValue` hecho sólo de guiones se normaliza al pintar; los demás textos se respetan. Las cifras usan `font-variant-numeric: tabular-nums` en la familia del cuerpo; `data-th-format="amount"` deja de usarse para números (Q-12) | Jurado 7 |
| D-R36 | Recorrido de ayuda = pasos de la configuración (en su orden) **seguidos** de las anclas visibles con texto que ningún paso cubre (en orden de documento). Un ancla oculta con `data-kx-help-fallback="<ns>.<nombre>"` se explica sobre el elemento de reserva con las claves `help.<nombre>.idle.title|body`. Sin interruptor de configuración (sencillez) | Jurado 8 |
| D-R37 | Presupuesto: iconos en `icon-data-core.ts` (estático, los del marco) y `icon-data-extra.ts` (importación dinámica, pedida al arrancar); `iconData(name)` es síncrono para el núcleo. Idiomas por demanda (`import.meta.glob` sin `eager`): al arrancar, sólo el idioma activo y los espacios del marco; el resto con su vista. Acceso y cambio de contraseña con `lazy()`. Límite obligatorio **215 kB** gzip y `target` informativo **200 kB** en `presupuesto.json`; si la medida queda ≤ 197 kB, el límite baja a 200 | Jurado 12 |
| D-R38 | La demo trae **árabe** (`ar`) como idioma RTL real, marcado como traducción de demostración **sin revisión nativa** (Q-5 sigue abierta). Reserva `ar → en`. Las cifras usan `numberingSystem: 'latn'` por omisión en todos los idiomas. Las capturas `rtl` usan `lng=ar`; `?dir=rtl` con inglés se sigue probando en e2e | Jurado 10; sustituye la opción por defecto de Q-5 |
| D-R39 | Mapa: entrar por URL en un nivel saltable (una sola delimitación con hijos) redirige al siguiente con `replace` (sólo GeoJSON; con PMTiles no se puede mirar el nivel sin abrirlo). Rótulos con una sola variante por nivel | Jurado 5 y 6 |
| D-R40 | El detalle añade las tarjetas «Registro» y «Relaciones», derivadas de la fila y de la configuración (sin código por pantalla); el pie sigue fijo | Jurado 4 |

Consultas nuevas o cambiadas para Javier (opción por defecto que se aplica mientras tanto):

| # | Pregunta | Por defecto |
|---|---|---|
| Q-5 (cambia) | ¿Se encarga una revisión nativa del árabe de la demo? | Árabe de demostración sin revisión (D-R38), rotulado así en el README y en `CONSULTAS-JAVIER.md` |
| Q-11 | Atribución de `@xyflow/react` (MIT permite ocultarla; el fabricante la pide por cortesía): (a) oculta, con su aviso en `THIRD-PARTY-NOTICES.md`; (b) visible y configurable; (c) suscripción | (a) |
| Q-12 | ¿Las cantidades de dinero (`money`) en monoespaciada? | No: `tabular-nums` en la familia del cuerpo para todas las cifras |

---

## 3. Reglas de esta ronda

- **Olas.** No hay ola 0 (ninguna dependencia nueva). **Ola 1:** R3-1 a R3-5 en paralelo en la
  misma carpeta. **Ola 2:** R3-6 sola (árabe, peticiones, capturas e integración).
- **Propiedad de ficheros.** Cada fichero pertenece a una sola tarea (campo `files`). Nadie toca
  `package.json`, `package-lock.json`, `tsconfig*.json`, `vitest*.config.ts`, `eslint.config.js`
  ni `stylelint.config.js`. `vite.lib.config.ts` y `vite.map-css.config.ts` son de R3-1;
  `vite.config.ts` no se toca. `e2e/lib/**` es de sólo lectura en la ola 1 (una ayuda nueva va
  dentro del fichero e2e de la tarea). R3-6, sola en su ola, puede hacer arreglos mínimos de
  integración en ficheros de otras tareas y los anota en `docs/notas/INTEGRACION.md`.
- **Textos.** Sólo por clave; cada espacio de nombres de `src/locales/{es,en}/<ns>.json` es de una
  tarea: `core`, `detail` → R3-2; `grid`, `fields` → R3-3; `map`, `tree` → R3-4; `help` → R3-5;
  `ui`, `auth`, `errors` → R3-1 (sólo si hace falta). `src/locales/ar/**` → R3-6 (ola 2, cuando las
  claves de es/en ya no cambian). Claves iguales en es y en (prueba existente).
- **Contratos.** `src/contracts/icons.ts` (`CORE_ICON_NAMES`) es de R3-1; `src/contracts/config.ts`
  (`priority` en la columna) es de R3-3. Las demás tareas no cambian contratos.
- **Interfaces entre tareas de la ola 1** (fijadas aquí; ninguna espera a otra):
  - `iconData('x')` y cualquier icono del marco siguen siendo síncronos (R3-1 garantiza que `x`,
    `caret-right`, `copy`, `link`, `arrow-square-out` y los ya usados por `shell`, `grid`, `detail`,
    `help` y `auth` están en `CORE_ICON_NAMES`; su prueba de contrato lo vigila).
  - Ancla de reserva: atributo `data-kx-help-fallback="<ns>.<nombre>"` en el elemento oculto; textos
    `help.<nombre>.idle.title|body` en el espacio `<ns>` del ancla oculta. R3-3 lo pone en la barra
    de lote (`grid.batch-bar` → `grid.toolbar`) y escribe los textos; R3-5 lo interpreta.
  - Ancla nueva `grid.undo` (par deshacer/rehacer), con `help.undo.title|body` en `grid.json`: R3-3.
  - Carga de espacios de nombres: R3-1 deja `ensureNamespaces(ns[])` en `src/i18n` y lo llama desde
    los cargadores diferidos de `src/shell/lazy.ts` y `src/shell/views.tsx`; ninguna otra tarea tiene
    que hacer nada para que sus textos estén cargados.
- **CSS.** Dentro de `@layer karpo.components`, sin literales (stylelint), sin `:dir(`; medidas de
  `--th-space-*`, `--th-control-*`, `--th-type-*`; `z-index` de `--kx-z-*`.
- **Notas.** Lo que haga falta de otra tarea, del backend o de Theros Front se anota en
  `docs/notas/R3-<tarea>.md` (fichero propio); R3-6 lo consolida en `PETICIONES-*.md` y
  `COMPATIBILIDAD-CONFIG.md`.
- **Al terminar cada tarea:** `npm run typecheck`, `npm run lint`, `npm run test` y
  `npm run test:contract` en verde, y sus e2e propias en verde con Edge (`channel: 'msedge'`). Sin
  commit (D-R17). Procesos parados (puertos libres, nunca 55432); nada descargado fuera de npm.
- **Pruebas que cambian a propósito** (ninguna otra se borra ni se debilita):
  `e2e/movil.e2e.ts:36-61` (el título ya no está en la barra: se mide el `h1` del contenido),
  `e2e/rtl.e2e.ts:60` (`[role="banner"] h1` pasa a `[data-kx-part="entity-title"]`), las pruebas de
  `shell.test.tsx`/`EntityHeader.test.ts` que buscan el `h1` de la barra o el `h2` de la cabecera,
  las de `style.test.ts`/`MapView.test.tsx` que esperan la capa de trama sin datos, las de
  `labels.test.ts` que esperan variantes mezcladas, las de `help.test.tsx`/`steps.test.ts` que
  esperan que la configuración sustituya a las anclas y la que espera `data-th-format="amount"` en
  los visores numéricos.

---

## 4. Tareas

### R3-1 · Paquete, avisos de terceros, presupuesto y consultas (ola 1)

B-1, jurado 12 (M-11) y jurado 14 (M-12).

- **Avisos (B-1).** `scripts/avisos.mjs`: registro `{ id, paquete, licencia, motivo, donde,
  generadoPor? }` con Phosphor (`scripts/gen-icons.mjs` → `src/ui/icon-data-*.ts`), MapLibre GL JS
  (`@import` de `src/map/maplibre.css`) y xyflow (`@import` de `src/tree/tree.css`); `render()`
  determinista (sin fechas) con nombre, versión, licencia, dónde está y texto íntegro de
  `node_modules/<paquete>/LICENSE*`. Plugin en `vite.lib.config.ts` que lo emite como
  `THIRD-PARTY-NOTICES.md` y antepone `/*! Incluye partes de … — ver THIRD-PARTY-NOTICES.md */` a
  `karpo.css`; lo mismo para `karpo-map.css` en `vite.map-css.config.ts` (comprobar si lightningcss
  conserva `/*!`; si no, se antepone en `generateBundle`). `licencias.mjs --avisos` lo regenera;
  `licencias --check` falla si un origen del registro no está en `package-lock.json`.
- **`pack:check`.** Exige `dist/THIRD-PARTY-NOTICES.md` en el contenido de `npm pack --dry-run`, que
  contenga cada origen y su `LICENSE` (espacios normalizados), y falla ante un `@import` de paquete en
  `src/**/*.css` o una fuente `node_modules/<paquete>/` en `dist/*.map` que no estén en el registro.
- **Iconos partidos.** `CORE_ICON_NAMES` en `src/contracts/icons.ts` (⊂ `ICON_NAMES`);
  `gen-icons.mjs` genera `src/ui/icon-data-core.ts` y `src/ui/icon-data-extra.ts` (borra
  `icon-data.ts`; `--check` vigila los dos). `iconData` síncrono para el núcleo; el resto, de un
  almacén que llena `import('./icon-data-extra')` pedido al arrancar. `Icon` usa
  `useSyncExternalStore`: sin dato pinta un `svg` vacío del mismo tamaño y se repinta al llegar.
  Prueba de contrato: los literales de icono de `src/{shell,ui,grid,detail,auth,help}/**/*.tsx` están
  en el núcleo.
- **Idiomas por demanda.** `import.meta.glob` sin `eager`; `ensureNamespaces`; al arrancar, el
  idioma activo con `core`, `ui`, `auth`, `errors` (y los de las vistas que el trozo inicial importa
  de forma estática); el resto lo cargan `lazy.ts`/`views.tsx` antes de montar la vista. Ninguna
  clave cruda en pantalla (prueba). `numberingSystem: 'latn'` por omisión en `formatters.ts`
  (D-R38). Las pruebas de componentes siguen con todos los recursos cargados (`src/test/setup.ts`).
- **Acceso diferido.** `LoginPage` y `ChangePasswordPage` con `lazy()` en `AppRouter.tsx`.
- **Presupuesto.** `presupuesto.json` `{ "limitKb": 215, "targetKb": 200, "enforce": true }` (200
  si la medida da ≤ 197); `presupuesto.mjs --detalle` imprime la atribución por paquete con el mapa de
  fuentes de `demo/dist`.
- **Consultas (M-12).** `docs/CONSULTAS-JAVIER.md` (Q-1 a Q-12: estado, recomendación, qué pasa si no
  responde, coste de cambiar después). README: apartado de licencias con enlace a
  `dist/THIRD-PARTY-NOTICES.md` y a `docs/CONSULTAS-JAVIER.md`, nota del árabe de demostración sin
  revisión (Q-5), presupuesto nuevo. `docs/LICENCIAS.md` regenerado.

### R3-2 · Encabezados, migas y detalle (ola 1)

Jurado 1 y 11 (M-1), jurado 4 (M-4) y el panel de jurado 3.

- `TopBar` sin `h1`: `nav[aria-label]` con `ol` de migas (grupo del menú con `flattenMenu` ›
  entidad; sin entidad, el nombre de la aplicación en `p`); separador `caret-right` con
  `data-kx-mirror`; texto secundario. `EntityHeader`: `h2` → `h1` con el recuento. `SheetHeader`: `h1`
  como página, `h2` en panel (mismo `id`). Compacto: miga `aria-hidden`, `h1` de la cabecera oculto
  sólo visualmente. `pages.tsx` y las páginas de acceso conservan su `h1`.
- `Panels.tsx`: lista 60 % / detalle 40 %, mínimo de la lista 40 %.
- `SheetMeta.tsx` («Registro»: id corto con copiar, `version`; `createdAt|By`, `updatedAt|By` sólo si
  vienen) y `SheetRelations.tsx` (enlaces de los `lookup` con valor, sólo si `canReadEntity`; «Dentro
  de» y enlace a la vista de árbol centrada en el registro si la entidad tiene `parentField`; el
  recuento de hijos sólo si la API lo da). Sin importar de `src/tree`. Pie sin cambios.
- Textos en `core.json` y `detail.json` (es, en). Nota de B-27 en `docs/notas/R3-R2.md`.

### R3-3 · Rejilla, campos y pulido de la tabla (ola 1)

Jurado 3 (M-3), 7.1, 7.2, 7.5, 9 y las anclas de la rejilla de jurado 8.

- Función pura `fitColumns(columnas, anchoDisponible, opciones)` en `src/grid/fit.ts` y hook
  `useFitColumns` (`ResizeObserver`); el filtrado se aplica en `useGridModel` para que `aria-colcount`
  y las flechas no alcancen una columna oculta. `ColumnsMenu` marca «oculta por falta de espacio»;
  contador `+n` (`aria-live="polite"`) en el pie, que comparte barra con «50 de 120 filas» (relleno
  `--th-space-2`/`--th-space-margin`, borde superior sutil, `tabular-nums`). `prefs.autoFit`.
- `priority?: number` en `ColumnDef` (`src/contracts/config.ts`), leído en `normalize.ts` y aceptado
  por `schema.ts`; nota en `docs/notas/R3-R3.md` para `COMPATIBILIDAD-CONFIG.md` y Theros Front.
- `EMPTY_MARK` y `emptyText(emptyValue)` en `src/fields/values.ts`; `Empty` (`viewers.tsx`) y
  `SheetField.tsx` con el mismo marcado (`aria-hidden` + `sr-only` «Sin valor»).
- Números y decimales sin `data-th-format="amount"`: regla propia en `fields.css` y `grid.css` con
  `tabular-nums`, `text-align: end`, `white-space: nowrap`.
- Casilla de selección: `--th-control-icon-size` en escritorio; en `(pointer: coarse)` la celda
  `__select` entera alterna la selección (sin doble disparo) y la casilla mide
  `--th-control-height-small`; `accent-color` del tema.
- Ayuda: `data-kx-help="grid.undo"` en el par deshacer/rehacer, `data-kx-help-fallback="grid.toolbar"`
  en la barra de lote (`Bars.tsx`) y textos `help.undo.*` y `help.batch-bar.idle.*` en `grid.json`.

### R3-4 · Mapa y organigrama (ola 1)

Jurado 2 (M-2), 5 (M-5), 6 (M-6), 7.2 en `MapTable`, 7.3 y 7.4.

- M-2: token `--kx-data-missing-hatch-map` en `kx-ext.css` (mezcla al 35 % entre la trama y el
  relleno); `pattern.ts` con tesela de 12 px y raya de 1 px; `buildMapStyle` sin capa
  `kx-missing` cuando no hay ningún dato; par nuevo en `contrast.contract.test.ts` (≥ 1,3:1, en los 4
  temas × claro y oscuro) sin tocar ΔL(missing, seq-1) ≥ 0,04 ni el 3:1 de la leyenda.
- M-5: función pura `redirectTarget(view, shown, nav)` y efecto en `MapView` con
  `navigate(…, { replace: true })`; el estado de carga se mantiene durante la redirección; sólo
  GeoJSON (comentario con el motivo). Si `useEntityNavigate` no admite `replace`, se resuelve dentro
  de `src/map` con `useNavigate` de `react-router`.
- M-6: `commonKind(fits)` en `labels.ts`; dos pasadas en `MapCanvas`; `data-kx-kind` en cada rótulo.
- `MapTable` con la regla de cifras de D-R35 (sin `amount`).
- Árbol: reserva lateral de la tarjeta sólo con hijos; rótulo y subtítulo a dos líneas
  (`line-clamp`, con `title`); altura del nodo en `layout.ts` si hace falta (con el espaciado del
  tema). Contador del árbol en lista justo detrás de la etiqueta (`flex: none`), sin `auto`.
- Textos en `map.json` y `tree.json` si hacen falta.

### R3-5 · Ayuda guiada completa (ola 1)

Jurado 8 (M-8), motor.

- `mergeSteps(configSteps, anchorSteps)` en `steps.ts`: configuración en su orden y después las
  anclas visibles con texto no cubiertas (se compara por elemento y por nombre de ancla), en orden de
  documento; sin duplicados. `HelpProvider.start` la usa siempre (D-R36).
- Ancla oculta con `data-kx-help-fallback`: el paso se resalta sobre el elemento de reserva con
  `help.<nombre>.idle.title|body`; si el ancla está visible, texto normal. Sin texto `idle`, se omite
  como hoy.
- Cierre del popover: el «×» de texto se sustituye en `decoratePopover` (`tour.ts`) por el SVG del
  icono `x` (`iconData('x')`, síncrono), tamaño `--th-control-icon-size`, botón de
  `--th-control-height-regular` con `inset` `--th-space-2`; `aria-label` se conserva.
- Textos nuevos de `help.json` (es, en), si los hay.

### R3-6 · Árabe, peticiones, capturas e integración (ola 2)

Jurado 10 (M-10), consolidación y cierre.

- `src/locales/ar/*.json` (los 10 espacios, paridad de claves y marcadores con `es`); en
  `gen-demo-i18n.mjs`, tabla `demo/config/i18n/ar.go.json` → `ar.json` para las entidades de las
  capturas, con `--check`; reserva `ar → en`; `ar` en `locales` del manifiesto de la demo; nombre
  propio «العربية» en el selector (`locale-name.ts`, si hace falta).
- `e2e/rtl-ar.e2e.ts`: `lang=ar`, `dir=rtl`, espejo de menú, migas, chevrons, árbol, diagrama y
  flechas de la rejilla, y las cinco cadenas del jurado (recuento, «Paso N de M», campos marcados,
  límites del mundo, filas) leídas en orden correcto con sus aislamientos.
- `scripts/capturas.mjs`: variante `rtl` con `lng=ar`; escena `mapa-region` con
  `level=region&code=ES`. Se regeneran las 41 PNG y `docs/capturas/README.md`.
- Consolida `docs/notas/R3-*.md` en `PETICIONES-THEROS-FRONT.md`, `PETICIONES-BACKEND.md` (B-27) y
  `COMPATIBILIDAD-CONFIG.md` (`priority`, vacío normalizado, fusión de pasos y
  `data-kx-help-fallback`).
- Integración: `npm run check` y `npm run test:e2e` completos; arreglos mínimos anotados en
  `docs/notas/INTEGRACION.md`.

---

## 5. Orden y dependencias

- **Ola 1** sin dependencias bloqueantes: todas programan contra las interfaces de §3. Dependencias
  suaves que cierra R3-6: el recorrido de `/product` con ≥ 7 pasos (e2e de R3-5) necesita las anclas
  de R3-3 (`grid.undo`, `data-kx-help-fallback`); el `h1` único de R3-2 cambia selectores que usan
  otras e2e (`mapa`, `organigrama`, `ayuda` sólo usan la cabecera por rol de botón, no por `h1`); el
  presupuesto de R3-1 se mide con el código de toda la ola.
- **Ola 2** (R3-6) cuando ya no cambian claves de es/en: el árabe exige paridad completa. R3-1 deja
  los idiomas por demanda, de modo que `ar` no entra en el trozo inicial.
- Comprobación final de la ronda: `npm run check` (tipos, lint, pruebas, contrato, licencias,
  iconos, i18n de la demo, build, presupuesto ≤ 215 kB y `pack:check` con avisos) y
  `npm run test:e2e` con Edge; después el jurado recorre las capturas.
