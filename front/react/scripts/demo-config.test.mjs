// Pruebas de la configuración de la demostración (demo/config): el manifiesto referencia los 8
// configs de Go por ruta sin copiarlos, añade puesto y delimitación, y los textos existen en es y en.
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const configDir = path.join(root, 'demo/config');
const goDir = path.resolve(root, '../angular/config/entities');
const readJson = (file) => JSON.parse(readFileSync(file, 'utf8'));

const manifest = readJson(path.join(configDir, 'manifest.json'));
const es = readJson(path.join(configDir, 'i18n/es.json'));
const en = readJson(path.join(configDir, 'i18n/en.json'));

const GO_PATHS = [
  'go/assets/asset.config.json',
  'go/facilities/facility.config.json',
  'go/geography/country.config.json',
  'go/geography/holiday.config.json',
  'go/hr/work-center.config.json',
  'go/products/product.config.json',
  'go/purchases/supplier.config.json',
  'go/shipments/carrier.config.json',
];

const entity = (id) => manifest.entities.find((e) => e.id === id);

function flatten(object, prefix = '') {
  return Object.entries(object).flatMap(([key, value]) => {
    const full = prefix ? `${prefix}.${key}` : key;
    return value && typeof value === 'object' ? flatten(value, full) : [full];
  });
}

/** Todas las claves de i18n citadas por `…Key` en un JSON de configuración. */
function referencedKeys(value, found = new Set()) {
  if (Array.isArray(value)) value.forEach((item) => referencedKeys(item, found));
  else if (value && typeof value === 'object') {
    for (const [key, child] of Object.entries(value)) {
      if (key !== 'primaryKey' && /Key$/.test(key) && typeof child === 'string') found.add(child);
      else referencedKeys(child, found);
    }
  }
  return found;
}

describe('manifiesto', () => {
  it('referencia los 8 configs de Go por ruta go/<carpeta>/<fichero> y existen, sin copiarlos', () => {
    const paths = manifest.entities.map((e) => e.path).filter((p) => p.startsWith('go/'));
    expect(paths.sort()).toEqual(GO_PATHS);
    for (const rel of paths) expect(existsSync(path.join(goDir, rel.slice(3)))).toBe(true);
    // El manifiesto no lleva copia: no hay carpeta go/ ni ficheros de Go dentro de demo/config.
    expect(existsSync(path.join(configDir, 'go'))).toBe(false);
    const own = readdirSync(path.join(configDir, 'entities')).sort();
    expect(own).toEqual(['boundary.config.json', 'facility-type.config.json', 'party.config.json', 'position.config.json']);
  });

  it('cada id del manifiesto coincide con el id de su config y todas las rutas existen', () => {
    for (const e of manifest.entities) {
      const file = e.path.startsWith('go/') ? path.join(goDir, e.path.slice(3)) : path.join(configDir, e.path);
      expect(readJson(file).id).toBe(e.id);
    }
    expect(new Set(manifest.entities.map((e) => e.id)).size).toBe(manifest.entities.length);
  });

  it('añade puesto y delimitación con el formato de Angular', () => {
    const position = readJson(path.join(configDir, 'entities/position.config.json'));
    expect(position).toMatchObject({ id: 'position', apiEndpoint: '/hr/positions', apiVersioned: false });
    const tree = position.views.find((v) => v.kind === 'tree');
    expect(tree.parentField).toBe('reportsTo');
    expect(tree.source).toMatchObject({ type: 'chart', rootsFrom: 'list', depth: 3, childrenField: 'reports', nodeField: 'position' });
    const boundary = readJson(path.join(configDir, 'entities/boundary.config.json'));
    expect(boundary).toMatchObject({ id: 'boundary', apiEndpoint: '/geography/boundaries', apiVersioned: false });
    for (const config of [position, boundary]) {
      expect(config.grid.columns.length).toBeGreaterThan(0);
      expect(config.availableViews.length).toBeGreaterThan(0);
    }
  });

  it('facility: árbol por partOf y mapa por location.geoBoundary con los 4 niveles y sus códigos', () => {
    const { overlay } = entity('facility');
    const tree = overlay.views.find((v) => v.kind === 'tree');
    expect(tree).toMatchObject({ parentField: 'partOf', labelField: 'name', source: { type: 'flat' } });
    const map = overlay.views.find((v) => v.kind === 'map');
    expect(map.boundaryField).toBe('location.geoBoundary');
    expect(map.levels.map((l) => l.id)).toEqual(['world', 'country', 'region', 'city']);
    expect(map.levels.map((l) => l.defaultCode)).toEqual([null, 'ES', 'ES30', 'ES300']);
    expect(map.levels.map((l) => l.boundaryLevel)).toEqual(['country', '2', '3', 'local']);
    for (const level of map.levels) expect(Object.keys(manifest.map.sources)).toContain(level.source);
  });

  it('lookups de claves ajenas, saveMode batch, orden del servidor, permisos y ayuda', () => {
    const lookupFields = (id, section) => entity(id).overlay[section].sections.flatMap((s) => s.fields).filter((f) => f.type === 'lookup');
    expect(lookupFields('facility', 'form').map((f) => f.field).sort()).toEqual(['partOf', 'type']);
    expect(lookupFields('work-center', 'form').map((f) => f.field).sort()).toEqual(['employer', 'facility']);
    expect(lookupFields('supplier', 'form')[0]).toMatchObject({ field: 'supplier', lookupEntity: 'party' });
    expect(entity('product').overlay.saveMode).toBe('batch');
    // Las listas blancas de orden de A2 §2.
    expect(Object.fromEntries(manifest.entities.filter((e) => e.overlay?.serverSortFields).map((e) => [e.id, e.overlay.serverSortFields]))).toEqual({
      facility: ['name'],
      country: ['alpha2', 'alpha3', 'name'],
      holiday: ['date', 'name'],
      'work-center': ['code'],
      product: ['sku', 'name'],
      asset: ['code', 'name'],
      carrier: ['code', 'name'],
      supplier: ['paymentDays'],
    });
    for (const id of ['facility', 'holiday', 'work-center', 'product', 'asset', 'carrier', 'supplier']) {
      expect(entity(id).permissions).toMatchObject({ read: expect.stringMatching(/\.Read$/), write: expect.stringMatching(/\.Update$/) });
    }
    expect(entity('facility').overlay.help.steps.length).toBeGreaterThan(0);
    expect(entity('product').overlay.help.steps.length).toBeGreaterThan(0);
    // Las entidades de apoyo de las claves ajenas existen.
    for (const target of ['party', 'facility-type', 'boundary', 'facility', 'position']) expect(entity(target)).toBeDefined();
  });

  it('tema, mapas, idiomas y API', () => {
    expect(manifest.theme).toEqual({ origin: '/themes/', default: 'modern-flat', available: ['modern-flat', 'classic-1995', 'glossy-2003', 'souq'] });
    expect(manifest.map.sources['demo-geojson']).toMatchObject({ type: 'geojson', url: '/geo/{level}.geojson' });
    expect(manifest.map.sources['demo-pmtiles']).toMatchObject({ type: 'pmtiles', url: '/geo/karpo-demo.pmtiles' });
    expect(manifest.locales).toEqual(['es', 'en', 'ar']);
    expect(manifest.api).toEqual({ baseUrl: '/api', apiVersion: 'v1' });
  });

  it('el menú agrupa entidades que existen', () => {
    const ids = new Set(manifest.entities.map((e) => e.id));
    const leaves = manifest.menu.flatMap((group) => group.children ?? []);
    expect(manifest.menu.length).toBeGreaterThanOrEqual(3);
    expect(leaves.length).toBeGreaterThanOrEqual(10);
    for (const leaf of leaves) expect(ids.has(leaf.entity)).toBe(true);
    for (const group of manifest.menu) expect(group.labelKey).toBeTruthy();
  });
});

