// Pruebas en Node de los handlers de MSW de la demostración (demo/mocks): el contrato de Go de
// docs/analisis/02-api-karpo-go.md (orden con lista blanca, filtros ignorados, `version`, 201/204/202,
// problem+json en sus dos formas, sesión y permisos).
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { mock, resetDataset, server, setMockOptions } from '../demo/mocks/node.ts';

const BASE = 'http://karpo.test';

async function call(method, path, { body, headers, token } = {}) {
  const init = { method, headers: { ...headers } };
  if (token) init.headers.Authorization = `Bearer ${token}`;
  if (body !== undefined) {
    init.headers['Content-Type'] = 'application/json';
    init.body = typeof body === 'string' ? body : JSON.stringify(body);
  }
  const res = await fetch(BASE + path, init);
  const text = await res.text();
  return { status: res.status, headers: res.headers, body: text ? JSON.parse(text) : null };
}

beforeAll(() => server.listen({ onUnhandledFrame: 'error' }));
afterEach(() => {
  server.resetHandlers();
  resetDataset();
  setMockOptions({ requireAuth: false, latencyMs: 0, requireApproval: false });
});
afterAll(() => server.close());

describe('lista del contrato de mantenimiento', () => {
  it('GET devuelve la página con total y totalCount y 20 por omisión', async () => {
    const res = await call('GET', '/api/v1/facilities');
    expect(res.status).toBe(200);
    expect(Object.keys(res.body).sort()).toEqual(['items', 'pageNumber', 'pageSize', 'total', 'totalCount', 'totalPages']);
    expect(res.body).toMatchObject({ total: 30, totalCount: 30, pageNumber: 1, pageSize: 20, totalPages: 2 });
    expect(res.body.items).toHaveLength(20);
  });

  it('un orden no admitido da 400 con errors.sortBy y errorCodes.sortBy', async () => {
    const res = await call('GET', '/api/v1/facilities?sortBy=areaM2');
    expect(res.status).toBe(400);
    expect(res.headers.get('content-type')).toContain('application/problem+json');
    expect(res.body.errors.sortBy).toHaveLength(1);
    expect(res.body.errorCodes.sortBy).toEqual(['enum']);
    expect(res.body.title).toBe('Validation Failed');
  });

  it('el orden admitido ordena y desempata por identidad', async () => {
    const asc = await call('GET', '/api/v1/products?sortBy=sku&sortOrder=asc&pageSize=3');
    const desc = await call('GET', '/api/v1/products?sortBy=sku&sortOrder=desc&pageSize=3');
    expect(asc.body.items.map((p) => p.sku)).toEqual(['SKU-0001', 'SKU-0002', 'SKU-0003']);
    expect(desc.body.items.map((p) => p.sku)).toEqual(['SKU-0120', 'SKU-0119', 'SKU-0118']);
  });

  it('un filtro desconocido se ignora en silencio (GET y POST /search)', async () => {
    const all = await call('GET', '/api/v1/facilities?pageSize=100');
    const ignored = await call('GET', '/api/v1/facilities?pageSize=100&colour=red&nonsense=1');
    expect(ignored.status).toBe(200);
    expect(ignored.body.total).toBe(all.body.total);
    const search = await call('POST', '/api/v1/facilities/search', { body: { filter: { colour: 'red' }, pagination: { pageNumber: 1, pageSize: 100 } } });
    expect(search.status).toBe(200);
    expect(search.body.total).toBe(all.body.total);
  });

  it('los filtros admitidos filtran: search, isActive, kind, año, eu', async () => {
    expect((await call('GET', '/api/v1/facilities?search=sala')).body.total).toBeGreaterThan(0);
    const kind = await call('GET', '/api/v1/products?kind=service&pageSize=200');
    expect(kind.body.items.every((p) => p.kind === 'service')).toBe(true);
    expect(kind.body.total).toBe(30);
    const year = await call('GET', '/api/v1/holidays?year=2027&pageSize=100');
    expect(year.body.items.every((h) => h.date.startsWith('2027-'))).toBe(true);
    const eu = await call('GET', '/api/v1/countries?eu=true&pageSize=100');
    expect(eu.body.total).toBe(27);
    expect((await call('GET', '/api/v1/suppliers?isActive=true&pageSize=100')).body.total).toBe(21);
  });

  it('el máximo de pageSize es 1000 y fuera de rango da 400', async () => {
    const big = await call('GET', '/api/v1/countries?pageSize=1000');
    expect(big.status).toBe(200);
    expect(big.body.items).toHaveLength(248);
    const over = await call('GET', '/api/v1/countries?pageSize=1001');
    expect(over.status).toBe(400);
    expect(over.body.errorCodes.pageSize).toEqual(['range']);
    expect((await call('GET', '/api/v1/countries?page=0')).status).toBe(400);
    expect((await call('GET', '/api/v1/countries?eu=quizas')).status).toBe(400);
  });

  it('POST /search: un filtro con valor no escalar da 400 y el orden se valida igual', async () => {
    const objectFilter = await call('POST', '/api/v1/facilities/search', { body: { filter: { type: { a: 1 } } } });
    expect(objectFilter.status).toBe(400);
    expect(objectFilter.body.errorCodes.type).toEqual(['type']);
    const badSort = await call('POST', '/api/v1/products/search', { body: { pagination: { orderBy: 'cost' } } });
    expect(badSort.status).toBe(400);
    expect(badSort.body.errors.sortBy).toBeDefined();
    const ok = await call('POST', '/api/v1/products/search', { body: { filter: { kind: 'good' }, pagination: { pageNumber: 2, pageSize: 10, orderBy: 'name', ascending: false } } });
    expect(ok.body).toMatchObject({ pageNumber: 2, pageSize: 10, total: 90 });
  });

  it('un cuerpo que no es JSON da 400 errors.body', async () => {
    const res = await call('POST', '/api/v1/facilities/search', { body: 'esto no es json' });
    expect(res.status).toBe(400);
    expect(res.body.errorCodes.body).toEqual(['json']);
  });

  it('el ámbito X-Organization-Scope estrecha la lista y uno mal formado da 400', async () => {
    const org = mock.data.organizations[0].id;
    const scoped = await call('GET', '/api/v1/facilities?pageSize=100', { headers: { 'X-Organization-Scope': org } });
    expect(scoped.body.items.length).toBeGreaterThan(0);
    expect(scoped.body.items.every((f) => f.organization === org)).toBe(true);
    expect(scoped.body.total).toBeLessThan(30);
    expect((await call('GET', '/api/v1/facilities', { headers: { 'X-Organization-Scope': 'no-es-un-uuid' } })).status).toBe(400);
  });
});

