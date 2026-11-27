import { describe, expect, it } from 'vitest';
import {
  ApiError,
  NO_FIELD,
  describeApiError,
  describeFieldError,
  isApiError,
  kindForStatus,
  localValidationError,
  networkError,
  parseProblem,
  splitFieldErrors,
  timeoutError,
} from './errors';

const DICT_FORM = {
  title: 'Validation Failed',
  status: 400,
  detail: 'One or more fields are invalid',
  instance: '/api/v1/facilities',
  errors: { type: ['type must be a facility type id'], name: ['is required', 'is too long'] },
  errorCodes: { type: ['format'], name: ['required', 'maxLength'] },
};

const LIST_FORM = {
  title: 'Validation Failed',
  status: 400,
  detail: 'One or more fields are invalid',
  correlationId: 'c-1',
  errors: [
    { field: 'type', code: 'format', message: 'type must be a facility type id' },
    { field: 'name', code: 'required', message: 'is required' },
    { field: 'name', code: 'maxLength', message: 'is too long' },
  ],
};

describe('problem+json en sus dos formas', () => {
  it('`errors` diccionario + `errorCodes` y `errors` lista dan el mismo ApiErrorShape', () => {
    const dict = parseProblem(400, DICT_FORM);
    const list = parseProblem(400, LIST_FORM);
    expect(dict.kind).toBe('validation');
    expect(list.kind).toBe('validation');
    expect(dict.status).toBe(400);
    expect(dict.title).toBe('Validation Failed');
    expect(dict.detail).toBe('One or more fields are invalid');
    const expected = {
      type: [{ code: 'format', message: 'type must be a facility type id' }],
      name: [
        { code: 'required', message: 'is required' },
        { code: 'maxLength', message: 'is too long' },
      ],
    };
    expect(dict.fieldErrors).toEqual(expected);
    expect(list.fieldErrors).toEqual(expected);
    expect(list.correlationId).toBe('c-1');
    expect(dict).toBeInstanceOf(ApiError);
    expect(dict).toBeInstanceOf(Error);
    expect(isApiError(dict)).toBe(true);
    expect(isApiError(new Error('x'))).toBe(false);
  });

  it('errores sin código, con un solo código para varios mensajes y valores sueltos', () => {
    const e = parseProblem(400, {
      errors: { a: ['uno'], b: ['x', 'y'], c: 'texto suelto', d: 5 },
      errorCodes: { b: ['range'], c: 'format' },
    });
    expect(e.fieldErrors.a).toEqual([{ code: 'invalid', message: 'uno' }]);
    expect(e.fieldErrors.b).toEqual([
      { code: 'range', message: 'x' },
      { code: 'range', message: 'y' },
    ]);
    expect(e.fieldErrors.c).toEqual([{ code: 'format', message: 'texto suelto' }]);
    expect(e.fieldErrors.d).toEqual([{ code: 'invalid', message: '5' }]);
  });

  it('la lista sin `field` va a la clave `_`; los elementos ilegibles se saltan', () => {
    const e = parseProblem(400, { errors: [{ code: 'x', message: 'm' }, 'basura', { field: 'f' }] });
    expect(e.fieldErrors[NO_FIELD]).toEqual([{ code: 'x', message: 'm' }]);
    expect(e.fieldErrors.f).toEqual([{ code: 'invalid', message: '' }]);
  });

  it('`code`/`errorCode`, título por omisión y cuerpos que no son JSON', () => {
    expect(parseProblem(422, { code: 'hr.reporting_cycle', title: 'x' }).code).toBe('hr.reporting_cycle');
    expect(parseProblem(422, { errorCode: 'approval.self_resolution' }).code).toBe('approval.self_resolution');
    expect(parseProblem(502, '<html>bad gateway</html>').title).toBe('HTTP 502');
    expect(parseProblem(502, undefined, { statusText: 'Bad Gateway' }).title).toBe('Bad Gateway');
    expect(parseProblem(503, null, { retryAfter: 5 }).retryAfter).toBe(5);
  });
});

describe('tipo de error por estado', () => {
  it.each([
    [400, {}, 'validation'],
    [401, {}, 'unauthorized'],
    [403, {}, 'forbidden'],
    [404, {}, 'notfound'],
    [409, {}, 'conflict'],
    [413, {}, 'too-large'],
    [422, { code: 'hr.work_center_in_use' }, 'business'],
    [422, {}, 'business'],
    [422, { errors: { a: ['x'] } }, 'validation'],
    [422, { code: 'x', errors: { a: ['x'] } }, 'business'],
    [408, {}, 'network'],
    [429, {}, 'server'],
    [500, {}, 'server'],
    [503, {}, 'server'],
    [418, {}, 'server'],
  ] as const)('%s con %j -> %s', (status, body, kind) => {
    expect(parseProblem(status, body).kind).toBe(kind);
  });

  it('kindForStatus es la misma regla', () => {
    expect(kindForStatus(0, false, false)).toBe('network');
    expect(kindForStatus(422, true, true)).toBe('business');
  });

  it('red, tiempo agotado y validación local', () => {
    const net = networkError(new TypeError('fetch failed'));
    expect(net).toMatchObject({ kind: 'network', status: 0, code: 'network', detail: 'fetch failed' });
    expect(networkError().detail).toBeUndefined();
    expect(timeoutError(30_000)).toMatchObject({ kind: 'network', code: 'timeout', status: 0 });
    const local = localValidationError('version', 'required', 'falta');
    expect(local).toMatchObject({ kind: 'validation', status: 400 });
    expect(local.fieldErrors.version).toEqual([{ code: 'required', message: 'falta' }]);
  });
});

describe('mensajes para el usuario', () => {
  it('por código de negocio o por tipo, con el texto del servidor sólo como reserva', () => {
    expect(describeApiError(parseProblem(422, { code: 'hr.reporting_cycle', detail: 'cycle!' }))).toEqual({
      key: 'errors:hr.reporting_cycle',
      fallback: 'cycle!',
    });
    expect(describeApiError(parseProblem(409, { title: 'Conflict' }))).toEqual({ key: 'errors:conflict', fallback: 'Conflict' });
    expect(describeApiError(parseProblem(403, {})).key).toBe('errors:forbidden');
    expect(describeApiError(parseProblem(404, {})).key).toBe('errors:notfound');
    expect(describeApiError(parseProblem(401, {})).key).toBe('errors:unauthorized');
    expect(describeApiError(parseProblem(413, {})).key).toBe('errors:too-large');
    expect(describeApiError(parseProblem(500, {})).key).toBe('errors:server');
    expect(describeApiError(parseProblem(400, {})).key).toBe('errors:invalid');
    expect(describeApiError(networkError()).key).toBe('errors:network');
    expect(describeApiError(timeoutError(1)).key).toBe('errors:timeout');
    expect(describeFieldError({ code: 'required', message: 'is required' })).toEqual({
      key: 'errors:required',
      fallback: 'is required',
    });
  });

  it('splitFieldErrors separa los errores de campos conocidos de los demás', () => {
    const e = parseProblem(400, DICT_FORM);
    const { matched, unmatched } = splitFieldErrors(e, ['name']);
    expect(Object.keys(matched)).toEqual(['name']);
    expect(unmatched).toEqual([{ field: 'type', code: 'format', message: 'type must be a facility type id' }]);
  });
});
