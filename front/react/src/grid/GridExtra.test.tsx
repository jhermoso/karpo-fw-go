// Casos de borde de la cuadrícula montada: idioma, salida de la rejilla, estados de guardado visibles,
// selección única, almacén compartido entre montajes, formularios y conflicto cancelado.
import { screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { assertTherosHooks } from '../theme';
import { getGridStore, resetGridStores } from './store';
import {
  createTestApi,
  mock,
  renderGrid,
  server,
  setMockOptions,
  setupMockServer,
  stubLayout,
  type GridHarness,
} from './test-utils';

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

const rowId = (h: GridHarness, index: number) => h.rows()[index]?.dataset.kxRow as string;

async function edit(h: GridHarness, row: number, field: string, value: string, finish = '{Enter}') {
  await h.user.click(h.cell(row, field));
  await h.user.keyboard('{F2}');
  await h.user.keyboard('{Control>}a{/Control}');
  await h.user.keyboard(value);
  await h.user.keyboard(finish);
}

describe('idioma', () => {
  it('los textos salen del espacio grid en el idioma activo', async () => {
    const h = renderGrid({ locale: 'en', overlay: { saveMode: 'batch' } });
    await h.ready();
    expect(screen.getByRole('button', { name: 'Filters' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Columns' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Undo' })).toBeInTheDocument();
    expect(h.container.querySelector('[role="status"][aria-live="polite"]')).not.toBeNull();
    await edit(h, 0, 'uom', 'zz', '{Tab}');
    expect(await screen.findByText('1 change')).toBeInTheDocument();
    expect(screen.getByText('50 of 120 rows')).toBeInTheDocument();
    expect(() => assertTherosHooks(h.container, { inlineStyle: 'layout' })).not.toThrow();
  });
});

describe('guardar al salir', () => {
  it('al sacar el foco de la rejilla se guarda la fila activa (optimista)', async () => {
    const api = createTestApi();
    const update = vi.spyOn(api, 'update');
    const h = renderGrid({ api });
    await h.ready();
    await edit(h, 0, 'uom', 'zz', '{Tab}');
    expect(update).not.toHaveBeenCalled();
    await h.user.click(screen.getByRole('searchbox', { name: 'Buscar' }));
    await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
    expect(update.mock.calls[0]?.[2]).toEqual({ uom: 'zz' });
  });

  it('al abrir el registro desde la fila (Mod+Enter) se guarda antes lo pendiente', async () => {
    const api = createTestApi();
    const update = vi.spyOn(api, 'update');
    const h = renderGrid({ api });
    await h.ready();
    await edit(h, 0, 'uom', 'zz', '{Tab}');
    await h.user.keyboard('{Control>}{Enter}{/Control}');
    expect(h.navigate).toHaveBeenCalledWith(expect.objectContaining({ id: rowId(h, 0), mode: 'edit' }));
    await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
  });

  it('Mod+S también guarda todo en modo optimista', async () => {
    const api = createTestApi();
    const update = vi.spyOn(api, 'update');
    const h = renderGrid({ api });
    await h.ready();
    await edit(h, 0, 'uom', 'zz', '{Tab}');
    await h.user.keyboard('{Control>}s{/Control}');
    await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
  });

  it('muestra «guardando» en la fila y en las celdas mientras llega la respuesta', async () => {
    setMockOptions({ latencyMs: 600 });
    const h = renderGrid();
    await h.ready();
    await edit(h, 0, 'uom', 'zz');
    await waitFor(() => expect(h.rows()[0]).toHaveAttribute('data-kx-state', 'saving'));
    expect(h.rows()[0]).toHaveAttribute('aria-busy', 'true');
    expect(h.cell(0, 'uom')).toHaveAttribute('data-kx-cell', 'saving');
    await waitFor(() => expect(h.rows()[0]).toHaveAttribute('data-kx-state', 'saved'), { timeout: 3000 });
    expect(h.cell(0, 'uom')).not.toHaveAttribute('data-kx-cell', 'saving');
    // la marca de «guardada» se apaga sola
    await waitFor(() => expect(h.rows()[0]).not.toHaveAttribute('data-kx-state'), { timeout: 5000 });
  });
});

describe('selección y almacén', () => {
  it('con selectionMode single sólo hay una fila seleccionada y no hay casilla general', async () => {
    const h = renderGrid({ overlay: { grid: { selectionMode: 'single' }, saveMode: 'batch' } });
    await h.ready();
    expect(h.grid()).not.toHaveAttribute('aria-multiselectable');
    expect(within(h.grid()).queryByRole('checkbox', { name: 'Seleccionar todas las filas cargadas' })).toBeNull();
    await h.user.click(within(h.cell(0, '__select')).getByRole('checkbox'));
    await h.user.click(within(h.cell(1, '__select')).getByRole('checkbox'));
    expect(h.rows()[0]).toHaveAttribute('aria-selected', 'false');
    expect(h.rows()[1]).toHaveAttribute('aria-selected', 'true');
    await h.user.click(within(h.cell(1, '__select')).getByRole('checkbox'));
    expect(h.rows()[1]).toHaveAttribute('aria-selected', 'false');
  });

  it('una entidad sin selección no pinta la columna de casillas', async () => {
    const h = renderGrid({ overlay: { grid: { selectable: false } } });
    await h.ready();
    expect(h.grid()).not.toHaveAttribute('data-kx-has-select');
    expect(h.rows()[0]?.querySelector('[data-kx-col="__select"]')).toBeNull();
    expect(h.rows()[0]).not.toHaveAttribute('aria-selected');
    expect(h.grid()).toHaveAttribute('aria-colcount', '7');
  });

  it('los cambios pendientes sobreviven al desmontar si se usa el almacén del registro', async () => {
    const first = renderGrid({ store: getGridStore('product'), overlay: { saveMode: 'batch' } });
    await first.ready();
    await edit(first, 0, 'uom', 'persistente', '{Tab}');
    first.unmount();
    const second = renderGrid({ store: getGridStore('product'), overlay: { saveMode: 'batch' } });
    await second.ready();
    expect(second.cell(0, 'uom').textContent).toBe('persistente');
    expect(second.cell(0, 'uom')).toHaveAttribute('data-kx-cell', 'dirty');
    expect(within(second.container.querySelector('[data-kx-ui="batch-bar"]') as HTMLElement).getByText('1 cambio')).toBeInTheDocument();
  });

  it('una lista nueva con los mismos registros alinea lo ya liquidado y respeta lo pendiente', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await edit(h, 0, 'uom', 'mío', '{Tab}');
    await h.queryClient.invalidateQueries({ queryKey: ['entity', 'product', 'list'] });
    await waitFor(() => expect(h.queryClient.isFetching()).toBe(0));
    expect(h.cell(0, 'uom').textContent).toBe('mío');
    expect(h.cell(0, 'uom')).toHaveAttribute('data-kx-cell', 'dirty');
  });
});

