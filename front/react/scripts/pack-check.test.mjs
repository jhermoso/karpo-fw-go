import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterEach, describe, expect, it } from 'vitest';
import { ORIGENES, renderNotices, withBanner } from './avisos.mjs';
import {
  checkPackFiles,
  hasMapLibreRules,
  missingMapStylesheetParts,
  missingStylesheetMarkers,
  thirdPartyProblems,
} from './pack-check.mjs';

const GOOD = [
  'package.json',
  'LICENSE',
  'README.md',
  'dist/karpo-react.js',
  'dist/karpo.css',
  'dist/karpo-map.css',
  'dist/THIRD-PARTY-NOTICES.md',
  'dist/types/index.d.ts',
];

describe('checkPackFiles', () => {
  it('acepta dist/**, package.json, README.md y LICENSE', () => {
    expect(checkPackFiles(GOOD)).toEqual({ unexpected: [], missing: [] });
  });

  it('no exige README.md todavía, pero sí las entradas públicas', () => {
    const files = GOOD.filter((file) => file !== 'README.md' && file !== 'dist/karpo.css');
    expect(checkPackFiles(files).missing).toEqual(['dist/karpo.css']);
  });

  it('exige el fichero de avisos de terceros (dist/THIRD-PARTY-NOTICES.md, B-1)', () => {
    const files = GOOD.filter((file) => file !== 'dist/THIRD-PARTY-NOTICES.md');
    expect(checkPackFiles(files)).toEqual({ unexpected: [], missing: ['dist/THIRD-PARTY-NOTICES.md'] });
  });

  it('exige la hoja de MapLibre aparte (dist/karpo-map.css)', () => {
    const files = GOOD.filter((file) => file !== 'dist/karpo-map.css');
    expect(checkPackFiles(files).missing).toEqual(['dist/karpo-map.css']);
  });

  it('rechaza todo lo que esté fuera de la lista', () => {
    const result = checkPackFiles([...GOOD, 'src/index.ts', 'demo/index.html', '.npmrc', 'docs/PLAN.md']);
    expect(result.unexpected).toEqual(['src/index.ts', 'demo/index.html', '.npmrc', 'docs/PLAN.md']);
  });

  it('rechaza pruebas dentro de dist y normaliza separadores de Windows', () => {
    const result = checkPackFiles([...GOOD, 'dist\\types\\grid\\store.test.d.ts', 'dist/types/test/setup.d.ts']);
    expect(result.unexpected).toEqual(['dist/types/grid/store.test.d.ts', 'dist/types/test/setup.d.ts']);
  });
});

describe('missingStylesheetMarkers', () => {
  it('detecta las hojas que el empaquetador descartó', () => {
    const complete = '[data-kx-ui=skeleton]{} [data-kx-part=sr-only]{} [data-kx-shell]{} [data-kx-view=tree]{} [data-kx-view=map]{}';
    expect(missingStylesheetMarkers(complete)).toEqual([]);
    expect(missingStylesheetMarkers('[data-kx-shell]{}')).toContain('[data-kx-ui=skeleton]');
  });
});

describe('missingMapStylesheetParts', () => {
  const MAPLIBRE = '.maplibregl-map{-webkit-tap-highlight-color:#0000;font:12px/20px Helvetica Neue,Arial,sans-serif;position:relative}';

  it('acepta la hoja de MapLibre con font: inherit, minificada o no', () => {
    expect(missingMapStylesheetParts(`${MAPLIBRE}@layer karpo.components{.maplibregl-map{font:inherit}}`)).toEqual([]);
    expect(missingMapStylesheetParts(`${MAPLIBRE}
.maplibregl-map {
  font: inherit;
}`)).toEqual([]);
  });

  it('no se deja engañar por la fuente propia de MapLibre', () => {
    expect(missingMapStylesheetParts(MAPLIBRE)).toEqual(['.maplibregl-map { font: inherit }']);
  });

  it('exige .maplibregl-map', () => {
    expect(missingMapStylesheetParts('.otra{font:inherit}')).toEqual(['.maplibregl-map', '.maplibregl-map { font: inherit }']);
  });

  it('la regla font: inherit tiene que ser de .maplibregl-map y no de otro selector', () => {
    expect(missingMapStylesheetParts(`${MAPLIBRE}.otra{font:inherit}`)).toEqual(['.maplibregl-map { font: inherit }']);
  });
});

describe('src/map/maplibre.css (fuente de dist/karpo-map.css)', () => {
  const source = readFileSync(
    path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'src', 'map', 'maplibre.css'),
    'utf8',
  );

  it('trae la regla font: inherit sobre .maplibregl-map en karpo.components', () => {
    expect(missingMapStylesheetParts(source)).toEqual([]);
    expect(source).toMatch(/@layer karpo\.components\s*\{[^}]*\.maplibregl-map/);
  });

  it('importa la hoja de MapLibre en la subcapa karpo.components.maplibre', () => {
    expect(source).toMatch(/@import url\("maplibre-gl\/dist\/maplibre-gl\.css"\) layer\(karpo\.components\.maplibre\)/);
  });
});

describe('hasMapLibreRules', () => {
  it('detecta reglas de MapLibre en la hoja principal', () => {
    expect(hasMapLibreRules('.a{color:red}.maplibregl-map{position:relative}')).toBe(true);
  });
  it('no avisa si la hoja principal no las trae', () => {
    expect(hasMapLibreRules('[data-kx-view=map]{display:block}')).toBe(false);
  });
});