describe('detalle, alta, cambio y baja', () => {
  const firstFacility = () => ({ ...mock.data.facilities[0] });

  it('GET del detalle: 200, 404 si no existe y 400 si el id no es válido', async () => {
    const f = firstFacility();
    expect((await call('GET', `/api/v1/facilities/${f.id}`)).body.name).toBe(f.name);
    expect((await call('GET', '/api/v1/facilities/0192a000-0000-7000-8000-00000000ffff')).status).toBe(404);
    const bad = await call('GET', '/api/v1/facilities/xyz');
    expect(bad.status).toBe(400);
    expect(bad.body.errorCodes.id).toEqual(['format']);
  });

  it('PUT sin version da 400 con código required', async () => {
    const f = firstFacility();
    const res = await call('PUT', `/api/v1/facilities/${f.id}`, { body: { name: 'Otro nombre' } });
    expect(res.status).toBe(400);
    expect(res.body.errors.version).toBeDefined();
    expect(res.body.errorCodes.version).toEqual(['required']);
  });

  it('PUT con una versión caducada da 409 y no cambia nada', async () => {
    const f = firstFacility();
    const res = await call('PUT', `/api/v1/facilities/${f.id}`, { body: { name: 'Otro nombre', version: 99 } });
    expect(res.status).toBe(409);
    expect(res.headers.get('content-type')).toContain('application/problem+json');
    expect(res.body.detail).toMatch(/changed after it was read/);
    expect((await call('GET', `/api/v1/facilities/${f.id}`)).body.name).toBe(f.name);
  });

  it('PUT se comporta como PATCH: mezcla, sube la versión en 1 y conserva lo que falta', async () => {
    const f = firstFacility();
    const res = await call('PUT', `/api/v1/facilities/${f.id}`, { body: { name: 'Sede renombrada', version: f.version } });
    expect(res.status).toBe(200);
    expect(res.body).toMatchObject({ name: 'Sede renombrada', version: f.version + 1, typeName: 'Building', organization: f.organization });
    // Devolver lo que se leyó (con campos que sobran) también vale, y sin cambios no sube la versión.
    const echoed = await call('PUT', `/api/v1/facilities/${f.id}`, { body: { ...res.body, extra: 'se ignora' } });
    expect(echoed.status).toBe(200);
    expect(echoed.body.version).toBe(f.version + 1);
  });

  it('un campo inmutable cambiado da 400 con código immutable', async () => {
    const f = firstFacility();
    const res = await call('PUT', `/api/v1/facilities/${f.id}`, { body: { type: mock.data.facilityTypes[0].ID, version: f.version } });
    expect(res.status).toBe(400);
    expect(res.body.errorCodes.type).toEqual(['immutable']);
    const holiday = mock.data.holidays[0];
    const date = await call('PUT', `/api/v1/holidays/${holiday.id}`, { body: { date: '2026-02-02', version: holiday.version } });
    expect(date.body.errorCodes.date).toEqual(['immutable']);
  });

  it('POST da 201 con Location y el objeto con version 1', async () => {
    const f = firstFacility();
    const res = await call('POST', '/api/v1/facilities', {
      body: { organization: f.organization, type: f.type, name: 'Edificio nuevo', areaM2: '350', location: { postalCode: '28001', country: 'ES' } },
    });
    expect(res.status).toBe(201);
    expect(res.headers.get('location')).toBe(`/api/v1/facilities/${res.body.id}`);
    expect(res.body).toMatchObject({ name: 'Edificio nuevo', version: 1, active: true, typeName: 'Building' });
    expect(res.body.location.geoBoundary).toBeTruthy();
    expect((await call('GET', res.headers.get('location'))).status).toBe(200);
  });

  it('POST con datos inválidos da 400 con errores por campo (type, name)', async () => {
    const f = firstFacility();
    const res = await call('POST', '/api/v1/facilities', { body: { organization: f.organization, type: 'no', name: '' } });
    expect(res.status).toBe(400);
    expect(res.body.errorCodes.type).toEqual(['format']);
    expect(res.body.errorCodes.name).toEqual(['required']);
  });

  it('DELETE da 204: desactiva instalaciones, retira productos y borra festivos', async () => {
    const f = firstFacility();
    expect((await call('DELETE', `/api/v1/facilities/${f.id}`)).status).toBe(204);
    const after = await call('GET', `/api/v1/facilities/${f.id}`);
    expect(after.body).toMatchObject({ active: false, version: f.version + 1 });
    // Repetir una baja ya dada es 204 sin versión nueva.
    expect((await call('DELETE', `/api/v1/facilities/${f.id}`)).status).toBe(204);
    expect((await call('GET', `/api/v1/facilities/${f.id}`)).body.version).toBe(f.version + 1);

    const product = mock.data.products.find((p) => p.discontinued === '');
    expect((await call('DELETE', `/api/v1/products/${product.id}`)).status).toBe(204);
    expect((await call('GET', `/api/v1/products/${product.id}`)).body.discontinued).toBe('2026-11-27');

    const holiday = mock.data.holidays[0];
    expect((await call('DELETE', `/api/v1/holidays/${holiday.id}`)).status).toBe(204);
    expect((await call('GET', `/api/v1/holidays/${holiday.id}`)).status).toBe(404);
  });

  it('los países son de sólo lectura (409) y los activos no se borran (405)', async () => {
    const country = await call('PUT', '/api/v1/countries/ES', { body: { name: 'España', version: 1 } });
    expect(country.status).toBe(409);
    expect((await call('POST', '/api/v1/countries', { body: {} })).status).toBe(409);
    const asset = mock.data.assets[0];
    expect((await call('DELETE', `/api/v1/assets/${asset.id}`)).status).toBe(405);
    const detail = await call('GET', '/api/v1/countries/ES');
    expect(detail.body).toMatchObject({ alpha2: 'ES', ibanLength: 24, currencies: ['EUR'] });
    expect((await call('GET', '/api/v1/countries?search=spain')).body.items[0].currencies).toBeNull();
  });

  it('un centro de trabajo se cierra con DELETE y no se reabre; un activo se da de baja con el cambio', async () => {
    const wc = mock.data.workCenters.find((w) => w.active);
    expect((await call('DELETE', `/api/v1/work-centers/${wc.id}?on=2026-11-30`)).status).toBe(204);
    const closed = (await call('GET', `/api/v1/work-centers/${wc.id}`)).body;
    expect(closed).toMatchObject({ active: false, closed: '2026-11-30' });
    const reopen = await call('PUT', `/api/v1/work-centers/${wc.id}`, { body: { closed: '', version: closed.version } });
    expect(reopen.body.errorCodes.closed).toEqual(['immutable']);

    const asset = { ...mock.data.assets.find((a) => a.status === 'in-service') };
    const disposed = await call('PUT', `/api/v1/assets/${asset.id}`, { body: { disposed: '2026-11-20', disposal: 'scrap', version: asset.version } });
    expect(disposed.status).toBe(200);
    expect(disposed.body).toMatchObject({ status: 'disposed', disposal: 'scrap', version: asset.version + 1 });
  });
});

