# 03. El contrato de temas de Theros Front, y lo que falta para un front de gestión

Análisis del carril «front React» de Karpo Go, 2026-11-27. Papel: analista. Sólo lectura sobre
`C:\Git\Paranoia\Theros.Front` y `C:\Git\Paranoia\Theros`; nada se ha cambiado allí.

## 0. En una pantalla

1. **El contrato es pequeño y está bien definido.** Tres cosas: el diccionario de conceptos (v3, 78
   conceptos `look`, 18 `design`, 31 `domain`), una gramática de ganchos (`data-th-*`, ARIA y `role`)
   y una regla de abstinencia (el front no lleva colores, medidas ni fuentes). El tema se enlaza con
   **un solo `index.css`** que importa cinco capas (`th.tokens, th.modes, th.base, th.domain,
   th.a11y`) y declara **167 variables `--th-*`** en `:root`.
2. **Para nuestro front sirve la mitad del camino.** El tema viste bien lo que un HTML semántico de
   gestión tiene (tablas, fichas, campos, acciones, avisos, menú, diálogos nativos) y trae modo
   claro/oscuro, alto contraste, colores forzados, RTL por propiedades lógicas y escrituras. No
   viste, y no tiene variables para, lo que hace excelente a un SaaS de gestión: celdas activas y en
   edición, estados de guardado y conflicto, pestañas, paginación, chips, progreso y esqueletos,
   avisos emergentes, árboles, diagramas, mapas, gráficos, apilado (`z-index`), anillo de foco
   completo, curvas de movimiento, pesos de letra y paleta de series.
3. **Hay siete trampas que cambian el diseño de nuestro front** (sección 7): las variables de color
   son `light-dark(...)` sin resolver (MapLibre y driver.js necesitan colores literales); sólo
   `modern-flat` tiene oscuro; `glossy-2003` pinta los iconos rellenos y los otros dos de contorno;
   la densidad es de compilación (no hay modo `compact`); el foco depende de una política que no
   viaja en una variable; las rampas de paleta (`palette.accent.*`) **no se exportan**; y el
   contrato prohíbe `style=` justo donde una rejilla virtualizada lo necesita.
4. **Decisión de arquitectura propuesta.** El front consume `--th-*` directamente (sin capa de alias
   propia), añade **una sola capa `karpo.ext`** con lo que el contrato no cubre, derivada de `--th-*`
   con `color-mix()`/`calc()` y con la forma de variable que Theros podría adoptar después
   (`--kx-*`), y lleva una **prueba de contrato** local (ganchos válidos, hash del diccionario, ni
   un literal de color/medida/fuente fuera de un fichero marcado). Las peticiones a Theros Front
   (sección 9) se numeran PT-1 a PT-14 para pasarlas a `PETICIONES-THEROS-FRONT.md`.

## 1. Qué se ha leído y de qué versión

| Fuente | Versión |
|---|---|
| `C:\Git\Paranoia\Theros.Front` rama `main`, `0794695a` (T4-4) | Diccionario **v3**, `sha256:2276b82045ae16302c9e07c65a1a4d71dddd3695b4fe490c8f2d4205e65eaeee` |
| La misma, rama `carril/margenes` (`a73d173`, T5-4), aún sin integrar | Cambia `base.css` y `domain.css` (sección 8.1); no cambia el hash del diccionario |
| `docs/Contrato.md`, `docs/Interfaz.md`, `docs/Theros_Front_Propuesta.md`, `docs/Plan-F2.md`, `Plan-T2.md`, `Plan-T3.md`, `Plan-T5.md` | Texto de los contratos y de lo que cada fase dejó fuera |
| `concepts/theros-front-v3.yml` (840 líneas), `styles/*.style.yml` | Las ranuras exactas de cada concepto |
| Plan de Theros `docs/plan/Theros_Plan_2026-10-23.md`, §8.1 y §8.4 (`C:\Git\Paranoia\Theros`, `40315425`) | Reparto de papeles y la entrada del contrato como requisito del pack de front |

**Cómo se ha medido.** Se ha ejecutado `go run ./cmd/theros-front build` (sólo escribe en una carpeta
temporal) para `modern-flat`, `classic-1995`, `glossy-2003` y los temas de prueba `souq` (árabe,
AAA) y `bolsa` (chino, el rojo sube), y se han leído `tokens.css`, `modes.css`, `base.css`,
`domain.css`, `a11y.css`, `manifest.json`, `theme.resolved.yml` y la exportación DTCG. El
comportamiento de `light-dark()` se ha comprobado en el Edge 158 instalado (sección 7.1). No se ha
descargado nada.

**El plan de Theros §8.4** («Lo que todavía no se modela») sitúa a Theros Front como el modelador de
temas y dice que **el front de cada tecnología lo genera Theros Back**, que aplica los estilos «a
través del diccionario». Dos consecuencias para nosotros: (a) nuestro front React no lo genera
Theros Back (es un runtime por configuración), pero **debe hablar la misma gramática** para que
cualquier tema de Theros lo vista; (b) el diccionario es del repositorio de Theros Front, y lo que
le falte se pide a su orquestación, no se arregla aquí.

## 2. El contrato

### 2.1 Las tres partes

1. **El diccionario** (`concepts/theros-front-v3.yml`). Cada concepto lleva `label` (nombre corto en
   español), `description`, `guides` (guías de estilo de las que sale) y `slots` con tipo
   (`color`, `length`, `number`, `family`, `shadow`, `duration` o una lista cerrada `a|b|c`). Lo
   leen el tema (YAML) y el generador. Un nombre no cambia nunca; v1, v2 y v3 sólo añadieron.
2. **La gramática de ganchos.** Atributos `data-th-*` para lo que ARIA no nombra y ARIA/`role` para lo
   demás (tabla 2.3).
3. **La abstinencia.** Lo generado no lleva `style=`, ni clases con nombre de aspecto, ni hojas que
   pinten lo que el diccionario cubre. El CSS propio del front va **fuera de las capas del tema o en
   una posterior**, y no pinta lo que el diccionario cubre. Es el contrato que Theros comprueba en su
   pack de front (hash, ganchos, abstinencia).

### 2.2 Tipos de concepto

| Capa | Concepto | Cuántos | Qué produce |
|---|---|---|---|
| `look` | Estilo (ranuras con tipo): `surface`, `text`, `edge`, `state`, `control`, `semantic`, `type`, `space`, `depth`, `motion`, `collection`, `nav` | 78 | Variables `--th-*` y reglas |
| `design` | Diseño, marketing, cultura y experiencia: paleta, modo, contraste, densidad, dirección, escritura, cifras, idioma, subidas, tono, época, público, objetivo, movimiento, logotipo | 18 | **Nada en CSS**: alimentan (`feeds`) a los `look`. Sólo quedan anotados en `manifest.json` y `theme.resolved.yml` |
| `domain` | Cómo se ve el dominio: colección, registro y sus estados, ficha, campo y sus estados, acciones, aviso, menú, marca, ayuda, barra | 31 | Reglas de `domain.css` sobre los ganchos |

### 2.3 Atributos (valores cerrados; un valor fuera de la lista no lo viste ningún tema)

| Atributo | Dónde | Valores |
|---|---|---|
| `data-th` | Elemento del dominio | `collection`, `collection-empty`, `record`, `sheet`, `section`, `field`, `field-error`, `reference`, `badge`, `action`, `feedback`, `nav`, `nav-item`, `brand`, `hint`, `toolbar` |
| `data-th-kind` | Acción (sin él, `secondary`) | `primary`, `secondary`, `destructive`, `quiet` |
| `data-th-tone` | Badge, aviso, error de campo | `error`, `warning`, `success`, `info`, `neutral` |
| `data-th-state` | Registro (lista separada por espacios) | `inactive`, `new`, `dirty` |
| `data-th-format` | Valor en ficha o celda | `code`, `amount` |
| `data-th-trend` | Valor que sube o baja | `up`, `down`, `flat` |
| `data-th-layer` | Superficie anidada | `1`, `2`, `3` |
| `data-th-elevation` | Lo que flota | `raised`, `overlay`, `modal` |
| `data-th-required`, `data-th-readonly` | Campo | presencia |
| `data-th-reason` | Acción no disponible | texto libre (el tema lo enseña con `::after`, sin leerlo: el motivo accesible va en `aria-describedby`) |
| `data-th-drop` | Destino de arrastre | `over` |
| `data-th-icon` | `svg` de icono | nombre libre |
| `data-th-mode` | Raíz o contenedor (lista) | `light`, `dark`, `high-contrast` y los modos que defina el tema |
| `dir` | Raíz o contenedor | `ltr`, `rtl` |
| `role` | Donde el HTML no da el rol; **siempre en la cabecera de columna** | `grid`, `table`, `row`, `columnheader`, `gridcell`, `cell`, `status`, `alert`, `dialog`, `tooltip`, `menu`, `menuitem`, `listbox`, `option`, `banner`, `navigation`, `search` |
| ARIA | `aria-selected`, `aria-current` (`true` o `page`), `aria-invalid`, `aria-disabled`, `aria-busy`, `aria-checked` (`mixed`), `aria-pressed`, `aria-sort`, y `readonly` | los de la tabla de §3 de `Contrato.md` |

