# Consultas abiertas a Javier (Q-1 a Q-12)

Estado a 2026-11-27 (ronda 3, tarea R3-1; jurado 14 y análisis M-12). Esta página existe para que cada consulta se
pueda contestar **en un minuto**: qué se pregunta, qué se recomienda, qué ocurre si no se contesta y cuánto costaría
cambiar de opinión más adelante. Ninguna bloquea nada: todas tienen una opción por defecto ya aplicada
(`docs/PLAN.md` §2 y `docs/PLAN-RONDA-3.md` §2).

**Cómo contestar.** Basta una línea por consulta: «Q-n: sí / no / la alternativa B». La sesión de orquestación
anota la respuesta en `docs/PLAN.md` y, si cambia algo, abre la tarea.

## Resumen

Coste de cambiar después: **bajo** = una línea o un fichero; **medio** = unos ficheros y sus pruebas; **alto** =
toca varias pantallas o lo que ya consumen otros proyectos.

| # | Pregunta | Recomendación | Si no se contesta | Coste de cambiar después |
|---|---|---|---|---|
| Q-1 | `style` con variables de disposición (CSSOM) | Sí | Sigue aplicado (D-R12) | Alto |
| Q-2 | ¿Tema por defecto dentro del paquete? | No | El paquete pide `themeOrigin` | Bajo |
| Q-3 | Licencia del paquete | `GPL-2.0-only` | Sigue `GPL-2.0-only` | Bajo (técnico); alto si ya se publicó |
| Q-4 | Geometría de la demo | Esquemática rotulada | Sigue esquemática | Bajo |
| Q-5 | Revisión nativa del árabe de la demo | Encargarla antes de usarlo fuera de la demo | Árabe «de demostración, sin revisión» (D-R38) | Bajo (sólo textos) |
| Q-6 | `lightningcss` (MPL-2.0) y transitivas ISC/BlueOak/CC0 | Aceptarlas | Se usan y se listan en el informe | Bajo |
| Q-7 | Paleta de datos provisional | Esperar a PT-5 | Sigue la provisional | Bajo |
| Q-8 | Geometría real | **Decidida (2026-11-27):** Natural Earth + OSM/Protomaps + IGN/CNIG | Sigue la esquemática (Q-4) hasta que se construya | Medio |
| Q-9 | `polylabel` como dependencia | No | Cálculo propio | Bajo |
| Q-10 | Nivel del mapa con un solo elemento | Saltarlo | Se salta (`skipSingle: true`) | Bajo |
| Q-11 | Atribución de `@xyflow/react` | Oculta, con su aviso | Oculta | Bajo |
| Q-12 | Cantidades de dinero en monoespaciada | No | `tabular-nums` en la familia del cuerpo | Bajo |

---

## Q-1. `style` con variables de disposición (CSSOM)

- **Pregunta.** La cuadrícula virtualizada necesita colocar filas y columnas con medidas que cambian al
  desplazarse o al redimensionar. ¿Se acepta que el único `style` del código sea el de **variables de
  disposición** (`--kx-col-width`, `--kx-row-index`, transformaciones), escritas por CSSOM, y nunca color, fuente,
  radio, sombra ni espacio?
- **Estado.** Aplicado desde la ronda 1 (D-R12). Es una desviación consciente de la regla de Theros Front (A3 §7.6).
- **Recomendación.** Sí. Todo lo visual sigue saliendo de `--th-*`; lo único que se escribe son posiciones.
- **Si no se contesta.** Sigue como está.
- **Coste de cambiar después.** Alto: la virtualización de filas y la anchura de columnas dependen de ello; sin
  `style` habría que generar clases por columna o reescribir la rejilla.

## Q-2. Tema por defecto dentro del paquete

- **Pregunta.** ¿Se incluye en el paquete una copia generada de `modern-flat` como tema por defecto? La licencia
  de lo que genera Theros Front no está declarada.
- **Estado.** No incluido: la demo lo sirve desde `demo/public/themes/` y el paquete pide `themeOrigin`.
- **Recomendación.** No, hasta que Theros Front declare la licencia de lo generado.
- **Si no se contesta.** El paquete sigue sin tema propio.
- **Coste de cambiar después.** Bajo: copiar la carpeta a `dist/` y hacer opcional `themeOrigin`.

## Q-3. Licencia del paquete `@karpo/react`

