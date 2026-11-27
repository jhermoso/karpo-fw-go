// Adaptador del contrato de mantenimiento con MSW (cuerpos exactos, versión obligatoria, 202, 409,
// estilo number-size) y las rutas auxiliares (catálogos PascalCase, árbol de puestos, geografía).
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import type { CommandDef, EntityConfig, Row } from '../contracts';
import { normalizeEntityConfig } from '../config';
import { readRealById } from '../config/__fixtures__/real-configs';
import { API, createGoMock } from './__fixtures__/go-server';
import { createEntityApi, expandTemplate, resolveServerSort } from './entity-api';
import { createHttpClient } from './http';
import { ApiError } from './errors';

const mock = createGoMock();
const { server, state } = mock;

beforeAll(() => server.listen({ onUnhandledFrame: 'error' }));
afterAll(() => server.close());
afterEach(() => server.resetHandlers());
beforeEach(() => {
  mock.calls.length = 0;
  state.validAccess = 'access-1';
  state.facilities = [
    { id: 'f1', name: 'Sede', type: 't1', partOf: '', active: true, version: 1 },
    { id: 'f2', name: 'Planta 1', type: 't2', partOf: 'f1', active: true, version: 3 },
  ];
});

function makeApi(apiVersion = 'v1') {
  const client = createHttpClient({
    baseUrl: API,
    getToken: () => 'access-1',
    refresh: async () => undefined,
    getScope: () => undefined,
    onUnauthorized: () => undefined,
    sleep: async () => undefined,
  });
  return createEntityApi(client, { api: { apiVersion } });
}

const facility = (): EntityConfig => normalizeEntityConfig(readRealById('facility'));
const supplier = (): EntityConfig => normalizeEntityConfig(readRealById('supplier'));
const row = (extra: Record<string, unknown> = {}): Row => ({ id: 'f1', version: 4, ...extra }) as Row;

