// Consulta de la cuadrícula montada: orden sólo por campos de servidor, filtros como chips, búsqueda rápida y
// estado en la URL (`route.params` que se pasa a `navigate`).
import { screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { resetGridStores } from './store';
import { createTestApi, realConfig, renderGrid, setupMockServer, stubLayout } from './test-utils';

setupMockServer();

let restoreLayout: () => void;
beforeEach(() => {
  restoreLayout = stubLayout();
});
afterEach(() => {
  restoreLayout();
  resetGridStores();
  vi.restoreAllMocks();
});

const lastQuery = (list: { mock: { calls: unknown[][] } }) => list.mock.calls.at(-1)?.[1] as { sort?: unknown; filter: Record<string, unknown> };

describe('orden', () => {
  it('la primera carga usa el orden por omisión de la configuración (campo de servidor)', async () => {
    const api = createTestApi();
    const list = vi.spyOn(api, 'list');
    const h = renderGrid({ api, overlay: { saveMode: 'batch' } });
    await h.ready();
    expect(list.mock.calls[0]?.[1]).toMatchObject({ sort: { field: 'sku', direction: 'asc' } });
  });

  it('pulsar una cabecera ordena por servidor: ascendente, descendente y sin orden, y queda en la URL', async () => {
    const api = createTestApi();
    const list = vi.spyOn(api, 'list');
    const h = renderGrid({ api, overlay: { saveMode: 'batch' } });
    await h.ready();
    const header = () => within(h.grid()).getByRole('columnheader', { name: /Nombre/ });

    await h.user.click(header());
    expect(h.route().params).toMatchObject({ sort: 'name:asc' });
    await waitFor(() => expect(lastQuery(list).sort).toEqual({ field: 'name', direction: 'asc' }));
    await waitFor(() => expect(header()).toHaveAttribute('aria-sort', 'ascending'));
    await h.ready();

    await h.user.click(header());
    expect(h.route().params).toMatchObject({ sort: 'name:desc' });
    await waitFor(() => expect(lastQuery(list).sort).toEqual({ field: 'name', direction: 'desc' }));
    await waitFor(() => expect(header()).toHaveAttribute('aria-sort', 'descending'));
    await h.ready();

    await h.user.click(header());
    expect(h.route().params).toMatchObject({ sort: '' });
    await waitFor(() => expect(lastQuery(list).sort).toBeUndefined());
    await waitFor(() => expect(header()).toHaveAttribute('aria-sort', 'none'));
  });

  it('una columna sin campo de orden de servidor no ofrece ordenar', async () => {
    const api = createTestApi();
    const list = vi.spyOn(api, 'list');
    const h = renderGrid({ api, overlay: { saveMode: 'batch' } });
    await h.ready();
    const before = list.mock.calls.length;
    const header = within(h.grid()).getByRole('columnheader', { name: /Precio base/ });
    expect(header).not.toHaveAttribute('data-kx-sortable');
    await h.user.click(header);
    expect(h.route().params).toEqual({});
    expect(list.mock.calls.length).toBe(before);
    // nunca viaja un orden por un campo no admitido
    for (const call of list.mock.calls) {
      const sort = (call[1] as { sort?: { field: string } }).sort;
      expect(sort === undefined || ['sku', 'name'].includes(sort.field)).toBe(true);
    }
  });

  it('un orden de la URL por un campo no admitido se ignora', async () => {
    const api = createTestApi();
    const list = vi.spyOn(api, 'list');
    const h = renderGrid({ api, params: { sort: 'basePrice:desc' }, overlay: { saveMode: 'batch' } });
    await h.ready();
    expect(list.mock.calls[0]?.[1]).not.toHaveProperty('sort');
  });
});

describe('filtros', () => {
  it('se muestran como chips con su valor, viajan al servidor y viven en la URL', async () => {
    const api = createTestApi();
    const list = vi.spyOn(api, 'list');
    const h = renderGrid({ api, overlay: { saveMode: 'batch' } });
    await h.ready();
    await h.user.click(screen.getByRole('button', { name: 'Filtros' }));
    const panel = await screen.findByRole('dialog', { name: 'Filtros' });
    await h.user.selectOptions(within(panel).getByRole('combobox', { name: 'Naturaleza' }), 'service');
    await h.user.click(within(panel).getByRole('button', { name: 'Aplicar' }));

    const filters = h.container.querySelector('[data-kx-part="filters"]') as HTMLElement;
    expect(within(filters).getByText('Naturaleza: Servicio')).toBeInTheDocument();
    expect(h.route().params).toMatchObject({ 'f.kind': 'service' });
    await waitFor(() => expect(lastQuery(list).filter).toMatchObject({ kind: 'service' }));
    await waitFor(() => {
      const kinds = h.rows().map((row) => row.querySelector('[data-kx-col="kind"]')?.textContent);
      expect(kinds.length).toBeGreaterThan(0);
      expect(new Set(kinds)).toEqual(new Set(['Servicio']));
    });
    expect(within(filters).getByRole('button', { name: 'Filtros (1)' })).toBeInTheDocument();
  });

  it('quitar el chip quita el filtro de la consulta y de la URL', async () => {
    const api = createTestApi();
    const list = vi.spyOn(api, 'list');
    const h = renderGrid({ api, params: { 'f.kind': 'good' }, overlay: { saveMode: 'batch' } });
    await h.ready();
    expect(lastQuery(list).filter).toMatchObject({ kind: 'good' });
    await h.user.click(screen.getByRole('button', { name: 'Quitar el filtro Naturaleza' }));
    expect(h.route().params).not.toHaveProperty('f.kind');
    await waitFor(() => expect(lastQuery(list).filter).not.toHaveProperty('kind'));
    expect(screen.queryByText(/Naturaleza: /)).toBeNull();
  });

  it('el editor es el del tipo: sí/no, número y fecha', async () => {
    const holiday = renderGrid({ config: realConfig('holiday') });
    await holiday.ready();
    await holiday.user.click(screen.getByRole('button', { name: 'Filtros' }));
    const panel = await screen.findByRole('dialog', { name: 'Filtros' });
    const year = within(panel).getByRole('spinbutton', { name: /Año/ });
    await holiday.user.type(year, '2026');
    await holiday.user.click(within(panel).getByRole('button', { name: 'Aplicar' }));
    expect(holiday.route().params).toMatchObject({ 'f.year': '2026' });
    holiday.unmount();

    const carrier = renderGrid({ config: realConfig('carrier') });
    await carrier.ready();
    await carrier.user.click(screen.getByRole('button', { name: 'Filtros' }));
    const carrierPanel = await screen.findByRole('dialog', { name: 'Filtros' });
    await carrier.user.selectOptions(within(carrierPanel).getByRole('combobox'), 'true');
    await carrier.user.click(within(carrierPanel).getByRole('button', { name: 'Aplicar' }));
    expect(carrier.route().params).toMatchObject({ 'f.isActive': 'true' });
    expect(await screen.findByText(/: Sí$/)).toBeInTheDocument();
  });

  it('«Vaciar» limpia el borrador del panel y se aplica', async () => {
    const h = renderGrid({ params: { 'f.kind': 'good' }, overlay: { saveMode: 'batch' } });
    await h.ready();
    await h.user.click(screen.getByRole('button', { name: 'Filtros (1)' }));
    const panel = await screen.findByRole('dialog', { name: 'Filtros' });
    await h.user.click(within(panel).getByRole('button', { name: 'Vaciar' }));
    await h.user.click(within(panel).getByRole('button', { name: 'Aplicar' }));
    expect(h.route().params).not.toHaveProperty('f.kind');
  });
});

describe('búsqueda rápida', () => {
  it('con una pequeña espera envía el texto al filtro de búsqueda y lo deja en la URL', async () => {
    const api = createTestApi();
    const list = vi.spyOn(api, 'list');
    const h = renderGrid({ api, overlay: { saveMode: 'batch' } });
    await h.ready();
    const search = screen.getByRole('searchbox', { name: 'Buscar' });
    await h.user.type(search, 'Correa');
    expect(h.route().params).not.toHaveProperty('q'); // todavía no: se espera a que deje de escribir
    await waitFor(() => expect(h.route().params).toMatchObject({ q: 'Correa' }), { timeout: 2000 });
    await waitFor(() => expect(lastQuery(list).filter).toMatchObject({ search: 'Correa' }));
    await waitFor(() => {
      expect(h.rows().length).toBeGreaterThan(0);
      for (const row of h.rows()) expect(row.textContent).toMatch(/Correa/i);
    });
  });

  it('Enter la aplica al momento y Esc la vacía', async () => {
    const h = renderGrid({ params: { q: 'Correa' }, overlay: { saveMode: 'batch' } });
    await h.ready();
    const search = screen.getByRole('searchbox', { name: 'Buscar' });
    expect(search).toHaveValue('Correa');
    await h.user.clear(search);
    await h.user.type(search, 'Sensor{Enter}');
    expect(h.route().params).toMatchObject({ q: 'Sensor' });
    await h.user.type(search, '{Escape}');
    expect(search).toHaveValue('');
    expect(h.route().params).not.toHaveProperty('q');
  });
});
