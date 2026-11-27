import { randomBytes } from 'node:crypto';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterEach, describe, expect, it } from 'vitest';
import {
  attributeBySource,
  attribution,
  decodeMappings,
  evaluateBudget,
  forbiddenInInitial,
  formatKb,
  groupOfSource,
  gzipBytes,
  initialChunkFiles,
  readBudget,
  run,
} from './presupuesto.mjs';

const MANIFEST = {
  'index.html': {
    file: 'assets/index-a.js',
    isEntry: true,
    imports: ['_vendor.js', '_ui.js'],
    css: ['assets/index-a.css'],
    dynamicImports: ['src/map/MapView.tsx'],
  },
  '_vendor.js': { file: 'assets/vendor-b.js', imports: ['_ui.js'] },
  '_ui.js': { file: 'assets/ui-c.js', css: ['assets/ui-c.css'] },
  'src/map/MapView.tsx': {
    file: 'assets/MapView-d.js',
    isDynamicEntry: true,
    imports: ['_vendor.js'],
    css: ['assets/MapView-d.css'],
  },
};

const folders = [];
afterEach(() => {
  for (const folder of folders.splice(0)) rmSync(folder, { recursive: true, force: true });
});

function tempFolder() {
  const folder = mkdtempSync(path.join(tmpdir(), 'presupuesto-'));
  folders.push(folder);
  return folder;
}

/** Carpeta con un demo/dist falso: cada fichero del manifiesto con contenido incompresible. */
function fixture({ manifest = MANIFEST, bytes = 2000, budget, withLibrary = true } = {}) {
  const root = tempFolder();
  const dist = path.join(root, 'demo', 'dist');
  mkdirSync(path.join(dist, '.vite'), { recursive: true });
  mkdirSync(path.join(dist, 'assets'), { recursive: true });
  writeFileSync(path.join(dist, '.vite', 'manifest.json'), JSON.stringify(manifest));
  const files = new Set();
  for (const chunk of Object.values(manifest)) {
    files.add(chunk.file);
    for (const css of chunk.css ?? []) files.add(css);
  }
  for (const file of files) writeFileSync(path.join(dist, ...file.split('/')), randomBytes(bytes));
  if (withLibrary) {
    mkdirSync(path.join(root, 'dist'), { recursive: true });
    writeFileSync(path.join(root, 'dist', 'karpo-react.js'), randomBytes(bytes));
  }
  if (budget) {
    mkdirSync(path.join(root, 'scripts'), { recursive: true });
    writeFileSync(path.join(root, 'scripts', 'presupuesto.json'), JSON.stringify(budget));
  }
  return root;
}

function capture() {
  const lines = { log: [], error: [] };
  return { lines, io: { log: (line) => lines.log.push(line), error: (line) => lines.error.push(line) } };
}

describe('initialChunkFiles', () => {
  it('toma la entrada, sus importaciones estáticas (sin repetir) y sus hojas; no las dinámicas', () => {
    const files = initialChunkFiles(MANIFEST).map((entry) => entry.file);
    expect([...files].sort()).toEqual(
      ['assets/index-a.js', 'assets/index-a.css', 'assets/vendor-b.js', 'assets/ui-c.js', 'assets/ui-c.css'].sort(),
    );
    expect(files).not.toContain('assets/MapView-d.js');
  });

  it('falla sin entradas o con una importación que no existe', () => {
    expect(() => initialChunkFiles({ a: { file: 'a.js' } })).toThrow(/ninguna entrada/);
    expect(() => initialChunkFiles({ a: { file: 'a.js', isEntry: true, imports: ['b'] } })).toThrow(/«b»/);
  });
});

