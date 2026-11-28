# Análisis: b2-mapas-pmtiles (B-2 y B-26). Tercera versión, con los hallazgos de los jueces y las respuestas del asesor

Carpeta: `C:/Git/Paranoia/karpo-fw-go-pmtiles` (rama `feat/mapas-pmtiles`).

## 1. Situación de partida

### Qué piden el front y Javier (hechos)
- **B-2 (P0)** (`front/react/docs/PETICIONES-BACKEND.md:16`) pide la geometría de cada delimitación, por `GET /api/geography/geometries` o en un `.pmtiles` que aloje el despliegue.
  - Propiedades: `code` y `lauCode`. `code` va en ISO 3166-2 o INE, nunca en NUTS.
  - Cada nivel de detalle tiene que estar **ya en los zooms bajos**, con sus centroides.
- **B-26 (P1)** (línea 40) pide servir el `.pmtiles` como fichero estático con soporte de `Range`.
  - Cabeceras: `206`, `Accept-Ranges`, `Content-Length`, `ETag`, CORS y caché larga. Ruta de ejemplo: `/geo/karpo.pmtiles`.
  - Propiedades: `code`, `name`, `shortName`, `level`, `parentCode` y `lauCode`.
- **Q-8, que Javier decidió el 2026-11-27** (`front/react/docs/CONSULTAS-JAVIER.md:106-125`), fija las fuentes:
  - Natural Earth para el mundo y los países.
  - OSM vía Protomaps para regiones y ciudades.
  - IGN/CNIG para comunidades, provincias y municipios de España.
  - Se descartan GADM, GISCO y geoBoundaries.
  - Las claves pasan de NUTS a ISO 3166-2 (regiones) y a INE (provincias y municipios).
  - Atribuciones: «© OpenStreetMap contributors» y «BDLJE CC-BY 4.0 ign.es». Si los datos se modifican, se pone «Obra derivada de BDLJE <fecha> CC-BY 4.0 ign.es».

### Qué hay en el código (hechos)
- **El backend no tiene geometría.**
  - La semilla es `contexts/geography/infrastructure/seed/boundaries.csv.gz`, con las columnas `id,name,type,geo_code,abbreviation,ine_code,nuts_code,lau_code,active,country,level`.
  - Tiene 248 países. Por debajo del país sólo hay España: 7 agrupaciones de nivel 1, 19 comunidades y ciudades autónomas, 52 provincias, 10 islas y 8.132 municipios (`docs/GEOGRAFIA-NUTS.md` §2 y §5).
- **Navarra en la semilla:**
  - comunidad: `geo_code=ES22`, `abbreviation=NA`, `level=2`;
  - provincia: `geo_code=31`, `ine_code=31`, `nuts_code=ES220`, `level=3`.
- **Regla ISO actual.** `contexts/geography/application/holidays.go:137-144` resuelve `ES-XX` así:
  - niveles 1 y 2: país más abreviatura;
  - provincia: `type=Province` y `geo_code` de 2 cifras;
  - municipio: `type=Municipality` y `geo_code` de 5 cifras.
  - Con esta regla, hoy `ES-NA` devuelve la comunidad de Navarra y `ES-NC` no devuelve nada. Ninguna prueba usa `ES-NA`: `holidays_test.go` sólo prueba `ES-MD` y `ES-CT`.
- **API.** La ruta `GET /api/geography/boundaries?type=&parent=&grouping=&country=&level=` se registra en `contexts/geography/distribution/http.go:24`. El manejador está en las líneas 36-37 y el filtro en `application/service.go:190-216`. Las provincias de una comunidad se piden con `type` y `parent`.
- **Provincias 07, 35 y 38.** Tienen `ine_code`, pero ni `nuts_code` ni `level`, porque su nivel 3 son las islas. La fuente es el asesor; yo no lo he comprobado fila a fila.
- **CORS.** `pkg/distribution/middleware.go:209-212` ya admite `Range` y expone `Content-Range` y `ETag`.
- **Rutas públicas.** Se registran en `host/host.go:322` (`RegisterPublicRoutes`). No hay middleware de compresión.
- **Front.**
  - `front/react/src/map/aggregate.ts:51` enlaza por `nutsCode || lauCode || ineCode || geoCode`.
  - `scripts/build-pmtiles.mjs` sólo genera la demo esquemática.
  - Si falta `shortName`, el front lo sustituye: `labels.ts:93-110` prueba el nombre, luego `shortName` y luego el código (`notas/R2-R3.md:72`).
- **Nombre corto.** No existe `shortName` ni `short_name` en ningún `.go` ni `.sql`, y `geo_boundaries` (`persistence.go:24`) no tiene esa columna. Sólo aparece en la demo (`SHORT_NAMES` en `demo/data/spain.mjs`).
- **Decisiones comunes.** D-14 (NUTS para traducir niveles) y D-26 (provincia e isla son delimitaciones distintas) siguen vigentes, porque NUTS no sale del modelo. Ninguna decisión común trata de geometría.

