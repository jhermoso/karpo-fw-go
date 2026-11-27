// Edición en la cuadrícula montada: validación por celda, deshacer y rehacer, guardado optimista y por
// lotes con `version`, conflicto 409, pegado desde una hoja de cálculo, relleno y vaciado.
import { screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flattenPages } from '../api';
import type { Page, Row } from '../contracts';
import { resetGridStores } from './store';
import {
  createTestApi,
  mock,
  realConfig,
  renderGrid,
  server,
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

const REQUIRED_NAME = { grid: { columns: [{ field: 'name', required: true }] } };
const rowId = (h: GridHarness, index: number) => h.rows()[index]?.dataset.kxRow as string;
const problem = (status: number, extra: Record<string, unknown> = {}) =>
  HttpResponse.json({ title: 'Error', status, ...extra }, { status, headers: { 'Content-Type': 'application/problem+json' } });

async function edit(h: GridHarness, row: number, field: string, value: string, finish = '{Enter}') {
  await h.user.click(h.cell(row, field));
  await h.user.keyboard('{F2}');
  await h.user.keyboard('{Control>}a{/Control}');
  await h.user.keyboard(value === '' ? '{Backspace}' : value);
  await h.user.keyboard(finish);
}

describe('validación por celda', () => {
  it('un valor inválido marca data-kx-cell=invalid, muestra el mensaje traducido y se queda en la celda', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch', ...REQUIRED_NAME } });
    await h.ready();
    await edit(h, 1, 'name', '');
    const cell = h.cell(1, 'name');
    expect(cell).toHaveAttribute('data-kx-cell', 'invalid');
    expect(cell).toHaveAttribute('aria-invalid', 'true');
    const message = within(cell).getByText('Este campo es obligatorio.');
    expect(cell.getAttribute('aria-describedby')).toBe(message.id);
    expect(cell).toHaveFocus(); // no avanza: se ve el error y Esc lo revierte
  });

  it('Esc revierte la celda inválida a su valor', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch', ...REQUIRED_NAME } });
    await h.ready();
    const original = h.cell(1, 'name').textContent;
    await edit(h, 1, 'name', '');
    expect(h.cell(1, 'name')).toHaveAttribute('data-kx-cell', 'invalid');
    await h.user.keyboard('{Escape}');
    expect(h.cell(1, 'name')).not.toHaveAttribute('data-kx-cell', 'invalid');
    expect(h.cell(1, 'name')).not.toHaveAttribute('aria-invalid');
    expect(h.cell(1, 'name').textContent).toBe(original);
    expect(h.store.getState().issues).toEqual({});
  });

  it('un número que no se entiende queda marcado como inválido con su texto', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await edit(h, 0, 'basePrice', 'abc');
    const cell = h.cell(0, 'basePrice');
    expect(cell).toHaveAttribute('data-kx-cell', 'invalid');
    expect(within(cell).getAllByText('abc').length).toBeGreaterThan(0);
    expect(within(cell).getByText('El valor no tiene un formato válido.')).toBeInTheDocument();
  });

  it('un valor correcto deja la celda sucia y la fila con data-th-state=dirty', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await edit(h, 2, 'uom', 'kg', '{Tab}');
    expect(h.cell(2, 'uom')).toHaveAttribute('data-kx-cell', 'dirty');
    expect(h.cell(2, 'uom').textContent).toBe('kg');
    expect(h.rows()[2]).toHaveAttribute('data-th-state', 'dirty');
    // y el foco avanzó a la celda siguiente de la misma fila
    expect(h.cell(2, 'basePrice')).toHaveFocus();
  });

  it('la región viva resume los cambios pendientes y los errores', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch', ...REQUIRED_NAME } });
    await h.ready();
    await edit(h, 0, 'uom', 'kg', '{Tab}');
    await edit(h, 1, 'name', '');
    const live = h.container.querySelector('[data-kx-part="live"]') as HTMLElement;
    expect(live).toHaveAttribute('aria-live', 'polite');
    expect(live).toHaveAttribute('role', 'status');
    expect(live.textContent).toContain('2 cambios pendientes, 1 con error.');
  });
});

