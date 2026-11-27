// Errores del adaptador. `ApiError` implementa `ApiErrorShape` y normaliza las DOS formas de
// problem+json de Go (A2 §2):
//   - rutas /api/v1:  errors = { campo: [mensajes] } y errorCodes = { campo: [códigos] }
//   - resto de rutas: errors = [{ field, code, message }] y correlationId
// Los errores sin campo (`errors` ausente) no tienen entrada en `fieldErrors`; los de lista sin
// `field` van a la clave `_` (NO_FIELD).
import type { ApiErrorKind, ApiErrorShape } from '../contracts';

/** Clave de `fieldErrors` para los errores de lista que no traen `field`. */
export const NO_FIELD = '_';

/** Error del adaptador, con la forma de `ApiErrorShape`. */
export class ApiError extends Error implements ApiErrorShape {
  readonly kind: ApiErrorKind;
  readonly status: number;
  readonly title: string;
  readonly detail?: string;
  readonly code?: string;
  readonly fieldErrors: ApiErrorShape['fieldErrors'];
  /** Identificador de correlación del servidor, si lo trae. */
  readonly correlationId?: string;
  /** Segundos que pide esperar el servidor (`Retry-After`), si los hay. */
  readonly retryAfter?: number;

  constructor(shape: ApiErrorShape & { correlationId?: string; retryAfter?: number }) {
    super(shape.title);
    this.name = 'ApiError';
    this.kind = shape.kind;
    this.status = shape.status;
    this.title = shape.title;
    this.detail = shape.detail;
    this.code = shape.code;
    this.fieldErrors = shape.fieldErrors;
    this.correlationId = shape.correlationId;
    this.retryAfter = shape.retryAfter;
  }
}

export function isApiError(value: unknown): value is ApiError {
  return value instanceof ApiError;
}

type Obj = Record<string, unknown>;

function isObj(value: unknown): value is Obj {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function str(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() !== '' ? value : undefined;
}

/** Tipo de error según el estado HTTP y lo que trae el cuerpo. */
export function kindForStatus(status: number, hasCode: boolean, hasFieldErrors: boolean): ApiErrorKind {
  if (status === 400) return 'validation';
  if (status === 401) return 'unauthorized';
  if (status === 403) return 'forbidden';
  if (status === 404) return 'notfound';
  if (status === 409) return 'conflict';
  if (status === 413) return 'too-large';
  if (status === 422) return hasCode || !hasFieldErrors ? 'business' : 'validation';
  if (status === 0 || status === 408) return 'network';
  return 'server';
}

function toList(value: unknown): unknown[] {
  if (Array.isArray(value)) return value;
  if (value === undefined || value === null) return [];
  return [value];
}

function parseFieldErrors(problem: Obj): ApiErrorShape['fieldErrors'] {
  const out: ApiErrorShape['fieldErrors'] = {};
  const push = (field: string, code: string, message: string) => {
    (out[field] ??= []).push({ code, message });
  };
  const { errors } = problem;
  if (Array.isArray(errors)) {
    for (const item of errors) {
      if (!isObj(item)) continue;
      push(str(item.field) ?? NO_FIELD, str(item.code) ?? 'invalid', str(item.message) ?? '');
    }
  } else if (isObj(errors)) {
    const codes = isObj(problem.errorCodes) ? problem.errorCodes : {};
    for (const [field, messages] of Object.entries(errors)) {
      const messageList = toList(messages);
      const codeList = toList(codes[field]);
      messageList.forEach((message, i) => {
        const code = str(codeList[i]) ?? (codeList.length === 1 ? str(codeList[0]) : undefined);
        push(field, code ?? 'invalid', typeof message === 'string' ? message : String(message));
      });
    }
  }
  return out;
}

/** Convierte una respuesta de error (estado + cuerpo ya leído) en un `ApiError`. */
export function parseProblem(
  status: number,
  body: unknown,
  extra: { statusText?: string; retryAfter?: number } = {},
): ApiError {
  const problem = isObj(body) ? body : {};
  const fieldErrors = parseFieldErrors(problem);
  const code = str(problem.code) ?? str(problem.errorCode);
  const title = str(problem.title) ?? str(extra.statusText) ?? `HTTP ${status}`;
  return new ApiError({
    kind: kindForStatus(status, code !== undefined, Object.keys(fieldErrors).length > 0),
    status,
    title,
    detail: str(problem.detail),
    code,
    fieldErrors,
    correlationId: str(problem.correlationId),
    retryAfter: extra.retryAfter,
  });
}

/** Fallo de red (sin respuesta). */
export function networkError(cause?: unknown): ApiError {
  const detail = cause instanceof Error ? cause.message : undefined;
  return new ApiError({ kind: 'network', status: 0, title: 'Network error', detail, code: 'network', fieldErrors: {} });
}

/** Se agotó el tiempo máximo de la petición. */
export function timeoutError(ms: number): ApiError {
  return new ApiError({
    kind: 'network',
    status: 0,
    title: 'Request timed out',
    detail: `no response after ${ms} ms`,
    code: 'timeout',
    fieldErrors: {},
  });
}

/** Error de validación detectado antes de enviar nada (p. ej. falta `version`). */
export function localValidationError(field: string, code: string, message: string): ApiError {
  return new ApiError({
    kind: 'validation',
    status: 400,
    title: 'Validation Failed',
    detail: message,
    fieldErrors: { [field]: [{ code, message }] },
  });
}

const KIND_KEY: Record<ApiErrorKind, string> = {
  validation: 'invalid',
  conflict: 'conflict',
  forbidden: 'forbidden',
  unauthorized: 'unauthorized',
  notfound: 'notfound',
  business: 'business',
  'too-large': 'too-large',
  network: 'network',
  server: 'server',
};

export interface ErrorText {
  /** Clave de i18n (espacio `errors`), p. ej. `errors:hr.reporting_cycle`. */
  key: string;
  /** Texto del servidor, sólo como reserva si la clave no está traducida. */
  fallback: string;
}

/** Mensaje general de un error para el usuario: por código de negocio o por tipo. */
export function describeApiError(error: ApiErrorShape): ErrorText {
  const fallback = error.detail ?? error.title;
  if (error.code) return { key: `errors:${error.code}`, fallback };
  return { key: `errors:${KIND_KEY[error.kind]}`, fallback };
}

/** Mensaje de un error de campo por su código (`required`, `format`, `range`…). */
export function describeFieldError(fieldError: { code: string; message: string }): ErrorText {
  return { key: `errors:${fieldError.code}`, fallback: fieldError.message };
}

/** Errores de campo con el que casa algún campo del formulario y el resto (para el aviso de la ficha). */
export function splitFieldErrors(
  error: ApiErrorShape,
  knownFields: readonly string[],
): { matched: ApiErrorShape['fieldErrors']; unmatched: { field: string; code: string; message: string }[] } {
  const known = new Set(knownFields);
  const matched: ApiErrorShape['fieldErrors'] = {};
  const unmatched: { field: string; code: string; message: string }[] = [];
  for (const [field, list] of Object.entries(error.fieldErrors)) {
    if (known.has(field)) matched[field] = list;
    else for (const item of list) unmatched.push({ field, ...item });
  }
  return { matched, unmatched };
}