- **Pregunta.** ¿Qué licencia lleva el paquete npm?
- **Estado.** `GPL-2.0-only`, la del repositorio (`LICENSE`); cambiarla es una línea de `package.json`. Todas las
  dependencias directas son MIT, Apache-2.0 o BSD, y los avisos de lo incrustado van en
  `dist/THIRD-PARTY-NOTICES.md`.
- **Recomendación.** Mantener `GPL-2.0-only` (coherente con el resto del repositorio).
- **Si no se contesta.** Sigue `GPL-2.0-only`.
- **Coste de cambiar después.** Bajo en lo técnico (una línea, `LICENSE` y regenerar `docs/LICENCIAS.md`). **Alto en
  lo legal una vez publicado**: lo ya publicado no se puede retirar; por eso conviene decidirlo antes de publicar.

## Q-4. Geometrías reales de la demo

- **Pregunta.** ¿De dónde sale la geometría de los mapas de la demo?
- **Estado.** Geometría **esquemática generada** (rotulada «Geometría esquemática de demostración») y origen
  configurable para un `.pmtiles` real (petición B-2).
- **Recomendación.** Mantenerla para la demo y resolver Q-8 antes de enseñar mapas a un cliente.
- **Si no se contesta.** Sigue la esquemática.
- **Coste de cambiar después.** Bajo: es un origen configurable; cambiar el fichero no toca código.

## Q-5. Revisión nativa del árabe de la demo

- **Pregunta.** La demo trae el árabe (`ar`) como idioma de derecha a izquierda real. ¿Se encarga una revisión
  nativa de la traducción?
- **Estado.** Cambia en la ronda 3 (D-R38): árabe **de demostración, sin revisión nativa**, rotulado así en el
  README. Sirve para probar el espejo, los aislamientos bidireccionales y las capturas `rtl`; no está pensado
  para producción. Las cifras salen siempre en dígitos latinos.
- **Recomendación.** Encargarla sólo cuando el árabe vaya a usarse fuera de la demo.
- **Si no se contesta.** Sigue sin revisión y con el rótulo de demostración.
- **Coste de cambiar después.** Bajo: son los ficheros de `src/locales/ar`; la estructura admite más idiomas.

## Q-6. `lightningcss` (MPL-2.0) y otras transitivas permisivas

- **Pregunta.** Vite usa `lightningcss` (MPL-2.0) para transformar y minificar CSS; otras transitivas son ISC,
  BlueOak, CC0 o CC-BY. ¿Se aceptan?
- **Estado.** Se usan **por defecto** y se listan en `docs/LICENCIAS.md`. `lightningcss` es sólo de
  compilación: no se distribuye ni se enlaza en ejecución. Ojo: sus `targets` cambian el CSS publicado (compila
  `:dir()` a `:lang()`, que no sirve con `?dir=rtl`; D-R22).
- **Recomendación.** Aceptarlas (copyleft débil por archivo y sólo de herramienta).
- **Si no se contesta.** Se siguen usando.
- **Coste de cambiar después.** Bajo: `cssMinify: 'esbuild'` en los dos ficheros de Vite; el CSS publicado
  puede variar en bytes.

## Q-7. Paleta de datos provisional

- **Pregunta.** ¿Se acepta la paleta de datos (mapas, leyendas) provisional hasta que Theros Front la emita
  (PT-5)?
- **Estado.** Provisional en `karpo.ext` (D-R13), con contraste comprobado en los cuatro temas, claro y oscuro.
- **Recomendación.** Sí; sustituirla cuando llegue PT-5.
- **Si no se contesta.** Sigue la provisional.
- **Coste de cambiar después.** Bajo: son variables `--kx-data-*`; al llegar PT-5 se borran de `karpo.ext`.

## Q-8. Geometría real del mapa

- **Pregunta.** ¿De qué fuentes sale la geometría real del mapa, que hoy es esquemática (Q-4)?
- **Decisión de Javier (2026-11-27).** Tres fuentes, servidas como PMTiles propio desde el despliegue:
  - **Natural Earth** para el mundo y los países: dominio público; la atribución es opcional.
  - **OpenStreetMap vía Protomaps** para regiones y ciudades: ODbL; atribución visible
    «© OpenStreetMap contributors». Las teselas tal cual son una *obra producida* y no obligan a publicar nada;
    si se filtran o corrigen los datos de OSM antes de generarlas, eso es una *base de datos derivada* que hay
    que publicar bajo ODbL si se usa públicamente. Código y estilo de Protomaps: BSD-3 y CC0.
  - **IGN/CNIG** para comunidades, provincias y municipios de España: CC BY 4.0; atribución obligatoria a pie
    de mapa, «BDLJE CC-BY 4.0 ign.es» (si se modifican: «Obra derivada de BDLJE <fecha> CC-BY 4.0 ign.es»).
