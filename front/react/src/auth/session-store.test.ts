// Almacén de sesión contra MSW: login de Go sin `success`, mustChangePassword, contexto, permisos
// con comodines, un único refresco, renovación programada, ámbito y 403.
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { API, createGoMock } from '../api/__fixtures__/go-server';
import { ApiError } from '../api';
import { SessionStore, parseExpiry } from './session-store';

const mock = createGoMock();
const { server, state } = mock;

beforeAll(() => server.listen({ onUnhandledFrame: 'error' }));
afterAll(() => server.close());
afterEach(() => {
  server.resetHandlers();
  vi.useRealTimers();
});
beforeEach(() => {
  mock.calls.length = 0;
  state.validAccess = 'access-1';
  state.validRefresh = 'refresh-1';
  state.refreshCount = 0;
  state.contextCalls = 0;
  state.mustChangePassword = false;
  state.permissions = ['*.*.*'];
});

function memoryStorage(initial: Record<string, string> = {}): Storage {
  const map = new Map(Object.entries(initial));
  return {
    getItem: (k) => map.get(k) ?? null,
    setItem: (k, v) => void map.set(k, String(v)),
    removeItem: (k) => void map.delete(k),
    clear: () => map.clear(),
    key: (i) => [...map.keys()][i] ?? null,
    get length() {
      return map.size;
    },
  };
}

const store = (options: ConstructorParameters<typeof SessionStore>[0] = {}) =>
  new SessionStore({ baseUrl: API, storage: memoryStorage(), ...options });

describe('login de Go', () => {
  it('guarda la sesión sin depender de `success`, carga el contexto y `can` entiende los comodines', async () => {
    const s = store();
    expect(s.getSnapshot().session.status).toBe('anonymous');
    const status = await s.login('admin', 'Correct-pass-1');
    expect(status).toBe('authenticated');
    const snapshot = s.getSnapshot();
    expect(snapshot.session.status).toBe('authenticated');
    expect(snapshot.username).toBe('admin');
    expect(snapshot.userId).toBe('u1');
    expect(snapshot.session.context).toMatchObject({
      subjectName: 'admin',
      isGlobalAdmin: true,
      permissions: ['*.*.*'],
      effectiveOrganizationIds: ['org-1', 'org-2'],
    });
    expect(snapshot.session.can('Facilities.Facility.Read')).toBe(true);
    expect(snapshot.session.can('Cualquier.Cosa.Mas')).toBe(true);
    expect(snapshot.contextError).toBe(false);
    expect(snapshot.loadingContext).toBe(false);
    const login = mock.callsTo('POST', '/api/auth/login')[0]!;
    expect(login.body).toEqual({ username: 'admin', password: 'Correct-pass-1' });
    expect(login.headers.authorization).toBeUndefined();
    expect(mock.callsTo('GET', '/api/auth/context')[0]!.headers.authorization).toBe('Bearer access-1');
    expect(state.contextCalls).toBe(1);
  });

  it('con permisos limitados `can` sólo concede lo suyo', async () => {
    state.permissions = ['Facilities.Facility.Read', 'Geography.*.*'];
    const s = store();
    await s.login('admin', 'Correct-pass-1');
    const { session } = s.getSnapshot();
    expect(session.can('Facilities.Facility.Read')).toBe(true);
    expect(session.can('Facilities.Facility.Update')).toBe(false);
    expect(session.can('Geography.Holiday.Update')).toBe(true);
    expect(session.can('Products.Product.Read')).toBe(false);
  });

  it('mustChangePassword deja la sesión en `must-change-password` y change-password pasa a `authenticated` con contexto', async () => {
    state.mustChangePassword = true;
    const s = store();
    expect(await s.login('admin', 'Correct-pass-1')).toBe('must-change-password');
    let snapshot = s.getSnapshot();
    expect(snapshot.session.status).toBe('must-change-password');
    // el contexto se carga igualmente (funciona aunque haya que cambiar la clave), pero no concede permisos
    expect(snapshot.session.context?.permissions).toEqual(['*.*.*']);
    expect(snapshot.session.can('Facilities.Facility.Read')).toBe(false);

    expect(await s.changePassword('Old-pass-1', 'Brand-new-pass-1')).toBe('authenticated');
    snapshot = s.getSnapshot();
    expect(snapshot.session.status).toBe('authenticated');
    expect(snapshot.session.context).toBeDefined();
    expect(snapshot.session.can('Facilities.Facility.Read')).toBe(true);
    const change = mock.callsTo('POST', '/api/auth/change-password')[0]!;
    expect(change.body).toEqual({ currentPassword: 'Old-pass-1', newPassword: 'Brand-new-pass-1' });
    expect(change.headers.authorization).toBe('Bearer access-1');
    // la sesión nueva se usa desde ya
    expect(mock.callsTo('GET', '/api/auth/context').at(-1)!.headers.authorization).toBe('Bearer access-changed');
    expect(state.contextCalls).toBe(2);
  });

  it('credenciales incorrectas: error 401 y sin sesión; errores de campo del cambio de clave llegan tipados', async () => {
    const s = store();
    const error = await s.login('admin', 'mala').catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ kind: 'unauthorized' });
    expect(s.getSnapshot().session.status).toBe('anonymous');
    expect(mock.callsTo('POST', '/api/auth/refresh')).toHaveLength(0); // un 401 de login no refresca

    await s.login('admin', 'Correct-pass-1');
    const bad = await s.changePassword('mal', 'otra-larga-1').catch((e: unknown) => e);
    expect(bad).toMatchObject({ kind: 'validation', fieldErrors: { currentPassword: [{ code: 'invalid' }] } });
    const short = await s.changePassword('Old-pass-1', 'corta').catch((e: unknown) => e);
    expect(short).toMatchObject({ fieldErrors: { newPassword: [{ code: 'minLength' }] } });
    expect(s.getSnapshot().session.status).toBe('authenticated');
  });

  it('una respuesta de sesión sin tokens es un error claro', async () => {
    server.use(http.post(`${API}/auth/login`, () => HttpResponse.json({ success: true })));
    await expect(store().login('a', 'b')).rejects.toThrow(/faltan/);
    server.use(http.post(`${API}/auth/login`, () => HttpResponse.json(['x'])));
    await expect(store().login('a', 'b')).rejects.toThrow(/objeto JSON/);
  });

  it('si el contexto falla, falla en cerrado: sin permisos y con aviso', async () => {
    server.use(http.get(`${API}/auth/context`, () => HttpResponse.json({ title: 'x' }, { status: 500 })));
    const s = store();
    await s.login('admin', 'Correct-pass-1');
    expect(s.getSnapshot().contextError).toBe(true);
    expect(s.getSnapshot().session.context).toBeUndefined();
    expect(s.getSnapshot().session.can('Facilities.Facility.Read')).toBe(false);
    server.use(http.get(`${API}/auth/context`, () => HttpResponse.json({ outcome: 'deny' })));
    await s.loadContext();
    expect(s.getSnapshot().contextError).toBe(true);
    server.use(http.get(`${API}/auth/context`, () => HttpResponse.json({ subjectId: 's', permissions: ['A.B.C', 7] })));
    await s.loadContext();
    expect(s.getSnapshot().contextError).toBe(false);
    expect(s.getSnapshot().session.context).toMatchObject({ subjectId: 's', permissions: ['A.B.C'], isGlobalAdmin: false, effectiveOrganizationIds: [] });
    server.use(http.get(`${API}/auth/context`, () => HttpResponse.json('texto')));
    await s.loadContext();
    expect(s.getSnapshot().contextError).toBe(true);
  });
});

