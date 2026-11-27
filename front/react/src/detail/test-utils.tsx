// Arnés de pruebas del detalle y del marco (sólo pruebas; no entra en el paquete).
//  - `installMockBackend()`: el backend falso de la demo (demo/mocks/node.ts) con sus hooks de vitest.
//  - `loadDemoManifest()`: el manifiesto REAL de la demo (los 8 JSON de Go + superposiciones) leído del disco.
//  - `renderDetail()`: un DetailView con todos los proveedores y un anfitrión que sigue las navegaciones.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen, waitFor, type RenderResult } from '@testing-library/react';
import userEvent, { type UserEvent } from '@testing-library/user-event';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { useCallback, useMemo, useState, type PropsWithChildren, type ReactNode } from 'react';
import { afterAll, afterEach, beforeAll, vi } from 'vitest';
import { server, resetDataset, setMockOptions, mock } from '../../demo/mocks/node';
import { createEntityApi } from '../api';
import { SessionProvider, useHttpClient, useSession } from '../auth';
import { loadManifest } from '../config';
import {
  ApiContext,
  ManifestContext,
  queryKeys,
  type AppManifest,
  type EntityConfig,
  type EntityRoute,
  type Row,
} from '../contracts';
import { HelpProvider } from '../help';
import { I18nProvider } from '../i18n';
import { ShortcutProvider, ToastProvider } from '../ui';
import { DetailView, type DetailViewProps } from './DetailView';

export const ORIGIN = 'http://localhost';
export const API_BASE = `${ORIGIN}/api`;

// ---------------------------------------------------------------------------------------------
// Manifiesto real de la demo
// ---------------------------------------------------------------------------------------------

const DEMO_CONFIG = path.resolve(import.meta.dirname, '../../demo/config');
const GO_CONFIG = path.resolve(import.meta.dirname, '../../../angular/config/entities');

/** `fetch` que sirve `http://localhost/config/…` desde el disco: `go/…` de front/angular, el resto de demo/config. */
export const diskFetch = (async (input: RequestInfo | URL) => {
  const url = new URL(String(input));
  const relative = url.pathname.replace(/^\/config\//, '');
  const file = relative.startsWith('go/')
    ? path.join(GO_CONFIG, relative.slice(3))
    : path.join(DEMO_CONFIG, relative);
  try {
    return new Response(readFileSync(file, 'utf8'), { status: 200, headers: { 'content-type': 'application/json' } });
  } catch {
    return new Response('no encontrado', { status: 404 });
  }
}) as typeof fetch;

let manifestPromise: Promise<AppManifest> | undefined;

export function loadDemoManifest(): Promise<AppManifest> {
  manifestPromise ??= loadManifest(`${ORIGIN}/config/manifest.json`, { fetch: diskFetch, logWarnings: false });
  return manifestPromise;
}

/** Manifiesto con el tema y el idioma de la demo, listo para `KarpoApp`. */
export async function demoManifest(): Promise<AppManifest> {
  const manifest = await loadDemoManifest();
  return { ...manifest, api: { ...manifest.api, baseUrl: API_BASE } };
}

// ---------------------------------------------------------------------------------------------
// Backend falso
// ---------------------------------------------------------------------------------------------

/** Almacén en memoria con la forma de `Storage` (la sesión no toca el `sessionStorage` de jsdom). */
export class MemoryStorage implements Storage {
  private data = new Map<string, string>();
  get length(): number {
    return this.data.size;
  }
  clear(): void {
    this.data.clear();
  }
  getItem(key: string): string | null {
    return this.data.get(key) ?? null;
  }
  key(index: number): string | null {
    return [...this.data.keys()][index] ?? null;
  }
  removeItem(key: string): void {
    this.data.delete(key);
  }
  setItem(key: string, value: string): void {
    this.data.set(key, value);
  }
}

/** Una sesión ya iniciada, como la dejaría `SessionStore` en el almacenamiento. */
export function seededSession(username = 'demo'): MemoryStorage {
  const storage = new MemoryStorage();
  storage.setItem(
    'karpo.session',
    JSON.stringify({
      accessToken: 'test-access',
      refreshToken: 'test-refresh',
      expiresAt: Date.now() + 60 * 60_000,
      userId: '0192a000-0000-7000-9000-000000000001',
      username,
      mustChangePassword: false,
    }),
  );
  return storage;
}

/** Registra los hooks de vitest del backend falso: sin sesión obligatoria y sin latencia. */
export function installMockBackend(): void {
  beforeAll(() => server.listen({ onUnhandledFrame: 'error' }));
  afterEach(() => {
    cleanup();
    server.resetHandlers();
    resetDataset();
    setMockOptions({ requireAuth: false, latencyMs: 0, requireApproval: false });
    localStorage.clear();
    sessionStorage.clear();
    // `afterEach` de limpieza antes de soltar los documentos: evita avisos de React tras el cierre.
    document.documentElement.removeAttribute('data-th-mode');
  });
  afterAll(() => server.close());
}

// ---------------------------------------------------------------------------------------------
// Proveedores
// ---------------------------------------------------------------------------------------------

function ApiBridge({ manifest, children }: PropsWithChildren<{ manifest: AppManifest }>) {
  const client = useHttpClient();
  const api = useMemo(() => createEntityApi(client, manifest), [client, manifest]);
  return <ApiContext.Provider value={api}>{children}</ApiContext.Provider>;
}

/** Espera a tener los permisos cargados: sin ellos `can()` es falso y la pantalla enseñaría «sin permiso». */
function SessionGate({ children }: PropsWithChildren) {
  const session = useSession();
  return session.context ? <>{children}</> : null;
}

export function testQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 30_000 }, mutations: { retry: false } } });
}

