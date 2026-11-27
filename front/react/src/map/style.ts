// Estilo de MapLibre construido en ejecución (PLAN §4.12): sin URL de estilo, sin glifos, sin
// sprites y sin teselas de terceros. Los colores salen de variables del tema resueltas con la sonda
// (`resolveThemeColor`, A3 §7.1): MapLibre no entiende `light-dark()` ni `var()`.
import type { StyleSpecification } from 'maplibre-gl';
import type { GeoCollection } from './geo';

export const SOURCE_ID = 'kx-boundaries';
export const LAYER_BACKGROUND = 'kx-background';
export const LAYER_FILL = 'kx-fill';
/** Trama de «sin dato» sobre el relleno: lo que no tiene dato se distingue por algo más que el color (M-2). */
export const LAYER_MISSING = 'kx-missing';
export const LAYER_LINE = 'kx-line';
export const LAYER_HOVER = 'kx-hover';
export const LAYER_ACTIVE = 'kx-active';

/** Variables del tema que pintan el mapa (todas son variables del tema o de `karpo.ext`). */
export const MAP_TOKENS = {
  background: '--th-surface-canvas-bg',
  missing: '--kx-data-missing',
  /**
   * Color de las rayas de la trama DEL MAPA: la versión suave (R3-4, M-2). La de la leyenda (`--kx-data-missing-hatch`,
   * 3:1) grita a propósito y sobre un nivel casi entero de «sin dato» ensuciaba el lienzo. Si el tema no trae el token
   * suave, la trama usa la marcada y, sin ella, el color de «sin dato».
   */
  missingHatch: 'var(--kx-data-missing-hatch-map, var(--kx-data-missing-hatch, var(--kx-data-missing)))',
  edge: '--th-edge-strong',
  hoverFill: '--th-state-hover-bg',
  activeFill: '--th-state-selected-bg',
  focus: '--th-state-focus-color',
  sequential: [
    '--kx-data-seq-1',
    '--kx-data-seq-2',
    '--kx-data-seq-3',
    '--kx-data-seq-4',
    '--kx-data-seq-5',
    '--kx-data-seq-6',
    '--kx-data-seq-7',
  ],
} as const;

export interface MapColors {
  background: string;
  /** Delimitación sin dato. */
  missing: string;
  /** Rayas de la trama de «sin dato» del mapa (la versión suave). */
  missingHatch: string;
  edge: string;
  hoverFill: string;
  activeFill: string;
  focus: string;
  /** Siete pasos, de claro a oscuro. */
  sequential: string[];
}

/** Color no válido o vacío (jsdom sin tema): transparente, para que MapLibre no rechace el estilo. */
const FALLBACK_COLOR = 'rgba(0, 0, 0, 0)';

/** Resuelve todos los colores del mapa con la sonda del tema (`ThemeApi.resolveColor`). */
export function resolveMapColors(resolve: (token: string) => string): MapColors {
  const pick = (token: string) => resolve(token) || FALLBACK_COLOR;
  return {
    background: pick(MAP_TOKENS.background),
    missing: pick(MAP_TOKENS.missing),
    missingHatch: pick(MAP_TOKENS.missingHatch),
    edge: pick(MAP_TOKENS.edge),
    hoverFill: pick(MAP_TOKENS.hoverFill),
    activeFill: pick(MAP_TOKENS.activeFill),
    focus: pick(MAP_TOKENS.focus),
    sequential: MAP_TOKENS.sequential.map(pick),
  };
}

export type MapDataSource =
  | { kind: 'geojson'; data: GeoCollection }
  | {
      kind: 'pmtiles';
      /** `pmtiles://` + la dirección del fichero, ya absoluta y del propio origen. */
      url: string;
      /** Capa vectorial con las delimitaciones (`boundaries`). */
      layer: string;
      /** Nivel de Go de lo que se pinta (`country`, `2`, `3`, `local`). */
      boundaryLevel: string;
      /** Código de la delimitación que se abre; `null` en el nivel de arriba (mundo). */
      parentCode: string | null;
      maxzoom: number;
    };

export interface MapStyleInput {
  source: MapDataSource;
  colors: MapColors;
  /** Paso de la paleta (1..7) de cada código con dato; lo que falta se pinta como «sin dato». */
  steps: ReadonlyMap<string, number>;
  /** Nombre de la imagen de la trama (`map.addImage`); sin él no hay capa de trama. */
  hatchImage?: string;
}

type Expression = unknown[];

/** `['match', ['get','code'], [códigos], color, …, sin dato]`: agrupa los códigos por color. */
export function fillColorExpression(steps: ReadonlyMap<string, number>, colors: MapColors): Expression | string {
  const byStep = new Map<number, string[]>();
  for (const [code, step] of steps) {
    const clamped = Math.min(colors.sequential.length, Math.max(1, Math.round(step)));
    const list = byStep.get(clamped) ?? [];
    list.push(code);
    byStep.set(clamped, list);
  }
  if (byStep.size === 0) return colors.missing;
  const expression: Expression = ['match', ['get', 'code']];
  for (const step of [...byStep.keys()].sort((a, b) => a - b)) {
    expression.push(byStep.get(step)!.sort(), colors.sequential[step - 1]!);
  }
  expression.push(colors.missing);
  return expression;
}

