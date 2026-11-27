// Guardado de la cuadrícula (PLAN §4.9): una fila = un PUT con `version` (Go mezcla como PATCH, A2 §2),
// sin reintentos (D-R14). Sin React: lo usa `GridView` y lo prueban las unitarias del componente.
//
//   optimistic  se guarda al salir de la fila; si el servidor falla (no es un error de campo ni un 409) se
//               revierte la fila (deshacible) y se avisa;
//   batch       «Guardar» envía los PUT en secuencia (D-R15, no atómico) con informe por fila; los fallos
//               no revierten nada: la fila queda pendiente con su error.
import type { QueryClient } from '@tanstack/react-query';
import { NO_FIELD, isApiError, patchEntityCaches, type ApiError } from '../api';
import type { EntityApi, EntityConfig, FieldKind, Row } from '../contracts';
import {
  buildChange,
  dirtyRowIds,
  hasBlockingIssues,
  type GridStore,
  type RebasePreview,
  type RowId,
} from './store';

export interface SaveContext {
  config: EntityConfig;
  api: EntityApi;
  client: QueryClient;
  store: GridStore;
  kinds: Record<string, FieldKind>;
  /** Campos que una fila nueva debe traer rellenos (los `required` editables). */
  requiredFields: readonly string[];
}

export type SaveOutcome =
  | { status: 'saved'; rowId: RowId; row: Row }
  | { status: 'skipped'; rowId: RowId; reason: 'clean' | 'invalid' | 'busy' }
  | { status: 'conflict'; rowId: RowId; error: ApiError }
  | { status: 'invalid'; rowId: RowId; error: ApiError }
  | { status: 'failed'; rowId: RowId; error: unknown; reverted: boolean };

export interface SaveReport {
  outcomes: SaveOutcome[];
  saved: number;
  conflicts: number;
  failed: number;
  skipped: number;
}

function messageOf(error: unknown): string {
  if (isApiError(error)) return error.detail ?? error.title;
  return error instanceof Error ? error.message : String(error);
}

/** Guarda una fila. Nunca lanza: el resultado dice qué pasó y el almacén ya lo refleja. */
export async function saveRow(
  ctx: SaveContext,
  rowId: RowId,
  options: { revertOnFailure: boolean },
): Promise<SaveOutcome> {
  const { store, api, config, client, kinds } = ctx;
  const state = store.getState();
  const change = buildChange(state, rowId, kinds);
  if (!change) return { status: 'skipped', rowId, reason: 'clean' };
  if (state.status[rowId]?.phase === 'saving') return { status: 'skipped', rowId, reason: 'busy' };
  if (hasBlockingIssues(state, rowId)) return { status: 'skipped', rowId, reason: 'invalid' };
  if (change.isNew && !store.getState().validateRow(rowId, ctx.requiredFields)) {
    return { status: 'skipped', rowId, reason: 'invalid' };
  }

  store.getState().setStatus(rowId, { phase: 'saving' });
  try {
    if (change.isNew) {
      const created = await api.create(config, change.patch);
      store.getState().discardRow(rowId);
      store.getState().syncServer([created]);
      void client.invalidateQueries({ queryKey: ['entity', config.id, 'list'] });
      return { status: 'saved', rowId, row: created };
    }
    const saved = await api.update(config, rowId, change.patch, change.version as number);
    store.getState().settle(rowId, saved);
    patchEntityCaches(client, config.id, saved);
    return { status: 'saved', rowId, row: saved };
  } catch (error) {
    return failure(ctx, rowId, error, change.isNew, options.revertOnFailure);
  }
}

function failure(ctx: SaveContext, rowId: RowId, error: unknown, isNew: boolean, revert: boolean): SaveOutcome {
  const { store, config } = ctx;
  const message = messageOf(error);
  if (isApiError(error)) {
    if (error.kind === 'conflict') {
      store.getState().setStatus(rowId, { phase: 'conflict', message });
      return { status: 'conflict', rowId, error };
    }
    const known = new Set(config.columns.map((column) => column.field));
    const forCells = Object.entries(error.fieldErrors).filter(([field]) => field !== NO_FIELD && known.has(field));
    if (forCells.length > 0) {
      const issues: Record<string, { code: string; source: 'server'; message: string }> = {};
      for (const [field, list] of forCells) {
        const first = list[0];
        if (first) issues[field] = { code: first.code, source: 'server', message: first.message };
      }
      store.getState().setIssues(rowId, issues);
      store.getState().setStatus(rowId, { phase: 'error', message });
      return { status: 'invalid', rowId, error };
    }
  }
  // Fallo del servidor o de la red: en optimista se devuelve la fila a lo leído, y «Deshacer» la recupera.
  const reverted = revert && !isNew;
  if (reverted) store.getState().revertRow(rowId);
  store.getState().setStatus(rowId, { phase: 'error', message });
  return { status: 'failed', rowId, error, reverted };
}

/** Guarda todas las filas con cambios, de una en una y en el orden de la cuadrícula. */
export async function saveAll(ctx: SaveContext, options: { revertOnFailure: boolean }): Promise<SaveReport> {
  const ids = dirtyRowIds(ctx.store.getState(), ctx.kinds);
  const outcomes: SaveOutcome[] = [];
  for (const id of ids) outcomes.push(await saveRow(ctx, id, options));
  return summarize(outcomes);
}

export function summarize(outcomes: SaveOutcome[]): SaveReport {
  return {
    outcomes,
    saved: outcomes.filter((o) => o.status === 'saved').length,
    conflicts: outcomes.filter((o) => o.status === 'conflict').length,
    failed: outcomes.filter((o) => o.status === 'failed' || o.status === 'invalid').length,
    skipped: outcomes.filter((o) => o.status === 'skipped' && o.reason === 'invalid').length,
  };
}

export type ReapplyResult =
  | { kind: 'reapplied'; fresh: Row }
  | { kind: 'choose'; fresh: Row; preview: RebasePreview }
  | { kind: 'failed'; error: unknown };

/**
 * «Recargar y reaplicar» tras un 409: relee la fila; si nadie tocó los mismos campos que yo, mis cambios se
 * reaplican sobre la versión nueva; si coinciden, se devuelve la elección para preguntar al usuario.
 */
export async function reloadAndReapply(ctx: SaveContext, rowId: RowId): Promise<ReapplyResult> {
  const { api, config, client, store } = ctx;
  let fresh: Row;
  try {
    fresh = await api.get(config, rowId);
  } catch (error) {
    return { kind: 'failed', error };
  }
  const preview = store.getState().previewRebase(rowId, fresh);
  if (preview.common.length > 0) return { kind: 'choose', fresh, preview };
  store.getState().applyRebase(rowId, fresh, []);
  patchEntityCaches(client, config.id, fresh);
  return { kind: 'reapplied', fresh };
}

/** Aplica la elección del usuario ante un conflicto en los mismos campos. */
export function resolveConflict(
  ctx: SaveContext,
  rowId: RowId,
  fresh: Row,
  choice: 'mine' | 'theirs',
  preview: RebasePreview,
): void {
  ctx.store.getState().applyRebase(rowId, fresh, choice === 'theirs' ? preview.common : []);
  patchEntityCaches(ctx.client, ctx.config.id, fresh);
}
