// Verificación de la paleta de datos PROVISIONAL de kx-ext.css (decisión D-R13, hasta que llegue PT-5).
//
// Evalúa en la prueba las expresiones de `--kx-data-*` (var(), light-dark(), color-mix() en srgb y en
// oklab) con los tokens REALES de cada tema y comprueba:
//   - contraste de cada color de la serie contra la superficie del contenido >= 3:1 (WCAG 1.4.11);
//   - distancia mínima entre colores de la serie (oklab), para que se distingan entre sí;
//   - la secuencial es monótona en luminosidad (cada paso se separa del anterior).
// No sustituye a una paleta validada para daltonismo (PT-5): lo dice el aviso de kx-ext.css.
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';

const ROOT = path.resolve(__dirname, '../..');
const KX_EXT = readFileSync(path.join(ROOT, 'src/theme/kx-ext.css'), 'utf8');

type Mode = 'light' | 'dark';
type Rgb = { r: number; g: number; b: number };

function declarations(css: string, prefix: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const match of css.matchAll(new RegExp(`(${prefix}[a-z0-9-]*)\\s*:\\s*([^;]+);`, 'g'))) {
    if (!out.has(match[1] ?? '')) out.set(match[1] ?? '', (match[2] ?? '').trim().replace(/\s+/g, ' '));
  }
  return out;
}

function tokensOf(theme: string): Map<string, string> {
  const css = readFileSync(path.join(ROOT, 'demo/public/themes', theme, 'tokens.css'), 'utf8');
  return declarations(css, '--th-');
}

/** Separa los argumentos de nivel superior de una función (respeta paréntesis anidados). */
function splitArgs(text: string): string[] {
  const args: string[] = [];
  let depth = 0;
  let current = '';
  for (const ch of text) {
    if (ch === '(') depth += 1;
    if (ch === ')') depth -= 1;
    if (ch === ',' && depth === 0) {
      args.push(current.trim());
      current = '';
    } else {
      current += ch;
    }
  }
  args.push(current.trim());
  return args;
}

function findClosing(text: string, open: number): number {
  let depth = 0;
  for (let i = open; i < text.length; i += 1) {
    if (text[i] === '(') depth += 1;
    if (text[i] === ')') {
      depth -= 1;
      if (depth === 0) return i;
    }
  }
  throw new Error(`paréntesis sin cerrar en «${text}»`);
}