Reglas de uso que se olvidan fácilmente (`Contrato.md` §4 y §7): la selección (`aria-selected`) sólo
se pinta en una colección `role="grid"` que pueda recibir el foco, y sin el foco dentro se pinta
**inactiva**; una acción no disponible es `button type="button" aria-disabled="true"` (nunca `submit`
ni `a href`) con `data-th-reason` y `aria-describedby`; una barra de búsqueda es `form`/`div
data-th="toolbar"` (con `role="search"` si busca), **nunca `role="toolbar"`**; `data-th-mode` es
sólo del tema (un front no lo usa para otra cosa); la acción principal es el primer botón de envío y
las demás llevan `type="button"` (si no, el pulso de `glossy-2003` se va al botón equivocado).

## 3. Cómo se aplica

- **Enlace.** `<link rel="stylesheet" href="theme/index.css">` y nada más. `index.css` es:
  `@layer th.tokens, th.modes, th.base, th.domain, th.a11y;` más cinco `@import`. Los **cinco ficheros
  se piden al servidor en cascada**: el despliegue debe servir el tema como estáticos (origen
  configurable, soberanía: el CSS de Theros no pide nada fuera de su carpeta; ni fuentes ni imágenes).
- **Once ficheros por tema** (`theros-front build`): `index.css`, `tokens.css`, `modes.css`,
  `base.css`, `domain.css`, `a11y.css`, `theme.resolved.yml`, `manifest.json`, `gallery.html`,
  `theme.tokens.json` y `theme.resolver.json` (DTCG 2025.10). **El front sólo necesita los seis CSS y
  `manifest.json`.** `manifest.json` (173 KB en `modern-flat`) trae: versión y hash del diccionario,
  nombre y cadena de herencia, hash de lo resuelto, `culture` (`locale`, `direction`, `script`,
  `numerals`, `trend`), los 17 conceptos `design` con su valor y de dónde se derivó, los modos
  (`dark`, `high-contrast`) con el número de cambios, y el **informe de contraste de 352 pares**
  (ratio medido, objetivo, `wcag`). Es la fuente fiable de «qué soporta este tema» (sección 7.2).
- **Capas.** Todo el tema está en `@layer`. Una regla nuestra **sin capa** gana a cualquier capa
  del tema, y una en una capa declarada después también. Por eso nuestro CSS va en `@layer
  karpo.ext, karpo.components` declaradas **después** de las del tema (o sin capa), y no al revés.
  Excepción: `[hidden]` se oculta desde `th.a11y`; una regla nuestra con `display` en un elemento
  que pueda llevar `hidden` debe volver a ocultarlo con un selector de peso igual o mayor.
- **Modo.** Con `design.mode: system` (sólo `modern-flat` y sus derivados) `:root` lleva `color-scheme:
  light dark` y cada color de modo es `light-dark(claro, oscuro)`. `data-th-mode="dark"` o `"light"`
  en la raíz o en un contenedor lo fuerza (`color-scheme` en la capa `th.modes`). `high-contrast`
  es un modo con sus propios valores, que se combina (`data-th-mode="dark high-contrast"`) y que
  también se activa solo con `prefers-contrast: more` (capa `th.a11y`). `forced-colors: active`
  reasigna **todas** las variables a colores del sistema (`Canvas`, `Highlight`, `ButtonFace`...) y
  quita las sombras; `forced-color-adjust: none` sólo en lo que se pinta con `Highlight`.
- **Cambio en caliente.** Cambiar el tema es cambiar el `href` de la hoja (los cinco `@import` se
  resuelven relativos al nuevo `index.css`). Cambiar el modo es cambiar un atributo. **Dos temas a la
  vez en una página no**: cada tema declara sus variables en `:root`; una vista previa de otro tema
  dentro de la página sólo cabe en un `iframe`.
- **Raíz.** `:root, [data-th-mode]` pintan `background-color`/`color` del lienzo y fijan la letra
  (`font-family`, `font-size`, `line-height` de `type.ui`), `accent-color` (`toggle-on-bg`) y el color
  de barras de desplazamiento. Un contenedor con `data-th-mode` (por ejemplo para una vista previa
  oscura) **también se pinta de lienzo**.
- **`dir` y `lang` no los pone el tema.** `manifest.json` los anota «sólo como información»; los pone
  quien genera la página (aquí, nuestro front, desde el idioma activo, no desde el tema).

## 4. Las variables CSS: lista exacta

### 4.1 Regla de nombres

`--th-<concepto sin el prefijo `look.`, con puntos→guiones>[-<ranura>]`. La ranura `value` no
aparece (`look.text.link` → `--th-text-link`, `look.edge.width` → `--th-edge-width`). Ejemplos:
`look.control.button-default.bg` → `--th-control-button-default-bg`; `look.semantic.up.strong` →
`--th-semantic-up-strong`. Hay **167** variables en `:root` y son **las mismas 167 en todo tema** (lo
exige la completitud): un front puede dar por seguras todas las de esta lista. Las que cambian por
modo van con `light-dark()` (118 en `modern-flat`, 0 en `classic-1995` y `glossy-2003`, que son
sólo claros); `high-contrast` reasigna 21 en `modern-flat`.

Escalas derivadas: `--th-space-1` a `--th-space-8` (múltiplos de `space.unit`), `--th-type-1` a
`--th-type-6` (`type.ui.size` por `type.scale` elevado al paso; el paso 1 es el tamaño base).

### 4.2 Por familia (resumen; los valores de los tres estilos de serie están en el anexo A)

| Familia | Variables | Notas para el front |
|---|---|---|
| `surface` (21) | `canvas`, `content`, `header`, `header-inactive`, `sidebar`, `popover` (`bg`, `fg`, `border`), `dialog`, `tooltip` (`bg`, `fg`), `layer-level1..3`, `scrim` (con alfa) | La ficha usa `level1`; sus secciones, `level2`; la sección dentro de otra, `level3`. En oscuro cada nivel es un paso más claro. No hay `surface.raised` ni `surface.input`: los campos usan `control.field.*` |
| `text` (11) | `primary`, `secondary`, `placeholder`, `disabled-color`, `heading-color`, `heading-weight`, `link`, `link-visited`, `inverse`, `selection-bg/fg` | El tema garantiza el contraste de `primary` y `secondary` sobre `content`, `canvas`, `layer-level3`, `sidebar` y la cebra (por `fit`) |
| `edge` (9) | `subtle`, `strong`, `width`, `rounding-small/medium/large`, `relief-light`, `relief-shadow-mid/dark` | `rounding-large` es 9999px en `modern-flat` (cápsula), 999px en `glossy`, 0 en `classic`. Las `relief-*` sólo importan a los estilos con relieve (`classic-1995`, `glossy-2003`) |
| `state` (16) | `hover`, `pressed`, `selected`, `selected-inactive` (`bg`/`fg`), `focus-color/width/offset`, `disabled-color/opacity`, `drop-target-bg/fg/border` | Faltan `focus-style`, `focus-policy`, `busy` (sección 6) |
| `control` (37) | `button`, `button-default`, `button-destructive`, `button-quiet` (`bg/fg/border`), `toggle-on`, `toggle-mixed`, `field`, `field-focused`, `field-invalid`, `field-readonly`, `height-small/regular/large`, `padding`, `icon-color/size`, `scrollbar-thumb/track/thickness` | `control-padding` es el relleno **horizontal** de botones y campos y también de las celdas de la lista |
| `semantic` (29) | `error`, `warning`, `success`, `info`, `neutral`, `up`, `down` (`bg`, `fg`, `border`, `strong`) y `required-color` | `strong` es el color «de texto o de línea» (cumple 4,5:1 o 3:1 según uso); `bg`/`border` son de relleno. `up`/`down` cambian con la región (en `bolsa`, el rojo sube). Nunca usar `success`/`error` para subidas y bajadas |
| `type` (12) | `ui-family`, `ui-size`, `ui-line-height`, `mono-family`, `small`, `scale`, `1..6` | La familia lleva ya la escritura (árabe, hebreo, han, japonés, coreano, devanagari) y el interlineado (1,6 en `souq` y `bolsa`) |
| `space` (13) | `unit`, `1..8`, `control-gap`, `group-gap`, `margin`, `row` | `row` es el `padding-block` de cada celda de un registro, **no** la altura de fila |
| `depth` (3) | `levels-raised`, `levels-overlay`, `levels-modal` (sombras completas) | `none` en `classic-1995` (que usa relieve) y en colores forzados |
| `motion` (3) | `duration-short/medium/long` | `0ms` en `classic-1995`; la curva **no** es una variable |
| `collection` (8) | `grid-color`, `header-bg/fg/border/weight`, `sort-bg/fg`, `zebra-color` | `zebra` y `grid` tienen además una política (`none/subtle`, `none/rows/all`) que decide si hay regla, no una variable |
| `nav` (5) | `item-bg/fg`, `item-current-bg/fg/border` | El marcador (`fill|bar|underline`) decide la regla; la barra del actual es `border-inline-start: 3 × edge-width` |