describe('conflicto cancelado y elección propia', () => {
  async function conflict(h: GridHarness) {
    const id = rowId(h, 0);
    await edit(h, 0, 'name', 'Mío', '{Tab}');
    server.use(http.put(`*/api/v1/products/${id}`, () => HttpResponse.json({ title: 'x', status: 409 }, { status: 409, headers: { 'Content-Type': 'application/problem+json' } }), { once: true }));
    await h.user.keyboard('{Control>}s{/Control}');
    await waitFor(() => expect(h.rows()[0]).toHaveAttribute('data-kx-state', 'conflict'));
    const stored = mock.data.products.find((p: { id: string }) => p.id === id) as Record<string, unknown>;
    stored.name = 'Del servidor';
    stored.version = (stored.version as number) + 1;
    await h.user.click(await screen.findByRole('button', { name: /Recargar y reaplicar/ }));
    return screen.findByRole('dialog', { name: 'Los dos habéis cambiado lo mismo' });
  }

  it('cancelar el diálogo deja la fila en conflicto', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    const dialog = await conflict(h);
    await h.user.click(within(dialog).getByRole('button', { name: 'Cancelar' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(h.rows()[0]).toHaveAttribute('data-kx-state', 'conflict');
  });

  it('conservar los míos reaplica mis valores sobre la versión nueva', async () => {
    const api = createTestApi();
    const update = vi.spyOn(api, 'update');
    const h = renderGrid({ api, overlay: { saveMode: 'batch' } });
    await h.ready();
    const dialog = await conflict(h);
    await h.user.click(within(dialog).getByRole('button', { name: 'Conservar los míos' }));
    await waitFor(() => expect(h.rows()[0]).not.toHaveAttribute('data-kx-state'));
    expect(h.cell(0, 'name').textContent).toBe('Mío');
    await h.user.click(h.cell(0, 'name'));
    await h.user.keyboard('{Control>}s{/Control}');
    await waitFor(() => expect(update).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(mock.data.products.find((p: { id: string }) => p.id === rowId(h, 0))?.name).toBe('Mío'));
  });
});

describe('sólo formulario', () => {
  it('con saveMode form no hay edición en línea, ni barra de cambios, ni deshacer', async () => {
    const h = renderGrid({ overlay: { saveMode: 'form' } });
    await h.ready();
    await h.user.click(h.cell(0, 'name'));
    await h.user.keyboard('{F2}');
    await h.user.keyboard('x');
    expect(within(h.cell(0, 'name')).queryByRole('textbox')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Deshacer' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Nueva fila' })).toBeNull();
    expect(h.cell(0, 'name')).not.toHaveAttribute('aria-readonly');
    expect(h.container.querySelector('[data-kx-ui="batch-bar"]')).toHaveAttribute('hidden');
  });
});
