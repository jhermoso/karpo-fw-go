import { describe, expect, it, vi } from 'vitest';
import type { MapLevelDef } from '../contracts';
import {
  childLevel,
  crumbsMatch,
  geojsonUrlFor,
  levelStem,
  mapLayoutOf,
  navParams,
  parentCandidates,
  parseNav,
  resolveEntry,
  sameNav,
  skipsSingleLevels,
  upTarget,
  type Crumb,
  type EntryFeature,
  type MapNav,
  type MapViewDef,
} from './nav';

const level = (id: MapLevelDef['id'], boundaryLevel: string): MapLevelDef => ({
  id,
  label: { literal: id },
  boundaryLevel,
  codeProperty: 'code',
  source: 'demo-geojson',
});
const view: MapViewDef = {
  kind: 'map',
  boundaryField: 'location.geoBoundary',
  aggregate: 'count',
  levels: [level('world', 'country'), level('country', '2'), level('region', '3'), level('city', 'local')],
};

describe('estado del mapa en la URL', () => {
  it('lee nivel y código; un nivel desconocido, el mundo o un nivel sin código vuelven al mundo', () => {
    expect(parseNav(view, { level: 'region', code: 'ES30' })).toEqual({ index: 2, code: 'ES30' });
    expect(parseNav(view, { level: 'city', code: ' ES300 ' })).toEqual({ index: 3, code: 'ES300' });
    expect(parseNav(view, { level: 'world', code: 'ES' })).toEqual({ index: 0, code: null });
    expect(parseNav(view, { level: 'galaxia', code: 'ES' })).toEqual({ index: 0, code: null });
    expect(parseNav(view, { level: 'country' })).toEqual({ index: 0, code: null });
    expect(parseNav(view, undefined)).toEqual({ index: 0, code: null });
  });

  it('escribe level y code, conserva el resto y los quita en el mundo', () => {
    expect(navParams(view, { index: 1, code: 'ES' }, { layout: 'list', level: 'x', code: 'y' })).toEqual({
      layout: 'list',
      level: 'country',
      code: 'ES',
    });
    expect(navParams(view, { index: 0, code: null }, { layout: 'list', level: 'country', code: 'ES' })).toEqual({ layout: 'list' });
    expect(navParams(view, { index: 0, code: null }, undefined)).toEqual({});
  });

  it('disposición del interruptor: mapa por defecto', () => {
    expect(mapLayoutOf(undefined)).toBe('map');
    expect(mapLayoutOf({ layout: 'list' })).toBe('list');
    expect(mapLayoutOf({ layout: 'otra' })).toBe('map');
  });

  it('el fichero de cada nivel es world o <nivel>-<código del padre>', () => {
    expect(levelStem(view.levels[0]!, 0, null)).toBe('world');
    expect(levelStem(view.levels[1]!, 1, 'ES')).toBe('country-ES');
    expect(levelStem(view.levels[3]!, 3, 'ES300')).toBe('city-ES300');
    expect(geojsonUrlFor('/geo/{level}.geojson', view, { index: 2, code: 'ES30' })).toBe('/geo/region-ES30.geojson');
    expect(geojsonUrlFor('/geo/{level}.geojson', view, { index: 0, code: null })).toBe('/geo/world.geojson');
    expect(geojsonUrlFor('/geo/fijo.geojson', view, { index: 1, code: 'ES' })).toBe('/geo/fijo.geojson');
  });

  it('niveles siguientes, padres posibles por código y comparación', () => {
    expect(childLevel(view, 0)?.id).toBe('country');
    expect(childLevel(view, 3)).toBeUndefined();
    expect(parentCandidates('ES300')).toEqual(['ES30', 'ES3', 'ES']);
    expect(parentCandidates('ES')).toEqual([]);
    expect(sameNav({ index: 1, code: 'ES' }, { index: 1, code: 'ES' })).toBe(true);
    expect(sameNav({ index: 1, code: 'ES' }, { index: 2, code: 'ES' })).toBe(false);
  });

  it('las migas conocidas sólo valen para el lugar al que apuntan', () => {
    const crumbs = [
      { index: 0, code: null, name: 'Mundo' },
      { index: 1, code: 'ES', name: 'España' },
    ];
    expect(crumbsMatch(crumbs, { index: 1, code: 'ES' })).toBe(true);
    expect(crumbsMatch(crumbs, { index: 1, code: 'FR' })).toBe(false);
    expect(crumbsMatch(crumbs, { index: 2, code: 'ES' })).toBe(false);
    expect(crumbsMatch(null, { index: 0, code: null })).toBe(false);
    expect(crumbsMatch([], { index: 0, code: null })).toBe(false);
  });
});

