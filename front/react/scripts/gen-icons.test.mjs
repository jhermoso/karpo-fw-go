// Pruebas del generador de iconos: los dos ficheros están al día, reparten ICON_NAMES entre núcleo y resto
// sin repetir dibujos, sólo llevan los pesos regular y fill, `--check` detecta un fichero desactualizado (o el
// antiguo icon-data.ts) y el marcado inesperado se rechaza.
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';
import { CORE_ICON_NAMES, ICON_NAMES, phosphorNameOf } from '../src/contracts/icons.ts';
import { LEGACY_OUTPUT, OUTPUTS, buildSources, pascalCase, pathsFromMarkup, run, splitNames, WEIGHTS } from './gen-icons.mjs';

const folders = [];
afterEach(() => {
  for (const folder of folders.splice(0)) rmSync(folder, { recursive: true, force: true });
});

function scratch() {
  const folder = mkdtempSync(path.join(tmpdir(), 'gen-icons-'));
  folders.push(folder);
  return folder;
}

function scratchOutputs() {
  const folder = scratch();
  return { core: path.join(folder, 'icon-data-core.ts'), extra: path.join(folder, 'icon-data-extra.ts') };
}

const quiet = () => undefined;
const keyOf = (name) => (/^[a-z][a-z0-9]*$/.test(name) ? name : `'${name}'`);

describe('pascalCase', () => {
  it('convierte el nombre kebab de Phosphor al de su módulo', () => {
    expect(pascalCase('arrow-u-up-left')).toBe('ArrowUUpLeft');
    expect(pascalCase('plus')).toBe('Plus');
    expect(pascalCase('globe-hemisphere-west')).toBe('GlobeHemisphereWest');
  });
});

describe('pathsFromMarkup', () => {
  it('lee los trazados de un peso (con o sin etiqueta de cierre)', () => {
    expect(pathsFromMarkup('<path d="M0,0L1,1Z"></path><path d="M2,2Z"/>', 'x')).toEqual(['M0,0L1,1Z', 'M2,2Z']);
  });

  it('rechaza otros elementos, atributos extra o un marcado vacío', () => {
    expect(() => pathsFromMarkup('<circle cx="1"></circle>', 'x')).toThrow(/no admitido/);
    expect(() => pathsFromMarkup('<path d="M0,0Z" opacity="0.2"></path>', 'x')).toThrow(/no admitido/);
    expect(() => pathsFromMarkup('', 'x')).toThrow(/no admitido/);
  });
});

describe('splitNames', () => {
  const phosphor = (name) => ({ borrar: 'papelera', papelera: 'papelera' })[name] ?? name;

  it('un alias del mismo dibujo que uno del núcleo viaja con él', () => {
    const split = splitNames({ names: ['papelera', 'borrar', 'luna', 'sol'], coreNames: ['borrar', 'sol'], phosphorNameOf: phosphor });
    expect(split.core).toEqual(['borrar', 'papelera', 'sol']);
    expect(split.extra).toEqual(['luna']);
  });

  it('con los datos reales: núcleo y resto suman ICON_NAMES, sin repetir y con todo CORE_ICON_NAMES dentro', () => {
    const split = splitNames({ names: ICON_NAMES, coreNames: CORE_ICON_NAMES, phosphorNameOf });
    expect(split.core.filter((name) => split.extra.includes(name))).toEqual([]);
    expect([...split.core, ...split.extra].sort()).toEqual([...ICON_NAMES].sort());
    for (const name of CORE_ICON_NAMES) expect(split.core, name).toContain(name);
    // Ningún dibujo se escribe en los dos módulos.
    const coreTargets = new Set(split.core.map((name) => phosphorNameOf(name)));
    expect(split.extra.filter((name) => coreTargets.has(phosphorNameOf(name)))).toEqual([]);
  });
});

