// Geometría ESQUEMÁTICA y determinista para el mapa por niveles de la demostración (PLAN §4.13).
// No es cartografía y no copia ningún fichero de terceros: los centroides están escritos a mano en
// demo/data/countries.mjs y la rejilla de España en demo/data/spain.mjs.
//
//   world.geojson            un octógono por país (248), separados por una relajación determinista
//   country-ES.geojson       las 19 comunidades (NUTS 2) como teselas de un cartograma en rejilla
//   region-<nuts2>.geojson   las provincias o islas (NUTS 3) de cada comunidad
//   city-<nuts3>.geojson     la muestra de municipios de Madrid (ES300) y Barcelona (ES511)
//   index.json               índice con el nombre de fichero, el nivel y la caja de cada colección
//
// Propiedades de cada elemento: code, name, shortName (opcional: lo que se rotula si el nombre completo no
// cabe en la delimitación, M-3), level (nivel de Go: country, 1, 2, 3, local), parentCode y los códigos de Go
// (alpha2/alpha3, nutsCode, lauCode/ineCode). `hasChildren` dice si
// el elemento tiene un nivel inferior con geometría (sólo España, y los municipios sólo de dos
// provincias). Los códigos casan con demo/data/dataset.mjs (hay una prueba).
//
// Uso: `node scripts/gen-geo.mjs` (escribe en demo/public/geo) o `--check` (falla si difiere).
import { mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync, existsSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { COUNTRY_SEEDS } from '../demo/data/countries.mjs';
import { MUNICIPALITIES, SHORT_NAMES, SPAIN_NUTS2, SPAIN_NUTS3 } from '../demo/data/spain.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
export const GEO_DIR = path.resolve(here, '../demo/public/geo');

/** Radio (grados) de cada clase de tamaño en el mapa del mundo. */
const RADIUS = { S: 0.9, M: 1.9, L: 3.4, X: 6.5 };

/** Origen y tamaño de celda del cartograma de España (grados de pseudo-longitud/latitud). */
const GRID = { lon0: -9.8, lat0: 44, cellW: 1.5, cellH: 1.1, gap: 0.05 };

const round = (n) => Math.round(n * 1000) / 1000;

function hash(text) {
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i += 1) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

/** Octógono irregular (determinista por código) en sentido antihorario, anillo cerrado. */
function octagon(code, lon, lat, radius) {
  const h = hash(code);
  const start = ((h % 45) * Math.PI) / 180;
  const ring = [];
  for (let i = 0; i < 8; i += 1) {
    const jitter = 0.88 + (((h >>> (i * 3)) & 7) / 7) * 0.24;
    const angle = start + (i * Math.PI) / 4;
    ring.push([round(lon + Math.cos(angle) * radius * jitter), round(lat + Math.sin(angle) * radius * jitter * 0.8)]);
  }
  ring.push(ring[0]);
  return ring;
}

/** Rectángulo antihorario con un pequeño margen entre vecinos. */
function rect(lon0, lat0, lon1, lat1, gap) {
  const x0 = lon0 + gap;
  const x1 = lon1 - gap;
  const y0 = lat1 + gap; // lat1 < lat0 (el norte arriba)
  const y1 = lat0 - gap;
  return [[round(x0), round(y0)], [round(x1), round(y0)], [round(x1), round(y1)], [round(x0), round(y1)], [round(x0), round(y0)]];
}

/**
 * Reparte n teselas en filas dentro de un rectángulo (lon0, lat0 arriba-izquierda; lon1, lat1
 * abajo-derecha): devuelve n rectángulos [lon0, lat0, lon1, lat1] en orden de lectura.
 */
function partition(box, n) {
  const [lon0, lat0, lon1, lat1] = box;
  const width = lon1 - lon0;
  const height = lat0 - lat1;
  const columns = Math.max(1, Math.ceil(Math.sqrt((n * width) / height)));
  const rows = Math.ceil(n / columns);
  const perRow = Array.from({ length: rows }, (_, r) => Math.floor(n / rows) + (r < n % rows ? 1 : 0));
  const tiles = [];
  perRow.forEach((count, r) => {
    const top = lat0 - (height * r) / rows;
    const bottom = lat0 - (height * (r + 1)) / rows;
    for (let c = 0; c < count; c += 1) {
      tiles.push([lon0 + (width * c) / count, top, lon0 + (width * (c + 1)) / count, bottom]);
    }
  });
  return tiles;
}

/** Separa los círculos que se solapan, de forma determinista (orden fijo, iteraciones fijas). */
function relax(items) {
  const iterations = 240;
  const margin = 0.2;
  for (let k = 0; k < iterations; k += 1) {
    for (let i = 0; i < items.length; i += 1) {
      for (let j = i + 1; j < items.length; j += 1) {
        const a = items[i];
        const b = items[j];
        const dx = b.lon - a.lon;
        const dy = (b.lat - a.lat) * 1.25;
        const dist = Math.hypot(dx, dy) || 0.001;
        const wanted = (a.radius + b.radius) * 1.12 + margin;
        if (dist < wanted) {
          const push = (wanted - dist) / 2;
          const ux = dx / dist;
          const uy = dy / dist;
          a.lon -= ux * push;
          a.lat -= (uy * push) / 1.25;
          b.lon += ux * push;
          b.lat += (uy * push) / 1.25;
        }
      }
    }
  }
}

function bbox(features) {
  let minX = Infinity;
  let minY = Infinity;
  let maxX = -Infinity;
  let maxY = -Infinity;
  for (const f of features) {
    for (const [x, y] of f.geometry.coordinates[0]) {
      minX = Math.min(minX, x);
      minY = Math.min(minY, y);
      maxX = Math.max(maxX, x);
      maxY = Math.max(maxY, y);
    }
  }
  return [round(minX), round(minY), round(maxX), round(maxY)];
}

/** Con nombre corto declarado (`SHORT_NAMES`), lo añade a las propiedades justo detrás del nombre. */
function withShortName(properties) {
  const short = SHORT_NAMES[properties.code];
  if (!short || short === properties.name) return properties;
  const out = {};
  for (const [key, value] of Object.entries(properties)) {
    out[key] = value;
    if (key === 'name') out.shortName = short;
  }
  return out;
}

const feature = (properties, ring) => ({
  type: 'Feature',
  properties: withShortName(properties),
  geometry: { type: 'Polygon', coordinates: [ring] },
});

/** Una colección con un elemento por línea (diferencias legibles en git). */
function serialize(features) {
  return `{"type":"FeatureCollection","features":[\n${features.map((f) => JSON.stringify(f)).join(',\n')}\n]}\n`;
}

/**
 * Genera todos los ficheros en memoria.
 * @returns {Map<string, string>} nombre de fichero → contenido
 */
export function buildGeo() {
  const files = new Map();
  const index = [];
  const add = (name, level, code, featureLevel, features) => {
    files.set(name, serialize(features));
    index.push({ file: name, level, code, featureLevel, features: features.length, bbox: bbox(features) });
  };

  // Mundo.
  const items = COUNTRY_SEEDS.map((c) => ({ seed: c, lon: c.lon, lat: c.lat, radius: RADIUS[c.size] }));
  relax(items);
  const world = items.map(({ seed: c, lon, lat, radius }) =>
    feature(
      { code: c.alpha2, name: c.name, level: 'country', parentCode: '', alpha2: c.alpha2, alpha3: c.alpha3, hasChildren: c.alpha2 === 'ES' },
      octagon(c.alpha2, lon, lat, radius),
    ),
  );
  add('world.geojson', 'world', null, 'country', world);

  // España: comunidades en un cartograma en rejilla.
  const cellBox = (cell) => [
    GRID.lon0 + cell.x * GRID.cellW,
    GRID.lat0 - cell.y * GRID.cellH,
    GRID.lon0 + (cell.x + cell.w) * GRID.cellW,
    GRID.lat0 - (cell.y + cell.h) * GRID.cellH,
  ];
  const regionsWithChildren = new Set(SPAIN_NUTS3.map((p) => p.nuts2));
  add(
    'country-ES.geojson',
    'country',
    'ES',
    '2',
    SPAIN_NUTS2.map((r) => {
      const b = cellBox(r.cell);
      return feature(
        { code: r.code, name: r.name, level: '2', parentCode: 'ES', nutsCode: r.code, hasChildren: regionsWithChildren.has(r.code) },
        rect(b[0], b[1], b[2], b[3], GRID.gap),
      );
    }),
  );

  // Provincias e islas de cada comunidad, y municipios de la muestra.
  const provinceBox = new Map();
  for (const region of SPAIN_NUTS2) {
    const provinces = SPAIN_NUTS3.filter((p) => p.nuts2 === region.code);
    const tiles = partition(cellBox(region.cell), provinces.length);
    const features = provinces.map((p, i) => {
      const t = tiles[i];
      provinceBox.set(p.code, t);
      return feature(
        { code: p.code, name: p.name, level: '3', parentCode: region.code, nutsCode: p.code, ineCode: p.ine, hasChildren: p.code in MUNICIPALITIES },
        rect(t[0], t[1], t[2], t[3], GRID.gap / 2),
      );
    });
    add(`region-${region.code}.geojson`, 'region', region.code, '3', features);
  }
  for (const [nuts3, list] of Object.entries(MUNICIPALITIES)) {
    const tiles = partition(provinceBox.get(nuts3), list.length);
    const features = list.map((m, i) => {
      const t = tiles[i];
      return feature(
        { code: m.ine, name: m.name, level: 'local', parentCode: nuts3, lauCode: m.ine, ineCode: m.ine },
        rect(t[0], t[1], t[2], t[3], GRID.gap / 4),
      );
    });
    add(`city-${nuts3}.geojson`, 'city', nuts3, 'local', features);
  }

  files.set(
    'index.json',
    `${JSON.stringify(
      {
        version: 1,
        schematic: true,
        notice: 'Geometría esquemática de demostración: no es cartografía. Generada por scripts/gen-geo.mjs.',
        fileStem: 'world | <nivel>-<código del padre>: country-ES, region-ES30, city-ES300',
        levels: ['world', 'country', 'region', 'city'],
        files: index,
      },
      null,
      2,
    )}\n`,
  );
  return files;
}

/** Compara lo generado con lo que hay en disco; devuelve las diferencias. */
export function diffGeo(files = buildGeo(), dir = GEO_DIR) {
  const problems = [];
  for (const [name, content] of files) {
    const file = path.join(dir, name);
    if (!existsSync(file)) problems.push(`falta ${name}`);
    else if (readFileSync(file, 'utf8').replace(/\r\n/g, '\n') !== content) problems.push(`difiere ${name}`);
  }
  if (existsSync(dir)) {
    for (const name of readdirSync(dir)) {
      if ((name.endsWith('.geojson') || name === 'index.json') && !files.has(name)) problems.push(`sobra ${name}`);
    }
  }
  return problems;
}

/** Escribe los ficheros (y borra los .geojson que ya no se generan). */
export function writeGeo(files = buildGeo(), dir = GEO_DIR) {
  mkdirSync(dir, { recursive: true });
  for (const name of readdirSync(dir)) {
    if (name.endsWith('.geojson') && !files.has(name)) rmSync(path.join(dir, name));
  }
  for (const [name, content] of files) writeFileSync(path.join(dir, name), content);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const files = buildGeo();
  if (process.argv.includes('--check')) {
    const problems = diffGeo(files);
    if (problems.length > 0) {
      console.error(`Geodatos desfasados:\n- ${problems.join('\n- ')}\nEjecuta \`npm run geo:build\`.`);
      process.exit(1);
    }
    console.log(`Geodatos al día (${files.size} ficheros).`);
  } else {
    writeGeo(files);
    console.log(`Geodatos escritos en ${path.relative(process.cwd(), GEO_DIR) || '.'}: ${files.size} ficheros.`);
  }
}
