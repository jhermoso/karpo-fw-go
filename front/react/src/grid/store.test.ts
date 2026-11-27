// Almacén de edición: capas servidor → borrador → pendiente, deshacer/rehacer con parches, 409.
import { beforeEach, describe, expect, it } from 'vitest';
import type { FieldKind, Row } from '../contracts';
import {
  buildChange,
  countDirtyCells,
  createGridStore,
  dirtyFields,
  dirtyRowIds,
  effectiveRow,
  effectiveValue,
  getGridStore,
  hasBlockingIssues,
  isRowDirty,
  resetGridStores,
  type GridStore,
} from './store';
import { emptyValueFor, valuesEqual } from './store-values';

const kinds: Record<string, FieldKind> = { name: 'text', price: 'money', qty: 'number', tags: 'multiselect', when: 'datetime' };

function validate(field: string, value: unknown): string[] {
  if (field === 'name') return value === '' || value === null || value === undefined ? ['required'] : [];
  if (field === 'qty' && typeof value === 'string') return ['format'];
  return [];
}

function row(id: string, extra: Record<string, unknown> = {}): Row {
  return { id, name: `Fila ${id}`, price: '10.00', qty: 1, version: 1, ...extra } as Row;
}

function make(): GridStore {
  const store = createGridStore({ kinds, validate });
  store.getState().syncServer([row('a'), row('b'), row('c')]);
  return store;
}

describe('valores de celda', () => {
  it('compara decimales, números y fechas por valor', () => {
    expect(valuesEqual('10', '10.00', 'money')).toBe(true);
    expect(valuesEqual('10.5', '10.50', 'decimal')).toBe(true);
    expect(valuesEqual('10.5', '10.51', 'percentage')).toBe(false);
    expect(valuesEqual('3', 3, 'number')).toBe(true);
    expect(valuesEqual('2026-11-27T09:00:00Z', '2026-11-27T10:00:00+01:00', 'datetime')).toBe(true);
    expect(valuesEqual(null, '', 'text')).toBe(true);
    expect(valuesEqual([], null)).toBe(true);
    expect(valuesEqual('x', null)).toBe(false);
    expect(valuesEqual(['a', 'b'], ['a', 'b'])).toBe(true);
  });

  it('da el valor vacío de cada tipo', () => {
    expect(emptyValueFor('text')).toBe('');
    expect(emptyValueFor('email')).toBe('');
    expect(emptyValueFor('multiselect')).toEqual([]);
    expect(emptyValueFor('boolean')).toBe(false);
    expect(emptyValueFor('date')).toBeNull();
    expect(emptyValueFor(undefined)).toBeNull();
  });
});

