// Informe de licencias (PLAN §3.5). Sin dependencias.
//
//   node scripts/licencias.mjs           escribe docs/LICENCIAS.md (conserva lo escrito a mano)
//   node scripts/licencias.mjs --check   falla si hay una licencia no permitida, si el informe
//                                        está desactualizado o si un origen incrustado del registro de
//                                        `avisos.mjs` no está en package-lock.json; no escribe nada
//   node scripts/licencias.mjs --avisos  escribe dist/THIRD-PARTY-NOTICES.md (lo mismo que emite el
//                                        build de la biblioteca; B-1, D-R31)
//
// Lee package.json, package-lock.json y, si existe node_modules, el package.json de cada paquete.
// La parte generada del informe va entre marcadores; fuera de ellos, el texto es de quien lo edite
// (por ejemplo, las desviaciones de versión respecto al plan).
import { existsSync, readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { NOTICES_FILE, ORIGENES, missingGenerators, readInstalledLicense, renderNotices } from './avisos.mjs';

export const BEGIN = '<!-- licencias:inicio (generado por scripts/licencias.mjs; no editar) -->';
export const END = '<!-- licencias:fin -->';

/** Licencias admitidas para las dependencias DIRECTAS. */
export const DIRECT_ALLOWED = new Set(['MIT', 'Apache-2.0', 'BSD-2-Clause', 'BSD-3-Clause']);

/** Licencias permisivas admitidas en transitivas (además de las directas). */
export const TRANSITIVE_PERMISSIVE = new Set([
  ...DIRECT_ALLOWED,
  'ISC',
  '0BSD',
  'CC0-1.0',
  'BlueOak-1.0.0',
  'MIT-0',
  'CC-BY-3.0',
  'CC-BY-4.0',
  'PSF-2.0',
  'Python-2.0',
  'Unlicense',
  'Zlib',
  'BSD-1-Clause',
]);

/** Copyleft fuerte y licencias comerciales o no libres: nunca. */
const FORBIDDEN = /^(A?GPL|LGPL|SSPL|EUPL|EPL|CDDL|OSL|CPAL|RPL|BUSL|CPOL|Sleepycat|Elastic|PolyForm|Commons-Clause|UNLICENSED|SEE LICENSE|Proprietary|Commercial)/i;

/** MPL-2.0 sólo para estas herramientas de compilación, con su motivo. */
export const EXCEPTIONS = [
  {
    license: 'MPL-2.0',
    match: /^lightningcss(-.+)?$/,
    reason:
      'Herramienta de compilación: Vite la usa para transformar y minificar CSS. No se distribuye con el ' +
      'paquete (`files` sólo incluye `dist/`) ni se enlaza en tiempo de ejecución; MPL-2.0 es copyleft débil ' +
      'por archivo y no afecta a nuestro código. Consulta Q-6: se usa por defecto mientras Javier no decida; ' +
      'alternativa `cssMinify: esbuild`. Ojo: los `targets` de lightningcss cambian el CSS ' +
      'publicado (por ejemplo, compila `:dir()` a `:lang()`, que no sirve con `?dir=rtl`; D-R22).',
  },
];

// ---------------------------------------------------------------------------------------------
// Expresiones SPDX mínimas: ids, AND, OR, WITH y paréntesis.

/** @returns {string[]} identificadores de licencia de una expresión */
export function licenseIds(expression) {
  return expression
    .replace(/[()]/g, ' ')
    .split(/\s+(?:AND|OR|WITH)\s+|\s+/i)
    .map((token) => token.trim())
    .filter((token) => token && !/^(AND|OR|WITH)$/i.test(token));
}

/**
 * Evalúa una expresión SPDX: OR = basta una alternativa; AND = todas.
 * @param {string} expression
 * @param {(id: string) => boolean} accepts
 */
export function evaluateExpression(expression, accepts) {
  const tokens = expression.match(/\(|\)|[^\s()]+/g) ?? [];
  let pos = 0;
  function parseOr() {
    let value = parseAnd();
    while (tokens[pos]?.toUpperCase() === 'OR') {
      pos += 1;
      const rhs = parseAnd();
      value = value || rhs;
    }
    return value;
  }
  function parseAnd() {
    let value = parseAtom();
    while (tokens[pos]?.toUpperCase() === 'AND') {
      pos += 1;
      const rhs = parseAtom();
      value = value && rhs;
    }
    return value;
  }
  function parseAtom() {
    const token = tokens[pos];
    if (token === '(') {
      pos += 1;
      const value = parseOr();
      if (tokens[pos] === ')') pos += 1;
      return value;
    }
    pos += 1;
    let id = token ?? '';
    // `GPL-2.0-only WITH Classpath-exception-2.0`: la excepción no cambia el id base.
    if (tokens[pos]?.toUpperCase() === 'WITH') pos += 2;
    id = id.replace(/\+$/, '');
    return accepts(id);
  }
  return parseOr();
}

/** Normaliza los formatos de licencia de package.json a una cadena (o `''` si no hay). */
export function licenseText(manifest) {
  const { license, licenses } = manifest ?? {};
  if (typeof license === 'string') return license.trim();
  if (license && typeof license === 'object' && typeof license.type === 'string') {
    return license.type.trim();
  }
  if (Array.isArray(licenses) && licenses.length > 0) {
    const types = licenses
      .map((entry) => (typeof entry === 'string' ? entry : entry?.type))
      .filter((type) => typeof type === 'string' && type);
    if (types.length === 1) return types[0];
    if (types.length > 1) return `(${types.join(' OR ')})`;
  }
  return '';
}

/** Nombre del paquete a partir de su ruta en `packages` del lock. */
export function nameFromLockPath(lockPath) {
  const marker = 'node_modules/';
  return lockPath.slice(lockPath.lastIndexOf(marker) + marker.length);
}

// ---------------------------------------------------------------------------------------------
// Análisis

/**
 * @typedef {{ name: string, version: string, license: string, dev: boolean, optional: boolean,
 *             lockPath: string }} PackageInfo
 * @typedef {{ id: string, message: string }} Problem
 */

/**
 * Clasifica una licencia de transitiva.
 * @returns {{ status: 'permissive' | 'exception' | 'forbidden' | 'missing' | 'unknown',
 *             exception?: typeof EXCEPTIONS[number] }}
 */
export function classifyTransitive(name, license) {
  if (!license) return { status: 'missing' };
  if (evaluateExpression(license, (id) => TRANSITIVE_PERMISSIVE.has(id))) {
    return { status: 'permissive' };
  }
  const exception = EXCEPTIONS.find(
    (candidate) =>
      candidate.match.test(name) &&
      evaluateExpression(license, (id) => TRANSITIVE_PERMISSIVE.has(id) || id === candidate.license),
  );
  if (exception) return { status: 'exception', exception };
  if (licenseIds(license).some((id) => FORBIDDEN.test(id))) return { status: 'forbidden' };
  return { status: 'unknown' };
}

/**
 * @param {{ pkg: any, lock: any, readInstalled?: (lockPath: string) => any,
 *           origenes?: readonly import('./avisos.mjs').Origen[] }} input
 *   `origenes`: registro de lo incrustado en `dist/` (`avisos.mjs`); cada uno debe estar en el lock.
 */
export function analyze({ pkg, lock, readInstalled = () => undefined, origenes = [] }) {
  /** @type {Problem[]} */
  const problems = [];
  /** @type {PackageInfo[]} */
  const packages = [];
  for (const [lockPath, entry] of Object.entries(lock.packages ?? {})) {
    if (lockPath === '' || entry.link) continue;
    const name = entry.name ?? nameFromLockPath(lockPath);
    const installed = readInstalled(lockPath);
    const fromLock = licenseText(entry);
    const fromDisk = installed ? licenseText(installed) : '';
    if (installed && fromDisk && fromLock && fromDisk !== fromLock) {
      problems.push({
        id: `${name}@${entry.version}`,
        message: `la licencia del lock (${fromLock}) no coincide con la instalada (${fromDisk})`,
      });
    }
    packages.push({
      name,
      version: entry.version ?? installed?.version ?? '?',
      license: fromDisk || fromLock,
      dev: Boolean(entry.dev),
      optional: Boolean(entry.optional),
      lockPath,
    });
  }
  packages.sort((a, b) => a.name.localeCompare(b.name) || a.version.localeCompare(b.version));

  const byName = new Map();
  for (const info of packages) {
    // En el árbol hay copias anidadas; para las directas manda la de primer nivel.
    if (info.lockPath === `node_modules/${info.name}`) byName.set(info.name, info);
  }

  /** @type {Array<PackageInfo & { kinds: string[] }>} */
  const direct = [];
  const kindsOf = new Map();
  const add = (names, kind) => {
    for (const name of Object.keys(names ?? {})) {
      kindsOf.set(name, [...(kindsOf.get(name) ?? []), kind]);
    }
  };
  add(pkg.dependencies, 'ejecución');
  add(pkg.peerDependencies, 'peer');
  add(pkg.devDependencies, 'desarrollo');
  for (const [name, kinds] of [...kindsOf].sort(([a], [b]) => a.localeCompare(b))) {
    const info = byName.get(name);
    if (!info) {
      if (!(pkg.peerDependencies?.[name] && kinds.length === 1)) {
        problems.push({ id: name, message: 'dependencia directa que no está en package-lock.json' });
      }
      continue;
    }
    direct.push({ ...info, kinds });
    if (!info.license) {
      problems.push({ id: name, message: 'dependencia directa sin licencia' });
    } else if (!evaluateExpression(info.license, (id) => DIRECT_ALLOWED.has(id))) {
      problems.push({
        id: name,
        message: `dependencia directa con licencia no admitida (${info.license}); sólo MIT, Apache-2.0 o BSD`,
      });
    }
  }

  const directNames = new Set(direct.map((info) => info.name));
  /** @type {Map<string, PackageInfo[]>} */
  const groups = new Map();
  /** @type {Array<{ info: PackageInfo, exception: typeof EXCEPTIONS[number] }>} */
  const exceptions = [];
  for (const info of packages) {
    if (info.lockPath === `node_modules/${info.name}` && directNames.has(info.name)) continue;
    const verdict = classifyTransitive(info.name, info.license);
    if (verdict.status === 'exception') {
      exceptions.push({ info, exception: verdict.exception });
    } else if (verdict.status === 'missing') {
      problems.push({ id: `${info.name}@${info.version}`, message: 'transitiva sin licencia' });
    } else if (verdict.status === 'forbidden') {
      problems.push({
        id: `${info.name}@${info.version}`,
        message: `licencia copyleft fuerte, comercial o no libre (${info.license})`,
      });
    } else if (verdict.status === 'unknown') {
      problems.push({
        id: `${info.name}@${info.version}`,
        message: `licencia que no está en la lista admitida (${info.license}); revisar y, si procede, añadirla al script`,
      });
    }
    const key = info.license || '(sin licencia)';
    groups.set(key, [...(groups.get(key) ?? []), info]);
  }

  /** Incrustados en dist/: cada origen del registro tiene que ser un paquete del lock (con su versión y licencia). */
  const embedded = origenes.map((origen) => {
    const info = byName.get(origen.paquete);
    if (!info) {
      problems.push({
        id: origen.paquete,
        message: 'origen incrustado en dist/ (scripts/avisos.mjs) que no está en package-lock.json',
      });
    } else if (info.license && info.license !== origen.licencia) {
      problems.push({
        id: origen.paquete,
        message: `el registro de avisos dice ${origen.licencia} pero el paquete declara ${info.license}`,
      });
    }
    return { origen, version: info?.version ?? '?', license: info?.license ?? origen.licencia };
  });

  return { packages, direct, groups, exceptions, embedded, problems };
}

// ---------------------------------------------------------------------------------------------
// Informe

function table(headers, rows) {
  const line = (cells) => `| ${cells.join(' | ')} |`;
  return [line(headers), line(headers.map(() => '---')), ...rows.map(line)].join('\n');
}

function uniqueList(infos) {
  const seen = new Set();
  const out = [];
  for (const info of infos) {
    const label = `${info.name}@${info.version}`;
    if (!seen.has(label)) {
      seen.add(label);
      out.push(`\`${label}\``);
    }
  }
  return out.join(', ');
}

/** Parte generada del informe (determinista: sin fechas ni rutas de la máquina). */
export function renderGenerated(pkg, analysis) {
  const { direct, groups, exceptions, problems, packages, embedded = [] } = analysis;
  const runtimeTransitive = [...groups.values()].flat().filter((info) => !info.dev).length;
  const lines = [];
  lines.push(BEGIN, '');
  lines.push('## Resumen', '');
  lines.push(
    `- Paquete: \`${pkg.name}@${pkg.version}\`, licencia \`${pkg.license}\` (consulta Q-3).`,
    `- Paquetes en \`package-lock.json\`: ${packages.length}; directas: ${direct.length}.`,
    `- Transitivas que llegan a quien instala el paquete (no son de desarrollo): ${runtimeTransitive}.`,
    `- Directas admitidas: ${[...DIRECT_ALLOWED].join(', ')}.`,
    `- Transitivas admitidas además: ${[...TRANSITIVE_PERMISSIVE].filter((id) => !DIRECT_ALLOWED.has(id)).join(', ')}.`,
    '- Nunca: GPL, AGPL, LGPL, SSPL, EUPL, EPL, licencias comerciales o sin licencia.',
    `- Estado de la comprobación: ${problems.length === 0 ? 'sin incidencias' : `${problems.length} incidencia(s)`}.`,
    '',
  );
  lines.push('## Dependencias directas', '');
  lines.push(
    table(
      ['Paquete', 'Versión', 'Licencia', 'Uso'],
      direct.map((info) => [`\`${info.name}\``, info.version, info.license || '(ninguna)', info.kinds.join(' + ')]),
    ),
    '',
  );
  lines.push('## Dependencias transitivas por licencia', '');
  const ordered = [...groups.entries()].sort(
    ([a, listA], [b, listB]) => listB.length - listA.length || a.localeCompare(b),
  );
  for (const [license, infos] of ordered) {
    const count = new Set(infos.map((info) => `${info.name}@${info.version}`)).size;
    lines.push(`### ${license} (${count})`, '');
    if (license === 'MIT' || license === 'ISC') {
      lines.push(`Lista completa en \`package-lock.json\` (${count} paquetes).`, '');
    } else {
      lines.push(uniqueList(infos), '');
    }
  }
  if (embedded.length > 0) {
    lines.push('## Incrustado en dist/ (avisos de terceros)', '');
    lines.push(
      'Estos proyectos no llegan como dependencia de quien instala el paquete: se copian dentro de `dist/` y su ' +
        'licencia exige conservar su aviso. El texto íntegro está en `dist/' + NOTICES_FILE + '` (se genera al ' +
        'construir la biblioteca; `npm run licencias -- --avisos` lo regenera). El registro es `scripts/avisos.mjs`.',
      '',
    );
    lines.push(
      table(
        ['Paquete', 'Versión', 'Licencia', 'Dónde queda', 'Cómo entra'],
        embedded.map(({ origen, version, license }) => [
          `\`${origen.paquete}\``,
          version,
          license,
          origen.donde.join('; '),
          origen.entra,
        ]),
      ),
      '',
    );
  }
  lines.push('## Excepciones aceptadas', '');
  if (exceptions.length === 0) {
    lines.push('Ninguna.', '');
  } else {
    const byReason = new Map();
    for (const { info, exception } of exceptions) {
      const key = `${exception.license}\u0000${exception.reason}`;
      byReason.set(key, [...(byReason.get(key) ?? []), info]);
    }
    lines.push(
      table(
        ['Licencia', 'Paquetes', 'Motivo'],
        [...byReason].map(([key, infos]) => {
          const [license, reason] = key.split('\u0000');
          return [license, uniqueList(infos), reason];
        }),
      ),
      '',
    );
  }
  if (problems.length > 0) {
    lines.push('## Incidencias', '');
    for (const problem of problems) lines.push(`- \`${problem.id}\`: ${problem.message}`);
    lines.push('');
  }
  lines.push(END);
  return lines.join('\n');
}

const MANUAL_TEMPLATE = `
## Desviaciones respecto a las versiones del plan

Las versiones directas de \`package.json\` son las de PLAN §3, sin \`^\` ni \`~\`. Si al instalar alguna
no casa con otra, se baja a la última compatible, se comprueba la licencia con \`npm view\` y se anota
aquí.

Ninguna por ahora.
`;

export function renderDocument(pkg, analysis, existing) {
  const generated = renderGenerated(pkg, analysis);
  if (existing) {
    const start = existing.indexOf(BEGIN);
    const end = existing.indexOf(END);
    if (start !== -1 && end !== -1 && end > start) {
      return existing.slice(0, start) + generated + existing.slice(end + END.length);
    }
  }
  return `# Licencias de dependencias de @karpo/react\n\n${generated}\n${MANUAL_TEMPLATE}`;
}

// ---------------------------------------------------------------------------------------------
// Programa

function readJson(file) {
  return JSON.parse(readFileSync(file, 'utf8'));
}

function main(argv) {
  const check = argv.includes('--check');
  const notices = argv.includes('--avisos');
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
  const pkg = readJson(path.join(root, 'package.json'));
  const lock = readJson(path.join(root, 'package-lock.json'));
  const readInstalled = (lockPath) => {
    const file = path.join(root, ...lockPath.split('/'), 'package.json');
    return existsSync(file) ? readJson(file) : undefined;
  };
  const analysis = analyze({ pkg, lock, readInstalled, origenes: ORIGENES });
  for (const message of missingGenerators(root, ORIGENES)) {
    analysis.problems.push({ id: 'avisos', message });
  }
  if (notices) {
    const file = path.join(root, 'dist', NOTICES_FILE);
    mkdirSync(path.dirname(file), { recursive: true });
    writeFileSync(file, renderNotices({ root, pkg }));
    const versions = ORIGENES.map((origen) => `${origen.paquete}@${readInstalledLicense(root, origen.paquete).version}`);
    console.log(`licencias: escrito dist/${NOTICES_FILE} (${versions.join(', ')}).`);
    return;
  }
  const target = path.join(root, 'docs', 'LICENCIAS.md');
  const existing = existsSync(target) ? readFileSync(target, 'utf8') : undefined;
  const next = renderDocument(pkg, analysis, existing);

  if (analysis.problems.length > 0) {
    console.error('licencias: incidencias de licencia');
    for (const problem of analysis.problems) console.error(`  - ${problem.id}: ${problem.message}`);
  }

  if (check) {
    let failed = analysis.problems.length > 0;
    // El informe se compara sin distinguir saltos de línea (Windows guarda CRLF).
    if ((existing ?? '').replaceAll('\r\n', '\n') !== next.replaceAll('\r\n', '\n')) {
      console.error('licencias: docs/LICENCIAS.md está desactualizado; ejecuta `npm run licencias`.');
      failed = true;
    }
    if (failed) process.exit(1);
    console.log(`licencias: correcto (${analysis.packages.length} paquetes, ${analysis.direct.length} directas).`);
    return;
  }

  mkdirSync(path.dirname(target), { recursive: true });
  writeFileSync(target, next);
  console.log(`licencias: escrito docs/LICENCIAS.md (${analysis.packages.length} paquetes).`);
  if (analysis.problems.length > 0) process.exitCode = 1;
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  main(process.argv.slice(2));
}
