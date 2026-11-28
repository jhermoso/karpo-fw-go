3 vueltas sin aprobar

- [juez-plan] El backend se puede construir tal como está. He comprobado la semilla: 248 países; 19 comunidades de nivel 2; 49 provincias de nivel 3 y 3 sin nivel (07, 35 y 38); 10 islas de nivel 3; 8.132 municipios con INE de 5 cifras, igual a lauCode y a geo_code; y 155 municipios que cuelgan de una isla. Las migraciones seedLevels e islandMunicipalities dejan la base igual que LoadSeed. El patrón de rondas de CountryOf, el tramo de 500 y los envoltorios con Unwrap (observe.go:347) también existen, así que SetWriteDeadline llega al conn. La cabecera CORS (middleware.go:209) ya expone Content-Range y ETag. Los fallos están en la pieza del front: el diseño no basta para construirla sin preguntar.
- [juez-plan] 1. Contradicción entre omitempty y el respaldo NUTS (§1, §4.7 y §6). BoundaryDTO.Code lleva omitempty, así que en el NUTS 1 (ok=false) la API no envía `code`. Pero §4.7 dice que boundaryCode() recurre a nutsCode «sólo cuando la respuesta no trae code (la demo)». El front no puede distinguir «API real con ok=false» de «demo sin code». ES5 caería a nutsCode y sería colocable, justo lo contrario de lo que pide la prueba «El NUTS 1 de la API no es colocable». Para aprobar hace falta un criterio que se pueda construir. Lo más sencillo es migrar la demo a `code` (ver 2) y quitar el respaldo NUTS. Si se quiere conservarlo, hay que decir con qué señal se activa: por ejemplo, `isSchematicSource` o una marca en la configuración, nunca la ausencia de `code`.
- [juez-plan] 2. La demo queda incoherente. El plan sólo cambia demo/config/manifest.json (ES30 a ES-MD, ES300 a 28) y scripts/build-pmtiles.mjs, pero la fuente que usa el manifest es «demo-geojson». Esa fuente la genera scripts/gen-geo.mjs con datos de demo/data/spain.mjs y la escribe como demo/public/geo/region-ES30.geojson, city-ES300.geojson, index.json, etc., con code NUTS y parentCode NUTS. Además, la API simulada (demo/mocks/handlers/geography.ts y demo/data/dataset.mjs) sólo emite nutsCode. Si el manifest arranca en ES-MD, pedirá region-ES-MD.geojson, que no existe. También fallarán scripts/demo-config.test.mjs:89 (espera ['ES30','ES300']) y la captura scripts/capturas.mjs:201 (code=ES300). Hay unos 20 ficheros con ES30 o ES300 (sources.test, nav.test, aggregate.test, geo.test, style.test, MapView.test, pmtiles-file.test, demo-geo.test, demo-dataset.test, entity-api.test…). Para aprobar, el plan tiene que decir una de dos cosas: (a) la demo entera pasa a la escalera ISO/INE (gen-geo.mjs, spain.mjs, dataset.mjs, el handler de geografía, los ficheros regenerados en demo/public/geo, los nombres region-ES-MD y city-28, karpo-demo.pmtiles, capturas y pruebas); o (b) la demo sigue en NUTS y el manifest no cambia. Recomiendo (a), porque elimina el respaldo NUTS y el hallazgo 1.
- [juez-plan] 3. Las migas desde la URL no tienen un mecanismo definido. resolveTrail (sources.ts:66-98) necesita el código del padre para cargar el nivel donde buscar `code` antes de tener la entidad. Hoy lo deduce del texto NUTS con parentCandidates. Con ISO/INE el padre no se puede deducir del código: 28 da ES-MD, y 07040 da ES532, no 07. El plan dice que parentCandidates «usa la propiedad parentCode de la entidad cargada», pero cuando se entra por URL esa entidad aún no está cargada, y la firma actual es parentCandidates(code: string). Hay que concretar de dónde sale el padre. Algunas opciones: un índice code→parentCode en la demo (index.json ya existe) o en el despliegue; reglas deducibles (ES-XX da ES; 5 cifras dan las 2 primeras, salvo los municipios insulares); o una consulta a la API, que hoy no filtra por código. También hay que concretar cómo lo comprueba nav.test con '28' y '07040'. Además, loadFeaturesOf (use-map-level.ts:176) devuelve null para cualquier fuente que no sea geojson. Con el .pmtiles real, las migas por URL no se resuelven aunque se arregle lo anterior. El plan debe decir si esto entra en la tarea (B-2/B-26) o se aplaza y se apunta en PETICIONES-BACKEND.md.
- [juez-plan] Cosa menor, que no impide aprobar: para un país, mapParents sube por subcontinente y continente sin encontrar nada que tenga ok=true. Bastaría con no subir desde un TypeCountry (por la tabla, un país no tiene parentCode) y se ahorrarían hasta dos consultas por página de países. La redacción de §6 «Tenerife → ES + NUTS de isla» debería decir «Tenerife → ES709».