export interface ProvidersProps {
  manifest: AppManifest;
  queryClient?: QueryClient;
  storage?: Storage | null;
  locale?: string;
}

export function TestProviders({ manifest, queryClient, storage, locale = 'es', children }: PropsWithChildren<ProvidersProps>) {
  const [client] = useState(() => queryClient ?? testQueryClient());
  const [store] = useState(() => storage ?? seededSession());
  return (
    <I18nProvider locale={locale} storageKey={null} search="">
      <QueryClientProvider client={client}>
        <ManifestContext.Provider value={manifest}>
          <SessionProvider apiBaseUrl={API_BASE} storage={store}>
            <ApiBridge manifest={manifest}>
              <SessionGate>
                <ToastProvider>
                  <ShortcutProvider platform="other">
                    <HelpProvider>{children}</HelpProvider>
                  </ShortcutProvider>
                </ToastProvider>
              </SessionGate>
            </ApiBridge>
          </SessionProvider>
        </ManifestContext.Provider>
      </QueryClientProvider>
    </I18nProvider>
  );
}

// ---------------------------------------------------------------------------------------------
// Detalle
// ---------------------------------------------------------------------------------------------

export interface DetailHostProps {
  config: EntityConfig;
  initial: EntityRoute;
  navigated: (to: EntityRoute) => void;
  presentation?: DetailViewProps['presentation'];
  onClose?: () => void;
  onToggleExpand?: () => void;
}

/** Anfitrión mínimo: guarda la ruta y la actualiza con cada `navigate`, como hace la pantalla de la entidad. */
export function DetailHost({ config, initial, navigated, presentation, onClose, onToggleExpand }: DetailHostProps): ReactNode {
  const [route, setRoute] = useState<EntityRoute>(initial);
  const navigate = useCallback(
    (to: EntityRoute) => {
      navigated(to);
      setRoute((current) => ({
        entity: to.entity,
        id: to.id,
        mode: to.id === undefined ? (to.mode === 'edit' ? 'edit' : undefined) : (to.mode ?? 'read'),
        view: to.view ?? current.view,
        params: to.params ?? current.params,
      }));
    },
    [navigated],
  );
  return (
    <DetailView
      key={route.id ?? 'new'}
      config={config}
      route={route}
      navigate={navigate}
      presentation={presentation}
      onClose={onClose}
      onToggleExpand={onToggleExpand}
    />
  );
}

export interface RenderDetailOptions {
  entity?: string;
  /** Identificador del registro; por omisión, el primero de la entidad en el conjunto de datos. */
  id?: string;
  mode?: 'read' | 'edit';
  /** Alta (`/:entity/nuevo`). */
  create?: boolean;
  presentation?: DetailViewProps['presentation'];
  /** Filas de la lista que se dejan en la caché (para anterior/siguiente). */
  list?: Row[];
  onToggleExpand?: () => void;
  /** Cambia una copia de la configuración (por ejemplo, para añadir un hint o repartir en columnas). */
  configure?: (config: EntityConfig) => void;
}

export interface DetailHarness {
  view: RenderResult;
  user: UserEvent;
  queryClient: QueryClient;
  manifest: AppManifest;
  config: EntityConfig;
  /** Navegaciones pedidas por la ficha, en orden. */
  navigated: ReturnType<typeof vi.fn<(to: EntityRoute) => void>>;
  onClose: ReturnType<typeof vi.fn<() => void>>;
}

/** Instalaciones del conjunto de datos, tal como las ve la API (con `version`). */
export function facilityRows(): Row[] {
  return mock.data.facilities.map((facility) => ({ ...facility })) as unknown as Row[];
}

export async function renderDetail(options: RenderDetailOptions = {}): Promise<DetailHarness> {
  const manifest = await demoManifest();
  const entity = options.entity ?? 'facility';
  const config = structuredClone(manifest.entities[entity] as EntityConfig);
  options.configure?.(config);
  const queryClient = testQueryClient();
  const user = userEvent.setup();
  const navigated = vi.fn<(to: EntityRoute) => void>();
  const onClose = vi.fn<() => void>();

  if (options.list) {
    const query = { page: 1, pageSize: 25, filter: {} };
    queryClient.setQueryData(queryKeys.list(entity, query), {
      pages: [{ items: options.list, total: options.list.length, page: 1, pageSize: 25 }],
      pageParams: [1],
    });
  }

  const first = facilityRows()[0] as Row;
  const initial: EntityRoute = options.create
    ? { entity, mode: 'edit', params: {} }
    : { entity, id: options.id ?? first.id, mode: options.mode ?? 'read', params: {} };

  const view = render(
    <TestProviders manifest={manifest} queryClient={queryClient}>
      <DetailHost
        config={config}
        initial={initial}
        navigated={navigated}
        presentation={options.presentation}
        onClose={onClose}
        onToggleExpand={options.onToggleExpand}
      />
    </TestProviders>,
  );
  return { view, user, queryClient, manifest, config, navigated, onClose };
}

/** Espera a que la ficha termine de cargar el registro (sale el título de la cabecera). */
export async function waitForSheet(): Promise<HTMLElement> {
  return waitFor(() => {
    const sheet = document.querySelector<HTMLElement>('[data-th="sheet"][data-kx-view="detail"][data-kx-mode]');
    if (!sheet) throw new Error('la ficha aún no está');
    return sheet;
  });
}

export { screen };
