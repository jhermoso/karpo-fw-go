# Análisis, ronda 2: causas y enfoque contra el informe del jurado

Fecha: 2026-11-27. Base: commit `1490a0d` (rama `claude/front-react-7b8564`). Papel: analista.
Entrada: `docs/jurado/ronda-1.md` (5 bloqueantes, 19 mejoras). Salida para: integrador de la ronda 2.

Método: lectura del código y sondeos reales con `playwright-core` + Edge contra `vite preview` de
la demo (puertos libres, parados al terminar; 0 peticiones fuera del origen). Los sondeos están
marcados con **[medido]**. Lo que no he podido medir se marca **[por verificar]**. No he tocado
ningún fichero del proyecto salvo este informe.

## Resumen ejecutivo

| Punto | Causa real | Dificultad |
|---|---|---|
| B-1 iconos | Cinco nombres de Phosphor en el manifiesto no están en `ICONS`; `Icon` cae en `FALLBACK_ICON` sin avisar | Baja |
| B-2 inglés | **Tres causas distintas**: (a) `MapView` usa el literal; (b) `useFieldText().resolve` ignora el diccionario de la configuración (por eso las insignias salen «Cubierto»); (c) la demo no trae superposiciones de texto para las 8 entidades | Media-alta (c es volumen) |
| B-3 tipografía | `maplibre-gl.css` fija `font` en `.maplibregl-map`; no hay regla que lo anule | Baja |
| B-4 Tab | `nextPosition` trata `Tab` como tecla de navegación y `onKeyDown` hace `preventDefault`. El tabindex itinerante ya está bien | Baja |
| B-5 móvil | La barra superior no tiene modo compacto: 11 controles en una fila de 390 px | Media |
| **Hallazgo nuevo** | `:dir(rtl)` se compila a `:lang(ar,he,…)` y **no espeja nada** con `?dir=rtl` (ni en español o inglés reales). Explica la mejora 11 | Baja |

Orden recomendado: B-1, B-3, B-4 (rápidos, aislados), después la causa (b) de B-2 y el hallazgo
`:dir()`, luego B-5, luego el contenido de B-2 (c), y al final las mejoras de diseño y rendimiento.

---

## B-1. Iconos del menú

**Causa.** `demo/config/manifest.json` usa `buildings`, `tree-structure`, `globe-hemisphere-west`,
`map-trifold`, `calendar-dots` (nombres de Phosphor). `src/ui/icons.ts` sólo tiene alias propios
(`building`, `org-chart`, `globe`, `map`, `calendar`…). `Icon.tsx:26` hace
`ICONS[name] ?? FALLBACK_ICON` sin ningún aviso. El menú toma el icono del manifiesto
(`config/manifest.ts:134`, `str(raw.icon) ?? entity?.icon`) y nadie lo valida. Las 8 configuraciones
de Go sólo usan nombres que sí existen (`box`, `briefcase`, `building`, `calendar`, `delete`, `edit`,
`globe`, `history`, `package`, `search`, `truck`, `view`), así que el fallo viene del manifiesto de
la demo, pero cualquier manifiesto de Theros puede traer nombres de Phosphor.

**Ficheros.** `src/ui/icons.ts`, `src/ui/Icon.tsx`, `src/config/manifest.ts`,
`src/config/normalize.ts` (iconos de opciones, secciones, filtros y órdenes: líneas 285, 605, 680,
831), nuevo `src/contracts/icons.ts`, `src/ui/ui.test.tsx`, nueva prueba de contrato.

**Enfoque.**
1. Aceptar los nombres de Phosphor tal cual: añadir `buildings`, `tree-structure`,
   `globe-hemisphere-west`, `map-trifold`, `calendar-dots` (importar `CalendarDotsIcon`; el resto
   ya está importado). Mejor aún: que cada icono importado se registre también con su nombre
   kebab de Phosphor, para que `Icon` acepte «los dos vocabularios» y los alias propios queden
   como sinónimos. Cuesta ~100 líneas y evita la siguiente ronda de lo mismo.
2. Lista de nombres sin Phosphor: `src/contracts/icons.ts` exporta `ICON_NAMES` como `const`
   (datos puros, sin importar Phosphor) y `icons.ts` se tipa `Record<IconName, IconEntry>`. Así
   TypeScript garantiza que lista y registro no se separan, y la capa `config` puede validar sin
   cargar 450 kB de iconos.
3. Aviso de configuración: tras normalizar, un recorrido genérico del `MenuNode[]` y de cada
   `EntityConfig` (campos `icon`) emite `ConfigWarning` con `severity: 'warn'` y mensaje
   «icono `x` desconocido: se usa un círculo» (el `logReport` de `manifest.ts:214` ya imprime los
   `warn`). Ruta del aviso: `menu[3].children[1].icon`.
4. `Icon` marca el fallo: `data-kx-icon="missing"` y, en desarrollo, `console.warn` una sola vez
   por nombre.
5. Pruebas: (a) de contrato: recorrer `demo/config/manifest.json`, las 4 entidades de la demo y las
   8 de `../angular/config/entities` y exigir que todo `icon` esté en `ICON_NAMES`; (b) unitaria de
   `Icon` con nombre desconocido; (c) e2e: ningún `[data-th-icon]` del menú con
   `data-kx-icon="missing"` en las 7 entradas citadas.

## B-2. Inglés a medias

### Causa (a): leyenda y etiquetas del mapa, organigrama y migas

`MapView.tsx:159` y `:182`, `TreeView.tsx:167`, `TreeDiagram.tsx:463`, `TreeList.tsx:261` usan
`config.namePlural.literal || config.name.literal`, es decir, el texto en español sin resolver.
Además `toLowerCase()` sólo vale para algunos idiomas. Ya existe `useText()` (`config/text.ts`).

**Enfoque.** Resolver con `const text = useText()` y `text(config.namePlural)`. Para la leyenda, no
poner en minúsculas por código: i18next admite `{{name, lowercase}}` con formateador y
`Intl`-aware (`toLocaleLowerCase(locale)`), o mejor, pasar a una cadena sin sustantivo
(«Instalaciones por delimitación») y evitar el problema. Revisar con `grep -rn "\.literal"` fuera de
`config/` y `fields/values.ts` (hay 7 usos en `tree/`, `map/`, `grid/FilterBar.tsx:101`,
`help/steps.ts:95`).

### Causa (b): las insignias salen «Cubierto» **[medido]**

