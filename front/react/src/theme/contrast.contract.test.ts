// Contraste automático (M-14, M-2): los pares de tokens que la interfaz pinta, resueltos de verdad
// para los cuatro temas de la demo y sus modos (claro, oscuro y contraste alto), sin navegador ni
// dependencias: el CSS del tema (`tokens.css`, `modes.css`) y el de `kx-ext.css` se evalúan aquí con un
// intérprete mínimo de `light-dark()`, `var()` y `color-mix()` (sRGB y oklab).
//
//   texto            >= 4,5:1   (WCAG 1.4.3)
//   no texto         >= 3:1     (WCAG 1.4.11: bordes de controles, marcas, foco, leyenda)
//   «sin dato»       ΔL(oklab) >= 0,04 respecto de la primera clase de la escala, y la escala
//                    secuencial seq-1..seq-7 crece en intensidad (se aleja de la superficie) paso a paso,
//                    con el mismo sentido en claro y en oscuro.
//
// Si un cambio de token (aquí o en el tema de Theros Front) rompe un par, esta prueba lo dice con el
// nombre del par, del tema y del modo. Es la red local: el CI de GitHub está bloqueado.
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import { parseThemeManifest } from './manifest';

const ROOT = path.resolve(__dirname, '../..');
const THEMES = ['modern-flat', 'classic-1995', 'glossy-2003', 'souq'] as const;
const read = (...parts: string[]) => readFileSync(path.join(ROOT, ...parts), 'utf8');

// ---------------------------------------------------------------------------------------------
// Color: sRGB <-> oklab, mezcla CSS y contraste WCAG

interface Rgba {
  r: number; // 0..1, codificado sRGB
  g: number;
  b: number;
  a: number;
}

const clamp01 = (value: number) => Math.min(1, Math.max(0, value));
const toLinear = (c: number) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
const fromLinear = (c: number) => (c <= 0.0031308 ? 12.92 * c : 1.055 * c ** (1 / 2.4) - 0.055);

function toOklab({ r, g, b }: Rgba): [number, number, number] {
  const lr = toLinear(r);
  const lg = toLinear(g);
  const lb = toLinear(b);
  const l = Math.cbrt(0.4122214708 * lr + 0.5363325363 * lg + 0.0514459929 * lb);
  const m = Math.cbrt(0.2119034982 * lr + 0.6806995451 * lg + 0.1073969566 * lb);
  const s = Math.cbrt(0.0883024619 * lr + 0.2817188376 * lg + 0.6299787005 * lb);
  return [
    0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s,
    1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s,
    0.0259040371 * l + 0.7827717662 * m - 0.8086757660 * s,
  ];
}

function fromOklab(L: number, a: number, b: number, alpha: number): Rgba {
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const lr = 4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s;
  const lg = -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s;
  const lb = -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s;
  return { r: clamp01(fromLinear(lr)), g: clamp01(fromLinear(lg)), b: clamp01(fromLinear(lb)), a: alpha };
}

const luminance = ({ r, g, b }: Rgba) => 0.2126 * toLinear(r) + 0.7152 * toLinear(g) + 0.0722 * toLinear(b);

/** `fg` (con su alfa) sobre `bg` opaco. */
function over(fg: Rgba, bg: Rgba): Rgba {
  const a = fg.a;
  return {
    r: fg.r * a + bg.r * (1 - a),
    g: fg.g * a + bg.g * (1 - a),
    b: fg.b * a + bg.b * (1 - a),
    a: 1,
  };
}

function contrast(fg: Rgba, bg: Rgba): number {
  const opaque = over(bg, { r: 1, g: 1, b: 1, a: 1 });
  const top = over(fg, opaque);
  const [light, dark] = [luminance(top), luminance(opaque)].sort((x, y) => y - x) as [number, number];
  return (light + 0.05) / (dark + 0.05);
}

