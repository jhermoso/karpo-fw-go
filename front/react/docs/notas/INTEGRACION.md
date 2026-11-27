# Integración de la ronda 1

Fecha: 2026-11-27. Rama `claude/front-react-7b8564`. Integrador sobre las olas T0 a T8.

## Resultado

`npm ci`, `typecheck`, `lint`, `test` (77 ficheros, 1130 pruebas), `test:contract` (98), `licencias --check`
(446 paquetes, 50 directas), `build` (biblioteca y demo) y `pack:check` pasan con `npm run check`.
`npm run test:e2e` pasa (58 pruebas, 3 saltadas sin `KARPO_LOCAL=1`) y, con `KARPO_LOCAL=1`, las 3 de
`e2e/local.e2e.ts` pasan contra `cmd/karpo` real (SQLite temporal, puerto libre): login, lista de 30
instalaciones y guardado con `version`. `npm run karpo:local -- --smoke` da las 5 comprobaciones en OK.
Sin dependencias nuevas.

## Defectos corregidos

| Defecto | Arreglo |
|---|---|
| `build:lib` fallaba en `tsc` (arneses de pruebas dentro de `src`) | `tsconfig.lib.json` excluye `test-utils.tsx`, `test-doubles.tsx` y `__fixtures__`; el paquete ya no lleva sus `.d.ts` |
| Orden de capas CSS: `karpo.components` salía con la menor prioridad en la demo | `src/index.ts` importa `./theme/layers.css` el primero |
| **`ui.css` y `fields.css` no entraban en ningún empaquetado** (ni `dist/karpo.css` de la biblioteca antes de este cambio, ni la demo): su `index.ts` no tiene efectos laterales y el empaquetador lo descartaba. Era la causa de los iconos, del texto «solo para lectores» a la vista, de la cabecera «Acciones» desbordada y del selector de la clave ajena con el chevron suelto | `src/index.ts` importa las dos hojas y `package.json` declara `sideEffects` para `src/ui/index.ts` y `src/fields/index.ts`; `pack:check` comprueba ahora que `dist/karpo.css` lleva los selectores de cada hoja |
| `vite build --mode demo` no emitía `demo/config` | `karpoConfigPlugin` admite `extraEmit` y `vite.config.ts` lo usa; se quita el `cpSync` de `e2e/lib/demo.mjs` |
| `npm run dev:local` roto | Quitado el script y la rama `mode === 'local'` de `vite.config.ts` (`karpo:local` ya arranca Vite con `--mode go` y su propia configuración) |
| `engines.node` | `>=24.15` |
| Iconos casi invisibles en botones primarios o seleccionados | Los iconos de acciones, elementos de menú e insignias heredan el color del texto (`shell.css`) |
| Insignia de descendientes del organigrama con contador `0+` | Sólo se pinta si hay descendientes conocidos (`TreeList`, `TreeDiagram`) |
| Columna fija «Acciones» con un hueco de 12 px a su lado en listas estrechas | El área de desplazamiento de la rejilla no lleva relleno horizontal |
| Página con scroll vertical en el mapa (margen de 8 px del `body`) | `demo/index.html` pone `body { margin: 0 }` (el README lo dice) |
| Pasos de ayuda de la demo hacia anclas inexistentes | `core.view-switch`, `grid.batch-bar` y `tree.node`; una prueba comprueba que todos los `target` del manifiesto existen en los fuentes |
| `schematic` y `defaultCode` se perdían al leer el manifiesto | Se conservan en `MapSourceDef` y `MapLevelDef` y están en la tabla de compatibilidad |
| Worker de MapLibre sólo resoluble con Vite | `setMapWorkerUrl()` para otros empaquetadores (README) |
| Favicon 404 | `<link rel="icon" href="data:,">` |

## Pendiente (no bloquea)

- Decisión de Javier: mantener oculta o no la atribución de `@xyflow/react`; consultas Q-1 a Q-7 del README.
- Diagrama del organigrama: el texto de las tarjetas queda pequeño cuando hay muchas hojas en una fila (el zoom de
  ajuste lo decide el ancho).
- Detalle: el error local de un campo obligatorio se quita al salir del campo, no al escribir
  (`reValidateMode: 'onChange'`); no hay botón visible de «Nuevo» en las listas (T8 §4.6).
- Iconos con trazo: sigue el parche de `shell.css` hasta que Theros Front admita un conjunto relleno (PT-18).


---

# Integración de la ronda 2 (R5)

Fecha: 2026-11-27. Rama `claude/front-react-7b8564`, base `1490a0d`. Tarea R5 de la ola 2 (integración, e2e en inglés, reglas de lint,
presupuesto, capturas y cierre), sola en la carpeta. Sin commit; sin dependencias nuevas; no se tocó `package.json` ni el lock.

## Resultado

