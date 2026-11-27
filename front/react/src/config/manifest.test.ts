// Carga del manifiesto con superposiciones, modo legado y descubrimiento por rutas.
import { afterEach, describe, expect, it, vi } from 'vitest';
import { readRealById, memoryFetch, realFetch, REAL_PATHS } from './__fixtures__/real-configs';
import {
  loadDiscovered,
  loadDiscoveredWithReport,
  loadLegacy,
  loadLegacyWithReport,
  loadManifest,
  loadManifestWithReport,
  resolveRelative,
} from './manifest';
import { clearConfigDictionaries, resolveText } from './text';
import { ConfigError } from './types';

const BASE = 'http://localhost/cfg/';
const MANIFEST_URL = `${BASE}manifest.json`;

afterEach(() => {
  clearConfigDictionaries();
});

function manifestFiles(extra: Record<string, unknown> = {}, manifestOverrides: Record<string, unknown> = {}) {
  return {
    [MANIFEST_URL]: {
      schemaVersion: 1,
      api: { baseUrl: '/api', apiVersion: 'v1' },
      locales: ['es', 'en'],
      defaultLocale: 'es',
      i18n: { es: 'i18n/es.json', en: 'i18n/en.json' },
      theme: { origin: '/themes/', default: 'modern-flat', available: ['modern-flat', 'classic-1995'] },
      map: {
        sources: {
          'demo-geojson': { type: 'geojson', url: '/geo/{level}.geojson' },
          'demo-pmtiles': { type: 'pmtiles', url: '/geo/karpo-demo.pmtiles', layer: 'boundaries' },
          roto: { type: 'pmtiles', url: '/x.pmtiles' },
          sinUrl: { type: 'geojson' },
        },
      },
      entities: [
        {
          id: 'facility',
          path: 'go/facilities/facility.config.json',
          permissions: { read: 'Facilities.Facility.Read', write: 'Facilities.Facility.Update' },
          overlay: {
            views: [{ kind: 'tree', parentField: 'partOf', labelField: 'name' }],
            form: { sections: [{ id: 'main', fields: [{ field: 'partOf', lookupEntity: 'facility', lookupDisplayField: 'name' }] }] },
            serverSortFields: { typeName: 'type' },
          },
        },
        { id: 'country', path: 'go/geography/country.config.json' },
        { id: 'inline', config: { id: 'ignored', apiEndpoint: '/inline', grid: { columns: [{ field: 'a' }] } } },
      ],
      menu: [
        {
          id: 'ops',
          labelKey: 'menu.operations',
          label: 'Operaciones',
          icon: 'gear',
          children: [{ entity: 'facility' }, { entity: 'desaparecida' }],
        },
        { entity: 'country', visibleWhen: { permission: 'Geography.Reference.Read' } },
        { id: 'vacio', label: 'Vacío', children: [{ entity: 'nope' }] },
        { id: 'inline-node', entity: 'inline', label: { es: 'En línea', en: 'Inline' } },
        'basura',
      ],
      ...manifestOverrides,
    },
    [`${BASE}go/facilities/facility.config.json`]: readRealById('facility'),
    [`${BASE}go/geography/country.config.json`]: readRealById('country'),
    [`${BASE}i18n/es.json`]: { menu: { operations: 'Operaciones (diccionario)' }, 'x.y': 'Equis' },
    [`${BASE}i18n/en.json`]: { menu: { operations: 'Operations (dictionary)' } },
    ...extra,
  };
}

describe('resolveRelative', () => {
  it('resuelve rutas relativas al manifiesto', () => {
    expect(resolveRelative('go/a.json', 'http://h/cfg/manifest.json')).toBe('http://h/cfg/go/a.json');
    expect(resolveRelative('../a.json', 'http://h/cfg/manifest.json')).toBe('http://h/a.json');
    expect(resolveRelative('go/a.json', '/cfg/manifest.json')).toBe('/cfg/go/a.json');
    expect(resolveRelative('go/a.json?x=1', 'cfg/manifest.json')).toBe('/cfg/go/a.json?x=1');
    expect(resolveRelative('/abs/a.json', 'http://h/cfg/manifest.json')).toBe('http://h/abs/a.json');
    expect(resolveRelative('https://otro/a.json', 'http://h/cfg/m.json')).toBe('https://otro/a.json');
    expect(resolveRelative('//otro/a.json', 'http://h/cfg/m.json')).toBe('//otro/a.json');
  });
});

