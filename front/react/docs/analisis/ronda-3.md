# Análisis, ronda 3: causas y enfoque contra el informe del jurado

Fecha: 2026-11-27. Base: commit `fe40aa0` (rama `claude/front-react-7b8564`). Papel: analista.
Entrada: `docs/jurado/ronda-2.md` (1 bloqueante, 12 mejoras; nota 8,2). Salida para: integrador de la ronda 3.

Método: lectura del código y mediciones locales. Lo medido se marca **[medido]** (por ejemplo, la
atribución de bytes del trozo inicial con el mapa de fuentes de `demo/dist`); lo que sólo he razonado
leyendo código se marca **[por verificar]**. No he tocado ningún fichero del proyecto salvo este informe.
No he arrancado procesos ni navegadores.

## Resumen ejecutivo

| Punto | Causa real | Dificultad |
|---|---|---|
| B-1 avisos de terceros | Hay **tres** orígenes incrustados (Phosphor, MapLibre CSS, xyflow CSS) y ningún fichero del paquete los atribuye; `licencias.mjs` sólo analiza dependencias, no lo incrustado | Baja-media |
| M-1 título duplicado | `TopBar` pinta `<h1>` y `EntityHeader` `<h2>` con el mismo texto; **además** `Home`, `Forbidden` y `NotFound` pintan su propio `<h1>` bajo el de la barra | Media (toca e2e) |
| M-2 «sin dato» | La capa `kx-missing` pinta la trama a opacidad 1 sobre todo lo que no tiene dato y, sin dato alguno, sobre todo el nivel; el relleno plano ya es casi el lienzo | Baja |
| M-3 columna cortada | La rejilla sólo tiene desplazamiento horizontal: con 4 columnas de 130+ px en un panel del 55 %, la última queda a medias. No hay ningún mecanismo de ajuste | Media |
| M-4 vacío en el detalle | `SheetFooter` va fijo abajo (`margin-block-start: auto`) y la ficha tiene una sola sección corta. La API no devuelve metadatos de auditoría | Media |
| M-5 nivel región | Sólo `enter()` (clic) pasa por `resolveEntry`; entrar por URL no salta el nivel de un solo elemento | Baja |
| M-6 rótulos del mapa | `fitLabel` elige la variante (nombre, corto, código) **por delimitación**: se mezclan | Baja |
| M-7 pulido | Cinco causas distintas, todas CSS o de un componente | Baja |
| M-8 ayuda | Si la configuración trae pasos, **sustituyen** a las anclas; `grid.batch-bar` está `hidden`; el cierre de driver.js es un «×» de texto | Media |
| M-9 casillas táctiles | `<input type="checkbox">` nativo sin tamaño propio | Baja |
| M-10 RTL | No hay ningún idioma RTL en `src/locales`; las capturas fuerzan `dir=rtl` con inglés | Media-alta (volumen de texto) |
| M-11 presupuesto | **El informe se equivoca en parte [medido]**: `react-hook-form` y `zod/mini` **no** están en el trozo inicial. Lo que pesa de verdad es `icon-data.ts` (~22 kB gzip), los idiomas cargados de golpe y la pantalla de acceso | Media |
| M-12 consultas | Q-1 a Q-10 y la atribución de xyflow siguen sin respuesta | Documentación |

Orden recomendado: B-1 (aislado, imprescindible) → M-1 (cambia la estructura y los e2e) → M-2, M-5, M-6
(mapa, un solo bloque) → M-3 y M-7 (rejilla, tablas, árbol) → M-9 → M-8 (ayuda) → M-11 (presupuesto; hacerlo
antes de M-10 deja holgura para el idioma nuevo) → M-10 → M-4 → M-12.

---

## B-1. Avisos de licencia de lo incrustado en `dist/`

### Causa **[medido]**

`npm pack` incluye `dist/**`, `package.json`, `README.md` y `LICENSE` (la GPL-2.0 propia). Lo ajeno que
acaba copiado dentro de `dist/`:

| Origen | Licencia | Cómo entra | Dónde queda |
|---|---|---|---|
| Phosphor Icons (`@phosphor-icons/react@2.1.10`, devDependency) | MIT, «Copyright (c) 2020 Phosphor Icons» | `scripts/gen-icons.mjs` copia las cadenas `d` a `src/ui/icon-data.ts` (se mantiene el comentario de cabecera, que el minificador borra) | `dist/ui-*.js` (0 apariciones de «Phosphor» en `dist/`) |
| `maplibre-gl@6.19.0` hoja `dist/maplibre-gl.css` | BSD-3-Clause, «Copyright (c) 2023, MapLibre contributors» (más los avisos de Mapbox que trae su `LICENSE.txt`) | `@import` en `src/map/maplibre.css` | `dist/karpo-map.css`, casi entera |
| `@xyflow/react@13.0.3` hoja `dist/base.css` | MIT, «Copyright (c) 2019-2025 webkid GmbH» | `@import` en `src/tree/tree.css` | `dist/karpo.css` (`.react-flow__*`) |

Comprobado con los mapas de fuentes de `dist/*.map`: no hay más código de `node_modules` empaquetado
(todas las dependencias son externas por `externalPattern` en `vite.lib.config.ts`). Los CSS
minificados no conservan ningún comentario. `driver.js`, `react-resizable-panels` y el resto llegan como
dependencias normales de npm, con su propia licencia, y no son parte del problema. Los dos `@import` y
`icon-data.ts` son **todo** lo incrustado hoy; el riesgo es que mañana entre uno más sin que nada avise.

### Ficheros

`scripts/licencias.mjs` (+ `licencias.test.mjs`), `scripts/pack-check.mjs` (+ test), `package.json`
(`files`, `exports`, script de `build:lib`), `vite.lib.config.ts`, `vite.map-css.config.ts`, `README.md`,
`docs/LICENCIAS.md` (se regenera), nuevo `scripts/avisos.mjs` (o un módulo dentro de `licencias.mjs`).

### Enfoque

1. **Registro de orígenes incrustados** (datos puros en `scripts/avisos.mjs`):
   `{ id, paquete, licencia, motivo, donde: ['dist/ui-*.js', …], detectar }`. Tres entradas hoy. El texto
   del aviso sale **de `node_modules/<paquete>/LICENSE*` en el momento de generar** (no se copia a mano:
   si el paquete actualiza su copyright, se actualiza el aviso). Tomar versión de `package.json` del paquete.
