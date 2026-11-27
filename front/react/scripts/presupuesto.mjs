// Presupuesto de bytes (D-R29, D-R37, M-15, M-11): `npm run presupuesto`, tras `npm run build`.
//
// Suma el tamaño gzip del trozo inicial de la demo (el módulo de entrada de `demo/dist`, sus
// importaciones estáticas y sus hojas de estilo, según `demo/dist/.vite/manifest.json` de Vite) y lo
// compara con el límite. Las importaciones dinámicas no cuentan: son los trozos diferidos.
// También informa del tamaño gzip de `dist/karpo-react.js` (la biblioteca), sin límite propio.
//
// Límite y obligatoriedad salen de `scripts/presupuesto.json`
// (`{ "limitKb": 215, "targetKb": 200, "enforce": true }`): `limitKb` es el techo obligatorio y `targetKb` el
// objetivo informativo (D-R37; si la medida queda en 197 kB o menos, el límite puede bajarse a 200).
// Si el fichero no existe: 250 kB y sólo aviso (código de salida 0).
// Un kB son 1000 bytes, como en el informe de Vite.
//
// `--detalle` imprime además la atribución por paquete del trozo inicial, leída de los mapas de fuentes de
// `demo/dist`, y comprueba que NO entra en él lo que debe ir diferido (iconos extra, pantallas de acceso y
// textos de las vistas): con `enforce`, encontrarlo también hace fallar.
import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { gzipSync } from 'node:zlib';

export const DEFAULT_BUDGET = Object.freeze({ limitKb: 250, enforce: false });

/** @param {Uint8Array | string} content */
export function gzipBytes(content) {
  return gzipSync(content, { level: 9 }).length;
}

/**
 * Lee `scripts/presupuesto.json`; sin fichero, los valores por omisión.
 * @param {string} file
 * @returns {{ limitKb: number, targetKb?: number, enforce: boolean }}
 */
export function readBudget(file) {
  if (!existsSync(file)) return { ...DEFAULT_BUDGET };
  const parsed = JSON.parse(readFileSync(file, 'utf8'));
  const limitKb = parsed.limitKb ?? DEFAULT_BUDGET.limitKb;
  if (typeof limitKb !== 'number' || !Number.isFinite(limitKb) || limitKb <= 0) {
    throw new Error(`${path.basename(file)}: limitKb debe ser un número positivo`);
  }
  const targetKb = parsed.targetKb;
  if (targetKb !== undefined && (typeof targetKb !== 'number' || !Number.isFinite(targetKb) || targetKb <= 0 || targetKb > limitKb)) {
    throw new Error(`${path.basename(file)}: targetKb debe ser un número positivo que no supere limitKb`);
  }
  if (parsed.enforce !== undefined && typeof parsed.enforce !== 'boolean') {
    throw new Error(`${path.basename(file)}: enforce debe ser true o false`);
  }
  return {
    limitKb,
    ...(targetKb === undefined ? {} : { targetKb }),
    enforce: parsed.enforce ?? DEFAULT_BUDGET.enforce,
  };
}

/**
 * Ficheros del trozo inicial: las entradas del manifiesto (`isEntry`), sus importaciones estáticas
 * (recursivas, sin repetir) y las hojas de estilo de todos ellos. Las dinámicas se ignoran.
 *
 * @param {Record<string, { file: string, isEntry?: boolean, imports?: string[], css?: string[] }>} manifest
 * @returns {{ key: string, file: string, kind: 'js' | 'css' }[]}
 */
export function initialChunkFiles(manifest) {
  const entries = Object.entries(manifest).filter(([, chunk]) => chunk.isEntry);
  if (entries.length === 0) throw new Error('el manifiesto no tiene ninguna entrada (isEntry)');
  const seen = new Set();
  /** @type {{ key: string, file: string, kind: 'js' | 'css' }[]} */
  const files = [];
  const visit = (key) => {
    if (seen.has(key)) return;
    seen.add(key);
    const chunk = manifest[key];
    if (!chunk) throw new Error(`el manifiesto no tiene el trozo «${key}»`);
    files.push({ key, file: chunk.file, kind: 'js' });
    for (const css of chunk.css ?? []) {
      if (!files.some((entry) => entry.file === css)) files.push({ key, file: css, kind: 'css' });
    }
    for (const imported of chunk.imports ?? []) visit(imported);
  };
  for (const [key] of entries) visit(key);
  return files;
}