---

# Plan: b2-mapas-pmtiles (B-2 y B-26). Tercera versión, con los hallazgos del juez y las directrices del asesor

Carpeta: `C:/Git/Paranoia/karpo-fw-go-pmtiles`, rama `feat/mapas-pmtiles`. Parte del análisis aprobado (tercera versión).

**Regla única que gobierna el plan:** el nivel de la tesela es el `level` de la semilla. Está en la escalera del mapa todo límite cuyo `Position().Level` sea `country`, `2`, `3` o `local` y cuyo tipo tenga regla de clave. De esta regla salen la tabla, los recuentos, `expected-unmatched`, las pruebas y D-c.

## 0. Resumen
- **Cuatro piezas:**
  1. **Regla de claves en el dominio de geografía.** `(*Boundary).MapCode()`, en `contexts/geography/domain/mapcode.go`. La aplicación sube a `parentCode` por lotes (`mapParents`), y la API devuelve `code` y `parentCode`.
  2. **Tubería reproducible** (script y contenedores) que genera `karpo.pmtiles` a partir de la semilla, Natural Earth y BDLJE.
  3. **Servidor estático mínimo** `StaticFile` en `pkg/distribution`, montado en `host/serve`.
  4. **Front:** enlaza por `code`, y las migas usan `parentCode`.
- **Fuera de esta rama:** OSM (D-a), `shortName` (pasa a B-24) y la corrección de Navarra (tarea aparte).
- `go.mod` no recibe ninguna dependencia nueva.

## 1. Contextos delimitados afectados
| Contexto o capa | Cambio |
|---|---|
| `contexts/geography/domain` | **Un fichero nuevo:** `mapcode.go`, con `func (b *Boundary) MapCode() (code, kind string, ok bool)`. Es una función pura y sólo usa la biblioteca estándar. **No cambian ni el modelo ni el esquema** |
| `contexts/geography/application` | `mapparents.go`, nuevo, con `mapParents`. `BoundaryDTO` gana `Code` y `ParentCode` (en JSON, `code` y `parentCode`, con `omitempty`). `SearchBoundaries` y `GetBoundary` los rellenan. **El contrato de `GET /api/geography/boundaries` (y del GET de un límite) se amplía de forma compatible.** `holidays.go` no cambia |
| `cmd/geokeys` (raíz) | Nuevo. Escribe `keys.csv` desde `infrastructure.LoadSeed()` y `MapCode` |
| `pkg/distribution` | `StaticFile` nuevo: Range, ETag que se recalcula si el fichero cambia y un plazo de escritura ampliado sólo en su manejador |
| `host/serve` | Variable `KARPO_PMTILES` y ruta `/geo/karpo.pmtiles`, montada fuera de la autenticación |
| `front/react` | `src/map/aggregate.ts`, `src/map/nav.ts` (`parentCandidates`), `src/map/sources.ts` (`resolveTrail`), `demo/config/manifest.json`, `scripts/build-pmtiles.mjs`, pruebas vitest y `docs/PETICIONES-BACKEND.md` |
| `tools/geo/` | Nuevo: `build.sh`, `join` (Go, sólo biblioteca estándar), `SOURCES.md` y `expected-unmatched.txt` |
| Documentación | `docs/GEOGRAFIA.md` (la regla de claves y la escalera), `docs/ARQUITECTURA.md` (`KARPO_PMTILES`) y `BACKLOG.md` |
| Resto de contextos | No cambian |