### Karpo C# y Trench (comprobado)
- **Comprobación del juez.** Ejecutó `git grep -l -i -E "pmtiles|geojson|geometr"`:
  - en `C:/Git/Paranoia/Karpo`, sobre `*.cs`, `*.sql`, `*.md` y `*.json`;
  - en el repositorio de Trench, sobre `*.cs`, `*.sql` y `*.csproj`, buscando también `SqlGeography` y `NetTopologySuite`.
- **Resultado:**
  - Karpo C#: sólo aparece `000-Docs/design/Karpo_Theros_Tecnologia_Front.md:120`, que menciona SVG en el front. No hay geometría de delimitaciones.
  - Trench: ningún resultado.
  - Ninguno de los dos sirve teselas ni guarda geometría. Esta entrega no copia nada de ellos ni tiene que trasladarles nada.
- **Navarra en Trench.** `050DBs/subdomain/oracle/076-parties-geographic-es.opendata.sql:37` define «Comunidad Foral de Navarra» con `GeoCode 'ES22'` y `Abbreviation 'NA'`. Usa el mismo id que Karpo Go: `b3100000-0002-0000-0002-000000000016`. El dato erróneo es, por tanto, común a los tres proyectos.

## 2. Fuentes y patrones

| Fuente o patrón | Referencia | Por qué encaja o no |
|---|---|---|
| PMTiles v3 servido con HTTP Range | github.com/protomaps/PMTiles | Encaja: es el formato que ya lee el front. Un solo fichero estático, sin servidor de teselas |
| `http.ServeContent` (biblioteca estándar) | Documentación de `net/http` | Encaja: gestiona Range, 206, 416, If-Range, If-None-Match y HEAD. El ETag hay que ponerlo antes de llamarla |
| Teselas públicas y cacheables | Prácticas de Protomaps y OpenFreeMap | Encaja: los datos son públicos, y si llevaran JWT no se podrían cachear |
| Natural Earth `ne_10m_admin_0_countries` | naturalearthdata.com, dominio público | Encaja para el mundo y los países: se enlaza por alfa-2 (`geo_code`) |
| IGN/CNIG BDLJE | Centro de Descargas del CNIG, CC BY 4.0 | Encaja para comunidades, provincias y municipios, que traen códigos INE |
| OSM vía Protomaps | docs.protomaps.com/basemaps/layers | No encaja para colorear regiones: la capa `boundaries` son líneas sin ISO 3166-2 (`kind`, `kind_detail`, `brk_a3`, `disputed`). Sólo serviría de fondo |
| tippecanoe (felt), versión fijada y en Docker | github.com/felt/tippecanoe, BSD-2 | Encaja: simplifica por zoom y escribe `.pmtiles`. Se ejecuta como herramienta, no entra en `go.mod` |
| GDAL `ogr2ogr` (`ghcr.io/osgeo/gdal`, MIT) | gdal.org | Encaja: pasa los shapefiles del IGN a GeoJSON WGS84 |
| mapshaper (`npx`, versión fijada, MPL-2.0) | github.com/mbloch/mapshaper | Encaja: simplifica conservando la topología |
| go-pmtiles CLI (BSD-3) | github.com/protomaps/go-pmtiles | Opcional: comprueba el fichero (`show` y `verify`) |
| Ampliar `build-pmtiles.mjs` | Propio | No encaja: habría que reescribir la simplificación y el recorte |
| Planetiler | github.com/onthegomap/planetiler | No encaja: es excesivo para este volumen |

### Propiedades de cada entidad (salen de la semilla; el esquema no cambia)

| Delimitación | `code` | `parentCode` | `name` | `level` |
|---|---|---|---|---|
| País | alfa-2 (`geo_code`) | — | `name` | `level` |
| Comunidad o ciudad autónoma | `country` + `-` + `abbreviation`, con la misma regla que `holidays.go` | `ES` | `name` | `level` |
| Provincia (las 52, filtradas por `type=Province`) | `ine_code` (2 cifras) | `ES-XX` de su comunidad | `name` | `level`; vacío en 07, 35 y 38 |
| Municipio | `ine_code` (5 cifras, igual a `lauCode`) | las 2 primeras cifras del INE | `name` | `level` |

- **`shortName`** no se emite, ni siquiera vacío. No existe en el modelo, y un nombre corto es un texto traducible: su sitio es B-24 (catálogos traducibles). Al implementar hay que comprobar que `text(props.shortName)` en `front/react/src/map/geo.ts:160` y `:208` soporta que falte.
  - Efecto: los rótulos largos caen al código.
  - En `PETICIONES-BACKEND.md`, B-26 quedará hecha salvo `shortName`, con un enlace a B-24.
  - Es una decisión técnica y reversible: se toma y se le cuenta a Javier.
- Islas y agrupaciones de nivel 1 quedan fuera del mapa en esta entrega.

## 3. Hechos, opiniones y decisiones