describe('lista', () => {
  it('POST {ep}/search con el cuerpo exacto {filter, pagination:{pageNumber,pageSize,orderBy,ascending}}', async () => {
    const api = makeApi();
    const page = await api.list(facility(), {
      page: 1,
      pageSize: 20,
      sort: { field: 'name', direction: 'asc' },
      filter: { search: 'sed', organization: 'org-1', empty: '', none: undefined as unknown as string, active: false },
    });
    const call = mock.callsTo('POST', '/api/v1/facilities/search')[0]!;
    expect(call.body).toEqual({
      filter: { search: 'sed', organization: 'org-1', active: false },
      pagination: { pageNumber: 1, pageSize: 20, orderBy: 'name', ascending: true },
    });
    expect(Object.keys(call.body as object)).toEqual(['filter', 'pagination']);
    expect(Object.keys((call.body as { pagination: object }).pagination)).toEqual([
      'pageNumber',
      'pageSize',
      'orderBy',
      'ascending',
    ]);
    expect(page).toMatchObject({ total: 2, page: 1, pageSize: 20 });
    expect(page.items.map((r) => r.id)).toEqual(['f1', 'f2']);
  });

  it('pageSize se acota a 1000 (y a 1 como mínimo) y la página a 1', async () => {
    const api = makeApi();
    await api.list(facility(), { page: 0, pageSize: 5000, filter: {} });
    await api.list(facility(), { page: 3, pageSize: 0, filter: {} });
    const [a, b] = mock.callsTo('POST', '/api/v1/facilities/search').map((c) => (c.body as { pagination: { pageNumber: number; pageSize: number } }).pagination);
    expect(a).toEqual({ pageNumber: 1, pageSize: 1000 });
    expect(b).toEqual({ pageNumber: 3, pageSize: 1 });
  });

  it('sólo manda el orden si el servidor lo admite (lista blanca o `serverSortField`)', async () => {
    const api = makeApi();
    await api.list(facility(), { page: 1, pageSize: 20, sort: { field: 'typeName', direction: 'desc' }, filter: {} });
    await api.list(facility(), { page: 1, pageSize: 20, sort: { field: 'name', direction: 'desc' }, filter: {} });
    const [none, desc] = mock.callsTo('POST', '/api/v1/facilities/search').map((c) => c.body as { pagination: Record<string, unknown> });
    expect(none!.pagination).toEqual({ pageNumber: 1, pageSize: 20 });
    expect(desc!.pagination).toMatchObject({ orderBy: 'name', ascending: false });
    // un campo de servidor declarado en la columna se acepta por su nombre de columna o de servidor
    const custom = normalizeEntityConfig(
      { id: 'x', apiEndpoint: '/otras', grid: { columns: [{ field: 'b', sortable: true }] } },
      { serverSortFields: { b: 'bee' } },
    );
    expect(resolveServerSort(custom, 'b')).toBe('bee');
    expect(resolveServerSort(custom, 'bee')).toBe('bee');
    expect(resolveServerSort(custom, 'zzz')).toBeUndefined();
    const sorted = normalizeEntityConfig({ id: 'y', apiEndpoint: '/api/zz', grid: { columns: [{ field: 'name', sortable: true }] } });
    expect(resolveServerSort(sorted, 'name')).toBeUndefined(); // sin versión: sin lista blanca
  });

  it('fusiona el `baseFilter` de la configuración bajo el filtro de la consulta', async () => {
    const api = makeApi();
    const cfg = normalizeEntityConfig(readRealById('facility'), { dataSource: { baseFilter: { type: 'Building', org: 'a' } } });
    await api.list(cfg, { page: 1, pageSize: 10, filter: { org: 'b' } });
    expect((mock.callsTo('POST', '/api/v1/facilities/search')[0]!.body as { filter: object }).filter).toEqual({ type: 'Building', org: 'b' });
  });

  it('GET con `listMethod: get` usa page, pageSize, sortBy y sortOrder', async () => {
    server.use(
      http.get(`${API}/v1/facilities`, ({ request }) => {
        mock.calls.push({ method: 'GET', path: '/api/v1/facilities', search: new URL(request.url).search, headers: {} });
        return HttpResponse.json({ data: [{ id: 'a' }], totalCount: 7, pageNumber: 2, pageSize: 1 });
      }),
    );
    const cfg = normalizeEntityConfig(readRealById('facility'), { dataSource: { listMethod: 'get' } });
    const page = await makeApi().list(cfg, { page: 2, pageSize: 1, sort: { field: 'name', direction: 'desc' }, filter: { search: 'x', active: true } });
    expect(mock.calls.at(-1)!.search).toBe('?page=2&pageSize=1&sortBy=name&sortOrder=desc&search=x&active=true');
    expect(page).toEqual({ items: [{ id: 'a' }], total: 7, page: 2, pageSize: 1 });
  });

  it('estilo number-size: GET con `size` y respuesta {items,total,number,size}', async () => {
    server.use(
      http.get(`${API}/hr/positions`, ({ request }) => {
        mock.calls.push({ method: 'GET', path: '/api/hr/positions', search: new URL(request.url).search, headers: {} });
        return HttpResponse.json({ items: [{ ID: 1, name: 'p' }], total: 41, number: 3, size: 10, totalPages: 5 });
      }),
    );
    const cfg = normalizeEntityConfig({ id: 'position', apiEndpoint: '/api/hr/positions' });
    expect(cfg.dataSource.listStyle).toBe('number-size');
    const page = await makeApi().list(cfg, { page: 3, pageSize: 10, filter: { unit: 'u1', vacant: true }, sort: { field: 'name', direction: 'asc' } });
    expect(mock.calls.at(-1)!.search).toBe('?page=3&size=10&unit=u1&vacant=true');
    expect(mock.calls.at(-1)!.search).not.toContain('pageSize');
    expect(page).toMatchObject({ total: 41, page: 3, pageSize: 10 });
    expect(page.items[0]).toEqual({ ID: 1, name: 'p', id: '1' });
  });

  it('normaliza items|data|results|records y total|totalCount|totalItems|totalElements|count', async () => {
    const api = makeApi();
    const cfg = facility();
    const respond = (body: unknown) =>
      server.use(http.post(`${API}/v1/facilities/search`, () => HttpResponse.json(body as never)));
    respond({ results: [{ id: 'r' }], totalItems: 9 });
    expect(await api.list(cfg, { page: 1, pageSize: 5, filter: {} })).toMatchObject({ total: 9, items: [{ id: 'r' }] });
    respond({ records: [{ id: 'r' }], totalElements: 11 });
    expect((await api.list(cfg, { page: 1, pageSize: 5, filter: {} })).total).toBe(11);
    respond({ items: [{ id: 'r' }], count: 12 });
    expect((await api.list(cfg, { page: 1, pageSize: 5, filter: {} })).total).toBe(12);
    respond([{ id: 'a' }, { id: 'b' }]);
    expect(await api.list(cfg, { page: 2, pageSize: 5, filter: {} })).toEqual({ items: [{ id: 'a' }, { id: 'b' }], total: 2, page: 2, pageSize: 5 });
    respond({ nada: true });
    expect((await api.list(cfg, { page: 1, pageSize: 5, filter: {} })).items).toEqual([]);
    respond({ items: [{ code: 77 }, 'basura', null] });
    const odd = await api.list(cfg, { page: 1, pageSize: 5, filter: {} });
    expect(odd.items.map((r) => r.id)).toEqual(['', '', '']);
    const withKey = createEntityApi(
      createHttpClient({ baseUrl: API, getToken: () => 'access-1', refresh: async () => undefined, getScope: () => undefined, onUnauthorized: () => undefined }),
    );
    server.use(http.post(`${API}/v1/x/search`, () => HttpResponse.json({ items: [{ code: 77 }] })));
    const byCode = await withKey.list(normalizeEntityConfig({ id: 'c', apiEndpoint: '/x', primaryKey: 'code' }), { page: 1, pageSize: 5, filter: {} });
    expect(byCode.items[0]!.id).toBe('77');
  });

  it('listAll sigue las páginas hasta el total', async () => {
    const api = makeApi();
    const many = Array.from({ length: 2500 }, (_v, i) => ({ id: `r${i}`, name: `n${i}` }));
    server.use(
      http.post(`${API}/v1/facilities/search`, async ({ request }) => {
        const { pagination } = (await request.json()) as { pagination: { pageNumber: number; pageSize: number } };
        mock.calls.push({ method: 'POST', path: '/api/v1/facilities/search', search: '', headers: {}, body: pagination });
        const start = (pagination.pageNumber - 1) * pagination.pageSize;
        return HttpResponse.json({ items: many.slice(start, start + pagination.pageSize), total: many.length });
      }),
    );
    const rows = await api.listAll(facility(), { filter: { search: 'x' } });
    expect(rows).toHaveLength(2500);
    expect(mock.callsTo('POST', '/api/v1/facilities/search').map((c) => (c.body as { pageNumber: number }).pageNumber)).toEqual([1, 2, 3]);
    // se detiene si el servidor devuelve una página vacía
    server.use(http.post(`${API}/v1/facilities/search`, () => HttpResponse.json({ items: [], total: 50 })));
    expect(await api.listAll(facility())).toEqual([]);
  });
});

