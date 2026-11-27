// Cliente HTTP sobre `fetch` (PLAN §4.4, D-R14).
//  - Cabeceras: Authorization, X-Client: web, X-Organization-Scope, Accept-Language.
//  - Tiempo máximo de 30 s por petición.
//  - Un 401 lanza UN solo refresco (las demás peticiones esperan) y se reintenta una vez; un segundo
//    401 llama a `onUnauthorized`.
//  - D-R14: NUNCA se reintenta POST, PUT, PATCH ni DELETE; un GET se reintenta hasta 2 veces ante
//    fallo de red o 503, respetando `Retry-After`.
import { ApiError, networkError, parseProblem, timeoutError } from './errors';

export interface HttpClientOptions {
  baseUrl: string;
  getToken(): string | undefined;
  /** Renueva la sesión y devuelve el token nuevo, o `undefined` si no se pudo. */
  refresh(): Promise<string | undefined>;
  getScope(): string | undefined;
  onUnauthorized(): void;
  /** Idioma para `Accept-Language` (por omisión, el `lang` del documento). */
  getLanguage?(): string | undefined;
  /** Se llama tras un 403 (el contexto de permisos puede haber cambiado). */
  onForbidden?(): void;
  fetch?: typeof fetch;
  /** Por omisión 30 000. */
  timeoutMs?: number;
  /** Reintentos de un GET ante red o 503 (por omisión 2). */
  maxGetRetries?: number;
  /** Espera base del retroceso exponencial, en ms (por omisión 300). */
  retryBaseDelayMs?: number;
  /** Tope de lo que se espera por un `Retry-After`, en ms (por omisión 10 000). */
  maxRetryAfterMs?: number;
  /** Espera inyectable (pruebas). */
  sleep?(ms: number, signal?: AbortSignal): Promise<void>;
}

export interface HttpRequestInit extends Omit<RequestInit, 'body'> {
  body?: BodyInit | null;
  /** Cuerpo JSON (pone `Content-Type`). */
  json?: unknown;
  /** Parámetros de la URL (se omiten `undefined`, `null` y `''`; los objetos se aplanan `a.b=`). */
  query?: Record<string, unknown>;
  /** Petición pública: sin `Authorization` y sin refresco ante 401 (login, refresh). */
  skipAuth?: boolean;
  timeoutMs?: number;
}

export interface HttpResponse<T> {
  status: number;
  headers: Headers;
  data: T;
}

export interface HttpClient {
  request<T>(path: string, init?: HttpRequestInit): Promise<T>;
  requestFull<T>(path: string, init?: HttpRequestInit): Promise<HttpResponse<T>>;
}

const DEFAULT_TIMEOUT = 30_000;

function isAbsolute(path: string): boolean {
  return /^[a-z][a-z0-9+.-]*:\/\//i.test(path);
}

function appendQuery(params: URLSearchParams, key: string, value: unknown): void {
  if (value === undefined || value === null || value === '') return;
  if (Array.isArray(value)) {
    const joined = value.filter((v) => v !== undefined && v !== null && v !== '').join(',');
    if (joined !== '') params.append(key, joined);
  } else if (value instanceof Date) {
    params.append(key, value.toISOString());
  } else if (typeof value === 'object') {
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) appendQuery(params, `${key}.${k}`, v);
  } else {
    params.append(key, String(value));
  }
}

/** Une la base, la ruta y los parámetros. Las rutas absolutas (`https://…`) van tal cual. */
export function buildUrl(baseUrl: string, path: string, query?: Record<string, unknown>): string {
  let url: string;
  if (isAbsolute(path)) url = path;
  else url = `${baseUrl.replace(/\/+$/, '')}${path.startsWith('/') ? '' : '/'}${path}`;
  if (query) {
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(query)) appendQuery(params, key, value);
    const qs = params.toString();
    if (qs) url += `${url.includes('?') ? '&' : '?'}${qs}`;
  }
  return url;
}

/** Segundos o fecha HTTP de `Retry-After` -> milisegundos. */
export function parseRetryAfter(value: string | null, now = Date.now()): number | undefined {
  if (!value) return undefined;
  if (/^\s*-?\d+(\.\d+)?\s*$/.test(value)) {
    const seconds = Number(value);
    return seconds >= 0 ? Math.round(seconds * 1000) : undefined;
  }
  const date = Date.parse(value);
  if (Number.isFinite(date)) return Math.max(0, date - now);
  return undefined;
}

function defaultSleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason ?? new DOMException('cancelado', 'AbortError'));
      return;
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(timer);
      reject(signal?.reason ?? new DOMException('cancelado', 'AbortError'));
    };
    signal?.addEventListener('abort', onAbort, { once: true });
  });
}

function anySignal(signals: AbortSignal[]): AbortSignal {
  if (typeof AbortSignal.any === 'function') return AbortSignal.any(signals);
  const controller = new AbortController();
  for (const signal of signals) {
    if (signal.aborted) {
      controller.abort(signal.reason);
      break;
    }
    signal.addEventListener('abort', () => controller.abort(signal.reason), { once: true });
  }
  return controller.signal;
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError';
}

interface RawResponse {
  status: number;
  statusText: string;
  headers: Headers;
  ok: boolean;
  body: unknown;
}

