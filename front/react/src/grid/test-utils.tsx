// Arnés de pruebas de la cuadrícula: QueryClient, API real contra el backend falso (MSW, demo/mocks/node),
// i18n real, tema falso y un anfitrión con ruta con estado. Sólo pruebas: no entra en el paquete.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState, type ReactNode } from 'react';
import { I18nextProvider } from 'react-i18next';
import { afterAll, afterEach, beforeAll, expect, vi, type Mock } from 'vitest';
import { mock, resetDataset, server, setMockOptions } from '../../demo/mocks/node';
import { createEntityApi, createHttpClient } from '../api';
import { normalizeEntityConfig, type EntityConfigExt } from '../config';
import { readRealById } from '../config/__fixtures__/real-configs';
import {
  ApiContext,
  SessionContext,
  ThemeContext,
  type EntityApi,
  type EntityRoute,
  type Session,
  type ThemeApi,
} from '../contracts';
import { createI18n } from '../i18n';
import { ShortcutProvider, ToastProvider } from '../ui';
import { GridView } from './GridView';
import { createGridStore, type GridStore } from './store';
import type { GridViewProps } from './types';

export const ORIGIN = 'http://localhost';

/** Servidor falso del backend de la demo: se arranca y se limpia en cada fichero de pruebas. */
export function setupMockServer(): void {
  beforeAll(() => server.listen({ onUnhandledFrame: 'error' }));
  afterEach(() => {
    server.resetHandlers();
    resetDataset();
    setMockOptions({ requireAuth: false, latencyMs: 0, requireApproval: false });
    localStorage.clear();
  });
  afterAll(() => server.close());
}

export { server, mock, setMockOptions };

/**
 * jsdom no mide nada: se le da una ventana de 600 de alto y 1000 de ancho a todo elemento, que es lo que
 * necesita TanStack Virtual para pintar filas. Devuelve la función que lo deshace.
 */
export function stubLayout(height = 600, width = 1000): () => void {
  const proto = HTMLElement.prototype;
  const originals = {
    offsetHeight: Object.getOwnPropertyDescriptor(proto, 'offsetHeight'),
    offsetWidth: Object.getOwnPropertyDescriptor(proto, 'offsetWidth'),
    clientHeight: Object.getOwnPropertyDescriptor(Element.prototype, 'clientHeight'),
  };
  Object.defineProperty(proto, 'offsetHeight', { configurable: true, get: () => height });
  Object.defineProperty(proto, 'offsetWidth', { configurable: true, get: () => width });
  Object.defineProperty(Element.prototype, 'clientHeight', { configurable: true, get: () => height });
  return () => {
    for (const [name, descriptor] of Object.entries(originals)) {
      const target = name === 'clientHeight' ? Element.prototype : proto;
      if (descriptor) Object.defineProperty(target, name, descriptor);
      else delete (target as unknown as Record<string, unknown>)[name];
    }
  };
}

/** Tema falso: lo justo para que la cuadrícula lo consulte y se entere de los cambios. */
export function createFakeTheme(): ThemeApi & { emit(): void } {
  const listeners = new Set<() => void>();
  return {
    info: { name: 'test', modes: ['light'], highContrast: false, iconStyle: 'outline', dictionaryHash: 'sha256:test' },
    mode: 'light',
    setMode: () => undefined,
    setTheme: async () => undefined,
    available: ['test'],
    resolveColor: () => 'rgb(0, 0, 0)',
    onChange(callback) {
      listeners.add(callback);
      return () => listeners.delete(callback);
    },
    emit() {
      listeners.forEach((callback) => callback());
    },
  };
}

export function createTestApi(): EntityApi {
  const client = createHttpClient({
    baseUrl: `${ORIGIN}/api`,
    getToken: () => 'token',
    refresh: async () => undefined,
    getScope: () => undefined,
    onUnauthorized: () => undefined,
    sleep: async () => undefined,
    maxGetRetries: 0,
  });
  return createEntityApi(client);
}

/** Configuración real de Go (los 8 JSON de `front/angular`) con una superposición opcional. */
export function realConfig(id: string, overlay?: unknown): EntityConfigExt {
  return normalizeEntityConfig(readRealById(id), overlay);
}

export function productConfig(overlay?: unknown): EntityConfigExt {
  return realConfig('product', overlay);
}