**Consecuencias en tres niveles:**
1. **Contexto.** La geografía es la única dueña de las claves públicas, y la regla vive en su dominio.
   - API y tesela salen de `MapCode`, así que casan por construcción, también en el nivel.
   - Los clientes de la API reciben dos campos nuevos y ninguno se rompe.
   - Otros contextos (`contracts.BoundaryRef`) no cambian.
2. **Artefactos.**
   - Al corregir Navarra en la semilla cambian a la vez el `code` de la API, la tesela (al regenerarla) y, en esa otra tarea, la ruta de festivos.
   - Si algún día las provincias 07, 35 y 38 reciben nivel (D-c ii), la escalera cambia sola, sin tocar código. Sólo hay que regenerar y actualizar `expected-unmatched` y los recuentos fijados en las pruebas.
   - `StaticFile` detecta un fichero nuevo por su tamaño y su `mtime`.
3. **Despliegue.**
   - El `.pmtiles` es un artefacto de despliegue y no entra en el repositorio.
   - Sin la variable, la ruta no existe y el front cae a la demo esquemática.
   - El fichero se sustituye en caliente con un `rename` atómico. Las lecturas en curso terminan con el descriptor antiguo.
   - La caché es de un día, así que tras regenerar los clientes tardan como mucho eso en ver el cambio, salvo que revaliden por ETag.

## 2. Artefactos y patrones de obligado cumplimiento
- **`pkg/domain`:** no se crean agregados, eventos ni especificaciones.
  - `MapCode` es vocabulario del dominio de geografía: un método de consulta sobre el agregado `Boundary` que ya existe, sin puertos ni estado nuevo.
  - Usa `TypeCountry`, `TypeAutonomous`, `TypeProvince`, `TypeIsland` y `TypeMunicipality` de `domain/geography.go`.
- **`pkg/application`:**
  - **La consulta de geografía** sigue con su cadena de permiso y su CQRS: sólo gana campos. `mapParents` es un ayudante de lectura dentro de la misma consulta y no tiene transacción propia.
  - **El fichero estático no usa `pkg/application`.** Ésta es una desviación, y su justificación es que seguir los contratos añadiría complejidad innecesaria:
    - GET y HEAD son idempotentes y no hay transacción;
    - los datos son públicos, y un JWT impediría la caché compartida;
    - la ruta va fuera de `Authorize`, como `/healthz`.
- **Migraciones:** ninguna.
- **Rutas HTTP:**
  - `GET` y `HEAD` en `/geo/karpo.pmtiles`. Con cualquier otro método, `405` y `Allow: GET, HEAD`.
  - `GET /api/geography/boundaries` y el GET de un límite devuelven además `code` y `parentCode`.

### Escalera del mapa y regla de claves
| Tipo en la semilla | Nº | `level` (semilla = API = tesela) | `kind` (informativo) | `code` | `parentCode` | Geometría |
|---|---|---|---|---|---|---|
| País | 248 | `country` | `country` | alfa-2 (`geo_code`) | — | Natural Earth |
| Agrupación NUTS 1 | 7 | `1` | — | vacío (`ok=false`) | — | ninguna |
| Comunidad o ciudad autónoma | 19 | `2` | `region` | `country` + `-` + `abbreviation` | `ES` (se salta el NUTS 1) | BDLJE, comunidades |
| Provincia con nivel | 49 | `3` | `province` | INE de 2 cifras | `ES-XX` de su comunidad | BDLJE, provincias |
| Provincia sin nivel (07, 35 y 38) | 3 | vacío | — | vacío (`ok=false`) | — | ninguna (su superficie la cubren sus islas) |
| Isla (ES531…ES709) | 10 | `3` | `island` | `geo_code` NUTS, por ejemplo `ES532` (D-c i, excepción documentada) | `ES-IB` o `ES-CN` (se salta la provincia sin nivel) | municipios de BDLJE disueltos por isla |
| Municipio | 8.132 | `local` | `municipality` | INE de 5 cifras (= `lauCode`) | `code` de su provincia o de su isla | BDLJE, municipios |
| Otros (continentes, uniones, tipos desconocidos) | — | — | — | vacío (`ok=false`) | — | — |

- **`ok=false`** si el nivel no es `country`, `2`, `3` ni `local`, o si el tipo no tiene regla.
- **`parentCode`** es el `code` del primer antecesor con `ok=true`. Ejemplos:
  - el padre de 07040 es Mallorca;
  - el de 38038, Tenerife;
  - el de 51001, la provincia 51;
  - el de Mallorca, `ES-IB` (se salta 07);
  - el de `ES-MD`, `ES` (se salta ES3).
