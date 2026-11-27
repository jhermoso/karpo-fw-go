// Adaptador fino sobre @tanstack/react-table 9 (PLAN §4.9, riesgo «TanStack Table 9 es mayor reciente»).
// Toda la dependencia de la API de TanStack vive aquí: el resto de la cuadrícula sólo ve `LayoutColumn`.
// El modelo aporta el orden, la visibilidad, el fijado, el tamaño y el estado de orden de las columnas y
// la lista de filas; el HTML, la virtualización y el teclado son propios.
import {
  columnOrderingFeature,
  columnPinningFeature,
  columnSizingFeature,
  columnVisibilityFeature,
  rowSortingFeature,
  tableFeatures,
  useTable,
  type ColumnDef as TableColumnDef,
} from '@tanstack/react-table';
import { useMemo } from 'react';
import type { ColumnDef, FieldKind, Row } from '../contracts';
import { fitColumns, type FitColumn } from './fit';
import { DEFAULT_COLUMN_WIDTH, MAX_COLUMN_WIDTH, MIN_COLUMN_WIDTH, type ColumnPrefs } from './prefs';
import type { SortState } from './url-state';

const features = tableFeatures({
  columnOrderingFeature,
  columnPinningFeature,
  columnSizingFeature,
  columnVisibilityFeature,
  rowSortingFeature,
});

type GridTableColumn = TableColumnDef<typeof features, Row>;

export interface LayoutColumn {
  /** Campo de la configuración. */
  id: string;
  def: ColumnDef;
  /** Ancho actual en píxeles (configuración o el fijado por el usuario). */
  width: number;
  pinned: false | 'start' | 'end';
  /** Desplazamiento desde el borde de su región (sólo las fijadas), en píxeles. */
  offset: number;
  /** Orden activo de esta columna. */
  sorted: false | 'asc' | 'desc';
  /** ¿El servidor admite ordenar por ella? */
  sortable: boolean;
  /** Posición entre las columnas de datos visibles (0 = la primera). */
  index: number;
}

export interface GridModel {
  /** Columnas visibles en orden de pantalla (fijadas al inicio primero, fijadas al final al último). */
  columns: LayoutColumn[];
  /** Suma de anchos de las visibles, en píxeles. */
  totalWidth: number;
  /** Número de filas del modelo (las que se pasaron). */
  rowCount: number;
  /**
   * Columnas que el ajuste al ancho ha ocultado (D-R34), en orden de pantalla. Es estado derivado: no toca
   * `prefs.hidden`, y `columns` ya no las incluye (así `aria-colcount` y las flechas no las alcanzan).
   */
  fitHidden: string[];
}

/** Valor de un campo, admitiendo rutas con punto (`location.country`). */
export function readField(row: Row, field: string): unknown {
  if (field in row) return row[field];
  if (!field.includes('.')) return undefined;
  let current: unknown = row;
  for (const part of field.split('.')) {
    if (typeof current !== 'object' || current === null) return undefined;
    current = (current as Record<string, unknown>)[part];
  }
  return current;
}

/**
 * Ancho mínimo por contenido según el tipo de campo (M-8): una insignia, una fecha o un importe no se cortan al
 * estrechar la columna (los textos sí: se acortan con puntos suspensivos). Las celdas son las de cada fila, que
 * comparten plantilla de columnas, así que el mínimo no se puede medir del contenido y se fija por tipo.
 */
export const CONTENT_MIN_WIDTH: Readonly<Partial<Record<FieldKind, number>>> = {
  badge: 96,
  date: 96,
  datetime: 136,
  money: 80,
  decimal: 80,
  number: 72,
  percentage: 72,
  boolean: 72,
};

export function minWidthOf(def: ColumnDef): number {
  return Math.max(MIN_COLUMN_WIDTH, CONTENT_MIN_WIDTH[def.kind] ?? 0);
}

function tableColumns(columns: readonly ColumnDef[]): GridTableColumn[] {
  return columns.map((def) => ({
    id: def.field,
    accessorFn: (row: Row) => readField(row, def.field),
    size: Math.max(def.widthPx ?? DEFAULT_COLUMN_WIDTH, minWidthOf(def)),
    minSize: minWidthOf(def),
    maxSize: MAX_COLUMN_WIDTH,
    enableSorting: Boolean(def.serverSortField),
    enablePinning: true,
  }));
}

/**
 * Aplica el ajuste al ancho a un diseño ya ordenado: quita las columnas ocultas, pone los anchos ajustados y
 * recalcula el desplazamiento de las fijadas (que depende de los anchos de sus vecinas fijadas).
 */