describe('refresco', () => {
  it('con tres peticiones concurrentes que reciben 401 se hace exactamente un POST /auth/refresh y las tres se reintentan una vez', async () => {
    const s = store();
    await s.login('admin', 'Correct-pass-1');
    mock.calls.length = 0;
    // el servidor rota el token: el que tiene el cliente ya no vale
    state.validAccess = 'rotado';
    state.validRefresh = 'refresh-1';
    server.use(
      http.get(`${API}/v1/facilities/:id`, ({ request, params }) =>
        request.headers.get('authorization') === `Bearer ${state.validAccess}`
          ? HttpResponse.json({ id: params.id })
          : HttpResponse.json({ title: 'Unauthorized', status: 401 }, { status: 401 }),
      ),
      http.post(`${API}/auth/refresh`, async ({ request }) => {
        mock.calls.push({ method: 'POST', path: '/api/auth/refresh', search: '', headers: {}, body: await request.json() });
        state.refreshCount += 1;
        state.validAccess = 'rotado-2';
        state.validRefresh = 'refresh-2';
        return HttpResponse.json({
          accessToken: 'rotado-2',
          refreshToken: 'refresh-2',
          expiresAt: new Date(Date.now() + 900_000).toISOString(),
          userId: 'u1',
          username: 'admin',
          mustChangePassword: false,
        });
      }),
    );
    const results = await Promise.all([
      s.client.request<{ id: string }>('/v1/facilities/a'),
      s.client.request<{ id: string }>('/v1/facilities/b'),
      s.client.request<{ id: string }>('/v1/facilities/c'),
    ]);
    expect(results.map((r) => r.id)).toEqual(['a', 'b', 'c']);
    expect(mock.callsTo('POST', '/api/auth/refresh')).toHaveLength(1);
    expect(mock.callsTo('POST', '/api/auth/refresh')[0]!.body).toEqual({ refreshToken: 'refresh-1' });
    expect(state.refreshCount).toBe(1);
    expect(s.getSnapshot().session.status).toBe('authenticated');
    // tras el refresco se recarga el contexto
    await vi.waitFor(() => expect(mock.callsTo('GET', '/api/auth/context').length).toBeGreaterThanOrEqual(1));
  });

  it('si el servidor rechaza el refresco se cierra la sesión (y se avisa de que no la cerró el usuario)', async () => {
    const onSignedOut = vi.fn();
    const s = store({ onSignedOut });
    await s.login('admin', 'Correct-pass-1');
    state.validAccess = 'rotado';
    state.validRefresh = 'otro'; // el refresco del cliente ya no casa
    const error = await s.client.request('/v1/facilities/f1').catch((e: unknown) => e);
    expect(error).toMatchObject({ kind: 'unauthorized' });
    expect(s.getSnapshot().session.status).toBe('anonymous');
    expect(onSignedOut).toHaveBeenCalledWith(true);
  });

  it('un fallo de red en un refresco programado no cierra la sesión', async () => {
    const s = store();
    await s.login('admin', 'Correct-pass-1');
    server.use(http.post(`${API}/auth/refresh`, () => HttpResponse.error()));
    expect(await s.refreshTokens()).toBeUndefined();
    expect(s.getSnapshot().session.status).toBe('authenticated');
    server.use(http.post(`${API}/auth/refresh`, () => HttpResponse.json({ title: 'x' }, { status: 503 })));
    expect(await s.refreshTokens()).toBeUndefined();
    expect(s.getSnapshot().session.status).toBe('authenticated');
    // una respuesta ilegible sí invalida
    server.use(http.post(`${API}/auth/refresh`, () => HttpResponse.json({ ok: 1 })));
    expect(await s.refreshTokens()).toBeUndefined();
    expect(s.getSnapshot().session.status).toBe('anonymous');
    expect(await s.refreshTokens()).toBeUndefined(); // sin sesión no hay nada que renovar
  });

  it('llamadas simultáneas a refreshTokens comparten el mismo refresco', async () => {
    const s = store();
    await s.login('admin', 'Correct-pass-1');
    const [a, b] = await Promise.all([s.refreshTokens(), s.refreshTokens()]);
    expect(a).toBe('access-2');
    expect(b).toBe('access-2');
    expect(mock.callsTo('POST', '/api/auth/refresh')).toHaveLength(1);
  });
});