describe('readBudget', () => {
  it('sin fichero: 250 kB y sólo aviso', () => {
    expect(readBudget(path.join(tmpdir(), 'no-existe-presupuesto.json'))).toEqual({ limitKb: 250, enforce: false });
  });

  it('lee limitKb y enforce, y rechaza valores absurdos', () => {
    const root = fixture({ budget: { limitKb: 120, enforce: true } });
    expect(readBudget(path.join(root, 'scripts', 'presupuesto.json'))).toEqual({ limitKb: 120, enforce: true });
    const bad = fixture({ budget: { limitKb: -1 } });
    expect(() => readBudget(path.join(bad, 'scripts', 'presupuesto.json'))).toThrow(/limitKb/);
    const badEnforce = fixture({ budget: { enforce: 'sí' } });
    expect(() => readBudget(path.join(badEnforce, 'scripts', 'presupuesto.json'))).toThrow(/enforce/);
  });

  it('un fichero sin enforce se queda en sólo aviso', () => {
    const root = fixture({ budget: { limitKb: 200 } });
    expect(readBudget(path.join(root, 'scripts', 'presupuesto.json'))).toEqual({ limitKb: 200, enforce: false });
  });
});

describe('evaluateBudget', () => {
  it('sólo falla al superar el límite con enforce', () => {
    expect(evaluateBudget({ gzipBytes: 250_001, limitKb: 250, enforce: true }).exitCode).toBe(1);
    expect(evaluateBudget({ gzipBytes: 250_001, limitKb: 250, enforce: false })).toMatchObject({
      overBudget: true,
      exitCode: 0,
    });
    expect(evaluateBudget({ gzipBytes: 250_000, limitKb: 250, enforce: true })).toMatchObject({
      overBudget: false,
      exitCode: 0,
    });
  });
});

describe('run', () => {
  it('imprime el desglose gzip del trozo inicial y de la biblioteca, y sale con 0 sin presupuesto.json', () => {
    const { lines, io } = capture();
    expect(run(fixture(), io)).toBe(0);
    const text = lines.log.join('\n');
    expect(text).toContain('assets/index-a.js');
    expect(text).toContain('assets/ui-c.css');
    expect(text).not.toContain('MapView-d');
    expect(text).toContain('TOTAL (5 ficheros)');
    expect(text).toContain('dist/karpo-react.js');
    expect(text).toContain('dentro del límite de 250 kB (sólo aviso)');
  });

  it('el total es la suma de los gzip de los ficheros del trozo', () => {
    const { lines, io } = capture();
    run(fixture({ bytes: 5000 }), io);
    // Contenido aleatorio: gzip no lo reduce, ronda los 5 kB por fichero (5 ficheros).
    const total = /\s([\d.]+) kB {2}TOTAL/.exec(lines.log.join('\n'));
    expect(Number(total?.[1])).toBeGreaterThan(24);
    expect(Number(total?.[1])).toBeLessThan(26);
    expect(gzipBytes(Buffer.alloc(10_000))).toBeLessThan(100);
  });

  it('sin enforce, superar el límite es sólo un aviso (código 0)', () => {
    const { lines, io } = capture();
    expect(run(fixture({ bytes: 5000, budget: { limitKb: 10 } }), io)).toBe(0);
    expect(lines.log.join('\n')).toContain('supera el límite de 10 kB');
    expect(lines.error).toEqual([]);
  });

  it('con enforce, superar el límite sale con 1; dentro del límite sale con 0', () => {
    const over = capture();
    expect(run(fixture({ bytes: 5000, budget: { limitKb: 10, enforce: true } }), over.io)).toBe(1);
    expect(over.lines.error.join('\n')).toContain('supera el límite de 10 kB (obligatorio)');
    const within = capture();
    expect(run(fixture({ bytes: 5000, budget: { limitKb: 100, enforce: true } }), within.io)).toBe(0);
  });

  it('un presupuesto.json mal formado sale con 1', () => {
    const { lines, io } = capture();
    expect(run(fixture({ budget: { limitKb: 0 } }), io)).toBe(1);
    expect(lines.error.join('\n')).toContain('limitKb');
  });

  it('sin manifiesto o con un fichero que falta, sale con 1 y lo explica', () => {
    const none = capture();
    expect(run(tempFolder(), none.io)).toBe(1);
    expect(none.lines.error.join('\n')).toContain('npm run build');

    const root = fixture();
    rmSync(path.join(root, 'demo', 'dist', 'assets', 'ui-c.js'));
    const missing = capture();
    expect(run(root, missing.io)).toBe(1);
    expect(missing.lines.error.join('\n')).toContain('assets/ui-c.js');
  });

  it('formatea kB de 1000 bytes', () => {
    expect(formatKb(12_345)).toBe('12.3 kB');
  });
});

