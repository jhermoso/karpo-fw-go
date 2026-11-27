// El .pmtiles de la demostración: se lee con la clase `PMTiles` del paquete `pmtiles` (cabecera,
// metadatos, una tesela z0 con la capa `boundaries`) y se comprueba el codificador MVT y el escritor.
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { PMTiles, zxyToTileId as libraryTileId, type RangeResponse, type Source } from 'pmtiles';
import { describe, expect, it } from 'vitest';
// @ts-expect-error -- script de Node en JS sin declaraciones de tipos
import * as builder from '../../scripts/build-pmtiles.mjs';

const ROOT = resolve(import.meta.dirname, '../..');
const FILE = resolve(ROOT, 'demo/public/geo/karpo-demo.pmtiles');

function memorySource(bytes: Uint8Array): Source {
  return {
    getKey: () => 'memory',
    getBytes: async (offset: number, length: number): Promise<RangeResponse> => ({
      data: bytes.buffer.slice(bytes.byteOffset + offset, bytes.byteOffset + offset + length) as ArrayBuffer,
    }),
  };
}

// --- Lector mínimo de MVT (sólo para comprobar lo que escribe el codificador) ---------------------

interface DecodedFeature {
  type: number;
  properties: Record<string, string>;
  rings: [number, number][][];
}
interface DecodedLayer {
  name: string;
  extent: number;
  version: number;
  features: DecodedFeature[];
}

function readVarint(bytes: Uint8Array, at: { i: number }): number {
  let result = 0;
  let shift = 0;
  for (;;) {
    const byte = bytes[at.i++]!;
    result += (byte & 0x7f) * 2 ** shift;
    if ((byte & 0x80) === 0) return result;
    shift += 7;
  }
}

function fields(bytes: Uint8Array): { field: number; wire: number; value: number | Uint8Array }[] {
  const at = { i: 0 };
  const out: { field: number; wire: number; value: number | Uint8Array }[] = [];
  while (at.i < bytes.length) {
    const key = readVarint(bytes, at);
    const field = key >> 3;
    const wire = key & 7;
    if (wire === 0) out.push({ field, wire, value: readVarint(bytes, at) });
    else if (wire === 2) {
      const length = readVarint(bytes, at);
      out.push({ field, wire, value: bytes.slice(at.i, at.i + length) });
      at.i += length;
    } else throw new Error(`tipo de campo ${wire} no esperado`);
  }
  return out;
}

function packed(bytes: Uint8Array): number[] {
  const at = { i: 0 };
  const out: number[] = [];
  while (at.i < bytes.length) out.push(readVarint(bytes, at));
  return out;
}

const unzigzag = (n: number) => (n >> 1) ^ -(n & 1);

function decodeGeometry(commands: number[]): [number, number][][] {
  const rings: [number, number][][] = [];
  let x = 0;
  let y = 0;
  let i = 0;
  let ring: [number, number][] = [];
  while (i < commands.length) {
    const command = commands[i++]! & 7;
    const count = commands[i - 1]! >> 3;
    if (command === 7) {
      rings.push(ring);
      ring = [];
      continue;
    }
    for (let k = 0; k < count; k += 1) {
      x += unzigzag(commands[i++]!);
      y += unzigzag(commands[i++]!);
      ring.push([x, y]);
    }
  }
  return rings;
}

function decodeTile(bytes: Uint8Array): DecodedLayer[] {
  return fields(bytes)
    .filter((f) => f.field === 3)
    .map((layerField) => {
      const parts = fields(layerField.value as Uint8Array);
      const keys = parts.filter((p) => p.field === 3).map((p) => Buffer.from(p.value as Uint8Array).toString('utf8'));
      const values = parts
        .filter((p) => p.field === 4)
        .map((p) => Buffer.from(fields(p.value as Uint8Array)[0]!.value as Uint8Array).toString('utf8'));
      const features = parts
        .filter((p) => p.field === 2)
        .map((featureField) => {
          const feature = fields(featureField.value as Uint8Array);
          const tags = packed(feature.find((f) => f.field === 2)!.value as Uint8Array);
          const properties: Record<string, string> = {};
          for (let i = 0; i < tags.length; i += 2) properties[keys[tags[i]!]!] = values[tags[i + 1]!]!;
          return {
            type: feature.find((f) => f.field === 3)!.value as number,
            properties,
            rings: decodeGeometry(packed(feature.find((f) => f.field === 4)!.value as Uint8Array)),
          };
        });
      return {
        name: Buffer.from(parts.find((p) => p.field === 1)!.value as Uint8Array).toString('utf8'),
        extent: parts.find((p) => p.field === 5)!.value as number,
        version: parts.find((p) => p.field === 15)!.value as number,
        features,
      };
    });
}

