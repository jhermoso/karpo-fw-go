// Arnés de pruebas del organigrama: monta `TreeView` contra el backend falso de la demostración
// (`demo/mocks/node.ts`), con la configuración real de la demo y un enrutador en memoria que
// traduce `EntityRoute` ⇄ URL como lo hará el marco (`?view=tree&layout=…`). Sólo para pruebas.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, render, type RenderResult } from '@testing-library/react';
import userEvent, { type UserEvent } from '@testing-library/user-event';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import type { ReactElement } from 'react';
import { MemoryRouter, useLocation, useNavigate } from 'react-router';
import { afterAll, afterEach, beforeAll } from 'vitest';
import { createEntityApi, createHttpClient } from '../api';
import { loadManifest } from '../config';
import {
  ApiContext,
  ManifestContext,
  type AppManifest,
  type EntityConfig,
  type EntityRoute,
  type EntityViewProps,
  type ViewStatus,
} from '../contracts';
import { I18nProvider } from '../i18n';
import { resetDataset, server } from '../../demo/mocks/node';

const ROOT = resolve(import.meta.dirname, '../..');
const CONFIG_BASE = 'http://localhost/config/';

/** `fetch` que sirve la configuración de la demo desde el disco (`go/…` sale de `front/angular`). */
export function demoConfigFetch(): typeof fetch {
  return (async (input: RequestInfo | URL) => {
    const url = String(input);
    if (!url.startsWith(CONFIG_BASE)) return new Response('no encontrado', { status: 404 });
    const path = url.slice(CONFIG_BASE.length);
    const file = path.startsWith('go/')
      ? resolve(ROOT, '../angular/config/entities', path.slice(3))
      : resolve(ROOT, 'demo/config', path);
    try {
      return new Response(readFileSync(file, 'utf8'), { status: 200, headers: { 'content-type': 'application/json' } });
    } catch {
      return new Response('no encontrado', { status: 404 });
    }
  }) as typeof fetch;
}

let cached: Promise<AppManifest> | undefined;

/** Manifiesto de la demo normalizado (una sola lectura por proceso de pruebas). */
export function loadDemoManifest(): Promise<AppManifest> {
  cached ??= loadManifest(`${CONFIG_BASE}manifest.json`, { fetch: demoConfigFetch(), logWarnings: false });
  return cached;
}

/** Arranca el backend falso con el ciclo de vida de vitest (llamar una vez por fichero de pruebas). */
export function installDemoBackend(): void {
  beforeAll(() => server.listen({ onUnhandledFrame: 'error' }));
  afterEach(() => {
    server.resetHandlers();
    resetDataset();
  });
  afterAll(() => server.close());
}

/** URL de una ruta de entidad: los `params` son el conjunto completo de parámetros de la vista. */
export function routeToUrl(to: EntityRoute, current: { entity: string; search: URLSearchParams }): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(to.params ?? {})) if (value !== '') search.set(key, value);
  const view = to.view ?? (current.entity === to.entity ? current.search.get('view') : null);
  if (view) search.set('view', view);
  if (to.id !== undefined && to.mode === 'edit') search.set('mode', 'edit');
  const query = search.toString();
  const path = to.id !== undefined ? `/${to.entity}/${encodeURIComponent(to.id)}` : `/${to.entity}`;
  return query ? `${path}?${query}` : path;
}

function Bridge({
  config,
  View,
  onStatus,
  expose,
}: {
  config: EntityConfig;
  View: (props: EntityViewProps) => ReactElement | null;
  onStatus?: (status: ViewStatus) => void;
  /** Entrega la navegación del enrutador: lo que hace la cabecera de la entidad al cambiar de disposición. */
  expose: (go: (to: string | number) => void) => void;
}) {
  const location = useLocation();
  const go = useNavigate();
  expose((to) => (typeof to === 'number' ? go(to) : go(to)));
  const search = new URLSearchParams(location.search);
  const params: Record<string, string> = {};
  for (const [key, value] of search) if (key !== 'view' && key !== 'mode') params[key] = value;
  const view = search.get('view');
  const route: EntityRoute = {
    entity: config.id,
    ...(view === 'tree' || view === 'map' || view === 'grid' ? { view } : {}),
    params,
  };
  return (
    <>
      <View
        config={config}
        route={route}
        navigate={(to) => go(routeToUrl(to, { entity: config.id, search }))}
        {...(onStatus ? { onStatus } : {})}
      />
      <output data-testid="location">{`${location.pathname}${location.search}`}</output>
    </>
  );
}