- **`hasChildren`** se pone a `true` si alguna entidad de `keys.csv` tiene ese `code` como `parentCode`. Se calcula en `cmd/geokeys`.
- **Recuentos esperados:** 248 países (menos los que Natural Earth no traiga, que se listan), 19 comunidades, 49 provincias, 10 islas y 8.132 municipios.
- **`tools/geo/expected-unmatched.txt`** es una lista cerrada de **claves BDLJE** (`NATCODE`):
  - los recintos provinciales 07, 35 y 38;
  - los territorios sin municipio en la semilla que aparezcan al ejecutar T4 (condominios y similares), cada uno con su `NATCODE` y su motivo.

  **Cualquier diferencia, de más o de menos, hace fallar la tubería.**
- **Defecto conocido:** la comunidad de Navarra sale como `ES-NA` (ISO dice `ES-NC`), igual en la API y en la tesela. Se corrige en la semilla en una tarea aparte (ver §7).

### Teselas
- **Capa `boundaries`** (polígonos), con estas propiedades:
  - `code`, `name`, `level` y `kind` (siempre);
  - `parentCode` (salvo en los países);
  - `hasChildren` (siempre);
  - `lauCode` (sólo en los municipios).
- **Capa `labels`**: un punto `ST_PointOnSurface` por entidad, con las mismas propiedades.
- **`shortName` no se emite**, ni siquiera vacío.
- **Atribución en la metadata:** «Natural Earth (dominio público); Obra derivada de BDLJE <fecha> CC-BY 4.0 ign.es».

## 3. Librerías y herramientas de terceros
Ninguna entra en `go.mod` ni en `package.json`.

| Necesidad | Candidatas | Coste y complejidad | Licencia | Elegida |
|---|---|---|---|---|
| Teselado a PMTiles | **tippecanoe (felt)**, Planetiler, ampliar `build-pmtiles.mjs` | tippecanoe: un comando. Planetiler: una JVM, excesivo. Script propio: reescribir la simplificación | BSD-2 / Apache-2 / propia | **tippecanoe** en Docker, con la versión fijada: la imagen oficial si existe; si no, un Dockerfile en `tools/geo/` con el commit fijado |
| Shapefile a GeoJSON WGS84, disolución por isla y centroides | **GDAL `ogr2ogr`**, mapshaper | GDAL: imagen oficial, con SQL/SpatiaLite para `ST_Union` y `ST_PointOnSurface` | MIT / MPL-2.0 | **GDAL** (`ghcr.io/osgeo/gdal`, con la etiqueta fijada) |
| Simplificación topológica | sólo tippecanoe, mapshaper | mapshaper añade un paso | MPL-2.0 | **Sólo tippecanoe**, con `--detect-shared-borders`. mapshaper sólo si aparecen huecos (decisión reversible) |
| Comprobación del fichero | go-pmtiles CLI, lector propio | un binario | BSD-3 | **CLI** en la tubería. La prueba Go lee la cabecera y la metadata con la biblioteca estándar |
| Servidor con Range | **`http.ServeContent`**, `pmtiles serve` | biblioteca estándar | BSD | **`http.ServeContent`** |

## 4. Diseño detallado

### 4.1 `contexts/geography/domain/mapcode.go`
```go
// MapCode devuelve la clave pública de b en el mapa y su clase.
// ok es false si b no está en la escalera: nivel distinto de country, 2, 3 o local,
// o tipo sin regla.
func (b *Boundary) MapCode() (code, kind string, ok bool)
```
- **País:** `geo_code`.
- **Comunidad:** `country` + `-` + `abbreviation` (la misma regla que `holidays.go`).
- **Provincia:** `ine_code` de 2 cifras.
- **Isla:** `geo_code`.
- **Municipio:** `ine_code` de 5 cifras.
- Sin `ine_code` o sin abreviatura, el resultado es `ok=false`, nunca una clave vacía con `ok=true`.