describe('niveles con una sola delimitación (M-4, Q-10)', () => {
  const box = [0, 0, 1, 1] as EntryFeature['bbox'];
  const feature = (code: string, name: string, hasChildren = true): EntryFeature => ({ code, name, hasChildren, bbox: box });
  // Lo que hay en cada nivel al abrir un lugar, como los ficheros de la demo: la comunidad de Madrid sólo tiene
  // la provincia de Madrid (con municipios); Cataluña tiene cuatro provincias.
  const data: Record<string, EntryFeature[]> = {
    '2:ES30': [feature('ES300', 'Madrid')],
    '2:ES51': [feature('ES511', 'Barcelona'), feature('ES512', 'Girona', false), feature('ES513', 'Lleida', false), feature('ES514', 'Tarragona', false)],
    '2:ES12': [feature('ES120', 'Asturias', false)],
    '3:ES300': [feature('28079', 'Madrid', false), feature('28005', 'Alcalá', false)],
  };
  const calls: string[] = [];
  const load = async (nav: MapNav) => {
    calls.push(`${nav.index}:${nav.code}`);
    return data[`${nav.index}:${nav.code}`] ?? null;
  };
  const entered = (index: number, code: string, name: string): Crumb => ({ index, code, name, bbox: box });

  it('se salta por omisión: la única provincia con detalle lleva directa a los municipios, y la miga saltada lo dice', async () => {
    calls.length = 0;
    expect(skipsSingleLevels(view)).toBe(true);
    const result = await resolveEntry(view, entered(2, 'ES30', 'Comunidad de Madrid'), load);
    expect(result.nav).toEqual({ index: 3, code: 'ES300' });
    expect(result.crumbs).toEqual([
      { index: 2, code: 'ES30', name: 'Comunidad de Madrid', bbox: box, skipped: true },
      { index: 3, code: 'ES300', name: 'Madrid', bbox: box },
    ]);
    // Mira el nivel de las provincias y, como el siguiente no tiene nivel debajo, no mira más.
    expect(calls).toEqual(['2:ES30']);
  });

  it('con skipSingle: false no se salta nada y ni siquiera mira el nivel', async () => {
    calls.length = 0;
    const conserved: MapViewDef = { ...view, skipSingle: false };
    expect(skipsSingleLevels(conserved)).toBe(false);
    const result = await resolveEntry(conserved, entered(2, 'ES30', 'Comunidad de Madrid'), load);
    expect(result.nav).toEqual({ index: 2, code: 'ES30' });
    expect(result.crumbs).toEqual([entered(2, 'ES30', 'Comunidad de Madrid')]);
    expect(calls).toEqual([]);
  });

  it('un nivel con varias delimitaciones se enseña, y uno con una sola sin detalle también (no hay a dónde bajar)', async () => {
    const catalonia = await resolveEntry(view, entered(2, 'ES51', 'Cataluña'), load);
    expect(catalonia.nav).toEqual({ index: 2, code: 'ES51' });
    expect(catalonia.crumbs).toHaveLength(1);
    expect(catalonia.crumbs[0]!.skipped).toBeUndefined();
    const asturias = await resolveEntry(view, entered(2, 'ES12', 'Asturias'), load);
    expect(asturias.nav).toEqual({ index: 2, code: 'ES12' });
    expect(asturias.crumbs[0]!.skipped).toBeUndefined();
  });

  it('sin geometría del nivel (404, o fuente de teselas) no se salta: se enseña lo pedido', async () => {
    const none = vi.fn(async () => null);
    const result = await resolveEntry(view, entered(2, 'ES99', 'Sin detalle'), none);
    expect(result.nav).toEqual({ index: 2, code: 'ES99' });
    expect(none).toHaveBeenCalledTimes(1);
  });

  it('puede saltar varios niveles seguidos y nunca pasa del último', async () => {
    const chain: Record<string, EntryFeature[]> = {
      '1:ES': [feature('ES30', 'Comunidad de Madrid')],
      '2:ES30': [feature('ES300', 'Madrid')],
      '3:ES300': [feature('28079', 'Madrid', true)],
    };
    const result = await resolveEntry(view, entered(1, 'ES', 'España'), async (nav) => chain[`${nav.index}:${nav.code}`] ?? null);
    expect(result.nav).toEqual({ index: 3, code: 'ES300' });
    expect(result.crumbs.map((c) => [c.code, c.skipped === true])).toEqual([
      ['ES', true],
      ['ES30', true],
      ['ES300', false],
    ]);
  });

  it('«Subir» pasa de largo las migas saltadas', () => {
    const crumbs: Crumb[] = [
      { index: 0, code: null, name: 'Mundo' },
      { index: 1, code: 'ES', name: 'España' },
      { index: 2, code: 'ES30', name: 'Comunidad de Madrid', skipped: true },
      { index: 3, code: 'ES300', name: 'Madrid' },
    ];
    expect(upTarget(crumbs)).toBe(1);
    expect(upTarget(crumbs.slice(0, 2))).toBe(0);
    expect(upTarget(crumbs.slice(0, 1))).toBe(-1);
    expect(upTarget([{ index: 0, code: null, name: 'Mundo' }, { index: 1, code: 'ES', name: 'España', skipped: true }, { index: 2, code: 'ES30', name: 'x' }])).toBe(0);
  });
});