describe('textos', () => {
  it('es y en tienen exactamente las mismas claves, todas con texto', () => {
    expect(flatten(es).sort()).toEqual(flatten(en).sort());
    const empty = (dictionary) => flatten(dictionary).filter((key) => key.split('.').reduce((o, k) => o[k], dictionary).trim() === '');
    expect(empty(es)).toEqual([]);
    expect(empty(en)).toEqual([]);
  });

  it('todas las claves citadas por el manifiesto, las configs nuevas y las superposiciones generadas existen en es y en en', () => {
    const files = readdirSync(path.join(configDir, 'entities')).map((f) => readJson(path.join(configDir, 'entities', f)));
    const overlays = readdirSync(path.join(configDir, 'overlays')).map((f) => readJson(path.join(configDir, 'overlays', f)));
    expect(overlays).toHaveLength(8);
    const keys = referencedKeys([manifest, ...files, ...overlays]);
    for (const dictionary of [es, en]) {
      const known = new Set(flatten(dictionary));
      expect([...keys].filter((key) => !known.has(key))).toEqual([]);
    }
    expect(keys.size).toBeGreaterThan(300);
  });

  it('el manifiesto referencia con `overlayPath` una superposición por entidad de Go y todas existen', () => {
    for (const e of manifest.entities.filter((x) => x.path.startsWith('go/'))) {
      expect(e.overlayPath).toBe(`overlays/${e.id}.i18n.json`);
      expect(existsSync(path.join(configDir, e.overlayPath))).toBe(true);
    }
    expect(manifest.entities.filter((x) => x.overlayPath)).toHaveLength(8);
  });

  it('los textos de menú de las dos lenguas difieren (no se copió el español)', () => {
    const differing = flatten(es).filter((key) => key.split('.').reduce((o, k) => o[k], es) !== key.split('.').reduce((o, k) => o[k], en));
    expect(differing.length).toBeGreaterThan(flatten(es).length * 0.8);
  });
});

describe('ayuda guiada de la demo', () => {
  /** Anclas de ayuda (`core.menu`, `grid.table`…) que aparecen como literal en los fuentes de la biblioteca (no las pruebas). */
  const sourceAnchors = () => {
    const found = new Set();
    const walk = (dir) => {
      for (const entry of readdirSync(dir, { withFileTypes: true })) {
        const file = path.join(dir, entry.name);
        if (entry.isDirectory()) walk(file);
        else if (/\.tsx$/.test(entry.name) && !/\.test\.tsx$/.test(entry.name)) {
          for (const match of readFileSync(file, 'utf8').matchAll(/['"]((?:core|grid|detail|tree|map|help)\.[a-z-]+)['"]/g)) found.add(match[1]);
        }
      }
    };
    walk(path.join(root, 'src'));
    return found;
  };

  it('todos los pasos del manifiesto apuntan a un ancla que existe en los fuentes', () => {
    const anchors = sourceAnchors();
    const targets = [...JSON.stringify(manifest).matchAll(/"target":\s*"([^"]+)"/g)].map((m) => m[1]);
    expect(targets.length).toBeGreaterThan(0);
    for (const target of targets) expect(anchors.has(target), `ancla inexistente: ${target}`).toBe(true);
  });
});
