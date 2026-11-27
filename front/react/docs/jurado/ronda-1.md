# Jurado, ronda 1: front React de Karpo Go (`@karpo/react`)

Fecha: 2026-11-27. Commit revisado: `fef34d1` (rama `claude/front-react-7b8564`).
Papel: jurado exigente, con el listón de Awwwards y de los mejores SaaS de gestión (Linear, Attio,
Stripe), WCAG AA y rendimiento.

## Veredicto

**No aprobado. Nota: 7,0 / 10.**

La ingeniería es sólida. La arquitectura sigue el plan, la disciplina de tokens se cumple casi
siempre, hay carga diferida, el informe de licencias es limpio y la batería de pruebas es amplia.
Pero quedan **cinco bloqueantes**. Cuatro se ven en las propias capturas que el integrador dio por
revisadas: iconos de relleno en el menú, inglés a medias, tipografía fija en el mapa y una barra
superior que desborda en móvil. El quinto es el teclado de la cuadrícula, que contradice el patrón
`grid` de WAI-ARIA. El diseño es correcto y ordenado, pero hoy no llega al nivel de Linear o Attio.

## Lo que he comprobado yo mismo

| Comprobación | Resultado |
|---|---|
| `npm ci` | 417 paquetes, 0 vulnerabilidades. Aviso `EBADENGINE`: `engines.node >=24.15` y el equipo tiene Node 24.14.1 |
| `npm run check` (tipos, eslint + stylelint, test, contrato, licencias `--check`, build, `pack:check`) | Código 0. Test: 77 ficheros y 1130 pruebas. Contrato: 3 ficheros y 98 pruebas. Licencias: 446 paquetes y 50 directas, sin incidencias. `pack:check`: 192 ficheros, todos dentro de `dist/` |
| `npm run test:e2e` (Edge instalado) | 9 ficheros pasan y 1 se salta; 58 pruebas pasan y 3 se saltan (las de `local.e2e.ts` sin `KARPO_LOCAL=1`), en 150 s. Ningún bloqueante de este informe tiene hoy una prueba que lo detecte |
| Sondeo propio con playwright-core y Edge contra `vite preview` de la demo (puerto 18977, parado al terminar) | Resultados citados en cada punto. 0 peticiones fuera del origen |
| Las 27 capturas de `docs/capturas`, leídas una a una | Comentarios abajo |
| Revisión de código | `src/grid`, `src/map`, `src/tree`, `src/ui/icons.ts`, `src/config/normalize.ts`, `stylelint.config.js`, manifiesto de la demo |

## Bloqueantes

### B-1. Iconos del menú: círculos genéricos en 7 entradas

El manifiesto de la demo usa los iconos `buildings`, `tree-structure`, `globe-hemisphere-west`,
`map-trifold` y `calendar-dots`. Ninguno existe en `src/ui/icons.ts`, así que caen en
`FALLBACK_ICON` (un círculo) sin ningún aviso. Se ve en todas las capturas: Organización,
Instalaciones, Puestos, Geografía, Países, Delimitaciones y Festivos. Para un producto que presume
de configuración, el fallo silencioso es tan grave como el aspecto.

Arreglo:
- Añadir esos nombres al registro, o alias de la nomenclatura de Phosphor.
- Emitir un `ConfigWarning` cuando un icono no se resuelva.
- Añadir una prueba de contrato que exija que todo icono del manifiesto y de las configuraciones de
  la demo se resuelva.

### B-2. Inglés a medias: el requisito es+en no se cumple en pantalla

Con `lng=en`, mi sondeo da esto:
- **Errores de código:**
  - La leyenda del mapa dice «Number of **instalaciones** per boundary». En
    `src/map/MapView.tsx:159` se usa `config.namePlural.literal` (el literal en español) más
    `toLowerCase()`, en lugar del texto resuelto. Además, pasar a minúsculas no vale para todos los
    idiomas.
  - Las insignias del organigrama salen como «Cubierto» en inglés, aunque `position.config.json`
    lleva `labelKey: entities.position.status.filled` y `demo/config/i18n/en.json` tiene «Filled».
    El sondeo lee `['Cubierto', 'Cubierto', …]` en `[role=treeitem] [data-kx-part=badge]`.
