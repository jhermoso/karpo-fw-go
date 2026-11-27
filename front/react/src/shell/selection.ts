// Selección de filas publicada por la vista activa (cuadrícula, árbol…) para que el marco la use en
// la paleta de órdenes («órdenes de la selección»). Es un almacén de módulo, sin contexto de React,
// para que una vista pueda publicar sin depender de ningún proveedor del marco.
import { useSyncExternalStore } from 'react';
import type { Row } from '../contracts';

interface SelectionState {
  entity: string | null;
  rows: readonly Row[];
}

const EMPTY: readonly Row[] = [];
let state: SelectionState = { entity: null, rows: EMPTY };
const listeners = new Set<() => void>();

function emit(): void {
  for (const listener of [...listeners]) listener();
}

/** La vista publica sus filas seleccionadas; una lista vacía borra la selección de esa entidad. */
export function publishSelection(entity: string, rows: readonly Row[]): void {
  if (rows.length === 0) {
    if (state.entity === entity && state.rows.length > 0) {
      state = { entity: null, rows: EMPTY };
      emit();
    }
    return;
  }
  state = { entity, rows };
  emit();
}

export function clearSelection(entity?: string): void {
  if (state.rows.length === 0) return;
  if (entity !== undefined && state.entity !== entity) return;
  state = { entity: null, rows: EMPTY };
  emit();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Filas seleccionadas de la entidad dada (vacío si la selección es de otra). */
export function useSelection(entity: string | undefined): readonly Row[] {
  const snapshot = useSyncExternalStore(
    subscribe,
    () => state,
    () => state,
  );
  return entity !== undefined && snapshot.entity === entity ? snapshot.rows : EMPTY;
}