async function readBody(response: Response): Promise<unknown> {
  const text = await response.text();
  if (text === '') return undefined;
  const type = response.headers.get('content-type') ?? '';
  if (/json/i.test(type)) {
    try {
      return JSON.parse(text) as unknown;
    } catch {
      return text;
    }
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return text;
  }
}

export function createHttpClient(options: HttpClientOptions): HttpClient {
  const sleep = options.sleep ?? defaultSleep;
  const maxRetries = options.maxGetRetries ?? 2;
  const baseDelay = options.retryBaseDelayMs ?? 300;
  const maxRetryAfter = options.maxRetryAfterMs ?? 10_000;
  let refreshing: Promise<string | undefined> | null = null;

  function language(): string | undefined {
    const configured = options.getLanguage?.();
    if (configured) return configured;
    if (typeof document !== 'undefined') return document.documentElement.lang || undefined;
    return undefined;
  }

  /** Un único refresco en vuelo; quien llega tarde con un token ya renovado no vuelve a refrescar. */
  function freshToken(tokenUsed: string | undefined): Promise<string | undefined> {
    const current = options.getToken();
    if (current && current !== tokenUsed) return Promise.resolve(current);
    if (!refreshing) {
      refreshing = options
        .refresh()
        .catch(() => undefined)
        .finally(() => {
          refreshing = null;
        });
    }
    return refreshing;
  }

  async function send(
    url: string,
    method: string,
    init: HttpRequestInit,
    token: string | undefined,
  ): Promise<RawResponse> {
    const doFetch = options.fetch ?? globalThis.fetch;
    const { json, query: _query, skipAuth: _skipAuth, timeoutMs: _timeoutMs, signal: callerSignal, ...rest } = init;
    const headers = new Headers(init.headers);
    if (!headers.has('Accept')) headers.set('Accept', 'application/json, application/problem+json');
    headers.set('X-Client', 'web');
    if (token) headers.set('Authorization', `Bearer ${token}`);
    const scope = options.getScope();
    if (scope) headers.set('X-Organization-Scope', scope);
    const lang = language();
    if (lang && !headers.has('Accept-Language')) headers.set('Accept-Language', lang);
    let body = init.body ?? undefined;
    if (json !== undefined) {
      body = JSON.stringify(json);
      if (!headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
    }

    const timeoutMs = init.timeoutMs ?? options.timeoutMs ?? DEFAULT_TIMEOUT;
    const timeout = new AbortController();
    const timer = setTimeout(() => timeout.abort(new Error('timeout')), timeoutMs);
    const signal = callerSignal ? anySignal([callerSignal, timeout.signal]) : timeout.signal;
    try {
      const response = await doFetch(url, { ...rest, method, headers, body, signal });
      const parsed = await readBody(response);
      return {
        status: response.status,
        statusText: response.statusText,
        headers: response.headers,
        ok: response.ok,
        body: parsed,
      };
    } catch (error) {
      if (callerSignal?.aborted) throw error;
      if (timeout.signal.aborted) throw timeoutError(timeoutMs);
      if (isAbortError(error)) throw error;
      throw networkError(error);
    } finally {
      clearTimeout(timer);
    }
  }

  async function requestFull<T>(path: string, init: HttpRequestInit = {}): Promise<HttpResponse<T>> {
    const method = (init.method ?? 'GET').toUpperCase();
    const retryable = method === 'GET' || method === 'HEAD';
    const url = buildUrl(options.baseUrl, path, init.query);
    let authRetried = false;
    let transientRetries = 0;

    for (;;) {
      const tokenUsed = init.skipAuth ? undefined : options.getToken();
      let raw: RawResponse;
      try {
        raw = await send(url, method, init, tokenUsed);
      } catch (error) {
        if (error instanceof ApiError && error.kind === 'network' && error.code === 'network' && retryable && transientRetries < maxRetries) {
          await sleep(baseDelay * 2 ** transientRetries, init.signal ?? undefined);
          transientRetries += 1;
          continue;
        }
        throw error;
      }

      if (raw.status === 401 && !init.skipAuth) {
        if (authRetried) {
          options.onUnauthorized();
          throw parseProblem(401, raw.body, { statusText: raw.statusText });
        }
        authRetried = true;
        const token = await freshToken(tokenUsed);
        if (!token) {
          options.onUnauthorized();
          throw parseProblem(401, raw.body, { statusText: raw.statusText });
        }
        continue;
      }

      if (raw.status === 503 && retryable && transientRetries < maxRetries) {
        const wait = parseRetryAfter(raw.headers.get('Retry-After'));
        const delay = wait !== undefined ? Math.min(wait, maxRetryAfter) : baseDelay * 2 ** transientRetries;
        await sleep(delay, init.signal ?? undefined);
        transientRetries += 1;
        continue;
      }

      if (!raw.ok) {
        if (raw.status === 403 && !init.skipAuth) options.onForbidden?.();
        const retryAfterMs = parseRetryAfter(raw.headers.get('Retry-After'));
        throw parseProblem(raw.status, raw.body, {
          statusText: raw.statusText,
          retryAfter: retryAfterMs !== undefined ? Math.ceil(retryAfterMs / 1000) : undefined,
        });
      }
      return { status: raw.status, headers: raw.headers, data: raw.body as T };
    }
  }

  return {
    requestFull,
    async request<T>(path: string, init?: HttpRequestInit): Promise<T> {
      return (await requestFull<T>(path, init)).data;
    },
  };
}
