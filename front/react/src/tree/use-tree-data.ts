// Datos del organigrama (PLAN §4.11): fuente `flat` (lista entera, padre por `parentField`) o
// `chart` (raíces de la lista + organigrama anidado de Go con profundidad limitada y carga
// perezosa al desplegar un nodo frontera).
import { useCallback, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { camelize, expandTemplate, useEntityApi } from '../api';
import type { EntityConfig } from '../contracts';
import {
  buildFromChart,
  buildFromFlat,
  emptyModel,
  mergeChart,
  parentOf,
  parseChart,
  type ChartSourceDef,
  type ChartTree,
  type TreeModel,
  type TreeViewDef,
} from './model';
import { useAllRows } from './use-all-rows';

export interface TreeData {
  model: TreeModel;
  loading: boolean;
  loadingMore: boolean;
  error: unknown;
  refetch(): void;
  /** Identificadores de ramas que se están pidiendo ahora mismo. */
  branchLoading: ReadonlySet<string>;
  /** Pide más profundidad de un nodo frontera (no hace nada con la fuente `flat`). */
  loadBranch(id: string): Promise<void>;
}

/** Con profundidad 1 el organigrama no trae hijos: la carga perezosa necesita al menos dos niveles. */
const MIN_DEPTH = 2;
const CHART_CONCURRENCY = 6;

export function chartDepth(source: ChartSourceDef): number {
  return Math.max(MIN_DEPTH, Math.min(20, Math.round(source.depth)));
}

export function chartPath(source: ChartSourceDef, id: string, depth: number): string {
  return `${expandTemplate(source.endpoint, { id })}?depth=${depth}`;
}

export function useTreeData(config: EntityConfig, view: TreeViewDef): TreeData {
  const api = useEntityApi();
  const all = useAllRows(config);
  const source = view.source;
  const chart = source.type === 'chart' ? source : null;
  const depth = chart ? chartDepth(chart) : 0;

  const rootRows = useMemo(
    () => (chart ? all.rows.filter((row) => parentOf(row, view.parentField) === null) : []),
    [chart, all.rows, view.parentField],
  );

  const fetchChart = useCallback(
    async (id: string, signal?: AbortSignal): Promise<ChartTree | null> => {
      if (!chart) return null;
      const data = await api.raw<unknown>(chartPath(chart, id, depth), signal ? { signal } : undefined);
      return parseChart(camelize(data), chart);
    },
    [api, chart, depth],
  );

  const rootKey = rootRows.map((row) => row.id).join(',');
  const chartsQuery = useQuery({
    queryKey: ['tree', config.id, 'charts', rootKey, depth],
    enabled: chart !== null && rootRows.length > 0,
    staleTime: 60_000,
    queryFn: async ({ signal }) => {
      const result = new Map<string, ChartTree>();
      let next = 0;
      const lanes = Array.from({ length: Math.min(CHART_CONCURRENCY, rootRows.length) }, async () => {
        while (next < rootRows.length) {
          const root = rootRows[next++]!;
          const tree = await fetchChart(root.id, signal);
          if (tree) result.set(root.id, tree);
        }
      });
      await Promise.all(lanes);
      return result;
    },
  });

  // Ramas pedidas al desplegar un nodo frontera, en el orden en que se pidieron.
  const [deeper, setDeeper] = useState<ReadonlyMap<string, ChartTree>>(() => new Map());
  const [branchLoading, setBranchLoading] = useState<ReadonlySet<string>>(() => new Set());
  const [branchError, setBranchError] = useState<unknown>(null);

  const chartError = chartsQuery.error ?? null;
  const chartsPending = chart !== null && rootRows.length > 0 && chartsQuery.isPending && chartError === null;

  const charts = chartsQuery.data;
  const base = useMemo<TreeModel>(() => {
    if (!chart) return buildFromFlat(all.rows, view.parentField);
    return buildFromChart(rootRows, charts ?? new Map<string, ChartTree>(), depth);
  }, [chart, all.rows, view.parentField, rootRows, charts, depth]);

  const model = useMemo(() => {
    let current = base;
    for (const branch of deeper.values()) current = mergeChart(current, branch, depth);
    return current;
  }, [base, deeper, depth]);

  const loadBranch = useCallback(
    async (id: string) => {
      if (!chart) return;
      setBranchLoading((prev) => new Set(prev).add(id));
      try {
        const branch = await fetchChart(id);
        if (branch) setDeeper((prev) => new Map(prev).set(id, branch));
      } catch (error) {
        setBranchError(error);
      } finally {
        setBranchLoading((prev) => {
          const next = new Set(prev);
          next.delete(id);
          return next;
        });
      }
    },
    [chart, fetchChart],
  );

  return {
    model: all.loading ? emptyModel() : model,
    loading: all.loading || chartsPending,
    loadingMore: all.loadingMore,
    error: all.error ?? chartError ?? branchError,
    refetch: () => {
      setBranchError(null);
      setDeeper(new Map());
      all.refetch();
      void chartsQuery.refetch();
    },
    branchLoading,
    loadBranch,
  };
}
