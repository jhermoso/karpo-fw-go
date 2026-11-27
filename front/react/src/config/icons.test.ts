// Recorrido de iconos de la configuración (B-1): menú y entidad, rutas y casos límite.
import { describe, expect, it } from 'vitest';
import type { MenuNode } from '../contracts';
import { entityIconWarnings, menuIconWarnings, rememberMenuOrigin, unknownIconMessage } from './icons';

const node = (id: string, extra: Partial<MenuNode> = {}): MenuNode => ({ id, label: { literal: id }, ...extra });

describe('menuIconWarnings', () => {
  it('avisa de los iconos fuera del vocabulario con la ruta del árbol recibido', () => {
    const menu = [
      node('a', { icon: 'buildings' }),
      node('b', { icon: 'no-such-icon', children: [node('c'), node('d', { icon: 'tampoco' })] }),
      node('e'),
    ];
    expect(menuIconWarnings(menu)).toEqual([
      { path: 'menu[1].icon', message: unknownIconMessage('no-such-icon'), severity: 'warn' },
      { path: 'menu[1].children[1].icon', message: unknownIconMessage('tampoco'), severity: 'warn' },
    ]);
    expect(menuIconWarnings(menu, {}, 'contexts')[0]!.path).toBe('contexts[1].icon');
  });

  it('prefiere la ruta de origen anotada por el cargador (índices del JSON, no los del menú filtrado)', () => {
    const lonely = node('x', { icon: 'raro' });
    rememberMenuOrigin(lonely, 'menu[4].children[2]');
    expect(menuIconWarnings([lonely]).map((w) => w.path)).toEqual(['menu[4].children[2].icon']);
  });

  it('no repite el aviso de un icono heredado de la entidad (lo avisa la entidad)', () => {
    const inherited = node('p', { entity: 'rara', icon: 'icono-raro' });
    const own = node('q', { entity: 'rara', icon: 'otro-raro' });
    expect(menuIconWarnings([inherited, own], { rara: { icon: 'icono-raro' } }).map((w) => w.path)).toEqual(['menu[1].icon']);
  });

  it('un menú vacío o sin iconos no avisa', () => {
    expect(menuIconWarnings([])).toEqual([]);
    expect(menuIconWarnings([node('a'), node('b', { children: [node('c')] })])).toEqual([]);
  });
});

describe('entityIconWarnings', () => {
  it('recorre cualquier propiedad `icon` del modelo y no los datos (filtros base, valores por omisión)', () => {
    const config = {
      icon: 'building',
      columns: [{ field: 'a', options: [{ value: 'x', icon: 'bad-option' }] }],
      dataSource: { baseFilter: { icon: 'es-un-dato' } },
      form: { sections: [{ id: 'main', icon: 'bad-section', fields: [{ field: 'z', defaultValue: { icon: 'otro-dato' } }] }] },
      warnings: [{ path: 'icon', message: 'icono `x`', severity: 'warn' }],
    } as unknown as Parameters<typeof entityIconWarnings>[0];
    expect(entityIconWarnings(config).map((w) => w.path)).toEqual(['columns[0].options[0].icon', 'form.sections[0].icon']);
  });

  it('un icono que no es texto no avisa y las referencias circulares no cuelgan el recorrido', () => {
    const loop: Record<string, unknown> = { icon: 3 };
    loop.self = loop;
    expect(entityIconWarnings(loop as unknown as Parameters<typeof entityIconWarnings>[0])).toEqual([]);
  });
});