El sondeo con `lng=en` en `/position?view=tree` da `['Cubierto','Cubierto','Cubierto']` y confirma
el informe. Causa exacta: `fields/hooks.ts:16-26`, `useFieldText().resolve` hace
`i18n.exists(text.key)` sobre la instancia de la **interfaz** y, si no la encuentra, `textFor(text,
locale)`. Las claves de la configuración (`entities.position.status.filled`) viven en el diccionario
del manifiesto (`registerConfigDictionary` / espacio `config:`), que ese resolvedor no consulta, y
cae al literal. `useText()` (`config/text.ts:86`) sí lo consulta, por eso los títulos de columna y
los nombres de entidad sí se traducen. Afecta a **todo** lo que pasa por `FieldView`: insignias,
selects, multiselect, texto de booleanos (`trueLabel`/`falseLabel` van por `textFor` en
`codecs.ts:297`) y etiquetas de campos del detalle si usan `useFieldText`.

**Enfoque.**
1. `useFieldText().resolve` debe delegar en `useText()` (misma cadena: clave en el idioma activo,
   diccionario de la configuración, `byLang`, literal). Una sola implementación de resolución.
2. `codecs.format` (`values.ts:optionLabel`, `codecs.ts:297`) es puro y recibe sólo `locale`; lo
   usan `plainFieldText` del organigrama (búsqueda y orden), la exportación y el ordenar/filtrar
   por texto. Darle un `resolve?: (text: Text) => string` opcional (contexto de formato) y que los
   llamadores pasen el de `useText()`. Si no, la búsqueda del organigrama en inglés no encuentra
   «Filled» y sí «Cubierto».
3. `findOption` ya compara con el literal; añadir también la etiqueta traducida para que el
   usuario pueda pegar o escribir «Filled» en una celda (paste/`parse`).
4. Pruebas: unitaria de `useFieldText` con diccionario registrado (hoy `fields.test.tsx` mockea
   i18n propio y por eso no lo detecta); de componente para `BadgeViewer` con `lng=en`; y la e2e
   de inglés (abajo).

### Causa (c): columnas, secciones y `badgeMap` de las 8 entidades de Go

Las 8 configuraciones de Go (`../angular/config/entities`) traen sólo literales en español:
unas 320 cadenas traducibles (asset 65, supplier 52, product 50, carrier 39, work-center 33,
country 30, facility 29, holiday 26), de ellas ~44 son opciones de `badgeMap`/`enumValues`. La demo
sólo superpone `nameKey/namePluralKey/descriptionKey` (y algún `form`/`detail` por lookups). El
mecanismo de superposición ya existe (`config/merge.ts`: objetos por clave, arrays por identidad
`id|field|action`), así que no hace falta código de producto nuevo, sólo contenido, con dos matices:
- `IDENTITY_KEYS` no incluye `value`: un `enumValues: [{value, labelKey}]` en la superposición
  **sustituye** el array entero. Para `badgeMap` (objeto) funciona por clave. Añadir `value` a
  `IDENTITY_KEYS` en `merge.ts:8` (con su prueba) o usar sólo `badgeMap` en las superposiciones.
- Las claves van como `*Key` junto al literal (`headerKey`, `labelKey`, `titleKey`, `hintKey`,
  `trueLabelKey`…). Comprobar que `normalize.ts` lee todas (`textOf(obj, props)` ya lo hace con
  `${prop}Key`).

**Enfoque.** No escribir 320 cadenas a mano en 8 superposiciones. Un script
`scripts/gen-demo-i18n.mjs` (sin dependencias nuevas):
1. Recorre cada configuración de Go y recoge, por ruta estable
   (`entities.<id>.columns.<campo>.header`, `.sections.<id>.title`, `.fields.<campo>.label|hint`,
   `.options.<campo>.<valor>`, `.filters.<id>.label`, `.commands.<id>.label`), los literales.
2. Escribe la superposición con las `*Key` (en `demo/config/entities-overlays/<id>.json`, que
   `manifest.json` referencia o que el plugin `karpo-config-plugin` mezcla) y el diccionario `es`
   (los literales de Go, sin tocar).
3. El diccionario `en` sale de una tabla revisada a mano (`demo/config/i18n/en.go.json`): el script
   **falla** si falta una clave en `en` (es la prueba de cobertura).
Es lo mismo que TC-3 pedirá al pack de Theros, así que sirve de prueba de concepto de la
convención de claves; anotarlo en `PETICIONES-THEROS-FRONT.md` (TC-3) con la convención usada.
Los valores de catálogo del detalle («Room», «Building», mejora 8) son datos del servidor
(`typeName`): se resuelven con una tabla de traducción de valores de catálogo en la demo (el MSW
del modo demo ya devuelve los datos; que devuelva `typeName` por idioma con `Accept-Language`) o,
con Go real, quedan como petición al backend en `PETICIONES-BACKEND.md`.

### Prueba e2e en inglés

Nueva `e2e/ingles.e2e.ts` con `lng=en`: para cada pantalla de `SCREENS` (las del i18n e2e):
1. Texto visible y atributos de accesibilidad (reutilizar `pageTexts` de `i18n.e2e.ts`).
2. Construir el conjunto de literales españoles de las 8 configuraciones de Go (leer los JSON de
   `../angular/config/entities`, quedarse con `header|label|title|hint|trueLabel|…` y valores de
   `badgeMap`) y fallar si alguno aparece tal cual, salvo los que coinciden con la traducción
   inglesa (nombres propios, «SKU»). Así detecta justo lo que el jurado vio, y el literal que se
   cuele en cualquier pantalla futura.
3. Afirmaciones concretas: leyenda del mapa sin «instalaciones»; insignias del organigrama en
   `Filled|Open|Planned|Closed`; cabeceras de `/product` en inglés.
Mover `signIn` a inglés ya funciona (`openDemoPage({ lng: 'en' })`).

---

## B-3. Tipografía fija en el mapa **[medido]**

**Causa.** `src/map/map.css:12` importa `maplibre-gl.css` en la subcapa `karpo.components.maplibre`
y esa hoja trae `.maplibregl-map{font:12px/20px "Helvetica Neue",Arial,Helvetica,sans-serif}`. El
contenedor del mapa (`[data-kx-part="map"]`) recibe la clase `maplibregl-map`, de modo que los
rótulos (`.kx-map-label`, marcadores HTML hijos) heredan esa fuente. Sondeo:
`getComputedStyle('.kx-map-label').fontFamily` = `"Helvetica Neue", Arial, Helvetica, sans-serif`.
`.kx-map-label` fija `font-size` y `line-height` desde el tema pero no la familia.