| Comprobación | Resultado |
|---|---|
| `npm run check` (typecheck, lint, test, test:contract, licencias, `icons:gen --check`, `i18n:demo --check`, build, presupuesto, pack:check) | **Código 0.** 96 ficheros y 1413 pruebas unitarias y de componentes; 7 ficheros y 154 de contrato; `licencias --check` sin cambios en `docs/LICENCIAS.md` (443 paquetes, 50 directas) |
| Presupuesto (D-R29) | **242,6 kB gzip** del trozo inicial de la demo con `enforce: true` y límite de 250 kB (margen 7,4 kB); `dist/karpo-react.js` 90,9 kB |
| `npm run test:e2e` con Edge | **138 pruebas en verde y 3 saltadas** (13 ficheros; las 3 saltadas son `local.e2e.ts` sin `KARPO_LOCAL=1`): `ingles`, `menu`, `rtl`, `rejilla`, `movil`, `mapa`, `organigrama`, `detalle`, `i18n`, `ayuda`, `temas`, `utilidades` y `lib`. 0 peticiones fuera del origen (cada prueba lo comprueba al terminar) |
| `KARPO_LOCAL=1 … e2e/local.e2e.ts` contra `cmd/karpo` real (SQLite temporal, puertos libres) | 3 de 3 en verde (login real, lista de 30 instalaciones, guardado con `version`, árbol de puestos con el control de vista de la cabecera) |
| `npm run capturas` | 41 PNG (27 de siempre, 9 `ingles`, 5 `movil`) y su README; ninguna petición fuera del origen |
| `grep -rn ":dir(" src` y `grep -rn "\.literal" src` (fuera de `src/config` y `src/fields/values.ts`) | Sin resultados |
| Procesos | Ninguno vivo al terminar (vite preview, `cmd/karpo`, vite local y navegadores los paran las propias suites y scripts); nada en el puerto 55432 |

Se ejecutó con Node v24.14.1: todas las órdenes de npm imprimen el aviso `EBADDEVENGINES` esperado (`devEngines` pide 24.15 con
`onFail: "warn"`); es sólo un aviso.

## Cada bloqueante tiene una prueba que fallaba en `1490a0d`

Comprobado: se extrajo `1490a0d` con `git archive` a una carpeta temporal fuera del repositorio (`front/react` y
`front/angular/config`, con `node_modules` enlazado), se copiaron encima las e2e actuales y se ejecutaron `ingles`, `menu`, `mapa`,
`rejilla`, `movil` y `rtl` contra **su** demo compilada. Resultado: **41 pruebas en rojo y 34 en verde** (las que ya valían en
la ronda 1). La carpeta temporal quedó fuera del repositorio y no se conserva.

| Punto | Prueba | Contra `1490a0d` |
|---|---|---|
| B-1 iconos del menú | `e2e/menu.e2e.ts`, «los cinco iconos de las entradas son dibujos distintos» | Falla: `expected 1 to be 5` (los cinco son el mismo círculo de reserva) |
| B-2 literales españoles en inglés | `e2e/ingles.e2e.ts` (todas las pantallas, las 8 entidades, leyenda, insignias, cabeceras de `/product`, detalle) | Fallan las 15 pruebas de la suite que dependen de la configuración |
| B-3 tipografía del mapa | `e2e/mapa.e2e.ts`, `font-family` de `.kx-map-label` igual a la de `body` (y comprobación directa con un guion aparte) | En `1490a0d` la tipografía del mapa y de sus rótulos es `"Helvetica Neue", Arial, Helvetica, sans-serif` y la de `body`, `system-ui, sans-serif`; en la rama actual, las dos `system-ui, sans-serif`. La prueba de `mapa.e2e.ts` falla antes, en los rótulos (17 de 18), porque el resto del mapa también cambió |
| B-4 `Tab` en la cuadrícula | `e2e/rejilla.e2e.ts`, «Tab saca el foco de la lista en tres pulsaciones como mucho…» y «tras moverse con flechas, salir con Tab…» | Fallan |
| B-5 móvil | `e2e/movil.e2e.ts` (360 y 390 px, 5 rutas) | Fallan las 11 pruebas |
| `:dir()` (hallazgo) | `e2e/rtl.e2e.ts`, «los iconos direccionales se espejan con dir=rtl…» y «con dir=rtl el idioma sigue siendo el inglés: el espejo no depende de :lang()» | Fallan: `expected 'none' not to be 'none'` (los chevrons del menú no se espejan) |
| M-12 bidi | `e2e/rtl.e2e.ts`, aislantes FSI/PDI | Falla: `expected '4 changes' to be '⁨4⁩ changes'` |

Además: `stylelint` falla ante `:dir(` y `eslint` ante `.literal` fuera de `src/config` y `src/fields/values.ts`
(`build/lint-config.test.ts`, 41 pruebas), y `build/css-dir.test.ts` compila `src/**/*.css` con la transformación de Vite y falla si la
salida contiene `:lang(ar`; su control compila un `:dir(rtl)` de prueba y comprueba que sí sale como `:lang(ar`.

## Ficheros nuevos de esta tarea

`e2e/ingles.e2e.ts`, `build/css-dir.test.ts`, `scripts/presupuesto.json`, `scripts/capturas.test.mjs` (la prueba, sin navegador, de las 41
capturas) y `e2e/lib/pantallas.ts` (las pantallas `SCREENS` y la lectura del texto de la página, compartidas por `i18n.e2e.ts` e
`ingles.e2e.ts`, porque importar una suite desde otra la registraría dos veces).

## Arreglos de integración en ficheros de otras tareas (uno a uno)

1. **`src/config/manifest.ts` (R2): el normalizador y `compat` se cargan con `import()`.** Con el presupuesto obligatorio, la compilación
   de producción daba **266,1 kB** gzip (límite 250). El análisis por módulo (mapa de fuentes) puso en el trozo de entrada: React DOM
   (~63 kB gzip), `react-router` (~32), los dibujos de iconos (~28), `src/config` (~22: `normalize.ts` y la tabla `compat.ts`), `i18next`
   (~16), `query-core` (~9) y `zod` (~6). Los cargadores del manifiesto (`loadManifestWithReport`, `loadLegacyWithReport`,
   `loadDiscoveredWithReport`) piden ahora `./normalize` y `./compat` con `import()` en paralelo con la descarga del manifiesto
   (`ensureNormalizer()`); `isObj` es local. `zod` y el esquema de entidad se van con ellos. Resultado: **242,6 kB**. Sin cambios de
   comportamiento (las 108 pruebas de `src/config` y la suite entera siguen en verde). No se subió el límite.