/**
 * @param {{ gzipBytes: number, limitKb: number, enforce: boolean }} input
 * @returns {{ overBudget: boolean, exitCode: number, limitBytes: number }}
 */
export function evaluateBudget({ gzipBytes: bytes, limitKb, enforce }) {
  const limitBytes = Math.round(limitKb * 1000);
  const overBudget = bytes > limitBytes;
  return { overBudget, exitCode: overBudget && enforce ? 1 : 0, limitBytes };
}

export function formatKb(bytes) {
  return `${(bytes / 1000).toFixed(1)} kB`;
}

// ---------------------------------------------------------------------------------------------
// Atribución por paquete con el mapa de fuentes (`--detalle`)

const BASE64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
const BASE64_VALUE = new Map([...BASE64].map((char, index) => [char, index]));

/**
 * Decodifica el campo `mappings` de un mapa de fuentes (v3) a segmentos por línea generada.
 * Un segmento es `{ column, source }`; `source` es el índice de `sources` o `-1` si no tiene fuente.
 *
 * @param {string} mappings
 * @returns {{ column: number, source: number }[][]}
 */
export function decodeMappings(mappings) {
  const lines = [];
  let source = 0;
  for (const lineText of mappings.split(';')) {
    const segments = [];
    let column = 0;
    for (const segmentText of lineText.split(',')) {
      if (segmentText === '') continue;
      /** @type {number[]} */
      const fields = [];
      let shift = 0;
      let value = 0;
      for (const char of segmentText) {
        const digit = BASE64_VALUE.get(char);
        if (digit === undefined) throw new Error(`mapa de fuentes con un carácter no válido: ${char}`);
        value += (digit & 31) << shift;
        if (digit & 32) {
          shift += 5;
        } else {
          fields.push(value & 1 ? -(value >> 1) : value >> 1);
          value = 0;
          shift = 0;
        }
      }
      column += fields[0] ?? 0;
      if (fields.length >= 4) {
        source += fields[1] ?? 0;
        segments.push({ column, source });
      } else {
        segments.push({ column, source: -1 });
      }
    }
    lines.push(segments);
  }
  return lines;
}

const NO_SOURCE = '(sin fuente)';

/**
 * Caracteres del código generado que corresponden a cada fuente del mapa. Cada segmento cubre desde su
 * columna hasta la del siguiente (o el final de la línea).
 *
 * @param {string} code contenido del fichero .js
 * @param {{ sources: string[], mappings: string }} map
 * @returns {Map<string, number>} fuente (como figura en el mapa; `(sin fuente)` si no la hay) → caracteres
 */
export function attributeBySource(code, map) {
  const codeLines = code.split('\n');
  /** @type {Map<string, number>} */
  const totals = new Map();
  const add = (name, count) => {
    if (count > 0) totals.set(name, (totals.get(name) ?? 0) + count);
  };
  decodeMappings(map.mappings).forEach((segments, lineIndex) => {
    const length = (codeLines[lineIndex] ?? '').length;
    const first = segments[0];
    if (!first) {
      add(NO_SOURCE, length);
      return;
    }
    add(NO_SOURCE, Math.min(first.column, length));
    segments.forEach((segment, index) => {
      const next = segments[index + 1];
      const end = next ? next.column : length;
      const name = segment.source < 0 ? NO_SOURCE : (map.sources[segment.source] ?? NO_SOURCE);
      add(name, Math.max(0, Math.min(end, length) - Math.min(segment.column, length)));
    });
  });
  // Los saltos de línea también pesan.
  add(NO_SOURCE, Math.max(0, codeLines.length - 1));
  return totals;
}

/** Agrupa una fuente por paquete de npm (`react-dom`, `@tanstack/query-core`) o por carpeta propia (`src/ui`). */
export function groupOfSource(source) {
  const normalized = source.replaceAll('\\', '/');
  const modules = normalized.lastIndexOf('node_modules/');
  if (modules !== -1) {
    const parts = normalized.slice(modules + 'node_modules/'.length).split('/');
    return parts[0]?.startsWith('@') ? parts.slice(0, 2).join('/') : (parts[0] ?? normalized);
  }
  if (normalized.startsWith('(')) return normalized;
  const own = /(?:^|\/)(src|demo)\/([^/]+)(?:\/|$)/.exec(normalized);
  if (own) {
    // Un fichero suelto de src/ (`src/index.ts`) no es una carpeta.
    return /\.[a-z]+$/.test(own[2] ?? '') ? `${own[1]}` : `${own[1]}/${own[2]}`;
  }
  return normalized.replace(/^(\.\.\/)+/, '');
}

