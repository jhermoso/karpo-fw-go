// Datos del nivel que se mira (PLAN §4.12): la geometría del nivel (GeoJSON propio o teselas
// vectoriales PMTiles propias), las migas y los valores por delimitación.
import { keepPreviousData, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useCallback, useContext, useMemo, useState } from 'react';
import { useEntityApiExt } from '../api';
import { ManifestContext, type EntityConfig, type MapSourceDef } from '../contracts';
import { aggregateRows, binOf, boundaryIds, computeBins, keyOf, type Bin } from './aggregate';
import { boundsOfFeatures, type BBox, type GeoCollection, type GeoFeature } from './geo';
import { crumbsMatch, geojsonUrlFor, resolveEntry, skipsSingleLevels, type Crumb, type EntryTrail, type MapNav, type MapViewDef } from './nav';
import { absoluteUrl, loadChains, loadGeoLevel, resolveTrail } from './sources';
import type { MapDataSource } from './style';
import { useAllRows } from '../tree/use-all-rows';

/** Lo que se ve en el mapa: una geometría y el nivel al que pertenece. */
export interface LevelShown {
  nav: MapNav;
  features: GeoFeature[];
  collection: GeoCollection | null;
}

interface LevelResult extends LevelShown {
  /** No hay geometría para este lugar (404): una delimitación sin detalle. */
  missing: boolean;
}

function levelQuery(url: string, nav: MapNav) {
  return {
    queryKey: ['map', 'geo', url] as const,
    staleTime: Infinity,
    queryFn: async ({ signal }: { signal?: AbortSignal }): Promise<LevelResult> => {
      const data = await loadGeoLevel(url, signal);
      return { nav, features: data?.features ?? [], collection: data?.collection ?? null, missing: data === null };
    },
  };
}

export function sourceFor(view: MapViewDef, sources: Record<string, MapSourceDef>, nav: MapNav): { id: string; def: MapSourceDef | undefined } {
  const id = view.levels[nav.index]!.source;
  return { id, def: sources[id] };
}

export interface MapLevelState {
  /** Fuente de datos para el estilo, o `null` si aún no hay nada que pintar. */
  source: MapDataSource | null;
  shown: LevelShown | null;
  /** Se está pidiendo un nivel y lo que se ve es el anterior. */
  loading: boolean;
  /** El lugar al que se va no tiene geometría. */
  missing: boolean;
  error: unknown;
  /** Teselas vectoriales: el lienzo informa de las entidades que ha cargado. */
  reportFeatures(features: GeoFeature[]): void;
  /** Definición de la fuente y su identificador. */
  sourceId: string;
  sourceDef: MapSourceDef | undefined;
}

export function useMapLevel(view: MapViewDef, nav: MapNav): MapLevelState {
  const manifest = useContext(ManifestContext);
  const sources = manifest?.map.sources ?? {};
  const { id: sourceId, def: sourceDef } = sourceFor(view, sources, nav);
  const level = view.levels[nav.index]!;
  const geojson = sourceDef?.type === 'geojson' ? sourceDef : undefined;
  const url = geojson ? absoluteUrl(geojsonUrlFor(geojson.url, view, nav)) : null;

  const query = useQuery({
    ...levelQuery(url ?? '', nav),
    enabled: url !== null,
    placeholderData: keepPreviousData,
  });

  const [reported, setReported] = useState<{ key: string; features: GeoFeature[] } | null>(null);
  const navKey = `${nav.index}:${nav.code ?? ''}`;
  const reportFeatures = useCallback(
    (features: GeoFeature[]) => setReported({ key: navKey, features }),
    [navKey],
  );

  // La fuente de teselas depende sólo del nivel y del padre: no se recrea cuando llegan las entidades
  // (si no, cada informe de entidades reaplicaría el estilo y éste volvería a pedirlas).
  const pmtiles = sourceDef?.type === 'pmtiles' ? sourceDef : undefined;
  const boundaryLevel = level.boundaryLevel;
  const tileSource = useMemo<MapDataSource | null>(
    () =>
      pmtiles
        ? {
            kind: 'pmtiles',
            url: `pmtiles://${absoluteUrl(pmtiles.url)}`,
            layer: pmtiles.layer,
            boundaryLevel,
            parentCode: nav.index === 0 ? null : nav.code,
            maxzoom: 8,
          }
        : null,
    [pmtiles, boundaryLevel, nav],
  );

  return useMemo<MapLevelState>(() => {
    const base = { reportFeatures, sourceId, sourceDef };
    if (tileSource) {
      const features = reported && reported.key === navKey ? reported.features : [];
      return {
        ...base,
        source: tileSource,
        shown: { nav, features, collection: null },
        loading: reported === null || reported.key !== navKey,
        missing: false,
        error: null,
      };
    }
    const data = query.data as LevelResult | undefined;
    return {
      ...base,
      source: data?.collection ? { kind: 'geojson', data: data.collection } : null,
      shown: data ? { nav: data.nav, features: data.features, collection: data.collection } : null,
      loading: url !== null && (query.isPending || query.isPlaceholderData),
      missing: Boolean(data?.missing) && !query.isPlaceholderData,
      error: sourceDef ? query.error : new Error(`La fuente de mapa «${sourceId}» no existe en el manifiesto`),
    };
  }, [tileSource, sourceDef, sourceId, reportFeatures, reported, navKey, nav, query.data, query.isPending, query.isPlaceholderData, query.error, url]);
}