2. **`e2e/lib/demo.mjs` (R0): la demo se compila en producción.** Bajo vitest `NODE_ENV` vale `test` y `vite build --mode demo`
   compilaba **React en desarrollo** (la entrada pasaba de 260 a 327 kB gzip y `import.meta.env.DEV` salía a verdadero). Ahora el
   proceso de compilación lleva `NODE_ENV=production`, como `npm run build:demo`. Así las e2e prueban lo mismo que se publica, y el
   presupuesto sale igual tras una e2e que tras `npm run build`.
3. **`e2e/lib/suite.ts` (R0): `go()` espera la pantalla de la entidad de destino.** `data-kx-ready="true"` sigue en el DOM de la lista
   anterior hasta que React confirma la ruta nueva (la carrera que anotó R4): el paso del conflicto 409 de `i18n.e2e.ts` abría un
   producto en vez de una instalación y fallaba en las dos lenguas. `go()` espera (con tope de 5 s y sin fallar) a
   `[data-kx-screen="entity"][data-kx-entity="<primer segmento de la ruta>"]` antes de esperar el selector de «listo».
4. **`e2e/utilidades.e2e.ts` (R4):** la prueba de las capturas pasa de 27 a 41 (variantes `ingles` y `movil`, tamaño de cada una, ni una
   imagen de más en la carpeta, README con 41).
5. **`src/map/map.css` (R3): en el móvil el aviso de geometría esquemática ya no tapa la leyenda.** Con el marco compacto la leyenda y el
   aviso «Geometría esquemática de demostración · Fuente…» caían a la vez en la esquina inferior y se pisaban (se vio en
   `mapa-mundo-movil.png`). Con `[data-kx-shell][data-kx-compact]` el aviso sube y ocupa todo el ancho (`inset-block: … auto`).
6. **Regla `.literal` (eslint):** tres pruebas de otras tareas leían `.literal` de un `Text` y dejaron de compilar bajo la regla nueva:
   `src/contracts/views.contract.test.ts` (R0), `src/fields/text-resolution.test.tsx` (R1) y `src/tree/diagram.test.tsx` (R3), más un
   comentario de `src/contracts/fields.ts`. Se reescribieron sin leer `.literal` (con `key` o `byLang`) sin debilitar ninguna comprobación.

## Lo visto al revisar las 41 capturas

Contra los puntos del jurado: iconos de menú, barras y cabeceras dibujados (nada de círculos de reserva); inglés completo en la variante
`ingles` (menú, cabeceras, recuento «30 facilities», insignias, errores y pies); tipografía del mapa igual a la del resto; barra móvil
con título, buscador, ayuda y «Más»; chevrons del árbol y del menú espejados en RTL; barra de cambios compacta y flotante con
«N cambios · Descartar · Guardar»; organigrama a tamaño legible con una rama abierta, los contadores `≥N` y el minimapa fuera de la
rama; leyenda del mapa que no tapa Canarias ni ninguna delimitación (mundo, país, región y ciudad), también en RTL y en oscuro. Se
cambió la escena del diagrama del organigrama: con dos ramas desplegadas el encuadre legible se salía de la pantalla y el ajuste a todo
dejaba una letra diminuta; ahora despliega la rama de Operaciones y pulsa «Ajustar» dos veces (todo y vuelta a legible).

Defectos que se vieron en esa revisión y se **resolvieron en el cierre** (véase la sección siguiente): títulos de puesto truncados y
estado de la búsqueda cortado en el listado del organigrama a 390 px; «Subir un nivel» pegado al borde derecho del mapa; fila asomando bajo
la cabecera pegajosa de la rejilla; orden inverso de las teclas de un atajo en RTL.

Defecto que **sigue** y no se ha tocado (no bloquea):

- Mapa del mundo: algunos rótulos son nombre corto («China», «India») y otros código ISO («CA», «US») según la caja disponible; es el
  efecto del medido real de la letra con la geometría esquemática (M-5 sigue pendiente de la consulta Q-8).

## Pendiente (no bloquea)

- Decisión de Javier: mantener oculta o no la atribución de `@xyflow/react`; consultas Q-1 a Q-7 del README.
- Diagrama del organigrama: el texto de las tarjetas queda pequeño cuando hay muchas hojas en una fila (el zoom de
  ajuste lo decide el ancho).
- Detalle: el error local de un campo obligatorio se quita al salir del campo, no al escribir
  (`reValidateMode: 'onChange'`); no hay botón visible de «Nuevo» en las listas (T8 §4.6).
- Iconos con trazo: sigue el parche de `shell.css` hasta que Theros Front admita un conjunto relleno (PT-18).


---

# Integración de la ronda 2 (R5)

Fecha: 2026-11-27. Rama `claude/front-react-7b8564`, base `1490a0d`. Tarea R5 de la ola 2 (integración, e2e en inglés, reglas de lint,
presupuesto, capturas y cierre), sola en la carpeta. Sin commit; sin dependencias nuevas; no se tocó `package.json` ni el lock.

## Resultado

