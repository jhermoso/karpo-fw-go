// Registro anterior y siguiente (Alt+↑/↓) según la lista que el usuario tenía abierta: se leen de la
// caché de react-query (las listas de la entidad), sin pedir nada nuevo.
import type { Query, QueryClient } from '@tanstack/react-query';
import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import type { Page, Row } from '../contracts';

export interface Neighbors {
  prev?: Row;
  next?: Row;
  /** Posición (desde 1) del registro en la lista cargada y filas cargadas. */
  position?: number;
  loaded?: number;
  /** Filas que tiene la lista en el servidor, si se sabe. */
  total?: number;
}

const EMPTY: Neighbors = {};

function itemsOf(data: unknown): Row[] | undefined {
  if (!data || typeof data !== 'object') return undefined;
  const value = data as { pages?: Page[]; items?: Row[] };
  if (Array.isArray(value.pages)) return value.pages.flatMap((page) => page.items);
  if (Array.isArray(value.items)) return value.items;
  return undefined;
}

function totalOf(data: unknown): number | undefined {
  const value = data as { pages?: Page[]; total?: number };
  if (Array.isArray(value.pages)) return value.pages.at(-1)?.total;
  return typeof value.total === 'number' ? value.total : undefined;
}

/** Vecinos de `id` en la lista de la entidad que se abrió más recientemente y lo contiene. */
export function findNeighbors(client: QueryClient, entity: string, id: string): Neighbors {
  const queries: Query[] = client.getQueryCache().findAll({ queryKey: ['entity', entity, 'list'] });
  const sorted = [...queries].sort((a, b) => b.state.dataUpdatedAt - a.state.dataUpdatedAt);
  for (const query of sorted) {
    const items = itemsOf(query.state.data);
    if (!items) continue;
    const index = items.findIndex((row) => row.id === id);
    if (index < 0) continue;
    return {
      prev: items[index - 1],
      next: items[index + 1],
      position: index + 1,
      loaded: items.length,
      total: totalOf(query.state.data),
    };
  }
  return EMPTY;
}

/** Vecinos del registro, actualizados cuando cambia la caché de listas. */
export function useNeighbors(entity: string, id: string | undefined): Neighbors {
  const client = useQueryClient();
  const [version, setVersion] = useState(0);
  useEffect(
    () =>
      client.getQueryCache().subscribe((event) => {
        const key = event.query.queryKey;
        if (key[0] === 'entity' && key[1] === entity && key[2] === 'list') setVersion((v) => v + 1);
      }),
    [client, entity],
  );
  // `version` fuerza el recálculo tras un cambio en las listas.
  void version;
  return id === undefined ? EMPTY : findNeighbors(client, entity, id);
}
