// Sembrador de la demostración contra un `cmd/karpo` en marcha (modo `local`). Crea por HTTP los
// mismos datos que el conjunto determinista de demo/data/dataset.mjs, en el orden de
// docs/analisis/02-api-karpo-go.md §7:
//
//   1. cambia la contraseña del administrador de arranque (mustChangePassword)
//   2. partes: organizaciones internas, unidades, proveedores, transportistas y personas
//   3. instalaciones (árbol), centros de trabajo, festivos
//   4. productos, activos, proveedores y transportistas (con su historial de bloqueos)
//   5. puestos (organigrama): altas, dependencias y titulares
//
// Los UUID de Go son otros: se guarda la correspondencia «id del dataset → id de Go» y las
// delimitaciones se resuelven por CÓDIGO (país alfa-2, NUTS, LAU/INE). Las altas de productos,
// activos, proveedores y transportistas no estaban probadas (A2 §9): cada fallo se tolera y se
// informa, y lo que depende de ello se omite con su motivo.
//
// Uso: node scripts/seed-karpo.mjs --url http://127.0.0.1:PUERTO --user admin --password <arranque>
//        [--new-password <nueva>] [--already-changed] [--report fichero.json] [--out credenciales.json]
// También por entorno: KARPO_URL, KARPO_BOOTSTRAP_ADMIN_USER, KARPO_BOOTSTRAP_ADMIN_PASSWORD.
import { randomBytes } from 'node:crypto';
import { mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { ROLE, generateDataset } from '../demo/data/dataset.mjs';

const RELATIONSHIP_ROLLUP = '10000000-0000-0000-0002-000000000005';
const RELATIONSHIP_EMPLOYMENT = '10000000-0000-0000-0002-000000000001';

/** Contraseña nueva aleatoria que cumple la política de Go (12 a 128 caracteres). */
export function randomPassword() {
  return `Karpo-${randomBytes(12).toString('base64url')}-9`;
}

class SeedError extends Error {
  constructor(message, status, body) {
    super(message);
    this.status = status;
    this.body = body;
  }
}

/** Cliente HTTP mínimo con sesión y un único refresco ante 401. */
function createClient(baseUrl) {
  const root = baseUrl.replace(/\/+$/, '');
  const session = { accessToken: '', refreshToken: '' };

  async function raw(method, route, { body, headers = {}, auth = true } = {}) {
    const init = { method, headers: { Accept: 'application/json', ...headers } };
    if (auth && session.accessToken) init.headers.Authorization = `Bearer ${session.accessToken}`;
    if (body !== undefined) {
      init.headers['Content-Type'] = 'application/json';
      init.body = JSON.stringify(body);
    }
    const res = await fetch(root + route, init);
    const text = await res.text();
    let parsed = null;
    if (text) {
      try {
        parsed = JSON.parse(text);
      } catch {
        parsed = text;
      }
    }
    return { status: res.status, headers: res.headers, body: parsed };
  }

  async function request(method, route, options) {
    let res = await raw(method, route, options);
    if (res.status === 401 && session.refreshToken && options?.auth !== false) {
      const refreshed = await raw('POST', '/api/auth/refresh', { body: { refreshToken: session.refreshToken }, auth: false });
      if (refreshed.status === 200) {
        session.accessToken = refreshed.body.accessToken;
        session.refreshToken = refreshed.body.refreshToken;
        res = await raw(method, route, options);
      }
    }
    return res;
  }

  /** Como `request`, pero lanza `SeedError` si el estado no es el esperado (2xx por omisión). */
  async function expect(method, route, options = {}, accept = [200, 201, 202, 204]) {
    const res = await request(method, route, options);
    if (!accept.includes(res.status)) throw new SeedError(describe(method, route, res), res.status, res.body);
    return res;
  }

  return { raw, request, expect, session, root };
}

function describe(method, route, res) {
  const body = res.body;
  let detail = '';
  if (body && typeof body === 'object') {
    const errors = body.errors;
    if (Array.isArray(errors)) detail = errors.map((e) => `${e.field}:${e.code}`).join(', ');
    else if (errors && typeof errors === 'object') detail = Object.entries(errors).map(([k, v]) => `${k}:${[].concat(v).join('/')}`).join(', ');
    detail = [body.code, detail || body.detail || body.title].filter(Boolean).join(' ');
  } else if (typeof body === 'string') {
    detail = body.slice(0, 120);
  }
  return `${method} ${route} -> ${res.status}${detail ? ` (${detail})` : ''}`;
}

/** Registro de pasos: cuántos elementos se crearon y los primeros fallos. */
function createReport() {
  const steps = [];
  return {
    steps,
    start(name, total) {
      const step = { name, total, ok: 0, failed: 0, skipped: 0, errors: [] };
      steps.push(step);
      return {
        ok: () => {
          step.ok += 1;
        },
        fail: (label, error) => {
          step.failed += 1;
          if (step.errors.length < 5) step.errors.push(`${label}: ${error instanceof Error ? error.message : String(error)}`);
        },
        skip: (label, reason) => {
          step.skipped += 1;
          if (step.errors.length < 5) step.errors.push(`${label}: omitido (${reason})`);
        },
      };
    },
  };
}

/**
 * Siembra los datos. Devuelve el informe y las credenciales vigentes.
 * @param {{ url: string, user: string, password: string, newPassword?: string, alreadyChanged?: boolean,
 *           log?: (line: string) => void, dataset?: ReturnType<typeof generateDataset> }} options
 */
export async function seed(options) {
  const { url, user, password, alreadyChanged = false, log = () => {} } = options;
  const ds = options.dataset ?? generateDataset();
  const client = createClient(url);
  const report = createReport();
  const map = new Map(); // id del dataset → id de Go

  // 1. Sesión y cambio de contraseña del administrador de arranque.
  const login = await client.expect('POST', '/api/auth/login', { body: { username: user, password }, auth: false });
  client.session.accessToken = login.body.accessToken;
  client.session.refreshToken = login.body.refreshToken;
  let currentPassword = password;
  if (login.body.mustChangePassword && !alreadyChanged) {
    currentPassword = options.newPassword ?? randomPassword();
    const changed = await client.expect('POST', '/api/auth/change-password', { body: { currentPassword: password, newPassword: currentPassword } });
    client.session.accessToken = changed.body.accessToken;
    client.session.refreshToken = changed.body.refreshToken;
    log('contraseña del administrador cambiada');
  }

  const mapped = (id) => map.get(id) ?? '';
  const idemp = (id) => ({ 'Idempotency-Key': `karpo-demo-${id}` });

  /** Recorre `items`; `run` crea cada uno y puede devolver `false` para omitirlo. */
  async function each(name, items, run) {
    const step = report.start(name, items.length);
    for (const item of items) {
      try {
        const outcome = await run(item);
        if (outcome && outcome.skip) step.skip(outcome.label ?? item.id, outcome.skip);
        else step.ok();
      } catch (error) {
        step.fail(item.name ?? item.code ?? item.sku ?? item.id, error);
      }
    }
    const last = report.steps.at(-1);
    log(`${name}: ${last.ok}/${last.total} creados${last.failed ? `, ${last.failed} con error` : ''}${last.skipped ? `, ${last.skipped} omitidos` : ''}`);
  }

  // 2. Partes.
  const orgIndex = ds.organizations.map((o) => o.id);
  await each('organizaciones internas', ds.organizations, async (o) => {
    const res = await client.expect('POST', '/api/organizations', { body: { legalName: o.legalName, tradeName: o.tradeName, roles: [ROLE.internalOrganization] }, headers: idemp(o.id) });
    map.set(o.id, res.body.id);
  });
  const mainOrg = mapped(orgIndex[0]);
  const parties = ds.parties.filter((p) => !orgIndex.includes(p.id));
  await each('partes (unidades, proveedores, transportistas, personas)', parties, async (p) => {
    if (p.kind === 'Person') {
      // Las personas se afilian a su empresa (relación laboral) para poder contratarlas después.
      const employer = mapped(p.organizations[0]);
      const affiliation = employer ? { organization: employer, relationshipType: RELATIONSHIP_EMPLOYMENT } : undefined;
      const res = await client.expect('POST', '/api/persons', { body: { givenName: p.givenName, firstSurname: p.firstSurname, secondSurname: p.secondSurname, affiliation }, headers: idemp(p.id) });
      map.set(p.id, res.body.id);
      return undefined;
    }
    const body = { legalName: p.name, roles: p.roles };
    if (p.roles.includes(ROLE.department)) {
      if (!mainOrg) return { skip: 'sin organización interna', label: p.name };
      body.affiliation = { organization: mainOrg, relationshipType: RELATIONSHIP_ROLLUP };
    }
    const res = await client.expect('POST', '/api/organizations', { body, headers: idemp(p.id) });
    map.set(p.id, res.body.id);
    return undefined;
  });

  // 3. Instalaciones, centros de trabajo y festivos.
  const typeCatalog = (await client.request('GET', '/api/catalogs/facility-types')).body;
  const typeIds = new Map((Array.isArray(typeCatalog) ? typeCatalog : []).map((t) => [t.Name ?? t.name, t.ID ?? t.id]));
  await each('instalaciones', ds.facilities, async (f) => {
    if (f.partOf && !map.has(f.partOf)) return { skip: 'el padre no se creó', label: f.name };
    const type = typeIds.get(f.typeName);
    if (!type) return { skip: `tipo ${f.typeName} sin catálogo`, label: f.name };
    const location = { ...f.location };
    // Go resuelve la delimitación a partir del código postal y el país.
    delete location.geoBoundary;
    delete location.geoPostalCode;
    const res = await client.expect('POST', '/api/v1/facilities', {
      body: { organization: mapped(f.organization), type, name: f.name, partOf: mapped(f.partOf), areaM2: f.areaM2, location: { ...location, country: location.country ?? 'ES' } },
    });
    map.set(f.id, res.body.id);
    return undefined;
  });
  await each('centros de trabajo', ds.workCenters, async (w) => {
    if (!map.has(w.facility)) return { skip: 'la instalación no se creó', label: w.code };
    const res = await client.expect('POST', '/api/v1/work-centers', { body: { employer: mapped(w.employer), facility: mapped(w.facility), code: w.code, headquarters: w.headquarters, opened: w.opened } });
    map.set(w.id, res.body.id);
    if (w.closed) await client.expect('DELETE', `/api/v1/work-centers/${res.body.id}?on=${w.closed}`);
    return undefined;
  });

  // Delimitaciones por código: país, NUTS y LAU/INE.
  const goCountry = new Map();
  const goByNuts = new Map();
  const goByLau = new Map();
  async function boundaries(query) {
    const res = await client.request('GET', `/api/geography/boundaries?${query}&size=1000`);
    return res.status === 200 ? res.body.items : [];
  }
  for (const b of await boundaries('level=2&country=ES')) goByNuts.set(b.nutsCode, b.id);
  for (const b of await boundaries('level=3&country=ES')) goByNuts.set(b.nutsCode, b.id);
  async function resolveBoundary(b) {
    if (b.level === 'country') {
      if (!goCountry.has(b.geoCode)) {
        const res = await client.request('GET', `/api/v1/countries/${b.geoCode}`);
        goCountry.set(b.geoCode, res.status === 200 ? res.body.boundary : '');
      }
      return goCountry.get(b.geoCode);
    }
    if (b.nutsCode) return goByNuts.get(b.nutsCode) ?? '';
    if (b.lauCode) {
      if (!goByLau.has(b.lauCode)) {
        const found = (await boundaries(`level=local&country=ES&q=${encodeURIComponent(b.name)}`)).find((x) => x.lauCode === b.lauCode);
        goByLau.set(b.lauCode, found?.id ?? '');
      }
      return goByLau.get(b.lauCode);
    }
    return '';
  }
  const dsBoundary = new Map(ds.boundaries.map((b) => [b.id, b]));
  await each('festivos', ds.holidays, async (h) => {
    const target = dsBoundary.get(h.boundary);
    const boundary = target ? await resolveBoundary(target) : '';
    if (!boundary) return { skip: `delimitación ${target?.nutsCode ?? target?.lauCode ?? target?.geoCode ?? '?'} sin resolver`, label: `${h.date} ${h.name}` };
    await client.expect('POST', '/api/v1/holidays', { body: { boundary, date: h.date, name: h.name } });
    return undefined;
  });

  // 4. Productos, activos, proveedores y transportistas.
  // En Go la categoría de un producto es una entidad (UUID): se crean las que usa el dataset.
  const categories = new Map();
  async function categoryId(company, name) {
    const key = `${company}|${name}`;
    if (!categories.has(key)) {
      const code = name.normalize('NFD').replace(/[^A-Za-z]/g, '').slice(0, 12).toUpperCase();
      const res = await client.request('POST', '/api/products/categories', { body: { company, code, name } });
      categories.set(key, res.status === 201 || res.status === 200 ? res.body.id : '');
    }
    return categories.get(key);
  }
  await each('productos', ds.products, async (p) => {
    const { discontinued, company, category, ...rest } = p;
    delete rest.id;
    delete rest.version;
    const goCompany = mapped(company);
    const res = await client.expect('POST', '/api/v1/products', { body: { ...rest, company: goCompany, category: category ? await categoryId(goCompany, category) : '' } });
    map.set(p.id, res.body.id);
    if (discontinued) await client.expect('DELETE', `/api/v1/products/${res.body.id}`);
    return undefined;
  });
  await each('activos', ds.assets, async (a) => {
    const body = {
      company: mapped(a.company), code: a.code, name: a.name, class: a.class, serial: a.serial, location: mapped(a.location),
      supplier: mapped(a.supplier), document: a.document, acquired: a.acquired, inService: a.inService, cost: a.cost,
      residual: a.residual, lifeMonths: a.lifeMonths,
    };
    const res = await client.expect('POST', '/api/v1/assets', { body });
    map.set(a.id, res.body.id);
    if (a.status === 'disposed') {
      await client.expect('PUT', `/api/v1/assets/${res.body.id}`, { body: { disposed: a.disposed, disposal: a.disposal, proceeds: a.proceeds, version: res.body.version } });
    }
    return undefined;
  });

  /** Reproduce el historial de bloqueos de un perfil, del más antiguo al más reciente. */
  async function replay(base, id, events) {
    for (const event of events) {
      const current = (await client.expect('GET', `${base}/${id}`)).body;
      await client.expect('POST', `${base}/${id}/${event.blocked ? 'block' : 'unblock'}`, { body: { version: current.version, reason: event.reason } });
    }
  }
  await each('proveedores', ds.suppliers, async (s) => {
    if (!map.has(s.supplier)) return { skip: 'la parte no se creó', label: s.supplier };
    const res = await client.expect('POST', '/api/v1/suppliers', { body: { company: mapped(s.company), supplier: mapped(s.supplier), category: s.category, withholdingRate: s.withholdingRate, paymentDays: s.paymentDays, iban: s.iban } });
    map.set(s.id, res.body.id);
    await replay('/api/v1/suppliers', res.body.id, ds.supplierBlockEvents.filter((e) => e.profile === s.id).sort((x, y) => x.at.localeCompare(y.at)));
    return undefined;
  });
  await each('transportistas', ds.carriers, async (c) => {
    const res = await client.expect('POST', '/api/v1/carriers', { body: { company: mapped(c.company), code: c.code, name: c.name, party: mapped(c.party), trackingUrl: c.trackingUrl } });
    map.set(c.id, res.body.id);
    await replay('/api/v1/carriers', res.body.id, ds.carrierBlockEvents.filter((e) => e.carrier === c.id).sort((x, y) => x.at.localeCompare(y.at)));
    return undefined;
  });

  // 5. Puestos: tipos de Go por título (o, si no coinciden, uno activo por orden), dependencias y titulares.
  const types = (await client.request('GET', '/api/hr/catalogs/position-types?active=true')).body;
  const activeTypes = (Array.isArray(types) ? types : []).map((t) => ({ id: t.ID ?? t.id, title: t.Title ?? t.title })).sort((a, b) => String(a.title).localeCompare(String(b.title)));
  const titleToType = new Map();
  const typeFor = (title) => {
    if (!titleToType.has(title)) {
      const exact = activeTypes.find((t) => String(t.title).toLowerCase() === title.toLowerCase());
      titleToType.set(title, exact?.id ?? activeTypes[titleToType.size % Math.max(1, activeTypes.length)]?.id ?? '');
    }
    return titleToType.get(title);
  };
  const hired = new Set();
  await each('puestos', ds.positions, async (p) => {
    if (!map.has(p.unit)) return { skip: 'la unidad no se creó', label: p.typeTitle };
    const type = typeFor(p.typeTitle);
    if (!type) return { skip: 'el catálogo de tipos de puesto está vacío', label: p.typeTitle };
    const res = await client.expect('POST', '/api/hr/positions', { body: { unit: mapped(p.unit), type, salaried: p.flags.Salaried, exempt: p.flags.Exempt, fullTime: p.flags.FullTime, temporary: p.flags.Temporary } });
    map.set(p.id, res.body.id);
    if (p.reportsTo) {
      if (!map.has(p.reportsTo)) throw new SeedError('el puesto superior no se creó');
      await client.expect('POST', `/api/hr/positions/${res.body.id}/reports-to`, { body: { supervisor: mapped(p.reportsTo), primary: true } });
    }
    if (p.holder && map.has(p.holder)) {
      // Para ocupar un puesto hay que ser empleado de su organización: se contrata una vez.
      if (!hired.has(p.holder)) {
        await client.expect('POST', '/api/hr/employments', { body: { person: mapped(p.holder), employer: mapped(p.organization), hired: '2019-01-01' } });
        hired.add(p.holder);
      }
      await client.expect('POST', `/api/hr/positions/${res.body.id}/fill`, { body: { person: mapped(p.holder) } });
    }
    return undefined;
  });

  return { steps: report.steps, credentials: { url: client.root, username: user, password: currentPassword }, counts: Object.fromEntries(report.steps.map((s) => [s.name, s.ok])) };
}

/** Resumen legible del informe. */
export function summarize(result) {
  const rows = result.steps.map((s) => `  ${s.name.padEnd(58)} ${String(s.ok).padStart(3)}/${String(s.total).padEnd(3)} ${s.failed ? `fallos ${s.failed}` : ''}${s.skipped ? ` omitidos ${s.skipped}` : ''}`.trimEnd());
  const issues = result.steps.flatMap((s) => s.errors.map((e) => `  [${s.name}] ${e}`));
  return [`Sembrado:`, ...rows, ...(issues.length ? ['Detalle de los primeros fallos:', ...issues] : [])].join('\n');
}

function parseArgs(argv) {
  const args = {};
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (!arg.startsWith('--')) continue;
    const key = arg.slice(2);
    const next = argv[i + 1];
    if (next === undefined || next.startsWith('--')) args[key] = true;
    else {
      args[key] = next;
      i += 1;
    }
  }
  return args;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const args = parseArgs(process.argv.slice(2));
  const url = args.url ?? process.env.KARPO_URL;
  const user = args.user ?? process.env.KARPO_BOOTSTRAP_ADMIN_USER ?? 'admin';
  const password = args.password ?? process.env.KARPO_BOOTSTRAP_ADMIN_PASSWORD;
  if (!url || !password) {
    console.error('Uso: node scripts/seed-karpo.mjs --url http://127.0.0.1:PUERTO --user admin --password <arranque> [--new-password X] [--already-changed] [--report f.json] [--out cred.json]');
    process.exit(2);
  }
  try {
    const result = await seed({ url, user, password, newPassword: args['new-password'], alreadyChanged: Boolean(args['already-changed']), log: (line) => console.log(line) });
    console.log(summarize(result));
    if (typeof args.report === 'string') {
      mkdirSync(path.dirname(path.resolve(args.report)), { recursive: true });
      writeFileSync(args.report, `${JSON.stringify({ steps: result.steps }, null, 2)}\n`);
    }
    if (typeof args.out === 'string') {
      mkdirSync(path.dirname(path.resolve(args.out)), { recursive: true });
      writeFileSync(args.out, `${JSON.stringify(result.credentials, null, 2)}\n`);
    }
    process.exit(0);
  } catch (error) {
    console.error(`El sembrado se detuvo: ${error instanceof Error ? error.message : String(error)}`);
    process.exit(1);
  }
}