describe('capas: servidor, borrador y pendiente', () => {
  let store: GridStore;
  beforeEach(() => {
    store = make();
  });

  it('el valor efectivo sale del servidor hasta que hay un cambio pendiente', () => {
    const state = store.getState();
    expect(effectiveValue(state, 'a', 'name')).toBe('Fila a');
    expect(effectiveRow(state, 'a')?.name).toBe('Fila a');
    expect(effectiveRow(state, 'zzz')).toBeUndefined();
    expect(isRowDirty(state, 'a', kinds)).toBe(false);
  });

  it('el borrador no cuenta como cambio hasta confirmarlo', () => {
    store.getState().beginEdit('a', 'name');
    expect(store.getState().editing).toMatchObject({ rowId: 'a', field: 'name', draft: 'Fila a', original: 'Fila a' });
    store.getState().setDraft('Nuevo');
    expect(effectiveValue(store.getState(), 'a', 'name')).toBe('Fila a');
    const result = store.getState().commitEdit();
    expect(result).toEqual({ changed: true, issue: undefined, cell: { rowId: 'a', field: 'name' } });
    expect(store.getState().editing).toBeNull();
    expect(effectiveValue(store.getState(), 'a', 'name')).toBe('Nuevo');
    expect(dirtyFields(store.getState(), 'a', kinds)).toEqual(['name']);
    expect(dirtyRowIds(store.getState(), kinds)).toEqual(['a']);
    expect(countDirtyCells(store.getState(), kinds)).toBe(1);
  });

  it('abrir escribiendo encima parte del texto tecleado', () => {
    store.getState().beginEdit('a', 'name', { initial: 'Z', overwrite: true });
    expect(store.getState().editing).toMatchObject({ draft: 'Z', original: 'Fila a', overwrite: true });
  });

  it('confirmar sin cambios no deja rastro; cancelar descarta el borrador', () => {
    store.getState().beginEdit('a', 'name');
    expect(store.getState().commitEdit()).toEqual({ changed: false, cell: { rowId: 'a', field: 'name' } });
    expect(store.getState().rows).toEqual({});
    expect(store.getState().commitEdit()).toEqual({ changed: false });
    store.getState().beginEdit('a', 'name');
    store.getState().setDraft('Otro');
    store.getState().cancelEdit();
    expect(store.getState().editing).toBeNull();
    expect(store.getState().rows).toEqual({});
    store.getState().setDraft('ignorado');
    expect(store.getState().editing).toBeNull();
  });

  it('un valor inválido se guarda como pendiente con su incidencia', () => {
    store.getState().beginEdit('a', 'name');
    store.getState().setDraft('');
    const result = store.getState().commitEdit();
    expect(result.issue).toBe('required');
    expect(store.getState().issues.a?.name).toEqual({ code: 'required', source: 'local' });
    expect(hasBlockingIssues(store.getState(), 'a')).toBe(true);
    // al corregirlo, la incidencia desaparece
    store.getState().setValue('a', 'name', 'Bien');
    expect(store.getState().issues.a).toBeUndefined();
    expect(hasBlockingIssues(store.getState(), 'a')).toBe(false);
  });

  it('un cambio que vuelve al valor del servidor deja la fila limpia', () => {
    store.getState().setValue('a', 'price', '12.00');
    expect(isRowDirty(store.getState(), 'a', kinds)).toBe(true);
    store.getState().setValue('a', 'price', '10');
    expect(isRowDirty(store.getState(), 'a', kinds)).toBe(false);
    expect(buildChange(store.getState(), 'a', kinds)).toBeUndefined();
    expect(buildChange(store.getState(), 'nada', kinds)).toBeUndefined();
  });

  it('el cuerpo del PUT lleva sólo los campos sucios y la versión leída', () => {
    store.getState().setValue('a', 'name', 'Uno');
    store.getState().setValue('a', 'price', '10.00'); // igual al servidor: no viaja
    expect(buildChange(store.getState(), 'a', kinds)).toEqual({
      patch: { name: 'Uno' },
      version: 1,
      isNew: false,
    });
  });

  it('syncServer alinea lo ya liquidado y respeta lo sucio', () => {
    store.getState().setValue('a', 'name', 'Mío');
    store.getState().setValue('b', 'price', '10.00'); // sin cambio real
    store.getState().syncServer([row('a', { name: 'Del servidor', version: 2 }), row('b', { price: '11.00', version: 2 })]);
    expect(effectiveValue(store.getState(), 'a', 'name')).toBe('Mío');
    expect(store.getState().rows.a?.baseVersion).toBe(1);
    expect(effectiveValue(store.getState(), 'b', 'price')).toBe('11.00');
    expect(store.getState().rows.b?.baseVersion).toBe(2);
  });
});