describe('renovación antes de expiresAt', () => {
  function fakeServer() {
    const log: string[] = [];
    let n = 0;
    const fetchImpl = (async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = new URL(String(input)).pathname;
      log.push(`${init?.method ?? 'GET'} ${path}`);
      const json = (body: unknown) => new Response(JSON.stringify(body), { headers: { 'content-type': 'application/json' } });
      if (path.endsWith('/auth/login') || path.endsWith('/auth/refresh')) {
        n += 1;
        return json({
          accessToken: `a${n}`,
          refreshToken: `r${n}`,
          expiresAt: new Date(Date.now() + 900_000).toISOString(),
          userId: 'u',
          username: 'admin',
          mustChangePassword: false,
        });
      }
      if (path.endsWith('/auth/context')) return json({ outcome: 'allow', context: { subjectId: 's', permissions: ['*.*.*'] } });
      return new Response(null, { status: 204 });
    }) as typeof fetch;
    return { fetchImpl, log };
  }

  it('renueva el token un poco antes de que caduque (margen del 20 %, máximo 60 s)', async () => {
    vi.useFakeTimers();
    const { fetchImpl, log } = fakeServer();
    const s = store({ fetch: fetchImpl });
    s.start();
    await s.login('admin', 'x');
    expect(log.filter((l) => l.endsWith('/auth/refresh'))).toHaveLength(0);
    // 15 min de vida - 60 s de margen = 14 min
    await vi.advanceTimersByTimeAsync(13 * 60_000);
    expect(log.filter((l) => l.endsWith('/auth/refresh'))).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(61_000);
    expect(log.filter((l) => l.endsWith('/auth/refresh'))).toHaveLength(1);
    // y programa la siguiente
    await vi.advanceTimersByTimeAsync(14 * 60_000 + 5_000);
    expect(log.filter((l) => l.endsWith('/auth/refresh'))).toHaveLength(2);
    s.dispose();
    await vi.advanceTimersByTimeAsync(60 * 60_000);
    expect(log.filter((l) => l.endsWith('/auth/refresh'))).toHaveLength(2);
  });

  it('con `refreshMarginMs` propio y reintento tras un fallo de red mientras el token valga', async () => {
    vi.useFakeTimers();
    let failRefresh = true;
    const base = fakeServer();
    const fetchImpl = (async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input).endsWith('/auth/refresh') && failRefresh) throw new TypeError('sin red');
      return base.fetchImpl(input, init);
    }) as typeof fetch;
    const s = store({ fetch: fetchImpl, refreshMarginMs: 120_000 });
    await s.login('admin', 'x');
    await vi.advanceTimersByTimeAsync(13 * 60_000 + 1000); // 15 - 2 min
    expect(s.getSnapshot().session.status).toBe('authenticated'); // el refresco falló pero el token vale
    failRefresh = false;
    await vi.advanceTimersByTimeAsync(16_000); // reintento a los 15 s
    expect(base.log.filter((l) => l.endsWith('/auth/refresh'))).toHaveLength(1);
    expect(s.getSnapshot().session.status).toBe('authenticated');
    s.dispose();
  });
});