describe('deshacer y rehacer', () => {
  it('Mod+Z deshace y Mod+Shift+Z rehace una edición', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    const original = h.cell(0, 'name').textContent;
    await edit(h, 0, 'name', 'Nuevo nombre', '{Tab}');
    expect(h.cell(0, 'name').textContent).toBe('Nuevo nombre');
    await h.user.keyboard('{Control>}z{/Control}');
    expect(h.cell(0, 'name').textContent).toBe(original);
    expect(h.cell(0, 'name')).not.toHaveAttribute('data-kx-cell', 'dirty');
    await h.user.keyboard('{Control>}{Shift>}z{/Shift}{/Control}');
    expect(h.cell(0, 'name').textContent).toBe('Nuevo nombre');
    expect(h.cell(0, 'name')).toHaveAttribute('data-kx-cell', 'dirty');
    await h.user.keyboard('{Control>}z{/Control}{Control>}y{/Control}');
    expect(h.cell(0, 'name').textContent).toBe('Nuevo nombre');
  });

  it('deshacer también quita la marca de inválido', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch', ...REQUIRED_NAME } });
    await h.ready();
    await edit(h, 0, 'name', '');
    expect(h.cell(0, 'name')).toHaveAttribute('data-kx-cell', 'invalid');
    await h.user.keyboard('{Control>}z{/Control}');
    expect(h.cell(0, 'name')).not.toHaveAttribute('aria-invalid');
  });

  it('los botones de la barra deshacen y rehacen y se desactivan cuando no hay nada', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    const undo = screen.getByRole('button', { name: 'Deshacer' });
    const redo = screen.getByRole('button', { name: 'Rehacer' });
    expect(undo).toHaveAttribute('aria-disabled', 'true');
    await edit(h, 0, 'uom', 'zz', '{Tab}');
    expect(undo).not.toHaveAttribute('aria-disabled');
    await h.user.click(undo);
    expect(h.cell(0, 'uom')).not.toHaveAttribute('data-kx-cell', 'dirty');
    expect(redo).not.toHaveAttribute('aria-disabled');
    await h.user.click(redo);
    expect(h.cell(0, 'uom').textContent).toBe('zz');
  });
});

