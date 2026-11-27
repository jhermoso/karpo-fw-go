// Avisos de terceros de lo que acaba COPIADO dentro de `dist/` (B-1, D-R31).
//
// `licencias.mjs` analiza las dependencias (`package.json`, `package-lock.json`). Esto es otra cosa: tres
// proyectos de terceros no llegan como dependencia de quien instala el paquete, sino incrustados en
// nuestros ficheros, y su licencia exige conservar su aviso de copyright. Aquí está el registro de esos
// orígenes y el generador de `dist/THIRD-PARTY-NOTICES.md`; lo usan `vite.lib.config.ts` (lo emite al
// construir), `vite.map-css.config.ts` (cabecera de `karpo-map.css`), `licencias.mjs` (`--avisos` y
// `--check`) y `pack-check.mjs` (lo exige en el paquete).
//
// El texto de cada licencia sale de `node_modules/<paquete>/LICENSE*` en el momento de generar, no se
// copia a mano: si el paquete cambia su copyright, el aviso lo sigue. La salida es determinista (sin
// fechas ni rutas de la máquina), de modo que dos construcciones seguidas dan el mismo fichero.
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

export const NOTICES_FILE = 'THIRD-PARTY-NOTICES.md';

/**
 * @typedef {object} Origen
 * @property {string} id identificador estable (ordena el documento)
 * @property {string} paquete nombre en npm
 * @property {string} licencia identificador SPDX, tal como lo declara el paquete
 * @property {string} motivo qué se incrusta y por qué
 * @property {string[]} donde dónde queda dentro de `dist/`
 * @property {string[]} cabeceraEn ficheros de `dist/` que llevan la cabecera `/*! … *\/` (sólo hojas de estilo)
 * @property {string} entra cómo entra en nuestro código (fichero y mecanismo)
 * @property {string[]} generadoPor ficheros de `src/` o `scripts/` que deben existir mientras el origen siga en el registro
 */

/**
 * Registro de lo incrustado. Un origen nuevo se añade AQUÍ; `pack:check` falla si algo nuevo entra sin
 * constar (un `@import` de un paquete en un CSS de `src/` o una fuente `node_modules/` en un mapa de `dist/`).
 *
 * @type {readonly Origen[]}
 */
export const ORIGENES = Object.freeze([
  {
    id: 'maplibre-gl',
    paquete: 'maplibre-gl',
    licencia: 'BSD-3-Clause',
    motivo:
      'Hoja de estilos de MapLibre GL JS (posición del lienzo, de los marcadores y de los controles del mapa). ' +
      'El motor del mapa en sí NO se copia: llega como dependencia de npm.',
    donde: ['`dist/karpo-map.css` (`@karpo/react/map.css`), casi entera'],
    cabeceraEn: ['karpo-map.css'],
    entra: '`src/map/maplibre.css`, con `@import url("maplibre-gl/dist/maplibre-gl.css")`',
    generadoPor: ['src/map/maplibre.css'],
  },
  {
    id: 'phosphor-icons',
    paquete: '@phosphor-icons/react',
    licencia: 'MIT',
    motivo:
      'Los trazados SVG (cadenas `d`) de los iconos de la interfaz, en los pesos `regular` y `fill`. Se copian ' +
      'a ficheros TypeScript propios al generar; el paquete de Phosphor no se necesita en ejecución.',
    donde: ['los trozos JavaScript de `dist/` que contienen los datos de iconos (`icon-data-core` e `icon-data-extra`)'],
    cabeceraEn: [],
    entra: '`scripts/gen-icons.mjs` genera `src/ui/icon-data-core.ts` y `src/ui/icon-data-extra.ts`',
    generadoPor: ['scripts/gen-icons.mjs', 'src/ui/icon-data-core.ts', 'src/ui/icon-data-extra.ts'],
  },
  {
    id: 'xyflow',
    paquete: '@xyflow/react',
    licencia: 'MIT',
    motivo:
      'Hoja base de React Flow (disposición del lienzo del organigrama en diagrama). La biblioteca en sí NO se ' +
      'copia: llega como dependencia de npm.',
    donde: ['`dist/karpo.css` (reglas `.react-flow__*`)'],
    cabeceraEn: ['karpo.css'],
    entra: '`src/tree/tree.css`, con `@import url("@xyflow/react/dist/base.css")`',
    generadoPor: ['src/tree/tree.css'],
  },
]);

const normalizeEol = (text) => text.replace(/\r\n?/g, '\n');

/** Colapsa los espacios (saltos incluidos) para comparar textos de licencia. */
export function normalizeSpaces(text) {
  return normalizeEol(text).replace(/\s+/g, ' ').trim();
}

/**
 * Lee el texto de la licencia de un paquete instalado (`LICENSE`, `LICENSE.txt`, `LICENCE.md`, `COPYING`…).
 * @param {string} root carpeta de `front/react`
 * @param {string} paquete
 * @returns {{ file: string, text: string, version: string }}
 */