| Comprobación | Resultado |
|---|---|
| `npm run check` (typecheck, lint, test, test:contract, licencias, `icons:gen --check`, `i18n:demo --check`, build, presupuesto, pack:check) | **Código 0.** 96 ficheros y 1413 pruebas unitarias y de componentes; 7 ficheros y 154 de contrato; `licencias --check` sin cambios en `docs/LICENCIAS.md` (443 paquetes, 50 directas) |
| Presupuesto (D-R29) | **242,6 kB gzip** del trozo inicial de la demo con `enforce: true` y límite de 250 kB (margen 7,4 kB); `dist/karpo-react.js` 90,9 kB |
| `npm run test:e2e` con Edge | **138 pruebas en verde y 3 saltadas** (13 ficheros; las 3 saltadas son `local.e2e.ts` sin `KARPO_LOCAL=1`): `ingles`, `menu`, `rtl`, `rejilla`, `movil`, `mapa`, `organigrama`, `detalle`, `i18n`, `ayuda`, `temas`, `utilidades` y `lib`. 0 peticiones fuera del origen (cada prueba lo comprueba al terminar) |
| `KARPO_LOCAL=1 … e2e/local.e2e.ts` contra `cmd/karpo` real (SQLite temporal, puertos libres) | 3 de 3 en verde (login real, lista de 30 instalaciones, guardado con `version`, árbol de puestos con el control de vista de la cabecera) |
| `npm run capturas` | 41 PNG (27 de siempre, 9 `ingles`, 5 `movil`) y su README; ninguna petición fuera del origen |
| `grep -rn ":dir(" src` y `grep -rn "\.literal" src` (fuera de `src/config` y `src/fields/values.ts`) | Sin resultados |
| Procesos | Ninguno vivo al terminar (vite preview, `cmd/karpo`, vite local y navegadores los paran las propias suites y scripts); nada en el puerto 55432 |

Se ejecutó con Node v24.14.1: todas las órdenes de npm imprimen el aviso `EBADDEVENGINES` esperado (`devEngines` pide 24.15 con
`onFail: "warn"`); es sólo un aviso.

## Cada bloqueante tiene una prueba que fallaba en `1490a0d`

Comprobado: se extrajo `1490a0d` con `git archive` a una carpeta temporal fuera del repositorio (`front/react` y
`front/angular/config`, con `node_modules` enlazado), se copiaron encima las e2e actuales y se ejecutaron `ingles`, `menu`, `mapa`,
`rejilla`, `movil` y `rtl` contra **su** demo compilada. Resultado: **41 pruebas en rojo y 34 en verde** (las que ya valían en
la ronda 1). La carpeta temporal quedó fuera del repositorio y no se conserva.

| Punto | Prueba | Contra `1490a0d` |
|---|---|---|
| B-1 iconos del menú | `e2e/menu.e2e.ts`, «los cinco iconos de las entradas son dibujos distintos» | Falla: `expected 1 to be 5` (los cinco son el mismo círculo de reserva) |
| B-2 literales españoles en inglés | `e2e/ingles.e2e.ts` (todas las pantallas, las 8 entidades, leyenda, insignias, cabeceras de `/product`, detalle) | Fallan las 15 pruebas de la suite que dependen de la configuración |
| B-3 tipografía del mapa | `e2e/mapa.e2e.ts`, `font-family` de `.kx-map-label` igual a la de `body` (y comprobación directa con un guion aparte) | En `1490a0d` la tipografía del mapa y de sus rótulos es `"Helvetica Neue", Arial, Helvetica, sans-serif` y la de `body`, `system-ui, sans-serif`; en la rama actual, las dos `system-ui, sans-serif`. La prueba de `mapa.e2e.ts` falla antes, en los rótulos (17 de 18), porque el resto del mapa también cambió |
| B-4 `Tab` en la cuadrícula | `e2e/rejilla.e2e.ts`, «Tab saca el foco de la lista en tres pulsaciones como mucho…» y «tras moverse con flechas, salir con Tab…» | Fallan |
| B-5 móvil | `e2e/movil.e2e.ts` (360 y 390 px, 5 rutas) | Fallan las 11 pruebas |
| `:dir()` (hallazgo) | `e2e/rtl.e2e.ts`, «los iconos direccionales se espejan con dir=rtl…» y «con dir=rtl el idioma sigue siendo el inglés: el espejo no depende de :lang()» | Fallan: `expected 'none' not to be 'none'` (los chevrons del menú no se espejan) |
| M-12 bidi | `e2e/rtl.e2e.ts`, aislantes FSI/PDI | Falla: `expected '4 changes' to be '⁨4⁩ changes'` |

Además: `stylelint` falla ante `:dir(` y `eslint` ante `.literal` fuera de `src/config` y `src/fields/values.ts`
(`build/lint-config.test.ts`, 41 pruebas), y `build/css-dir.test.ts` compila `src/**/*.css` con la transformación de Vite y falla si la
salida contiene `:lang(ar`; su control compila un `:dir(rtl)` de prueba y comprueba que sí sale como `:lang(ar`.

## Ficheros nuevos de esta tarea

`e2e/ingles.e2e.ts`, `build/css-dir.test.ts`, `scripts/presupuesto.json`, `scripts/capturas.test.mjs` (la prueba, sin navegador, de las 41
capturas) y `e2e/lib/pantallas.ts` (las pantallas `SCREENS` y la lectura del texto de la página, compartidas por `i18n.e2e.ts` e
`ingles.e2e.ts`, porque importar una suite desde otra la registraría dos veces).

## Arreglos de integración en ficheros de otras tareas (uno a uno)