### 4.3 Lo que el diccionario define y **no** llega al CSS como variable (20 ranuras)

Son elecciones que el generador convierte en reglas fijas, así que el front **no puede leerlas** con
`var()` y, si las necesita, tiene que **deducirlas del `manifest.json` o medir el DOM**:

`surface.canvas.texture` (rayas de `glossy-2003`), `surface.layer.policy`, `text.disabled.style`,
`edge.relief` (plano/relieve/brillo), `edge.separator`, `state.focus.style`, `state.focus-policy`,
`state.busy`, `control.button-default.emphasis` (color/marco/pulso), `control.scrollbar.size` y
`style`, `control.icon.style`, `semantic.required.marker`, `type.emphasis`, `depth.policy`,
`motion.easing`, `collection.zebra.policy`, `collection.grid.policy`, `collection.sort.marker`,
`nav.item-current.marker`.

Además de éstas, el CSS generado tiene valores fijos que no son variables: la curva
`cubic-bezier(0.2, 0, 0, 1)` (o `linear` en `classic-1995`) de las transiciones, `font-weight: 500`
o `700` de `strong`/`b`, `scrollbar-width: thin|auto`, `z-index: 1` del motivo de una acción no
disponible, el grosor `3 × edge-width` de las marcas y los símbolos `↑ ↓ ▲ ▼ =`.

## 5. Qué viste el tema y con qué selectores

Todo es **por atributo**, en `th.domain` y `th.base`. Resumen de lo que ya tenemos gratis si emitimos
los ganchos:

| Nuestro elemento | Gancho | El tema da |
|---|---|---|
| Rejilla/listado | `table|div[data-th=collection][role=grid]`, `th|div[role=columnheader][aria-sort]`, `[data-th=record]` | Fondo, cabecera, orden (flecha), hover, cebra y rejilla según política, fila elegida (con doble línea para no depender del color), elegida **inactiva** sin foco, abierta (`aria-current`), `inactive`, `new`, `dirty` (marca al final de la fila), `aria-busy` (atenuada) |
| Lista vacía | `[data-th=collection-empty]` | Texto secundario centrado |
| Ficha | `[data-th=sheet]` y `[data-th=section]`, `[data-th=field]` con `label`, `[data-th=hint]`, `[data-th=field-error]` | Capas 1/2/3, rejilla con hueco, asterisco de obligatorio, error con `aria-invalid` |
| Campos | `input`, `select`, `textarea`, `[readonly]`, `[aria-invalid]` | Alto, relleno, borde, foco, sólo lectura, inválido |
| Interruptores | `[aria-checked]`, `[aria-pressed]` | Color de `toggle-on`/`mixed` |
| Acciones | `[data-th=action][data-th-kind]` y `[aria-disabled]` + `data-th-reason` | Cuatro jerarquías, hover/pulsado, motivo en un tooltip de CSS |
| Avisos | `[data-th=feedback][data-th-tone]` con `role=status|alert` | Banda con marca al inicio |
| Insignias | `[data-th=badge][data-th-tone]` | Cápsula tonal |
| Importes, códigos, subidas | `data-th-format`, `data-th-trend` | Mono con `tabular-nums` y a la derecha; flecha y color regional |
| Menú | `nav[data-th=nav]` > `a[data-th=nav-item][aria-current=page]` | Columna, marca del actual (barra) |
| Cabecera | `[role=banner]`, `[data-th=brand]` | Franja, foco con `currentColor` |
| Barra | `[data-th=toolbar]` (+ `role=search`) | Fila con hueco y separador |
| Flotantes | `dialog`, `[role=dialog]`, `[role=tooltip]`, `[role=menu]`, `[role=listbox]`, `[data-th-elevation]` | Fondo, sombra, radio, relleno; `::backdrop` con el velo |
| Otros | `hr`, `h1..h6`, `a`, `::selection`, `::placeholder`, `:focus-visible`, `svg[data-th-icon]`, `[data-th-drop=over]` | Ver `base.css` |

**Sin estilo en el tema aunque el rol esté en la lista**: `role="menuitem"`, `role="option"`, `row`,
`cell` y `gridcell` (sólo se les da el relleno horizontal), `navigation` (sólo `banner` y el gancho
`nav`). Es decir, el tema viste el **contenedor** del menú y de la lista de opciones, no sus
elementos. El estado «elegida» de una opción (`aria-selected` en `option`) tampoco: sólo `record`.

## 6. Lo que falta para un front de gestión de primer nivel

Cada fila dice qué necesita nuestro front, qué se puede hacer **hoy** con `--th-*` y qué se pide a
Theros (PT-n, sección 9). «Hoy» significa en nuestra capa `karpo.ext`, derivado del tema, sin literales.

### 6.1 Estados de interacción y foco

| Falta | Por qué importa | Hoy | Petición |
|---|---|---|---|
| El **estilo** del foco (`solid|dotted|double|glow`) y su política (`always|keyboard`) no son variables | Los elementos propios (celda, nodo de árbol, nodo de mapa) deben ver el mismo anillo que el resto, y un anillo `glow` o `dotted` no se reproduce con ancho y color | La regla global `:focus-visible` (o `:focus` en `classic-1995`) ya cubre cualquier elemento con `tabindex`: **no sobrescribir `outline`** en componentes propios. Para anillos interiores (celda en edición), usar `outline: var(--th-state-focus-width) solid var(--th-state-focus-color)` con `outline-offset` negativo | PT-1 |
| Estado **celda activa / celda en edición / celda sucia / celda inválida / celda de sólo lectura** en una rejilla | La edición en línea de Javier necesita distinguirlos de la fila elegida; el contrato sólo conoce registro (`dirty`, `new`, `inactive`) | Celda activa: anillo del foco. En edición: `control.field-focused.*`. Sucia: marca en la esquina con `semantic.warning.strong`. Inválida: `control.field-invalid.*` y `semantic.error.strong`. Sólo lectura: `control.field-readonly.*`. Atributo propio `data-kx-cell` | PT-2 |
| Estados de **guardado** del registro: `saving`, `saved`, `error`, `conflict` (409 por versión), `pending` (202 con aprobación pendiente, ver `front/README.md`), `deleted` (marcado en un lote), `locked` | El guardado optimista y por lotes necesita decir en qué punto está cada fila; `data-th-state` sólo admite tres valores | Atributo propio `data-kx-state` con `semantic.info/warning/error` y `state.disabled-opacity` | PT-3 |
| Estado **ocupado**: `busy` sólo admite `dim`, y el generador **rechaza** `skeleton|spinner` | Esqueleto de carga y spinner | Esqueleto con `surface.layer.level3` y `motion.duration.long`; spinner con `semantic.info.strong` | PT-6 |
| Estado **arrastrando** (reordenar columnas o filas) y **destino** ya existe (`data-th-drop=over`) | Reordenar columnas, mover nodos | Reutilizar `data-th-drop`; la fila arrastrada con `state.pressed.*` y `depth.levels.overlay` | — |
| `aria-selected` en **opciones** y **celdas** | Menús y listas desplegables de la búsqueda, selección de rango de celdas | `role=option[aria-selected]` con `state.selected-*`; rango con `state.selected-inactive-*` | PT-2 |

### 6.2 Color: semántica, validación y datos

| Falta | Hoy | Petición |
|---|---|---|
| **Validación**: hay `error`, `warning`, `success`, `info`, `neutral` con `bg/fg/border/strong`, y `field-invalid`. Faltan `field-warning` (aviso en un campo) y `field-valid` (confirmado), y un tono **marca** | Para un aviso de campo: `semantic.warning.*`, y para el tono marca (`color: "primary"` de la configuración Angular, 8 usos) derivarlo de `control.button-default.bg` con `color-mix` | PT-4 |
| **Paleta de series** (categórica 8 a 10 colores seguros para daltónicos, secuencial de 5 a 7 pasos, divergente, «sin dato») para mapas coropléticos, organigramas por departamento, gráficos | Sólo existen los colores semánticos y el acento; no se exportan las rampas del estilo (`palette.accent.*`, `palette.neutral.*`: viven en el YAML del estilo y no llegan al CSS ni a `manifest.json`). Se puede derivar una secuencial con `color-mix(in oklab, var(--th-surface-content-bg), var(--th-control-button-default-bg) N%)`, **pero una categórica no se puede derivar con garantías** (ni de distinción ni de contraste): no se improvisa | **PT-5 (la más importante)** |
| **Mapa**: tierra, agua, límites, etiquetas y su halo, país elegido y pasado el ratón, país sin dato | `surface.content/canvas` para tierra y agua, `edge.strong`, `text.secondary`, halo con `surface.content.bg`, elegido con `state.selected-*`; **resueltos a literales** (7.1) | PT-5 (con `map.*` opcional) |
| **Diagrama**: nodo, nodo elegido, línea de conexión, línea resaltada | Nodo = `surface.layer.level1/2`; línea = `edge.strong`; resaltado = `semantic.info.strong` | — |
| Colores **con alfa** (superposiciones, selección de rango) | `color-mix(in srgb, var(--th-x) 20%, transparent)`; funciona en el Edge probado, que es la base (la API `color-mix` no es un gancho del contrato) | — |
| Color de `entity.color` en la configuración Angular (`"#8e44ad"` y otros cuatro hexadecimales, 8 usos) | **Contradice «sin valores fijos»**: se ignora el hexadecimal y la entidad toma un color de la paleta de series por posición estable (hash del `id`); `green|danger|warning|gray|blue|primary` pasan a tonos: `green→success`, `danger→error`, `warning→warning`, `gray→neutral`, `blue→info`, `primary→marca` | PT-5 |

