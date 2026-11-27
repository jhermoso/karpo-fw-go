import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { EntityApi, FieldDef, FieldKind, LookupDef, Row } from '../contracts';
import {
  cachedLookupId,
  cachedLookupLabel,
  clearLookupCache,
  lookupEntityConfig,
  resolveLookupLabel,
  searchLookup,
} from './lookup-cache';
import { parsePaste, parseTsv, toTsv } from './paste';

function def(kind: FieldKind, extra: Partial<FieldDef> = {}): FieldDef {
  return { field: kind, label: { literal: kind }, kind, required: false, editable: true, ...extra };
}

describe('parseTsv', () => {
  it('separa filas y columnas, con CRLF y sin la fila vacía final de Excel', () => {
    expect(parseTsv('a\tb\r\nc\td\r\n')).toEqual([
      ['a', 'b'],
      ['c', 'd'],
    ]);
    expect(parseTsv('uno')).toEqual([['uno']]);
    expect(parseTsv('')).toEqual([]);
    expect(parseTsv('a\t\tc')).toEqual([['a', '', 'c']]);
    expect(parseTsv('a\rb')).toEqual([['a'], ['b']]);
  });

  it('entiende las comillas de Excel con tabuladores, saltos y comillas dobles', () => {
    expect(parseTsv('"a\tb"\t"línea 1\nlínea 2"\t"di ""hola"""')).toEqual([
      ['a\tb', 'línea 1\nlínea 2', 'di "hola"'],
    ]);
    expect(parseTsv('x"y\tz')).toEqual([['x"y', 'z']]);
  });
});

describe('parsePaste', () => {
  const defs = [
    def('text', { required: true }),
    def('money'),
    def('date'),
    def('select', {
      options: [
        { value: 'A', label: { literal: 'Activo' } },
        { value: 'I', label: { literal: 'Inactivo' } },
      ],
    }),
  ];

  it('aplica la coerción de cada columna (es: coma decimal, dd/mm/aaaa, select por etiqueta)', () => {
    const result = parsePaste('Acme\t1.234,50\t14/03/2025\tactivo\nBeta\t7,5\t01/12/2024\tI', defs, 'es');
    expect(result.errors).toBe(0);
    expect(result.rows.map((row) => row.map((cell) => cell.value))).toEqual([
      ['Acme', '1234.50', '2025-03-14', 'A'],
      ['Beta', '7.5', '2024-12-01', 'I'],
    ]);
  });

  it('en en: separadores y fechas del idioma', () => {
    const result = parsePaste('Acme\t1,234.50\t03/14/2025\tA', defs, 'en');
    expect(result.errors).toBe(0);
    expect(result.rows[0]?.map((cell) => cell.value)).toEqual(['Acme', '1234.50', '2025-03-14', 'A']);
  });

  it('marca cada celda errónea con su código para la vista previa', () => {
    const result = parsePaste('\tabc\t31/02/2025\tnada\nOk\t1\t\t', defs, 'es');
    expect(result.errors).toBe(4);
    const [bad, good] = result.rows;
    expect(bad?.map((cell) => cell.code)).toEqual(['required', 'format', 'format', 'format']);
    expect(bad?.every((cell) => !cell.ok)).toBe(true);
    expect(good?.every((cell) => cell.ok)).toBe(true);
    expect(bad?.[1]?.text).toBe('abc');
  });

  it('descarta las columnas sobrantes y rellena las que faltan', () => {
    const result = parsePaste('Acme\t1\t14/03/2025\tA\tsobra', defs, 'es');
    expect(result.rows[0]).toHaveLength(4);
    expect(parsePaste('solo', defs, 'es').rows[0]?.map((c) => c.ok)).toEqual([true, true, true, true]);
  });
});

describe('toTsv', () => {
  it('copia en forma editable y se puede volver a pegar', () => {
    const defs = [def('text'), def('money'), def('date'), def('boolean')];
    const rows = [
      ['Acme', '1200.10', '2025-03-14', true],
      ['con\ttab "y" comillas', '5', null, false],
    ];
    const tsv = toTsv(rows, defs, 'es');
    expect(tsv.split('\n')[0]).toBe('Acme\t1200,10\t14/03/2025\tSí');
    const back = parsePaste(tsv, defs, 'es');
    expect(back.errors).toBe(0);
    expect(back.rows[0]?.map((cell) => cell.value)).toEqual(['Acme', '1200.10', '2025-03-14', true]);
    expect(back.rows[1]?.[0]?.value).toBe('con\ttab "y" comillas');
    expect(toTsv([['x', 'sin def']], [def('text')], 'es')).toBe('x\tsin def');
  });
});

