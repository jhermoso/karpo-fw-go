// Todas las filas de una entidad (todas las páginas de la lista): el organigrama y el mapa
// necesitan el conjunto entero para plegar, contar y agregar.
import { useEffect, useMemo } from 'react';
import { flattenPages, useEntityList } from '../api';
import type { EntityConfig, ListQuery, Row } from '../contracts';

/** Tope de seguridad: más filas que esto no caben en un organigrama ni en una coropleta útil. */
export const ALL_ROWS_LIMIT = 20_000;
const PAGE_SIZE = 1000;

export interface AllRows {
  rows: Row[];
  /** Todavía no ha llegado la primera página. */
  loading: boolean;
  /** Siguen llegando páginas. */
  loadingMore: boolean;
  error: unknown;
  refetch(): void;
}

export function useAllRows(config: EntityConfig, filter: ListQuery['filter'] = EMPTY_FILTER): AllRows {
  const query = useMemo<ListQuery>(() => ({ page: 1, pageSize: PAGE_SIZE, filter }), [filter]);
  const list = useEntityList(config, query);
  const { hasNextPage, isFetchingNextPage, fetchNextPage, isError } = list;
  const rows = useMemo(() => flattenPages(list.data), [list.data]);
  const capped = rows.length >= ALL_ROWS_LIMIT;
  useEffect(() => {
    if (hasNextPage && !isFetchingNextPage && !isError && !capped) void fetchNextPage();
  }, [hasNextPage, isFetchingNextPage, isError, capped, fetchNextPage]);
  return {
    rows,
    loading: list.isPending,
    loadingMore: Boolean(hasNextPage) && !capped && !isError,
    error: list.error,
    refetch: () => void list.refetch(),
  };
}

const EMPTY_FILTER: ListQuery['filter'] = {};
