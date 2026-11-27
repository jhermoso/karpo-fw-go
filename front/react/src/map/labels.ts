// Rótulos del mapa como marcadores HTML (sin glifos ni sprites): cuáles caben y cómo se escriben.
//
// Los rótulos se MIDEN con el tipo de letra real del tema (`CanvasRenderingContext2D.measureText`) y
// se parten en líneas con esa medida; nada de estimar por número de caracteres (M-3). Cada delimitación
// prueba, por este orden, su nombre completo, el nombre corto y el código, y se rotula con el primero
// que cabe en su caja en pantalla; si ninguno cabe, no se rotula (y la tabla del nivel la nombra).
//
// Un nivel, un criterio (R3-4, M-6): mezclar «Rusia» con «CA» en el mismo mapa no es un criterio. Se rotula en
// dos pasadas: la primera mira qué variante cabe en cada delimitación (`fitLabel`); `commonKind` elige la más
// corta de las elegidas y la segunda pasada rotula todas con ESA variante («nombre sólo si cabe entero en
// todas; si no, código en todas»). El nombre completo sigue en la tabla del nivel y en el `title` del rótulo.

/** Mide el ancho en píxeles de un texto con una fuente ya fijada. */
export type Measure = (text: string) => number;

/** Todo lo que hace falta saber del aspecto del rótulo para encajarlo en una delimitación. */
export interface LabelMeter {
  /** Ancho del texto del nombre (con su tipo y peso de letra). */
  name: Measure;
  /** Ancho del texto del valor. */
  value: Measure;
  /** Alto de una línea en píxeles. */
  lineHeight: number;
  /** Relleno de cada lado del rótulo en píxeles (en horizontal y en vertical). */
  paddingX: number;
  paddingY: number;
  /** Ancho máximo del rótulo en píxeles (relleno incluido). */
  maxWidth: number;
}

/** Variante del rótulo: el nombre completo, el corto o el código. */
export type LabelKind = 'full' | 'short' | 'code';

/** De más larga a más corta: `full > short > code`. */
export const LABEL_KINDS: readonly LabelKind[] = ['full', 'short', 'code'];

/** Cómo se escribe un rótulo: qué variante del nombre, en qué líneas y qué tamaño ocupa. */
export interface LabelFit {
  kind: LabelKind;
  /** Líneas del nombre, ya partidas con la medida real. */
  lines: string[];
  /** ¿Lleva el valor (segunda parte del rótulo)? */
  showValue: boolean;
  width: number;
  height: number;
}

export interface LabelTexts {
  name: string;
  /** Nombre corto (`shortName` de la geometría); vacío si no lo hay. */
  shortName: string;
  /** Código de la delimitación: la última variante. */
  code: string;
  /** Valor ya formateado; vacío si la delimitación no tiene dato. */
  value: string;
}

/** Líneas máximas del nombre en un rótulo. */
export const MAX_NAME_LINES = 3;

/** Trozos de un nombre: cada uno sabe si iba precedido de espacio; se puede partir tras un espacio o tras un guion (`Castilla-La Mancha`). */
function tokens(text: string): { word: string; spaced: boolean }[] {
  const out: { word: string; spaced: boolean }[] = [];
  for (const match of text.trim().matchAll(/(\s*)([^\s-]+-?|-)/g)) out.push({ word: match[2]!, spaced: match[1]! !== '' });
  return out;
}

/**
 * Parte `text` en líneas de hasta `maxWidth` píxeles, sin partir palabras. Devuelve `null` si alguna
 * palabra es más ancha que el máximo o si hacen falta más de `maxLines` líneas.
 */
export function wrapLines(text: string, maxWidth: number, measure: Measure, maxLines = MAX_NAME_LINES): string[] | null {
  const pieces = tokens(text);
  if (pieces.length === 0) return [];
  const lines: string[] = [];
  let current = '';
  for (const { word, spaced } of pieces) {
    if (measure(word) > maxWidth) return null;
    const candidate = current === '' ? word : `${current}${spaced ? ' ' : ''}${word}`;
    if (current === '' || measure(candidate) <= maxWidth) {
      current = candidate;
    } else {
      lines.push(current);
      if (lines.length >= maxLines) return null;
      current = word;
    }
  }
  lines.push(current);
  return lines.length <= maxLines ? lines : null;
}