**Enfoque.** En `map.css`, dentro de `@layer karpo.components` (que gana a su subcapa
`.maplibre` por ser la capa padre sin anidar): `[data-kx-part="map"].maplibregl-map { font:
inherit; }`. Así hereda del contenedor, que hereda de `body` (el tema). Revisar el resto de
reglas de `maplibre-gl.css` por si fijan `color`, `font-*` o `font-family` en controles
(`.maplibregl-ctrl*`, `.maplibregl-popup-content`, `.maplibregl-ctrl-attrib`): por el momento se
ocultan los controles, pero la atribución puede verse.
**Prueba.** (a) Unitaria: leer `map.css` y exigir que contiene una regla sobre `.maplibregl-map`
con `font: inherit` (barata, protege contra la próxima actualización de MapLibre). (b) E2E en
`mapa.e2e.ts`: la `font-family` calculada de `.kx-map-label` es igual a la de `body`. Añadirlo a
la regla de stylelint si se puede (`declaration-property-value-disallowed-list` para `font` con
literales en `src/**`, excluyendo `maplibre-gl.css` que es de terceros).

---

## B-4. Tab en la cuadrícula **[medido]**

**Causa.** El tabindex itinerante está bien (`GridCell.tsx:96`, `GridRow.tsx:108/150`,
`GridTable.tsx:165/284/310`: una sola parada). El atrapado lo causa el manejador de teclado:
`keyboard.ts:60` define `Tab` dentro de `nextPosition`, `isNavigationKey` (`:86`) lo incluye, y
`GridView.tsx:837-842` hace `preventDefault()` + `moveTo(next)` siempre que haya celda siguiente;
sólo en la última celda devuelve `null` y deja salir. Sondeo: clic en una celda de `/product` y 12
`Tab` → el foco sigue en `[role=grid]`, columna 7.

**Enfoque (patrón APG: una parada de tabulación).**
1. `nextPosition` deja de conocer `Tab` (caso eliminado, devuelve `null` por `default`);
   `isNavigationKey` ya no lo incluye. En modo navegación `onKeyDown` no intercepta `Tab` y el
   navegador mueve el foco al siguiente elemento tabulable (celda activa → fuera). Mayús+Tab
   sale hacia atrás, a la barra de herramientas.
2. En edición, `Tab` confirma y pasa a la siguiente celda: ya lo hace `editor-props.ts:44`
   (`move: 'next'|'prev'`) con `afterCommit` (`keyboard.ts:124`). Mantener ese comportamiento, pero
   `afterCommit` no puede llamar a `nextPosition({key:'Tab'})`: extraer una función
   `stepCell(pos, dir, dims)` con la lógica actual y usarla sólo desde `afterCommit`.
   En la última celda editable `afterCommit` devuelve la misma posición (hoy `?? pos`): comprobar
   que en ese caso el foco vuelve a la celda y no se pierde.
3. `MultiSelectEditor.tsx:90` maneja `Tab` por su cuenta: revisar que sigue cerrando el editor y
   confirmando.
4. Ayuda y atajos: `grid/shortcuts.tsx:17` documenta `next` = `Tab`; reescribir las etiquetas
   (`Tab`: «Confirmar y pasar a la celda siguiente (sólo al editar)»; fuera de edición, «Salir de
   la cuadrícula»), y los textos de `locales/*/grid.json` y de la ayuda `grid.table`.
**Pruebas.** Unitaria `keyboard.test.ts:53` (hoy exige que `Tab` recorra celdas): invertirla
(`Tab` y `Shift+Tab` devuelven `null`) y probar `afterCommit` aparte. En `GridEditing.test.tsx`
los `{Tab}` de `edit()` siguen valiendo (están en edición). Cambiar `e2e/rejilla.e2e.ts:195-199`
(hoy exige que Tab recorra celdas) y añadir: con el foco en una celda, `Tab` ×3 saca el foco de
`[role=grid]` (afirmar `!document.activeElement.closest('[role=grid]')` en ≤ 3 pulsaciones, no
60) y `Shift+Tab` vuelve a la celda activa; con una celda en edición, `Tab` confirma y deja el foco
dentro, en la celda siguiente.

---

## B-5. Reflujo en móvil **[medido]**

Sondeo a 390 px con `lng=en` (se entra a 1440 y se reduce la ventana; ver nota de prueba):

| Ruta | `scrollWidth` | Desbordan |
|---|---|---|
| `/facility` | 595 | 6 botones de la barra superior (hasta `r=595`), `[head]` de la cuadrícula (733) |
| `/position?view=tree` | 549 | barra superior y `search-status` (437) |
| `/facility?view=map` | 595 | barra superior |
| `/product` | 453 | barra superior (usuario, `r=453`) y cabeceras de la cuadrícula (`head` 933: scroll propio, ver abajo) |

El jurado midió 453 en `/product`; en las vistas con selector de vista llega a 595. El `h1` mide
**0 px**: el título desaparece porque `min-inline-size: 0; overflow: hidden` lo comprime a nada.

**Causa.** `TopBar.tsx` pone 11 controles en una fila sin modo compacto: alternar menú, título,
selector de vista (3 botones), paleta, ayuda, tema, modo, idioma, usuario. `shell.css:543-565` sólo
oculta etiquetas con contenedor ≤ 80em; en ≤ 64em sólo cambia el menú a cajón. `Ctrl K` (`Kbd`)
se pinta siempre en `palette-button` (`TopBar.tsx:60`). Los iconos del tema y del modo «sistema» son
el mismo (`MODE_ICON.system = 'sparkles'` y `ThemeMenu icon="sparkles"`): los «dos iconos casi
iguales».

**Enfoque.**
1. Barra compacta con `@container kx-shell (max-width: 40em)` (≈ 640 px; el 390 queda dentro con
   holgura): título primero (`flex: 1 1 auto`, `min-inline-size: 6ch`, elipsis), `nav-toggle`, y a
   la derecha sólo `paleta` (icono), `ayuda` y un menú `Más` (`DropdownMenu`, icono
   `dots-three`). Dentro de `Más`: tema, modo, idioma, atajos, cambiar clave, cerrar sesión. Las
   tres opciones (tema, modo, idioma) pueden reutilizar `ChoiceMenu` como submenús
   (`DropdownMenu.Sub`) o grupos de radio. Un CSS puro (ocultar con `display:none`) dejaría los
   controles en el árbol de accesibilidad; mejor un hook `useCompact()` con `ResizeObserver` sobre
   `[data-kx-shell]` (el contenedor ya se llama `kx-shell`) y renderizar un árbol u otro.