describe('guardado optimista', () => {
  it('al salir de la fila envía un PUT con version y escribe la fila devuelta en la caché', async () => {
    const api = createTestApi();
    const update = vi.spyOn(api, 'update');
    const h = renderGrid({ api });
    await h.ready();
    const id = rowId(h, 0);
    const version = h.store.getState().server[id]?.version;
    expect(typeof version).toBe('number');
    await edit(h, 0, 'name', 'Guardado optimista');
    await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
    expect(update.mock.calls[0]?.[1]).toBe(id);
    expect(update.mock.calls[0]?.[2]).toEqual({ name: 'Guardado optimista' });
    expect(update.mock.calls[0]?.[3]).toBe(version);
    await waitFor(() => expect(h.rows()[0]).toHaveAttribute('data-kx-state', 'saved'));
    // la fila que devolvió el servidor (versión +1) está en la caché de la lista
    const cached = h.queryClient.getQueriesData<{ pages: Page[] }>({ queryKey: ['entity', 'product', 'list'] });
    const rows = cached.flatMap(([, data]) => flattenPages(data as never));
    expect(rows.find((row: Row) => row.id === id)).toMatchObject({ name: 'Guardado optimista', version: (version as number) + 1 });
    expect(h.store.getState().server[id]?.name).toBe('Guardado optimista');
    expect(h.cell(0, 'name')).not.toHaveAttribute('data-kx-cell', 'dirty');
    expect(h.rows()[0]).not.toHaveAttribute('data-th-state');
    // el servidor guardó
    expect(mock.data.products.find((p: { id: string }) => p.id === id)?.name).toBe('Guardado optimista');
  });

  it('no guarda mientras se sigue en la misma fila', async () => {
    const api = createTestApi();
    const update = vi.spyOn(api, 'update');
    const h = renderGrid({ api });
    await h.ready();
    await edit(h, 0, 'name', 'Uno', '{Tab}');
    await edit(h, 0, 'uom', 'zz', '{Tab}');
    expect(update).not.toHaveBeenCalled();
    await h.user.keyboard('{ArrowDown}');
    await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
    expect(update.mock.calls[0]?.[2]).toEqual({ name: 'Uno', uom: 'zz' });
  });

  it('ante un 500 revierte la fila y ofrece Deshacer en el aviso', async () => {
    const h = renderGrid();
    await h.ready();
    const original = h.cell(0, 'name').textContent;
    server.use(http.put('*/api/v1/products/:id', () => problem(500)));
    await edit(h, 0, 'name', 'No se va a guardar');
    await waitFor(() => expect(h.cell(0, 'name').textContent).toBe(original));
    expect(h.rows()[0]).toHaveAttribute('data-kx-state', 'error');
    const toasts = await screen.findByRole('region', { name: 'Avisos' });
    expect(within(toasts).getByText(/No se ha podido guardar/)).toBeInTheDocument();
    await h.user.click(within(toasts).getByRole('button', { name: 'Deshacer' }));
    expect(h.cell(0, 'name').textContent).toBe('No se va a guardar');
    expect(h.cell(0, 'name')).toHaveAttribute('data-kx-cell', 'dirty');
  });

  it('un error de campo del servidor se marca en la celda sin revertir', async () => {
    const h = renderGrid();
    await h.ready();
    server.use(
      http.put('*/api/v1/products/:id', () =>
        problem(400, { errors: { name: ['duplicate'] }, errorCodes: { name: ['duplicate'] } }),
      ),
    );
    await edit(h, 0, 'name', 'Repetido');
    await waitFor(() => expect(h.cell(0, 'name')).toHaveAttribute('data-kx-cell', 'error'));
    expect(h.cell(0, 'name').textContent).toContain('Repetido');
    expect(within(h.cell(0, 'name')).getByText('Ya existe un registro con este valor.')).toBeInTheDocument();
    expect(h.rows()[0]).toHaveAttribute('data-kx-state', 'error');
    // corregir la celda retira el error del servidor
    await h.user.click(h.cell(0, 'name'));
    await edit(h, 0, 'name', 'Otro', '{Tab}');
    expect(h.cell(0, 'name')).toHaveAttribute('data-kx-cell', 'dirty');
  });

  it('no envía una fila con celdas inválidas', async () => {
    const api = createTestApi();
    const update = vi.spyOn(api, 'update');
    const h = renderGrid({ api, overlay: REQUIRED_NAME });
    await h.ready();
    await edit(h, 0, 'name', '');
    await h.user.keyboard('{Escape}{Escape}');
    await edit(h, 0, 'basePrice', 'xx', '{Tab}');
    await h.user.keyboard('{ArrowDown}');
    expect(update).not.toHaveBeenCalled();
    expect(h.cell(0, 'basePrice')).toHaveAttribute('data-kx-cell', 'invalid');
  });

  it('deshacer algo ya guardado envía otro PUT', async () => {
    const api = createTestApi();
    const update = vi.spyOn(api, 'update');
    const h = renderGrid({ api });
    await h.ready();
    const original = h.cell(0, 'name').textContent;
    await edit(h, 0, 'name', 'Primero');
    await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(h.rows()[0]).toHaveAttribute('data-kx-state', 'saved'));
    await h.user.keyboard('{Control>}z{/Control}');
    await waitFor(() => expect(update).toHaveBeenCalledTimes(2));
    expect(update.mock.calls[1]?.[2]).toEqual({ name: original });
    // la versión del segundo PUT es la que devolvió el primero
    expect(update.mock.calls[1]?.[3]).toBe((update.mock.calls[0]?.[3] as number) + 1);
    await waitFor(() => expect(h.cell(0, 'name').textContent).toBe(original));
  });

  it('una fila nueva se rellena en la cuadrícula y se crea al salir de ella', async () => {
    const api = createTestApi();
    const create = vi.spyOn(api, 'create');
    const h = renderGrid({ api, config: realConfig('carrier') });
    await h.ready();
    await h.user.click(screen.getByRole('button', { name: 'Nueva fila' }));
    const first = h.rows()[0] as HTMLElement;
    expect(first).toHaveAttribute('data-th-state', 'dirty new');
    // el editor de la primera celda editable está abierto
    expect(within(first).getByRole('textbox')).toHaveFocus();
    await h.user.keyboard('TRX{Tab}');
    await h.user.keyboard('Transportes X{Enter}');
    await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
    expect(create.mock.calls[0]?.[1]).toMatchObject({ code: 'TRX', name: 'Transportes X' });
    await waitFor(() => expect(h.store.getState().newRows).toEqual([]));
  });

  it('sólo ofrece «Nueva fila» si el formulario no exige campos que la cuadrícula no puede editar', async () => {
    const h = renderGrid();
    await h.ready();
    // el producto exige la naturaleza (`kind`), que en la lista es de sólo lectura: el alta va por el formulario
    expect(screen.queryByRole('button', { name: 'Nueva fila' })).toBeNull();
  });
});

