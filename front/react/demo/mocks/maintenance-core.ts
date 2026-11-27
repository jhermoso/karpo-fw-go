// Motor del contrato de mantenimiento de Go (`/api/v1/<entidad>`, docs/analisis/02-api-karpo-go.md §2):
// lista por GET y POST /search con lista blanca de orden y filtros admitidos (el resto se ignora),
// detalle, alta (201 + Location), cambio (PUT como PATCH con `version`), baja (204) y las órdenes
// propias de cada entidad. Cada entidad sólo declara lo suyo (demo/mocks/entities.ts).
import { http, type HttpHandler } from 'msw';
import type { DemoRow } from '../data/dataset.mjs';
import {
  DEFAULT_PAGE_SIZE,
  MAX_PAGE_SIZE,
  compareValues,
  fold,
  formOf,
  isUuid,
  json,
  noContent,
  organizationScope,
  preflight,
  problem,
  readObject,
  scalarText,
  slice,
  totalPages,
  type FieldIssue,
} from './http';
import { localizeRows } from './catalog-i18n';
import { nextId } from './state';

export type Row = DemoRow;

/** Acumula errores de campo (código + mensaje técnico en inglés, como Go). */
export class Validation {
  readonly issues: FieldIssue[] = [];
  add(field: string, code: string, message: string): void {
    this.issues.push({ field, code, message });
  }
  require(condition: boolean, field: string, code: string, message: string): void {
    if (!condition) this.add(field, code, message);
  }
  get ok(): boolean {
    return this.issues.length === 0;
  }
}

export interface Failure {
  ok: false;
  status: number;
  issues?: FieldIssue[];
  code?: string;
  detail?: string;
}
export type Outcome<T> = { ok: true; value: T } | Failure;

export const fail = (issues: FieldIssue[]): Failure => ({ ok: false, status: 400, issues });
export const violation = (code: string, detail: string): Failure => ({ ok: false, status: 422, code, detail });
export const conflict = (detail: string): Failure => ({ ok: false, status: 409, detail });
export const good = <T>(value: T): Outcome<T> => ({ ok: true, value });
export const noContentResult = (): Outcome<RemoveResult> => ({ ok: true, value: { status: 204 } });

export type RemoveResult =
  | { status: 204 }
  | { status: 202; body: unknown }
  | { status: 200; body: unknown };

export interface FilterContext {
  filters: Map<string, string>;
  text: string;
}

export type ImmutableKind = 'id' | 'text' | 'decimal' | 'date' | 'int' | 'upper';

export interface MaintenanceDef {
  /** Nombre en singular para los mensajes («the facility changed after it was read…»). */
  singular: string;
  base: string;
  rows(): Row[];
  /** `false` en los datos de referencia (países): POST/PUT/DELETE dan 409. */
  writable: boolean;
  readPermission: string;
  writePermission: string;
  sortable: string[];
  defaultSort: string;
  /** Campos en los que busca `search` / `name`. */
  searchFields: string[];
  /** Campo de empresa que estrecha `X-Organization-Scope`. */
  scopeField?: string;
  /** Forma del id: UUID o código ISO alfa-2 (países). */
  idKind?: 'uuid' | 'alpha2';
  /** Filtros admitidos además de `search` y `name`; el que no esté se ignora. */
  matches(row: Row, f: FilterContext): boolean;
  /** Validación de filtros (p. ej. `eu` sólo admite true|false). */
  checkFilters?(filters: Map<string, string>): FieldIssue[];
  listView?(row: Row): unknown;
  detailView?(row: Row): unknown;
  view(row: Row): unknown;
  create?(body: Record<string, unknown>): Outcome<Row>;
  /** Campos que no cambian tras el alta y cómo se comparan. */
  immutable: Record<string, ImmutableKind>;
  /** Aplica el cuerpo (ya mezclado sobre lo que había) y devuelve el parche; vacío = sin cambios. */
  change?(current: Row, merged: Record<string, unknown>): Outcome<Partial<Row>>;
  remove?(current: Row, request: Request): Outcome<RemoveResult> | Promise<Outcome<RemoveResult>>;
  /** Rutas propias (bloquear, historial…). */
  extra?(): HttpHandler[];
}

// ---------------------------------------------------------------------------------------------
// Comparaciones de campos inmutables
// ---------------------------------------------------------------------------------------------