describe('detalle, alta, cambio y baja', () => {
  it('get: GET {ep}/{id} con el id codificado; 404 -> notfound', async () => {
    const api = makeApi();
    const found = await api.get(facility(), 'f1');
    expect(found).toMatchObject({ id: 'f1', name: 'Sede', version: 1 });
    const error = await api.get(facility(), 'no existe/x').catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ kind: 'notfound' });
    expect(mock.calls.at(-1)!.path).toBe('/api/v1/facilities/no%20existe%2Fx');
  });

  it('get usa el agregado (`aggregateEndpoint`) cuando lo hay', async () => {
    server.use(http.get(`${API}/v1/facilities/:id/aggregate`, ({ params }) => HttpResponse.json({ id: params.id, agg: true })));
    const cfg = normalizeEntityConfig(readRealById('facility'), { aggregateEndpoint: '/facilities/{id}/aggregate' });
    expect(await makeApi().get(cfg, 'f9')).toEqual({ id: 'f9', agg: true });
  });

  it('create: POST, 201 con el objeto; sin cuerpo sigue la cabecera Location', async () => {
    const api = makeApi();
    server.use(
      http.post(`${API}/v1/facilities`, async ({ request }) => {
        const body = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: 'nuevo', version: 1, ...body }, { status: 201, headers: { Location: '/api/v1/facilities/nuevo' } });
      }),
    );
    expect(await api.create(facility(), { name: 'Nueva' })).toEqual({ id: 'nuevo', version: 1, name: 'Nueva' });
    server.use(
      http.post(`${API}/v1/facilities`, () => new HttpResponse(null, { status: 201, headers: { Location: '/api/v1/facilities/f2?x=1' } })),
    );
    expect(await api.create(facility(), { name: 'Otra' })).toMatchObject({ id: 'f2', name: 'Planta 1' });
    server.use(http.post(`${API}/v1/facilities`, () => new HttpResponse(null, { status: 201 })));
    expect(await api.create(facility(), {})).toEqual({ id: '' });
  });

  it('A1 §5 punto 14: update SIEMPRE envía `version` ({...patch, version}) y la versión manda sobre el parche', async () => {
    const api = makeApi();
    const updated = await api.update(facility(), 'f1', { name: 'Sede central', version: 99 }, 1);
    const call = mock.callsTo('PUT', '/api/v1/facilities/f1')[0]!;
    expect(call.body).toEqual({ name: 'Sede central', version: 1 });
    expect(updated).toMatchObject({ name: 'Sede central', version: 2 });
  });

  it('update sin versión falla antes de enviar nada, con el error de campo `version` -> required', async () => {
    const api = makeApi();
    for (const version of [undefined, null, '3', Number.NaN] as unknown[]) {
      const error = await api.update(facility(), 'f1', { name: 'x' }, version as number).catch((e: unknown) => e);
      expect(error).toBeInstanceOf(ApiError);
      expect(error).toMatchObject({ kind: 'validation', fieldErrors: { version: [{ code: 'required' }] } });
    }
    expect(mock.calls).toHaveLength(0);
  });

  it('update con una versión caducada -> 409 conflict', async () => {
    const error = await makeApi().update(facility(), 'f2', { name: 'x' }, 1).catch((e: unknown) => e);
    expect(error).toMatchObject({ kind: 'conflict', status: 409 });
    expect((error as ApiError).detail).toContain('read it again');
  });

  it('remove: DELETE 204 -> done; con cuerpo ({reason}); 202 y `pendingApproval` -> pending', async () => {
    const api = makeApi();
    server.use(
      http.delete(`${API}/v1/facilities/f1`, async ({ request }) => {
        mock.calls.push({ method: 'DELETE', path: '/api/v1/facilities/f1', search: '', headers: {}, body: await request.text() });
        return new HttpResponse(null, { status: 204 });
      }),
      http.delete(`${API}/v1/facilities/f2`, () => HttpResponse.json({ pendingApproval: true, approvalId: 'a1' }, { status: 202 })),
      http.delete(`${API}/v1/facilities/f3`, () => HttpResponse.json({ pendingApproval: true })),
      http.delete(`${API}/v1/facilities/f4`, () => HttpResponse.json({ id: 'f4', active: false, version: 5 })),
    );
    expect(await api.remove(facility(), 'f1')).toEqual({ status: 'done' });
    expect(mock.calls.at(-1)!.body).toBe('');
    expect(await api.remove(facility(), 'f1', {})).toEqual({ status: 'done' });
    expect(await api.remove(facility(), 'f1', { reason: 'cierre' })).toEqual({ status: 'done' });
    expect(mock.calls.at(-1)!.body).toBe('{"reason":"cierre"}');
    expect(await api.remove(facility(), 'f2', { reason: 'x' })).toEqual({ status: 'pending' });
    expect(await api.remove(facility(), 'f3')).toEqual({ status: 'pending' });
    expect(await api.remove(facility(), 'f4')).toEqual({ status: 'done', row: { id: 'f4', active: false, version: 5 } });
  });

  it('413 -> too-large', async () => {
    server.use(http.post(`${API}/v1/facilities`, () => HttpResponse.json({ title: 'Request Entity Too Large', status: 413 }, { status: 413 })));
    expect(await makeApi().create(facility(), { big: 'x' }).catch((e: unknown) => e)).toMatchObject({ kind: 'too-large' });
  });
});

