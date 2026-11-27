// Genera `demo/public/geo/karpo-demo.pmtiles` desde los GeoJSON esquemáticos de la demostración
// (`demo/public/geo/*.geojson`, hechos por `scripts/gen-geo.mjs`). PLAN §4.13.
//
//   node scripts/build-pmtiles.mjs            escribe el .pmtiles
//   node scripts/build-pmtiles.mjs --check    falla si el fichero versionado no es el que saldría ahora
//   node scripts/build-pmtiles.mjs --in <carpeta> --out <fichero>
//
// Sin dependencias: codificador MVT mínimo (sólo polígonos y propiedades de texto, una capa
// `boundaries`) y escritor de PMTiles v3 (compresión `none`, teselas MVT, zooms 0..8). La salida es
// determinista (sin fechas ni orden dependiente del sistema de ficheros).
//
// Cada nivel del mapa vive en su rango de zooms (como `minzoom`/`maxzoom` de tippecanoe). Todos los
// niveles de detalle (comunidades, provincias, municipios) empiezan en z0: la vista del mapa lee
// de las teselas cargadas las entidades que tiene que listar y encuadrar, y con la cámara en el
// mundo sólo las teselas bajas están cargadas (un nivel que no esté en ellas no se podría abrir por
// URL directa). Los países, en cambio, dejan de existir a partir de z6: ya no se miran.
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(HERE, '..');

export const LAYER = 'boundaries';
export const EXTENT = 4096;
/** Margen alrededor de cada tesela (en unidades de tesela) para que los bordes no se corten. */
export const BUFFER = 64;
export const MIN_ZOOM = 0;
export const MAX_ZOOM = 8;
/** Rango de zooms por nivel de la delimitación (`properties.level`). */
export const LEVEL_ZOOMS = {
  country: [0, 6],
  1: [0, 8],
  2: [0, 8],
  3: [0, 8],
  local: [0, 8],
};
/** Propiedades que viajan en la tesela (todas de texto). */
export const PROPERTY_KEYS = ['code', 'name', 'shortName', 'level', 'parentCode', 'hasChildren', 'alpha2', 'alpha3', 'nutsCode', 'lauCode', 'ineCode'];

const MAX_LAT = 85.0511287798066;

// ---------------------------------------------------------------------------------------------
// Protobuf mínimo
// ---------------------------------------------------------------------------------------------

/** Escritor de bytes con varints. */
export class ByteWriter {
  constructor() {
    this.bytes = [];
  }

  varint(value) {
    let v = BigInt(value);
    if (v < 0n) throw new Error('varint negativo');
    while (v > 0x7fn) {
      this.bytes.push(Number(v & 0x7fn) | 0x80);
      v >>= 7n;
    }
    this.bytes.push(Number(v));
    return this;
  }

  tag(field, wireType) {
    return this.varint((field << 3) | wireType);
  }

  /** Campo de tipo varint. */
  uint(field, value) {
    return this.tag(field, 0).varint(value);
  }

  /** Campo con longitud (cadena, mensaje anidado o lista empaquetada). */
  bytesField(field, payload) {
    this.tag(field, 2).varint(payload.length);
    for (const byte of payload) this.bytes.push(byte);
    return this;
  }

  string(field, text) {
    return this.bytesField(field, Buffer.from(text, 'utf8'));
  }

  packed(field, numbers) {
    const inner = new ByteWriter();
    for (const n of numbers) inner.varint(n);
    return this.bytesField(field, inner.bytes);
  }

  toUint8Array() {
    return Uint8Array.from(this.bytes);
  }
}

const zigzag = (n) => (n << 1) ^ (n >> 31);

// ---------------------------------------------------------------------------------------------
// Proyección y recorte
// ---------------------------------------------------------------------------------------------

/** Mercator a coordenadas de mundo 0..1. */
export function project([lon, lat]) {
  const clamped = Math.max(-MAX_LAT, Math.min(MAX_LAT, lat));
  const sin = Math.sin((clamped * Math.PI) / 180);
  return [(lon + 180) / 360, 0.5 - Math.log((1 + sin) / (1 - sin)) / (4 * Math.PI)];
}

/** Anillo en unidades de tesela (0..EXTENT) de la tesela z/x/y; sin redondear. */
export function toTileCoords(ring, z, x, y) {
  const scale = 2 ** z;
  return ring.map((point) => {
    const [wx, wy] = project(point);
    return [(wx * scale - x) * EXTENT, (wy * scale - y) * EXTENT];
  });
}