describe('loadManifest', () => {
  it('lee entidades con rutas relativas al manifiesto y aplica la superposición ANTES de normalizar', async () => {
    const calls: string[] = [];
    const manifest = await loadManifest(MANIFEST_URL, { fetch: memoryFetch(manifestFiles(), calls) });
    expect(calls).toContain(`${BASE}go/facilities/facility.config.json`);
    expect(calls).toContain(`${BASE}i18n/es.json`);

    const facility = manifest.entities.facility!;
    // La clave ajena sólo puede ser `lookup` si la fusión ocurrió antes de normalizar.
    const partOf = facility.form!.sections[0]!.fields.find((f) => f.field === 'partOf')!;
    expect(partOf.kind).toBe('lookup');
    expect(partOf.lookup).toMatchObject({ entity: 'facility', displayField: 'name' });
    expect(facility.form!.sections[0]!.fields).toHaveLength(4); // fusión en profundidad, no sustitución
    expect(facility.views.map((v) => v.kind)).toEqual(['grid', 'tree']);
    expect(facility.permissions).toEqual({ read: 'Facilities.Facility.Read', write: 'Facilities.Facility.Update' });
    expect(facility.warnings.filter((w) => w.severity === 'warn')).toEqual([]);

    // El JSON original no se toca (se funde una copia).
    expect(readRealById('facility').views).toBeUndefined();

    expect(manifest.entities.inline).toMatchObject({ id: 'inline', endpoint: '/inline' });
    expect(manifest.entities.country!.editable).toBe(false);
  });

  it('valores por omisión, tema, mapa y API', async () => {
    const manifest = await loadManifest(MANIFEST_URL, { fetch: memoryFetch(manifestFiles()) });
    expect(manifest).toMatchObject({
      schemaVersion: 1,
      locales: ['es', 'en'],
      defaultLocale: 'es',
      api: { baseUrl: '/api', apiVersion: 'v1' },
      theme: { origin: '/themes/', default: 'modern-flat', available: ['modern-flat', 'classic-1995'] },
    });
    expect(Object.keys(manifest.map.sources)).toEqual(['demo-geojson', 'demo-pmtiles']);
    expect(manifest.map.sources['demo-pmtiles']).toEqual({ type: 'pmtiles', url: '/geo/karpo-demo.pmtiles', layer: 'boundaries' });

    const minimal = await loadManifest(MANIFEST_URL, {
      fetch: memoryFetch({ [MANIFEST_URL]: { schemaVersion: 1 } }),
    });
    expect(minimal).toMatchObject({
      entities: {},
      menu: [],
      locales: ['es', 'en'],
      defaultLocale: 'es',
      api: { baseUrl: '/api', apiVersion: 'v1' },
      theme: { origin: '/themes/', default: 'modern-flat', available: ['modern-flat'] },
      map: { sources: {} },
    });
  });

  it('el menú usa clave o etiqueta, descarta lo que no existe y filtra por permiso', async () => {
    const { manifest, warnings } = await loadManifestWithReport(MANIFEST_URL, { fetch: memoryFetch(manifestFiles()) });
    expect(manifest.menu.map((n) => n.id)).toEqual(['ops', 'country', 'inline-node']);
    const ops = manifest.menu[0]!;
    expect(ops).toMatchObject({ icon: 'gear', label: { literal: 'Operaciones', key: 'menu.operations' } });
    expect(ops.children).toHaveLength(1);
    expect(ops.children![0]).toMatchObject({ id: 'facility', entity: 'facility', icon: 'building' });
    expect(ops.children![0]!.label.literal).toBe('Instalaciones');
    expect(manifest.menu[1]!.visibleWhen).toEqual({ permission: 'Geography.Reference.Read' });
    expect(manifest.menu[2]!.label.byLang).toEqual({ es: 'En línea', en: 'Inline' });
    const messages = warnings.map((w) => w.message).join('\n');
    expect(messages).toMatch(/desaparecida/);
    expect(messages).toMatch(/grupo de menú vacío|nope/);
    expect(warnings.some((w) => w.path === 'map.sources.roto')).toBe(true);
    expect(warnings.some((w) => w.path === 'map.sources.sinUrl')).toBe(true);
  });

  it('sin `menu`, una entrada por entidad', async () => {
    const files = manifestFiles({}, { menu: undefined });
    const manifest = await loadManifest(MANIFEST_URL, { fetch: memoryFetch(files) });
    expect(manifest.menu.map((n) => n.entity)).toEqual(['facility', 'country', 'inline']);
  });

  it('carga los diccionarios por idioma y `resolveText` los usa tras la clave traducida', async () => {
    await loadManifest(MANIFEST_URL, { fetch: memoryFetch(manifestFiles()) });
    const text = { literal: 'Operaciones', key: 'menu.operations' };
    expect(resolveText(text, 'es', () => undefined)).toBe('Operaciones (diccionario)');
    expect(resolveText(text, 'en', () => undefined)).toBe('Operations (dictionary)');
    expect(resolveText(text, 'en-GB', () => undefined)).toBe('Operations (dictionary)');
    expect(resolveText(text, 'es', () => 'Traducida')).toBe('Traducida');
    expect(resolveText({ literal: 'L', key: 'x.y' }, 'es', () => undefined)).toBe('Equis');
  });

  it('una entidad que falla no tumba el manifiesto: se informa y se omite', async () => {
    const files = manifestFiles();
    delete (files as Record<string, unknown>)[`${BASE}go/geography/country.config.json`];
    (files as Record<string, unknown>)[`${BASE}i18n/en.json`] = undefined;
    const { manifest, warnings } = await loadManifestWithReport(MANIFEST_URL, { fetch: memoryFetch(files) });
    expect(Object.keys(manifest.entities)).toEqual(['facility', 'inline']);
    expect(warnings.some((w) => w.path === 'entities.country' && /HTTP 404/.test(w.message))).toBe(true);
    expect(warnings.some((w) => w.path === 'entities.country' || w.path === 'i18n.en')).toBe(true);
    expect(manifest.menu.some((n) => n.id === 'country')).toBe(false);
  });

  it('rechaza un manifiesto que no lo es', async () => {
    await expect(loadManifest(MANIFEST_URL, { fetch: memoryFetch({ [MANIFEST_URL]: [] }) })).rejects.toThrow(/objeto JSON/);
    await expect(
      loadManifest(MANIFEST_URL, { fetch: memoryFetch({ [MANIFEST_URL]: { schemaVersion: 2 } }) }),
    ).rejects.toThrow(/schemaVersion/);
    await expect(loadManifest(MANIFEST_URL, { fetch: memoryFetch({}) })).rejects.toBeInstanceOf(ConfigError);
    const html = (async () => new Response('<html>', { status: 200 })) as unknown as typeof fetch;
    await expect(loadManifest(MANIFEST_URL, { fetch: html })).rejects.toThrow(/JSON válido/);
    const down = (async () => {
      throw new TypeError('red caída');
    }) as unknown as typeof fetch;
    await expect(loadManifest(MANIFEST_URL, { fetch: down })).rejects.toThrow(/red caída/);
  });

  it('propaga el cancelado y acepta una señal directamente', async () => {
    const aborted = (async () => {
      throw new DOMException('cancelado', 'AbortError');
    }) as unknown as typeof fetch;
    await expect(loadManifest(MANIFEST_URL, { fetch: aborted })).rejects.toThrow(/cancelado/);
    const controller = new AbortController();
    const seen: (AbortSignal | null | undefined)[] = [];
    const spy = (async (_url: unknown, init?: RequestInit) => {
      seen.push(init?.signal);
      return new Response(JSON.stringify({ schemaVersion: 1 }));
    }) as unknown as typeof fetch;
    vi.stubGlobal('fetch', spy);
    try {
      await loadManifest(MANIFEST_URL, controller.signal);
    } finally {
      vi.unstubAllGlobals();
    }
    expect(seen[0]).toBe(controller.signal);
  });

  it('avisa por consola con `logWarnings` y omite entradas ilegibles o repetidas', async () => {
    const group = vi.spyOn(console, 'groupCollapsed').mockImplementation(() => undefined);
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    vi.spyOn(console, 'groupEnd').mockImplementation(() => undefined);
    try {
      const files = manifestFiles(
        {},
        {
          zzz: 1,
          entities: [
            'raro',
            { path: 'sin-id.json' },
            { id: 'facility', path: 'go/facilities/facility.config.json', otraCosa: 1 },
            { id: 'facility', path: 'go/facilities/facility.config.json' },
          ],
          menu: undefined,
        },
      );
      const { manifest, warnings } = await loadManifestWithReport(MANIFEST_URL, {
        fetch: memoryFetch(files),
        logWarnings: true,
      });
      expect(Object.keys(manifest.entities)).toEqual(['facility']);
      expect(warnings.map((w) => w.path)).toEqual(
        expect.arrayContaining(['zzz', 'entities[0]', 'entities[1]', 'entities[2].otraCosa', 'entities[3]']),
      );
      expect(group).toHaveBeenCalled();
      expect(warn).toHaveBeenCalled();
    } finally {
      vi.restoreAllMocks();
    }
  });

  it('avisa (`warn`) de los iconos desconocidos del menú con la ruta del JSON, aunque haya nodos descartados antes', async () => {
    const { warnings } = await loadManifestWithReport(MANIFEST_URL, {
      fetch: memoryFetch(
        manifestFiles(
          {},
          {
            menu: [
              { id: 'roto', label: 'Roto', children: [{ entity: 'no-existe' }] },
              {
                id: 'grupo',
                label: 'Grupo',
                icon: 'no-such-icon',
                children: [
                  { entity: 'facility', icon: 'buildings' },
                  { entity: 'country', icon: 'another-bad-one' },
                ],
              },
            ],
          },
        ),
      ),
    });
    const icons = warnings.filter((w) => /^icono `.+` desconocido/.test(w.message));
    expect(icons.map((w) => w.path)).toEqual(['menu[1].icon', 'menu[1].children[1].icon']);
    expect(icons.every((w) => w.severity === 'warn')).toBe(true);
    expect(icons[0]!.message).toBe('icono `no-such-icon` desconocido: se usa un círculo');
  });

  it('un ítem que hereda el icono de su entidad no se avisa dos veces: lo avisa la entidad', async () => {
    const manifest = await loadManifestWithReport(MANIFEST_URL, {
      fetch: memoryFetch(
        manifestFiles(
          {
            [`${BASE}rara.json`]: { id: 'rara', apiEndpoint: '/rara', icon: 'icono-raro', grid: { columns: [{ field: 'a' }] } },
          },
          { entities: [{ id: 'rara', path: 'rara.json' }], menu: [{ entity: 'rara' }] },
        ),
      ),
    });
    expect(manifest.warnings.filter((w) => /^icono `.+` desconocido/.test(w.message))).toEqual([]);
    expect(manifest.manifest.entities.rara!.warnings.filter((w) => /^icono `.+` desconocido/.test(w.message))).toHaveLength(1);
  });

  it('`overlayPath` carga la superposición de un fichero y `overlay` se funde encima', async () => {
    const files = manifestFiles(
      {
        [`${BASE}overlays/facility.json`]: {
          grid: { columns: [{ field: 'name', headerKey: 'entities.facility.columns.name.header' }] },
          form: { sections: [{ id: 'main', fields: [{ field: 'name', hintKey: 'from.file' }] }] },
        },
      },
      {
        entities: [
          {
            id: 'facility',
            path: 'go/facilities/facility.config.json',
            overlayPath: 'overlays/facility.json',
            overlay: { form: { sections: [{ id: 'main', fields: [{ field: 'name', hintKey: 'from.inline' }] }] } },
          },
        ],
        menu: [{ entity: 'facility' }],
      },
    );
    const calls: string[] = [];
    const { manifest, warnings } = await loadManifestWithReport(MANIFEST_URL, { fetch: memoryFetch(files, calls) });
    expect(calls).toContain(`${BASE}overlays/facility.json`);
    const facility = manifest.entities.facility!;
    expect(facility.columns.find((c) => c.field === 'name')!.label).toEqual({ literal: 'Nombre', key: 'entities.facility.columns.name.header' });
    // El literal de Go sigue ahí y la clave de `overlay` manda sobre la del fichero.
    expect(facility.form!.sections[0]!.fields.find((f) => f.field === 'name')!.hint).toEqual({ literal: '', key: 'from.inline' });
    expect(warnings.filter((w) => /overlayPath|^entities/.test(w.path))).toEqual([]);
  });

  it('una `overlayPath` que no se puede leer o no es un objeto avisa y la entidad se carga sin ella', async () => {
    const files = manifestFiles(
      { [`${BASE}overlays/mala.json`]: ['no', 'es', 'un', 'objeto'] },
      {
        entities: [
          { id: 'facility', path: 'go/facilities/facility.config.json', overlayPath: 'overlays/falta.json' },
          { id: 'country', path: 'go/geography/country.config.json', overlayPath: 'overlays/mala.json' },
        ],
        menu: [],
      },
    );
    const { manifest, warnings } = await loadManifestWithReport(MANIFEST_URL, { fetch: memoryFetch(files) });
    expect(Object.keys(manifest.entities)).toEqual(['facility', 'country']);
    const bad = warnings.filter((w) => /overlayPath/.test(w.path));
    expect(bad.map((w) => w.path)).toEqual(['entities.facility.overlayPath', 'entities.country.overlayPath']);
    expect(bad[0]!.message).toContain('superposición no cargada');
    expect(bad.every((w) => w.severity === 'warn')).toBe(true);
  });
});

