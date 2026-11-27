// Servidor MSW de pruebas con la parte del contrato de Go que usa T1 (A2 §1, §2): autenticación con
// refresco, `GET /api/auth/context` y el contrato de mantenimiento de instalaciones. Sólo pruebas.
import { http, HttpResponse, type HttpHandler } from 'msw';
import { setupServer } from 'msw/node';

export const ORIGIN = 'http://localhost';
export const API = `${ORIGIN}/api`;

export interface RecordedRequest {
  method: string;
  path: string;
  search: string;
  headers: Record<string, string>;
  body?: unknown;
}

export interface FacilityRow {
  id: string;
  name: string;
  type: string;
  partOf: string;
  active: boolean;
  version: number;
}

export interface MockState {
  /** Token de acceso que el servidor acepta ahora. */
  validAccess: string;
  validRefresh: string;
  refreshCount: number;
  mustChangePassword: boolean;
  permissions: string[];
  contextCalls: number;
  facilities: FacilityRow[];
}

function problem(status: number, body: Record<string, unknown>): Response {
  return HttpResponse.json({ status, ...body }, { status, headers: { 'Content-Type': 'application/problem+json' } });
}

export function createGoMock() {
  const calls: RecordedRequest[] = [];
  const state: MockState = {
    validAccess: 'access-1',
    validRefresh: 'refresh-1',
    refreshCount: 0,
    mustChangePassword: false,
    permissions: ['*.*.*'],
    contextCalls: 0,
    facilities: [
      { id: 'f1', name: 'Sede', type: 't1', partOf: '', active: true, version: 1 },
      { id: 'f2', name: 'Planta 1', type: 't2', partOf: 'f1', active: true, version: 3 },
    ],
  };

  const session = (override: Partial<{ mustChangePassword: boolean }> = {}) => ({
    accessToken: state.validAccess,
    refreshToken: state.validRefresh,
    expiresAt: new Date(Date.now() + 15 * 60_000).toISOString(),
    userId: 'u1',
    username: 'admin',
    mustChangePassword: override.mustChangePassword ?? state.mustChangePassword,
  });

  async function record(request: Request): Promise<RecordedRequest> {
    const url = new URL(request.url);
    const headers: Record<string, string> = {};
    request.headers.forEach((value, key) => {
      headers[key] = value;
    });
    const text = request.method === 'GET' || request.method === 'HEAD' ? '' : await request.clone().text();
    let body: unknown;
    if (text) {
      try {
        body = JSON.parse(text);
      } catch {
        body = text;
      }
    }
    const entry = { method: request.method, path: url.pathname, search: url.search, headers, body };
    calls.push(entry);
    return entry;
  }

  const authorized = (rec: RecordedRequest) => rec.headers.authorization === `Bearer ${state.validAccess}`;
  const unauthorized = () =>
    HttpResponse.json({ title: 'Unauthorized', status: 401 }, { status: 401, headers: { 'WWW-Authenticate': 'Bearer' } });

  /** Rutas del servidor; las pruebas añaden las suyas con `server.use`. */
  const handlers: HttpHandler[] = [
    http.post(`${API}/auth/login`, async ({ request }) => {
      const rec = await record(request);
      const body = rec.body as { username?: string; password?: string };
      if (!body.username || !body.password) {
        return problem(400, {
          title: 'Validation Failed',
          errors: [
            { field: body.username ? 'password' : 'username', code: 'required', message: 'is required' },
          ],
        });
      }
      if (body.username !== 'admin' || body.password !== 'Correct-pass-1') {
        return problem(401, { title: 'Unauthorized' });
      }
      return HttpResponse.json(session());
    }),

    http.post(`${API}/auth/refresh`, async ({ request }) => {
      const rec = await record(request);
      if ((rec.body as { refreshToken?: string }).refreshToken !== state.validRefresh) {
        return problem(401, { title: 'Unauthorized' });
      }
      state.refreshCount += 1;
      state.validAccess = `access-${state.refreshCount + 1}`;
      state.validRefresh = `refresh-${state.refreshCount + 1}`;
      return HttpResponse.json(session());
    }),

    http.post(`${API}/auth/logout`, async ({ request }) => {
      await record(request);
      return new HttpResponse(null, { status: 204 });
    }),

    http.post(`${API}/auth/change-password`, async ({ request }) => {
      const rec = await record(request);
      if (!authorized(rec)) return unauthorized();
      const body = rec.body as { currentPassword?: string; newPassword?: string };
      if (body.currentPassword !== 'Old-pass-1') {
        return problem(400, {
          title: 'Validation Failed',
          errors: { currentPassword: ['the current password is not correct'] },
          errorCodes: { currentPassword: ['invalid'] },
        });
      }
      if ((body.newPassword ?? '').length < 8) {
        return problem(400, {
          title: 'Validation Failed',
          errors: { newPassword: ['password too short'] },
          errorCodes: { newPassword: ['minLength'] },
        });
      }
      state.mustChangePassword = false;
      state.validAccess = 'access-changed';
      state.validRefresh = 'refresh-changed';
      return HttpResponse.json(session({ mustChangePassword: false }));
    }),

    http.get(`${API}/auth/context`, async ({ request }) => {
      const rec = await record(request);
      if (!authorized(rec)) return unauthorized();
      state.contextCalls += 1;
      return HttpResponse.json({
        outcome: 'allow',
        context: {
          subjectId: 'u1',
          subjectName: 'admin',
          actorKind: 'human',
          isGlobalAdmin: state.permissions.includes('*.*.*'),
          grants: [],
          permissions: state.permissions,
          effectiveOrganizationIds: ['org-1', 'org-2'],
          policyVersion: 'p1',
        },
      });
    }),

    http.get(`${API}/internal-organizations/names`, async ({ request }) => {
      const rec = await record(request);
      if (!authorized(rec)) return unauthorized();
      return HttpResponse.json([
        { id: 'org-2', name: 'Zeta SL' },
        { id: 'org-1', name: 'Acme SA' },
        { id: 'org-3', name: 'Otra SA' },
      ]);
    }),

    http.post(`${API}/v1/facilities/search`, async ({ request }) => {
      const rec = await record(request);
      if (!authorized(rec)) return unauthorized();
      const body = rec.body as { pagination: { pageNumber: number; pageSize: number } };
      const { pageNumber, pageSize } = body.pagination;
      const start = (pageNumber - 1) * pageSize;
      return HttpResponse.json({
        items: state.facilities.slice(start, start + pageSize),
        total: state.facilities.length,
        totalCount: state.facilities.length,
        pageNumber,
        pageSize,
        totalPages: Math.ceil(state.facilities.length / pageSize),
      });
    }),

    http.get(`${API}/v1/facilities/:id`, async ({ request, params }) => {
      const rec = await record(request);
      if (!authorized(rec)) return unauthorized();
      const row = state.facilities.find((f) => f.id === params.id);
      return row ? HttpResponse.json(row) : problem(404, { title: 'Not Found' });
    }),

    http.put(`${API}/v1/facilities/:id`, async ({ request, params }) => {
      const rec = await record(request);
      if (!authorized(rec)) return unauthorized();
      const row = state.facilities.find((f) => f.id === params.id);
      if (!row) return problem(404, { title: 'Not Found' });
      const body = rec.body as Partial<FacilityRow>;
      if (typeof body.version !== 'number') {
        return problem(400, {
          title: 'Validation Failed',
          errors: { version: ['the version that was read is required'] },
          errorCodes: { version: ['required'] },
        });
      }
      if (body.version !== row.version) {
        return problem(409, { title: 'Conflict', detail: 'the facility changed after it was read: read it again' });
      }
      Object.assign(row, body, { version: row.version + 1 });
      return HttpResponse.json(row);
    }),
  ];

  const server = setupServer(...handlers);
  return {
    server,
    calls,
    state,
    callsTo: (method: string, path: string) => calls.filter((c) => c.method === method && c.path === path),
  };
}

export type GoMock = ReturnType<typeof createGoMock>;
