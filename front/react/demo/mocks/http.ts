// Utilidades HTTP del backend falso: errores problem+json en las dos formas de Go, paginación,
// autenticación, permisos, latencia determinista y errores forzados con `?fallo=`.
import { HttpResponse } from 'msw';
import { mock, type DemoUser } from './state';

export type ErrorForm = 'v1' | 'list';

export interface FieldIssue {
  field: string;
  code: string;
  message: string;
}

const PROBLEM = 'application/problem+json; charset=utf-8';

const TITLES: Record<number, string> = {
  400: 'Validation Failed',
  401: 'Unauthorized',
  403: 'Forbidden',
  404: 'Not Found',
  405: 'Method Not Allowed',
  409: 'Conflict',
  413: 'Payload Too Large',
  422: 'Unprocessable Entity',
  500: 'Internal Server Error',
  503: 'Service Unavailable',
};

export interface ProblemInit {
  status: number;
  detail?: string;
  code?: string;
  issues?: FieldIssue[];
  form: ErrorForm;
  instance: string;
  headers?: Record<string, string>;
}

/**
 * Respuesta de error de Go. En el contrato de mantenimiento (`/api/v1`) los errores de campo van
 * como diccionario `errors` (campo → mensajes) más `errorCodes`; en el resto, como lista
 * `[{field, code, message}]` con `correlationId`.
 */
export function problem(init: ProblemInit): Response {
  const { status, detail, code, issues = [], form, instance, headers } = init;
  const body: Record<string, unknown> = { title: TITLES[status] ?? 'Error', status };
  if (detail) body.detail = detail;
  body.instance = instance;
  if (code) body.code = code;
  if (issues.length > 0) {
    if (form === 'v1') {
      const errors: Record<string, string[]> = {};
      const errorCodes: Record<string, string[]> = {};
      for (const issue of issues) {
        (errors[issue.field] ??= []).push(issue.message);
        (errorCodes[issue.field] ??= []).push(issue.code);
      }
      body.errors = errors;
      body.errorCodes = errorCodes;
    } else {
      body.errors = issues;
      body.correlationId = correlationId();
    }
  } else if (form === 'list' && status >= 400) {
    body.correlationId = correlationId();
  }
  return new HttpResponse(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': PROBLEM, ...headers },
  });
}

let correlation = 0;
function correlationId(): string {
  correlation += 1;
  return `demo-${correlation.toString(16).padStart(8, '0')}`;
}

/** Forma de los errores según la ruta: contrato de mantenimiento si empieza por /api/v1/. */
export function formOf(path: string): ErrorForm {
  return path.startsWith('/api/v1/') || path === '/api/v1' ? 'v1' : 'list';
}

export function json(body: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return new HttpResponse(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json; charset=utf-8', ...headers },
  });
}

export function noContent(): Response {
  return new HttpResponse(null, { status: 204 });
}

// ---------------------------------------------------------------------------------------------
// Autenticación y permisos
// ---------------------------------------------------------------------------------------------

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
export const isUuid = (value: unknown): value is string => typeof value === 'string' && UUID.test(value);

export interface Caller {
  user: DemoUser | null;
}

/** Rutas que un usuario obligado a cambiar la contraseña sí puede usar (como en Go). */
const OPEN_FOR_PASSWORD_CHANGE = new Set(['/api/auth/context', '/api/auth/change-password']);

function permissionMatches(granted: string, wanted: string): boolean {
  const g = granted.split('.');
  const w = wanted.split('.');
  return g.length === w.length && g.every((part, i) => part === '*' || part.toLowerCase() === w[i]?.toLowerCase());
}

export function userCan(user: DemoUser, permission: string): boolean {
  return user.isGlobalAdmin || user.permissions.some((granted) => permissionMatches(granted, permission));
}

/** Identifica al que llama por su token (sin comprobar permisos). */
export function identify(request: Request): { user: DemoUser | null; expired: boolean } {
  const header = request.headers.get('Authorization') ?? '';
  const token = header.startsWith('Bearer ') ? header.slice(7).trim() : '';
  const session = token ? mock.accessTokens.get(token) : undefined;
  if (!session) return { user: null, expired: false };
  if (session.expiresAt <= Date.now()) return { user: null, expired: true };
  return { user: mock.users.get(session.username) ?? null, expired: false };
}

