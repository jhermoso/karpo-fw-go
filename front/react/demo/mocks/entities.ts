// Las 8 entidades del contrato de mantenimiento de Go (facilities, countries, holidays, work-centers,
// products, assets, carriers, suppliers) sobre el motor de maintenance-core.ts. Las reglas y los
// códigos de error siguen a Go (contexts/*/maintenance.go) en lo que A2 documenta; lo demás es
// plausible y está marcado en docs/notas/T4.md.
import { http, type HttpHandler } from 'msw';
import { stableStringify, type DemoParty } from '../data/dataset.mjs';
import {
  approvalForced,
  identify,
  isUuid,
  json,
  preflight,
  problem,
  readObject,
  type FieldIssue,
} from './http';
import {
  Validation,
  fail,
  findRow,
  good,
  isDate,
  isDecimal,
  newId,
  noContentResult,
  str,
  violation,
  type MaintenanceDef,
  type Outcome,
  type RemoveResult,
  type Row,
} from './maintenance-core';
import { mock } from './state';

const data = () => mock.data;
const today = () => mock.data.meta.today;
const sameId = (a: unknown, b: unknown) => String(a ?? '').trim().toLowerCase() === String(b ?? '').trim().toLowerCase();
const present = (value: unknown) => value !== undefined && value !== null && value !== '';
const truthy = (filters: Map<string, string>, key: string) => filters.get(key) === 'true';

function boolOf(value: unknown, fallback: boolean): boolean {
  return typeof value === 'boolean' ? value : fallback;
}

/** Instante determinista de una orden: «ahora» fijo más un minuto por orden dada. */
function orderInstant(): string {
  const base = Date.parse(mock.data.meta.now);
  return new Date(base + (mock.counter + 1) * 60_000).toISOString().replace(/\.\d{3}Z$/, 'Z');
}

function defaultCompany(value: unknown, v: Validation): string {
  if (!present(value)) return data().organizations[0]!.id;
  const id = str(value);
  if (!isUuid(id) || !data().organizations.some((o) => sameId(o.id, id))) {
    v.add('company', 'format', 'company must be an internal organization id');
  }
  return id.toLowerCase();
}

// ---------------------------------------------------------------------------------------------
// Instalaciones
// ---------------------------------------------------------------------------------------------

type Location = Record<string, string>;

function resolveLocation(raw: unknown): Location {
  const location: Location = {};
  if (raw && typeof raw === 'object') {
    for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
      if (typeof value === 'string' && value.trim() !== '') location[key] = value.trim();
    }
  }
  // Go resuelve la delimitación a partir del código postal y el país.
  if (location.postalCode && location.country) {
    const municipality = data().municipalities.find((m) => m.postalCode === location.postalCode);
    if (municipality && location.country.toUpperCase() === 'ES') location.geoBoundary = municipality.boundary;
  }
  return location;
}

function isInside(candidate: string, ancestorId: string): boolean {
  let cursor: Row | undefined = findRow(facilities, candidate);
  for (let guard = 0; cursor && guard < 50; guard += 1) {
    if (sameId(cursor.id, ancestorId)) return true;
    cursor = cursor.partOf ? findRow(facilities, String(cursor.partOf)) : undefined;
  }
  return false;
}

function checkParent(partOf: string, organization: unknown, selfId: string | null): Outcome<null> {
  if (partOf === '') return good(null);
  const parent = isUuid(partOf) ? findRow(facilities, partOf) : undefined;
  if (!parent) return violation('facilities.parent_not_found', 'the facility that contains this one does not exist');
  if (!sameId(parent.organization, organization)) {
    return violation('facilities.parent_other_organization', 'the containing facility belongs to another organization');
  }
  if (selfId && isInside(partOf, selfId)) return violation('facilities.cycle', 'a facility cannot be inside itself');
  return good(null);
}