describe('loadLegacy (index.json + sidebar-config.json de Angular)', () => {
  const INDEX = 'http://localhost/config/entities/index.json';
  const SIDEBAR = 'http://localhost/sidebar-config.json';
  const entityBase = 'http://localhost/config/entities/';

  function legacyFiles() {
    return {
      [INDEX]: {
        version: '1',
        description: 'x',
        entities: {
          facility: 'facilities/facility.config.json',
          country: 'geography/country.config.json',
          holiday: 'geography/holiday.config.json',
        },
        domains: { geo: ['country'] },
      },
      [SIDEBAR]: {
        contexts: [
          {
            id: 'empresas',
            label: 'Mi empresa',
            icon: 'building',
            items: [
              { id: 'inst', label: 'Instalaciones', entityId: 'facility', icon: 'building', roles: ['admin'], viewType: 'grid' },
              {
                id: 'geo',
                label: 'Geografía',
                type: 'folder',
                children: [
                  { id: 'paises', label: 'Países', entityId: 'country', visibleWhen: 'x == 1' },
                  { id: 'grafana', label: 'Grafana', externalUrl: 'http://grafana', embedSupported: true },
                  { id: 'fantasma', label: 'Fantasma', entityId: 'no-existe' },
                ],
              },
            ],
          },
          { id: 'vacio', label: 'Vacío', items: [] },
          'basura',
        ],
      },
      [`${entityBase}facilities/facility.config.json`]: readRealById('facility'),
      [`${entityBase}geography/country.config.json`]: readRealById('country'),
      [`${entityBase}geography/holiday.config.json`]: readRealById('holiday'),
      [`${entityBase}no-existe.config.json`]: { id: 'no-existe', apiEndpoint: '/ghost' },
    };
  }

  it('compone el registro y el menú desde los dos ficheros', async () => {
    const { manifest, warnings } = await loadLegacyWithReport(INDEX, SIDEBAR, { fetch: memoryFetch(legacyFiles()) });
    expect(Object.keys(manifest.entities).sort()).toEqual(['country', 'facility', 'no-existe']);
    expect(manifest.menu).toHaveLength(1);
    const context = manifest.menu[0]!;
    expect(context).toMatchObject({ id: 'empresas', icon: 'building', label: { literal: 'Mi empresa' } });
    expect(context.children!.map((n) => n.id)).toEqual(['inst', 'geo']);
    expect(context.children![1]!.children!.map((n) => n.entity)).toEqual(['country', 'no-existe']);
    expect(context.children![1]!.children![0]!.visibleWhen).toEqual({ expr: 'x == 1' });
    expect(context.children![0]!.visibleWhen).toBeUndefined(); // `roles` legado descartado
    expect(warnings.some((w) => /roles/.test(w.message))).toBe(true);
    expect(warnings.some((w) => /externalUrl/.test(w.message))).toBe(true);
    expect(warnings.some((w) => /no-existe.*no está en el índice/.test(w.message))).toBe(true);
  });

  it('`all: true` carga todo el índice; `loadLegacy` devuelve el manifiesto', async () => {
    const manifest = await loadLegacy(INDEX, SIDEBAR, { fetch: memoryFetch(legacyFiles()), all: true, manifest: { locales: ['es'] } });
    expect(Object.keys(manifest.entities).sort()).toEqual(['country', 'facility', 'holiday', 'no-existe']);
    expect(manifest.locales).toEqual(['es']);
  });

  it('sin menú utilizable, una entrada por entidad; entradas ilegibles fallan con claridad', async () => {
    const files = legacyFiles();
    (files as Record<string, unknown>)[SIDEBAR] = { contexts: [] };
    const manifest = await loadLegacy(INDEX, SIDEBAR, { fetch: memoryFetch(files), all: true });
    expect(manifest.menu.map((n) => n.entity)).toEqual(['facility', 'country', 'holiday']);
    await expect(loadLegacy(INDEX, SIDEBAR, { fetch: memoryFetch({ ...files, [INDEX]: [] }) })).rejects.toThrow(/índice/);
    await expect(loadLegacy(INDEX, SIDEBAR, { fetch: memoryFetch({ ...files, [SIDEBAR]: 3 }) })).rejects.toThrow(/menú/);
  });
});

