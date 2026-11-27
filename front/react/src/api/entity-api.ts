// Adaptador del contrato de mantenimiento de Go (A2 §2) y de sus rutas auxiliares.
//  - Lista por `POST {ep}/search` con `{filter, pagination:{pageNumber,pageSize,orderBy,ascending}}`
//    (sólo con campos de orden admitidos) o por GET; estilo `number-size` para rutas sin /v1.
//  - Alta (201), cambio con `{...patch, version}` (la versión es obligatoria), baja con cuerpo
//    opcional (204), órdenes `POST {ep}/{id}/{orden}`, 202/pendingApproval -> `pending`.
import type {
  AppManifest,
  CommandDef,
  CommandResult,
  EntityApi,
  EntityConfig,
  ListQuery,
  Page,
  Row,
} from '../contracts';
import { asExt, knownServerSort } from '../config';
import { localValidationError } from './errors';
import type { HttpClient } from './http';
import {
  normalizeBoundaries,
  normalizeBoundary,
  normalizeCatalog,
  normalizeChart,
  toPage,
  toRow,
  extractItems,
  type Boundary,
  type BoundaryDetail,
  type ChartNode,
} from './normalizers';

export const MAX_PAGE_SIZE = 1000;
/** Tope de filas de `listAll` (10 páginas de 1000). */
export const LIST_ALL_LIMIT = 10_000;

type Obj = Record<string, unknown>;

function isObj(value: unknown): value is Obj {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export interface BoundaryQuery {
  q?: string;
  type?: string;
  parent?: string;
  grouping?: string;
  country?: string;
  level?: string | number;
  page?: number;
  size?: number;
}

/** El adaptador de la API más las rutas auxiliares (catálogos, organigrama, geografía). */
export interface EntityApiExt extends EntityApi {
  /** Todas las páginas de una consulta (hasta `LIST_ALL_LIMIT` filas). */
  listAll(cfg: EntityConfig, q?: Partial<ListQuery>, signal?: AbortSignal): Promise<Row[]>;
  /** Catálogo (PascalCase o camelCase) en filas camelCase con `id`. */
  catalog(path: string, signal?: AbortSignal): Promise<Row[]>;
  /** `GET /api/hr/positions/{id}/chart?depth=`. */
  positionChart(rootId: string, depth?: number, signal?: AbortSignal): Promise<ChartNode>;
  /** `GET /api/geography/boundaries`. */
  boundaries(query?: BoundaryQuery, signal?: AbortSignal): Promise<Page<Boundary>>;
  /** `GET /api/geography/boundaries/{id}` con `ancestors[]` y `groupings[]`. */
  boundary(id: string, signal?: AbortSignal): Promise<BoundaryDetail>;
}

function clamp(n: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, n));
}

/** Sustituye `{campo}` por el valor (codificado) de la fila; `{id}` es el identificador. */
export function expandTemplate(template: string, row: Obj): string {
  return template.replace(/\{([A-Za-z_][\w.]*)\}/g, (_match, name: string) => {
    let value: unknown = row;
    for (const part of name.split('.')) value = isObj(value) ? value[part] : undefined;
    return value === undefined || value === null ? '' : encodeURIComponent(String(value));
  });
}

/** Sin valores vacíos: `undefined`, `null` y `''` no viajan (el servidor ignora filtros que no conoce). */
function cleanFilter(filter: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(filter)) {
    if (value === undefined || value === null || value === '') continue;
    out[key] = value;
  }
  return out;
}

/** Campo de orden del servidor para el campo pedido, o `undefined` si el servidor no lo admite. */
export function resolveServerSort(cfg: EntityConfig, field: string): string | undefined {
  const column = cfg.columns.find((c) => c.field === field || c.serverSortField === field);
  if (column?.serverSortField) return column.serverSortField;
  if (cfg.apiVersioned && knownServerSort(cfg.endpoint).includes(field)) return field;
  return undefined;
}

function pendingOf(status: number, data: unknown): boolean {
  if (status === 202) return true;
  return isObj(data) && Boolean(data.pendingApproval);
}

function commandRow(data: unknown): Row | undefined {
  if (isObj(data) && data.id !== undefined && !data.pendingApproval) return toRow(data);
  return undefined;
}