/**
 * Autoriza una petición. Devuelve la respuesta de error (401/403) o `null` si puede seguir.
 * Con `requireAuth` desactivado (valor por omisión en nodo) no comprueba nada.
 */
export function authorize(request: Request, permission?: string): Response | null {
  if (!mock.options.requireAuth) return null;
  const path = new URL(request.url).pathname;
  const form = formOf(path);
  const { user } = identify(request);
  if (!user) {
    return problem({ status: 401, form, instance: path, headers: { 'WWW-Authenticate': 'Bearer' } });
  }
  if (user.mustChangePassword && !OPEN_FOR_PASSWORD_CHANGE.has(path)) {
    return problem({ status: 403, form, instance: path, code: 'security.password_change_required', detail: 'the password must be changed before using the API' });
  }
  if (permission && !userCan(user, permission)) {
    return problem({ status: 403, form, instance: path });
  }
  return null;
}

/** Ámbito de la petición por `X-Organization-Scope`: lista de ids, o un error 400 si está mal formado. */
export function organizationScope(request: Request): { ids: Set<string> | null } | { error: Response } {
  const raw = request.headers.get('X-Organization-Scope');
  if (!raw) return { ids: null };
  const parts = raw.split(',').map((part) => part.trim()).filter(Boolean);
  if (parts.length === 0 || parts.some((part) => !isUuid(part))) {
    const path = new URL(request.url).pathname;
    return {
      error: problem({
        status: 400,
        form: formOf(path),
        instance: path,
        detail: 'One or more fields are invalid',
        issues: [{ field: 'X-Organization-Scope', code: 'format', message: 'a comma separated list of organization identities' }],
      }),
    };
  }
  return { ids: new Set(parts.map((part) => part.toLowerCase())) };
}

// ---------------------------------------------------------------------------------------------
// Latencia y errores forzados
// ---------------------------------------------------------------------------------------------

function fnv1a(text: string): number {
  let hash = 0x811c9dc5;
  for (let i = 0; i < text.length; i += 1) {
    hash ^= text.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  return hash >>> 0;
}

/** Espera simulada: fija si se configuró un número; si no, entre 15 y 55 ms según método y ruta. */
export async function simulateLatency(request: Request): Promise<void> {
  const { latencyMs } = mock.options;
  const ms = latencyMs === 'auto' ? 15 + (fnv1a(`${request.method} ${new URL(request.url).pathname}`) % 41) : latencyMs;
  if (ms > 0) await new Promise((resolve) => setTimeout(resolve, ms));
}

/** Códigos que `?fallo=` sabe forzar. 202 simula una aprobación pendiente (sólo en las órdenes). */
export const FORCED_FAILURES = [202, 403, 404, 409, 413, 422, 500, 503] as const;

function pageSearch(): URLSearchParams | null {
  const search = (globalThis as { location?: { search?: string } }).location?.search;
  return typeof search === 'string' ? new URLSearchParams(search) : null;
}

/**
 * Fallo forzado: `?fallo=409` en la URL de la petición, o en la de la página (sólo escrituras,
 * para no romper las lecturas mientras se prueba un guardado).
 */
export function forcedFailure(request: Request): number | null {
  const url = new URL(request.url);
  let value = url.searchParams.get('fallo');
  if (value === null && request.method !== 'GET' && request.method !== 'HEAD' && !url.pathname.endsWith('/search')) {
    value = pageSearch()?.get('fallo') ?? null;
  }
  const code = value === null ? NaN : Number(value);
  return (FORCED_FAILURES as readonly number[]).includes(code) ? code : null;
}

/** ¿Se pide una orden con aprobación pendiente? (`fallo=202`, `aprobacion=1` o la opción del mock) */
export function approvalForced(request: Request): boolean {
  if (mock.options.requireApproval) return true;
  const url = new URL(request.url);
  return forcedFailure(request) === 202 || url.searchParams.get('aprobacion') === '1' || pageSearch()?.get('aprobacion') === '1';
}

/** Respuesta de error para un fallo forzado (el 202 no es un error: lo trata cada orden). */
export function forcedFailureResponse(request: Request, code: number): Response | null {
  if (code === 202) return null;
  const path = new URL(request.url).pathname;
  const form = formOf(path);
  const details: Record<number, { detail: string; code?: string }> = {
    403: { detail: 'forced by ?fallo=403' },
    404: { detail: 'forced by ?fallo=404' },
    409: { detail: 'the record changed after it was read: read it again' },
    413: { detail: 'the body is larger than 1 MiB' },
    422: { detail: 'forced business rule violation', code: 'demo.forced_failure' },
    500: { detail: 'forced by ?fallo=500' },
    503: { detail: 'forced by ?fallo=503' },
  };
  const spec = details[code] ?? { detail: 'forced' };
  return problem({
    status: code,
    form,
    instance: path,
    detail: spec.detail,
    code: spec.code,
    headers: code === 503 ? { 'Retry-After': '5' } : undefined,
  });
}

/**
 * Pasos comunes de cada petición: latencia, fallo forzado y autorización. Devuelve la respuesta si
 * hay que cortar, o `null` si se puede seguir.
 */
export async function preflight(request: Request, permission?: string): Promise<Response | null> {
  await simulateLatency(request);
  const denied = authorize(request, permission);
  if (denied) return denied;
  const forced = forcedFailure(request);
  if (forced !== null) return forcedFailureResponse(request, forced);
  return null;
}

// ---------------------------------------------------------------------------------------------
// Cuerpo, paginación, orden y texto
// ---------------------------------------------------------------------------------------------

/** Lee un cuerpo JSON de objeto; devuelve `null` si no lo es o si supera 1 MiB. */
export async function readObject(request: Request): Promise<{ body: Record<string, unknown> } | { error: 'json' | 'large' }> {
  const text = await request.text();
  if (text.length > 1 << 20) return { error: 'large' };
  try {
    const parsed: unknown = JSON.parse(text);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) return { body: parsed as Record<string, unknown> };
  } catch {
    // cae al error
  }
  return { error: 'json' };
}