export const facilities: MaintenanceDef = {
  singular: 'facility',
  base: '/api/v1/facilities',
  rows: () => data().facilities as Row[],
  writable: true,
  readPermission: 'Facilities.Facility.Read',
  writePermission: 'Facilities.Facility.Update',
  sortable: ['name'],
  defaultSort: 'name',
  searchFields: ['name'],
  scopeField: 'organization',
  matches: (row, { filters }) =>
    (!filters.get('type') || sameId(row.type, filters.get('type'))) &&
    (!filters.get('organization') || sameId(row.organization, filters.get('organization'))) &&
    (!filters.get('partOf') || sameId(row.partOf, filters.get('partOf'))) &&
    (!truthy(filters, 'isActive') || row.active === true),
  view: (row) => row,
  immutable: { organization: 'id', type: 'id', description: 'text', areaM2: 'decimal' },
  create(body) {
    const v = new Validation();
    const organization = str(body.organization);
    v.require(isUuid(organization) && data().organizations.some((o) => sameId(o.id, organization)), 'organization', 'format', 'organization must be an internal organization id');
    const type = str(body.type);
    const typeRow = data().facilityTypes.find((t) => sameId(t.ID, type));
    v.require(!!typeRow, 'type', 'format', 'type must be a facility type id');
    const name = str(body.name);
    v.require(name !== '', 'name', 'required', 'the name is required');
    v.require(name.length <= 200, 'name', 'maxLength', 'at most 200 characters');
    const areaText = present(body.areaM2) ? String(body.areaM2).trim() : '0';
    v.require(isDecimal(areaText) && Number(areaText) >= 0, 'areaM2', 'format', 'a non-negative number');
    if (body.active === false) v.add('active', 'unsupported', 'a facility is registered active; deactivate it afterwards');
    if (!v.ok) return fail(v.issues);
    const partOf = str(body.partOf);
    const parent = checkParent(partOf, organization, null);
    if (!parent.ok) return parent;
    return good({
      id: newId(),
      organization: organization.toLowerCase(),
      type: typeRow!.ID,
      typeName: typeRow!.Name,
      name,
      description: str(body.description),
      partOf: partOf.toLowerCase(),
      areaM2: areaText,
      location: resolveLocation(body.location),
      active: true,
      version: 1,
    });
  },
  change(current, merged) {
    const v = new Validation();
    const patch: Partial<Row> = {};
    const name = str(merged.name);
    v.require(name !== '', 'name', 'required', 'the name is required');
    v.require(name.length <= 200, 'name', 'maxLength', 'at most 200 characters');
    if (!v.ok) return fail(v.issues);
    if (name !== current.name) patch.name = name;
    const partOf = str(merged.partOf).toLowerCase();
    if (partOf !== String(current.partOf ?? '').toLowerCase()) {
      const moved = checkParent(partOf, current.organization, current.id);
      if (!moved.ok) return moved;
      patch.partOf = partOf;
    }
    if (merged.location && typeof merged.location === 'object') {
      const next = resolveLocation(merged.location);
      if (Object.keys(next).length > 0 && stableStringify(next) !== stableStringify(current.location)) patch.location = next;
    }
    if (typeof merged.active === 'boolean' && merged.active !== current.active) patch.active = merged.active;
    return good(patch);
  },
  remove(current) {
    // Dar de baja es desactivar: no se borra porque otros contextos la citan.
    if (current.active === true) {
      current.active = false;
      current.version = (current.version ?? 1) + 1;
    }
    return noContentResult();
  },
};

// ---------------------------------------------------------------------------------------------
// Países (sólo lectura)
// ---------------------------------------------------------------------------------------------

const BOOLEAN_FILTERS = ['eu', 'eurozone', 'sepa'];

function countryList(row: Row): Row {
  const { id, alpha2, alpha3, name, boundary, callingCode, defaultLocale, eu, eurozone, sepa } = row as Row & Record<string, unknown>;
  return {
    id,
    alpha2,
    alpha3,
    name,
    boundary,
    callingCode,
    ...(defaultLocale ? { defaultLocale } : {}),
    eu,
    eurozone,
    sepa,
    currencies: null,
    languages: null,
    timeZones: null,
  };
}

export const countries: MaintenanceDef = {
  singular: 'country',
  base: '/api/v1/countries',
  rows: () => data().countries as Row[],
  writable: false,
  readPermission: 'Geography.Reference.Read',
  writePermission: 'Geography.Reference.Read',
  sortable: ['alpha2', 'alpha3', 'name'],
  defaultSort: 'alpha2',
  searchFields: ['name', 'alpha2', 'alpha3'],
  idKind: 'alpha2',
  checkFilters(filters) {
    const issues: FieldIssue[] = [];
    for (const key of BOOLEAN_FILTERS) {
      const value = filters.get(key);
      if (value !== undefined && value !== '' && value !== 'true' && value !== 'false') {
        issues.push({ field: key, code: 'format', message: 'true or false' });
      }
    }
    return issues;
  },
  matches: (row, { filters }) =>
    BOOLEAN_FILTERS.every((key) => !filters.get(key) || String(row[key]) === filters.get(key)),
  view: countryList,
  detailView: (row) => row,
  immutable: {},
};

// ---------------------------------------------------------------------------------------------
// Festivos
// ---------------------------------------------------------------------------------------------

export const holidays: MaintenanceDef = {
  singular: 'holiday',
  base: '/api/v1/holidays',
  rows: () => data().holidays as Row[],
  writable: true,
  readPermission: 'Geography.Holiday.Read',
  writePermission: 'Geography.Holiday.Update',
  sortable: ['date', 'name'],
  defaultSort: 'date',
  searchFields: ['name'],
  checkFilters(filters) {
    const year = filters.get('year');
    if (year && !(/^\d+$/.test(year) && Number(year) >= 1900 && Number(year) <= 2200)) {
      return [{ field: 'year', code: 'range', message: 'a year from 1900 to 2200' }];
    }
    return [];
  },
  matches: (row, { filters }) =>
    (!filters.get('boundary') || sameId(row.boundary, filters.get('boundary'))) &&
    (!filters.get('year') || String(row.date).startsWith(`${filters.get('year')}-`)),
  view: (row) => row,
  immutable: { boundary: 'id', date: 'date' },
  create(body) {
    const v = new Validation();
    const boundary = str(body.boundary);
    const target = isUuid(boundary) ? data().boundaries.find((b) => sameId(b.id, boundary)) : undefined;
    v.require(!!target, 'boundary', 'format', 'boundary must be the identity of a geographic boundary');
    v.require(isDate(body.date), 'date', 'format', 'a day as YYYY-MM-DD');
    const name = str(body.name);
    v.require(name !== '', 'name', 'required', 'the name is required');
    v.require(name.length <= 200, 'name', 'maxLength', 'at most 200 characters');
    if (!v.ok) return fail(v.issues);
    if (data().holidays.some((h) => sameId(h.boundary, boundary) && h.date === body.date)) {
      return violation('geography.holiday_exists', 'the boundary already has a holiday that day');
    }
    return good({ id: newId(), boundary: target!.id, boundaryName: target!.name, date: String(body.date), name, version: 1 });
  },
  change(current, merged) {
    const name = str(merged.name);
    const v = new Validation();
    v.require(name !== '', 'name', 'required', 'the name is required');
    v.require(name.length <= 200, 'name', 'maxLength', 'at most 200 characters');
    if (!v.ok) return fail(v.issues);
    return good(name === current.name ? {} : { name });
  },
  remove(current) {
    // El único contrato donde la baja borra de verdad.
    const rows = data().holidays;
    const index = rows.indexOf(current as (typeof rows)[number]);
    if (index >= 0) rows.splice(index, 1);
    return noContentResult();
  },
};