2. **Generación**: `node scripts/licencias.mjs --avisos` (o `npm run avisos`) escribe
   `dist/THIRD-PARTY-NOTICES.md`: cabecera (qué es y por qué está), y por origen: nombre, versión, licencia,
   dónde está incrustado, texto íntegro de licencia. Determinista (sin fechas). Debe correr **al final de
   `build:lib`** (`vite build -c vite.lib.config.ts && … && tsc … && node scripts/licencias.mjs --avisos`),
   porque la primera vite vacía `dist` (`emptyOutDir`).
3. **Paquete**: añadir `"dist/THIRD-PARTY-NOTICES.md"` explícito a `files` (ya lo cubre `dist`, pero así
   queda dicho y `npm pack` lo exige) y a `exports` como `"./THIRD-PARTY-NOTICES.md"`. Añadirlo a
   `ALLOWED_FILES`/`REQUIRED_FILES` de `pack-check`.
4. **README**: en «Licencias», un párrafo y enlace relativo `dist/THIRD-PARTY-NOTICES.md` más el aviso de
   que el generador es `npm run avisos`.
5. **Cabecera legal en los CSS** (cinturón y tirantes, opcional pero barato): un `/*! … */` al inicio de
   `karpo.css` y `karpo-map.css` con «Incluye partes de … (MIT/BSD-3), ver THIRD-PARTY-NOTICES.md».
   Verificar **[por verificar]** que lightningcss respeta los comentarios `/*!` en `cssMinify`; si no, un
   plugin `generateBundle` que los antepone (los dos plugins de Vite ya existen en los configs).
6. **`pack:check` falla si falta un origen** (el pedido del jurado), y lo hace **sin lista fija**:
   - El fichero existe y contiene, por cada origen del registro, el nombre del paquete y el texto de su
     `LICENSE` (comparación con espacios normalizados).
   - Detección automática de lo incrustado: (a) recorrer `src/**/*.css` y rechazar cualquier
     `@import url("<paquete>/…")` no relativo cuyo paquete no esté en el registro; (b) recorrer los
     `dist/*.map` y rechazar cualquier fuente `node_modules/<paquete>/` que no esté en el registro (el
     caso de una dependencia que olvidemos marcar como externa); (c) los `scripts/gen-*.mjs` que
     embeben datos de terceros se declaran en el registro (`generadoPor`), y la prueba comprueba que el
     fichero generado sigue existiendo.
   - `licencias --check` también falla si un origen del registro no está en `package-lock.json`.
7. **Pruebas**: `licencias.test.mjs` (render del documento con un registro de juguete, orden estable,
   fallo si falta `LICENSE`), `pack-check.test.mjs` (falla sin el fichero, falla con un origen que no
   aparece, falla con un `@import` desconocido). Una prueba de contrato que lee `dist/` real sólo si existe.
8. **Atribución de `@xyflow/react`** (consulta abierta): `TreeDiagram.tsx:198` pone
   `proOptions={{ hideAttribution: true }}`. La licencia es MIT y permite ocultarla; el fabricante la pide
   como cortesía y la apoya con una suscripción. No es un problema de licencia ni bloquea. Proponer a
   Javier tres salidas: (a) mantener oculta, con el aviso del paquete (opción por defecto); (b) mostrarla,
   configurable con `manifest.diagram.attribution`; (c) suscribirse. Anotarlo en la lista de consultas
   (M-12) con la recomendación (a).

---

## M-1. Título único por pantalla

### Causa **[medido por lectura]**

- `src/shell/TopBar.tsx:48`: `<h1 data-kx-part="title">` con `title = resolve(config.namePlural)` o el
  nombre de la aplicación.
- `src/shell/EntityHeader.tsx:76`: `<h2 data-kx-part="entity-title">` con el mismo texto.
- En modo compacto `shell.css:662` oculta visualmente el título de la entidad y deja el de la barra.
- **Otros duplicados que el jurado no cita**: `pages.tsx` pinta `<h1>` (`home.title`, `forbidden.title`,
  `notFound.title`) **debajo** del `<h1>` de la barra; en la ficha como página (`asPage`) el `h1` de la
  barra repite el título que la cabecera de la ficha (`SheetHeader.tsx:64`, `h2`) ya pinta.
- Hay e2e que dependen de ello: `e2e/movil.e2e.ts:36` busca `[data-kx-part="topbar"] h1`;
  `e2e/detalle.e2e.ts` busca `h2` dentro de la ficha; `e2e/rtl.e2e.ts:60` mide `[role="banner"] h1`.

### Ficheros

`src/shell/TopBar.tsx`, `src/shell/EntityHeader.tsx`, `src/shell/shell.css`, `src/shell/pages.tsx`,
`src/shell/menu.ts` (reutiliza `flattenMenu`), `src/detail/SheetHeader.tsx` (+ `Sheet.tsx` si necesita el
nivel), `src/auth/*` (comprobar que cada página de acceso tiene su `h1`), `src/locales/*/core.json`
(texto de la miga), e2e: `movil`, `detalle`, `rtl`, `navegacion` (si existe) y las pruebas
`shell.test.tsx`/`EntityHeader.test.ts`/`detail*.test.tsx`.

### Enfoque

Regla: **cada pantalla tiene exactamente un `h1` y lo pinta su contenido, no la barra.**

1. `TopBar` deja de pintar `<h1>`. Pinta una **miga** `nav[aria-label=Ruta]` con `ol`: sección del menú (el
   `group` que ya calcula `flattenMenu`, p. ej. «Comercial») › entidad (texto secundario, sin enlace
   salvo la sección si el menú la admite) y, en el detalle, › nombre del registro cuando se conozca.
   Sin entidad (inicio, ajustes): sólo el nombre de la aplicación como `p`. Estilo: `--th-text-secondary`,
   `--th-type-small`; separador con icono `caret-right` (reflejado en RTL con el atributo
   `data-kx-mirror` que ya existe en `ui.css`).
2. `EntityHeader`: `h2` → `h1` (único, con el recuento al lado como ahora).
3. `SheetHeader`: nuevo prop o contexto `level: 1 | 2`; `h1` cuando `presentation === 'page'` (la cabecera
   de la entidad no se pinta en ese caso), `h2` en panel (convive con el `h1` de la cabecera). Cuidado: el
   `id` `titleId` y `aria-labelledby` no cambian.
4. `pages.tsx` ya tiene un `h1` propio: sin cambios; basta con que la barra deje de tener el suyo.
   Revisar `LoginPage` y `ChangePasswordPage`: deben tener `h1` (no usan la barra).
5. **Modo compacto**: la barra ya muestra el texto de la pantalla; para que no duplique ante el lector de
   pantalla, la miga compacta se pinta como `p` con `aria-hidden="true"` y el `h1` de la cabecera de
   entidad sigue oculto visualmente (regla actual de `shell.css:662`, retargeteada a `entity-title`). Así
   el árbol de accesibilidad tiene un único `h1` en todos los marcos y no hay que mover el `h1` entre
   componentes con un portal.
