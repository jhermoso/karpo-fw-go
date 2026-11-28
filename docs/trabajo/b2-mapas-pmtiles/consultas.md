## analista

- ¿El esquema actual del basemap de Protomaps (capa boundaries) trae polígonos administrativos con ISO 3166-2 o sólo líneas? Si son sólo líneas, ¿qué vía reconocida obtiene polígonos OSM de regiones con código, y cumple Q-8 (ODbL, base de datos derivada)?
- ¿Debe la ruta /geo/*.pmtiles ser pública (sin JWT)? Desde el punto de vista de seguridad y de lo que permite el protocolo pmtiles de MapLibre, ¿cuál es la práctica aceptada para teselas de datos públicos en una API que, por lo demás, pide autenticación?
- Para pasar a ISO/INE, ¿se añade una columna iso_code a geo_boundaries (cambio de esquema que habría que llevar a Karpo C# y Trench), o se deriva ISO 3166-2 de abbreviation al generar las teselas? ¿Y cómo se tratan las islas NUTS 3 frente a las provincias INE 07, 35 y 38, que faltan en la semilla?
- Para generar el PMTiles: ¿tippecanoe en Docker o WSL más go-pmtiles como herramientas externas, o ampliar el escritor sin dependencias de front/react/scripts/build-pmtiles.mjs con una simplificación previa? Criterios: coste, reproducibilidad y licencia.

# Respuesta del asesor — tarea «b2-mapas-pmtiles»

Leído para responder: `front/react/docs/CONSULTAS-JAVIER.md` (Q-8), `front/react/docs/PETICIONES-BACKEND.md` (B-2, B-26), `docs/GEOGRAFIA-NUTS.md`, `contexts/geography/infrastructure/seed/boundaries.csv.gz`, `contexts/geography/infrastructure/persistence.go`, `contexts/geography/application/{service,holidays}.go`, `host/host.go`, `pkg/distribution/middleware.go`, `front/react/scripts/build-pmtiles.mjs`, `front/react/src/map/aggregate.ts`. Consultado en la web (datos no fiables, sólo como referencia): `docs.protomaps.com/basemaps/layers` y `osm-boundaries.com`.

---

## 1. Protomaps `boundaries`: ¿polígonos con ISO 3166-2 o líneas?

**Recomendación: no usar OSM en esta entrega.** La semilla sólo tiene delimitaciones por debajo del país para España, y España sale entera de IGN/CNIG; el mundo sale de Natural Earth. OSM se deja documentado para cuando la semilla tenga regiones de otro país.

**Hechos**
- La capa `boundaries` del basemap de Protomaps son **líneas** de frontera, no polígonos. Sus propiedades documentadas son `kind` (`country`, `region`, `county`, `locality`), `kind_detail` (el `admin_level` de OSM), `brk_a3` y `disputed`. **No lleva ISO 3166-2** y ninguna otra capa trae polígonos administrativos con código (`docs.protomaps.com/basemaps/layers`, leído hoy; la página no dice el tipo de geometría: que son líneas lo sé del código del basemap, no de esa página).
- Por tanto «OSM vía Protomaps», tal como está escrito en Q-8, **no da polígonos coloreables por región**. Sirve como mapa de fondo, no como coroplético.
- La semilla tiene 248 países y, por debajo, sólo España: 7 agrupaciones (nivel 1), 19 comunidades y ciudades autónomas, 52 provincias, 10 islas y 8.132 municipios (`docs/GEOGRAFIA-NUTS.md` §2 y §5; comprobado en el CSV).
- `osm-boundaries.com` exige cuenta de OSM (OAuth) para descargar y sus condiciones prohíben redistribuir su material: no vale para un proceso reproducible.

**Vías reconocidas para polígonos OSM con código (cuando hagan falta)**
1. **Extracto de Geofabrik (`<país>-latest.osm.pbf`, ODbL) + `osmium tags-filter` (`r/boundary=administrative`) + `osmium export`**. Las relaciones llevan las etiquetas `ISO3166-2` y `admin_level`. Sin cuenta, reproducible. El extracto de un país grande pasa de 1 GB.
2. **Overpass API** con `relation["boundary"="administrative"]["ISO3166-2"]`: descarga pequeña, pero con límites de uso y hay que ensamblar la geometría.
3. **Natural Earth `ne_10m_admin_1_states_provinces`** (dominio público, propiedad `iso_3166_2`): cubre el primer nivel de todo el mundo sin ODbL. Opinión: es la vía más barata para «regiones del mundo» y ya está dentro de Q-8. Sin comprobar: qué unidad usa para España (creo que provincias, con la comunidad en otra propiedad).

**ODbL (Q-8)**
- Hecho: la ODbL obliga a atribuir y, si se usa públicamente una base de datos derivada, a ofrecerla o a ofrecer **el método para rehacerla** (ODbL §4.6.b).
- Opinión: filtrar por etiqueta y convertir de formato es, según las guías comunitarias de la OSMF («Trivial Transformations»), una transformación trivial. No lo he releído hoy; no hace falta resolverlo porque el repositorio es público y el script de generación vive en él, lo que cumple §4.6.b en cualquiera de las dos lecturas.
- Condición práctica: no mezclar geometría de OSM con la del IGN dentro de la misma delimitación (cada entidad, una sola fuente), para que el fichero sea una base colectiva y no una derivada de las dos licencias.

**Para el IGN, dos avisos**
- Simplificar, disolver (islas, agrupaciones de nivel 1) o reproyectar es modificar: la atribución pasa a ser «Obra derivada de BDLJE <fecha> CC-BY 4.0 ign.es» (Q-8). Hay que guardar la fecha de los datos.
- Sin comprobar: el Centro de Descargas del CNIG suele pedir aceptar la licencia en un formulario, así que la descarga puede no ser automatizable; y Canarias viene en fichero y sistema de referencia aparte (REGCAN95). El analista debe confirmarlo al descargar.

---

## 2. ¿Ruta `/geo/*.pmtiles` pública, sin JWT?

**Recomendación: sí, pública, sólo `GET` y `HEAD`, fuera de `/api`, registrada junto a `RegisterPublicRoutes` (`host/host.go:322`).**

**Hechos**
- El fichero sólo contiene datos públicos (límites, códigos y nombres oficiales). No hay dato de inquilino ni de negocio, siempre que el generador no meta nada más: eso debe ser criterio de aceptación.
- La librería `pmtiles` pide trozos con `fetch` y cabecera `Range`. Admite cabeceras propias (`new FetchSource(url, customHeaders)` y `protocol.add(instancia)`), así que **autenticar es técnicamente posible**. El `transformRequest` de MapLibre no alcanza a esas peticiones.
- `distribution.CORSOrigins` ya admite `Range` en la consulta previa y expone `Content-Range` y `ETag` (`pkg/distribution/middleware.go:209-212`): la parte de CORS de B-26 ya está cubierta para otro origen.
- `http.ServeContent` de la biblioteca estándar resuelve `Range`, `206`, `416`, `If-Range`, `If-None-Match` y `HEAD`. **No calcula el `ETag`**: hay que ponerlo antes de llamarla. Ninguna dependencia nueva.
- No he visto middleware de compresión en `pkg/distribution` ni en `host/serve`.

**Opinión (práctica aceptada)**
- Las teselas de datos públicos se sirven como estáticos públicos y cacheables (es lo que hacen Protomaps, OpenFreeMap y los despliegues sobre S3 o R2). Con `Authorization`, las cachés compartidas dejan de servir, cada trozo lleva el token y hay que renovar la sesión dentro del protocolo: coste sin beneficio.
- Requisitos del manejador:
  - sirve **un fichero fijo dado por configuración**, nunca una ruta construida con lo que pide el cliente (evita recorrer directorios);
  - `ETag` fuerte calculado una vez al arrancar, `Cache-Control: public` con caducidad larga; mejor aún, nombre de fichero con versión;
  - **nunca comprimir esta ruta** (`Content-Encoding` rompe los rangos; las teselas ya van comprimidas por dentro);
  - si la variable de entorno no está, la ruta da 404 y el front sigue con su origen configurado.
- DDD: es un estático de distribución, no hay comportamiento de dominio. No lleva agregado, comando ni permiso. Encaja como pieza pequeña de `pkg/distribution` montada por el anfitrión. Es una desviación justificada por complejidad innecesaria y conviene decírselo a Javier en el informe, como pide `FORMA-DE-TRABAJO.md` §5.
- Reversible: si un despliegue lo quisiera privado, se pasa a `FetchSource` con cabeceras sin tocar el fichero.

---

## 3. ¿Columna `iso_code` o derivar de `abbreviation`? Islas y provincias 07, 35, 38

**Recomendación: no añadir columna. Derivar al generar las teselas, con la regla que el propio contexto ya usa.** Ningún cambio de esquema, nada que llevar a Karpo C# ni a Trench.

**Hechos**
- `holidays.go:137-140` ya resuelve un lugar `ES-XX` como `country` + `-` + `abbreviation` en los niveles 1 y 2. La regla ISO 3166-2 = país + abreviatura **ya existe en el repositorio**.
- Las 19 comunidades y ciudades autónomas llevan en `abbreviation` el sufijo ISO: GA, AS, CB, PV, NA, RI, AR, MD, CL, CM, EX, CT, VC, IB, AN, MC, CE, ML, CN.
- **Una discrepancia: Navarra.** La semilla dice `NA`; en ISO 3166-2:ES la comunidad foral es `ES-NC` y `ES-NA` es la provincia. Lo afirmo de memoria, con bastante seguridad; hay que contrastarlo con la tabla de ISO antes de tocar nada. Si se confirma, es una corrección de dato (una migración de una fila) que sí se traslada a Trench y Karpo C#, según la regla de «Datos» de `CLAUDE.md`. No he encontrado ningún `ES-NA` ni `ES-NC` en el repositorio, así que hoy no rompe nada.
- Las 7 agrupaciones de nivel 1 llevan `abbreviation` = `ES1`…`ES7`: la regla daría `ES-ES1`, que no existe. No tienen código ISO ni INE.
- Provincias: Q-8 pide **INE**, no ISO. Las 52 tienen `ine_code` y `geo_code` de dos cifras. Municipios: `ine_code` = `lau_code` de cinco cifras.
- **La premisa de la consulta no es exacta: las provincias 07, 35 y 38 están en la semilla**, con su `ine_code`. Lo que no tienen es `nuts_code` ni nivel (la columna `level` del CSV está vacía en esas tres filas), porque su nivel 3 son las islas. Faltan en `?level=3`, no en la tabla.
- El front enlaza hoy por `nutsCode || lauCode || ineCode || geoCode` (`front/react/src/map/aggregate.ts:51`); por eso las tres provincias insulares caen ahora en su `ineCode`.

**Clave `code` de la tesela (opinión)**

| Delimitación | `code` | Sale de |
|---|---|---|
| País | alfa-2 | `geo_code` |
| Comunidad o ciudad autónoma | `ES-CT` | `country` + `-` + `abbreviation` |
| Provincia (las 52, por tipo «Province») | `28` | `ine_code` |
| Municipio | `28079` | `ine_code` |

- El front calcula la misma clave con campos que la API ya devuelve (`country`, `abbreviation`, `ineCode`, `level`). La regla queda en dos sitios (generador y front): debe estar escrita en `docs/GEOGRAFIA.md` y cubierta por una prueba que compare las claves del fichero con la semilla.
- **Islas y agrupaciones de nivel 1: fuera del mapa en esta entrega.** El mapa navega por la escalera administrativa (país, comunidad, provincia, municipio); la isla y la agrupación son niveles estadísticos. El `parentCode` de un municipio son las dos primeras cifras de su código INE, también en las islas, aunque en la base cuelgue de su isla (migración 7). El INE tiene códigos de isla de tres cifras si algún día se quieren pintar (dato de memoria, sin contrastar).
- Esto último roza el producto («¿el mapa enseña provincias o islas en Baleares y Canarias?»). Es barato de cambiar, porque basta regenerar el fichero, así que lo tomaría como decisión técnica anotada y se lo contaría a Javier.
- El planificador debe comprobar que la API permite pedir «las provincias de una comunidad» por tipo y no sólo por `level=3`; no lo he verificado.

---

## 4. Herramienta para generar el PMTiles

**Recomendación: tippecanoe con versión fijada, en Docker, escribiendo `.pmtiles` directamente.** No ampliar `build-pmtiles.mjs`, que se queda para la demo esquemática.

| Opción | Coste | Reproducibilidad | Licencia |
|---|---|---|---|
| **tippecanoe (felt) en Docker** | Bajo: un script. Simplifica por zoom, recorta y comprime | Buena si se fija la etiqueta y el resumen de la imagen. La salida no es idéntica byte a byte entre ejecuciones (opinión), así que no vale un `--check` como el de la demo | BSD-2-Clause |
| go-pmtiles (CLI) | Opcional, sólo para `pmtiles show` y `verify`. Tippecanoe ya escribe PMTiles desde la 2.17 | Binario publicado para Windows | BSD-3-Clause |
| Ampliar `build-pmtiles.mjs` | **Alto.** Hoy son 560 líneas sin compresión ni simplificación. Con 8.132 municipios reales habría que escribir simplificación que conserve las fronteras compartidas, recorte por tesela, descarte de polígonos mínimos, gzip y directorios hoja: rehacer tippecanoe y mapshaper | Excelente (determinista) | Propia |
| Planetiler | Medio: Java 21. Pensado para `.osm.pbf` | Buena | Apache-2.0 |

**Hechos**
- En esta máquina hay Docker 29.9.2 y WSL con Ubuntu. No hay `tippecanoe`, `pmtiles`, `ogr2ogr` ni `mapshaper` en el `PATH`.
- Ninguna de estas herramientas entra en `go.mod`: se ejecutan, no se enlazan, y su licencia no afecta al fichero generado. Es el mismo trato que gremlins en `FORMA-DE-TRABAJO.md` §8.

**Sin comprobar:** si felt publica una imagen oficial de tippecanoe. Si no, se construye desde el `Dockerfile` de su repositorio en una etiqueta fijada.

**Preparación previa (opinión)**
- GDAL (`ogr2ogr`, licencia MIT, imagen `ghcr.io/osgeo/gdal`) para pasar los shapefiles del IGN a GeoJSON en WGS84 y unir Canarias.
- mapshaper (MPL-2.0) con `npx mapshaper@<versión>` para disolver y simplificar conservando la topología, **sin añadirlo a `package.json`**. Si se quisiera fijar como dependencia de desarrollo, eso sí es decisión de Javier.
- Anotar SHA-256, fecha, fuente, licencia y tamaño de cada descarga. La fecha de los datos del IGN es obligatoria para la atribución.

**Aviso de diseño para el planificador (el punto de más riesgo)**
- B-2 pide «cada nivel de detalle ya presente en los zooms bajos», porque el front lista y encuadra las entidades leyendo las teselas cargadas (cabecera de `build-pmtiles.mjs`). Con geometría real, eso mete 8.132 municipios en la tesela de z0.
- Tippecanoe, por defecto, descarta entidades y funde los polígonos diminutos, y limita la tesela a 500 KB. Para garantizar la presencia hay que desactivarlo (`-r1`, `--no-feature-limit`, `--no-tile-size-limit`, `--no-tiny-polygon-reduction`; confirmar los nombres con `--help`) y aceptar teselas bajas pesadas.
- Opinión: es más sólido generar junto al `.pmtiles` un **índice JSON pequeño** (código, caja y centroide por delimitación, que B-2 ya pide como «centroides») y dejar que los municipios empiecen en un zoom medio. Cambia el contrato con el front, así que lo debe valorar el planificador con una medida real del tamaño de z0 antes de elegir.

---

## Lo que no he podido comprobar
- El tipo de geometría de `boundaries` en la documentación de Protomaps (la página no lo dice).
- El código ISO de Navarra (`ES-NC`) contra la tabla oficial.
- La forma de descarga y los sistemas de referencia de los ficheros del CNIG.
- La unidad que usa Natural Earth admin-1 para España.
- Qué hacen Karpo C# y Trench con `abbreviation` y `geo_code`: no los he abierto. La recomendación del punto 3 no cambia el esquema, así que sólo les afectaría la corrección de Navarra.
- Si la API de delimitaciones filtra por tipo además de por nivel.

## analista

- ¿Se acepta omitir shortName en esta entrega y anotarlo en PETICIONES-BACKEND.md, en vez de añadir columna al esquema?
- ¿La corrección de Navarra (abreviatura NA→NC con migración) se incluye en esta rama o va en tarea aparte, dado que cambia lo que resuelve ES-NA en holidays.go y debe trasladarse a Trench y Karpo C#?

Respuestas del asesor a las dos consultas del analista (tarea b2-mapas-pmtiles).

## 1. shortName: sí, se omite en esta entrega y se anota

Recomendación: omitirlo. No se añade columna al esquema ni una tabla de nombres cortos en la herramienta generadora.

Lo que sé con seguridad:
- El front ya degrada sin él. `front/react/src/map/labels.ts` (líneas 93-110) prueba nombre completo, `shortName` y código, y `textOfKind` usa `texts.shortName || texts.name`. `front/react/docs/notas/R2-R3.md` línea 72 lo dice expresamente: el nombre corto sólo existe si la geometría lo trae.
- No hay nombre corto en el backend: ni `shortName` ni `short_name` aparecen en ningún `.go` ni `.sql`. Hoy sólo vive en la demo (`SHORT_NAMES` en `front/react/demo/data/spain.mjs`).
- `geo_boundaries` (`contexts/geography/infrastructure/persistence.go` línea 24) no tiene esa columna. Añadirla sería una diferencia de esquema con Trench y Karpo C#, que `CLAUDE.md` obliga a justificar por escrito.

Mi opinión:
- Un nombre corto es un texto traducible, así que su sitio es la petición de catálogos traducibles (B-24), no una columna suelta ni una tabla en español dentro del generador.
- Conviene que la propiedad no se emita (ni siquiera vacía) y que el analista compruebe que `text(props.shortName)` en `front/react/src/map/geo.ts` (líneas 160 y 208) tolera la ausencia.
- En `PETICIONES-BACKEND.md`, B-26 quedaría como hecha salvo `shortName`, con la razón y el enlace a B-24. Efecto visible: los rótulos largos («Comunidad Foral de Navarra») caen al código en vez de a «Navarra».
- Es una decisión técnica y reversible: se toma, se anota y se le cuenta a Javier; no hace falta pararse.

## 2. Navarra (NA→NC): tarea aparte, no en esta rama

Recomendación: fuera de esta rama. Se anota en `BACKLOG.md` y se propone como decisión común para los cuatro proyectos.

Lo que sé con seguridad:
- El error existe: la semilla (`contexts/geography/infrastructure/seed/boundaries.csv.gz`) tiene «Comunidad Foral de Navarra, ES22, abreviatura NA». En ISO 3166-2:ES la comunidad es `ES-NC` y `ES-NA` es la provincia. Las otras 18 abreviaturas de nivel 2 coinciden con ISO.
- `Locate` (`contexts/geography/application/holidays.go` líneas 137-140) busca por país, niveles 1-2 y abreviatura, así que hoy `ES-NA` devuelve la comunidad y `ES-NC` no devuelve nada. El arreglo cambia el comportamiento de una ruta pública.
- Las provincias llevan como abreviatura el código INE (`31`, `28`), no letras ISO, así que dentro del fichero de teselas `ES-NA` no choca con ninguna otra clave.
- Ninguna prueba ni código Go usa `ES-NA` (`holidays_test.go` sólo prueba `ES-MD` y `ES-CT`).

Sin comprobar:
- No he podido verificar qué tienen Trench y Karpo C# para Navarra: la búsqueda en el repositorio hermano no terminó a tiempo. El analista o el orquestador deben confirmarlo antes de trasladar el cambio.
- No he mirado si una base ya sembrada se actualiza al cambiar el CSV; doy por hecho que hace falta una migración de datos y eso hay que confirmarlo en la tarea nueva.

Por qué aparte (opinión):
- `CLAUDE.md` pide un commit por tarea. Esto es una corrección de datos con migración, cambio de comportamiento en festivos y traslado a otros dos repositorios; nada de eso es «generar y servir PMTiles».
- Los calendarios o datos de festivos ya declarados con `ES-NA` necesitan su propio análisis.
- No es decisión de Javier: `CLAUDE.md` ya fija que un error de Trench se corrige aquí y se traslada. Basta con informarle.

Cómo debe quedar B-2 mientras tanto (opinión):
- El generador saca `code` de la misma semilla que usa la API (país + abreviatura para niveles 1-2, INE para provincias y municipios), sin excepción a mano para Navarra. Si la tesela dijera `ES-NC` y la API `NA`, Navarra no casaría con sus datos en el mapa, que es peor que un código mal rotulado.
- La tesela dirá `ES-NA` hasta que se arregle la semilla; al regenerarla después sale `ES-NC` sola. Se anota como defecto conocido en el análisis y en `PETICIONES-BACKEND.md`.
- Conviene una prueba en esta rama que compruebe que toda clave `code` de nivel 1-2 del fichero existe en la semilla, para que el arreglo posterior no deje el mapa descolgado sin que nadie lo note.

Rutas consultadas: `C:/Git/Paranoia/karpo-fw-go-pmtiles/front/react/docs/PETICIONES-BACKEND.md`, `C:/Git/Paranoia/karpo-fw-go-pmtiles/front/react/src/map/labels.ts`, `C:/Git/Paranoia/karpo-fw-go-pmtiles/contexts/geography/application/holidays.go`, `C:/Git/Paranoia/karpo-fw-go-pmtiles/contexts/geography/infrastructure/persistence.go`, `C:/Git/Paranoia/karpo-fw-go-pmtiles/contexts/geography/infrastructure/seed/boundaries.csv.gz`, `C:/Git/Paranoia/karpo-fw-go-pmtiles/docs/GEOGRAFIA-NUTS.md`.

## orquestador (por planificador)

El juez ha suspendido 2 veces esta entrega del planificador. Da directrices concretas para que apruebe a la siguiente.

Entrega:
# Plan: b2-mapas-pmtiles (B-2 y B-26). Segunda versión, con los ocho hallazgos del juez resueltos

Carpeta: `C:/Git/Paranoia/karpo-fw-go-pmtiles`, rama `feat/mapas-pmtiles`. Parte del análisis aprobado (tercera versión).

## 0. Resumen
- **Cuatro piezas:**
  - **Regla de claves en geografía:** `BoundaryCode` produce `code` y `parentCode`, y la API de geografía los devuelve. Esto amplía el contrato de la API sin cambiar el modelo ni el esquema (hallazgo 1).
  - **Tubería reproducible** (script y contenedores) que genera `karpo.pmtiles` a partir de la semilla, Natural Earth y BDLJE.
  - **Servidor estático mínimo** en `pkg/distribution`, montado en `host/serve`.
  - **Front:** enlaza por `code`, y se ajustan sus padres y sus códigos por defecto.
- **Fuera de esta rama:** OSM (pendiente de D-a), `shortName` (pasa a B-24) y la corrección de Navarra (tarea aparte).
- `go.mod` no recibe ninguna dependencia nueva.

## 1. Contextos delimitados afectados
| Contexto o capa | Cambio |
|---|---|
| `contexts/geography` | **El modelo y el esquema no cambian.** (a) `application/codes.go`, nuevo, con `BoundaryCode`, una función pura. (b) `BoundaryDTO` gana `Code` y `ParentCode` (JSON `code` y `parentCode`), que se calculan con `BoundaryCode` al construir el DTO. **El contrato de `GET /api/geography/boundaries` cambia: es una ampliación compatible**, porque ningún campo desaparece. (c) `cmd/geokeys` escribe `keys.csv`. (d) Pruebas de integridad y de ida y vuelta con `Locate`. (e) La regla se documenta en `docs/GEOGRAFIA.md`. `holidays.go` **no se refactoriza** (hallazgo 7): la coherencia entre las dos reglas la fija una prueba de ida y vuelta |
| `pkg/distribution` | `StaticFile` nuevo: Range y un ETag que se recalcula si el fichero cambia |
| `host/serve` | Variable `KARPO_PMTILES` y ruta `/geo/karpo.pmtiles`, montada fuera de la autenticación |
| `front/react` | `aggregate.ts`, `sources.ts` (`parentCandidates`), `demo/config/manifest.json` (`defaultCode`), `geo.ts` (`shortName` opcional), `scripts/build-pmtiles.mjs` y `docs/PETICIONES-BACKEND.md` |
| Herramientas | `tools/geo/` (nuevo): `build.sh`, `join` (Go, sólo biblioteca estándar) y `SOURCES.md` |
| Resto de contextos | No cambian |

**Consecuencias en tres niveles:**
1. **Contexto.** La geografía es la única dueña de las claves. API y tesela salen de la misma función, así que casan por construcción. Los clientes de la API reciben dos campos nuevos, y ninguno se rompe.
2. **Artefactos.** Cuando se corrija la semilla de Navarra, cambiarán a la vez el `code` de la API, la tesela (al regenerarla) y la ruta de festivos (en esa tarea aparte). `StaticFile` detecta que el fichero ha cambiado por su tamaño y su `mtime`, y recalcula el ETag sin reiniciar el proceso (hallazgo 8).
3. **Despliegue.** El `.pmtiles` es un artefacto de despliegue y no entra en el repositorio. Sin la variable, la ruta no existe y el front cae a la demo esquemática. Si el fichero se sustituye en caliente, conviene hacerlo con un `rename` atómico. Las lecturas que ya estén en curso terminan con el descriptor antiguo, porque cada petición abre el fichero por su cuenta.

## 2. Artefactos y patrones de obligado cumplimiento
- **`pkg/domain`: no se usa.** No hay estado ni invariantes: se sirve un fichero público de sólo lectura, y un agregado sería complejidad innecesaria.
- **`pkg/application`: no se usa para el fichero.**
  - GET y HEAD son idempotentes y no hay transacción.
  - Los datos son públicos, y un JWT impediría la caché compartida.
  - La ruta se monta fuera de `Authorize`, como `/healthz`.
  - La consulta de geografía que ya existe sigue con su cadena de permiso. Sólo gana campos.
- **`BoundaryCode`** vive en `contexts/geography/application`.
- **Migraciones:** ninguna.
- **Rutas HTTP:**
  - `GET` y `HEAD` en `/geo/karpo.pmtiles`. Con cualquier otro método, `405` y `Allow: GET, HEAD`.
  - `GET /api/geography/boundaries` devuelve además `code` y `parentCode`.

### Escalera del mapa y regla de claves (hallazgos 2, 3, 4 y 5)
El mapa va de `country` a `2`, de `2` a `3` y de `3` a `local`, como dice `demo/config/manifest.json:45-48`. **El nivel 1 (las agrupaciones NUTS 1) queda excluido de forma explícita**: no tiene ISO ni geometría en BDLJE.

| Tipo en la semilla | `kind` (tesela) | `level` (tesela) | `code` | `parentCode` | Geometría |
|---|---|---|---|---|---|
| País | `country` | `country` | alfa-2 (`geo_code`) | — | Natural Earth |
| Agrupación de nivel 1 (7) | — | — | **vacío**: `ok=false` en la tesela y `code` vacío en la API | — | ninguna |
| Comunidad o ciudad autónoma (19) | `region` | `2` | `ES-` + `abbreviation` | `ES` (**antecesor más cercano que está en la escalera**: se salta el nivel 1) | BDLJE, comunidades |
| Provincia (50) | `province` | `3` | INE de 2 cifras | `ES-XX` de su comunidad | BDLJE, provincias |
| Isla (10, ES531…ES709) | `island` | `3` | **ver D-c**; provisionalmente, `geo_code` | `ES-IB` o `ES-CN` | municipios de BDLJE disueltos por isla |
| Municipio (8.132) | `municipality` | `local` | INE de 5 cifras | `code` de su padre en la semilla: provincia o isla | BDLJE, municipios |

- **El `level` de la tesela sale del `kind`, no de la columna `level`.** Así, Santa Cruz de Tenerife (38), que en la semilla tiene `level` vacío, se pinta en el nivel `3` (hallazgo 5). La API devuelve `level` tal cual está en la semilla, porque no se toca.
- **`parentCode`** es el `code` del antecesor más cercano cuyo tipo está en la escalera: se sube por `Parent` hasta encontrarlo. Ejemplos:
  - el padre de 07040 (Palma) es la isla de Mallorca;
  - el padre de una isla es su comunidad;
  - el padre de una comunidad es `ES`.
- **`hasChildren`** se pone a `true` si alguna entidad de la tesela tiene a ésta como `parentCode`. Se calcula en `cmd/geokeys`. España sale con `true` y el resto de países con `false`.
- **Recuentos esperados**, sacados de la semilla: 248 países (menos los que NE no traiga, que se listan), 19 comunidades, 50 provincias, 10 islas y 8.132 municipios.
- **Entidades de BDLJE que se esperan sin enlazar**, en una lista cerrada que vive en `tools/geo/expected-unmatched.txt`:
  - las provincias 07 y 35, que la semilla sustituye por islas;
  - los condominios o territorios sin municipio en la semilla que aparezcan, cada uno anotado con su `NATCODE` al ejecutar T4.

  Cualquier otra entidad sin enlazar hace fallar la tubería.

### Teselas
- **Capa `boundaries`** (polígonos), con estas propiedades:
  - `code`, `name`, `level` y `kind` (siempre);
  - `parentCode` (salvo en los países);
  - `hasChildren` (siempre);
  - `lauCode` (sólo en los municipios).
- **Capa `labels`**: un punto `ST_PointOnSurface` por entidad, con las mismas propiedades.
- **Atribución en la metadata:** «Natural Earth (dominio público); Obra derivada de BDLJE <fecha> CC-BY 4.0 ign.es».

## 3. Librerías y herramientas de terceros
Ninguna entra en `go.mod` ni en `package.json`.

| Necesidad | Candidatas | Coste y complejidad | Licencia | Elegida |
|---|---|---|---|---|
| Teselado a PMTiles | **tippecanoe (felt)**, Planetiler, ampliar `build-pmtiles.mjs` | tippecanoe: un comando. Planetiler: una JVM, excesivo. Script propio: reescribir la simplificación | BSD-2 / Apache-2 / propia | **tippecanoe** en Docker, con la versión fijada (la imagen oficial si existe; si no, un Dockerfile en `tools/geo/` con el commit fijado) |
| Shapefile a GeoJSON WGS84, disolución por isla y centroides | **GDAL `ogr2ogr`**, mapshaper | GDAL: la imagen oficial, con SQL/SpatiaLite para `ST_Union` y `ST_PointOnSurface` | MIT / MPL-2.0 | **GDAL** (`ghcr.io/osgeo/gdal`, con la etiqueta fijada) |
| Simplificación topológica | sólo tippecanoe, mapshaper | mapshaper añade un paso | MPL-2.0 | **Sólo tippecanoe**, con `--detect-shared-borders`. mapshaper se añade sólo si aparecen huecos (decisión reversible) |
| Comprobación del fichero | go-pmtiles CLI, lector propio | un binario | BSD-3 | **CLI** en la tubería. La prueba Go lee la cabecera y la metadata con la biblioteca estándar |
| Servidor con Range | **`http.ServeContent`**, `pmtiles serve` | biblioteca estándar | BSD | **`http.ServeContent`** |

## 4. Diseño detallado

### 4.1 `pkg/distribution/static.go` (hallazgo 8)
```go
type StaticFile struct {
    path  string
    ctype string
    mu    sync.Mutex
    size  int64
    mod   time.Time
    etag  string // "\"sha256-<hex>\""
}
func NewStaticFile(path, contentType string) (*StaticFile, error)
func (s *StaticFile) ServeHTTP(w http.ResponseWriter, r *http.Request)
```
- **Método.** Sólo GET y HEAD. Con cualquier otro, `405` y `Allow`.
- **Por cada petición:**
  1. `os.Open`, y después `f.Stat()` sobre el **mismo descriptor**.
  2. Si el tamaño o el `mtime` no coinciden con los guardados, se recalcula el SHA-256 de ese descriptor (con el mutex cogido) y se actualizan los tres campos. Así el ETag corresponde siempre a los bytes que se sirven.
  3. Se ponen `ETag`, `Cache-Control: public, max-age=86400, stale-while-revalidate=604800`, `Content-Type: application/vnd.pmtiles` y `Accept-Ranges: bytes`.
  4. Se llama a `http.ServeContent(w, r, "", mod, f)` y se cierra el fichero.
- **Sin compresión.** No hay middleware de compresión y no se añade.
- **Errores.** Si el fichero no existe, `404`.
- **CORS.** Lo resuelve ya `middleware.go:209-212`.

### 4.2 `host/serve/serve.go`
- **Configuración.** `Config.PMTiles` se lee de `KARPO_PMTILES`. Si está vacía, la ruta no se monta.
- **Montaje.** Después de `health.Mount(mux)`, si la variable tiene valor, se llama a `NewStaticFile` y se registra `mux.Handle("/geo/karpo.pmtiles", f)`.
- **Fichero inexistente.** El arranque falla con un error que nombra `KARPO_PMTILES`.
- **Documentación.** La variable se documenta en el comentario de `Config` y en `docs/ARQUITECTURA.md`.

### 4.3 `contexts/geography/application/codes.go` (hallazgos 1, 3, 4 y 7)
```go
// BoundaryCode devuelve la clave pública de b y la de su antecesor más cercano en la escalera del mapa.
// ok es false para la agrupación de nivel 1 y para tipos desconocidos.
func BoundaryCode(b BoundaryDTO, byID func(id string) (BoundaryDTO, bool)) (code, parentCode string, ok bool)

// MapKind devuelve country, region, province, island o municipality; "" si no está en la escalera.
func MapKind(b BoundaryDTO) string
```
- **En la API.** El servicio rellena `BoundaryDTO.Code` y `ParentCode` al construir cada DTO. Usa un `byID` sobre el catálogo que ya tiene en memoria; si no lo tiene, hace una lectura por padre, que se mide en T2. Si `ok=false`, los dos campos quedan vacíos (`omitempty`).
- **`holidays.go` no cambia.** En su lugar, una prueba de ida y vuelta comprueba, sobre toda la semilla, que para cada comunidad, provincia y municipio `Locate(BoundaryCode(b))` devuelve `b`. Así la regla de ida (`BoundaryCode`) y la de vuelta (`Locate`) no pueden divergir sin que falle una prueba. Las islas quedan fuera de esa prueba, porque `Locate` no las resuelve.
- **`cmd/geokeys`** (biblioteca estándar) escribe `keys.csv` con las columnas `code,parentCode,name,level,kind,hasChildren,lauCode,ine,islandOf`. `islandOf` es el `code` de la isla de cada municipio insular, y es lo que la tubería usa para disolver.

### 4.4 Tubería `tools/geo/build.sh`
1. `go run ./cmd/geokeys > $OUT/keys.csv`.
2. **Descarga** a `$GEO_WORK` (por defecto `C:/Git/Paranoia/geo-downloads/`, fuera del repositorio) de NE admin-0 y de los recintos autonómicos, provinciales y municipales de BDLJE. Se comprueba el SHA-256 contra `SOURCES.md` y la tubería aborta si no coincide.
3. **Conversión** con `ogr2ogr -t_srs EPSG:4326` a GeoJSON, una capa por nivel.
4. **Islas.** Se unen los municipios de BDLJE con `keys.csv` por `NATCODE`→INE y se disuelven con `ST_Union … GROUP BY islandOf`. Salen 10 polígonos.
5. **Enlace** con `go run ./tools/geo/join`:
   - NE por `ISO_A2_EH`;
   - BDLJE por `NATCODE`.

   Lo que quede sin enlazar se compara con `expected-unmatched.txt`, y cualquier diferencia hace fallar la tubería. Se informa de los países de la semilla que NE no trae.
6. **Centroides** con `ST_PointOnSurface`, en la capa `labels`.
7. **Teselado** con tippecanoe: `-Z0 -z12`, capas `boundaries` y `labels`, las opciones de no descartar (que se confirman con `--help` en T1), `--detect-shared-borders` y `--attribution`.
8. **Verificación** con `pmtiles verify` y `pmtiles show`. Se anotan el tamaño total y el de z0 para D-b.

### 4.5 Front (`front/react`) (hallazgos 1 y 6)
- **`src/map/aggregate.ts:47-52`.** `boundaryCode()` pasa a devolver `code || lauCode` y deja `ineCode` y `geoCode` como respaldo para la demo. Funciona porque la API ya devuelve `code`.
- **`src/map/sources.ts` (`parentCandidates`).** Deja de deducir el padre recortando NUTS (`ES300` → `ES30` → `ES`). El padre de cada código pasa a ser:
  - `ES-XX` → `ES`;
  - INE de 2 cifras → la comunidad, buscada en la tabla de entidades cargadas;
  - INE de 5 cifras → sus 2 primeras cifras, o la isla, según el `parentCode` de la entidad.

  La regla es: usar `parentCode` si la entidad está cargada, y recurrir al recorte sólo para los códigos de la demo.
- **`demo/config/manifest.json`.** Los `defaultCode` pasan de `ES30` a `ES-MD` y de `ES300` a `28`.
- **`src/map/geo.ts:160` y `:208`.** `text(props.shortName)` tiene que tolerar `undefined`: se protege, y el rótulo cae al nombre y luego al código.
- **`scripts/build-pmtiles.mjs`.** La demo emite `code`, `parentCode`, `level` y `hasChildren` con la misma escalera.
- **`docs/PETICIONES-BACKEND.md`:**
  - B-2 queda hecha (o hecha con el contrato nuevo, si D-b(ii));
  - B-26 queda hecha salvo `shortName`, con un enlace a B-24;
  - se anotan Navarra y las claves provisionales de las islas (D-c).

## 5. Tareas
| # | Tarea | Depende de | Paralelo con | Modelo y esfuerzo | Por qué |
|---|---|---|---|---|---|
| T1 | Descargas y registro en `SOURCES.md` (NE, BDLJE, imágenes de tippecanoe y GDAL, CLI de pmtiles). Hay que confirmar las opciones de tippecanoe, el sistema de referencia de Canarias y si el CNIG exige formulario; si lo exige, se para y se avisa a Javier | — | T2, T3 | Sonnet, esfuerzo bajo | Mecánica, con cuidado de tratar los datos como no fiables |
| T2 | `BoundaryCode`, `MapKind`, `Code`/`ParentCode` en el DTO, `cmd/geokeys` con `hasChildren` e `islandOf`, pruebas (tabla, integridad e ida y vuelta) y `docs/GEOGRAFIA.md`. Hay que comprobar cuál es el padre de los municipios de 38 y que en la semilla no existen las provincias 07 y 35 | — | T1, T3 | Sonnet, esfuerzo medio | Cambia el contrato de la API y exige pruebas de mutación |
| T3 | `StaticFile` con recálculo del ETag, `KARPO_PMTILES` y sus pruebas | — | T1, T2 | Sonnet, esfuerzo medio | Pieza pequeña, pero los casos de Range, ETag y cambio de fichero son delicados |
| T4 | Tubería y `join`, disolución por isla, `expected-unmatched.txt`; generar el fichero y medir el total y z0 | T1, T2 | T5 | Opus, esfuerzo medio | La más incierta: sistemas de referencia, enlaces y opciones de tippecanoe |
| T5 | Front: `aggregate`, `sources`, manifest, `shortName`, demo y `PETICIONES-BACKEND.md`, con vitest | T2 | T4 | Sonnet, esfuerzo medio | Hay que tocar las migas y los niveles por defecto, no sólo el enlace |
| T6 | Cierre: `BACKLOG.md` (Navarra, OSM, `shortName` y D-c), batería, mutación y un solo commit | T3, T4, T5 | — | Sonnet, esfuerzo bajo | Integración |

## 6. Pruebas
- **`pkg/distribution/static_test.go`:**
  - GET completo: `200`, `Accept-Ranges`, `ETag`, `Cache-Control` y `Content-Length`;
  - `Range: bytes=0-15`: `206`, `Content-Range` y cuerpo exacto;
  - rango fuera del fichero: `416`;
  - `If-None-Match`: `304`;
  - `If-Range` con un ETag distinto: `200` completo;
  - HEAD sin cuerpo;
  - POST: `405` con `Allow`;
  - fichero borrado: `404`;
  - `NewStaticFile` sobre una ruta inexistente: error;
  - **fichero sustituido (otro contenido y otro `mtime`): el ETag cambia, y un `If-Range` con el ETag antiguo recibe `200` completo, nunca `206`;**
  - CORS a través de `CORSOrigins`: se exponen `Content-Range` y `ETag`.
- **`host/serve`:** sin la variable, la ruta no se monta; con una ruta inexistente, el arranque falla nombrando `KARPO_PMTILES`.
- **`contexts/geography`:**
  - **Tabla de `BoundaryCode`:**
    - el país `ES`;
    - `ES-MD` con padre `ES`, que se salta ES3;
    - `ES-NA`, la comunidad: es el defecto documentado, y la prueba lo fija para que la corrección futura lo cambie a propósito;
    - la provincia `28` con padre `ES-MD`;
    - **la provincia `38`, con `level` vacío en la semilla, que da `kind=province` y nivel de tesela `3`**;
    - una isla (Mallorca) con padre `ES-IB`;
    - el municipio `07040` con padre Mallorca;
    - el municipio `28079` con padre `28` y `lauCode`;
    - la agrupación ES3, que da `ok=false`;
    - un tipo desconocido, que da `ok=false`.
  - **Integridad sobre la semilla:**
    - cada `parentCode` de `keys.csv` existe como `code`;
    - no hay códigos duplicados;
    - los recuentos son 19, 50, 10 y 8.132;
    - `hasChildren` es coherente con los hijos.
  - **Ida y vuelta** de `Locate` con `BoundaryCode` sobre todas las comunidades, provincias y municipios.
  - **API:** `GET /api/geography/boundaries?type=…` devuelve `code` y `parentCode`.
  - `holidays_test.go` sigue verde.
- **Fichero generado** (etiqueta de compilación `geo`, fuera de la batería normal):
  - la cabecera v3 y la metadata llevan la atribución;
  - el recuento por `kind` coincide con `keys.csv`, menos los países que NE no trae, que van listados;
  - todas las claves pertenecen a `keys.csv`;
  - todas las entidades llevan `level` y `hasChildren`.

  No se compara byte a byte.
- **Front (vitest):**
  - `boundaryCode` con `code`;
  - `parentCandidates` para `ES-MD`, `28`, `28079` y un municipio insular;
  - el manifest arranca en `ES-MD` y en `28`;
  - una entidad sin `shortName` se rotula con su nombre.
- **Mutación:** `go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 unleash --workers 4 --timeout-coefficient 10` sobre:
  - `./contexts/geography/domain` (sólo se mide, porque no cambia);
  - `./contexts/geography/application`;
  - `./pkg/distribution`.

  Los supervivientes anteriores a la tarea se apuntan en `BACKLOG.md`. StrykerJS no está instalado, así que la del front figura como «no ejecutada».
- **Batería:** `go build ./... && go vet ./... && go test -count=1 -cover ./...` (mínimo del 80 % en los paquetes tocados) y `vitest`.
- **Comando para Javier:** arrancar con `KARPO_PMTILES=<ruta>/karpo.pmtiles` y ejecutar `curl -sI -H "Range: bytes=0-126" http://localhost:8080/geo/karpo.pmtiles`. Debería mostrar `206`, `Accept-Ranges: bytes` y un `ETag`.

## 7. Decisiones para Javier
1. **D-a.** Sin OSM en esta entrega: el mundo sale de Natural Earth y España del IGN. OSM se aplaza hasta que haya regiones de otros países (con Geofabrik y osmium). Es un recorte frente a Q-8.
2. **D-b.** Municipios en los zooms bajos. Primero se mide z0 con todos los municipios desde z0. Si pesa demasiado, se le lleva la cifra junto con la alternativa: municipios desde un zoom medio y un índice JSON de cajas y centroides.
3. **D-c (nueva). La clave de las 10 islas.** Ni ISO 3166-2 ni INE (en provincias y municipios) tienen código de isla, y la semilla pone las islas en lugar de las provincias 07 y 35. Hay dos opciones:
   - **(i)** Usar la clave `geo_code` de la isla (por ejemplo `ES532`) como excepción documentada. Es NUTS, lo que choca con «`code` nunca en NUTS» de B-2.
   - **(ii)** Añadir las provincias 07 y 35 a la semilla y dejar las islas fuera del mapa. Cambia los datos, y hay que trasladarlo a Trench y a Karpo C#.

  Propuesta: implementar (i) de forma provisional, porque es reversible regenerando, y anotarlo en `PETICIONES-BACKEND.md`.

Decisiones técnicas que sólo se le cuentan:
- el fichero estático va sin DDD ni CQRS;
- la API de geografía gana `code` y `parentCode` (ampliación compatible);
- el nivel 1 queda fuera de la escalera del mapa;
- `holidays.go` no se refactoriza, y una prueba de ida y vuelta mantiene alineadas las dos reglas;
- `shortName` pasa a B-24;
- la caché es de un día, y el ETag se recalcula si el fichero cambia;
- Navarra va en una tarea aparte.

## 8. Consultas al asesor
No hay consultas.

Hallazgos:
- [juez-plan] 1. (Bloqueante) El plan parte de un hecho falso sobre las provincias insulares. Lo he comprobado en la semilla (contexts/geography/infrastructure/seed/boundaries.csv.gz y associations.csv.gz) y en docs/GEOGRAFIA-NUTS.md §4.2 y su nota posterior. Las provincias 07, 35 y 38 SÍ existen: son de tipo Province y tienen `level` vacío a propósito. Las 10 islas cuelgan de ellas (Mallorca → 07, Tenerife → 38) y los 155 municipios insulares cuelgan de su isla. La partición NUTS 3 es «49 provincias + 10 islas». En total hay 52 provincias. Esto invalida varias partes del plan: (a) la frase «la semilla pone las islas en lugar de las provincias 07 y 35»; (b) el recuento de 50 provincias; (c) la lista `expected-unmatched` con 07 y 35 (y sin 38); (d) la opción D-c(ii) «añadir 07 y 35 a la semilla», que no cambia ningún dato porque ya están; (e) la tarea de T2 de «comprobar que no existen». Además, la regla y la tabla se contradicen: con «antecesor más cercano en la escalera», el `parentCode` de Mallorca sería `07`, no `ES-IB`. Y pintar 38 en el nivel 3 (por el `kind`) lo solapa con sus cuatro islas, que también están en el nivel 3. Debajo de 07, 35 y 38 el nivel `local` saldría vacío. Para aprobar, la escalera tiene que ser coherente. Por ejemplo: el `level` de la tesela es el de la semilla, y las provincias sin nivel quedan fuera de la escalera, como la agrupación de nivel 1 (`ok=false`). Así la isla sube a `ES-IB`/`ES-CN`, el municipio a su isla, y el recuento es de 49 provincias y 10 islas. Hay que corregir la tabla, los recuentos, `expected-unmatched` (07, 35 y 38), la tabla de pruebas de `BoundaryCode` (38 debe dar `ok=false`, no nivel 3) y reformular D-c con opciones reales. Por ejemplo: (i) islas en el nivel 3 con la clave NUTS como excepción; (ii) nivel 3 con las 52 provincias INE y las islas fuera del mapa. Ésta no exige tocar la semilla, pero sí el criterio de nivel. Hay que decir qué consecuencias tiene cada opción.
- [juez-plan] 2. (Bloqueante) La agregación del front tiene que casar con la tesela. `front/react/src/map/aggregate.ts` (`chainOf` y `keyOf(level, code)`) usa el `level` que devuelve la API, y el plan deriva el nivel de la tesela del `kind`, distinto del de la API (está declarado: «la API devuelve level tal cual»). Si los dos niveles difieren para alguna entidad, sus recuentos no se pintan. El plan tiene que garantizar que nivel de tesela = nivel de la API (lo resuelve la opción del hallazgo 1) o, si no, que la API devuelva también ese nivel y que el front lo use para la clave. Cualquiera de las dos, con su prueba en vitest y en Go.
- [juez-plan] 3. El cálculo de `parentCode` en la API no está diseñado. El servicio NO tiene el catálogo en memoria: `SearchBoundaries` hace un `FindPage` de hasta 1.000 elementos. La alternativa del plan, «una lectura por padre, que se mide en T2», es un N+1 de hasta tres saltos por elemento (municipio → isla → provincia sin nivel → comunidad). Para construir sin preguntar, el plan debe fijar la subida por lotes, nivel a nivel, con `Ports.boundaries` (por tramos de 500), como ya hace `Ports.CountryOf` en `contexts/geography/application/ports.go`. Así son como mucho tres consultas por página, y `GetBoundary` reutiliza los `Ancestors` que ya carga. También hay que nombrar la prueba que lo fija, por ejemplo con un repositorio en memoria que cuente las consultas.
- [juez-plan] 4. Falta justificar la ubicación de `BoundaryCode` y `MapKind`. Son una regla pura sobre el tipo, los códigos y el nivel de un límite: conocimiento del dominio de geografía, sin puertos, solo biblioteca estándar. El plan las pone en `contexts/geography/application` y sobre el DTO sin justificar la desviación. Para aprobar: o bien se ponen en `contexts/geography/domain` (sobre `*domain.Boundary` o un valor pequeño con tipo, códigos y nivel, usando `TypeCountry`, `TypeAutonomous`, `TypeProvince`, `TypeIsland` y `TypeMunicipality`, que ya existen en `domain/geography.go`) y la aplicación solo las llama al construir el DTO; o bien se justifica por complejidad innecesaria. Si va al dominio, la mutación de `./contexts/geography/domain` deja de ser «sólo se mide».
- [juez-plan] Menores (se corrigen en la misma vuelta): (a) `parentCandidates` está en `src/map/nav.ts`, no en `sources.ts` (ahí está `resolveTrail`, que la usa), y su prueba está en `nav.test.ts:74`. (b) `text()` de `src/map/geo.ts:139` ya tolera `undefined` y `labels.ts:110` ya cae al nombre, así que el cambio por `shortName` se reduce a una prueba. (c) `pkg/distribution/server.go:22` fija `WriteTimeout` en 30 s: un GET completo de un `.pmtiles` grande puede cortarse. Hay que anotarlo o ampliar el plazo para este manejador con `http.ResponseController.SetWriteDeadline`, con su prueba. (d) Hay que decir que `cmd/geokeys` va en la raíz (`./cmd/geokeys`, como lo invoca `build.sh`) y de dónde lee la semilla (paquete `infrastructure` o los CSV comprimidos directamente).

## Directrices del asesor para la tercera versión del plan (b2-mapas-pmtiles)

**Recomendación en una frase:** el plan aprueba si adopta una sola regla, «el nivel de la tesela es el `level` de la semilla, y está en la escalera lo que tiene nivel `country`, `2`, `3` o `local`», y deriva de ella la tabla, los recuentos, `expected-unmatched`, las pruebas y D-c. Los cuatro hallazgos del juez son correctos; los he comprobado en la semilla.

### Hechos comprobados (seguros)

Leídos de `contexts/geography/infrastructure/seed/boundaries.csv.gz` y `associations.csv.gz`:

| Tipo | Nº | `level` | Padre (containment) |
|---|---|---|---|
| País | 248 | `country` | — |
| Agrupación NUTS 1 | 7 | `1` | España |
| Comunidad o ciudad autónoma | 19 | `2` | su NUTS 1 |
| Provincia con nivel | 49 | `3` | su comunidad |
| Provincia sin nivel (07, 35, 38) | 3 | vacío | su comunidad |
| Isla (ES531…ES709) | 10 | `3` | su provincia (Mallorca → 07, Tenerife → 38) |
| Municipio | 8.132 | `local` | provincia o isla (07040 → Mallorca, 38038 → Tenerife, 51001 → provincia 51) |

- Ningún límite tiene más de un padre de contención.
- Las abreviaturas de las 19 comunidades coinciden con ISO 3166-2 salvo Navarra (`NA`; ISO dice `NC`).
- Cuatro nombres llevan coma entre comillas («Balears, Illes», «Palmas, Las», «Coruña, A», «Rioja, La»). Quien parta el CSV por comas contará mal; hay que usar `encoding/csv` o, mejor, `infrastructure.LoadSeed()`.
- `contracts.BoundaryRef` sólo trae `ID`, `Name`, `Type` y `Active`. Los `Ancestors` de `GetBoundary` no llevan códigos ni nivel, así que no sirven tal cual para calcular `parentCode`.
- `contexts/geography/application` no tiene pruebas propias: las del contexto están en `contexts/geography/*_test.go` y cargan la semilla en memoria con `infrastructure.LoadMemory`.

### Hallazgo 1: escalera coherente

1. **Regla única.** Un límite está en la escalera si su `Position().Level` es `country`, `2`, `3` o `local` y su tipo tiene regla de clave. Quedan fuera el nivel `1`, los límites sin nivel (las provincias 07, 35 y 38, continentes, uniones) y los tipos desconocidos: `ok=false`, `code` vacío.
2. **Se elimina** «el `level` de la tesela sale del `kind`». El `kind` es sólo una propiedad informativa.
3. **Tabla corregida:**

| Tipo | `level` | `code` | `parentCode` |
|---|---|---|---|
| País | `country` | alfa-2 | — |
| Comunidad (19) | `2` | `ES-` + abreviatura | `ES` (salta el nivel 1) |
| Provincia (49) | `3` | INE de 2 cifras | su comunidad |
| Isla (10) | `3` | según D-c | `ES-IB` o `ES-CN` (salta la provincia sin nivel) |
| Municipio (8.132) | `local` | INE de 5 cifras | su provincia o su isla |
| NUTS 1 (7) y provincias 07, 35, 38 | — | vacío | — |

4. **`parentCode`** es el `code` del primer antecesor con `ok=true`. Con esta regla la tabla y la definición ya no se contradicen.
5. **Recuentos:** 248 países (menos los que Natural Earth no traiga), 19, 49, 10 y 8.132.
6. **`expected-unmatched.txt`:** los recintos provinciales 07, 35 y 38 de BDLJE, más los territorios sin municipio que aparezcan en T4. El plan debe decir que la lista es de claves BDLJE y que cualquier diferencia, de más o de menos, hace fallar la tubería.
7. **T2:** se quita «comprobar que no existen 07 y 35». En su lugar, una prueba de integridad fija «3 provincias sin nivel, cada una con al menos una isla hija, y ningún municipio cuelga directamente de ellas».
8. **Tabla de pruebas de `BoundaryCode`:** 38 da `ok=false`; Tenerife da nivel `3` con padre `ES-CN`; 38038 tiene padre Tenerife; 07040 tiene padre Mallorca.

**D-c reformulada, con opciones reales:**

- **(i) Nivel 3 = 49 provincias + 10 islas (lo que dice la semilla).** La clave de la isla es su código NUTS (`ES532`), como excepción documentada a «`code` nunca en NUTS».
  - No toca datos y es reversible regenerando.
  - API y tesela casan sin más.
  - `Locate` de festivos no resuelve islas, igual que hoy.
- **(ii) Nivel 3 = 52 provincias INE, islas fuera del mapa.** Todas las claves son INE puras.
  - Exige que 07, 35 y 38 pasen a nivel `3` y las islas lo pierdan. O se cambia la semilla, contra `docs/GEOGRAFIA-NUTS.md` §4.2 y con traslado a Trench y a Karpo C#, o la API expone un «nivel de mapa» distinto del `level`.
  - Los 155 municipios insulares suben a la provincia y se pierde la isla como unidad de análisis.

Propuesta: (i) provisional. Es mi opinión, y coincide con el ejemplo que el propio juez da.

Nota sin verificar: el INE publica códigos de isla propios, pero separa Eivissa y Formentera, que la semilla une en ES531. No casarían uno a uno, y por eso no los propongo. Si el planificador quiere citarlo, que lo confirme en T1.

### Hallazgo 2: nivel de la tesela = nivel de la API

Con la regla anterior se cumple por construcción. El plan debe decirlo y fijarlo con dos pruebas:

- **Go:** para toda entidad de la semilla con `ok=true`, el `level` de `keys.csv` es igual a `BoundaryDTO.Level`.
- **vitest (`aggregate.test.ts`):** `chainOf` de 07040 con antecesores [Mallorca, provincia 07 sin nivel, Illes Balears, ES5, España] da `(local,07040)`, `(3,ES532)`, `(2,ES-IB)` y `(country,ES)`. La provincia 07 se omite porque no tiene nivel.

Un detalle del front: `boundaryCode()` con respaldo a `nutsCode` hará «colocable» al NUTS 1 (`level=1`, `ES5`). Es inocuo, porque ninguna vista usa `boundaryLevel: "1"`. El plan debe elegir entre dejarlo anotado o limitar el respaldo a cuando la respuesta no trae `code`, y fijar lo elegido en la prueba.

### Hallazgo 3: `parentCode` por lotes

El plan debe fijar este diseño:

- Un ayudante en `contexts/geography/application`, por ejemplo `mapParents(ctx, p Ports, bs []*domain.Boundary) (map[domain.BoundaryID]string, error)`.
- Funciona por rondas, con el mismo patrón que `Ports.CountryOf`:
  1. reúne los padres pendientes sin repetir;
  2. los carga con `p.boundaries` (tramos de 500);
  3. a cada pendiente cuyo padre tiene `ok=true` le asigna el `code`; los demás pasan al padre del padre.
- Tope de `MaxDepth` rondas. Con la semilla actual son dos como mucho: isla → 07 → comunidad, y comunidad → NUTS 1 → país.
- `SearchBoundaries` lo llama una vez por página.
- `GetBoundary` lo llama con una lista de un elemento. No se amplía `contracts.BoundaryRef` ni se reutilizan los `Ancestors`: es un contrato para otros contextos y no trae códigos.

Prueba nombrada: `TestSearchBoundaries_ParentCodesInBatches`, con un `domain.BoundaryRepository` envoltorio que cuenta las llamadas a `Find`. Una página con municipios insulares, islas, provincias y comunidades (menos de 500 padres distintos) hace como mucho 2 llamadas a `Find` además del `FindPage`, y nunca una por elemento.

### Hallazgo 4: la regla va al dominio

Recomiendo ponerla en `contexts/geography/domain/mapcode.go`:

- Una función pura sobre `*Boundary`, por ejemplo `func (b *Boundary) MapCode() (code, kind string, ok bool)`. Usa `Type()`, `Codes()` y `Position()` con `TypeCountry`, `TypeAutonomous`, `TypeProvince`, `TypeIsland` y `TypeMunicipality`.
- Sólo biblioteca estándar. Es vocabulario del dominio (qué clave pública tiene un límite) y no necesita puertos.
- La aplicación se queda sólo con la subida (`mapParents`) y con rellenar el DTO.
- La mutación de `./contexts/geography/domain` pasa a ser exigida, no «sólo se mide».
- La tabla de pruebas de `MapCode` va en `contexts/geography/domain`.

### Menores

- **(a)** `parentCandidates` está en `front/react/src/map/nav.ts:69`, con su prueba en `nav.test.ts:74`. `resolveTrail` está en `sources.ts`. El plan debe decir de dónde saca el padre `resolveTrail`: de la propiedad `parentCode` de la entidad cargada, con el recorte NUTS sólo como respaldo de la demo.
- **(b)** `shortName` se reduce a una prueba de que el rótulo cae al nombre. No hay cambio de código en `geo.ts`.
- **(c)** `pkg/distribution/server.go:22` fija `WriteTimeout` en 30 s. Recomiendo que `StaticFile` amplíe el plazo sólo en su manejador con `http.NewResponseController(w).SetWriteDeadline(...)`.
  - Prueba: con un `ResponseWriter` que registra `SetWriteDeadline`, el manejador lo llama.
  - El cliente PMTiles sólo pide rangos pequeños, así que el GET completo es un caso raro. Si se prefiere no tocarlo, hay que anotarlo como límite conocido en `docs/ARQUITECTURA.md`. Una de las dos cosas, dicha de forma explícita.
- **(d)** `cmd/geokeys` vive en `./cmd/geokeys`, en la raíz, y se invoca con `go run ./cmd/geokeys`. Lee la semilla con `infrastructure.LoadSeed()` o `LoadMemory`, que ya existen. No vuelve a partir los CSV.
- **(e)** El enlace con BDLJE por `NATCODE` debe decir qué cifras son el INE. Creo que son las 5 últimas de 11, pero no lo he verificado: se confirma en T1 y se fija en una prueba de `join` con un recinto real.

### Para que el juez no encuentre un tercer motivo

- La tabla «Contextos afectados» debe reflejar el cambio en `domain` (un fichero nuevo, sin cambiar el modelo ni el esquema).
- En «Decisiones técnicas que se cuentan» hay que quitar «el nivel sale del `kind`» y añadir «las provincias 07, 35 y 38 quedan fuera de la escalera, como el nivel 1».
- La sección 6 debe recoger las pruebas nuevas: lotes, nivel de API = nivel de tesela, `chainOf` insular, plazo de escritura y las tres provincias sin nivel.
- En T2, el modelo y esfuerzo se justifican ahora por la regla de dominio con mutación exigida y la subida por lotes.

### Rutas consultadas

- `C:/Git/Paranoia/karpo-fw-go-pmtiles/contexts/geography/application/ports.go` (`boundaries`, `CountryOf`, `Ancestors`)
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/contexts/geography/application/service.go` (`BoundaryDTO`, `boundaryDTO`, `SearchBoundaries` con `FindPage`, `GetBoundary`)
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/contexts/geography/application/holidays.go` (`Locate`)
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/contexts/geography/domain/geography.go` (tipos y niveles)
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/contexts/geography/infrastructure/seed.go` (`LoadSeed`) y `persistence.go` (`LoadMemory`)
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/front/react/src/map/aggregate.ts`, `nav.ts`, `sources.ts`
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/front/react/demo/config/manifest.json`
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/pkg/distribution/server.go`, `middleware.go`

## constructor

- La generación hasta z12 con la geometría completa de la BDDAE tarda horas (se paró al 78 % tras unos 80 min). ¿Se acepta un zoom máximo menor para el artefacto de despliegue (z10, por ejemplo), simplificar antes con GDAL o mapshaper, o se deja la generación a z12 como tarea larga fuera de la sesión?
- La BDDAE se lee del WFS INSPIRE del IGN en vez de los shapefiles del CNIG, cuyo servidor cortó las conexiones. ¿Basta con registrar su huella en cada ejecución (out/ign.sha256), dado que no se puede fijar porque el servicio se actualiza cada trimestre?

# Respuesta del asesor — b2-mapas-pmtiles (consulta de constructor)

## 1. Zoom máximo del artefacto

**Recomendación: el artefacto de despliegue se genera a z8 (lo que ya hace `MAXZOOM=8`, 21 min, 15,2 MB). No se genera a z12, ni en la sesión ni como tarea larga, y no se añade simplificación previa con GDAL o mapshaper.** Conviene cambiar el valor por defecto de `build.sh` de 12 a 8 y anotarlo como desviación del plan (el plan decía `-Z0 -z12`).

Hechos comprobados en el repositorio:
- El front fija `maxzoom: 8` en la fuente vectorial (`front/react/src/map/use-map-level.ts:92`; lo confirman `style.test.ts:96` y `pmtiles-file.test.ts:144`). MapLibre no pide teselas por encima del `maxzoom` de la fuente: amplía las de z8. Las teselas z9–z12 no las leería nadie.
- La cámara no pasa de z10 al encuadrar (`FIT_MAX_ZOOM = 10` en `MapCanvas.tsx:63`), es decir, dos niveles de ampliación sobre z8.
- El generador de demostración del front también escribe un único `MAX_ZOOM` en la cabecera (`front/react/scripts/build-pmtiles.mjs`); el contrato no exige z12 en ningún sitio (B-2 y B-26 piden «cada nivel ya presente en los zooms bajos», no un zoom alto).
- Ya existe `../geo-downloads/out/karpo.pmtiles` de 15.217.066 B generado a z8.

Hechos generales (tippecanoe / MapLibre):
- Una tesela vectorial tiene 4096 unidades de lado. A z8 y 40° N mide unos 120 km, o sea unos 29 m por unidad; a z10 un píxel de pantalla son unos 58 m. La precisión de z8 queda por debajo del píxel hasta el zoom máximo del front.
- El coste por zoom se multiplica por 4, y `--detect-shared-borders` es de las opciones más lentas de tippecanoe.

Opinión (no medido):
- La mayor parte de las horas de z12 se va probablemente en teselar los países de Natural Earth sobre todo el planeta (millones de teselas de relleno), no en la BDDAE. Simplificar la BDDAE antes no lo arreglaría, y mapshaper añadiría una herramienta y un paso más sin beneficio visible. La decisión del plan «sólo tippecanoe; mapshaper sólo si aparecen huecos» sigue valiendo.
- Si algún día el front sube `maxzoom` o `FIT_MAX_ZOOM` (callejero, zoom de barrio), z10 es el siguiente escalón razonable (unos 7 m por unidad, la precisión nominal de la BDDAE). Hasta entonces es coste sin uso. Es reversible: `MAXZOOM=10 tools/geo/build.sh`.

Qué debe dejar escrito el constructor:
- `docs/GEOGRAFIA.md` y `tools/geo/SOURCES.md`: zoom 0–8, por qué (el front fija 8 y amplía), tiempo y tamaño medidos, y que el `maxzoom` de la cabecera del fichero y el del front deben coincidir (si el fichero tuviera menos que 8, el front pediría teselas que no existen).
- Una comprobación en la verificación (paso 8 o `pmtiles_geo_test.go`): que la cabecera diga `maxzoom = 8` y que a z0 estén los municipios (D-b i), con el tamaño de z0 anotado (`z0.pmtiles` pesa 357.894 B: es el dato que D-b pedía medir).
- No es una decisión para Javier: es técnica y reversible. Se le cuenta.

## 2. BDDAE por WFS: ¿basta con registrar la huella?

**Recomendación: sí, registrar en vez de fijar es lo correcto para una fuente que cambia cada trimestre, pero la huella sola no basta. Lo que protege de verdad ya existe (la lista cerrada de `expected-unmatched.txt` y el error por «clave española sin geometría» en `tools/geo/join/main.go:251-268`). Faltan cuatro cosas pequeñas.**

Hechos comprobados en `tools/geo/build.sh`:
1. **La fecha de la atribución puede ser falsa.** El script no vuelve a descargar si ya hay `ign/au-*.json`, pero escribe la atribución con `date +%Y-%m` del día de la generación. Una generación en febrero con páginas de noviembre diría «BDDAE 2027-02». La licencia CC-BY 4.0 y Q-8 piden la fecha de los datos. Hay que guardar la fecha de descarga junto a las páginas (un fichero de fecha en `ign/` escrito al descargar) y usar esa en `--attribution` y en `ign.sha256`.
2. **Un fallo del servicio se confunde con el final de la paginación.** El bucle termina cuando una página no contiene `"Feature"`. Una respuesta 200 con un informe de excepción del WFS, o una página cortada, también corta el bucle sin error. Hoy lo acabaría detectando `join` (faltarían claves), pero con un mensaje que no dice la causa. Conviene comprobar el número de unidades: el total descargado contra `numberMatched` de la primera página (hoy 8.294) y fallar si no coincide.
3. **Una descarga interrumpida deja páginas a medias que la siguiente ejecución da por buenas** (la condición es «existe algún `au-*.json`»). Descargar a una carpeta temporal y renombrarla al terminar bien lo evita.
4. **La huella debe viajar con el artefacto, no quedarse sólo en `out/`.** Propongo que la verificación escriba junto a `karpo.pmtiles` un registro de procedencia (fecha de descarga, número de unidades, huella de cada página, huella del `.pmtiles`, versiones de las imágenes) y que `SOURCES.md` recoja el de la generación que se despliega. Así se puede decir de qué datos sale el fichero servido en `/geo/karpo.pmtiles`.

Opinión:
- Reproducibilidad exacta no se puede prometer con un WFS vivo, y tampoco la da tippecanoe byte a byte (ya se dijo en `consultas.md`). Lo reproducible es el **artefacto**: el `.pmtiles` generado es lo que se despliega y se conserva. Si se quiere poder regenerar exactamente, CC-BY 4.0 permite guardar la instantánea de las 9 páginas (752 MB, mucho menos comprimida) fuera del repositorio; lo veo opcional y lo dejaría en `BACKLOG.md`, no en esta tarea.
- El cambio de fuente (WFS INSPIRE del IGN en lugar de los shapefiles del CNIG) es la misma base, misma licencia y mismo organismo: es una desviación técnica bien documentada en `SOURCES.md`, no una decisión de producto. Se le cuenta a Javier, no se le pregunta.
- Cuando el IGN publique un trimestre nuevo y cambien municipios (fusiones, segregaciones), `join` fallará por la lista cerrada. Es el comportamiento deseado: obliga a revisar la semilla y `expected-unmatched.txt`. Merece una frase en `docs/GEOGRAFIA.md` («qué hacer si falla tras una actualización del IGN»).

## Referencias
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/tools/geo/build.sh` (pasos 2, 7 y 8)
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/tools/geo/SOURCES.md`
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/tools/geo/join/main.go` (líneas 95-111 y 251-268)
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/front/react/src/map/use-map-level.ts` (línea 92), `MapCanvas.tsx` (línea 63), `style.ts` (línea 151)
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/docs/trabajo/b2-mapas-pmtiles/01-analisis.md` (D-b) y `02-plan-parado.md` (paso 7)
- Documentación de tippecanoe (github.com/felt/tippecanoe, opciones `-z`, `--detect-shared-borders`) y de MapLibre GL JS (`maxzoom` de una fuente vectorial: las teselas se amplían por encima).

No he ejecutado nada: sólo he leído el repositorio y la carpeta `../geo-downloads/out`. Los tiempos (21 min a z8, parada al 78 % a z12) son los que da el constructor.

## constructor

- ¿Vale documentar que la API da code a un límite inactivo, en vez de filtrarlo como la tesela?

## Consulta: ¿vale documentar que la API da `code` a un límite inactivo, en vez de filtrarlo como la tesela?

**Recomendación: sí, vale documentarlo y no filtrar en la API.** Con dos condiciones para que no sea una frase sin respaldo: una prueba que fije cada mitad de la regla y una comprobación en la tubería para el caso «hijo activo con padre inactivo».

### Lo que sé con seguridad (leído en el repositorio)

- **Hoy es un caso hipotético.** La semilla (`contexts/geography/infrastructure/seed/boundaries.csv.gz`) tiene 8.507 límites y ninguno inactivo. En `contexts/geography` no hay ningún caso de uso que desactive un límite (no existe `Deactivate` ni nada parecido). Sólo se llega a un inactivo cambiando la semilla o la base.
- **La asimetría ya está escrita y es coherente con el código:**
  - `docs/GEOGRAFIA.md:206-208` lo dice.
  - `boundaryDTO` (`application/service.go:162-175`) no mira `IsActive()` para dar `code`.
  - `infrastructure.MapKeys` (`mapkeys.go:56`) sí lo filtra.
- **El DTO ya lleva `active`** (`service.go:83`), así que el cliente puede distinguir sin pedir nada más.
- **`domain.MapParents` no mira `IsActive()`** (`domain/mapcode.go:71-115`), y la tesela usa esa misma función. Si un límite activo cuelga de un padre inactivo, `keys.csv` saldría con un `parentCode` que no está en la tabla. Hoy no ocurre. `TestMapKeys_LadderIsComplete` lo detectaría, pero sólo sobre la semilla empaquetada, no al generar.
- **La rama `!b.IsActive()` de `MapKeys` no la ejerce ninguna prueba**, porque la semilla no trae inactivos. Tampoco hay prueba que fije que la API da `code` a un inactivo. La frase del documento no tiene hoy ninguna prueba detrás.

### Por qué no filtrar en la API (opinión, razonada)

- **`code` es identidad, no presencia en el mapa.** `MapCode()` es una función pura del dominio sobre tipo, nivel y códigos. Meterle `active` mezcla dos preguntas distintas: «qué clave tiene» y «se pinta».
- **Los datos antiguos siguen apuntando a límites inactivos** (direcciones, festivos). Sin `code`, el front no podría agregarlos ni subirlos por `parentCode`, y un inactivo sería indistinguible de un NUTS 1 (ambos sin `code`).
- **Es coherente con el resto de la API:** `GET /boundaries` devuelve inactivos con `active:false`; no los oculta.
- **Filtrar en la tesela es lo correcto:** la tesela es lo que se pinta y se regenera; la API es el registro.

### Condiciones que pondría al constructor

1. **Fijar las dos mitades con prueba:**
   - En `contexts/geography/domain` (`mapcode_test.go`): un límite reconstituido con `active=false` da el mismo `MapCode()` que activo. Es la que da respaldo a la frase y cuenta para la mutación exigida.
   - En `infrastructure` (`mapkeys_test.go`): un `Seed` sintético con un municipio inactivo; `MapKeys` no lo incluye. Sin esto, quitar `|| !b.IsActive()` no rompe ninguna prueba.
2. **Padre inactivo en la tesela: fallar, no subir.** Que `MapKeys` devuelva error si un `parentCode` no vacío no está entre las claves emitidas. Es una comprobación de tres líneas, sin lógica de subida nueva. Subir al siguiente antecesor activo sería más código para un caso que no existe (regla de sencillez de `CLAUDE.md`). Con el error, quien desactive una provincia con municipios activos se entera al regenerar.
3. **Completar el párrafo de `docs/GEOGRAFIA.md` con dos frases:**
   - el cliente distingue por `active`, y un `code` de un inactivo no tiene geometría en la tesela;
   - `parentCode` puede apuntar a un inactivo en la API, y en la tesela eso hace fallar la generación.

### Lo que no he verificado

- **Qué hace el front con un dato cuyo `code` no está en la tesela** (si lo cuenta como «sin colocar» o lo pierde en silencio). `front/react/src/map/aggregate.ts` agrega por `keyOf(level, code)` y no mira `active`. Si la tarea toca el front, conviene una prueba de vitest de ese caso; si no, anotarlo como límite conocido.
- **Unicidad de `code` entre un inactivo y su sustituto activo.** Si algún día se desactiva un límite y se crea otro con el mismo INE, la API daría dos límites con la misma clave. La tesela no se ve afectada (sólo lleva el activo). No pediría código para esto; basta una línea en el documento: «la clave es única entre los activos».

### Rutas consultadas

- `C:/Git/Paranoia/karpo-fw-go-pmtiles/docs/GEOGRAFIA.md` (líneas 198-222)
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/contexts/geography/domain/mapcode.go`
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/contexts/geography/application/service.go`
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/contexts/geography/infrastructure/mapkeys.go`
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/contexts/geography/infrastructure/mapkeys_test.go`
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/contexts/geography/infrastructure/seed/boundaries.csv.gz`
- `C:/Git/Paranoia/karpo-fw-go-pmtiles/front/react/src/map/aggregate.ts`