export interface RenderViewOptions {
  entity: string;
  /** Cadena de consulta inicial, p. ej. `?view=tree&layout=diagram`. */
  search?: string;
  locale?: 'es' | 'en';
  /** Dirección forzada (`?dir=rtl` del proveedor de idioma). */
  dir?: 'ltr' | 'rtl';
  /** Cambia la configuración de la entidad antes de montar (por ejemplo, otra fuente del árbol). */
  configure?: (config: EntityConfig) => EntityConfig;
  /** Proveedores extra por dentro del enrutador (por ejemplo, el del tema). */
  wrap?: (children: ReactElement) => ReactElement;
  /** Recibe lo que la vista cuenta a la cabecera de la entidad (M-7). */
  onStatus?: (status: ViewStatus) => void;
}

export interface RenderedView extends RenderResult {
  user: UserEvent;
  config: EntityConfig;
  manifest: AppManifest;
  /** Ruta y consulta actuales. */
  location(): string;
  /** Parámetros de la consulta actual. */
  params(): URLSearchParams;
  /** Navega a otra dirección como lo haría la cabecera de la entidad (`/facility?view=tree&layout=diagram`). */
  goto(to: string): void;
  /** «Atrás» del navegador: recorre el historial del enrutador en memoria (R3-4, M-5). */
  back(): void;
}

export async function renderEntityView(
  View: (props: EntityViewProps) => ReactElement | null,
  options: RenderViewOptions,
): Promise<RenderedView> {
  const manifest = await loadDemoManifest();
  const base = manifest.entities[options.entity];
  const config = base && options.configure ? options.configure(base) : base;
  if (!config) throw new Error(`La entidad ${options.entity} no está en el manifiesto de la demo`);
  const client = createHttpClient({
    baseUrl: 'http://localhost/api',
    getToken: () => undefined,
    refresh: async () => undefined,
    getScope: () => undefined,
    onUnauthorized: () => undefined,
    maxGetRetries: 0,
  });
  const api = createEntityApi(client, manifest);
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  const user = userEvent.setup();
  const search = options.search ?? '';
  const dirSearch = options.dir ? `?dir=${options.dir}` : '';

  let goto: ((to: string | number) => void) | undefined;
  const body = (
    <MemoryRouter initialEntries={[`/${options.entity}${search}`]}>
      <Bridge
        config={config}
        View={View}
        {...(options.onStatus ? { onStatus: options.onStatus } : {})}
        expose={(go) => {
          goto = go;
        }}
      />
    </MemoryRouter>
  );
  const result = render(
    <I18nProvider locale={options.locale ?? 'es'} search={dirSearch} storageKey={null}>
      <ManifestContext.Provider value={manifest}>
        <ApiContext.Provider value={api}>
          <QueryClientProvider client={queryClient}>{options.wrap ? options.wrap(body) : body}</QueryClientProvider>
        </ApiContext.Provider>
      </ManifestContext.Provider>
    </I18nProvider>,
  );
  return {
    ...result,
    user,
    config,
    manifest,
    location: () => result.getByTestId('location').textContent ?? '',
    params: () => new URLSearchParams((result.getByTestId('location').textContent ?? '').split('?')[1] ?? ''),
    goto: (to) => {
      act(() => goto?.(to));
    },
    back: () => {
      act(() => goto?.(-1));
    },
  };
}