2. El selector de vista (3 botones) pasa a un único control compacto (icono de la vista actual y
   menú) en compacto; ver mejora 6, que lo unifica.
3. `Kbd`: `@media (hover: none), (pointer: coarse) { [data-kx-ui="shortcut-keys"] { display:none } }`
   en `ui.css` y además fuera en compacto (la emulación de Playwright sin `hasTouch` no activa
   `pointer: coarse`). `aria-keyshortcuts` se mantiene.
4. Icono del modo «sistema»: usar un icono distinto del de tema (`monitor`, `desktop` de Phosphor;
   añadirlo a `ICON_NAMES`) y `palette` para el tema.
5. `search-status` del organigrama y cabeceras de cuadrícula: el `head` de 933 px está dentro del
   `viewport` con scroll propio (aceptable, WCAG 1.4.10 excepciona las tablas de datos), pero
   comprobar que `[data-kx-part="viewport"]` tiene `overflow: auto` y que el desbordamiento de la
   página sale sólo de la barra superior. El `search-status` debe poder partir línea
   (`overflow-wrap: anywhere`).
6. Detalle y panel: a 390 px el panel de detalle (redimensionable) debería pasar a página
   completa (`asPage`) por debajo de 64em en vez de partir la pantalla; comprobar con
   `react-resizable-panels` **[por verificar]**.
**Pruebas.** E2E nueva `e2e/movil.e2e.ts` a 360 y 390 px (`isMobile: true, hasTouch: true`, lo que
requiere un parámetro `viewport/hasTouch/isMobile` en `openDemoPage`): en `/facility`,
`/position?view=tree`, `/facility?view=map`, `/product` y `/`, `document.documentElement.scrollWidth
<= innerWidth`, el `h1` mide > 0 y tiene texto, no hay `kbd` visible, y los botones de la barra son
accesibles por teclado (el menú `Más` abre y cierra). **Nota:** `signIn` de `e2e/lib/demo.mjs:213`
espera `nav[data-th="nav"]` visible y a ≤ 1024 px el menú es un cajón oculto: entrar a 1440 y luego
`setViewportSize`, o esperar `state: 'attached'`.
Capturas: añadir escena móvil 390×844 (ver mejora 20).

---

## Hallazgo nuevo: `:dir(rtl)` no funciona **[medido]**

**Causa.** El CSS compilado trae `:lang(ar),:lang(he),:lang(ku)…) svg[data-th-icon]
[data-kx-mirror=true]{transform:scaleX(-1)}`: lightningcss reescribe `:dir(rtl)` a una lista de
`:lang()` para los navegadores objetivo. La aplicación pone `dir` en `<html>` (`I18nProvider.tsx:100`,
también forzado con `?dir=rtl` con `lang=en`), pero `:lang(en)` no coincide con esa lista. Sondeo con
`dir=rtl`: los chevrons con `data-kx-mirror="true"` tienen `transform: none`. Hay tres usos:
`ui.css:20` (espejo de iconos), `grid.css:308` (marca de celda sucia) y `shell.css:582` (cajón del
menú en RTL).

**Enfoque.** Sustituir por `[dir="rtl"] …` (o `:where([dir="rtl"]) …` para no subir la
especificidad); `<html dir>` es lo que el proveedor garantiza. Añadir a `stylelint.config.js` una
regla que prohíba `:dir(` (selector-pseudo-class-disallowed-list) y una prueba que compile
`src/**/*.css` con lightningcss con los mismos `targets` de Vite y falle si aparece `:lang(` con
`ar` en la salida. **Prueba e2e** en `rtl.e2e.ts`: los iconos con `data-kx-mirror` tienen
`transform` ≠ `none` con `dir=rtl` y `none` con `ltr`. Esto cubre la mejora 11 (chevrons de las
migas y del árbol): `MapBreadcrumbs` y `TreeList` ya marcan los iconos como direccionales
(`chevron-right` es `directional`); sólo falta que el CSS funcione.

---

## Mejoras (por orden de impacto en la nota)

### 1. Organigrama legible
**Causa.** `TreeDiagram.tsx:298-306`: `fit()` llama a `getViewportForBounds(bounds, w, h, MIN_ZOOM,
1, FIT_PADDING)` con `MIN_ZOOM = 0.1`, así que un árbol grande se reduce hasta escala ~0,6 o menos.
`:357-362` ya usa un mínimo de 0,6 al centrar un nodo. El `«14+»` (`:170`) es
`count.partial ? '${count}+'`: descendientes cargados y quedan ramas sin cargar; no se explica.
**Enfoque.** (a) Mínimo del ajuste a la vista: `FIT_MIN_ZOOM = 0.85`. Si el árbol no cabe, centrar
en la raíz (esquina superior central) y dejar el resto para desplazar; el botón «Ajustar» alterna
entre «ajustar a lo que cabe» (0,85) y «todo» (`MIN_ZOOM`). (b) Zoom semántico: `useStore(zoom)` de
@xyflow expone el zoom; por debajo de 0,6 `data-kx-zoom="far"` en el contenedor y CSS que oculta
subtítulo e insignia y sube el tamaño del título con `calc(1em / var(--kx-zoom))` (una variable de
disposición por CSSOM, como las demás). Mantener el texto ≥ 11 px efectivos. (c) Contador: el `+`
pasa a `≥14` con `title`/`aria-label` «al menos 14 descendientes; hay ramas sin cargar» (ya existe
`descendantsPartial` en `tree.json`, falta usarlo en el visible) y `<bdi>`. **Prueba.** E2E: tras el
ajuste inicial, el tamaño de fuente calculado de `[data-kx-part=label]` × zoom ≥ 11 px.