6. Pruebas: una prueba de componentes que monte `Shell` con cada ruta y exija
   `getAllByRole('heading', { level: 1 }).length === 1`; e2e que lo comprueba en las 8 entidades de Go,
   las 4 de la demo, inicio, 403, 404, detalle como página y compacto (360 px). Actualizar los tres e2e
   citados (el selector del título pasa a `[data-kx-part="entity-title"]`).
7. Capturas: se regeneran todas (`npm run capturas`); la barra queda más ligera, sin título grande.

---

## M-2. «Sin dato» discreto en el mapa

### Causa **[medido por lectura]**

- `src/map/style.ts` (`buildMapStyle`) añade la capa `kx-missing` con `fill-pattern` a **opacidad 1**
  encima del relleno plano. `missingFilter` sólo deja pasar los códigos que no tienen dato; con
  `steps.size === 0` devuelve el filtro del nivel y la trama cubre **todo** el nivel.
- `pattern.ts`: tesela de 8 px con raya de 2 px (`strokeOf(size) = round(size/5)`), pensada para destacar.
- Tokens: `--kx-data-missing` (60 % superficie + 40 % texto inverso: en claro es prácticamente la
  superficie) y `--kx-data-missing-hatch` (75 % `text-secondary`): raya de contraste alto **a propósito**
  (la prueba de contraste la exige a 3:1 contra el relleno).
- Con un país con dato de 248, la trama cubre 247: ruido sobre el 99 % del lienzo.

### Ficheros

`src/map/style.ts`, `src/map/pattern.ts`, `src/map/MapCanvas.tsx` (clave de la imagen), `src/map/map.css`
(leyenda), `src/theme/kx-ext.css`, `src/theme/contrast.contract.test.ts`, `src/map/style.test.ts`,
`src/map/MapView.test.tsx`, `docs/PETICIONES-THEROS-FRONT.md` (los tokens `--kx-data-*` son «propuestos»).

### Enfoque

Separar **dos contrastes** que hoy comparten un token: el de la leyenda (hay que ver la trama) y el del mapa
(no debe gritar).

1. Nuevo token `--kx-data-missing-hatch-map` en `kx-ext.css`: mezcla mucho más suave
   (`color-mix(in oklab, var(--kx-data-missing-hatch) 35%, var(--kx-data-missing))`). La leyenda sigue con
   `--kx-data-missing-hatch` (contraste ≥ 3:1 comprobado); el mapa usa el suave. Añadir a la prueba de
   contraste un par propio con mínimo 1,3:1 (distinguible sin gritar) y **dejar intacta la prueba
   ΔL(missing, seq-1) ≥ 0,04**, que es la que garantiza que «sin dato» no se confunde con la clase 1
   (aquí `missing` no cambia).
2. Tesela más fina y más rala: lado 12 px y raya de 1 px (parámetros de `drawHatch`; `HATCH_SIZE`
   sube de 8 a 12, `strokeOf` pasa a `1`), que sigue siendo reconocible como trama sin dominar. `fill-opacity`
   de la capa **no** se usa (una opacidad fija en TS es un valor de diseño: mejor el token).
3. **Sin ningún dato, sin trama**: si `steps.size === 0` no hay clase de la escala con la que confundir
   «sin dato» y la capa no se añade (`buildMapStyle` omite `LAYER_MISSING`; `missingFilter` devuelve
   `undefined` ya cuando…). Con un solo país con dato, la trama suave es la que queda, y es aceptable.
4. Borde fino: la capa `kx-line` ya usa `--th-edge-strong` a 1 px; comprobar contra el relleno «sin dato»
   (3:1 `kx-data-border` ya está en la prueba). No cambiar.
5. **Leyenda**: sin cambios visuales; sigue con la trama marcada (`swatch[data-kx-step="none"]`).
6. Pruebas: `style.test.ts` (sin datos → sin capa; con datos → capa con el token suave), `pattern`
   (tamaño y grosor), contraste (par nuevo en los 4 temas × claro/oscuro), e2e/capturas del mapa en
   claro y oscuro.

---

## M-3. Columna «Estado» cortada con el detalle abierto (M-8 de la ronda 1)

### Causa **[medido en la captura `detalle-edicion-claro.png`]**

- `GridTable.tsx:80-88` reparte `--kx-cols` con la última columna `minmax(ancho, 1fr)`; `useGridModel`
  (`model.ts:133`) da el ancho de TanStack Table. Si la suma de anchos supera la del visor, la rejilla
  **sólo se desplaza en horizontal**; la columna «Estado» queda a medias junto a las acciones fijadas
  (`data-kx-pin="end"`). No hay noción de prioridad, ni de «caben N columnas».
- Con el detalle abierto, el panel de la lista mide el 55 % (`Panels.tsx:41`, `defaultSize`) y `minSize=30`.
- La configuración no tiene `priority` (`ColumnDef` en `contracts/config.ts:70-78`), y la de Go no
  puede traerlo.

### Ficheros

`src/grid/model.ts`, `src/grid/GridTable.tsx`, `src/grid/GridView.tsx`, `src/grid/prefs.ts`,
`src/grid/ColumnsMenu.tsx`, `src/grid/grid.css`, `src/contracts/config.ts`, `src/config/normalize.ts`,
`src/config/schema.ts`, `src/locales/{es,en}/grid.json`, `docs/COMPATIBILIDAD-CONFIG.md`,
`src/shell/Panels.tsx`.

### Enfoque

Combinación (las tres, en este orden de importancia):

1. **Ocultación automática por falta de ancho** («columnas a medias nunca»). Hook `useFitColumns`:
   `ResizeObserver` sobre el visor; con `ancho_visible = clientWidth − (columna de selección + acciones)`
   decide un conjunto `autoHidden`. Algoritmo: orden de prioridad = (1) fijadas y la primera columna, que
   **nunca** se ocultan; (2) el resto por `priority` ascendente si la configuración lo trae, y si no, de
   **derecha a izquierda** (las últimas son las menos importantes: convención de la configuración Go,
   comprobable en las 8 entidades); se quitan columnas hasta que la suma de anchos cabe. Antes de quitar,
   se **encogen** las columnas sin tamaño fijado por el usuario hasta un mínimo (`width × 0,7`, nunca por
   debajo de un mínimo de legibilidad medido por tipo) para no ocultar de más.
