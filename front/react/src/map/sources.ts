// Datos del mapa (PLAN §4.12, §4.13): geometrías GeoJSON propias por nivel, migas resueltas desde la
// URL y cadena de antecesores de cada delimitación (con caché). Nada sale del origen configurado.
import type { MapSourceDef } from '../contracts';
import type { EntityApiExt } from '../api';
import type { BoundaryDetail } from '../api';
import { chainOf, isLocatable, type BoundaryLike, type ChainLink } from './aggregate';
import { featuresFromCollection, type GeoCollection, type GeoFeature } from './geo';
import { parentCandidates, type Crumb, type MapNav } from './nav';

/** Dirección absoluta de un recurso del origen (los estilos de MapLibre no admiten rutas relativas). */
export function absoluteUrl(url: string, base?: string): string {
  const root = base ?? (typeof location !== 'undefined' ? location.href : 'http://localhost/');
  return new URL(url, root).href;
}

/** `null` si el nivel no tiene geometría (404): es un país o una provincia sin detalle. */
export async function fetchGeoCollection(url: string, signal?: AbortSignal): Promise<GeoCollection | null> {
  const response = await fetch(url, { signal, headers: { Accept: 'application/geo+json, application/json' } });
  if (response.status === 404) return null;
  if (!response.ok) throw new Error(`No se pudo leer ${url}: HTTP ${response.status}`);
  const data = (await response.json()) as GeoCollection;
  if (data.type !== 'FeatureCollection' || !Array.isArray(data.features)) throw new Error(`${url} no es un GeoJSON de colección`);
  return data;
}

export interface GeoLevelData {
  collection: GeoCollection;
  features: GeoFeature[];
}

export async function loadGeoLevel(url: string, signal?: AbortSignal): Promise<GeoLevelData | null> {
  const collection = await fetchGeoCollection(url, signal);
  return collection ? { collection, features: featuresFromCollection(collection) } : null;
}

export function geojsonSource(sources: Record<string, MapSourceDef>, id: string): Extract<MapSourceDef, { type: 'geojson' }> | undefined {
  const source = sources[id];
  return source?.type === 'geojson' ? source : undefined;
}

/** La fuente es de demostración (geometría esquemática): así lo declara `schematic` o su identificador. */
export function isSchematicSource(id: string, source: MapSourceDef | undefined): boolean {
  if (!source) return false;
  if (source.schematic === true) return true;
  return id.startsWith('demo');
}

// ---------------------------------------------------------------------------------------------
// Migas desde la URL
// ---------------------------------------------------------------------------------------------

export interface TrailArgs {
  nav: MapNav;
  rootName: string;
  /** Entidades que se ven en el nivel `index` al abrir `parentCode`; `null` si no hay geometría. */
  load(index: number, parentCode: string | null): Promise<GeoFeature[] | null>;
  /** Los niveles con una sola delimitación se saltan (M-4): su miga lo dice (`skipped`). */
  skipSingle?: boolean;
}

/**
 * Migas de un lugar al que se llegó por la URL (sin pasar por los clics): sube de la delimitación a
 * su padre buscándola en la geometría del nivel de arriba. El padre se deduce del código (en NUTS,
 * ES300 → ES30 → ES) y se confirma con `parentCode` de la propia entidad.
 */
export async function resolveTrail({ nav, rootName, load, skipSingle = false }: TrailArgs): Promise<Crumb[]> {
  const chain: Crumb[] = [];
  let index = nav.index;
  let code = nav.code;
  // El nivel de la miga que se va a escribir sólo tenía un elemento con detalle: se saltó (M-4).
  let skipped = false;
  while (index > 0 && code !== null) {
    const displayIndex = index - 1;
    let found: GeoFeature | undefined;
    let siblings: GeoFeature[] | null = null;
    if (displayIndex === 0) {
      siblings = await load(0, null);
      found = siblings?.find((feature) => feature.code === code);
    } else {
      for (const parent of parentCandidates(code)) {
        siblings = await load(displayIndex, parent);
        found = siblings?.find((feature) => feature.code === code);
        if (found) break;
      }
    }
    chain.unshift({
      index,
      code,
      name: found?.name ?? code,
      ...(found ? { bbox: found.bbox } : {}),
      ...(skipped ? { skipped: true } : {}),
    });
    if (!found) break;
    // `siblings` es lo que se ve en el nivel de arriba: si sólo es esta delimitación, ese nivel se saltó al bajar.
    skipped = skipSingle && found.hasChildren && siblings?.length === 1;
    index = displayIndex;
    code = found.parentCode === '' ? null : found.parentCode;
  }
  return [{ index: 0, code: null, name: rootName }, ...chain];
}

// ---------------------------------------------------------------------------------------------
// Cadena de antecesores de las delimitaciones de las filas
// ---------------------------------------------------------------------------------------------

const CONCURRENCY = 6;
const caches = new WeakMap<object, Map<string, Promise<BoundaryDetail | null>>>();

/** `GET /api/geography/boundaries/{id}` con caché por API: un identificador se pide una sola vez. */
export function boundaryDetail(api: EntityApiExt, id: string, signal?: AbortSignal): Promise<BoundaryDetail | null> {
  let cache = caches.get(api);
  if (!cache) {
    cache = new Map();
    caches.set(api, cache);
  }
  let pending = cache.get(id);
  if (!pending) {
    pending = api.boundary(id, signal).catch(() => {
      // Sin permiso o inexistente: la fila no cuenta, y se podrá reintentar.
      cache!.delete(id);
      return null;
    });
    cache.set(id, pending);
  }
  return pending;
}

/** Vacía la caché de delimitaciones (pruebas). */
export function clearBoundaryCache(api: EntityApiExt): void {
  caches.delete(api);
}

/**
 * Cadena `[propia, ...antecesores]` de cada identificador. Si un antecesor no trae nivel ni código
 * (el contrato de `ancestors[]` es corto) se completa pidiendo esa delimitación, también con caché.
 */
export async function loadChains(
  api: EntityApiExt,
  ids: readonly string[],
  signal?: AbortSignal,
): Promise<Map<string, ChainLink[]>> {
  const out = new Map<string, ChainLink[]>();
  let next = 0;
  const lanes = Array.from({ length: Math.min(CONCURRENCY, ids.length) }, async () => {
    while (next < ids.length) {
      const id = ids[next++]!;
      const detail = await boundaryDetail(api, id, signal);
      if (!detail) continue;
      const ancestors: BoundaryLike[] = [];
      for (const ancestor of detail.ancestors) {
        const like = ancestor as BoundaryLike & { id: string };
        if (isLocatable(like)) ancestors.push(like);
        else {
          const full = await boundaryDetail(api, like.id, signal);
          if (full) ancestors.push(full as BoundaryLike);
        }
      }
      out.set(id, chainOf(detail as BoundaryLike, ancestors));
    }
  });
  await Promise.all(lanes);
  return out;
}
