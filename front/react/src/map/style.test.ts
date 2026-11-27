import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import type { GeoCollection } from './geo';
import { HATCH_IMAGE_PREFIX, HATCH_SIZE, HATCH_STROKE, drawHatch, parseRgb } from './pattern';
import {
  LAYER_ACTIVE,
  LAYER_FILL,
  LAYER_HOVER,
  LAYER_MISSING,
  MAP_TOKENS,
  NO_CODE_FILTER,
  buildMapStyle,
  codeFilter,
  collectUrls,
  fillColorExpression,
  missingFilter,
  resolveMapColors,
  vectorFilter,
  type MapDataSource,
} from './style';

/** Un color distinto por token, como lo devolvería la sonda del tema. */
const probe = (token: string) => `rgb(${token.length}, ${token.charCodeAt(token.length - 1)}, 7)`;
const requested: string[] = [];
const colors = resolveMapColors((token) => {
  requested.push(token);
  return probe(token);
});

const collection: GeoCollection = {
  type: 'FeatureCollection',
  features: [{ type: 'Feature', properties: { code: 'ES' }, geometry: { type: 'Polygon', coordinates: [[[0, 0], [1, 0], [1, 1], [0, 0]]] } }],
};
const geojson: MapDataSource = { kind: 'geojson', data: collection };
const vector: MapDataSource = {
  kind: 'pmtiles',
  url: 'pmtiles://http://localhost:3000/geo/karpo-demo.pmtiles',
  layer: 'boundaries',
  boundaryLevel: '2',
  parentCode: 'ES',
  maxzoom: 8,
};