describe('deshacer y rehacer', () => {
  let store: GridStore;
  beforeEach(() => {
    store = make();
  });

  it('deshace y rehace un cambio y su validación', () => {
    store.getState().setValue('a', 'name', '');
    expect(store.getState().issues.a?.name?.code).toBe('required');
    const undone = store.getState().undo();
    expect(undone).toMatchObject({ kind: 'edit', rowIds: ['a'], cells: [{ rowId: 'a', field: 'name' }] });
    expect(effectiveValue(store.getState(), 'a', 'name')).toBe('Fila a');
    expect(store.getState().issues.a).toBeUndefined();
    expect(store.getState().undo()).toBeNull();
    const redone = store.getState().redo();
    expect(redone?.rowIds).toEqual(['a']);
    expect(effectiveValue(store.getState(), 'a', 'name')).toBe('');
    expect(store.getState().issues.a?.name?.code).toBe('required');
    expect(store.getState().redo()).toBeNull();
  });

  it('un cambio nuevo vacía la pila de rehacer', () => {
    store.getState().setValue('a', 'name', 'Uno');
    store.getState().undo();
    expect(store.getState().redoStack).toHaveLength(1);
    store.getState().setValue('a', 'name', 'Dos');
    expect(store.getState().redoStack).toHaveLength(0);
  });

  it('un pegado o relleno es un solo paso de deshacer', () => {
    const result = store.getState().applyEdits(
      [
        { rowId: 'a', field: 'name', value: 'X' },
        { rowId: 'b', field: 'name', value: 'Y' },
        { rowId: 'c', field: 'name', value: 'Z' },
      ],
      'paste',
    );
    expect(result?.kind).toBe('paste');
    expect(result?.rowIds).toEqual(['a', 'b', 'c']);
    expect(store.getState().undoStack).toHaveLength(1);
    store.getState().undo();
    expect(dirtyRowIds(store.getState(), kinds)).toEqual([]);
    store.getState().redo();
    expect(dirtyRowIds(store.getState(), kinds)).toEqual(['a', 'b', 'c']);
  });

  it('deshacer algo ya guardado deja la celda sucia: pide otro PUT', () => {
    store.getState().setValue('a', 'name', 'Guardado');
    store.getState().settle('a', row('a', { name: 'Guardado', version: 2 }));
    expect(isRowDirty(store.getState(), 'a', kinds)).toBe(false);
    expect(store.getState().status.a).toEqual({ phase: 'saved' });
    expect(store.getState().rows.a?.baseVersion).toBe(2);

    const undone = store.getState().undo();
    expect(undone?.rowIds).toEqual(['a']);
    expect(effectiveValue(store.getState(), 'a', 'name')).toBe('Fila a');
    expect(isRowDirty(store.getState(), 'a', kinds)).toBe(true);
    expect(buildChange(store.getState(), 'a', kinds)).toEqual({ patch: { name: 'Fila a' }, version: 2, isNew: false });
    // y rehacer vuelve al valor guardado: otra vez limpia
    store.getState().redo();
    expect(isRowDirty(store.getState(), 'a', kinds)).toBe(false);
  });

  it('un nuevo cambio borra el estado «guardada» de la fila', () => {
    store.getState().setValue('a', 'name', 'Uno');
    store.getState().settle('a', row('a', { name: 'Uno', version: 2 }));
    store.getState().setValue('a', 'name', 'Dos');
    expect(store.getState().status.a).toBeUndefined();
  });

  it('limita la historia', () => {
    for (let i = 0; i < 230; i += 1) store.getState().setValue('a', 'qty', i + 2);
    expect(store.getState().undoStack.length).toBe(200);
  });

  it('descarta un paso cuya ruta ya no existe', () => {
    const id = store.getState().addRow({ name: 'Nuevo' });
    store.getState().setValue(id, 'name', 'Editado');
    store.getState().setValue('a', 'name', 'Otro');
    store.getState().discardRow(id);
    // la historia de la fila descartada se limpió: queda sólo el cambio de «a»
    expect(store.getState().undoStack).toHaveLength(1);
    expect(store.getState().undo()?.rowIds).toEqual(['a']);
    expect(store.getState().undo()).toBeNull();
  });
});