// ---------------------------------------------------------------------------------------------
// Migas
// ---------------------------------------------------------------------------------------------

export function useCrumbs(view: MapViewDef, nav: MapNav, known: readonly Crumb[] | null, rootName: string): readonly Crumb[] {
  const client = useQueryClient();
  const manifest = useContext(ManifestContext);
  const sources = manifest?.map.sources ?? {};
  const matches = crumbsMatch(known, nav);
  const key = `${nav.index}:${nav.code ?? ''}`;
  const skipSingle = skipsSingleLevels(view);
  const resolved = useQuery({
    queryKey: ['map', 'trail', view.levels.map((l) => l.id).join('>'), key, rootName, skipSingle],
    enabled: !matches && nav.index > 0,
    staleTime: Infinity,
    queryFn: () =>
      resolveTrail({
        nav,
        rootName,
        skipSingle,
        load: (index, parentCode) => loadFeaturesOf(client, view, sources, index, parentCode),
      }),
  });
  if (matches) return known;
  if (nav.index === 0) return [{ index: 0, code: null, name: rootName }];
  return resolved.data ?? [{ index: 0, code: null, name: rootName }, { index: nav.index, code: nav.code, name: nav.code ?? '' }];
}

/**
 * Entra en una delimitación saltando los niveles con una sola delimitación (M-4). Sólo sabe de las
 * fuentes GeoJSON: con teselas vectoriales no se puede mirar un nivel sin abrirlo y no se salta nada
 * (la vista lo hace cuando las teselas llegan).
 */
export function useEntryResolver(view: MapViewDef): (entered: Crumb) => Promise<EntryTrail> {
  const client = useQueryClient();
  const manifest = useContext(ManifestContext);
  const sources = manifest?.map.sources ?? {};
  return useCallback(
    (entered) => resolveEntry(view, entered, (nav) => loadFeaturesOf(client, view, sources, nav.index, nav.code)),
    // `sources` sale del manifiesto, que no cambia durante la vida de la vista.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [client, view, manifest],
  );
}

async function loadFeaturesOf(
  client: QueryClient,
  view: MapViewDef,
  sources: Record<string, MapSourceDef>,
  index: number,
  parentCode: string | null,
): Promise<GeoFeature[] | null> {
  const nav: MapNav = { index, code: parentCode };
  const def = sources[view.levels[index]!.source];
  if (def?.type !== 'geojson') return null;
  const url = absoluteUrl(geojsonUrlFor(def.url, view, nav));
  try {
    const result = await client.fetchQuery(levelQuery(url, nav));
    return result.missing ? null : result.features;
  } catch {
    return null;
  }
}

// ---------------------------------------------------------------------------------------------
// Valores
// ---------------------------------------------------------------------------------------------

export interface MapValues {
  /** Valor por `(nivel, código)`. */
  totals: ReadonlyMap<string, number>;
  /** Filas cuya delimitación no se pudo situar. */
  unplaced: number;
  /** Filas de la entidad que se han leído (con o sin delimitación conocida). */
  rows: number;
  loading: boolean;
}

export function useMapValues(config: EntityConfig, view: MapViewDef): MapValues {
  const api = useEntityApiExt();
  const all = useAllRows(config);
  const ids = useMemo(() => boundaryIds(all.rows, view.boundaryField), [all.rows, view.boundaryField]);
  const chains = useQuery({
    queryKey: ['map', 'chains', config.id, view.boundaryField, ids.join(',')],
    enabled: !all.loading && !all.loadingMore,
    staleTime: 60_000,
    queryFn: ({ signal }) => loadChains(api, ids, signal),
  });
  return useMemo(() => {
    if (!chains.data) return { totals: new Map(), unplaced: 0, rows: all.rows.length, loading: true };
    const { totals, unplaced } = aggregateRows(
      all.rows,
      { boundaryField: view.boundaryField, aggregate: view.aggregate, valueField: view.valueField },
      chains.data,
    );
    return { totals, unplaced, rows: all.rows.length, loading: false };
  }, [chains.data, all.rows, view.boundaryField, view.aggregate, view.valueField]);
}

export interface LevelValues {
  values: Map<string, number>;
  steps: Map<string, number>;
  bins: Bin[];
}

/** Valor y paso de color de cada entidad del nivel que se ve. */
export function levelValues(features: readonly GeoFeature[], totals: ReadonlyMap<string, number>, boundaryLevel: string): LevelValues {
  const values = new Map<string, number>();
  for (const feature of features) {
    const total = totals.get(keyOf(feature.level || boundaryLevel, feature.code));
    if (total !== undefined) values.set(feature.code, total);
  }
  const bins = computeBins([...values.values()]);
  const steps = new Map<string, number>();
  for (const [code, value] of values) {
    const index = binOf(value, bins);
    if (index >= 0) steps.set(code, bins[index]!.step);
  }
  return { values, steps, bins };
}

export function boundsOf(shown: LevelShown | null): BBox | null {
  return shown ? boundsOfFeatures(shown.features) : null;
}
