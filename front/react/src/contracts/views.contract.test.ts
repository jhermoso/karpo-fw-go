// Contrato de las vistas: viewChoices (M-6, D-R23), onStatus (M-7), resolve de los códecs y skipSingle (Q-10).
import { describe, expect, expectTypeOf, it } from 'vitest';
import {
  isIconName,
  viewChoices,
  type EntityConfig,
  type EntityViewProps,
  type FieldTypeDef,
  type TextResolver,
  type ViewChoice,
  type ViewDef,
  type ViewStatus,
} from './index';

const MAP_VIEW: Extract<ViewDef, { kind: 'map' }> = {
  kind: 'map',
  boundaryField: 'boundary',
  levels: [],
  aggregate: 'count',
};
const TREE_VIEW: ViewDef = {
  kind: 'tree',
  parentField: 'parent',
  labelField: 'name',
  source: { type: 'flat' },
};

function ids(views: ViewDef[]): string[] {
  return viewChoices({ views }).map((choice) => choice.id);
}

describe('viewChoices', () => {
  it('una entidad sólo con cuadrícula da Tabla', () => {
    expect(viewChoices({ views: [{ kind: 'grid' }] })).toEqual([
      { id: 'table', view: 'grid', icon: 'table', labelKey: 'core:view.table' },
    ]);
  });

  it('tree da Árbol (layout list) y Diagrama (layout diagram) sobre la misma vista', () => {
    const choices = viewChoices({ views: [{ kind: 'grid' }, TREE_VIEW] });
    expect(choices.map((choice) => choice.id)).toEqual(['table', 'tree', 'diagram']);
    expect(choices[1]).toMatchObject({ id: 'tree', view: 'tree', layout: 'list', icon: 'list-bullets' });
    expect(choices[2]).toMatchObject({ id: 'diagram', view: 'tree', layout: 'diagram', icon: 'graph' });
  });

  it('map da Mapa; sólo las opciones de árbol llevan layout', () => {
    const choices = viewChoices({ views: [{ kind: 'grid' }, MAP_VIEW] });
    expect(choices.map((choice) => choice.id)).toEqual(['table', 'map']);
    expect(choices[1]).toEqual({ id: 'map', view: 'map', icon: 'map-trifold', labelKey: 'core:view.map' });
    const all = viewChoices({ views: [{ kind: 'grid' }, TREE_VIEW, MAP_VIEW] });
    expect(all.filter((choice) => choice.layout !== undefined).map((choice) => choice.id)).toEqual(['tree', 'diagram']);
  });

  it('el orden es siempre Tabla, Árbol, Diagrama, Mapa, sea cual sea el de la configuración', () => {
    expect(ids([MAP_VIEW, TREE_VIEW, { kind: 'grid' }])).toEqual(['table', 'tree', 'diagram', 'map']);
    expect(ids([TREE_VIEW, MAP_VIEW, { kind: 'grid' }])).toEqual(['table', 'tree', 'diagram', 'map']);
  });

  it('una vista repetida cuenta una vez y sin vistas no hay opciones', () => {
    expect(ids([{ kind: 'grid' }, { kind: 'grid' }])).toEqual(['table']);
    expect(ids([])).toEqual([]);
  });

  it('cada opción lleva un icono válido, la ruta que escribe y una clave de core', () => {
    const all = viewChoices({ views: [{ kind: 'grid' }, TREE_VIEW, MAP_VIEW] });
    expect(all).toHaveLength(4);
    for (const choice of all) {
      expect(isIconName(choice.icon), choice.id).toBe(true);
      expect(choice.labelKey).toBe(`core:view.${choice.id}`);
      expect(['grid', 'tree', 'map']).toContain(choice.view);
    }
  });

  it('devuelve copias: modificar el resultado no altera las siguientes llamadas', () => {
    const first = viewChoices({ views: [{ kind: 'grid' }] });
    (first[0] as ViewChoice).icon = 'plus';
    expect(viewChoices({ views: [{ kind: 'grid' }] })[0]?.icon).toBe('table');
  });

  it('acepta una EntityConfig completa', () => {
    const config = { views: [{ kind: 'grid' }] } as EntityConfig;
    expect(viewChoices(config)).toHaveLength(1);
  });
});

describe('tipos de los contratos nuevos', () => {
  it('EntityViewProps.onStatus es opcional y recibe un ViewStatus', () => {
    expectTypeOf<EntityViewProps['onStatus']>().toEqualTypeOf<((status: ViewStatus) => void) | undefined>();
    const sin: EntityViewProps = { config: {} as EntityConfig, route: { entity: 'x' }, navigate: () => undefined };
    expect(sin.onStatus).toBeUndefined();
  });

  it('ViewStatus tiene total, matched y loading, todos opcionales', () => {
    const vacio: ViewStatus = {};
    const lleno: ViewStatus = { total: 1204, matched: 12, loading: false };
    expect(vacio).toEqual({});
    expect(lleno.matched).toBe(12);
    expectTypeOf<ViewStatus['total']>().toEqualTypeOf<number | undefined>();
    expectTypeOf<ViewStatus['loading']>().toEqualTypeOf<boolean | undefined>();
  });

  it('FieldTypeDef.format y parse aceptan un resolve opcional (compatible hacia atrás)', () => {
    expectTypeOf<Parameters<FieldTypeDef['format']>[3]>().toEqualTypeOf<TextResolver | undefined>();
    expectTypeOf<Parameters<FieldTypeDef['parse']>[3]>().toEqualTypeOf<TextResolver | undefined>();
    // Una implementación de la ronda 1 (tres parámetros) sigue siendo un FieldTypeDef válido.
    const viejo: Pick<FieldTypeDef, 'format' | 'parse'> = {
      format: (value: unknown) => String(value),
      parse: (text) => ({ ok: true, value: text }),
    };
    expect(viejo.format(1, {} as never, 'es')).toBe('1');
    const resolve: TextResolver = (text) => (text.key ?? '').toUpperCase();
    expect(resolve({ literal: 'Hola', key: 'hola' })).toBe('HOLA');
  });

  it('la vista map admite skipSingle opcional', () => {
    expectTypeOf<NonNullable<(typeof MAP_VIEW)['skipSingle']>>().toEqualTypeOf<boolean>();
    const conservar: typeof MAP_VIEW = { ...MAP_VIEW, skipSingle: false };
    expect(conservar.skipSingle).toBe(false);
    expect(MAP_VIEW.skipSingle).toBeUndefined();
  });
});
