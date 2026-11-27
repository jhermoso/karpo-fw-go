// URL <-> EntityRoute: la ruta es la fuente de verdad de la pantalla.
import { describe, expect, it } from 'vitest';
import type { EntityConfig } from '../contracts';
import { buildEntityTarget, isViewKind, parseEntityRoute, pickView, targetToString } from './route';

const search = (text: string) => new URLSearchParams(text);

describe('parseEntityRoute', () => {
  it('una lista: vista por defecto y el resto de parámetros son de la vista', () => {
    const route = parseEntityRoute({ entity: 'facility', create: false, search: search('q=sede&sort=name') });
    expect(route).toEqual({ entity: 'facility', params: { q: 'sede', sort: 'name' } });
  });

  it('?view= sólo admite grid, tree y map', () => {
    expect(parseEntityRoute({ entity: 'a', create: false, search: search('view=map') }).view).toBe('map');
    expect(parseEntityRoute({ entity: 'a', create: false, search: search('view=otro') }).view).toBeUndefined();
    expect(isViewKind('tree')).toBe(true);
    expect(isViewKind('kanban')).toBe(false);
    expect(isViewKind(null)).toBe(false);
  });

  it('un registro: lectura por omisión y edición con ?mode=edit; mode y page no van en params', () => {
    const read = parseEntityRoute({ entity: 'a', id: '7', create: false, search: search('') });
    expect(read).toMatchObject({ id: '7', mode: 'read' });
    const edit = parseEntityRoute({ entity: 'a', id: '7', create: false, search: search('mode=edit&page=1&level=region') });
    expect(edit).toEqual({ entity: 'a', id: '7', mode: 'edit', params: { level: 'region' } });
  });

  it('el alta es edición sin id', () => {
    expect(parseEntityRoute({ entity: 'a', create: true, search: search('') })).toEqual({
      entity: 'a',
      mode: 'edit',
      params: {},
    });
  });
});

describe('buildEntityTarget', () => {
  it('la lista es /:entity y el registro /:entity/:id (con el id codificado)', () => {
    expect(buildEntityTarget({ entity: 'facility' })).toEqual({ pathname: '/facility', search: '' });
    expect(buildEntityTarget({ entity: 'facility', id: 'a b/c' })).toEqual({ pathname: '/facility/a%20b%2Fc', search: '' });
  });

  it('?mode=edit sólo con id; sin id y con mode edit es el alta', () => {
    expect(targetToString(buildEntityTarget({ entity: 'facility', id: '7', mode: 'edit' }))).toBe('/facility/7?mode=edit');
    expect(targetToString(buildEntityTarget({ entity: 'facility', id: '7', mode: 'read' }))).toBe('/facility/7');
    expect(targetToString(buildEntityTarget({ entity: 'facility', mode: 'edit' }))).toBe('/facility/nuevo');
  });

  it('la vista por defecto no se escribe; las demás sí', () => {
    expect(targetToString(buildEntityTarget({ entity: 'a', view: 'grid' }, { defaultView: 'grid' }))).toBe('/a');
    expect(targetToString(buildEntityTarget({ entity: 'a', view: 'map' }, { defaultView: 'grid' }))).toBe('/a?view=map');
    expect(targetToString(buildEntityTarget({ entity: 'a', view: 'grid' }, { defaultView: 'tree' }))).toBe('/a?view=grid');
  });

  it('sin params ni view se conserva lo de la pantalla actual (abrir o cerrar un detalle no pierde filtros)', () => {
    const context = { currentEntity: 'a', currentSearch: search('view=map&level=region&mode=edit') };
    expect(targetToString(buildEntityTarget({ entity: 'a', id: '7' }, context))).toBe('/a/7?level=region&view=map');
    expect(targetToString(buildEntityTarget({ entity: 'a' }, context))).toBe('/a?level=region&view=map');
  });

  it('con params, son el conjunto completo: lo que falta se quita y lo vacío no viaja', () => {
    const context = { currentEntity: 'a', currentSearch: search('q=viejo&sort=name') };
    const target = buildEntityTarget({ entity: 'a', params: { q: 'nuevo', sort: '' } }, context);
    expect(targetToString(target)).toBe('/a?q=nuevo');
  });

  it('otra entidad empieza limpia', () => {
    const context = { currentEntity: 'a', currentSearch: search('q=x&view=map') };
    expect(targetToString(buildEntityTarget({ entity: 'b', id: '1' }, context))).toBe('/b/1');
  });

  it('los parámetros reservados no se pueden colar en params', () => {
    const target = buildEntityTarget({ entity: 'a', id: '1', params: { mode: 'edit', view: 'map', page: '1', keep: 'x' } });
    expect(targetToString(target)).toBe('/a/1?keep=x');
  });

  it('la página completa (?page=1) se conserva al moverse entre registros y se quita al volver a la lista', () => {
    const context = { currentEntity: 'a', currentSearch: search('page=1') };
    expect(targetToString(buildEntityTarget({ entity: 'a', id: '2' }, context))).toBe('/a/2?page=1');
    expect(targetToString(buildEntityTarget({ entity: 'a' }, context))).toBe('/a');
  });
});

describe('pickView', () => {
  const config = { views: [{ kind: 'grid' }, { kind: 'tree' }], defaultView: 'tree' } as unknown as EntityConfig;

  it('la de la URL si la entidad la declara; si no, la de por defecto', () => {
    expect(pickView(config, { view: 'grid' })).toBe('grid');
    expect(pickView(config, { view: 'map' })).toBe('tree');
    expect(pickView(config, {})).toBe('tree');
  });
});