### 6.3 Apilado, movimiento, tipografía, densidad

| Falta | Por qué importa | Hoy | Petición |
|---|---|---|---|
| **`z-index`**: el tema sólo pone `1` al tooltip de `data-th-reason`; no hay escala (sticky, menú, diálogo, aviso, tooltip, recorrido guiado) | Cabeceras y columnas fijas, menús, avisos y el recorrido (driver.js) se pisan | Un fichero `karpo.ext` con la escala (`--kx-z-sticky` ... `--kx-z-tour`), marcado como la única excepción numérica; usar `<dialog>`/`popover` nativos (capa superior del navegador) para flotantes, que **no necesitan `z-index`** | PT-7 |
| **Curvas** de movimiento (`motion.easing` no es variable) y **respeto de `prefers-reduced-motion`**: el tema sólo aplica transiciones si `no-preference`; no anula animaciones ajenas ni pone las duraciones a 0 | Zoom del árbol, `flyTo` del mapa, recorrido, animación de plegado | Duraciones `--th-motion-duration-*`; curva `ease-out` si no hay variable; **todo movimiento propio, bajo `@media (prefers-reduced-motion: no-preference)`**, y en `classic-1995` (duraciones 0 ms) queda sin animar | PT-8 |
| **Pesos de letra** (`type.emphasis`): sólo `heading-weight` y `collection-header-weight` | Distintos énfasis (etiqueta, valor, total) | `font-weight: var(--th-text-heading-weight)` para énfasis fuerte; el normal, `normal`; no se inventa un 500 | PT-8 |
| **Densidad en caliente.** `design.density` se resuelve al compilar el tema; los estilos de serie no definen un modo `compact` ni `spacious` (la maquinaria de modos de un tema lo admite: `modes.compact` en `Plan-F1`), y `data-th-mode="dark compact"` está en el contrato pero **no existe en ningún tema** | Un conmutador «compacto/cómodo» es lo esperable en un SaaS de gestión | Dos temas generados o ninguna conmutación; **no se simula** con factores propios | PT-9 |
| **Altura de fila.** `space.row` es el `padding-block` de la celda; la altura real es línea + 2 × `row` | La virtualización exige una altura fija | Se **mide** al montar (una fila sonda oculta) y se vuelve a medir al cambiar de tema o de fuente | PT-9 |
| Escala tipográfica fuera de 6 pasos y sin `letter-spacing`, `line-height` de títulos ni `font-variant-numeric` general | Etiquetas pequeñas de cabecera de columna, números en gráficos | `tabular-nums` propio en celdas numéricas (el tema sólo lo pone en `amount`) | — |
| **Radios**: sólo `small/medium/large` (con `large` = cápsula) | El anillo de un nodo de diagrama, el avatar | `small` para chips, `medium` para tarjetas, `large` para cápsulas | — |
| **Breakpoints y contenedores**: ninguno | Diseño adaptable | **Propios del front** (una media query no admite `var()`); contenedores con `@container` y tamaños en `em` | no se pide |

### 6.4 Componentes sin estilo en el tema (el front debe construirlos con el vocabulario del tema)

El criterio del contrato es que **un concepto sin gancho se pide a Theros solo si lo necesita otro
front**; un componente que se compone con conceptos existentes **no se pide**. Esta tabla separa los
dos casos:

| Componente | Cómo se compone hoy (sin pedir nada) | Se pide |
|---|---|---|
| Pestañas, migas, paginador | `nav`/`nav-item` con `aria-current`; el marcador `underline` de `nav.item-current` | PT-10 (gancho `tabs`) si Theros quiere una pestaña de verdad; mientras tanto, hook propio con `nav.item-current.*` |
| Elemento de menú, opción de lista | `role=menuitem|option` con `state.hover`, `state.selected` | PT-10 |
| Chip, etiqueta, filtro activo | `[data-th=badge]` + `data-th-tone`, con botón de quitar `quiet` | — |
| Aviso emergente (toast) | `[data-th=feedback]` + `data-th-elevation=overlay` + `role=status` | — |
| Barra de progreso, spinner, esqueleto | `semantic.info.strong` sobre `surface.layer.level3`; `motion.duration.long` | PT-6 |
| Diálogo con cabecera, pie y tamaños | `dialog` nativo (el tema pone fondo, sombra, radio, relleno, `::backdrop`) + `[data-th=sheet]` por dentro | — |
| Panel lateral / cajón | `dialog` o `popover` con `surface.sidebar` y `depth.levels.modal` | — |
| Separador de paneles arrastrable | `edge.strong` y, al arrastrar, `semantic.info.strong` | — |
| Avatar, icono de entidad | `svg[data-th-icon]` y `surface.layer.level2` con `edge.rounding.large` | — |
| Atajo de teclado (`kbd`) | `surface.layer.level3`, `edge.subtle`, `type.mono-family` | — |
| Ayuda guiada (driver.js) | Popover con `surface.popover.*`, `text.*`, acciones del tema, velo `surface.scrim` (7.4) | — |
| Árbol/diagrama, mapa, gráfico | Ver 6.2 | PT-5 |

### 6.5 Iconos

El tema define tamaño y color (`control-icon-size`, `control-icon-color`) y **aplica `fill`/`stroke`
según el estilo**: `modern-flat` y `classic-1995` usan `fill: none; stroke: currentColor` (contorno),
**`glossy-2003` usa `fill: currentColor; stroke: none`** (relleno). Un icono de contorno con trazos
sin cierre se ve como una mancha en `glossy-2003`. El estilo no viaja en una variable (`control.icon.style`
está en las 20 ranuras de 4.3). El contrato no trae ningún conjunto de iconos ni lista de nombres
(`data-th-icon` es texto libre); las configuraciones usan `building`, `view`, `edit`, `delete`,
`search`, `box`, `truck`, `package`, `calendar`, `briefcase`, `history`, `globe`, `lock`.
El espejo en RTL está **fuera de alcance** (Plan-F2 §8) y se pide (PT-11).

## 7. Trampas que condicionan el diseño del front

### 7.1 `light-dark()` sin resolver: MapLibre y driver.js necesitan literales

Comprobado en Edge 158: `getComputedStyle(document.documentElement).getPropertyValue('--th-semantic-up-strong')`
devuelve **`light-dark(#0b7a6e, #4cc2b0)`**, el texto escrito, tanto en claro como con
`data-th-mode="dark"`. Para resolverlo hay que pintar la variable en un elemento y leer el color
calculado: con `color: var(--th-semantic-up-strong)` en un elemento sonda se obtiene
`rgb(11, 122, 110)` en claro y `rgb(76, 194, 176)` en oscuro (comprobado). Consecuencias:

- Hace falta un **`resolveThemeColor(token)`** (sonda en el elemento donde se va a pintar) y un
  **observador de cambio de tema y de modo** (cambio del `href`, atributo `data-th-mode`,
  `matchMedia('(prefers-color-scheme: dark)')`, `prefers-contrast`, `forced-colors`) que reconstruya
  estilos de MapLibre, colores de canvas/SVG y el velo de driver.js.
- En **forced-colors**, las variables valen `Canvas`, `CanvasText`, `Highlight`...; la sonda devuelve
  el color real del sistema, que es lo correcto.
- Todo lo que se pinte en **DOM/SVG** (organigrama, gráficos, nodos) usa `var(--th-*)` directamente en
  CSS (`fill`, `stroke`) y no necesita sonda; **canvas/WebGL** sí.

### 7.2 El soporte de modos depende del tema: leer `manifest.json`

Sólo `modern-flat` (y los temas que extienden de él: `souq`, `bolsa`, `taller`, `limon`) tiene
`design.mode: system` y oscuro; `classic-1995` y `glossy-2003` son sólo claros (`color-scheme: light`,
ningún `light-dark()`). Un conmutador de modo que no hace nada es una falta de diseño. Regla: el
front lee `manifest.json` al cargar el tema (`design["design.mode"].value`, `modes`) y **oculta o
desactiva** lo que el tema no tiene (oscuro, alto contraste); también lee `culture.direction`,
`culture.locale`, `culture.trend`, `design.density`, y compara `dictionary.version/hash` con lo que
soporta (aviso en consola y en la página de ayuda si no coincide).

### 7.3 Los tres estilos de serie son tres estructuras, no tres paletas