describe('persistencia en sessionStorage', () => {
  it('guarda la sesión, la restaura al crear otro almacén y la borra al cerrar', async () => {
    const storage = memoryStorage();
    const first = store({ storage });
    await first.login('admin', 'Correct-pass-1');
    expect(JSON.parse(storage.getItem('karpo.session')!)).toMatchObject({ accessToken: 'access-1', refreshToken: 'refresh-1', username: 'admin' });

    const second = store({ storage });
    expect(second.getSnapshot().session.status).toBe('authenticated');
    expect(second.getSnapshot().username).toBe('admin');
    expect(second.getSnapshot().session.context).toBeUndefined(); // el contexto se vuelve a pedir al arrancar
    second.start();
    second.start(); // idempotente
    await vi.waitFor(() => expect(second.getSnapshot().session.context).toBeDefined());
    second.dispose();

    await second.logout();
    expect(storage.getItem('karpo.session')).toBeNull();
    expect(mock.callsTo('POST', '/api/auth/logout')[0]!.body).toEqual({ refreshToken: 'refresh-1' });
    expect(store({ storage }).getSnapshot().session.status).toBe('anonymous');
  });

  it('una sesión restaurada y ya caducada se renueva al arrancar', async () => {
    const storage = memoryStorage({
      'karpo.session': JSON.stringify({
        accessToken: 'viejo',
        refreshToken: 'refresh-1',
        expiresAt: Date.now() - 10_000,
        userId: 'u1',
        username: 'admin',
        mustChangePassword: false,
      }),
    });
    const s = store({ storage });
    s.start();
    await vi.waitFor(() => expect(s.getSnapshot().session.context).toBeDefined());
    expect(state.refreshCount).toBe(1);
    expect(JSON.parse(storage.getItem('karpo.session')!).accessToken).toBe('access-2');
    s.dispose();
  });

  it('un almacenamiento que lanza o no existe no rompe nada (la sesión vive en memoria)', async () => {
    const broken: Storage = {
      getItem: () => {
        throw new Error('bloqueado');
      },
      setItem: () => {
        throw new Error('bloqueado');
      },
      removeItem: () => {
        throw new Error('bloqueado');
      },
      clear: () => undefined,
      key: () => null,
      length: 0,
    };
    for (const storage of [broken, null]) {
      const s = store({ storage });
      expect(s.getSnapshot().session.status).toBe('anonymous');
      await s.login('admin', 'Correct-pass-1');
      expect(s.getSnapshot().session.status).toBe('authenticated');
      await s.setScope(['org-1']);
      await s.logout();
    }
    const corrupt = store({ storage: memoryStorage({ 'karpo.session': '{no es json', 'karpo.scope': '[1' }) });
    expect(corrupt.getSnapshot().session.status).toBe('anonymous');
    const partial = store({ storage: memoryStorage({ 'karpo.session': '{"accessToken":"x"}', 'karpo.scope': '{"a":1}' }) });
    expect(partial.getSnapshot().session.status).toBe('anonymous');
    expect(partial.getSnapshot().scope).toEqual([]);
  });

  it('usa `sessionStorage` por omisión si existe', async () => {
    const real = memoryStorage();
    vi.stubGlobal('sessionStorage', real);
    try {
      const s = new SessionStore({ baseUrl: API });
      await s.login('admin', 'Correct-pass-1');
      expect(real.getItem('karpo.session')).not.toBeNull();
    } finally {
      vi.unstubAllGlobals();
    }
  });
});