### 4.2 `contexts/geography/application/mapparents.go`
```go
// mapParents devuelve, para cada límite, el code de su primer antecesor con MapCode ok.
func mapParents(ctx context.Context, p Ports, bs []*domain.Boundary) (map[domain.BoundaryID]string, error)
```
Funciona **por rondas**, con el mismo patrón que `Ports.CountryOf`:
1. Reúne los padres pendientes, sin repetir.
2. Los carga con `p.boundaries`, en tramos de 500.
3. A cada pendiente cuyo padre tiene `ok=true` le asigna su `code`. Los demás pasan al padre del padre.

Tiene un tope de `MaxDepth` rondas. Con la semilla actual son dos como mucho (isla → 07 → comunidad; comunidad → NUTS 1 → país).

- **`SearchBoundaries`** lo llama una vez por página, después de `FindPage`.
- **`GetBoundary`** lo llama con una lista de un elemento.
- **No se amplía `contracts.BoundaryRef` ni se reutilizan los `Ancestors`:** son un contrato para otros contextos y no traen códigos.
- **`boundaryDTO`** rellena `Code` con `MapCode`, y `ParentCode` con el mapa. Si `ok=false`, los dos campos quedan vacíos.
- **`holidays.go` no se refactoriza.** Una prueba de ida y vuelta mantiene alineadas su regla y `MapCode` (§6).

### 4.3 `cmd/geokeys` (raíz; `go run ./cmd/geokeys`)
- Lee la semilla con `infrastructure.LoadSeed()` y **no vuelve a partir los CSV**: cuatro nombres llevan coma entre comillas.
- Para los padres usa la misma lógica de subida sobre el catálogo en memoria.
- Escribe `keys.csv` con las columnas `code,parentCode,name,level,kind,hasChildren,lauCode,ine,islandOf`.
  - `islandOf` es el `code` de la isla de cada municipio insular. La tubería lo usa para disolver.
  - Sólo se escriben las entidades con `ok=true`.

### 4.4 `pkg/distribution/static.go`
```go
type StaticFile struct {
    path, ctype string
    mu          sync.Mutex
    size        int64
    mod         time.Time
    etag        string // "\"sha256-<hex>\""
}
func NewStaticFile(path, contentType string) (*StaticFile, error)
func (s *StaticFile) ServeHTTP(w http.ResponseWriter, r *http.Request)
```
- **Método.** Sólo GET y HEAD. Con cualquier otro, `405` con `Allow`.
- **Por cada petición:**
  1. `os.Open`, y `f.Stat()` sobre el mismo descriptor.
  2. Si el tamaño o el `mtime` cambiaron, se recalcula el SHA-256 de ese descriptor con el mutex cogido.
  3. Se ponen `ETag`, `Cache-Control: public, max-age=86400, stale-while-revalidate=604800`, `Content-Type: application/vnd.pmtiles` y `Accept-Ranges: bytes`.
  4. `http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10*time.Minute))`. Si devuelve error (un writer que no lo soporta), se ignora.
  5. `http.ServeContent(w, r, "", mod, f)`, y se cierra el fichero.
- **Plazo de escritura.** El `WriteTimeout` global de 30 s (`server.go:22`) no se toca. Sólo este manejador lo amplía.
- **Sin compresión.**
- **Errores.** Si el fichero no existe, `404`.
- **CORS.** Lo resuelve `middleware.go:209-212`.

### 4.5 `host/serve/serve.go`
- **Configuración.** `Config.PMTiles` se lee de `KARPO_PMTILES`. Vacía, la ruta no se monta.
- **Montaje.** Después de `health.Mount(mux)`: `NewStaticFile` y `mux.Handle("/geo/karpo.pmtiles", f)`.
- **Ruta inexistente.** El arranque falla con un error que nombra `KARPO_PMTILES`.
- **Documentación.** La variable va en el comentario de `Config` y en `docs/ARQUITECTURA.md`.

### 4.6 Tubería `tools/geo/build.sh`
1. `go run ./cmd/geokeys > $OUT/keys.csv`.
2. **Descarga** a `$GEO_WORK` (por defecto `C:/Git/Paranoia/geo-downloads/`, una carpeta nueva fuera del repositorio, tratada como datos no fiables):
   - Natural Earth admin-0;
   - recintos autonómicos, provinciales y municipales de BDLJE.

   Se comprueba el SHA-256 contra `SOURCES.md`, y la tubería aborta si no coincide.
