# Capturas de la demo

Generadas por `npm run capturas` (`scripts/capturas.mjs`, PLAN §7). No se editan a mano: se regeneran.

- **Fecha:** 2026-11-27
- **Navegador:** Microsoft Edge 158.0.4370.33 (playwright-core, `channel`; WebGL por software con `--enable-unsafe-swiftshader`)
- **Ventana:** 1440×900 (390×844 con pantalla táctil en la variante `movil`), escala 1, movimiento reducido (`prefers-reduced-motion: reduce`), fuentes del sistema
- **Árabe:** la variante `rtl` es árabe real (`lng=ar`); es una traducción de demostración **sin revisión nativa** (Q-5 sigue abierta) y los datos de la demo (nombres de puestos, delimitaciones) no se traducen
- **Datos:** demo determinista (MSW en el navegador); ninguna petición salió del origen de la demo

## Variantes

- `claro` (1440×900, 9 escenas): modern-flat, modo claro, español
- `oscuro` (1440×900, 9 escenas): modern-flat, modo oscuro, español
- `rtl` (1440×900, 9 escenas): modern-flat, modo claro, árabe (`lng=ar`, `dir=rtl` por el idioma; traducción de demostración sin revisión nativa; los datos de la demo no se traducen)
- `ingles` (1440×900, 9 escenas): modern-flat, modo claro, inglés (LTR), 1440×900
- `movil` (390×844, 5 escenas): modern-flat, modo claro, español, 390×844 con `isMobile` y `hasTouch` (sólo cinco escenas)

## Capturas

| Escena | Qué enseña | Claro | Oscuro | RTL | Inglés | Móvil |
|---|---|---|---|---|---|---|
| Cuadrícula editable | Productos con tres celdas sucias, una inválida con su error, una en edición y la barra de cambios por lotes (en el móvil, sólo con las columnas que caben y sin la celda inválida) | [`cuadricula-editable-claro.png`](cuadricula-editable-claro.png) | [`cuadricula-editable-oscuro.png`](cuadricula-editable-oscuro.png) | [`cuadricula-editable-rtl.png`](cuadricula-editable-rtl.png) | [`cuadricula-editable-ingles.png`](cuadricula-editable-ingles.png) | [`cuadricula-editable-movil.png`](cuadricula-editable-movil.png) |
| Detalle en edición | Instalación abierta con doble clic desde la lista: un campo cambiado y un error de campo | [`detalle-edicion-claro.png`](detalle-edicion-claro.png) | [`detalle-edicion-oscuro.png`](detalle-edicion-oscuro.png) | [`detalle-edicion-rtl.png`](detalle-edicion-rtl.png) | [`detalle-edicion-ingles.png`](detalle-edicion-ingles.png) | [`detalle-edicion-movil.png`](detalle-edicion-movil.png) |
| Organigrama · listado | Árbol de puestos con dos ramas desplegadas, una plegada y una búsqueda activa | [`organigrama-listado-claro.png`](organigrama-listado-claro.png) | [`organigrama-listado-oscuro.png`](organigrama-listado-oscuro.png) | [`organigrama-listado-rtl.png`](organigrama-listado-rtl.png) | [`organigrama-listado-ingles.png`](organigrama-listado-ingles.png) | [`organigrama-listado-movil.png`](organigrama-listado-movil.png) |
| Organigrama · diagrama | Diagrama a tamaño legible, centrado en la raíz, con una rama desplegada, contadores de descendientes y el minimapa | [`organigrama-diagrama-claro.png`](organigrama-diagrama-claro.png) | [`organigrama-diagrama-oscuro.png`](organigrama-diagrama-oscuro.png) | [`organigrama-diagrama-rtl.png`](organigrama-diagrama-rtl.png) | [`organigrama-diagrama-ingles.png`](organigrama-diagrama-ingles.png) | — |
| Mapa · mundo | Instalaciones por país (coropletas) | [`mapa-mundo-claro.png`](mapa-mundo-claro.png) | [`mapa-mundo-oscuro.png`](mapa-mundo-oscuro.png) | [`mapa-mundo-rtl.png`](mapa-mundo-rtl.png) | [`mapa-mundo-ingles.png`](mapa-mundo-ingles.png) | [`mapa-mundo-movil.png`](mapa-mundo-movil.png) |
| Mapa · país | Comunidades autónomas de España | [`mapa-pais-claro.png`](mapa-pais-claro.png) | [`mapa-pais-oscuro.png`](mapa-pais-oscuro.png) | [`mapa-pais-rtl.png`](mapa-pais-rtl.png) | [`mapa-pais-ingles.png`](mapa-pais-ingles.png) | — |
| Mapa · región | Provincias de Cataluña (nivel región): cuatro delimitaciones, una con datos y tres sin dato | [`mapa-region-claro.png`](mapa-region-claro.png) | [`mapa-region-oscuro.png`](mapa-region-oscuro.png) | [`mapa-region-rtl.png`](mapa-region-rtl.png) | [`mapa-region-ingles.png`](mapa-region-ingles.png) | — |
| Mapa · ciudad | Municipios de Madrid con las migas completas | [`mapa-ciudad-claro.png`](mapa-ciudad-claro.png) | [`mapa-ciudad-oscuro.png`](mapa-ciudad-oscuro.png) | [`mapa-ciudad-rtl.png`](mapa-ciudad-rtl.png) | [`mapa-ciudad-ingles.png`](mapa-ciudad-ingles.png) | — |
| Ayuda guiada | Recorrido de la pantalla de productos con el segundo paso abierto | [`ayuda-recorrido-claro.png`](ayuda-recorrido-claro.png) | [`ayuda-recorrido-oscuro.png`](ayuda-recorrido-oscuro.png) | [`ayuda-recorrido-rtl.png`](ayuda-recorrido-rtl.png) | [`ayuda-recorrido-ingles.png`](ayuda-recorrido-ingles.png) | [`ayuda-recorrido-movil.png`](ayuda-recorrido-movil.png) |

Total: 41 imágenes.
