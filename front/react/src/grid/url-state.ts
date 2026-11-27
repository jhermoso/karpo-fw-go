// Estado de la consulta de la cuadrícula en la URL (PLAN §4.9): búsqueda rápida, filtros y orden.
// Viaja en `EntityRoute.params` (el marco de la app lo pasa a la cadena de consulta):
//   q=texto            búsqueda rápida (el filtro de config con `quickSearch`)
//   f.<campo>=valor    un filtro por campo de `config.filters` (vacío = quitado a propósito)
//   sort=campo:asc     orden (sólo si el campo tiene `serverSortField`)
import type { ColumnDef, EntityConfig, FilterDef, ListQuery } from '../contracts';
import { asExt } from '../config';

export interface SortState {
  field: string;
  direction: 'asc' | 'desc';
}

export interface QueryState {
  search: string;
  /** Valor en cadena de cada filtro activo, por `FilterDef.field`. */
  filters: Record<string, string>;
  sort?: SortState;
}

export const SEARCH_PARAM = 'q';
export const SORT_PARAM = 'sort';
export const FILTER_PREFIX = 'f.';

/** Los filtros que no son la búsqueda rápida: los que se muestran como chips. */
export function chipFilters(config: EntityConfig): FilterDef[] {
  return config.filters.filter((filter) => !filter.quickSearch);
}

export function quickSearchFilter(config: EntityConfig): FilterDef | undefined {
  return config.filters.find((filter) => filter.quickSearch);
}

/** Columna por la que el servidor admite ordenar (la única con orden de servidor). */
export function sortableColumn(config: EntityConfig, field: string): ColumnDef | undefined {
  return config.columns.find((column) => column.field === field && column.serverSortField);
}

function defaultSort(config: EntityConfig): SortState | undefined {
  const wanted = asExt(config).grid.defaultSort;
  if (!wanted || !sortableColumn(config, wanted.field)) return undefined;
  return { field: wanted.field, direction: wanted.direction };
}

function stringOf(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (Array.isArray(value)) return value.join(',');
  return String(value);
}

/** Lee el estado de `route.params`; lo que falta toma el valor por omisión de la configuración. */
export function readQueryState(config: EntityConfig, params: Record<string, string> | undefined): QueryState {
  const source = params ?? {};
  const filters: Record<string, string> = {};
  for (const filter of chipFilters(config)) {
    const key = `${FILTER_PREFIX}${filter.field}`;
    if (key in source) {
      if (source[key] !== '') filters[filter.field] = source[key] as string;
    } else if (filter.defaultValue !== undefined && filter.defaultValue !== null && filter.defaultValue !== '') {
      filters[filter.field] = stringOf(filter.defaultValue);
    }
  }
  let sort: SortState | undefined;
  const raw = source[SORT_PARAM];
  if (raw !== undefined) {
    const [field = '', direction = 'asc'] = raw.split(':');
    if (sortableColumn(config, field)) sort = { field, direction: direction === 'desc' ? 'desc' : 'asc' };
  } else {
    sort = defaultSort(config);
  }
  return { search: source[SEARCH_PARAM] ?? '', filters, ...(sort ? { sort } : {}) };
}

/** Parámetros de la URL para un estado, conservando los ajenos a la cuadrícula (`params` previos). */
export function writeQueryState(
  config: EntityConfig,
  state: QueryState,
  previous: Record<string, string> | undefined,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(previous ?? {})) {
    if (key === SEARCH_PARAM || key === SORT_PARAM || key.startsWith(FILTER_PREFIX)) continue;
    out[key] = value;
  }
  if (state.search.trim() !== '') out[SEARCH_PARAM] = state.search;
  const sort = state.sort;
  if (sort && !(defaultSort(config)?.field === sort.field && defaultSort(config)?.direction === sort.direction)) {
    out[SORT_PARAM] = `${sort.field}:${sort.direction}`;
  } else if (!sort && defaultSort(config)) {
    out[SORT_PARAM] = ''; // sin orden a propósito, aunque la configuración proponga uno
  }
  for (const filter of chipFilters(config)) {
    const value = state.filters[filter.field];
    if (value !== undefined && value !== '') out[`${FILTER_PREFIX}${filter.field}`] = value;
    else if (filter.defaultValue !== undefined && filter.defaultValue !== null && filter.defaultValue !== '') {
      out[`${FILTER_PREFIX}${filter.field}`] = ''; // quitado a propósito
    }
  }
  return out;
}

export function sameParams(a: Record<string, string> | undefined, b: Record<string, string> | undefined): boolean {
  const left = a ?? {};
  const right = b ?? {};
  const keys = new Set([...Object.keys(left), ...Object.keys(right)]);
  for (const key of keys) if ((left[key] ?? '') !== (right[key] ?? '')) return false;
  return true;
}

function typed(filter: FilterDef, value: string): string | number | boolean | undefined {
  if (value === '') return undefined;
  if (filter.kind === 'boolean') return value === 'true';
  if (filter.kind === 'number') {
    const n = Number(value);
    return Number.isFinite(n) ? n : undefined;
  }
  return value;
}

/** Consulta al servidor: `serverField` de cada filtro, la búsqueda rápida y el orden admitido. */
export function buildListQuery(config: EntityConfig, state: QueryState, pageSize: number): ListQuery {
  const filter: ListQuery['filter'] = {};
  const search = quickSearchFilter(config);
  if (search && state.search.trim() !== '') filter[search.serverField] = state.search.trim();
  for (const def of chipFilters(config)) {
    const value = typed(def, state.filters[def.field] ?? '');
    if (value !== undefined) filter[def.serverField] = value;
  }
  const sort = state.sort && sortableColumn(config, state.sort.field) ? state.sort : undefined;
  return { page: 1, pageSize, filter, ...(sort ? { sort: { field: sort.field, direction: sort.direction } } : {}) };
}

/** Siguiente orden al pulsar la cabecera de una columna: ascendente, descendente, sin orden. */
export function cycleSort(current: SortState | undefined, field: string): SortState | undefined {
  if (!current || current.field !== field) return { field, direction: 'asc' };
  if (current.direction === 'asc') return { field, direction: 'desc' };
  return undefined;
}