2. **Las columnas ocultadas por ajuste no tocan las preferencias del usuario** (`prefs.hidden`): son un
   estado derivado. `ColumnsMenu` las muestra con una nota «oculta por falta de espacio» y un
   contador `+2` junto al botón «Columnas» (`aria-live` cortés). Si el usuario la marca a mano,
   cuenta como elegida y se vuelve a calcular el resto. Un modo `autoFit` en las preferencias (por
   omisión sí) permite desactivarlo, volviendo al desplazamiento horizontal.
3. **Prioridad en la configuración**: clave opcional `priority` (entero, mayor = más importante) en la
   columna; la lee `normalize.ts` y el esquema permisivo; anotarla en `COMPATIBILIDAD-CONFIG.md` como
   extensión **opcional** (la configuración de Angular la ignora) y en `PETICIONES-THEROS-FRONT.md` (que el
   pack karpo-go de Theros pueda emitirla). Sin ella manda el orden.
4. **Panel**: subir el mínimo de la lista con detalle a 40 % y bajar el detalle a `defaultSize=40`
   (`Panels.tsx`); en 1440 px la rejilla dispone de ~560 px en lugar de ~470. No resuelve el problema
   (la rejilla de 30 columnas seguirá sin caber), pero evita caer en el caso límite a la primera.
5. **Teclado/accesibilidad**: `aria-colcount` debe reflejar las columnas **pintadas** (ya lo calcula
   `colCount` desde `layout`); la navegación con flechas no puede alcanzar una columna oculta. Es lo
   que justifica filtrar en `useGridModel` (que devuelve `layout`), no en CSS.
6. Pruebas: unitaria de la función pura `fitColumns(columnas, ancho)` (prioridades, mínimos, nada de
   ocultar fijadas), de componentes con `ResizeObserver` simulado, e2e: con el detalle abierto al 55 %
   ninguna celda visible está recortada (`scrollWidth ≤ clientWidth` en cada columna pintada, salvo
   la absorbente) y el contador `+n` existe.

---

## M-4. Hueco de ~230 px en el detalle

### Causa **[medido por lectura y captura]**

`detail.css:34` fija el pie (`margin-block-start: auto`, `position: sticky`) y `SheetBody` pinta una sola
sección corta (instalación: 7 campos en 2 columnas). Lo sobrante queda vacío entre el último campo y el pie.
No hay metadatos que mostrar: **la API no devuelve el sello de auditoría** (`pkg/domain/traits/audit.go`
lo tiene en el dominio, pero el contrato REST de mantenimiento sólo da `id` y `version`; no aparece en
`demo/data` ni en `docs/analisis/02-api-karpo-go.md`).

### Ficheros

`src/detail/SheetBody.tsx`, nuevo `src/detail/SheetMeta.tsx` y `SheetRelations.tsx`, `src/detail/detail.css`,
`src/detail/Sheet.tsx`, `src/locales/{es,en}/detail.json`, `docs/PETICIONES-BACKEND.md` (B-27),
`docs/COMPATIBILIDAD-CONFIG.md`.

### Enfoque

El pie fijo es **correcto** (acciones siempre a mano); lo que sobra es el hueco. Rellenarlo con contenido
útil y derivado de la configuración, sin código por pantalla:

1. **Tarjeta «Registro»** (`SheetMeta`, bajo la última sección, en modo edición y consulta): identificador
   corto con botón de copiar, `version`, y, **si el servidor los envía**, creado/modificado y por quién
   (`createdAt`, `createdBy`, `updatedAt`, `updatedBy` leídos de la fila sin tocar la configuración; se omiten
   las que no vengan). Es honesto con lo que la API da hoy. Petición **B-27** al backend: exponer el sello
   de auditoría (`AuditStamp`) en las respuestas de lectura.
2. **Tarjeta «Relaciones»** (`SheetRelations`), derivada de lo que la configuración ya declara:
   - cada campo `lookup` con valor se ofrece como enlace «Abrir <entidad>: <nombre>» (navegación a
     `/<entidad>/<id>`; ya existe `lookupEntity`);
   - si la entidad tiene vista de árbol (`parentField`), «Dentro de» y «Contiene N» (recuento con la
     consulta de hijos que ya usa `use-tree-data`), con enlace a la vista de árbol centrada en el registro;
   - las `childGrids` siguen siendo pestañas (no se duplican).
3. **Pie**: sin cambios (sticky). Si aun así el contenido es muy corto, `SheetBody` toma `flex: 1` y las
   tarjetas se apilan arriba: no se reparte el espacio con `space-between`.
4. Pruebas: componentes (metadatos opcionales, sin enlace si no hay permiso de lectura de la entidad
   destino, `canReadEntity`), e2e en `/facility/<id>` (enlace «Forma parte de» navega) y captura. Se
   comprobará que el hueco bajo el formulario es < 80 px en `detalle-edicion-*` con la ficha de la
   captura.

Alternativa más barata si no hay tiempo: sólo `SheetMeta` (id y versión) + tarjeta de relaciones de campos
`lookup`; dejar los hijos del árbol para la ronda siguiente.

---

## M-5. Mapa: nivel «región» de un solo elemento

### Causa **[medido por lectura]**

`MapView.enter()` (clic) pasa por `useEntryResolver` → `resolveEntry`, que baja mientras el nivel tenga una
sola delimitación con detalle (Q-10). La captura `mapa-region-*` entra por URL
(`/facility?view=map&level=region&code=ES30`): `parseNav` acepta cualquier nivel y código y `useMapLevel` lo
pinta tal cual. Sólo `useCrumbs` (`resolveTrail`) conoce el salto, y lo usa para las migas, no para la vista.

### Ficheros

`src/map/MapView.tsx`, `src/map/use-map-level.ts`, `src/map/nav.ts`, `scripts/capturas.mjs`,
`src/map/MapView.test.tsx`, `src/map/nav.test.ts`, e2e del mapa.

### Enfoque

1. **Redirección al entrar por URL**: un efecto en `MapViewInner` que, cuando la fuente ya cargó el nivel
   pedido (`shown.nav` igual a `nav`), no se ha llegado con un clic (`arrived.current !== navKey`),
   `skipsSingleLevels(view)` y el nivel tiene **una** delimitación con `hasChildren`, llama
   `resolveEntry` con la miga de ese nivel y hace `navigate(..., { replace: true })` al destino (sin
   dejar la URL intermedia en el historial, para que «Atrás» no rebote). Reutiliza `resolveEntry` y `go()`;
   `useCrumbs` ya produce las migas con el salto marcado. Comprobar que `navigate` admite `replace`
   (`useEntityNavigate` en `shell/hooks.ts`); si no, añadirlo. **[por verificar]**
