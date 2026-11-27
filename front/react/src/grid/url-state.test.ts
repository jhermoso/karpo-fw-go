// Estado de la consulta en la URL: lectura, escritura, valores por omisión y consulta al servidor.
import { describe, expect, it } from 'vitest';
import { normalizeEntityConfig } from '../config';
import { readRealById } from '../config/__fixtures__/real-configs';
import {
  buildListQuery,
  chipFilters,
  cycleSort,
  quickSearchFilter,
  readQueryState,
  sameParams,
  sortableColumn,
  writeQueryState,
} from './url-state';

const product = normalizeEntityConfig(readRealById('product'));
const holiday = normalizeEntityConfig(readRealById('holiday'));
const carrier = normalizeEntityConfig(readRealById('carrier'));

describe('filtros de la configuración', () => {
  it('separa la búsqueda rápida de los filtros que se muestran como chips', () => {
    expect(quickSearchFilter(product)?.field).toBe('_search');
    expect(quickSearchFilter(product)?.serverField).toBe('search');
    expect(chipFilters(product).map((f) => f.field)).toEqual(['kind']);
    expect(chipFilters(holiday).map((f) => f.field)).toEqual(['year', 'boundary']);
    expect(quickSearchFilter(normalizeEntityConfig(readRealById('work-center')))).toBeUndefined();
  });

  it('sólo las columnas con serverSortField admiten orden de servidor', () => {
    expect(sortableColumn(product, 'sku')?.serverSortField).toBe('sku');
    expect(sortableColumn(product, 'name')?.serverSortField).toBe('name');
    expect(sortableColumn(product, 'basePrice')).toBeUndefined();
    expect(sortableColumn(product, 'inexistente')).toBeUndefined();
  });
});

describe('readQueryState', () => {
  it('sin parámetros toma el orden por omisión de la configuración', () => {
    expect(readQueryState(product, undefined)).toEqual({ search: '', filters: {}, sort: { field: 'sku', direction: 'asc' } });
  });

  it('lee búsqueda, filtros y orden', () => {
    const state = readQueryState(product, { q: 'correa', 'f.kind': 'service', sort: 'name:desc', otro: 'x' });
    expect(state).toEqual({ search: 'correa', filters: { kind: 'service' }, sort: { field: 'name', direction: 'desc' } });
  });

  it('ignora un orden por un campo que el servidor no admite y un filtro que no existe', () => {
    const state = readQueryState(product, { sort: 'basePrice:asc', 'f.nada': '1' });
    expect(state.sort).toBeUndefined();
    expect(state.filters).toEqual({});
  });

  it('un sort vacío es «sin orden» aunque la configuración proponga uno', () => {
    expect(readQueryState(product, { sort: '' }).sort).toBeUndefined();
  });
});

describe('writeQueryState', () => {
  it('conserva los parámetros ajenos y no repite el orden por omisión', () => {
    const state = readQueryState(product, undefined);
    expect(writeQueryState(product, state, { vista: 'a', q: 'viejo', 'f.kind': 'good', sort: 'name:asc' })).toEqual({ vista: 'a' });
  });

  it('escribe lo que se aparta del valor por omisión', () => {
    const params = writeQueryState(
      product,
      { search: 'sensor', filters: { kind: 'good' }, sort: { field: 'name', direction: 'desc' } },
      undefined,
    );
    expect(params).toEqual({ q: 'sensor', 'f.kind': 'good', sort: 'name:desc' });
    expect(readQueryState(product, params)).toEqual({ search: 'sensor', filters: { kind: 'good' }, sort: { field: 'name', direction: 'desc' } });
  });

  it('quitar el orden queda anotado para que no vuelva el de la configuración', () => {
    const params = writeQueryState(product, { search: '', filters: {} }, undefined);
    expect(params).toEqual({ sort: '' });
    expect(readQueryState(product, params).sort).toBeUndefined();
  });

  it('un filtro con valor por omisión que se quita queda anotado como vacío', () => {
    const withDefault = { ...carrier, filters: carrier.filters.map((f) => (f.field === 'isActive' ? { ...f, defaultValue: true } : f)) };
    expect(readQueryState(withDefault, undefined).filters).toEqual({ isActive: 'true' });
    const cleared = writeQueryState(withDefault, { search: '', filters: {} }, undefined);
    expect(cleared['f.isActive']).toBe('');
    expect(readQueryState(withDefault, cleared).filters).toEqual({});
  });
});

describe('buildListQuery', () => {
  it('mapea cada filtro a su campo de servidor, con su tipo, y la búsqueda a su filtro', () => {
    const query = buildListQuery(product, { search: ' bomba ', filters: { kind: 'good' } }, 25);
    expect(query).toEqual({ page: 1, pageSize: 25, filter: { search: 'bomba', kind: 'good' } });
    const hol = buildListQuery(holiday, { search: 'x', filters: { year: '2026' } }, 50);
    expect(hol.filter).toEqual({ search: 'x', year: 2026 });
    const car = buildListQuery(carrier, { search: '', filters: { isActive: 'true' } }, 10);
    expect(car.filter).toEqual({ isActive: true });
  });

  it('sólo envía el orden si el servidor lo admite', () => {
    expect(buildListQuery(product, { search: '', filters: {}, sort: { field: 'name', direction: 'desc' } }, 25).sort).toEqual({
      field: 'name',
      direction: 'desc',
    });
    expect(buildListQuery(product, { search: '', filters: {}, sort: { field: 'basePrice', direction: 'asc' } }, 25).sort).toBeUndefined();
    expect(buildListQuery(product, { search: '', filters: {} }, 25).sort).toBeUndefined();
  });

  it('un número que no lo es no se envía', () => {
    expect(buildListQuery(holiday, { search: '', filters: { year: 'abc' } }, 25).filter).toEqual({});
  });
});

describe('ayudas', () => {
  it('el orden cicla: ascendente, descendente, sin orden', () => {
    const asc = cycleSort(undefined, 'name');
    expect(asc).toEqual({ field: 'name', direction: 'asc' });
    const desc = cycleSort(asc, 'name');
    expect(desc).toEqual({ field: 'name', direction: 'desc' });
    expect(cycleSort(desc, 'name')).toBeUndefined();
    expect(cycleSort(desc, 'sku')).toEqual({ field: 'sku', direction: 'asc' });
  });

  it('compara parámetros sin distinguir ausente de vacío', () => {
    expect(sameParams(undefined, {})).toBe(true);
    expect(sameParams({ a: '' }, {})).toBe(true);
    expect(sameParams({ a: '1' }, { a: '2' })).toBe(false);
    expect(sameParams({ a: '1' }, undefined)).toBe(false);
  });
});