// ---------------------------------------------------------------------------------------------
// Avisos de terceros (B-1)

const realRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const PKG = { name: '@karpo/react', license: 'GPL-2.0-only' };

const folders = [];
afterEach(() => {
  for (const folder of folders.splice(0)) rmSync(folder, { recursive: true, force: true });
});

/** Proyecto de juguete: un paquete instalado y los ficheros dados. */
function toyProject(files = {}) {
  const root = mkdtempSync(path.join(tmpdir(), 'pack-check-'));
  folders.push(root);
  const lib = path.join(root, 'node_modules', 'toy-lib');
  mkdirSync(lib, { recursive: true });
  writeFileSync(path.join(lib, 'package.json'), JSON.stringify({ name: 'toy-lib', version: '1.0.0' }));
  writeFileSync(path.join(lib, 'LICENSE'), 'MIT License\n\nCopyright (c) 2026 Toy\n\nSe concede permiso…');
  for (const [rel, content] of Object.entries(files)) {
    const target = path.join(root, ...rel.split('/'));
    mkdirSync(path.dirname(target), { recursive: true });
    writeFileSync(target, content);
  }
  return root;
}

const TOY = {
  id: 'toy',
  paquete: 'toy-lib',
  licencia: 'MIT',
  motivo: 'Una hoja de estilos de prueba.',
  donde: ['`dist/karpo.css`'],
  cabeceraEn: ['karpo.css'],
  entra: '`src/a.css`',
  generadoPor: [],
};

/** Quita del documento la sección de un paquete (las secciones son `## <n>. <paquete> <versión>`). */
function withoutSection(notices, paquete) {
  const sections = notices.split(/\n(?=## \d+\. )/);
  return sections.filter((section) => !section.startsWith(`## `) || !section.split('\n')[0].includes(paquete)).join('\n');
}

describe('thirdPartyProblems: el paquete lleva los avisos de lo incrustado', () => {
  const real = () => {
    const input = { root: realRoot };
    return {
      notices: renderNotices({ root: realRoot, pkg: PKG }),
      karpoCss: withBanner('.react-flow{}', 'karpo.css', input),
      mapCss: withBanner('.maplibregl-map{font:inherit}', 'karpo-map.css', input),
    };
  };

  it('pasa con el documento generado, las dos cabeceras y el repositorio real', () => {
    expect(thirdPartyProblems({ root: realRoot, ...real() })).toEqual([]);
  });

  it('FALLA sin el fichero de avisos', () => {
    const problems = thirdPartyProblems({ root: realRoot, ...real(), notices: undefined });
    expect(problems).toEqual([expect.stringContaining('falta dist/THIRD-PARTY-NOTICES.md')]);
  });

  it('FALLA con un origen del registro ausente del fichero (cada uno de los tres)', () => {
    const complete = real();
    for (const origen of ORIGENES) {
      const without = withoutSection(complete.notices, origen.paquete);
      expect(without.length, origen.paquete).toBeLessThan(complete.notices.length);
      const problems = thirdPartyProblems({ root: realRoot, ...complete, notices: without });
      expect(
        problems.some((problem) => problem.includes(origen.paquete)),
        `${origen.paquete}: ${problems.join(' | ')}`,
      ).toBe(true);
    }
  });

  it('FALLA con el texto de una licencia recortado', () => {
    const complete = real();
    const trimmed = complete.notices.replace('Permission is hereby granted', 'Permiso');
    expect(trimmed).not.toBe(complete.notices);
    const problems = thirdPartyProblems({ root: realRoot, ...complete, notices: trimmed });
    expect(problems.length).toBeGreaterThan(0);
    expect(problems[0]).toContain('no trae el texto íntegro de la licencia');
  });

  it('FALLA con un @import de un paquete no registrado en un CSS de src', () => {
    const root = toyProject({
      'src/a.css': '@import url("../theme/layers.css");\n@import url("intruso/dist/estilos.css") layer(karpo.x);',
    });
    const input = { root, origenes: [TOY] };
    const problems = thirdPartyProblems({
      ...input,
      notices: renderNotices({ root, origenes: [TOY], pkg: PKG }),
      karpoCss: withBanner('.a{}', 'karpo.css', input),
      mapCss: '.b{}',
    });
    expect(problems).toEqual([expect.stringMatching(/^src\/a\.css: .*@import.*intruso/)]);
  });

  it('FALLA con una fuente de node_modules no registrada en un mapa de dist', () => {
    const root = toyProject({
      'dist/karpo-react.js.map': JSON.stringify({ sources: ['../src/index.ts', '../node_modules/colado/index.js'] }),
    });
    const input = { root, origenes: [TOY] };
    const problems = thirdPartyProblems({
      ...input,
      notices: renderNotices({ root, origenes: [TOY], pkg: PKG }),
      karpoCss: withBanner('.a{}', 'karpo.css', input),
      mapCss: '.b{}',
    });
    expect(problems).toEqual([expect.stringMatching(/node_modules\/colado/)]);
  });

  it('FALLA si a una hoja le falta la cabecera /*!', () => {
    const complete = real();
    expect(thirdPartyProblems({ root: realRoot, ...complete, karpoCss: '.react-flow{}' })).toEqual([
      expect.stringContaining('dist/karpo.css debería empezar por un comentario /*!'),
    ]);
    expect(thirdPartyProblems({ root: realRoot, ...complete, mapCss: '.maplibregl-map{}' })).toEqual([
      expect.stringContaining('dist/karpo-map.css debería empezar por un comentario /*!'),
    ]);
  });
});