- **Contenido:**
  - Las cabeceras de la cuadrícula de productos siguen en español: «Referencia, Nombre, Naturaleza,
    Unidad, Precio base, Retirado desde».
  - Lo mismo pasa con las secciones y etiquetas del detalle de instalaciones y los valores de
    `badgeMap` («Bien», «Servicio», «Activa»).
  - TC-3 lo pide a Theros con razón, pero el manifiesto de la demo ya admite `overlay` y
    `*Key`/`{es,en}`. La demo debe traer sus superposiciones de texto para las 8 entidades de Go
    mientras TC-3 no llegue.
  - Hace falta una prueba e2e en inglés que falle si aparece un literal de configuración sin
    traducir.

### B-3. Tipografía fija en el mapa

`maplibre-gl.css` entra en la capa `karpo.components.maplibre` con
`.maplibregl-map{font:12px/20px Helvetica Neue,Arial,Helvetica,sans-serif}`. El sondeo da, para
`.kx-map-label`, `fontFamily = "Helvetica Neue", Arial, Helvetica, sans-serif`. Se nota en las
capturas: los rótulos del mapa están en otra familia que el resto de la interfaz. Esto incumple
«todas las tipografías salen de sus variables».

Arreglo:
- Escribir `[data-kx-part="map"] .maplibregl-map { font: inherit; }` (o la familia del tema) en
  `map.css`.
- Ampliar la prueba de contrato para que compruebe la `font-family` calculada de los rótulos.

### B-4. La cuadrícula atrapa la tecla Tab

En modo navegación (sin editar), `Tab` avanza de celda en celda (`keyboard.ts`, `nextPosition`).
Con 60 pulsaciones el foco sigue dentro de la rejilla de productos, que tiene unas 960 celdas
cargadas. Lo mismo ocurre con la rejilla de instalaciones, cuyo panel de detalle va después en el
orden de foco. El patrón `grid` de WAI-ARIA (APG) hace de la rejilla una única parada de
tabulación, y las flechas ya cubren el movimiento. Sin esto, un usuario de teclado no tiene una
forma razonable de salir (WCAG 2.1.1 y 2.4.3; casi una trampa de teclado según 2.1.2).

Arreglo:
- En navegación, `Tab` y `Shift+Tab` salen de la rejilla.
- Mientras se edita, `Tab` confirma y pasa a la celda siguiente, como en una hoja de cálculo.
- Una prueba e2e que lo compruebe.

### B-5. Reflujo en móvil (WCAG 1.4.10 AA)

A 390×844, `document.documentElement.scrollWidth` vale 453: hay scroll horizontal de página. La
barra superior desborda: el título de la entidad desaparece, los iconos se cortan, se pinta la
tecla `Ctrl K` en un dispositivo táctil y hay dos iconos casi iguales seguidos. Que la rejilla
tenga su propio scroll es aceptable (es una tabla de datos), pero el marco no.

Arreglo:
- Barra superior con `@container`: título primero y acciones secundarias en un menú «Más».
- `kbd` oculto con `(pointer: coarse)`.
- Prueba e2e a 360 px que exija `scrollWidth <= innerWidth`.

## Mejoras importantes (no bloquean por sí solas, pero bajan la nota)

### Diseño y jerarquía

1. **Organigrama legible.** En la captura del diagrama, el ajuste a la vista deja los nodos a
   escala ~0,6, con unos 7–8 px de texto: no se puede leer a 1440×900.
   - Limitar el zoom mínimo del ajuste inicial (≥ 0,85) y centrar en la raíz.
   - O bien un zoom semántico: con zoom bajo, sólo título y contador, en cuerpo legible.
   - Revisar también el contador «14+» (el «+» es ambiguo: ¿descendientes o «más de»?).