// ---------------------------------------------------------------------------------------------
// Centros de trabajo
// ---------------------------------------------------------------------------------------------

export const workCenters: MaintenanceDef = {
  singular: 'work center',
  base: '/api/v1/work-centers',
  rows: () => data().workCenters as Row[],
  writable: true,
  readPermission: 'HR.WorkCenter.Read',
  writePermission: 'HR.WorkCenter.Update',
  sortable: ['code'],
  defaultSort: 'code',
  searchFields: [],
  scopeField: 'employer',
  matches: (row, { filters }) => {
    const employer = filters.get('employer') || filters.get('company');
    return (!employer || sameId(row.employer, employer)) && (!truthy(filters, 'isActive') || row.active === true);
  },
  view: (row) => row,
  immutable: { employer: 'id', facility: 'id', code: 'upper', opened: 'date' },
  create(body) {
    const v = new Validation();
    const employer = str(body.employer);
    v.require(isUuid(employer) && data().organizations.some((o) => sameId(o.id, employer)), 'employer', 'format', 'employer must be an internal organization id');
    const facility = str(body.facility);
    v.require(isUuid(facility) && !!findRow(facilities, facility), 'facility', 'format', 'facility must be a facility id');
    const code = str(body.code).toUpperCase();
    v.require(code !== '', 'code', 'required', 'the code is required');
    v.require(code.length <= 20, 'code', 'maxLength', 'at most 20 characters');
    v.require(isDate(body.opened), 'opened', 'format', 'a day as YYYY-MM-DD');
    if (!v.ok) return fail(v.issues);
    if (data().workCenters.some((w) => sameId(w.employer, employer) && w.code === code)) {
      return violation('hr.work_center_code_taken', 'the employer already has a work center with that code');
    }
    return good({
      id: newId(),
      employer: employer.toLowerCase(),
      facility: facility.toLowerCase(),
      code,
      headquarters: boolOf(body.headquarters, false),
      opened: String(body.opened),
      closed: '',
      active: true,
      version: 1,
    });
  },
  change(current, merged) {
    const patch: Partial<Row> = {};
    const closedNow = str(current.closed) !== '';
    const closed = str(merged.closed);
    if (closedNow && closed !== current.closed) return fail([{ field: 'closed', code: 'immutable', message: 'a closed work center does not reopen' }]);
    if (!closedNow && closed !== '') {
      if (!isDate(closed)) return fail([{ field: 'closed', code: 'format', message: 'a day as YYYY-MM-DD' }]);
      if (closed < String(current.opened)) return violation('hr.closing_before_opening', 'a work center cannot close before it opens');
      patch.closed = closed;
      patch.active = false;
    }
    if (typeof merged.headquarters === 'boolean' && merged.headquarters !== current.headquarters) {
      if (closedNow || patch.closed) return violation('hr.work_center_closed', 'a closed work center cannot change its headquarters flag');
      patch.headquarters = merged.headquarters;
    }
    return good(patch);
  },
  remove(current, request) {
    // Dar de baja es cerrar: `?on=YYYY-MM-DD` opcional, hoy por omisión; cerrado ya, 204.
    if (str(current.closed) !== '') return noContentResult();
    const on = new URL(request.url).searchParams.get('on') || today();
    if (!isDate(on)) return fail([{ field: 'on', code: 'format', message: 'a day as YYYY-MM-DD' }]);
    if (on < String(current.opened)) return violation('hr.closing_before_opening', 'a work center cannot close before it opens');
    current.closed = on;
    current.active = false;
    current.version = (current.version ?? 1) + 1;
    return noContentResult();
  },
};

// ---------------------------------------------------------------------------------------------
// Productos
// ---------------------------------------------------------------------------------------------

const PRODUCT_KINDS = ['good', 'service'];
const TRACKING = ['none', 'lot', 'serial'];
const FULFILMENT = ['delivery_note', 'direct_invoice'];

