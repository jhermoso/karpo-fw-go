// Genera los datos de iconos (D-R19, D-R37, B-1, M-11): los dibujos de los iconos de `ICON_NAMES`, sólo con los
// pesos `regular` y `fill`, como datos puros (cadenas `d` de trazados SVG). Así el paquete no lleva
// `@phosphor-icons/react` en ejecución (que trae seis pesos por icono) y `Icon` pinta el SVG con sus
// propios datos.
//
// Salen DOS módulos (el reparto lo fija `CORE_ICON_NAMES` de `src/contracts/icons.ts`):
//   src/ui/icon-data-core.ts   los iconos del marco: estático, dentro del trozo inicial, lectura síncrona
//   src/ui/icon-data-extra.ts  el resto: se pide con `import()` al arrancar y no cuenta para el presupuesto
// Un alias del mismo dibujo que uno del núcleo (`delete` y `trash`) va al núcleo, para no repetir el trazado.
//
//   npm run icons:gen             escribe los dos ficheros (y borra el antiguo src/ui/icon-data.ts)
//   npm run icons:gen -- --check  falla (código 1) si alguno no está al día o si queda el antiguo
//
// Fuente: `@phosphor-icons/react/dist/defs/<Nombre>.es.js` (MIT, © Phosphor Icons, dependencia de DESARROLLO;
// su aviso va en dist/THIRD-PARTY-NOTICES.md, registro en scripts/avisos.mjs). Cada módulo exporta un
// `Map<peso, elemento React>`; se renderiza el peso con `react-dom/server` y se leen los trazados del marcado.
// Cualquier otra cosa que un trazado simple (otro elemento o atributo) se rechaza: preferimos fallar al
// generar a pintar un icono distinto del que dibuja Phosphor.
import { existsSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { createElement, Fragment } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

export const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
export const OUTPUTS = Object.freeze({
  core: path.join(ROOT, 'src', 'ui', 'icon-data-core.ts'),
  extra: path.join(ROOT, 'src', 'ui', 'icon-data-extra.ts'),
});
/** Fichero único de antes del reparto: ya no debe existir. */
export const LEGACY_OUTPUT = path.join(ROOT, 'src', 'ui', 'icon-data.ts');
export const WEIGHTS = ['regular', 'fill'];

const DEFS_DIR = path.join(ROOT, 'node_modules', '@phosphor-icons', 'react', 'dist', 'defs');
const CONTRACT = path.join(ROOT, 'src', 'contracts', 'icons.ts');

/** `arrow-u-up-left` -> `ArrowUUpLeft` (nombre del módulo de `defs`). */
export function pascalCase(kebabName) {
  return kebabName
    .split('-')
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join('');
}

/**
 * Trazados (`d`) del marcado de un peso. Sólo se admite `<path d="…"/>`: es lo que usan los dos pesos
 * que generamos; si Phosphor añadiera otra cosa, mejor fallar.
 * @param {string} markup
 * @param {string} where nombre para el mensaje de error
 * @returns {string[]}
 */
export function pathsFromMarkup(markup, where) {
  const paths = [];
  const rest = markup.replace(/<path d="([^"]+)"><\/path>|<path d="([^"]+)"\/>/g, (_all, a, b) => {
    paths.push(a ?? b);
    return '';
  });
  if (rest.trim() !== '' || paths.length === 0) {
    throw new Error(`${where}: marcado de icono no admitido: ${markup.slice(0, 120)}`);
  }
  return paths;
}

/** Lee `ICON_NAMES`, `CORE_ICON_NAMES`, el mapa de alias y el nombre de Phosphor de cada uno. */
async function readContract() {
  const contract = await import(pathToFileURL(CONTRACT).href);
  return {
    names: [...contract.ICON_NAMES],
    coreNames: [...contract.CORE_ICON_NAMES],
    phosphorNameOf: contract.phosphorNameOf,
  };
}

/** Dibujos de un icono de Phosphor por nombre kebab: `{ regular: string[], fill: string[] }`. */
async function loadGlyph(phosphorName) {
  const file = path.join(DEFS_DIR, `${pascalCase(phosphorName)}.es.js`);
  if (!existsSync(file)) throw new Error(`no existe el icono de Phosphor «${phosphorName}» (${file})`);
  const defs = (await import(pathToFileURL(file).href)).default;
  const glyph = {};
  for (const weight of WEIGHTS) {
    const element = defs.get(weight);
    if (!element) throw new Error(`${phosphorName}: falta el peso ${weight}`);
    glyph[weight] = pathsFromMarkup(renderToStaticMarkup(createElement(Fragment, null, element)), `${phosphorName}/${weight}`);
  }
  return glyph;
}

const quote = (text) => `'${text.replaceAll('\\', '\\\\').replaceAll("'", "\\'")}'`;
const list = (paths) => `[${paths.map(quote).join(', ')}]`;

const header = (file, what) => [
  '// GENERADO por scripts/gen-icons.mjs (npm run icons:gen). No se edita a mano.',
  '//',
  `// ${what}`,
  '// Dibujos como cadenas `d` de trazados SVG (pesos regular y fill) en una cuadrícula de 256.',
  '// Origen: Phosphor Icons (MIT, © Phosphor Icons); su aviso va en dist/THIRD-PARTY-NOTICES.md.',
  `// Regenerar tras cambiar ICON_NAMES o CORE_ICON_NAMES: \`npm run icons:gen\`; \`-- --check\` lo comprueba (${file}).`,
];

/**
 * Reparto de los nombres entre núcleo y resto: un nombre va al núcleo si su dibujo (el icono de Phosphor al
 * que apunta) es el de alguno de `CORE_ICON_NAMES`.
 * @returns {{ core: string[], extra: string[] }}
 */
export function splitNames({ names, coreNames, phosphorNameOf }) {
  const coreTargets = new Set(coreNames.map((name) => phosphorNameOf(name)));
  const sorted = [...names].sort();
  return {
    core: sorted.filter((name) => coreTargets.has(phosphorNameOf(name))),
    extra: sorted.filter((name) => !coreTargets.has(phosphorNameOf(name))),
  };
}

async function appendRecord({ listNames, prefix, constName, doc, phosphorNameOf, lines }) {
  const targets = [...new Set(listNames.map((name) => phosphorNameOf(name)))].sort();
  const variable = new Map();
  for (const [index, target] of targets.entries()) {
    const id = `${prefix}${index}`;
    variable.set(target, id);
    const glyph = await loadGlyph(target);
    lines.push(`// ${target}`);
    lines.push(`const ${id}: IconData = { regular: ${list(glyph.regular)}, fill: ${list(glyph.fill)} };`);
  }
  lines.push('', doc);
  lines.push(`export const ${constName}: Readonly<Partial<Record<IconName, IconData>>> = {`);
  for (const name of listNames) {
    const key = /^[a-z][a-z0-9]*$/.test(name) ? name : quote(name);
    lines.push(`  ${key}: ${variable.get(phosphorNameOf(name))},`);
  }
  lines.push('};', '');
}

/**
 * Texto de los dos módulos. Los datos se escriben una vez por icono de Phosphor y los alias apuntan a la
 * misma constante, de modo que un sinónimo no duplica bytes.
 * @returns {Promise<{ core: string, extra: string }>}
 */
export async function buildSources() {
  const contract = await readContract();
  const { phosphorNameOf } = contract;
  const split = splitNames(contract);

  const core = [
    ...header('src/ui/icon-data-core.ts', 'Iconos del NÚCLEO (CORE_ICON_NAMES y sus alias): estáticos, dentro del trozo inicial.'),
    "import type { IconName } from '../contracts/icons';",
    '',
    '/** Dibujo de un icono: trazados de cada peso, en una cuadrícula de 256x256. */',
    'export interface IconData {',
    '  readonly regular: readonly string[];',
    '  readonly fill: readonly string[];',
    '}',
    '',
  ];
  await appendRecord({
    listNames: split.core,
    prefix: 'g',
    constName: 'ICON_DATA_CORE',
    doc: '/** Dibujo de cada nombre del núcleo (los alias comparten el del icono de Phosphor al que apuntan). */',
    phosphorNameOf,
    lines: core,
  });

  const extra = [
    ...header('src/ui/icon-data-extra.ts', 'Iconos que NO son del núcleo: trozo diferido, se pide con import() al arrancar.'),
    "import type { IconName } from '../contracts/icons';",
    "import type { IconData } from './icon-data-core';",
    '',
  ];
  await appendRecord({
    listNames: split.extra,
    prefix: 'x',
    constName: 'ICON_DATA_EXTRA',
    doc: '/** Dibujo de cada nombre que no es del núcleo. */',
    phosphorNameOf,
    lines: extra,
  });
  return { core: core.join('\n'), extra: extra.join('\n') };
}

const normalize = (text) => text.replace(/\r\n/g, '\n');

/**
 * Escribe o comprueba los ficheros.
 * @param {{ check?: boolean, outputs?: { core: string, extra: string }, legacy?: string | null,
 *           log?: (line: string) => void }} [options]
 *   `legacy`: ruta del antiguo icon-data.ts, que debe NO existir (por omisión, sólo con las salidas reales).
 * @returns {Promise<{ ok: boolean, changed: boolean }>}
 */
export async function run(options = {}) {
  const { check = false, outputs = OUTPUTS, log = (line) => console.log(line) } = options;
  const legacy = options.legacy === undefined ? (outputs === OUTPUTS ? LEGACY_OUTPUT : null) : options.legacy;
  const sources = await buildSources();
  let ok = true;
  let changed = false;
  for (const key of ['core', 'extra']) {
    const output = outputs[key];
    const label = path.relative(ROOT, output).replaceAll('\\', '/');
    const current = existsSync(output) ? normalize(readFileSync(output, 'utf8')) : undefined;
    const upToDate = current === sources[key];
    if (check) {
      log(upToDate ? `icons:gen: ${label} está al día.` : `icons:gen: ${label} no está al día; ejecuta \`npm run icons:gen\`.`);
      ok &&= upToDate;
    } else if (!upToDate) {
      writeFileSync(output, sources[key], 'utf8');
      changed = true;
      log(`icons:gen: ${label} escrito.`);
    }
  }
  if (legacy && existsSync(legacy)) {
    if (check) {
      log('icons:gen: sobra src/ui/icon-data.ts (lo sustituyó el reparto en núcleo y resto); ejecuta `npm run icons:gen`.');
      ok = false;
    } else {
      rmSync(legacy);
      changed = true;
      log('icons:gen: borrado el antiguo src/ui/icon-data.ts.');
    }
  }
  if (!check && !changed) log('icons:gen: sin cambios.');
  return { ok, changed };
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const result = await run({ check: process.argv.includes('--check') });
  process.exitCode = result.ok ? 0 : 1;
}