`classic-1995` (relieve: bordes dobles con `edge-relief-*`, foco punteado dentro del elemento con
offset -4px, cero redondeo y cero sombras, cero duraciones), `glossy-2003` (degradados en botones,
lienzo a rayas, **pulso** del botón por defecto, iconos rellenos) y `modern-flat`. Lo que se
comparte son los nombres de variable, no la geometría: `--th-control-height-regular` vale 23, 28 o 32
px; `--th-type-ui-size` 12, 13 o 14 px; `--th-edge-rounding-small` 0, 4 o 4 px. **Un componente que
fije su altura, su borde o su sombra se ve roto en otro estilo**; los componentes del front toman
siempre `height/min-block-size` de `control-height-*`, el radio de `edge-rounding-*`, el borde de
`edge-width`, y **añaden el relieve** (`edge-relief-*`) cuando el selector del tema lo hace para
elementos equivalentes (botón, campo, diálogo). Esto se prueba con **los tres estilos** en las capturas.

### 7.4 driver.js (ayuda guiada) no sigue el tema por sí solo

driver.js (MIT) pinta con colores fijos en su hoja `driver.css` y con una capa SVG de velo cuyo
color se pasa por opciones (`overlayColor`, `overlayOpacity`, `popoverClass`, `stagePadding`,
`stageRadius`, `animate`, `smoothScroll`). **A verificar al integrarlo**: no se importa su hoja por
defecto, se escribe una propia para `.driver-popover` (título, descripción, pie, botones, flecha,
progreso) con `surface.popover.*`, `text.*`, y los botones como `data-th-kind`; el velo se calcula
con la sonda de 7.1 desde `--th-surface-scrim` (el valor trae alfa); `animate` y `smoothScroll`
dependen de `prefers-reduced-motion`; el recorrido necesita un `z-index` encima de todo (PT-7) y
**no puede dejar el foco atrapado** fuera de él (WCAG 2.1.2 y 2.4.3).

### 7.5 Foco

La regla global del tema, `:focus-visible { outline: width solid color; outline-offset: offset }`, va
en `th.base`, **sin selector por componente**; en `classic-1995` es `:focus` con `dotted` y offset
-4px; en `glossy-2003`, `glow`. Dentro de `[role="banner"]` el color es `currentColor`. Reglas:
(a) no escribir `outline: none` en ningún componente propio; (b) una celda que se edita pasa su
anillo al campo (que ya lo tiene) y no lo duplica; (c) la cabecera de la rejilla y las celdas
enfocables llevan `tabindex` gestionado (roving) para que la regla global las vea; (d) el anillo
de selección dentro de una fila elegida cae sobre el fondo de selección (el tema lo conoce y lo anota
como defecto abierto de `classic-1995` y su tarea v4, Plan-T2 §4).

### 7.6 La abstinencia y una rejilla virtualizada

El contrato prohíbe `style=` y medidas en lo generado: está escrito para HTML de servidor. Una
rejilla virtualizada necesita **posiciones y anchos calculados** (`transform` por fila, ancho de
columna que viene de `width: 140` de la configuración, columnas redimensionadas). Propuesta:

- Todo valor numérico de **disposición** (índice de fila, anchos, desplazamiento) entra como
  **variable propia** puesta con `element.style.setProperty('--kx-col-width', '140px')` (la CSSOM no
  la bloquea una CSP sin `unsafe-inline`; el atributo `style` en el HTML serializado, sí) y se
  consume en `karpo.components` (`grid-template-columns: var(--kx-cols)`).
- **Nunca** por `style` un color, una fuente, un radio, una sombra ni un espacio.
- Se anota como **desviación consciente** del texto del contrato (la intención del contrato, que el
  tema decida el aspecto, se cumple) y se pregunta a Javier (sección 10, consulta 1).

### 7.7 El tema ya fija comportamiento que un front SPA debe conocer

- `[aria-checked="true"], [aria-pressed="true"]` pintan **cualquier** elemento con el color de
  `toggle-on` (incluidos `role=switch` y `menuitemcheckbox` si los usáramos): usar `aria-checked`
  sólo en interruptores y casillas.
- `[aria-invalid="true"]` y `[readonly]` pintan cualquier elemento, no sólo campos.
- `:disabled, [aria-disabled="true"]` pone `cursor: not-allowed` y el borde y el texto de
  deshabilitado.
- `[data-th="record"] > *` recibe `padding-block: var(--th-space-row)`: **las celdas deben ser hijas
  directas del registro** (no envolverlas en un `div` por celda), y los registros que sean `div`
  deben llevar `role="row"`.
- `[data-th="collection"]` es `border-collapse: collapse` y su fondo es `surface.content`; una
  rejilla de `div` con `display: grid` tiene que repetir sus propios bordes (`border-block-end` ya
  los pone el tema en `th, td, [role=columnheader|gridcell|cell]`).
- El hover de una fila **no** se pinta si está elegida. La selección requiere `role="grid"` en la
  colección **y** foco dentro: un grid sin `tabindex` ni hijo enfocable se pinta inactivo siempre.
- La cebra es `nth-child(even)`: con virtualización cambia de color al desplazarse si el índice DOM
  no coincide con el de datos; la rejilla pone `aria-rowindex` y calcula la paridad en el índice
  de datos (o se desactiva la política).
- El estado vacío es un `p`; la carga es sólo la atenuación (`opacity` = `state.disabled.opacity`).
- `dialog` tiene `padding: space.margin` y radio `medium`; el `::backdrop` es el velo del tema.
- Los símbolos `↑ ↓ ▲ ▼ =` los pinta el tema con `content: ... / ""` (fuera del nombre accesible).
  Un front que los repita en texto los duplica.

### 7.8 Idioma, dirección y escritura

El tema **sólo usa propiedades lógicas** (se ha buscado `left`, `right`, `margin-left`, `padding-right`,
`text-align: left|right`: ninguna aparición). Con `souq` (árabe de Egipto) sale `--th-type-ui-family`
con `Noto Sans Arabic`, interlineado 1,6 y contraste AAA; con `bolsa` (chino), `Noto Sans SC` y el
rojo sube. Para **nuestro front**: `dir` y `lang` en `<html>` por idioma activo (es, en; estructura
para más); el cambio de idioma **no** cambia el tema (la fuente y el interlineado de la escritura
salen del tema que se genere para ese idioma, y un cambio de idioma que cambia de escritura necesita
cambiar de tema o, mejor, un tema por escritura: PT-12). Los iconos direccionales (flecha de
volver, chevron de plegado, adelante/atrás de un paginador) se espejan por **marca propia en nuestro
CSS**, no con un `scaleX(-1)` general. Los números y los gráficos no se espejan. Los diagramas
(organigrama) fijan su dirección de despliegue según `dir`.

## 8. Cosas que están a punto de cambiar

### 8.1 `carril/margenes` (T5, `a73d173`, sin integrar en `main`)

Añade en `th.base` `:where(body, h1..h6, p, ul, ol, menu, dl, dd, figure, blockquote, pre, fieldset)
{ margin: 0 }` y `:where(main) { padding: space.margin }` con el foco de `main` por dentro; quita
`margin-block-end` de `domain.field` (**el campo ya no lleva margen**: el espacio entre campos es el
hueco del contenedor) y la regla de la barra que lo anulaba. Para nosotros: **el front debe poner
`gap` en sus contenedores** (fichas, secciones, barras) en lugar de depender de márgenes, y no
asumir `margin` en el campo. El hash del diccionario no cambia. La integración se vigila con el
`resolved` del manifiesto y con la prueba de contrato.

### 8.2 v4 del diccionario

Lo anotado como pendiente en los planes de Theros Front (no es un compromiso): el anillo con
`currentColor` para la selección (Plan-T2 §4), el tamaño de letra como palanca de diseño, el espejo
RTL de iconos y el `feeds` invertido de `reading-direction` (Plan-F2 §8), y `depth.policy: blur` y la
era `glass`. **Nada de esto es lo que nos falta**: nuestras peticiones (sección 9) son nuevas.

## 9. Peticiones propuestas a Theros Front (para `PETICIONES-THEROS-FRONT.md`)

Se ordenan por prioridad. Forma de cada una: **qué concepto**, **ranuras** y de qué derivaría. En
v4, sólo añaden (la regla del diccionario). Mientras no lleguen, el front usa la solución de la
columna «Hoy» de la sección 6, en `karpo.ext`.