function parseHex(text: string): Rgba {
  const hex = text.replace('#', '');
  const full = hex.length <= 4 ? [...hex].map((c) => c + c).join('') : hex;
  const channel = (index: number) => Number.parseInt(full.slice(index * 2, index * 2 + 2), 16) / 255;
  return { r: channel(0), g: channel(1), b: channel(2), a: full.length === 8 ? channel(3) : 1 };
}

// ---------------------------------------------------------------------------------------------
// Intérprete de valores CSS: light-dark(), var(), color-mix(), #hex y transparent

type Scope = Readonly<Record<string, string>>;

/** Parte `texto` por las comas de primer nivel (sin entrar en paréntesis). */
function splitTopLevel(text: string): string[] {
  const parts: string[] = [];
  let depth = 0;
  let start = 0;
  for (let i = 0; i < text.length; i += 1) {
    const char = text[i];
    if (char === '(') depth += 1;
    else if (char === ')') depth -= 1;
    else if (char === ',' && depth === 0) {
      parts.push(text.slice(start, i).trim());
      start = i + 1;
    }
  }
  parts.push(text.slice(start).trim());
  return parts;
}

/** Contenido de la función `name(...)` si `text` es exactamente esa llamada. */
function callArguments(text: string, name: string): string | undefined {
  const prefix = `${name}(`;
  return text.startsWith(prefix) && text.endsWith(')') ? text.slice(prefix.length, -1) : undefined;
}

interface MixStop {
  color: Rgba;
  percent?: number;
}

function mix(space: 'srgb' | 'oklab', first: MixStop, second: MixStop): Rgba {
  let p1 = first.percent;
  let p2 = second.percent;
  if (p1 === undefined && p2 === undefined) {
    p1 = 50;
    p2 = 50;
  } else if (p1 === undefined) p1 = 100 - (p2 ?? 0);
  else if (p2 === undefined) p2 = 100 - p1;
  const sum = p1 + (p2 ?? 0);
  const alphaScale = sum < 100 ? sum / 100 : 1;
  const w1 = p1 / sum;
  const w2 = (p2 ?? 0) / sum;
  const alpha = (first.color.a * w1 + second.color.a * w2) * alphaScale;
  // Mezcla con alfa premultiplicado, como pide CSS Color 4.
  const channels = (color: Rgba): [number, number, number] =>
    space === 'srgb' ? [color.r, color.g, color.b] : toOklab(color);
  const [a0, a1, a2] = channels(first.color);
  const [b0, b1, b2] = channels(second.color);
  const premultiplied = (x: number, y: number) =>
    alpha === 0 ? 0 : (x * first.color.a * w1 + y * second.color.a * w2) / (alpha / alphaScale);
  const mixed: [number, number, number] = [premultiplied(a0, b0), premultiplied(a1, b1), premultiplied(a2, b2)];
  return space === 'srgb'
    ? { r: clamp01(mixed[0]), g: clamp01(mixed[1]), b: clamp01(mixed[2]), a: alpha }
    : fromOklab(mixed[0], mixed[1], mixed[2], alpha);
}

