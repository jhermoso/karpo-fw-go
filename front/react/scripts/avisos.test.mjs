// Avisos de terceros (B-1, D-R31): registro de lo incrustado, documento determinista con el texto íntegro de
// cada licencia, cabecera /*! de las hojas de estilos y detección de lo que entra sin constar.
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterEach, describe, expect, it } from 'vitest';
import {
  NOTICES_FILE,
  ORIGENES,
  bannerProblems,
  cssBanner,
  cssPackageImports,
  hasBanner,
  mapNodeModulesPackages,
  missingGenerators,
  noticesProblems,
  normalizeSpaces,
  packageOfSpecifier,
  readInstalledLicense,
  renderNotices,
  unregisteredEmbeds,
  withBanner,
} from './avisos.mjs';

const realRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

const folders = [];
afterEach(() => {
  for (const folder of folders.splice(0)) rmSync(folder, { recursive: true, force: true });
});

/** Una carpeta de proyecto de juguete con paquetes instalados (`name: [licencia, texto, fichero]`). */
function project(packages, files = {}) {
  const root = mkdtempSync(path.join(tmpdir(), 'avisos-'));
  folders.push(root);
  for (const [name, [version, text, file = 'LICENSE']] of Object.entries(packages)) {
    const dir = path.join(root, 'node_modules', ...name.split('/'));
    mkdirSync(dir, { recursive: true });
    writeFileSync(path.join(dir, 'package.json'), JSON.stringify({ name, version }));
    if (text !== null) writeFileSync(path.join(dir, file), text);
  }
  for (const [rel, content] of Object.entries(files)) {
    const target = path.join(root, ...rel.split('/'));
    mkdirSync(path.dirname(target), { recursive: true });
    writeFileSync(target, content);
  }
  return root;
}

const toy = (overrides = {}) => ({
  id: 'toy',
  paquete: 'toy-lib',
  licencia: 'MIT',
  motivo: 'Una hoja de estilos.',
  donde: ['`dist/karpo.css`'],
  cabeceraEn: ['karpo.css'],
  entra: '`src/a.css`',
  generadoPor: [],
  ...overrides,
});

const PKG = { name: '@karpo/react', license: 'GPL-2.0-only' };

describe('registro ORIGENES', () => {
  it('trae los tres orígenes incrustados de hoy: Phosphor, MapLibre GL JS y xyflow', () => {
    expect(ORIGENES.map((origen) => origen.paquete).sort()).toEqual(['@phosphor-icons/react', '@xyflow/react', 'maplibre-gl']);
    expect(new Set(ORIGENES.map((origen) => origen.id)).size).toBe(ORIGENES.length);
    for (const origen of ORIGENES) {
      expect(origen.licencia, origen.id).toMatch(/^(MIT|BSD-3-Clause)$/);
      expect(origen.motivo.length, origen.id).toBeGreaterThan(20);
      expect(origen.donde.length, origen.id).toBeGreaterThan(0);
    }
  });

  it('cada origen está instalado, con su LICENSE, y declara la licencia que dice el registro', () => {
    for (const origen of ORIGENES) {
      const license = readInstalledLicense(realRoot, origen.paquete);
      expect(license.text.length, origen.id).toBeGreaterThan(200);
      expect(license.version, origen.id).toMatch(/^\d/);
    }
  });

  it('los ficheros que lo generan existen', () => {
    expect(missingGenerators(realRoot)).toEqual([]);
  });

  it('la hoja de MapLibre y la de xyflow son las dos que llevan cabecera', () => {
    expect(ORIGENES.flatMap((origen) => origen.cabeceraEn).sort()).toEqual(['karpo-map.css', 'karpo.css']);
  });
});

