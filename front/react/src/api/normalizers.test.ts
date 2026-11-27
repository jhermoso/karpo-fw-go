import { describe, expect, it } from 'vitest';
import {
  camelKey,
  camelize,
  extractItems,
  normalizeBoundary,
  normalizeCatalog,
  normalizeChart,
  toPage,
  toRow,
} from './normalizers';

describe('camelKey y camelize', () => {
  it('pasa las claves PascalCase a camelCase', () => {
    expect(camelKey('ID')).toBe('id');
    expect(camelKey('Name')).toBe('name');
    expect(camelKey('RequiresContact')).toBe('requiresContact');
    expect(camelKey('ISOCode')).toBe('isoCode');
    expect(camelKey('already')).toBe('already');
    expect(camelKey('A1')).toBe('a1');
  });

  it('recorre objetos y arrays anidados y respeta los valores', () => {
    expect(camelize({ Position: { Flags: { Salaried: true } }, Items: [{ Name: 'a' }, 3, null] })).toEqual({
      position: { flags: { salaried: true } },
      items: [{ name: 'a' }, 3, null],
    });
    expect(camelize('texto')).toBe('texto');
    expect(camelize(null)).toBeNull();
  });
});

describe('filas y páginas', () => {
  it('toRow garantiza `id` en cadena (id, ID, Id o la clave primaria)', () => {
    expect(toRow({ id: 5 })).toEqual({ id: '5' });
    expect(toRow({ ID: 'a' })).toEqual({ ID: 'a', id: 'a' });
    expect(toRow({ Id: 'b' })).toMatchObject({ id: 'b' });
    expect(toRow({ code: 'X' }, 'code')).toMatchObject({ id: 'X' });
    expect(toRow({})).toEqual({ id: '' });
    expect(toRow('no objeto')).toEqual({ id: '' });
    expect(toRow({ id: null })).toEqual({ id: '' });
  });

  it('extractItems y toPage admiten los dos estilos de página de Go', () => {
    expect(extractItems([1, 2])).toEqual([1, 2]);
    expect(extractItems({ items: [1] })).toEqual([1]);
    expect(extractItems({ data: [2] })).toEqual([2]);
    expect(extractItems('x')).toEqual([]);
    expect(toPage({ items: [{ id: 'a' }], total: 9, totalCount: 9, pageNumber: 2, pageSize: 20 }, { page: 1, pageSize: 5 })).toEqual({
      items: [{ id: 'a' }],
      total: 9,
      page: 2,
      pageSize: 20,
    });
    expect(toPage({ items: [{ id: 'a' }], total: 9, number: 4, size: 10 }, { page: 1, pageSize: 5 })).toMatchObject({ page: 4, pageSize: 10 });
    expect(toPage(undefined, { page: 3, pageSize: 7 })).toEqual({ items: [], total: 0, page: 3, pageSize: 7 });
  });
});

describe('catálogos, árbol de puestos y delimitaciones', () => {
  it('normalizeCatalog acepta lista plana o página', () => {
    expect(normalizeCatalog([{ ID: 'a', Name: 'A' }])).toEqual([{ id: 'a', name: 'A' }]);
    expect(normalizeCatalog({ items: [{ Id: 1 }] })).toEqual([{ id: '1' }]);
    expect(normalizeCatalog(null)).toEqual([]);
  });

  it('normalizeChart tolera nodos vacíos y PascalCase', () => {
    expect(normalizeChart({})).toEqual({ position: { id: '' }, reports: [] });
    const chart = normalizeChart({ Position: { ID: 'r' }, Reports: [{ Position: { ID: 'c' }, Reports: [{ Position: { ID: 'g' } }] }] });
    expect(chart.reports[0]!.reports[0]!.position.id).toBe('g');
  });

  it('normalizeBoundary devuelve antecesores y agrupaciones aunque falten', () => {
    expect(normalizeBoundary({ id: 'b', name: 'N' })).toMatchObject({ id: 'b', ancestors: [], groupings: [] });
  });
});