/** Sutherland-Hodgman contra un rectángulo. Devuelve un anillo abierto (sin repetir el primer punto). */
export function clipRing(ring, min, max) {
  let output = ring.length > 1 && ring[0][0] === ring[ring.length - 1][0] && ring[0][1] === ring[ring.length - 1][1] ? ring.slice(0, -1) : ring.slice();
  const edges = [
    { inside: (p) => p[0] >= min, cut: (a, b) => [min, a[1] + ((b[1] - a[1]) * (min - a[0])) / (b[0] - a[0])] },
    { inside: (p) => p[0] <= max, cut: (a, b) => [max, a[1] + ((b[1] - a[1]) * (max - a[0])) / (b[0] - a[0])] },
    { inside: (p) => p[1] >= min, cut: (a, b) => [a[0] + ((b[0] - a[0]) * (min - a[1])) / (b[1] - a[1]), min] },
    { inside: (p) => p[1] <= max, cut: (a, b) => [a[0] + ((b[0] - a[0]) * (max - a[1])) / (b[1] - a[1]), max] },
  ];
  for (const edge of edges) {
    const input = output;
    output = [];
    if (input.length === 0) break;
    let previous = input[input.length - 1];
    for (const current of input) {
      if (edge.inside(current)) {
        if (!edge.inside(previous)) output.push(edge.cut(previous, current));
        output.push(current);
      } else if (edge.inside(previous)) {
        output.push(edge.cut(previous, current));
      }
      previous = current;
    }
  }
  return output;
}

/** Área con signo por la fórmula del agrimensor (positiva = anillo exterior en el MVT). */
export function signedArea(ring) {
  let sum = 0;
  for (let i = 0; i < ring.length; i += 1) {
    const a = ring[i];
    const b = ring[(i + 1) % ring.length];
    sum += a[0] * b[1] - b[0] * a[1];
  }
  return sum / 2;
}

function roundRing(ring) {
  const out = [];
  for (const [px, py] of ring) {
    const point = [Math.round(px), Math.round(py)];
    const last = out[out.length - 1];
    if (!last || last[0] !== point[0] || last[1] !== point[1]) out.push(point);
  }
  while (out.length > 1 && out[0][0] === out[out.length - 1][0] && out[0][1] === out[out.length - 1][1]) out.pop();
  return out;
}

/** Polígonos de una geometría GeoJSON: lista de polígonos, cada uno una lista de anillos. */
export function polygonsOf(geometry) {
  if (!geometry) return [];
  if (geometry.type === 'Polygon') return [geometry.coordinates];
  if (geometry.type === 'MultiPolygon') return geometry.coordinates;
  return [];
}

/** Anillos de la geometría ya recortados y orientados para la tesela z/x/y (vacío si no la toca). */
export function tileRings(polygons, z, x, y) {
  const out = [];
  for (const polygon of polygons) {
    polygon.forEach((ring, index) => {
      const clipped = roundRing(clipRing(toTileCoords(ring, z, x, y), -BUFFER, EXTENT + BUFFER));
      if (clipped.length < 3) return;
      const area = signedArea(clipped);
      if (area === 0) return;
      const exterior = index === 0;
      const wanted = exterior ? area > 0 : area < 0;
      out.push(wanted ? clipped : clipped.reverse());
    });
  }
  return out;
}

// ---------------------------------------------------------------------------------------------
// MVT
// ---------------------------------------------------------------------------------------------

function encodeGeometry(rings) {
  const commands = [];
  let cx = 0;
  let cy = 0;
  for (const ring of rings) {
    commands.push((1 & 7) | (1 << 3));
    commands.push(zigzag(ring[0][0] - cx), zigzag(ring[0][1] - cy));
    cx = ring[0][0];
    cy = ring[0][1];
    commands.push((2 & 7) | ((ring.length - 1) << 3));
    for (let i = 1; i < ring.length; i += 1) {
      commands.push(zigzag(ring[i][0] - cx), zigzag(ring[i][1] - cy));
      cx = ring[i][0];
      cy = ring[i][1];
    }
    commands.push((7 & 7) | (1 << 3));
  }
  return commands;
}

/**
 * Teselas MVT con una capa `boundaries`. `features` = [{ rings, properties }] (propiedades de texto).
 * Devuelve los bytes de la tesela.
 */