function productFields(source: Record<string, unknown>, v: Validation) {
  const name = str(source.name);
  v.require(name !== '', 'name', 'required', 'the name is required');
  v.require(name.length <= 200, 'name', 'maxLength', 'at most 200 characters');
  const kind = str(source.kind);
  v.require(PRODUCT_KINDS.includes(kind), 'kind', 'enum', PRODUCT_KINDS.join(', '));
  const uom = str(source.uom);
  v.require(uom !== '', 'uom', 'required', 'the unit of measure is required');
  v.require(uom.length <= 10, 'uom', 'maxLength', 'at most 10 characters');
  for (const field of ['basePrice', 'standardCost']) {
    const value = present(source[field]) ? String(source[field]).trim() : '0';
    v.require(isDecimal(value) && Number(value) >= 0, field, 'format', 'a non-negative amount');
  }
  const tracking = present(source.tracking) ? str(source.tracking) : 'none';
  v.require(TRACKING.includes(tracking), 'tracking', 'enum', TRACKING.join(', '));
  const mode = str(source.serviceFulfilmentMode);
  v.require(mode === '' || FULFILMENT.includes(mode), 'serviceFulfilmentMode', 'enum', FULFILMENT.join(', '));
  return { name, kind, uom, tracking, mode };
}

const amount = (value: unknown, fallback = '0.00') => (present(value) ? String(value).trim() : fallback);

export const products: MaintenanceDef = {
  singular: 'product',
  base: '/api/v1/products',
  rows: () => data().products as Row[],
  writable: true,
  readPermission: 'Products.Product.Read',
  writePermission: 'Products.Product.Update',
  sortable: ['sku', 'name'],
  defaultSort: 'sku',
  searchFields: ['sku', 'name'],
  scopeField: 'company',
  matches: (row, { filters }) =>
    (!filters.get('company') || sameId(row.company, filters.get('company'))) &&
    (!filters.get('kind') || row.kind === filters.get('kind')) &&
    (!filters.get('category') || String(row.category).toLowerCase() === filters.get('category')!.toLowerCase()),
  view: (row) => row,
  immutable: { company: 'id', sku: 'text' },
  create(body) {
    const v = new Validation();
    const company = defaultCompany(body.company, v);
    const sku = str(body.sku);
    v.require(sku !== '', 'sku', 'required', 'the SKU is required');
    v.require(sku.length <= 40, 'sku', 'maxLength', 'at most 40 characters');
    const f = productFields(body, v);
    if (!v.ok) return fail(v.issues);
    if (data().products.some((p) => sameId(p.company, company) && String(p.sku).toLowerCase() === sku.toLowerCase())) {
      return violation('products.sku_taken', 'the company already has a product with that SKU');
    }
    return good({
      id: newId(),
      company,
      sku,
      name: f.name,
      description: str(body.description),
      kind: f.kind,
      uom: f.uom,
      category: str(body.category),
      taxCode: str(body.taxCode),
      expenseCategory: str(body.expenseCategory),
      basePrice: amount(body.basePrice),
      standardCost: amount(body.standardCost),
      forSale: boolOf(body.forSale, true),
      forPurchase: boolOf(body.forPurchase, false),
      stocked: boolOf(body.stocked, f.kind === 'good'),
      tracking: f.tracking,
      blockedSales: boolOf(body.blockedSales, false),
      blockedPurchase: boolOf(body.blockedPurchase, false),
      serviceFulfilmentMode: f.kind === 'service' ? f.mode : '',
      discontinued: '',
      version: 1,
    });
  },
  change(current, merged) {
    const v = new Validation();
    const f = productFields(merged, v);
    if (!v.ok) return fail(v.issues);
    const next: Record<string, unknown> = {
      name: f.name,
      description: str(merged.description),
      kind: f.kind,
      uom: f.uom,
      category: str(merged.category),
      taxCode: str(merged.taxCode),
      expenseCategory: str(merged.expenseCategory),
      basePrice: amount(merged.basePrice),
      standardCost: amount(merged.standardCost),
      forSale: boolOf(merged.forSale, current.forSale === true),
      forPurchase: boolOf(merged.forPurchase, current.forPurchase === true),
      stocked: boolOf(merged.stocked, current.stocked === true),
      tracking: f.tracking,
      blockedSales: boolOf(merged.blockedSales, current.blockedSales === true),
      blockedPurchase: boolOf(merged.blockedPurchase, current.blockedPurchase === true),
      serviceFulfilmentMode: f.kind === 'service' ? f.mode : '',
    };
    const patch: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(next)) {
      const same = key === 'basePrice' || key === 'standardCost' ? Number(value) === Number(current[key]) : value === current[key];
      if (!same) patch[key] = value;
    }
    return good(patch as Partial<Row>);
  },
  remove(current) {
    // Dar de baja es retirar desde hoy; retirado ya, 204 sin versión nueva.
    if (str(current.discontinued) === '') {
      current.discontinued = today();
      current.version = (current.version ?? 1) + 1;
    }
    return noContentResult();
  },
};

// ---------------------------------------------------------------------------------------------
// Activos
// ---------------------------------------------------------------------------------------------

const ASSET_CLASSES = ['land', 'buildings', 'machinery', 'tooling', 'furniture', 'computers', 'vehicles', 'software', 'other'];
const DISPOSALS = ['sale', 'scrap'];