describe('colores del mapa', () => {
  it('se resuelven con la sonda a partir de variables del tema, ninguna escrita a mano', () => {
    const tokens = [...new Set(requested)].sort();
    expect(tokens).toEqual(
      [
        MAP_TOKENS.background,
        MAP_TOKENS.missing,
        MAP_TOKENS.missingHatch,
        MAP_TOKENS.edge,
        MAP_TOKENS.hoverFill,
        MAP_TOKENS.activeFill,
        MAP_TOKENS.focus,
        ...MAP_TOKENS.sequential,
      ].sort(),
    );
    // Variables del tema; la trama del mapa pide la suave (R3-4) y, si el tema aún no la trae, las de respaldo.
    const themeVariable = /^(--(th|kx)-[\w-]+|(var\(--(th|kx)-[\w-]+, )*var\(--(th|kx)-[\w-]+\)+)$/;
    expect(tokens.every((token) => themeVariable.test(token))).toBe(true);
    expect(MAP_TOKENS.missingHatch).toBe('var(--kx-data-missing-hatch-map, var(--kx-data-missing-hatch, var(--kx-data-missing)))');
    // El mapa usa la trama suave; la marcada es la de la leyenda y no se pide aquí.
    expect(MAP_TOKENS.missingHatch).toContain('--kx-data-missing-hatch-map');
    expect(colors.missingHatch).toBe(probe(MAP_TOKENS.missingHatch));
    expect(colors.sequential).toHaveLength(7);
    expect(colors.background).toBe(probe('--th-surface-canvas-bg'));
    expect(colors.edge).toBe(probe('--th-edge-strong'));
    expect(colors.missing).toBe(probe('--kx-data-missing'));
  });

  it('una sonda que no resuelve (sin tema) deja un color transparente válido', () => {
    const empty = resolveMapColors(() => '');
    expect(empty.background).toBe('rgba(0, 0, 0, 0)');
    expect(empty.sequential.every((c) => c === 'rgba(0, 0, 0, 0)')).toBe(true);
  });
});

describe('estilo de MapLibre construido en ejecución', () => {
  it('con GeoJSON propio: datos en el estilo, sin sprite, sin glifos y sin ninguna URL', () => {
    const style = buildMapStyle({ source: geojson, colors, steps: new Map([['ES', 7]]) });
    expect(style.version).toBe(8);
    // Sin imagen de trama no hay capa de trama (se pide con `hatchImage`).
    expect(style.layers.some((l) => l.id === LAYER_MISSING)).toBe(false);
    expect(style.sprite).toBeUndefined();
    expect(style.glyphs).toBeUndefined();
    expect(style.sources['kx-boundaries']).toMatchObject({ type: 'geojson', data: collection });
    expect(collectUrls(style)).toEqual([]);
    expect(JSON.stringify(style)).not.toMatch(/https?:|\/\//);
    expect(style.layers.map((l) => l.id)).toEqual(['kx-background', 'kx-fill', 'kx-active', 'kx-hover', 'kx-line']);
  });

  it('con teselas PMTiles: una fuente vectorial del propio origen y capas filtradas por nivel y padre', () => {
    const style = buildMapStyle({ source: vector, colors, steps: new Map() });
    expect(style.sources['kx-boundaries']).toEqual({ type: 'vector', url: vector.url, minzoom: 0, maxzoom: 8 });
    expect(collectUrls(style)).toEqual([vector.url]);
    const fill = style.layers.find((l) => l.id === LAYER_FILL) as { filter: unknown; 'source-layer': string };
    expect(fill['source-layer']).toBe('boundaries');
    expect(fill.filter).toEqual(['all', ['==', ['get', 'level'], '2'], ['==', ['get', 'parentCode'], 'ES']]);
    expect(vectorFilter({ ...vector, parentCode: null })).toEqual(['==', ['get', 'level'], '2']);
    // Las capas de resaltado no llevan el filtro de nivel: eligen por código.
    for (const id of [LAYER_ACTIVE, LAYER_HOVER]) {
      const layer = style.layers.find((l) => l.id === id) as { filter: unknown; 'source-layer': string };
      expect(layer['source-layer']).toBe('boundaries');
      expect(layer.filter).toEqual(NO_CODE_FILTER);
    }
    expect(codeFilter('ES30')).toEqual(['==', ['get', 'code'], 'ES30']);
    expect(codeFilter(null)).toEqual(NO_CODE_FILTER);
  });

  it('todos los colores del estilo son los que dio la sonda', () => {
    const style = buildMapStyle({ source: geojson, colors, steps: new Map([['ES', 3], ['FR', 7]]) });
    const text = JSON.stringify(style);
    const allowed = new Set([colors.background, colors.missing, colors.edge, colors.hoverFill, colors.activeFill, ...colors.sequential]);
    const found = [...text.matchAll(/rgba?\([^)]*\)/g)].map((m) => m[0]);
    expect(found.length).toBeGreaterThan(5);
    expect(found.every((color) => allowed.has(color))).toBe(true);
    expect(text).not.toMatch(/#[0-9a-f]{3,8}\b/i);
  });

  it('el relleno agrupa los códigos por paso de la paleta y deja el resto sin dato', () => {
    const steps = new Map([
      ['ES30', 7],
      ['ES51', 7],
      ['ES11', 2],
    ]);
    expect(fillColorExpression(steps, colors)).toEqual([
      'match',
      ['get', 'code'],
      ['ES11'],
      colors.sequential[1],
      ['ES30', 'ES51'],
      colors.sequential[6],
      colors.missing,
    ]);
    // Sin datos todo el mapa es «sin dato», sin expresión.
    expect(fillColorExpression(new Map(), colors)).toBe(colors.missing);
    // Un paso fuera de rango se ajusta a la paleta.
    expect(fillColorExpression(new Map([['X', 99], ['Y', 0]]), colors)).toEqual([
      'match',
      ['get', 'code'],
      ['Y'],
      colors.sequential[0],
      ['X'],
      colors.sequential[6],
      colors.missing,
    ]);
  });

  it('lo que no tiene dato lleva una capa de trama (`fill-pattern`) sobre el relleno y bajo los bordes (M-2)', () => {
    const style = buildMapStyle({ source: geojson, colors, steps: new Map([['ES', 7]]), hatchImage: `${HATCH_IMAGE_PREFIX}-1` });
    expect(style.layers.map((l) => l.id)).toEqual(['kx-background', 'kx-fill', 'kx-missing', 'kx-active', 'kx-hover', 'kx-line']);
    const layer = style.layers.find((l) => l.id === LAYER_MISSING) as { type: string; paint: Record<string, unknown>; filter: unknown };
    expect(layer.type).toBe('fill');
    expect(layer.paint['fill-pattern']).toBe('kx-missing-hatch-1');
    // Sólo los códigos que no están entre los de la paleta.
    expect(layer.filter).toEqual(['match', ['get', 'code'], ['ES'], false, true]);
    // La imagen no es una URL: el estilo sigue sin referencias externas.
    expect(collectUrls(style)).toEqual([]);
  });

  it('el filtro de la trama combina el del nivel (teselas) con «no tiene dato»; sin datos cubre todo el nivel', () => {
    const steps = new Map([['ES30', 7], ['ES11', 2]]);
    expect(missingFilter(vector, steps)).toEqual(['all', vectorFilter(vector as never), ['match', ['get', 'code'], ['ES11', 'ES30'], false, true]]);
    expect(missingFilter(geojson, steps)).toEqual(['match', ['get', 'code'], ['ES11', 'ES30'], false, true]);
    expect(missingFilter(geojson, new Map())).toBeUndefined();
    expect(missingFilter(vector, new Map())).toEqual(vectorFilter(vector as never));
    // Con datos, la capa lleva el filtro del nivel y «sin dato» (también en teselas vectoriales).
    const style = buildMapStyle({ source: vector, colors, steps, hatchImage: 'x' });
    const layer = style.layers.find((l) => l.id === LAYER_MISSING) as { 'source-layer': string; filter: unknown };
    expect(layer['source-layer']).toBe('boundaries');
    expect(layer.filter).toEqual(missingFilter(vector, steps));
  });

  it('un nivel sin ningún dato no lleva capa de trama: no hay clase con la que confundir «sin dato» (R3-4, M-2)', () => {
    for (const source of [geojson, vector]) {
      const none = buildMapStyle({ source, colors, steps: new Map(), hatchImage: 'x' });
      expect(none.layers.map((l) => l.id)).toEqual(['kx-background', 'kx-fill', 'kx-active', 'kx-hover', 'kx-line']);
      expect(none.layers.some((l) => l.id === LAYER_MISSING)).toBe(false);
      // Todo el relleno es «sin dato», liso.
      const fill = none.layers.find((l) => l.id === 'kx-fill') as { paint: Record<string, unknown> };
      expect(fill.paint['fill-color']).toBe(colors.missing);
      // Con un solo dato vuelve la capa.
      const some = buildMapStyle({ source, colors, steps: new Map([['ES', 3]]), hatchImage: 'x' });
      expect(some.layers.some((l) => l.id === LAYER_MISSING)).toBe(true);
    }
  });

  it('con datos la capa de trama pinta con la imagen del token suave: la leyenda y el mapa no comparten color de raya', () => {
    // El estilo sólo conoce el NOMBRE de la imagen; el color de sus rayas sale de `colors.missingHatch` (el token suave).
    expect(colors.missingHatch).toBe(probe(MAP_TOKENS.missingHatch));
    expect(colors.missingHatch).not.toBe(probe('--kx-data-missing-hatch'));
  });

  it('collectUrls encuentra direcciones anidadas y relativas al protocolo', () => {
    expect(collectUrls({ a: ['https://x.test/a', { b: '//cdn.test/b' }], c: 'texto', d: 3, e: 'pmtiles://http://h/p' })).toEqual([
      'https://x.test/a',
      '//cdn.test/b',
      'pmtiles://http://h/p',
    ]);
  });
});

describe('trama de «sin dato» (M-2)', () => {
  const base = 'rgb(200, 200, 200)';
  const line = 'rgb(10, 20, 30)';

  it('lee colores rgb() y rgba() de la sonda', () => {
    expect(parseRgb('rgb(1, 2, 3)')).toEqual([1, 2, 3, 255]);
    expect(parseRgb('rgba(1, 2, 3, 0.5)')).toEqual([1, 2, 3, 128]);
    expect(parseRgb('rgb(1 2 3 / 50%)')).toEqual([1, 2, 3, 128]);
    expect(parseRgb('oklab(0.5 0 0)')).toBeNull();
    expect(parseRgb('')).toBeNull();
  });

  it('sin lienzo rellena a mano rayas diagonales con los dos colores y encaja con su vecina', () => {
    const image = drawHatch({ base, line }, 3, { canvas: () => null })!;
    expect(image.name).toBe('kx-missing-hatch-3');
    expect(image.width).toBe(HATCH_SIZE);
    expect(image.height).toBe(HATCH_SIZE);
    expect(image.pixelRatio).toBe(1);
    expect(image.data).toHaveLength(HATCH_SIZE * HATCH_SIZE * 4);
    const pixel = (x: number, y: number) => [...image.data.slice((y * image.width + x) * 4, (y * image.width + x) * 4 + 4)];
    // Una diagonal de «línea» y el resto del color de fondo.
    const lines = new Set<string>();
    for (let y = 0; y < image.height; y += 1) for (let x = 0; x < image.width; x += 1) lines.add(pixel(x, y).join(','));
    expect([...lines].sort()).toEqual(['10,20,30,255', '200,200,200,255']);
    expect(pixel(0, 0)).toEqual([10, 20, 30, 255]);
    // La raya mide 1 px: el vecino ya es del fondo.
    expect(pixel(1, 0)).toEqual([200, 200, 200, 255]);
    expect(pixel(0, 1)).toEqual([200, 200, 200, 255]);
    // Periódica: la raya que sale por un lado entra por el otro.
    expect(pixel(HATCH_SIZE - 1, 1)).toEqual([10, 20, 30, 255]);
    expect(pixel(HATCH_SIZE - 1, 0)).toEqual([200, 200, 200, 255]);
    // Una sola diagonal de raya por tesela: HATCH_SIZE píxeles de cada HATCH_SIZE².
    let stripe = 0;
    for (let y = 0; y < image.height; y += 1) for (let x = 0; x < image.width; x += 1) if (pixel(x, y)[0] === 10) stripe += 1;
    expect(stripe).toBe(HATCH_SIZE);
  });

  it('la tesela mide 12 px y la raya 1 px (R3-4, M-2): rala y fina, para que «sin dato» no domine', () => {
    expect(HATCH_SIZE).toBe(12);
    expect(HATCH_STROKE).toBe(1);
    const image = drawHatch({ base, line }, 1, { canvas: () => null })!;
    expect(image.width).toBe(12);
    expect(image.height).toBe(12);
  });

  it('el tamaño sigue a la densidad de píxeles y los colores no rgb() sin lienzo no dan imagen', () => {
    const doubled = drawHatch({ base, line }, 1, { pixelRatio: 2, canvas: () => null })!;
    expect(doubled.width).toBe(HATCH_SIZE * 2);
    // La raya mide lo mismo en píxeles CSS: el doble de píxeles de imagen a densidad doble.
    const widths = [...doubled.data].filter((_, index) => index % 4 === 0 && doubled.data[index] === 10).length;
    expect(widths).toBe(HATCH_SIZE * 2 * 2);
    expect(doubled.pixelRatio).toBe(2);
    expect(drawHatch({ base: 'oklab(0.5 0 0)', line }, 1, { canvas: () => null })).toBeNull();
  });

  it('con lienzo se pinta en él con los dos colores', () => {
    const calls: string[] = [];
    const data = new Uint8ClampedArray(HATCH_SIZE * HATCH_SIZE * 4).fill(9);
    const ctx = {
      set fillStyle(value: string) {
        calls.push(`fill:${value}`);
      },
      set strokeStyle(value: string) {
        calls.push(`stroke:${value}`);
      },
      set lineWidth(value: number) {
        calls.push(`width:${value}`);
      },
      set lineCap(_value: string) {
        calls.push('cap');
      },
      fillRect: () => calls.push('fillRect'),
      beginPath: () => calls.push('beginPath'),
      moveTo: () => calls.push('moveTo'),
      lineTo: () => calls.push('lineTo'),
      stroke: () => calls.push('stroke()'),
      getImageData: () => ({ data }),
    };
    const canvas = { width: 0, height: 0, getContext: () => ctx } as unknown as HTMLCanvasElement;
    const image = drawHatch({ base, line }, 2, { canvas: () => canvas })!;
    expect(canvas.width).toBe(HATCH_SIZE);
    expect(calls).toContain(`fill:${base}`);
    expect(calls).toContain(`stroke:${line}`);
    expect(calls.filter((call) => call === 'lineTo')).toHaveLength(3);
    expect(image.data[0]).toBe(9);
  });
});

describe('la hoja de MapLibre no va con la del paquete (M-16, B-3)', () => {
  const read = (file: string) => readFileSync(resolve(import.meta.dirname, file), 'utf8');

  it('map.css ya no importa maplibre-gl.css', () => {
    expect(read('map.css')).not.toContain('maplibre-gl.css');
    expect(read('index.ts')).not.toContain('maplibre.css');
  });

  it('el motor carga maplibre.css de forma diferida, junto a maplibre-gl', () => {
    const engine = read('engine.ts');
    expect(engine).toMatch(/import\('\.\/maplibre\.css'\)/);
    expect(engine).toMatch(/import\('maplibre-gl'\)/);
    // Dentro de la carga diferida, no como importación estática...
    expect(engine).not.toMatch(/^import .*maplibre\.css/m);
    // ...y la biblioteca sustituye ese import por un módulo vacío: la hoja no se cuela en `dist/karpo.css`;
    // quien consume el paquete importa `@karpo/react/map.css`.
    expect(engine).not.toContain("import.meta.env.MODE");
    const lib = readFileSync(resolve(import.meta.dirname, '../../vite.lib.config.ts'), 'utf8');
    expect(lib).toContain(String.raw`maplibre\.css`);
    expect(lib).toContain('withoutMapLibreCss()');
  });

  it('maplibre.css fuerza la tipografía del tema sobre la de MapLibre', () => {
    const css = read('maplibre.css');
    expect(css).toMatch(/\.maplibregl-map\s*\{[^}]*font:\s*inherit/);
  });

  it('las cifras de la tabla llevan tabular-nums en la familia del cuerpo, sin la monoespaciada de `amount` (D-R35)', () => {
    const css = read('map.css');
    const rule = /\[data-kx-part="table"\]\s*\[data-kx-align="end"\]\s*\{([^}]*)\}/.exec(css);
    expect(rule?.[1]).toContain('font-variant-numeric: tabular-nums');
    expect(rule?.[1]).toContain('text-align: end');
    expect(rule?.[1]).not.toContain('font-family');
    expect(read('MapTable.tsx')).not.toContain('data-th-format');
  });

  it('la leyenda conserva la trama marcada y el mapa pide la suave (R3-4, M-2)', () => {
    expect(readFileSync(resolve(import.meta.dirname, '../theme/kx-ext.css'), 'utf8')).toMatch(
      /--kx-data-missing-hatch-map:\s*color-mix\(in oklab, var\(--kx-data-missing-hatch\) 35%, var\(--kx-data-missing\)\)/,
    );
    // La leyenda no usa el token suave.
    expect(read('map.css')).not.toContain('--kx-data-missing-hatch-map');
    expect(MAP_TOKENS.missingHatch).toContain('--kx-data-missing-hatch-map');
  });

  it('«sin dato» tiene su trama en la leyenda, hecha con variables del tema', () => {
    const css = read('map.css');
    const rule = /\[data-kx-part="swatch"\]\[data-kx-step="none"\]\s*\{([^}]*)\}/.exec(css);
    expect(rule?.[1]).toContain('repeating-linear-gradient');
    expect(rule?.[1]).toContain('var(--kx-data-missing)');
    expect(rule?.[1]).toContain('--kx-data-missing-hatch');
    // Y los textos de estado no se pegan al borde (M-10).
    expect(css).toMatch(/\[data-kx-part="status"\][\s\S]*?padding-inline:\s*var\(--th-space-3\);[\s\S]*?overflow-wrap:\s*anywhere/);
  });
});
