// Tipos y constantes compartidos por los componentes de la cuadrícula.
import type { ColumnDef, CommandDef, EntityConfig, EntityViewProps, FieldKind, Row, Text } from '../contracts';
import type { EntityConfigExt } from '../config';
import type { GridTranslate } from './i18n';
import type { Position, Range } from './keyboard';
import type { CellIssue, GridState, GridStore } from './store';

/** Columna de selección (casilla), siempre la primera si la entidad es seleccionable. */
export const SELECT_COL = '__select';
/** Columna de acciones de fila, siempre la última si hay órdenes de fila. */
export const ACTIONS_COL = '__actions';

export interface GridViewProps extends EntityViewProps {
  /** Almacén de edición propio (pruebas); por omisión, uno por entidad compartido en el registro. */
  store?: GridStore;
  /** Filas que se pintan fuera de la ventana (pruebas). */
  overscan?: number;
}

/** Lo que los componentes de la cuadrícula necesitan de `GridView` (evita pasar veinte propiedades). */
export interface GridEnv {
  config: EntityConfig;
  ext: EntityConfigExt;
  store: GridStore;
  /** ¿Se edita en línea (`saveMode` distinto de `form`, entidad editable y permiso de escritura)? */
  editable: boolean;
  saveMode: EntityConfig['saveMode'];
  /** Tipo de cada campo (comparar valores y vaciar celdas). */
  kinds: Record<string, FieldKind>;
  locale: string;
  t: GridTranslate;
  /** Texto de la configuración en el idioma activo. */
  text(value: Text | undefined): string;
  /** Mensaje traducido de una incidencia de celda. */
  issueMessage(issue: CellIssue, def: ColumnDef): string;
  /** Etiqueta de una fila para lectores de pantalla (su `displayField`). */
  rowLabel(row: Row | undefined): string;
  /** Tipo de campo → ¿es editable esta celda? */
  cellEditable(def: ColumnDef, rowId: string): boolean;
}

export interface TableHandlers {
  onCellFocus(rowId: string | null, colId: string): void;
  onCellMouseDown(event: { shiftKey: boolean }, rowId: string, colId: string): void;
  onSort(field: string): void;
  onResize(field: string, width: number | undefined): void;
  onReorder(field: string, before: string | undefined): void;
  onToggleSelected(rowId: string, shiftKey: boolean): void;
  onToggleAll(selected: boolean): void;
  onOpenRow(rowId: string): void;
  onCommand(command: CommandDef, row: Row): void;
  onEditorChange(value: unknown): void;
  onEditorCommit(move?: 'next' | 'prev' | 'down' | 'up'): void;
  onEditorCancel(): void;
  onLoadMore(): void;
}

export interface TableView {
  state: GridState;
  /** Identificadores de las filas visibles, las nuevas primero. */
  rowIds: string[];
  /** Fila de servidor por identificador (para filas que aún no están en el almacén). */
  serverById: Map<string, Row>;
  hasSelect: boolean;
  hasActions: boolean;
  /** Columnas navegables en orden: selección, datos, acciones. */
  navCols: string[];
  active: Position;
  range: Range | null;
  total: number;
  loading: boolean;
  fetchingMore: boolean;
  hasMore: boolean;
  rowHeight: number;
  /** Órdenes de fila que existen para alguna fila (decide si hay columna de acciones). */
  rowCommands: CommandDef[];
  /** Las visibles para una fila concreta (`visibleWhen`). */
  commandsForRow(row: Row): CommandDef[];
  overscan: number;
}