function sameValue(kind: ImmutableKind, a: unknown, b: unknown): boolean {
  const text = (v: unknown) => String(v ?? '').trim();
  switch (kind) {
    case 'id':
      return text(a).toLowerCase() === text(b).toLowerCase();
    case 'upper':
      return text(a).toUpperCase() === text(b).toUpperCase();
    case 'decimal': {
      const x = text(a) === '' ? 0 : Number(text(a));
      const y = text(b) === '' ? 0 : Number(text(b));
      return !Number.isNaN(x) && !Number.isNaN(y) && x === y;
    }
    case 'int':
      return Number(a ?? 0) === Number(b ?? 0);
    case 'date':
    case 'text':
      return text(a) === text(b);
  }
}

// ---------------------------------------------------------------------------------------------
// Peticiones de lista
// ---------------------------------------------------------------------------------------------

interface ListRequest {
  page: number;
  size: number;
  sortBy: string;
  order: string;
  filters: Map<string, string>;
}

const INTEGER = /^-?\d+$/;

function parseGetQuery(url: URL): ListRequest {
  const filters = new Map<string, string>();
  for (const [key, value] of url.searchParams) if (!filters.has(key)) filters.set(key, value);
  const page = url.searchParams.get('page');
  const size = url.searchParams.get('pageSize');
  return {
    page: page === null || page === '' ? 1 : INTEGER.test(page) ? Number(page) : 0,
    size: size === null || size === '' ? DEFAULT_PAGE_SIZE : INTEGER.test(size) ? Number(size) : 0,
    sortBy: url.searchParams.get('sortBy') ?? '',
    order: url.searchParams.get('sortOrder') ?? '',
    filters,
  };
}

function checkList(def: MaintenanceDef, q: ListRequest): FieldIssue[] {
  const v = new Validation();
  v.require(q.page >= 1 && q.page <= 1_000_000, 'page', 'range', 'a page from 1 to 1000000');
  v.require(q.size >= 1 && q.size <= MAX_PAGE_SIZE, 'pageSize', 'range', 'a page size from 1 to 1000');
  v.require(q.sortBy === '' || def.sortable.includes(q.sortBy), 'sortBy', 'enum', def.sortable.join(', '));
  v.require(q.order === '' || q.order === 'asc' || q.order === 'desc', 'sortOrder', 'enum', 'asc or desc');
  if (v.ok && def.checkFilters) v.issues.push(...def.checkFilters(q.filters));
  return v.issues;
}

function runList(def: MaintenanceDef, request: Request, q: ListRequest): Response {
  const path = new URL(request.url).pathname;
  const issues = checkList(def, q);
  if (issues.length > 0) {
    return problem({ status: 400, form: 'v1', instance: path, detail: 'One or more fields are invalid', issues });
  }
  const scope = organizationScope(request);
  if ('error' in scope) return scope.error;
  const text = q.filters.get('search') || q.filters.get('name') || '';
  const needle = fold(text);
  const ctx: FilterContext = { filters: q.filters, text };
  let rows = def.rows().filter((row) => {
    if (scope.ids && def.scopeField) {
      const own = String(row[def.scopeField] ?? '').toLowerCase();
      if (!scope.ids.has(own)) return false;
    }
    if (needle && def.searchFields.length > 0 && !def.searchFields.some((field) => fold(row[field]).includes(needle))) return false;
    return def.matches(row, ctx);
  });
  const sortBy = q.sortBy || def.defaultSort;
  const direction = q.order === 'desc' ? -1 : 1;
  rows = [...rows].sort((a, b) => direction * compareValues(a[sortBy], b[sortBy]) || compareValues(a.id, b.id));
  const total = rows.length;
  const items = localizeRows(request, slice(rows, q.page, q.size).map((row) => (def.listView ?? def.view)(row)));
  return json({
    items,
    total,
    totalCount: total,
    pageNumber: q.page,
    pageSize: q.size,
    totalPages: totalPages(total, q.size),
  });
}

// ---------------------------------------------------------------------------------------------
// Respuestas de resultado
// ---------------------------------------------------------------------------------------------