describe('vaciar y revertir', () => {
  let store: GridStore;
  beforeEach(() => {
    store = make();
  });

  it('vacía con el valor vacío del tipo', () => {
    store.getState().clearCell('a', 'name');
    expect(effectiveValue(store.getState(), 'a', 'name')).toBe('');
    store.getState().clearCell('a', 'qty');
    expect(effectiveValue(store.getState(), 'a', 'qty')).toBeNull();
    store.getState().clearCell('a', 'tags');
    expect(effectiveValue(store.getState(), 'a', 'tags')).toEqual([]);
  });

  it('revierte una celda, una fila y todo, y cada reversión se puede deshacer', () => {
    store.getState().setValue('a', 'name', 'Uno');
    store.getState().setValue('a', 'qty', 5);
    store.getState().setValue('b', 'name', 'Dos');
    store.getState().revertCell('a', 'name');
    expect(effectiveValue(store.getState(), 'a', 'name')).toBe('Fila a');
    expect(effectiveValue(store.getState(), 'a', 'qty')).toBe(5);
    store.getState().revertCell('a', 'name'); // ya limpia: no hace nada
    expect(store.getState().undoStack).toHaveLength(4);

    store.getState().revertRow('a');
    expect(isRowDirty(store.getState(), 'a', kinds)).toBe(false);
    expect(isRowDirty(store.getState(), 'b', kinds)).toBe(true);

    store.getState().revertAll();
    expect(dirtyRowIds(store.getState(), kinds)).toEqual([]);

    store.getState().undo(); // deshace «revertAll»
    expect(dirtyRowIds(store.getState(), kinds)).toEqual(['b']);
  });

  it('revertir una fila nueva la descarta', () => {
    const id = store.getState().addRow({ name: 'Provisional' });
    store.getState().revertRow(id);
    expect(store.getState().rows[id]).toBeUndefined();
    expect(store.getState().newRows).toEqual([]);
    store.getState().revertCell(id, 'name'); // no hay nada: no falla
  });
});

describe('filas nuevas', () => {
  it('se añaden, se validan por campos y se descartan', () => {
    const store = make();
    const first = store.getState().addRow({ name: '' });
    const second = store.getState().addRow({ name: 'Con nombre' });
    expect(store.getState().newRows).toEqual([second, first]);
    expect(isRowDirty(store.getState(), first, kinds)).toBe(true);
    expect(dirtyFields(store.getState(), second, kinds)).toEqual(['name']);
    expect(store.getState().validateRow(first, ['name'])).toBe(false);
    expect(store.getState().issues[first]?.name?.code).toBe('required');
    expect(store.getState().validateRow(second, ['name'])).toBe(true);
    expect(buildChange(store.getState(), second, kinds)).toMatchObject({ isNew: true, patch: { name: 'Con nombre' } });

    store.getState().toggleSelected(first, true);
    store.getState().beginEdit(first, 'name');
    store.getState().discardRow(first);
    expect(store.getState().editing).toBeNull();
    expect(store.getState().selected[first]).toBeUndefined();
    expect(store.getState().issues[first]).toBeUndefined();
    expect(store.getState().newRows).toEqual([second]);
  });

  it('revertAll descarta las nuevas', () => {
    const store = make();
    store.getState().addRow({ name: 'x' });
    store.getState().revertAll();
    expect(store.getState().newRows).toEqual([]);
  });
});

describe('estado de guardado e incidencias', () => {
  it('lleva el estado de la fila y las incidencias del servidor, que se retiran al reescribir la celda', () => {
    const store = make();
    store.getState().setValue('a', 'name', 'Uno');
    store.getState().setStatus('a', { phase: 'saving' });
    expect(store.getState().status.a?.phase).toBe('saving');
    store.getState().setStatus('a', { phase: 'error', message: 'boom' });
    store.getState().setIssues('a', { name: { code: 'duplicate', source: 'server', message: 'ya existe' } });
    expect(hasBlockingIssues(store.getState(), 'a')).toBe(true);
    // editar otra celda no la toca; escribir la misma sí
    store.getState().setValue('a', 'qty', 3);
    expect(store.getState().issues.a?.name?.source).toBe('server');
    store.getState().setValue('a', 'name', 'Dos');
    expect(store.getState().issues.a).toBeUndefined();
    expect(store.getState().status.a).toBeUndefined();
    store.getState().setStatus('a', null);
    store.getState().setIssues('a', { name: { code: 'x', source: 'server' } });
    store.getState().clearIssues('a');
    expect(store.getState().issues.a).toBeUndefined();
  });

  it('una fila en conflicto conserva su estado al seguir editando', () => {
    const store = make();
    store.getState().setValue('a', 'name', 'Uno');
    store.getState().setStatus('a', { phase: 'conflict' });
    store.getState().setValue('a', 'qty', 9);
    expect(store.getState().status.a?.phase).toBe('conflict');
  });
});