/**
 * Lo que NO debe estar en el trozo inicial (D-R37, M-11): cada regla es una expresión sobre la ruta de la
 * fuente y la razón.
 */
export const NOT_IN_INITIAL = Object.freeze([
  { pattern: /(^|\/)icon-data-extra\.ts$/, reason: 'los iconos que no son del núcleo van en su trozo diferido (icon-data-extra)' },
  { pattern: /(^|\/)auth\/(LoginPage|ChangePasswordPage)\.tsx$/, reason: 'las pantallas de acceso se cargan con lazy()' },
  {
    pattern: /(^|\/)locales\/[^/]+\/(?!(?:core|ui|fields|auth|errors|help)\.json$)[^/]+\.json$/,
    reason: 'los textos de las vistas (grid, detail, tree, map…) y los idiomas no activos llegan por demanda',
  },
]);

/**
 * Fuentes del trozo inicial que no deberían estar en él.
 * @param {Iterable<string>} sources
 * @returns {{ source: string, reason: string }[]}
 */
export function forbiddenInInitial(sources) {
  const found = [];
  for (const source of new Set(sources)) {
    const normalized = source.replaceAll('\\', '/');
    const rule = NOT_IN_INITIAL.find((candidate) => candidate.pattern.test(normalized));
    if (rule) found.push({ source: normalized, reason: rule.reason });
  }
  return found.sort((a, b) => a.source.localeCompare(b.source));
}

/**
 * Atribución del trozo inicial: lee `<fichero>.js.map` de cada .js del trozo.
 *
 * @param {string} distFolder demo/dist
 * @param {{ file: string, kind: 'js' | 'css' }[]} entries ficheros del trozo inicial
 * @returns {{ groups: { group: string, chars: number, gzip: number }[], sources: Set<string>, missing: string[] }}
 */
export function attribution(distFolder, entries) {
  /** @type {Map<string, { group: string, chars: number, gzip: number }>} */
  const byGroup = new Map();
  const sources = new Set();
  const missing = [];
  for (const entry of entries.filter((candidate) => candidate.kind === 'js')) {
    const file = path.join(distFolder, ...entry.file.split('/'));
    const mapFile = `${file}.map`;
    if (!existsSync(mapFile)) {
      missing.push(entry.file);
      continue;
    }
    const code = readFileSync(file, 'utf8');
    const map = JSON.parse(readFileSync(mapFile, 'utf8'));
    const ratio = gzipBytes(code) / Math.max(1, code.length);
    for (const [source, chars] of attributeBySource(code, map)) {
      sources.add(source);
      const group = groupOfSource(source);
      const current = byGroup.get(group) ?? { group, chars: 0, gzip: 0 };
      current.chars += chars;
      current.gzip += chars * ratio;
      byGroup.set(group, current);
    }
  }
  const groups = [...byGroup.values()].sort((a, b) => b.gzip - a.gzip);
  return { groups, sources, missing };
}

/**
 * Mide y escribe el desglose. Devuelve el código de salida (no llama a `process.exit`).
 *
 * @param {string} root carpeta de `front/react`
 * @param {{ log?: (line: string) => void, error?: (line: string) => void }} [io]
 * @param {{ detalle?: boolean }} [options] `detalle`: atribución por paquete y comprobación de lo diferido
 * @returns {number}
 */