describe('guardado por lotes', () => {
  it('cuenta los cambios y Mod+S envía los PUT en secuencia con informe por fila', async () => {
    const api = createTestApi();
    const update = vi.spyOn(api, 'update');
    const h = renderGrid({ api, overlay: { saveMode: 'batch' } });
    await h.ready();
    await edit(h, 0, 'name', 'Lote uno', '{Tab}');
    await edit(h, 2, 'uom', 'lt', '{Tab}');
    await edit(h, 2, 'basePrice', '99,5', '{Tab}');
    const bar = h.container.querySelector('[data-kx-ui="batch-bar"]') as HTMLElement;
    expect(bar).not.toHaveAttribute('hidden');
    expect(within(bar).getByText('3 cambios')).toBeInTheDocument();
    expect(within(bar).getByRole('button', { name: /Guardar/ })).toBeInTheDocument();
    expect(update).not.toHaveBeenCalled();
    await h.user.keyboard('{Control>}s{/Control}');
    await waitFor(() => expect(update).toHaveBeenCalledTimes(2));
    expect(update.mock.calls.map((call) => call[1])).toEqual([rowId(h, 0), rowId(h, 2)]);
    expect(update.mock.calls[1]?.[2]).toEqual({ uom: 'lt', basePrice: 99.5 });
    const toasts = await screen.findByRole('region', { name: 'Avisos' });
    await within(toasts).findByText('2 filas guardadas');
    await waitFor(() => expect(bar).toHaveAttribute('hidden'));
    expect(h.store.getState().server[rowId(h, 0)]?.name).toBe('Lote uno');
  });

  it('el botón Guardar y Descartar de la barra', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await edit(h, 0, 'uom', 'zz', '{Tab}');
    const bar = h.container.querySelector('[data-kx-ui="batch-bar"]') as HTMLElement;
    await h.user.click(within(bar).getByRole('button', { name: 'Descartar' }));
    expect(h.cell(0, 'uom')).not.toHaveAttribute('data-kx-cell', 'dirty');
    expect(bar).toHaveAttribute('hidden');
    // descartar se puede deshacer
    await h.user.click(h.cell(0, 'uom'));
    await h.user.keyboard('{Control>}z{/Control}');
    expect(h.cell(0, 'uom').textContent).toBe('zz');
    await h.user.click(within(bar).getByRole('button', { name: /Guardar/ }));
    await waitFor(() => expect(bar).toHaveAttribute('hidden'));
    expect(mock.data.products.find((p: { id: string }) => p.id === rowId(h, 0))?.uom).toBe('zz');
  });

  it('un fallo no revierte: la fila queda pendiente y el informe cuenta lo que falló', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await edit(h, 0, 'name', 'Falla', '{Tab}');
    await edit(h, 1, 'name', 'Funciona', '{Tab}');
    const failing = rowId(h, 0);
    server.use(http.put(`*/api/v1/products/${failing}`, () => problem(500)));
    await h.user.keyboard('{Control>}s{/Control}');
    const bar = h.container.querySelector('[data-kx-ui="batch-bar"]') as HTMLElement;
    await within(bar).findByText('1 guardadas, 1 con problemas');
    expect(h.rows()[0]).toHaveAttribute('data-kx-state', 'error');
    expect(h.cell(0, 'name').textContent).toBe('Falla');
    expect(within(bar).getByText('1 cambio')).toBeInTheDocument();
    await h.user.click(within(bar).getByRole('button', { name: 'Cerrar el informe' }));
    expect(within(bar).queryByText('1 guardadas, 1 con problemas')).toBeNull();
  });
});