describe('bloqueos con motivo, aprobación (202) e historial', () => {
  const supplier = () => ({ ...mock.data.suppliers.find((s) => !s.blocked) });

  it('bloquear exige motivo (422) y versión (400), y la versión vigente (409)', async () => {
    const s = supplier();
    const noReason = await call('POST', `/api/v1/suppliers/${s.id}/block`, { body: { version: s.version } });
    expect(noReason.status).toBe(422);
    expect(noReason.body.code).toBe('purchases.block_reason_required');
    expect((await call('POST', `/api/v1/suppliers/${s.id}/block`, { body: { reason: 'x' } })).body.errorCodes.version).toEqual(['required']);
    expect((await call('POST', `/api/v1/suppliers/${s.id}/block`, { body: { version: 77, reason: 'x' } })).status).toBe(409);
  });

  it('bloquear y desbloquear sube la versión y añade al historial', async () => {
    const s = supplier();
    const before = (await call('GET', `/api/v1/suppliers/${s.id}/block-history`)).body.length;
    const blocked = await call('POST', `/api/v1/suppliers/${s.id}/block`, { body: { version: s.version, reason: 'Revisión fiscal' } });
    expect(blocked.status).toBe(200);
    expect(blocked.body).toMatchObject({ blocked: true, blockReason: 'Revisión fiscal', version: s.version + 1 });
    const unblocked = await call('POST', `/api/v1/suppliers/${s.id}/unblock`, { body: { version: s.version + 1, reason: 'Resuelto' } });
    expect(unblocked.body).toMatchObject({ blocked: false, unblockReason: 'Resuelto', version: s.version + 2 });
    const history = (await call('GET', `/api/v1/suppliers/${s.id}/block-history`)).body;
    expect(history).toHaveLength(before + 2);
    expect(history.at(-1)).toMatchObject({ blocked: false, reason: 'Resuelto' });
    // Bloquear lo ya bloqueado: 422.
    const again = await call('POST', `/api/v1/suppliers/${s.id}/unblock`, { body: { version: s.version + 2, reason: 'Otra vez' } });
    expect(again.status).toBe(422);
  });

  it('una orden forzada da 202 con pendingApproval y no cambia el perfil', async () => {
    const s = supplier();
    const forced = await call('POST', `/api/v1/suppliers/${s.id}/block?fallo=202`, { body: { version: s.version, reason: 'Pendiente' } });
    expect(forced.status).toBe(202);
    expect(typeof forced.body.pendingApproval).toBe('string');
    expect(forced.body).toMatchObject({ blocked: false, version: s.version });
    expect((await call('GET', `/api/v1/suppliers/${s.id}`)).body).toMatchObject({ blocked: false, version: s.version });

    const c = mock.data.carriers.find((x) => !x.blocked);
    const del = await call('DELETE', `/api/v1/carriers/${c.id}?aprobacion=1`, { body: { reason: 'Motivo' } });
    expect(del.status).toBe(202);
    expect(del.body.pendingApproval).toBeTruthy();
    setMockOptions({ requireApproval: true });
    const other = mock.data.carriers.filter((x) => !x.blocked)[1];
    expect((await call('POST', `/api/v1/carriers/${other.id}/block`, { body: { version: other.version, reason: 'Motivo' } })).status).toBe(202);
  });

  it('DELETE bloquea con motivo (cuerpo o ?reason=) y 204; sin motivo 422; ya bloqueado 204', async () => {
    const c = { ...mock.data.carriers.find((x) => !x.blocked) };
    const noReason = await call('DELETE', `/api/v1/carriers/${c.id}`);
    expect(noReason.status).toBe(422);
    expect(noReason.body.code).toBe('shipments.block_reason_required');
    expect((await call('DELETE', `/api/v1/carriers/${c.id}?reason=Incidencia`)).status).toBe(204);
    const after = (await call('GET', `/api/v1/carriers/${c.id}`)).body;
    expect(after).toMatchObject({ blocked: true, blockReason: 'Incidencia', version: c.version + 1 });
    expect((await call('DELETE', `/api/v1/carriers/${c.id}`)).status).toBe(204);
    const history = (await call('GET', `/api/v1/carriers/${c.id}/block-history`)).body;
    expect(history.at(-1)).toMatchObject({ blocked: true, reason: 'Incidencia' });
    expect(history.at(-1).byName).toBeTruthy();
  });

  it('cambiar `blocked` con PUT es 422: se usan las órdenes', async () => {
    const s = supplier();
    const res = await call('PUT', `/api/v1/suppliers/${s.id}`, { body: { blocked: true, version: s.version } });
    expect(res.status).toBe(422);
    expect(res.body.code).toBe('purchases.block_change_requires_command');
  });
});