describe('readInstalledLicense', () => {
  it('lee LICENSE, LICENSE.txt o LICENCE.md y normaliza los saltos de línea', () => {
    const root = project({ a: ['1.0.0', 'MIT\r\n\r\nCopyright A\r\n', 'LICENSE.txt'], b: ['2.1.0', 'ISC b', 'LICENCE.md'] });
    expect(readInstalledLicense(root, 'a')).toEqual({ file: 'node_modules/a/LICENSE.txt', text: 'MIT\n\nCopyright A', version: '1.0.0' });
    expect(readInstalledLicense(root, 'b').file).toBe('node_modules/b/LICENCE.md');
  });

  it('falla con un mensaje claro si el paquete no está instalado o no trae licencia', () => {
    const root = project({ sinlicencia: ['1.0.0', null] });
    expect(() => readInstalledLicense(root, 'no-esta')).toThrow(/no está instalado/);
    expect(() => readInstalledLicense(root, 'sinlicencia')).toThrow(/ningún fichero LICENSE/);
  });

  it('un paquete con ámbito (@scope/nombre) se busca en su carpeta', () => {
    const root = project({ '@s/n': ['3.0.0', 'MIT Copyright S'] });
    expect(readInstalledLicense(root, '@s/n').version).toBe('3.0.0');
  });
});

describe('renderNotices', () => {
  const root = () => project({ 'toy-lib': ['1.2.3', 'MIT License\n\nCopyright (c) 2026 Toy\n\nPermission is hereby granted…'] });

  it('lleva nombre, versión, licencia, dónde está y el texto íntegro de cada origen', () => {
    const text = renderNotices({ root: root(), origenes: [toy()], pkg: PKG });
    expect(text).toContain('# Avisos de terceros de @karpo/react');
    expect(text).toContain('## 1. toy-lib 1.2.3');
    expect(text).toContain('- Licencia: MIT');
    expect(text).toContain('- Dónde queda: `dist/karpo.css`');
    expect(text).toContain('- Cómo entra: `src/a.css`');
    expect(text).toContain('Copyright (c) 2026 Toy');
    expect(text).toContain('Permission is hereby granted…');
    expect(text.endsWith('\n')).toBe(true);
  });

  it('es determinista: sin fechas ni rutas de la máquina, y no depende del orden del registro', () => {
    const r = project({
      'a-lib': ['1.0.0', 'MIT A'],
      'b-lib': ['2.0.0', 'BSD B'],
    });
    const a = toy({ id: 'a', paquete: 'a-lib' });
    const b = toy({ id: 'b', paquete: 'b-lib', licencia: 'BSD-3-Clause' });
    const first = renderNotices({ root: r, origenes: [a, b], pkg: PKG });
    expect(renderNotices({ root: r, origenes: [b, a], pkg: PKG })).toBe(first);
    expect(first).not.toMatch(/\b20\d\d-\d\d-\d\d\b/);
    expect(first).not.toContain(r);
    expect(first.indexOf('a-lib')).toBeLessThan(first.indexOf('b-lib'));
  });

  it('usa una valla más larga si la licencia contiene acentos graves', () => {
    const r = project({ 'toy-lib': ['1.0.0', 'MIT ```código``` fin'] });
    const text = renderNotices({ root: r, origenes: [toy()], pkg: PKG });
    expect(text).toContain('````text');
  });

  it('con los orígenes reales, dos generaciones seguidas dan el mismo documento', () => {
    const first = renderNotices({ root: realRoot, pkg: PKG });
    expect(renderNotices({ root: realRoot, pkg: PKG })).toBe(first);
    for (const origen of ORIGENES) {
      expect(first, origen.paquete).toContain(`## `);
      expect(first, origen.paquete).toContain(origen.paquete);
    }
    for (const origen of ORIGENES) {
      expect(normalizeSpaces(first), origen.paquete).toContain(normalizeSpaces(readInstalledLicense(realRoot, origen.paquete).text));
    }
  });
});

