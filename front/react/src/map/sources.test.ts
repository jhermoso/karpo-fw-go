import { afterEach, describe, expect, it, vi } from 'vitest';
import type { BoundaryDetail, EntityApiExt } from '../api';
import type { GeoFeature } from './geo';
import {
  absoluteUrl,
  boundaryDetail,
  clearBoundaryCache,
  fetchGeoCollection,
  isSchematicSource,
  loadChains,
  resolveTrail,
} from './sources';

afterEach(() => vi.restoreAllMocks());

const feature = (code: string, name: string, parentCode: string, hasChildren = true): GeoFeature => ({
  code,
  name,
  shortName: '',
  level: '',
  parentCode,
  hasChildren,
  bbox: [0, 0, 1, 1],
  center: [0.5, 0.5],
  poleRadius: 0,
  props: {},
});

describe('migas deducidas de la URL', () => {
  // Geometría por nivel y padre, como los ficheros world / country-ES / region-ES30 de la demo.
  const data: Record<string, GeoFeature[]> = {
    '0:': [feature('ES', 'Spain', ''), feature('FR', 'France', '')],
    '1:ES': [feature('ES30', 'Comunidad de Madrid', 'ES'), feature('ES51', 'Cataluña', 'ES')],
    '2:ES30': [feature('ES300', 'Madrid', 'ES30')],
  };
  const calls: string[] = [];
  const load = async (index: number, parent: string | null) => {
    calls.push(`${index}:${parent ?? ''}`);
    return data[`${index}:${parent ?? ''}`] ?? null;
  };

  it('sube de la delimitación a su padre probando los códigos del padre más probables', async () => {
    calls.length = 0;
    const crumbs = await resolveTrail({ nav: { index: 3, code: 'ES300' }, rootName: 'Mundo', load });
    expect(crumbs).toEqual([
      { index: 0, code: null, name: 'Mundo' },
      { index: 1, code: 'ES', name: 'Spain', bbox: [0, 0, 1, 1] },
      { index: 2, code: 'ES30', name: 'Comunidad de Madrid', bbox: [0, 0, 1, 1] },
      { index: 3, code: 'ES300', name: 'Madrid', bbox: [0, 0, 1, 1] },
    ]);
    // ES300 está en region-ES30 (primer intento); ES30, tras probar ES3, en country-ES; ES en el mundo.
    expect(calls).toEqual(['2:ES30', '1:ES3', '1:ES', '0:']);
  });

  it('con skipSingle marca como saltada la miga cuyo nivel sólo tenía una delimitación con detalle (M-4)', async () => {
    const trail = await resolveTrail({ nav: { index: 3, code: 'ES300' }, rootName: 'Mundo', load, skipSingle: true });
    expect(trail.map((crumb) => [crumb.name, crumb.skipped === true])).toEqual([
      ['Mundo', false],
      ['Spain', false],
      ['Comunidad de Madrid', true],
      ['Madrid', false],
    ]);
    // La comunidad tiene dos provincias en `country-ES` pero el nivel de las provincias de Madrid sólo una.
    const without = await resolveTrail({ nav: { index: 3, code: 'ES300' }, rootName: 'Mundo', load });
    expect(without.some((crumb) => crumb.skipped)).toBe(false);
  });

  it('un nivel de arriba se resuelve con una sola lectura y el mundo no lee nada', async () => {
    calls.length = 0;
    expect(await resolveTrail({ nav: { index: 1, code: 'ES' }, rootName: 'Mundo', load })).toEqual([
      { index: 0, code: null, name: 'Mundo' },
      { index: 1, code: 'ES', name: 'Spain', bbox: [0, 0, 1, 1] },
    ]);
    expect(calls).toEqual(['0:']);
    calls.length = 0;
    expect(await resolveTrail({ nav: { index: 0, code: null }, rootName: 'Mundo', load })).toEqual([{ index: 0, code: null, name: 'Mundo' }]);
    expect(calls).toEqual([]);
  });

  it('si el padre no se encuentra, la miga lleva el código y la ruta termina ahí', async () => {
    const crumbs = await resolveTrail({ nav: { index: 3, code: '28079' }, rootName: 'Mundo', load });
    expect(crumbs).toEqual([
      { index: 0, code: null, name: 'Mundo' },
      { index: 3, code: '28079', name: '28079' },
    ]);
  });
});

