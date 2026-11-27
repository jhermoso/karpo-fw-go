# Jurado, ronda 3: front React de Karpo Go (`@karpo/react`)

Fecha: 2026-11-27. Commit revisado: `477887f` (rama `claude/front-react-7b8564`).
Papel: jurado exigente, con el listón de Awwwards y de los mejores SaaS de gestión (Linear, Attio,
Stripe), WCAG AA y rendimiento.

## Veredicto

**Aprobado. Nota: 8,6 / 10.**

No queda ningún bloqueante. B-1 (avisos de terceros) está resuelto y lo vigila `pack:check`. De las
mejoras que el jurado de la ronda 2 pedía para pasar de 8,5 (1, 2, 3, 8 y 10), las cinco están
hechas y las he visto en las capturas. También lo están 4, 5, 6, 7, 9 y 14. El producto tiene ya una
jerarquía limpia, con un solo título, migas, cabecera con recuento y acciones. La rejilla no corta
columnas, la ficha no tiene huecos muertos, el recorrido de ayuda es completo y el árabe es real.
El conjunto se lee como un SaaS de gestión serio.

No llega a 9 por cuatro motivos:

- el presupuesto sigue al límite (1,9 kB de margen);
- la vista de árbol en lista carga todo el diagrama;
- el mapa sigue siendo esquemático (Q-8) y tiene dos defectos visibles en el nivel mundo;
- la ficha no marca qué campo ha cambiado.

Son las mejoras de abajo. Ninguna bloquea.

## Lo que he comprobado yo mismo

| Comprobación | Resultado |
|---|---|
| `npm ci` | 417 paquetes, 0 vulnerabilidades. Sólo avisos `EBADDEVENGINES`/`EBADENGINE`: el equipo tiene Node 24.14.1 y el paquete pide ≥ 24.15 con `onFail: warn` |
| `npm run check` | Código 0. Incluye tipos, eslint y stylelint, y 104 ficheros con 1650 pruebas unitarias y de componentes. También 8 ficheros con 176 pruebas de contrato, `licencias --check` (443 paquetes, 50 directas), `icons:gen --check` (núcleo y resto), `i18n:demo --check` (268 textos), el build de biblioteca y demo, el presupuesto y `pack:check` (326 ficheros, avisos de 3 orígenes) |
| Presupuesto | 213,1 kB gzip del trozo inicial, con límite obligatorio de 215 kB: margen de 1,9 kB. El objetivo de 200 kB no se alcanza (faltan 13,1 kB). `karpo-react.js`: 96,4 kB gzip |
| `npm run test:e2e` (Edge instalado) | Código 0: pasan 16 ficheros y se salta 1; pasan 181 pruebas y se saltan 3 (`local.e2e.ts` sin `KARPO_LOCAL`), entre ellas `rtl-ar` |
| Las 41 capturas de `docs/capturas`, leídas una a una con Read | Comentarios abajo. He medido píxeles de los mapas en claro y oscuro |
| Licencias | Las 50 directas son MIT, Apache-2.0 o BSD (cruzado con `package-lock.json`). No hay ninguna `resolved` fuera de `registry.npmjs.org`. Transitivas: MIT, ISC, BSD, Apache, 0BSD, CC0, MIT-0, BlueOak, CC-BY-4.0 (caniuse-lite), PSF-2.0, y 12 MPL-2.0 (lightningcss y sus binarios, herramienta de compilación que no se distribuye; Q-6). Ni copyleft fuerte ni comerciales. axe-core y d3 no son directas: d3 entra transitivo por xyflow |
| Paquete | `@karpo/react@0.1.0`, `files: ["dist"]` y `exports` `.`, `./karpo.css` y `./map.css`. `prepublishOnly` bloquea la publicación. `dist/THIRD-PARTY-NOTICES.md` trae MapLibre GL JS 6.19.0 (BSD-3), Phosphor 2.1.10 (MIT) y xyflow 13.0.3 (MIT) con su texto íntegro. `karpo.css` y `karpo-map.css` empiezan por `/*! Incluye partes de … */` |
| Tema | 0 literales de color o `px` en los CSS de `src/`. En TS/TSX sólo hay `rgb()` en la sonda del tema, en `pattern.ts` (conversión para WebGL) y en utilidades de prueba. Los únicos `style={{…}}` son posiciones del virtualizador. El `color` hexadecimal de las configuraciones de Go se ignora con aviso y el tono sale del tema (`normalize.ts`) |
| Idiomas | es, en y ar con las mismas claves en los 10 espacios de nombres. El árabe trae las categorías de plural propias (`_two`, `_few`, `_many`…) en `grid`, `map`, `tree` y `detail` |
| Accesibilidad (código) | Rejilla `role=grid` con `aria-rowcount`, `aria-colcount` (cuenta sólo las columnas pintadas tras el ajuste) y `aria-multiselectable`. Hay un solo `h1` por pantalla (`EntityHeader`). El interruptor de vistas compacto lleva `aria-label` y `title` |
| Rendimiento (trozos de la demo, gzip) | Trozo inicial de 207 kB. Mapa aparte (`maplibre-gl` 298 kB) y árbol aparte (108 kB, véase M-2). `msw` (188 kB) sólo en la demo |
| `git status` | Limpio tras `npm ci`, `check` y e2e |