// ---------------------------------------------------------------------------------------------
// Objetivo, atribución por paquete y lo que no debe estar en el trozo inicial (D-R37, M-11)

const B64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';

/** Codifica un entero como VLQ en base 64 (lo contrario de lo que hace `decodeMappings`). */
function vlq(value) {
  let rest = value < 0 ? (-value << 1) | 1 : value << 1;
  let out = '';
  do {
    let digit = rest & 31;
    rest >>>= 5;
    if (rest > 0) digit |= 32;
    out += B64[digit];
  } while (rest > 0);
  return out;
}

/** Segmento `[columna, fuente, línea, columna origen]` relativo al anterior, como en un mapa real. */
const segment = (...fields) => fields.map(vlq).join('');

describe('readBudget con objetivo (targetKb)', () => {
  it('lee limitKb, targetKb y enforce', () => {
    const root = fixture({ budget: { limitKb: 215, targetKb: 200, enforce: true } });
    expect(readBudget(path.join(root, 'scripts', 'presupuesto.json'))).toEqual({ limitKb: 215, targetKb: 200, enforce: true });
  });

  it('rechaza un objetivo que no es un número positivo o que supera el límite', () => {
    for (const targetKb of [0, -5, 'doscientos', 216]) {
      const root = fixture({ budget: { limitKb: 215, targetKb } });
      expect(() => readBudget(path.join(root, 'scripts', 'presupuesto.json')), String(targetKb)).toThrow(/targetKb/);
    }
  });

  it('el presupuesto.json del repositorio es 215 kB obligatorio con objetivo de 200 kB (D-R37)', () => {
    const real = JSON.parse(
      readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), 'presupuesto.json'), 'utf8'),
    );
    expect(real).toEqual({ limitKb: 215, targetKb: 200, enforce: true });
  });
});

describe('decodeMappings y attributeBySource', () => {
  it('decodifica segmentos con y sin fuente y reinicia la columna en cada línea', () => {
    // Línea 0: columna 0 → fuente 0; columna 5 → fuente 1 (salto relativo de +1). Línea 1: columna 2 → fuente 1.
    const mappings = `${segment(0, 0, 0, 0)},${segment(5, 1, 0, 0)};${segment(2, 0, 1, 0)}`;
    expect(decodeMappings(mappings)).toEqual([
      [
        { column: 0, source: 0 },
        { column: 5, source: 1 },
      ],
      [{ column: 2, source: 1 }],
    ]);
    expect(decodeMappings(segment(3))).toEqual([[{ column: 3, source: -1 }]]);
    expect(decodeMappings('')).toEqual([[]]);
  });

  it('decodifica números negativos y de varias cifras', () => {
    expect(decodeMappings(segment(100, 0, 0, 0))[0][0].column).toBe(100);
    expect(decodeMappings(`${segment(0, 3, 0, 0)},${segment(4, -2, 0, 0)}`)[0].map((s) => s.source)).toEqual([3, 1]);
  });

  it('rechaza un mapa con caracteres que no son base 64', () => {
    expect(() => decodeMappings('AA!A')).toThrow(/carácter no válido/);
  });

  it('atribuye a cada fuente los caracteres hasta el siguiente segmento y lo anterior a «sin fuente»', () => {
    // 10 caracteres en la línea 0: [0,4) de a.ts, [4,10) de b.ts; la línea 1 (5 caracteres) arranca en la columna 1.
    const code = 'aaaabbbbbb\nxxxxx';
    const map = {
      sources: ['a.ts', 'b.ts'],
      mappings: `${segment(0, 0, 0, 0)},${segment(4, 1, 0, 0)};${segment(1, -1, 0, 0)}`,
    };
    const totals = attributeBySource(code, map);
    expect(totals.get('a.ts')).toBe(4 + 4);
    expect(totals.get('b.ts')).toBe(6);
    // 1 carácter anterior al primer segmento de la línea 1 y el salto de línea.
    expect(totals.get('(sin fuente)')).toBe(1 + 1);
  });

  it('una línea sin segmentos cuenta entera como «sin fuente»', () => {
    const totals = attributeBySource('abc\ndefg', { sources: [], mappings: ';' });
    expect(totals.get('(sin fuente)')).toBe(3 + 4 + 1);
  });
});