function evaluate(raw: string, scheme: 'light' | 'dark', scope: Scope, depth = 0): Rgba {
  if (depth > 20) throw new Error(`referencia circular al resolver «${raw}»`);
  const text = raw.trim();
  const lightDark = callArguments(text, 'light-dark');
  if (lightDark !== undefined) {
    const [light, dark] = splitTopLevel(lightDark);
    return evaluate((scheme === 'light' ? light : dark) ?? '', scheme, scope, depth + 1);
  }
  const variable = callArguments(text, 'var');
  if (variable !== undefined) {
    const name = splitTopLevel(variable)[0] ?? '';
    const value = scope[name];
    if (value === undefined) throw new Error(`variable sin definir: ${name}`);
    return evaluate(value, scheme, scope, depth + 1);
  }
  const colorMix = callArguments(text, 'color-mix');
  if (colorMix !== undefined) {
    const [spaceArg = '', ...stops] = splitTopLevel(colorMix);
    const space = /^in\s+(srgb|oklab)\b/.exec(spaceArg)?.[1] as 'srgb' | 'oklab' | undefined;
    if (!space || stops.length !== 2) throw new Error(`color-mix no admitido: ${text}`);
    const [first, second] = stops.map((stop): MixStop => {
      const match = /^(.*?)(?:\s+(\d+(?:\.\d+)?)%)?$/.exec(stop);
      const colorText = match?.[1]?.trim() ?? stop;
      return {
        color: evaluate(colorText, scheme, scope, depth + 1),
        ...(match?.[2] !== undefined ? { percent: Number(match[2]) } : {}),
      };
    }) as [MixStop, MixStop];
    return mix(space, first, second);
  }
  if (text === 'transparent') return { r: 0, g: 0, b: 0, a: 0 };
  if (/^#[0-9a-f]{3,8}$/i.test(text)) return parseHex(text);
  throw new Error(`valor de color no admitido: «${text}»`);
}

// ---------------------------------------------------------------------------------------------
// Lectura de los CSS

function declarations(css: string, prefix: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const match of css.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(new RegExp(`(${prefix}[a-z0-9-]+)\\s*:\\s*([^;]+);`, 'g'))) {
    out[match[1] as string] = (match[2] as string).trim();
  }
  return out;
}

/** Variables `--kx-*` de `kx-ext.css` (la primera declaración: la de `:root, [data-th-mode]`). */
const KX = declarations(read('src/theme/kx-ext.css').split('@media')[0] ?? '', '--kx-');

interface ThemeMode {
  theme: (typeof THEMES)[number];
  mode: string;
  scheme: 'light' | 'dark';
  highContrast: boolean;
  scope: Scope;
}

/** Cada tema con sus modos (los del manifiesto sin `system`) y, si lo declara, su contraste alto. */
function themeModes(): ThemeMode[] {
  const out: ThemeMode[] = [];
  for (const theme of THEMES) {
    const info = parseThemeManifest(JSON.parse(read('demo/public/themes', theme, 'manifest.json')), theme);
    const tokens = declarations(read('demo/public/themes', theme, 'tokens.css'), '--th-');
    const modes = read('demo/public/themes', theme, 'modes.css');
    const highContrast = declarations(/\[data-th-mode~="high-contrast"\]\s*\{([^}]*)\}/.exec(modes)?.[1] ?? '', '--th-');
    const schemes = info.modes.filter((mode): mode is 'light' | 'dark' => mode === 'light' || mode === 'dark');
    for (const scheme of schemes) {
      out.push({ theme, mode: scheme, scheme, highContrast: false, scope: { ...tokens, ...KX } });
      if (info.highContrast && Object.keys(highContrast).length > 0) {
        out.push({ theme, mode: `${scheme}+high-contrast`, scheme, highContrast: true, scope: { ...tokens, ...highContrast, ...KX } });
      }
    }
  }
  return out;
}

const MODES = themeModes();

// ---------------------------------------------------------------------------------------------
// Pares que la interfaz pinta

interface Pair {
  name: string;
  /**
   * Primer plano y fondo: tokens (`--th-x`, `--kx-x`) o expresiones de color. Con varios primeros planos
   * basta que uno cumpla (el límite de un botón lo marca su relleno o su borde).
   */
  fg: string | readonly string[];
  bg: string;
  /** Mínimo de contraste: 4,5 texto, 3 no texto. */
  min: 4.5 | 3;
  /** Capa sobre la que va el fondo si es translúcido (por omisión, el contenido). */
  under?: string;
}

const text = (name: string, fg: string | readonly string[], bg: string, under?: string): Pair => ({ name, fg, bg, min: 4.5, ...(under ? { under } : {}) });
const graphic = (name: string, fg: string | readonly string[], bg: string, under?: string): Pair => ({ name, fg, bg, min: 3, ...(under ? { under } : {}) });
const CONTENT = '--th-surface-content-bg';
const TONES = ['neutral', 'info', 'success', 'warning', 'error'] as const;

