// Contrato del núcleo de iconos (D-R37, M-11): los iconos que pintan las pantallas del marco están en
// `CORE_ICON_NAMES` (dentro del trozo inicial, de lectura síncrona) y el núcleo es un subconjunto de
// `ICON_NAMES`. Si una pantalla usa un icono nuevo y no lo añade al núcleo, el menú o la barra lo pintarían
// vacío un instante al arrancar (llega con el trozo diferido): esta prueba lo impide.
//
// «Literal de icono» = una cadena que se pasa como icono: `icon="x"`, `icon={a ? 'x' : 'y'}`, `icon: 'x'`,
// `<Icon name="x" />`, `<Icon name={a ? 'x' : 'y'} />` o el valor de una tabla `MODE_ICON = { … }`. Las
// comparaciones (`kind === 'delete'`) no cuentan.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import { CORE_ICON_NAMES, ICON_NAMES } from '../contracts';

const SRC = path.resolve(__dirname, '..');
const FOLDERS = ['shell', 'ui', 'grid', 'detail', 'auth', 'help'] as const;

function files(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) return entry === '__fixtures__' ? [] : files(full);
    return /\.tsx$/.test(entry) && !/\.test\.tsx$|test-utils|test-doubles/.test(entry) ? [full] : [];
  });
}

/** Cadenas entre comillas de un trozo de código, sin las que son lado derecho de una comparación. */
function quoted(code: string): string[] {
  const withoutComparisons = code.replace(/[!=]==?\s*(['"])[^'"]*\1/g, '');
  return [...withoutComparisons.matchAll(/(['"])([a-z][a-z0-9-]*)\1/g)].map((match) => match[2] ?? '');
}

/** Literales de icono de un fichero fuente. */
function iconLiterals(source: string): string[] {
  const out: string[] = [];
  // `(?<![\w-])`: no es `data-kx-icon=…` ni `xicon=`.
  for (const match of source.matchAll(/(?<![\w-])icon\s*=\s*\{([^}]*)\}/g)) out.push(...quoted(match[1] ?? ''));
  for (const match of source.matchAll(/(?<![\w-])icon\s*=\s*"([^"]+)"/g)) out.push(match[1] ?? '');
  for (const match of source.matchAll(/<Icon\s+name=\{([^}]*)\}/g)) out.push(...quoted(match[1] ?? ''));
  for (const match of source.matchAll(/<Icon\s+name="([^"]+)"/g)) out.push(match[1] ?? '');
  for (const match of source.matchAll(/(?<![\w-])icon\s*:\s*([^,}\n]+)/g)) out.push(...quoted(match[1] ?? ''));
  for (const match of source.matchAll(/\b[A-Z][A-Z_]*ICONS?\b[^=\n]*=\s*\{([^}]*)\}/g)) {
    out.push(...quoted((match[1] ?? '').replace(/\b[a-z]+\s*:/g, '')));
  }
  return out;
}

describe('iconLiterals', () => {
  it('encuentra los literales de icono y se salta comparaciones y atributos data-*', () => {
    const source = `
      <Action icon="plus" />
      <Action icon={open ? 'chevron-up' : 'chevron-down'} />
      <Icon name="search" />
      <Icon name={closed ? 'caret-right' : 'caret-down'} />
      const item = { icon: mode === 'dark' ? 'moon' : 'sun', label: 'x' };
      const MODE_ICON: Record<string, string> = { light: 'sun', dark: 'moon' };
      <svg data-kx-icon={data ? undefined : 'missing'} />
      <Action icon={presentation === 'page' ? 'collapse' : 'expand'} />
    `;
    expect(iconLiterals(source).sort()).toEqual(
      ['plus', 'chevron-up', 'chevron-down', 'search', 'caret-right', 'caret-down', 'moon', 'sun', 'sun', 'moon', 'collapse', 'expand'].sort(),
    );
  });
});

describe('núcleo de iconos', () => {
  it('CORE_ICON_NAMES es un subconjunto de ICON_NAMES', () => {
    const all = new Set<string>(ICON_NAMES);
    expect(CORE_ICON_NAMES.filter((name) => !all.has(name))).toEqual([]);
  });

  it('todo literal de icono de shell, ui, grid, detail, auth y help está en CORE_ICON_NAMES', () => {
    const core = new Set<string>(CORE_ICON_NAMES);
    const outside: string[] = [];
    let seen = 0;
    for (const folder of FOLDERS) {
      for (const file of files(path.join(SRC, folder))) {
        for (const literal of iconLiterals(readFileSync(file, 'utf8'))) {
          seen += 1;
          if (!core.has(literal)) outside.push(`${path.relative(SRC, file).replaceAll('\\', '/')}: «${literal}»`);
        }
      }
    }
    // Si el extractor dejara de encontrar literales, la prueba pasaría en vacío.
    expect(seen).toBeGreaterThan(40);
    expect(outside, 'iconos del marco fuera de CORE_ICON_NAMES (src/contracts/icons.ts; luego `npm run icons:gen`)').toEqual([]);
  });

  it('los iconos que piden las pantallas de la ronda 3 están en el núcleo', () => {
    for (const name of ['x', 'caret-right', 'copy', 'link', 'arrow-square-out'] as const) {
      expect(CORE_ICON_NAMES, name).toContain(name);
    }
  });
});