describe('groupOfSource', () => {
  it('agrupa por paquete de npm (con ámbito y anidados) y por carpeta propia', () => {
    expect(groupOfSource('../../node_modules/react-dom/cjs/react-dom.production.js')).toBe('react-dom');
    expect(groupOfSource('../../node_modules/@tanstack/query-core/build/modern/queryClient.js')).toBe('@tanstack/query-core');
    expect(groupOfSource('..\\..\\node_modules\\a\\node_modules\\b\\i.js')).toBe('b');
    expect(groupOfSource('../../src/ui/Icon.tsx')).toBe('src/ui');
    expect(groupOfSource('../../src/locales/es/core.json')).toBe('src/locales');
    expect(groupOfSource('../../src/index.ts')).toBe('src');
    expect(groupOfSource('../../demo/main.tsx')).toBe('demo');
    expect(groupOfSource('(sin fuente)')).toBe('(sin fuente)');
    expect(groupOfSource('../../vite/preload-helper.js')).toBe('vite/preload-helper.js');
  });
});

describe('forbiddenInInitial', () => {
  it('señala los iconos extra, las pantallas de acceso y los textos de las vistas', () => {
    const found = forbiddenInInitial([
      '../../src/ui/icon-data-extra.ts',
      '../../src/auth/LoginPage.tsx',
      '../../src/auth/ChangePasswordPage.tsx',
      '../../src/locales/en/grid.json',
      '../../src/locales/es/detail.json',
      '../../src/locales/ar/map.json',
      '../../src/locales/es/tree.json',
    ]);
    expect(found.map((item) => item.source)).toEqual([
      '../../src/auth/ChangePasswordPage.tsx',
      '../../src/auth/LoginPage.tsx',
      '../../src/locales/ar/map.json',
      '../../src/locales/en/grid.json',
      '../../src/locales/es/detail.json',
      '../../src/locales/es/tree.json',
      '../../src/ui/icon-data-extra.ts',
    ]);
    for (const item of found) expect(item.reason.length).toBeGreaterThan(10);
  });

  it('deja pasar lo del marco, el núcleo de iconos y el resto del código', () => {
    expect(
      forbiddenInInitial([
        '../../src/ui/icon-data-core.ts',
        '../../src/ui/Icon.tsx',
        '../../src/auth/session-store.ts',
        '../../src/locales/es/core.json',
        '../../src/locales/en/ui.json',
        '../../src/locales/en/fields.json',
        '../../src/locales/es/errors.json',
        '../../src/locales/es/help.json',
        '../../src/locales/es/auth.json',
        '../../node_modules/react-dom/index.js',
      ]),
    ).toEqual([]);
  });
});

/** demo/dist con un único trozo cuyo mapa de fuentes atribuye mitad a un paquete y mitad a src/ui. */
function fixtureWithMap(sources) {
  const root = fixture({ budget: { limitKb: 100, targetKb: 50, enforce: true } });
  const dist = path.join(root, 'demo', 'dist');
  // Un solo trozo: el manifiesto con la entrada y nada más.
  writeFileSync(
    path.join(dist, '.vite', 'manifest.json'),
    JSON.stringify({ 'index.html': { file: 'assets/index-a.js', isEntry: true } }),
  );
  const code = 'a'.repeat(100) + '\n' + 'b'.repeat(100);
  writeFileSync(path.join(dist, 'assets', 'index-a.js'), code);
  const mappings = sources.map((_, index) => segment(0, index === 0 ? 0 : 1, 0, 0)).join(';');
  writeFileSync(path.join(dist, 'assets', 'index-a.js.map'), JSON.stringify({ version: 3, sources, mappings }));
  return root;
}

