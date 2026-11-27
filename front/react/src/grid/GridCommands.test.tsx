// Acciones de fila y masivas desde `config.commands`: ver, editar, baja con confirmación, órdenes HTTP con
// motivo, 202 «pendiente de aprobación» y errores.
import { screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import manifest from '../../demo/config/manifest.json';
import { resetGridStores } from './store';
import {
  createTestApi,
  mock,
  realConfig,
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
const carrierOverlay = manifest.entities.find((entity) => entity.id === 'carrier')?.overlay;

async function openMenu(h: GridHarness, row: number) {
  await h.user.click(within(h.cell(row, '__actions')).getByRole('button'));
  return screen.findByRole('menu');
}

describe('menú de la fila', () => {
  it('lista las órdenes de la configuración; ver y editar navegan', async () => {
    const h = renderGrid();
    await h.ready();
    const menu = await openMenu(h, 1);
    const items = within(menu).getAllByRole('menuitem').map((item) => item.textContent);
    expect(items).toEqual(['Ver detalle', 'Editar', 'Retirar']);
    await h.user.click(within(menu).getByRole('menuitem', { name: 'Ver detalle' }));
    expect(h.navigate).toHaveBeenCalledWith({ entity: 'product', id: rowId(h, 1), mode: 'read' });

    const again = await openMenu(h, 2);
    await h.user.click(within(again).getByRole('menuitem', { name: 'Editar' }));
    expect(h.navigate).toHaveBeenCalledWith({ entity: 'product', id: rowId(h, 2), mode: 'edit' });
  });

  it('con el teclado: Enter en la celda de acciones abre el menú y Esc devuelve el foco a la celda', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await h.user.click(h.cell(0, '__select'));
    await h.user.keyboard('{End}{Enter}');
    expect(await screen.findByRole('menu')).toBeInTheDocument();
    await h.user.keyboard('{Escape}');
    await waitFor(() => expect(screen.queryByRole('menu')).toBeNull());
    await waitFor(() => expect(h.cell(0, '__actions')).toHaveFocus());
  });

  it('una entidad de sólo lectura sólo ofrece ver', async () => {
    const h = renderGrid({ config: realConfig('country') });
    await h.ready();
    const menu = await openMenu(h, 0);
    expect(within(menu).getAllByRole('menuitem').map((item) => item.textContent)).toEqual(['Ver detalle']);
  });
});

describe('baja con confirmación', () => {
  it('pide confirmar con el mensaje de la configuración y llama a la API', async () => {
    const api = createTestApi();
    const remove = vi.spyOn(api, 'remove');
    const h = renderGrid({ api });
    await h.ready();
    const id = rowId(h, 0);
    const menu = await openMenu(h, 0);
    await h.user.click(within(menu).getByRole('menuitem', { name: 'Retirar' }));
    const dialog = await screen.findByRole('dialog', { name: 'Retirar' });
    expect(within(dialog).getByText(/¿Retirar este producto desde hoy\?/)).toBeInTheDocument();
    expect(remove).not.toHaveBeenCalled();
    await h.user.click(within(dialog).getByRole('button', { name: 'Retirar' }));
    await waitFor(() => expect(remove).toHaveBeenCalledTimes(1));
    expect(remove.mock.calls[0]?.[1]).toBe(id);
    const toasts = await screen.findByRole('region', { name: 'Avisos' });
    expect(await within(toasts).findByText('Retirar: hecho en 1 fila.')).toBeInTheDocument();
  });

  it('cancelar no hace nada', async () => {
    const api = createTestApi();
    const remove = vi.spyOn(api, 'remove');
    const h = renderGrid({ api });
    await h.ready();
    const menu = await openMenu(h, 0);
    await h.user.click(within(menu).getByRole('menuitem', { name: 'Retirar' }));
    const dialog = await screen.findByRole('dialog', { name: 'Retirar' });
    await h.user.click(within(dialog).getByRole('button', { name: 'Cancelar' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(remove).not.toHaveBeenCalled();
  });

  it('un 202 avisa de que queda pendiente de aprobación y marca la fila', async () => {
    setMockOptions({ requireApproval: true });
    const h = renderGrid({ config: realConfig('carrier', carrierOverlay) });
    await h.ready();
    const menu = await openMenu(h, 0);
    await h.user.click(within(menu).getByRole('menuitem', { name: 'Bloquear' }));
    const dialog = await screen.findByRole('dialog', { name: 'Bloquear' });
    await h.user.type(within(dialog).getByRole('textbox'), 'Pendiente de revisar');
    await h.user.click(within(dialog).getByRole('button', { name: 'Bloquear' }));
    const toasts = await screen.findByRole('region', { name: 'Avisos' });
    expect(await within(toasts).findByText('1 fila queda pendiente de aprobación.')).toBeInTheDocument();
    expect(h.rows()[0]).toHaveAttribute('data-kx-state', 'pending');
  });

  it('un error de la orden se avisa con su motivo traducido', async () => {
    server.use(
      http.delete('*/api/v1/products/:id', () =>
        HttpResponse.json({ title: 'Forbidden', status: 403 }, { status: 403, headers: { 'Content-Type': 'application/problem+json' } }),
      ),
    );
    const h = renderGrid();
    await h.ready();
    const menu = await openMenu(h, 0);
    await h.user.click(within(menu).getByRole('menuitem', { name: 'Retirar' }));
    const dialog = await screen.findByRole('dialog', { name: 'Retirar' });
    await h.user.click(within(dialog).getByRole('button', { name: 'Retirar' }));
    const toasts = await screen.findByRole('region', { name: 'Avisos' });
    expect(await within(toasts).findByText(/No tienes permiso para hacer esto\./)).toBeInTheDocument();
  });
});

describe('órdenes HTTP con motivo', () => {
  it('pide el motivo (obligatorio) y envía la versión y el motivo', async () => {
    const api = createTestApi();
    const command = vi.spyOn(api, 'command');
    const h = renderGrid({ api, config: realConfig('carrier', carrierOverlay) });
    await h.ready();
    // la primera fila no está bloqueada: ofrece «Bloquear»
    expect(mock.data.carriers[0]?.blocked).toBeFalsy();
    const menu = await openMenu(h, 0);
    const names = within(menu).getAllByRole('menuitem').map((item) => item.textContent);
    expect(names).toContain('Bloquear');
    expect(names).not.toContain('Desbloquear');
    await h.user.click(within(menu).getByRole('menuitem', { name: 'Bloquear' }));
    const dialog = await screen.findByRole('dialog', { name: 'Bloquear' });
    await h.user.click(within(dialog).getByRole('button', { name: 'Bloquear' }));
    expect(await within(dialog).findByText('Indica un motivo.')).toBeInTheDocument();
    expect(command).not.toHaveBeenCalled();
    await h.user.type(within(dialog).getByRole('textbox'), 'Sin seguro vigente');
    await h.user.click(within(dialog).getByRole('button', { name: 'Bloquear' }));
    await waitFor(() => expect(command).toHaveBeenCalledTimes(1));
    const [, cmd, row, body] = command.mock.calls[0] as unknown as [unknown, { id: string }, { id: string }, Record<string, unknown>];
    expect(cmd.id).toBe('block');
    expect(row.id).toBe(rowId(h, 0));
    expect(body).toEqual({ reason: 'Sin seguro vigente' });
    const toasts = await screen.findByRole('region', { name: 'Avisos' });
    expect(await within(toasts).findByText('Bloquear: hecho en 1 fila.')).toBeInTheDocument();
    await waitFor(() => expect(mock.data.carriers.find((c: { id: string }) => c.id === rowId(h, 0))?.blocked).toBe(true));
  });

  it('una orden con visibleWhen sólo aparece cuando la fila lo cumple', async () => {
    const h = renderGrid({ config: realConfig('carrier', carrierOverlay) });
    await h.ready();
    const blocked = mock.data.carriers.find((carrier) => Boolean((carrier as { blocked?: boolean }).blocked)) as { id: string };
    const index = h.rows().findIndex((row) => row.dataset.kxRow === blocked.id);
    expect(index).toBeGreaterThanOrEqual(0);
    const menu = await openMenu(h, index);
    const names = within(menu).getAllByRole('menuitem').map((item) => item.textContent);
    expect(names).toContain('Desbloquear');
    expect(names).not.toContain('Bloquear');
  });
});

describe('órdenes masivas', () => {
  it('con filas seleccionadas aparece la barra y la orden se aplica a todas', async () => {
    const api = createTestApi();
    const remove = vi.spyOn(api, 'remove');
    const h = renderGrid({ api });
    await h.ready();
    await h.user.click(within(h.cell(0, '__select')).getByRole('checkbox'));
    await h.user.click(within(h.cell(1, '__select')).getByRole('checkbox'));
    const bar = await screen.findByRole('group', { name: 'Filas seleccionadas' });
    expect(within(bar).getByText('2 filas seleccionadas')).toBeInTheDocument();
    // sólo las órdenes de servidor: ni ver ni editar
    expect(within(bar).queryByRole('button', { name: 'Ver detalle' })).toBeNull();
    await h.user.click(within(bar).getByRole('button', { name: 'Retirar' }));
    const dialog = await screen.findByRole('dialog', { name: 'Retirar' });
    await h.user.click(within(dialog).getByRole('button', { name: 'Retirar' }));
    await waitFor(() => expect(remove).toHaveBeenCalledTimes(2));
    expect(remove.mock.calls.map((call) => call[1])).toEqual([rowId(h, 0), rowId(h, 1)]);
    const toasts = await screen.findByRole('region', { name: 'Avisos' });
    expect(await within(toasts).findByText('Retirar: hecho en 2 filas.')).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole('group', { name: 'Filas seleccionadas' })).toBeNull());
  });
});
