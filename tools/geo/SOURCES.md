# Fuentes de la tesela de delimitaciones

Todo se descarga a `GEO_WORK` (por defecto `../geo-downloads`, fuera del repositorio) y se trata
como datos no fiables: sólo lo leen `ogr2ogr`, `tools/geo/join` y tippecanoe, nunca se ejecuta.
Registro de la primera descarga: 2026-11-28.

## Datos

| Fichero | Fuente | Licencia | Tamaño | SHA-256 |
|---|---|---|---|---|
| `ne/ne_10m_admin_0_countries.zip` | https://naciscdn.org/naturalearth/10m/cultural/ne_10m_admin_0_countries.zip | Dominio público (Natural Earth) | 4.930.492 B | `ce1ac7036499a0edd641fbc093cd209a98f96a49d2eca8480aaacad35138a7f6` (lo comprueba `build.sh`) |
| `ign/au-0.json` … `au-8.json` | BDDAE del IGN por su servicio WFS INSPIRE de unidades administrativas: `https://www.ign.es/wfs-inspire/unidades-administrativas` (`au:AdministrativeUnit`, GeoJSON, EPSG:4326, 1.000 por página, 8.294 unidades) | CC-BY 4.0, atribución «Obra derivada de BDDAE <fecha> CC-BY 4.0 ign.es» | 752.043.470 B en 9 páginas | se actualiza cada trimestre: `build.sh` registra la huella de cada descarga en `out/ign.sha256` en vez de fijarla |

**Desviación del plan:** el plan preveía los ficheros shapefile del Centro de Descargas del CNIG.
Su servidor cortó las conexiones TLS desde aquí, y el WFS del IGN publica la misma base (BDDAE)
sin formulario, ya en WGS84 y con el código nacional. No hace falta conversión de sistema de
referencia, tampoco para Canarias.

**El código nacional de la BDDAE** tiene 11 cifras: `34`, la comunidad (código INE de comunidad),
la provincia y 5 cifras más, que en un municipio son su INE (`34040707040` es Palma, 07040). Lo
fija `TestIGNCode_RealUnits`. Los municipios `xx53xxx` son condominios y `3420…` los territorios no
asociados; no están en la semilla y se listan en `expected-unmatched.txt`.

**Países que Natural Earth 10m no trae por separado** (11): BQ, BV, CC, CX, GF, GP, MQ, RE, SJ, TK
y YT (Natural Earth los incluye dentro de Francia, Noruega, Australia, Países Bajos o Nueva
Zelanda). Se quedan sin geometría propia.

## Herramientas (imágenes de Docker)

| Herramienta | Imagen | Licencia | Tamaño | Digest |
|---|---|---|---|---|
| GDAL 3.10.3 (con SpatiaLite) | `ghcr.io/osgeo/gdal:alpine-normal-3.10.3` | MIT | 402.537.314 B | `sha256:c855af69471754610150905d394c212804e5a12e6bfc092bf7ed8c93973a3c2a` |
| go-pmtiles 1.28.0 | `protomaps/go-pmtiles:v1.28.0` | BSD-3 | 59.139.625 B | `sha256:bc983638e5f59c935dac1952d876be24552de53f2b4976c9726f55533f259e9c` |
| tippecanoe 2.79.0 | `karpo/tippecanoe:2.79.0`, compilada con `tools/geo/tippecanoe.Dockerfile` desde la etiqueta `2.79.0` de github.com/felt/tippecanoe (no hay imagen oficial pública: `ghcr.io/felt/tippecanoe` deniega el acceso) | BSD-2 | — | se construye: `docker build -t karpo/tippecanoe:2.79.0 -f tools/geo/tippecanoe.Dockerfile tools/geo` |