2. Evitar el parpadeo: mientras se resuelve, `loading` se mantiene (no se pinta el cuadrado de un elemento).
3. **Escena de capturas**: cambiarla a algo con varios elementos: `level=region&code=ES` (comunidades
   autónomas) para que la captura enseñe un nivel de verdad, y conservar la escena `ES30` sólo en un test
   que verifica la redirección (no como imagen).
4. Con teselas vectoriales (`pmtiles`) el nivel no se puede mirar sin abrirlo (comentario de
   `useEntryResolver`): la redirección sólo se aplica a fuentes GeoJSON; anotar la limitación y el
   motivo en el código.
5. Pruebas: unitaria de la decisión (función pura `redirectTarget(view, shown, nav)`), de componentes
   (URL de un nivel de un elemento → URL final del siguiente, con `replace`), y con `skipSingle: false` no
   redirige.

---

## M-6. Rótulos del mapa: criterio único (Q-8)

### Causa **[medido por lectura]**

`fitLabel` (`labels.ts:104`) prueba por **cada delimitación** las variantes `full`, `short`, `code` y se
queda con la primera que cabe. Países grandes enseñan «Rusia» y los pequeños «CA»: mezcla por diseño. En el
mundo esquemático esto es inevitable por tamaño, pero no es un criterio.

### Ficheros

`src/map/labels.ts`, `src/map/MapCanvas.tsx` (~línea 426), `src/map/labels.test.ts`,
`src/map/MapTable.tsx` (la tabla equivalente conserva el nombre completo), `src/locales/*/map.json`.

### Enfoque

Criterio por **nivel**, calculado una vez por vista y pasada de rotulado:

1. Dos pasadas en `MapCanvas`: primero, para cada delimitación candidata, la variante **más larga que cabe**
   (como hoy). Después, `kind` común = la variante **más corta** entre las elegidas de las delimitaciones que
   reciben rótulo (rango `full > short > code`); todas se repintan con ese `kind`. Es la regla del jurado:
   «nombre sólo si cabe entero en todas; si no, código en todas».
2. Función pura `commonKind(fits: LabelFit[]): LabelFit['kind']` y reajuste de tamaños (`fitLabel` con
   `kinds` permitidas para que el rótulo más corto no deje un hueco sin usar: se puede ofrecer la variante
   común y recolocar con `pickLabels`).
3. Consecuencia buscada y aceptable mientras llegue la geometría real (Q-8): en el mundo, **códigos**
   (ISO) en todas; en un país o una región con unidades grandes, nombres. El nombre completo sigue en la
   tabla equivalente, en el `title`/tooltip y en el estado de la miga.
4. Los rótulos «sin valor» se pintan igual (sólo cambia el texto).
5. Pruebas: `labels.test.ts` (un mix de tamaños → un único `kind`; todo cabe → `full`), y un e2e que, en
   la escena de mundo, comprueba que todos los `[data-kx-part="label"]` comparten `data-kx-kind`
   (atributo nuevo).

---

## M-7. Pulido

Cinco causas independientes, todas baratas.

1. **Vacío «-» frente a «—» [medido].** La configuración de Go trae `"emptyValue": "-"` (por ejemplo
   `facility.config.json:51`), la de la demo `"—"`, y `SheetField.tsx:54-56` pinta «—» cuando no hay
   `emptyValue`. `viewers.tsx:15` (`Empty`) pinta el que venga.
   *Enfoque:* constante `EMPTY_MARK = '—'` en `src/fields/values.ts` y `normalizeEmptyValue()` en
   `normalize.ts` que convierte un `emptyValue` formado sólo por guiones (`-`, `–`, `—`) en `EMPTY_MARK`; el
   resto de textos se respeta (`"sin fecha"`). `Empty` y `SheetField` usan la misma constante y el mismo
   marcado accesible (`aria-hidden` + `sr-only` con «Sin valor»). Pruebas de normalización y de ambos
   visores.
2. **Cifras en monoespaciada [medido].** `NumberViewer` (`viewers.tsx:46`) pone `data-th-format="amount"` y
   el tema (`domain.css`, `[data-th-format="amount"]`) aplica `font-family: var(--th-type-mono-family)`.
   También `MapTable.tsx:85`.
   *Enfoque:* dejar de usar `data-th-format="amount"` para números y decimales; reglas propias en
   `fields.css`: `[data-kx-ui="value"][data-kx-align="end"] { font-variant-numeric: tabular-nums;
   text-align: end; white-space: nowrap }` (sin cambiar de familia). Conservar `amount` sólo para `money` si
   Javier lo prefiere mono (consulta menor; recomendado: ninguno). Petición a Theros Front: que `amount`
   no fuerce la familia monoespaciada o que ofrezca `data-th-format="figure"` (PETICIONES-THEROS-FRONT).
   El gancho `code` sí es monoespaciado y se queda.
3. **Truncado en los nodos del diagrama [medido por lectura].** `tree.css` (~línea 290): la tarjeta reserva
   `padding-inline-end: calc(var(--th-space-8) * 2)` **siempre** (sitio para el contador y el plegado) y el
   rótulo y el subtítulo van con `white-space: nowrap; text-overflow: ellipsis`. Un nodo hoja trunca con
   espacio libre a su lado.
   *Enfoque:* reservar el espacio sólo cuando hay hijos (`[data-kx-has-children]`; la tarjeta ya sabe
   `fold`), y permitir **dos líneas** en el rótulo y el subtítulo (`-webkit-line-clamp: 2` con el
   `title` accesible) en lugar de una con elipsis; el ancho del nodo (`layout.ts`, `node.width`) no
   cambia, para no mover el diseño de dagre. Verificar que la altura de la tarjeta soporta 2+2 líneas (hoy
   rejilla de tres filas); si no, subir la altura del nodo en `layout.ts` con el espaciado del tema.
4. **Contadores del árbol a ~1000 px de la etiqueta [medido].** `tree.css:194`: `[data-kx-part="count"]
   { margin-inline-start: auto }` en una fila que ocupa todo el ancho de la vista.
   *Enfoque:* quitar el `auto` y dejar el contador **justo detrás** del subtítulo/etiqueta (`flex: none`); la
   fila conserva el fondo de selección a todo el ancho. Opcional: `max-inline-size` en la fila de contenido
   (~60em) para que el árbol en pantallas anchas no se estire. Revisar RTL (el espacio se calcula con
   propiedades lógicas, ya es así).
5. **«50 de 120 filas» sin relleno [medido en capturas].** `grid.css:529`: el pie de la cuadrícula no
   tiene `padding`. *Enfoque:* `padding-block: var(--th-space-2); padding-inline: var(--th-space-margin);
   border-block-start: var(--th-edge-width) solid var(--th-edge-subtle)` y `font-variant-numeric:
   tabular-nums`. Con M-3 (contador `+n` de columnas) comparten barra: mantenerlos en el mismo contenedor
   flex con `margin-inline-start: auto` sólo para el contador de columnas.