const toCents = (value: unknown) => Math.round(Number(value ?? 0) * 100);
const fromCents = (n: number) => `${n < 0 ? '-' : ''}${Math.floor(Math.abs(n) / 100)}.${String(Math.abs(n) % 100).padStart(2, '0')}`;

function monthsBetween(from: string, to: string): number {
  const [fy, fm, fd] = from.split('-').map(Number) as [number, number, number];
  const [ty, tm, td] = to.split('-').map(Number) as [number, number, number];
  return Math.max(0, (ty - fy) * 12 + (tm - fm) - (td < fd ? 1 : 0));
}

/** Amortización lineal hasta «hoy»: cuota, acumulada y valor neto contable. */
function depreciation(cost: number, residual: number, life: number, inService: string) {
  if (life === 0) return { monthly: 0, accumulated: 0, net: cost, status: 'in-service' as const };
  const monthly = Math.round((cost - residual) / life);
  const elapsed = Math.min(life, monthsBetween(inService, today()));
  const accumulated = Math.min(cost - residual, monthly * elapsed);
  return { monthly, accumulated, net: cost - accumulated, status: elapsed >= life ? ('depreciated' as const) : ('in-service' as const) };
}

export const assets: MaintenanceDef = {
  singular: 'asset',
  base: '/api/v1/assets',
  rows: () => data().assets as Row[],
  writable: true,
  readPermission: 'Assets.Asset.Read',
  writePermission: 'Assets.Asset.Update',
  sortable: ['code', 'name'],
  defaultSort: 'code',
  searchFields: ['code', 'name'],
  scopeField: 'company',
  matches: (row, { filters }) =>
    (!filters.get('company') || sameId(row.company, filters.get('company'))) &&
    (!filters.get('class') || row.class === filters.get('class')) &&
    (!(truthy(filters, 'inService') || truthy(filters, 'isActive')) || row.status !== 'disposed'),
  view: (row) => row,
  immutable: {
    company: 'id',
    code: 'text',
    class: 'text',
    supplier: 'id',
    document: 'text',
    acquired: 'date',
    inService: 'date',
    cost: 'decimal',
    residual: 'decimal',
    lifeMonths: 'int',
  },
  create(body) {
    const v = new Validation();
    const company = defaultCompany(body.company, v);
    const code = str(body.code);
    v.require(code !== '', 'code', 'required', 'the code is required');
    const name = str(body.name);
    v.require(name !== '', 'name', 'required', 'the name is required');
    v.require(name.length <= 200, 'name', 'maxLength', 'at most 200 characters');
    const cls = str(body.class);
    v.require(ASSET_CLASSES.includes(cls), 'class', 'enum', ASSET_CLASSES.join(', '));
    v.require(isDate(body.acquired), 'acquired', 'format', 'a day as YYYY-MM-DD');
    v.require(isDate(body.inService), 'inService', 'format', 'a day as YYYY-MM-DD');
    v.require(isDecimal(body.cost) && Number(body.cost) > 0, 'cost', 'range', 'a positive amount');
    const residualText = amount(body.residual);
    v.require(isDecimal(residualText) && Number(residualText) >= 0 && Number(residualText) <= Number(body.cost), 'residual', 'range', 'between zero and the cost');
    const life = body.lifeMonths === undefined || body.lifeMonths === '' ? 0 : Number(body.lifeMonths);
    v.require(Number.isInteger(life) && life >= 0 && life <= 1200, 'lifeMonths', 'range', 'from 0 to 1200 months');
    if (!v.ok) return fail(v.issues);
    if (String(body.inService) < String(body.acquired)) {
      return fail([{ field: 'inService', code: 'range', message: 'not before the acquisition' }]);
    }
    if (data().assets.some((a) => sameId(a.company, company) && a.code === code)) {
      return violation('assets.code_taken', 'the company already has an asset with that code');
    }
    const cost = toCents(body.cost);
    const residual = toCents(residualText);
    const d = depreciation(cost, residual, life, String(body.inService));
    return good({
      id: newId(),
      company,
      code,
      name,
      class: cls,
      serial: str(body.serial),
      location: str(body.location),
      supplier: str(body.supplier),
      document: str(body.document),
      acquired: String(body.acquired),
      inService: String(body.inService),
      cost: fromCents(cost),
      residual: fromCents(residual),
      lifeMonths: life,
      monthly: fromCents(d.monthly),
      accumulated: fromCents(d.accumulated),
      netBookValue: fromCents(d.net),
      status: d.status,
      disposed: '',
      disposal: '',
      proceeds: '0.00',
      result: '0.00',
      version: 1,
    });
  },
  change(current, merged) {
    const v = new Validation();
    const name = str(merged.name);
    v.require(name !== '', 'name', 'required', 'the name is required');
    v.require(name.length <= 200, 'name', 'maxLength', 'at most 200 characters');
    if (!v.ok) return fail(v.issues);
    const patch: Partial<Row> = {};
    if (name !== current.name) patch.name = name;
    for (const field of ['serial', 'location'] as const) {
      if (str(merged[field]) !== current[field]) patch[field] = str(merged[field]);
    }
    const wasDisposed = str(current.disposed) !== '';
    const disposed = str(merged.disposed);
    if (wasDisposed) {
      const same = disposed === current.disposed && str(merged.disposal) === current.disposal && Number(merged.proceeds ?? 0) === Number(current.proceeds);
      if (!same) return fail([{ field: 'disposed', code: 'immutable', message: 'the disposal does not change once registered' }]);
    } else if (disposed !== '') {
      if (!isDate(disposed)) return fail([{ field: 'disposed', code: 'format', message: 'a day as YYYY-MM-DD' }]);
      if (disposed < String(current.inService)) return violation('assets.disposal_before_service', 'an asset cannot be disposed of before it enters service');
      const disposal = str(merged.disposal);
      if (!DISPOSALS.includes(disposal)) return fail([{ field: 'disposal', code: 'enum', message: DISPOSALS.join(', ') }]);
      const proceedsText = amount(merged.proceeds);
      if (!isDecimal(proceedsText) || Number(proceedsText) < 0) return fail([{ field: 'proceeds', code: 'range', message: 'a non-negative amount' }]);
      const proceeds = disposal === 'sale' ? toCents(proceedsText) : 0;
      patch.disposed = disposed;
      patch.disposal = disposal;
      patch.proceeds = fromCents(proceeds);
      patch.status = 'disposed';
      patch.result = fromCents(proceeds - toCents(current.netBookValue));
    }
    return good(patch);
  },
  // Sin `remove`: los activos no se borran; la baja es el cambio con los datos de baja (405).
};

