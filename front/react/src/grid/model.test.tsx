// Modelo de la cuadrícula (adaptador de TanStack Table) y preferencias de columnas.
import { renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { ColumnDef, Row } from '../contracts';
import { CONTENT_MIN_WIDTH, readField, useGridModel } from './model';
import {
  clampWidth,
  defaultPrefs,
  loadPrefs,
  mergePrefs,
  moveColumn,
  placeColumn,
  prefsKey,
  savePrefs,
  setHidden,
  setAutoFit,
  setPinned,
  setWidth,
  MAX_COLUMN_WIDTH,
  MIN_COLUMN_WIDTH,
} from './prefs';

function column(field: string, extra: Partial<ColumnDef> = {}): ColumnDef {
  return { field, label: { literal: field }, kind: 'text', required: false, editable: true, sortable: false, ...extra };
}

const columns: ColumnDef[] = [
  column('sku', { serverSortField: 'sku', widthPx: 140 }),
  column('name', { serverSortField: 'name', widthPx: 260 }),
  column('kind'),
  column('price', { hidden: true }),
  column('location.country'),
];

const rows: Row[] = [
  { id: 'a', sku: 'A', name: 'Uno', location: { country: 'ES' } },
  { id: 'b', sku: 'B', name: 'Dos' },
];

describe('useGridModel', () => {
  it('una insignia, una fecha o un importe no bajan de su ancho mínimo por contenido; un texto sí', () => {
    const narrow: ColumnDef[] = [
      column('state', { kind: 'badge', widthPx: 40 }),
      column('day', { kind: 'date', widthPx: 50 }),
      column('amount', { kind: 'money', widthPx: 60 }),
      column('note', { widthPx: 40 }),
    ];
    const prefs = { ...defaultPrefs(narrow), sizes: { amount: 70 } };
    const { result } = renderHook(() => useGridModel({ columns: narrow, rows, prefs }));
    expect(result.current.columns.map((c) => c.width)).toEqual([
      CONTENT_MIN_WIDTH.badge,
      CONTENT_MIN_WIDTH.date,
      // El ancho que fija el usuario también se respeta como mínimo: 70 < 80.
      CONTENT_MIN_WIDTH.money,
      MIN_COLUMN_WIDTH,
    ]);
    // Una columna más ancha que su mínimo conserva su ancho.
    const wide = renderHook(() => useGridModel({ columns: [column('state', { kind: 'badge', widthPx: 130 })], rows, prefs: defaultPrefs([column('state')]) }));
    expect(wide.result.current.columns[0]?.width).toBe(130);
  });

  it('ordena, oculta, fija y dimensiona según las preferencias', () => {
    const prefs = {
      ...defaultPrefs(columns),
      order: ['name', 'sku', 'kind', 'price', 'location.country'],
      hidden: ['kind'],
      pinned: { start: ['sku'], end: ['location.country'] },
      sizes: { name: 300 },
    };
    const { result } = renderHook(() => useGridModel({ columns, rows, prefs, sort: { field: 'name', direction: 'desc' } }));
    const model = result.current;
    expect(model.columns.map((c) => c.id)).toEqual(['sku', 'name', 'price', 'location.country']);
    expect(model.columns.map((c) => c.pinned)).toEqual(['start', false, false, 'end']);
    expect(model.columns.map((c) => c.width)).toEqual([140, 300, 160, 160]);
    expect(model.columns.map((c) => c.index)).toEqual([0, 1, 2, 3]);
    expect(model.columns[1]).toMatchObject({ sorted: 'desc', sortable: true });
    expect(model.columns[0]).toMatchObject({ sorted: false, sortable: true, offset: 0 });
    expect(model.columns[3]?.offset).toBe(0);
    expect(model.totalWidth).toBe(140 + 300 + 160 + 160);
    expect(model.rowCount).toBe(2);
  });

  it('la columna fijada al final y la segunda fijada al inicio tienen su desplazamiento', () => {
    const prefs = { ...defaultPrefs(columns), pinned: { start: ['sku', 'name'], end: [] } };
    const { result } = renderHook(() => useGridModel({ columns, rows, prefs }));
    const pinned = result.current.columns.filter((c) => c.pinned === 'start');
    expect(pinned.map((c) => c.offset)).toEqual([0, 140]);
    expect(result.current.columns.find((c) => c.id === 'sku')?.sorted).toBe(false);
  });
});

describe('useGridModel · ajuste al ancho (D-R34)', () => {
  const wide: ColumnDef[] = [
    column('a', { widthPx: 160 }),
    column('b', { widthPx: 160, priority: 1 }),
    column('c', { widthPx: 160, priority: 9 }),
    column('d', { widthPx: 160, priority: 5 }),
  ];
  const model = (args: { prefs?: ReturnType<typeof defaultPrefs>; available?: number; columns?: ColumnDef[] }) => {
    const cols = args.columns ?? wide;
    return renderHook(() =>
      useGridModel({ columns: cols, rows, prefs: args.prefs ?? defaultPrefs(cols), ...(args.available !== undefined ? { available: args.available } : {}) }),
    ).result.current;
  };

  it('sin medida (available indefinido) no ajusta nada', () => {
    const result = model({});
    expect(result.columns.map((c) => c.id)).toEqual(['a', 'b', 'c', 'd']);
    expect(result.fitHidden).toEqual([]);
    expect(result.totalWidth).toBe(640);
  });

  it('con ancho suficiente no oculta ni encoge', () => {
    const result = model({ available: 700 });
    expect(result.columns.map((c) => c.width)).toEqual([160, 160, 160, 160]);
    expect(result.fitHidden).toEqual([]);
  });

  it('encoge antes de ocultar y la suma de anchos es el ancho disponible', () => {
    const result = model({ available: 560 });
    expect(result.fitHidden).toEqual([]);
    expect(result.totalWidth).toBe(560);
    expect(result.columns.every((c) => c.width >= 112 && c.width <= 160)).toBe(true);
  });

  it('oculta por priority: sale de `columns` (así aria-colcount y las flechas no la ven) y de `fitHidden`', () => {
    const result = model({ available: 400 });
    expect(result.fitHidden).toEqual(['b']);
    expect(result.columns.map((c) => c.id)).toEqual(['a', 'c', 'd']);
    expect(result.columns.map((c) => c.index)).toEqual([0, 1, 2]);
  });

  it('no toca las preferencias: la columna oculta por ajuste no está en prefs.hidden', () => {
    const prefs = defaultPrefs(wide);
    const before = JSON.stringify(prefs);
    model({ prefs, available: 230 });
    expect(JSON.stringify(prefs)).toBe(before);
    expect(prefs.hidden).toEqual([]);
  });

  it('la primera columna y las fijadas no se ocultan aunque tengan la prioridad más baja', () => {
    const columns = [column('a', { priority: -5 }), column('b', { priority: 9 }), column('c', { priority: 8, pinned: 'end' }), column('d', { priority: 7 })];
    const result = model({ columns, available: 250 });
    expect(result.columns.map((c) => c.id)).toContain('a');
    expect(result.columns.map((c) => c.id)).toContain('c');
    expect(result.fitHidden).toEqual(['b', 'd']);
  });

  it('recalcula el desplazamiento de las fijadas con los anchos ajustados', () => {
    const columns = [column('a', { pinned: 'start' }), column('b', { pinned: 'start' }), column('c'), column('d', { pinned: 'end' }), column('e', { pinned: 'end' })];
    const result = model({ columns, available: 470 });
    const byId = Object.fromEntries(result.columns.map((c) => [c.id, c]));
    expect(result.fitHidden).toEqual(['c']);
    expect(byId.a?.offset).toBe(0);
    expect(byId.b?.offset).toBe(byId.a?.width);
    expect(byId.e?.offset).toBe(0);
    expect(byId.d?.offset).toBe(byId.e?.width);
  });

  it('el ancho que fijó el usuario no se encoge', () => {
    const prefs = setWidth(defaultPrefs(wide), 'a', 300);
    const result = model({ prefs, available: 600 });
    expect(result.columns.find((c) => c.id === 'a')?.width).toBe(300);
    expect(result.totalWidth).toBeLessThanOrEqual(600);
  });

  it('con prefs.autoFit === false no se ajusta nada', () => {
    const prefs = setAutoFit(defaultPrefs(wide), false);
    expect(prefs.autoFit).toBe(false);
    const result = model({ prefs, available: 230 });
    expect(result.columns).toHaveLength(4);
    expect(result.fitHidden).toEqual([]);
    expect(result.totalWidth).toBe(640);
  });

  it('autoFit: por omisión activo (no se guarda); sólo se guarda desactivado y se recupera de localStorage', () => {
    const base = defaultPrefs(wide);
    expect('autoFit' in base).toBe(false);
    expect(setAutoFit(setAutoFit(base, false), true)).toEqual(base);
    const key = prefsKey('fit-test');
    savePrefs(key, setAutoFit(base, false));
    expect(loadPrefs(key, wide).autoFit).toBe(false);
    savePrefs(key, setAutoFit(base, true));
    expect(loadPrefs(key, wide).autoFit).toBeUndefined();
    localStorage.clear();
  });
});

describe('readField', () => {
  it('lee campos planos y con ruta', () => {
    expect(readField(rows[0] as Row, 'name')).toBe('Uno');
    expect(readField(rows[0] as Row, 'location.country')).toBe('ES');
    expect(readField(rows[1] as Row, 'location.country')).toBeUndefined();
    expect(readField(rows[0] as Row, 'name.x')).toBeUndefined();
  });
});

describe('preferencias de columnas', () => {
  afterEach(() => localStorage.clear());

  it('los valores por omisión salen de la configuración', () => {
    const prefs = defaultPrefs([column('a', { pinned: 'start' }), column('b', { hidden: true }), column('c', { pinned: 'end' })]);
    expect(prefs).toEqual({ order: ['a', 'b', 'c'], hidden: ['b'], pinned: { start: ['a'], end: ['c'] }, sizes: {} });
  });

  it('guarda y recupera por usuario y entidad, y se queda con lo que sigue existiendo', () => {
    const key = prefsKey('product', 'u1');
    expect(key).toBe('karpo.grid.u1.product');
    expect(prefsKey('product')).toBe('karpo.grid.anon.product');
    const prefs = setWidth(setPinned(setHidden(moveColumn(defaultPrefs(columns), 'kind', -1), 'name', true), 'sku', 'start'), 'sku', 200);
    savePrefs(key, prefs);
    expect(loadPrefs(key, columns)).toEqual(prefs);
    // una columna nueva aparece al final; una desaparecida se ignora
    const fewer = columns.filter((c) => c.field !== 'kind');
    const loaded = loadPrefs(key, [...fewer, column('extra')]);
    expect(loaded.order).not.toContain('kind');
    expect(loaded.order.at(-1)).toBe('extra');
    expect(loaded.hidden).toEqual(['price', 'name']);
  });

  it('un almacenamiento ilegible o vacío da los valores por omisión', () => {
    expect(loadPrefs('nada', columns)).toEqual(defaultPrefs(columns));
    localStorage.setItem('roto', '{no es json');
    expect(loadPrefs('roto', columns)).toEqual(defaultPrefs(columns));
    expect(mergePrefs(columns, null)).toEqual(defaultPrefs(columns));
    expect(mergePrefs(columns, { sizes: { sku: 'x', name: 9999, ghost: 100 }, hidden: 'no' }).sizes).toEqual({ name: MAX_COLUMN_WIDTH });
  });

  it('no falla si localStorage lanza', () => {
    const original = Storage.prototype.getItem;
    Storage.prototype.getItem = () => {
      throw new Error('bloqueado');
    };
    try {
      expect(loadPrefs('k', columns)).toEqual(defaultPrefs(columns));
    } finally {
      Storage.prototype.getItem = original;
    }
  });

  it('mueve, coloca, oculta, fija y redimensiona', () => {
    const base = defaultPrefs(columns);
    expect(moveColumn(base, 'sku', -1)).toBe(base);
    expect(moveColumn(base, 'zzz', 1)).toBe(base);
    expect(moveColumn(base, 'sku', 1).order.slice(0, 2)).toEqual(['name', 'sku']);
    expect(placeColumn(base, 'kind', 'sku').order[0]).toBe('kind');
    expect(placeColumn(base, 'sku', undefined).order.at(-1)).toBe('sku');
    expect(placeColumn(base, 'sku', 'sku')).toBe(base);
    expect(placeColumn(base, 'sku', 'nada').order.at(-1)).toBe('sku');
    const hidden = setHidden(base, 'sku', true);
    expect(hidden.hidden).toContain('sku');
    expect(setHidden(hidden, 'sku', false).hidden).not.toContain('sku');
    expect(setPinned(setPinned(base, 'sku', 'start'), 'sku', 'end').pinned).toEqual({ start: [], end: ['sku'] });
    expect(setPinned(setPinned(base, 'sku', 'end'), 'sku', false).pinned).toEqual({ start: [], end: [] });
    expect(setWidth(base, 'sku', 10).sizes.sku).toBe(MIN_COLUMN_WIDTH);
    expect(setWidth(setWidth(base, 'sku', 200), 'sku', undefined).sizes).toEqual({});
    expect(clampWidth(123.6)).toBe(124);
  });
});
