// Almacén de edición de la cuadrícula (PLAN §4.9): zustand + immer, uno por entidad.
//
// Tres capas de valor (de abajo arriba):
//   servidor   `server`   las filas tal como las devolvió el servidor (se sincronizan desde la caché);
//   pendiente  `rows`     lo confirmado y aún no guardado (o guardado y liquidado): el documento que
//                         deshacer/rehacer recorre con parches de immer (`produceWithPatches`);
//   borrador   `editing`  lo que se está escribiendo en la celda abierta, aún sin confirmar.
//
// El valor efectivo de una celda es borrador → pendiente → servidor. «Sucio» = el pendiente difiere del
// servidor (se calcula, no se guarda): así un cambio guardado y vuelto a deshacer vuelve a estar sucio y
// pide otro PUT, que es lo que PLAN §4.9 exige («deshacer algo guardado = otro PUT»).
//
// Invariante que hace válidos los parches: un campo tocado se «cebima» antes (valor del servidor en
// `values`), de modo que toda edición es un `replace` sobre una ruta existente y nunca se borra una ruta
// que un parche posterior necesite (ni siquiera tras guardar: `settle` reescribe el valor, no la ruta).
import { applyPatches, enablePatches, produceWithPatches, type Patch } from 'immer';
import { createStore, type StoreApi } from 'zustand/vanilla';
import type { FieldKind, Row } from '../contracts';
import { emptyValueFor, valuesEqual } from './store-values';

enablePatches();

export type RowId = string;

/** Cambios de una fila: valores de los campos tocados y la versión leída al empezar. */
export interface RowOverride {
  values: Record<string, unknown>;
  /** `version` del servidor cuando se empezó a editar; es la que viaja en el PUT. */
  baseVersion?: number;
  isNew?: boolean;
}

export interface CellIssue {
  code: string;
  /** `local`: la dio `validate`; `server`: llegó en `fieldErrors`. */
  source: 'local' | 'server';
  message?: string;
}

export type RowPhase = 'saving' | 'saved' | 'error' | 'conflict' | 'pending';

export interface RowStatus {
  phase: RowPhase;
  message?: string;
}

export interface EditingCell {
  rowId: RowId;
  field: string;
  draft: unknown;
  /** Valor efectivo al abrir el editor: Esc vuelve a él. */
  original: unknown;
  /** Se abrió escribiendo encima (el editor parte del texto tecleado). */
  overwrite: boolean;
}

export type HistoryKind = 'edit' | 'clear' | 'paste' | 'fill' | 'revert' | 'revertRow' | 'revertAll';

export interface CellRef {
  rowId: RowId;
  field: string;
}

export interface HistoryEntry {
  kind: HistoryKind;
  patches: Patch[];
  inverse: Patch[];
  /** Celdas tocadas, para el anuncio y para volver a guardar tras deshacer. */
  cells: CellRef[];
}

export interface GridSchema {
  /** Tipo de cada campo editable (para comparar y vaciar). */
  kinds: Record<string, FieldKind>;
  /** Códigos de validación de un valor (vacío = válido). */
  validate(field: string, value: unknown, row: Record<string, unknown>): string[];
}

export interface CellEdit {
  rowId: RowId;
  field: string;
  value: unknown;
}

export interface HistoryResult {
  kind: HistoryKind;
  rowIds: RowId[];
  cells: CellRef[];
}

export interface RebasePreview {
  /** Campos que yo he cambiado (respecto a lo que leí). */
  mine: string[];
  /** Campos que han cambiado en el servidor desde que leí. */
  theirs: string[];
  /** Los dos: sólo éstos obligan a elegir. */
  common: string[];
}

export interface GridState {
  server: Record<RowId, Row>;
  rows: Record<RowId, RowOverride>;
  editing: EditingCell | null;
  status: Record<RowId, RowStatus>;
  issues: Record<RowId, Record<string, CellIssue>>;
  undoStack: HistoryEntry[];
  redoStack: HistoryEntry[];
  selected: Record<RowId, true>;
  /** Filas nuevas (altas en la cuadrícula), la más reciente primero. */
  newRows: RowId[];

  configure(schema: GridSchema): void;
  syncServer(rows: readonly Row[]): void;