1. **`src/config/manifest.ts` (R2): el normalizador y `compat` se cargan con `import()`.** Con el presupuesto obligatorio, la compilación
   de producción daba **266,1 kB** gzip (límite 250). El análisis por módulo (mapa de fuentes) puso en el trozo de entrada: React DOM
   (~63 kB gzip), `react-router` (~32), los dibujos de iconos (~28), `src/config` (~22: `normalize.ts` y la tabla `compat.ts`), `i18next`
   (~16), `query-core` (~9) y `zod` (~6). Los cargadores del manifiesto (`loadManifestWithReport`, `loadLegacyWithReport`,
   `loadDiscoveredWithReport`) piden ahora `./normalize` y `./compat` con `import()` en paralelo con la descarga del manifiesto
   (`ensureNormalizer()`); `isObj` es local. `zod` y el esquema de entidad se van con ellos. Resultado: **242,6 kB**. Sin cambios de
   comportamiento (las 108 pruebas de `src/config` y la suite entera siguen en verde). No se subió el límite.
2. **`e2e/lib/demo.mjs` (R0): la demo se compila en producción.** Bajo vitest `NODE_ENV` vale `test` y `vite build --mode demo`
   compilaba **React en desarrollo** (la entrada pasaba de 260 a 327 kB gzip y `import.meta.env.DEV` salía a verdadero). Ahora el
   proceso de compilación lleva `NODE_ENV=production`, como `npm run build:demo`. Así las e2e prueban lo mismo que se publica, y el
   presupuesto sale igual tras una e2e que tras `npm run build`.
3. **`e2e/lib/suite.ts` (R0): `go()` espera la pantalla de la entidad de destino.** `data-kx-ready="true"` sigue en el DOM de la lista
   anterior hasta que React confirma la ruta nueva (la carrera que anotó R4): el paso del conflicto 409 de `i18n.e2e.ts` abría un
   producto en vez de una instalación y fallaba en las dos lenguas. `go()` espera (con tope de 5 s y sin fallar) a
   `[data-kx-screen="entity"][data-kx-entity="<primer segmento de la ruta>"]` antes de esperar el selector de «listo».
4. **`e2e/utilidades.e2e.ts` (R4):** la prueba de las capturas pasa de 27 a 41 (variantes `ingles` y `movil`, tamaño de cada una, ni una
   imagen de más en la carpeta, README con 41).
5. **`src/map/map.css` (R3): en el móvil el aviso de geometría esquemática ya no tapa la leyenda.** Con el marco compacto la leyenda y el
   aviso «Geometría esquemática de demostración · Fuente…» caían a la vez en la esquina inferior y se pisaban (se vio en
   `mapa-mundo-movil.png`). Con `[data-kx-shell][data-kx-compact]` el aviso sube y ocupa todo el ancho (`inset-block: … auto`).
6. **Regla `.literal` (eslint):** tres pruebas de otras tareas leían `.literal` de un `Text` y dejaron de compilar bajo la regla nueva:
   `src/contracts/views.contract.test.ts` (R0), `src/fields/text-resolution.test.tsx` (R1) y `src/tree/diagram.test.tsx` (R3), más un
   comentario de `src/contracts/fields.ts`. Se reescribieron sin leer `.literal` (con `key` o `byLang`) sin debilitar ninguna comprobación.

## Lo visto al revisar las 41 capturas

Contra los puntos del jurado: iconos de menú, barras y cabeceras dibujados (nada de círculos de reserva); inglés completo en la variante
`ingles` (menú, cabeceras, recuento «30 facilities», insignias, errores y pies); tipografía del mapa igual a la del resto; barra móvil
con título, buscador, ayuda y «Más»; chevrons del árbol y del menú espejados en RTL; barra de cambios compacta y flotante con
«N cambios · Descartar · Guardar»; organigrama a tamaño legible con una rama abierta, los contadores `≥N` y el minimapa fuera de la
rama; leyenda del mapa que no tapa Canarias ni ninguna delimitación (mundo, país, región y ciudad), también en RTL y en oscuro. Se
cambió la escena del diagrama del organigrama: con dos ramas desplegadas el encuadre legible se salía de la pantalla y el ajuste a todo
dejaba una letra diminuta; ahora despliega la rama de Operaciones y pulsa «Ajustar» dos veces (todo y vuelta a legible).

Defectos que **siguen** y que no se han tocado (no bloquean; para la ronda 3):

- Organigrama en listado a 390 px: los títulos de puesto se truncan a cinco o seis letras («Director/a Ge…») mientras el titular conserva
  más sitio; y el estado de la búsqueda («Coincid…») se corta a la derecha.
- Mapa del mundo a 1440 px: el botón «Subir un nivel» (desactivado) llega justo al borde derecho de la ventana, sin el margen que sí tiene
  en los otros niveles.
- Lista con el detalle abierto: bajo la cabecera pegajosa de la rejilla asoma un trozo de la fila anterior al desplazar.
- RTL: dentro de la barra de cambios, las teclas del atajo salen en orden inverso («S Ctrl»); un atajo debería conservar su orden LTR
  (`<kbd>` con `dir="ltr"`).
- Mapa del mundo: algunos rótulos son nombre corto («China», «India») y otros código ISO («CA», «US») según la caja disponible; es el
  efecto del medido real de la letra con la geometría esquemática (M-5 sigue pendiente de la consulta Q-8).

## Pendiente (no bloquea)

- **Consultas a Javier** Q-6 (reescrita), Q-8 (geometría real), Q-9 (`polylabel`) y Q-10 (nivel de un elemento) del README, con su opción por
  defecto aplicada. Siguen abiertas Q-1 a Q-5 y Q-7 y la de la atribución de `@xyflow/react`.