- **Descartadas (licencias leídas en sus webs oficiales el 2026-11-27).** GADM y Eurostat GISCO (NUTS, LAU:
  límites © EuroGeographics) prohíben el uso comercial sin licencia aparte. geoBoundaries lleva una licencia
  por país (España ADM2: licencia de datos del INE), así que no se usa sin revisarla país a país.
- **Consecuencias.**
  - Los códigos de la demo son NUTS (`ES51`, `ES300`), que es justo la fuente descartada: pasan a ISO 3166-2
    (`ES-CT`) para regiones y a los del INE para provincias y municipios. El mapa deja de enlazar por
    `nutsCode` (hoy lo usan `src/map/` y `src/api/normalizers.ts`); el campo puede seguir en el modelo de Go.
  - El mapa muestra las atribuciones de OSM y del IGN (control de atribución de MapLibre).
  - Generar el PMTiles es trabajo del orquestador de Karpo Go (B-2 y B-26), con una herramienta externa.
- **Estado.** Decidida; hasta que se construya, sigue la esquemática de Q-4.
- **Coste de cambiar después.** Medio: regenerar el PMTiles, cambiar `manifest.map.sources` y los códigos, y
  revisar rótulos y niveles con datos reales.

## Q-9. `polylabel` como dependencia

- **Pregunta.** ¿Se añade `polylabel` (ISC) para colocar los rótulos del mapa?
- **Estado.** No: cálculo propio del polo de inaccesibilidad en `src/map/polylabel.ts` (ISC no está entre las
  licencias admitidas como directas).
- **Recomendación.** Mantener el cálculo propio.
- **Si no se contesta.** Sigue el cálculo propio.
- **Coste de cambiar después.** Bajo: sustituir una función y añadir la dependencia con su visto bueno.

## Q-10. Nivel del mapa con un solo elemento

- **Pregunta.** Si un nivel tiene una sola delimitación (por ejemplo, una región con una sola provincia), ¿se salta
  o se conserva con el zoom limitado?
- **Estado.** Se salta (`map.skipSingle: true`, configurable por entidad). La ronda 3 lo hace coherente también al
  entrar por URL (D-R39).
- **Recomendación.** Saltarlo.
- **Si no se contesta.** Se salta.
- **Coste de cambiar después.** Bajo: es una opción de configuración.

## Q-11. Atribución de `@xyflow/react` en el diagrama (nueva, ronda 3)

- **Pregunta.** La licencia MIT de `@xyflow/react` permite ocultar su pequeña atribución en el diagrama; el
  fabricante la pide por cortesía y apoya el proyecto con una suscripción. Opciones: **(a)** oculta, con su aviso
  de copyright en `dist/THIRD-PARTY-NOTICES.md`; **(b)** visible y configurable (`manifest.diagram.attribution`);
  **(c)** suscribirse.
- **Estado.** (a): `proOptions.hideAttribution` en `src/tree/TreeDiagram.tsx`. No es un problema de licencia: el
  aviso de la hoja base que sí se copia en `dist/karpo.css` consta en el fichero de avisos.
- **Recomendación.** (a) mientras sea una demo; (c) si el producto se vende.
- **Si no se contesta.** (a).
- **Coste de cambiar después.** Bajo: una línea para mostrarla; la opción (b) añade una clave de configuración y
  una nota en `COMPATIBILIDAD-CONFIG.md`.

## Q-12. Cantidades de dinero en monoespaciada (nueva, ronda 3)

- **Pregunta.** ¿Las cantidades de dinero (`money`) van en fuente monoespaciada, como pedía el contrato de
  Theros Front con `data-th-format="amount"`?
- **Estado.** No: todas las cifras (números, decimales y dinero) usan `font-variant-numeric: tabular-nums` en la
  **familia del cuerpo** (D-R35), alineadas a la derecha y sin saltos de línea. `amount` deja de usarse en los
  visores numéricos. Se anota en `PETICIONES-THEROS-FRONT.md` para que el pack decida si lo mantiene.
- **Recomendación.** No: la monoespaciada rompe el ritmo tipográfico de la tabla y `tabular-nums` ya alinea las
  cifras.
- **Si no se contesta.** `tabular-nums` en la familia del cuerpo.
- **Coste de cambiar después.** Bajo: una regla en `fields.css` y `grid.css` y la prueba que la fija.