describe('conflicto 409', () => {
  it('deja la fila en conflicto y «Recargar y reaplicar» vuelve a leer y reaplica si no hay campos en común', async () => {
    const api = createTestApi();
    const update = vi.spyOn(api, 'update');
    const get = vi.spyOn(api, 'get');
    const h = renderGrid({ api, overlay: { saveMode: 'batch' } });
    await h.ready();
    const id = rowId(h, 0);
    await edit(h, 0, 'name', 'Mío', '{Tab}');
    server.use(http.put(`*/api/v1/products/${id}`, () => problem(409, { code: 'conflict' }), { once: true }));
    await h.user.keyboard('{Control>}s{/Control}');
    await waitFor(() => expect(h.rows()[0]).toHaveAttribute('data-kx-state', 'conflict'));
    // otro usuario cambió OTRO campo mientras tanto
    const stored = mock.data.products.find((p: { id: string }) => p.id === id) as Record<string, unknown>;
    stored.uom = 'otro';
    stored.version = (stored.version as number) + 1;

    await h.user.click(await screen.findByRole('button', { name: /Recargar y reaplicar/ }));
    await waitFor(() => expect(get).toHaveBeenCalledWith(expect.anything(), id));
    await waitFor(() => expect(h.rows()[0]).not.toHaveAttribute('data-kx-state'));
    expect(h.cell(0, 'name').textContent).toBe('Mío');
    expect(h.cell(0, 'name')).toHaveAttribute('data-kx-cell', 'dirty');
    expect(h.cell(0, 'uom').textContent).toBe('otro');
    expect(screen.queryByRole('button', { name: /Recargar y reaplicar/ })).toBeNull();

    // ahora el PUT lleva la versión nueva y entra
    const bar = h.container.querySelector('[data-kx-ui="batch-bar"]') as HTMLElement;
    const reread = stored.version;
    await h.user.click(within(bar).getByRole('button', { name: /Guardar/ }));
    await waitFor(() => expect(update.mock.calls.length).toBe(2));
    expect(update.mock.calls[1]?.[3]).toBe(reread);
    await waitFor(() => expect(h.store.getState().server[id]?.name).toBe('Mío'));
  });

  it('si coinciden los campos pregunta con qué valores quedarse', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    const id = rowId(h, 0);
    await edit(h, 0, 'name', 'Mío', '{Tab}');
    server.use(http.put(`*/api/v1/products/${id}`, () => problem(409), { once: true }));
    await h.user.keyboard('{Control>}s{/Control}');
    await waitFor(() => expect(h.rows()[0]).toHaveAttribute('data-kx-state', 'conflict'));
    const stored = mock.data.products.find((p: { id: string }) => p.id === id) as Record<string, unknown>;
    stored.name = 'Del servidor';
    stored.version = (stored.version as number) + 1;

    await h.user.click(await screen.findByRole('button', { name: /Recargar y reaplicar/ }));
    const dialog = await screen.findByRole('dialog', { name: 'Los dos habéis cambiado lo mismo' });
    expect(within(dialog).getByText('Mío')).toBeInTheDocument();
    expect(within(dialog).getByText('Del servidor')).toBeInTheDocument();
    await h.user.click(within(dialog).getByRole('button', { name: 'Usar los del servidor' }));
    await waitFor(() => expect(h.cell(0, 'name').textContent).toBe('Del servidor'));
    expect(h.cell(0, 'name')).not.toHaveAttribute('data-kx-cell', 'dirty');
    expect(h.rows()[0]).not.toHaveAttribute('data-kx-state');
  });

  it('en optimista, tras reaplicar sin campos en común vuelve a guardar sola', async () => {
    const api = createTestApi();
    const update = vi.spyOn(api, 'update');
    const h = renderGrid({ api });
    await h.ready();
    const id = rowId(h, 0);
    server.use(http.put(`*/api/v1/products/${id}`, () => problem(409), { once: true }));
    await edit(h, 0, 'name', 'Reintento');
    await waitFor(() => expect(h.rows()[0]).toHaveAttribute('data-kx-state', 'conflict'));
    await h.user.click(await screen.findByRole('button', { name: /Recargar y reaplicar/ }));
    await waitFor(() => expect(update).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(h.store.getState().server[id]?.name).toBe('Reintento'));
  });
});

