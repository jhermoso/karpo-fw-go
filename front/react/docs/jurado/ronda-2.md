# Jurado, ronda 2: front React de Karpo Go (`@karpo/react`)

Fecha: 2026-11-27. Commit revisado: `119c4da` (rama `claude/front-react-7b8564`).
Papel: jurado exigente, con el listón de Awwwards y de los mejores SaaS de gestión (Linear, Attio,
Stripe), WCAG AA y rendimiento.

## Veredicto

**No aprobado. Nota: 8,2 / 10.**

El salto desde la ronda 1 (7,0) es claro. Los cinco bloqueantes están resueltos y los he comprobado
yo mismo, no sólo en las pruebas: iconos, inglés, tipografía del mapa, Tab en la rejilla y reflujo
en móvil. La mayoría de las 19 mejoras también. La ingeniería es muy buena: 1417 pruebas unitarias y
de componentes, 154 de contrato y 138 e2e; cero literales de color o medida en el CSS; carga
diferida real y presupuesto de bytes obligatorio.

No apruebo por dos razones:

1. Queda **un bloqueante nuevo**: el paquete npm redistribuye código de terceros sin sus avisos de
   licencia.
2. **La nota no llega a 8,5.** El producto es correcto, ordenado y accesible, pero todavía no tiene
   el acabado de Linear o Attio. Hay un título duplicado en todas las pantallas, mapas dominados por
   la trama de «sin dato», capturas RTL que no demuestran nada, un recorrido de ayuda de dos pasos y
   una columna cortada en la escena estrella del detalle.

## Lo que he comprobado yo mismo

| Comprobación | Resultado |
|---|---|
| `npm ci` | 417 paquetes, 0 vulnerabilidades. Sólo el aviso esperado `EBADDEVENGINES` (`devEngines` ≥ 24.15 con `onFail: warn`; el equipo tiene Node 24.14.1) |
| `npm run check` | Código 0. Tipos, eslint, stylelint, 96 ficheros y 1417 pruebas, 7 ficheros y 154 de contrato, `licencias --check`, `icons:gen --check`, `i18n:demo --check`, build de biblioteca y demo, presupuesto y `pack:check` (244 ficheros) |
| Presupuesto | 242,7 kB gzip del trozo inicial (límite de 250 kB, obligatorio). Biblioteca: `karpo-react.js` 91 kB gzip, `karpo.css` 12 kB y `karpo-map.css` 10 kB aparte |
| `npm run test:e2e` (Edge instalado) | 13 ficheros pasan y 1 se salta; 138 pruebas pasan y 3 se saltan (`local.e2e.ts` sin `KARPO_LOCAL`) |
| Sondeo propio con playwright-core y Edge contra `vite preview` de la demo (puerto libre 18431, parado al terminar) | Resultados abajo. 0 peticiones fuera del origen y 0 excepciones de página |
| Las 41 capturas de `docs/capturas`, leídas una a una | Comentarios abajo |
| Revisión de código | `package.json`, lock (443 `resolved` y todos de `registry.npmjs.org`), `stylelint.config.js`, `src/grid/keyboard.ts`, `src/i18n/bidi.ts` y `createI18n.ts`, `src/map/nav.ts`, `src/help/steps.ts`, `src/shell/TopBar.tsx` y `EntityHeader.tsx`, `src/ui/icon-data.ts`, `src/map/maplibre.css`, `src/tree/tree.css`, manifiesto y superposiciones de la demo, contenido de `npm pack --dry-run` |
| `git status` tras todo lo anterior | Limpio: ni `npm ci` ni el build tocan ficheros versionados |

### Sondeo propio (Edge, 1440×900 y 360×780 táctil)

- **Contraste.** Recorrí cada elemento con texto propio visible y cada marcador de posición en
  `/product`, `/facility`, el organigrama en lista y en diagrama, y el mapa en los niveles país y
  ciudad, en claro y en oscuro. Comparé el color del texto con el fondo efectivo (mezclando capas
  semitransparentes) contra 4,5:1, o 3:1 en texto grande. **Ningún fallo.**
- **Rejilla.**
  - Clic en una celda: el foco queda en `gridcell`, con anillo de 2 px del tema.
  - Flechas: funcionan.
  - `Tab` sale de la rejilla y `Shift+Tab` vuelve a la misma celda (B-4 resuelto).
  - `Ctrl+End`, `Ctrl+Home` y `Enter` (edición): funcionan.
  - Doble clic en una fila de instalaciones lleva a `/facility/<id>?mode=edit` con el foco en un
    `input` del detalle.