describe('formas de error', () => {
  it('en /api/v1: errors como diccionario y errorCodes en el mismo orden', async () => {
    const res = await call('POST', '/api/v1/facilities', { body: { organization: 'x', type: 'y', name: '' } });
    expect(res.status).toBe(400);
    expect(Array.isArray(res.body.errors)).toBe(false);
    expect(Object.keys(res.body.errors).sort()).toEqual(['name', 'name', 'organization', 'type'].filter((v, i, a) => a.indexOf(v) === i));
    expect(res.body.errors.name).toHaveLength(res.body.errorCodes.name.length);
    expect(res.body.correlationId).toBeUndefined();
  });

  it('fuera de /api/v1: errors como lista [{field, code, message}] y correlationId', async () => {
    const res = await call('GET', '/api/hr/positions/no-es-un-uuid');
    expect(res.status).toBe(400);
    expect(Array.isArray(res.body.errors)).toBe(true);
    expect(res.body.errors[0]).toEqual({ field: 'id', code: 'format', message: expect.any(String) });
    expect(res.body.correlationId).toBeTruthy();
    expect(res.body.errorCodes).toBeUndefined();
    const level = await call('GET', '/api/geography/boundaries?level=planeta');
    expect(level.status).toBe(400);
    expect(level.body.errors[0].field).toBe('level');
  });

  it('?fallo= fuerza 409, 422 y 500 con problem+json', async () => {
    const f = mock.data.facilities[0];
    for (const code of [409, 422, 500]) {
      const res = await call('PUT', `/api/v1/facilities/${f.id}?fallo=${code}`, { body: { name: 'x', version: f.version } });
      expect(res.status).toBe(code);
      expect(res.headers.get('content-type')).toContain('application/problem+json');
      expect(res.body.status).toBe(code);
    }
    const list = await call('GET', '/api/v1/facilities?fallo=500');
    expect(list.status).toBe(500);
    // Un fallo desconocido se ignora.
    expect((await call('GET', '/api/v1/facilities?fallo=418')).status).toBe(200);
  });
});