export function encodeTile(features) {
  const keys = [];
  const values = [];
  const keyIndex = new Map();
  const valueIndex = new Map();
  const index = (map, list, item) => {
    if (!map.has(item)) {
      map.set(item, list.length);
      list.push(item);
    }
    return map.get(item);
  };

  const layer = new ByteWriter();
  layer.uint(15, 2);
  layer.string(1, LAYER);
  for (const feature of features) {
    const tags = [];
    for (const [key, value] of Object.entries(feature.properties)) {
      if (value === undefined || value === null || value === '') continue;
      tags.push(index(keyIndex, keys, key), index(valueIndex, values, String(value)));
    }
    const encoded = new ByteWriter();
    encoded.packed(2, tags);
    encoded.uint(3, 3);
    encoded.packed(4, encodeGeometry(feature.rings));
    layer.bytesField(2, encoded.bytes);
  }
  for (const key of keys) layer.string(3, key);
  for (const value of values) layer.bytesField(4, new ByteWriter().string(1, value).bytes);
  layer.uint(5, EXTENT);
  return new ByteWriter().bytesField(3, layer.bytes).toUint8Array();
}

// ---------------------------------------------------------------------------------------------
// PMTiles v3
// ---------------------------------------------------------------------------------------------

/** Identificador de tesela en la curva de Hilbert (especificación PMTiles v3). */
export function zxyToTileId(z, x, y) {
  let acc = 0;
  for (let t = 0; t < z; t += 1) acc += 4 ** t;
  const n = 2 ** z;
  let d = 0;
  let tx = x;
  let ty = y;
  for (let s = n / 2; s > 0; s /= 2) {
    const rx = (tx & s) > 0 ? 1 : 0;
    const ry = (ty & s) > 0 ? 1 : 0;
    d += s * s * ((3 * rx) ^ ry);
    if (ry === 0) {
      if (rx === 1) {
        tx = s - 1 - tx;
        ty = s - 1 - ty;
      }
      const swap = tx;
      tx = ty;
      ty = swap;
    }
  }
  return acc + d;
}

/** Directorio serializado (entradas ordenadas por `tileId`). */
export function serializeDirectory(entries) {
  const writer = new ByteWriter();
  writer.varint(entries.length);
  let last = 0;
  for (const entry of entries) {
    writer.varint(entry.tileId - last);
    last = entry.tileId;
  }
  for (const entry of entries) writer.varint(entry.runLength);
  for (const entry of entries) writer.varint(entry.length);
  entries.forEach((entry, i) => {
    const previous = entries[i - 1];
    if (i > 0 && entry.offset === previous.offset + previous.length) writer.varint(0);
    else writer.varint(entry.offset + 1);
  });
  return writer.toUint8Array();
}

const HEADER_SIZE = 127;
const ROOT_LIMIT = 16384 - HEADER_SIZE;

function concat(parts) {
  const total = parts.reduce((sum, part) => sum + part.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const part of parts) {
    out.set(part, offset);
    offset += part.length;
  }
  return out;
}

/** Raíz y hojas: todo en la raíz si cabe en los primeros 16 KiB; si no, hojas de tamaño creciente. */
export function buildDirectories(entries) {
  const single = serializeDirectory(entries);
  if (single.length <= ROOT_LIMIT) return { root: single, leaves: new Uint8Array(0) };
  for (let leafSize = 512; leafSize <= entries.length; leafSize *= 2) {
    const leafParts = [];
    const rootEntries = [];
    let offset = 0;
    for (let i = 0; i < entries.length; i += leafSize) {
      const chunk = entries.slice(i, i + leafSize);
      const bytes = serializeDirectory(chunk);
      rootEntries.push({ tileId: chunk[0].tileId, offset, length: bytes.length, runLength: 0 });
      leafParts.push(bytes);
      offset += bytes.length;
    }
    const root = serializeDirectory(rootEntries);
    if (root.length <= ROOT_LIMIT) return { root, leaves: concat(leafParts) };
  }
  throw new Error('El directorio de teselas no cabe en la raíz de PMTiles');
}