export interface RenderGridOptions {
  config?: EntityConfigExt;
  overlay?: unknown;
  params?: Record<string, string>;
  api?: EntityApi;
  store?: GridStore;
  session?: Session;
  locale?: 'es' | 'en';
  theme?: ThemeApi;
  shortcuts?: boolean;
  overscan?: number;
  /** Receptor del recuento que la vista cuenta a la cabecera de la entidad (`EntityViewProps.onStatus`). */
  onStatus?: GridViewProps['onStatus'];
  /** Algo más que pintar junto a la cuadrícula (por ejemplo, un lector del registro de atajos). */
  extra?: ReactNode;
}

export interface GridHarness {
  user: ReturnType<typeof userEvent.setup>;
  navigate: Mock<(to: EntityRoute) => void>;
  store: GridStore;
  api: EntityApi;
  queryClient: QueryClient;
  config: EntityConfigExt;
  container: HTMLElement;
  /** Última ruta que el anfitrión recibió con `navigate` sin `id` (la de la lista). */
  route(): EntityRoute;
  /** `role=grid` de la cuadrícula. */
  grid(): HTMLElement;
  /** Filas de datos pintadas (sin cabecera, sonda ni esqueleto). */
  rows(): HTMLElement[];
  /** Celda de datos por posición de fila (0 = la primera pintada) y campo. */
  cell(rowIndex: number, field: string): HTMLElement;
  /** Espera a que haya filas pintadas. */
  ready(): Promise<void>;
  unmount(): void;
}

let hostRoute: EntityRoute | undefined;

function Host(props: { config: EntityConfigExt; initial: EntityRoute; navigate: Mock<(to: EntityRoute) => void>; gridProps: Partial<GridViewProps> }) {
  const [route, setRoute] = useState<EntityRoute>(props.initial);
  return (
    <GridView
      config={props.config}
      route={route}
      navigate={(to) => {
        props.navigate(to);
        if (to.id === undefined) {
          hostRoute = to;
          setRoute(to);
        }
      }}
      {...props.gridProps}
    />
  );
}

export function renderGrid(options: RenderGridOptions = {}): GridHarness {
  const config = options.config ?? productConfig(options.overlay);
  const api = options.api ?? createTestApi();
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  const store = options.store ?? createGridStore();
  const i18n = createI18n({ locale: options.locale ?? 'es', pseudo: false });
  const navigate = vi.fn<(to: EntityRoute) => void>();
  const initial: EntityRoute = { entity: config.id, params: options.params ?? {} };
  hostRoute = initial;
  const user = userEvent.setup({ pointerEventsCheck: 0 });

  let tree: ReactNode = (
    <Host
      config={config}
      initial={initial}
      navigate={navigate}
      gridProps={{
        store,
        ...(options.overscan !== undefined ? { overscan: options.overscan } : {}),
        ...(options.onStatus ? { onStatus: options.onStatus } : {}),
      }}
    />
  );
  tree = (
    <>
      {tree}
      {options.extra}
    </>
  );
  if (options.shortcuts !== false) tree = <ShortcutProvider platform="other">{tree}</ShortcutProvider>;
  const result = render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <ApiContext.Provider value={api}>
          <SessionContext.Provider value={options.session ?? null}>
            <ThemeContext.Provider value={options.theme ?? null}>
              <ToastProvider>{tree}</ToastProvider>
            </ThemeContext.Provider>
          </SessionContext.Provider>
        </ApiContext.Provider>
      </QueryClientProvider>
    </I18nextProvider>,
  );

  const grid = () => within(result.container).getByRole('grid');
  const rows = () =>
    Array.from(result.container.querySelectorAll<HTMLElement>('[role="grid"] [data-th="record"][data-kx-row]'));
  return {
    user,
    navigate,
    store,
    api,
    queryClient,
    config,
    container: result.container,
    route: () => hostRoute ?? initial,
    grid,
    rows,
    cell(rowIndex, field) {
      const row = rows()[rowIndex];
      const cell = row?.querySelector<HTMLElement>(`:scope > [data-kx-col="${field}"]`);
      if (!cell) throw new Error(`No hay celda ${field} en la fila ${rowIndex}`);
      return cell;
    },
    ready: () => waitFor(() => expect(rows().length).toBeGreaterThan(0)).then(() => undefined),
    unmount: result.unmount,
  };
}