// ---------------------------------------------------------------------------------------------
// Proveedores y transportistas: bloqueo con motivo, historial y aprobación (D-156)
// ---------------------------------------------------------------------------------------------

interface BlockableSpec {
  /** Prefijo de los códigos de error de negocio: `purchases` o `shipments`. */
  area: 'purchases' | 'shipments';
  singular: string;
  base: string;
  events: () => Row[];
  /** Clave del perfil dentro de cada evento del historial. */
  eventKey: 'profile' | 'carrier';
  readPermission: string;
  writePermission: string;
  buildEvent(profile: Row, blocked: boolean, reason: string, userName: string, userId: string): Row;
}

function blockOutcome(spec: BlockableSpec, request: Request, profile: Row, blocked: boolean, reason: string): Outcome<Row & { pendingApproval?: string }> {
  const label = blocked ? 'block' : 'unblock';
  if (reason.trim() === '') {
    return violation(`${spec.area}.${label}_reason_required`, `a ${label} needs a reason`);
  }
  if ((profile.blocked === true) === blocked) {
    return violation(`${spec.area}.${blocked ? 'already_blocked' : 'not_blocked'}`, blocked ? 'the profile is already blocked' : 'the profile is not blocked');
  }
  if (approvalForced(request)) return good({ ...profile, pendingApproval: newId() });
  const caller = identify(request).user;
  profile.blocked = blocked;
  if (blocked) profile.blockReason = reason.trim();
  else profile.unblockReason = reason.trim();
  profile.version = (profile.version ?? 1) + 1;
  spec.events().push(spec.buildEvent(profile, blocked, reason.trim(), caller?.displayName ?? 'Demo', caller?.userId ?? ''));
  return good(profile as Row & { pendingApproval?: string });
}

function blockHandlers(spec: BlockableSpec, lookupProfile: (id: string) => Row | undefined): HttpHandler[] {
  const route = (blocked: boolean) =>
    http.post(`*${spec.base}/:id/${blocked ? 'block' : 'unblock'}`, async ({ request, params }) => {
      const denied = await preflight(request, spec.writePermission);
      if (denied) return denied;
      const path = new URL(request.url).pathname;
      const id = String(params.id);
      if (!isUuid(id)) {
        return problem({ status: 400, form: 'v1', instance: path, detail: 'One or more fields are invalid', issues: [{ field: 'id', code: 'format', message: 'the identity is not valid' }] });
      }
      const profile = lookupProfile(id);
      if (!profile) return problem({ status: 404, form: 'v1', instance: path });
      const read = await readObject(request);
      if ('error' in read) {
        return problem({ status: read.error === 'large' ? 413 : 400, form: 'v1', instance: path, detail: 'One or more fields are invalid', issues: read.error === 'large' ? [] : [{ field: 'body', code: 'json', message: 'the body is not a JSON object of the expected shape' }] });
      }
      if (read.body.version === undefined || read.body.version === null) {
        return problem({ status: 400, form: 'v1', instance: path, detail: 'One or more fields are invalid', issues: [{ field: 'version', code: 'required', message: 'the version that was read is required' }] });
      }
      if (read.body.version !== profile.version) {
        return problem({ status: 409, form: 'v1', instance: path, detail: `the ${spec.singular} changed after it was read: read it again` });
      }
      const outcome = blockOutcome(spec, request, profile, blocked, typeof read.body.reason === 'string' ? read.body.reason : '');
      if (!outcome.ok) return problem({ status: outcome.status, form: 'v1', instance: path, detail: outcome.detail, code: outcome.code });
      return json(outcome.value, outcome.value.pendingApproval ? 202 : 200);
    });

  const history = http.get(`*${spec.base}/:id/block-history`, async ({ request, params }) => {
    const denied = await preflight(request, spec.readPermission);
    if (denied) return denied;
    const path = new URL(request.url).pathname;
    const id = String(params.id);
    if (!isUuid(id)) {
      return problem({ status: 400, form: 'v1', instance: path, detail: 'One or more fields are invalid', issues: [{ field: 'id', code: 'format', message: 'the identity is not valid' }] });
    }
    const profile = lookupProfile(id);
    if (!profile) return problem({ status: 404, form: 'v1', instance: path });
    const rows = spec.events().filter((e) => sameId(e[spec.eventKey], id));
    rows.sort((a, b) => String(a.at).localeCompare(String(b.at)));
    return json(rows);
  });
  return [route(true), route(false), history];
}