describe('puestos, geografía, catálogos y organizaciones', () => {
  const root = () => mock.data.positions.find((p) => p.reportsTo === '');

  it('GET /api/hr/positions pagina con {items,total,number,size,totalPages} y filtra', async () => {
    const res = await call('GET', '/api/hr/positions?size=10&page=2');
    expect(res.body).toMatchObject({ total: 45, number: 2, size: 10, totalPages: 5 });
    expect(res.body.items).toHaveLength(10);
    const vacant = await call('GET', '/api/hr/positions?vacant=true&size=100');
    expect(vacant.body.total).toBe(7);
    expect(vacant.body.items.every((p) => p.vacant)).toBe(true);
    expect(root().typeTitle).toBe('Director/a General');
    expect(root().flags).toEqual({ Salaried: true, Exempt: true, FullTime: true, Temporary: false });
  });

  it('el organigrama anida {position, reports} con la profundidad pedida', async () => {
    const r = root();
    const depth = (node) => (node.reports.length === 0 ? 1 : 1 + Math.max(...node.reports.map(depth)));
    const three = (await call('GET', `/api/hr/positions/${r.id}/chart?depth=3`)).body;
    expect(three.position.id).toBe(r.id);
    expect(three.reports).toHaveLength(4);
    expect(depth(three)).toBe(3);
    expect(three.reports.every((n) => n.position.reportsTo === r.id)).toBe(true);
    const count = (node) => 1 + node.reports.reduce((sum, n) => sum + count(n), 0);
    expect(count((await call('GET', `/api/hr/positions/${r.id}/chart?depth=5`)).body)).toBe(45);
    expect(depth((await call('GET', `/api/hr/positions/${r.id}/chart`)).body)).toBe(3);
    expect((await call('GET', `/api/hr/positions/${r.id}/chart?depth=0`)).status).toBe(400);
    expect((await call('GET', `/api/hr/positions/${r.id}/chart?depth=21`)).status).toBe(400);
  });

  it('las delimitaciones: hijas por parent, nivel, tamaño máximo y antecesores', async () => {
    const es = mock.data.boundaries.find((b) => b.geoCode === 'ES');
    const nuts1 = await call('GET', `/api/geography/boundaries?parent=${es.id}&size=100`);
    expect(nuts1.body.total).toBe(7);
    expect(nuts1.body.items.every((b) => b.level === '1')).toBe(true);
    const level2 = await call('GET', '/api/geography/boundaries?level=2&country=ES&size=5000');
    expect(level2.body).toMatchObject({ total: 19, size: 1000 });
    const madridCity = mock.data.boundaries.find((b) => b.lauCode === '28079');
    const detail = (await call('GET', `/api/geography/boundaries/${madridCity.id}`)).body;
    expect(detail.ancestors.map((a) => a.name)).toEqual(['Madrid', 'Comunidad de Madrid', 'Comunidad de Madrid', 'Spain']);
    expect(detail.groupings).toEqual([]);
    expect((await call('GET', '/api/geography/boundaries?q=alcala&level=local')).body.items[0].name).toBe('Alcalá de Henares');
    expect((await call('GET', '/api/geography/boundaries?country=ES&size=1000')).body.total).toBe(114);
  });

  it('los catálogos van en PascalCase salvo boundary-levels', async () => {
    const types = (await call('GET', '/api/catalogs/facility-types')).body;
    expect(types[0]).toEqual({ ID: expect.any(String), Name: 'Warehouse', Description: expect.any(String), RequiresContact: false, Active: true });
    const hr = (await call('GET', '/api/hr/catalogs/position-types?active=true')).body;
    expect(hr[0]).toHaveProperty('Title');
    const levels = (await call('GET', '/api/reference/boundary-levels')).body;
    expect(levels[0]).toHaveProperty('level');
    expect((await call('GET', '/api/reference/boundary-types')).body[0]).toHaveProperty('Name');
  });

  it('organizaciones y partes: nombres, página y detalle', async () => {
    const names = (await call('GET', '/api/internal-organizations/names')).body;
    expect(names).toHaveLength(3);
    expect(Object.keys(names[0]).sort()).toEqual(['id', 'name']);
    const suppliers = await call('GET', '/api/parties?role=10000000-0000-0000-0001-000000000003&size=100');
    expect(suppliers.body.total).toBe(25);
    const search = await call('GET', `/api/parties?q=${encodeURIComponent('direccion')}`);
    expect(search.body.items.some((p) => p.name === 'Dirección General')).toBe(true);
    const one = await call('GET', `/api/parties/${mock.data.parties[0].id}`);
    expect(one.body.kind).toBe('Organization');
  });
});

