// `npm run pack:check`: ejecuta `npm pack --dry-run --json` y comprueba el contenido del paquete.
// Sólo puede haber dist/**, package.json, README.md y LICENSE; y deben estar las entradas públicas.
//
// Avisos de terceros (B-1, D-R31): el paquete lleva `dist/THIRD-PARTY-NOTICES.md` con cada origen incrustado del
// registro de `scripts/avisos.mjs` y el texto íntegro de su licencia; las dos hojas de estilos empiezan por un
// comentario `/*!` que lo cita; y no puede haber ningún origen incrustado que no conste en el registro (un
// `@import` de un paquete en un CSS de `src/` o una fuente `node_modules/` en un mapa de `dist/`).
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { NOTICES_FILE, ORIGENES, bannerProblems, missingGenerators, noticesProblems, unregisteredEmbeds } from './avisos.mjs';

const ALLOWED_FILES = new Set(['package.json', 'README.md', 'LICENSE']);
const REQUIRED_FILES = [
  'package.json',
  'LICENSE',
  'dist/karpo-react.js',
  'dist/karpo.css',
  'dist/karpo-map.css',
  `dist/${NOTICES_FILE}`,
  'dist/types/index.d.ts',
];

/**
 * @param {string[]} files rutas del paquete tal como las lista npm
 * @returns {{ unexpected: string[], missing: string[] }}
 */
export function checkPackFiles(files) {
  const normalized = files.map((file) => file.replaceAll('\\', '/'));
  const unexpected = normalized.filter(
    (file) =>
      !(ALLOWED_FILES.has(file) || file.startsWith('dist/')) ||
      /\.(test|spec)\.d\.ts$/.test(file) ||
      file.includes('/test/'),
  );
  const missing = REQUIRED_FILES.filter((file) => !normalized.includes(file));
  return { unexpected, missing };
}

/** Selectores de cada hoja de estilos de la biblioteca: si falta uno, el empaquetador descartó esa hoja. */
const STYLESHEET_MARKERS = ['[data-kx-ui=skeleton]', '[data-kx-part=sr-only]', '[data-kx-shell]', '[data-kx-view=tree]', '[data-kx-view=map]'];

/**
 * @param {string} css contenido de dist/karpo.css
 * @returns {string[]} los marcadores que no aparecen
 */
export function missingStylesheetMarkers(css) {
  return STYLESHEET_MARKERS.filter((marker) => !css.includes(marker));
}

/**
 * dist/karpo-map.css (D-R24, B-3): la hoja de MapLibre debe traer `.maplibregl-map` y la regla propia
 * `font: inherit` sobre él (sin ella, el mapa usa Helvetica Neue y no la tipografía del tema).
 *
 * @param {string} css contenido de dist/karpo-map.css
 * @returns {string[]} lo que falta
 */
export function missingMapStylesheetParts(css) {
  const missing = [];
  if (!css.includes('.maplibregl-map')) missing.push('.maplibregl-map');
  if (!/\.maplibregl-map\s*\{[^}]*\bfont\s*:\s*inherit\b/.test(css)) {
    missing.push('.maplibregl-map { font: inherit }');
  }
  return missing;
}

/**
 * dist/karpo.css no lleva la hoja de MapLibre (M-16): va aparte, en `@karpo/react/map.css`.
 *
 * @param {string} css contenido de dist/karpo.css
 * @returns {boolean} true si se coló alguna regla `.maplibregl-`
 */
export function hasMapLibreRules(css) {
  return css.includes('.maplibregl-');
}

/**
 * Avisos de terceros del paquete ya construido.
 *
 * @param {{ root: string, notices: string | undefined, karpoCss: string, mapCss: string,
 *           origenes?: readonly import('./avisos.mjs').Origen[] }} input
 *   `notices`: contenido de dist/THIRD-PARTY-NOTICES.md (`undefined` si no está).
 * @returns {string[]} lo que falla (vacío si todo está bien)
 */
