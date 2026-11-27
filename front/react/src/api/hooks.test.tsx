// Ganchos de react-query sobre el adaptador, montados contra MSW.
import { QueryClient, QueryClientProvider, type InfiniteData } from '@tanstack/react-query';
import { act, render, renderHook, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import type { PropsWithChildren } from 'react';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { normalizeEntityConfig } from '../config';
import { readRealById } from '../config/__fixtures__/real-configs';
import {
  ApiContext,
  ManifestContext,
  queryKeys,
  type AppManifest,
  type EntityApi,
  type ListQuery,
  type Page,
  type Row,
} from '../contracts';
import { API, createGoMock } from './__fixtures__/go-server';
import { createEntityApi } from './entity-api';
import {
  flattenPages,
  patchEntityCaches,
  useEntityApi,
  useEntityApiExt,
  useEntityList,
  useEntityPage,
  useEntityRecord,
  useLookupLabel,
} from './hooks';
import { createHttpClient } from './http';

const mock = createGoMock();
const { server, state } = mock;
const facility = normalizeEntityConfig(readRealById('facility'));
const manifest = { schemaVersion: 1, entities: { facility } } as unknown as AppManifest;

beforeAll(() => server.listen({ onUnhandledFrame: 'error' }));
afterAll(() => server.close());
afterEach(() => server.resetHandlers());
beforeEach(() => {
  mock.calls.length = 0;
  state.validAccess = 'access-1';
  state.facilities = Array.from({ length: 5 }, (_v, i) => ({
    id: `f${i + 1}`,
    name: `Instalación ${i + 1}`,
    type: 't',
    partOf: '',
    active: true,
    version: 1,
  }));
});

function setup() {
  const client = createHttpClient({
    baseUrl: API,
    getToken: () => 'access-1',
    refresh: async () => undefined,
    getScope: () => undefined,
    onUnauthorized: () => undefined,
    sleep: async () => undefined,
  });
  const api: EntityApi = createEntityApi(client);
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper({ children }: PropsWithChildren) {
    return (
      <QueryClientProvider client={queryClient}>
        <ApiContext.Provider value={api}>
          <ManifestContext.Provider value={manifest}>{children}</ManifestContext.Provider>
        </ApiContext.Provider>
      </QueryClientProvider>
    );
  }
  return { Wrapper, queryClient, api };
}

/** react-query sólo avisa de las propiedades que se leen al pintar: se leen todas. */
const all = <T extends object>(result: T): T => ({ ...result });

const query: ListQuery = { page: 1, pageSize: 2, sort: { field: 'name', direction: 'asc' }, filter: {} };

describe('useEntityList', () => {
  it('pagina con desplazamiento infinito y usa las claves de contracts.queryKeys', async () => {
    const { Wrapper, queryClient } = setup();
    const { result } = renderHook(() => all(useEntityList(facility, query)), { wrapper: Wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(flattenPages(result.current.data).map((r) => r.id)).toEqual(['f1', 'f2']);
    expect(result.current.hasNextPage).toBe(true);
    expect(queryClient.getQueryCache().find({ queryKey: queryKeys.list('facility', query) })).toBeDefined();

    await act(async () => {
      await result.current.fetchNextPage();
    });
    await waitFor(() => expect(flattenPages(result.current.data)).toHaveLength(4));
    await act(async () => {
      await result.current.fetchNextPage();
    });
    await waitFor(() => expect(flattenPages(result.current.data).map((r) => r.id)).toEqual(['f1', 'f2', 'f3', 'f4', 'f5']));
    expect(result.current.hasNextPage).toBe(false);
    const pages = mock.callsTo('POST', '/api/v1/facilities/search').map((c) => (c.body as { pagination: { pageNumber: number } }).pagination.pageNumber);
    expect(pages).toEqual([1, 2, 3]);
    expect(flattenPages(undefined)).toEqual([]);
  });

  it('useEntityPage lee una sola página bajo el mismo prefijo de claves', async () => {
    const { Wrapper, queryClient } = setup();
    const { result } = renderHook(() => all(useEntityPage(facility, query)), { wrapper: Wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data!.total).toBe(5);
    expect(queryClient.getQueryCache().findAll({ queryKey: ['entity', 'facility', 'list'] })).toHaveLength(1);
  });
});

describe('useEntityRecord', () => {
  it('carga un registro y no consulta sin identificador', async () => {
    const { Wrapper, queryClient } = setup();
    const { result, rerender } = renderHook(({ id }: { id: string | undefined }) => all(useEntityRecord(facility, id)), {
      wrapper: Wrapper,
      initialProps: { id: undefined as string | undefined },
    });
    expect(result.current.fetchStatus).toBe('idle');
    expect(mock.calls).toHaveLength(0);
    rerender({ id: 'f2' });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toMatchObject({ id: 'f2', name: 'Instalación 2' });
    expect(queryClient.getQueryData(queryKeys.record('facility', 'f2'))).toBeDefined();
    rerender({ id: '' });
    expect(result.current.fetchStatus).toBe('idle');
  });
});

describe('patchEntityCaches', () => {
  it('escribe el registro nuevo en la caché de registro y en las de lista (infinita y de página) sin releer', async () => {
    const { Wrapper, queryClient } = setup();
    const list = renderHook(() => all(useEntityList(facility, query)), { wrapper: Wrapper });
    const page = renderHook(() => all(useEntityPage(facility, query)), { wrapper: Wrapper });
    const record = renderHook(() => all(useEntityRecord(facility, 'f1')), { wrapper: Wrapper });
    await waitFor(() => {
      expect(list.result.current.isSuccess && page.result.current.isSuccess && record.result.current.isSuccess).toBe(true);
    });
    const before = mock.calls.length;
    act(() => patchEntityCaches(queryClient, 'facility', { id: 'f1', name: 'Renombrada', version: 2 } as Row));
    await waitFor(() => {
      expect(flattenPages(list.result.current.data)[0]).toMatchObject({ id: 'f1', name: 'Renombrada', version: 2, type: 't' });
      expect(page.result.current.data!.items[0]).toMatchObject({ name: 'Renombrada' });
      expect(record.result.current.data).toMatchObject({ name: 'Renombrada', version: 2 });
    });
    expect(mock.calls).toHaveLength(before);
    // cachés ajenas o con otra forma no se tocan
    queryClient.setQueryData(['entity', 'facility', 'list', 'raro'], 'texto');
    queryClient.setQueryData(['entity', 'facility', 'list', 'nulo'], null);
    queryClient.setQueryData(['entity', 'facility', 'list', 'otro'], { otra: 1 });
    act(() => patchEntityCaches(queryClient, 'facility', { id: 'f9' } as Row));
    expect(queryClient.getQueryData(['entity', 'facility', 'list', 'raro'])).toBe('texto');
    expect(queryClient.getQueryData(['entity', 'facility', 'list', 'otro'])).toEqual({ otra: 1 });
    const data = queryClient.getQueryData<InfiniteData<Page>>(queryKeys.list('facility', query));
    expect(data!.pages[0]!.items).toHaveLength(2);
  });
});

describe('useEntityApi', () => {
  it('sin proveedor da un error claro y con él devuelve la API (también con las rutas auxiliares)', () => {
    expect(() => renderHook(() => useEntityApi())).toThrow(/ApiContext/);
    const { Wrapper, api } = setup();
    expect(renderHook(() => useEntityApi(), { wrapper: Wrapper }).result.current).toBe(api);
    expect(typeof renderHook(() => useEntityApiExt(), { wrapper: Wrapper }).result.current.catalog).toBe('function');
  });
});

describe('useLookupLabel', () => {
  function Label({ id }: { id: string | undefined }) {
    const { label, isLoading } = useLookupLabel('facility', id);
    return <li>{isLoading ? 'cargando' : (label ?? 'sin etiqueta')}</li>;
  }

  it('resuelve etiquetas del mismo fotograma con una petición por identificador distinto', async () => {
    const { Wrapper, queryClient } = setup();
    render(
      <Wrapper>
        <ul>
          <Label id="f1" />
          <Label id="f1" />
          <Label id="f2" />
          <Label id={undefined} />
        </ul>
      </Wrapper>,
    );
    expect(screen.getAllByText('cargando')).toHaveLength(3);
    await waitFor(() => expect(screen.getAllByText(/Instalación/)).toHaveLength(3));
    const gets = mock.calls.filter((c) => c.method === 'GET' && c.path.startsWith('/api/v1/facilities/'));
    expect(gets.map((c) => c.path).sort()).toEqual(['/api/v1/facilities/f1', '/api/v1/facilities/f2']);
    expect(screen.getByText('sin etiqueta')).toBeInTheDocument();
    expect(queryClient.getQueryData(queryKeys.label('facility', 'f1'))).toBe('Instalación 1');
  });

  it('con muchos identificadores se lee una página en lugar de uno a uno', async () => {
    const { Wrapper } = setup();
    render(
      <Wrapper>
        <ul>
          {['f1', 'f2', 'f3', 'f4', 'f5'].map((id) => (
            <Label key={id} id={id} />
          ))}
        </ul>
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getAllByText(/Instalación/)).toHaveLength(5));
    expect(mock.callsTo('POST', '/api/v1/facilities/search')).toHaveLength(1);
    expect(mock.calls.filter((c) => c.method === 'GET')).toHaveLength(0);
  });

  it('un registro que no existe da «sin etiqueta» y sin proveedor de API no consulta', async () => {
    server.use(http.get(`${API}/v1/facilities/nope`, () => HttpResponse.json({ title: 'Not Found' }, { status: 404 })));
    const { Wrapper } = setup();
    render(
      <Wrapper>
        <ul>
          <Label id="nope" />
        </ul>
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByText('sin etiqueta')).toBeInTheDocument());
    const client = new QueryClient();
    const { result } = renderHook(() => useLookupLabel('facility', 'f1'), {
      wrapper: ({ children }: PropsWithChildren) => <QueryClientProvider client={client}>{children}</QueryClientProvider>,
    });
    expect(result.current).toEqual({ label: undefined, isLoading: false });
  });
});
