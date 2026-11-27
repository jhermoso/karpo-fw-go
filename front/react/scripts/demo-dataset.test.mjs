// Pruebas del conjunto de datos determinista de la demostración (demo/data/dataset.mjs).
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { TODAY, generateDataset, mulberry32, stableStringify } from '../demo/data/dataset.mjs';

const sha256 = (value) => createHash('sha256').update(stableStringify(value)).digest('hex');
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

const ds = generateDataset();

describe('determinismo', () => {
  it('dos generaciones dan el mismo SHA-256', () => {
    const a = sha256(generateDataset());
    const b = sha256(generateDataset());
    expect(a).toBe(b);
    expect(a).toMatch(/^[0-9a-f]{64}$/);
  });

  it('el PRNG mulberry32 es reproducible y está en [0, 1)', () => {
    const one = mulberry32(20261127);
    const two = mulberry32(20261127);
    const seq = Array.from({ length: 50 }, () => one());
    expect(seq).toEqual(Array.from({ length: 50 }, () => two()));
    expect(seq.every((v) => v >= 0 && v < 1)).toBe(true);
    expect(new Set(seq).size).toBe(50);
    expect(mulberry32(1)()).not.toBe(mulberry32(2)());
  });

  it('el generador no usa el reloj ni Math.random y «hoy» es el 2026-11-27', () => {
    const source = readFileSync(new URL('../demo/data/dataset.mjs', import.meta.url), 'utf8');
    expect(source).not.toMatch(/Math\.random|Date\.now|new Date\(\)/);
    expect(TODAY).toBe('2026-11-27');
    expect(ds.meta).toMatchObject({ seed: 20261127, today: '2026-11-27' });
  });
});

describe('contenido', () => {
  it('trae las cantidades del encargo', () => {
    expect(ds.organizations).toHaveLength(3);
    expect(ds.facilities).toHaveLength(30);
    expect(ds.workCenters).toHaveLength(12);
    expect(ds.holidays).toHaveLength(40);
    expect(ds.products).toHaveLength(120);
    expect(ds.assets).toHaveLength(60);
    expect(ds.suppliers).toHaveLength(25);
    expect(ds.carriers).toHaveLength(10);
    expect(ds.positions).toHaveLength(45);
    expect(ds.countries).toHaveLength(248);
  });

  it('todo con id UUID único y version', () => {
    const rows = [...ds.facilities, ...ds.workCenters, ...ds.holidays, ...ds.products, ...ds.assets, ...ds.suppliers, ...ds.carriers, ...ds.positions, ...ds.parties, ...ds.boundaries];
    const ids = rows.map((r) => r.id);
    expect(ids.every((id) => UUID.test(id))).toBe(true);
    expect(new Set(ids).size).toBe(ids.length);
    for (const list of [ds.facilities, ds.workCenters, ds.holidays, ds.products, ds.assets, ds.positions]) {
      expect(list.every((r) => r.version === 1)).toBe(true);
    }
    // Los perfiles bloqueables suben de versión con cada bloqueo del historial.
    for (const list of [ds.suppliers, ds.carriers]) expect(list.every((r) => Number.isInteger(r.version) && r.version >= 1)).toBe(true);
    // Las organizaciones internas son también partes (mismo id).
    expect(ds.organizations.every((o) => ds.parties.some((p) => p.id === o.id))).toBe(true);
    // Los países se identifican por el código ISO alfa-2, como en Go.
    expect(ds.countries.every((c) => /^[A-Z]{2}$/.test(c.id) && c.id === c.alpha2)).toBe(true);
  });

  it('las instalaciones son un árbol edificio, planta y sala con dirección española', () => {
    const byId = new Map(ds.facilities.map((f) => [f.id, f]));
    const counts = ds.facilities.reduce((acc, f) => ({ ...acc, [f.typeName]: (acc[f.typeName] ?? 0) + 1 }), {});
    expect(counts).toEqual({ Building: 6, Floor: 10, Room: 14 });
    const rank = { Building: 0, Floor: 1, Room: 2 };
    for (const f of ds.facilities) {
      if (f.typeName === 'Building') expect(f.partOf).toBe('');
      else expect(rank[byId.get(f.partOf).typeName]).toBe(rank[f.typeName] - 1);
      expect(f.location).toMatchObject({ country: 'ES' });
      expect(f.location.postalCode).toMatch(/^\d{5}$/);
      expect(Number(f.areaM2)).toBeGreaterThan(0);
    }
    const municipalities = new Set(ds.municipalities.map((m) => m.boundary));
    expect(ds.facilities.every((f) => municipalities.has(f.location.geoBoundary))).toBe(true);
    expect(new Set(ds.facilities.map((f) => f.organization)).size).toBe(3);
  });

  it('los centros de trabajo cuelgan de una empresa y una instalación; hay cerrados', () => {
    const facilities = new Set(ds.facilities.map((f) => f.id));
    const orgs = new Set(ds.organizations.map((o) => o.id));
    expect(ds.workCenters.every((w) => facilities.has(w.facility) && orgs.has(w.employer) && w.code === w.code.toUpperCase())).toBe(true);
    expect(ds.workCenters.filter((w) => !w.active)).toHaveLength(2);
    expect(ds.workCenters.every((w) => w.active === (w.closed === ''))).toBe(true);
    expect(new Set(ds.workCenters.map((w) => w.code)).size).toBe(12);
  });

  it('los festivos citan delimitaciones que existen y no se repiten', () => {
    const names = new Map(ds.boundaries.map((b) => [b.id, b.name]));
    for (const h of ds.holidays) expect(names.get(h.boundary)).toBe(h.boundaryName);
    expect(new Set(ds.holidays.map((h) => `${h.boundary}|${h.date}`)).size).toBe(40);
    expect(ds.holidays.every((h) => /^20(26|27)-\d{2}-\d{2}$/.test(h.date))).toBe(true);
  });

  it('productos: bienes y servicios, importes como cadena, SKU únicos', () => {
    expect(ds.products.filter((p) => p.kind === 'service')).toHaveLength(30);
    expect(new Set(ds.products.map((p) => p.sku)).size).toBe(120);
    expect(ds.products.every((p) => /^\d+\.\d{2}$/.test(p.basePrice) && /^\d+\.\d{2}$/.test(p.standardCost))).toBe(true);
    expect(ds.products.some((p) => p.discontinued !== '')).toBe(true);
    expect(ds.products.every((p) => p.kind === 'service' || p.serviceFulfilmentMode === '')).toBe(true);
  });

  it('activos: la amortización cuadra con el coste', () => {
    for (const a of ds.assets) {
      const cost = Number(a.cost);
      expect(Number(a.netBookValue)).toBeCloseTo(cost - Number(a.accumulated), 2);
      expect(['in-service', 'depreciated', 'disposed']).toContain(a.status);
      if (a.status === 'disposed') expect(['sale', 'scrap']).toContain(a.disposal);
      else expect(a.disposed).toBe('');
    }
    expect(ds.assets.filter((a) => a.status === 'disposed').length).toBeGreaterThan(0);
    expect(new Set(ds.assets.map((a) => a.code)).size).toBe(60);
  });

  it('proveedores: algunos bloqueados con historial; el IBAN es válido', () => {
    const blocked = ds.suppliers.filter((s) => s.blocked);
    expect(blocked).toHaveLength(4);
    for (const s of blocked) {
      const events = ds.supplierBlockEvents.filter((e) => e.profile === s.id);
      expect(events.length).toBeGreaterThan(0);
      expect(events.at(-1)).toMatchObject({ blocked: true, reason: s.blockReason });
    }
    const iban = (value) => {
      const moved = `${value.slice(4)}${value.slice(0, 4)}`;
      let rest = 0;
      for (const ch of moved) for (const d of /[A-Z]/.test(ch) ? String(ch.charCodeAt(0) - 55) : ch) rest = (rest * 10 + Number(d)) % 97;
      return rest;
    };
    expect(ds.suppliers.every((s) => /^ES\d{22}$/.test(s.iban) && iban(s.iban) === 1)).toBe(true);
    const parties = new Set(ds.parties.map((p) => p.id));
    expect(ds.suppliers.every((s) => parties.has(s.supplier))).toBe(true);
    expect(ds.carriers.filter((c) => c.blocked)).toHaveLength(2);
    expect(ds.carrierBlockEvents).toHaveLength(2);
  });

  it('puestos: un árbol de 5 niveles sin ciclos, con titulares que existen', () => {
    const byId = new Map(ds.positions.map((p) => [p.id, p]));
    const roots = ds.positions.filter((p) => p.reportsTo === '');
    expect(roots).toHaveLength(1);
    const depthOf = (p) => (p.reportsTo === '' ? 1 : 1 + depthOf(byId.get(p.reportsTo)));
    const depths = ds.positions.map(depthOf);
    expect(Math.max(...depths)).toBe(5);
    expect([1, 2, 3, 4, 5].map((d) => depths.filter((x) => x === d).length)).toEqual([1, 4, 10, 14, 16]);
    const parties = new Map(ds.parties.map((p) => [p.id, p]));
    for (const p of ds.positions) {
      expect(p.vacant).toBe(p.holder === '');
      if (p.holder) expect(parties.get(p.holder).kind).toBe('Person');
      expect(p.lines).toHaveLength(p.reportsTo === '' ? 0 : 1);
    }
    expect(ds.positions.filter((p) => p.vacant)).toHaveLength(7);
  });
});

