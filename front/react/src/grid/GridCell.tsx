// Celda de datos de la cuadrícula: visor, editor en línea y estado visual (`data-kx-cell`).
//
// `data-kx-cell` lleva UN valor, por precedencia: editing > invalid > error > saving > dirty > active >
// readonly. La celda activa lleva además `data-kx-active`, de modo que una celda inválida y activa sigue
// pintando el anillo del foco. Sin `outline: none` en ningún sitio: el foco lo pinta la regla global del tema.
//
// Recibe sólo lo suyo (no el almacén entero) para que `GridRow` pueda memoizarse: teclear en un editor
// repinta una fila, no toda la ventana.
import { memo } from 'react';
import { FieldEditor, FieldView, getFieldType } from '../fields';
import type { Row } from '../contracts';
import { readField, type LayoutColumn } from './model';
import type { CellIssue, EditingCell } from './store';
import type { GridEnv, TableHandlers } from './types';

export type CellState = 'editing' | 'invalid' | 'error' | 'saving' | 'dirty' | 'active' | 'readonly';

export function cellStateOf(args: {
  editing: boolean;
  issue: CellIssue | undefined;
  saving: boolean;
  dirty: boolean;
  active: boolean;
  readonly: boolean;
}): CellState | undefined {
  if (args.editing) return 'editing';
  if (args.issue) return args.issue.source === 'server' ? 'error' : 'invalid';
  if (args.saving && args.dirty) return 'saving';
  if (args.dirty) return 'dirty';
  if (args.active) return 'active';
  if (args.readonly) return 'readonly';
  return undefined;
}

export type CellHandlers = Pick<
  TableHandlers,
  'onCellFocus' | 'onCellMouseDown' | 'onEditorChange' | 'onEditorCommit' | 'onEditorCancel'
>;

interface DataCellProps {
  env: GridEnv;
  handlers: CellHandlers;
  rowId: string;
  /** Fila con los cambios pendientes aplicados. */
  row: Row;
  column: LayoutColumn;
  /** `aria-colindex` (1 = la primera columna de la cuadrícula, incluida la de selección). */
  colIndex: number;
  isActive: boolean;
  inRange: boolean;
  /** Se está editando esta celda (con su borrador), o `null`. */
  editing: EditingCell | null;
  issue: CellIssue | undefined;
  dirty: boolean;
  saving: boolean;
  /** Prefijo de los identificadores (para enlazar el mensaje de error con `aria-describedby`). */
  idPrefix: string;
}

export const DataCell = memo(function DataCell({
  env,
  handlers,
  rowId,
  row,
  column,
  colIndex,
  isActive,
  inRange,
  editing,
  issue,
  dirty,
  saving,
  idPrefix,
}: DataCellProps) {
  const { def } = column;
  const type = getFieldType(def.kind);
  const isEditing = editing !== null;
  const editable = env.editable && env.cellEditable(def, rowId);
  const cellState = cellStateOf({
    editing: isEditing,
    issue,
    saving,
    dirty,
    active: isActive,
    readonly: env.editable && !editable,
  });
  const value = readField(row, column.id);
  const issueId = issue ? `${idPrefix}-${rowId}-${column.id}-issue` : undefined;
  const message = issue ? env.issueMessage(issue, def) : undefined;
  const label = env.text(def.label);

  return (
    <div
      role="gridcell"
      aria-colindex={colIndex}
      tabIndex={isActive && !isEditing ? 0 : -1}
      data-kx-col={column.id}
      data-kx-cell={cellState}
      data-kx-active={isActive ? 'true' : undefined}
      data-kx-align={type.align}
      data-kx-pin={column.pinned || undefined}
      data-kx-kind={def.kind}
      aria-readonly={env.editable && !editable ? true : undefined}
      aria-invalid={issue ? true : undefined}
      aria-describedby={issueId}
      aria-selected={inRange ? true : undefined}
      aria-busy={cellState === 'saving' ? true : undefined}
      data-kx-range={inRange ? 'true' : undefined}
      style={column.pinned ? { '--kx-pin': `${column.offset}px` } : undefined}
      onFocus={() => handlers.onCellFocus(rowId, column.id)}
      onMouseDown={(event) => handlers.onCellMouseDown(event, rowId, column.id)}
    >
      {editing ? (
        <FieldEditor
          value={editing.draft}
          def={def}
          ariaLabel={label}
          autoFocus
          invalid={Boolean(issue)}
          describedBy={issueId}
          onChange={handlers.onEditorChange}
          onCommit={handlers.onEditorCommit}
          onCancel={handlers.onEditorCancel}
        />
      ) : issue?.code === 'format' && typeof value === 'string' ? (
        <span data-kx-ui="value" data-kx-kind={def.kind}>
          {value}
        </span>
      ) : (
        <FieldView value={value} def={def} row={row} />
      )}
      {issue && message ? (
        <span id={issueId} data-th="field-error" data-kx-part="issue">
          {message}
        </span>
      ) : null}
    </div>
  );
});