export function createEntityApi(
  http: HttpClient,
  manifest?: { api?: Partial<AppManifest['api']> } | null,
): EntityApiExt {
  const apiVersion = manifest?.api?.apiVersion ?? 'v1';

  /** Ruta bajo la base de la API: `/v1/…` si la entidad lleva versión; `~/…` y `https://…` sin versión. */
  function route(endpoint: string, versioned: boolean): string {
    if (/^https?:\/\//i.test(endpoint)) return endpoint;
    if (endpoint.startsWith('~/')) return endpoint.slice(1);
    const path = endpoint.startsWith('/') ? endpoint : `/${endpoint}`;
    return versioned ? `/${apiVersion}${path}` : path;
  }

  function entityRoute(cfg: EntityConfig, suffix = ''): string {
    return route(cfg.endpoint, cfg.apiVersioned) + suffix;
  }

  function rowRoute(cfg: EntityConfig, id: string, suffix = ''): string {
    return `${entityRoute(cfg)}/${encodeURIComponent(id)}${suffix}`;
  }

  function requireVersion(version: unknown): number {
    if (typeof version !== 'number' || !Number.isFinite(version)) {
      throw localValidationError('version', 'required', 'the version that was read is required');
    }
    return version;
  }

  async function list(cfg: EntityConfig, q: ListQuery, signal?: AbortSignal): Promise<Page> {
    const ext = asExt(cfg);
    const pageSize = clamp(Math.trunc(q.pageSize) || 1, 1, MAX_PAGE_SIZE);
    const page = Math.max(1, Math.trunc(q.page) || 1);
    const fallback = { page, pageSize };
    const filter = cleanFilter({ ...ext.dataSource.baseFilter, ...q.filter });

    if (ext.dataSource.listStyle === 'number-size') {
      const data = await http.request<unknown>(entityRoute(cfg), {
        signal,
        query: { page, size: pageSize, ...filter },
      });
      return toPage(data, fallback, cfg.primaryKey);
    }

    const sortField = q.sort ? resolveServerSort(cfg, q.sort.field) : undefined;
    if (ext.dataSource.listMethod === 'get') {
      const data = await http.request<unknown>(entityRoute(cfg), {
        signal,
        query: {
          page,
          pageSize,
          ...(sortField ? { sortBy: sortField, sortOrder: q.sort!.direction } : {}),
          ...filter,
        },
      });
      return toPage(data, fallback, cfg.primaryKey);
    }

    const body = {
      filter,
      pagination: {
        pageNumber: page,
        pageSize,
        ...(sortField ? { orderBy: sortField, ascending: q.sort!.direction === 'asc' } : {}),
      },
    };
    const data = await http.request<unknown>(entityRoute(cfg, '/search'), { method: 'POST', json: body, signal });
    return toPage(data, fallback, cfg.primaryKey);
  }

  const api: EntityApiExt = {
    list,

    async listAll(cfg, q = {}, signal) {
      const rows: Row[] = [];
      let page = q.page ?? 1;
      for (;;) {
        const result = await list(cfg, { filter: {}, ...q, page, pageSize: MAX_PAGE_SIZE }, signal);
        rows.push(...result.items);
        if (result.items.length === 0 || rows.length >= result.total || rows.length >= LIST_ALL_LIMIT) break;
        page += 1;
      }
      return rows;
    },

    async get(cfg, id, signal) {
      const aggregate = asExt(cfg).aggregateEndpoint;
      const path = aggregate
        ? route(expandTemplate(aggregate, { id }), cfg.apiVersioned)
        : rowRoute(cfg, id);
      const data = await http.request<unknown>(path, { signal });
      return toRow(data, cfg.primaryKey);
    },

    async create(cfg, body) {
      const response = await http.requestFull<unknown>(entityRoute(cfg), { method: 'POST', json: body });
      if (isObj(response.data)) return toRow(response.data, cfg.primaryKey);
      const location = response.headers.get('Location');
      const id = location?.split('?')[0]?.split('/').filter(Boolean).pop();
      if (id) return api.get(cfg, decodeURIComponent(id));
      return toRow({}, cfg.primaryKey);
    },

    async update(cfg, id, patch, version) {
      const checked = requireVersion(version);
      const data = await http.request<unknown>(rowRoute(cfg, id), {
        method: 'PUT',
        json: { ...patch, version: checked },
      });
      return toRow(data, cfg.primaryKey);
    },

    async remove(cfg, id, body) {
      const response = await http.requestFull<unknown>(rowRoute(cfg, id), {
        method: 'DELETE',
        ...(body && Object.keys(body).length > 0 ? { json: body } : {}),
      });
      const row = commandRow(response.data);
      return { status: pendingOf(response.status, response.data) ? 'pending' : 'done', ...(row ? { row } : {}) };
    },

    async command(cfg, cmd: CommandDef, row: Row, body): Promise<CommandResult> {
      if (cmd.kind === 'delete') return api.remove(cfg, row.id, body);
      if (cmd.kind !== 'http') {
        throw new Error(`La orden \`${cmd.id}\` (${cmd.kind}) es de navegación: no hace ninguna petición`);
      }
      const payload: Obj = { ...(body ?? {}) };
      if (cmd.sendVersion) payload.version = requireVersion(row.version);
      const template = cmd.endpoint ?? `${cfg.endpoint}/{id}/${cmd.id}`;
      const path = route(expandTemplate(template, row), cfg.apiVersioned);
      const response = await http.requestFull<unknown>(path, {
        method: cmd.method ?? 'POST',
        ...(Object.keys(payload).length > 0 ? { json: payload } : {}),
      });
      const updated = commandRow(response.data);
      return { status: pendingOf(response.status, response.data) ? 'pending' : 'done', ...(updated ? { row: updated } : {}) };
    },

    async childList(endpoint, row, signal) {
      const path = route(expandTemplate(endpoint, row), true);
      const data = await http.request<unknown>(path, { signal });
      return extractItems(data).map((item) => toRow(item));
    },

    raw<T>(path: string, init?: RequestInit): Promise<T> {
      return http.request<T>(path, init);
    },

    async catalog(path, signal) {
      return normalizeCatalog(await http.request<unknown>(path, { signal }));
    },

    async positionChart(rootId, depth = 3, signal) {
      const data = await http.request<unknown>(`/hr/positions/${encodeURIComponent(rootId)}/chart`, {
        signal,
        query: { depth: clamp(Math.trunc(depth) || 3, 1, 20) },
      });
      return normalizeChart(data);
    },

    async boundaries(query = {}, signal) {
      const { page: wantedPage, size: wantedSize, ...filters } = query;
      const size = clamp(Math.trunc(wantedSize ?? 100) || 100, 1, MAX_PAGE_SIZE);
      const page = Math.max(1, Math.trunc(wantedPage ?? 1) || 1);
      const data = await http.request<unknown>('/geography/boundaries', {
        signal,
        query: { ...filters, page, size },
      });
      return normalizeBoundaries(data, { page, pageSize: size });
    },

    async boundary(id, signal) {
      return normalizeBoundary(await http.request<unknown>(`/geography/boundaries/${encodeURIComponent(id)}`, { signal }));
    },
  };
  return api;
}