describe('órdenes', () => {
  const block: CommandDef = {
    id: 'block',
    label: { literal: 'Bloquear' },
    surfaces: ['row'],
    kind: 'http',
    method: 'POST',
    endpoint: '/suppliers/{id}/block',
    sendVersion: true,
    prompt: { field: 'reason', label: { literal: 'Motivo' }, required: true },
  };

  it('POST {ep}/{id}/{orden} con {version, ...cuerpo}, {id} y {campo} sustituidos y codificados', async () => {
    let seen: unknown;
    let seenPath = '';
    server.use(
      http.post(`${API}/v1/suppliers/:id/block`, async ({ request, params }) => {
        seen = await request.json();
        seenPath = String(params.id);
        return HttpResponse.json({ id: params.id, blocked: true, version: 5 });
      }),
    );
    const result = await makeApi().command(supplier(), block, row({ id: 'a b' }), { reason: 'impago' });
    expect(seen).toEqual({ reason: 'impago', version: 4 });
    expect(seenPath).toBe('a b');
    expect(mock.calls.length).toBe(0);
    expect(result).toEqual({ status: 'done', row: { id: 'a b', blocked: true, version: 5 } });
  });

  it('202 o `pendingApproval` -> pending, sin fila', async () => {
    const api = makeApi();
    server.use(http.post(`${API}/v1/suppliers/:id/block`, () => HttpResponse.json({ pendingApproval: true, id: 'x' }, { status: 202 })));
    expect(await api.command(supplier(), block, row())).toEqual({ status: 'pending' });
    server.use(http.post(`${API}/v1/suppliers/:id/block`, () => HttpResponse.json({ pendingApproval: true })));
    expect((await api.command(supplier(), block, row())).status).toBe('pending');
    server.use(http.post(`${API}/v1/suppliers/:id/block`, () => new HttpResponse(null, { status: 200 })));
    expect(await api.command(supplier(), block, row())).toEqual({ status: 'done' });
  });

  it('422 con `code` -> business (hr.reporting_cycle)', async () => {
    server.use(
      http.post(`${API}/v1/suppliers/:id/block`, () =>
        HttpResponse.json({ title: 'Unprocessable Entity', status: 422, code: 'hr.reporting_cycle', detail: 'cycle' }, { status: 422 }),
      ),
    );
    const error = await makeApi().command(supplier(), block, row()).catch((e: unknown) => e);
    expect(error).toMatchObject({ kind: 'business', code: 'hr.reporting_cycle', status: 422 });
  });

  it('sin `sendVersion` no exige versión; con `sendVersion` y sin versión falla en local', async () => {
    const api = makeApi();
    server.use(http.post(`${API}/v1/suppliers/:id/unblock`, async ({ request }) => HttpResponse.json({ id: 'f1', echoed: await request.json() })));
    const unblock: CommandDef = { ...block, id: 'unblock', endpoint: undefined, sendVersion: false };
    const result = await api.command(supplier(), unblock, { id: 'f1' } as Row, { reason: 'ok' });
    expect(result.row).toMatchObject({ echoed: { reason: 'ok' } });
    const error = await api.command(supplier(), block, { id: 'f1' } as Row).catch((e: unknown) => e);
    expect(error).toMatchObject({ kind: 'validation', fieldErrors: { version: [{ code: 'required' }] } });
  });

  it('endpoint por omisión `{ep}/{id}/{orden}`, método propio, sin versión (`~/`) y absoluta', async () => {
    const api = makeApi();
    const seen: string[] = [];
    server.use(
      http.put(`${API}/v1/suppliers/f1/archive`, () => {
        seen.push('default');
        return new HttpResponse(null, { status: 204 });
      }),
      http.post(`${API}/purchases/approvals/f1/approve`, () => {
        seen.push('tilde');
        return new HttpResponse(null, { status: 204 });
      }),
      http.post(`https://otro.test/x/f1`, () => {
        seen.push('absolute');
        return new HttpResponse(null, { status: 204 });
      }),
    );
    await api.command(supplier(), { ...block, id: 'archive', endpoint: undefined, method: 'PUT', sendVersion: false }, row());
    await api.command(supplier(), { ...block, endpoint: '~/purchases/approvals/{id}/approve', sendVersion: false }, row());
    await api.command(supplier(), { ...block, endpoint: 'https://otro.test/x/{id}', sendVersion: false }, row());
    expect(seen).toEqual(['default', 'tilde', 'absolute']);
    // entidad sin versión: la ruta tampoco la lleva
    const approvals = normalizeEntityConfig({ id: 'approval', apiEndpoint: '~/purchases/approvals' });
    server.use(http.post(`${API}/purchases/approvals/f1/reject`, () => new HttpResponse(null, { status: 204 })));
    expect(await api.command(approvals, { ...block, id: 'reject', endpoint: undefined, sendVersion: false }, row())).toEqual({ status: 'done' });
  });

  it('las órdenes `delete` van a remove y las de navegación no hacen petición', async () => {
    const api = makeApi();
    server.use(http.delete(`${API}/v1/facilities/f1`, () => new HttpResponse(null, { status: 204 })));
    const del: CommandDef = { id: 'delete', label: { literal: 'x' }, surfaces: ['row'], kind: 'delete' };
    expect(await api.command(facility(), del, row(), { reason: 'r' })).toEqual({ status: 'done' });
    await expect(api.command(facility(), { ...del, id: 'view', kind: 'view' }, row())).rejects.toThrow(/navegación/);
  });
});