- **Orden de tabulación.** Hay enlace «Saltar al contenido». Después: menú, búsqueda del menú,
  entradas, paleta y botones de la barra. Todos los controles tienen nombre accesible (0 botones
  sin nombre).
- **Estructura.** Hay `nav` con nombre, `header[banner]`, `main`, `role=search` y `nav` «Ruta del
  mapa». Pero hay **dos encabezados con el mismo texto** en cada pantalla: `H1: Productos` y
  `H2: Productos` (véase M-1).
- **Mapa.** La `font-family` de los rótulos y de `.maplibregl-map` es la de `body` (B-3 resuelto).
- **Diagrama.**
  - Escala inicial 0,996 (legible): M-1 de la ronda 1 resuelto.
  - Nodos con `role=group` y nombre.
- **Árbol.** Las flechas y la tecla de expandir funcionan, con `aria-expanded` y `aria-level`.
- **Móvil a 360 px.**
  - `scrollWidth == innerWidth` en producto, instalación, detalle, árbol, diagrama, mapa, festivos y
    activos.
  - Hay `h1` en todas y ningún `kbd` visible (B-5 resuelto).
- **Tamaño de objetivo.** Las casillas de selección de fila miden 13×13 px. En escritorio pasan
  WCAG 2.5.8 por la excepción de espaciado. En táctil son demasiado pequeñas (véase M-9).

## Bloqueantes

### B-1. El paquete npm redistribuye código de terceros sin sus avisos de licencia

`npm pack --dry-run` incluye sólo `LICENSE` (GPL-2.0), `README.md`, `package.json` y `dist/`. Pero
`dist/` lleva incrustado código ajeno:

| Origen | Licencia | Dónde acaba |
|---|---|---|
| Trazados de los iconos de Phosphor (`src/ui/icon-data.ts`, generado; D-R19 sacó `@phosphor-icons/react` de las dependencias) | MIT | `dist/ui-*.js`. El comentario de atribución del fuente desaparece al minificar: 0 apariciones de «Phosphor» en `dist/` |
| `maplibre-gl/dist/maplibre-gl.css` (`@import` en `src/map/maplibre.css`) | BSD-3-Clause | `dist/karpo-map.css`, casi entera (71 kB) |
| `@xyflow/react/dist/base.css` (`@import` en `src/tree/tree.css`) | MIT | `dist/karpo.css` (42 selectores `.react-flow__*`) |

- **MIT** exige incluir el aviso de copyright y de permiso «in all copies or substantial portions».
- **BSD-3** exige reproducirlo en toda redistribución binaria.

Antes, Phosphor llegaba como dependencia con su propio `LICENSE`. Ahora va copiado en el paquete y
ningún fichero del paquete lo atribuye. Publicar así incumple las licencias, y el requisito pide un
empaquetado preparado para publicar. El arreglo es barato:

- `scripts/licencias.mjs` genera `dist/THIRD-PARTY-NOTICES.md` (o `NOTICE`) con el texto de licencia
  y el copyright de cada origen incrustado (Phosphor, MapLibre GL JS y xyflow). Si se incrusta algo
  más (por ejemplo, la hoja de driver.js), entra también.
- `package.json#files` lo incluye y el README lo enlaza.
- `pack:check` falla si falta el fichero o si falta un origen conocido. Conviene detectarlos de forma
  automática, por ejemplo cruzando los `@import` de los CSS y `icon-data.ts`.
- Opcional: conservar un comentario `/*! … */` en cabeza de `karpo-map.css` y de `karpo.css`.

## Mejoras (no bloquean por sí solas, pero son las que separan este 8,2 de un 9)

### Diseño y jerarquía

1. **Título duplicado en todas las pantallas.**
   - La barra superior pinta `<h1>Productos</h1>` y, 50 px más abajo, la cabecera de entidad pinta
     `<h2>Productos</h2>` con el recuento. Se ve en todas las capturas de escritorio y lo confirma el
     sondeo.
   - Es la mayor pérdida de jerarquía del producto y es nueva de esta ronda (M-7 añadió
     `EntityHeader` sin quitar el título de `TopBar`).
   - Propuesta como Linear o Attio: la barra superior lleva migas o la sección («Comercial ›
     Productos», texto secundario) y un único `h1` en la cabecera de entidad con recuento y acciones.
     En modo compacto, el `h1` sube a la barra.