describe('noticesProblems', () => {
  const r = () => project({ 'toy-lib': ['1.0.0', 'MIT\n\nCopyright (c) Toy\n\nTexto   de la licencia.'] });

  it('acepta el documento generado, aunque cambien los espacios', () => {
    const root = r();
    const text = renderNotices({ root, origenes: [toy()], pkg: PKG });
    expect(noticesProblems(text, { root, origenes: [toy()] })).toEqual([]);
    expect(noticesProblems(text.replace(/ /g, '  '), { root, origenes: [toy()] })).toEqual([]);
  });

  it('falla si falta un origen del registro', () => {
    const root = project({ 'toy-lib': ['1.0.0', 'MIT Toy'], 'otra-lib': ['1.0.0', 'MIT Otra'] });
    const text = renderNotices({ root, origenes: [toy()], pkg: PKG });
    const problems = noticesProblems(text, { root, origenes: [toy(), toy({ id: 'otra', paquete: 'otra-lib' })] });
    expect(problems).toEqual([`${NOTICES_FILE} no cita otra-lib`]);
  });

  it('falla si el texto de la licencia no está íntegro', () => {
    const root = r();
    const text = renderNotices({ root, origenes: [toy()], pkg: PKG }).replace('Copyright (c) Toy', 'Copyright (c) Otro');
    expect(noticesProblems(text, { root, origenes: [toy()] })).toEqual([
      expect.stringContaining('no trae el texto íntegro de la licencia de toy-lib'),
    ]);
  });
});

describe('cabecera /*! de las hojas de estilos', () => {
  const root = () => project({ 'toy-lib': ['1.2.3', 'MIT Toy'] });

  it('cita el paquete, su versión y su licencia, y nombra THIRD-PARTY-NOTICES.md', () => {
    const banner = cssBanner('karpo.css', { root: root(), origenes: [toy()] });
    expect(banner).toBe('/*! Incluye partes de toy-lib 1.2.3 (MIT); ver THIRD-PARTY-NOTICES.md */\n');
  });

  it('una hoja sin orígenes no lleva cabecera', () => {
    expect(cssBanner('otra.css', { root: root(), origenes: [toy()] })).toBe('');
    expect(withBanner('a{}', 'otra.css', { root: root(), origenes: [toy()] })).toBe('a{}');
  });

  it('se antepone una sola vez, también si el minificador ya la conservó', () => {
    const input = { root: root(), origenes: [toy()] };
    const once = withBanner('.a{color:red}', 'karpo.css', input);
    expect(once.startsWith('/*!')).toBe(true);
    expect(hasBanner(once)).toBe(true);
    expect(withBanner(once, 'karpo.css', input)).toBe(once);
  });

  it('bannerProblems avisa si falta, y con las hojas reales pide la cabecera', () => {
    const input = { root: root(), origenes: [toy()] };
    expect(bannerProblems('karpo.css', '.a{}', input)).toEqual([expect.stringContaining('dist/karpo.css debería empezar por un comentario /*!')]);
    expect(bannerProblems('karpo.css', withBanner('.a{}', 'karpo.css', input), input)).toEqual([]);
    expect(bannerProblems('karpo-map.css', '.a{}', { root: realRoot })).toHaveLength(1);
    expect(bannerProblems('otra.css', '.a{}', input)).toEqual([]);
  });

  it('con los orígenes reales cada hoja cita su paquete', () => {
    expect(cssBanner('karpo.css', { root: realRoot })).toMatch(/^\/\*! .*@xyflow\/react \d.*MIT.*THIRD-PARTY-NOTICES\.md \*\/\n$/);
    expect(cssBanner('karpo-map.css', { root: realRoot })).toMatch(/^\/\*! .*maplibre-gl \d.*BSD-3-Clause.*THIRD-PARTY-NOTICES\.md \*\/\n$/);
  });
});