**Hechos** (además de los de §1):
- **Navarra en ISO 3166-2:ES.** Según la Online Browsing Platform de ISO (https://www.iso.org/obp/ui/#iso:code:3166:ES):
  - `ES-NC` es «Navarra, Comunidad Foral de», la comunidad autónoma;
  - `ES-NA` es «Navarra», la provincia.
  - La semilla, Trench y Karpo C# usan `NA` para la comunidad, así que el dato es erróneo. Las otras 18 abreviaturas de nivel 2 coinciden con ISO (según el asesor).
- Las provincias llevan el código INE como abreviatura. Por eso `ES-NA` no choca con ninguna otra clave del fichero.
- **Licencias:**
  - ODbL §4.6.b: se cumple publicando el método para rehacer la base derivada.
  - IGN: simplificar sus datos produce una obra derivada, así que hay que conservar su fecha.
- **Herramientas en la máquina:** Docker 29.9.2 y WSL Ubuntu. No hay tippecanoe, pmtiles, ogr2ogr ni mapshaper.

**Decisiones técnicas reversibles (se toman, se anotan y se le cuentan a Javier):**
1. **Ruta del fichero.**
   - Pública, sólo GET y HEAD, fuera de `/api` y junto a `RegisterPublicRoutes`.
   - Sirve un fichero fijo cuya ruta viene de una variable de entorno. Sin la variable, responde 404.
   - ETag fuerte calculado al arrancar, `Cache-Control: public` largo y nunca comprimida.
   - Es una pieza pequeña de `pkg/distribution`, sin agregado ni permiso. Se aparta de DDD porque el resto sería complejidad innecesaria, y así se le cuenta a Javier.
2. **`shortName` se omite** (ver §2).
3. **Navarra va en una tarea aparte, fuera de esta rama.**
   - Esta rama genera `code` desde la misma semilla que la API, sin excepción a mano. Así la tesela dice `ES-NA`, igual que la API, y los datos casan.
   - Al corregir la semilla y regenerar el fichero, saldrá `ES-NC` sin tocar nada más.
   - Se anota en `BACKLOG.md` como defecto conocido y en `PETICIONES-BACKEND.md`.
   - Se propone como decisión común para trasladar a Trench y a Karpo C#: la tabla de Trench está en `076-parties-geographic-es.opendata.sql:37`. Esa corrección cambia la ruta pública de festivos y necesita una migración de datos.
   - No hace falta que Javier lo decida, porque `CLAUDE.md` ya dice que un error de Trench se corrige aquí y se traslada. Basta con informarle.
4. **Prueba de claves.** En esta rama, una prueba comprueba que cada `code` y cada `parentCode` del fichero existen en la semilla. La regla de la clave se escribe en `docs/GEOGRAFIA.md`.
5. **Comprobación por contenido.** La salida de tippecanoe no tiene por qué ser idéntica byte a byte. Por eso no se compara el fichero entero: se comprueban las claves, el número de entidades y la atribución.

**Decisiones de producto que tiene que confirmar Javier:**
- **D-a. Sin OSM en esta entrega.** Q-8 asigna a OSM las regiones y ciudades. Hoy la semilla sólo tiene regiones de España, que ya cubre el IGN.
  - Propuesta: mundo y países con Natural Earth, y España con el IGN. Cada entidad sale de una sola fuente.
  - OSM se aplaza hasta que haya regiones de otros países. Entonces se usaría Geofabrik con `osmium` (ODbL), porque Protomaps no trae polígonos con código.
  - Es un recorte de alcance frente a una decisión de Javier, así que la confirma él.
- **D-b. Municipios en los zooms bajos.** B-2 pide cada nivel ya presente en los zooms bajos. Hay dos opciones:
  - **(i) Cumplirlo al pie de la letra:** los 8.132 municipios desde z0. Hay que desactivar los descartes de tippecanoe y las teselas pesan más.
  - **(ii) Cambiar el contrato con el front:** municipios desde un zoom medio y, junto al `.pmtiles`, un índice JSON con el código, la caja y el centroide de cada delimitación.
  - Propuesta: medir primero el tamaño real de z0 con (i). Si es aceptable, se queda (i). Si no, se lleva (ii) a Javier y al front con la cifra medida.

**Sin comprobar:**
1. Si la descarga del CNIG exige aceptar la licencia en un formulario. En ese caso Javier tendría que descargar a mano. También falta ver el sistema de referencia de Canarias.
2. La fecha y el SHA-256 de los datos. Se anotarán al descargar.
3. Si existe una imagen Docker oficial de tippecanoe y los nombres exactos de sus opciones (`--no-feature-limit`, `--no-tile-size-limit`, `--no-tiny-polygon-reduction`, `-r1`).
4. Que las provincias 07, 35 y 38 tengan `level` vacío (lo dice el asesor).
5. El tamaño real del `.pmtiles` y de z0.

**Registro de descargas (pendiente).** Todo va en una carpeta nueva fuera del repositorio y se trata como datos no fiables. De cada descarga se anotan el fichero, la URL, la licencia, el tamaño, el SHA-256 y la fecha:
- Natural Earth admin-0;
- BDLJE del CNIG;
- imagen de tippecanoe;
- imagen de GDAL;
- opcionalmente, el CLI de pmtiles.

`go.mod` no recibe ninguna dependencia nueva.

## 4. Consultas al asesor
No hay consultas nuevas. Las dos decisiones de producto (D-a y D-b) son para Javier.