2. **«Sin dato» domina el mapa.**
   - La trama resolvió la ambigüedad de luminancia (M-2), pero ahora cubre el 99 % del mundo
     (247 de 248), 17 de 19 comunidades y 10 de 14 municipios.
   - El dato real (un país, dos comunidades) queda ahogado en ruido diagonal. En oscuro el efecto es
     aún más pesado.
   - Conviene un «sin dato» liso y casi del color del lienzo, con borde fino, y la trama sólo en la
     leyenda o con baja opacidad. Debe seguir pasando la prueba de ΔL respecto a `seq-1`.
3. **Columna «Estado» cortada en la escena del detalle.**
   - En `detalle-edicion-*` la rejilla al 55 % enseña «Es» y media insignia junto a las acciones
     fijadas. El integrador lo da como abierto, aunque M-8 lo planificaba.
   - Opciones:
     - Ocultar columnas de baja prioridad cuando el panel está abierto (`priority` en la columna).
     - Estrechar el panel por defecto.
     - Que la última columna visible nunca quede a medias (degradado y «+1 columna»).
4. **Espacio muerto en el detalle.** El formulario de instalación ocupa la mitad superior del panel
   y deja ~230 px vacíos encima del pie. Dos columnas a 28em están bien. Faltan:
   - un resumen o metadatos (creado, modificado, `version`), o pestañas de relaciones;
   - o un pie que suba con el contenido.
5. **Nivel región de un solo elemento.**
   - `skipSingle` funciona al navegar: la miga «Comunidad de Madrid» del nivel ciudad sale como
     saltada.
   - Pero `mapa-region-*` sigue enseñando el cuadrado de 430 px porque la captura entra por URL. O
     se cambia la escena, o al entrar por URL en un nivel saltable se redirige al siguiente.
6. **Geometría esquemática y rótulos mezclados.** Los blobs del mundo y las rejillas de España
   siguen siendo lo más débil visualmente. Además, los rótulos mezclan código ISO («CA», «US»,
   «KZ») con nombre («Rusia», «China», «Libia»). Depende de Q-8. Mientras tanto, conviene un criterio
   único (siempre código, o nombre sólo si cabe entero en todos).
7. **Detalles de pulido.**
   - El vacío se pinta «-» (guion) en la rejilla y «—» (raya) en el detalle: hay que unificar.
   - Las cifras de la rejilla van en monoespaciada, otra familia en mitad de la fila. Mejor
     `font-variant-numeric: tabular-nums` sobre la familia del cuerpo, si el tema la tiene.
   - «Responsable de Manteni…» se trunca en el diagrama con espacio libre en la tarjeta.
   - Los contadores `≥14` del árbol en lista quedan a 1100 px de su etiqueta en escritorio.
   - «50 de 120 filas» sigue pegado al borde de la barra lateral, sin relleno.

### Ayuda guiada

8. **Recorridos muy cortos.**
   - El de productos tiene 2 pasos (filtros y tabla): el tercero (`grid.batch-bar`) se omite porque
     no hay cambios.
   - No enseña la cabecera, «Nuevo», las vistas, columnas, deshacer ni la paleta, aunque esas anclas
     existen.
   - Propuesta: los pasos de la configuración se completan con las anclas visibles que no
     mencionan, o la demo configura recorridos completos. El paso opcional de la barra de lote debería
     enseñarse con un texto alternativo («aparece al editar») en lugar de desaparecer.
   - El botón de cerrar del popover se ve diminuto (una «×» de ~8 px de dibujo).

### Accesibilidad

9. **Casillas de 13×13 px en táctil.** Pasan 2.5.8 por espaciado, pero a 390 px con `hasTouch`
   deberían ocupar la celda (diana ≥ 24 px, idealmente 44). Basta con `inline-size` y `block-size`
   del token de control en `(pointer: coarse)`.
10. **RTL: las capturas no demuestran la calidad bidi.**
    - La variante `rtl` es inglés con `?dir=rtl`. Con texto latino en un párrafo RTL, el aislamiento
      FSI/PDI (bien implementado en `src/i18n/bidi.ts`) no puede evitar «facilities 30 of 18»,
      «.Check the marked fields :1», «2 Step 2 of» o «.boundaries 248 :World». Un lector humano lee
      lo contrario de lo que dice.
    - La disposición espejada sí es correcta: barra lateral, migas, chevrons, árbol, diagrama,
      leyenda y columnas.
    - Para que la variante tenga sentido, la demo necesita un idioma RTL de verdad, aunque sea
      parcial: un pseudo-idioma `ar-XA` o `he` con las cadenas de `core`, `grid`, `map` y `tree`. Las
      capturas `rtl` deben usarlo.