describe('src/ui/icon-data-core.ts e icon-data-extra.ts', () => {
  it('están al día: npm run icons:gen -- --check pasa y el antiguo icon-data.ts ya no existe', async () => {
    expect(await run({ check: true, log: quiet })).toEqual({ ok: true, changed: false });
    expect(existsSync(LEGACY_OUTPUT)).toBe(false);
    expect(existsSync(OUTPUTS.core)).toBe(true);
    expect(existsSync(OUTPUTS.extra)).toBe(true);
  });

  it('el texto generado es estable, sin CR, y tiene una entrada por cada IconName en uno de los dos', async () => {
    const first = await buildSources();
    const second = await buildSources();
    expect(second).toEqual(first);
    expect(first.core).not.toContain('\r');
    expect(first.extra).not.toContain('\r');
    for (const name of ICON_NAMES) {
      const needle = `\n  ${keyOf(name)}: `;
      const inCore = first.core.includes(needle);
      const inExtra = first.extra.includes(needle);
      expect(inCore !== inExtra, `${name} debe estar en exactamente uno`).toBe(true);
    }
    for (const name of CORE_ICON_NAMES) expect(first.core, name).toContain(`\n  ${keyOf(name)}: `);
  });

  it('sólo llevan los dos pesos y ninguna dependencia de ejecución; el extra sólo importa tipos', async () => {
    const { core, extra } = await buildSources();
    expect(WEIGHTS).toEqual(['regular', 'fill']);
    for (const text of [core, extra]) {
      expect(text).not.toMatch(/\b(bold|thin|light|duotone)\b:/);
      expect(text).not.toMatch(/from '@phosphor-icons|from 'react/);
      expect(text).toContain('Readonly<Partial<Record<IconName, IconData>>>');
    }
    expect(core).toContain('export const ICON_DATA_CORE');
    expect(extra).toContain('export const ICON_DATA_EXTRA');
    expect(extra).toContain("import type { IconData } from './icon-data-core';");
    // Ningún import de valores en el módulo diferido: no arrastra nada al trozo inicial.
    expect(extra).not.toMatch(/^import (?!type)/m);
  });

  it('los ficheros escritos en disco coinciden con lo que se genera', async () => {
    const sources = await buildSources();
    expect(readFileSync(OUTPUTS.core, 'utf8').replace(/\r\n/g, '\n')).toBe(sources.core);
    expect(readFileSync(OUTPUTS.extra, 'utf8').replace(/\r\n/g, '\n')).toBe(sources.extra);
  });
});

describe('run', () => {
  it('escribe los ficheros la primera vez, no los toca la segunda y --check los da por buenos', async () => {
    const outputs = scratchOutputs();
    expect(await run({ outputs, log: quiet })).toEqual({ ok: true, changed: true });
    expect(await run({ outputs, log: quiet })).toEqual({ ok: true, changed: false });
    expect(await run({ outputs, check: true, log: quiet })).toEqual({ ok: true, changed: false });
    const sources = await buildSources();
    expect(readFileSync(outputs.core, 'utf8')).toBe(sources.core);
    expect(readFileSync(outputs.extra, 'utf8')).toBe(sources.extra);
  });

  it('--check falla (ok: false) si falta un fichero o está desactualizado, y no escribe nada', async () => {
    const outputs = scratchOutputs();
    expect(await run({ outputs, check: true, log: quiet })).toEqual({ ok: false, changed: false });
    expect(existsSync(outputs.core)).toBe(false);

    const sources = await buildSources();
    writeFileSync(outputs.core, sources.core, 'utf8');
    writeFileSync(outputs.extra, sources.extra.replace("regular: ['M", "regular: ['M0,0Z', 'M"), 'utf8');
    const before = readFileSync(outputs.extra, 'utf8');
    expect(await run({ outputs, check: true, log: quiet })).toEqual({ ok: false, changed: false });
    expect(readFileSync(outputs.extra, 'utf8')).toBe(before);
  });

  it('--check vigila cada módulo por separado', async () => {
    const outputs = scratchOutputs();
    const sources = await buildSources();
    writeFileSync(outputs.core, sources.core.replace("regular: ['M", "regular: ['M0,0Z', 'M"), 'utf8');
    writeFileSync(outputs.extra, sources.extra, 'utf8');
    const lines = [];
    expect(await run({ outputs, check: true, log: (line) => lines.push(line) })).toEqual({ ok: false, changed: false });
    expect(lines.some((line) => line.includes('icon-data-core.ts') && line.includes('no está al día'))).toBe(true);
    expect(lines.some((line) => line.includes('icon-data-extra.ts') && line.includes('está al día'))).toBe(true);
  });

  it('--check ignora la diferencia entre CRLF y LF (Git en Windows)', async () => {
    const outputs = scratchOutputs();
    const sources = await buildSources();
    writeFileSync(outputs.core, sources.core.replace(/\n/g, '\r\n'), 'utf8');
    writeFileSync(outputs.extra, sources.extra.replace(/\n/g, '\r\n'), 'utf8');
    expect(await run({ outputs, check: true, log: quiet })).toEqual({ ok: true, changed: false });
  });

  it('con el antiguo icon-data.ts presente, --check falla y escribir lo borra', async () => {
    const outputs = scratchOutputs();
    const legacy = path.join(scratch(), 'icon-data.ts');
    writeFileSync(legacy, '// antiguo\n', 'utf8');
    await run({ outputs, log: quiet, legacy: null });
    expect(await run({ outputs, check: true, log: quiet, legacy })).toEqual({ ok: false, changed: false });
    expect(await run({ outputs, log: quiet, legacy })).toEqual({ ok: true, changed: true });
    expect(existsSync(legacy)).toBe(false);
    expect(await run({ outputs, check: true, log: quiet, legacy })).toEqual({ ok: true, changed: false });
  });
});
