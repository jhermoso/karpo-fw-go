// La rejilla en sí (PLAN §4.9): `div[data-th=collection][role=grid]` con cabecera fija y filas virtualizadas.
//
//  - Las filas son `role=row` + `data-th=record` y las celdas son HIJAS DIRECTAS del registro (A3 §7.7).
//  - `aria-rowindex` es el índice de datos (la cabecera es la fila 1); la cebra se calcula con él
//    (`data-kx-even`), no con `nth-child`, que cambiaría de color al desplazarse (A3 §7.7).
//  - La disposición va sólo en variables `--kx-*` (D-R12): `--kx-cols`, `--kx-row-y`, `--kx-pin`.
//  - Roving tabindex: una sola celda (o cabecera) con `tabindex=0`, la activa.
//  - Fila sonda oculta para medir la altura real de la fila con el tema vigente.
import { defaultRangeExtractor, useVirtualizer, type Range as VirtualRange } from '@tanstack/react-virtual';
import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type RefObject } from 'react';
import { Skeleton } from '../ui';
import { GridRow } from './GridRow';
import { HEADER_ROW } from './keyboard';
import type { LayoutColumn } from './model';
import type { SortState } from './url-state';
import { useCoarsePointer } from './use-coarse-pointer';
import { ACTIONS_COL, SELECT_COL, type GridEnv, type TableHandlers, type TableView } from './types';

const LOAD_AHEAD = 6;
const SKELETON_ROWS = 8;

interface GridTableProps {
  env: GridEnv;
  view: TableView;
  handlers: TableHandlers;
  layout: LayoutColumn[];
  sort?: SortState;
  scrollRef: RefObject<HTMLDivElement | null>;
  /** Altura medida de la fila sonda: la mide `GridView`, que conoce el tema. */
  probeRef: RefObject<HTMLDivElement | null>;
  menuRowId: string | null;
  onMenuChange(rowId: string, open: boolean): void;
  onMenuClosed(): void;
  onKeyDown(event: React.KeyboardEvent<HTMLDivElement>): void;
  onGridBlur(event: React.FocusEvent<HTMLDivElement>): void;
  onPaste(event: React.ClipboardEvent<HTMLDivElement>): void;
  onCopy(event: React.ClipboardEvent<HTMLDivElement>): void;
  rowDoubleClick(event: React.MouseEvent<HTMLElement>, rowId: string): void;
}

function setEdge(element: HTMLElement, side: 'start' | 'end', on: boolean): void {
  const name = `data-kx-more-${side}`;
  if (on) element.setAttribute(name, '');
  else element.removeAttribute(name);
}

function isRtl(element: Element): boolean {
  return element.closest('[dir="rtl"]') !== null || getComputedStyle(element).direction === 'rtl';
}