export function run(root, io = {}, options = {}) {
  const log = io.log ?? console.log;
  const error = io.error ?? console.error;
  const manifestFile = path.join(root, 'demo', 'dist', '.vite', 'manifest.json');
  if (!existsSync(manifestFile)) {
    error('presupuesto: falta demo/dist/.vite/manifest.json; ejecuta `npm run build` antes.');
    return 1;
  }
  let budget;
  try {
    budget = readBudget(path.join(root, 'scripts', 'presupuesto.json'));
  } catch (cause) {
    error(`presupuesto: ${cause instanceof Error ? cause.message : String(cause)}`);
    return 1;
  }
  const manifest = JSON.parse(readFileSync(manifestFile, 'utf8'));
  const rows = [];
  const entries = initialChunkFiles(manifest);
  for (const entry of entries) {
    const file = path.join(root, 'demo', 'dist', ...entry.file.split('/'));
    if (!existsSync(file)) {
      error(`presupuesto: el manifiesto cita ${entry.file}, que no está en demo/dist.`);
      return 1;
    }
    rows.push({ file: entry.file, gzip: gzipBytes(readFileSync(file)) });
  }
  const total = rows.reduce((sum, row) => sum + row.gzip, 0);
  log('presupuesto: trozo inicial de la demo (entrada + importaciones estáticas + hojas), gzip');
  for (const row of [...rows].sort((a, b) => b.gzip - a.gzip)) {
    log(`  ${formatKb(row.gzip).padStart(10)}  ${row.file}`);
  }
  log(`  ${formatKb(total).padStart(10)}  TOTAL (${rows.length} ficheros)`);

  const library = path.join(root, 'dist', 'karpo-react.js');
  if (existsSync(library)) {
    log(`presupuesto: dist/karpo-react.js ${formatKb(gzipBytes(readFileSync(library)))} gzip (sin límite propio)`);
  } else {
    log('presupuesto: dist/karpo-react.js no existe (¿se ejecutó `npm run build:lib`?)');
  }

  let exitCode = 0;
  if (options.detalle) {
    const detail = attribution(path.join(root, 'demo', 'dist'), entries);
    if (detail.groups.length === 0) {
      log(
        `presupuesto: sin mapa de fuentes para ${detail.missing.join(', ')}; no se puede atribuir ` +
          '(¿`sourcemap: true` en vite.config.ts?).',
      );
    } else {
      // El pequeño módulo de runtime de Rolldown no trae mapa: se avisa y se sigue con el resto.
      if (detail.missing.length > 0) log(`presupuesto: sin mapa de fuentes (no se atribuye): ${detail.missing.join(', ')}.`);
      log('presupuesto: atribución del trozo inicial por paquete o carpeta (gzip aproximado, por proporción de caracteres)');
      for (const row of detail.groups.slice(0, 30)) {
        log(`  ${formatKb(row.gzip).padStart(10)}  ${(row.chars / 1000).toFixed(1).padStart(7)} kB sin comprimir  ${row.group}`);
      }
      const forbidden = forbiddenInInitial(detail.sources);
      if (forbidden.length === 0) {
        log('presupuesto: el trozo inicial no trae icon-data-extra, las pantallas de acceso ni los textos de las vistas.');
      } else {
        for (const item of forbidden) {
          const line = `presupuesto: ${item.source} está en el trozo inicial y no debería: ${item.reason}.`;
          if (budget.enforce) error(line);
          else log(line);
        }
        if (budget.enforce) exitCode = 1;
      }
    }
  }

  const verdict = evaluateBudget({ gzipBytes: total, limitKb: budget.limitKb, enforce: budget.enforce });
  const mode = budget.enforce ? 'obligatorio' : 'sólo aviso';
  if (verdict.overBudget) {
    const line = `presupuesto: ${formatKb(total)} supera el límite de ${budget.limitKb} kB (${mode}).`;
    if (budget.enforce) error(line);
    else log(`${line} Sigue siendo sólo un aviso: crea scripts/presupuesto.json con enforce: true para exigirlo.`);
  } else {
    log(`presupuesto: dentro del límite de ${budget.limitKb} kB (${mode}); margen ${formatKb(verdict.limitBytes - total)}.`);
  }
  if (budget.targetKb !== undefined) {
    const targetBytes = Math.round(budget.targetKb * 1000);
    if (total <= targetBytes) {
      log(`presupuesto: objetivo de ${budget.targetKb} kB alcanzado (${formatKb(total)}).`);
    } else {
      log(`presupuesto: objetivo de ${budget.targetKb} kB (informativo): faltan ${formatKb(total - targetBytes)}.`);
    }
    if (budget.targetKb < budget.limitKb && total <= 197_000) {
      log(
        `presupuesto: la medida (${formatKb(total)}) deja margen: el límite puede bajarse a ${budget.targetKb} kB ` +
          'en scripts/presupuesto.json.',
      );
    }
  }
  return Math.max(verdict.exitCode, exitCode);
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
  process.exit(run(root, {}, { detalle: process.argv.includes('--detalle') }));
}