### 2. Coropletas y «sin dato»
**Causa.** `theme/kx-ext.css:68-86`: `--kx-data-seq-1` = 12 % de `info-strong` sobre
`surface-content-bg`; `--kx-data-missing` = `surface-layer-level3`. En claro ambos son grises muy
cercanos; en oscuro `level3` es más claro que el fondo y el 12 % de azul queda **por debajo** de
`level3` en luminancia: el orden se invierte.
**Enfoque.** (a) «Sin dato» siempre con trama: en la leyenda, un `repeating-linear-gradient` con
tokens (`--kx-data-missing` y `--kx-data-border`), y en el mapa, `fill-pattern` de MapLibre con una
imagen generada al arrancar con un `<canvas>` desde los colores resueltos (`resolveThemeColor`),
registrada con `map.addImage`; se regenera al cambiar de tema o modo. Así no hace falta que el color
distinga por sí solo. (b) Subir `seq-1` al 18-22 % y dar a `missing` una luminancia fuera de la
escala (más neutro: `neutral` mezclado con la superficie). (c) Prueba de contrato (nueva, en
`theme/`): para cada tema (`modern-flat`, `classic-1995`, `glossy-2003`, `souq`) y modo, calcular
diferencia de luminancia relativa/ΔL en oklab entre `missing` y `seq-1` y exigir un mínimo
(propuesta: ΔL ≥ 0,04 y orden monótono `seq-1 < … < seq-7` en el mismo sentido en ambos modos).
Cruza con PT-5 de `PETICIONES-THEROS-FRONT.md`.

### 3. Mapa, nivel país
**Causas.** `labels.ts:58` estima el rótulo con `CHAR_WIDTH = 7` px por carácter, y `pickLabels`
(`:30`) exige que quepa el 60 % del rótulo (`MIN_FIT`), pero el rótulo real (fuente del tema,
negrita) mide otra cosa y su CSS permite `max-inline-size` de 3,5 × `space-8`, de modo que
«Comunidad Foral de Navarra» desborda su tesela. Dos teselas del norte sin rótulo: se descartan por
caber menos del 60 % o porque otro rótulo ya ocupa el sitio. La leyenda (`MapLegend`, absoluta,
`map.css:273`) tapa Canarias porque `fitBounds` (`MapCanvas.tsx:216-225`) rellena con
`--th-space-8` por todos lados, no con el tamaño de la leyenda.
**Enfoque.** (a) Medir de verdad: `CanvasRenderingContext2D.measureText` con la fuente calculada del
rótulo (`getComputedStyle(label).font`), cacheado por (texto, fuente); partir en líneas con ese
ancho. (b) Si no cabe el nombre completo, usar un nombre corto: el `shortName` o el código
(«NA», «CE», «MU») con el nombre completo en `title` y en la lista accesible; para ello
`names.ts` acepta un campo opcional `shortName` en las propiedades de la geometría. (c) Un solo
sitio de colocación: el «polo de inaccesibilidad» (centro del mayor círculo inscrito) en lugar del
centroide, con `polylabel` (ISC, no directa: sería dependencia **directa** con licencia ISC →
consulta a Javier) o un cálculo propio de ~40 líneas sobre el contorno ya proyectado; la segunda
opción evita la consulta. (d) `fitBounds` con relleno por lados: `{top, bottom, start, end}` donde
el lado de la leyenda suma su `offsetWidth/offsetHeight` (medido con `ResizeObserver`); en RTL
cambia de lado. (e) Rótulos de las teselas sin rótulo: tras (a) y (b) deben caber; la prueba lo
exige (todo nivel con ≤ 20 elementos rotula ≥ 90 % de ellos).

### 4. Mapa, nivel región con un solo elemento
**Causa.** `use-map-level.ts`/`nav.ts` bajan siempre al nivel siguiente aunque tenga una única
delimitación; `fitBounds` con `maxZoom: FIT_MAX_ZOOM` pinta ese elemento a pantalla completa.
**Enfoque.** En `nav.ts`, un paso `skipSingle`: si el nivel al que se baja tiene un solo elemento,
saltar al siguiente con migas que lo reflejan, o conservarlo con zoom limitado
(`maxZoom` = zoom que cabe al elemento a ≤ 60 % del lienzo) y el estado «1 elemento» en el texto
de estado. Decisión de producto: recomendado saltarlo y mostrar sus datos en la tabla accesible.
Con un solo elemento no tiene sentido una coropleta (la leyenda de 7 pasos se reduce a «Con datos»).

### 5. Geometría
Propuesta para Javier (no ejecutar sin su visto bueno): Natural Earth (dominio público) para
mundo y admin-1, convertido con un script propio (`scripts/gen-geo.mjs` ya genera GeoJSON y
`build-pmtiles.mjs` PMTiles) a un PMTiles de ~1-2 MB, alojado por el despliegue. Natural Earth no se
puede descargar sin permiso explícito (norma de descargas): se pide a Javier el fichero o la
autorización, o se toma de un paquete npm con licencia permitida si existe (p. ej. los paquetes
`world-atlas` y `topojson-client` son ISC, no entran como directas sin consulta). Registrar como
consulta **Q-nueva** en `PLAN.md` y en `PETICIONES-BACKEND.md` (servicio de teselas propio).
España por debajo de admin-1 (CCAA y provincias) no está en Natural Earth a escala útil: seguiría
siendo esquemática, rotulada como hasta ahora.

### 6. Un solo control de vista
**Causa.** `TopBar.ViewSwitch` (`Listado | Organigrama | Mapa`, `config.views`) y `LayoutSwitch`
(`tree/LayoutSwitch.tsx`) dentro de `TreeView.tsx:184` («Listado | Diagrama») y `MapView.tsx:205`
(«Mapa | Listado») son dos niveles de la misma idea: vista de la entidad × disposición de esa
vista.
**Enfoque.** Un único control segmentado en la cabecera de la entidad (ver 7) con una opción por
**par vista/disposición**: `Tabla`, `Árbol` (tree+list), `Diagrama` (tree+diagram), `Mapa`
(map+map) y la tabla accesible del mapa pasa a ser el botón «Ver como tabla» dentro del mapa (es una
alternativa accesible, no una vista). El estado sigue en la URL (`view` y `layout`). Quitar
`ViewSwitch` de `TopBar` (también ayuda a B-5) y reutilizar `LayoutSwitch` como componente
(`role=radiogroup`, con `aria-pressed` hoy). Actualizar ayuda (`core.view-switch`, `tree.switch`,
`map.switch`) y las e2e que buscan «Listado» (`organigrama.e2e.ts`, `mapa.e2e.ts`).