describe('cadena de antecesores con caché', () => {
  const details: Record<string, Partial<BoundaryDetail>> = {
    muni: { id: 'muni', level: 'local', lauCode: '28079', ancestors: [{ id: 'prov', name: 'Madrid' }, { id: 'ccaa', name: 'CM' }] as never },
    prov: { id: 'prov', level: '3', nutsCode: 'ES300', ancestors: [] },
    ccaa: { id: 'ccaa', level: '2', nutsCode: 'ES30', ancestors: [] },
    other: { id: 'other', level: 'local', lauCode: '08019', ancestors: [{ id: 'ccaa', name: 'CM' }, { id: 'perdido', name: 'x' }] as never },
  };
  function fakeApi() {
    const calls: string[] = [];
    const api = {
      boundary: vi.fn(async (id: string) => {
        calls.push(id);
        const found = details[id];
        if (!found) throw new Error('404');
        return found as BoundaryDetail;
      }),
    } as unknown as EntityApiExt;
    return { api, calls };
  }

  it('completa los antecesores sin nivel ni código pidiéndolos, una sola vez cada uno', async () => {
    const { api, calls } = fakeApi();
    const chains = await loadChains(api, ['muni', 'other']);
    expect(chains.get('muni')).toEqual([
      { level: 'local', code: '28079' },
      { level: '3', code: 'ES300' },
      { level: '2', code: 'ES30' },
    ]);
    // `other` comparte la comunidad con `muni` (caché) y un antecesor inexistente no rompe la cadena.
    expect(chains.get('other')).toEqual([
      { level: 'local', code: '08019' },
      { level: '2', code: 'ES30' },
    ]);
    expect(calls.filter((id) => id === 'ccaa')).toHaveLength(1);
    expect(calls.filter((id) => id === 'muni')).toHaveLength(1);
    // Una segunda carga no vuelve a pedir lo ya conocido.
    const before = calls.length;
    await loadChains(api, ['muni']);
    expect(calls.length).toBe(before);
  });

  it('una delimitación inexistente no se cuenta y se puede reintentar', async () => {
    const { api, calls } = fakeApi();
    const chains = await loadChains(api, ['no-existe', 'prov']);
    expect(chains.has('no-existe')).toBe(false);
    expect(chains.get('prov')).toEqual([{ level: '3', code: 'ES300' }]);
    await boundaryDetail(api, 'no-existe');
    expect(calls.filter((id) => id === 'no-existe')).toHaveLength(2);
    clearBoundaryCache(api);
    await boundaryDetail(api, 'prov');
    expect(calls.filter((id) => id === 'prov')).toHaveLength(2);
  });
});

describe('lectura de la geometría', () => {
  const ok = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } });

  it('lee una colección, devuelve null ante un 404 y falla ante otros errores o un JSON que no es una colección', async () => {
    const spy = vi.spyOn(globalThis, 'fetch');
    spy.mockResolvedValueOnce(ok({ type: 'FeatureCollection', features: [] }));
    expect(await fetchGeoCollection('http://x.test/geo/world.geojson')).toEqual({ type: 'FeatureCollection', features: [] });
    spy.mockResolvedValueOnce(new Response('no', { status: 404 }));
    expect(await fetchGeoCollection('http://x.test/geo/city-XX.geojson')).toBeNull();
    spy.mockResolvedValueOnce(new Response('mal', { status: 500 }));
    await expect(fetchGeoCollection('http://x.test/geo/world.geojson')).rejects.toThrow(/HTTP 500/);
    spy.mockResolvedValueOnce(ok({ hola: 1 }));
    await expect(fetchGeoCollection('http://x.test/geo/world.geojson')).rejects.toThrow(/colección/);
  });

  it('las direcciones relativas se hacen absolutas contra el origen', () => {
    expect(absoluteUrl('/geo/world.geojson', 'http://localhost:3000/a/b')).toBe('http://localhost:3000/geo/world.geojson');
    expect(absoluteUrl('https://cdn.propio.test/x.pmtiles', 'http://localhost:3000/')).toBe('https://cdn.propio.test/x.pmtiles');
  });

  it('la fuente de demostración se reconoce por su declaración o por su nombre', () => {
    expect(isSchematicSource('x', { type: 'geojson', url: '/g', schematic: true } as never)).toBe(true);
    expect(isSchematicSource('demo-geojson', { type: 'geojson', url: '/g' })).toBe(true);
    expect(isSchematicSource('produccion', { type: 'pmtiles', url: '/g', layer: 'b' })).toBe(false);
    expect(isSchematicSource('demo', undefined)).toBe(false);
  });
});