export function readInstalledLicense(root, paquete) {
  const dir = path.join(root, 'node_modules', ...paquete.split('/'));
  if (!existsSync(dir)) throw new Error(`avisos: ${paquete} no está instalado (¿falta \`npm ci\`?)`);
  const candidates = readdirSync(dir)
    .filter((name) => /^(licen[cs]e|copying)(\.[a-z]+)?$/i.test(name))
    .sort((a, b) => a.localeCompare(b));
  const file = candidates[0];
  if (!file) throw new Error(`avisos: ${paquete} no trae ningún fichero LICENSE en node_modules`);
  const manifest = JSON.parse(readFileSync(path.join(dir, 'package.json'), 'utf8'));
  return {
    file: `node_modules/${paquete}/${file}`,
    text: normalizeEol(readFileSync(path.join(dir, file), 'utf8')).trimEnd(),
    version: String(manifest.version ?? '?'),
  };
}

/** Valla de código más larga que cualquier racha de acentos graves del texto. */
function fenceFor(text) {
  const runs = text.match(/`+/g) ?? [];
  return '`'.repeat(Math.max(3, ...runs.map((run) => run.length + 1)));
}

/**
 * Documento `THIRD-PARTY-NOTICES.md`.
 * @param {{ root: string, origenes?: readonly Origen[], pkg: { name: string, license?: string } }} input
 * @returns {string}
 */
export function renderNotices({ root, origenes = ORIGENES, pkg }) {
  const sorted = [...origenes].sort((a, b) => a.id.localeCompare(b.id));
  const lines = [
    `# Avisos de terceros de ${pkg.name}`,
    '',
    `Los ficheros de \`dist/\` de \`${pkg.name}\` (licencia propia \`${pkg.license ?? 'GPL-2.0-only'}\`, ver \`LICENSE\`) ` +
      'incluyen partes de los proyectos de terceros que se citan a continuación. Sus licencias exigen conservar ' +
      'el aviso de copyright y el texto de la licencia: se reproducen enteros más abajo.',
    '',
    'Las dependencias que NO se copian dentro de `dist/` (React, i18next, driver.js…) las instala npm con su propia ' +
      'licencia y no figuran aquí; su relación está en `docs/LICENCIAS.md` del repositorio.',
    '',
    'Este fichero lo genera `scripts/avisos.mjs` al construir la biblioteca (`npm run build:lib`) y se puede ' +
      'regenerar con `npm run licencias -- --avisos`. No se edita a mano.',
    '',
  ];
  sorted.forEach((origen, index) => {
    const license = readInstalledLicense(root, origen.paquete);
    const fence = fenceFor(license.text);
    lines.push(
      `## ${index + 1}. ${origen.paquete} ${license.version}`,
      '',
      `- Licencia: ${origen.licencia}`,
      `- Qué se incluye: ${origen.motivo}`,
      `- Dónde queda: ${origen.donde.join('; ')}`,
      `- Cómo entra: ${origen.entra}`,
      `- Texto de la licencia (\`${license.file}\`):`,
      '',
      `${fence}text`,
      license.text,
      fence,
      '',
    );
  });
  return `${lines.join('\n').trimEnd()}\n`;
}

/**
 * Cabecera `/*! … *\/` de una hoja de estilos de `dist/`, o `''` si ningún origen la lleva.
 * Los comentarios `/*!` son los que los minificadores conservan por convención.
 *
 * @param {string} cssFile nombre de la hoja dentro de `dist/` (`karpo.css`)
 * @param {{ root: string, origenes?: readonly Origen[] }} input
 */
export function cssBanner(cssFile, { root, origenes = ORIGENES }) {
  const own = origenes.filter((origen) => origen.cabeceraEn.includes(cssFile));
  if (own.length === 0) return '';
  const parts = own.map((origen) => `${origen.paquete} ${readInstalledLicense(root, origen.paquete).version} (${origen.licencia})`);
  return `/*! Incluye partes de ${parts.join(' y ')}; ver ${NOTICES_FILE} */\n`;
}

/** ¿Ya empieza por la cabecera de avisos? (para anteponerla una sola vez) */
export function hasBanner(css) {
  return css.trimStart().startsWith('/*!') && css.slice(0, 400).includes(NOTICES_FILE);
}

/** Antepone la cabecera si hace falta y si la hoja tiene alguna. */
export function withBanner(css, cssFile, input) {
  const banner = cssBanner(cssFile, input);
  if (banner === '' || hasBanner(css)) return css;
  return banner + css;
}

// ---------------------------------------------------------------------------------------------
// Detección de lo incrustado que no consta en el registro

/** Nombre de paquete de un especificador no relativo (`@xyflow/react/dist/base.css` → `@xyflow/react`). */
export function packageOfSpecifier(specifier) {
  const parts = specifier.split('/');
  return specifier.startsWith('@') ? parts.slice(0, 2).join('/') : (parts[0] ?? '');
}

/**
 * Paquetes importados con `@import` desde un CSS. Se ignoran las rutas relativas, absolutas y las URL.
 * @param {string} css
 * @returns {string[]}
 */
export function cssPackageImports(css) {
  const withoutComments = css.replace(/\/\*[\s\S]*?\*\//g, '');
  const found = new Set();
  for (const match of withoutComments.matchAll(/@import\s+(?:url\(\s*)?(["'])([^"']+)\1/gi)) {
    const specifier = match[2] ?? '';
    if (/^(\.{1,2}\/|\/|[a-z][a-z0-9+.-]*:)/i.test(specifier)) continue;
    found.add(packageOfSpecifier(specifier));
  }
  return [...found];
}

/**
 * Paquetes de `node_modules/` cuyas fuentes salen en un mapa de fuentes (`dist/*.map`).
 * @param {{ sources?: string[] }} map
 * @returns {string[]}
 */
export function mapNodeModulesPackages(map) {
  const found = new Set();
  for (const source of map.sources ?? []) {
    const normalized = String(source).replaceAll('\\', '/');
    const at = normalized.lastIndexOf('node_modules/');
    if (at === -1) continue;
    const rest = normalized.slice(at + 'node_modules/'.length);
    const name = packageOfSpecifier(rest);
    if (name) found.add(name);
  }
  return [...found];
}

function listFiles(dir, accepts) {
  if (!existsSync(dir)) return [];
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) return entry.name === 'node_modules' ? [] : listFiles(full, accepts);
    return accepts(entry.name) ? [full] : [];
  });
}

/**
 * Incrustados que no están en el registro, mirando `src/**\/*.css` y `dist/*.map`.
 * @param {string} root
 * @param {readonly Origen[]} [origenes]
 * @returns {string[]} frases para el mensaje de error (vacío si todo consta)
 */
export function unregisteredEmbeds(root, origenes = ORIGENES) {
  const known = new Set(origenes.map((origen) => origen.paquete));
  const problems = [];
  for (const file of listFiles(path.join(root, 'src'), (name) => name.endsWith('.css')).sort()) {
    for (const paquete of cssPackageImports(readFileSync(file, 'utf8'))) {
      if (!known.has(paquete)) {
        const rel = path.relative(root, file).replaceAll('\\', '/');
        problems.push(`${rel}: \`@import\` del paquete ${paquete}, que no está en el registro de scripts/avisos.mjs`);
      }
    }
  }
  const dist = path.join(root, 'dist');
  const maps = existsSync(dist)
    ? readdirSync(dist)
        .filter((name) => name.endsWith('.map'))
        .sort()
    : [];
  for (const name of maps) {
    let map;
    try {
      map = JSON.parse(readFileSync(path.join(dist, name), 'utf8'));
    } catch {
      problems.push(`dist/${name}: mapa de fuentes ilegible`);
      continue;
    }
    for (const paquete of mapNodeModulesPackages(map)) {
      if (!known.has(paquete)) {
        problems.push(`dist/${name}: lleva fuentes de node_modules/${paquete}, que no está en el registro de scripts/avisos.mjs`);
      }
    }
  }
  return problems;
}

/**
 * Los ficheros que el registro declara (`generadoPor`) siguen existiendo: si alguien borra el generador,
 * el origen quedaría atribuido sin existir (o, peor, al revés).
 * @returns {string[]}
 */
export function missingGenerators(root, origenes = ORIGENES) {
  const problems = [];
  for (const origen of origenes) {
    for (const file of origen.generadoPor) {
      if (!existsSync(path.join(root, ...file.split('/')))) {
        problems.push(`${origen.paquete}: el registro cita ${file}, que no existe`);
      }
    }
  }
  return problems;
}

/**
 * Comprueba un `THIRD-PARTY-NOTICES.md` ya escrito: cada origen con su nombre y su licencia íntegra.
 * @param {string} notices contenido del fichero
 * @param {{ root: string, origenes?: readonly Origen[] }} input
 * @returns {string[]}
 */
export function noticesProblems(notices, { root, origenes = ORIGENES }) {
  const haystack = normalizeSpaces(notices);
  const problems = [];
  for (const origen of origenes) {
    if (!haystack.includes(origen.paquete)) {
      problems.push(`${NOTICES_FILE} no cita ${origen.paquete}`);
      continue;
    }
    const license = readInstalledLicense(root, origen.paquete);
    if (!haystack.includes(normalizeSpaces(license.text))) {
      problems.push(`${NOTICES_FILE} no trae el texto íntegro de la licencia de ${origen.paquete} (${license.file})`);
    }
  }
  return problems;
}

/**
 * Las hojas de estilos publicadas llevan su cabecera de avisos.
 * @param {string} cssFile `karpo.css`
 * @param {string} css contenido
 * @param {{ root: string, origenes?: readonly Origen[] }} input
 * @returns {string[]}
 */
export function bannerProblems(cssFile, css, input) {
  if (cssBanner(cssFile, input) === '') return [];
  return hasBanner(css) ? [] : [`dist/${cssFile} debería empezar por un comentario /*! que cite ${NOTICES_FILE}`];
}