| N.º | Prioridad | Petición | Propuesta |
|---|---|---|---|
| PT-5 | **P0** | **Colores de datos** | Concepto `look.data.series` (10 colores categóricos con `bg`, `fg`, `strong`, validados para daltonismo y para contraste sobre `surface.content`), `look.data.sequential` (7 pasos desde el acento), `look.data.diverging` (7 pasos) y `look.data.missing`. Exportar las variables `--th-data-series-1..10`, `--th-data-seq-1..7`, `--th-data-div-1..7`, `--th-data-missing`. Opcional: `look.map.{land,water,border,label,label-halo,hover}` derivables de superficies |
| PT-2 | **P0** | **Celdas** | `domain.cell` con estados `active`, `editing`, `dirty`, `invalid`, `readonly` y rango (`data-th-state` ampliable a la celda, o `data-th="cell"`); las variables `look.collection.cell-active-*`, `cell-dirty` |
| PT-3 | **P0** | **Estados de guardado** | Ampliar `data-th-state` (o `data-th-save`) con `saving`, `saved`, `error`, `conflict`, `pending`, `deleted`, `locked`, y los conceptos `domain.record.*` equivalentes con color y marca (no sólo color) |
| PT-1 | P1 | **Foco completo** | Variables `--th-state-focus-style` (palabra) y `--th-state-focus-ring` (el `outline` completo) y `--th-state-focus-shadow` (para `glow`); la política como `[data-th-focus="keyboard|always"]` en la raíz |
| PT-7 | P1 | **Apilado** | `look.depth.stack` con `sticky`, `dropdown`, `overlay`, `modal`, `toast`, `tooltip`, `tour` como enteros: `--th-depth-stack-*` |
| PT-8 | P1 | **Movimiento y letra** | `--th-motion-easing-standard|emphasized`; que `design.motion: none` y `prefers-reduced-motion` pongan las duraciones a `0ms` en `th.a11y`; `--th-type-weight-regular|medium|strong` (de `type.emphasis`) y su `font-variant-numeric` |
| PT-9 | P1 | **Densidad en caliente y altura de fila** | Modos `compact` y `spacious` en los estilos de serie; `look.collection.row-height` (variable `--th-collection-row-height`) |
| PT-4 | P1 | **Tono marca y estados de campo** | `data-th-tone`: `accent`; `look.control.field-warning` y `field-valid` |
| PT-6 | P2 | **Ocupado** | Ampliar `state.busy` a `skeleton`, `spinner` y exportar `--th-state-busy-skeleton-a/b` y `--th-state-busy-spinner` |
| PT-10 | P2 | **Pestañas y opciones** | Ganchos `domain.tabs` y estilo de `role=menuitem|option` con `state.*` |
| PT-11 | P2 | **Iconos** | Exportar `--th-control-icon-style` (`outline|filled`) y un atributo `data-th-icon-mirror` y su regla RTL |
| PT-12 | P2 | **Tipografía por escritura en vivo** | Variable de familia por escritura (`--th-type-ui-family-arabic`, etc.) o selector `:lang(ar)`, para cambiar de idioma sin cambiar de tema |
| PT-13 | P2 | **Rampas** | Exportar las rampas del estilo (`--th-ramp-accent-0..100`, `--th-ramp-neutral-0..100`) como variables, o en `manifest.json` |
| PT-14 | P2 | **Manifiesto máquina** | Un `manifest.json` ligero (≤ 5 KB: modos, `culture`, `density`, hashes), separado del informe de contraste (173 KB) |

Pregunta de contexto: ninguna de estas peticiones afecta a Theros Back (ganchos), salvo PT-2, PT-3 y
PT-10, que serían ganchos nuevos que emitir.

## 10. Decisiones de arquitectura propuestas para el front

1. **Consumir `--th-*` directamente.** Sin capa de alias `--k-*` que duplique el tema (cualquier
   alias sería otro sitio donde un valor fijo puede esconderse). Una **única capa de extensión**,
   `--kx-*`, para lo que el contrato no cubre, definida **sólo** con `var(--th-*)`, `color-mix()` y
   `calc()`, y con los nombres que Theros adoptaría (`--kx-data-series-1` → `--th-data-series-1`), de
   modo que cuando llegue la petición se cambie una línea.
2. **Orden de capas**: `@layer th.tokens, th.modes, th.base, th.domain, th.a11y, karpo.ext,
   karpo.components;` (la de nuestro CSS **después**; nunca `!important`).
3. **Ganchos primero.** Cada componente emite el gancho de la tabla 2.3 antes que cualquier clase
   propia; las clases propias sólo disponen (rejilla, flex) con `var(--th-space-*)`. El prefijo de lo
   nuestro es `data-kx-*`, fuera del espacio `data-th-*` del contrato.
4. **Prueba de contrato local** (el CI de GitHub está bloqueado): una prueba que (a) recalcula el
   hash del diccionario copiado (SHA-256 con finales de línea `\n`) y lo compara con el esperado;
   (b) renderiza cada componente y comprueba que todo `data-th*`/`role`/`aria-*` emitido pertenece a
   la lista de 2.3; (c) comprueba la abstinencia con una regla de lint de CSS que prohíba literales
   de color, `px`/`rem`/`em` y nombres de fuente fuera de `kx-structure.css`, y el `style=` de color
   o de medida; (d) abre la página con los **tres estilos de serie** y un tema RTL y mira que nada
   se rompa (7.3).
5. **Un `ThemeProvider`** que: carga `index.css` y `manifest.json` desde el origen configurable (por
   defecto el propio despliegue; **ninguna URL de terceros**), expone el modo y los modos disponibles,
   `resolveThemeColor`, el cambio en caliente (cambiar el `href`, esperar la carga de los cinco
   `@import`, volver a medir las filas) y avisa de un hash de diccionario desconocido.
6. **Tema por defecto en el paquete.** `@karpo/react` necesita un tema para funcionar sin nada más:
   propuesta, **incluir `modern-flat` generado** como `@karpo/react/theme`: los seis CSS (unos 38 KB)
   y `manifest.json` (173 KB por el informe de contraste de 352 pares; PT-14); los demás
   ficheros (galería, resolver y tokens DTCG, YAML resuelto: unos 420 KB) no hacen falta. Es una
   decisión de Javier (consulta 2).

## 11. Consultas para Javier

1. **`style` con variables de disposición** en una rejilla virtualizada y en diagramas: una
   desviación del texto de abstinencia del contrato, no de su intención (7.6). Propuesta: sí, sólo
   para posición y tamaño, vía CSSOM.
2. **Incluir en `@karpo/react` una copia generada de `modern-flat`** como tema por defecto, con su
   hash. Theros Front es otro repositorio suyo, y su licencia no está declarada en lo leído
   (`README.md`, `CLAUDE.md`): hay que decir bajo qué licencia se distribuye lo generado.
3. **Prioridad de PT-5** (colores de datos): si Theros no la entrega a tiempo para el organigrama y el
   mapa, el front puede entregar una paleta propia en `karpo.ext` marcada como provisional (con una
   verificación de contraste propia), o esperar. Propuesta: provisional, con la etiqueta y la prueba.

## Anexo A. Las 167 variables, con sus valores en los tres estilos de serie

Valores tal como salen en `tokens.css` (`light-dark(claro, oscuro)` en `modern-flat`; los otros dos
estilos son sólo claros). Para repetir: `go run ./cmd/theros-front build <estilo> -out <carpeta>`
desde `C:\Git\Paranoia\Theros.Front`.

### A.collection: Listas (`look.collection.*`) (8)

| Variable | modern-flat | classic-1995 | glossy-2003 |
|---|---|---|---|
| `--th-collection-grid-color` | `light-dark(#dde2e8, #404854)` | `#c0c0c0` | `#e6eaee` |
| `--th-collection-header-bg` | `light-dark(#f7f8fa, #21272e)` | `#c0c0c0` | `#eff2f5` |
| `--th-collection-header-border` | `light-dark(#dde2e8, #404854)` | `#808080` | `#cbd2da` |
| `--th-collection-header-fg` | `light-dark(#404854, #cbd1da)` | `#000000` | `#21272e` |
| `--th-collection-header-weight` | `600` | `400` | `400` |
| `--th-collection-sort-bg` | `light-dark(#eff2f5, #313842)` | `#c0c0c0` | `#eaf2ff` |
| `--th-collection-sort-fg` | `light-dark(#21272e, #e6eaef)` | `#000000` | `#15191e` |
| `--th-collection-zebra-color` | `light-dark(#f7f8fa, #21272e)` | `#ffffff` | `#f5f9ff` |

### A.control: Controles (`look.control.*`) (37)