const area = (ring: [number, number][]) => builder.signedArea(ring) as number;

// ------------------------------------------------------------------------------------------------

describe('karpo-demo.pmtiles leído con la clase PMTiles', () => {
  const bytes = new Uint8Array(readFileSync(FILE));
  const archive = new PMTiles(memorySource(bytes));

  it('la cabecera es PMTiles v3, MVT sin compresión, zooms 0 a 8', async () => {
    const header = await archive.getHeader();
    expect(header.specVersion).toBe(3);
    expect(header.tileType).toBe(1); // MVT
    expect(header.tileCompression).toBe(1); // none
    expect(header.internalCompression).toBe(1); // none
    expect(header.clustered).toBe(true);
    expect(header.minZoom).toBe(0);
    expect(header.maxZoom).toBe(8);
    expect(header.numAddressedTiles).toBeGreaterThan(100);
    expect(header.numTileEntries).toBe(header.numAddressedTiles);
    expect(header.minLon).toBeLessThan(-170);
    expect(header.maxLon).toBeGreaterThan(170);
    expect(header.rootDirectoryOffset + header.rootDirectoryLength).toBeLessThanOrEqual(16384);
  });

  it('los metadatos declaran la capa boundaries y avisan de que no es cartografía', async () => {
    const metadata = (await archive.getMetadata()) as {
      format: string;
      attribution: string;
      vector_layers: { id: string; fields: Record<string, string> }[];
    };
    expect(metadata.format).toBe('pbf');
    expect(metadata.vector_layers.map((l) => l.id)).toEqual(['boundaries']);
    expect(Object.keys(metadata.vector_layers[0]!.fields)).toEqual(expect.arrayContaining(['code', 'name', 'level', 'parentCode']));
    expect(metadata.attribution).toMatch(/esquemática/);
  });

  it('la tesela z0 trae la capa boundaries con los países y sus propiedades', async () => {
    const tile = await archive.getZxy(0, 0, 0);
    expect(tile).toBeDefined();
    const layers = decodeTile(new Uint8Array(tile!.data));
    expect(layers.map((l) => l.name)).toEqual(['boundaries']);
    const [layer] = layers;
    expect(layer!.extent).toBe(4096);
    expect(layer!.version).toBe(2);
    expect(layer!.features.filter((f) => f.properties.level === 'country').length).toBeGreaterThanOrEqual(240);
    const spain = layer!.features.find((f) => f.properties.code === 'ES')!;
    expect(spain.properties).toMatchObject({ code: 'ES', level: 'country', alpha2: 'ES', hasChildren: 'true' });
    expect(spain.properties.name).toBeTruthy();
    // Sólo polígonos; el anillo exterior con orientación positiva (fórmula del agrimensor).
    expect(layer!.features.every((f) => f.type === 3)).toBe(true);
    for (const feature of layer!.features) {
      expect(feature.rings.length).toBeGreaterThan(0);
      expect(area(feature.rings[0]!)).toBeGreaterThan(0);
      for (const ring of feature.rings) {
        for (const [x, y] of ring) {
          expect(x).toBeGreaterThanOrEqual(-builder.BUFFER);
          expect(x).toBeLessThanOrEqual(4096 + builder.BUFFER);
          expect(y).toBeGreaterThanOrEqual(-builder.BUFFER);
          expect(y).toBeLessThanOrEqual(4096 + builder.BUFFER);
        }
      }
    }
  });

  it('cada nivel está en sus zooms: los de detalle desde z0 (para poder abrirlos por URL) y los países hasta z6', async () => {
    const lon = -3.7;
    const lat = 40.4;
    const tileAt = async (z: number) => {
      const [wx, wy] = builder.project([lon, lat]) as [number, number];
      const tile = await archive.getZxy(z, Math.floor(wx * 2 ** z), Math.floor(wy * 2 ** z));
      return tile ? decodeTile(new Uint8Array(tile.data))[0]!.features : [];
    };
    const z0 = await tileAt(0);
    expect(z0.some((f) => f.properties.level === 'country' && f.properties.code === 'ES')).toBe(true);
    expect(z0.some((f) => f.properties.level === '2' && f.properties.parentCode === 'ES')).toBe(true);
    const z4 = await tileAt(4);
    expect(z4.some((f) => f.properties.level === '2' && f.properties.code === 'ES30')).toBe(true);
    expect(z4.some((f) => f.properties.level === 'country')).toBe(true);
    const z7 = await tileAt(7);
    expect(z7.some((f) => f.properties.level === '3' && f.properties.parentCode === 'ES30')).toBe(true);
    expect(z7.some((f) => f.properties.level === 'local' && f.properties.parentCode === 'ES300')).toBe(true);
    expect(z7.some((f) => f.properties.level === 'country')).toBe(false);
  });

  it('el fichero versionado es exactamente lo que genera el script (determinista)', () => {
    const output = execFileSync(process.execPath, [resolve(ROOT, 'scripts/build-pmtiles.mjs'), '--check'], { encoding: 'utf8' });
    expect(output).toMatch(/al día/);
    const again = builder.buildFromDir(resolve(ROOT, 'demo/public/geo')) as Uint8Array;
    expect(Buffer.compare(Buffer.from(again), Buffer.from(bytes))).toBe(0);
  });
});

