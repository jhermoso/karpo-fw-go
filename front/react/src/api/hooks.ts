// Ganchos de react-query sobre el adaptador (claves de `contracts.queryKeys`).
import {
  useInfiniteQuery,
  useQuery,
  type InfiniteData,
  type QueryClient,
  type UseInfiniteQueryResult,
  type UseQueryResult,
} from '@tanstack/react-query';
import { useContext, useMemo } from 'react';
import {
  ApiContext,
  ManifestContext,
  queryKeys,
  type EntityApi,
  type EntityConfig,
  type ListQuery,
  type Page,
  type Row,
} from '../contracts';
import { isApiError } from './errors';
import type { EntityApiExt } from './entity-api';

/** La API del proveedor `ApiContext`; falla con un mensaje claro si falta. */
export function useEntityApi(): EntityApi {
  const api = useContext(ApiContext);
  if (!api) throw new Error('Falta el proveedor de la API (`ApiContext`): envuelve la app en <KarpoApp> o en un ApiContext.Provider.');
  return api;
}

/** Igual que `useEntityApi`, con las rutas auxiliares (catálogos, organigrama, geografía). */
export function useEntityApiExt(): EntityApiExt {
  return useEntityApi() as EntityApiExt;
}

/** Lista con desplazamiento infinito: `query.page` es la primera página; cada página trae `pageSize` filas. */
export function useEntityList(
  config: EntityConfig,
  query: ListQuery,
): UseInfiniteQueryResult<InfiniteData<Page, number>> {
  const api = useEntityApi();
  return useInfiniteQuery({
    queryKey: queryKeys.list(config.id, query),
    queryFn: ({ pageParam, signal }) => api.list(config, { ...query, page: pageParam }, signal),
    initialPageParam: query.page,
    getNextPageParam: (last) => (last.page * last.pageSize < last.total && last.items.length > 0 ? last.page + 1 : undefined),
  });
}

/** Una sola página (sin desplazamiento infinito); comparte el prefijo de claves de las listas. */
export function useEntityPage(config: EntityConfig, query: ListQuery): UseQueryResult<Page> {
  const api = useEntityApi();
  return useQuery({
    queryKey: [...queryKeys.list(config.id, query), 'page'],
    queryFn: ({ signal }) => api.list(config, query, signal),
  });
}

/** Todas las filas de las páginas cargadas, en orden. */
export function flattenPages(data: InfiniteData<Page, number> | undefined): Row[] {
  return data ? data.pages.flatMap((page) => page.items) : [];
}

/** Un registro por identificador (no consulta mientras `id` sea `undefined`). */
export function useEntityRecord(config: EntityConfig, id: string | undefined): UseQueryResult<Row> {
  const api = useEntityApi();
  return useQuery({
    queryKey: queryKeys.record(config.id, id ?? ''),
    queryFn: ({ signal }) => api.get(config, id as string, signal),
    enabled: id !== undefined && id !== '',
  });
}

/** Escribe un registro devuelto por el servidor en las cachés de registro y de listas, sin releerlo. */
export function patchEntityCaches(client: QueryClient, entity: string, row: Row): void {
  client.setQueryData(queryKeys.record(entity, row.id), row);
  client.setQueriesData<unknown>({ queryKey: ['entity', entity, 'list'] }, (current: unknown) => {
    const replace = (items: Row[]) => items.map((item) => (item.id === row.id ? { ...item, ...row } : item));
    if (!current || typeof current !== 'object') return current;
    const data = current as { pages?: Page[]; items?: Row[] };
    if (Array.isArray(data.pages)) {
      return { ...data, pages: data.pages.map((page) => ({ ...page, items: replace(page.items) })) };
    }
    if (Array.isArray(data.items)) return { ...data, items: replace(data.items) };
    return current;
  });
}

// ---------------------------------------------------------------------------------------------
// Etiquetas de claves ajenas, agrupadas por fotograma
// ---------------------------------------------------------------------------------------------

/** Con tantos identificadores de una entidad en el mismo fotograma se pide una página en lugar de uno a uno. */
export const LABEL_BULK_THRESHOLD = 5;
const LABEL_CONCURRENCY = 6;

type Frame = (callback: () => void) => void;

const defaultFrame: Frame = (callback) => {
  if (typeof requestAnimationFrame === 'function') requestAnimationFrame(() => callback());
  else setTimeout(callback, 16);
};

interface Waiter {
  resolve(label: string | undefined): void;
  reject(error: unknown): void;
}

export interface LabelBatcher {
  enqueue(entity: string, id: string): Promise<string | undefined>;
}

