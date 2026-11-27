// Contrato del vocabulario de iconos (D-R20): datos puros, sin Phosphor, y coherentes con el registro actual.
import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, expectTypeOf, it } from 'vitest';
import {
  CORE_ICON_NAMES,
  DIRECTIONAL_ICONS,
  ICON_ALIASES,
  ICON_NAMES,
  isDirectionalIcon,
  isIconName,
  phosphorNameOf,
  type IconName,
} from './index';
import { ICON_DATA_CORE } from '../ui/icon-data-core';
import { ICON_DATA_EXTRA } from '../ui/icon-data-extra';

/** Dibujo de cualquier nombre: núcleo y trozo diferido juntos. */
const ICON_DATA: Readonly<Record<string, unknown>> = { ...ICON_DATA_EXTRA, ...ICON_DATA_CORE };

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..', '..');

function pascal(kebabName: string): string {
  return kebabName
    .split('-')
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join('');
}

describe('ICON_NAMES e isIconName', () => {
  it('es una lista sin duplicados y en minúsculas kebab', () => {
    expect(new Set(ICON_NAMES).size).toBe(ICON_NAMES.length);
    for (const name of ICON_NAMES) expect(name, name).toMatch(/^[a-z][a-z0-9]*(-[a-z0-9]+)*$/);
  });

  it('incluye los cinco nombres del manifiesto de la demo y los que piden las pantallas', () => {
    const required = [
      'buildings',
      'tree-structure',
      'globe-hemisphere-west',
      'map-trifold',
      'calendar-dots',
      'address-book',
      'monitor',
      'palette',
      'dots-three',
      'plus',
      'table',
      'graph',
      'list-bullets',
      'arrows-in',
      'arrows-out',
    ];
    for (const name of required) expect(isIconName(name), name).toBe(true);
  });

  it('acepta los iconos de la demo y de la configuración de Go, y todos los alias actuales', () => {
    for (const name of ['view', 'search', 'edit', 'building', 'package', 'truck', 'delete', 'briefcase', 'box']) {
      expect(isIconName(name), name).toBe(true);
    }
    for (const alias of Object.keys(ICON_ALIASES)) expect(isIconName(alias), alias).toBe(true);
  });

  it('rechaza lo que no es un nombre de icono', () => {
    for (const value of ['', 'Buildings', 'no-existe', 'tree structure', 'constructor', '__proto__', 'toString']) {
      expect(isIconName(value), value).toBe(false);
    }
    for (const value of [undefined, null, 3, {}, ['plus']]) expect(isIconName(value)).toBe(false);
  });

  it('estrecha el tipo a IconName', () => {
    const value: unknown = 'plus';
    expect(isIconName(value)).toBe(true);
    if (isIconName(value)) expectTypeOf(value).toEqualTypeOf<IconName>();
  });
});

describe('ICON_ALIASES', () => {
  it('cada alias apunta a un nombre admitido y todos los alias son nombres', () => {
    for (const [alias, target] of Object.entries(ICON_ALIASES)) {
      expect(isIconName(alias), alias).toBe(true);
      expect(isIconName(target), `${alias} -> ${target}`).toBe(true);
    }
  });

  it('cada alias se dibuja con los datos del icono de Phosphor al que apunta (icon-data-*.ts generados)', () => {
    for (const [alias, target] of Object.entries(ICON_ALIASES)) {
      expect(ICON_DATA[alias], alias).toBe(ICON_DATA[target]);
    }
    expect(Object.keys(ICON_DATA).sort()).toEqual([...ICON_NAMES].sort());
  });

  it('el núcleo y el trozo diferido reparten ICON_NAMES sin solaparse', () => {
    const core = Object.keys(ICON_DATA_CORE);
    const extra = Object.keys(ICON_DATA_EXTRA);
    expect(core.filter((name) => extra.includes(name))).toEqual([]);
    expect([...core, ...extra].sort()).toEqual([...ICON_NAMES].sort());
  });

  it('phosphorNameOf resuelve alias y deja los nombres de Phosphor tal cual', () => {
    expect(phosphorNameOf('trash')).toBe('trash');
    expect(phosphorNameOf('delete')).toBe('trash');
    expect(phosphorNameOf('chevron-down')).toBe('caret-down');
    expect(phosphorNameOf('buildings')).toBe('buildings');
    expect(phosphorNameOf('calendar-dots')).toBe('calendar-dots');
  });

  it('todo nombre resuelve a un icono que existe en Phosphor (con sus pesos regular y fill)', () => {
    const defs = path.join(root, 'node_modules', '@phosphor-icons', 'react', 'dist', 'defs');
    if (!existsSync(defs)) return;
    for (const name of ICON_NAMES) {
      const file = path.join(defs, `${pascal(phosphorNameOf(name))}.es.js`);
      expect(existsSync(file), `${name} -> ${phosphorNameOf(name)}`).toBe(true);
      const source = readFileSync(file, 'utf8');
      expect(source, name).toContain('regular');
      expect(source, name).toContain('fill');
    }
  });
});