const PAIRS: Pair[] = [
  // Texto sobre las superficies
  text('texto principal / contenido', '--th-text-primary', CONTENT),
  text('texto secundario / contenido', '--th-text-secondary', CONTENT),
  text('texto de encabezado / contenido', '--th-text-heading-color', CONTENT),
  text('enlace / contenido', '--th-text-link', CONTENT),
  text('contenido fg / bg', '--th-surface-content-fg', CONTENT),
  text('lienzo fg / bg', '--th-surface-canvas-fg', '--th-surface-canvas-bg'),
  text('cabecera fg / bg', '--th-surface-header-fg', '--th-surface-header-bg'),
  text('barra lateral fg / bg', '--th-surface-sidebar-fg', '--th-surface-sidebar-bg'),
  text('diálogo fg / bg', '--th-surface-dialog-fg', '--th-surface-dialog-bg'),
  text('menú emergente fg / bg', '--th-surface-popover-fg', '--th-surface-popover-bg'),
  text('tooltip fg / bg', '--th-surface-tooltip-fg', '--th-surface-tooltip-bg'),
  text('texto principal / nivel 3 (kbd)', '--th-text-primary', '--th-surface-layer-level3'),
  // Acciones y campos
  text('botón', '--th-control-button-fg', '--th-control-button-bg'),
  text('botón principal', '--th-control-button-default-fg', '--th-control-button-default-bg'),
  text('botón destructivo', '--th-control-button-destructive-fg', '--th-control-button-destructive-bg'),
  text('botón discreto / contenido', '--th-control-button-quiet-fg', CONTENT),
  text('campo', '--th-control-field-fg', '--th-control-field-bg'),
  text('campo enfocado', '--th-control-field-focused-fg', '--th-control-field-focused-bg'),
  text('campo inválido', '--th-control-field-invalid-fg', '--th-control-field-invalid-bg'),
  text('campo de sólo lectura', '--th-control-field-readonly-fg', '--th-control-field-readonly-bg'),
  text('marcador de posición / campo', '--th-text-placeholder', '--th-control-field-bg'),
  text('interruptor activado', '--th-control-toggle-on-fg', '--th-control-toggle-on-bg'),
  // Estados y navegación
  text('selección', '--th-state-selected-fg', '--th-state-selected-bg'),
  text('selección inactiva', '--th-state-selected-inactive-fg', '--th-state-selected-inactive-bg'),
  text('pasar el puntero', '--th-state-hover-fg', '--th-state-hover-bg'),
  text('entrada del menú / barra lateral', '--th-nav-item-fg', '--th-surface-sidebar-bg'),
  text('entrada actual del menú', '--th-nav-item-current-fg', '--th-nav-item-current-bg'),
  text('cabecera de cuadrícula', '--th-collection-header-fg', '--th-collection-header-bg'),
  text('columna ordenada', '--th-collection-sort-fg', '--th-collection-sort-bg'),
  text('fila cebreada', '--th-text-primary', '--th-collection-zebra-color'),
  // Insignias y avisos por tono (el tema pinta fg sobre bg)
  ...TONES.map((tone) => text(`insignia ${tone}`, `--th-semantic-${tone}-fg`, `--th-semantic-${tone}-bg`)),
  text('insignia accent (kx)', '--kx-accent-fg', '--kx-accent-bg'),
  ...(['info', 'success', 'warning', 'error'] as const).map((tone) =>
    text(`texto ${tone} fuerte / contenido`, `--th-semantic-${tone}-strong`, CONTENT),
  ),
  // Cuadrícula: celda sucia, inválida, editando y rango
  text('celda sucia', '--th-text-primary', '--kx-cell-dirty-bg'),
  text('celda inválida', '--th-text-primary', '--kx-cell-invalid-bg'),
  text('celda editando', '--kx-cell-editing-fg', '--kx-cell-editing-bg'),
  text('celda de sólo lectura', '--kx-cell-readonly-fg', '--kx-cell-readonly-bg'),
  text('rango de celdas', '--kx-cell-range-fg', '--kx-cell-range-bg', CONTENT),
  // Estados de guardado: el texto del estado sobre su relleno
  ...(['saving', 'saved', 'error', 'conflict', 'pending'] as const).map((state) =>
    text(`guardado ${state}`, `--kx-save-${state}`, `--kx-save-${state}-bg`),
  ),
  // No texto: foco, bordes de controles, marcas y datos
  graphic('anillo de foco / contenido', '--th-state-focus-color', CONTENT),
  graphic('borde de campo / campo', '--th-control-field-border', '--th-control-field-bg'),
  graphic(
    'botón principal / contenido (relleno o borde)',
    ['--th-control-button-default-bg', '--th-control-button-default-border'],
    CONTENT,
  ),
  graphic('interruptor activado / contenido', '--th-control-toggle-on-bg', CONTENT),
  graphic('marca de celda sucia / contenido', '--kx-cell-dirty-mark', CONTENT),
  graphic('marca de celda inválida / contenido', '--kx-cell-invalid-mark', CONTENT),
  graphic('borde de la leyenda / contenido', '--kx-data-border', CONTENT),
  graphic('clase más alta de la leyenda / contenido', '--kx-data-seq-7', CONTENT),
  graphic('trama de «sin dato» / relleno de «sin dato»', '--kx-data-missing-hatch', '--kx-data-missing'),
  ...[1, 2, 3].map((n) => graphic(`serie ${n} / contenido`, `--kx-data-series-${n}`, CONTENT)),
  graphic('acento / contenido', '--kx-accent-strong', CONTENT),
];