function writeHeader(h) {
  const buffer = new ArrayBuffer(HEADER_SIZE);
  const view = new DataView(buffer);
  new Uint8Array(buffer).set(Buffer.from('PMTiles', 'ascii'), 0);
  view.setUint8(7, 3);
  const u64 = (offset, value) => view.setBigUint64(offset, BigInt(value), true);
  u64(8, h.rootOffset);
  u64(16, h.rootLength);
  u64(24, h.metadataOffset);
  u64(32, h.metadataLength);
  u64(40, h.leafOffset);
  u64(48, h.leafLength);
  u64(56, h.dataOffset);
  u64(64, h.dataLength);
  u64(72, h.addressedTiles);
  u64(80, h.tileEntries);
  u64(88, h.tileContents);
  view.setUint8(96, 1); // clustered
  view.setUint8(97, 1); // compresión interna: none
  view.setUint8(98, 1); // compresión de teselas: none
  view.setUint8(99, 1); // tipo: MVT
  view.setUint8(100, h.minZoom);
  view.setUint8(101, h.maxZoom);
  const e7 = (value) => Math.round(value * 1e7);
  view.setInt32(102, e7(h.bounds[0]), true);
  view.setInt32(106, e7(h.bounds[1]), true);
  view.setInt32(110, e7(h.bounds[2]), true);
  view.setInt32(114, e7(h.bounds[3]), true);
  view.setUint8(118, h.centerZoom);
  view.setInt32(119, e7(h.center[0]), true);
  view.setInt32(123, e7(h.center[1]), true);
  return new Uint8Array(buffer);
}

// ---------------------------------------------------------------------------------------------
// Del GeoJSON al PMTiles
// ---------------------------------------------------------------------------------------------

/** Rango de teselas (a un zoom) que cubre una caja en grados. */
export function tileRange(bbox, z) {
  const scale = 2 ** z;
  const [wx0, wy0] = project([bbox[0], bbox[3]]);
  const [wx1, wy1] = project([bbox[2], bbox[1]]);
  const clamp = (v) => Math.max(0, Math.min(scale - 1, Math.floor(v)));
  // Margen: una tesela puede necesitar geometría que sólo cae en su búfer.
  const pad = BUFFER / EXTENT;
  return {
    x0: clamp(wx0 * scale - pad),
    x1: clamp(wx1 * scale + pad),
    y0: clamp(wy0 * scale - pad),
    y1: clamp(wy1 * scale + pad),
  };
}

export function bboxOf(polygons) {
  let w = Infinity;
  let s = Infinity;
  let e = -Infinity;
  let n = -Infinity;
  for (const polygon of polygons) {
    for (const ring of polygon) {
      for (const [lon, lat] of ring) {
        w = Math.min(w, lon);
        e = Math.max(e, lon);
        s = Math.min(s, lat);
        n = Math.max(n, lat);
      }
    }
  }
  return [w, s, e, n];
}

/** Todas las entidades de los GeoJSON de una carpeta, en orden de nombre de fichero y de aparición. */
export function readGeoDir(dir) {
  const files = readdirSync(dir)
    .filter((name) => name.endsWith('.geojson'))
    .sort();
  const features = [];
  for (const file of files) {
    const collection = JSON.parse(readFileSync(path.join(dir, file), 'utf8'));
    for (const feature of collection.features ?? []) {
      const polygons = polygonsOf(feature.geometry);
      if (polygons.length === 0) continue;
      const properties = {};
      for (const key of PROPERTY_KEYS) {
        const value = feature.properties?.[key];
        if (value !== undefined && value !== null && value !== '') properties[key] = String(value);
      }
      features.push({ properties, polygons, bbox: bboxOf(polygons), file });
    }
  }
  return features;
}