describe('packageOfSpecifier y cssPackageImports', () => {
  it('saca el paquete de un especificador', () => {
    expect(packageOfSpecifier('maplibre-gl/dist/maplibre-gl.css')).toBe('maplibre-gl');
    expect(packageOfSpecifier('@xyflow/react/dist/base.css')).toBe('@xyflow/react');
    expect(packageOfSpecifier('solo')).toBe('solo');
  });

  it('encuentra los @import de paquetes y se salta relativos, absolutos, URL y comentarios', () => {
    const css = `
      /* @import url("comentado/estilos.css"); */
      @import url("../theme/layers.css");
      @import "./local.css";
      @import url('/publico/x.css');
      @import url("https://fuentes.example/css");
      @import url("maplibre-gl/dist/maplibre-gl.css") layer(karpo.components.maplibre);
      @import "@scope/pkg/x.css";
      @import url(otro-paquete/y.css);
    `;
    expect(cssPackageImports(css)).toEqual(['maplibre-gl', '@scope/pkg']);
  });
});

describe('mapNodeModulesPackages', () => {
  it('saca los paquetes de node_modules de las fuentes de un mapa, con ámbito y en rutas de Windows', () => {
    expect(
      mapNodeModulesPackages({
        sources: [
          '../src/a.ts',
          '../node_modules/zod/index.js',
          '..\\node_modules\\@xyflow\\react\\dist\\x.js',
          '../node_modules/a/node_modules/b/c.js',
        ],
      }),
    ).toEqual(['zod', '@xyflow/react', 'b']);
  });

  it('un mapa sin fuentes no da nada', () => {
    expect(mapNodeModulesPackages({})).toEqual([]);
  });
});

describe('unregisteredEmbeds', () => {
  const files = (extra = {}) => ({
    'src/a/a.css': '@import url("../theme/layers.css");\n.a{}',
    'src/b/b.css': '@import url("toy-lib/dist/x.css") layer(karpo.x);',
    'dist/karpo-react.js.map': JSON.stringify({ sources: ['../src/index.ts', '../node_modules/toy-lib/x.js'] }),
    ...extra,
  });

  it('no dice nada si todo lo incrustado consta en el registro', () => {
    const root = project({ 'toy-lib': ['1.0.0', 'MIT'] }, files());
    expect(unregisteredEmbeds(root, [toy()])).toEqual([]);
  });

  it('señala un @import de un paquete que no consta, con su fichero', () => {
    const root = project({ 'toy-lib': ['1.0.0', 'MIT'] }, files({ 'src/c/c.css': '@import url("intruso/dist/i.css");' }));
    expect(unregisteredEmbeds(root, [toy()])).toEqual([expect.stringMatching(/^src\/c\/c\.css: .*intruso/)]);
  });

  it('señala una fuente de node_modules en un mapa de dist/ que no consta', () => {
    const root = project(
      { 'toy-lib': ['1.0.0', 'MIT'] },
      files({ 'dist/otro.js.map': JSON.stringify({ sources: ['../node_modules/colado/index.js'] }) }),
    );
    expect(unregisteredEmbeds(root, [toy()])).toEqual([expect.stringMatching(/^dist\/otro\.js\.map: .*node_modules\/colado/)]);
  });

  it('un mapa ilegible se avisa; sin dist/ (todavía sin construir) sólo se miran los CSS', () => {
    const broken = project({ 'toy-lib': ['1.0.0', 'MIT'] }, files({ 'dist/roto.js.map': '{no es json' }));
    expect(unregisteredEmbeds(broken, [toy()])).toEqual(['dist/roto.js.map: mapa de fuentes ilegible']);
    const noDist = project({ 'toy-lib': ['1.0.0', 'MIT'] }, { 'src/b/b.css': '@import url("toy-lib/x.css");' });
    expect(unregisteredEmbeds(noDist, [toy()])).toEqual([]);
  });

  it('en el repositorio real no entra nada sin constar (src/**/*.css y dist/*.map)', () => {
    expect(unregisteredEmbeds(realRoot)).toEqual([]);
  });
});

describe('missingGenerators', () => {
  it('avisa si un fichero del registro ya no existe', () => {
    const root = project({}, { 'src/existe.ts': '' });
    expect(missingGenerators(root, [toy({ generadoPor: ['src/existe.ts', 'scripts/no-esta.mjs'] })])).toEqual([
      'toy-lib: el registro cita scripts/no-esta.mjs, que no existe',
    ]);
  });
});