3. **Conversión** con `ogr2ogr -t_srs EPSG:4326`, una capa por nivel. El sistema de referencia de Canarias se confirma en T1.
4. **Islas.** Los municipios de BDLJE se enlazan con `keys.csv` por INE y se disuelven con `ST_Union … GROUP BY islandOf`. Salen 10 polígonos.
5. **Enlace** con `go run ./tools/geo/join`:
   - Natural Earth por `ISO_A2_EH`;
   - BDLJE por `NATCODE`. **Qué cifras de `NATCODE` son el INE** (se cree que las 5 últimas de 11 en municipios) **se confirma en T1 y se fija en una prueba de `join` con un recinto real.**

   Lo que no enlaza se compara con `expected-unmatched.txt`, y cualquier diferencia hace fallar la tubería. Se listan los países de la semilla que Natural Earth no trae.
6. **Centroides** con `ST_PointOnSurface`, en la capa `labels`.
7. **Teselado** con tippecanoe: `-Z0 -z12`, capas `boundaries` y `labels`, las opciones de no descartar (confirmadas con `--help` en T1), `--detect-shared-borders` y `--attribution`.
8. **Verificación** con `pmtiles verify` y `pmtiles show`. Se anotan el tamaño total y el de z0 para D-b.

### 4.7 Front (`front/react`)
- **`src/map/aggregate.ts`, `boundaryCode()`.** Devuelve `code || lauCode`, y recurre a `nutsCode`, `ineCode` y `geoCode` **sólo cuando la respuesta no trae `code`** (la demo). Así el NUTS 1, que tiene `code` vacío y por tanto `ok=false`, no se vuelve «colocable» con la API real. Lo fija una prueba.
  - `chainOf` sigue usando el `level` de la API, que ahora es por construcción el de la tesela. La provincia sin nivel se omite.
- **`src/map/nav.ts`, `parentCandidates`.** Usa la propiedad `parentCode` de la entidad cargada. El recorte NUTS (`ES300` → `ES30` → `ES`) queda sólo como respaldo de la demo.
- **`src/map/sources.ts`, `resolveTrail`.** Saca el padre de `parentCode` a través de `parentCandidates`.
- **`demo/config/manifest.json`.** Los `defaultCode` pasan de `ES30` a `ES-MD` y de `ES300` a `28`.
- **`shortName`.** No cambia código: `text()` (`geo.ts:139`) ya tolera `undefined` y `labels.ts:110` cae al nombre. Sólo se añade una prueba.
- **`scripts/build-pmtiles.mjs`.** La demo emite `code`, `parentCode`, `level` y `hasChildren` con la misma escalera.
- **`docs/PETICIONES-BACKEND.md`:**
  - B-2 queda hecha (o hecha con el contrato nuevo, si D-b ii);
  - B-26 queda hecha salvo `shortName`, con un enlace a B-24;
  - se anotan Navarra, la clave NUTS de las islas (D-c) y que el NUTS 1 y las provincias 07, 35 y 38 no están en el mapa.

## 5. Tareas
| # | Tarea | Depende de | Paralelo con | Modelo y esfuerzo | Por qué |
|---|---|---|---|---|---|
| T1 | Descargas y registro en `SOURCES.md` (fichero, URL, licencia, tamaño, SHA-256 y fecha) de NE admin-0, BDLJE, imágenes de tippecanoe y GDAL y CLI de pmtiles. Hay que confirmar: las opciones de tippecanoe, el sistema de referencia de Canarias, qué cifras de `NATCODE` son el INE y si el CNIG exige formulario (si lo exige, se para y se avisa a Javier) | — | T2, T3 | Sonnet, esfuerzo bajo | Mecánica, con cuidado de tratar los datos como no fiables |
| T2 | `MapCode` (dominio), `mapParents`, `Code` y `ParentCode` en el DTO, `cmd/geokeys`, pruebas (tabla, integridad, lotes, nivel de API = nivel de tesela, ida y vuelta) y `docs/GEOGRAFIA.md` | — | T1, T3 | Sonnet, esfuerzo medio | Regla de dominio con **mutación exigida**, subida por lotes sin N+1 y ampliación del contrato de la API |
| T3 | `StaticFile` (ETag que se recalcula, plazo de escritura), `KARPO_PMTILES` y sus pruebas | — | T1, T2 | Sonnet, esfuerzo medio | Pieza pequeña, pero los casos de Range, ETag y cambio de fichero son delicados |
| T4 | Tubería, `join` (con su prueba de `NATCODE`), disolución por isla, `expected-unmatched.txt`. Generar el fichero y medir el total y z0 | T1, T2 | T5 | Opus, esfuerzo medio | La más incierta: sistemas de referencia, enlaces y opciones de tippecanoe |
| T5 | Front: `aggregate`, `nav`, `sources`, manifest, demo, pruebas vitest y `PETICIONES-BACKEND.md` | T2 | T4 | Sonnet, esfuerzo medio | Migas, cadena de agregación y respaldo de la demo |
| T6 | Cierre: `BACKLOG.md` (Navarra, OSM, `shortName`, D-c y supervivientes previos), batería, mutación y un solo commit | T3, T4, T5 | — | Sonnet, esfuerzo bajo | Integración |