describe('pegar, rellenar y vaciar', () => {
  it('pegar un TSV de 2×3 abre la vista previa y marca las celdas con error antes de aplicar', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await h.user.click(h.cell(1, 'uom'));
    await h.user.paste('kg\t12,5\tX\nlt\tabc\tY');
    const dialog = await screen.findByRole('dialog', { name: 'Pegar desde una hoja de cálculo' });
    expect(within(dialog).getByText('3 celdas correctas, 1 con error, 2 no editables.')).toBeInTheDocument();
    const table = within(dialog).getByRole('table');
    const cells = within(table).getAllByRole('cell');
    expect(cells).toHaveLength(6);
    expect(cells.map((cell) => cell.getAttribute('data-kx-paste'))).toEqual(['ok', 'ok', 'skipped', 'ok', 'error', 'skipped']);
    expect(within(cells[4] as HTMLElement).getByText('El valor no tiene un formato válido.')).toBeInTheDocument();
    // todavía no se ha tocado nada
    expect(h.cell(1, 'uom')).not.toHaveAttribute('data-kx-cell', 'dirty');
    await h.user.click(within(dialog).getByRole('button', { name: 'Pegar 3 celdas' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(h.cell(1, 'uom').textContent).toBe('kg');
    expect(h.cell(1, 'basePrice').textContent).toBe('12,5');
    expect(h.cell(2, 'uom').textContent).toBe('lt');
    expect(h.cell(2, 'basePrice')).not.toHaveAttribute('data-kx-cell', 'dirty');
    // un solo paso para deshacerlo todo
    await h.user.click(h.cell(1, 'uom'));
    await h.user.keyboard('{Control>}z{/Control}');
    expect(h.store.getState().undoStack).toHaveLength(0);
    expect(h.cell(1, 'uom')).not.toHaveAttribute('data-kx-cell', 'dirty');
    expect(h.cell(2, 'uom')).not.toHaveAttribute('data-kx-cell', 'dirty');
  });

  it('pegar un solo valor lo aplica directamente; si no se entiende queda inválido', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await h.user.click(h.cell(0, 'uom'));
    await h.user.paste('caja');
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(h.cell(0, 'uom').textContent).toBe('caja');
    await h.user.click(h.cell(0, 'basePrice'));
    await h.user.paste('no es número');
    expect(h.cell(0, 'basePrice')).toHaveAttribute('data-kx-cell', 'invalid');
  });

  it('pegar sin ninguna celda correcta deja el botón de pegar sin disponible', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await h.user.click(h.cell(0, 'basePrice'));
    await h.user.paste('x\ty\nz\tw');
    const dialog = await screen.findByRole('dialog', { name: 'Pegar desde una hoja de cálculo' });
    expect(within(dialog).getByRole('button', { name: 'Pegar 0 celdas' })).toHaveAttribute('aria-disabled', 'true');
    await h.user.click(within(dialog).getByRole('button', { name: 'Cancelar' }));
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('copiar deja en el portapapeles las celdas del rango en forma editable', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await h.user.click(h.cell(0, 'uom'));
    await h.user.keyboard('{Shift>}{ArrowDown}{ArrowRight}{/Shift}');
    const setData = vi.fn();
    const event = new Event('copy', { bubbles: true, cancelable: true }) as Event & { clipboardData: unknown };
    event.clipboardData = { setData };
    h.cell(1, 'basePrice').dispatchEvent(event);
    expect(setData).toHaveBeenCalledWith('text/plain', expect.stringMatching(/^.+\t.+\n.+\t.+$/));
    expect(event.defaultPrevented).toBe(true);
  });

  it('Mod+D rellena hacia abajo con el valor de la fila de arriba (un paso de deshacer)', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await edit(h, 0, 'uom', 'caja', '{Enter}');
    expect(h.cell(1, 'uom')).toHaveFocus();
    await h.user.keyboard('{Control>}d{/Control}');
    expect(h.cell(1, 'uom').textContent).toBe('caja');
    await h.user.keyboard('{Shift>}{ArrowDown}{ArrowDown}{/Shift}{Control>}d{/Control}');
    expect(h.cell(2, 'uom').textContent).toBe('caja');
    expect(h.cell(3, 'uom').textContent).toBe('caja');
    await h.user.keyboard('{Control>}z{/Control}');
    expect(h.cell(2, 'uom').textContent).not.toBe('caja');
    expect(h.cell(1, 'uom').textContent).toBe('caja');
  });

  it('Supr vacía la celda o el rango, y se puede deshacer', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    const unit = h.cell(0, 'uom').textContent;
    await h.user.click(h.cell(0, 'uom'));
    await h.user.keyboard('{Delete}');
    expect(h.cell(0, 'uom')).toHaveAttribute('data-kx-cell', 'dirty');
    expect(h.store.getState().rows[rowId(h, 0)]?.values.uom).toBe('');
    await h.user.keyboard('{Control>}z{/Control}');
    expect(h.cell(0, 'uom').textContent).toBe(unit);
    await h.user.keyboard('{Shift>}{ArrowDown}{/Shift}{Delete}');
    expect(h.store.getState().rows[rowId(h, 0)]?.values.uom).toBe('');
    expect(h.store.getState().rows[rowId(h, 1)]?.values.uom).toBe('');
  });

  it('Esc dos veces seguidas devuelve la fila entera a sus valores', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await edit(h, 0, 'uom', 'zz', '{Tab}');
    await edit(h, 0, 'basePrice', '5', '{Tab}');
    await h.user.click(h.cell(0, 'basePrice'));
    await h.user.keyboard('{F2}{Escape}{Escape}');
    expect(h.cell(0, 'basePrice')).not.toHaveAttribute('data-kx-cell', 'dirty');
    expect(h.cell(0, 'uom')).not.toHaveAttribute('data-kx-cell', 'dirty');
    expect(h.rows()[0]).not.toHaveAttribute('data-th-state');
  });
});
