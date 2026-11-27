// Sincroniza los temas de la demo con Theros Front (PLAN §4.5).
//
//   npm run themes:sync
//
// Ejecuta `go run ./cmd/theros-front build <tema> -out <tmp>` con cwd en Theros.Front (SOLO LECTURA:
// el generador escribe en una carpeta temporal nuestra), copia los seis CSS y `manifest.json` a
// `demo/public/themes/<tema>/` y escribe `demo/public/themes/THEMES.md` con el commit de Theros Front,
// el hash del diccionario y la fecha. Los ficheros generados se versionan (la demo no depende de
// tener Theros Front instalado).
//
// Variables de entorno:
//   THEROS_FRONT_DIR   carpeta de Theros Front (por defecto C:\Git\Paranoia\Theros.Front)
//   THEROS_THEMES      lista de temas separados por comas (por defecto los cuatro de la demo)
import { spawnSync } from 'node:child_process';
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

export const DEFAULT_THEROS_FRONT_DIR = 'C:\\Git\\Paranoia\\Theros.Front';
export const DEFAULT_THEMES = ['modern-flat', 'classic-1995', 'glossy-2003', 'souq'];
/**
 * Referencia que entiende `theros-front build` (relativa a su carpeta): los estilos de serie se llaman por
 * nombre; `souq` (árabe, AAA) es un tema de prueba de Theros Front y se llama por su ruta.
 */
export const THEME_REFS = { souq: 'testdata/themes/souq.theme.yml' };
/** Lo único que el front necesita de cada tema generado (A3 §3). */
export const THEME_FILES = [
  'index.css',
  'tokens.css',
  'modes.css',
  'base.css',
  'domain.css',
  'a11y.css',
  'manifest.json',
];

const here = path.dirname(fileURLToPath(import.meta.url));
const projectRoot = path.resolve(here, '..');

/**
 * @param {string} command
 * @param {string[]} args
 * @param {string} cwd
 * @returns {string}
 */
function run(command, args, cwd) {
  const result = spawnSync(command, args, { cwd, encoding: 'utf8', windowsHide: true });
  if (result.error) throw new Error(`No se pudo ejecutar «${command} ${args.join(' ')}»: ${result.error.message}`);
  if (result.status !== 0) {
    throw new Error(
      `«${command} ${args.join(' ')}» terminó con código ${result.status}\n${result.stderr || result.stdout}`,
    );
  }
  return result.stdout.trim();
}

/**
 * Renderiza el THEMES.md de la salida.
 * @param {{ commit: string, branch: string, hash: string, date: string, themes: { name: string, hash: string, modes: string, direction: string }[] }} info
 */
export function renderThemesMd(info) {
  const rows = info.themes
    .map((t) => `| \`${t.name}\` | ${t.modes} | ${t.direction} | \`${t.hash}\` |`)
    .join('\n');
  return [
    '# Temas de la demo (generados por Theros Front)',
    '',
    'Fichero generado por `npm run themes:sync` (`scripts/sync-themes.mjs`). No se edita a mano.',
    '',
    `- Commit de Theros Front: \`${info.commit}\` (rama \`${info.branch}\`)`,
    `- Hash del diccionario: \`${info.hash}\``,
    `- Fecha de generación: ${info.date}`,
    '',
    'Cada carpeta contiene los seis CSS del tema (`index.css`, `tokens.css`, `modes.css`, `base.css`,',
    '`domain.css`, `a11y.css`) y `manifest.json`. El resto de la salida del generador (galería, YAML',
    'resuelto, tokens DTCG) no se copia: el front no lo necesita.',
    '',
    '| Tema | Modos | Dirección | Hash de lo resuelto |',
    '|---|---|---|---|',
    rows,
    '',
    'Regenerar: `npm run themes:sync` (con `THEROS_FRONT_DIR` si Theros Front no está en la ruta por defecto).',
    '',
  ].join('\n');
}

/** @param {unknown} manifest */
function describeManifest(manifest) {
  const m = /** @type {Record<string, any>} */ (manifest);
  const modeKeys = Object.keys(m.modes ?? {});
  const modes = modeKeys.includes('dark') ? ['claro', 'oscuro', 'sistema'] : ['claro'];
  if (modeKeys.includes('high-contrast')) modes.push('contraste alto');
  return {
    hash: String(typeof m.resolved === 'string' ? m.resolved : (m.resolved?.hash ?? '')),
    modes: modes.join(', '),
    direction: String(m.culture?.direction ?? 'ltr'),
  };
}

export function syncThemes() {
  const therosDir = process.env.THEROS_FRONT_DIR || DEFAULT_THEROS_FRONT_DIR;
  const themes = (process.env.THEROS_THEMES ? process.env.THEROS_THEMES.split(',') : DEFAULT_THEMES)
    .map((t) => t.trim())
    .filter(Boolean);
  if (!existsSync(path.join(therosDir, 'cmd', 'theros-front'))) {
    throw new Error(`No encuentro Theros Front en «${therosDir}». Define THEROS_FRONT_DIR.`);
  }

  const outRoot = path.join(projectRoot, 'demo', 'public', 'themes');
  const tmp = mkdtempSync(path.join(os.tmpdir(), 'karpo-themes-'));
  try {
    const commit = run('git', ['rev-parse', 'HEAD'], therosDir);
    const branch = run('git', ['branch', '--show-current'], therosDir);
    const dictionaryHash = run('go', ['run', './cmd/theros-front', 'hash'], therosDir);

    const described = [];
    for (const theme of themes) {
      const out = path.join(tmp, theme);
      mkdirSync(out, { recursive: true });
      run('go', ['run', './cmd/theros-front', 'build', THEME_REFS[theme] ?? theme, '-out', out], therosDir);
      const target = path.join(outRoot, theme);
      mkdirSync(target, { recursive: true });
      for (const file of THEME_FILES) {
        const source = path.join(out, file);
        if (!existsSync(source)) throw new Error(`El tema «${theme}» no generó ${file}`);
        copyFileSync(source, path.join(target, file));
      }
      const manifest = JSON.parse(readFileSync(path.join(target, 'manifest.json'), 'utf8'));
      described.push({ name: theme, ...describeManifest(manifest) });
      process.stdout.write(`  ${theme}: ${THEME_FILES.length} ficheros\n`);
    }

    // La fecha es la del día (sin hora) para que una regeneración sin cambios no ensucie el diff.
    const date = new Date().toISOString().slice(0, 10);
    writeFileSync(
      path.join(outRoot, 'THEMES.md'),
      renderThemesMd({ commit, branch, hash: dictionaryHash, date, themes: described }),
      'utf8',
    );
    process.stdout.write(`THEMES.md escrito (commit ${commit.slice(0, 8)}, ${dictionaryHash})\n`);
  } finally {
    rmSync(tmp, { recursive: true, force: true });
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    syncThemes();
  } catch (error) {
    process.stderr.write(`themes:sync falló: ${error instanceof Error ? error.message : String(error)}\n`);
    process.exit(1);
  }
}