## 6. Pruebas
- **`contexts/geography/domain` (tabla de `MapCode`):**
  - `ES` → `country`;
  - `ES-MD`;
  - `ES-NA` para la comunidad de Navarra (el defecto documentado, para que la corrección lo cambie a propósito);
  - `28`;
  - **38 → `ok=false`**;
  - 07 → `ok=false`;
  - Tenerife → `ES` + NUTS de isla y nivel `3`;
  - Mallorca;
  - `07040`, `38038`, `28079` y `51001`;
  - ES3 (NUTS 1) → `ok=false`;
  - un tipo desconocido → `ok=false`;
  - un municipio sin `ine_code` → `ok=false`.
- **`contexts/geography/*_test.go` (semilla con `infrastructure.LoadMemory`):**
  - **`TestSearchBoundaries_ParentCodesInBatches`:** un `domain.BoundaryRepository` envoltorio que cuenta las llamadas a `Find`. Una página con municipios insulares, islas, provincias y comunidades (menos de 500 padres distintos) hace **como mucho 2 llamadas a `Find`** además del `FindPage`, y nunca una por elemento.
  - **Padres:** 38038 → Tenerife; 07040 → Mallorca; Tenerife → `ES-CN`; Mallorca → `ES-IB`; `ES-MD` → `ES`; 51001 → `51`.
  - **Nivel de API = nivel de tesela:** para toda entidad con `ok=true`, el `level` de `keys.csv` es igual a `BoundaryDTO.Level`.
  - **Integridad:**
    - cada `parentCode` existe como `code`;
    - no hay códigos duplicados;
    - los recuentos son 19, 49, 10 y 8.132;
    - `hasChildren` es coherente con los hijos;
    - **hay 3 provincias sin nivel, cada una con al menos una isla hija, y ningún municipio cuelga directamente de ellas.**
  - **Ida y vuelta:** `Locate(code)` devuelve `b` para todas las comunidades, provincias con nivel y municipios. Las islas quedan fuera, porque `Locate` no las resuelve.
  - **API:** `GET /api/geography/boundaries?type=…` devuelve `code` y `parentCode`, y ninguno de los dos en el NUTS 1.
  - `holidays_test.go` sigue verde.
- **`pkg/distribution/static_test.go`:**
  - GET `200` con `Accept-Ranges`, `ETag`, `Cache-Control` y `Content-Length`;
  - `Range: bytes=0-15` → `206`, `Content-Range` y cuerpo exacto;
  - rango fuera del fichero → `416`;
  - `If-None-Match` → `304`;
  - `If-Range` con un ETag distinto → `200` completo;
  - HEAD sin cuerpo;
  - POST → `405` con `Allow`;
  - fichero borrado → `404`;
  - `NewStaticFile` sobre una ruta inexistente → error;
  - fichero sustituido → el ETag cambia, y un `If-Range` con el ETag antiguo recibe `200`, nunca `206`;
  - **plazo de escritura:** un `ResponseWriter` que registra `SetWriteDeadline` comprueba que se llama;
  - CORS: se exponen `Content-Range` y `ETag`.
- **`host/serve`:** sin la variable, la ruta no se monta; con una ruta inexistente, el arranque falla nombrando `KARPO_PMTILES`.
- **`tools/geo/join`:** el enlace de un recinto real de BDLJE con su INE, y que `expected-unmatched` falla tanto si sobra una clave como si falta.
- **Fichero generado** (etiqueta de compilación `geo`, fuera de la batería normal):
  - la cabecera v3 y la metadata llevan la atribución;
  - el recuento por `kind` coincide con `keys.csv`, menos los países listados;
  - todas las claves pertenecen a `keys.csv`;
  - todas las entidades llevan `level` y `hasChildren`.

  No se compara byte a byte.