describe('geografía', () => {
  it('248 países, cada uno con su delimitación', () => {
    const boundaries = new Map(ds.boundaries.map((b) => [b.id, b]));
    expect(new Set(ds.countries.map((c) => c.alpha2)).size).toBe(248);
    for (const c of ds.countries) {
      expect(boundaries.get(c.boundary)).toMatchObject({ level: 'country', geoCode: c.alpha2 });
    }
    expect(ds.countries.filter((c) => c.eu)).toHaveLength(27);
    expect(ds.countries.filter((c) => c.eurozone)).toHaveLength(21);
  });

  it('España: 7 + 19 + 59 y la muestra de municipios con nutsCode, lauCode, ineCode y parent', () => {
    const level = (l) => ds.boundaries.filter((b) => b.level === l && b.country === 'ES');
    expect(level('1')).toHaveLength(7);
    expect(level('2')).toHaveLength(19);
    expect(level('3')).toHaveLength(59);
    expect(level('local')).toHaveLength(28);
    const byId = new Map(ds.boundaries.map((b) => [b.id, b]));
    for (const b of level('2')) expect(b.nutsCode).toMatch(/^ES\d{2}$/);
    for (const b of level('3')) {
      expect(b.nutsCode).toMatch(/^ES\d{3}$/);
      expect(byId.get(b.parent).nutsCode).toBe(b.nutsCode.slice(0, 4));
    }
    for (const b of level('local')) {
      expect(b.lauCode).toMatch(/^\d{5}$/);
      expect(b.ineCode).toBe(b.lauCode);
      expect(['ES300', 'ES511']).toContain(byId.get(b.parent).nutsCode);
    }
    expect(ds.boundaries.find((b) => b.nutsCode === 'ES30').name).toBe('Comunidad de Madrid');
    expect(ds.boundaries.find((b) => b.nutsCode === 'ES511').name).toBe('Barcelona');
  });
});