export function GridTable({
  env,
  view,
  handlers,
  layout,
  sort,
  scrollRef,
  probeRef,
  menuRowId,
  onMenuChange,
  onMenuClosed,
  onKeyDown,
  onGridBlur,
  onPaste,
  onCopy,
  rowDoubleClick,
}: GridTableProps) {
  const { state, rowIds, navCols, active, hasSelect, hasActions } = view;
  const { t } = env;
  const idPrefix = useId();
  const [dropTarget, setDropTarget] = useState<string | null>(null);
  const resizing = useRef(false);
  const dragged = useRef<string | null>(null);
  const coarse = useCoarsePointer();

  const colOffset = hasSelect ? 2 : 1;
  const colCount = layout.length + (hasSelect ? 1 : 0) + (hasActions ? 1 : 0);
  const rowCount = view.hasMore ? view.total + state.newRows.length : rowIds.length;

  // Las columnas se dimensionan por CSSOM (D-R12): una variable con la lista de anchos.
  useLayoutEffect(() => {
    const element = scrollRef.current;
    if (!element) return;
    // La última columna absorbe el ancho que sobra: la cabecera y las filas llegan siempre al borde.
    element.style.setProperty(
      '--kx-cols',
      layout.map((column, index) => (index === layout.length - 1 ? `minmax(${column.width}px, 1fr)` : `${column.width}px`)).join(' '),
    );
    element.style.setProperty('--kx-width', `${layout.reduce((sum, column) => sum + column.width, 0)}px`);
  }, [layout, scrollRef]);

  // Sombra en el borde de las columnas fijadas cuando hay más contenido detrás (M-8): se marca con atributos
  // porque CSS no sabe si la lista se ha desplazado. `scrollLeft` es negativo en RTL: se usa su valor absoluto.
  useLayoutEffect(() => {
    const element = scrollRef.current;
    if (!element) return undefined;
    const update = () => {
      const offset = Math.abs(element.scrollLeft);
      const room = element.scrollWidth - element.clientWidth;
      setEdge(element, 'start', offset > 1);
      setEdge(element, 'end', room - offset > 1);
    };
    update();
    element.addEventListener('scroll', update, { passive: true });
    const observer = typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(update);
    observer?.observe(element);
    return () => {
      element.removeEventListener('scroll', update);
      observer?.disconnect();
    };
  }, [scrollRef, layout, hasSelect, hasActions]);

  const activeIndex = active.row >= 0 ? active.row : -1;
  const rangeExtractor = useCallback(
    (range: VirtualRange) => {
      const base = defaultRangeExtractor(range);
      // La fila activa se mantiene pintada aunque salga de la ventana: así conserva el foco y el tabindex.
      if (activeIndex >= 0 && activeIndex < rowIds.length && !base.includes(activeIndex)) {
        base.push(activeIndex);
        base.sort((a, b) => a - b);
      }
      return base;
    },
    [activeIndex, rowIds.length],
  );

  // eslint-disable-next-line react-hooks/incompatible-library -- TanStack Virtual no es compatible con el compilador de React
  const virtualizer = useVirtualizer({
    count: rowIds.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => view.rowHeight,
    overscan: view.overscan,
    getItemKey: (index) => rowIds[index] ?? index,
    rangeExtractor,
  });

  // Si cambia la altura medida (tema, fuente), se vuelve a calcular el reparto.
  useEffect(() => {
    virtualizer.measure();
  }, [virtualizer, view.rowHeight]);

  const items = virtualizer.getVirtualItems();
  const lastIndex = items.length > 0 ? (items[items.length - 1]?.index ?? -1) : -1;
  const { hasMore, fetchingMore, loading } = view;
  const { onLoadMore } = handlers;
  useEffect(() => {
    if (hasMore && !fetchingMore && !loading && lastIndex >= rowIds.length - 1 - LOAD_AHEAD) onLoadMore();
  }, [hasMore, fetchingMore, loading, lastIndex, rowIds.length, onLoadMore]);

  const allSelected = rowIds.length > 0 && rowIds.every((id) => state.selected[id]);
  const someSelected = !allSelected && rowIds.some((id) => state.selected[id]);
  const selectAllRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (selectAllRef.current) selectAllRef.current.indeterminate = someSelected;
  }, [someSelected]);

  function startResize(event: React.PointerEvent<HTMLElement>, column: LayoutColumn): void {
    event.preventDefault();
    event.stopPropagation();
    resizing.current = true;
    const target = event.currentTarget;
    const startX = event.clientX;
    const startWidth = column.width;
    const sign = isRtl(target) ? -1 : 1;
    target.setPointerCapture?.(event.pointerId);
    const move = (next: PointerEvent) => handlers.onResize(column.id, startWidth + (next.clientX - startX) * sign);
    const end = () => {
      target.removeEventListener('pointermove', move);
      target.removeEventListener('pointerup', end);
      target.removeEventListener('pointercancel', end);
      resizing.current = false;
    };
    target.addEventListener('pointermove', move);
    target.addEventListener('pointerup', end);
    target.addEventListener('pointercancel', end);
  }

  const headerActive = active.row === HEADER_ROW;
  const sorted = (column: LayoutColumn): 'ascending' | 'descending' | 'none' | undefined => {
    if (!column.sortable) return undefined;
    if (sort?.field === column.id) return sort.direction === 'asc' ? 'ascending' : 'descending';
    return 'none';
  };

  const headerCells = layout.map((column, index) => {
    const isActive = headerActive && active.col === index + (hasSelect ? 1 : 0);
    const label = env.text(column.def.label);
    return (
      <div
        key={column.id}
        role="columnheader"
        aria-colindex={index + colOffset}
        aria-sort={sorted(column)}
        tabIndex={isActive ? 0 : -1}
        data-kx-col={column.id}
        data-kx-active={isActive ? 'true' : undefined}
        data-kx-sortable={column.sortable ? 'true' : undefined}
        data-kx-pin={column.pinned || undefined}
        data-kx-align={column.def.kind === 'number' || column.def.kind === 'money' || column.def.kind === 'decimal' ? 'end' : undefined}
        data-th-drop={dropTarget === column.id ? 'over' : undefined}
        draggable
        style={column.pinned ? { '--kx-pin': `${column.offset}px` } : undefined}
        onFocus={() => handlers.onCellFocus(null, column.id)}
        onClick={() => column.sortable && handlers.onSort(column.id)}
        onDragStart={(event) => {
          if (resizing.current) {
            event.preventDefault();
            return;
          }
          dragged.current = column.id;
          event.dataTransfer.setData('text/plain', column.id);
          event.dataTransfer.effectAllowed = 'move';
        }}
        onDragOver={(event) => {
          if (!dragged.current || dragged.current === column.id) return;
          event.preventDefault();
          setDropTarget(column.id);
        }}
        onDragLeave={() => setDropTarget((current) => (current === column.id ? null : current))}
        onDrop={(event) => {
          event.preventDefault();
          const field = dragged.current ?? event.dataTransfer.getData('text/plain');
          dragged.current = null;
          setDropTarget(null);
          if (field && field !== column.id) handlers.onReorder(field, column.id);
        }}
        onDragEnd={() => {
          dragged.current = null;
          setDropTarget(null);
        }}
      >
        <span data-kx-part="label">{label}</span>
        <span
          role="separator"
          aria-orientation="vertical"
          aria-label={t('header.resize', { label })}
          data-kx-part="resizer"
          onPointerDown={(event) => startResize(event, column)}
          onClick={(event) => event.stopPropagation()}
          onDoubleClick={(event) => {
            event.stopPropagation();
            handlers.onResize(column.id, undefined);
          }}
        />
      </div>
    );
  });

  const rows = items.map((item) => {
    const rowId = rowIds[item.index];
    if (rowId === undefined) return null;
    const rowActive = active.row === item.index;
    const range = view.range && item.index >= view.range.top && item.index <= view.range.bottom ? view.range : null;
    return (
      <GridRow
        key={rowId}
        env={env}
        handlers={handlers}
        rowId={rowId}
        dataIndex={item.index}
        start={item.start}
        base={view.serverById.get(rowId)}
        server={state.server[rowId]}
        override={state.rows[rowId]}
        issues={state.issues[rowId]}
        status={state.status[rowId]}
        selected={Boolean(state.selected[rowId])}
        selectable={env.ext.grid.selectable}
        editing={state.editing?.rowId === rowId ? state.editing : null}
        layout={layout}
        hasSelect={hasSelect}
        hasActions={hasActions}
        coarse={coarse}
        activeCol={rowActive ? active.col : -1}
        range={range}
        colCount={colCount}
        lastNavCol={navCols.length - 1}
        idPrefix={idPrefix}
        menuOpen={menuRowId === rowId}
        commandsForRow={view.commandsForRow}
        onMenuChange={onMenuChange}
        onMenuClosed={onMenuClosed}
        onRowDoubleClick={rowDoubleClick}
      />
    );
  });

  return (
    <div
      ref={scrollRef}
      role="grid"
      data-th="collection"
      data-kx-scope="grid"
      data-kx-part="viewport"
      data-kx-has-select={hasSelect ? 'true' : undefined}
      data-kx-has-actions={hasActions ? 'true' : undefined}
      aria-label={env.text(env.config.namePlural)}
      aria-rowcount={rowCount + 1}
      aria-colcount={colCount}
      aria-multiselectable={env.ext.grid.selectable && env.ext.grid.selectionMode === 'multiple' ? true : undefined}
      aria-busy={view.loading ? true : undefined}
      data-kx-help="grid.table"
      onKeyDown={onKeyDown}
      onBlur={onGridBlur}
      onPaste={onPaste}
      onCopy={onCopy}
    >
      <div role="rowgroup" data-kx-part="head">
        <div role="row" aria-rowindex={1} data-kx-part="header-row">
          {hasSelect ? (
            <div
              role="columnheader"
              aria-colindex={1}
              tabIndex={headerActive && active.col === 0 ? 0 : -1}
              data-kx-col={SELECT_COL}
              data-kx-active={headerActive && active.col === 0 ? 'true' : undefined}
              data-kx-pin="start"
              onFocus={() => handlers.onCellFocus(null, SELECT_COL)}
              onClick={
                coarse && env.ext.grid.selectionMode === 'multiple'
                  ? (event) => {
                      if (event.target instanceof HTMLInputElement) return;
                      handlers.onToggleAll(!allSelected);
                    }
                  : undefined
              }
            >
              {env.ext.grid.selectionMode === 'multiple' ? (
                <input
                  ref={selectAllRef}
                  type="checkbox"
                  tabIndex={-1}
                  checked={allSelected}
                  aria-label={t('select.all')}
                  onChange={() => undefined}
                  onClick={() => handlers.onToggleAll(!allSelected)}
                />
              ) : (
                <span data-kx-part="sr-only">{t('select.header')}</span>
              )}
            </div>
          ) : null}
          {headerCells}
          {hasActions ? (
            <div
              role="columnheader"
              aria-colindex={colCount}
              tabIndex={headerActive && active.col === navCols.length - 1 ? 0 : -1}
              data-kx-col={ACTIONS_COL}
              data-kx-active={headerActive && active.col === navCols.length - 1 ? 'true' : undefined}
              data-kx-pin="end"
              data-kx-help="grid.row-actions"
              onFocus={() => handlers.onCellFocus(null, ACTIONS_COL)}
            >
              <span data-kx-part="sr-only">{t('actions.header')}</span>
            </div>
          ) : null}
        </div>
      </div>
      <div
        role="rowgroup"
        data-kx-part="body"
        style={{ blockSize: view.loading ? undefined : `${virtualizer.getTotalSize()}px` }}
      >
        <div ref={probeRef} role="row" data-th="record" data-kx-part="probe" aria-hidden="true" inert>
          {/* Lo más alto que cabe en una celda (texto o insignia): todas las filas miden lo mismo. */}
          <div role="gridcell">
            <span data-kx-ui="value">M</span>
            <span data-th="badge" data-th-tone="neutral">
              M
            </span>
          </div>
        </div>
        {view.loading
          ? Array.from({ length: SKELETON_ROWS }, (_unused, index) => (
              <div key={index} role="row" data-th="record" data-kx-part="skeleton-row" aria-hidden="true">
                {hasSelect ? <div role="gridcell" /> : null}
                {layout.map((column) => (
                  <div key={column.id} role="gridcell">
                    <Skeleton />
                  </div>
                ))}
                {hasActions ? <div role="gridcell" /> : null}
              </div>
            ))
          : rows}
      </div>
    </div>
  );
}