/**
 * Huecos del propio tema de Theros Front (no de este front): pares que no llegan al mínimo en su salida.
 * Están anotados en docs/notas/R2-R1.md para PETICIONES-THEROS-FRONT.md. La prueba de abajo exige que
 * sigan siendo reales: el día que Theros los arregle, la excepción sobra y hay que borrarla.
 */
const THEME_GAPS: readonly { theme: string; mode: string; pair: string; reason: string }[] = [
  {
    theme: 'classic-1995',
    mode: 'light',
    pair: 'marcador de posición / campo',
    reason: 'text.placeholder de classic-1995 queda en 3,95:1 sobre el campo; en contraste alto sube a #595959',
  },
];

function isKnownGap(mode: ThemeMode, pair: Pair): boolean {
  return THEME_GAPS.some((gap) => gap.theme === mode.theme && gap.mode === mode.mode && gap.pair === pair.name);
}

function resolve(mode: ThemeMode, token: string, under?: Rgba): Rgba {
  const value = token.startsWith('--') ? `var(${token})` : token;
  const color = evaluate(value, mode.scheme, mode.scope);
  return under ? over(color, under) : color;
}

const fixed = (value: number) => value.toFixed(2);

describe('los temas y modos que se comprueban', () => {
  it('son los cuatro temas de la demo, cada uno con claro (y oscuro si lo declara) y contraste alto', () => {
    const names = MODES.map((m) => `${m.theme}:${m.mode}`);
    expect(names).toEqual(
      expect.arrayContaining([
        'modern-flat:light',
        'modern-flat:dark',
        'modern-flat:light+high-contrast',
        'modern-flat:dark+high-contrast',
        'classic-1995:light',
        'classic-1995:light+high-contrast',
        'glossy-2003:light',
        'glossy-2003:light+high-contrast',
        'souq:light',
        'souq:dark',
      ]),
    );
    expect(new Set(MODES.map((m) => m.theme))).toEqual(new Set(THEMES));
  });

  it('el intérprete reproduce valores conocidos (blanco/negro, color-mix en sRGB y en oklab)', () => {
    const scope: Scope = { '--a': '#ffffff', '--b': '#000000' };
    const mid = evaluate('color-mix(in srgb, var(--a) 50%, var(--b))', 'light', scope);
    expect(mid.r).toBeCloseTo(0.5, 6);
    const oklab = evaluate('color-mix(in oklab, var(--a) 50%, var(--b))', 'light', scope);
    expect(toOklab(oklab)[0]).toBeCloseTo(0.5, 3); // L de oklab, no el gris de sRGB (0,5)
    expect(contrast(evaluate('var(--a)', 'light', scope), evaluate('var(--b)', 'light', scope))).toBeCloseTo(21, 6);
    expect(evaluate('light-dark(#fff, #000)', 'dark', scope).r).toBe(0);
    expect(evaluate('color-mix(in srgb, #fff 40%, transparent)', 'light', scope).a).toBeCloseTo(0.4, 6);
  });
});

