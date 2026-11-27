// GridView (PLAN §4.9): la cuadrícula editable de una entidad, dirigida por su configuración.
// Orquesta datos (useEntityList con desplazamiento infinito), estado de consulta en la URL, edición con el
// almacén por capas, guardado optimista o por lotes, teclado, portapapeles y órdenes. El HTML de la rejilla
// está en `GridTable`; la lógica pura, en `store`, `keyboard`, `save`, `paste`, `url-state` y `prefs`.
import { useQueryClient } from '@tanstack/react-query';
import { Suspense, lazy, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import { useStore } from 'zustand';
import { describeApiError, flattenPages, isApiError, useEntityApi, useEntityList } from '../api';
import { asExt, useText } from '../config';
import { SessionContext, ThemeContext, type ColumnDef, type CommandDef, type Row } from '../contracts';
import { getFieldType, useFieldErrorMessage } from '../fields';
import { useHelp } from '../help';
import { Action, ConfirmDialog, Feedback, Icon, PromptDialog, Spinner, Toolbar, detectPlatform, useShortcutRegistry, useToast } from '../ui';
import { BatchBar, ConflictNotice, SelectionBar } from './Bars';
import { ColumnsMenu } from './ColumnsMenu';
import { FilterBar } from './FilterBar';
import { GridTable } from './GridTable';
import { GridShortcuts, matchesGridShortcut } from './shortcuts';
import { useGridI18n } from './i18n';
import {
  HEADER_ROW,
  afterCommit,
  isMultiCell,
  isNavigationKey,
  isPrintable,
  nextPosition,
  rangeBetween,
  type Dimensions,
  type Position,
} from './keyboard';
import { readField, useGridModel } from './model';
import { useFitColumns } from './use-fit-columns';
import { copyText, editsOfPlan, fillEdits, isSingleValue, planPaste, type PastePlan } from './paste';
import { emptyValueFor } from './store-values';
import { clampWidth, loadPrefs, placeColumn, prefsKey, savePrefs, setWidth, type ColumnPrefs } from './prefs';
import { bulkCommands, commandsFor, runCommand, type CommandOutcome } from './commands';
import {
  reloadAndReapply,
  resolveConflict,
  saveAll,
  saveRow,
  type ReapplyResult,
  type SaveContext,
  type SaveOutcome,
} from './save';
import {
  countDirtyCells,
  dirtyFields,
  dirtyRowIds,
  effectiveRow,
  getGridStore,
  hasBlockingIssues,
  isRowDirty,
  type CellIssue,
  type GridSchema,
  type HistoryResult,
} from './store';
import { ACTIONS_COL, SELECT_COL, type GridEnv, type GridViewProps, type TableHandlers, type TableView } from './types';
import { buildListQuery, cycleSort, readQueryState, sameParams, writeQueryState, type QueryState } from './url-state';
import type { SaveReport } from './save';

// Carga diferida: la vista previa del pegado y el diálogo de conflicto sólo se descargan cuando hacen falta.
const PastePreview = lazy(() => import('./Dialogs').then((module) => ({ default: module.PastePreview })));
const ConflictDialog = lazy(() => import('./Dialogs').then((module) => ({ default: module.ConflictDialog })));

const FALLBACK_ROW_HEIGHT = 40;
const RESIZE_STEP = 16;
const SAVED_FLASH_MS = 2500;

type Pending =
  | { kind: 'confirm'; command: CommandDef; rows: Row[] }
  | { kind: 'prompt'; command: CommandDef; rows: Row[] };

interface ConflictChoice {
  rowId: string;
  label: string;
  fresh: Row;
  common: string[];
  mine: Record<string, unknown>;
  preview: Extract<ReapplyResult, { kind: 'choose' }>['preview'];
}

function cssEscape(value: string): string {
  return value.replace(/["\\]/g, '\\$&');
}

function isRtl(element: Element): boolean {
  return element.closest('[dir="rtl"]') !== null || getComputedStyle(element).direction === 'rtl';
}

/**
 * La última versión de un valor, sin cambiar la identidad de lo que lo lee (callbacks estables hacia las filas
 * memoizadas). Se actualiza en un efecto de maquetación para que los efectos de los hijos, que corren antes que
 * los del padre, ya vean la versión de este pintado.
 */
function useLatest<T>(value: T) {
  const ref = useRef(value);
  useLayoutEffect(() => {
    ref.current = value;
  });
  return ref;
}

export function GridView({ config, route, navigate: navigateProp, onStatus, store: storeProp, overscan = 8 }: GridViewProps) {
  const navigateRef = useLatest(navigateProp);
  const onStatusRef = useLatest(onStatus);
  const navigate = useCallback<GridViewProps['navigate']>((to) => navigateRef.current(to), [navigateRef]);
  const ext = useMemo(() => asExt(config), [config]);
  const api = useEntityApi();
  const client = useQueryClient();
  const session = useContext(SessionContext);
  const theme = useContext(ThemeContext);
  const toast = useToast();
  const text = useText();
  const { t, tError, locale } = useGridI18n();
  const fieldMessage = useFieldErrorMessage();
  const platform = useMemo(() => detectPlatform(), []);
  // Teclas sueltas de atajos globales (la `N` de «Nuevo»): escribirlas sobre una celda abre el editor con esa letra, así que la
  // rejilla no deja que el registro las reciba (cancelaría el carácter). Las demás (`?`, Ctrl+K…) siguen su camino.
  const registry = useShortcutRegistry();
  const bareGlobalKeys = useMemo(
    () => new Set(registry.filter((def) => def.scope === 'global' && [...def.keys].length === 1).map((def) => def.keys.toLowerCase())),
    [registry],
  );
  const store = useMemo(() => storeProp ?? getGridStore(config.id), [storeProp, config.id]);
  const state = useStore(store);
  /** Fila en la que acaba de cancelarse una edición: un segundo Esc seguido devuelve toda la fila. */
  const escArmed = useRef<string | null>(null);
  const timers = useRef(new Set<ReturnType<typeof setTimeout>>());
  const selectionAnchor = useRef<string | null>(null);

  const saveMode = config.saveMode;
  const writeAllowed = !config.permissions?.write || !session || session.can(config.permissions.write);
  const editable = config.editable && saveMode !== 'form' && writeAllowed;

  // ---------------------------------------------------------------------------------------------
  // Esquema de edición: tipos de campo y validación por el registro de tipos
  // ---------------------------------------------------------------------------------------------
  const byField = useMemo(() => new Map(config.columns.map((column) => [column.field, column])), [config.columns]);
  const kinds = useMemo(() => Object.fromEntries(config.columns.map((column) => [column.field, column.kind])), [config.columns]);
  const schema = useMemo<GridSchema>(
    () => ({
      kinds,
      validate: (field, value) => {
        const column = byField.get(field);
        return column ? getFieldType(column.kind).validate(value, column) : [];
      },
    }),
    [kinds, byField],
  );
  useLayoutEffect(() => {
    store.getState().configure(schema);
  }, [store, schema]);

  /** Campos que el formulario exige: sin ellos el servidor rechaza el alta. */
  const formRequired = useMemo(
    () => (config.form?.sections ?? []).flatMap((section) => section.fields).filter((field) => field.required).map((field) => field.field),
    [config.form],
  );
  const requiredFields = useMemo(
    () => [...new Set([...formRequired, ...config.columns.filter((column) => column.required && column.editable).map((column) => column.field)])],
    [config.columns, formRequired],
  );

  // ---------------------------------------------------------------------------------------------
  // Consulta: filtros, orden y búsqueda viven en la URL (`route.params`)
  // ---------------------------------------------------------------------------------------------
  const [query, setQuery] = useState<QueryState>(() => readQueryState(config, route.params));
  const [seenParams, setSeenParams] = useState(route.params);
  if (!sameParams(seenParams, route.params)) {
    setSeenParams(route.params);
    setQuery(readQueryState(config, route.params));
  }
  const updateQuery = useCallback(
    (next: QueryState) => {
      setQuery(next);
      navigate({
        entity: config.id,
        ...(route.view ? { view: route.view } : {}),
        params: writeQueryState(config, next, route.params),
      });
    },
    [config, navigate, route.params, route.view],
  );

  const pageSize = ext.grid.pageSize;
  const listQuery = useMemo(() => buildListQuery(config, query, pageSize), [config, query, pageSize]);
  const list = useEntityList(config, listQuery);
  const serverRows = useMemo(() => flattenPages(list.data), [list.data]);
  const total = list.data?.pages[list.data.pages.length - 1]?.total ?? serverRows.length;
  const serverById = useMemo(() => new Map(serverRows.map((row) => [row.id, row])), [serverRows]);
  useLayoutEffect(() => {
    store.getState().syncServer(serverRows);
  }, [store, serverRows]);

  // Recuento para la cabecera de la entidad (M-7): lo que pasa el filtro y, sin filtro, el total. El total sin
  // filtrar sólo se conoce cuando se ha visto la lista sin filtrar; con un filtro puesto desde el principio no se
  // adivina (la cabecera cuenta entonces sólo las coincidencias).
  const filtered = query.search !== '' || Object.keys(query.filters).length > 0;
  const knownTotal = useRef<{ entity: string; total: number } | null>(null);
  const listPending = list.isPending;
  useEffect(() => {
    const known = knownTotal.current?.entity === config.id ? knownTotal.current.total : undefined;
    if (listPending) {
      onStatusRef.current?.({ loading: true, ...(known !== undefined ? { total: known } : {}) });
      return;
    }
    if (!filtered) knownTotal.current = { entity: config.id, total };
    const unfiltered = filtered ? (knownTotal.current?.entity === config.id ? knownTotal.current.total : undefined) : total;
    onStatusRef.current?.({ loading: false, matched: total, ...(unfiltered !== undefined ? { total: unfiltered } : {}) });
  }, [config.id, filtered, listPending, total, onStatusRef]);

  const rowIds = useMemo(() => [...state.newRows, ...serverRows.map((row) => row.id)], [state.newRows, serverRows]);
  const indexById = useMemo(() => new Map(rowIds.map((id, index) => [id, index])), [rowIds]);

  // ---------------------------------------------------------------------------------------------
  // Columnas: modelo de TanStack y preferencias por usuario
  // ---------------------------------------------------------------------------------------------
  const storageKey = prefsKey(config.id, session?.context?.subjectId);
  const [prefs, setPrefs] = useState<ColumnPrefs>(() => loadPrefs(storageKey, config.columns));
  const [prefsOwner, setPrefsOwner] = useState({ storageKey, columns: config.columns });
  if (prefsOwner.storageKey !== storageKey || prefsOwner.columns !== config.columns) {
    setPrefsOwner({ storageKey, columns: config.columns });
    setPrefs(loadPrefs(storageKey, config.columns));
  }
  const updatePrefs = useCallback(
    (next: ColumnPrefs) => {
      setPrefs(next);
      savePrefs(storageKey, next);
    },
    [storageKey],
  );
  const scrollRef = useRef<HTMLDivElement>(null);
  const rowCommands = useMemo(() => commandsFor(config, 'row', session), [config, session]);
  const hasSelect = ext.grid.selectable;
  const hasActions = rowCommands.length > 0;
  // Ancho para las columnas de datos (D-R34): las que no caben se encogen y, si no basta, se ocultan sin tocar
  // las preferencias; el modelo ya las deja fuera de `layout` para que ni `aria-colcount` ni las flechas las vean.
  const autoFit = prefs.autoFit !== false;
  const available = useFitColumns(scrollRef, autoFit, `${hasSelect}|${hasActions}`);
  const model = useGridModel({ columns: config.columns, rows: serverRows, prefs, sort: query.sort, ...(available !== undefined ? { available } : {}) });
  const layout = model.columns;
  const fitHidden = model.fitHidden;

  // ---------------------------------------------------------------------------------------------
  // Órdenes de fila y masivas
  // ---------------------------------------------------------------------------------------------
  const bulk = useMemo(() => bulkCommands(config, session), [config, session]);
  const commandsForRow = useCallback((row: Row) => commandsFor(config, 'row', session, row), [config, session]);
  const navCols = useMemo(
    () => [...(hasSelect ? [SELECT_COL] : []), ...layout.map((column) => column.id), ...(hasActions ? [ACTIONS_COL] : [])],
    [hasSelect, hasActions, layout],
  );
  const firstDataCol = hasSelect ? 1 : 0;

  // ---------------------------------------------------------------------------------------------
  // Posición activa, rango y foco
  // ---------------------------------------------------------------------------------------------
  const [activeIds, setActiveIds] = useState<{ rowId: string | null | undefined; colId: string | undefined }>({
    rowId: undefined,
    colId: undefined,
  });
  const [rangeState, setRangeState] = useState<{ anchor: Position; focus: Position } | null>(null);
  const [focusTick, setFocusTick] = useState(0);
  const [menuRowId, setMenuRowId] = useState<string | null>(null);
  const [rowHeight, setRowHeight] = useState(FALLBACK_ROW_HEIGHT);
  const probeRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);

  const activeRowIndex =
    activeIds.rowId === null
      ? HEADER_ROW
      : activeIds.rowId === undefined
        ? rowIds.length > 0
          ? 0
          : HEADER_ROW
        : (indexById.get(activeIds.rowId) ?? (rowIds.length > 0 ? 0 : HEADER_ROW));
  const activeColIndex = Math.max(0, activeIds.colId === undefined ? firstDataCol : navCols.indexOf(activeIds.colId));
  const activeCol = Math.min(activeColIndex, Math.max(0, navCols.length - 1));
  const active = useMemo<Position>(() => ({ row: activeRowIndex, col: activeCol }), [activeRowIndex, activeCol]);
  const activeRowId = active.row >= 0 ? (rowIds[active.row] ?? null) : null;
  const activeColId = navCols[active.col] ?? SELECT_COL;
  const multiRange = useMemo(() => {
    const range = rangeState ? rangeBetween(rangeState.anchor, rangeState.focus) : null;
    return isMultiCell(range) ? range : null;
  }, [rangeState]);

  const dims = useCallback(
    (): Dimensions => {
      const element = scrollRef.current;
      const viewport = element?.clientHeight || 600;
      return {
        rows: rowIds.length,
        cols: navCols.length,
        page: Math.max(1, Math.floor(viewport / rowHeight) - 1),
        rtl: element ? isRtl(element) : false,
      };
    },
    [rowIds.length, navCols.length, rowHeight],
  );

  const idsOf = useCallback(
    (pos: Position) => ({
      rowId: pos.row < 0 ? null : (rowIds[pos.row] ?? null),
      colId: navCols[pos.col] ?? SELECT_COL,
    }),
    [rowIds, navCols],
  );

  const moveTo = useCallback(
    (pos: Position, options: { extend?: boolean; focus?: boolean } = {}) => {
      const ids = idsOf(pos);
      setActiveIds(ids);
      if (options.extend) setRangeState((current) => ({ anchor: current?.anchor ?? active, focus: pos }));
      else setRangeState(null);
      escArmed.current = null;
      if (options.focus !== false) setFocusTick((tick) => tick + 1);
    },
    [idsOf, active],
  );

  // Devolver el foco a la celda activa tras mover, editar o cerrar un menú.
  useEffect(() => {
    if (focusTick === 0) return;
    const root = scrollRef.current;
    if (!root) return;
    const selector =
      activeRowId === null || active.row < 0
        ? `[data-kx-part="header-row"] > [data-kx-col="${cssEscape(activeColId)}"]`
        : `[data-kx-row="${cssEscape(activeRowId)}"] > [data-kx-col="${cssEscape(activeColId)}"]`;
    root.querySelector<HTMLElement>(selector)?.focus();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusTick]);

  // ---------------------------------------------------------------------------------------------
  // Medida de la fila con el tema vigente (fila sonda) y repetición al cambiar de tema
  // ---------------------------------------------------------------------------------------------
  const measure = useCallback(() => {
    const height = probeRef.current?.getBoundingClientRect().height ?? 0;
    if (height > 0) setRowHeight(Math.ceil(height));
  }, []);
  useLayoutEffect(measure, [measure]);
  useEffect(() => theme?.onChange(measure), [theme, measure]);
  useEffect(() => {
    let cancelled = false;
    void document.fonts?.ready.then(() => {
      if (!cancelled) measure();
    });
    return () => {
      cancelled = true;
    };
  }, [measure]);
  useLayoutEffect(() => {
    scrollRef.current?.style.setProperty('--kx-row-height', `${rowHeight}px`);
  }, [rowHeight]);

  // ---------------------------------------------------------------------------------------------
  // Mensajes: región viva, avisos y textos de error
  // ---------------------------------------------------------------------------------------------
  const [announcement, setAnnouncement] = useState('');
  const announce = useCallback((message: string) => setAnnouncement(message), []);

  const issueMessage = useCallback(
    (issue: CellIssue, def: ColumnDef) =>
      issue.source === 'local' ? fieldMessage(issue.code, def) : tError(issue.code, issue.message),
    [fieldMessage, tError],
  );

  const rowLabel = useCallback(
    (row: Row | undefined) => {
      if (!row) return '';
      const value = readField(row, config.displayField) ?? row.name ?? row.id;
      return value === null || value === undefined ? '' : String(value);
    },
    [config.displayField],
  );

  const errorText = useCallback(
    (error: unknown): string => {
      if (isApiError(error)) {
        const described = describeApiError(error);
        return tError(described.key.replace(/^errors:/, ''), described.fallback);
      }
      return error instanceof Error ? error.message : String(error);
    },
    [tError],
  );

  const cellEditable = useCallback(
    (def: ColumnDef, rowId: string) => {
      if (!def.editable || def.field.includes('.')) return false;
      return !def.immutable || store.getState().rows[rowId]?.isNew === true;
    },
    [store],
  );

  const env = useMemo<GridEnv>(
    () => ({ config, ext, store, editable, saveMode, kinds, locale, t, text, issueMessage, rowLabel, cellEditable }),
    [config, ext, store, editable, saveMode, kinds, locale, t, text, issueMessage, rowLabel, cellEditable],
  );

  // ---------------------------------------------------------------------------------------------
  // Guardado
  // ---------------------------------------------------------------------------------------------
  const saveCtx = useMemo<SaveContext>(
    () => ({ config, api, client, store, kinds, requiredFields }),
    [config, api, client, store, kinds, requiredFields],
  );
  const [saving, setSaving] = useState(false);
  const [report, setReport] = useState<SaveReport | null>(null);
  const [conflictBusy, setConflictBusy] = useState<string | null>(null);
  const [conflictChoice, setConflictChoice] = useState<ConflictChoice | null>(null);
  useEffect(() => {
    const pending = timers.current;
    return () => pending.forEach((timer) => clearTimeout(timer));
  }, []);

  const label = useCallback(
    (rowId: string) => rowLabel(state.server[rowId] ?? serverById.get(rowId)) || rowId,
    [rowLabel, state.server, serverById],
  );

  const afterOutcome = useCallback(
    (outcome: SaveOutcome) => {
      const name = label(outcome.rowId);
      switch (outcome.status) {
        case 'saved': {
          announce(t('live.saved', { label: name }));
          const timer = setTimeout(() => {
            timers.current.delete(timer);
            if (store.getState().status[outcome.rowId]?.phase === 'saved') store.getState().setStatus(outcome.rowId, null);
          }, SAVED_FLASH_MS);
          timers.current.add(timer);
          break;
        }
        case 'conflict':
          announce(t('live.conflict', { label: name }));
          toast.show({ tone: 'warning', message: t('toast.conflict', { label: name }) });
          break;
        case 'invalid':
          announce(t('live.serverInvalid', { label: name }));
          toast.show({ tone: 'warning', message: t('toast.invalid', { label: name }) });
          break;
        case 'failed':
          announce(t('live.failed', { label: name }));
          toast.show({
            tone: 'error',
            message: t(outcome.reverted ? 'toast.failedReverted' : 'toast.failed', {
              label: name,
              reason: errorText(outcome.error),
            }),
            ...(outcome.reverted
              ? {
                  actionLabel: t('toast.undo'),
                  onAction: () => {
                    store.getState().undo();
                    announce(t('live.undone', { what: t('history.revertRow') }));
                  },
                  duration: 12_000,
                }
              : {}),
          });
          break;
        case 'skipped':
          if (outcome.reason === 'invalid') announce(t('live.cannotSave', { label: name }));
          break;
        default:
      }
    },
    [label, announce, t, store, toast, errorText],
  );

  const saveOne = useCallback(
    async (rowId: string): Promise<SaveOutcome> => {
      const outcome = await saveRow(saveCtx, rowId, { revertOnFailure: saveMode === 'optimistic' });
      afterOutcome(outcome);
      return outcome;
    },
    [saveCtx, saveMode, afterOutcome],
  );

  const saveEverything = useCallback(async () => {
    if (saving) return;
    setSaving(true);
    setReport(null);
    try {
      const result = await saveAll(saveCtx, { revertOnFailure: false });
      for (const outcome of result.outcomes) if (outcome.status !== 'saved') afterOutcome(outcome);
      if (result.saved > 0) {
        announce(t('live.savedMany', { count: result.saved }));
        toast.show({ tone: 'success', message: t('toast.savedMany', { count: result.saved }) });
      }
      if (result.failed + result.conflicts + result.skipped > 0) setReport(result);
    } finally {
      setSaving(false);
    }
  }, [saving, saveCtx, afterOutcome, announce, t, toast]);

  /** En modo optimista, las operaciones sueltas (pegar, rellenar, deshacer…) se guardan al momento. */
  const autoSave = useCallback(
    (rowIdsToSave: readonly string[]) => {
      if (saveMode !== 'optimistic') return;
      for (const id of rowIdsToSave) {
        const current = store.getState();
        if (isRowDirty(current, id, kinds) && !hasBlockingIssues(current, id)) void saveOne(id);
      }
    },
    [saveMode, store, kinds, saveOne],
  );

  // Optimista: se guarda la fila al salir de ella (o al salir de la rejilla).
  const previousRow = useRef<string | null>(null);
  useEffect(() => {
    const previous = previousRow.current;
    previousRow.current = activeRowId;
    if (saveMode === 'optimistic' && previous && previous !== activeRowId) {
      const current = store.getState();
      if (isRowDirty(current, previous, kinds)) void saveOne(previous);
    }
  }, [activeRowId, saveMode, store, kinds, saveOne]);

  const onGridBlur = useCallback(
    (event: React.FocusEvent<HTMLDivElement>) => {
      if (saveMode !== 'optimistic') return;
      if (store.getState().editing) return;
      if (event.relatedTarget instanceof Node && event.currentTarget.contains(event.relatedTarget)) return;
      if (activeRowId && isRowDirty(store.getState(), activeRowId, kinds)) void saveOne(activeRowId);
    },
    [saveMode, store, activeRowId, kinds, saveOne],
  );

  // ---------------------------------------------------------------------------------------------
  // Historia: deshacer y rehacer (también pegado y relleno)
  // ---------------------------------------------------------------------------------------------
  const handleHistory = useCallback(
    (direction: 'undo' | 'redo') => {
      const result: HistoryResult | null = direction === 'undo' ? store.getState().undo() : store.getState().redo();
      if (!result) {
        announce(t(direction === 'undo' ? 'live.nothingToUndo' : 'live.nothingToRedo'));
        return;
      }
      announce(t(direction === 'undo' ? 'live.undone' : 'live.redone', { what: t(`history.${result.kind}`) }));
      const first = result.cells[0];
      const index = first ? indexById.get(first.rowId) : undefined;
      if (first && index !== undefined) {
        const col = navCols.indexOf(first.field);
        if (col >= 0) moveTo({ row: index, col });
      }
      autoSave(result.rowIds);
    },
    [store, announce, t, indexById, navCols, moveTo, autoSave],
  );

  // ---------------------------------------------------------------------------------------------
  // Edición en línea
  // ---------------------------------------------------------------------------------------------
  const startEdit = useCallback(
    (rowId: string, colId: string, overwrite: boolean) => {
      const column = byField.get(colId);
      if (!column || !editable || !cellEditable(column, rowId)) return false;
      if (store.getState().status[rowId]?.phase === 'saving') return false;
      flushSync(() => {
        store.getState().beginEdit(rowId, colId, { overwrite });
      });
      return true;
    },
    [byField, editable, cellEditable, store],
  );

  const commitEditing = useCallback(
    (move?: 'next' | 'prev' | 'down' | 'up') => {
      const editing = store.getState().editing;
      if (!editing) return;
      const result = store.getState().commitEdit();
      if (result.issue) {
        const column = byField.get(editing.field);
        const code = result.issue;
        announce(t('live.invalid', { label: column ? text(column.label) : editing.field, message: column ? fieldMessage(code, column) : code }));
        // Con un valor inválido la celda se queda con el foco: se ve el error y Esc lo revierte.
        setFocusTick((tick) => tick + 1);
        return;
      }
      if (move) moveTo(afterCommit(active, move, dims()));
      else setFocusTick((tick) => tick + 1);
    },
    [store, byField, announce, t, text, fieldMessage, moveTo, active, dims],
  );

  const cancelEditing = useCallback(() => {
    store.getState().cancelEdit();
    escArmed.current = activeRowId;
    setFocusTick((tick) => tick + 1);
  }, [store, activeRowId]);

  const editorChange = useCallback((value: unknown) => store.getState().setDraft(value), [store]);

  // ---------------------------------------------------------------------------------------------
  // Selección, filas y órdenes
  // ---------------------------------------------------------------------------------------------
  const toggleSelected = useCallback(
    (rowId: string, shiftKey: boolean) => {
      const current = store.getState();
      if (ext.grid.selectionMode === 'single') {
        const wasOn = Boolean(current.selected[rowId]);
        current.clearSelection();
        if (!wasOn) current.toggleSelected(rowId, true);
        return;
      }
      if (shiftKey && selectionAnchor.current && indexById.has(selectionAnchor.current)) {
        const from = indexById.get(selectionAnchor.current) as number;
        const to = indexById.get(rowId) ?? from;
        current.selectMany(rowIds.slice(Math.min(from, to), Math.max(from, to) + 1), true);
      } else {
        current.toggleSelected(rowId);
      }
      selectionAnchor.current = rowId;
    },
    [store, ext.grid.selectionMode, indexById, rowIds],
  );

  const openRow = useCallback(
    (rowId: string, mode: 'edit' | 'read' = 'edit') => {
      if (rowId.startsWith('new:')) return;
      const current = store.getState();
      if (saveMode === 'optimistic' && isRowDirty(current, rowId, kinds)) void saveOne(rowId);
      navigate({ entity: config.id, id: rowId, mode });
    },
    [store, saveMode, kinds, saveOne, navigate, config.id],
  );

  const rowDoubleClick = useCallback(
    (event: React.MouseEvent<HTMLElement>, rowId: string) => {
      const target = event.target as HTMLElement;
      if (target.closest('input, select, textarea, button, a, [role="menu"], [data-kx-part="resizer"]')) return;
      if (target.closest(`[data-kx-col="${SELECT_COL}"], [data-kx-col="${ACTIONS_COL}"]`)) return;
      if (store.getState().editing) return;
      openRow(rowId, 'edit');
    },
    [store, openRow],
  );

  const [pending, setPending] = useState<Pending | null>(null);
  const [commandBusy, setCommandBusy] = useState(false);

  const execute = useCallback(
    async (command: CommandDef, rows: Row[], body?: Record<string, unknown>) => {
      setCommandBusy(true);
      const outcomes: CommandOutcome[] = [];
      try {
        for (const row of rows) {
          const base = store.getState().server[row.id] ?? row;
          outcomes.push(await runCommand({ config, api, client, store }, command, base, body));
        }
      } finally {
        setCommandBusy(false);
      }
      const done = outcomes.filter((o) => o.status === 'done').length;
      const waiting = outcomes.filter((o) => o.status === 'pending').length;
      const failed = outcomes.filter((o): o is Extract<CommandOutcome, { status: 'failed' }> => o.status === 'failed');
      const name = text(command.label);
      /** Texto de la orden si la configuración lo trae traducido; si es sólo una clave sin traducir, el de la cuadrícula. */
      const own = (value: typeof command.successMessage, fallback: string): string => {
        const resolved = value ? text(value) : '';
        return resolved !== '' && resolved !== value?.key ? resolved : fallback;
      };
      if (done > 0) {
        const message = own(command.successMessage, t('toast.commandDone', { label: name, count: done }));
        announce(message);
        toast.show({ tone: 'success', message });
      }
      if (waiting > 0) {
        const message = own(command.acceptedMessage, t('toast.pendingApproval', { count: waiting }));
        announce(message);
        toast.show({ tone: 'info', message });
      }
      if (failed.length > 0) {
        const reason = errorText(failed[0]?.error);
        announce(t('live.commandFailed', { label: name }));
        toast.show({ tone: 'error', message: t('toast.commandFailed', { label: name, count: failed.length, reason }) });
      }
      store.getState().clearSelection();
    },
    [store, config, api, client, text, t, announce, toast, errorText],
  );

  const runOn = useCallback(
    (command: CommandDef, rows: Row[]) => {
      if (command.kind === 'view') {
        const first = rows[0];
        if (first) openRow(first.id, 'read');
        return;
      }
      if (command.kind === 'edit') {
        const first = rows[0];
        if (first) openRow(first.id, 'edit');
        return;
      }
      if (command.prompt) setPending({ kind: 'prompt', command, rows });
      else if (command.confirm || command.kind === 'delete') setPending({ kind: 'confirm', command, rows });
      else void execute(command, rows);
    },
    [openRow, execute],
  );

  const selectedRows = useMemo(
    () => rowIds.filter((id) => state.selected[id]).flatMap((id) => (serverById.get(id) ? [serverById.get(id) as Row] : [])),
    [rowIds, state.selected, serverById],
  );

  // ---------------------------------------------------------------------------------------------
  // Teclado
  // ---------------------------------------------------------------------------------------------
  const valueOf = useCallback(
    (rowId: string, field: string): unknown => {
      const row = effectiveRow(store.getState(), rowId) ?? serverById.get(rowId);
      return row ? readField(row, field) : undefined;
    },
    [store, serverById],
  );

  /** Celdas de datos del rango (o la activa): filas y columnas editables. */
  const targetCells = useCallback(() => {
    const top = multiRange ? multiRange.top : active.row;
    const bottom = multiRange ? multiRange.bottom : active.row;
    const start = multiRange ? multiRange.start : active.col;
    const end = multiRange ? multiRange.end : active.col;
    const ids = rowIds.slice(Math.max(0, top), bottom + 1);
    const fields = navCols.slice(start, end + 1).filter((id) => byField.has(id));
    return { ids, fields };
  }, [multiRange, active.row, active.col, rowIds, navCols, byField]);

  const clearTargets = useCallback(() => {
    if (!editable) return;
    const { ids, fields } = targetCells();
    const edits = ids.flatMap((rowId) =>
      fields
        .filter((field) => cellEditable(byField.get(field) as ColumnDef, rowId))
        .map((field) => ({ rowId, field, value: emptyValueFor(byField.get(field)?.kind) })),
    );
    if (edits.length === 0) return;
    const result = store.getState().applyEdits(edits, 'clear');
    if (result) {
      announce(t('live.cleared', { count: edits.length }));
      autoSave(result.rowIds);
    }
  }, [editable, targetCells, cellEditable, byField, store, announce, t, autoSave]);

  const fillDown = useCallback(() => {
    if (!editable || active.row < 0) return;
    let edits;
    if (multiRange && multiRange.top !== multiRange.bottom) {
      const { ids, fields } = targetCells();
      edits = fillEdits(ids, fields, valueOf).filter((edit) => cellEditable(byField.get(edit.field) as ColumnDef, edit.rowId));
    } else {
      const source = rowIds[active.row - 1];
      const target = rowIds[active.row];
      const field = navCols[active.col];
      if (!source || !target || !field || !byField.has(field)) return;
      edits = cellEditable(byField.get(field) as ColumnDef, target) ? [{ rowId: target, field, value: valueOf(source, field) }] : [];
    }
    const result = store.getState().applyEdits(edits, 'fill');
    if (result) {
      announce(t('live.filled', { count: edits.length }));
      autoSave(result.rowIds);
    }
  }, [editable, active.row, active.col, multiRange, targetCells, valueOf, cellEditable, byField, rowIds, navCols, store, announce, t, autoSave]);

  const activateCell = useCallback(() => {
    const col = activeColId;
    if (activeRowId === null || active.row < 0) {
      if (byField.get(col)?.serverSortField) updateQuery({ ...query, sort: cycleSort(query.sort, col) });
      return;
    }
    if (col === SELECT_COL) toggleSelected(activeRowId, false);
    else if (col === ACTIONS_COL) setMenuRowId(activeRowId);
    else startEdit(activeRowId, col, false);
  }, [activeColId, activeRowId, active.row, byField, updateQuery, query, toggleSelected, startEdit]);

  const handleEscape = useCallback(() => {
    if (!activeRowId) return;
    const current = store.getState();
    const issue = current.issues[activeRowId]?.[activeColId];
    const cellDirty = dirtyFields(current, activeRowId, kinds).includes(activeColId);
    if (escArmed.current === activeRowId && isRowDirty(current, activeRowId, kinds)) {
      escArmed.current = null;
      current.revertRow(activeRowId);
      announce(t('live.rowReverted'));
      return;
    }
    if (cellDirty || issue) {
      current.revertCell(activeRowId, activeColId);
      // La incidencia que queda (una fila nueva no tiene valor al que volver) se retira a mano.
      if (store.getState().issues[activeRowId]?.[activeColId]) store.getState().clearIssue(activeRowId, activeColId);
      escArmed.current = activeRowId;
      announce(t('live.cellReverted'));
      return;
    }
    escArmed.current = null;
  }, [activeRowId, activeColId, store, kinds, announce, t]);

  const onKeyDown = useCallback(
    (event: React.KeyboardEvent<HTMLDivElement>) => {
      const current = store.getState();
      const mod = platform === 'mac' ? event.metaKey : event.ctrlKey;
      // Las teclas de la cuadrícula no llegan al registro de atajos (ver `matchesGridShortcut`).
      if (matchesGridShortcut(event.nativeEvent, platform)) event.stopPropagation();
      if (current.editing) {
        // Dentro del editor mandan el propio campo y el navegador (deshacer del texto, pegar...).
        if (event.ctrlKey || event.metaKey) event.stopPropagation();
        return;
      }
      const target = event.target as HTMLElement;
      const cell = target.closest<HTMLElement>('[role="gridcell"], [role="columnheader"]');
      if (!cell || !event.currentTarget.contains(cell)) return;
      const onCell = target === cell;
      const key = event.key;
      const lower = key.toLowerCase();
      const position = active;

      if (mod && (lower === 'c' || lower === 'v' || lower === 'x')) {
        event.stopPropagation(); // el navegador lanza copy/paste y la rejilla los atiende
        return;
      }
      if (mod && lower === 'z') {
        event.preventDefault();
        handleHistory(event.shiftKey ? 'redo' : 'undo');
        return;
      }
      if (mod && lower === 'y') {
        event.preventDefault();
        handleHistory('redo');
        return;
      }
      if (mod && lower === 's') {
        event.preventDefault();
        void saveEverything();
        return;
      }
      if (mod && lower === 'd') {
        event.preventDefault();
        fillDown();
        return;
      }
      if (mod && key === 'Enter') {
        event.preventDefault();
        if (activeRowId) openRow(activeRowId, 'edit');
        return;
      }
      if (event.altKey && position.row === HEADER_ROW && (key === 'ArrowLeft' || key === 'ArrowRight')) {
        const column = layout.find((candidate) => candidate.id === activeColId);
        if (column) {
          event.preventDefault();
          const sign = (key === 'ArrowRight' ? 1 : -1) * (isRtl(event.currentTarget) ? -1 : 1);
          updatePrefs(setWidth(prefs, column.id, column.width + sign * RESIZE_STEP));
        }
        return;
      }
      if (mod && key.startsWith('Arrow')) {
        event.preventDefault();
        const d = dims();
        const horizontalStart = (key === 'ArrowLeft') !== Boolean(d.rtl);
        if (key === 'ArrowUp') moveTo({ row: d.rows > 0 ? 0 : HEADER_ROW, col: position.col }, { extend: event.shiftKey });
        else if (key === 'ArrowDown') moveTo({ row: Math.max(HEADER_ROW, d.rows - 1), col: position.col }, { extend: event.shiftKey });
        else moveTo({ row: position.row, col: horizontalStart ? 0 : d.cols - 1 }, { extend: event.shiftKey });
        return;
      }
      // Tab y Mayús+Tab no se interceptan fuera de la edición (D-R21): la cuadrícula es una sola parada de
      // tabulación y el navegador saca el foco de ella; Mayús+Tab vuelve a la celda activa (tabindex itinerante).
      if (isNavigationKey(key) && !mod) {
        const next = nextPosition(position, event, dims());
        if (next === null) return;
        event.preventDefault();
        moveTo(next, { extend: event.shiftKey && key !== 'Home' && key !== 'End' });
        return;
      }
      if (isNavigationKey(key) && mod) {
        const next = nextPosition(position, event, dims());
        if (next) {
          event.preventDefault();
          moveTo(next, { extend: event.shiftKey });
        }
        return;
      }
      if (key === 'Enter' || key === 'F2') {
        if (!onCell && key === 'Enter') return; // el botón o la casilla hijos actúan solos
        event.preventDefault();
        activateCell();
        return;
      }
      if (key === ' ') {
        if (!onCell) return;
        event.preventDefault();
        if (activeRowId === null) activateCell();
        else toggleSelected(activeRowId, event.shiftKey);
        return;
      }
      if (key === 'Delete') {
        event.preventDefault();
        clearTargets();
        return;
      }
      if (key === 'Escape') {
        handleEscape();
        return;
      }
      if (key === '/' && !event.altKey && searchRef.current) {
        event.preventDefault();
        searchRef.current.focus();
        return;
      }
      if (isPrintable(event) && onCell && activeRowId !== null && activeColId !== SELECT_COL && activeColId !== ACTIONS_COL) {
        // No se cancela el evento: el carácter cae en el editor recién enfocado y sustituye el valor. Si es la tecla
        // suelta de un atajo global (la `N` de «Nuevo») se detiene su subida: el registro lo cancelaría y no se dispararía.
        if (bareGlobalKeys.has(key.toLowerCase())) event.stopPropagation();
        startEdit(activeRowId, activeColId, true);
      }
    },
    [
      store,
      platform,
      bareGlobalKeys,
      active,
      activeRowId,
      activeColId,
      layout,
      prefs,
      handleHistory,
      saveEverything,
      fillDown,
      openRow,
      updatePrefs,
      dims,
      moveTo,
      activateCell,
      toggleSelected,
      clearTargets,
      handleEscape,
      startEdit,
    ],
  );

  // ---------------------------------------------------------------------------------------------
  // Portapapeles
  // ---------------------------------------------------------------------------------------------
  const [plan, setPlan] = useState<PastePlan | null>(null);

  const onCopy = useCallback(
    (event: React.ClipboardEvent<HTMLDivElement>) => {
      if (store.getState().editing || active.row < 0) return;
      const { ids, fields } = targetCells();
      if (fields.length === 0 || ids.length === 0) return;
      const defs = fields.map((field) => byField.get(field) as ColumnDef);
      const values = ids.map((rowId) => fields.map((field) => valueOf(rowId, field)));
      event.clipboardData.setData('text/plain', copyText(values, defs, locale));
      event.preventDefault();
      announce(t('live.copied', { count: ids.length * fields.length }));
    },
    [store, active.row, targetCells, byField, valueOf, locale, announce, t],
  );

  const onPaste = useCallback(
    (event: React.ClipboardEvent<HTMLDivElement>) => {
      if (store.getState().editing || active.row < 0 || !editable) return;
      const pasted = event.clipboardData.getData('text/plain');
      if (!pasted) return;
      const startField = navCols[active.col];
      if (!startField || !byField.has(startField)) return;
      event.preventDefault();
      const targets = navCols
        .slice(active.col)
        .filter((id) => byField.has(id))
        .map((id) => {
          const def = byField.get(id) as ColumnDef;
          return { def, editable: (rowId: string) => cellEditable(def, rowId) };
        });
      const planned = planPaste(pasted, targets, rowIds.slice(active.row), locale);
      const rowId = rowIds[active.row];
      if (isSingleValue(pasted) && rowId) {
        const def = byField.get(startField) as ColumnDef;
        const cell = planned.rows[0]?.[0];
        if (!cellEditable(def, rowId)) return;
        // Un solo valor: se aplica directo; si no se entiende, entra tal cual y la celda queda inválida.
        const parsed = getFieldType(def.kind).parse(pasted, def, locale);
        const result = store.getState().applyEdits(
          [{ rowId, field: startField, value: cell?.status === 'ok' ? cell.value : parsed.ok ? parsed.value : pasted.trim() }],
          'paste',
        );
        if (result) {
          announce(t('live.pasted', { count: 1 }));
          autoSave(result.rowIds);
        }
        return;
      }
      setPlan(planned);
    },
    [store, active.row, active.col, editable, navCols, byField, cellEditable, rowIds, locale, announce, t, autoSave],
  );

  const applyPaste = useCallback(() => {
    if (!plan) return;
    const edits = editsOfPlan(plan);
    setPlan(null);
    const result = store.getState().applyEdits(edits, 'paste');
    setFocusTick((tick) => tick + 1);
    if (result) {
      announce(t('live.pasted', { count: edits.length }));
      autoSave(result.rowIds);
    }
  }, [plan, store, announce, t, autoSave]);

  // ---------------------------------------------------------------------------------------------
  // Conflictos 409
  // ---------------------------------------------------------------------------------------------
  const conflictRows = useMemo(
    () =>
      Object.entries(state.status)
        .filter(([, status]) => status.phase === 'conflict')
        .map(([id]) => ({ id, label: label(id) })),
    [state.status, label],
  );

  const reapply = useCallback(
    async (rowId: string) => {
      setConflictBusy(rowId);
      const result = await reloadAndReapply(saveCtx, rowId);
      setConflictBusy(null);
      if (result.kind === 'reapplied') {
        announce(t('live.reapplied', { label: label(rowId) }));
        toast.show({ tone: 'success', message: t('toast.reapplied', { label: label(rowId) }) });
        if (saveMode === 'optimistic') void saveOne(rowId);
      } else if (result.kind === 'choose') {
        setConflictChoice({
          rowId,
          label: label(rowId),
          fresh: result.fresh,
          common: result.preview.common,
          mine: effectiveRow(store.getState(), rowId) ?? {},
          preview: result.preview,
        });
      } else {
        toast.show({ tone: 'error', message: t('toast.reloadFailed', { reason: errorText(result.error) }) });
      }
    },
    [saveCtx, announce, t, label, toast, saveMode, saveOne, store, errorText],
  );

  const chooseConflict = useCallback(
    (choice: 'mine' | 'theirs') => {
      const current = conflictChoice;
      setConflictChoice(null);
      if (!current) return;
      resolveConflict(saveCtx, current.rowId, current.fresh, choice, current.preview);
      announce(t('live.reapplied', { label: current.label }));
      if (saveMode === 'optimistic') void saveOne(current.rowId);
    },
    [conflictChoice, saveCtx, announce, t, saveMode, saveOne],
  );

  // ---------------------------------------------------------------------------------------------
  // Altas en la cuadrícula
  // ---------------------------------------------------------------------------------------------
  // Se puede dar de alta en la cuadrícula sólo si todo lo que el formulario exige tiene columna editable.
  const canCreate =
    editable &&
    config.form?.create === true &&
    formRequired.every((field) => {
      const column = byField.get(field);
      return Boolean(column?.editable) && !field.includes('.');
    });
  const addRow = useCallback(() => {
    const id = store.getState().addRow({});
    const first = layout.find((column) => cellEditable(column.def, id));
    if (!first) return;
    flushSync(() => setActiveIds({ rowId: id, colId: first.id }));
    startEdit(id, first.id, false);
  }, [store, layout, cellEditable, startEdit]);

  // ---------------------------------------------------------------------------------------------
  // Ayuda y atajos
  // ---------------------------------------------------------------------------------------------
  useHelp(`grid:${config.id}`, config.help);
  const shortcutHandlers = {
    undo: () => handleHistory('undo'),
    redo: () => handleHistory('redo'),
    save: () => void saveEverything(),
    fill: fillDown,
    open: () => activeRowId && openRow(activeRowId, 'edit'),
    search: () => searchRef.current?.focus(),
  };

  const onMenuChange = useCallback((rowId: string, open: boolean) => setMenuRowId(open ? rowId : null), []);
  const onMenuClosed = useCallback(() => setFocusTick((tick) => tick + 1), []);

  // ---------------------------------------------------------------------------------------------
  // Vista
  // ---------------------------------------------------------------------------------------------
  const dirtyCount = countDirtyCells(state, kinds);
  const dirtyRows = dirtyRowIds(state, kinds);
  const blockedRows = dirtyRows.filter((id) => hasBlockingIssues(state, id) || ['error', 'conflict'].includes(state.status[id]?.phase ?? ''));
  const invalidCells = Object.values(state.issues).reduce((sum, cells) => sum + Object.keys(cells).length, 0);
  const showBatchBar = saveMode === 'batch' ? dirtyCount > 0 || report !== null : blockedRows.length > 0 || report !== null;
  const summary = dirtyCount > 0 ? t('live.summary', { count: dirtyCount, invalid: invalidCells }) : '';

  // La barra de cambios flota sobre el borde inferior de la lista (M-9): su alto se publica en una variable
  // para que el relleno inferior de la lista sea igual y la última fila no quede tapada.
  const batchRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const bar = batchRef.current;
    const root = bar?.parentElement;
    if (!bar || !root) return undefined;
    const publish = () => root.style.setProperty('--kx-batch-height', showBatchBar ? `${bar.offsetHeight}px` : '0px');
    publish();
    if (typeof ResizeObserver === 'undefined') return undefined;
    const observer = new ResizeObserver(publish);
    observer.observe(bar);
    return () => observer.disconnect();
  }, [showBatchBar]);

  const view: TableView = {
    state,
    rowIds,
    serverById,
    hasSelect,
    hasActions,
    navCols,
    active,
    range: multiRange,
    total,
    loading: list.isPending,
    fetchingMore: list.isFetchingNextPage,
    hasMore: Boolean(list.hasNextPage),
    rowHeight,
    rowCommands,
    commandsForRow,
    overscan,
  };

  const latestHandlers: TableHandlers = {
    onCellFocus: (rowId, colId) => {
      if (store.getState().editing) return;
      setActiveIds({ rowId, colId });
    },
    onCellMouseDown: (event, rowId, colId) => {
      const editing = store.getState().editing;
      if (editing && (editing.rowId !== rowId || editing.field !== colId)) commitEditing();
      const row = indexById.get(rowId);
      const col = navCols.indexOf(colId);
      if (row === undefined || col < 0) return;
      const pos = { row, col };
      if (event.shiftKey) setRangeState((current) => ({ anchor: current?.anchor ?? active, focus: pos }));
      else setRangeState(null);
      escArmed.current = null;
    },
    onSort: (field) => {
      if (byField.get(field)?.serverSortField) updateQuery({ ...query, sort: cycleSort(query.sort, field) });
    },
    onResize: (field, width) => updatePrefs(setWidth(prefs, field, width === undefined ? undefined : clampWidth(width))),
    onReorder: (field, before) => updatePrefs(placeColumn(prefs, field, before)),
    onToggleSelected: toggleSelected,
    onToggleAll: (selected) => store.getState().selectMany(rowIds, selected),
    onOpenRow: (rowId) => openRow(rowId, 'edit'),
    onCommand: (command, row) => runOn(command, [row]),
    onEditorChange: editorChange,
    onEditorCommit: commitEditing,
    onEditorCancel: cancelEditing,
    onLoadMore: () => {
      if (list.hasNextPage && !list.isFetchingNextPage) void list.fetchNextPage();
    },
  };


  // Los manejadores que se pasan a las filas son estables: delegan en la última versión.
  const handlersRef = useLatest(latestHandlers);
  const handlers = useMemo<TableHandlers>(
    () => ({
      onCellFocus: (...args) => handlersRef.current.onCellFocus(...args),
      onCellMouseDown: (...args) => handlersRef.current.onCellMouseDown(...args),
      onSort: (...args) => handlersRef.current.onSort(...args),
      onResize: (...args) => handlersRef.current.onResize(...args),
      onReorder: (...args) => handlersRef.current.onReorder(...args),
      onToggleSelected: (...args) => handlersRef.current.onToggleSelected(...args),
      onToggleAll: (...args) => handlersRef.current.onToggleAll(...args),
      onOpenRow: (...args) => handlersRef.current.onOpenRow(...args),
      onCommand: (...args) => handlersRef.current.onCommand(...args),
      onEditorChange: (...args) => handlersRef.current.onEditorChange(...args),
      onEditorCommit: (...args) => handlersRef.current.onEditorCommit(...args),
      onEditorCancel: (...args) => handlersRef.current.onEditorCancel(...args),
      onLoadMore: (...args) => handlersRef.current.onLoadMore(...args),
    }),
    [handlersRef],
  );

  const hasFilters = filtered;
  const emptyMessage = ext.emptyState?.message ? text(ext.emptyState.message) : t('empty.none');
  const canUndo = state.undoStack.length > 0;
  const canRedo = state.redoStack.length > 0;

  return (
    <div
      data-kx-ui="grid"
      data-kx-save-mode={saveMode}
      data-kx-editable={editable ? 'true' : 'false'}
      data-kx-ready={list.isPending ? undefined : 'true'}
      data-kx-batch={showBatchBar ? '' : undefined}
    >
      <GridShortcuts handlers={shortcutHandlers} />
      <Toolbar data-kx-help="grid.toolbar">
        <FilterBar env={env} query={query} onChange={updateQuery} searchRef={searchRef} />
        <span data-kx-part="spacer" />
        {editable ? (
          <div role="group" aria-label={t('history.label')} data-kx-part="history" data-kx-help="grid.undo">
            <Action
              kind="secondary"
              icon="undo"
              aria-label={t('history.undoLabel')}
              disabledReason={canUndo ? undefined : t('history.noUndo')}
              onClick={() => handleHistory('undo')}
            />
            <Action
              kind="secondary"
              icon="redo"
              aria-label={t('history.redoLabel')}
              disabledReason={canRedo ? undefined : t('history.noRedo')}
              onClick={() => handleHistory('redo')}
            />
          </div>
        ) : null}
        {canCreate ? (
          <Action kind="secondary" icon="plus" onClick={addRow}>
            {t('toolbar.add')}
          </Action>
        ) : null}
        <ColumnsMenu env={env} columns={config.columns} prefs={prefs} fitHidden={fitHidden} onChange={updatePrefs} />
      </Toolbar>

      <SelectionBar
        env={env}
        count={selectedRows.length}
        commands={bulk}
        busy={commandBusy}
        onRun={(command) => runOn(command, selectedRows)}
        onClear={() => store.getState().clearSelection()}
      />
      <ConflictNotice env={env} rows={conflictRows} busyId={conflictBusy} onReapply={(id) => void reapply(id)} />

      {list.isError ? (
        <Feedback tone="error">
          <Icon name="warning-circle" />
          <span>{errorText(list.error)}</span>
          <Action kind="secondary" icon="refresh" onClick={() => void list.refetch()}>
            {t('error.retry')}
          </Action>
        </Feedback>
      ) : null}

      <GridTable
        env={env}
        view={view}
        handlers={handlers}
        layout={layout}
        sort={query.sort}
        scrollRef={scrollRef}
        probeRef={probeRef}
        menuRowId={menuRowId}
        onMenuChange={onMenuChange}
        onMenuClosed={onMenuClosed}
        onKeyDown={onKeyDown}
        onGridBlur={onGridBlur}
        onPaste={onPaste}
        onCopy={onCopy}
        rowDoubleClick={rowDoubleClick}
      />

      <BatchBar
        ref={batchRef}
        env={env}
        count={dirtyCount}
        blocked={blockedRows.length}
        saving={saving}
        report={report}
        hidden={!showBatchBar}
        onSave={() => void saveEverything()}
        onDiscard={() => {
          store.getState().revertAll();
          setReport(null);
          announce(t('live.discarded'));
        }}
        onDismissReport={() => setReport(null)}
      />

      {!list.isPending && !list.isError && rowIds.length === 0 ? (
        <p data-th="collection-empty">
          {hasFilters ? t('empty.filtered') : emptyMessage}
          {hasFilters ? (
            <>
              {' '}
              <Action
                kind="quiet"
                onClick={() => updateQuery({ ...query, search: '', filters: {} })}
              >
                {t('filters.clearAll')}
              </Action>
            </>
          ) : null}
        </p>
      ) : null}

      <footer data-kx-part="footer">
        <span data-kx-part="footer-count">{list.isPending ? '' : t('footer.count', { loaded: serverRows.length, total })}</span>
        {list.isFetchingNextPage ? <Spinner label={t('footer.loadingMore')} /> : null}
        {/* Siempre en el árbol (vacío si no hay ocultas) para que el cambio se anuncie por la región viva. */}
        <span data-kx-part="fit-count" aria-live="polite">
          {fitHidden.length > 0 ? (
            <>
              <span data-kx-part="fit-badge" aria-hidden="true" title={t('fit.hidden', { count: fitHidden.length })}>
                +{fitHidden.length}
              </span>
              <span data-kx-part="sr-only">{t('fit.hidden', { count: fitHidden.length })}</span>
            </>
          ) : null}
        </span>
      </footer>

      <div data-kx-part="live" role="status" aria-live="polite" aria-atomic="true">
        <span data-kx-part="sr-only">{[announcement, summary].filter(Boolean).join(' ')}</span>
      </div>

      <Suspense fallback={null}>
        {plan ? <PastePreview env={env} plan={plan} onApply={applyPaste} onCancel={() => setPlan(null)} /> : null}
        {conflictChoice ? (
          <ConflictDialog env={env} data={conflictChoice} onChoose={chooseConflict} onCancel={() => setConflictChoice(null)} />
        ) : null}
      </Suspense>

      {pending?.kind === 'confirm' ? (
        <ConfirmDialog
          open
          title={text(pending.command.label)}
          message={
            pending.command.confirm
              ? text(pending.command.confirm)
              : t('confirm.default', { label: text(pending.command.label), count: pending.rows.length })
          }
          destructive={pending.command.kind === 'delete' || pending.command.tone === 'error'}
          confirmLabel={text(pending.command.label)}
          onOpenChange={(open) => {
            if (!open) setPending(null);
          }}
          onConfirm={() => void execute(pending.command, pending.rows)}
        />
      ) : null}
      {pending?.kind === 'prompt' && pending.command.prompt ? (
        <PromptDialog
          open
          title={text(pending.command.label)}
          label={text(pending.command.prompt.label)}
          required={pending.command.prompt.required}
          maxLength={pending.command.prompt.maxLength}
          submitLabel={text(pending.command.label)}
          onOpenChange={(open) => {
            if (!open) setPending(null);
          }}
          onSubmit={(value) => {
            const prompt = pending.command.prompt;
            if (prompt) void execute(pending.command, pending.rows, { [prompt.field]: value });
          }}
        />
      ) : null}
    </div>
  );
}
