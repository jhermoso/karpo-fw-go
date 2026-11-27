// `:dir()` frente a lightningcss (D-R22, hallazgo de la ronda 1).
//
// Vite minifica el CSS con lightningcss y, con sus `targets` por omisión, compila `:dir(rtl)` a
// `:lang(ar,he,fa,…)`: la dirección pasa a depender del idioma de la página y deja de seguir a
// `?dir=rtl` o a `<html dir>`. Esta prueba compila todo `src/**/*.css` con la misma transformación de CSS
// y los mismos `targets` que las dos configuraciones de Vite del proyecto (biblioteca y demo) y falla si
// la salida contiene `:lang(ar`. Además comprueba que la canalización detecta el problema de verdad
// (un CSS con `:dir()` sí sale como `:lang(ar`) y que las configuraciones no cambian los `targets`
// sin que esta prueba se entere.
import { mkdtempSync, readdirSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { build, loadConfigFromFile, type Plugin } from 'vite';
import { afterAll, describe, expect, it } from 'vitest';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const VIRTUAL = '\0karpo-css-dir-entry';

/** Todos los `.css` de una carpeta, recursivamente y en orden estable. */
function cssFiles(dir: string): string[] {
  const found: string[] = [];
  for (const entry of readdirSync(dir).sort()) {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) found.push(...cssFiles(full));
    else if (entry.endsWith('.css')) found.push(full);
  }
  return found;
}

interface Targets {
  target?: unknown;
  cssTarget?: unknown;
}

/** Los `targets` de una configuración de Vite del proyecto (`undefined` = los de Vite por omisión). */
async function targetsOf(file: string, mode: string): Promise<Targets> {
  const loaded = await loadConfigFromFile({ command: 'build', mode }, path.join(root, file), root);
  const config = loaded?.config as { build?: Targets } | undefined;
  return { target: config?.build?.target, cssTarget: config?.build?.cssTarget };
}

/** Compila los ficheros CSS con la transformación de Vite y devuelve el CSS de salida. */
async function compile(files: string[], targets: Targets): Promise<string> {
  const entry = files.map((file) => `import ${JSON.stringify(file.replaceAll('\\', '/'))};`).join('\n');
  const virtual: Plugin = {
    name: 'karpo-css-dir-entry',
    resolveId: (id) => (id === VIRTUAL ? VIRTUAL : null),
    load: (id) => (id === VIRTUAL ? entry : null),
  };
  const result = await build({
    configFile: false,
    root,
    logLevel: 'silent',
    plugins: [virtual],
    build: {
      write: false,
      minify: true,
      cssMinify: 'lightningcss',
      cssCodeSplit: false,
      reportCompressedSize: false,
      ...(targets.target === undefined ? {} : { target: targets.target as string }),
      ...(targets.cssTarget === undefined ? {} : { cssTarget: targets.cssTarget as string }),
      rollupOptions: { input: VIRTUAL },
    },
  });
  const outputs = (Array.isArray(result) ? result : [result]).flatMap((r) => ('output' in r ? r.output : []));
  const css = outputs.filter((o) => o.type === 'asset' && o.fileName.endsWith('.css')).map((o) => String((o as { source: unknown }).source));
  return css.join('\n');
}

const scratch: string[] = [];
afterAll(() => {
  for (const dir of scratch) rmSync(dir, { recursive: true, force: true });
});

describe('el CSS compilado no contiene :lang(ar (D-R22)', () => {
  const sources = cssFiles(path.join(root, 'src'));

  it('hay hojas que comprobar', () => {
    expect(sources.length).toBeGreaterThanOrEqual(10);
    expect(sources.map((file) => path.basename(file))).toEqual(expect.arrayContaining(['ui.css', 'grid.css', 'shell.css', 'map.css']));
  });

  it.each([
    ['la biblioteca', 'vite.lib.config.ts', 'production'],
    ['la demo', 'vite.config.ts', 'demo'],
  ])('src/**/*.css compilado con los targets de %s', async (_name, file, mode) => {
    const targets = await targetsOf(file, mode);
    const output = await compile(sources, targets);
    expect(output.length).toBeGreaterThan(10_000);
    // Se ve lo que se espera de un CSS compilado de verdad: nuestras reglas con `[dir="rtl"]` siguen ahí.
    expect(output).toContain('[dir=rtl]');
    expect(output, 'lightningcss ha compilado un `:dir()` a `:lang(…)`: usa `[dir="rtl"]` (D-R22)').not.toContain(':lang(ar');
    expect(output).not.toMatch(/:lang\([^)]*\bhe\b/);
  }, 120_000);

  it('las configuraciones de Vite no fijan `build.target` ni `build.cssTarget` (la prueba usa los de Vite)', async () => {
    // Si alguien los fija, `compile` ya los usa; este aviso sólo recuerda que cambia el CSS publicado (LICENCIAS, Q-6).
    expect(await targetsOf('vite.lib.config.ts', 'production')).toEqual({ target: undefined, cssTarget: undefined });
    expect(await targetsOf('vite.config.ts', 'demo')).toEqual({ target: undefined, cssTarget: undefined });
  });
});

describe('control: la prueba detecta un :dir() que se cuela', () => {
  it('un CSS con `:dir(rtl)` sale de la compilación como `:lang(ar`', async () => {
    const dir = mkdtempSync(path.join(tmpdir(), 'karpo-css-dir-'));
    scratch.push(dir);
    const file = path.join(dir, 'dir.css');
    writeFileSync(file, '.kx-flecha:dir(rtl) {\n  transform: scaleX(-1);\n}\n');
    const output = await compile([file], await targetsOf('vite.config.ts', 'demo'));
    expect(output).toContain(':lang(ar');
  }, 120_000);

  it('con `[dir="rtl"]` el selector llega intacto', async () => {
    const dir = mkdtempSync(path.join(tmpdir(), 'karpo-css-dir-'));
    scratch.push(dir);
    const file = path.join(dir, 'dir.css');
    writeFileSync(file, '[dir="rtl"] .kx-flecha {\n  transform: scaleX(-1);\n}\n');
    const output = await compile([file], await targetsOf('vite.config.ts', 'demo'));
    expect(output).toContain('[dir=rtl]');
    expect(output).not.toContain(':lang(');
  }, 120_000);
});
