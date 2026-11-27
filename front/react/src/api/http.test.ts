// Cliente HTTP: cabeceras, refresco único, D-R14 (reintentos), tiempo máximo y análisis de respuestas.
import { delay, http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from './errors';
import { buildUrl, createHttpClient, parseRetryAfter, type HttpClientOptions } from './http';
import { API, createGoMock } from './__fixtures__/go-server';

const mock = createGoMock();
const { server, state } = mock;

beforeAll(() => server.listen({ onUnhandledFrame: 'error' }));
afterAll(() => server.close());
afterEach(() => server.resetHandlers());
beforeEach(() => {
  mock.calls.length = 0;
  state.validAccess = 'access-1';
  state.validRefresh = 'refresh-1';
  state.refreshCount = 0;
});

function makeClient(overrides: Partial<HttpClientOptions> = {}) {
  let token: string | undefined = 'access-1';
  const sleeps: number[] = [];
  const events = { unauthorized: 0, forbidden: 0, refreshes: 0 };
  const client = createHttpClient({
    baseUrl: API,
    getToken: () => token,
    refresh: async () => {
      events.refreshes += 1;
      const response = await fetch(`${API}/auth/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refreshToken: state.validRefresh }),
      });
      if (!response.ok) return undefined;
      token = ((await response.json()) as { accessToken: string }).accessToken;
      return token;
    },
    getScope: () => undefined,
    onUnauthorized: () => {
      events.unauthorized += 1;
    },
    onForbidden: () => {
      events.forbidden += 1;
    },
    sleep: async (ms) => {
      sleeps.push(ms);
    },
    ...overrides,
  });
  return { client, sleeps, events, setToken: (t: string | undefined) => (token = t) };
}

describe('cabeceras', () => {
  it('manda Authorization, X-Client, X-Organization-Scope, Accept-Language y Content-Type', async () => {
    const { client } = makeClient({ getScope: () => 'org-1,org-2', getLanguage: () => 'es-ES' });
    await client.request('/v1/facilities/f1');
    await client.request('/v1/facilities/f1', { method: 'PUT', json: { name: 'x', version: 1 } });
    const [get, put] = mock.calls;
    expect(get!.headers.authorization).toBe('Bearer access-1');
    expect(get!.headers['x-client']).toBe('web');
    expect(get!.headers['x-organization-scope']).toBe('org-1,org-2');
    expect(get!.headers['accept-language']).toBe('es-ES');
    expect(get!.headers.accept).toContain('application/json');
    expect(put!.headers['content-type']).toBe('application/json');
    expect(put!.body).toEqual({ name: 'x', version: 1 });
  });

  it('Accept-Language sigue al idioma activo: cada petición lleva el de ese momento y `getLanguage` manda sobre el `lang` del documento', async () => {
    let active = 'es';
    const { client } = makeClient({ getLanguage: () => active });
    await client.request('/v1/facilities/f1');
    active = 'en';
    await client.request('/v1/facilities/f1');
    expect(mock.calls.map((call) => call.headers['accept-language'])).toEqual(['es', 'en']);

    // Sin `getLanguage`, sale del `lang` de <html> (el que mantiene el proveedor de i18n al cambiar de idioma).
    const root = { documentElement: { lang: 'en' } };
    vi.stubGlobal('document', root);
    try {
      const fromDocument = makeClient({ getLanguage: undefined });
      await fromDocument.client.request('/v1/facilities/f1');
      root.documentElement.lang = 'es';
      await fromDocument.client.request('/v1/facilities/f1');
      expect(mock.calls.slice(-2).map((call) => call.headers['accept-language'])).toEqual(['en', 'es']);
      // Una cabecera explícita de la petición no se pisa.
      await fromDocument.client.request('/v1/facilities/f1', { headers: { 'Accept-Language': 'fr' } });
      expect(mock.calls.at(-1)!.headers['accept-language']).toBe('fr');
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it('sin token, sin ámbito y con `skipAuth` no manda Authorization ni X-Organization-Scope', async () => {
    const { client } = makeClient({ getToken: () => undefined, getLanguage: () => undefined });
    await client.request('/v1/facilities/f1').catch(() => undefined);
    expect(mock.calls[0]!.headers.authorization).toBeUndefined();
    expect(mock.calls[0]!.headers['x-organization-scope']).toBeUndefined();
    expect(mock.calls[0]!.headers['x-client']).toBe('web');
    const authed = makeClient({ getScope: () => 'x' });
    await authed.client.request('/auth/logout', { method: 'POST', json: {}, skipAuth: true });
    expect(mock.calls.at(-1)!.headers.authorization).toBeUndefined();
  });
});

describe('refresco único ante 401', () => {
  it('con tres peticiones concurrentes que reciben 401 se hace exactamente un POST /auth/refresh y las tres se reintentan una vez', async () => {
    const { client, events, setToken } = makeClient();
    setToken('caducado');
    const results = await Promise.all([
      client.request<{ id: string }>('/v1/facilities/f1'),
      client.request<{ id: string }>('/v1/facilities/f2'),
      client.request<{ id: string }>('/v1/facilities/f1'),
    ]);
    expect(results.map((r) => r.id)).toEqual(['f1', 'f2', 'f1']);
    expect(mock.callsTo('POST', '/api/auth/refresh')).toHaveLength(1);
    expect(state.refreshCount).toBe(1);
    expect(events.refreshes).toBe(1);
    expect(events.unauthorized).toBe(0);
    // 3 intentos con el token caducado + 3 reintentos con el nuevo
    const attempts = mock.calls.filter((c) => c.path.startsWith('/api/v1/facilities/'));
    expect(attempts).toHaveLength(6);
    expect(attempts.filter((c) => c.headers.authorization === 'Bearer caducado')).toHaveLength(3);
    expect(attempts.filter((c) => c.headers.authorization === 'Bearer access-2')).toHaveLength(3);
  });

  it('quien llega con 401 después de renovado el token no vuelve a refrescar', async () => {
    const { client, events, setToken } = makeClient();
    let first = true;
    server.use(
      http.get(`${API}/v1/facilities/slow`, ({ request }) => {
        if (first) {
          first = false;
          // mientras esta petición estaba en vuelo, otra ya renovó el token
          setToken('fresco');
          return HttpResponse.json({ title: 'Unauthorized', status: 401 }, { status: 401 });
        }
        return HttpResponse.json({ id: 'slow', seen: request.headers.get('authorization') });
      }),
    );
    const result = await client.request<{ seen: string }>('/v1/facilities/slow');
    expect(result.seen).toBe('Bearer fresco');
    expect(events.refreshes).toBe(0);
  });

  it('un segundo 401 tras el refresco llama a `onUnauthorized` y falla', async () => {
    const { client, events } = makeClient();
    server.use(http.get(`${API}/v1/always401`, () => HttpResponse.json({ title: 'Unauthorized' }, { status: 401 })));
    const error = await client.request('/v1/always401').catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ kind: 'unauthorized', status: 401 });
    expect(events.unauthorized).toBe(1);
    expect(events.refreshes).toBe(1);
  });

  it('si el refresco no da token, `onUnauthorized` y error 401', async () => {
    const { client, events, setToken } = makeClient({ refresh: async () => undefined });
    setToken('caducado');
    await expect(client.request('/v1/facilities/f1')).rejects.toMatchObject({ kind: 'unauthorized' });
    expect(events.unauthorized).toBe(1);
    const throwing = makeClient({
      refresh: async () => {
        throw new Error('boom');
      },
    });
    throwing.setToken('caducado');
    await expect(throwing.client.request('/v1/facilities/f1')).rejects.toMatchObject({ kind: 'unauthorized' });
    expect(throwing.events.unauthorized).toBe(1);
  });

  it('una petición pública (`skipAuth`) con 401 no refresca ni cierra la sesión', async () => {
    const { client, events } = makeClient();
    const error = await client
      .request('/auth/login', { method: 'POST', json: { username: 'x', password: 'y' }, skipAuth: true })
      .catch((e: unknown) => e);
    expect(error).toMatchObject({ kind: 'unauthorized' });
    expect(events.refreshes).toBe(0);
    expect(events.unauthorized).toBe(0);
  });
});

describe('D-R14: reintentos', () => {
  it.each(['PUT', 'POST', 'PATCH', 'DELETE'] as const)('un %s que recibe 503 no se reintenta', async (method) => {
    let hits = 0;
    server.use(
      http.all(`${API}/v1/things`, () => {
        hits += 1;
        return HttpResponse.json({ title: 'Service Unavailable' }, { status: 503, headers: { 'Retry-After': '1' } });
      }),
    );
    const { client, sleeps } = makeClient();
    const error = await client.request('/v1/things', { method, json: { a: 1 } }).catch((e: unknown) => e);
    expect(error).toMatchObject({ kind: 'server', status: 503, retryAfter: 1 });
    expect(hits).toBe(1);
    expect(sleeps).toEqual([]);
  });

  it('un PUT con fallo de red tampoco se reintenta', async () => {
    let hits = 0;
    server.use(
      http.put(`${API}/v1/things`, () => {
        hits += 1;
        return HttpResponse.error();
      }),
    );
    const { client } = makeClient();
    await expect(client.request('/v1/things', { method: 'PUT', json: {} })).rejects.toMatchObject({ kind: 'network' });
    expect(hits).toBe(1);
  });

  it('un GET con 503 y Retry-After se reintenta como máximo 2 veces, esperando lo que pide el servidor', async () => {
    let hits = 0;
    server.use(
      http.get(`${API}/v1/things`, () => {
        hits += 1;
        return HttpResponse.json({ title: 'Service Unavailable' }, { status: 503, headers: { 'Retry-After': '2' } });
      }),
    );
    const { client, sleeps } = makeClient();
    await expect(client.request('/v1/things')).rejects.toMatchObject({ kind: 'server', status: 503 });
    expect(hits).toBe(3);
    expect(sleeps).toEqual([2000, 2000]);
  });

  it('un GET con 503 que luego responde bien devuelve los datos', async () => {
    let hits = 0;
    server.use(
      http.get(`${API}/v1/things`, () => {
        hits += 1;
        return hits === 1
          ? HttpResponse.json({ title: 'x' }, { status: 503 })
          : HttpResponse.json({ ok: true });
      }),
    );
    const { client, sleeps } = makeClient({ retryBaseDelayMs: 100 });
    await expect(client.request('/v1/things')).resolves.toEqual({ ok: true });
    expect(hits).toBe(2);
    expect(sleeps).toEqual([100]); // sin Retry-After: retroceso exponencial
  });

  it('el Retry-After muy largo se acota; el retroceso exponencial crece', async () => {
    server.use(
      http.get(`${API}/v1/long`, () =>
        HttpResponse.json({}, { status: 503, headers: { 'Retry-After': '3600' } }),
      ),
      http.get(`${API}/v1/plain`, () => HttpResponse.json({}, { status: 503 })),
    );
    const { client, sleeps } = makeClient({ maxRetryAfterMs: 5000, retryBaseDelayMs: 50 });
    await client.request('/v1/long').catch(() => undefined);
    expect(sleeps).toEqual([5000, 5000]);
    sleeps.length = 0;
    await client.request('/v1/plain').catch(() => undefined);
    expect(sleeps).toEqual([50, 100]);
  });

  it('un GET con fallo de red se reintenta 2 veces y acaba en error de red', async () => {
    let hits = 0;
    server.use(
      http.get(`${API}/v1/down`, () => {
        hits += 1;
        return HttpResponse.error();
      }),
    );
    const { client, sleeps } = makeClient({ retryBaseDelayMs: 10 });
    const error = await client.request('/v1/down').catch((e: unknown) => e);
    expect(error).toMatchObject({ kind: 'network', status: 0, code: 'network' });
    expect(hits).toBe(3);
    expect(sleeps).toEqual([10, 20]);
    hits = 0;
    const strict = makeClient({ maxGetRetries: 0 });
    await strict.client.request('/v1/down').catch(() => undefined);
    expect(hits).toBe(1);
  });
});

describe('tiempo máximo y cancelación', () => {
  it('agota el tiempo (30 s por omisión, configurable) con un error de red de código `timeout` que no se reintenta', async () => {
    let hits = 0;
    server.use(
      http.get(`${API}/v1/slow`, async () => {
        hits += 1;
        await delay(300);
        return HttpResponse.json({});
      }),
    );
    const { client } = makeClient({ timeoutMs: 40 });
    const error = await client.request('/v1/slow').catch((e: unknown) => e);
    expect(error).toMatchObject({ kind: 'network', code: 'timeout' });
    expect(hits).toBe(1);
    const perCall = await client.request('/v1/slow', { timeoutMs: 20 }).catch((e: unknown) => e);
    expect(perCall).toMatchObject({ code: 'timeout' });
  });

  it('cancelar con la señal del llamante lanza AbortError, no ApiError', async () => {
    server.use(
      http.get(`${API}/v1/slow`, async () => {
        await delay(300);
        return HttpResponse.json({});
      }),
    );
    const { client } = makeClient();
    const controller = new AbortController();
    const pending = client.request('/v1/slow', { signal: controller.signal }).catch((e: unknown) => e);
    setTimeout(() => controller.abort(), 20);
    const error = await pending;
    expect(error).not.toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ name: 'AbortError' });
  });

  it('una señal ya cancelada no hace ninguna petición y las esperas la respetan', async () => {
    const { client } = makeClient({ sleep: undefined, retryBaseDelayMs: 5000 });
    server.use(http.get(`${API}/v1/down`, () => HttpResponse.error()));
    const controller = new AbortController();
    const pending = client.request('/v1/down', { signal: controller.signal }).catch((e: unknown) => e);
    setTimeout(() => controller.abort(), 30);
    expect(await pending).toMatchObject({ name: 'AbortError' });
    controller.abort();
    mock.calls.length = 0;
    await client.request('/v1/facilities/f1', { signal: controller.signal }).catch(() => undefined);
    expect(mock.calls).toHaveLength(0);
  });

  it('con la espera por defecto reintenta de verdad (retroceso corto)', async () => {
    let hits = 0;
    server.use(
      http.get(`${API}/v1/flaky`, () => {
        hits += 1;
        return hits < 2 ? HttpResponse.error() : HttpResponse.json({ ok: true });
      }),
    );
    const { client } = makeClient({ sleep: undefined, retryBaseDelayMs: 5 });
    await expect(client.request('/v1/flaky')).resolves.toEqual({ ok: true });
    expect(hits).toBe(2);
  });
});

describe('respuestas', () => {
  it('204 sin cuerpo, texto, JSON mal formado y problem+json', async () => {
    server.use(
      http.get(`${API}/v1/empty`, () => new HttpResponse(null, { status: 204 })),
      http.get(`${API}/v1/text`, () => new HttpResponse('hola', { headers: { 'Content-Type': 'text/plain' } })),
      http.get(`${API}/v1/json-as-text`, () => new HttpResponse('{"a":1}', { headers: { 'Content-Type': 'text/plain' } })),
      http.get(`${API}/v1/broken`, () => new HttpResponse('{rota', { headers: { 'Content-Type': 'application/json' } })),
      http.get(`${API}/v1/html`, () => new HttpResponse('<html>', { status: 502, headers: { 'Content-Type': 'text/html' } })),
    );
    const { client } = makeClient();
    expect(await client.request('/v1/empty')).toBeUndefined();
    expect(await client.request('/v1/text')).toBe('hola');
    expect(await client.request('/v1/json-as-text')).toEqual({ a: 1 });
    expect(await client.request('/v1/broken')).toBe('{rota');
    const full = await client.requestFull('/v1/empty');
    expect(full.status).toBe(204);
    await expect(client.request('/v1/html', { skipAuth: true })).rejects.toMatchObject({ status: 502, kind: 'server' });
  });

  it('403 avisa a `onForbidden`; en una petición pública no', async () => {
    server.use(http.get(`${API}/v1/secret`, () => HttpResponse.json({ title: 'Forbidden', status: 403 }, { status: 403 })));
    const { client, events } = makeClient();
    await expect(client.request('/v1/secret')).rejects.toMatchObject({ kind: 'forbidden' });
    expect(events.forbidden).toBe(1);
    await client.request('/v1/secret', { skipAuth: true }).catch(() => undefined);
    expect(events.forbidden).toBe(1);
  });

  it('409, 413 y 422 con código llegan como ApiError tipados', async () => {
    server.use(
      http.put(`${API}/v1/things`, () => HttpResponse.json({ title: 'Conflict', detail: 'read it again' }, { status: 409 })),
      http.post(`${API}/v1/things`, () => HttpResponse.json({ title: 'Request Entity Too Large' }, { status: 413 })),
      http.delete(`${API}/v1/things`, () =>
        HttpResponse.json({ title: 'Unprocessable', code: 'hr.work_center_in_use' }, { status: 422 }),
      ),
    );
    const { client } = makeClient();
    expect(await client.request('/v1/things', { method: 'PUT', json: {} }).catch((e: unknown) => e)).toMatchObject({
      kind: 'conflict',
      detail: 'read it again',
    });
    expect(await client.request('/v1/things', { method: 'POST', json: {} }).catch((e: unknown) => e)).toMatchObject({
      kind: 'too-large',
    });
    expect(await client.request('/v1/things', { method: 'DELETE' }).catch((e: unknown) => e)).toMatchObject({
      kind: 'business',
      code: 'hr.work_center_in_use',
    });
  });
});

describe('buildUrl y parseRetryAfter', () => {
  it('une base, ruta y parámetros (arrays con coma, objetos aplanados, vacíos fuera)', () => {
    expect(buildUrl('/api/', '/v1/x')).toBe('/api/v1/x');
    expect(buildUrl('/api', 'v1/x')).toBe('/api/v1/x');
    expect(buildUrl('/api', 'https://otro.test/y?z=1')).toBe('https://otro.test/y?z=1');
    expect(
      buildUrl('/api', '/x', {
        a: 1,
        b: true,
        c: '',
        d: undefined,
        e: null,
        f: ['x', '', 'y'],
        g: [],
        h: { k: 'v', n: { m: 2 } },
        i: new Date('2026-11-27T00:00:00.000Z'),
      }),
    ).toBe('/api/x?a=1&b=true&f=x%2Cy&h.k=v&h.n.m=2&i=2026-11-27T00%3A00%3A00.000Z');
    expect(buildUrl('/api', '/x?q=1', { a: 2 })).toBe('/api/x?q=1&a=2');
  });

  it('Retry-After en segundos, en fecha HTTP o ilegible', () => {
    expect(parseRetryAfter('5')).toBe(5000);
    expect(parseRetryAfter('0')).toBe(0);
    expect(parseRetryAfter('1.5')).toBe(1500);
    const now = Date.parse('2026-11-27T10:00:00Z');
    expect(parseRetryAfter('Fri, 27 Nov 2026 10:00:10 GMT', now)).toBe(10_000);
    expect(parseRetryAfter('Fri, 27 Nov 2026 09:00:00 GMT', now)).toBe(0);
    expect(parseRetryAfter('pronto')).toBeUndefined();
    expect(parseRetryAfter(null)).toBeUndefined();
    expect(parseRetryAfter('-3')).toBeUndefined();
  });
});

describe('fetch inyectado', () => {
  it('usa el `fetch` de las opciones', async () => {
    const spy = vi.fn(async () => new Response(JSON.stringify({ via: 'spy' }), { headers: { 'content-type': 'application/json' } }));
    const { client } = makeClient({ fetch: spy as unknown as typeof fetch });
    expect(await client.request('/v1/lo-que-sea')).toEqual({ via: 'spy' });
    expect(spy).toHaveBeenCalledTimes(1);
  });
});