- **Presupuesto sin holgura grande (7,4 kB).** Si hace falta bajarlo: servir los dibujos de iconos en dos trozos (los usados por el marco en
  la entrada y el resto con `import()`; el peso `fill` sólo hace falta con temas de iconos rellenos, ~8 kB gzip) y el reparto de
  `react-router`. Subir el límite sería una consulta a Javier.
- **Detalle con el panel al 55 %:** la columna fijada de acciones puede tapar la última columna de datos hasta que se desplaza (la sombra
  de borde lo señala; R4).
- Las suites e2e no se pueden ejecutar a la vez en la misma carpeta (bloqueo de `vite preview`).

## Dónde fue a parar cada nota de la ronda 2

| Nota | Lo que pedía a otros | Dónde quedó |
|---|---|---|
| `R2-R1.md` | PT-5 (`missing`, hatch y ΔL), huecos de contraste de los temas, `look.icons` con nombres de Phosphor, `:dir()` frente a lightningcss | `PETICIONES-THEROS-FRONT.md`: PT-5 ampliada, PT-25, PT-26, PT-27 y PT-28 |
| `R2-R2.md` | TC-3 con la convención de claves; catálogos traducibles; total sin filtrar | `PETICIONES-THEROS-FRONT.md` (TC-3) y `PETICIONES-BACKEND.md` (B-24, B-25) |
| `R2-R3.md` | Servicio de teselas propio; hoja de MapLibre en la biblioteca | `PETICIONES-BACKEND.md` (B-26) y «Pendiente» de arriba |
| `R2-R4.md` | Total sin filtrar; carrera de `go()`; capturas con el control de vista | `PETICIONES-BACKEND.md` (B-25), arreglo 3 de arriba y `scripts/capturas.mjs` |

---

# Cierre de la ronda 2 (integrador)

Fecha: 2026-11-27. Rama `claude/front-react-7b8564`, base `1490a0d`. Se repitió todo desde cero (`npm ci` incluido) y se resolvió lo que
las tareas dejaron pendiente. Sin dependencias nuevas; `package.json` y el lock no cambian en este cierre.

## Resultado

| Comprobación | Resultado |
|---|---|
| `npm ci` | 417 paquetes, 0 vulnerabilidades |
| `npm run check` (typecheck, lint, test, test:contract, licencias, `icons:gen`, `i18n:demo`, build, presupuesto, pack:check) | **Código 0**: 96 ficheros y 1417 pruebas unitarias y de componentes; 7 ficheros y 154 de contrato |
| Presupuesto | **242,7 kB** gzip del trozo inicial (límite obligatorio de 250 kB) |
| `npm run licencias` | Sin cambios en `docs/LICENCIAS.md` (443 paquetes, 50 directas); ninguna licencia copyleft fuerte ni comercial |
| `npm run test:e2e` con Edge | 138 en verde y 3 saltadas (las de `local.e2e.ts` sin `KARPO_LOCAL=1`) |
| Humo real: `npm run karpo:local -- --smoke` y `KARPO_LOCAL=1 … e2e/local.e2e.ts` | Las 5 comprobaciones en OK (login, 30 instalaciones por Go y por el proxy de Vite, configuración y manifiesto) y 3 de 3 e2e contra `cmd/karpo` real (SQLite temporal, puertos libres): login, lista y guardado con `version`. Procesos parados; nada en el 55432 |
| `npm run capturas` | 41 PNG y su README, revisados uno a uno contra los puntos del jurado; ninguna petición fuera del origen |

## Pendientes de las tareas que se cerraron

1. **Caché al cambiar de idioma (R2).** `KarpoApp` monta `RefetchOnLanguageChange`: al cambiar el idioma activo marca como caducadas las
   consultas `['entity']`, y las que se ven se piden de nuevo con el `Accept-Language` nuevo (las filas ya no conservan el `typeName` del
   idioma anterior). Prueba en `src/shell/shell.test.tsx`.
2. **Hoja de MapLibre en la biblioteca (R0/R3).** `vite.lib.config.ts` sustituye el `import('./maplibre.css')` por un módulo vacío
   (`withoutMapLibreCss`) y `src/map/engine.ts` ya no mira `import.meta.env.MODE`. `dist/karpo.css` no lleva `.maplibregl-`; `pack:check`
   lo comprueba ahora (`hasMapLibreRules`, con su prueba) y la prueba de `src/map/style.test.ts` vigila la sustitución.
3. **Pruebas intermitentes.** Bajo carga (varios ficheros a la vez) dos pruebas de vistas cargadas en diferido pasaban o fallaban según el
   momento: `diagram.test.tsx` esperaba 1 s a `data-kx-ready` y `shell.test.tsx` 1 s a la cuadrícula. `src/test/setup.ts` fija
   `asyncUtilTimeout` en 5 s para todas y `diagram.test.tsx` espera hasta 10 s en sus ayudas.

## Defectos visuales corregidos (vistos en las capturas)

- **Fila asomando bajo la cabecera pegajosa de la rejilla** (`src/grid/grid.css`): el relleno superior del área de desplazamiento dejaba una
  franja de 12 px por encima de la cabecera pegajosa, por la que se veían las filas al desplazar. Ahora `padding-block-start: 0`; además la
  fila de cabeceras mide como una fila de datos (antes 21 px frente a 38 px).
- **Orden de las teclas de un atajo en RTL** (`src/ui/ui.css`): `shortcut-keys` lleva `direction: ltr` y `unicode-bidi: isolate`, así que
  «Ctrl K» y «Ctrl S» se leen igual en RTL (prueba en `src/ui/css.test.ts`).