describe('piezas del escritor', () => {
  it('el identificador de Hilbert coincide con el del paquete pmtiles', () => {
    for (let z = 0; z <= 10; z += 1) {
      const n = 2 ** z;
      for (const [x, y] of [
        [0, 0],
        [n - 1, 0],
        [0, n - 1],
        [n - 1, n - 1],
        [Math.floor(n / 3), Math.floor(n / 2)],
        [Math.floor(n / 2), Math.floor(n / 5)],
      ] as const) {
        expect(builder.zxyToTileId(z, x, y)).toBe(libraryTileId(z, x, y));
      }
    }
  });

  it('recorta anillos al rectángulo y descarta lo que queda fuera', () => {
    const square = [
      [10, 10],
      [90, 10],
      [90, 90],
      [10, 90],
      [10, 10],
    ];
    expect(builder.clipRing(square, 0, 100)).toHaveLength(4);
    const wide = [
      [-50, 10],
      [150, 10],
      [150, 90],
      [-50, 90],
      [-50, 10],
    ];
    const clipped = builder.clipRing(wide, 0, 100) as [number, number][];
    expect(clipped.length).toBeGreaterThanOrEqual(4);
    for (const [x, y] of clipped) {
      expect(x).toBeGreaterThanOrEqual(0);
      expect(x).toBeLessThanOrEqual(100);
      expect(y).toBeGreaterThanOrEqual(0);
      expect(y).toBeLessThanOrEqual(100);
    }
    expect(Math.abs(area(clipped))).toBeCloseTo(100 * 80, 5);
    const outside = [
      [200, 200],
      [300, 200],
      [300, 300],
      [200, 300],
    ];
    expect(builder.clipRing(outside, 0, 100)).toEqual([]);
  });

  it('un MVT codificado se lee igual: propiedades, tipo polígono y orientación', () => {
    const ring = [
      [10, 10],
      [200, 10],
      [200, 300],
      [10, 300],
    ];
    const hole = [
      [50, 50],
      [50, 100],
      [100, 100],
      [100, 50],
    ];
    const bytes = builder.encodeTile([
      { rings: [ring, hole], properties: { code: 'X1', name: 'Uno', level: '2', vacio: '' } },
      { rings: [ring], properties: { code: 'X2', name: 'Uno', level: '2' } },
    ]) as Uint8Array;
    const [layer] = decodeTile(bytes);
    expect(layer!.name).toBe('boundaries');
    expect(layer!.features).toHaveLength(2);
    expect(layer!.features[0]!.properties).toEqual({ code: 'X1', name: 'Uno', level: '2' });
    expect(layer!.features[0]!.rings).toEqual([ring, hole]);
    expect(layer!.features[1]!.properties.code).toBe('X2');
  });

  it('los anillos de una tesela salen con el exterior positivo y los huecos negativos', () => {
    const polygon = [
      [
        [0, 0],
        [10, 0],
        [10, 10],
        [0, 10],
        [0, 0],
      ],
      [
        [3, 3],
        [3, 6],
        [6, 6],
        [6, 3],
        [3, 3],
      ],
    ];
    const rings = builder.tileRings([polygon], 0, 0, 0) as [number, number][][];
    expect(rings).toHaveLength(2);
    expect(area(rings[0]!)).toBeGreaterThan(0);
    expect(area(rings[1]!)).toBeLessThan(0);
  });

  it('con muchas teselas el directorio pasa a hojas y la raíz cabe en los primeros 16 KiB', () => {
    const entries = Array.from({ length: 40_000 }, (_unused, i) => ({
      tileId: i * 2,
      offset: i * 997,
      length: 997,
      runLength: 1,
    }));
    const { root, leaves } = builder.buildDirectories(entries) as { root: Uint8Array; leaves: Uint8Array };
    expect(root.length).toBeLessThanOrEqual(16384 - 127);
    expect(leaves.length).toBeGreaterThan(0);
    const small = builder.buildDirectories(entries.slice(0, 50)) as { root: Uint8Array; leaves: Uint8Array };
    expect(small.leaves.length).toBe(0);
  });
});