### 7. Botón «Nuevo» y contador
**Causa.** `GridView.tsx:1028-1035`: el botón «Añadir fila» sólo aparece si todos los campos
obligatorios del formulario tienen columna editable en la rejilla (`canCreate`), lo que no pasa en
instalaciones, centros de trabajo ni proveedores; para esas entidades la creación es sólo Ctrl+K
→ `/<entity>/nuevo`. No hay cabecera de lista con el recuento.
**Enfoque.** Una cabecera de entidad (`EntityHeader`, en `EntityScreen.tsx` encima del grupo de
paneles): título, recuento («248 instalaciones», con `Intl.PluralRules`/i18next plural, y «de
1.204» si hay filtro; el total ya lo trae la lista paginada), el control de vista (6) y la acción
primaria **«Nuevo»** (`kind=primary`, icono `plus`) que navega a `{entity, create: true}` si
`config.form?.create && config.editable` y la sesión tiene permiso de escritura (la regla de
`permissions.write` ya se evalúa en `menu.ts:canWriteEntity` **[por verificar el nombre]**). El
«Añadir fila» de la rejilla sigue para las entidades editables en línea, renombrado «Añadir fila» (y
sin competir: el primario es «Nuevo»). Atajo `N` o `C` documentado. En compacto, «Nuevo» sigue
visible (es la acción principal).

### 8. Panel de detalle
- **Estado cortado por la columna de acciones fijada.** `grid.css:131-139,434-439`: columna
  `__actions` sticky con `--kx-trail`; la columna «Estado» de la lista se solapa porque la anchura
  de la rejilla no incluye `--kx-actions-width` en el cálculo de la última columna, o porque la
  insignia (`white-space: nowrap`, ancho fijo 130) se parte al reducirse el panel al 55 %.
  **Enfoque:** `min-inline-size` de columna = contenido de la insignia (`fit-content`), `padding`
  derecho de `--kx-trail` en la última columna de datos y sombra de borde cuando hay scroll. **[por
  verificar con captura a 55 %]**.
- **Pistas (`hint`) no se pintan.** Causa: en las configuraciones de Go, `hint` sólo está en
  `form.sections[].fields[]` (instalaciones, líneas 39-40), mientras que las secciones de `detail`
  (lo que se ve en lectura y edición) no las llevan; `DetailView.sectionsForCreate` sólo usa
  `form` en el alta. **Enfoque:** al normalizar, heredar `hint` (y `placeholder`, `maxLength`, `help`)
  del campo homónimo del formulario cuando el de `detail` no los tenga (en `normalize.ts`, tras
  construir ambos), y avisar con `info`. Prueba de componente: ficha en edición muestra
  `[data-th=hint]` con `aria-describedby`.
- **`columns: 2` no se nota.** Causa: `detail.css:180` `@container kx-sheet (max-width: 36em)`
  colapsa a una columna y el panel de detalle (45 % de 1440 ≈ 600 px menos relleno) cae justo por
  debajo; `fieldSpan` (`form-model.ts:145`) sí calcula `span 6`. **Enfoque:** bajar el umbral a
  ≈ 28em (448 px) o escalonar (2 columnas desde 28em, `columns: 3` desde 44em) y probarlo con el
  panel a su tamaño por omisión. **[medir el ancho real del contenedor `kx-sheet`]**.
- **Espacio muerto bajo el formulario.** Aprovecharlo con pestañas/secciones plegables colapsadas
  por defecto o colocar el historial; mínimo: `align-content: start` y que el pie de acciones sea
  `position: sticky` abajo.
- **Valores de catálogo sin traducir** («Room», «Building»): ver B-2 (c).

### 9. Barra de lote
**Causa.** `grid.css:57-72`: la barra `batch-bar` usa `--kx-cell-dirty-bg` y borde
`--kx-cell-dirty-mark` a todo el ancho (`display: flex` de bloque). Deshacer/rehacer (`GridView.tsx
1156-1167`) son `Action kind="quiet"` y el «Descartar» es `secondary`: estilos mezclados.
**Enfoque.** Barra flotante compacta (`position: sticky; inset-block-end`, `inline-size:
fit-content`, centrada, radio `--th-edge-rounding-large`, elevación `--th-depth-levels-overlay`,
fondo `surface-popover`, con un punto del color de «sucio» en lugar del fondo ámbar entero); botones
iguales (`secondary` para deshacer/rehacer, `primary` sólo Guardar). Mover deshacer/rehacer a un
grupo (`role=group`) de la barra superior de la cuadrícula con el mismo estilo `quiet` y
`aria-label`. Cuidado con el solape con la última fila: `padding-block-end` del viewport igual al
alto de la barra.

### 10. Textos pegados al borde
`map.css`: los `<p>` de estado («Mundo: 248 delimitaciones.») y `[data-kx-part="notice"]` no llevan
`padding-inline`. Añadir `padding-inline: var(--th-space-3)` (o el `--th-space-margin`) y
`overflow-wrap: anywhere`. Un `stylelint` ya impide literales, así que sólo token.

### 11. RTL
Ver «Hallazgo nuevo»: los chevrons se arreglan con `[dir="rtl"]`.

### 12. Cadenas mezcladas en RTL
**Causa.** Cadenas i18next con números interpolados («changes {{count}}», «to {{to}} {{from}}») se
pintan sin aislamiento bidi. **Enfoque.** (a) Interpolar con `<bdi>`: i18next admite
`<Trans components={{ num: <bdi /> }}>` o un formateador propio `{{count, bdi}}` que devuelve
`⁨…⁩` (FSI/PDI, sin elemento); lo más simple y compatible con `textContent` y lectores de
pantalla: un formateador en `createI18n.ts` que envuelve números y rangos en isolates Unicode en
cualquier idioma con `dir=rtl` (en LTR no se aplica para no ensuciar los textos de pruebas). (b)
Rangos con `Intl.NumberFormat#formatRange` (leyenda del mapa, `legend.range`), en el
formateador compartido de `i18n/formatters.ts`; `formatRange` está en Edge ≥ 106, y en el entorno
de pruebas jsdom/Node 24 también. (c) Nodos con texto libre del usuario o del servidor
(nombre de un puesto, de una delimitación) en `dir="auto"` o `<bdi>`. **Prueba.** E2E en
`rtl.e2e.ts` con árabe real (`lng` ar si hay diccionario; si no, `?dir=rtl`) y comprobación de los
isolates en las 5 cadenas citadas por el jurado.

### 13. Nodos del diagrama sin `role`
**Causa.** `CardNode` (`TreeDiagram.tsx:128`) es un `div` dentro de la región `role="region"` con
`aria-roledescription`; la alternativa accesible es la lista `role=tree`. **Enfoque.** Los nodos
son botones con `aria-pressed` y `tabIndex`: conservar eso y dar al contenedor de cada tarjeta
`role="group"` con `aria-label` = nombre del nodo, o, más limpio, marcar el lienzo
`aria-hidden="true"` (los botones siguen tabulables, así que mejor `inert` **no**) y ofrecer un
enlace «Ir a la lista» (`href="#tree-list"`) con `aria-describedby`. Recomendado: `role="group"`
+ `aria-roledescription` por nodo + enlace «Ver como lista». Añadir prueba axe-like propia (sin
axe-core: licencia MPL) comprobando roles y nombres accesibles de todos los controles del diagrama.

