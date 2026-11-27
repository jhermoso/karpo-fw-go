// Etiquetas resueltas de los campos `lookup` (id → texto de `displayField`). Caché de módulo, sin
// React: la leen `format` (sin red) y los visores; la rellenan el editor y las búsquedas.
import type { AppManifest, EntityApi, EntityConfig, LookupDef, Row } from '../contracts';

const labels = new Map<string, string>();
const pending = new Map<string, Promise<string | undefined>>();

function key(lookup: LookupDef, id: string): string {
  return `${lookup.entity}\u0000${lookup.valueField}\u0000${id}`;
}

/** Etiqueta ya conocida, si la hay. */
export function cachedLookupLabel(lookup: LookupDef, id: unknown): string | undefined {
  if (id === null || id === undefined || id === '') return undefined;
  return labels.get(key(lookup, String(id)));
}

export function rememberLookupLabel(lookup: LookupDef, id: string, label: string): void {
  labels.set(key(lookup, id), label);
}

/** Id cuyo texto (sin distinguir mayúsculas) ya se conoce; sirve al pegado sin red. */
export function cachedLookupId(lookup: LookupDef, label: string): string | undefined {
  const prefix = `${lookup.entity}\u0000${lookup.valueField}\u0000`;
  const wanted = label.trim().toLowerCase();
  for (const [stored, text] of labels) {
    if (stored.startsWith(prefix) && text.trim().toLowerCase() === wanted) {
      return stored.slice(prefix.length);
    }
  }
  return undefined;
}

export function clearLookupCache(): void {
  labels.clear();
  pending.clear();
}

/**
 * Configuración de la entidad de destino: la del manifiesto si existe; si no (pruebas, uso
 * suelto), una mínima derivada de la propia definición del lookup.
 */
export function lookupEntityConfig(manifest: AppManifest | null, lookup: LookupDef): EntityConfig {
  const known = manifest?.entities[lookup.entity];
  if (known) return known;
  return {
    id: lookup.entity,
    name: { literal: lookup.entity },
    namePlural: { literal: lookup.entity },
    endpoint: lookup.entity,
    apiVersioned: true,
    primaryKey: lookup.valueField,
    displayField: lookup.displayField,
    editable: false,
    saveMode: 'form',
    presentation: 'panel',
    dataSource: { pagination: 'server', listStyle: 'maintenance', baseFilter: {} },
    columns: [],
    filters: [],
    form: null,
    detail: { sections: [], childGrids: [] },
    views: [{ kind: 'grid' }],
    defaultView: 'grid',
    commands: [],
    help: [],
    warnings: [],
  };
}

export interface LookupOption {
  /** Valor del `valueField`: es lo que se guarda. */
  value: string;
  /** Texto del `displayField`: es lo que se muestra. */
  label: string;
  row: Row;
}

function rowValue(row: Row, field: string): string {
  const raw = row[field] ?? (field === 'id' ? row.id : undefined);
  return raw === null || raw === undefined ? '' : String(raw);
}

export function toLookupOption(row: Row, lookup: LookupDef): LookupOption {
  const value = rowValue(row, lookup.valueField);
  const label = rowValue(row, lookup.displayField) || value;
  if (value) rememberLookupLabel(lookup, value, label);
  return { value, label, row };
}

export const LOOKUP_PAGE_SIZE = 20;

/** Busca en el servidor con `EntityApi.list` y el filtro `search`; recuerda las etiquetas. */
export async function searchLookup(
  api: EntityApi,
  manifest: AppManifest | null,
  lookup: LookupDef,
  query: string,
  signal?: AbortSignal,
): Promise<LookupOption[]> {
  const filter: Record<string, string | number | boolean> = { ...lookup.filter };
  if (query.trim() !== '') filter.search = query.trim();
  const page = await api.list(
    lookupEntityConfig(manifest, lookup),
    { page: 1, pageSize: LOOKUP_PAGE_SIZE, filter },
    signal,
  );
  return page.items.map((row) => toLookupOption(row, lookup));
}

/** Etiqueta de un id: de la caché o con `EntityApi.get` (una sola petición por id). */
export function resolveLookupLabel(
  api: EntityApi,
  manifest: AppManifest | null,
  lookup: LookupDef,
  id: string,
  signal?: AbortSignal,
): Promise<string | undefined> {
  const known = cachedLookupLabel(lookup, id);
  if (known !== undefined) return Promise.resolve(known);
  const cacheKey = key(lookup, id);
  const inFlight = pending.get(cacheKey);
  if (inFlight) return inFlight;
  const request = api
    .get(lookupEntityConfig(manifest, lookup), id, signal)
    .then((row) => toLookupOption(row, lookup).label)
    .catch(() => undefined)
    .finally(() => {
      pending.delete(cacheKey);
    });
  pending.set(cacheKey, request);
  return request;
}