## Bloqueantes

Ninguno.

## Estado de lo que pidió la ronda 2

| Ronda 2 | Estado | Cómo lo he comprobado |
|---|---|---|
| B-1 avisos de terceros | **Resuelto** | `dist/THIRD-PARTY-NOTICES.md` con los 3 orígenes y su texto. Cabeceras `/*!` en las dos hojas. `pack:check` lo exige y detecta orígenes nuevos |
| 1 y 11 título duplicado | **Resuelto** | La barra superior lleva migas («Comercial › Productos») y el contenido, un único `h1` con recuento. En móvil, el título queda en la barra |
| 2 «sin dato» domina el mapa | **Resuelto en lo esencial** | La trama es mucho más suave y el dato real se ve. En oscuro, el tramo bajo se confunde con el fondo (M-4) |
| 3 columna «Estado» cortada | **Resuelto** | El detalle muestra Nombre, Tipo, Superficie y Estado enteros. En móvil, el pie tiene contador «+4» |
| 4 espacio muerto en el detalle | **Resuelto** | Tarjetas «Relaciones», con enlace y «Ver en el árbol», y «Registro», con el identificador copiable y la versión |
| 5 nivel región de un elemento | **Resuelto** | La escena es Cataluña, con 4 provincias |
| 6 rótulos mezclados | **Resuelto** | En el mundo, todos son códigos. Pero el único país con dato no tiene rótulo (M-3) |
| 7 pulido | **Resuelto** | Vacío único «—». Cifras con `tabular-nums` en la familia del cuerpo. Nodos del diagrama a dos líneas. Contador del árbol junto a su etiqueta. Pie «50 de 120 filas» con relleno |
| 8 recorridos cortos | **Resuelto** | 14 pasos en productos (12 en móvil) y cierre con icono SVG |
| 9 casillas táctiles | **Resuelto** | En móvil, la casilla ocupa la celda. Visualmente pesa un poco (M-9) |
| 10 RTL sin idioma real | **Resuelto** | Capturas `rtl` en árabe, con espejo correcto de menú, migas, chevrons, árbol, diagrama, leyenda, barra de lote y recorrido. Cifras latinas |
| 12 presupuesto justo | **A medias** | Bajó de 242,7 a 213,1 kB, pero el límite bajó a 215 y el margen sigue en 1,9 kB (M-1) |
| 13 columnas no virtualizadas | Sin cambio, anotado | — |
| 14 consultas abiertas | **Resuelto** | `docs/CONSULTAS-JAVIER.md` (Q-1 a Q-12, con recomendación y coste), enlazado desde el README |

## Las capturas, una a una (41)

- **Cuadrícula editable** (claro, oscuro, RTL, inglés y móvil).
  - Es la mejor pantalla del producto: densidad correcta y ritmo de filas regular.
  - Hay marcas de celda cambiada, un error en línea con mensaje anclado y un editor activo con anillo
    del tema. La barra de lote flotante es compacta, con atajos `Ctrl S`.
  - En oscuro, el elemento activo del menú es un bloque azul sólido, mucho más pesado que el tinte
    del claro. Es coherente con el tema, pero desequilibra la barra lateral (M-8).
  - En RTL todo se espeja bien: casillas a la derecha, acciones a la izquierda y cifras alineadas al
    final.
- **Detalle en edición** (5).
  - Panel 60/40 sin columnas cortadas y error resumido arriba.
  - Las tarjetas de relaciones y de registro llenan el hueco con información útil.
  - La escena desmarca «Activa», pero la cabecera sigue con la insignia «Activa» y el campo
    cambiado no lleva marca (M-5).
  - En árabe, los tipos («Floor», «Room») salen en inglés por la reserva `ar → en` del catálogo
    (M-7).
