// Plan de pegado, relleno y copia: coerción por tipo, celdas saltadas y descartadas.
import { describe, expect, it } from 'vitest';
import type { ColumnDef } from '../contracts';
import { copyText, editsOfPlan, fillEdits, isSingleValue, planPaste, type PasteTarget } from './paste';

function column(field: string, kind: ColumnDef['kind'], extra: Partial<ColumnDef> = {}): ColumnDef {
  return { field, label: { literal: field }, kind, required: false, editable: true, sortable: false, ...extra };
}

const targets = (...defs: [ColumnDef, boolean?][]): PasteTarget[] =>
  defs.map(([def, editable = true]) => ({ def, editable: () => editable }));

const name = column('name', 'text', { required: true });
const price = column('price', 'number', { min: 0 });
const when = column('when', 'date');

describe('isSingleValue', () => {
  it('distingue un valor de una matriz', () => {
    expect(isSingleValue('hola')).toBe(true);
    expect(isSingleValue('a\tb')).toBe(false);
    expect(isSingleValue('a\nb')).toBe(false);
    expect(isSingleValue('"a\tb"')).toBe(true); // comillas de Excel: una celda con un tabulador dentro
  });
});

describe('planPaste', () => {
  it('analiza cada celda con la coerción de su tipo y cuenta correctas, errores y saltadas', () => {
    const plan = planPaste('Uno\t10,5\t2026-11-27\nDos\tx\t31/12/2026', targets([name], [price], [when, false]), ['r1', 'r2'], 'es');
    expect(plan.ok).toBe(3);
    expect(plan.errors).toBe(1);
    expect(plan.skipped).toBe(2);
    expect(plan.columns.map((c) => c.field)).toEqual(['name', 'price', 'when']);
    expect(plan.rows[0]?.map((cell) => [cell.rowId, cell.field, cell.status])).toEqual([
      ['r1', 'name', 'ok'],
      ['r1', 'price', 'ok'],
      ['r1', 'when', 'skipped'],
    ]);
    expect(plan.rows[0]?.[1]?.value).toBe(10.5);
    expect(plan.rows[1]?.[1]).toMatchObject({ status: 'error', code: 'format', text: 'x' });
    expect(plan.rows[0]?.[2]).toMatchObject({ status: 'skipped', code: 'readonly' });
  });

  it('un valor fuera de rango es un error con su código', () => {
    const plan = planPaste('-5', targets([price]), ['r1'], 'es');
    expect(plan.rows[0]?.[0]).toMatchObject({ status: 'error', code: 'min' });
  });

  it('descarta lo que no cabe por abajo y por la derecha y lo cuenta', () => {
    const plan = planPaste('a\tb\tc\nd\te\tf\ng\th\ti', targets([name], [price]), ['r1', 'r2'], 'es');
    expect(plan.rows).toHaveLength(2);
    expect(plan.rows[0]).toHaveLength(2);
    expect(plan.droppedRows).toBe(1);
    expect(plan.droppedColumns).toBe(1);
  });

  it('un texto vacío en una columna obligatoria es un error; en una opcional, un valor vacío', () => {
    const plan = planPaste('\t', targets([name], [price]), ['r1'], 'es');
    expect(plan.rows[0]?.[0]).toMatchObject({ status: 'error', code: 'required' });
    expect(plan.rows[0]?.[1]).toMatchObject({ status: 'ok', value: null });
  });
});

describe('editsOfPlan', () => {
  it('sólo se aplican las celdas correctas', () => {
    const plan = planPaste('Uno\tx\nDos\t3', targets([name], [price]), ['r1', 'r2'], 'es');
    expect(editsOfPlan(plan)).toEqual([
      { rowId: 'r1', field: 'name', value: 'Uno' },
      { rowId: 'r2', field: 'name', value: 'Dos' },
      { rowId: 'r2', field: 'price', value: 3 },
    ]);
  });
});

describe('fillEdits', () => {
  it('copia el valor de la primera fila en el resto, por columna', () => {
    const values: Record<string, Record<string, unknown>> = { r1: { a: 1, b: 'x' }, r2: { a: 2, b: 'y' }, r3: { a: 3, b: 'z' } };
    expect(fillEdits(['r1', 'r2', 'r3'], ['a', 'b'], (id, field) => values[id]?.[field])).toEqual([
      { rowId: 'r2', field: 'a', value: 1 },
      { rowId: 'r3', field: 'a', value: 1 },
      { rowId: 'r2', field: 'b', value: 'x' },
      { rowId: 'r3', field: 'b', value: 'x' },
    ]);
    expect(fillEdits([], ['a'], () => 1)).toEqual([]);
    expect(fillEdits(['r1'], ['a'], () => 1)).toEqual([]);
  });
});

describe('copyText', () => {
  it('copia en forma editable, para poder volver a pegar', () => {
    const text = copyText([['Uno', 10.5, '2026-11-27']], [name, price, when], 'es');
    expect(text).toBe('Uno\t10,5\t27/11/2026');
    const back = planPaste(text, targets([name], [price], [when]), ['r1'], 'es');
    expect(back.errors).toBe(0);
    expect(back.rows[0]?.map((cell) => cell.value)).toEqual(['Uno', 10.5, '2026-11-27']);
  });
});
