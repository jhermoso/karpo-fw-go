// Valor por delimitación (PLAN §4.12): agregado en cliente (`count` o `sum`) de las filas de la
// entidad por `boundaryField`, subido por la cadena de antecesores (`ancestors[]`) de cada
// delimitación. Go no tiene agregados por delimitación (A2 §5), así que se hace aquí. Puro.
import type { Row } from '../contracts';

type Obj = Record<string, unknown>;

function isObj(value: unknown): value is Obj {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/** Valor de un campo posiblemente anidado (`location.geoBoundary`). */
export function readPath(row: unknown, path: string): unknown {
  let value: unknown = row;
  for (const part of path.split('.')) {
    if (!isObj(value)) return undefined;
    value = value[part];
  }
  return value;
}

/** Lo que se necesita saber de una delimitación para colocarla en el mapa. */
export interface BoundaryLike {
  id?: string;
  level?: string | number;
  geoCode?: string;
  nutsCode?: string;
  lauCode?: string;
  ineCode?: string;
  abbreviation?: string;
}

/** Un eslabón de la cadena: el nivel de Go y el código con el que se une a la geometría. */
export interface ChainLink {
  level: string;
  code: string;
}

function str(value: unknown): string {
  return typeof value === 'string' ? value.trim() : typeof value === 'number' ? String(value) : '';
}

/**
 * Código de la delimitación para unirla con la geometría: país = ISO alfa-2 (`geoCode`), niveles
 * NUTS = `nutsCode`, municipio = `lauCode` (o `ineCode`).
 */
export function boundaryCode(boundary: BoundaryLike): string {
  const level = str(boundary.level);
  if (level === 'country') return str(boundary.geoCode) || str(boundary.abbreviation);
  if (level === 'local') return str(boundary.lauCode) || str(boundary.ineCode);
  return str(boundary.nutsCode) || str(boundary.lauCode) || str(boundary.ineCode) || str(boundary.geoCode);
}

/** ¿Se puede colocar en el mapa sin pedir más datos (tiene nivel y código)? */
export function isLocatable(boundary: BoundaryLike): boolean {
  return str(boundary.level) !== '' && boundaryCode(boundary) !== '';
}

/** `[la propia delimitación, ...antecesores]` con nivel y código; las que no se pueden colocar se omiten. */
export function chainOf(boundary: BoundaryLike, ancestors: readonly BoundaryLike[]): ChainLink[] {
  const out: ChainLink[] = [];
  for (const item of [boundary, ...ancestors]) {
    if (!isLocatable(item)) continue;
    out.push({ level: str(item.level), code: boundaryCode(item) });
  }
  return out;
}

export const keyOf = (level: string, code: string): string => `${level}\u0000${code}`;

/** Un valor numérico de una celda: admite cadenas decimales de Go (`'1200.10'`) y descarta lo que no lo es. */
export function numeric(value: unknown): number {
  if (typeof value === 'number') return Number.isFinite(value) ? value : 0;
  if (typeof value === 'string') {
    const parsed = Number(value.trim());
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}

export interface AggregateOptions {
  boundaryField: string;
  aggregate: 'count' | 'sum';
  valueField?: string;
}

/**
 * Totales por `(nivel, código)`. Cada fila suma en su delimitación y en todas las que la contienen.
 * `chains` da la cadena de cada identificador de delimitación; una fila sin delimitación conocida
 * no cuenta (y se anota en `unplaced`).
 */
export function aggregateRows(
  rows: readonly Row[],
  options: AggregateOptions,
  chains: ReadonlyMap<string, readonly ChainLink[]>,
): { totals: Map<string, number>; unplaced: number } {
  const totals = new Map<string, number>();
  let unplaced = 0;
  for (const row of rows) {
    const boundary = readPath(row, options.boundaryField);
    const id = isObj(boundary) ? str(boundary.id) : str(boundary);
    const chain = id === '' ? undefined : chains.get(id);
    if (!chain || chain.length === 0) {
      unplaced += 1;
      continue;
    }
    const amount = options.aggregate === 'sum' && options.valueField ? numeric(readPath(row, options.valueField)) : 1;
    // Un mismo nivel no se cuenta dos veces aunque la cadena lo repita.
    const seen = new Set<string>();
    for (const link of chain) {
      const key = keyOf(link.level, link.code);
      if (seen.has(key)) continue;
      seen.add(key);
      totals.set(key, (totals.get(key) ?? 0) + amount);
    }
  }
  return { totals, unplaced };
}

/** Identificadores de delimitación distintos de las filas (lo que hay que resolver). */
export function boundaryIds(rows: readonly Row[], boundaryField: string): string[] {
  const ids = new Set<string>();
  for (const row of rows) {
    const boundary = readPath(row, boundaryField);
    const id = isObj(boundary) ? str(boundary.id) : str(boundary);
    if (id !== '') ids.add(id);
  }
  return [...ids];
}

// ---------------------------------------------------------------------------------------------
// Clases de color
// ---------------------------------------------------------------------------------------------

export interface Bin {
  /** Posición de la clase, desde 0. */
  index: number;
  /** Paso de la paleta secuencial (`--kx-data-seq-N`, 1..7). */
  step: number;
  from: number;
  to: number;
}

export const SEQUENTIAL_STEPS = 7;

/**
 * Clases de igual amplitud entre el mínimo y el máximo de los valores positivos (hasta siete, y no
 * más que valores distintos). Los pasos de la paleta se reparten de claro a oscuro.
 */
export function computeBins(values: readonly number[]): Bin[] {
  const positive = values.filter((v) => Number.isFinite(v) && v > 0);
  if (positive.length === 0) return [];
  const distinct = new Set(positive);
  const count = Math.min(SEQUENTIAL_STEPS, distinct.size);
  const min = Math.min(...positive);
  const max = Math.max(...positive);
  const width = (max - min) / count;
  return Array.from({ length: count }, (_unused, index) => ({
    index,
    step: count === 1 ? SEQUENTIAL_STEPS : Math.round(1 + (index * (SEQUENTIAL_STEPS - 1)) / (count - 1)),
    from: min + index * width,
    to: index === count - 1 ? max : min + (index + 1) * width,
  }));
}

/** Clase de un valor (`-1` si no hay dato). */
export function binOf(value: number | undefined, bins: readonly Bin[]): number {
  if (value === undefined || !Number.isFinite(value) || value <= 0 || bins.length === 0) return -1;
  const last = bins[bins.length - 1]!;
  if (value >= last.from) return last.index;
  for (const bin of bins) if (value < bin.to) return bin.index;
  return last.index;
}

export interface LegendItem {
  step: number;
  text: string;
}

/**
 * Textos de los intervalos: enteros sin solaparse (`1 a 4`, `5 a 9`) si todos los valores lo son. `range`
 * recibe los extremos ya formateados y también sin formatear: con `Intl.NumberFormat#formatRange` el
 * intervalo sale con el separador, el orden y las cifras del idioma (M-12).
 */
export function binLabels(
  bins: readonly Bin[],
  integer: boolean,
  format: (value: number) => string,
  range: (from: string, to: string, fromValue: number, toValue: number) => string,
  single: (value: string) => string,
): LegendItem[] {
  return bins.map((bin, index) => {
    const last = index === bins.length - 1;
    const from = integer ? Math.ceil(bin.from) : bin.from;
    let to = bin.to;
    if (integer) to = last ? Math.floor(bin.to) : Math.max(from, Math.ceil(bin.to) - 1);
    const a = format(from);
    const b = format(to);
    return { step: bin.step, text: a === b ? single(a) : range(a, b, from, to) };
  });
}