/** Bytes de un PMTiles v3 con las entidades dadas. */
export function buildPmtiles(features, { name = 'karpo-demo', description = '' } = {}) {
  /** @type {Map<number, Uint8Array>} */
  const tiles = new Map();
  for (let z = MIN_ZOOM; z <= MAX_ZOOM; z += 1) {
    /** @type {Map<string, {x:number,y:number,items:object[]}>} */
    const perTile = new Map();
    for (const feature of features) {
      const range = LEVEL_ZOOMS[feature.properties.level];
      if (!range || z < range[0] || z > range[1]) continue;
      const bounds = tileRange(feature.bbox, z);
      for (let x = bounds.x0; x <= bounds.x1; x += 1) {
        for (let y = bounds.y0; y <= bounds.y1; y += 1) {
          const rings = tileRings(feature.polygons, z, x, y);
          if (rings.length === 0) continue;
          const key = `${x}/${y}`;
          if (!perTile.has(key)) perTile.set(key, { x, y, items: [] });
          perTile.get(key).items.push({ rings, properties: feature.properties });
        }
      }
    }
    for (const { x, y, items } of perTile.values()) tiles.set(zxyToTileId(z, x, y), encodeTile(items));
  }

  const ids = [...tiles.keys()].sort((a, b) => a - b);
  const entries = [];
  const chunks = [];
  let offset = 0;
  for (const tileId of ids) {
    const bytes = tiles.get(tileId);
    entries.push({ tileId, offset, length: bytes.length, runLength: 1 });
    chunks.push(bytes);
    offset += bytes.length;
  }
  const data = concat(chunks);
  const { root, leaves } = buildDirectories(entries);

  const all = features.map((f) => f.bbox);
  const bounds = [
    Math.min(...all.map((b) => b[0])),
    Math.min(...all.map((b) => b[1])),
    Math.max(...all.map((b) => b[2])),
    Math.max(...all.map((b) => b[3])),
  ];
  const metadata = Buffer.from(
    JSON.stringify({
      name,
      description,
      version: '1',
      type: 'overlay',
      format: 'pbf',
      minzoom: String(MIN_ZOOM),
      maxzoom: String(MAX_ZOOM),
      attribution: 'Geometría esquemática de demostración de Karpo (no es cartografía)',
      vector_layers: [
        {
          id: LAYER,
          description: 'Delimitaciones por nivel: country, 2 (comunidades), 3 (provincias) y local (municipios)',
          minzoom: MIN_ZOOM,
          maxzoom: MAX_ZOOM,
          fields: Object.fromEntries(PROPERTY_KEYS.map((key) => [key, 'String'])),
        },
      ],
    }),
    'utf8',
  );

  const rootOffset = HEADER_SIZE;
  const metadataOffset = rootOffset + root.length;
  const leafOffset = metadataOffset + metadata.length;
  const dataOffset = leafOffset + leaves.length;
  const header = writeHeader({
    rootOffset,
    rootLength: root.length,
    metadataOffset,
    metadataLength: metadata.length,
    leafOffset,
    leafLength: leaves.length,
    dataOffset,
    dataLength: data.length,
    addressedTiles: entries.length,
    tileEntries: entries.length,
    tileContents: entries.length,
    minZoom: MIN_ZOOM,
    maxZoom: MAX_ZOOM,
    bounds,
    centerZoom: 2,
    center: [(bounds[0] + bounds[2]) / 2, (bounds[1] + bounds[3]) / 2],
  });
  return concat([header, root, new Uint8Array(metadata), leaves, data]);
}

export const DEFAULT_IN = path.join(ROOT, 'demo/public/geo');
export const DEFAULT_OUT = path.join(ROOT, 'demo/public/geo/karpo-demo.pmtiles');

export function buildFromDir(dir = DEFAULT_IN) {
  return buildPmtiles(readGeoDir(dir), {
    description: 'Geometría esquemática de demostración generada por scripts/gen-geo.mjs: no es cartografía.',
  });
}

function parseArgs(argv) {
  const args = { check: false, in: DEFAULT_IN, out: DEFAULT_OUT };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === '--check') args.check = true;
    else if (arg === '--in') args.in = path.resolve(argv[++i] ?? '');
    else if (arg === '--out') args.out = path.resolve(argv[++i] ?? '');
    else throw new Error(`Argumento desconocido: ${arg}`);
  }
  return args;
}

function main() {
  const args = parseArgs(process.argv.slice(2));
  const bytes = buildFromDir(args.in);
  if (args.check) {
    const current = existsSync(args.out) ? readFileSync(args.out) : null;
    if (!current || Buffer.compare(current, Buffer.from(bytes)) !== 0) {
      console.error(`${path.relative(ROOT, args.out)} no coincide con lo que genera build-pmtiles.mjs: ejecuta \`npm run tiles:build\`.`);
      process.exit(1);
    }
    console.log(`${path.relative(ROOT, args.out)} al día (${bytes.length} bytes).`);
    return;
  }
  mkdirSync(path.dirname(args.out), { recursive: true });
  writeFileSync(args.out, bytes);
  console.log(`${path.relative(ROOT, args.out)}: ${bytes.length} bytes.`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main();