---

## M-8. Ayuda guiada completa

### Causa **[medido por lectura]**

- `HelpProvider.start` (`HelpProvider.tsx:78-79`): si el registro de pantalla trae pasos de configuración
  (`defs.length > 0`) usa **sólo esos**, y las anclas sólo cuando no hay ninguno. La configuración de
  productos declara 2 pasos (filtros y tabla) y se pierden las anclas visibles: `core.new`,
  `core.view-switch`, `grid.columns`, `grid.toolbar` (deshacer), `core.palette`, `core.menu`…
- `grid.batch-bar` (`Bars.tsx:33`) va `hidden` sin cambios, y `stepsFromConfig`/`stepsFromAnchors` descartan
  los pasos sin elemento visible (`isVisible`).
- El cierre de driver.js (`help.css:154`) fija sólo un tamaño mínimo; su contenido es el carácter «×» a
  tamaño de texto pequeño.
- Textos: ya existen para las anclas del núcleo (`help.<nombre>.title|body` en `core`, `grid`, `detail`…).

### Ficheros

`src/help/HelpProvider.tsx`, `src/help/steps.ts` (+ `steps.test.ts`), `src/help/tour.ts`, `src/help/help.css`,
`src/grid/Bars.tsx`, `src/shell/EntityHeader.tsx`, `src/locales/{es,en}/help.json`, `grid.json`, `core.json`,
`docs/COMPATIBILIDAD-CONFIG.md`, `e2e/ayuda.e2e.ts`.

### Enfoque

1. **Fusión en lugar de sustitución.** Nueva `mergeSteps(config, anchors)`: pasos de la configuración
   primero **en su orden** (son el relato que quiso el autor de la configuración), y después las anclas
   visibles con texto que ningún paso de configuración cubre (se compara por elemento y por nombre
   de ancla: un `target` que sea `grid.table` cubre `data-kx-help="grid.table"`). Resultado ordenado por
   posición en el documento **dentro de cada grupo** de prioridad, de modo que el recorrido sea
   natural (cabecera → vistas → «Nuevo» → filtros → columnas → deshacer → tabla → paleta). Un interruptor
   por pantalla permite apagarlo: `help.mode: 'config' | 'merge'` en la configuración (por omisión
   `merge`), para quien quiera exactamente su guion.
2. **Pasos de la barra de lote sin cambios**: el elemento oculto no se puede resaltar, así que la ancla
   oculta declara dónde enseñarlo: `data-kx-help-fallback="grid.toolbar"` en `Bars.tsx`. `steps.ts` usa ese
   elemento, y el texto alterno (`help.batch-bar.titleIdle|bodyIdle`: «Aparece al editar: guarda o descarta
   los cambios de golpe») cuando la barra está oculta. Con cambios pendientes se enseña la barra real y el
   texto normal. Misma técnica para otros elementos efímeros (la barra de selección).
3. **Más anclas** (en componentes, no en la configuración): `core.view-switch` ya existe; comprobar que
   `core.new`, `grid.columns`, `grid.undo` (nueva, sobre el par deshacer/rehacer de `GridView`),
   `grid.row-actions`, `core.palette` y `core.menu` están visibles y con textos en es/en (y `ar`, M-10).
   La prueba de contrato recorre las anclas del DOM y exige textos en todos los idiomas.
4. **Cierre**: sustituir el «×» de texto por un `Icon name="x"` con tamaño del tema
   (`--th-control-icon-size`) y ampliar el botón a `--th-control-height-regular` (32 px; ≥ 24 px de
   WCAG 2.5.8) con `inset` de `--th-space-2`. Se hace en `decoratePopover` (`tour.ts`): `closeButton.replaceChildren(svg)`
   con el mismo SVG de `Icon` (usar `iconData('x')`/`renderToStaticMarkup` en diferido sólo si hace falta; mejor
   una función `iconSvgString(name)` en `ui/icons.ts`). El texto accesible ya lo pone `aria-label`.
5. **Progreso y orden**: «Paso N de M» ya sale de driver.js; con los pasos fusionados el total sube a ~8-10.
   Un recorrido largo debe poder saltarse: botón «Cerrar» siempre disponible y `Esc` (ya implementado).
6. Pruebas: unitarias de `mergeSteps` (sin duplicados, orden, `help.mode`), de `steps.ts` con el
   `fallback`, de componentes con el recorrido y e2e: `/product` ≥ 7 pasos incluyendo el de la barra de
   lote sin cambios pendientes; el botón de cerrar mide ≥ 28 px.

---

## M-9. Casillas de selección en táctil

### Causa

`GridRow.tsx:116` y `GridTable.tsx:320` usan `<input type="checkbox">` nativo sin dimensiones propias
(13×13 px en Edge/Chrome); el Theros base (`base.css`) lo excluye de los estilos de campo.

### Ficheros

`src/grid/grid.css`, `src/grid/GridRow.tsx`, `src/grid/GridTable.tsx`, e2e móvil.

### Enfoque

1. En `grid.css`: para el selector `[data-kx-col="__select"] input[type="checkbox"]`:
   `inline-size/block-size: var(--th-control-icon-size)` en escritorio (algo mayor que el nativo sin
   romper la densidad) y, en `@media (pointer: coarse)`, `var(--th-control-height-small)` como mínimo
   (28 px ≥ 24) y `margin: auto`.
2. **Zona de impacto** sin agrandar el dibujo: `position: relative` en el input y un `::after` (o
   `padding` en la celda con `onClick` que alterna) hasta `var(--th-control-height-large)` (40 px) en
   táctil. Más sencillo: en `(pointer: coarse)` hacer que **toda la celda** `__select` sea el objetivo
   (`onClick` de la celda llama a `onToggleSelected`; hoy ya tiene `onMouseDown` para el foco). Cuidado
   con el doble disparo (el clic del `input` y el de la celda); usar `stopPropagation` en el input.
3. `accent-color` con el token de selección del tema (sin color literal).
4. Prueba e2e con `hasTouch`: ancho y alto del objetivo táctil ≥ 24 px; y de componentes: un clic en el
   borde de la celda alterna la selección.

---

## M-10. RTL con idioma real

### Causa

No hay idioma RTL en `src/locales` (sólo `es`, `en`) ni en `demo/config/i18n`. Las capturas `rtl` usan
`lng=en&dir=rtl` (`capturas.mjs:49`), así que el texto es latino dentro de un contexto RTL y el algoritmo
bidi invierte lo que no puede aislar. El aislamiento FSI/PDI de `src/i18n/bidi.ts` funciona, pero no se ve
con texto latino. `locales.ts:77` ya lista `ar`, `he`… como RTL.