describe('sesión y permisos (requireAuth)', () => {
  beforeEach(() => setMockOptions({ requireAuth: true }));

  const login = (username, password) => call('POST', '/api/auth/login', { body: { username, password } });

  it('sin token: 401 con WWW-Authenticate', async () => {
    const res = await call('GET', '/api/v1/facilities');
    expect(res.status).toBe(401);
    expect(res.headers.get('www-authenticate')).toBe('Bearer');
  });

  it('login de Go: tokens, expiresAt, userId, username y mustChangePassword (sin success)', async () => {
    const res = await login('demo', 'Demo-Karpo-2026!');
    expect(res.status).toBe(200);
    expect(Object.keys(res.body).sort()).toEqual(['accessToken', 'expiresAt', 'mustChangePassword', 'refreshToken', 'userId', 'username']);
    expect(res.body.mustChangePassword).toBe(false);
    expect(res.body).not.toHaveProperty('success');
    expect((await call('GET', '/api/v1/facilities', { token: res.body.accessToken })).status).toBe(200);
    expect((await login('demo', 'mala')).status).toBe(401);
    const missing = await call('POST', '/api/auth/login', { body: {} });
    expect(missing.status).toBe(400);
    expect(Array.isArray(missing.body.errors)).toBe(true);
  });

  it('refresh rota el par de tokens y logout retira el refresco', async () => {
    const first = (await login('demo', 'Demo-Karpo-2026!')).body;
    const second = await call('POST', '/api/auth/refresh', { body: { refreshToken: first.refreshToken } });
    expect(second.status).toBe(200);
    expect(second.body.accessToken).not.toBe(first.accessToken);
    expect((await call('POST', '/api/auth/refresh', { body: { refreshToken: first.refreshToken } })).status).toBe(401);
    expect((await call('POST', '/api/auth/logout', { body: { refreshToken: second.body.refreshToken } })).status).toBe(204);
    expect((await call('POST', '/api/auth/refresh', { body: { refreshToken: second.body.refreshToken } })).status).toBe(401);
  });

  it('un token caducado da 401', async () => {
    const { accessToken } = (await login('demo', 'Demo-Karpo-2026!')).body;
    mock.accessTokens.get(accessToken).expiresAt = Date.now() - 1;
    expect((await call('GET', '/api/v1/facilities', { token: accessToken })).status).toBe(401);
  });

  it('/api/auth/context devuelve los permisos y el ámbito', async () => {
    const { accessToken } = (await login('lectura', 'Lectura-Karpo-2026!')).body;
    const res = await call('GET', '/api/auth/context', { token: accessToken });
    expect(res.body.outcome).toBe('allow');
    expect(res.body.context.isGlobalAdmin).toBe(false);
    expect(res.body.context.permissions).toContain('Facilities.Facility.Read');
    expect(res.body.context.effectiveOrganizationIds).toHaveLength(3);
    const admin = (await login('demo', 'Demo-Karpo-2026!')).body.accessToken;
    expect((await call('GET', '/api/auth/context', { token: admin })).body.context.permissions).toEqual(['*.*.*']);
  });

  it('sin el permiso: 403 en la lista y en cualquier escritura', async () => {
    const { accessToken } = (await login('lectura', 'Lectura-Karpo-2026!')).body;
    expect((await call('GET', '/api/v1/facilities', { token: accessToken })).status).toBe(200);
    expect((await call('GET', '/api/v1/work-centers', { token: accessToken })).status).toBe(403);
    const f = mock.data.facilities[0];
    const put = await call('PUT', `/api/v1/facilities/${f.id}`, { token: accessToken, body: { name: 'x', version: f.version } });
    expect(put.status).toBe(403);
  });

  it('mustChangePassword: todo da 403 salvo el contexto y el cambio, que devuelve una sesión nueva', async () => {
    const first = (await login('nuevo', 'Nuevo-Karpo-2026!')).body;
    expect(first.mustChangePassword).toBe(true);
    expect((await call('GET', '/api/v1/facilities', { token: first.accessToken })).status).toBe(403);
    expect((await call('GET', '/api/auth/context', { token: first.accessToken })).status).toBe(200);
    const weak = await call('POST', '/api/auth/change-password', { token: first.accessToken, body: { currentPassword: 'Nuevo-Karpo-2026!', newPassword: 'corta' } });
    expect(weak.status).toBe(400);
    expect(weak.body.errors[0]).toMatchObject({ field: 'password', code: 'too_short' });
    const wrong = await call('POST', '/api/auth/change-password', { token: first.accessToken, body: { currentPassword: 'mala', newPassword: 'Otra-Clave-Segura-2027!' } });
    expect(wrong.body.errors[0].field).toBe('currentPassword');
    const changed = await call('POST', '/api/auth/change-password', { token: first.accessToken, body: { currentPassword: 'Nuevo-Karpo-2026!', newPassword: 'Otra-Clave-Segura-2027!' } });
    expect(changed.status).toBe(200);
    expect(changed.body.mustChangePassword).toBe(false);
    expect((await call('GET', '/api/v1/facilities', { token: changed.body.accessToken })).status).toBe(200);
    expect((await call('GET', '/api/v1/facilities', { token: first.accessToken })).status).toBe(401);
    expect((await login('nuevo', 'Nuevo-Karpo-2026!')).status).toBe(401);
    expect((await login('nuevo', 'Otra-Clave-Segura-2027!')).status).toBe(200);
  });

  it('resetDataset devuelve datos y sesiones al estado inicial', async () => {
    const f = mock.data.facilities[0];
    setMockOptions({ requireAuth: false });
    await call('DELETE', `/api/v1/facilities/${f.id}`);
    expect(mock.data.facilities[0].active).toBe(false);
    resetDataset();
    expect(mock.data.facilities[0].active).toBe(true);
    expect(mock.data.facilities[0].version).toBe(1);
  });
});