function applyFit(base: LayoutColumn[], prefs: ColumnPrefs, available: number): { columns: LayoutColumn[]; hidden: string[] } {
  const inputs: FitColumn[] = base.map((column, index) => ({
    id: column.id,
    width: column.width,
    minWidth: minWidthOf(column.def),
    // El ancho que fijó el usuario no se encoge; el de la configuración sí.
    sized: prefs.sizes[column.id] !== undefined,
    // Nunca se ocultan la primera columna ni las fijadas.
    locked: index === 0 || column.pinned !== false,
    ...(column.def.priority !== undefined ? { priority: column.def.priority } : {}),
  }));
  const fit = fitColumns(inputs, available);
  if (fit.hidden.length === 0 && base.every((column) => fit.widths[column.id] === column.width)) {
    return { columns: base, hidden: [] };
  }
  const shown = base
    .filter((column) => fit.widths[column.id] !== undefined)
    .map((column, index) => ({ ...column, width: fit.widths[column.id] ?? column.width, index }));
  let before = 0;
  for (const column of shown) {
    if (column.pinned === 'start') {
      column.offset = before;
      before += column.width;
    }
  }
  let after = 0;
  for (let index = shown.length - 1; index >= 0; index -= 1) {
    const column = shown[index];
    if (column?.pinned === 'end') {
      column.offset = after;
      after += column.width;
    }
  }
  return { columns: shown, hidden: fit.hidden };
}

/**
 * Modelo de la cuadrícula: columnas con su disposición y filas. `rows` debe ser estable entre renders.
 * `available` es el ancho en píxeles para las columnas de datos (sin la de selección ni la de acciones); sin él
 * (aún no medido) o con `prefs.autoFit === false` no se ajusta nada.
 */
export function useGridModel(args: {
  columns: readonly ColumnDef[];
  rows: readonly Row[];
  prefs: ColumnPrefs;
  sort?: SortState;
  available?: number;
}): GridModel {
  const { columns, rows, prefs, sort, available } = args;
  const defs = useMemo(() => tableColumns(columns), [columns]);
  const byId = useMemo(() => new Map(columns.map((column) => [column.field, column])), [columns]);

  const state = useMemo(
    () => ({
      columnOrder: prefs.order,
      columnVisibility: Object.fromEntries(prefs.hidden.map((field) => [field, false])),
      columnPinning: { start: prefs.pinned.start, end: prefs.pinned.end },
      columnSizing: prefs.sizes,
      sorting: sort ? [{ id: sort.field, desc: sort.direction === 'desc' }] : [],
    }),
    [prefs, sort],
  );

  const table = useTable({
    features,
    columns: defs,
    data: rows as Row[],
    getRowId: (row: Row) => row.id,
    manualSorting: true,
    state,
  });

  // El diseño de las columnas sólo cambia con las preferencias, el orden o la configuración: se memoiza
  // para que las filas (memoizadas) no se repinten con cada cambio de datos.
  const layout = useMemo(() => {
    // El orden de pantalla pone las fijadas al inicio primero y las fijadas al final al último.
    const visible = table.getVisibleLeafColumns();
    const start = prefs.pinned.start.flatMap((id) => visible.filter((leaf) => leaf.id === id));
    const end = prefs.pinned.end.flatMap((id) => visible.filter((leaf) => leaf.id === id));
    const centre = visible.filter((leaf) => !start.includes(leaf) && !end.includes(leaf));
    const result: LayoutColumn[] = [];
    let index = 0;
    for (const leaf of [...start, ...centre, ...end]) {
      const def = byId.get(leaf.id);
      if (!def) continue;
      const pinned = leaf.getIsPinned();
      const sorted = leaf.getIsSorted();
      result.push({
        id: leaf.id,
        def,
        width: leaf.getSize(),
        pinned,
        offset: pinned === 'start' ? leaf.getStart('start') : pinned === 'end' ? leaf.getAfter('end') : 0,
        sorted: sorted === 'asc' || sorted === 'desc' ? sorted : false,
        sortable: Boolean(def.serverSortField),
        index: index++,
      });
    }
    return result;
    // `table` es estable y lee el estado de `state`: basta con las preferencias, el orden y las columnas.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state, byId]);
  const autoFit = prefs.autoFit !== false;
  const fitted = useMemo(
    () => (autoFit && available !== undefined ? applyFit(layout, prefs, available) : { columns: layout, hidden: [] as string[] }),
    [layout, prefs, autoFit, available],
  );
  return {
    columns: fitted.columns,
    totalWidth: fitted.columns.reduce((sum, column) => sum + column.width, 0),
    rowCount: table.getRowModel().rows.length,
    fitHidden: fitted.hidden,
  };
}
