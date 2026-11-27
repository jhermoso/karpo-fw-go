// Normalizadores de las rutas auxiliares de Go que no siguen el contrato de mantenimiento
// (A2 §3, §4, §5): catálogos en PascalCase, árbol de puestos y delimitaciones geográficas.
import type { Page, Row } from '../contracts';

type Obj = Record<string, unknown>;

function isObj(value: unknown): value is Obj {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function num(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined;
}

/** `ID` -> `id`, `RequiresContact` -> `requiresContact`, `ISOCode` -> `isoCode`, `Name` -> `name`. */
export function camelKey(key: string): string {
  if (/^[A-Z0-9]+$/.test(key)) return key.toLowerCase();
  const lead = /^([A-Z]+)(?=[A-Z][a-z])/.exec(key);
  if (lead) return lead[1]!.toLowerCase() + key.slice(lead[1]!.length);
  return key.charAt(0).toLowerCase() + key.slice(1);
}

/** Pasa a camelCase las claves de un objeto y de los anidados (los arrays se recorren). */
export function camelize<T = unknown>(value: unknown): T {
  if (Array.isArray(value)) return value.map((v) => camelize(v)) as T;
  if (!isObj(value)) return value as T;
  const out: Obj = {};
  for (const [key, v] of Object.entries(value)) out[camelKey(key)] = camelize(v);
  return out as T;
}

/** Fila con `id` en cadena (los identificadores de Go pueden llegar como número o con otro nombre). */
export function toRow(value: unknown, primaryKey = 'id'): Row {
  const row = (isObj(value) ? { ...value } : {}) as Obj;
  const id = row.id ?? row.ID ?? row.Id ?? row[primaryKey];
  row.id = id === undefined || id === null ? '' : String(id);
  return row as Row;
}

/** Filas de una respuesta: array directo o `{items|data|results|records}`. */
export function extractItems(data: unknown): unknown[] {
  if (Array.isArray(data)) return data;
  if (isObj(data)) {
    for (const key of ['items', 'data', 'results', 'records']) {
      if (Array.isArray(data[key])) return data[key] as unknown[];
    }
  }
  return [];
}

/** Página de una respuesta, con los dos estilos de Go (`pageNumber/pageSize` y `number/size`). */
export function toPage(
  data: unknown,
  fallback: { page: number; pageSize: number },
  primaryKey = 'id',
): Page {
  const items = extractItems(data).map((item) => toRow(item, primaryKey));
  const meta = isObj(data) ? data : {};
  const total =
    num(meta.total) ?? num(meta.totalCount) ?? num(meta.totalItems) ?? num(meta.totalElements) ?? num(meta.count) ?? items.length;
  return {
    items,
    total,
    page: num(meta.pageNumber) ?? num(meta.number) ?? num(meta.page) ?? fallback.page,
    pageSize: num(meta.pageSize) ?? num(meta.size) ?? fallback.pageSize,
  };
}

/** Catálogo (PascalCase o camelCase, lista plana o página) -> filas en camelCase con `id`. */
export function normalizeCatalog(data: unknown): Row[] {
  return extractItems(data).map((item) => toRow(camelize(item)));
}

export interface ChartNode {
  position: Row;
  reports: ChartNode[];
}

/** Árbol anidado de `GET /api/hr/positions/{id}/chart` -> `{position, reports[]}` (PascalCase tolerado). */
export function normalizeChart(data: unknown): ChartNode {
  const node = camelize<Obj>(data);
  const reports = Array.isArray(node.reports) ? node.reports : [];
  return {
    position: toRow(isObj(node.position) ? node.position : {}),
    reports: reports.map((child) => normalizeChart(child)),
  };
}

export interface Boundary extends Row {
  name: string;
  type?: string;
  typeName?: string;
  geoCode?: string;
  abbreviation?: string;
  nutsCode?: string;
  ineCode?: string;
  lauCode?: string;
  country?: string;
  level?: string | number;
  parent?: string;
  active?: boolean;
}

export interface BoundaryDetail extends Boundary {
  ancestors: Boundary[];
  groupings: Row[];
}

/** Delimitaciones de `GET /api/geography/boundaries` -> página con `id` en cadena. */
export function normalizeBoundaries(data: unknown, fallback: { page: number; pageSize: number }): Page<Boundary> {
  return toPage(camelize(data), fallback) as Page<Boundary>;
}

/** Delimitación con su cadena de antecesores y agrupaciones. */
export function normalizeBoundary(data: unknown): BoundaryDetail {
  const node = camelize<Obj>(data);
  const row = toRow(node) as Boundary;
  return {
    ...row,
    ancestors: (Array.isArray(node.ancestors) ? node.ancestors : []).map((a) => toRow(a) as Boundary),
    groupings: (Array.isArray(node.groupings) ? node.groupings : []).map((g) => toRow(g)),
  };
}