describe('conflicto 409: recargar y reaplicar', () => {
  it('sin campos en común, mis cambios se reaplican sobre la fila fresca', () => {
    const store = make();
    store.getState().setValue('a', 'name', 'Mío');
    store.getState().setStatus('a', { phase: 'conflict' });
    const fresh = row('a', { price: '99.00', version: 3 });
    const preview = store.getState().previewRebase('a', fresh);
    expect(preview).toEqual({ mine: ['name'], theirs: ['price'], common: [] });
    store.getState().applyRebase('a', fresh, []);
    expect(effectiveValue(store.getState(), 'a', 'name')).toBe('Mío');
    expect(effectiveValue(store.getState(), 'a', 'price')).toBe('99.00');
    expect(store.getState().rows.a?.baseVersion).toBe(3);
    expect(store.getState().status.a).toBeUndefined();
    expect(buildChange(store.getState(), 'a', kinds)).toEqual({ patch: { name: 'Mío' }, version: 3, isNew: false });
  });

  it('con campos en común hay que elegir: los míos o los del servidor', () => {
    const store = make();
    store.getState().setValue('a', 'name', 'Mío');
    store.getState().setValue('a', 'qty', 7);
    const fresh = row('a', { name: 'De otro', version: 4 });
    const preview = store.getState().previewRebase('a', fresh);
    expect(preview.common).toEqual(['name']);

    store.getState().applyRebase('a', fresh, ['name']);
    expect(effectiveValue(store.getState(), 'a', 'name')).toBe('De otro');
    expect(effectiveValue(store.getState(), 'a', 'qty')).toBe(7);
    expect(dirtyFields(store.getState(), 'a', kinds)).toEqual(['qty']);

    const other = make();
    other.getState().setValue('a', 'name', 'Mío');
    other.getState().applyRebase('a', fresh, []);
    expect(effectiveValue(other.getState(), 'a', 'name')).toBe('Mío');
    expect(dirtyFields(other.getState(), 'a', kinds)).toEqual(['name']);
  });
});

describe('selección y registro', () => {
  it('marca, alterna y borra la selección', () => {
    const store = make();
    store.getState().toggleSelected('a');
    store.getState().toggleSelected('b', true);
    expect(Object.keys(store.getState().selected)).toEqual(['a', 'b']);
    store.getState().toggleSelected('a');
    store.getState().selectMany(['b', 'c'], false);
    expect(store.getState().selected).toEqual({});
    store.getState().selectMany(['a', 'c'], true);
    expect(Object.keys(store.getState().selected)).toEqual(['a', 'c']);
    store.getState().clearSelection();
    expect(store.getState().selected).toEqual({});
  });

  it('un almacén por entidad, hasta que se reinicia el registro', () => {
    resetGridStores();
    const one = getGridStore('product');
    expect(getGridStore('product')).toBe(one);
    expect(getGridStore('facility')).not.toBe(one);
    resetGridStores();
    expect(getGridStore('product')).not.toBe(one);
  });

  it('configure cambia el esquema de validación', () => {
    const store = createGridStore();
    store.getState().syncServer([row('a')]);
    store.getState().setValue('a', 'name', '');
    expect(store.getState().issues.a).toBeUndefined();
    store.getState().configure({ kinds, validate });
    store.getState().setValue('a', 'name', 'x');
    store.getState().setValue('a', 'name', '');
    expect(store.getState().issues.a?.name?.code).toBe('required');
  });
});