/** Filtro de la fuente vectorial: sólo el nivel y el padre que se están mirando. */
export function vectorFilter(source: Extract<MapDataSource, { kind: 'pmtiles' }>): Expression {
  const level = ['==', ['get', 'level'], source.boundaryLevel];
  if (source.parentCode === null) return level;
  return ['all', level, ['==', ['get', 'parentCode'], source.parentCode]];
}

/** Filtro que no deja pasar nada (la capa de resaltado sin elemento). */
export const NO_CODE_FILTER: Expression = ['==', ['get', 'code'], ''];

export function codeFilter(code: string | null): Expression {
  return code === null ? NO_CODE_FILTER : ['==', ['get', 'code'], code];
}

/**
 * Filtro de la trama de «sin dato»: el del nivel y, si hay datos, «el código no está entre los que
 * tienen dato». Sin ningún dato devuelve el del nivel entero, pero `buildMapStyle` no añade entonces la capa
 * (M-2): sin una clase con la que confundirlo, «sin dato» ya se distingue por el relleno y no hace falta trama.
 */
export function missingFilter(source: MapDataSource, steps: ReadonlyMap<string, number>): Expression | undefined {
  const base = source.kind === 'pmtiles' ? vectorFilter(source) : undefined;
  if (steps.size === 0) return base;
  const absent: Expression = ['match', ['get', 'code'], [...steps.keys()].sort(), false, true];
  return base ? ['all', base, absent] : absent;
}

/** Estilo completo del mapa: fondo, relleno coroplético, trama de «sin dato», bordes y las dos capas de resaltado. */
export function buildMapStyle(input: MapStyleInput): StyleSpecification {
  const { source, colors, steps, hatchImage } = input;
  const vector = source.kind === 'pmtiles';
  const layerSource = vector ? { 'source-layer': source.layer, filter: vectorFilter(source) } : {};
  const highlightSource = vector ? { 'source-layer': source.layer } : {};

  const sources: StyleSpecification['sources'] = vector
    ? { [SOURCE_ID]: { type: 'vector', url: source.url, minzoom: 0, maxzoom: source.maxzoom } }
    : { [SOURCE_ID]: { type: 'geojson', data: source.data as never } };

  return {
    version: 8,
    name: 'karpo-map',
    sources,
    layers: [
      { id: LAYER_BACKGROUND, type: 'background', paint: { 'background-color': colors.background } },
      {
        id: LAYER_FILL,
        type: 'fill',
        source: SOURCE_ID,
        ...layerSource,
        paint: { 'fill-color': fillColorExpression(steps, colors) as never, 'fill-antialias': true },
      },
      // Sin ningún dato en el nivel no hay clase de la escala con la que confundir «sin dato»: sin trama (M-2).
      ...(hatchImage && steps.size > 0
        ? [
            {
              id: LAYER_MISSING,
              type: 'fill',
              source: SOURCE_ID,
              ...highlightSource,
              ...(missingFilter(source, steps) ? { filter: missingFilter(source, steps) } : {}),
              paint: { 'fill-pattern': hatchImage, 'fill-antialias': false },
            },
          ]
        : []),
      {
        id: LAYER_ACTIVE,
        type: 'fill',
        source: SOURCE_ID,
        ...highlightSource,
        filter: NO_CODE_FILTER as never,
        paint: { 'fill-color': colors.activeFill, 'fill-opacity': 0.55 },
      },
      {
        id: LAYER_HOVER,
        type: 'fill',
        source: SOURCE_ID,
        ...highlightSource,
        filter: NO_CODE_FILTER as never,
        paint: { 'fill-color': colors.hoverFill, 'fill-opacity': 0.7 },
      },
      {
        id: LAYER_LINE,
        type: 'line',
        source: SOURCE_ID,
        ...layerSource,
        layout: { 'line-join': 'round' },
        paint: { 'line-color': colors.edge, 'line-width': 1 },
      },
    ],
  } as StyleSpecification;
}

/** Todas las cadenas con aspecto de dirección de un estilo (para comprobar que no sale del origen). */
export function collectUrls(value: unknown, out: string[] = []): string[] {
  if (typeof value === 'string') {
    if (/^[a-z][a-z0-9+.-]*:\/\//i.test(value) || value.startsWith('//')) out.push(value);
  } else if (Array.isArray(value)) {
    for (const item of value) collectUrls(item, out);
  } else if (typeof value === 'object' && value !== null) {
    for (const item of Object.values(value)) collectUrls(item, out);
  }
  return out;
}
