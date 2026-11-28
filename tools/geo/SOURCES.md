# Fuentes de la tesela de delimitaciones

Todo se descarga a `GEO_WORK` (por defecto `../geo-downloads`, fuera del repositorio) y se trata
como datos no fiables: sólo lo leen `ogr2ogr`, `tools/geo/join` y tippecanoe, nunca se ejecuta.
Registro de la primera descarga: 2026-11-28.

## Datos

| Fichero | Fuente | Licencia | Tamaño | SHA-256 |
|---|---|---|---|---|
| `ne/ne_10m_admin_0_countries.zip` | https://naciscdn.org/naturalearth/10m/cultural/ne_10m_admin_0_countries.zip | Dominio público (Natural Earth) | 4.930.492 B | `ce1ac7036499a0edd641fbc093cd209a98f96a49d2eca8480aaacad35138a7f6` (lo comprueba `build.sh`) |
| `ign/au-0.json` … `au-8.json` | BDDAE del IGN por su servicio WFS INSPIRE de unidades administrativas: `https://www.ign.es/wfs-inspire/unidades-administrativas` (`au:AdministrativeUnit`, GeoJSON, EPSG:4326, 1.000 por página, 8.294 unidades) | CC-BY 4.0, atribución «Obra derivada de BDDAE <fecha> CC-BY 4.0 ign.es» | 752.043.470 B en 9 páginas | se actualiza cada trimestre: la huella de cada página se registra en la procedencia en vez de fijarla |

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

## Descarga de la BDDAE

`build.sh` pregunta antes al WFS cuántas unidades hay (`resultType=hits`, `numberMatched`), baja
las páginas a `ign.tmp/` y sólo al terminar las pasa a `ign/` con `date` (día de descarga) y
`count`. Cada generación cuenta las unidades descargadas y se detiene si no coinciden: un informe
de excepción del servicio o una página cortada no se confunden con el final. La atribución lleva
el mes de `date`, no el de la generación. Para descargar de nuevo, se borra `ign/`.

## El artefacto

| | |
|---|---|
| Zoom | 0–8 (`MAXZOOM`, 8 por defecto): el `maxzoom` que fija el front, que amplía por encima |
| Tiempo | 16 min (MAXZOOM=8, opciones finales; la vuelta anterior, sin `--drop-rate=1`, 21 min); z12 se detuvo al 78 % tras horas |
| Tamaño | 16.771.614 B |
| Tesela z0 (D-b) | 409.766 B comprimida, con los 8.132 municipios como rótulo y 7.818 como polígono (los demás no tienen área a z0) |

Junto a `karpo.pmtiles` queda `karpo.provenance.txt`: huella y tamaño del fichero, zoom, fecha y
número de unidades de la BDDAE, huella de cada página, Natural Earth e identificador local de cada
imagen. El de la generación del 2026-11-28:

```
karpo.pmtiles 3496e536980de7a69158f80042bf2e0ce35f1b7f274ab316b51fc63effbea475 16771614 B
zoom 0-8
BDDAE descargada 2026-11-28, 8294 unidades
  5bea6ecc53cc5634365fa3106310f60e91824549cce26782d7816179e28b2714 *au-0.json
  476ae36b29cd1de49a6fab5235276785815beaf662890a391022a2cf6e6bf156 *au-1.json
  4ce6ee4ae0b7d4c72c726d54fd458b434bd6dd62d68eb24869dbafc301c883f9 *au-2.json
  50e25cf803963b81aa627653be2b2144bc261fe0a33500a409845d99daa57496 *au-3.json
  492aa343cb533747189ea68f4d72bd2d2c387c079286d6a0ada1b3e31731fca6 *au-4.json
  b7e47d85c462d8344c15f59b026d38008c8dc7ed1768c6695912b3c36628cbe0 *au-5.json
  ef5fca6480c6caf5ac6a24e859ba8b09b8a79edf05bcd96d48786ba713394a52 *au-6.json
  db082efa0e4b367a33035e69c55e955c923d422f1e81782d4311d7d93085aead *au-7.json
  37d36a83b553762690279d8b32b39cd8c6be84db3f9eec8472b47cb58e72e52f *au-8.json
Natural Earth ne_10m_admin_0_countries.zip ce1ac7036499a0edd641fbc093cd209a98f96a49d2eca8480aaacad35138a7f6
ghcr.io/osgeo/gdal:alpine-normal-3.10.3 sha256:d4b82322f433c1ec03d803ee42c718d7b34fbede17538c096bc5e9a3bc4d776e
karpo/tippecanoe:2.79.0 sha256:389ddbda7ba127b6b7a4ddbc324d3cb8f77f8d0ac1418a57401c9042b707327e
protomaps/go-pmtiles:v1.28.0 sha256:4bfe4da8b2d74f98703d2a7f103008c3b80d2bf0ef423f479aedfd28ef893765
```