  beginEdit(rowId: RowId, field: string, options?: { initial?: unknown; overwrite?: boolean }): void;
  setDraft(value: unknown): void;
  cancelEdit(): void;
  /** Confirma el borrador: lo escribe como cambio pendiente (con su incidencia si no es válido). */
  commitEdit(): { changed: boolean; issue?: string; cell?: CellRef };

  setValue(rowId: RowId, field: string, value: unknown, kind?: HistoryKind): void;
  applyEdits(edits: readonly CellEdit[], kind: HistoryKind): HistoryResult | null;
  clearCell(rowId: RowId, field: string): void;
  revertCell(rowId: RowId, field: string): void;
  revertRow(rowId: RowId): void;
  revertAll(): void;

  undo(): HistoryResult | null;
  redo(): HistoryResult | null;

  addRow(initial?: Record<string, unknown>): RowId;
  discardRow(rowId: RowId): void;

  setStatus(rowId: RowId, status: RowStatus | null): void;
  setIssues(rowId: RowId, issues: Record<string, CellIssue>): void;
  clearIssues(rowId: RowId): void;
  clearIssue(rowId: RowId, field: string): void;
  /** Valida todos los campos dados de la fila (altas): marca las incidencias; `true` si todo es válido. */
  validateRow(rowId: RowId, fields: readonly string[]): boolean;
  /** El servidor aceptó la fila: la capa de servidor se actualiza y el pendiente se liquida. */
  settle(rowId: RowId, saved: Row): void;
  /** Tras un 409: lo que cambió cada parte (sin tocar nada). */
  previewRebase(rowId: RowId, fresh: Row): RebasePreview;
  /** Adopta la fila fresca como base; en `takeTheirs` se descartan mis cambios a favor del servidor. */
  applyRebase(rowId: RowId, fresh: Row, takeTheirs: readonly string[]): void;

  toggleSelected(rowId: RowId, force?: boolean): void;
  selectMany(rowIds: readonly RowId[], selected: boolean): void;
  clearSelection(): void;
}

export type GridStore = StoreApi<GridState>;

const HISTORY_LIMIT = 200;
const NO_KINDS: Record<string, FieldKind> = {};

// ---------------------------------------------------------------------------------------------
// Lecturas puras (las usan los componentes y las pruebas)
// ---------------------------------------------------------------------------------------------

type Layers = Pick<GridState, 'rows' | 'server'>;

export function effectiveValue(state: Layers, rowId: RowId, field: string): unknown {
  const override = state.rows[rowId];
  if (override && field in override.values) return override.values[field];
  return state.server[rowId]?.[field];
}

/** La fila con los cambios pendientes aplicados. */
export function effectiveRow(state: Layers, rowId: RowId): Row | undefined {
  const base = state.server[rowId];
  const override = state.rows[rowId];
  if (!override) return base;
  return { ...(base ?? { id: rowId }), ...override.values } as Row;
}

/** Campos pendientes que difieren del servidor (todos, en una fila nueva). */
export function dirtyFields(state: Layers, rowId: RowId, kinds: Record<string, FieldKind> = NO_KINDS): string[] {
  const override = state.rows[rowId];
  if (!override) return [];
  const server = state.server[rowId];
  return Object.keys(override.values).filter(
    (field) => override.isNew || !valuesEqual(override.values[field], server?.[field], kinds[field]),
  );
}

/** ¿Difiere de lo que leyó el servidor el valor pendiente de un campo (siempre, en una fila nueva)? */
export function isFieldDirty(
  override: RowOverride | undefined,
  server: Row | undefined,
  field: string,
  kinds: Record<string, FieldKind> = NO_KINDS,
): boolean {
  if (!override || !(field in override.values)) return false;
  return override.isNew === true || !valuesEqual(override.values[field], server?.[field], kinds[field]);
}

export function isRowDirty(state: Layers, rowId: RowId, kinds: Record<string, FieldKind> = NO_KINDS): boolean {
  const override = state.rows[rowId];
  if (!override) return false;
  return override.isNew === true || dirtyFields(state, rowId, kinds).length > 0;
}

export function dirtyRowIds(state: Layers, kinds: Record<string, FieldKind> = NO_KINDS): RowId[] {
  return Object.keys(state.rows).filter((id) => isRowDirty(state, id, kinds));
}