function labelOf(row: Row, config: EntityConfig): string {
  const value = row[config.displayField] ?? row.name ?? row.id;
  return String(value);
}

/**
 * Agrupa las peticiones de etiquetas del mismo fotograma: ids repetidos se piden una vez; si hay
 * muchos de una entidad se lee una página y sólo lo que falte se pide uno a uno (Go no tiene un
 * filtro por varios identificadores).
 */
export function createLabelBatcher(
  api: EntityApi,
  getConfig: (entity: string) => EntityConfig | undefined,
  frame: Frame = defaultFrame,
): LabelBatcher {
  const pending = new Map<string, Map<string, Waiter[]>>();
  let scheduled = false;

  async function single(config: EntityConfig, id: string): Promise<string | undefined> {
    try {
      return labelOf(await api.get(config, id), config);
    } catch (error) {
      if (isApiError(error) && error.kind === 'notfound') return undefined;
      throw error;
    }
  }

  async function runPool<T>(items: T[], worker: (item: T) => Promise<void>): Promise<void> {
    let next = 0;
    const lanes = Array.from({ length: Math.min(LABEL_CONCURRENCY, items.length) }, async () => {
      while (next < items.length) {
        const item = items[next++] as T;
        await worker(item);
      }
    });
    await Promise.all(lanes);
  }

  async function flushEntity(entity: string, waiters: Map<string, Waiter[]>): Promise<void> {
    const config = getConfig(entity);
    const settle = (id: string, run: () => Promise<string | undefined> | string | undefined) =>
      Promise.resolve()
        .then(run)
        .then(
          (label) => waiters.get(id)?.forEach((w) => w.resolve(label)),
          (error: unknown) => waiters.get(id)?.forEach((w) => w.reject(error)),
        );
    if (!config) {
      for (const id of waiters.keys()) await settle(id, () => undefined);
      return;
    }
    let remaining = [...waiters.keys()];
    if (remaining.length >= LABEL_BULK_THRESHOLD) {
      try {
        const page = await api.list(config, { page: 1, pageSize: 1000, filter: {} });
        const byId = new Map(page.items.map((row) => [row.id, labelOf(row, config)]));
        const missing: string[] = [];
        for (const id of remaining) {
          const label = byId.get(id);
          if (label !== undefined) await settle(id, () => label);
          else missing.push(id);
        }
        remaining = missing;
      } catch {
        // si la página falla, se intenta uno a uno
      }
    }
    await runPool(remaining, (id) => settle(id, () => single(config, id)));
  }

  function flush(): void {
    scheduled = false;
    const batch = [...pending.entries()];
    pending.clear();
    for (const [entity, waiters] of batch) void flushEntity(entity, waiters);
  }

  return {
    enqueue(entity, id) {
      return new Promise<string | undefined>((resolve, reject) => {
        let byId = pending.get(entity);
        if (!byId) {
          byId = new Map();
          pending.set(entity, byId);
        }
        const list = byId.get(id) ?? [];
        list.push({ resolve, reject });
        byId.set(id, list);
        if (!scheduled) {
          scheduled = true;
          frame(flush);
        }
      });
    },
  };
}

const NO_MANIFEST = {};
const batchers = new WeakMap<EntityApi, WeakMap<object, LabelBatcher>>();

/**
 * Etiqueta (campo de visualización) de un registro referenciado por clave ajena. Las peticiones del
 * mismo fotograma se agrupan y el resultado se cachea con `queryKeys.label`.
 */
export function useLookupLabel(
  entity: string,
  id: string | undefined,
): { label: string | undefined; isLoading: boolean } {
  const api = useContext(ApiContext);
  const manifest = useContext(ManifestContext);
  const batcher = useMemo(() => {
    if (!api) return undefined;
    let byManifest = batchers.get(api);
    if (!byManifest) {
      byManifest = new WeakMap();
      batchers.set(api, byManifest);
    }
    const key = manifest ?? NO_MANIFEST;
    let found = byManifest.get(key);
    if (!found) {
      found = createLabelBatcher(api, (name) => manifest?.entities[name]);
      byManifest.set(key, found);
    }
    return found;
  }, [api, manifest]);
  const enabled = batcher !== undefined && id !== undefined && id !== '';
  const query = useQuery({
    queryKey: queryKeys.label(entity, id ?? ''),
    queryFn: () => (batcher as LabelBatcher).enqueue(entity, id as string),
    enabled,
    staleTime: 5 * 60_000,
    retry: 1,
  });
  return { label: query.data, isLoading: enabled && query.isPending };
}