describe('lookup-cache', () => {
  const lookup: LookupDef = { entity: 'company', valueField: 'id', displayField: 'name', filter: { active: true } };
  const rows: Row[] = [
    { id: 'c-1', name: 'Acme' },
    { id: 'c-2', name: 'Beta' },
  ];

  beforeEach(() => {
    clearLookupCache();
  });

  function fakeApi(overrides: Partial<EntityApi> = {}): EntityApi {
    return {
      list: vi.fn().mockResolvedValue({ items: rows, total: 2, page: 1, pageSize: 20 }),
      get: vi.fn().mockResolvedValue(rows[0]),
      ...overrides,
    } as unknown as EntityApi;
  }

  it('busca con EntityApi.list y el filtro search (más el filtro de la definición)', async () => {
    const api = fakeApi();
    const options = await searchLookup(api, null, lookup, '  ac ');
    expect(api.list).toHaveBeenCalledTimes(1);
    const [cfg, query] = (api.list as ReturnType<typeof vi.fn>).mock.calls[0] as [
      { id: string },
      { page: number; pageSize: number; filter: Record<string, unknown> },
    ];
    expect(cfg.id).toBe('company');
    expect(query).toEqual({ page: 1, pageSize: 20, filter: { active: true, search: 'ac' } });
    expect(options).toEqual([
      { value: 'c-1', label: 'Acme', row: rows[0] },
      { value: 'c-2', label: 'Beta', row: rows[1] },
    ]);
    expect(cachedLookupLabel(lookup, 'c-2')).toBe('Beta');
    expect(cachedLookupId(lookup, ' ACME ')).toBe('c-1');
    expect(cachedLookupId(lookup, 'nadie')).toBeUndefined();
  });

  it('sin texto no envía search', async () => {
    const api = fakeApi();
    await searchLookup(api, null, lookup, '');
    const query = (api.list as ReturnType<typeof vi.fn>).mock.calls[0]?.[1] as { filter: Record<string, unknown> };
    expect(query.filter).toEqual({ active: true });
  });

  it('usa valueField y displayField distintos de id y cae al valor si falta la etiqueta', async () => {
    const custom: LookupDef = { entity: 'country', valueField: 'code', displayField: 'name' };
    const api = fakeApi({
      list: vi.fn().mockResolvedValue({
        items: [{ id: '1', code: 'ES', name: 'España' }, { id: '2', code: 'FR' }],
        total: 2,
        page: 1,
        pageSize: 20,
      }),
    });
    const options = await searchLookup(api, null, custom, '');
    expect(options.map((o) => [o.value, o.label])).toEqual([
      ['ES', 'España'],
      ['FR', 'FR'],
    ]);
  });

  it('resuelve la etiqueta de un id con EntityApi.get, una sola petición y desde la caché después', async () => {
    const api = fakeApi();
    const [a, b] = await Promise.all([
      resolveLookupLabel(api, null, lookup, 'c-1'),
      resolveLookupLabel(api, null, lookup, 'c-1'),
    ]);
    expect([a, b]).toEqual(['Acme', 'Acme']);
    expect(api.get).toHaveBeenCalledTimes(1);
    expect(await resolveLookupLabel(api, null, lookup, 'c-1')).toBe('Acme');
    expect(api.get).toHaveBeenCalledTimes(1);
  });

  it('un fallo de red deja la etiqueta sin resolver', async () => {
    const api = fakeApi({ get: vi.fn().mockRejectedValue(new Error('boom')) });
    expect(await resolveLookupLabel(api, null, lookup, 'c-9')).toBeUndefined();
  });

  it('usa la configuración del manifiesto si existe', () => {
    const known = { id: 'company', endpoint: '/companies' } as never;
    const manifest = { entities: { company: known } } as never;
    expect(lookupEntityConfig(manifest, lookup)).toBe(known);
    expect(lookupEntityConfig(null, lookup).displayField).toBe('name');
    expect(cachedLookupLabel(lookup, '')).toBeUndefined();
  });
});