- **Organigrama en listado a 390 px** (`src/tree/tree.css`): la búsqueda parte línea (campo y botones, luego «Coincidencia 1 de 1») en vez
  de cortarse, y el subtítulo cede antes que el título (`flex-shrink`), de modo que el puesto se lee entero más a menudo.
- **«Subir un nivel» pegado al borde** (`src/map/map.css`, `src/tree/tree.css`): las barras de las dos vistas llevan `padding-inline` con
  el espaciado del tema.

## Pendiente (no bloquea)

- Rótulos del mapa que mezclan nombre corto y código ISO (arriba), a la espera de la geometría real (Q-8).
- Presupuesto con 7,3 kB de holgura; si hace falta, partir `icon-data.ts` (véase la sección de R5).
- Consultas a Javier abiertas: Q-1 a Q-10 del README y la atribución de `@xyflow/react`.


---

# Integración de la ronda 3 (R3-6)

Fecha: 2026-11-27. Rama `claude/front-react-7b8564`. Tarea R3-6 de la ola 2 (árabe, peticiones consolidadas, capturas e integración), sola en la
carpeta. Sin commit; sin dependencias nuevas; no se tocó `package.json` ni el lock; nada en el puerto 55432; nada descargado fuera de npm.

## Resultado

| Comprobación | Resultado |
|---|---|
| `npm run check` | **Código 0.** 104 ficheros y 1648 pruebas unitarias y de componentes; 8 ficheros y 176 de contrato; `licencias --check` (443 paquetes, 50 directas); `icons:gen --check` y `i18n:demo --check` al día; build de la biblioteca y de la demo |
| Presupuesto (D-R29, límite de la ronda 3) | **213,1 kB gzip** del trozo inicial de la demo (límite obligatorio 215 kB; margen 1,9 kB) con `ar` disponible: el árabe es un trozo aparte (`src/locales/ar/*.json` por demanda) y no entra en el inicial; `dist/karpo-react.js` 96,4 kB |
| `pack:check` | 326 ficheros dentro de `dist/**`, `package.json`, `README.md` y `LICENSE`; `THIRD-PARTY-NOTICES.md` con los 3 orígenes |
| `npm run test:e2e` con Edge | **181 pruebas en verde y 3 saltadas** (16 ficheros; las 3 saltadas son `local.e2e.ts` sin `KARPO_LOCAL=1`): `encabezados`, `columnas`, `ayuda` (≥ 7 pasos), `mapa`, `organigrama`, `movil`, `rtl`, `rtl-ar` (12 nuevas), `ingles`, `i18n` (ahora con `ar`), `menu`, `rejilla`, `detalle`, `temas`, `utilidades` y `lib`. 0 peticiones fuera del origen (cada prueba lo comprueba) |
| `npm run capturas` | 41 PNG y su README regenerados con Edge 158 (la variante `rtl` en árabe real con `lng=ar`, sin `?dir=`); ninguna petición fuera del origen |
| Enlaces relativos de `docs/` y `README.md` | 36 ficheros y 89 enlaces, ninguno roto |
| Procesos | Ninguno vivo al terminar (los paran las propias suites y scripts) |

## Árabe (M-10, D-R38)

- `src/locales/ar/*.json`: los 10 espacios, con las claves y los marcadores de `es`. Es traducción **de demostración, sin revisión nativa**
  (Q-5 sigue abierta). Las claves plurales traen las categorías `one`, `two`, `few`, `many` y `other` (más `zero` donde se muestra el cero); `zero`
  que falta cae a `other` (i18next 27).
- `src/i18n/localeParity.ts`: `compareLocales` acepta, en cualquier idioma, las categorías plurales `_zero|_two|_few|_many` si la referencia tiene
  `_other` (sus marcadores se comparan con los de `_other`) y en una clave plural no exige `{{count}}` (el árabe dice «صف واحد» y «صفان», sin
  número). Una clave que no es plural no tiene esa tolerancia. `locales.contract.test.ts` exige ahora `ar` como idioma completo (los 10 espacios,
  todas las claves, las cinco categorías de cada plural) y prueba la regla.
- **Reserva `ar → en`** (`src/i18n/locales.ts`: `reserveFor`, `fallbackLanguages`; `createI18n` pasa `fallbackLng` por idioma; `languagesNeeded('ar')` es
  `['en', 'ar']`). Los textos de la configuración (`src/config/text.ts`, `lookupDictionary`) también caen al diccionario inglés y no al literal
  castellano de Go.
- Demo: `demo/config/i18n/ar.json` (a mano: menú, nombres de entidades, vistas, mapa, órdenes y ayuda; generado: textos de las entidades) y
  `ar.go.json` (tabla clave → árabe). `gen-demo-i18n.mjs` exige TODA clave de `facility` y `product` (`ARABIC_ENTITIES`, las entidades de las capturas)
  y tolera que falten las de las otras seis: caen al inglés. `--check` falla si falta o sobra una clave. `manifest.json` lista `ar` en `locales` e `i18n`.
