// Pegado desde una hoja de cálculo (TSV): plan de pegado (qué celda recibe qué y cuáles fallan), copia y
// relleno hacia abajo. Sin React; la coerción por tipo es la del registro de campos (`parsePaste`).
import type { ColumnDef } from '../contracts';
import { parsePaste, parseTsv, toTsv } from '../fields';
import type { CellEdit } from './store';

export interface PasteTarget {
  def: ColumnDef;
  /** ¿Se puede escribir en esta columna en la fila dada? */
  editable(rowId: string): boolean;
}

export interface PlanCell {
  rowId: string;
  field: string;
  text: string;
  status: 'ok' | 'error' | 'skipped';
  /** Código de error (`required`, `format`, `max`…) o `readonly` si se salta. */
  code?: string;
  value?: unknown;
}

export interface PastePlan {
  /** Filas del pegado que caben (desde la fila activa hacia abajo). */
  rows: PlanCell[][];
  /** Columnas destino (las que reciben datos). */
  columns: ColumnDef[];
  ok: number;
  errors: number;
  skipped: number;
  /** Filas del texto que no caben porque no hay más filas cargadas. */
  droppedRows: number;
  /** Columnas del texto que sobran a la derecha. */
  droppedColumns: number;
}

/** ¿Es un pegado de una sola celda (un valor sin tabuladores ni saltos de línea)? */
export function isSingleValue(text: string): boolean {
  const matrix = parseTsv(text);
  return matrix.length === 1 && (matrix[0]?.length ?? 0) === 1;
}

/**
 * Plan de un pegado. `targets` son las columnas a partir de la activa, en orden de pantalla; `rowIds`, las
 * filas a partir de la activa. Lo que se sale por la derecha o por abajo se descarta y se cuenta.
 */
export function planPaste(text: string, targets: readonly PasteTarget[], rowIds: readonly string[], locale: string): PastePlan {
  const matrix = parseTsv(text);
  const width = matrix.reduce((max, cells) => Math.max(max, cells.length), 0);
  const used = targets.slice(0, width);
  const parsed = parsePaste(text, used.map((target) => target.def), locale);
  let ok = 0;
  let errors = 0;
  let skipped = 0;
  const rows = parsed.rows.slice(0, rowIds.length).map((cells, rowIndex) => {
    const rowId = rowIds[rowIndex] as string;
    return cells.map((cell, columnIndex): PlanCell => {
      const target = used[columnIndex] as PasteTarget;
      const base = { rowId, field: target.def.field, text: cell.text };
      if (!target.editable(rowId)) {
        skipped += 1;
        return { ...base, status: 'skipped', code: 'readonly' };
      }
      if (!cell.ok) {
        errors += 1;
        return { ...base, status: 'error', code: cell.code };
      }
      ok += 1;
      return { ...base, status: 'ok', value: cell.value };
    });
  });
  return {
    rows,
    columns: used.map((target) => target.def),
    ok,
    errors,
    skipped,
    droppedRows: Math.max(0, parsed.rows.length - rowIds.length),
    droppedColumns: Math.max(0, width - used.length),
  };
}

/** Las ediciones que aplica un plan: sólo las celdas correctas. */
export function editsOfPlan(plan: PastePlan): CellEdit[] {
  return plan.rows.flat().filter((cell) => cell.status === 'ok').map((cell) => ({ rowId: cell.rowId, field: cell.field, value: cell.value }));
}

/** Copia: valores en su forma editable (se pueden volver a pegar), tabulados. */
export function copyText(values: readonly (readonly unknown[])[], defs: readonly ColumnDef[], locale: string): string {
  return toTsv(values, defs, locale);
}

/** Relleno hacia abajo: el valor de la fila de arriba se copia en cada fila del rango, por columna. */
export function fillEdits(
  rowIds: readonly string[],
  fields: readonly string[],
  valueOf: (rowId: string, field: string) => unknown,
): CellEdit[] {
  const [first, ...rest] = rowIds;
  if (first === undefined) return [];
  const edits: CellEdit[] = [];
  for (const field of fields) {
    const value = valueOf(first, field);
    for (const rowId of rest) edits.push({ rowId, field, value });
  }
  return edits;
}