- **Front (vitest):**
  - `boundaryCode` usa `code`, y el respaldo NUTS sólo actúa sin `code`. El NUTS 1 de la API no es colocable.
  - `chainOf` de 07040 con antecesores [Mallorca, provincia 07 sin nivel, Illes Balears, ES5, España] da `(local,07040)`, `(3,ES532)`, `(2,ES-IB)` y `(country,ES)`.
  - `parentCandidates` (`nav.test.ts`) con `ES-MD`, `28`, `28079` y `07040` (a través de `parentCode`).
  - El manifest arranca en `ES-MD` y en `28`.
  - Una entidad sin `shortName` se rotula con su nombre.
- **Mutación (exigida):** `go run github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0 unleash --workers 4 --timeout-coefficient 10` sobre:
  - `./contexts/geography/domain`;
  - `./contexts/geography/application`;
  - `./pkg/distribution`.

  Los supervivientes anteriores a la tarea se apuntan en `BACKLOG.md`. StrykerJS no está instalado, así que la mutación del front figura como «no ejecutada».
- **Batería:** `go build ./... && go vet ./... && go test -count=1 -cover ./...` (mínimo del 80 % en los paquetes tocados) y `vitest`.
- **Comando para Javier:** arrancar con `KARPO_PMTILES=<ruta>/karpo.pmtiles` y ejecutar `curl -sI -H "Range: bytes=0-126" http://localhost:8080/geo/karpo.pmtiles`. Debería mostrar `206`, `Accept-Ranges: bytes` y un `ETag`.

## 7. Decisiones para Javier
1. **D-a. Sin OSM en esta entrega.** Mundo con Natural Earth y España con el IGN. OSM se aplaza hasta que haya regiones de otros países (con Geofabrik y osmium). Es un recorte frente a Q-8.
2. **D-b. Municipios en los zooms bajos.** Primero se mide z0 con todos los municipios desde z0. Si pesa demasiado, se le lleva la cifra junto con la alternativa: municipios desde un zoom medio y un índice JSON de cajas y centroides.
3. **D-c. Qué es el nivel 3 del mapa en Baleares y Canarias.**
   - **(i) Lo que dice la semilla: 49 provincias + 10 islas.** La clave de la isla es su NUTS (`ES532`), como excepción documentada a «`code` nunca en NUTS».
     - No toca datos y es reversible regenerando.
     - API y tesela casan sin más.
     - `Locate` de festivos no resuelve islas, igual que hoy.
   - **(ii) Las 52 provincias INE, con las islas fuera del mapa.** Todas las claves serían INE puras. Pero exige que 07, 35 y 38 pasen a nivel `3` y que las islas lo pierdan:
     - o se cambia la semilla, contra `docs/GEOGRAFIA-NUTS.md` §4.2 y con traslado a Trench y a Karpo C#;
     - o la API expone un «nivel de mapa» distinto de `level`.

     Además, los 155 municipios insulares suben a la provincia y se pierde la isla como unidad de análisis.

  **Propuesta:** (i), de forma provisional.

**Decisiones técnicas que sólo se le cuentan:**
- el fichero estático va sin DDD ni CQRS;
- la regla de claves va al dominio de geografía;
- la API de geografía gana `code` y `parentCode` (ampliación compatible);
- el nivel de la tesela es el `level` de la semilla, y **quedan fuera de la escalera el nivel 1 y las provincias 07, 35 y 38**, que no tienen nivel;
- `parentCode` se calcula por lotes, sin N+1;
- `holidays.go` no se refactoriza, y una prueba de ida y vuelta mantiene alineadas las dos reglas;
- `shortName` pasa a B-24;
- la caché es de un día, con un ETag que se recalcula si el fichero cambia y un plazo de escritura propio para este manejador;
- **Navarra va en una tarea aparte:** la comunidad debe ser `ES-NC`. El dato es común con Trench (`076-parties-geographic-es.opendata.sql:37`) y con Karpo C#. Cambia la ruta pública de festivos y necesita una migración de datos. Se propone como decisión común y se traslada a esas dos sesiones.

## 8. Consultas al asesor
No hay consultas.