describe('CORE_ICON_NAMES (D-R37, M-11)', () => {
  it('es un subconjunto de ICON_NAMES, sin duplicados', () => {
    expect(new Set(CORE_ICON_NAMES).size).toBe(CORE_ICON_NAMES.length);
    for (const name of CORE_ICON_NAMES) expect(isIconName(name), name).toBe(true);
    expect(CORE_ICON_NAMES.length).toBeLessThan(ICON_NAMES.length);
  });

  it('incluye los iconos que piden las pantallas nuevas de la ronda 3 (x, caret-right, copy, link, arrow-square-out)', () => {
    for (const name of ['x', 'caret-right', 'copy', 'link', 'arrow-square-out'] as const) {
      expect(CORE_ICON_NAMES, name).toContain(name);
    }
  });

  it('todos están en el módulo estático del núcleo, y sus alias del mismo dibujo con ellos', () => {
    for (const name of CORE_ICON_NAMES) expect(ICON_DATA_CORE[name], name).toBeDefined();
    for (const [alias, target] of Object.entries(ICON_ALIASES)) {
      if (ICON_DATA_CORE[target as IconName]) expect(ICON_DATA_CORE[alias as IconName], alias).toBeDefined();
    }
  });
});

describe('DIRECTIONAL_ICONS', () => {
  it('son nombres admitidos', () => {
    for (const name of DIRECTIONAL_ICONS) expect(isIconName(name), name).toBe(true);
  });

  it('son exactamente los que se espejaban en el registro de la ronda 1, con sus nombres de Phosphor', () => {
    const current = [
      'arrow-left', 'arrow-right', 'back', 'caret-left', 'caret-right', 'chevron-left', 'chevron-right',
      'list-bullets', 'log-in', 'log-out', 'next', 'redo', 'sidebar', 'sign-in', 'sign-out', 'undo',
    ];
    for (const alias of current) {
      expect(isDirectionalIcon(alias), alias).toBe(true);
      expect(isDirectionalIcon(ICON_ALIASES[alias as keyof typeof ICON_ALIASES]), alias).toBe(true);
    }
    const mirrored = new Set<string>(
      current.flatMap((alias) => [alias, ICON_ALIASES[alias as keyof typeof ICON_ALIASES]]),
    );
    expect([...DIRECTIONAL_ICONS].sort()).toEqual([...mirrored].sort());
  });

  it('el resto no se espeja y lo desconocido tampoco', () => {
    for (const name of ['plus', 'trash', 'caret-down', 'map-trifold', 'no-existe']) {
      expect(isDirectionalIcon(name), name).toBe(false);
    }
  });
});

describe('src/contracts/icons.ts es de datos puros', () => {
  it('no importa Phosphor ni nada de la interfaz', () => {
    const source = readFileSync(path.join(here, 'icons.ts'), 'utf8');
    expect(source).not.toMatch(/^\s*import\b/m);
    expect(source).not.toMatch(/from\s+['"]@phosphor-icons/);
  });
});
