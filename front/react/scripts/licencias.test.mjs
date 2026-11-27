import { readFileSync, existsSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import {
  analyze,
  classifyTransitive,
  evaluateExpression,
  licenseIds,
  licenseText,
  nameFromLockPath,
  renderDocument,
  EXCEPTIONS,
  BEGIN,
  END,
} from './licencias.mjs';
import { ORIGENES } from './avisos.mjs';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

function lockOf(entries) {
  const packages = { '': { name: 'x' } };
  for (const [name, entry] of Object.entries(entries)) {
    packages[`node_modules/${name}`] = { version: '1.0.0', ...entry };
  }
  return { lockfileVersion: 3, packages };
}

function problemsOf(pkg, entries, installed) {
  return analyze({
    pkg,
    lock: lockOf(entries),
    readInstalled: (lockPath) => installed?.[nameFromLockPath(lockPath)],
  }).problems.map((p) => p.id);
}

describe('expresiones SPDX', () => {
  it('extrae ids', () => {
    expect(licenseIds('(MIT OR Apache-2.0)')).toEqual(['MIT', 'Apache-2.0']);
    expect(licenseIds('GPL-2.0-only WITH Classpath-exception-2.0')).toEqual([
      'GPL-2.0-only',
      'Classpath-exception-2.0',
    ]);
  });

  it('OR basta una alternativa; AND exige todas', () => {
    const only = (...ids) => (id) => ids.includes(id);
    expect(evaluateExpression('GPL-3.0-only OR MIT', only('MIT'))).toBe(true);
    expect(evaluateExpression('GPL-3.0-only AND MIT', only('MIT'))).toBe(false);
    expect(evaluateExpression('(MIT OR Apache-2.0) AND ISC', only('Apache-2.0', 'ISC'))).toBe(true);
  });

  it('normaliza los formatos antiguos de package.json', () => {
    expect(licenseText({ license: 'MIT' })).toBe('MIT');
    expect(licenseText({ license: { type: 'ISC' } })).toBe('ISC');
    expect(licenseText({ licenses: [{ type: 'MIT' }, { type: 'Apache-2.0' }] })).toBe('(MIT OR Apache-2.0)');
    expect(licenseText({})).toBe('');
  });
});

describe('política de licencias', () => {
  const pkg = { name: 'x', version: '1.0.0', dependencies: { directa: '1.0.0' } };

  it('admite directas MIT, Apache-2.0 y BSD', () => {
    for (const license of ['MIT', 'Apache-2.0', 'BSD-3-Clause', 'BSD-2-Clause', '(MIT OR Apache-2.0)']) {
      expect(problemsOf(pkg, { directa: { license } })).toEqual([]);
    }
  });

  it.each(['ISC', 'MPL-2.0', 'GPL-3.0-only', 'AGPL-3.0-or-later', 'UNLICENSED', 'BlueOak-1.0.0'])(
    'rechaza una directa %s',
    (license) => {
      expect(problemsOf(pkg, { directa: { license } })).toEqual(['directa']);
    },
  );

  it('rechaza una directa sin licencia', () => {
    expect(problemsOf(pkg, { directa: {} })).toEqual(['directa']);
  });

  it('admite transitivas permisivas (ISC, 0BSD, CC0, BlueOak, MIT-0, CC-BY-4.0)', () => {
    const entries = { directa: { license: 'MIT' } };
    for (const license of ['ISC', '0BSD', 'CC0-1.0', 'BlueOak-1.0.0', 'MIT-0', 'CC-BY-4.0', '(MIT OR CC0-1.0)']) {
      entries[`t-${license}`] = { license, dev: true };
    }
    expect(problemsOf(pkg, entries)).toEqual([]);
  });

  it.each([
    ['GPL-3.0-only', 'copyleft'],
    ['LGPL-2.1-only', 'copyleft'],
    ['AGPL-3.0-only', 'copyleft'],
    ['SSPL-1.0', 'copyleft'],
    ['EPL-2.0', 'copyleft'],
    ['SEE LICENSE IN LICENSE.md', 'comercial'],
    ['UNLICENSED', 'comercial'],
    ['Algo-Raro-1.0', 'desconocida'],
  ])('rechaza la transitiva %s (%s)', (license) => {
    const ids = problemsOf(pkg, { directa: { license: 'MIT' }, trans: { license } });
    expect(ids).toEqual(['trans@1.0.0']);
  });

  it('rechaza una transitiva sin licencia', () => {
    expect(problemsOf(pkg, { directa: { license: 'MIT' }, trans: {} })).toEqual(['trans@1.0.0']);
  });

  it('acepta MPL-2.0 sólo para lightningcss*', () => {
    const entries = {
      directa: { license: 'MIT' },
      lightningcss: { license: 'MPL-2.0', dev: true },
      'lightningcss-win32-x64-msvc': { license: 'MPL-2.0', dev: true, optional: true },
    };
    expect(problemsOf(pkg, entries)).toEqual([]);
    expect(problemsOf(pkg, { ...entries, 'otra-mpl': { license: 'MPL-2.0' } })).toEqual(['otra-mpl@1.0.0']);
    expect(classifyTransitive('lightningcss-linux-x64-gnu', 'MPL-2.0').status).toBe('exception');
    expect(classifyTransitive('lightningcss-extra', 'GPL-3.0-only').status).toBe('forbidden');
  });

  it('avisa si la licencia del lock y la instalada no coinciden', () => {
    const ids = problemsOf(
      pkg,
      { directa: { license: 'MIT' } },
      { directa: { version: '1.0.0', license: 'GPL-3.0-only' } },
    );
    expect(ids).toContain('directa@1.0.0');
  });
});

describe('origenes incrustados en dist/ (B-1)', () => {
  const pkg = { name: 'x', version: '1.0.0', license: 'GPL-2.0-only', dependencies: { directa: '1.0.0' } };
  const origen = {
    id: 'toy',
    paquete: 'toy-lib',
    licencia: 'MIT',
    motivo: 'Una hoja de estilos.',
    donde: ['`dist/karpo.css`'],
    cabeceraEn: [],
    entra: '`src/a.css`',
    generadoPor: [],
  };

  it('un origen del registro que no está en package-lock.json es una incidencia', () => {
    const { problems } = analyze({ pkg, lock: lockOf({ directa: { license: 'MIT' } }), origenes: [origen] });
    expect(problems).toEqual([
      { id: 'toy-lib', message: 'origen incrustado en dist/ (scripts/avisos.mjs) que no está en package-lock.json' },
    ]);
  });

  it('con el origen en el lock no hay incidencia y se anota su versión y su licencia', () => {
    const lock = lockOf({ directa: { license: 'MIT' }, 'toy-lib': { version: '2.3.4', license: 'MIT', dev: true } });
    const analysis = analyze({ pkg, lock, origenes: [origen] });
    expect(analysis.problems).toEqual([]);
    expect(analysis.embedded).toEqual([{ origen, version: '2.3.4', license: 'MIT' }]);
  });

  it('si el paquete declara otra licencia que el registro, es una incidencia', () => {
    const lock = lockOf({ directa: { license: 'MIT' }, 'toy-lib': { license: 'ISC', dev: true } });
    expect(analyze({ pkg, lock, origenes: [origen] }).problems.map((problem) => problem.id)).toEqual(['toy-lib']);
  });

  it('sin registro (por omisión) no se exige nada', () => {
    expect(analyze({ pkg, lock: lockOf({ directa: { license: 'MIT' } }) }).embedded).toEqual([]);
  });

  it('el informe lista lo incrustado, con su versión, y remite a dist/THIRD-PARTY-NOTICES.md', () => {
    const lock = lockOf({ directa: { license: 'MIT' }, 'toy-lib': { version: '2.3.4', license: 'MIT', dev: true } });
    const text = renderDocument(pkg, analyze({ pkg, lock, origenes: [origen] }), undefined);
    expect(text).toContain('## Incrustado en dist/ (avisos de terceros)');
    expect(text).toContain('| `toy-lib` | 2.3.4 | MIT | `dist/karpo.css` | `src/a.css` |');
    expect(text).toContain('dist/THIRD-PARTY-NOTICES.md');
    expect(renderDocument(pkg, analyze({ pkg, lock: lockOf({ directa: { license: 'MIT' } }) }), undefined)).not.toContain(
      'Incrustado en dist/',
    );
  });

  it('los tres orígenes reales están en el lock del repositorio', () => {
    const lockFile = path.join(root, 'package-lock.json');
    if (!existsSync(lockFile)) return;
    const lock = JSON.parse(readFileSync(lockFile, 'utf8'));
    const realPkg = JSON.parse(readFileSync(path.join(root, 'package.json'), 'utf8'));
    const analysis = analyze({ pkg: realPkg, lock, origenes: ORIGENES });
    expect(analysis.problems).toEqual([]);
    expect(analysis.embedded.map((entry) => entry.origen.paquete).sort()).toEqual([
      '@phosphor-icons/react',
      '@xyflow/react',
      'maplibre-gl',
    ]);
  });
});

describe('informe', () => {
  const pkg = { name: '@karpo/react', version: '0.1.0', license: 'GPL-2.0-only', dependencies: { a: '1.0.0' } };
  const lock = lockOf({ a: { license: 'MIT' }, lightningcss: { license: 'MPL-2.0', dev: true } });
  const analysis = analyze({ pkg, lock });

  it('lista directas, transitivas por licencia y excepciones con su motivo', () => {
    const text = renderDocument(pkg, analysis, undefined);
    expect(text).toContain('## Dependencias directas');
    expect(text).toContain('| `a` | 1.0.0 | MIT | ejecución |');
    expect(text).toContain('### MPL-2.0 (1)');
    expect(text).toContain('## Excepciones aceptadas');
    expect(text).toContain('Herramienta de compilación');
    expect(text).toContain(BEGIN);
    expect(text).toContain(END);
  });

  it('conserva lo escrito a mano fuera de los marcadores', () => {
    const first = renderDocument(pkg, analysis, undefined);
    const edited = first.replace('Ninguna por ahora.', 'Nota escrita a mano.');
    const next = renderDocument(pkg, analysis, edited);
    expect(next).toContain('Nota escrita a mano.');
  });

  it('la excepción de lightningcss dice que se usa por defecto mientras Javier no decida (Q-6)', () => {
    const [exception] = EXCEPTIONS;
    expect(exception.reason).toContain('Consulta Q-6: se usa por defecto mientras Javier no decida');
    expect(exception.reason).toContain('cssMinify: esbuild');
    expect(exception.reason).toContain('targets');
    expect(exception.reason).not.toContain('Aceptada en Q-6');
    const text = renderDocument(pkg, analysis, undefined);
    expect(text).toContain('se usa por defecto mientras Javier no decida');
    expect(text).not.toContain('Aceptada en Q-6');
  });

  it('es determinista', () => {
    expect(renderDocument(pkg, analysis, undefined)).toBe(renderDocument(pkg, analysis, undefined));
  });
});

describe('repositorio real', () => {
  it('package-lock.json y package.json cumplen la política y el informe está al día', () => {
    const lockFile = path.join(root, 'package-lock.json');
    if (!existsSync(lockFile)) return;
    const pkg = JSON.parse(readFileSync(path.join(root, 'package.json'), 'utf8'));
    const lock = JSON.parse(readFileSync(lockFile, 'utf8'));
    expect(lock.lockfileVersion).toBe(3);
    const analysis = analyze({ pkg, lock, origenes: ORIGENES });
    expect(analysis.problems).toEqual([]);
    // El paquete no declara engines (D-R25) y Phosphor es sólo de desarrollo (D-R19).
    expect(pkg.engines).toBeUndefined();
    expect(pkg.devEngines.runtime).toEqual({ name: 'node', version: '>=24.15', onFail: 'warn' });
    expect(pkg.dependencies['@phosphor-icons/react']).toBeUndefined();
    expect(pkg.devDependencies['@phosphor-icons/react']).toBeDefined();
    // Ninguna versión directa lleva ^ ni ~ y todas están en el lock tal cual.
    for (const [name, version] of Object.entries({ ...pkg.dependencies, ...pkg.devDependencies })) {
      expect(version, name).toMatch(/^\d+\.\d+\.\d+$/);
      expect(lock.packages[`node_modules/${name}`]?.version, name).toBe(version);
    }
    const doc = readFileSync(path.join(root, 'docs/LICENCIAS.md'), 'utf8').replaceAll('\r\n', '\n');
    expect(renderDocument(pkg, analysis, doc).replaceAll('\r\n', '\n')).toBe(doc);
  });
});