/** Variantes del nombre sin repetir, de la más completa a la más corta. */
export function labelVariants(texts: Pick<LabelTexts, 'name' | 'shortName' | 'code'>): { kind: LabelFit['kind']; text: string }[] {
  const out: { kind: LabelFit['kind']; text: string }[] = [];
  const seen = new Set<string>();
  const add = (kind: LabelFit['kind'], text: string) => {
    const clean = text.trim();
    if (clean === '' || seen.has(clean)) return;
    seen.add(clean);
    out.push({ kind, text: clean });
  };
  add('full', texts.name);
  add('short', texts.shortName);
  add('code', texts.code);
  return out;
}

/** Texto de la variante `kind`: sin nombre corto, la «corta» es el nombre (así todas las delimitaciones admiten la misma variante). */
export function textOfKind(texts: Pick<LabelTexts, 'name' | 'shortName' | 'code'>, kind: LabelKind): string {
  const pick = kind === 'full' ? texts.name : kind === 'short' ? texts.shortName || texts.name : texts.code;
  return pick.trim();
}

/**
 * La variante más corta de las elegidas (`full > short > code`): el criterio único del nivel. Sin rótulos
 * elegidos no hay nada que unificar y se devuelve `full`.
 */
export function commonKind(fits: readonly Pick<LabelFit, 'kind'>[]): LabelKind {
  let rank = 0;
  for (const fit of fits) rank = Math.max(rank, LABEL_KINDS.indexOf(fit.kind));
  return LABEL_KINDS[rank] ?? 'full';
}

/**
 * Primera variante del nombre que cabe en una caja de `box` píxeles. Primero con el valor y, si no hay
 * sitio para las dos cosas, sólo el nombre (el valor sigue en la leyenda y en la tabla). `null` si
 * ninguna variante cabe. Con `only` se prueba esa variante y ninguna otra (segunda pasada, M-6).
 */
export function fitLabel(
  texts: LabelTexts,
  meter: LabelMeter,
  box: { width: number; height: number },
  only?: LabelKind,
): LabelFit | null {
  const maxWidth = Math.min(meter.maxWidth, box.width);
  const textWidth = maxWidth - 2 * meter.paddingX;
  if (textWidth <= 0) return null;
  const variants = only
    ? [{ kind: only, text: textOfKind(texts, only) }].filter((variant) => variant.text !== '')
    : labelVariants(texts);
  for (const showValue of texts.value === '' ? [false] : [true, false]) {
    for (const variant of variants) {
      const lines = wrapLines(variant.text, textWidth, meter.name);
      if (!lines || lines.length === 0) continue;
      const widest = Math.max(...lines.map((line) => meter.name(line)), showValue ? meter.value(texts.value) : 0);
      if (widest > textWidth) continue;
      const width = Math.ceil(widest) + 2 * meter.paddingX;
      const height = (lines.length + (showValue ? 1 : 0)) * meter.lineHeight + 2 * meter.paddingY;
      if (width <= box.width && height <= box.height) return { kind: variant.kind, lines, showValue, width, height };
    }
  }
  return null;
}

export interface LabelCandidate {
  code: string;
  /** Centro del rótulo en píxeles del lienzo. */
  x: number;
  y: number;
  /** Tamaño de la delimitación en píxeles: un rótulo nunca es mayor que su delimitación. */
  featureWidth: number;
  featureHeight: number;
  /** Tamaño del rótulo en píxeles (ya medido). */
  width: number;
  height: number;
}

export interface LabelViewport {
  width: number;
  height: number;
}

/**
 * Elige qué rótulos se pintan: sólo los de delimitaciones visibles y con sitio, de mayor a menor, sin
 * que ninguno pise a uno ya elegido. El orden es determinista (a igual tamaño, por código).
 */
export function pickLabels(candidates: readonly LabelCandidate[], viewport: LabelViewport): string[] {
  const placed: LabelCandidate[] = [];
  const sorted = [...candidates].sort(
    (a, b) => b.featureWidth * b.featureHeight - a.featureWidth * a.featureHeight || a.code.localeCompare(b.code),
  );
  for (const item of sorted) {
    if (item.featureWidth < item.width || item.featureHeight < item.height) continue;
    const left = item.x - item.width / 2;
    const right = item.x + item.width / 2;
    const top = item.y - item.height / 2;
    const bottom = item.y + item.height / 2;
    if (right < 0 || bottom < 0 || left > viewport.width || top > viewport.height) continue;
    const overlaps = placed.some(
      (other) =>
        left < other.x + other.width / 2 &&
        right > other.x - other.width / 2 &&
        top < other.y + other.height / 2 &&
        bottom > other.y - other.height / 2,
    );
    if (!overlaps) placed.push(item);
  }
  return placed.map((item) => item.code);
}