describe('attribution y run --detalle', () => {
  it('atribuye los caracteres del trozo inicial a su paquete o carpeta', () => {
    const root = fixtureWithMap(['../../node_modules/react-dom/index.js', '../../src/ui/Icon.tsx']);
    const entries = initialChunkFiles(JSON.parse(readFileSync(path.join(root, 'demo', 'dist', '.vite', 'manifest.json'), 'utf8')));
    const detail = attribution(path.join(root, 'demo', 'dist'), entries);
    expect(detail.missing).toEqual([]);
    expect(detail.groups.map((group) => group.group).sort()).toEqual(['(sin fuente)', 'react-dom', 'src/ui']);
    expect([...detail.sources].sort()).toEqual(['(sin fuente)', '../../node_modules/react-dom/index.js', '../../src/ui/Icon.tsx']);
    expect(detail.groups.reduce((sum, group) => sum + group.chars, 0)).toBe(201);
    for (const group of detail.groups) expect(group.gzip).toBeGreaterThan(0);
  });

  it('--detalle imprime la tabla y confirma que no entra nada que deba ir diferido', () => {
    const root = fixtureWithMap(['../../node_modules/react-dom/index.js', '../../src/ui/Icon.tsx']);
    const { lines, io } = capture();
    expect(run(root, io, { detalle: true })).toBe(0);
    const text = lines.log.join('\n');
    expect(text).toContain('atribución del trozo inicial por paquete o carpeta');
    expect(text).toMatch(/react-dom\n/);
    expect(text).toContain('src/ui');
    expect(text).toContain('no trae icon-data-extra, las pantallas de acceso ni los textos de las vistas');
    expect(text).toContain('objetivo de 50 kB alcanzado');
  });

  it('con enforce, si el trozo inicial trae icon-data-extra, LoginPage o textos de una vista, sale con 1', () => {
    for (const forbidden of ['../../src/ui/icon-data-extra.ts', '../../src/auth/LoginPage.tsx', '../../src/locales/en/grid.json']) {
      const root = fixtureWithMap(['../../node_modules/react-dom/index.js', forbidden]);
      const { lines, io } = capture();
      expect(run(root, io, { detalle: true }), forbidden).toBe(1);
      expect(lines.error.join('\n'), forbidden).toContain(`${forbidden.replace('../../', '')} está en el trozo inicial`);
    }
  });

  it('sin --detalle no mira los mapas (y no falla por ellos)', () => {
    const root = fixtureWithMap(['../../node_modules/react-dom/index.js', '../../src/ui/icon-data-extra.ts']);
    const { lines, io } = capture();
    expect(run(root, io)).toBe(0);
    expect(lines.log.join('\n')).not.toContain('atribución');
  });

  it('sin mapa de fuentes lo dice y sigue', () => {
    const root = fixture({ budget: { limitKb: 100, enforce: true } });
    const { lines, io } = capture();
    expect(run(root, io, { detalle: true })).toBe(0);
    expect(lines.log.join('\n')).toContain('sin mapa de fuentes');
  });
});

describe('objetivo informativo', () => {
  it('por encima del objetivo y dentro del límite: sale con 0 y dice cuánto falta', () => {
    const root = fixture({ bytes: 5000, budget: { limitKb: 100, targetKb: 20, enforce: true } });
    const { lines, io } = capture();
    expect(run(root, io)).toBe(0);
    expect(lines.log.join('\n')).toMatch(/objetivo de 20 kB \(informativo\): faltan [\d.]+ kB/);
  });

  it('con la medida en 197 kB o menos sugiere bajar el límite al objetivo', () => {
    const root = fixture({ bytes: 5000, budget: { limitKb: 215, targetKb: 200, enforce: true } });
    const { lines, io } = capture();
    run(root, io);
    expect(lines.log.join('\n')).toContain('el límite puede bajarse a 200 kB');
  });
});