/** DELETE de un perfil bloqueable: bloquea con motivo (cuerpo o `?reason=`); 202 si hay aprobación. */
function blockingRemove(spec: BlockableSpec) {
  return async (current: Row, request: Request): Promise<Outcome<RemoveResult>> => {
    if (current.blocked === true) return noContentResult();
    let reason = new URL(request.url).searchParams.get('reason') ?? '';
    const text = await request.text();
    if (text.trim() !== '') {
      try {
        const parsed = JSON.parse(text) as { reason?: unknown };
        if (typeof parsed.reason === 'string' && parsed.reason.trim() !== '') reason = parsed.reason;
      } catch {
        return fail([{ field: 'body', code: 'json', message: 'the body is not a JSON object of the expected shape' }]);
      }
    }
    const outcome = blockOutcome(spec, request, current, true, reason);
    if (!outcome.ok) return outcome;
    if (outcome.value.pendingApproval) return good<RemoveResult>({ status: 202, body: { pendingApproval: outcome.value.pendingApproval } });
    return noContentResult();
  };
}

/** Los perfiles no llevan `pendingApproval` salvo en la respuesta de una orden pendiente. */
const profileView = (row: Row): Row => row;

const IBAN = /^ES\d{22}$/;

function ibanValid(value: string): boolean {
  const compact = value.replace(/\s+/g, '').toUpperCase();
  if (!IBAN.test(compact)) return false;
  const rearranged = `${compact.slice(4)}${compact.slice(0, 4)}`;
  let remainder = 0;
  for (const ch of rearranged) {
    const digits = ch >= 'A' && ch <= 'Z' ? String(ch.charCodeAt(0) - 55) : ch;
    for (const d of digits) remainder = (remainder * 10 + Number(d)) % 97;
  }
  return remainder === 1;
}

const SUPPLIER_CATEGORIES = ['goods', 'rent', 'repairs', 'professional-services', 'transport', 'insurance', 'advertising', 'supplies', 'other-services', 'fixed-asset'];

const supplierSpec: BlockableSpec = {
  area: 'purchases',
  singular: 'supplier',
  base: '/api/v1/suppliers',
  events: () => data().supplierBlockEvents as Row[],
  eventKey: 'profile',
  readPermission: 'Purchases.Supplier.Read',
  writePermission: 'Purchases.Supplier.Update',
  buildEvent: (profile, blocked, reason, userName, userId) => ({
    id: newId(),
    profile: profile.id,
    company: profile.company,
    supplier: profile.supplier,
    blocked,
    reason,
    userId,
    userName,
    at: orderInstant(),
  }),
};

function supplierFields(source: Record<string, unknown>, v: Validation) {
  const category = str(source.category);
  v.require(SUPPLIER_CATEGORIES.includes(category), 'category', 'enum', SUPPLIER_CATEGORIES.join(', '));
  const rate = amount(source.withholdingRate, '0');
  v.require(isDecimal(rate) && Number(rate) >= 0 && Number(rate) <= 100, 'withholdingRate', 'range', 'from 0 to 100');
  const days = source.paymentDays === undefined || source.paymentDays === '' ? 0 : Number(source.paymentDays);
  v.require(Number.isInteger(days) && days >= 0 && days <= 365, 'paymentDays', 'range', 'from 0 to 365 days');
  const iban = str(source.iban).replace(/\s+/g, '').toUpperCase();
  v.require(iban === '' || ibanValid(iban), 'iban', 'format', 'a valid Spanish IBAN');
  return { category, rate, days, iban };
}