describe('idioma de los catálogos (Accept-Language, M-8)', () => {
  const lang = (value) => ({ headers: { 'Accept-Language': value } });
  const facilityOf = (typeName) => mock.data.facilities.find((f) => f.typeName === typeName);

  it('con `en`, `typeName` sale en inglés («Building», «Room»); con `es`, en español', async () => {
    const building = facilityOf('Building');
    const room = facilityOf('Room');
    const en = await call('GET', `/api/v1/facilities/${building.id}`, lang('en'));
    expect(en.body.typeName).toBe('Building');
    expect((await call('GET', `/api/v1/facilities/${room.id}`, lang('en-GB,en;q=0.9'))).body.typeName).toBe('Room');
    const es = await call('GET', `/api/v1/facilities/${building.id}`, lang('es'));
    expect(es.body.typeName).toBe('Edificio');
    expect((await call('GET', `/api/v1/facilities/${room.id}`, lang('es-ES,es;q=0.9,en;q=0.8'))).body.typeName).toBe('Sala');
    // El dato guardado no cambia: sólo la respuesta.
    expect(mock.data.facilities.find((f) => f.id === building.id).typeName).toBe('Building');
  });

  it('sin cabecera, o con otro idioma, el dato sale como está en el conjunto de datos', async () => {
    const building = facilityOf('Building');
    expect((await call('GET', `/api/v1/facilities/${building.id}`)).body.typeName).toBe('Building');
    expect((await call('GET', `/api/v1/facilities/${building.id}`, lang('fr-FR,fr;q=0.9'))).body.typeName).toBe('Building');
    // El idioma preferido manda sobre el orden de aparición.
    expect((await call('GET', `/api/v1/facilities/${building.id}`, lang('en;q=0.4,es;q=0.9'))).body.typeName).toBe('Edificio');
    expect((await call('GET', `/api/v1/facilities/${building.id}`, lang('es;q=0'))).body.typeName).toBe('Building');
  });

  it('la lista, el alta y el cambio también traducen `typeName`', async () => {
    const list = await call('GET', '/api/v1/facilities?pageSize=100', lang('es'));
    const names = new Set(list.body.items.map((f) => f.typeName));
    expect(names.has('Edificio') && names.has('Sala')).toBe(true);
    expect([...names].every((name) => !['Building', 'Room', 'Floor'].includes(name))).toBe(true);
    const org = mock.data.organizations[0].id;
    const type = mock.data.facilityTypes.find((t) => t.Name === 'Office').ID;
    const created = await call('POST', '/api/v1/facilities', { ...lang('es'), body: { organization: org, type, name: 'Oficina nueva' } });
    expect(created.status).toBe(201);
    expect(created.body.typeName).toBe('Oficina');
    const changed = await call('PUT', `/api/v1/facilities/${created.body.id}`, { ...lang('en'), body: { name: 'New office', version: created.body.version } });
    expect(changed.body).toMatchObject({ name: 'New office', typeName: 'Office' });
  });

  it('el catálogo de tipos de instalación y el de tipos de delimitación siguen el idioma', async () => {
    const english = (await call('GET', '/api/catalogs/facility-types', lang('en'))).body;
    const spanish = (await call('GET', '/api/catalogs/facility-types', lang('es'))).body;
    expect(english.map((t) => t.Name)).toEqual(mock.data.facilityTypes.map((t) => t.Name));
    expect(spanish.map((t) => t.Name)).toEqual(['Almacén', 'Fábrica', 'Edificio', 'Oficina', 'Planta', 'Sala', 'Centro de datos', 'Centro de distribución', 'Oficina de cambio de divisas']);
    expect(spanish[0].Description).toBe('Instalación de almacenamiento de bienes y materiales');
    expect(spanish[0]).toMatchObject({ ID: english[0].ID, RequiresContact: false, Active: true });
    const boundaryTypes = (await call('GET', '/api/reference/boundary-types', lang('es'))).body;
    expect(boundaryTypes.map((t) => t.Name)).toEqual(['País', 'Región', 'Comunidad autónoma', 'Provincia', 'Isla', 'Municipio']);
    expect((await call('GET', '/api/reference/boundary-types', lang('en'))).body[2].Name).toBe('Autonomous Community');
  });

  it('las delimitaciones traducen su tipo; puestos, estados, clases y títulos, divisas, idiomas y tipos de vía también', async () => {
    const province = mock.data.boundaries.find((b) => b.typeName === 'Province');
    expect((await call('GET', `/api/geography/boundaries/${province.id}`, lang('es'))).body.typeName).toBe('Provincia');
    expect((await call('GET', '/api/geography/boundaries?level=country&size=3', lang('es'))).body.items.every((b) => b.typeName === 'País')).toBe(true);
    expect((await call('GET', '/api/geography/boundaries?level=country&size=3', lang('en'))).body.items.every((b) => b.typeName === 'Country')).toBe(true);

    const statusesEn = (await call('GET', '/api/hr/catalogs/position-statuses', lang('en'))).body;
    expect(statusesEn.map((s) => s.Name)).toEqual(['Planned', 'Open', 'Filled', 'Closed']);
    expect((await call('GET', '/api/hr/catalogs/position-statuses', lang('es'))).body.map((s) => s.Name)).toEqual(['Planificado', 'Abierto', 'Cubierto', 'Cerrado']);
    expect((await call('GET', '/api/hr/catalogs/position-classes', lang('en'))).body.map((c) => c.Name)).toEqual(['Management', 'Middle management', 'Specialist']);
    const titlesEn = (await call('GET', '/api/hr/catalogs/position-types', lang('en'))).body;
    expect(titlesEn[0]).toMatchObject({ Title: 'General Manager', Description: 'General Manager position' });
    expect((await call('GET', '/api/hr/catalogs/position-types', lang('es'))).body[0].Title).toBe('Director/a General');
    expect(titlesEn.every((t) => !/[áéíóúñ/]/.test(t.Title))).toBe(true);

    const root = mock.data.positions.find((p) => !p.reportsTo);
    const chart = (await call('GET', `/api/hr/positions/${root.id}/chart?depth=2`, lang('en'))).body;
    expect(chart.position.typeTitle).toBe('General Manager');
    expect(chart.reports[0].position.typeTitle).toMatch(/Director$/);
    // `statusName` es la clave del badgeMap de la configuración: no se traduce en las filas.
    expect(chart.position.statusName).toBe(root.statusName);
    expect((await call('GET', `/api/hr/positions/${root.id}`, lang('en'))).body.typeTitle).toBe('General Manager');
    expect((await call('GET', '/api/hr/positions?number=1&size=2', lang('en'))).body.items[0].typeTitle).toBe('General Manager');
    expect((await call('GET', `/api/hr/positions/${root.id}`)).body.typeTitle).toBe('Director/a General');

    expect((await call('GET', '/api/reference/currencies', lang('es'))).body[1].Name).toBe('Dólar estadounidense');
    expect((await call('GET', '/api/reference/languages', lang('es'))).body.map((l) => l.Name)).toEqual(['Español', 'Inglés', 'Catalán']);
    expect((await call('GET', '/api/reference/street-types', lang('en'))).body.map((s) => s.Name)).toEqual(['Street', 'Avenue', 'Promenade', 'Square', 'Road', 'Ring road']);
    expect((await call('GET', '/api/reference/boundary-levels', lang('en'))).body.map((l) => l.name)).toEqual([
      'Country',
      'Grouping of autonomous communities',
      'Autonomous community',
      'Province or island',
      'Municipality',
    ]);
  });
});