- **Organigrama, listado** (5).
  - Limpio, con coincidencia resaltada, contadores junto a la etiqueta y marca de elemento actual.
  - En móvil, títulos y personas se truncan a la vez («Director/a Gene…», «Javier Bermejo F…»):
    conviene ocultar o bajar la persona a una segunda línea en compacto (M-9).
- **Organigrama, diagrama** (4).
  - Tarjetas legibles a escala 1, con plegado `≥n` y minimapa.
  - «Responsable de Mantenimiento» ya va a dos líneas.
  - El encuadre se centra en la raíz: queda mucho vacío a la izquierda y la rama derecha roza el
    minimapa. Mejor encuadrar el conjunto visible (M-10).
- **Mapa** (mundo 5, país 4, región 4, ciudad 4).
  - Las migas por niveles, «Subir un nivel» con `Esc`, el recuento filtrado en la cabecera («12 de
    30 instalaciones») y la leyenda por tramos funcionan bien.
  - La trama de «sin dato» ya no domina.
  - Defectos (M-3 y M-4):
    - en `mapa-mundo-claro` y `mapa-mundo-oscuro`, Groenlandia queda cortada por el borde superior,
      cuando en inglés y en árabe entra entera;
    - España, el único país con dato, es el único sin rótulo;
    - en oscuro, el tramo bajo apenas se distingue del fondo.
  - La geometría esquemática sigue siendo lo más débil visualmente, pero depende de Q-4 y Q-8.
- **Recorrido de ayuda** (5).
  - «Paso 2 de 14», popover con jerarquía clara, botón de cerrar con icono, foco del paso sobre la
    tabla y oscuro y RTL correctos.
  - En móvil, el texto habla de `Ctrl+Z` y `Ctrl+S`, que no existen en un teléfono (M-6).

## Mejoras (no bloquean; son las que separan este 8,6 de un 9)

### Rendimiento

1. **M-1. El presupuesto sigue al límite.**
   - 213,1 de 215 kB: cualquier tarea que toque el trozo inicial lo rompe, y el objetivo de 200 kB
     no se alcanza.
   - Las palancas están anotadas en `docs/notas/R3-R1.md`. Convendría aplicarlas antes de añadir
     funciones, no después.
2. **M-2. La vista de árbol en lista carga el diagrama entero.**
   - El trozo `tree` de la demo pesa 108 kB gzip. Lleva `@xyflow/react`, `@xyflow/system`,
     `@dagrejs/dagre` y siete paquetes `d3-*` junto a `TreeList`.
   - Quien sólo abre el organigrama como lista (la vista «Árbol») paga el diagrama.
   - Hay que partir `TreeDiagram` (y `layout.ts` con dagre) en su propio `lazy()`, igual que el
     mapa.

### Mapa

3. **M-3. Nivel mundo.**
   - El país con dato (España) no lleva rótulo porque no cabe. Precisamente el único elemento que
     importa queda sin nombre. Conviene darle prioridad: un rótulo exterior con guía, o el código
     aunque desborde un poco.
   - Además, en las capturas en español el encuadre deja Groenlandia cortada por arriba (≈ 45 px de
     desfase frente a inglés y árabe). Parece que el `fitBounds` se calcula antes de que el lienzo
     tenga su tamaño final. Hay que volver a encuadrar tras el `ResizeObserver`, o esperar a `idle`
     en la escena.
4. **M-4. Tramo bajo de la paleta en oscuro.**
   - He medido los píxeles. En `mapa-ciudad-oscuro`, el tramo 4-5 (`#334153`) frente al lienzo
     (`#171B21`) da ≈ 1,7:1, y su muestra en la leyenda frente al fondo de la leyenda (`#313842`),
     ≈ 1,05:1: sólo se distingue por el borde. En claro, el tramo bajo frente al lienzo da ≈ 1,3:1.
   - El número dentro de la delimitación y la tabla equivalente evitan que sea un fallo de WCAG
     1.4.1. Aun así, «Alcalá de Henares 4» o «Cataluña 12» se leen a primera vista como «sin dato».
   - Conviene subir la luminancia del primer tramo en oscuro, o dar a las muestras de la leyenda un
     borde de 3:1 frente a su fondo (1.4.11).

### Ficha y textos