| Variable | modern-flat | classic-1995 | glossy-2003 |
|---|---|---|---|
| `--th-control-button-bg` | `light-dark(#ffffff, #313842)` | `#c0c0c0` | `#ffffff` |
| `--th-control-button-border` | `light-dark(#cbd1da, #5a6472)` | `#000000` | `#b0b8c3` |
| `--th-control-button-default-bg` | `light-dark(#1f6fc5, #5a9eef)` | `#c0c0c0` | `#629bf5` |
| `--th-control-button-default-border` | `light-dark(#1f6fc5, #5a9eef)` | `#000000` | `#03337a` |
| `--th-control-button-default-fg` | `light-dark(#ffffff, #090b0f)` | `#000000` | `#090b0f` |
| `--th-control-button-destructive-bg` | `light-dark(#c62828, #ef7a7a)` | `#c0c0c0` | `#ffffff` |
| `--th-control-button-destructive-border` | `light-dark(#c62828, #ef7a7a)` | `#000000` | `#b0b8c3` |
| `--th-control-button-destructive-fg` | `light-dark(#ffffff, #090b0f)` | `#800000` | `#c4281c` |
| `--th-control-button-fg` | `light-dark(#21272e, #e6eaef)` | `#000000` | `#15191e` |
| `--th-control-button-quiet-bg` | `#00000000` | `#00000000` | `#00000000` |
| `--th-control-button-quiet-border` | `#00000000` | `#00000000` | `#00000000` |
| `--th-control-button-quiet-fg` | `light-dark(#1f6fc5, #83bbff)` | `#000000` | `#215ebe` |
| `--th-control-field-bg` | `light-dark(#ffffff, #15191e)` | `#ffffff` | `#ffffff` |
| `--th-control-field-border` | `light-dark(#6b7583, #939ca9)` | `#808080` | `#77818f` |
| `--th-control-field-fg` | `light-dark(#21272e, #e6eaef)` | `#000000` | `#15191e` |
| `--th-control-field-focused-bg` | `light-dark(#ffffff, #15191e)` | `#ffffff` | `#ffffff` |
| `--th-control-field-focused-border` | `light-dark(#1f6fc5, #5a9eef)` | `#000000` | `#4d85dd` |
| `--th-control-field-focused-fg` | `light-dark(#21272e, #e6eaef)` | `#000000` | `#15191e` |
| `--th-control-field-invalid-bg` | `light-dark(#fdf3f3, #2a1517)` | `#ffffff` | `#ffffff` |
| `--th-control-field-invalid-border` | `light-dark(#c62828, #ef7a7a)` | `#800000` | `#c4281c` |
| `--th-control-field-invalid-fg` | `light-dark(#21272e, #e6eaef)` | `#000000` | `#15191e` |
| `--th-control-field-readonly-bg` | `light-dark(#f7f8fa, #21272e)` | `#c0c0c0` | `#f7f8fa` |
| `--th-control-field-readonly-border` | `light-dark(#dde2e8, #404854)` | `#808080` | `#cbd2da` |
| `--th-control-field-readonly-fg` | `light-dark(#5a6472, #b0b8c4)` | `#000000` | `#21272e` |
| `--th-control-height-large` | `40px` | `28px` | `32px` |
| `--th-control-height-regular` | `32px` | `23px` | `28px` |
| `--th-control-height-small` | `28px` | `20px` | `24px` |
| `--th-control-icon-color` | `light-dark(#5a6472, #b0b8c4)` | `#000000` | `#5a6471` |
| `--th-control-icon-size` | `16px` | `16px` | `16px` |
| `--th-control-padding` | `12px` | `8px` | `12px` |
| `--th-control-scrollbar-thickness` | `8px` | `16px` | `15px` |
| `--th-control-scrollbar-thumb` | `light-dark(#88909b, #5d6775)` | `#c0c0c0` | `#3875d7` |
| `--th-control-scrollbar-track` | `light-dark(#f7f8fa, #15191e)` | `#dfdfdf` | `#eff2f5` |
| `--th-control-toggle-mixed-bg` | `light-dark(#6b7583, #939ca9)` | `#808080` | `#5a6471` |
| `--th-control-toggle-mixed-fg` | `light-dark(#ffffff, #090b0f)` | `#ffffff` | `#ffffff` |
| `--th-control-toggle-on-bg` | `light-dark(#1f6fc5, #5a9eef)` | `#000080` | `#5c95ee` |
| `--th-control-toggle-on-fg` | `light-dark(#ffffff, #090b0f)` | `#ffffff` | `#090b0f` |

### A.depth: Profundidad (3)

| Variable | modern-flat | classic-1995 | glossy-2003 |
|---|---|---|---|
| `--th-depth-levels-modal` | `0 12px 32px #0d0f1240` | `none` | `0 12px 32px #00000059` |
| `--th-depth-levels-overlay` | `0 4px 12px #0d0f122e` | `none` | `0 4px 12px #00000040` |
| `--th-depth-levels-raised` | `0 1px 2px #0d0f1229` | `none` | `0 1px 2px #00000033` |

### A.edge: Bordes y radios (9)

| Variable | modern-flat | classic-1995 | glossy-2003 |
|---|---|---|---|
| `--th-edge-relief-light` | `light-dark(#ffffff, #5a6472)` | `#ffffff` | `#ffffff4d` |
| `--th-edge-relief-shadow-dark` | `light-dark(#939ca9, #000000)` | `#000000` | `#00000014` |
| `--th-edge-relief-shadow-mid` | `light-dark(#cbd1da, #090b0f)` | `#808080` | `#8a95a31a` |
| `--th-edge-rounding-large` | `9999px` | `0` | `999px` |
| `--th-edge-rounding-medium` | `8px` | `0` | `6px` |
| `--th-edge-rounding-small` | `4px` | `0` | `4px` |
| `--th-edge-strong` | `light-dark(#939ca9, #6b7583)` | `#000000` | `#77818f` |
| `--th-edge-subtle` | `light-dark(#dde2e8, #404854)` | `#808080` | `#cbd2da` |
| `--th-edge-width` | `1px` | `1px` | `1px` |

### A.motion: Movimiento (3)

| Variable | modern-flat | classic-1995 | glossy-2003 |
|---|---|---|---|
| `--th-motion-duration-long` | `300ms` | `0ms` | `400ms` |
| `--th-motion-duration-medium` | `200ms` | `0ms` | `240ms` |
| `--th-motion-duration-short` | `100ms` | `0ms` | `120ms` |

### A.nav: Navegaci�n (5)

| Variable | modern-flat | classic-1995 | glossy-2003 |
|---|---|---|---|
| `--th-nav-item-bg` | `#00000000` | `#00000000` | `#00000000` |
| `--th-nav-item-current-bg` | `light-dark(#e9f3ff, #00376e)` | `#000080` | `#215ebe` |
| `--th-nav-item-current-border` | `light-dark(#1f6fc5, #5a9eef)` | `#000080` | `#215ebe` |
| `--th-nav-item-current-fg` | `light-dark(#00376e, #e9f3ff)` | `#ffffff` | `#ffffff` |
| `--th-nav-item-fg` | `light-dark(#404854, #cbd1da)` | `#000000` | `#21272e` |

### A.semantic: Colores sem�nticos (29)

| Variable | modern-flat | classic-1995 | glossy-2003 |
|---|---|---|---|
| `--th-semantic-down-bg` | `light-dark(#fdecec, #3a1416)` | `#ffffff` | `#fdeceb` |
| `--th-semantic-down-border` | `light-dark(#f3a9a9, #7a2a2a)` | `#800000` | `#c4281c` |
| `--th-semantic-down-fg` | `light-dark(#7a1616, #f8c9c9)` | `#800000` | `#c4281c` |
| `--th-semantic-down-strong` | `light-dark(#c62828, #f08c8c)` | `#800000` | `#c4281c` |
| `--th-semantic-error-bg` | `light-dark(#fdecec, #3a1416)` | `#ffffff` | `#fdeceb` |
| `--th-semantic-error-border` | `light-dark(#f3a9a9, #7a2a2a)` | `#800000` | `#c4281c` |
| `--th-semantic-error-fg` | `light-dark(#7a1616, #f8c9c9)` | `#800000` | `#c4281c` |
| `--th-semantic-error-strong` | `light-dark(#c62828, #f08c8c)` | `#800000` | `#c4281c` |
| `--th-semantic-info-bg` | `light-dark(#e9f3ff, #00264f)` | `#ffffff` | `#f5f9ff` |
| `--th-semantic-info-border` | `light-dark(#b2d4ff, #1f6fc5)` | `#000080` | `#629bf5` |
| `--th-semantic-info-fg` | `light-dark(#00376e, #cee4ff)` | `#000080` | `#215ebe` |
| `--th-semantic-info-strong` | `light-dark(#1f6fc5, #83bbff)` | `#000080` | `#215ebe` |
| `--th-semantic-neutral-bg` | `light-dark(#eff2f5, #313842)` | `#c0c0c0` | `#eff2f5` |
| `--th-semantic-neutral-border` | `light-dark(#cbd1da, #5a6472)` | `#808080` | `#b0b8c3` |
| `--th-semantic-neutral-fg` | `light-dark(#404854, #cbd1da)` | `#000000` | `#21272e` |
| `--th-semantic-neutral-strong` | `light-dark(#6b7583, #939ca9)` | `#000000` | `#404954` |
| `--th-semantic-required-color` | `light-dark(#c62828, #f08c8c)` | `#800000` | `#c4281c` |
| `--th-semantic-success-bg` | `light-dark(#e3f5f2, #0f2e2a)` | `#ffffff` | `#e9f6ec` |
| `--th-semantic-success-border` | `light-dark(#8fd3c8, #1f6a60)` | `#008000` | `#1f7a33` |
| `--th-semantic-success-fg` | `light-dark(#0a4d45, #a8e6dc)` | `#008000` | `#1f7a33` |
| `--th-semantic-success-strong` | `light-dark(#0b7a6e, #4cc2b0)` | `#008000` | `#1f7a33` |
| `--th-semantic-up-bg` | `light-dark(#e3f5f2, #0f2e2a)` | `#ffffff` | `#e9f6ec` |
| `--th-semantic-up-border` | `light-dark(#8fd3c8, #1f6a60)` | `#008000` | `#1f7a33` |
| `--th-semantic-up-fg` | `light-dark(#0a4d45, #a8e6dc)` | `#008000` | `#1f7a33` |
| `--th-semantic-up-strong` | `light-dark(#0b7a6e, #4cc2b0)` | `#008000` | `#1f7a33` |
| `--th-semantic-warning-bg` | `light-dark(#fff4e0, #3a2a0e)` | `#ffffff` | `#fff5d6` |
| `--th-semantic-warning-border` | `light-dark(#f2c879, #7a5a1c)` | `#808000` | `#8a6200` |
| `--th-semantic-warning-fg` | `light-dark(#6b3d00, #f7d89a)` | `#5c5c00` | `#8a6200` |
| `--th-semantic-warning-strong` | `light-dark(#a35f00, #e8a33a)` | `#5c5c00` | `#8a6200` |