describe('ámbito de organizaciones', () => {
  it('viaja en X-Organization-Scope, se guarda, recarga el contexto y avisa', async () => {
    const onScopeChange = vi.fn();
    const storage = memoryStorage();
    const s = store({ storage, onScopeChange });
    await s.login('admin', 'Correct-pass-1');
    expect(state.contextCalls).toBe(1);
    await s.setScope(['org-2', 'org-1', 'org-2', '']);
    expect(s.getSnapshot().scope).toEqual(['org-2', 'org-1']);
    expect(state.contextCalls).toBe(2);
    expect(onScopeChange).toHaveBeenCalledTimes(1);
    expect(mock.callsTo('GET', '/api/auth/context').at(-1)!.headers['x-organization-scope']).toBe('org-2,org-1');
    expect(JSON.parse(storage.getItem('karpo.scope')!)).toEqual(['org-2', 'org-1']);
    await s.client.request('/v1/facilities/f1');
    expect(mock.calls.at(-1)!.headers['x-organization-scope']).toBe('org-2,org-1');
    // lo restaura otro almacén
    expect(store({ storage }).getSnapshot().scope).toEqual(['org-2', 'org-1']);
    await s.setScope([]);
    expect(storage.getItem('karpo.scope')).toBeNull();
    await s.client.request('/v1/facilities/f1');
    expect(mock.calls.at(-1)!.headers['x-organization-scope']).toBeUndefined();
  });

  it('un cambio de ámbito durante una carga de contexto espera y vuelve a cargar', async () => {
    const s = store();
    await s.login('admin', 'Correct-pass-1');
    const inFlight = s.loadContext();
    await s.setScope(['org-1']);
    await inFlight;
    expect(mock.callsTo('GET', '/api/auth/context').at(-1)!.headers['x-organization-scope']).toBe('org-1');
  });
});

describe('403', () => {
  it('recarga el contexto una vez y no más de una vez cada 10 s', async () => {
    let now = 1_000_000;
    const s = store({ now: () => now });
    await s.login('admin', 'Correct-pass-1');
    server.use(http.get(`${API}/v1/secret`, () => HttpResponse.json({ title: 'Forbidden', status: 403 }, { status: 403 })));
    const before = state.contextCalls;
    await s.client.request('/v1/secret').catch(() => undefined);
    await vi.waitFor(() => expect(state.contextCalls).toBe(before + 1));
    await s.client.request('/v1/secret').catch(() => undefined);
    await s.client.request('/v1/secret').catch(() => undefined);
    expect(state.contextCalls).toBe(before + 1);
    now += 11_000;
    await s.client.request('/v1/secret').catch(() => undefined);
    await vi.waitFor(() => expect(state.contextCalls).toBe(before + 2));
  });

  it('con la clave pendiente de cambiar (todo es 403) no recarga el contexto', async () => {
    state.mustChangePassword = true;
    const s = store();
    await s.login('admin', 'Correct-pass-1');
    server.use(http.get(`${API}/v1/secret`, () => HttpResponse.json({ title: 'Forbidden' }, { status: 403 })));
    const before = state.contextCalls;
    await s.client.request('/v1/secret').catch(() => undefined);
    expect(state.contextCalls).toBe(before);
  });
});

describe('suscripción y utilidades', () => {
  it('getSnapshot es estable mientras nada cambie y avisa a los suscriptores', async () => {
    const s = store();
    const listener = vi.fn();
    const unsubscribe = s.subscribe(listener);
    const first = s.getSnapshot();
    expect(s.getSnapshot()).toBe(first);
    await s.login('admin', 'Correct-pass-1');
    expect(listener).toHaveBeenCalled();
    expect(s.getSnapshot()).not.toBe(first);
    const count = listener.mock.calls.length;
    unsubscribe();
    await s.logout();
    expect(listener.mock.calls.length).toBe(count);
  });

  it('parseExpiry entiende RFC 3339, segundos, milisegundos y cae a 15 minutos', () => {
    expect(parseExpiry('2026-11-27T10:00:00Z', 0)).toBe(Date.parse('2026-11-27T10:00:00Z'));
    expect(parseExpiry(1_800_000_000, 0)).toBe(1_800_000_000_000);
    expect(parseExpiry(1_800_000_000_000, 0)).toBe(1_800_000_000_000);
    expect(parseExpiry('mañana', 1000)).toBe(1000 + 15 * 60_000);
    expect(parseExpiry(undefined, 1000)).toBe(1000 + 15 * 60_000);
  });
});