export function thirdPartyProblems({ root, notices, karpoCss, mapCss, origenes = ORIGENES }) {
  const problems = [];
  if (notices === undefined) {
    problems.push(`falta dist/${NOTICES_FILE}: lo emite \`npm run build:lib\` (o \`npm run licencias -- --avisos\`)`);
  } else {
    problems.push(...noticesProblems(notices, { root, origenes }));
  }
  problems.push(...bannerProblems('karpo.css', karpoCss, { root, origenes }));
  problems.push(...bannerProblems('karpo-map.css', mapCss, { root, origenes }));
  problems.push(...unregisteredEmbeds(root, origenes));
  problems.push(...missingGenerators(root, origenes));
  return problems;
}

function readOptional(file) {
  try {
    return readFileSync(file, 'utf8');
  } catch {
    return undefined;
  }
}

function runNpmPack(cwd) {
  const args = ['pack', '--dry-run', '--json', '--ignore-scripts'];
  const execPath = process.env.npm_execpath;
  // Desde `npm run`, npm_execpath apunta a npm-cli.js: se evita el shell (npm.cmd en Windows).
  const result =
    execPath && execPath.endsWith('.js')
      ? spawnSync(process.execPath, [execPath, ...args], { cwd, encoding: 'utf8' })
      : spawnSync('npm', args, { cwd, encoding: 'utf8', shell: process.platform === 'win32' });
  if (result.status !== 0) {
    throw new Error(`npm pack falló (${result.status}):\n${result.stderr}`);
  }
  const parsed = JSON.parse(result.stdout);
  const entry = Array.isArray(parsed) ? parsed[0] : parsed;
  return entry.files.map((file) => file.path);
}

function main() {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
  const files = runNpmPack(root);
  const { unexpected, missing } = checkPackFiles(files);
  if (unexpected.length > 0 || missing.length > 0) {
    if (unexpected.length > 0) {
      console.error(`pack:check: ficheros no permitidos en el paquete:\n  ${unexpected.join('\n  ')}`);
    }
    if (missing.length > 0) {
      console.error(`pack:check: faltan ficheros (¿se ejecutó build:lib?):\n  ${missing.join('\n  ')}`);
    }
    process.exit(1);
  }
  const lacking = missingStylesheetMarkers(readFileSync(path.join(root, 'dist', 'karpo.css'), 'utf8'));
  if (lacking.length > 0) {
    console.error(`pack:check: dist/karpo.css no incluye todas las hojas (faltan: ${lacking.join(', ')})`);
    process.exit(1);
  }
  if (hasMapLibreRules(readFileSync(path.join(root, 'dist', 'karpo.css'), 'utf8'))) {
    console.error('pack:check: dist/karpo.css lleva reglas de MapLibre; deben ir sólo en dist/karpo-map.css');
    process.exit(1);
  }
  const mapCss = readFileSync(path.join(root, 'dist', 'karpo-map.css'), 'utf8');
  const lackingMap = missingMapStylesheetParts(mapCss);
  if (lackingMap.length > 0) {
    console.error(`pack:check: dist/karpo-map.css no trae todo lo necesario (falta: ${lackingMap.join(', ')})`);
    process.exit(1);
  }
  const third = thirdPartyProblems({
    root,
    notices: readOptional(path.join(root, 'dist', NOTICES_FILE)),
    karpoCss: readFileSync(path.join(root, 'dist', 'karpo.css'), 'utf8'),
    mapCss,
  });
  if (third.length > 0) {
    console.error(`pack:check: avisos de terceros (B-1):\n  ${third.join('\n  ')}`);
    process.exit(1);
  }
  console.log(
    `pack:check: ${files.length} ficheros, todos dentro de dist/**, package.json, README.md y LICENSE; ` +
      `avisos de terceros de ${ORIGENES.length} origen(es) en ${NOTICES_FILE}.`,
  );
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  main();
}
