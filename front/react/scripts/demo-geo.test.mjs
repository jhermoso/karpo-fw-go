// Pruebas de los geodatos esquemáticos (scripts/gen-geo.mjs, demo/public/geo): reproducibles, con
// códigos que casan con el conjunto de datos y con lo que sirve /api/geography/boundaries.
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { generateDataset } from '../demo/data/dataset.mjs';
import { server } from '../demo/mocks/node.ts';
import { GEO_DIR, buildGeo, diffGeo } from './gen-geo.mjs';

const ds = generateDataset();
const read = (name) => JSON.parse(readFileSync(path.join(GEO_DIR, name), 'utf8'));
const codes = (fc) => fc.features.map((f) => f.properties.code).sort();
const index = read('index.json');
const BASE = 'http://karpo.test';

beforeAll(() => server.listen({ onUnhandledFrame: 'error' }));
afterAll(() => server.close());

describe('generación', () => {
  it('npm run geo:build no deja diferencias con lo versionado', () => {
    expect(diffGeo()).toEqual([]);
  });

  it('dos generaciones son idénticas byte a byte', () => {
    const a = buildGeo();
    const b = buildGeo();
    expect([...a.keys()]).toEqual([...b.keys()]);
    for (const [name, content] of a) expect(b.get(name)).toBe(content);
  });

  it('hay 4 niveles: mundo, país, 19 regiones y 2 ciudades, más el índice', () => {
    expect(index.files.map((f) => f.level).reduce((acc, l) => ({ ...acc, [l]: (acc[l] ?? 0) + 1 }), {})).toEqual({ world: 1, country: 1, region: 19, city: 2 });
    expect(index.schematic).toBe(true);
    for (const entry of index.files) expect(read(entry.file).features).toHaveLength(entry.features);
  });

  it('las geometrías son anillos cerrados, antihorarios y dentro de rango', () => {
    for (const entry of index.files) {
      for (const f of read(entry.file).features) {
        expect(f.geometry.type).toBe('Polygon');
        const ring = f.geometry.coordinates[0];
        expect(ring[0]).toEqual(ring.at(-1));
        let area = 0;
        for (let i = 0; i < ring.length - 1; i += 1) area += ring[i][0] * ring[i + 1][1] - ring[i + 1][0] * ring[i][1];
        expect(area).toBeGreaterThan(0);
        for (const [x, y] of ring) {
          expect(Math.abs(x)).toBeLessThanOrEqual(180);
          expect(Math.abs(y)).toBeLessThanOrEqual(90);
        }
      }
    }
  });
});

describe('nombres cortos para los rótulos (M-3)', () => {
  it('npm run tiles:build y geo:build reproducen los ficheros de demo/public/geo sin descargar nada', () => {
    // El generador sólo lee los datos escritos a mano de demo/data; no hay red ni ficheros de terceros.
    expect(diffGeo()).toEqual([]);
  });

  it('las delimitaciones con nombre largo traen shortName, siempre más corto que el nombre', () => {
    const all = index.files.flatMap((entry) => read(entry.file).features);
    const withShort = all.filter((f) => f.properties.shortName);
    expect(withShort.length).toBeGreaterThanOrEqual(25);
    for (const f of withShort) {
      expect(f.properties.shortName.length).toBeLessThan(f.properties.name.length);
      expect(f.properties.shortName.length).toBeLessThanOrEqual(14);
    }
    const navarra = read('country-ES.geojson').features.find((f) => f.properties.code === 'ES22');
    expect(navarra.properties).toMatchObject({ name: 'Comunidad Foral de Navarra', shortName: 'Navarra' });
    // Las comunidades con nombre corto de por sí no llevan alias.
    expect(read('country-ES.geojson').features.find((f) => f.properties.code === 'ES61').properties.shortName).toBeUndefined();
  });

  it('los nombres largos de las comunidades y municipios de la muestra tienen alias', () => {
    const long = (fc) => fc.features.filter((f) => f.properties.name.length > 14 && !f.properties.shortName).map((f) => f.properties.name);
    expect(long(read('country-ES.geojson'))).toEqual([]);
    expect(long(read('city-ES300.geojson'))).toEqual([]);
    expect(long(read('city-ES511.geojson'))).toEqual([]);
  });
});