export function countDirtyCells(state: Layers, kinds: Record<string, FieldKind> = NO_KINDS): number {
  let total = 0;
  for (const id of Object.keys(state.rows)) total += dirtyFields(state, id, kinds).length;
  return total;
}

/** ¿Tiene la fila incidencias que impiden guardarla (locales o del servidor aún sin corregir)? */
export function hasBlockingIssues(state: Pick<GridState, 'issues'>, rowId: RowId): boolean {
  return Object.keys(state.issues[rowId] ?? {}).length > 0;
}

/** Cuerpo del PUT (sólo los campos sucios) y versión leída; `undefined` si no hay nada que enviar. */
export function buildChange(
  state: Layers,
  rowId: RowId,
  kinds: Record<string, FieldKind> = NO_KINDS,
): { patch: Record<string, unknown>; version: number | undefined; isNew: boolean } | undefined {
  const override = state.rows[rowId];
  if (!override) return undefined;
  const fields = dirtyFields(state, rowId, kinds);
  if (fields.length === 0) return undefined;
  const patch: Record<string, unknown> = {};
  for (const field of fields) patch[field] = override.values[field];
  return {
    patch,
    version: override.baseVersion ?? state.server[rowId]?.version,
    isNew: override.isNew === true,
  };
}

function cellsOfPatches(patches: readonly Patch[]): CellRef[] {
  const seen = new Set<string>();
  const out: CellRef[] = [];
  for (const patch of patches) {
    const [root, rowId, group, field] = patch.path as (string | number)[];
    if (root !== 'rows' || typeof rowId !== 'string' || group !== 'values' || typeof field !== 'string') continue;
    const key = `${rowId}\u0000${field}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push({ rowId, field });
  }
  return out;
}

// ---------------------------------------------------------------------------------------------
// Fábrica
// ---------------------------------------------------------------------------------------------

export function createGridStore(initialSchema?: Partial<GridSchema>): GridStore {
  let schema: GridSchema = {
    kinds: initialSchema?.kinds ?? NO_KINDS,
    validate: initialSchema?.validate ?? (() => []),
  };
  let newCounter = 0;

  return createStore<GridState>()((set, get) => {
    const kinds = () => schema.kinds;

    /** Recalcula la incidencia local de una celda (la del servidor ya se retiró al escribir). */
    function revalidate(cells: readonly CellRef[]): void {
      const state = get();
      const issues = { ...state.issues };
      for (const { rowId, field } of cells) {
        const row = { ...(state.server[rowId] ?? {}), ...(state.rows[rowId]?.values ?? {}) };
        const dirty = state.rows[rowId]?.isNew === true || dirtyFields(state, rowId, kinds()).includes(field);
        const codes = dirty ? schema.validate(field, effectiveValue(state, rowId, field), row) : [];
        const forRow = { ...(issues[rowId] ?? {}) };
        const code = codes[0];
        if (code) forRow[field] = { code, source: 'local' };
        else if (forRow[field]?.source === 'local') delete forRow[field];
        if (Object.keys(forRow).length > 0) issues[rowId] = forRow;
        else delete issues[rowId];
      }
      set({ issues });
    }

    /** Escribe el valor del servidor en `values` para los campos que aún no se habían tocado. */
    function prime(fieldsByRow: Map<RowId, string[]>): void {
      const state = get();
      const rows = { ...state.rows };
      let changed = false;
      for (const [rowId, fields] of fieldsByRow) {
        const server = state.server[rowId];
        const current = rows[rowId];
        const values = { ...(current?.values ?? {}) };
        let touched = current === undefined;
        for (const field of fields) {
          if (!(field in values)) {
            values[field] = server?.[field] ?? null;
            touched = true;
          }
        }
        if (touched) {
          rows[rowId] = { ...(current ?? { baseVersion: server?.version }), values };
          changed = true;
        }
      }
      if (changed) set({ rows });
    }

    function resetPhaseAfterEdit(rowIds: readonly RowId[]): void {
      const state = get();
      let status = state.status;
      for (const rowId of rowIds) {
        const phase = status[rowId]?.phase;
        if (phase === 'saved' || phase === 'error' || phase === 'pending') {
          status = { ...status };
          delete status[rowId];
        }
      }
      if (status !== state.status) set({ status });
    }

    function dropServerIssues(cells: readonly CellRef[]): void {
      const issues = { ...get().issues };
      let changed = false;
      for (const { rowId, field } of cells) {
        const forRow = issues[rowId];
        if (forRow?.[field]?.source !== 'server') continue;
        const copy = { ...forRow };
        delete copy[field];
        if (Object.keys(copy).length > 0) issues[rowId] = copy;
        else delete issues[rowId];
        changed = true;
      }
      if (changed) set({ issues });
    }

    function applyEdits(edits: readonly CellEdit[], kind: HistoryKind): HistoryResult | null {
      if (edits.length === 0) return null;
      const fieldsByRow = new Map<RowId, string[]>();
      for (const edit of edits) fieldsByRow.set(edit.rowId, [...(fieldsByRow.get(edit.rowId) ?? []), edit.field]);
      prime(fieldsByRow);

      const [next, patches, inverse] = produceWithPatches({ rows: get().rows }, (draft) => {
        for (const edit of edits) {
          const entry = draft.rows[edit.rowId];
          if (entry) entry.values[edit.field] = edit.value;
        }
      });
      const cells = edits.map(({ rowId, field }) => ({ rowId, field }));
      dropServerIssues(cells);
      if (patches.length === 0) return null;
      const state = get();
      set({
        rows: next.rows,
        undoStack: [...state.undoStack, { kind, patches, inverse, cells: cellsOfPatches(patches) }].slice(-HISTORY_LIMIT),
        redoStack: [],
      });
      revalidate(cells);
      const rowIds = [...fieldsByRow.keys()];
      resetPhaseAfterEdit(rowIds);
      return { kind, rowIds, cells };
    }

    function replay(direction: 'undo' | 'redo'): HistoryResult | null {
      const state = get();
      const from = direction === 'undo' ? state.undoStack : state.redoStack;
      const entry = from[from.length - 1];
      if (!entry) return null;
      const rest = from.slice(0, -1);
      let nextRows: Record<RowId, RowOverride>;
      try {
        nextRows = (applyPatches({ rows: state.rows }, direction === 'undo' ? entry.inverse : entry.patches) as {
          rows: Record<RowId, RowOverride>;
        }).rows;
      } catch {
        // Una ruta que ya no existe (fila nueva descartada): se tira el paso y se sigue con el anterior.
        set(direction === 'undo' ? { undoStack: rest } : { redoStack: rest });
        return replay(direction);
      }
      const other = direction === 'undo' ? state.redoStack : state.undoStack;
      set(
        direction === 'undo'
          ? { rows: nextRows, undoStack: rest, redoStack: [...other, entry] }
          : { rows: nextRows, redoStack: rest, undoStack: [...other, entry] },
      );
      dropServerIssues(entry.cells);
      revalidate(entry.cells);
      const rowIds = [...new Set(entry.cells.map((cell) => cell.rowId))];
      resetPhaseAfterEdit(rowIds);
      return { kind: entry.kind, rowIds, cells: entry.cells };
    }

    function scrubHistory(rowId: RowId): void {
      const own = (patch: Patch) => patch.path[1] === rowId;
      const clean = (stack: HistoryEntry[]): HistoryEntry[] =>
        stack
          .map((entry) => ({
            ...entry,
            patches: entry.patches.filter((patch) => !own(patch)),
            inverse: entry.inverse.filter((patch) => !own(patch)),
            cells: entry.cells.filter((cell) => cell.rowId !== rowId),
          }))
          .filter((entry) => entry.patches.length > 0);
      set({ undoStack: clean(get().undoStack), redoStack: clean(get().redoStack) });
    }

    function dirtyCellsOf(rowId: RowId): CellRef[] {
      return dirtyFields(get(), rowId, kinds()).map((field) => ({ rowId, field }));
    }

    function serverValue(rowId: RowId, field: string): unknown {
      const value = get().server[rowId]?.[field];
      return value === undefined ? emptyValueFor(kinds()[field]) : value;
    }

    return {
      server: {},
      rows: {},
      editing: null,
      status: {},
      issues: {},
      undoStack: [],
      redoStack: [],
      selected: {},
      newRows: [],

      configure(next) {
        schema = next;
      },

      syncServer(incoming) {
        const state = get();
        const server = { ...state.server };
        let rows = state.rows;
        for (const row of incoming) {
          const previous = state.server[row.id];
          server[row.id] = row;
          const override = rows[row.id];
          if (!override || override.isNew) continue;
          // Un cambio ya liquidado (igual a lo que leí) se alinea con la fila nueva; uno sucio se respeta
          // (si el servidor cambió mientras tanto, el PUT dará 409 y se resolverá entonces).
          const dirty = Object.keys(override.values).some(
            (field) => !valuesEqual(override.values[field], previous?.[field] ?? row[field], kinds()[field]),
          );
          if (dirty) continue;
          const values = { ...override.values };
          for (const field of Object.keys(values)) values[field] = row[field] ?? null;
          if (rows === state.rows) rows = { ...rows };
          rows[row.id] = { ...override, values, baseVersion: row.version };
        }
        set({ server, rows });
      },

      beginEdit(rowId, field, options) {
        const original = effectiveValue(get(), rowId, field);
        set({
          editing: {
            rowId,
            field,
            draft: options && 'initial' in options ? options.initial : original,
            original,
            overwrite: options?.overwrite === true,
          },
        });
      },

      setDraft(value) {
        const editing = get().editing;
        if (editing) set({ editing: { ...editing, draft: value } });
      },

      cancelEdit() {
        set({ editing: null });
      },

      commitEdit() {
        const editing = get().editing;
        if (!editing) return { changed: false };
        const { rowId, field, draft } = editing;
        set({ editing: null });
        const cell = { rowId, field };
        if (valuesEqual(draft, effectiveValue(get(), rowId, field), kinds()[field])) return { changed: false, cell };
        applyEdits([{ rowId, field, value: draft }], 'edit');
        return { changed: true, issue: get().issues[rowId]?.[field]?.code, cell };
      },

      setValue(rowId, field, value, kind = 'edit') {
        applyEdits([{ rowId, field, value }], kind);
      },

      applyEdits,

      clearCell(rowId, field) {
        applyEdits([{ rowId, field, value: emptyValueFor(kinds()[field]) }], 'clear');
      },

      revertCell(rowId, field) {
        if (get().rows[rowId]?.isNew) return;
        if (!dirtyFields(get(), rowId, kinds()).includes(field)) return;
        applyEdits([{ rowId, field, value: serverValue(rowId, field) }], 'revert');
      },

      revertRow(rowId) {
        if (get().rows[rowId]?.isNew) {
          get().discardRow(rowId);
          return;
        }
        applyEdits(
          dirtyCellsOf(rowId).map(({ field }) => ({ rowId, field, value: serverValue(rowId, field) })),
          'revertRow',
        );
      },

      revertAll() {
        const state = get();
        const edits: CellEdit[] = [];
        for (const rowId of Object.keys(state.rows)) {
          if (state.rows[rowId]?.isNew) continue;
          for (const { field } of dirtyCellsOf(rowId)) edits.push({ rowId, field, value: serverValue(rowId, field) });
        }
        applyEdits(edits, 'revertAll');
        for (const rowId of [...get().newRows]) get().discardRow(rowId);
      },

      undo() {
        return replay('undo');
      },

      redo() {
        return replay('redo');
      },

      addRow(initial = {}) {
        newCounter += 1;
        const rowId = `new:${newCounter}`;
        set((state) => ({
          rows: { ...state.rows, [rowId]: { values: { ...initial }, isNew: true } },
          newRows: [rowId, ...state.newRows],
        }));
        return rowId;
      },

      discardRow(rowId) {
        const state = get();
        const rows = { ...state.rows };
        delete rows[rowId];
        const issues = { ...state.issues };
        delete issues[rowId];
        const status = { ...state.status };
        delete status[rowId];
        const selected = { ...state.selected };
        delete selected[rowId];
        set({
          rows,
          issues,
          status,
          selected,
          newRows: state.newRows.filter((id) => id !== rowId),
          editing: state.editing?.rowId === rowId ? null : state.editing,
        });
        scrubHistory(rowId);
      },

      setStatus(rowId, status) {
        const next = { ...get().status };
        if (status) next[rowId] = status;
        else delete next[rowId];
        set({ status: next });
      },

      setIssues(rowId, issues) {
        set({ issues: { ...get().issues, [rowId]: { ...(get().issues[rowId] ?? {}), ...issues } } });
      },

      clearIssues(rowId) {
        const issues = { ...get().issues };
        delete issues[rowId];
        set({ issues });
      },

      clearIssue(rowId, field) {
        const forRow = { ...(get().issues[rowId] ?? {}) };
        delete forRow[field];
        const issues = { ...get().issues };
        if (Object.keys(forRow).length > 0) issues[rowId] = forRow;
        else delete issues[rowId];
        set({ issues });
      },

      validateRow(rowId, fields) {
        const state = get();
        const row = { ...(state.server[rowId] ?? {}), ...(state.rows[rowId]?.values ?? {}) };
        const found: Record<string, CellIssue> = {};
        for (const field of fields) {
          const code = schema.validate(field, row[field], row)[0];
          if (code) found[field] = { code, source: 'local' };
        }
        const ok = Object.keys(found).length === 0;
        if (!ok) set({ issues: { ...state.issues, [rowId]: { ...(state.issues[rowId] ?? {}), ...found } } });
        return ok;
      },

      settle(rowId, saved) {
        const state = get();
        const server = { ...state.server, [saved.id]: saved };
        const rows = { ...state.rows };
        const override = rows[rowId];
        if (override) {
          const values = { ...override.values };
          for (const field of Object.keys(values)) if (field in saved) values[field] = saved[field];
          rows[rowId] = { ...override, values, baseVersion: saved.version, isNew: false };
        }
        const issues = { ...state.issues };
        delete issues[rowId];
        set({ server, rows, issues, status: { ...state.status, [rowId]: { phase: 'saved' } } });
      },

      previewRebase(rowId, fresh) {
        const state = get();
        const old = state.server[rowId];
        const mine = dirtyFields(state, rowId, kinds());
        const fields = new Set([...Object.keys(old ?? {}), ...Object.keys(fresh)]);
        const theirs = [...fields].filter(
          (field) => field !== 'version' && !valuesEqual(fresh[field], old?.[field], kinds()[field]),
        );
        return { mine, theirs, common: mine.filter((field) => theirs.includes(field)) };
      },

      applyRebase(rowId, fresh, takeTheirs) {
        const state = get();
        const old = state.server[rowId];
        const mine = new Set(dirtyFields(state, rowId, kinds()));
        const server = { ...state.server, [rowId]: fresh };
        const rows = { ...state.rows };
        const override = rows[rowId];
        if (override) {
          const values = { ...override.values };
          for (const field of Object.keys(values)) {
            // Lo que no he tocado (o cedo al servidor) se alinea con la fila fresca.
            if (takeTheirs.includes(field) || !mine.has(field) || valuesEqual(values[field], old?.[field], kinds()[field])) {
              values[field] = fresh[field] ?? null;
            }
          }
          rows[rowId] = { ...override, values, baseVersion: fresh.version };
        }
        const status = { ...state.status };
        delete status[rowId];
        const issues = { ...state.issues };
        delete issues[rowId];
        set({ server, rows, status, issues });
        revalidate(Object.keys(rows[rowId]?.values ?? {}).map((field) => ({ rowId, field })));
      },

      toggleSelected(rowId, force) {
        const selected = { ...get().selected };
        const on = force ?? !selected[rowId];
        if (on) selected[rowId] = true;
        else delete selected[rowId];
        set({ selected });
      },

      selectMany(rowIds, on) {
        const selected = { ...get().selected };
        for (const id of rowIds) {
          if (on) selected[id] = true;
          else delete selected[id];
        }
        set({ selected });
      },

      clearSelection() {
        set({ selected: {} });
      },
    };
  });
}

// ---------------------------------------------------------------------------------------------
// Registro: un almacén por entidad, para que los cambios pendientes sobrevivan a la navegación
// ---------------------------------------------------------------------------------------------

const registry = new Map<string, GridStore>();

export function getGridStore(entity: string): GridStore {
  let store = registry.get(entity);
  if (!store) {
    store = createGridStore();
    registry.set(entity, store);
  }
  return store;
}

/** Olvida todos los almacenes (pruebas, cierre de sesión). */
export function resetGridStores(): void {
  registry.clear();
}