export const DEFAULT_PAGE_SIZE = 20;
export const MAX_PAGE_SIZE = 1000;

export interface PageRequest {
  page: number;
  size: number;
  sortBy: string;
  order: string;
  filters: Map<string, string>;
}

const collator = new Intl.Collator('es', { sensitivity: 'base', numeric: true });

/** Compara dos valores de orden: números como números, el resto por texto en español. */
export function compareValues(a: unknown, b: unknown): number {
  if (typeof a === 'number' && typeof b === 'number') return a - b;
  const na = typeof a === 'string' && a !== '' && /^-?\d+(\.\d+)?$/.test(a) ? Number(a) : NaN;
  const nb = typeof b === 'string' && b !== '' && /^-?\d+(\.\d+)?$/.test(b) ? Number(b) : NaN;
  if (!Number.isNaN(na) && !Number.isNaN(nb)) return na - nb;
  return collator.compare(String(a ?? ''), String(b ?? ''));
}

/** Texto sin tildes y en minúsculas, para las búsquedas. */
export function fold(text: unknown): string {
  return String(text ?? '')
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase();
}

export function totalPages(total: number, size: number): number {
  return size > 0 ? Math.ceil(total / size) : 0;
}

export function slice<T>(rows: T[], page: number, size: number): T[] {
  return rows.slice((page - 1) * size, page * size);
}

/** Texto de un valor escalar de filtro tal como lo leería Go (`fmt.Sprint`). */
export function scalarText(value: unknown): string | null {
  if (value === null || value === undefined) return '';
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return null;
}

/** Página `{items,total,number,size,totalPages}` de las rutas que no son del contrato de mantenimiento. */
export function numberSizePage<T>(items: T[], total: number, number: number, size: number) {
  return { items, total, number, size, totalPages: totalPages(total, size) };
}

/** `page` y `size` como los normaliza Go fuera de `/api/v1`: por omisión 1 y 20, y `size` ≤ 1000. */
export function parseNumberSize(url: URL): { number: number; size: number } {
  const read = (key: string) => {
    const raw = url.searchParams.get(key);
    return raw !== null && /^-?\d+$/.test(raw) ? Number(raw) : 0;
  };
  const number = read('page');
  const size = read('size');
  return {
    number: number >= 1 ? number : 1,
    size: size <= 0 ? DEFAULT_PAGE_SIZE : Math.min(size, MAX_PAGE_SIZE),
  };
}