describe('las excepciones del tema siguen siendo reales', () => {
  it.each(THEME_GAPS.map((gap) => [`${gap.theme} · ${gap.mode}: ${gap.pair}`, gap] as const))('%s', (_label, gap) => {
    const mode = MODES.find((m) => m.theme === gap.theme && m.mode === gap.mode);
    const pair = PAIRS.find((p) => p.name === gap.pair);
    expect(mode, 'modo del tema').toBeDefined();
    expect(pair, 'par').toBeDefined();
    if (!mode || !pair) return;
    const candidates = typeof pair.fg === 'string' ? [pair.fg] : pair.fg;
    const under = resolve(mode, pair.under ?? CONTENT);
    const ratio = Math.max(...candidates.map((token) => contrast(resolve(mode, token), resolve(mode, pair.bg, under))));
    expect(ratio, gap.reason).toBeLessThan(pair.min);
  });
});

describe.each(MODES.map((mode) => [`${mode.theme} · ${mode.mode}`, mode] as const))('%s', (_label, mode) => {
  it('los pares de texto cumplen 4,5:1 y los de no texto 3:1', () => {
    const failures: string[] = [];
    for (const pair of PAIRS) {
      const under = resolve(mode, pair.under ?? CONTENT);
      const background = resolve(mode, pair.bg, under);
      const candidates = typeof pair.fg === 'string' ? [pair.fg] : pair.fg;
      const ratio = Math.max(...candidates.map((token) => contrast(resolve(mode, token), background)));
      if (ratio < pair.min && !isKnownGap(mode, pair)) {
        failures.push(`${pair.name}: ${fixed(ratio)}:1 (mínimo ${pair.min}:1; ${candidates.join(' | ')} sobre ${pair.bg})`);
      }
    }
    expect(failures, failures.join('\n')).toEqual([]);
  });
});