describe('hijos, raw y rutas auxiliares', () => {
  it('childList sustituye {id}; `~/` va sin versión; admite lista plana y página', async () => {
    const api = makeApi();
    server.use(
      http.get(`${API}/v1/suppliers/f1/block-history`, ({ request }) => {
        mock.calls.push({ method: 'GET', path: '/api/v1/suppliers/f1/block-history', search: new URL(request.url).search, headers: {} });
        return HttpResponse.json([{ id: 'e1', reason: 'x' }, { at: 't', reason: 'sin id' }]);
      }),
      http.get(`${API}/hr/positions/f1/holders`, () => HttpResponse.json({ items: [{ id: 'h1' }] })),
    );
    const rows = await api.childList('/suppliers/{id}/block-history', { id: 'f1' } as Row);
    expect(rows.map((r) => r.id)).toEqual(['e1', '']);
    expect(mock.calls.at(-1)!.search).toBe(''); // sin ?foreignKey=
    expect(await api.childList('~/hr/positions/{id}/holders', { id: 'f1' } as Row)).toEqual([{ id: 'h1' }]);
  });

  it('el hijo con foreignKey lleva ?<fk>={id}', async () => {
    server.use(
      http.get(`${API}/v1/history`, ({ request }) => {
        mock.calls.push({ method: 'GET', path: '/api/v1/history', search: new URL(request.url).search, headers: {} });
        return HttpResponse.json({ items: [] });
      }),
    );
    const cfg = normalizeEntityConfig({ id: 'x', apiEndpoint: '/x', detail: { childGrids: [{ id: 'h', apiEndpoint: '/history', foreignKey: 'xId' }] } });
    await makeApi().childList(cfg.detail.childGrids[0]!.endpoint, { id: 'a/b' } as Row);
    expect(mock.calls.at(-1)!.search).toBe('?xId=a%2Fb');
  });

  it('raw pasa la ruta y la inicialización al cliente', async () => {
    server.use(http.get(`${API}/modules/current`, () => HttpResponse.json({ modules: ['a'] })));
    expect(await makeApi().raw<{ modules: string[] }>('/modules/current')).toEqual({ modules: ['a'] });
  });

  it('catalog: PascalCase -> camelCase con `id` en cadena', async () => {
    server.use(
      http.get(`${API}/catalogs/facility-types`, () =>
        HttpResponse.json([
          { ID: 'ft1', Name: 'Edificio', Description: 'x', RequiresContact: true, Active: true },
          { ID: 7, ISOCode: 'ES', Name: 'Otro' },
        ]),
      ),
      http.get(`${API}/reference/boundary-levels`, () => HttpResponse.json({ items: [{ id: 'l1', name: 'Provincia' }] })),
    );
    const api = makeApi();
    expect(await api.catalog('/catalogs/facility-types')).toEqual([
      { id: 'ft1', name: 'Edificio', description: 'x', requiresContact: true, active: true },
      { id: '7', isoCode: 'ES', name: 'Otro' },
    ]);
    expect(await api.catalog('/reference/boundary-levels')).toEqual([{ id: 'l1', name: 'Provincia' }]);
  });

  it('positionChart: árbol {position, reports[]} con banderas en camelCase y depth acotado', async () => {
    server.use(
      http.get(`${API}/hr/positions/root/chart`, ({ request }) => {
        mock.calls.push({ method: 'GET', path: '/api/hr/positions/root/chart', search: new URL(request.url).search, headers: {} });
        return HttpResponse.json({
          Position: { ID: 'root', name: 'Dirección', Flags: { Salaried: true } },
          Reports: [
            { position: { id: 'p2', flags: { Salaried: false } }, reports: [] },
            { position: { id: 'p3' } },
          ],
        });
      }),
    );
    const api = makeApi();
    const chart = await api.positionChart('root');
    expect(mock.calls.at(-1)!.search).toBe('?depth=3');
    expect(chart.position).toEqual({ id: 'root', name: 'Dirección', flags: { salaried: true } });
    expect(chart.reports.map((n) => n.position.id)).toEqual(['p2', 'p3']);
    expect(chart.reports[0]!.position.flags).toEqual({ salaried: false });
    expect(chart.reports[1]!.reports).toEqual([]);
    await api.positionChart('root', 99);
    expect(mock.calls.at(-1)!.search).toBe('?depth=20');
    await api.positionChart('root', 0);
    expect(mock.calls.at(-1)!.search).toBe('?depth=3');
  });

  it('boundaries y boundary: página number/size y antecesores', async () => {
    server.use(
      http.get(`${API}/geography/boundaries`, ({ request }) => {
        mock.calls.push({ method: 'GET', path: '/api/geography/boundaries', search: new URL(request.url).search, headers: {} });
        return HttpResponse.json({
          items: [{ id: 'b1', name: 'Madrid', nutsCode: 'ES30', level: 2, parent: 'b0', active: true }],
          total: 19,
          number: 1,
          size: 100,
          totalPages: 1,
        });
      }),
      http.get(`${API}/geography/boundaries/b1`, () =>
        HttpResponse.json({ id: 'b1', name: 'Madrid', ancestors: [{ id: 'b0', name: 'España' }], groupings: [{ id: 'eu', name: 'UE' }] }),
      ),
    );
    const api = makeApi();
    const page = await api.boundaries({ parent: 'b0', level: 2, size: 5000 });
    expect(mock.calls.at(-1)!.search).toBe('?parent=b0&level=2&page=1&size=1000');
    expect(page).toMatchObject({ total: 19, page: 1, pageSize: 100 });
    expect(page.items[0]).toMatchObject({ id: 'b1', nutsCode: 'ES30' });
    const detail = await api.boundary('b1');
    expect(detail.ancestors.map((a) => a.id)).toEqual(['b0']);
    expect(detail.groupings).toEqual([{ id: 'eu', name: 'UE' }]);
    await api.boundaries();
    expect(mock.calls.at(-1)!.search).toBe('?page=1&size=100');
  });
});

