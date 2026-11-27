// Cabecera de la entidad: recuento plural, elección del control de vista y ruta a la que lleva cada opción.
import { describe, expect, it } from 'vitest';
import { classifyWidth } from './hooks';
import { canCreateEntity, countText, currentChoice, routeForChoice } from './EntityHeader';
import { viewChoices, type EntityConfig, type EntityRoute } from '../contracts';

const text = (literal: string) => ({ literal });

function entity(views: EntityConfig['views'], extra: Partial<EntityConfig> = {}): EntityConfig {
  return {
    id: 'facility',
    name: text('Instalación'),
    namePlural: text('Instalaciones'),
    editable: true,
    form: { create: true, sections: [] },
    permissions: {},
    views,
    defaultView: 'grid',
    ...extra,
  } as unknown as EntityConfig;
}

const ALL_VIEWS = [{ kind: 'grid' }, { kind: 'tree' }, { kind: 'map' }] as unknown as EntityConfig['views'];

const messages: Record<string, string> = {
  'header.count': '{{n}} {{name}}',
  'header.countOf': '{{matched}} de {{total}} {{name}}',
};
function t(key: string, vars: Record<string, unknown> = {}): string {
  return (messages[key] ?? key).replace(/\{\{(\w+)\}\}/g, (_all, name: string) => String(vars[name]));
}

describe('recuento de la cabecera', () => {
  it('«248 instalaciones» con el separador de miles del idioma', () => {
    expect(countText(t, 'es', { total: 12345, matched: 12345 }, 'Instalación', 'Instalaciones')).toBe('12.345 instalaciones');
    expect(countText(t, 'en', { total: 12345, matched: 12345 }, 'Facility', 'Facilities')).toBe('12,345 facilities');
  });

  it('con una sola unidad usa el singular y con cero el plural', () => {
    expect(countText(t, 'es', { total: 1, matched: 1 }, 'Instalación', 'Instalaciones')).toBe('1 instalación');
    expect(countText(t, 'es', { total: 0, matched: 0 }, 'Instalación', 'Instalaciones')).toBe('0 instalaciones');
  });

  it('con filtro cuenta lo que coincide «de» lo que hay', () => {
    expect(countText(t, 'es', { total: 12345, matched: 12 }, 'Instalación', 'Instalaciones')).toBe('12 de 12.345 instalaciones');
    // El nombre concuerda con el total: «1 de 3 instalaciones», no «1 de 3 instalación».
    expect(countText(t, 'es', { total: 3, matched: 1 }, 'Instalación', 'Instalaciones')).toBe('1 de 3 instalaciones');
  });

  it('sin el total sin filtrar cuenta sólo las coincidencias; sin recuento, nada', () => {
    expect(countText(t, 'es', { matched: 12 }, 'Instalación', 'Instalaciones')).toBe('12 instalaciones');
    expect(countText(t, 'es', { loading: true }, 'Instalación', 'Instalaciones')).toBe('');
    expect(countText(t, 'es', undefined, 'Instalación', 'Instalaciones')).toBe('');
  });

  it('baja la inicial del nombre, salvo acrónimos y alemán', () => {
    expect(countText(t, 'es', { total: 5, matched: 5 }, 'CRM', 'CRM')).toBe('5 CRM');
    expect(countText(t, 'de', { total: 5, matched: 5 }, 'Anlage', 'Anlagen')).toBe('5 Anlagen');
    expect(countText(t, 'en', { total: 5, matched: 5 }, 'Facility', 'Facilities')).toBe('5 facilities');
  });
});

describe('control de vista', () => {
  it('la opción elegida sale de la vista de la ruta y, en el árbol, de su disposición', () => {
    const config = entity(ALL_VIEWS);
    const route = (extra: Partial<EntityRoute>): EntityRoute => ({ entity: 'facility', params: {}, ...extra });
    expect(currentChoice(config, route({}))?.id).toBe('table');
    expect(currentChoice(config, route({ view: 'tree' }))?.id).toBe('tree');
    expect(currentChoice(config, route({ view: 'tree', params: { layout: 'diagram' } }))?.id).toBe('diagram');
    expect(currentChoice(config, route({ view: 'map', params: { layout: 'list' } }))?.id).toBe('map');
    // Una vista que la entidad no declara se sustituye por la de por defecto.
    expect(currentChoice(entity([{ kind: 'grid' }] as never), route({ view: 'map' }))?.id).toBe('table');
  });

  it('cada opción lleva a su vista y su disposición, y conserva el registro y los demás parámetros', () => {
    const choices = viewChoices(entity(ALL_VIEWS));
    const base: EntityRoute = { entity: 'facility', id: '7', mode: 'read', params: { q: 'sur', layout: 'diagram' } };
    const by = (id: string) => routeForChoice(base, choices.find((choice) => choice.id === id)!);
    expect(by('table')).toEqual({ ...base, view: 'grid', params: { q: 'sur' } });
    expect(by('tree')).toEqual({ ...base, view: 'tree', params: { q: 'sur', layout: 'list' } });
    expect(by('diagram')).toEqual({ ...base, view: 'tree', params: { q: 'sur', layout: 'diagram' } });
    // En el mapa se descarta la disposición del árbol: `layout=list` allí sería «Ver como tabla».
    expect(by('map')).toEqual({ ...base, view: 'map', params: { q: 'sur' } });
  });

  it('una entidad con una sola vista tiene una sola opción', () => {
    expect(viewChoices(entity([{ kind: 'grid' }] as never))).toHaveLength(1);
  });
});

describe('«Nuevo»', () => {
  const session = (allowed: boolean) => ({ can: () => allowed });

  it('exige formulario de alta, entidad editable y permiso de escritura', () => {
    expect(canCreateEntity(entity(ALL_VIEWS), session(true))).toBe(true);
    expect(canCreateEntity(entity(ALL_VIEWS, { permissions: { write: 'facility.write' } }), session(true))).toBe(true);
    expect(canCreateEntity(entity(ALL_VIEWS, { permissions: { write: 'facility.write' } }), session(false))).toBe(false);
    expect(canCreateEntity(entity(ALL_VIEWS, { editable: false }), session(true))).toBe(false);
    expect(canCreateEntity(entity(ALL_VIEWS, { form: null }), session(true))).toBe(false);
    expect(canCreateEntity(entity(ALL_VIEWS, { form: { create: false, sections: [] } } as never), session(true))).toBe(false);
  });
});

describe('tamaño del marco', () => {
  it('compacto hasta 40em y estrecho hasta 64em, según el tamaño de letra', () => {
    expect(classifyWidth(360, 16)).toEqual({ compact: true, narrow: true });
    expect(classifyWidth(640, 16)).toEqual({ compact: true, narrow: true });
    expect(classifyWidth(641, 16)).toEqual({ compact: false, narrow: true });
    expect(classifyWidth(1024, 16)).toEqual({ compact: false, narrow: true });
    expect(classifyWidth(1025, 16)).toEqual({ compact: false, narrow: false });
    // Con letra más grande los mismos píxeles son menos «em»: 700 px con 20 px por em son 35em.
    expect(classifyWidth(700, 20)).toEqual({ compact: true, narrow: true });
    // Sin tamaño de letra legible se supone 16 px.
    expect(classifyWidth(500, Number.NaN)).toEqual({ compact: true, narrow: true });
  });
});