### A.space: Espaciado (13)

| Variable | modern-flat | classic-1995 | glossy-2003 |
|---|---|---|---|
| `--th-space-1` | `4px` | `4px` | `4px` |
| `--th-space-2` | `8px` | `8px` | `8px` |
| `--th-space-3` | `12px` | `12px` | `12px` |
| `--th-space-4` | `16px` | `16px` | `16px` |
| `--th-space-5` | `20px` | `20px` | `20px` |
| `--th-space-6` | `24px` | `24px` | `24px` |
| `--th-space-7` | `28px` | `28px` | `28px` |
| `--th-space-8` | `32px` | `32px` | `32px` |
| `--th-space-control-gap` | `8px` | `6px` | `12px` |
| `--th-space-group-gap` | `24px` | `12px` | `20px` |
| `--th-space-margin` | `16px` | `8px` | `16px` |
| `--th-space-row` | `8px` | `2px` | `4px` |
| `--th-space-unit` | `4px` | `4px` | `4px` |

### A.state: Estados (16)

| Variable | modern-flat | classic-1995 | glossy-2003 |
|---|---|---|---|
| `--th-state-disabled-color` | `light-dark(#939ca9, #6b7583)` | `#808080` | `#8a95a3` |
| `--th-state-disabled-opacity` | `0.5` | `1` | `0.5` |
| `--th-state-drop-target-bg` | `light-dark(#e9f3ff, #00376e)` | `#000080` | `#eaf2ff` |
| `--th-state-drop-target-border` | `light-dark(#1f6fc5, #5a9eef)` | `#000000` | `#3875d7` |
| `--th-state-drop-target-fg` | `light-dark(#00376e, #e9f3ff)` | `#ffffff` | `#15191e` |
| `--th-state-focus-color` | `light-dark(#1f6fc5, #5a9eef)` | `#000000` | `#4d85dd` |
| `--th-state-focus-offset` | `2px` | `-4px` | `2px` |
| `--th-state-focus-width` | `2px` | `1px` | `2px` |
| `--th-state-hover-bg` | `light-dark(#eff2f5, #313842)` | `#c0c0c0` | `#f5f9ff` |
| `--th-state-hover-fg` | `light-dark(#21272e, #e6eaef)` | `#000000` | `#15191e` |
| `--th-state-pressed-bg` | `light-dark(#dde2e8, #404854)` | `#c0c0c0` | `#eaf2ff` |
| `--th-state-pressed-fg` | `light-dark(#21272e, #e6eaef)` | `#000000` | `#15191e` |
| `--th-state-selected-bg` | `light-dark(#e9f3ff, #00376e)` | `#000080` | `#215ebe` |
| `--th-state-selected-fg` | `light-dark(#00376e, #e9f3ff)` | `#ffffff` | `#ffffff` |
| `--th-state-selected-inactive-bg` | `light-dark(#eff2f5, #313842)` | `#c0c0c0` | `#e6eaee` |
| `--th-state-selected-inactive-fg` | `light-dark(#21272e, #e6eaef)` | `#000000` | `#21272e` |

### A.surface: Superficies (21)

| Variable | modern-flat | classic-1995 | glossy-2003 |
|---|---|---|---|
| `--th-surface-canvas-bg` | `light-dark(#f7f8fa, #15191e)` | `#c0c0c0` | `#f7f8fa` |
| `--th-surface-canvas-fg` | `light-dark(#21272e, #e6eaef)` | `#000000` | `#21272e` |
| `--th-surface-content-bg` | `light-dark(#ffffff, #21272e)` | `#ffffff` | `#ffffff` |
| `--th-surface-content-fg` | `light-dark(#21272e, #e6eaef)` | `#000000` | `#21272e` |
| `--th-surface-dialog-bg` | `light-dark(#ffffff, #313842)` | `#c0c0c0` | `#f7f8fa` |
| `--th-surface-dialog-fg` | `light-dark(#21272e, #e6eaef)` | `#000000` | `#21272e` |
| `--th-surface-header-bg` | `light-dark(#ffffff, #090b0f)` | `#000080` | `#e6eaee` |
| `--th-surface-header-fg` | `light-dark(#21272e, #e6eaef)` | `#ffffff` | `#15191e` |
| `--th-surface-header-inactive-bg` | `light-dark(#f7f8fa, #15191e)` | `#808080` | `#eff2f5` |
| `--th-surface-header-inactive-fg` | `light-dark(#5a6472, #939ca9)` | `#c0c0c0` | `#666f7d` |
| `--th-surface-layer-level1` | `light-dark(#ffffff, #21272e)` | `#c0c0c0` | `#ffffff` |
| `--th-surface-layer-level2` | `light-dark(#f7f8fa, #313842)` | `#c0c0c0` | `#f7f8fa` |
| `--th-surface-layer-level3` | `light-dark(#eff2f5, #404854)` | `#c0c0c0` | `#eff2f5` |
| `--th-surface-popover-bg` | `light-dark(#ffffff, #313842)` | `#c0c0c0` | `#ffffff` |
| `--th-surface-popover-border` | `light-dark(#dde2e8, #5a6472)` | `#000000` | `#cbd2da` |
| `--th-surface-popover-fg` | `light-dark(#21272e, #e6eaef)` | `#000000` | `#21272e` |
| `--th-surface-scrim` | `light-dark(#090b0f66, #000000a3)` | `#00000000` | `#0000004d` |
| `--th-surface-sidebar-bg` | `light-dark(#eff2f5, #090b0f)` | `#c0c0c0` | `#f5f9ff` |
| `--th-surface-sidebar-fg` | `light-dark(#21272e, #e6eaef)` | `#000000` | `#21272e` |
| `--th-surface-tooltip-bg` | `light-dark(#313842, #e6eaef)` | `#ffffe1` | `#fffbd9` |
| `--th-surface-tooltip-fg` | `light-dark(#ffffff, #15191e)` | `#000000` | `#15191e` |

### A.text: Texto (11)

| Variable | modern-flat | classic-1995 | glossy-2003 |
|---|---|---|---|
| `--th-text-disabled-color` | `light-dark(#939ca9, #6b7583)` | `#808080` | `#8a95a3` |
| `--th-text-heading-color` | `light-dark(#15191e, #f7f8fa)` | `#000000` | `#15191e` |
| `--th-text-heading-weight` | `600` | `700` | `700` |
| `--th-text-inverse` | `light-dark(#ffffff, #090b0f)` | `#ffffff` | `#ffffff` |
| `--th-text-link` | `light-dark(#1f6fc5, #83bbff)` | `#0000ff` | `#2c69ca` |
| `--th-text-link-visited` | `light-dark(#6a3fb5, #b195e8)` | `#800080` | `#6b3fa0` |
| `--th-text-placeholder` | `light-dark(#6b7583, #939ca9)` | `#808080` | `#6d7785` |
| `--th-text-primary` | `light-dark(#21272e, #e6eaef)` | `#000000` | `#21272e` |
| `--th-text-secondary` | `light-dark(#5a6472, #b0b8c4)` | `#000000` | `#5a6471` |
| `--th-text-selection-bg` | `light-dark(#cee4ff, #00478a)` | `#000080` | `#d1e3ff` |
| `--th-text-selection-fg` | `light-dark(#21272e, #ffffff)` | `#ffffff` | `#15191e` |

### A.type: Tipograf�a (12)

| Variable | modern-flat | classic-1995 | glossy-2003 |
|---|---|---|---|
| `--th-type-1` | `14px` | `12px` | `13px` |
| `--th-type-2` | `16.8px` | `13.8px` | `15.6px` |
| `--th-type-3` | `20.16px` | `15.87px` | `18.72px` |
| `--th-type-4` | `24.19px` | `18.25px` | `22.46px` |
| `--th-type-5` | `29.03px` | `20.99px` | `26.96px` |
| `--th-type-6` | `34.84px` | `24.14px` | `32.35px` |
| `--th-type-mono-family` | `ui-monospace, monospace` | `ui-monospace, monospace` | `ui-monospace, monospace` |
| `--th-type-scale` | `1.2` | `1.15` | `1.2` |
| `--th-type-small` | `12px` | `11px` | `11px` |
| `--th-type-ui-family` | `system-ui, sans-serif` | `system-ui, sans-serif` | `system-ui, sans-serif` |
| `--th-type-ui-line-height` | `1.43` | `1.34` | `1.4` |
| `--th-type-ui-size` | `14px` | `12px` | `13px` |