### Ficheros

nuevo `src/locales/ar/*.json` (10 espacios), `demo/config/i18n/ar.json` + `ar.go.json`,
`scripts/gen-demo-i18n.mjs` (+ test), `demo/config/manifest.json` (`locales`), `src/i18n/locales.ts`,
`src/shell/locale-name.ts`, `scripts/capturas.mjs`, `e2e/rtl.e2e.ts`, `src/i18n/locales.contract.test.ts`,
`docs/PLAN.md` (Q-5), README.

### Enfoque

1. **Idioma real, no seudo-idioma**: `ar` (árabe, escrito en dirección real). Un pseudo-idioma con texto
   latino no prueba bidi; uno con transliteración sin significado engaña al jurado. El volumen es
   asumible: **573 claves** en `src/locales/es` (≈ 62 kB entre los dos idiomas) más las cadenas de la
   configuración de la demo. El contrato `locales.contract.test.ts` ya exige paridad de claves y de
   marcadores `{{x}}` en todo `src/locales/*`: con ello el idioma nuevo no puede quedar a medias.
2. **Etiquetado honesto**: encabezado en cada JSON no es posible (JSON); se anota en
   `docs/PLAN.md` (Q-5) y README: «traducción de demostración, **sin revisión nativa**; Q-5 sigue abierta
   para Javier». El selector de idioma lo muestra con su nombre propio («العربية») vía `languageName`.
3. **Configuración de la demo**: `gen-demo-i18n.mjs` genera `ar.json` desde una tabla `ar.go.json` (clave →
   árabe), como ya hace con `en.go.json`, para **las entidades que salen en capturas**; `--check` hace
   fallar si falta una clave. Cadena de reserva `ar → en` para las entidades que no se traducen
   (fallback de i18next), de modo que el idioma funciona completo aunque la configuración no esté entera.
   Los **datos** de la demo (nombres de instalaciones, productos) siguen en español/inglés: es el caso real
   de una base de datos multilingüe y es justo donde se demuestra el aislamiento de `<bdi>`.
4. **Formato**: `Intl` con `ar` puede usar dígitos arábigo-índicos según el ICU de Edge. Fijar la
   decisión: `numberingSystem: 'latn'` en `createFormatters` para `ar` por defecto (coherente con el resto
   de aplicaciones de gestión) y dejarlo como opción del idioma; comprobar `formatRange` y las unidades.
5. **Capturas**: la variante `rtl` pasa a `lng=ar` (sin `dir`, que lo da el idioma). Conservar los e2e
   existentes de `?dir=rtl` con `en` (prueban el forzado) y **añadir** el bloque equivalente con `ar`
   para: espejo del menú, migas, chevrons, árbol, diagrama y flechas de la rejilla, además de la lectura
   correcta de «٣٠ من ١٨»-tipo con el aislamiento en las cinco cadenas que citó el jurado (recuento,
   paso `N de M`, «campos marcados: 1», «límites: 248»).
6. Pruebas: `locales.contract` (paridad con `es`), `bidi.test.tsx` con `ar`, e2e `rtl` con `ar` (dirección
   y que `document.documentElement.lang === 'ar'`), y `capturas.test.mjs`.

---

## M-11. Presupuesto de bytes

### Causa **[medido]**

El trozo inicial de la demo pesa **242,7 kB gzip** (`index-*.js` 237,2 + css 5,0 + runtime 0,4). Atribución por
mapa de fuentes del `index-*.js` (755 kB sin comprimir; ratio gzip ≈ 0,31):

| Contribución | kB sin comprimir | kB gzip aprox. |
|---|---|---|
| `react-dom` | 202 | ~62 |
| **`src/ui` (casi todo `icon-data.ts`, 569 iconos × 2 pesos)** | **110** | **~25** |
| `react-router` | 97 | ~30 |
| `i18next` | 49 | ~15 |
| `@tanstack/query-core` | 31 | ~9 |
| `src/shell`, `auth`, `config`, `api`, `theme`, `help`, `i18n`, `contracts` | 92 | ~28 |
| Radix + floating-ui | ~65 | ~20 |
| Idiomas (62 kB JSON de **ambos** idiomas, cargados con `import.meta.glob` eager, `locales.ts:14`) | ~62 | ~14 |

**Corrección al informe:** `react-hook-form`, `@hookform/resolvers` y `zod/mini` **no** aparecen en el trozo
inicial (ninguna fuente de `node_modules/zod|react-hook-form|@hookform` en el mapa; el detalle ya es diferido).
Mover `form-model` fuera ya está hecho; no hay ahorro ahí. Tampoco están `fuse.js`, `driver.js`, `dagre`,
`xyflow` ni `maplibre`.

### Ficheros

`scripts/gen-icons.mjs`, `src/contracts/icons.ts`, `src/ui/icons.ts`, `src/ui/Icon.tsx`, `src/ui/icon-data.ts`
(generado, se parte en dos), `src/i18n/locales.ts`, `src/i18n/I18nProvider.tsx`, `src/i18n/createI18n.ts`,
`src/auth/*` (carga diferida de la pantalla de acceso), `scripts/presupuesto.json`, `scripts/presupuesto.mjs`,
`vite.config.ts`, `src/shell/lazy.ts`.

### Enfoque (en orden de rentabilidad)

1. **Partir los iconos (~20-22 kB gzip).**
   - `contracts/icons.ts` gana `CORE_ICON_NAMES` (los ~45 que usan el chrome: barra, menú, cabecera de
     entidad, acciones de la rejilla, diálogos, toasts y estados) y `ICON_NAMES` sigue siendo la lista total.
   - `gen-icons.mjs` genera **dos módulos**: `icon-data-core.ts` (estático) y `icon-data-extra.ts` (resto).
     `--check` sigue vigilando los dos. `icons.ts`: `iconData(name)` devuelve el dato si está en el núcleo; si es
     un icono extra, lo busca en un almacén que se llena con `import('./icon-data-extra')`.
   - `Icon` usa `useSyncExternalStore` sobre ese almacén: mientras no hay dato, pinta el `svg` vacío **con el
     mismo tamaño** (sin saltos de maquetación); al llegar el trozo, se repinta. El trozo extra se pide en
     cuanto arranca la aplicación (`requestIdleCallback`/`queueMicrotask` desde el entrypoint), en paralelo con
     el resto, de modo que los iconos del manifiesto (menú) aparecen al instante en la práctica y **no
     cuentan** para el presupuesto inicial (es importación dinámica).
   - Prueba de contrato nueva: un recorrido por `src/{shell,ui,grid,detail,auth,help}/**/*.tsx` que extrae
     literales `icon="x"`/`icon: 'x'` y exige que estén en `CORE_ICON_NAMES`; y otra que `CORE ⊂ ICON_NAMES`.
     Para la **biblioteca** (`dist/`) el reparto es el mismo y baja `ui-*.js`.