### 14. Contraste automático
**Enfoque.** Prueba de contrato `src/theme/contrast.contract.test.ts` que, para cada tema del
pack compilado en `theme/` (`scripts/sync-themes.mjs`) y modo claro/oscuro, resuelve los pares
(texto, fondo) usados por la interfaz (lista cerrada por tabla: `text.primary/surface.content`,
`text.secondary/…`, acciones, insignias por tono, `kx-cell-dirty`, leyenda, etc.) a oklab y
exige ≥ 4,5:1 (texto) o ≥ 3:1 (no texto). Sin dependencias: la conversión sRGB→luminancia son 25
líneas (`resolveThemeColor` ya devuelve rgb). Las acciones salen a 4,52:1, así que cualquier
cambio de token la rompería: la prueba es justamente eso. Cubre también la mejora 2.

### 15. Rendimiento: 1,41 MB / 412 kB gzip **[medido]**
Composición del trozo inicial (tamaño de fuente sin minificar, del mapa de fuentes): `react-dom`
620 kB, código propio 548, **Phosphor 451**, `react-router` 377, **zod 361**,
`react-resizable-panels` 244, `react-hook-form` 155, `@tanstack/query-core` 146, i18next 94,
fuse.js 49, driver.js 46. (El otro trozo grande de `demo/dist`, `browser-*.js` de 489 kB, es MSW:
sólo en modo demo y fuera del presupuesto; `maplibre-gl` 1,1 MB y `tree` 320 kB ya son diferidos.)
Objetivo ≤ 250 kB gzip. Palancas por orden de ahorro estimado **[estimado, medir con el mismo
script tras cada cambio]**:
1. **Phosphor (−60 a −80 kB gzip).** `@phosphor-icons/react/dist/csr/*` incluye los seis pesos de
   cada icono. Alternativa MIT dentro de la lista de licencias: `@phosphor-icons/core` (MIT, SVGs por
   peso) como **dependencia de desarrollo** y un script `scripts/gen-icons.mjs` que emite
   `src/ui/icon-data.ts` con sólo `regular` y `fill` (los dos que usa `Icon` según `iconStyle`) y un
   componente `<svg>` propio de 30 líneas (path data en una cadena). 120 iconos × 2 pesos ≈ 40 kB
   sin comprimir. Mantiene `ICON_NAMES` y los alias. Cuidado con B-1: hacerlo **después** de
   B-1, con la misma lista como fuente única.
2. **Detalle, paleta y ayuda en diferido (−40 a −60 kB).** `EntityScreen.tsx` importa `DetailView`
   (formulario, `react-hook-form`, `zod`, `@hookform/resolvers`) de forma estática: pasar a `lazy`
   con `Suspense` (el panel de detalle ya tiene `Skeleton`). `CommandPalette`/`cmdk`/`fuse.js`
   (`shell/CommandPalette.tsx`, `palette-items.ts`) con `lazy` al primer Ctrl+K (precargar al
   `focus`/`pointerenter` del botón de la paleta). `driver.js` (`help/runtime.ts`) con `import()`
   dentro de `startTour`.
3. **zod (−25 a −35 kB).** `config/schema.ts` y `detail/form-model.ts` importan `zod` completo. Pasar
   a `zod/mini` (misma licencia, API funcional) o, para `form-model.ts`, sustituir el esquema por
   funciones de validación propias (el normalizador ya conoce `min/max/pattern`); `schema.ts` puede
   quedarse con `zod/mini`. `@hookform/resolvers` se retira si se hace lo segundo.
4. **`react-router` (−20 kB).** Si sólo se usa `BrowserRouter/Routes/useNavigate`, comprobar que se
   importa desde el módulo de DOM y no del barril (v8): sin `Link`/`data routers` no hace falta el
   resto.
5. `manualChunks` en `vite.config.ts` para `react-*` y vendor estable (caché a largo plazo, no
   reduce el primer trozo).
**Prueba.** `scripts/presupuesto.mjs` (nueva, en `npm run check`): lee `demo/dist/assets`, comprime
con `zlib.gzipSync` el trozo `index-*.js` y falla si > 250 kB gzip (umbral configurable); lo mismo
para `dist/karpo-react.js` del paquete (404 kB hoy sin comprimir). Aviso, no error, mientras no se
llegue (para que el integrador pueda ir bajando).

### 16. `dist/karpo.css` con la hoja de MapLibre
**Causa.** `map.css:12` hace `@import url("maplibre-gl/dist/maplibre-gl.css")` y `src/map/index.ts:3`
importa `map.css`; `vite.lib.config.ts` tiene `cssCodeSplit: false`, así que `dist/karpo.css`
(142 kB) la lleva siempre. **Enfoque.** Sacar el `@import` a `src/map/maplibre.css` y generar una
segunda hoja `dist/karpo-map.css` (exportada como `@karpo/react/map.css`): un paso posterior a
`vite build` en `scripts/` que concatena `maplibre-gl.css` envuelta en
`@layer karpo.components.maplibre { … }` más las reglas de mapa. En la demo (build de aplicación,
con división de CSS) basta `import('./maplibre.css')` junto a `import('maplibre-gl')` en
`map/engine.ts`, así sólo se descarga con la vista del mapa. Actualizar `exports`, `files`,
`pack-check.mjs` (`STYLESHEET_MARKERS` quita `[data-kx-view=map]`, `REQUIRED_FILES` añade
`dist/karpo-map.css`), README y la prueba de B-3 (que debe leer la hoja nueva). Documentar que el
consumidor que use mapas importa las dos hojas.

### 17. Columnas no virtualizadas
Anotarlo en `PLAN.md` §3 y en el README (límite conocido: ≤ ~40 columnas; filas sí virtualizadas).
Sin cambio de código en esta ronda. Si se quisiera: `@tanstack/react-virtual` ya está, `horizontal:
true` con la columna activa y las fijadas siempre renderizadas.