function outcomeError(request: Request, outcome: Failure): Response {
  const path = new URL(request.url).pathname;
  const form = formOf(path);
  if (outcome.status === 400) {
    return problem({ status: 400, form, instance: path, detail: 'One or more fields are invalid', issues: outcome.issues });
  }
  return problem({ status: outcome.status, form, instance: path, detail: outcome.detail, code: outcome.code, issues: outcome.issues });
}

function badId(request: Request): Response {
  const path = new URL(request.url).pathname;
  return problem({
    status: 400,
    form: 'v1',
    instance: path,
    detail: 'One or more fields are invalid',
    issues: [{ field: 'id', code: 'format', message: 'the identity is not valid' }],
  });
}

function notFound(request: Request): Response {
  return problem({ status: 404, form: 'v1', instance: new URL(request.url).pathname });
}

function bodyError(request: Request, error: 'json' | 'large'): Response {
  const path = new URL(request.url).pathname;
  if (error === 'large') return problem({ status: 413, form: 'v1', instance: path });
  return problem({
    status: 400,
    form: 'v1',
    instance: path,
    detail: 'One or more fields are invalid',
    issues: [{ field: 'body', code: 'json', message: 'the body is not a JSON object of the expected shape' }],
  });
}

function validId(def: MaintenanceDef, id: string): boolean {
  return def.idKind === 'alpha2' ? /^[A-Za-z]{2}$/.test(id) : isUuid(id);
}

export function findRow(def: MaintenanceDef, id: string): Row | undefined {
  const wanted = id.toLowerCase();
  return def.rows().find((row) => String(row.id).toLowerCase() === wanted);
}

function methodNotAllowed(request: Request): Response {
  return problem({ status: 405, form: 'v1', instance: new URL(request.url).pathname, headers: { Allow: 'GET, POST, PUT' } });
}

/** Respuesta de «sólo lectura» para POST/PUT/DELETE de datos de referencia. */
function readOnly(request: Request, def: MaintenanceDef): Response {
  return problem({
    status: 409,
    form: 'v1',
    instance: new URL(request.url).pathname,
    detail: `${def.singular} data is reference data and is maintained with migrations`,
    code: 'reference_data',
  });
}

// ---------------------------------------------------------------------------------------------
// Rutas
// ---------------------------------------------------------------------------------------------

export function maintenanceHandlers(def: MaintenanceDef): HttpHandler[] {
  const path = `*${def.base}`;
  const lookup = (request: Request, id: string): Row | Response => {
    if (!validId(def, id)) return badId(request);
    return findRow(def, id) ?? notFound(request);
  };

  const handlers: HttpHandler[] = [
    http.get(path, async ({ request }) => {
      const blocked = await preflight(request, def.readPermission);
      if (blocked) return blocked;
      return runList(def, request, parseGetQuery(new URL(request.url)));
    }),

    http.post(`${path}/search`, async ({ request }) => {
      const blocked = await preflight(request, def.readPermission);
      if (blocked) return blocked;
      const read = await readObject(request);
      if ('error' in read) return bodyError(request, read.error);
      const pagination = (read.body.pagination ?? {}) as Record<string, unknown>;
      const filters = new Map<string, string>();
      const issues: FieldIssue[] = [];
      for (const [key, value] of Object.entries((read.body.filter ?? {}) as Record<string, unknown>)) {
        const text = scalarText(value);
        if (text === null) issues.push({ field: key, code: 'type', message: 'a filter is a single value' });
        else if (value !== null && value !== undefined) filters.set(key, text);
      }
      if (issues.length > 0) {
        return problem({ status: 400, form: 'v1', instance: new URL(request.url).pathname, detail: 'One or more fields are invalid', issues });
      }
      const size = typeof pagination.pageSize === 'number' && pagination.pageSize !== 0 ? pagination.pageSize : DEFAULT_PAGE_SIZE;
      const page = typeof pagination.pageNumber === 'number' ? pagination.pageNumber : 1;
      return runList(def, request, {
        page,
        size,
        sortBy: typeof pagination.orderBy === 'string' ? pagination.orderBy : '',
        order: pagination.ascending === false ? 'desc' : '',
        filters,
      });
    }),

    http.get(`${path}/:id`, async ({ request, params }) => {
      const blocked = await preflight(request, def.readPermission);
      if (blocked) return blocked;
      const row = lookup(request, String(params.id));
      if (row instanceof Response) return row;
      return json(localizeRows(request, (def.detailView ?? def.view)(row)));
    }),

    http.post(path, async ({ request }) => {
      const blocked = await preflight(request, def.writePermission);
      if (blocked) return blocked;
      if (!def.writable) return readOnly(request, def);
      if (!def.create) return methodNotAllowed(request);
      const read = await readObject(request);
      if ('error' in read) return bodyError(request, read.error);
      const made = def.create(read.body);
      if (!made.ok) return outcomeError(request, made);
      const row = made.value;
      def.rows().push(row);
      return json(localizeRows(request, def.view(row)), 201, { Location: `${def.base}/${row.id}` });
    }),

    http.put(`${path}/:id`, async ({ request, params }) => {
      const blocked = await preflight(request, def.writePermission);
      if (blocked) return blocked;
      if (!def.writable) return readOnly(request, def);
      if (!def.change) return methodNotAllowed(request);
      const current = lookup(request, String(params.id));
      if (current instanceof Response) return current;
      const read = await readObject(request);
      if ('error' in read) return bodyError(request, read.error);
      return applyChange(def, request, current, read.body);
    }),

    http.delete(`${path}/:id`, async ({ request, params }) => {
      const blocked = await preflight(request, def.writePermission);
      if (blocked) return blocked;
      if (!def.writable) return readOnly(request, def);
      if (!def.remove) return methodNotAllowed(request);
      const current = lookup(request, String(params.id));
      if (current instanceof Response) return current;
      const outcome = await def.remove(current, request);
      if (!outcome.ok) return outcomeError(request, outcome);
      const result = outcome.value;
      if (result.status === 204) return noContent();
      return json(result.body, result.status);
    }),
  ];
  return [...(def.extra?.() ?? []), ...handlers];
}