11. **Encabezados duplicados.** Lo mismo que el punto 1, visto desde el lector de pantalla: dos
    encabezados consecutivos con el mismo texto.

### Rendimiento

12. **Presupuesto justo.**
    - 242,7 de 250 kB gzip: cualquier función nueva lo rompe.
    - Siguen en el trozo inicial `icon-data.ts` completo (todos los iconos de `ICON_NAMES`, aunque el
      manifiesto use una docena), `react-hook-form` y `zod/mini` (sólo los usan el detalle y el
      alta) y los contratos.
    - Conviene:
      - partir `icon-data` (los de la barra y el menú en el inicial, el resto con la vista);
      - mover `form-model` con el detalle;
      - fijar el objetivo en ≤ 200 kB.
13. **Columnas no virtualizadas.** Está anotado (≤ ~40 columnas). Suficiente por ahora.

### Licencias y documentación

14. La atribución de `@xyflow/react` y Q-1 a Q-10 siguen abiertas a Javier. No bloquean, pero B-1 se
    resuelve con independencia de ellas.

## Lo que está bien (y hay que conservar)

- **Configuración.** No hay ninguna pantalla escrita a mano:
  - Las 8 configuraciones de Go se leen tal cual desde `front/angular/config/entities`.
  - La demo sólo añade superposiciones `*.i18n.json` generadas (`gen-demo-i18n.mjs`, con `--check`)
    y 4 entidades propias.
  - El recorrido de ayuda sale de `overlay.help.steps` o de las anclas.
- **Temas.**
  - Ni un literal de color, medida, familia o peso en ~3400 líneas de CSS; stylelint lo impone.
  - Las únicas excepciones son la escala `z-index` y los puntos de ruptura `@container` en `em`, que
    no admiten variables.
  - Fuera de las pruebas no hay colores literales en TS; la sonda de tema resuelve para WebGL y
    driver.js.
  - No hay `:dir()`, prohibido por regla.
- **Rejilla.** Patrón APG correcto:
  - una única parada de tabulación;
  - flechas, inicio y fin, página;
  - edición con F2 o Enter;
  - `Tab` en edición confirma y avanza;
  - deshacer y rehacer, y lote flotante compacto.
  - El doble clic abre el detalle en edición con el foco en el primer campo.
- **Organigrama.** Arranca legible (escala ~1, centrado en la raíz), contadores `≥n` con nombre
  accesible, enlace «Ver como lista», roles `group` y árbol `role=tree` completo con búsqueda y
  navegación por coincidencias.
- **Mapa.**
  - Navegación mundo › país › región › ciudad con migas, `Esc` para subir y tabla equivalente del
    mismo nivel.
  - Rótulos que caben (medidos y con salto de línea), leyenda con rangos `formatRange` y relleno de
    `fitBounds`.
  - Cero peticiones de terceros; la hoja de MapLibre va aparte, en `@karpo/react/map.css`.
- **Idiomas.** Inglés completo en pantalla. Hay prueba e2e que falla ante cualquier literal español.
  Las insignias y catálogos se traducen y la caché se invalida al cambiar de idioma.
- **Móvil.** Barra compacta con «Más», menú como cajón, detalle a página completa y vista por
  iconos. No hay desbordamiento a 360 px.
- **Licencias de dependencias.** Las 50 directas son MIT, Apache-2.0 o BSD, sin axe-core ni d3
  directos. Lock confirmado y todo del registro público. El informe está generado y comprobado.
- **Paquete.** `prepublishOnly` bloquea la publicación. `exports` limpio (`.`, `./karpo.css`,
  `./map.css`) y tipos generados. `pack:check` vigila el contenido, aunque le falta lo de B-1.

## Para la ronda 3

- **Imprescindible:** B-1, con su comprobación en `pack:check`.
- **Para pasar de 8,5:** las mejoras 1 (título único), 2 («sin dato» discreto), 3 (columna cortada),
  8 (recorridos completos) y 10 (idioma RTL real en la demo y en sus capturas).
- **Después, para acercarse a 9:**
  - 4 (detalle sin vacío);
  - 7 (pulido);
  - 9 (casillas táctiles);
  - 12 (presupuesto con holgura real).