describe('casan con el conjunto de datos', () => {
  it('mundo: un elemento por país del conjunto y viceversa', () => {
    const world = read('world.geojson');
    expect(codes(world)).toEqual(ds.countries.map((c) => c.alpha2).sort());
    for (const f of world.features) {
      const country = ds.countries.find((c) => c.alpha2 === f.properties.code);
      expect(f.properties).toMatchObject({ name: country.name, level: 'country', alpha2: country.alpha2, alpha3: country.alpha3, parentCode: '' });
    }
    expect(world.features.filter((f) => f.properties.hasChildren).map((f) => f.properties.code)).toEqual(['ES']);
  });

  it('España: las comunidades (NUTS 2) del conjunto, con su nombre', () => {
    const fc = read('country-ES.geojson');
    const expected = ds.boundaries.filter((b) => b.level === '2');
    expect(codes(fc)).toEqual(expected.map((b) => b.nutsCode).sort());
    for (const f of fc.features) {
      expect(f.properties).toMatchObject({ level: '2', parentCode: 'ES', hasChildren: true });
      expect(f.properties.name).toBe(expected.find((b) => b.nutsCode === f.properties.code).name);
    }
  });

  it('cada comunidad tiene su fichero de provincias con los NUTS 3 del conjunto', () => {
    const byId = new Map(ds.boundaries.map((b) => [b.id, b]));
    for (const region of ds.boundaries.filter((b) => b.level === '2')) {
      const fc = read(`region-${region.nutsCode}.geojson`);
      const children = ds.boundaries.filter((b) => b.level === '3' && byId.get(b.parent).nutsCode === region.nutsCode);
      expect(codes(fc)).toEqual(children.map((b) => b.nutsCode).sort());
      expect(fc.features.every((f) => f.properties.parentCode === region.nutsCode && f.properties.level === '3')).toBe(true);
    }
  });

  it('los municipios de la muestra tienen elemento y sólo hay ficheros de Madrid y Barcelona', () => {
    const byId = new Map(ds.boundaries.map((b) => [b.id, b]));
    const cityFiles = index.files.filter((f) => f.level === 'city').map((f) => f.code);
    expect(cityFiles.sort()).toEqual(['ES300', 'ES511']);
    for (const nuts3 of cityFiles) {
      const fc = read(`city-${nuts3}.geojson`);
      const children = ds.boundaries.filter((b) => b.level === 'local' && byId.get(b.parent).nutsCode === nuts3);
      expect(codes(fc)).toEqual(children.map((b) => b.lauCode).sort());
      for (const f of fc.features) expect(f.properties).toMatchObject({ level: 'local', parentCode: nuts3, lauCode: f.properties.code, ineCode: f.properties.code });
    }
    const flagged = ds.boundaries.filter((b) => b.level === '3').filter((b) => read(`region-${byId.get(b.parent).nutsCode}.geojson`).features.find((f) => f.properties.code === b.nutsCode).properties.hasChildren);
    expect(flagged.map((b) => b.nutsCode).sort()).toEqual(['ES300', 'ES511']);
  });
});

describe('casan con /api/geography/boundaries', () => {
  async function children(query) {
    const res = await fetch(`${BASE}/api/geography/boundaries?${query}&size=1000`);
    expect(res.status).toBe(200);
    return (await res.json()).items;
  }

  it('los países que lista la API son los del mundo', async () => {
    const items = await children('level=country');
    expect(items.map((b) => b.geoCode).sort()).toEqual(codes(read('world.geojson')));
  });

  it('las hijas de España por nivel 2, de cada comunidad por parent y de cada provincia con municipios', async () => {
    const level2 = await children('level=2&country=ES');
    expect(level2.map((b) => b.nutsCode).sort()).toEqual(codes(read('country-ES.geojson')));
    for (const region of level2) {
      const provinces = await children(`parent=${region.id}`);
      expect(provinces.map((b) => b.nutsCode).sort()).toEqual(codes(read(`region-${region.nutsCode}.geojson`)));
      for (const province of provinces) {
        const munis = await children(`parent=${province.id}`);
        if (['ES300', 'ES511'].includes(province.nutsCode)) {
          expect(munis.map((b) => b.lauCode).sort()).toEqual(codes(read(`city-${province.nutsCode}.geojson`)));
        } else {
          expect(munis).toEqual([]);
        }
      }
    }
  });
});
