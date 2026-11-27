// Carga del manifiesto (PLAN §4.3.1), del modo legado (index.json + sidebar-config.json de Angular)
// y del descubrimiento por lista de rutas. Las superposiciones se funden ANTES de normalizar.
import type {
  AppManifest,
  ConfigWarning,
  EntityConfig,
  MapSourceDef,
  MenuNode,
  Text,
} from '../contracts';
import type * as CompatModule from './compat';
import { menuIconWarnings, rememberMenuOrigin } from './icons';
import { mergeOverlay } from './merge';
import type * as NormalizeModule from './normalize';
import { humanize, registerConfigDictionary, toText } from './text';
import { ConfigError } from './types';

type Obj = Record<string, unknown>;

function isObj(value: unknown): value is Obj {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

// El normalizador y la tabla de compatibilidad (con su validación por esquema) pesan ~60 kB sin comprimir y
// sólo hacen falta al cargar la configuración: se piden con `import()` y quedan fuera del trozo inicial
// (presupuesto de bytes, D-R29). Cada cargador exportado los espera antes de empezar.
interface Normalizer {
  normalizeEntityConfig: typeof NormalizeModule.normalizeEntityConfig;
  normalizeRule: typeof NormalizeModule.normalizeRule;
  checkKeys: typeof CompatModule.checkKeys;
}

let normalizer: Promise<Normalizer> | undefined;
let loaded: Normalizer | undefined;

async function ensureNormalizer(): Promise<void> {
  normalizer ??= Promise.all([import('./normalize'), import('./compat')]).then(([normalize, compat]) => ({
    normalizeEntityConfig: normalize.normalizeEntityConfig,
    normalizeRule: normalize.normalizeRule,
    checkKeys: compat.checkKeys,
  }));
  try {
    loaded = await normalizer;
  } catch (error) {
    normalizer = undefined; // una descarga fallida se puede reintentar
    throw error;
  }
}

/** El normalizador ya cargado: sólo se llama desde código que corre después de `ensureNormalizer()`. */
function norm(): Normalizer {
  if (!loaded) throw new ConfigError('El normalizador de la configuración no está cargado');
  return loaded;
}

export interface LoadOptions {
  signal?: AbortSignal;
  /** Para pruebas o entornos sin `fetch` global. */
  fetch?: typeof fetch;
  /** Escribe en la consola los avisos de configuración (por omisión sólo en desarrollo). */
  logWarnings?: boolean;
}

export interface LoadReport {
  manifest: AppManifest;
  /** Avisos del propio manifiesto y entidades que no se pudieron cargar (los de cada entidad van en `entity.warnings`). */
  warnings: ConfigWarning[];
}

type LoadInput = AbortSignal | LoadOptions | undefined;

function toOptions(input: LoadInput): LoadOptions {
  if (!input) return {};
  if (typeof AbortSignal !== 'undefined' && input instanceof AbortSignal) return { signal: input };
  return input as LoadOptions;
}

function shouldLog(options: LoadOptions): boolean {
  if (options.logWarnings !== undefined) return options.logWarnings;
  const mode = (import.meta as { env?: { MODE?: string; DEV?: boolean } }).env;
  return mode?.DEV === true && mode?.MODE !== 'test';
}

function absoluteBase(): string {
  return (globalThis as { location?: { href?: string } }).location?.href ?? 'http://localhost/';
}

/** Resuelve `path` relativo a la URL del manifiesto; devuelve una ruta del origen si el manifiesto es relativo. */
export function resolveRelative(path: string, baseUrl: string): string {
  if (/^[a-z][a-z0-9+.-]*:/i.test(path) || path.startsWith('//')) return path;
  const baseIsAbsolute = /^[a-z][a-z0-9+.-]*:/i.test(baseUrl);
  const resolved = new URL(path, new URL(baseUrl, absoluteBase()));
  if (baseIsAbsolute) return resolved.href;
  return `${resolved.pathname}${resolved.search}`;
}

async function fetchJson(url: string, options: LoadOptions): Promise<unknown> {
  const doFetch = options.fetch ?? globalThis.fetch;
  if (!doFetch) throw new ConfigError(`No hay \`fetch\` para leer ${url}`);
  let response: Response;
  try {
    response = await doFetch(url, { signal: options.signal, headers: { Accept: 'application/json' } });
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') throw error;
    throw new ConfigError(`No se pudo leer ${url}: ${error instanceof Error ? error.message : String(error)}`);
  }
  if (!response.ok) throw new ConfigError(`No se pudo leer ${url}: HTTP ${response.status}`);
  try {
    return await response.json();
  } catch {
    throw new ConfigError(`${url} no es JSON válido`);
  }
}

function addScan(warnings: ConfigWarning[], scope: string, obj: Obj, path: string): void {
  for (const w of norm().checkKeys(scope, obj)) {
    warnings.push({ path: path ? `${path}.${w.key}` : w.key, message: w.message, severity: w.severity });
  }
}

function str(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() !== '' ? value : undefined;
}

// ---------------------------------------------------------------------------------------------
// Menú
// ---------------------------------------------------------------------------------------------

function buildMenu(
  rawMenu: unknown[],
  entities: Record<string, EntityConfig>,
  warnings: ConfigWarning[],
  path: string,
  scope: 'manifestMenu' | 'sidebarItem' | 'sidebarContext',
): MenuNode[] {
  const out: MenuNode[] = [];
  rawMenu.forEach((raw, i) => {
    const here = `${path}[${i}]`;
    if (!isObj(raw)) {
      warnings.push({ path: here, message: 'ítem de menú ilegible: se omite', severity: 'warn' });
      return;
    }
    addScan(warnings, scope, raw, here);
    const entityId = str(raw.entity) ?? str(raw.entityId);
    if (str(raw.externalUrl)) {
      warnings.push({ path: here, message: 'ítem con `externalUrl` (iframe): se descarta', severity: 'info' });
      return;
    }
    if (entityId && !entities[entityId]) {
      warnings.push({
        path: here,
        message: `el ítem apunta a la entidad \`${entityId}\`, que no está cargada: se descarta`,
        severity: 'warn',
      });
      return;
    }
    const children = Array.isArray(raw.children)
      ? buildMenu(raw.children, entities, warnings, `${here}.children`, scope === 'manifestMenu' ? 'manifestMenu' : 'sidebarItem')
      : Array.isArray(raw.items)
        ? buildMenu(raw.items, entities, warnings, `${here}.items`, 'sidebarItem')
        : undefined;
    if (!entityId && (!children || children.length === 0)) {
      warnings.push({ path: here, message: 'grupo de menú vacío: se descarta', severity: 'info' });
      return;
    }
    const entity = entityId ? entities[entityId] : undefined;
    const label: Text =
      toText(raw.label, raw.labelKey) ??
      entity?.namePlural ??
      { literal: humanize(str(raw.id) ?? `node-${i + 1}`) };
    const node: MenuNode = { id: str(raw.id) ?? entityId ?? `node-${i + 1}`, label };
    const icon = str(raw.icon) ?? entity?.icon;
    if (icon) node.icon = icon;
    if (entityId) node.entity = entityId;
    if (children && children.length > 0) node.children = children;
    const visibleWhen = norm().normalizeRule(
      raw.visibleWhen !== undefined ? raw.visibleWhen : legacyRule(raw),
      warnings,
      `${here}.visibleWhen`,
    );
    if (visibleWhen) node.visibleWhen = visibleWhen;
    rememberMenuOrigin(node, here);
    out.push(node);
  });
  return out;
}

/** Claves legadas de un ítem de menú (`roles`, `sector`, `capability`, `featureFlag`) -> regla única. */
function legacyRule(raw: Obj): Obj | undefined {
  const legacy: Obj = {};
  for (const key of ['roles', 'sector', 'capability', 'featureFlag']) {
    if (raw[key] !== undefined) legacy[key] = raw[key];
  }
  return Object.keys(legacy).length > 0 ? legacy : undefined;
}

/** Menú por omisión: una entrada por entidad, en el orden de carga. */
function flatMenu(entities: Record<string, EntityConfig>): MenuNode[] {
  return Object.values(entities).map((e) => {
    const node: MenuNode = { id: e.id, label: e.namePlural, entity: e.id };
    if (e.icon) node.icon = e.icon;
    return node;
  });
}

// ---------------------------------------------------------------------------------------------
// Entidades
// ---------------------------------------------------------------------------------------------

interface EntityEntry {
  id: string;
  url?: string;
  config?: unknown;
  overlay?: unknown;
  /** Fichero de superposición (URL ya resuelta): se funde antes que `overlay`. */
  overlayUrl?: string;
  permissions?: unknown;
}

async function loadEntities(
  entries: EntityEntry[],
  options: LoadOptions,
  warnings: ConfigWarning[],
): Promise<Record<string, EntityConfig>> {
  const results = await Promise.all(
    entries.map(async (entry) => {
      try {
        const raw = entry.config ?? (await fetchJson(entry.url as string, options));
        let overlay = entry.overlay;
        if (entry.overlayUrl) {
          try {
            const fromFile = await fetchJson(entry.overlayUrl, options);
            if (!isObj(fromFile)) throw new ConfigError(`${entry.overlayUrl} no es una superposición: se esperaba un objeto JSON`);
            overlay = overlay === undefined ? fromFile : mergeOverlay(fromFile, overlay);
          } catch (error) {
            if (error instanceof DOMException && error.name === 'AbortError') throw error;
            warnings.push({
              path: `entities.${entry.id}.overlayPath`,
              message: `superposición no cargada: ${error instanceof Error ? error.message : String(error)}`,
              severity: 'warn',
            });
          }
        }
        if (isObj(entry.permissions)) {
          overlay = isObj(overlay) ? { ...overlay, permissions: entry.permissions } : { permissions: entry.permissions };
        }
        return { entry, config: norm().normalizeEntityConfig(raw, overlay, { id: entry.id }) };
      } catch (error) {
        if (error instanceof DOMException && error.name === 'AbortError') throw error;
        return { entry, error };
      }
    }),
  );
  const out: Record<string, EntityConfig> = {};
  for (const result of results) {
    if ('config' in result && result.config) {
      out[result.entry.id] = result.config;
    } else if ('error' in result) {
      warnings.push({
        path: `entities.${result.entry.id}`,
        message: `entidad no cargada: ${result.error instanceof Error ? result.error.message : String(result.error)}`,
        severity: 'warn',
      });
    }
  }
  return out;
}

function logReport(report: LoadReport): void {
  const groups: [string, ConfigWarning[]][] = [['manifiesto', report.warnings]];
  for (const e of Object.values(report.manifest.entities)) groups.push([e.id, e.warnings]);
  for (const [name, list] of groups) {
    const relevant = list.filter((w) => w.severity === 'warn');
    if (relevant.length === 0) continue;
    console.groupCollapsed(`[karpo] configuración de \`${name}\`: ${relevant.length} aviso(s)`);
    for (const w of relevant) console.warn(`${w.path}: ${w.message}`);
    console.groupEnd();
  }
}

// ---------------------------------------------------------------------------------------------
// Manifiesto
// ---------------------------------------------------------------------------------------------

function parseMapSources(raw: unknown, warnings: ConfigWarning[]): Record<string, MapSourceDef> {
  const out: Record<string, MapSourceDef> = {};
  if (!isObj(raw)) return out;
  const sources = isObj(raw.sources) ? raw.sources : {};
  for (const [id, source] of Object.entries(sources)) {
    const path = `map.sources.${id}`;
    if (!isObj(source) || !str(source.url)) {
      warnings.push({ path, message: 'fuente de mapa sin `url`: se omite', severity: 'warn' });
      continue;
    }
    const schematic = source.schematic === true ? { schematic: true } : {};
    if (source.type === 'geojson') out[id] = { type: 'geojson', url: source.url as string, ...schematic };
    else if (source.type === 'pmtiles' && str(source.layer)) {
      out[id] = { type: 'pmtiles', url: source.url as string, layer: source.layer as string, ...schematic };
    } else {
      warnings.push({
        path,
        message: 'fuente de mapa con `type` no válido (`geojson` o `pmtiles` con `layer`): se omite',
        severity: 'warn',
      });
    }
  }
  return out;
}

function strList(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string' && v !== '') : [];
}

async function loadDictionaries(
  i18n: unknown,
  manifestUrl: string,
  options: LoadOptions,
  warnings: ConfigWarning[],
): Promise<void> {
  if (!isObj(i18n)) return;
  await Promise.all(
    Object.entries(i18n).map(async ([lng, value]) => {
      const url = str(value);
      if (!url) return;
      try {
        registerConfigDictionary(lng, await fetchJson(resolveRelative(url, manifestUrl), options));
      } catch (error) {
        if (error instanceof DOMException && error.name === 'AbortError') throw error;
        warnings.push({
          path: `i18n.${lng}`,
          message: `diccionario no cargado: ${error instanceof Error ? error.message : String(error)}`,
          severity: 'warn',
        });
      }
    }),
  );
}

function assemble(
  base: {
    api?: unknown;
    locales?: unknown;
    defaultLocale?: unknown;
    theme?: unknown;
    map?: unknown;
  },
  entities: Record<string, EntityConfig>,
  menu: MenuNode[],
  warnings: ConfigWarning[],
): AppManifest {
  const locales = strList(base.locales);
  const finalLocales = locales.length > 0 ? locales : ['es', 'en'];
  const defaultLocale = str(base.defaultLocale) ?? finalLocales[0]!;
  const api = isObj(base.api) ? base.api : {};
  const theme = isObj(base.theme) ? base.theme : {};
  const themeDefault = str(theme.default) ?? 'modern-flat';
  const available = strList(theme.available);
  return {
    schemaVersion: 1,
    entities,
    menu,
    locales: finalLocales,
    defaultLocale,
    theme: {
      origin: str(theme.origin) ?? '/themes/',
      default: themeDefault,
      available: available.length > 0 ? available : [themeDefault],
    },
    map: { sources: parseMapSources(base.map, warnings) },
    api: { baseUrl: str(api.baseUrl) ?? '/api', apiVersion: str(api.apiVersion) ?? 'v1' },
  };
}

/** Lee `manifest.json` y devuelve el modelo normalizado con el informe de avisos. */
export async function loadManifestWithReport(
  url: string,
  input?: LoadInput,
): Promise<LoadReport> {
  const options = toOptions(input);
  const [raw] = await Promise.all([fetchJson(url, options), ensureNormalizer()]);
  if (!isObj(raw)) throw new ConfigError(`${url} no es un manifiesto: se esperaba un objeto JSON`);
  const warnings: ConfigWarning[] = [];
  addScan(warnings, 'manifest', raw, '');
  if (raw.schemaVersion !== 1) {
    throw new ConfigError(
      `Manifiesto con \`schemaVersion\` ${JSON.stringify(raw.schemaVersion)}: sólo se admite 1`,
    );
  }

  const entries: EntityEntry[] = [];
  const rawEntities = Array.isArray(raw.entities) ? raw.entities : [];
  rawEntities.forEach((item, i) => {
    const path = `entities[${i}]`;
    if (!isObj(item)) {
      warnings.push({ path, message: 'entrada de entidad ilegible: se omite', severity: 'warn' });
      return;
    }
    addScan(warnings, 'manifestEntity', item, path);
    const id = str(item.id);
    const entityPath = str(item.path);
    if (!id || (!entityPath && !isObj(item.config))) {
      warnings.push({ path, message: 'entrada sin `id` o sin `path`/`config`: se omite', severity: 'warn' });
      return;
    }
    if (entries.some((e) => e.id === id)) {
      warnings.push({ path, message: `\`id\` repetido (\`${id}\`): se omite la segunda`, severity: 'warn' });
      return;
    }
    entries.push({
      id,
      url: entityPath ? resolveRelative(entityPath, url) : undefined,
      config: isObj(item.config) ? item.config : undefined,
      overlay: item.overlay,
      overlayUrl: str(item.overlayPath) ? resolveRelative(str(item.overlayPath) as string, url) : undefined,
      permissions: item.permissions,
    });
  });

  const [entities] = await Promise.all([
    loadEntities(entries, options, warnings),
    loadDictionaries(raw.i18n, url, options, warnings),
  ]);
  const menu = Array.isArray(raw.menu)
    ? buildMenu(raw.menu, entities, warnings, 'menu', 'manifestMenu')
    : flatMenu(entities);
  warnings.push(...menuIconWarnings(menu, entities));
  const manifest = assemble(raw, entities, menu, warnings);
  const report = { manifest, warnings };
  if (shouldLog(options)) logReport(report);
  return report;
}

/** Lee `manifest.json` (PLAN §4.3.1) y devuelve el modelo normalizado. */
export async function loadManifest(url: string, input?: LoadInput): Promise<AppManifest> {
  return (await loadManifestWithReport(url, input)).manifest;
}

// ---------------------------------------------------------------------------------------------
// Modo legado: index.json + sidebar-config.json
// ---------------------------------------------------------------------------------------------

export interface LegacyOptions extends LoadOptions {
  /** Carga todas las entidades del índice (por omisión, sólo las que cita el menú). */
  all?: boolean;
  /** Ajustes del manifiesto resultante (idiomas, tema, mapa, API). */
  manifest?: Partial<Pick<AppManifest, 'locales' | 'defaultLocale' | 'theme' | 'map' | 'api'>>;
}

function collectEntityIds(items: unknown[], into: Set<string>): void {
  for (const item of items) {
    if (!isObj(item)) continue;
    const entityId = str(item.entityId) ?? str(item.entity);
    if (entityId && !str(item.externalUrl)) into.add(entityId);
    if (Array.isArray(item.children)) collectEntityIds(item.children, into);
    if (Array.isArray(item.items)) collectEntityIds(item.items, into);
  }
}

/** Modo legado: `index.json` + `sidebar-config.json` del runtime Angular. */
export async function loadLegacyWithReport(
  indexUrl: string,
  sidebarUrl: string,
  input?: LegacyOptions | AbortSignal,
): Promise<LoadReport> {
  const options = (toOptions(input) as LegacyOptions) ?? {};
  const [rawIndex, rawSidebar] = await Promise.all([
    fetchJson(indexUrl, options),
    fetchJson(sidebarUrl, options),
    ensureNormalizer(),
  ]);
  if (!isObj(rawIndex)) throw new ConfigError(`${indexUrl} no es un índice: se esperaba un objeto JSON`);
  if (!isObj(rawSidebar)) throw new ConfigError(`${sidebarUrl} no es un menú: se esperaba un objeto JSON`);
  const warnings: ConfigWarning[] = [];
  addScan(warnings, 'index', rawIndex, '');

  const index: Record<string, string> = {};
  if (isObj(rawIndex.entities)) {
    for (const [id, path] of Object.entries(rawIndex.entities)) if (typeof path === 'string') index[id] = path;
  }
  const contexts = Array.isArray(rawSidebar.contexts) ? rawSidebar.contexts : [];
  const referenced = new Set<string>();
  for (const context of contexts) {
    if (isObj(context)) collectEntityIds(Array.isArray(context.items) ? context.items : [], referenced);
  }
  const wanted = options.all ? [...new Set([...Object.keys(index), ...referenced])] : [...referenced];
  const entries: EntityEntry[] = [];
  for (const id of wanted) {
    const path = index[id] ?? `${id}.config.json`;
    if (!index[id]) {
      warnings.push({
        path: `entities.${id}`,
        message: `\`${id}\` no está en el índice: se busca \`${path}\` junto a él`,
        severity: 'info',
      });
    }
    entries.push({ id, url: resolveRelative(path, indexUrl) });
  }
  const entities = await loadEntities(entries, options, warnings);

  const menu: MenuNode[] = [];
  contexts.forEach((context, i) => {
    const path = `contexts[${i}]`;
    if (!isObj(context)) {
      warnings.push({ path, message: 'contexto ilegible: se omite', severity: 'warn' });
      return;
    }
    addScan(warnings, 'sidebarContext', context, path);
    const children = buildMenu(Array.isArray(context.items) ? context.items : [], entities, warnings, `${path}.items`, 'sidebarItem');
    if (children.length === 0) return;
    const node: MenuNode = {
      id: str(context.id) ?? `context-${i + 1}`,
      label: toText(context.label, context.labelKey) ?? { literal: humanize(str(context.id) ?? 'menu') },
      children,
    };
    const icon = str(context.icon);
    if (icon) node.icon = icon;
    rememberMenuOrigin(node, path);
    menu.push(node);
  });
  warnings.push(...menuIconWarnings(menu, entities, 'contexts'));

  const manifest = assemble(options.manifest ?? {}, entities, menu.length > 0 ? menu : flatMenu(entities), warnings);
  const report = { manifest, warnings };
  if (shouldLog(options)) logReport(report);
  return report;
}

export async function loadLegacy(
  indexUrl: string,
  sidebarUrl: string,
  input?: LegacyOptions | AbortSignal,
): Promise<AppManifest> {
  return (await loadLegacyWithReport(indexUrl, sidebarUrl, input)).manifest;
}

// ---------------------------------------------------------------------------------------------
// Descubrimiento por lista de rutas
// ---------------------------------------------------------------------------------------------

export interface DiscoverOptions extends LoadOptions {
  manifest?: LegacyOptions['manifest'];
  /** Permisos y superposiciones por id de entidad. */
  overlays?: Record<string, unknown>;
}

/** Entidades sin índice (los 8 de Go): `paths` relativas a `baseUrl`; el menú agrupa por carpeta. */
export async function loadDiscoveredWithReport(
  baseUrl: string,
  paths: string[],
  input?: DiscoverOptions | AbortSignal,
): Promise<LoadReport> {
  const options = (toOptions(input) as DiscoverOptions) ?? {};
  const warnings: ConfigWarning[] = [];
  const base = baseUrl.endsWith('/') ? baseUrl : `${baseUrl}/`;
  const entries: (EntityEntry & { folder?: string })[] = paths.map((path) => {
    const parts = path.split('/').filter(Boolean);
    const file = parts[parts.length - 1] ?? path;
    const id = file.replace(/\.config\.json$/i, '').replace(/\.json$/i, '');
    return {
      id,
      url: resolveRelative(path, base),
      overlay: options.overlays?.[id],
      folder: parts.length > 1 ? parts[0] : undefined,
    };
  });
  await ensureNormalizer();
  const entities = await loadEntities(entries, options, warnings);

  const menu: MenuNode[] = [];
  const groups = new Map<string, MenuNode>();
  for (const entry of entries) {
    const entity = entities[entry.id];
    if (!entity) continue;
    const leaf: MenuNode = { id: entity.id, label: entity.namePlural, entity: entity.id };
    if (entity.icon) leaf.icon = entity.icon;
    if (!entry.folder) {
      menu.push(leaf);
      continue;
    }
    let group = groups.get(entry.folder);
    if (!group) {
      group = {
        id: entry.folder,
        label: { literal: humanize(entry.folder), key: `menu.${entry.folder}` },
        children: [],
      };
      groups.set(entry.folder, group);
      menu.push(group);
    }
    group.children!.push(leaf);
  }
  warnings.push(...menuIconWarnings(menu, entities));
  const manifest = assemble(options.manifest ?? {}, entities, menu, warnings);
  const report = { manifest, warnings };
  if (shouldLog(options)) logReport(report);
  return report;
}

export async function loadDiscovered(
  baseUrl: string,
  paths: string[],
  input?: DiscoverOptions | AbortSignal,
): Promise<AppManifest> {
  return (await loadDiscoveredWithReport(baseUrl, paths, input)).manifest;
}