2. **Coropletas.**
   - En claro, el primer paso (Cataluña, 12; Alcalá, 4) apenas se distingue de «Sin dato».
   - En oscuro la luminancia se invierte: los valores bajos son *más oscuros* que «Sin dato».
   - Hace falta un «sin dato» con trama o claramente neutro, y una diferencia de luminancia mínima
     comprobada entre `--kx-data-missing` y `--kx-data-seq-1` en los dos modos.
3. **Mapa, nivel país.**
   - Rótulos que se salen de su tesela: «Comunidad Foral de Navarra», «Ciudad de Ceuta», «Región
     de Murcia».
   - Dos teselas sin rótulo (norte).
   - La leyenda tapa Canarias.
   - Hace falta un `fitBounds` con relleno igual al tamaño de la leyenda.
4. **Mapa, nivel región.** Con una sola delimitación se pinta un cuadrado azul de 700 px. Cuando el
   nivel tiene un único elemento, conviene saltarlo o limitar el zoom.
5. **Geometría esquemática.** Q-4 la acepta y está rotulada, pero visualmente es lo más débil del
   producto: hexágonos en el mundo y rejillas en España. Conviene proponer a Javier una fuente
   real con licencia admitida (dominio público, como Natural Earth, convertida por script a
   PMTiles propio) para la demo.
6. **Dos interruptores con el mismo nombre.**
   - En la barra superior: «Listado | Organigrama | Mapa».
   - En la subbarra: «Listado | Diagrama» y «Mapa | Listado».
   - «Listado» significa dos cosas distintas en la misma pantalla. Conviene unificar en un solo
     control segmentado («Tabla · Árbol · Diagrama · Mapa») o renombrar la subbarra.
7. **Falta el botón «Nuevo».** Sólo se crea desde Ctrl+K. Cualquier SaaS de referencia pone la
   acción primaria visible en la cabecera de la lista. Lo mismo para el número de registros, que
   no aparece en la cabecera.
8. **Panel de detalle.**
   - La columna «Estado» queda cortada («Es» y una insignia partida) por la columna de acciones
     fijada.
   - Las pistas (`hint`) de la configuración de instalaciones no se pintan, aunque el plan §4.10 lo
     exige.
   - `columns: 2` no se nota.
   - Mucho espacio muerto bajo el formulario.
   - Valores de catálogo sin traducir («Room», «Building» en la interfaz española).
9. **Barra de lote.** El bloque ámbar ocupa todo el ancho y pesa más que los propios datos. Una
   barra flotante compacta o una franja más sutil se acerca más a Linear o Airtable. Los botones
   deshacer y rehacer tienen un estilo distinto entre sí (uno con borde y otro sin él).
10. **Textos pegados al borde.** «Mundo: 248 delimitaciones.» y la nota de accesibilidad del mapa
    no tienen relleno lateral y tocan la barra lateral.

### RTL

11. Los chevrons de las migas del mapa («Madrid › Comunidad de Madrid › Spain › World») y los del
    árbol plegado no se espejan.
12. Las cadenas mezcladas no se aíslan: «changes 4», «row cannot be saved 1», «to 14 12»,
    «.Check the marked fields: 1», «…Search». Con árabe real se notaría menos, pero los números
    con texto deben ir en `<bdi>` o con `dir="auto"`, y los rangos se deben formatear con
    `Intl.NumberFormat#formatRange`.

### Accesibilidad

13. Los nodos del diagrama no tienen `role` (`null` en el sondeo). La lista `role=tree` es la
    alternativa accesible, lo cual es correcto, pero el diagrama debería anunciarse
    (`role=group`/`img` con nombre, o `aria-hidden` con un enlace a la lista).