export const suppliers: MaintenanceDef = {
  singular: 'supplier',
  base: supplierSpec.base,
  rows: () => data().suppliers as Row[],
  writable: true,
  readPermission: supplierSpec.readPermission,
  writePermission: supplierSpec.writePermission,
  sortable: ['paymentDays'],
  defaultSort: 'supplier',
  searchFields: [],
  scopeField: 'company',
  matches: (row, { filters }) =>
    (!filters.get('company') || sameId(row.company, filters.get('company'))) &&
    (!filters.get('supplier') || sameId(row.supplier, filters.get('supplier'))) &&
    (!filters.get('category') || row.category === filters.get('category')) &&
    (!truthy(filters, 'isActive') || row.blocked !== true),
  view: profileView,
  immutable: { company: 'id', supplier: 'id' },
  create(body) {
    const v = new Validation();
    const company = defaultCompany(body.company, v);
    const supplier = str(body.supplier);
    const party = isUuid(supplier) ? (data().parties.find((p: DemoParty) => sameId(p.id, supplier) && p.kind === 'Organization')) : undefined;
    v.require(!!party, 'supplier', 'format', 'supplier must be an organization id');
    const f = supplierFields(body, v);
    if (!v.ok) return fail(v.issues);
    if (data().suppliers.some((s) => sameId(s.company, company) && sameId(s.supplier, supplier))) {
      return violation('purchases.supplier_profile_exists', 'the company already has a profile for that supplier');
    }
    return good({
      id: newId(),
      company,
      supplier: supplier.toLowerCase(),
      category: f.category,
      withholdingRate: f.rate,
      paymentDays: f.days,
      iban: f.iban,
      blocked: false,
      blockReason: '',
      unblockReason: '',
      version: 1,
    });
  },
  change(current, merged) {
    if (typeof merged.blocked === 'boolean' && merged.blocked !== current.blocked) {
      return violation('purchases.block_change_requires_command', 'block and unblock a supplier with their own commands');
    }
    const v = new Validation();
    const f = supplierFields(merged, v);
    if (!v.ok) return fail(v.issues);
    const patch: Partial<Row> = {};
    if (f.category !== current.category) patch.category = f.category;
    if (Number(f.rate) !== Number(current.withholdingRate)) patch.withholdingRate = f.rate;
    if (f.days !== current.paymentDays) patch.paymentDays = f.days;
    if (f.iban !== String(current.iban ?? '').replace(/\s+/g, '').toUpperCase()) patch.iban = f.iban;
    return good(patch);
  },
  remove: blockingRemove(supplierSpec),
  extra: () => blockHandlers(supplierSpec, (id) => findRow(suppliers, id)),
};

const carrierSpec: BlockableSpec = {
  area: 'shipments',
  singular: 'carrier',
  base: '/api/v1/carriers',
  events: () => data().carrierBlockEvents as Row[],
  eventKey: 'carrier',
  readPermission: 'Shipments.Carrier.Read',
  writePermission: 'Shipments.Carrier.Update',
  buildEvent: (profile, blocked, reason, userName, userId) => ({
    id: newId(),
    carrier: profile.id,
    company: profile.company,
    blocked,
    reason,
    userId,
    byName: userName,
    at: orderInstant(),
  }),
};

function carrierFields(source: Record<string, unknown>, v: Validation) {
  const name = str(source.name);
  v.require(name !== '', 'name', 'required', 'the name is required');
  v.require(name.length <= 200, 'name', 'maxLength', 'at most 200 characters');
  const trackingUrl = str(source.trackingUrl);
  v.require(trackingUrl === '' || /^https?:\/\//i.test(trackingUrl), 'trackingUrl', 'format', 'an http or https address');
  const party = str(source.party);
  v.require(party === '' || isUuid(party), 'party', 'format', 'party must be an organization id');
  return { name, trackingUrl, party };
}

export const carriers: MaintenanceDef = {
  singular: 'carrier',
  base: carrierSpec.base,
  rows: () => data().carriers as Row[],
  writable: true,
  readPermission: carrierSpec.readPermission,
  writePermission: carrierSpec.writePermission,
  sortable: ['code', 'name'],
  defaultSort: 'code',
  searchFields: ['code', 'name'],
  scopeField: 'company',
  matches: (row, { filters }) =>
    (!filters.get('company') || sameId(row.company, filters.get('company'))) &&
    (!truthy(filters, 'isActive') || row.blocked !== true),
  view: profileView,
  immutable: { company: 'id', code: 'text' },
  create(body) {
    const v = new Validation();
    const company = defaultCompany(body.company, v);
    const code = str(body.code);
    v.require(code !== '', 'code', 'required', 'the code is required');
    v.require(code.length <= 20, 'code', 'maxLength', 'at most 20 characters');
    const f = carrierFields(body, v);
    if (!v.ok) return fail(v.issues);
    if (data().carriers.some((c) => sameId(c.company, company) && c.code === code)) {
      return violation('shipments.carrier_code_taken', 'the company already has a carrier with that code');
    }
    return good({
      id: newId(),
      company,
      code,
      name: f.name,
      party: f.party.toLowerCase(),
      trackingUrl: f.trackingUrl,
      blocked: false,
      blockReason: '',
      unblockReason: '',
      version: 1,
    });
  },
  change(current, merged) {
    if (typeof merged.blocked === 'boolean' && merged.blocked !== current.blocked) {
      return violation('shipments.block_change_requires_command', 'block and unblock a carrier with their own commands');
    }
    const v = new Validation();
    const f = carrierFields(merged, v);
    if (!v.ok) return fail(v.issues);
    const patch: Partial<Row> = {};
    if (f.name !== current.name) patch.name = f.name;
    if (f.trackingUrl !== current.trackingUrl) patch.trackingUrl = f.trackingUrl;
    if (f.party.toLowerCase() !== String(current.party ?? '').toLowerCase()) patch.party = f.party.toLowerCase();
    return good(patch);
  },
  remove: blockingRemove(carrierSpec),
  extra: () => blockHandlers(carrierSpec, (id) => findRow(carriers, id)),
};

/** Las 8 entidades, en el orden de A2 §2. */
export const MAINTENANCE_ENTITIES: MaintenanceDef[] = [countries, holidays, facilities, workCenters, products, assets, carriers, suppliers];