2. **Idiomas por demanda (~10-14 kB gzip).** `import.meta.glob(..., { eager: false })`: sólo el idioma activo
   y los espacios de nombres del chrome (`core`, `ui`, `auth`, `errors`) en el arranque; `grid`, `detail`,
   `tree`, `map`, `help`, `fields` se cargan con su vista (`lazy.ts` ya conoce cada trozo) o con
   `i18n.loadNamespaces`. Es la forma estándar de i18next y concuerda con tener `ar` (M-10) sin pagarlo.
   Con `react-i18next` y `Suspense` desactivado, hay que garantizar que un componente no pinte claves
   antes de tener su espacio: el trozo de la vista importa su `ensureNamespaces` antes de montarse.
3. **Pantalla de acceso diferida (~4-5 kB gzip)**: `src/auth/LoginPage` y `ChangePasswordPage` con `lazy()`
   (el 99 % de las visitas ya tiene sesión).
4. **No tocar** `react-dom`, `react-router` ni `i18next`: son la base. Evitar sustituirlos.
5. **Objetivo y límite.** Con 1+2+3 el trozo inicial queda en **~205-210 kB** (estimación; **[por
   verificar]**: medir tras cada paso con `npm run presupuesto`). El jurado pide «200 kB o menos»; llegar
   exacto puede exigir además diferir parte de `src/shell` (paleta de comandos y panel de atajos ya
   están diferidos) o la capa de `query-core` de rutas poco usadas. Propuesta: fijar el límite en
   **215 kB** en esta ronda (obligatorio, `presupuesto.json`) y **200 kB** en cuanto se mida un margen de
   más de 3 kB; dejar escrito en `presupuesto.json` un campo `target` informativo con el objetivo.
6. **Medida reproducible**: `presupuesto.mjs` gana `--detalle`, que imprime la atribución por paquete (el
   script de esta tarea, de 30 líneas, decodifica el mapa de fuentes de `demo/dist`) para que la próxima
   ronda no tenga que adivinarla.
7. Pruebas: de contrato (iconos del núcleo), `presupuesto.test.mjs` (campo `target`, `--detalle`),
   componentes (`Icon` con dato extra: tamaño estable antes y después de cargar), e2e (menú con todos los
   iconos sin `data-kx-icon="missing"` tras cargar).

---

## M-12. Consultas abiertas a Javier (Q-1 a Q-10, xyflow)

No hay nada que programar; hay que dejarlo **respondible en un minuto**.

1. Crear `docs/CONSULTAS-JAVIER.md` (o una sección corta al principio del README) con la tabla de
   `docs/PLAN.md` (91-100) reducida a: consulta, **recomendación**, **qué pasa si no responde**, **coste de
   cambiar después**. Estado actual de cada una:
   - Q-1 `style` con variables de disposición (CSSOM): ya aplicado (D-R12); coste de cambiar: alto.
   - Q-2 tema por defecto incluido: no incluido; pide `themeOrigin`.
   - Q-3 licencia del paquete: `GPL-2.0-only`; una línea.
   - Q-4 geometría esquemática: en uso.
   - Q-5 idioma RTL: **se actúa en esta ronda** (M-10); pide revisión nativa.
   - Q-6 `lightningcss` (MPL-2.0, sólo compilación): en uso; alternativa `cssMinify: 'esbuild'`.
   - Q-7 paleta de datos provisional.
   - Q-8 geometría real (Natural Earth): **necesita su autorización o el fichero**; hasta entonces M-6.
   - Q-9 `polylabel`: cálculo propio.
   - Q-10 nivel de un solo elemento: saltarlo (M-5 lo hace coherente).
   - Nueva Q-11: atribución de `@xyflow/react` (B-1.8).
   - Nueva Q-12: `amount` monoespaciado sólo para `money` o ninguno (M-7.2).
2. Enlazarlo desde el README y desde `docs/jurado/` (el jurado lo verá resuelto o respondible).
3. `PETICIONES-THEROS-FRONT.md`: añadir (a) `amount` sin familia mono, (b) `priority` de columna en el pack
   (M-3), (c) `help.mode` y las claves de ayuda por ancla, (d) tokens `--kx-data-missing-hatch-map` (M-2).
   `PETICIONES-BACKEND.md`: B-27 (sello de auditoría, M-4).

---

## Riesgos y dependencias entre cambios

- **M-1 ↔ capturas y e2e**: cambia el DOM de todas las pantallas; hacerlo el primero y regenerar capturas
  una sola vez al final (`npm run capturas`).
- **M-3 y M-7.5** tocan el mismo pie de la rejilla (contador `+n` y «50 de 120»): un solo cambio.
- **M-11.2 (idiomas diferidos) y M-10**: primero M-11 (para que `ar` no cargue por defecto), luego M-10.
- **M-8.4** (cierre con SVG) usa el sistema de iconos de M-11.1: debe pedir el icono `x` del núcleo.
- **B-1.5**: sin comentarios `/*!` conservados por lightningcss, un plugin local los antepone; es una
  línea más en dos configuraciones de Vite.
- **CSS**: stylelint prohíbe literales de color, medida, familia o peso: toda medida nueva sale de
  `--th-space-*`, `--th-control-*`, `--th-type-*`; los puntos de ruptura siguen en `em` con `@container`.
  Los `z-index` salen de la escala `--kx-z-*`.
- **Licencias**: las únicas dependencias nuevas posibles son **ninguna** (todo se resuelve con el código
  propio y lo ya instalado). No añadir `d3-*`, `axe-core` ni Shepherd.
- **Verificación**: `npm run check` completo (tipos, lint, 1417+ pruebas, contrato, licencias, iconos,
  i18n, build, presupuesto, `pack:check`) y `npm run test:e2e` con Edge; después el jurado recorre las
  capturas.

## Lo que no hay que tocar

- La estructura de paquete (`exports`, `prepublishOnly` que bloquea la publicación).
- La carga diferida ya existente (detalle, paneles, paleta, vistas, `driver.js`, MapLibre, xyflow).
- El patrón de teclado de la rejilla (una sola parada de tabulación; Tab sale y vuelve).
- Los contratos de tema (`--th-*`) y la prueba de contraste de cuatro temas × claro/oscuro: sólo se
  **añaden** pares, nunca se relajan.