14. El contraste medido en claro es correcto:
    - Insignias: 8,2:1.
    - Cabeceras: 8,7:1.
    - Celdas: 15:1.
    - Acciones: 4,52:1, justo en el límite.
    - Cerrar del recorrido: 36×28 px.
    - El foco vuelve al botón «Ayuda» al cerrar el recorrido.
    - Conviene una prueba de contraste automática de los pares de tokens usados en los dos modos.

### Rendimiento

15. **Trozo inicial de la demo:** 1,41 MB minificado y 412 kB gzip. Lo que más pesa:
    - Iconos Phosphor, con los seis pesos de cada icono: ~450 kB de fuente.
    - `zod/v4` completo.
    - `react-resizable-panels`, `react-hook-form`, `driver.js`, `fuse.js` y `cmdk`, todos con carga
      inmediata.

    Objetivo: ≤ 250 kB gzip. Para ello:
    - Generar un juego de iconos propio con sólo los pesos que el tema usa.
    - Pasar a `zod/mini`.
    - Cargar de forma diferida el detalle, la paleta y la ayuda.

    Las vistas de rejilla, árbol y mapa ya se cargan de forma diferida, con MapLibre (1,1 MB) y
    @xyflow aparte. Eso está bien.
16. `dist/karpo.css` pesa 142 kB y mete la hoja de MapLibre a todo consumidor del paquete, aunque
    no use mapas. Mejor separarla (`@karpo/react/map.css`) o cargarla con la vista.
17. Las columnas no se virtualizan (el plan §3 lo menciona). Es aceptable con las anchuras de las
    configuraciones actuales, pero conviene anotarlo.

### Empaquetado y documentación

18. `engines.node >=24.15` hace que `npm ci` avise en el propio equipo (Node 24.14.1). Hay que
    bajarlo a lo que de verdad se exige o documentar la actualización.
19. `docs/LICENCIAS.md` dice «Aceptada en Q-6» para `lightningcss`, pero Q-6 sigue siendo una
    consulta abierta a Javier. Debería decir «por defecto mientras Javier no decida».

## Lo que está bien (y hay que conservar)

- **Configuración.** No hay ninguna pantalla escrita a mano: las 8 configuraciones de Go se leen
  tal cual, con normalizador zod, superposiciones, avisos e informe de compatibilidad clave a
  clave.
- **Temas.**
  - No hay colores ni medidas literales en las 2994 líneas de CSS. stylelint lo impone; la única
    excepción justificada es la escala `z-index`.
  - En TS no hay colores fijos.
  - `style` sólo lleva variables de disposición.
  - Hay una sonda `resolveThemeColor` para WebGL y driver.js.
- **Cuadrícula.**
  - `role=grid` con `aria-rowcount`, `aria-colcount`, `aria-rowindex`, `aria-colindex`,
    `aria-sort` y tabindex itinerante.
  - Filas virtualizadas.
  - F2 y Enter editan, la escritura sobrescribe, Ctrl+Z deshace.
  - Doble clic → `/product/<id>?mode=edit` con el foco en el primer campo (comprobado).
  - Guardado por lotes con informe y estados de celda.
- **Soberanía.** 0 peticiones fuera del origen en todo mi recorrido. PMTiles propio y origen
  configurable.
- **Licencias.** Las 50 directas son MIT, Apache-2.0 o BSD. Sin axe-core ni d3 directos. Informe
  generado y comprobado.
- **Ayuda.** driver.js con hoja propia, pasos de la configuración, `role=dialog` y foco devuelto.
- **Paquete.** `prepublishOnly` lo bloquea. `pack:check` valida el contenido y los selectores de
  cada hoja.

## Para la ronda 2

Hay que resolver B-1 a B-5, cada uno con su prueba. Las mejoras 1, 2, 6, 7 y 15 son las que más
suben la nota. Después hay que regenerar las capturas y añadir dos nuevas: inglés LTR y móvil
a 390 px.
