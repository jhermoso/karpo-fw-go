// Una fila de la rejilla: `role=row` + `data-th=record` con sus celdas HIJAS DIRECTAS (A3 §7.7).
// Memoizada: recibe sólo lo de su fila, así que una edición repinta una fila y no toda la ventana.
import { memo, useMemo } from 'react';
import type { CommandDef, Row } from '../contracts';
import { DataCell } from './GridCell';
import { RowActions } from './RowActions';
import { inRange, type Range } from './keyboard';
import type { LayoutColumn } from './model';
import { isFieldDirty, type CellIssue, type EditingCell, type RowOverride, type RowStatus } from './store';
import { ACTIONS_COL, SELECT_COL, type GridEnv, type TableHandlers } from './types';

export interface GridRowProps {
  env: GridEnv;
  handlers: TableHandlers;
  rowId: string;
  /** Posición entre los datos (0 = la primera): índice ARIA, cebra y posición vertical salen de él. */
  dataIndex: number;
  /** Desplazamiento vertical en píxeles (lo da el virtualizador). */
  start: number;
  /** Fila tal como la devolvió la lista. */
  base: Row | undefined;
  /** Fila de servidor del almacén (con ella se comparan los cambios). */
  server: Row | undefined;
  override: RowOverride | undefined;
  issues: Record<string, CellIssue> | undefined;
  status: RowStatus | undefined;
  selected: boolean;
  selectable: boolean;
  editing: EditingCell | null;
  layout: LayoutColumn[];
  hasSelect: boolean;
  hasActions: boolean;
  /** Puntero táctil: la celda de selección entera alterna la selección (M-9). */
  coarse: boolean;
  /** Índice de la columna activa si ésta es la fila activa; `-1` si no. */
  activeCol: number;
  /** Rango de celdas seleccionado, sólo si toca a esta fila. */
  range: Range | null;
  colCount: number;
  lastNavCol: number;
  idPrefix: string;
  menuOpen: boolean;
  commandsForRow(row: Row): CommandDef[];
  onMenuChange(rowId: string, open: boolean): void;
  onMenuClosed(): void;
  onRowDoubleClick(event: React.MouseEvent<HTMLElement>, rowId: string): void;
}

export const GridRow = memo(function GridRow({
  env,
  handlers,
  rowId,
  dataIndex,
  start,
  base,
  server,
  override,
  issues,
  status,
  selected,
  selectable,
  editing,
  layout,
  hasSelect,
  hasActions,
  coarse,
  activeCol,
  range,
  colCount,
  lastNavCol,
  idPrefix,
  menuOpen,
  commandsForRow,
  onMenuChange,
  onMenuClosed,
  onRowDoubleClick,
}: GridRowProps) {
  const row = useMemo<Row>(
    () => (override ? ({ ...(base ?? { id: rowId }), ...override.values } as Row) : (base ?? ({ id: rowId } as Row))),
    [override, base, rowId],
  );
  const phase = status?.phase;
  const dirtyColumns = new Set(override ? layout.filter((column) => isFieldDirty(override, server, column.id, env.kinds)).map((column) => column.id) : []);
  const dirty = override
    ? override.isNew === true || Object.keys(override.values).some((field) => isFieldDirty(override, server, field, env.kinds))
    : false;
  const tokens = [dirty ? 'dirty' : '', override?.isNew ? 'new' : '', row.active === false || row.isActive === false ? 'inactive' : ''].filter(Boolean);
  const commands = useMemo(() => (hasActions ? commandsForRow(row) : []), [hasActions, commandsForRow, row]);
  const colOffset = hasSelect ? 2 : 1;
  const selectActive = activeCol === 0 && hasSelect;
  const actionsActive = hasActions && activeCol === lastNavCol;

  return (
    <div
      role="row"
      data-th="record"
      data-kx-row={rowId}
      aria-rowindex={dataIndex + 2}
      aria-selected={selectable ? selected : undefined}
      aria-busy={phase === 'saving' ? true : undefined}
      data-th-state={tokens.length > 0 ? tokens.join(' ') : undefined}
      data-kx-state={phase}
      data-kx-even={dataIndex % 2 === 1 ? 'true' : 'false'}
      style={{ '--kx-row-y': `${start}px` }}
      onDoubleClick={(event) => onRowDoubleClick(event, rowId)}
    >
      {hasSelect ? (
        <div
          role="gridcell"
          aria-colindex={1}
          tabIndex={selectActive ? 0 : -1}
          data-kx-col={SELECT_COL}
          data-kx-active={selectActive ? 'true' : undefined}
          data-kx-pin="start"
          onFocus={() => handlers.onCellFocus(rowId, SELECT_COL)}
          onMouseDown={(event) => handlers.onCellMouseDown(event, rowId, SELECT_COL)}
          // En táctil el toque en cualquier punto de la celda alterna la selección. El clic de la propia casilla ya
          // lo hace su `onClick` y sube hasta aquí: se descarta para no alternar dos veces.
          onClick={coarse ? (event) => {
            if (event.target instanceof HTMLInputElement) return;
            handlers.onToggleSelected(rowId, event.shiftKey);
          } : undefined}
        >
          <input
            type="checkbox"
            tabIndex={-1}
            checked={selected}
            aria-label={env.t('select.row', { label: env.rowLabel(row) })}
            onChange={() => undefined}
            onClick={(event) => handlers.onToggleSelected(rowId, event.shiftKey)}
          />
        </div>
      ) : null}
      {layout.map((column, index) => {
        const navIndex = index + (hasSelect ? 1 : 0);
        return (
          <DataCell
            key={column.id}
            env={env}
            handlers={handlers}
            rowId={rowId}
            row={row}
            column={column}
            colIndex={index + colOffset}
            isActive={activeCol === navIndex}
            inRange={inRange(range, dataIndex, navIndex)}
            editing={editing?.field === column.id ? editing : null}
            issue={issues?.[column.id]}
            dirty={dirtyColumns.has(column.id)}
            saving={phase === 'saving'}
            idPrefix={idPrefix}
          />
        );
      })}
      {hasActions ? (
        <div
          role="gridcell"
          aria-colindex={colCount}
          tabIndex={actionsActive ? 0 : -1}
          data-kx-col={ACTIONS_COL}
          data-kx-active={actionsActive ? 'true' : undefined}
          data-kx-pin="end"
          onFocus={() => handlers.onCellFocus(rowId, ACTIONS_COL)}
          onMouseDown={(event) => handlers.onCellMouseDown(event, rowId, ACTIONS_COL)}
        >
          <RowActions
            row={row}
            commands={commands}
            open={menuOpen}
            label={env.t('actions.open', { label: env.rowLabel(row) })}
            commandLabel={(command) => env.text(command.label)}
            onOpenChange={(open) => onMenuChange(rowId, open)}
            onSelect={handlers.onCommand}
            onClosed={onMenuClosed}
          />
        </div>
      ) : null}
    </div>
  );
});