5. **M-5. La ficha no marca qué campo ha cambiado.**
   - La rejilla marca la celda con un triángulo, pero en la ficha sólo se ve «Cambios sin guardar»
     en la cabecera.
   - La insignia de estado de la cabecera sigue con el valor guardado («Activa») mientras el campo
     está desmarcado.
   - Hay que marcar el campo con el mismo token `--kx-cell-dirty-mark` (ya existe en `detail.css`
     para los conflictos) y atenuar, o rotular «guardado», la insignia de la cabecera mientras haya
     cambios.
6. **M-6. Atajos de teclado en la ayuda táctil.**
   - En `(pointer: coarse)` se ocultan los `kbd`, pero los textos del recorrido siguen hablando de
     `Ctrl+Z`, `Ctrl+S` y del doble clic.
   - Basta una variante `…_touch` de las claves afectadas, o una frase sin atajos, que se elija
     cuando no hay teclado fino.
7. **M-7. Árabe incompleto en la demo.**
   - Los valores de catálogo del tipo de instalación salen en inglés en árabe.
   - «120 المنتجات» y «30 المنشآت» usan el plural con artículo junto a la cifra, una construcción
     poco natural. Hace falta un patrón de recuento por entidad con plural CLDR, no `{{count}}
     {{namePlural}}`.
   - Depende de Q-5 (revisión nativa), pero el patrón es del marco.

### Pulido visual

8. **M-8. Elemento activo del menú en oscuro.** Es un bloque azul sólido, mientras que en claro es
   un tinte con barra. Conviene usar el mismo lenguaje en los dos modos (tinte y barra), o anotarlo
   en `PETICIONES-THEROS-FRONT.md` si es el token del tema el que lo decide.
9. **M-9. Compacto.**
   - En el árbol en lista, a 390 px, se truncan a la vez el puesto y la persona: mejor ocultar o
     bajar la persona a una segunda línea.
   - Las casillas táctiles a celda completa resuelven el objetivo táctil, pero su borde grueso pesa
     más que el contenido de la fila. Conviene un borde más fino con el mismo objetivo.
10. **M-10. Encuadre inicial del diagrama.** Hay que centrar el conjunto visible, no la raíz.
    Así se evita el vacío lateral y que una rama quede bajo el minimapa.
11. **M-11. Enlace del README.** `README.md` enlaza `dist/THIRD-PARTY-NOTICES.md`, que no existe en
    el repositorio (`dist/` no se versiona). Funciona en el paquete publicado, pero en GitHub el
    enlace está roto. Hay que añadir una nota «se genera al construir» junto al enlace, o enlazar
    también `docs/LICENCIAS.md`.

## Lo que está bien (y hay que conservar)

- **Configuración.**
  - No hay ninguna pantalla escrita a mano. Las 8 configuraciones de Go se leen tal cual desde
    `front/angular/config/entities`.
  - Las tarjetas nuevas de la ficha («Relaciones» y «Registro»), el ajuste de columnas por
    `priority` y los pasos de ayuda salen de la configuración y de las anclas.
  - El `color` hexadecimal de Go se ignora en favor de los tonos del tema.
- **Temas.**
  - Ni un literal de color ni de medida en el CSS, y stylelint lo impone.
  - Claro, oscuro y RTL salen del mismo contrato de variables de Theros Front.
- **Rejilla.** Sigue el patrón APG completo:
  - una parada de tabulación, flechas, edición con F2 o Enter;
  - `Tab` confirma en edición, deshacer y rehacer, y lote con `Ctrl+S`;
  - doble clic, que abre la ficha en edición.
  - Ahora además ajusta columnas al ancho sin tocar las preferencias, y `aria-colcount` es coherente.
- **Árabe real.** Menú, migas, rejilla, ficha, árbol, diagrama, mapa, leyenda y recorrido se
  espejan correctamente y los recuentos se leen en orden. La geometría del mapa no se espeja, y
  así debe ser.
- **Licencias y paquete.** B-1 está resuelto de forma automática y comprobada. Las directas están
  limpias, el lock es del registro público y la publicación está bloqueada.
- **Ingeniería.** 1650 pruebas unitarias y de componentes, 176 de contrato y la e2e completa con
  Edge, sin descargas fuera de npm.

## Para la ronda siguiente (si la hay)

- **Para acercarse a 9:** M-1 (margen real de presupuesto), M-2 (diagrama diferido aparte de la
  lista), M-3 (mundo: rótulo del dato y encuadre) y M-5 (campo cambiado en la ficha).
- **Después:** M-4, M-6 a M-11.