describe('loadDiscovered (descubrimiento por lista de rutas)', () => {
  it('carga las 8 configuraciones de Go sin índice y agrupa el menú por carpeta', async () => {
    const base = 'http://localhost/config/entities/';
    const { manifest, warnings } = await loadDiscoveredWithReport(base, [...REAL_PATHS], {
      fetch: realFetch(base),
      overlays: { facility: { views: [{ kind: 'tree', parentField: 'partOf', labelField: 'name' }] } },
    });
    expect(Object.keys(manifest.entities).sort()).toEqual([
      'asset', 'carrier', 'country', 'facility', 'holiday', 'product', 'supplier', 'work-center',
    ]);
    expect(manifest.entities.facility!.views.map((v) => v.kind)).toEqual(['grid', 'tree']);
    expect(manifest.menu.map((n) => n.id)).toEqual(['assets', 'facilities', 'geography', 'hr', 'products', 'purchases', 'shipments']);
    const geography = manifest.menu.find((n) => n.id === 'geography')!;
    expect(geography.label).toEqual({ literal: 'Geography', key: 'menu.geography' });
    expect(geography.children!.map((n) => n.entity)).toEqual(['country', 'holiday']);
    expect(warnings).toEqual([]);
  });

  it('las rutas sin carpeta cuelgan de la raíz del menú; los fallos se informan', async () => {
    const base = 'http://localhost/c/';
    const manifest = await loadDiscovered(base, ['loose.config.json', 'dir/missing.config.json'], {
      fetch: memoryFetch({ [`${base}loose.config.json`]: { id: 'loose', apiEndpoint: '/loose', icon: 'box' } }),
      manifest: { defaultLocale: 'en', locales: ['en', 'es'] },
    });
    expect(manifest.menu).toEqual([{ id: 'loose', label: { literal: 'Loose' }, entity: 'loose', icon: 'box' }]);
    expect(manifest.defaultLocale).toBe('en');
  });
});