/** Cuerpo del cambio mezclado sobre lo que hay (los `null` se descartan, como hace Go en proveedores). */
function applyChange(def: MaintenanceDef, request: Request, current: Row, body: Record<string, unknown>): Response {
  const merged: Record<string, unknown> = { ...(def.view(current) as Record<string, unknown>) };
  delete merged.version;
  for (const [key, value] of Object.entries(body)) if (value !== null) merged[key] = value;
  const version = body.version;
  if (version === undefined || version === null) {
    return outcomeError(request, fail([{ field: 'version', code: 'required', message: 'the version that was read is required' }]));
  }
  if (typeof version !== 'number' || !Number.isInteger(version)) {
    return outcomeError(request, fail([{ field: 'body', code: 'json', message: 'the body is not a JSON object of the expected shape' }]));
  }
  if (version !== current.version) {
    return outcomeError(request, conflict(`the ${def.singular} changed after it was read: read it again`));
  }
  const issues = new Validation();
  for (const [field, kind] of Object.entries(def.immutable)) {
    issues.require(sameValue(kind, merged[field], current[field]), field, 'immutable', `the ${field} does not change after registration`);
  }
  if (!issues.ok) return outcomeError(request, fail(issues.issues));
  const patch = def.change!(current, merged);
  if (!patch.ok) return outcomeError(request, patch);
  if (Object.keys(patch.value).length > 0) {
    Object.assign(current, patch.value);
    current.version = (current.version ?? 1) + 1;
  }
  return json(localizeRows(request, def.view(current)));
}

// ---------------------------------------------------------------------------------------------
// Validadores de campo reutilizables
// ---------------------------------------------------------------------------------------------

const DATE = /^(\d{4})-(\d{2})-(\d{2})$/;

export function isDate(value: unknown): value is string {
  if (typeof value !== 'string') return false;
  const m = DATE.exec(value);
  if (!m) return false;
  const d = new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])));
  return d.getUTCFullYear() === Number(m[1]) && d.getUTCMonth() === Number(m[2]) - 1 && d.getUTCDate() === Number(m[3]);
}

export function isDecimal(value: unknown): boolean {
  return typeof value === 'string' ? /^-?\d+(\.\d+)?$/.test(value.trim()) : typeof value === 'number' && Number.isFinite(value);
}

export const str = (value: unknown): string => (typeof value === 'string' ? value.trim() : '');

/** Nuevo id de un alta (UUID v7 determinista por contador). */
export const newId = nextId;
