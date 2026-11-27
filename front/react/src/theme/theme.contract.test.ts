// Prueba de contrato de los temas (PLAN §4.5, análisis 03 §10.4): lo que Theros Front promete y este
// front necesita. El CI de GitHub está bloqueado: esta prueba es la red local.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import {
  ARIA_ROLES,
  EXPECTED_DICTIONARY_HASH,
  THEROS_ROLES,
  THEROS_VALUES,
  TherosHookError,
  assertTherosHooks,
  findTherosHookViolations,
} from './contract';
import { modesFromManifest, parseThemeManifest } from './manifest';

const ROOT = path.resolve(__dirname, '../..');
const THEMES = ['modern-flat', 'classic-1995', 'glossy-2003', 'souq'];
const THEME_FILES = ['index.css', 'tokens.css', 'modes.css', 'base.css', 'domain.css', 'a11y.css', 'manifest.json'];

const read = (...parts: string[]) => readFileSync(path.join(ROOT, ...parts), 'utf8');

function cssFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) return cssFiles(full);
    return full.endsWith('.css') ? [full] : [];
  });
}

function html(markup: string): HTMLElement {
  const host = document.createElement('div');
  host.innerHTML = markup;
  return host;
}

describe('temas generados por Theros Front (demo/public/themes)', () => {
  it('el hash esperado es el del diccionario v3 del plan', () => {
    expect(EXPECTED_DICTIONARY_HASH).toBe(
      'sha256:2276b82045ae16302c9e07c65a1a4d71dddd3695b4fe490c8f2d4205e65eaeee',
    );
  });

  for (const theme of THEMES) {
    describe(theme, () => {
      it('tiene los seis CSS y manifest.json', () => {
        for (const file of THEME_FILES) {
          expect(() => read('demo/public/themes', theme, file), `${theme}/${file}`).not.toThrow();
        }
      });

      it('su manifest.json declara el hash sha256:2276b820… del diccionario', () => {
        const manifest = JSON.parse(read('demo/public/themes', theme, 'manifest.json')) as {
          dictionary: { hash: string; version: number };
        };
        expect(manifest.dictionary.hash).toBe(EXPECTED_DICTIONARY_HASH);
        expect(manifest.dictionary.hash.startsWith('sha256:2276b820')).toBe(true);
        expect(manifest.dictionary.version).toBe(3);
      });

      it('index.css declara las capas del tema y sus cinco @import, sin nada de terceros', () => {
        const index = read('demo/public/themes', theme, 'index.css');
        expect(index).toContain('@layer th.tokens, th.modes, th.base, th.domain, th.a11y;');
        expect(index.match(/@import url\("[a-z0-9]+\.css"\)/g)).toHaveLength(5);
        expect(index).not.toMatch(/https?:\/\//);
      });

      it('ningún CSS del tema pide recursos fuera de su carpeta (soberanía)', () => {
        for (const file of THEME_FILES.filter((f) => f.endsWith('.css'))) {
          expect(read('demo/public/themes', theme, file), `${theme}/${file}`).not.toMatch(/url\(\s*["']?https?:/);
        }
      });

      it('declara las 167 variables --th-* del contrato', () => {
        const tokens = read('demo/public/themes', theme, 'tokens.css').match(/^\s*--th-[a-z0-9-]+:/gm);
        expect(tokens).toHaveLength(167);
      });
    });
  }

  it('THEMES.md anota commit, hash del diccionario y fecha', () => {
    const doc = read('demo/public/themes/THEMES.md');
    expect(doc).toMatch(/Commit de Theros Front: `[0-9a-f]{40}`/);
    expect(doc).toContain(EXPECTED_DICTIONARY_HASH);
    expect(doc).toMatch(/Fecha de generación: \d{4}-\d{2}-\d{2}/);
    for (const theme of THEMES) expect(doc).toContain(`\`${theme}\``);
  });

  it('los manifiestos reales dan los modos esperados (A3 §7.2)', () => {
    const modes = Object.fromEntries(
      THEMES.map((theme) => [
        theme,
        parseThemeManifest(JSON.parse(read('demo/public/themes', theme, 'manifest.json')), theme),
      ]),
    );
    expect(modes['modern-flat']?.modes).toEqual(['light', 'dark', 'system']);
    expect(modes['souq']?.modes).toEqual(['light', 'dark', 'system']);
    expect(modes['classic-1995']?.modes).toEqual(['light']);
    expect(modes['glossy-2003']?.modes).toEqual(['light']);
    expect(modes['glossy-2003']?.iconStyle).toBe('filled');
    expect(modes['classic-1995']?.iconStyle).toBe('outline');
    expect(modes['souq']?.direction).toBe('rtl');
    expect(modes['modern-flat']?.direction).toBe('ltr');
    for (const info of Object.values(modes)) expect(info.highContrast).toBe(true);
  });

  it('un manifiesto vacío o raro no rompe la lectura', () => {
    expect(parseThemeManifest(null, 'x')).toMatchObject({ name: 'x', modes: ['light'], dictionaryHash: '' });
    expect(parseThemeManifest([], 'x').modes).toEqual(['light']);
    expect(modesFromManifest({ 'design.mode': { value: 'dark' } }, {})).toEqual(['dark']);
    expect(modesFromManifest(undefined, { dark: {} })).toEqual(['light', 'dark', 'system']);
  });
});

describe('karpo.ext: sólo variables del tema, color-mix() y calc()', () => {
  const ext = read('src/theme/kx-ext.css');
  const body = ext.replace(/\/\*[\s\S]*?\*\//g, '');
  const tokens = new Set(
    THEMES.flatMap((theme) =>
      [...read('demo/public/themes', theme, 'tokens.css').matchAll(/^\s*(--th-[a-z0-9-]+):/gm)].map((m) => m[1]),
    ),
  );

  it('toda variable --th-* que usa existe en el contrato (las 167 de los 4 temas)', () => {
    const used = [...body.matchAll(/var\((--th-[a-z0-9-]+)\)/g)].map((m) => m[1]);
    expect(used.length).toBeGreaterThan(50);
    for (const name of new Set(used)) expect(tokens.has(name ?? ''), `${name} no existe en el tema`).toBe(true);
  });

  it('los valores de cada declaración son var(), color-mix(), calc() o palabras clave', () => {
    const declarations = [...body.matchAll(/(--kx-[a-z0-9-]+)\s*:\s*([^;]+);/g)];
    expect(declarations.length).toBeGreaterThan(60);
    for (const [, name, value] of declarations) {
      const text = value ?? '';
      expect(text, name).not.toMatch(/#[0-9a-f]{3,8}\b/i);
      expect(text, name).not.toMatch(/\b(?:rgb|rgba|hsl|hsla|hwb|lab|lch|oklab|oklch)\(/);
      expect(text, name).not.toMatch(/\d(?:px|rem|em|vh|vw|pt|ch|ex)\b/);
      // ni siquiera un número suelto: sólo porcentajes y el factor entero de un calc()
      const stripped = text
        .replace(/var\(--[a-z0-9-]+\)/g, '')
        .replace(/color-mix\(/g, '(')
        .replace(/calc\(/g, '(')
        .replace(/\b(?:in|srgb|oklab|transparent|ease-out)\b/g, '')
        .replace(/\b\d+(?:\.\d+)?%/g, '')
        .replace(/[*()\s,-]/g, '');
      expect(stripped, `${name}: queda «${stripped}» fuera de var()/color-mix()/calc()`).toMatch(/^[0-9]{0,2}$/);
    }
  });

  it('las variables de las tablas del plan existen (celdas, guardado, marca, datos, esqueleto, recorrido)', () => {
    const names = new Set([...body.matchAll(/(--kx-[a-z0-9-]+)\s*:/g)].map((m) => m[1]));
    const expected = [
      '--kx-cell-ring-color', '--kx-cell-editing-bg', '--kx-cell-dirty-mark', '--kx-cell-invalid-mark',
      '--kx-cell-readonly-bg', '--kx-cell-range-bg',
      '--kx-save-saving', '--kx-save-saved', '--kx-save-error', '--kx-save-conflict', '--kx-save-pending',
      '--kx-save-deleted', '--kx-save-locked',
      '--kx-accent-strong', '--kx-accent-bg', '--kx-accent-border',
      '--kx-data-seq-1', '--kx-data-seq-7', '--kx-data-series-1', '--kx-data-series-10', '--kx-data-missing',
      '--kx-skeleton-a', '--kx-skeleton-b', '--kx-spinner-color',
      '--kx-tour-scrim', '--kx-tour-highlight-color',
    ];
    for (const name of expected) expect(names.has(name), name).toBe(true);
  });

  it('declara sobre :root y [data-th-mode], dentro de @layer karpo.ext', () => {
    expect(ext).toContain('@layer karpo.ext {');
    expect(ext).toMatch(/:root,\s*\[data-th-mode\]/);
  });
});

describe('capas y estructura', () => {
  it('layers.css declara el orden de capas del plan', () => {
    expect(read('src/theme/layers.css')).toContain(
      '@layer th.tokens, th.modes, th.base, th.domain, th.a11y, karpo.ext, karpo.components;',
    );
  });

  it('kx-structure.css tiene la escala --kx-z-* con el recorrido por encima del velo de driver.js', () => {
    const css = read('src/theme/kx-structure.css');
    const scale = Object.fromEntries([...css.matchAll(/--kx-z-([a-z]+):\s*(\d+)/g)].map((m) => [m[1], Number(m[2])]));
    expect(Object.keys(scale)).toEqual(
      expect.arrayContaining(['base', 'sticky', 'dropdown', 'overlay', 'modal', 'toast', 'tooltip', 'tour']),
    );
    const order = ['base', 'sticky', 'dropdown', 'overlay', 'modal', 'toast', 'tooltip'].map((n) => scale[n] ?? 0);
    expect(order).toEqual([...order].sort((a, b) => a - b));
    // driver.js pinta su velo con z-index: 10000 en línea
    expect(scale['tour']).toBeGreaterThan(10000);
    expect(Math.max(...order)).toBeLessThan(10000);
  });

  it('kx-structure.css es el único CSS de src con literales numéricos de z-index', () => {
    for (const file of cssFiles(path.join(ROOT, 'src'))) {
      if (file.endsWith('kx-structure.css')) continue;
      const css = readFileSync(file, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
      expect(css, path.relative(ROOT, file)).not.toMatch(/z-index:\s*-?\d/);
    }
  });
});

describe('todo el CSS de src usa variables del tema que existen', () => {
  const tokens = new Set(
    [...read('demo/public/themes/modern-flat/tokens.css').matchAll(/^\s*(--th-[a-z0-9-]+):/gm)].map((m) => m[1]),
  );

  it('ninguna var(--th-*) apunta a una variable inexistente', () => {
    const problems: string[] = [];
    for (const file of cssFiles(path.join(ROOT, 'src'))) {
      const css = readFileSync(file, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
      for (const match of css.matchAll(/var\((--th-[a-z0-9-]+)/g)) {
        if (!tokens.has(match[1])) problems.push(`${path.relative(ROOT, file)}: ${match[1]}`);
      }
    }
    expect(problems).toEqual([]);
  });

  it('ningún CSS de src declara variables --th-* (son del tema, no nuestras)', () => {
    const problems: string[] = [];
    for (const file of cssFiles(path.join(ROOT, 'src'))) {
      const css = readFileSync(file, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
      for (const match of css.matchAll(/^\s*(--th-[a-z0-9-]+)\s*:/gm)) problems.push(`${path.relative(ROOT, file)}: ${match[1]}`);
    }
    expect(problems).toEqual([]);
  });
});

describe('assertTherosHooks', () => {
  it('acepta un DOM con todos los ganchos del contrato', () => {
    const dom = html(`
      <header role="banner" data-th="brand"><nav data-th="nav"><a data-th="nav-item" href="#" aria-current="page">Inicio</a></nav></header>
      <main data-th="sheet" data-th-layer="1">
        <section data-th="section" data-th-layer="2" data-th-elevation="raised">
          <div data-th="field" data-th-required data-th-readonly><label for="a">A</label><input id="a" aria-invalid="false" readonly>
            <p data-th="hint">ayuda</p><p data-th="field-error" data-th-tone="error" role="alert">mal</p></div>
        </section>
        <div data-th="toolbar" role="search"><button type="button" data-th="action" data-th-kind="primary">Ok</button></div>
        <button type="button" id="b" data-th="action" data-th-kind="destructive" aria-disabled="true" data-th-reason="Sin permiso" aria-describedby="why">Borrar</button><span id="why">Sin permiso</span>
        <div data-th="collection" role="grid" aria-busy="false">
          <div role="row"><div role="columnheader" aria-sort="ascending">Nombre</div></div>
          <div data-th="record" role="row" data-th-state="dirty new" aria-selected="true"><div role="gridcell" data-th-format="amount" data-th-trend="up">1</div></div>
        </div>
        <table data-th="collection"><tr data-th="record" data-th-state="inactive"><td>1</td></tr></table>
        <p data-th="collection-empty">Vacío</p>
        <span data-th="badge" data-th-tone="success">ok</span>
        <div data-th="feedback" data-th-tone="info" data-th-elevation="overlay" role="status">hecho</div>
        <button type="button" aria-pressed="mixed" aria-checked="mixed">x</button>
        <svg data-th-icon="building"></svg>
        <div data-th-drop="over" data-th-mode="dark high-contrast" dir="rtl"></div>
        <span data-th="reference" data-th-format="code">X-1</span>
        <div role="dialog" data-th-elevation="modal"></div><div role="tooltip"></div><ul role="menu"><li role="menuitem">a</li></ul><ul role="listbox"><li role="option" aria-selected="false">b</li></ul>
      </main>`);
    expect(() => assertTherosHooks(dom)).not.toThrow();
    expect(findTherosHookViolations(dom)).toEqual([]);
  });

  it('falla ante un data-th con un valor fuera de la lista de A3 §2.3', () => {
    const dom = html('<div data-th="panel">x</div>');
    expect(() => assertTherosHooks(dom)).toThrow(TherosHookError);
    expect(() => assertTherosHooks(dom)).toThrow(/«panel» no es un valor de data-th/);
  });

  const invalid: [string, string, RegExp][] = [
    ['data-th-kind desconocido', '<button type="button" data-th="action" data-th-kind="ghost">x</button>', /«ghost»/],
    ['data-th-tone fuera de lista (accent no es del contrato)', '<span data-th="badge" data-th-tone="accent">x</span>', /«accent»/],
    ['data-th-state fuera de lista', '<table><tbody><tr data-th="record" data-th-state="dirty saving"><td>1</td></tr></tbody></table>', /«saving»/],
    ['data-th-state vacío', '<table><tbody><tr data-th="record" data-th-state=""><td>1</td></tr></tbody></table>', /vacío/],
    ['data-th-format desconocido', '<span data-th-format="money">x</span>', /«money»/],
    ['data-th-trend desconocido', '<span data-th-trend="sideways">x</span>', /«sideways»/],
    ['data-th-layer fuera de 1..3', '<div data-th-layer="4"></div>', /«4»/],
    ['data-th-elevation desconocido', '<div data-th-elevation="floating"></div>', /«floating»/],
    ['data-th-required con valor falso', '<div data-th="field" data-th-required="false"></div>', /«false»/],
    ['data-th-drop desconocido', '<div data-th-drop="hover"></div>', /«hover»/],
    ['data-th-mode desconocido', '<div data-th-mode="sepia"></div>', /«sepia»/],
    ['atributo data-th-* inventado', '<div data-th-foo="1"></div>', /«data-th-foo» no es un gancho/],
    ['data-th-reason vacío', '<button type="button" data-th="action" data-th-reason="">x</button>', /no puede estar vacío/],
    ['data-th-icon vacío', '<svg data-th-icon=""></svg>', /no puede estar vacío/],
    ['role="toolbar" prohibido', '<div role="toolbar"></div>', /role="toolbar" está prohibido/],
    ['role inexistente', '<div role="tabel"></div>', /«tabel» no es un rol ARIA/],
    ['aria-* inexistente', '<div aria-foo="x"></div>', /«aria-foo» no es un atributo ARIA/],
    ['aria-selected con valor raro', '<div role="option" aria-selected="yes"></div>', /«yes»/],
    ['aria-current que el tema no viste', '<a data-th="nav-item" href="#" aria-current="step">x</a>', /«step»/],
    ['aria-sort inválido', '<div role="columnheader" aria-sort="up"></div>', /«up»/],
    ['aria-checked inválido', '<div aria-checked="maybe"></div>', /«maybe»/],
    ['dir inválido', '<div dir="up"></div>', /«dir»/],
    ['data-th-kind fuera de una acción', '<div data-th-kind="primary"></div>', /sólo va en data-th="action"/],
    ['acción no disponible como submit', '<button type="submit" data-th="action" aria-disabled="true" data-th-reason="x" aria-describedby="w">x</button><i id="w"></i>', /type="button"/],
    ['acción no disponible como enlace', '<a href="#" data-th="action" aria-disabled="true" data-th-reason="x" aria-describedby="w">x</a><i id="w"></i>', /type="button"/],
    ['acción no disponible sin motivo visible', '<button type="button" data-th="action" aria-disabled="true" aria-describedby="w">x</button><i id="w"></i>', /data-th-reason/],
    ['acción no disponible sin motivo accesible', '<button type="button" data-th="action" aria-disabled="true" data-th-reason="x">x</button>', /aria-describedby/],
    ['motivo accesible que no existe', '<button type="button" data-th="action" aria-disabled="true" data-th-reason="x" aria-describedby="nada">x</button>', /no existe/],
    ['registro div sin role=row', '<div data-th="record"></div>', /role="row"/],
    ['colección sin role', '<div data-th="collection"></div>', /role="grid"/],
    ['aviso sin role', '<div data-th="feedback"></div>', /role="status"/],
  ];
  for (const [name, markup, message] of invalid) {
    it(`detecta: ${name}`, () => {
      const dom = html(markup);
      const violations = findTherosHookViolations(dom);
      expect(violations.length, markup).toBeGreaterThan(0);
      expect(violations.map((v) => v.message).join('\n')).toMatch(message);
    });
  }

  it('lista TODAS las violaciones, con el elemento, en el mensaje del error', () => {
    const dom = html('<div data-th="panel"></div><span data-th-tone="rojo"></span>');
    try {
      assertTherosHooks(dom);
      expect.unreachable();
    } catch (error) {
      expect(error).toBeInstanceOf(TherosHookError);
      const hook = error as TherosHookError;
      expect(hook.violations).toHaveLength(2);
      expect(hook.violations[0]?.element.getAttribute('data-th')).toBe('panel');
      expect(hook.message).toContain('div[data-th="panel"]');
      expect(hook.message).toContain('span');
    }
  });

  it('comprueba también el propio contenedor', () => {
    const container = document.createElement('div');
    container.setAttribute('data-th', 'nope');
    expect(() => assertTherosHooks(container)).toThrow(/nope/);
  });

  it('con strictRoles sólo admite los 17 roles del tema', () => {
    const dom = html('<ul role="tree"><li role="treeitem">x</li></ul>');
    expect(findTherosHookViolations(dom)).toEqual([]); // tree es un rol ARIA válido
    const strict = findTherosHookViolations(dom, { strictRoles: true });
    expect(strict.map((v) => v.value).sort()).toEqual(['tree', 'treeitem']);
    expect(THEROS_ROLES).toHaveLength(17);
    for (const role of THEROS_ROLES) expect(ARIA_ROLES).toContain(role);
  });

  it('extraModes admite modos propios del tema comprobado', () => {
    const dom = html('<div data-th-mode="dark sepia"></div>');
    expect(findTherosHookViolations(dom)).toHaveLength(1);
    expect(findTherosHookViolations(dom, { extraModes: ['sepia'] })).toEqual([]);
  });

  it('inlineStyle=layout sólo admite variables --kx-* y propiedades de disposición', () => {
    const ok = html('<div style="--kx-col-width: 140px; transform: translateY(10px); inline-size: 4px"></div>');
    expect(findTherosHookViolations(ok, { inlineStyle: 'layout' })).toEqual([]);
    const bad = html('<div style="color: red; background: blue"></div>');
    const violations = findTherosHookViolations(bad, { inlineStyle: 'layout' });
    expect(violations.map((v) => v.value)).toEqual(expect.arrayContaining(['color']));
    // por defecto el atributo style no se inspecciona (Radix y floating-ui lo usan en sus capas)
    expect(findTherosHookViolations(bad)).toEqual([]);
  });

  it('los valores cerrados cubren exactamente los atributos de A3 §2.3', () => {
    expect(Object.keys(THEROS_VALUES).sort()).toEqual(
      [
        'data-th', 'data-th-drop', 'data-th-elevation', 'data-th-format', 'data-th-kind', 'data-th-layer',
        'data-th-mode', 'data-th-readonly', 'data-th-required', 'data-th-state', 'data-th-tone', 'data-th-trend',
      ].sort(),
    );
    expect(THEROS_VALUES['data-th']).toHaveLength(16);
  });
});