describe('A1 §5 punto 15: errores RFC 9457 mapeados a campos', () => {
  it('un 400 de alta con `errors` y `errorCodes` llega como errores de cada campo', async () => {
    server.use(
      http.post(`${API}/v1/facilities`, () =>
        HttpResponse.json(
          {
            title: 'Validation Failed',
            status: 400,
            detail: 'One or more fields are invalid',
            instance: '/api/v1/facilities',
            errors: { type: ['type must be a facility type id'], name: ['name is required'] },
            errorCodes: { type: ['format'], name: ['required'] },
          },
          { status: 400, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    );
    const error = (await makeApi().create(facility(), {}).catch((e: unknown) => e)) as ApiError;
    expect(error.kind).toBe('validation');
    expect(error.fieldErrors).toEqual({
      type: [{ code: 'format', message: 'type must be a facility type id' }],
      name: [{ code: 'required', message: 'name is required' }],
    });
  });

  it('un 400 con `errors` en lista (rutas sin v1) llega igual', async () => {
    server.use(
      http.post(`${API}/hr/positions`, () =>
        HttpResponse.json(
          { title: 'Validation Failed', status: 400, errors: [{ field: 'unit', code: 'required', message: 'unit is required' }], correlationId: 'c1' },
          { status: 400 },
        ),
      ),
    );
    const error = (await makeApi().raw('/hr/positions', { method: 'POST', body: '{}' }).catch((e: unknown) => e)) as ApiError;
    expect(error.fieldErrors).toEqual({ unit: [{ code: 'required', message: 'unit is required' }] });
  });
});

describe('expandTemplate', () => {
  it('sustituye {id} y {campo.anidado}; lo que falta queda vacío', () => {
    expect(expandTemplate('/a/{id}/b/{loc.country}/{missing}', { id: 'x y', loc: { country: 'ES' } })).toBe('/a/x%20y/b/ES/');
    expect(expandTemplate('/sin/marcas', { id: '1' })).toBe('/sin/marcas');
    expect(expandTemplate('/n/{n}/{z}', { n: 0, z: null })).toBe('/n/0/');
  });
});