/** Centro de un rótulo de `size` dentro de la caja `[left, top, right, bottom]` lo más cerca posible de `(x, y)`. */
export function clampToBox(
  point: { x: number; y: number },
  size: { width: number; height: number },
  box: { left: number; top: number; right: number; bottom: number },
): { x: number; y: number } {
  const clamp = (value: number, min: number, max: number) => (min > max ? (min + max) / 2 : Math.min(max, Math.max(min, value)));
  return {
    x: clamp(point.x, box.left + size.width / 2, box.right - size.width / 2),
    y: clamp(point.y, box.top + size.height / 2, box.bottom - size.height / 2),
  };
}

// ---------------------------------------------------------------------------------------------
// Medida real con el tipo de letra del tema
// ---------------------------------------------------------------------------------------------

const widthCache = new Map<string, number>();
const CACHE_LIMIT = 4000;
let sharedContext: CanvasRenderingContext2D | null | undefined;

function context2d(): CanvasRenderingContext2D | null {
  if (sharedContext === undefined) {
    try {
      sharedContext = typeof document === 'undefined' ? null : document.createElement('canvas').getContext('2d');
    } catch {
      sharedContext = null;
    }
  }
  return sharedContext;
}

/** Ancho aproximado por carácter cuando no hay lienzo (jsdom): el mismo que usaba la estimación antigua. */
const FALLBACK_CHAR_WIDTH = 7;

/** Vacía la caché de medidas (cambia el tipo de letra o las fuentes web acaban de cargar). */
export function clearMeasureCache(): void {
  widthCache.clear();
}

/**
 * Medidor de texto para la fuente CSS `font` (la forma `style weight size family` de `ctx.font`).
 * Cachea por (texto, fuente); sin lienzo usa una estimación por caracteres.
 */
export function canvasMeasure(font: string, provider: () => CanvasRenderingContext2D | null = context2d): Measure {
  return (text) => {
    const key = `${font}\u0000${text}`;
    const known = widthCache.get(key);
    if (known !== undefined) return known;
    const ctx = provider();
    let width: number;
    if (ctx) {
      ctx.font = font;
      width = ctx.measureText(text).width;
    } else {
      width = text.length * FALLBACK_CHAR_WIDTH;
    }
    if (widthCache.size >= CACHE_LIMIT) widthCache.clear();
    widthCache.set(key, width);
    return width;
  };
}

/** Cadena `ctx.font` a partir de las propiedades calculadas de un elemento. */
export function fontOf(style: Pick<CSSStyleDeclaration, 'fontStyle' | 'fontWeight' | 'fontSize' | 'fontFamily'>): string {
  return [style.fontStyle, style.fontWeight, style.fontSize, style.fontFamily].filter((part) => part && part !== 'normal').join(' ');
}

const px = (value: string, fallback: number): number => {
  const parsed = Number.parseFloat(value);
  return Number.isFinite(parsed) ? parsed : fallback;
};

/** Valores de respaldo (sin estilos cargados: jsdom): los de la estimación antigua. */
export const FALLBACK_METER: LabelMeter = {
  name: (text) => text.length * FALLBACK_CHAR_WIDTH,
  value: (text) => text.length * FALLBACK_CHAR_WIDTH,
  lineHeight: 16,
  paddingX: 4,
  paddingY: 2,
  maxWidth: 112,
};

/**
 * Medidor de los rótulos a partir de un marcador de rótulo ya en el DOM: lee el tipo de letra
 * calculado del nombre y del valor, la altura de línea, el relleno y el ancho máximo que le da el
 * CSS del tema, de modo que la medida y lo que se pinta no pueden discrepar.
 */
export function readMeter(label: HTMLElement): LabelMeter {
  const view = label.ownerDocument.defaultView;
  const name = label.querySelector<HTMLElement>('[data-kx-part="name"]');
  const value = label.querySelector<HTMLElement>('[data-kx-part="value"]');
  if (!view || !name || !value) return FALLBACK_METER;
  const own = view.getComputedStyle(label);
  const nameStyle = view.getComputedStyle(name);
  const valueStyle = view.getComputedStyle(value);
  const nameFont = fontOf(nameStyle);
  if (nameFont === '' || !Number.isFinite(Number.parseFloat(nameStyle.fontSize))) return FALLBACK_METER;
  const size = px(nameStyle.fontSize, 13);
  return {
    name: canvasMeasure(nameFont),
    value: canvasMeasure(fontOf(valueStyle) || nameFont),
    lineHeight: px(nameStyle.lineHeight, size * 1.4),
    paddingX: px(own.paddingLeft, 0),
    paddingY: px(own.paddingTop, 0),
    maxWidth: px(own.maxWidth, FALLBACK_METER.maxWidth),
  };
}