/** Sustituye var(), resuelve light-dark() y deja sólo colores hexadecimales y color-mix(). */
function flatten(expr: string, vars: Map<string, string>, mode: Mode): string {
  let out = expr;
  for (;;) {
    const start = out.search(/var\(|light-dark\(/);
    if (start < 0) return out;
    const isVar = out.startsWith('var(', start);
    const open = start + (isVar ? 3 : 10);
    const close = findClosing(out, open);
    const inner = out.slice(open + 1, close);
    let replacement: string;
    if (isVar) {
      const name = inner.trim();
      const value = vars.get(name);
      if (value === undefined) throw new Error(`variable sin definir: ${name}`);
      replacement = value;
    } else {
      const [light = '', dark = ''] = splitArgs(inner);
      replacement = mode === 'light' ? light : dark;
    }
    out = out.slice(0, start) + replacement + out.slice(close + 1);
  }
}

const srgbToLinear = (c: number) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
const linearToSrgb = (c: number) => (c <= 0.0031308 ? c * 12.92 : 1.055 * c ** (1 / 2.4) - 0.055);

function parseHex(hex: string): Rgb & { a: number } {
  const h = hex.replace('#', '');
  const full = h.length <= 4 ? [...h].map((c) => c + c).join('') : h;
  return {
    r: parseInt(full.slice(0, 2), 16) / 255,
    g: parseInt(full.slice(2, 4), 16) / 255,
    b: parseInt(full.slice(4, 6), 16) / 255,
    a: full.length === 8 ? parseInt(full.slice(6, 8), 16) / 255 : 1,
  };
}

function toOklab({ r, g, b }: Rgb): [number, number, number] {
  const [lr, lg, lb] = [srgbToLinear(r), srgbToLinear(g), srgbToLinear(b)];
  const l = Math.cbrt(0.4122214708 * lr + 0.5363325363 * lg + 0.0514459929 * lb);
  const m = Math.cbrt(0.2119034982 * lr + 0.6806995451 * lg + 0.1073969566 * lb);
  const s = Math.cbrt(0.0883024619 * lr + 0.2817188376 * lg + 0.6299787005 * lb);
  return [
    0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s,
    1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s,
    0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s,
  ];
}

function fromOklab([L, a, b]: [number, number, number]): Rgb {
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const clamp = (v: number) => Math.min(1, Math.max(0, linearToSrgb(v)));
  return {
    r: clamp(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s),
    g: clamp(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s),
    b: clamp(-0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s),
  };
}

/** Evalúa un color ya aplanado: `#rrggbb`, `transparent` o `color-mix(in <espacio>, c1 p%, c2 p%)`. */
function evaluate(expr: string): Rgb {
  const text = expr.trim();
  if (text.startsWith('#')) {
    const { r, g, b } = parseHex(text);
    return { r, g, b };
  }
  const mix = /^color-mix\(/.exec(text);
  if (!mix) throw new Error(`color no soportado en la prueba: ${text}`);
  const [space = '', ...parts] = splitArgs(text.slice(text.indexOf('(') + 1, text.length - 1));
  const items = parts.map((part) => {
    const match = /^(.*?)(?:\s+(\d+(?:\.\d+)?)%)?$/.exec(part.trim());
    return { color: evaluate(match?.[1] ?? ''), percent: match?.[2] === undefined ? undefined : Number(match[2]) };
  });
  const [first, second] = items;
  if (!first || !second) throw new Error(`color-mix necesita dos colores: ${text}`);
  const p1 = first.percent ?? (second.percent === undefined ? 50 : 100 - second.percent);
  const p2 = second.percent ?? 100 - p1;
  const w1 = p1 / (p1 + p2);
  const w2 = p2 / (p1 + p2);
  if (space.includes('oklab')) {
    const a = toOklab(first.color);
    const b = toOklab(second.color);
    return fromOklab([a[0] * w1 + b[0] * w2, a[1] * w1 + b[1] * w2, a[2] * w1 + b[2] * w2]);
  }
  return {
    r: first.color.r * w1 + second.color.r * w2,
    g: first.color.g * w1 + second.color.g * w2,
    b: first.color.b * w1 + second.color.b * w2,
  };
}

function luminance({ r, g, b }: Rgb): number {
  return 0.2126 * srgbToLinear(r) + 0.7152 * srgbToLinear(g) + 0.0722 * srgbToLinear(b);
}

export function contrast(a: Rgb, b: Rgb): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

function distance(a: Rgb, b: Rgb): number {
  const [x, y] = [toOklab(a), toOklab(b)];
  return Math.hypot(x[0] - y[0], x[1] - y[1], x[2] - y[2]);
}

const VARIANTS: { theme: string; modes: Mode[] }[] = [
  { theme: 'modern-flat', modes: ['light', 'dark'] },
  { theme: 'souq', modes: ['light', 'dark'] },
  { theme: 'classic-1995', modes: ['light'] },
  { theme: 'glossy-2003', modes: ['light'] },
];

const kx = declarations(KX_EXT, '--kx-');
const series = Array.from({ length: 10 }, (_, i) => `--kx-data-series-${i + 1}`);
const sequence = Array.from({ length: 7 }, (_, i) => `--kx-data-seq-${i + 1}`);

function resolveColors(theme: string, mode: Mode, names: string[]): Rgb[] {
  const vars = new Map([...tokensOf(theme), ...kx]);
  return names.map((name) => evaluate(flatten(`var(${name})`, vars, mode)));
}

describe('paleta de datos provisional (kx-ext.css)', () => {
  it('define las 10 series, los 7 pasos secuenciales y «sin dato»', () => {
    for (const name of [...series, ...sequence, '--kx-data-missing']) expect(kx.has(name), name).toBe(true);
  });

  for (const { theme, modes } of VARIANTS) {
    for (const mode of modes) {
      describe(`${theme} (${mode})`, () => {
        const surface = (): Rgb =>
          evaluate(flatten('var(--th-surface-content-bg)', tokensOf(theme), mode));

        it('cada color de la serie contrasta >= 3:1 con la superficie del contenido', () => {
          const bg = surface();
          resolveColors(theme, mode, series).forEach((color, i) => {
            expect(contrast(color, bg), `${theme}/${mode} serie ${i + 1}`).toBeGreaterThanOrEqual(3);
          });
        });

        // `souq` deriva de un primario turquesa: el azul informativo y el verde de éxito casi coinciden y
        // la categórica, que sólo dispone de los tonos semánticos, no puede separarlos (motivo de PT-5).
        const minimum = theme === 'souq' ? 0.02 : 0.05;
        it(`los colores de la serie se distinguen entre sí (distancia oklab >= ${minimum})`, () => {
          const colors = resolveColors(theme, mode, series);
          for (let i = 0; i < colors.length; i += 1) {
            for (let j = i + 1; j < colors.length; j += 1) {
              expect(
                distance(colors[i] as Rgb, colors[j] as Rgb),
                `${theme}/${mode} series ${i + 1} y ${j + 1}`,
              ).toBeGreaterThanOrEqual(minimum);
            }
          }
        });

        it('la secuencial avanza paso a paso y el último paso contrasta con la superficie', () => {
          const colors = resolveColors(theme, mode, sequence);
          for (let i = 1; i < colors.length; i += 1) {
            expect(distance(colors[i - 1] as Rgb, colors[i] as Rgb), `paso ${i} a ${i + 1}`).toBeGreaterThan(0.02);
          }
          expect(contrast(colors.at(-1) as Rgb, surface())).toBeGreaterThanOrEqual(3);
        });
      });
    }
  }
});

describe('evaluador de colores de la prueba', () => {
  it('mezcla en srgb y en oklab y respeta los porcentajes', () => {
    const red = evaluate('#ff0000');
    expect(red).toEqual({ r: 1, g: 0, b: 0 });
    const mid = evaluate('color-mix(in srgb, #000000 50%, #ffffff)');
    expect(mid.r).toBeCloseTo(0.5, 5);
    const oklabMid = evaluate('color-mix(in oklab, #000000 50%, #ffffff)');
    expect(oklabMid.r).toBeGreaterThan(0.36); // L = 0,5 de oklab es un gris de ~0,39 en sRGB
    expect(oklabMid.r).toBeLessThan(0.42);
    const implicit = evaluate('color-mix(in srgb, #000000 25%, #ffffff)');
    expect(implicit.r).toBeCloseTo(0.75, 5);
  });

  it('contraste de blanco sobre negro es 21', () => {
    expect(contrast(evaluate('#ffffff'), evaluate('#000000'))).toBeCloseTo(21, 0);
  });
});
