#!/usr/bin/env bash
# Genera karpo.pmtiles, la tesela de delimitaciones del mapa: países de Natural Earth y comunidades,
# provincias, islas y municipios de la BDDAE del IGN, con las claves de la API de geografía
# (ISO 3166 e INE; la isla, su NUTS). Ver tools/geo/SOURCES.md y docs/GEOGRAFIA.md.
#
#   tools/geo/build.sh            # desde la raíz del repositorio
#
# GEO_WORK es la carpeta de trabajo, fuera del repositorio (datos descargados: no fiables).
# Necesita Go y Docker. El resultado queda en $GEO_WORK/out/karpo.pmtiles.
set -euo pipefail

REPO=$(cd "$(dirname "$0")/../.." && pwd)
GEO_WORK=${GEO_WORK:-$REPO/../geo-downloads}
mkdir -p "$GEO_WORK/ne" "$GEO_WORK/ign" "$GEO_WORK/out"
GEO_WORK=$(cd "$GEO_WORK" && pwd)
# Go en Windows no entiende las rutas /c/... de Git Bash.
command -v cygpath >/dev/null && GEO_WORK=$(cygpath -m "$GEO_WORK")
OUT=$GEO_WORK/out

GDAL=ghcr.io/osgeo/gdal:alpine-normal-3.10.3
PMTILES=protomaps/go-pmtiles:v1.28.0
TIPPECANOE=karpo/tippecanoe:2.79.0
NE_ZIP=ne_10m_admin_0_countries.zip
NE_SHA=ce1ac7036499a0edd641fbc093cd209a98f96a49d2eca8480aaacad35138a7f6
WFS="https://www.ign.es/wfs-inspire/unidades-administrativas?service=WFS&version=2.0.0&request=GetFeature&typeNames=au:AdministrativeUnit&srsName=EPSG:4326&sortBy=au:nationalCode&outputFormat=application/geo%2Bjson&count=1000"

# En Git Bash para Windows, que no traduzca las rutas de los contenedores.
export MSYS_NO_PATHCONV=1
docker_run() { docker run --rm -v "$GEO_WORK:/w" "$@"; }

echo "1. Claves"
(cd "$REPO" && go run ./cmd/geokeys) > "$OUT/keys.csv"

echo "2. Descargas"
if [ ! -f "$GEO_WORK/ne/$NE_ZIP" ]; then
  curl -fsSL -o "$GEO_WORK/ne/$NE_ZIP" "https://naciscdn.org/naturalearth/10m/cultural/$NE_ZIP"
fi
echo "$NE_SHA  $GEO_WORK/ne/$NE_ZIP" | sha256sum -c -
if ! ls "$GEO_WORK"/ign/au-*.json >/dev/null 2>&1; then
  i=0
  while :; do
    page="$GEO_WORK/ign/au-$i.json"
    curl -fsS --max-time 900 -o "$page" "$WFS&startIndex=$((i * 1000))"
    grep -q '"Feature"' "$page" || { rm -f "$page"; break; }
    i=$((i + 1))
  done
fi
# La BDDAE se lee de su servicio WFS, que cambia cada trimestre: su huella se registra, no se fija.
(cd "$GEO_WORK/ign" && sha256sum au-*.json) > "$OUT/ign.sha256"

echo "3. Países"
docker_run "$GDAL" ogr2ogr -f GeoJSON -t_srs EPSG:4326 -select ISO_A2_EH /w/out/ne.geojson "/vsizip//w/ne/$NE_ZIP"

echo "4. Enlace"
(cd "$REPO" && go run ./tools/geo/join -keys "$OUT/keys.csv" -ne "$OUT/ne.geojson" -ign "$GEO_WORK/ign/au-*.json" \
  -expected tools/geo/expected-unmatched.txt -out "$OUT/boundaries.geojsonl" -islands "$OUT/island-municipalities.geojsonl")

echo "5. Islas"
rm -f "$OUT/islands.geojsonl" "$OUT/labels.geojsonl"
docker_run "$GDAL" ogr2ogr -f GeoJSONSeq /w/out/islands.geojsonl GeoJSONSeq:/w/out/island-municipalities.geojsonl -dialect sqlite \
  -sql "SELECT code, name, level, kind, parentCode, hasChildren, ST_Union(geometry) AS geometry FROM \"island-municipalities\" GROUP BY code, name, level, kind, parentCode, hasChildren"
cat "$OUT/boundaries.geojsonl" "$OUT/islands.geojsonl" > "$OUT/all.geojsonl"
# Como colección: el lector de GeoJSON deduce las columnas de todas las entidades, no de las primeras.
{ echo '{"type":"FeatureCollection","features":['; paste -sd, "$OUT/all.geojsonl"; echo ']}'; } > "$OUT/all.geojson"

echo "6. Rótulos"
docker_run "$GDAL" ogr2ogr -f GeoJSONSeq /w/out/labels.geojsonl /w/out/all.geojson -dialect sqlite \
  -sql "SELECT code, name, level, kind, parentCode, hasChildren, lauCode, ST_PointOnSurface(geometry) AS geometry FROM \"all\""

echo "7. Teselado"
docker_run "$TIPPECANOE" -o /w/out/karpo.pmtiles --force --quiet -Z0 -z12 \
  -L boundaries:/w/out/all.geojsonl -L labels:/w/out/labels.geojsonl \
  --detect-shared-borders --no-feature-limit --no-tile-size-limit --no-tiny-polygon-reduction \
  --attribution "Natural Earth (dominio público); Obra derivada de BDDAE $(date +%Y-%m) CC-BY 4.0 ign.es"

echo "8. Verificación"
docker_run "$PMTILES" verify /w/out/karpo.pmtiles
docker_run "$PMTILES" show /w/out/karpo.pmtiles
ls -l "$OUT/karpo.pmtiles"