### 18. `engines.node`
**Hecho medido.** El aviso `EBADENGINE` lo producen las **dependencias de desarrollo**, no el
`engines` del paquete: `jsdom 31` (`^22.22.2 || ^24.15.0 || >=26`), `stylelint 18` y similares piden
24.15. Cambiar `engines` en `package.json` no lo quita con Node 24.14.1. Los demás piden bastante
menos: Vite 8 `^20.19 || >=22.12`, vitest 5 `^22.12 || ^24 || >=26`, playwright-core `>=20`.
**Enfoque.** Separar lo que importa (a) **para quien instala el paquete**: no pedir nada a Node
(es una librería de navegador): quitar `engines` del paquete o `>=20`, y (b) **para desarrollar**:
`devEngines` (npm ≥ 10.9) con `>=24.15` y la nota en README/CONTRIBUTING de actualizar Node
(`24.15.0` está publicada). No bajar `jsdom` ni `stylelint` (rompería el lockfile confirmado).
Decir la verdad en el README: «con 24.14 `npm ci` avisa pero funciona; las pruebas de componentes
requieren 24.15» (hay que confirmar que funcionan: el jurado ejecutó `npm run check` con 24.14.1
con código 0).

### 19. `docs/LICENCIAS.md` y Q-6
Líneas 132 y 155-156 dicen «Aceptada en Q-6» y «consulta Q-6, aceptada». Q-6 sigue abierta
(`PLAN.md:88`). Redactar: «`lightningcss` (MPL-2.0, transitiva de desarrollo de Vite): se aplica por
defecto mientras Javier no decida Q-6; no se distribuye con el paquete. Si Javier la rechaza, la
alternativa es `cssMinify: 'esbuild'` y sin `css.transformer: 'lightningcss'`, con la pérdida de
`light-dark()`/anidamiento transformado» **[por verificar qué usa Vite 8 por omisión]**. Cambiar en
`scripts/licencias.mjs` el texto que genera esas líneas (es un informe generado: no editar sólo a
mano) y su prueba `licencias.test.mjs`.
**Ojo nuevo:** el hallazgo `:dir()` viene de lightningcss; al corregirlo, anotarlo en
`LICENCIAS.md`/README (los `targets` de lightningcss afectan al CSS que se publica).

### 20. Capturas e pruebas e2e por bloqueante
**Capturas.** `scripts/capturas.mjs`: `VARIANTS` pasa de 3 a 5: `ingles` (`lng=en`, LTR, claro,
1440×900) y `movil` (390×844, `isMobile: true`, `hasTouch: true`, es, claro). No se pueden hacer las
9 escenas × 5 sin coste (45 PNG): proponer `ingles` en las 9 y `movil` en 5 (cuadrícula, detalle,
organigrama listado, mapa mundo, ayuda). Para `movil`, `openDemoPage` necesita
`viewport`, `isMobile`, `hasTouch` (hoy fija `deviceScaleFactor: 1` y 1440×900 por omisión) y
`signIn` debe aceptar el menú como cajón. Regenerar el `README.md` de capturas (el script ya lo
escribe) y comprobar que las 27 anteriores se regeneran idénticas salvo lo corregido.
**Pruebas e2e nuevas (una por bloqueante, rojas hoy):**

| Bloqueante | Fichero | Qué falla hoy |
|---|---|---|
| B-1 | `e2e/menu.e2e.ts` (nuevo) | 7 entradas con `[data-kx-icon="missing"]` o con el glifo de círculo |
| B-2 | `e2e/ingles.e2e.ts` (nuevo) | Leyenda «instalaciones», insignias «Cubierto», cabeceras en español |
| B-3 | `e2e/mapa.e2e.ts` | `font-family` de `.kx-map-label` ≠ la del `body` |
| B-4 | `e2e/rejilla.e2e.ts` | Tras 3 `Tab` el foco sigue en `[role=grid]` |
| B-5 | `e2e/movil.e2e.ts` (nuevo) | `scrollWidth` 453-595 > 390; `h1` de ancho 0 |
| `:dir()` | `e2e/rtl.e2e.ts` | `transform: none` en iconos con `data-kx-mirror` y `dir=rtl` |

Más las unitarias/contrato citadas: iconos (contrato), `keyboard.test.ts`, regla de `font` en
`map.css`, contraste de tokens, presupuesto de bytes, `:dir(` ausente en el CSS compilado.

---

## Cambios en la documentación de peticiones

- `PETICIONES-THEROS-FRONT.md`: TC-3 con la convención de claves usada por `gen-demo-i18n.mjs`;
  PT-5 con la prueba ΔL de `missing` frente a `seq-1`; nueva petición para el icono: nombres de
  Phosphor en el contrato (`look.icons`); nueva petición: `:dir(rtl)` frente a `lightningcss` en el
  CSS que emite Theros Front (si Theros usa `:dir()` en sus hojas, la compilación aquí lo rompería
  igual) **[por verificar en el CSS de `theme/*.css`]**.
- `PETICIONES-BACKEND.md`: valores de catálogo traducibles (`typeName` por idioma o clave),
  recuento total en la cabecera de la lista (`X-Total-Count`/`meta.total`, ya se usa para el
  recuento del encabezado: comprobar que Go lo devuelve), servicio de teselas propio (PMTiles).

## Consultas a Javier que salen de este análisis

1. **Geometría real** (mejora 5): Natural Earth (dominio público) convertido a PMTiles propio; hay
   que traer el fichero o autorizar la descarga. Alternativa por npm (`world-atlas`, ISC) sólo con
   su visto bueno por la regla de licencias directas.
2. **`polylabel`** (ISC) para colocar rótulos: se evita con cálculo propio salvo que diga lo contrario.
3. **Q-6 `lightningcss`**: sigue abierta; la decisión afecta al CSS publicado (ver 19).
4. **Nivel de un solo elemento** (mejora 4): saltarlo (recomendado) o conservarlo con zoom limitado.

## Dependencias entre cambios (para repartir el trabajo)

- B-1 → mejora 15.1 (generar iconos): la lista de nombres es la fuente de ambos.
- B-2 (b) → B-2 (c) → e2e inglés. Sin (b), las superposiciones no se ven en insignias.
- Hallazgo `:dir()` → mejora 11 y las capturas RTL (hay que regenerarlas: cambian el aspecto del
  menú, del árbol, de las migas y de la marca de celda sucia).
- B-5 → mejora 6 (quita el selector de vista de la barra) y 7 (cabecera con «Nuevo»): conviene
  hacer 6 y 7 antes o a la vez para no diseñar el modo compacto dos veces.
- Mejora 16 → B-3 (la regla `font: inherit` y su prueba deben apuntar a la hoja nueva).
- Todo lo que toque CSS: `npm run lint` (stylelint prohíbe literales) y regenerar capturas al final.