describe('paleta de datos: «sin dato» y escala secuencial (M-2)', () => {
  const SCALE = [1, 2, 3, 4, 5, 6, 7].map((n) => `--kx-data-seq-${n}`);

  it('seq-1 se mezcla al 18-22 % con la superficie y «sin dato» no es una clase de la escala', () => {
    const seq1 = KX['--kx-data-seq-1'] ?? '';
    const percent = Number(/var\(--th-semantic-info-strong\)\s+(\d+)%/.exec(seq1)?.[1]);
    expect(percent).toBeGreaterThanOrEqual(18);
    expect(percent).toBeLessThanOrEqual(22);
    expect(KX['--kx-data-missing']).toBeDefined();
    expect(SCALE).not.toContain('--kx-data-missing');
    expect(KX['--kx-data-missing-hatch']).toBeDefined();
  });

  it.each(MODES.map((mode) => [`${mode.theme} · ${mode.mode}`, mode] as const))(
    '%s: ΔL(missing, seq-1) >= 0,04 y la escala crece en intensidad en cada paso',
    (_label, mode) => {
      const L = (token: string) => toOklab(resolve(mode, token, resolve(mode, CONTENT)))[0];
      const delta = Math.abs(L('--kx-data-missing') - L('--kx-data-seq-1'));
      expect(delta, `ΔL = ${delta.toFixed(3)}`).toBeGreaterThanOrEqual(0.04);

      // La intensidad es la distancia de luminosidad a la superficie: crece de seq-1 a seq-7 en claro y en
      // oscuro (en claro la luminosidad baja y en oscuro sube: el mismo sentido «hacia la tinta»).
      const surface = L(CONTENT);
      const steps = SCALE.map((token) => L(token) - surface);
      const sign = Math.sign(steps[steps.length - 1] ?? 0);
      expect(sign).not.toBe(0);
      for (let i = 1; i < steps.length; i += 1) {
        expect(Math.sign(steps[i] ?? 0), `${SCALE[i]} cambia de lado de la superficie`).toBe(sign);
        expect(Math.abs(steps[i] ?? 0), `${SCALE[i]} no es más intensa que ${SCALE[i - 1]}`).toBeGreaterThan(
          Math.abs(steps[i - 1] ?? 0),
        );
      }
      // «Sin dato» queda fuera de la escala: del lado de la superficie que seq-1 o más allá, pero nunca
      // entre dos clases con el mismo tono (su croma es el del gris neutro, no el del azul de la escala).
      const missing = toOklab(resolve(mode, '--kx-data-missing', resolve(mode, CONTENT)));
      const seq1 = toOklab(resolve(mode, '--kx-data-seq-1', resolve(mode, CONTENT)));
      const chroma = (lab: [number, number, number]) => Math.hypot(lab[1], lab[2]);
      expect(chroma(missing)).toBeLessThan(chroma(seq1));
    },
  );

  // R3-4 (M-2): el mapa pinta la trama con un token SUAVE (la de la leyenda, a 3:1, gritaba sobre el 99 % del
  // lienzo). Debe seguir viéndose como trama (>= 1,3:1 contra el relleno) y no pasar de la de la leyenda.
  it('el token --kx-data-missing-hatch-map existe, mezcla la trama de la leyenda con el relleno y es suave', () => {
    expect(KX['--kx-data-missing-hatch-map']).toMatch(/var\(--kx-data-missing-hatch\)\s+35%.*var\(--kx-data-missing\)/);
  });

  it.each(MODES.map((mode) => [`${mode.theme} · ${mode.mode}`, mode] as const))(
    '%s: la trama del mapa se distingue de su relleno (>= 1,3:1) y es más suave que la de la leyenda',
    (_label, mode) => {
      const under = resolve(mode, CONTENT);
      const fill = resolve(mode, '--kx-data-missing', under);
      const soft = contrast(resolve(mode, '--kx-data-missing-hatch-map', under), fill);
      const marked = contrast(resolve(mode, '--kx-data-missing-hatch', under), fill);
      expect(soft, `trama del mapa / relleno de «sin dato»: ${fixed(soft)}:1`).toBeGreaterThanOrEqual(1.3);
      expect(soft, 'la trama del mapa no puede gritar más que la de la leyenda').toBeLessThan(marked);
      // La leyenda conserva su trama marcada: 3:1 contra el relleno (el par ya está en PAIRS; aquí, a la vista).
      expect(marked, `trama de la leyenda: ${fixed(marked)}:1`).toBeGreaterThanOrEqual(3);
    },
  );

  it('la escala sube en el mismo sentido en todos los modos: el signo de L(seq-7) − L(superficie) sigue al modo', () => {
    for (const mode of MODES) {
      const L = (token: string) => toOklab(resolve(mode, token, resolve(mode, CONTENT)))[0];
      const darkSurface = L(CONTENT) < 0.5;
      const direction = Math.sign(L('--kx-data-seq-7') - L(CONTENT));
      expect(direction, `${mode.theme} ${mode.mode}`).toBe(darkSurface ? 1 : -1);
    }
  });
});