- Nombre propio del idioma: `locale-name.ts` (`Intl.DisplayNames` y, si el motor no tiene datos, una tabla corta con «العربية»).
- Los datos de la demo (nombres de puestos, delimitaciones) **no se traducen**: con `ar` salen en castellano.
- `e2e/rtl-ar.e2e.ts` (12 pruebas): `lang=ar`, `dir=rtl` sin `?dir=`, cifras latinas, selector de idioma, reserva `ar → en`, espejo de menú, migas,
  chevrons, rejilla (flechas), árbol, diagrama, mapa, recorrido de ayuda y detalle; las cinco cadenas del jurado (recuento de la cabecera, «الخطوة N من M»,
  campos marcados, límites del mundo y de la ciudad, filas del pie) con sus FSI/PDI en orden lógico **y** el orden visual medido con `Range`;
  recuentos de la barra de cambios con las categorías plurales. Unitarias nuevas: `src/i18n/arabic.test.tsx`, y los casos de árabe de
  `gen-demo-i18n.test.mjs` y `capturas.test.mjs`.

## Arreglos mínimos en ficheros de otras tareas

| Fichero | Arreglo | Por qué |
|---|---|---|
| `src/i18n/localeParity.ts`, `src/i18n/locales.contract.test.ts` | Paridad con las categorías plurales del árabe (arriba) | Sin ello `ar` no podía ser un idioma exigido |
| `src/i18n/locales.ts`, `src/i18n/createI18n.ts`, `src/i18n/bundles.test.tsx` | Reserva `ar → en`; la prueba de `languagesNeeded('ar')` pasa de `['es', 'ar']` a `['en', 'ar']` | D-R38 |
| `src/config/text.ts` | `lookupDictionary` cae a la reserva del idioma | Un texto de configuración sin árabe salía en castellano |
| `src/config/compat.ts` (+ `docs/COMPATIBILIDAD-CONFIG.md` regenerado) | Clave de columna `priority` y descripción de `emptyValue` | Nota R3-3: una configuración válida con `priority` avisaba «clave desconocida» |
| `src/shell/EntityHeader.tsx`, `src/map/MapView.tsx` | `numberingSystem: 'latn'` explícito en el recuento y en los números del mapa | D-R38: dígitos latinos también si el motor eligiera los indoarábigos para `ar` |
| `scripts/demo-config.test.mjs` | `locales` esperado: `['es', 'en', 'ar']` | Cambia a propósito |
| `e2e/i18n.e2e.ts` | `ar` se suma a `es` y `en` en el barrido de claves sin traducir | Que el árabe no enseñe nunca una clave cruda |
| `e2e/utilidades.e2e.ts` | La variante `rtl` es `{ lng: 'ar', locale: 'ar' }` y la escena `mapa-region` va a `level=region&code=ES51` | Cambian a propósito |
| `e2e/ingles.e2e.ts` | El diagrama abre las ramas con `dispatchEvent('click')` | Con las tarjetas más altas de R3-4 los hijos de la última fila quedan bajo el minimapa y el clic con puntero lo recibía el minimapa (`intercepts pointer events`) |

## Decisiones y hallazgos

- **`mapa-region`: `level=region&code=ES51`, no `code=ES`.** El plan proponía `level=region&code=ES`, pero la demo no tiene `region-ES.geojson` (el nivel región se
  pide con el código de una comunidad): «No hay geometría para ES». Con `ES30` (Madrid) hay una sola provincia y el nivel se salta. Cataluña (`ES51`) enseña
  cuatro delimitaciones, una con datos y tres con la trama de «sin dato».
- **Móvil y `cuadricula-editable`.** Con 390 px la rejilla (R3-3) sólo deja `sku` y `name`; la escena edita lo que se ve (tres nombres sucios y uno abierto) y
  sólo en escritorio añade la celda inválida de `basePrice`.
- **Presupuesto.** 1,9 kB de margen sobre el límite de 215 kB; el objetivo informativo de 200 kB queda a 13,1 kB. Palancas si hiciera falta: las de `R3-R1.md`.
- **Atributos de idioma del navegador.** Las capturas y e2e de árabe abren el navegador con `locale: 'ar'` (cabecera `Accept-Language: ar`); la demo no tiene
  catálogos en árabe y devuelve los datos tal como están.

## Pendiente (no bloquea)

- Revisión nativa del árabe (Q-5) antes de usarlo fuera de la demo; traducir al árabe el resto de entidades de Go (`ar.go.json` admite añadirlas: basta con
  ampliar `ARABIC_ENTITIES`).

## Cierre de la integración (ronda 3)

- **Resuelto en la integración.** «Ver en el árbol» (`?view=tree&focus=<id>`): `TreeView` despliega los antecesores, selecciona el elemento, lo deja como actual y lo centra, una sola vez por valor y en cuanto está en el modelo (dos pruebas nuevas en `TreeView.test.tsx`). Se retira la clave `sheet.noValue` de `detail.json` (es, en, ar), que ya nadie usaba.
- **Defecto visual corregido.** El campo de clave ajena (`lookup`) cortaba el principio del nombre en RTL con texto latino; ahora lleva `text-overflow: ellipsis` y `unicode-bidi: plaintext` (`fields.css`).
- **Comprobado.** `npm ci`; `npm run check` (tipos, lint, 1650 pruebas unitarias y de componentes, 176 de contrato, licencias, iconos, i18n de la demo, compilación, presupuesto 213,1 kB de 215 y `pack:check`); `npm run test:e2e` con Edge: 181 en verde y 3 saltadas (las de `local.e2e.ts` sin `KARPO_LOCAL=1`); humo real con `KARPO_LOCAL=1` contra `cmd/karpo` (SQLite temporal): login, lista, cambio con `version` y árbol de puestos, 3 de 3; 41 capturas regeneradas y revisadas. Sin procesos vivos al terminar.
- **Sigue pendiente (no bloquea).** El objetivo informativo de 200 kB (faltan 13,1 kB); la revisión nativa del árabe.
