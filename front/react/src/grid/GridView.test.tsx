// La cuadrícula montada: estructura ARIA y ganchos de Theros, doble clic, teclado, selección, columnas,
// carga, vacío, errores, ayuda y atajos. El backend es el falso de la demo (MSW).
import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useShortcut, useShortcutRegistry } from '../ui';
import { assertTherosHooks, findTherosHookViolations } from '../theme';
import { resetGridStores } from './store';
import {
  createFakeTheme,
  createTestApi,
  productConfig,
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

const rowId = (h: GridHarness, index: number) => h.rows()[index]?.dataset.kxRow as string;
const text = (cell: HTMLElement) => cell.textContent;

describe('estructura de la rejilla', () => {
  it('es un role=grid con aria-rowcount, filas role=row con aria-rowindex y celdas hijas directas del registro', async () => {
    const h = renderGrid();
    await h.ready();
    const grid = h.grid();
    expect(grid).toHaveAttribute('data-th', 'collection');
    // 120 productos + la fila de cabecera
    expect(grid).toHaveAttribute('aria-rowcount', '121');
    // selección + 6 columnas + acciones
    expect(grid).toHaveAttribute('aria-colcount', '8');
    expect(grid).toHaveAttribute('aria-multiselectable', 'true');

    const header = within(grid).getAllByRole('row')[0] as HTMLElement;
    expect(header).toHaveAttribute('aria-rowindex', '1');
    const first = h.rows()[0] as HTMLElement;
    expect(first).toHaveAttribute('role', 'row');
    expect(first).toHaveAttribute('aria-rowindex', '2');
    expect(h.rows()[1]).toHaveAttribute('aria-rowindex', '3');
    for (const row of h.rows()) {
      expect(row.children.length).toBe(8);
      for (const child of Array.from(row.children)) expect(child).toHaveAttribute('role', 'gridcell');
      expect(row.querySelectorAll(':scope > [role="gridcell"]')).toHaveLength(8);
    }
    const indexes = Array.from(first.children).map((child) => child.getAttribute('aria-colindex'));
    expect(indexes).toEqual(['1', '2', '3', '4', '5', '6', '7', '8']);
  });

  it('assertTherosHooks pasa sobre la rejilla montada', async () => {
    const h = renderGrid();
    await h.ready();
    expect(() => assertTherosHooks(h.grid(), { inlineStyle: 'layout' })).not.toThrow();
    expect(findTherosHookViolations(h.container, { inlineStyle: 'layout' })).toEqual([]);
  });

  it('las cabeceras son role=columnheader con aria-sort sólo en las ordenables por el servidor', async () => {
    const h = renderGrid();
    await h.ready();
    const headers = within(h.grid()).getAllByRole('columnheader');
    const byName = (name: string) => headers.find((header) => header.textContent === name) as HTMLElement;
    expect(byName('Referencia')).toHaveAttribute('aria-sort', 'ascending'); // orden por omisión de la configuración
    expect(byName('Nombre')).toHaveAttribute('aria-sort', 'none');
    expect(byName('Precio base')).not.toHaveAttribute('aria-sort');
    expect(byName('Naturaleza')).not.toHaveAttribute('aria-sort');
  });

  it('virtualiza: sólo se pintan las filas de la ventana, con la cebra por índice de datos', async () => {
    const h = renderGrid();
    await h.ready();
    const rendered = h.rows();
    expect(rendered.length).toBeGreaterThan(10);
    expect(rendered.length).toBeLessThan(60);
    for (const row of rendered) {
      const dataIndex = Number(row.getAttribute('aria-rowindex')) - 2;
      expect(row.dataset.kxEven).toBe(dataIndex % 2 === 1 ? 'true' : 'false');
    }
  });

  it('carga la página siguiente cuando la ventana llega al final de lo cargado', async () => {
    const api = createTestApi();
    const list = vi.spyOn(api, 'list');
    const h = renderGrid({ api });
    await h.ready();
    await waitFor(() => expect(list.mock.calls.length).toBeGreaterThanOrEqual(2));
    expect(list.mock.calls[0]?.[1]).toMatchObject({ page: 1, pageSize: 25 });
    expect(list.mock.calls[1]?.[1]).toMatchObject({ page: 2, pageSize: 25 });
    expect(await screen.findByText('50 de 120 filas')).toBeInTheDocument();
  });

  it('muestra el esqueleto con las columnas reales mientras carga', async () => {
    const h = renderGrid();
    expect(h.grid()).toHaveAttribute('aria-busy', 'true');
    const skeleton = h.container.querySelectorAll('[data-kx-part="skeleton-row"]');
    expect(skeleton.length).toBeGreaterThan(0);
    expect(skeleton[0]?.querySelectorAll('[role="gridcell"]')).toHaveLength(8);
    await h.ready();
    expect(h.grid()).not.toHaveAttribute('aria-busy');
    expect(h.container.querySelectorAll('[data-kx-part="skeleton-row"]')).toHaveLength(0);
  });

  it('un listado vacío enseña data-th=collection-empty; con filtros ofrece quitarlos', async () => {
    const h = renderGrid({ params: { q: 'zzzzzz-no-existe' } });
    const empty = await waitFor(() => {
      const element = h.container.querySelector('[data-th="collection-empty"]');
      expect(element).not.toBeNull();
      return element as HTMLElement;
    });
    expect(empty).toHaveTextContent('Ningún registro cumple la búsqueda y los filtros.');
    await h.user.click(within(empty).getByRole('button', { name: 'Quitar todos los filtros' }));
    await h.ready();
    expect(h.container.querySelector('[data-th="collection-empty"]')).toBeNull();
  });

  it('un error de la lista se avisa con un botón para reintentar', async () => {
    server.use(
      http.post('*/api/v1/products/search', () =>
        HttpResponse.json({ title: 'Fallo', status: 500 }, { status: 500, headers: { 'Content-Type': 'application/problem+json' } }),
      ),
    );
    const h = renderGrid();
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Reintentar');
    server.resetHandlers();
    await h.user.click(within(alert).getByRole('button', { name: 'Reintentar' }));
    await h.ready();
    expect(screen.queryByRole('alert')).toBeNull();
  });
});

describe('doble clic y Mod+Enter', () => {
  it('el doble clic en una fila abre el registro en edición', async () => {
    const h = renderGrid();
    await h.ready();
    await h.user.dblClick(h.cell(2, 'name'));
    expect(h.navigate).toHaveBeenCalledWith({ entity: 'product', id: rowId(h, 2), mode: 'edit' });
  });

  it('Mod+Enter sobre una fila enfocada también', async () => {
    const h = renderGrid();
    await h.ready();
    await h.user.click(h.cell(3, 'uom'));
    await h.user.keyboard('{Control>}{Enter}{/Control}');
    expect(h.navigate).toHaveBeenCalledWith({ entity: 'product', id: rowId(h, 3), mode: 'edit' });
  });

  it('no se abre con el doble clic sobre la casilla ni sobre el menú de acciones', async () => {
    const h = renderGrid();
    await h.ready();
    await h.user.dblClick(within(h.cell(0, '__select')).getByRole('checkbox'));
    await h.user.dblClick(within(h.cell(0, '__actions')).getByRole('button'));
    expect(h.navigate.mock.calls.filter(([to]) => to.id !== undefined)).toEqual([]);
  });

  it('con una entidad sólo de formulario el doble clic sigue abriendo el registro', async () => {
    const h = renderGrid({ overlay: { saveMode: 'form' } });
    await h.ready();
    await h.user.dblClick(h.cell(0, 'name'));
    expect(h.navigate).toHaveBeenCalledWith(expect.objectContaining({ id: rowId(h, 0), mode: 'edit' }));
  });
});

describe('teclado', () => {
  async function focused(h: GridHarness, row: number, field: string) {
    await h.user.click(h.cell(row, field));
    expect(h.cell(row, field)).toHaveFocus();
  }

  it('las flechas mueven la celda activa y el foco, con un solo tabindex=0 (roving)', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await focused(h, 0, 'sku');
    await h.user.keyboard('{ArrowDown}');
    expect(h.cell(1, 'sku')).toHaveFocus();
    await h.user.keyboard('{ArrowRight}');
    expect(h.cell(1, 'name')).toHaveFocus();
    await h.user.keyboard('{ArrowUp}{ArrowLeft}');
    expect(h.cell(0, 'sku')).toHaveFocus();
    expect(h.grid().querySelectorAll('[tabindex="0"]')).toHaveLength(1);
    expect(h.cell(0, 'sku')).toHaveAttribute('data-kx-active', 'true');
    expect(h.cell(0, 'sku')).toHaveAttribute('data-kx-cell', 'active');
  });

  it('desde la primera fila, la flecha arriba llega a la cabecera y Enter ordena', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await focused(h, 0, 'name');
    await h.user.keyboard('{ArrowUp}');
    const header = within(h.grid()).getByRole('columnheader', { name: /Nombre/ });
    expect(header).toHaveFocus();
    await h.user.keyboard('{Enter}');
    expect(h.route().params).toMatchObject({ sort: 'name:asc' });
    await waitFor(() => expect(header).toHaveAttribute('aria-sort', 'ascending'));
  });

  it('Tab y Mayús+Tab no recorren las celdas: la lista es una sola parada y el foco sale de ella (B-4)', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await focused(h, 0, 'sku');
    // La lista no cancela la tecla: el navegador decide a dónde va el foco.
    expect(fireEvent.keyDown(h.cell(0, 'sku'), { key: 'Tab' })).toBe(true);
    expect(fireEvent.keyDown(h.cell(0, 'sku'), { key: 'Tab', shiftKey: true })).toBe(true);
    expect(h.cell(0, 'sku')).toHaveFocus();
    await h.user.keyboard('{Tab}');
    expect(h.grid().contains(document.activeElement)).toBe(false);
    // Y con Mayús+Tab se vuelve a la celda activa (tabindex itinerante), no a la primera de la lista.
    await h.user.keyboard('{Shift>}{Tab}{/Shift}');
    expect(h.cell(0, 'sku')).toHaveFocus();
  });

  it('tras ir con las flechas a otra celda, Mayús+Tab desde fuera vuelve a esa celda', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await focused(h, 0, 'sku');
    await h.user.keyboard('{ArrowDown}{ArrowRight}');
    expect(h.cell(1, 'name')).toHaveFocus();
    await h.user.keyboard('{Tab}');
    expect(h.grid().contains(document.activeElement)).toBe(false);
    await h.user.keyboard('{Shift>}{Tab}{/Shift}');
    expect(h.cell(1, 'name')).toHaveFocus();
  });

  it('al editar, Tab confirma y el foco pasa a la celda siguiente sin salir de la lista', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await focused(h, 0, 'name');
    await h.user.keyboard('{F2}');
    expect(within(h.cell(0, 'name')).getByRole('textbox')).toHaveFocus();
    await h.user.keyboard('{Tab}');
    const next = document.activeElement as HTMLElement;
    expect(h.grid().contains(next)).toBe(true);
    expect(next.getAttribute('role')).toBe('gridcell');
    expect(next.closest('[data-kx-row]')).toBe(h.rows()[0]);
    expect(next.getAttribute('data-kx-col')).not.toBe('name');
    expect(within(h.cell(0, 'name')).queryByRole('textbox')).toBeNull();
  });

  it('al editar, Mayús+Tab confirma y el foco pasa a la celda anterior sin salir de la lista', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await focused(h, 0, 'name');
    await h.user.keyboard('{F2}');
    await h.user.keyboard('{Shift>}{Tab}{/Shift}');
    const previous = document.activeElement as HTMLElement;
    expect(h.grid().contains(previous)).toBe(true);
    expect(previous.closest('[data-kx-row]')).toBe(h.rows()[0]);
    expect(previous.getAttribute('data-kx-col')).toBe('sku');
  });

  it('Inicio y Fin van a los extremos de la fila; con Mod, a los de la lista', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await focused(h, 2, 'name');
    await h.user.keyboard('{End}');
    expect(h.cell(2, '__actions')).toHaveFocus();
    await h.user.keyboard('{Home}');
    expect(h.cell(2, '__select')).toHaveFocus();
    await h.user.keyboard('{Control>}{End}{/Control}');
    const last = h.rows().at(-1) as HTMLElement;
    expect(last.querySelector('[data-kx-col="__actions"]')).toHaveFocus();
    await h.user.keyboard('{Control>}{Home}{/Control}');
    expect(h.cell(0, '__select')).toHaveFocus();
  });

  it('RePág y AvPág saltan una ventana de filas', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await focused(h, 0, 'name');
    await h.user.keyboard('{PageDown}');
    const row = document.activeElement?.closest('[data-kx-row]') as HTMLElement;
    expect(Number(row.getAttribute('aria-rowindex')) - 2).toBe(14); // 600 / 40 - 1
    await h.user.keyboard('{PageUp}');
    expect(h.cell(0, 'name')).toHaveFocus();
  });

  it('Enter y F2 abren el editor; Esc lo cierra sin cambios y devuelve el foco a la celda', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await focused(h, 0, 'name');
    await h.user.keyboard('{Enter}');
    const editor = within(h.cell(0, 'name')).getByRole('textbox');
    expect(editor).toHaveFocus();
    expect(h.cell(0, 'name')).toHaveAttribute('data-kx-cell', 'editing');
    await h.user.keyboard('{Escape}');
    expect(within(h.cell(0, 'name')).queryByRole('textbox')).toBeNull();
    expect(h.cell(0, 'name')).toHaveFocus();
    await h.user.keyboard('{F2}');
    expect(within(h.cell(0, 'name')).getByRole('textbox')).toHaveFocus();
  });

  it('escribir sobre una celda abre el editor y sustituye el valor', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await focused(h, 0, 'name');
    await h.user.keyboard('Zeta');
    expect(within(h.cell(0, 'name')).getByRole('textbox')).toHaveValue('Zeta');
    await h.user.keyboard('{Tab}');
    expect(text(h.cell(0, 'name'))).toBe('Zeta');
  });

  it('una celda de sólo lectura no se edita ni con Enter ni escribiendo', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await focused(h, 0, 'kind');
    await h.user.keyboard('{Enter}');
    await h.user.keyboard('x');
    expect(within(h.cell(0, 'kind')).queryByRole('textbox')).toBeNull();
    expect(h.cell(0, 'kind')).toHaveAttribute('aria-readonly', 'true');
  });

  it('Espacio selecciona la fila y Shift+flechas amplían el rango de celdas', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await focused(h, 1, 'name');
    await h.user.keyboard(' ');
    expect(h.rows()[1]).toHaveAttribute('aria-selected', 'true');
    expect(await screen.findByText('1 fila seleccionada')).toBeInTheDocument();
    await h.user.keyboard('{Shift>}{ArrowDown}{ArrowRight}{/Shift}');
    expect(h.cell(1, 'name')).toHaveAttribute('aria-selected', 'true');
    expect(h.cell(2, 'name')).toHaveAttribute('aria-selected', 'true');
    expect(h.cell(2, 'kind')).toHaveAttribute('aria-selected', 'true');
    expect(h.cell(3, 'name')).not.toHaveAttribute('aria-selected');
    await h.user.keyboard('{ArrowDown}');
    expect(h.cell(2, 'name')).not.toHaveAttribute('aria-selected', 'true');
  });

  it('Mod+flecha salta al borde y «/» lleva el foco a la búsqueda', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await focused(h, 3, 'name');
    await h.user.keyboard('{Control>}{ArrowUp}{/Control}');
    expect(h.cell(0, 'name')).toHaveFocus();
    await h.user.keyboard('{Control>}{ArrowRight}{/Control}');
    expect(h.cell(0, '__actions')).toHaveFocus();
    await h.user.keyboard('{Control>}{ArrowLeft}{/Control}');
    expect(h.cell(0, '__select')).toHaveFocus();
    await h.user.keyboard('/');
    expect(screen.getByRole('searchbox', { name: 'Buscar' })).toHaveFocus();
  });

  it('en RTL las flechas horizontales se invierten', async () => {
    document.documentElement.dir = 'rtl';
    try {
      const h = renderGrid({ overlay: { saveMode: 'batch' } });
      await h.ready();
      await focused(h, 0, 'name');
      await h.user.keyboard('{ArrowRight}');
      expect(h.cell(0, 'sku')).toHaveFocus();
      await h.user.keyboard('{ArrowLeft}');
      expect(h.cell(0, 'name')).toHaveFocus();
    } finally {
      document.documentElement.removeAttribute('dir');
    }
  });

  it('la casilla de la fila y la de la cabecera seleccionan; Shift+clic selecciona un rango', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    const boxes = h.rows().map((row) => within(row).getByRole('checkbox'));
    await h.user.click(boxes[1] as HTMLElement);
    await h.user.keyboard('{Shift>}');
    await h.user.click(boxes[4] as HTMLElement);
    await h.user.keyboard('{/Shift}');
    expect(h.rows().slice(1, 5).every((row) => row.getAttribute('aria-selected') === 'true')).toBe(true);
    expect(await screen.findByText('4 filas seleccionadas')).toBeInTheDocument();
    await h.user.click(screen.getByRole('button', { name: 'Quitar la selección' }));
    expect(h.rows().every((row) => row.getAttribute('aria-selected') === 'false')).toBe(true);
    await h.user.click(within(h.grid()).getByRole('checkbox', { name: 'Seleccionar todas las filas cargadas' }));
    expect(h.rows().every((row) => row.getAttribute('aria-selected') === 'true')).toBe(true);
  });
});

/** Un atajo global como los del marco: la `N` de «Nuevo» (tecla suelta) y el `?` del panel de atajos (con Mayús). */
function GlobalShortcuts({ onNew, onHelp }: { onNew(): void; onHelp(): void }) {
  useShortcut({ id: 'test.new', scope: 'global', keys: 'N', labelKey: 'core:shortcuts.new', run: onNew });
  useShortcut({ id: 'test.help', scope: 'global', keys: 'Shift+?', labelKey: 'core:shortcuts.panel', run: onHelp });
  return null;
}

describe('el registro de atajos no estorba', () => {
  it('escribir sobre una celda una tecla que es atajo global suelto (N) abre el editor con esa letra y no lanza el atajo', async () => {
    const onNew = vi.fn();
    const h = renderGrid({ overlay: { saveMode: 'batch' }, extra: <GlobalShortcuts onNew={onNew} onHelp={vi.fn()} /> });
    await h.ready();
    await h.user.click(h.cell(0, 'name'));
    await h.user.keyboard('n');
    expect(onNew).not.toHaveBeenCalled();
    expect(within(h.cell(0, 'name')).getByRole('textbox')).toHaveValue('n');
    await h.user.keyboard('{Escape}');
    // Fuera de la lista (con el foco en el cuadro de búsqueda, que es un campo de texto) tampoco se lanza.
    await h.user.click(screen.getByRole('searchbox', { name: 'Buscar' }));
    await h.user.keyboard('n');
    expect(onNew).not.toHaveBeenCalled();
  });

  it('el ? (Mayús+?) sigue llegando al registro desde una celda: abre el panel de atajos', async () => {
    const onHelp = vi.fn();
    const h = renderGrid({ overlay: { saveMode: 'batch' }, extra: <GlobalShortcuts onNew={vi.fn()} onHelp={onHelp} /> });
    await h.ready();
    await h.user.click(h.cell(0, 'name'));
    await h.user.keyboard('?');
    expect(onHelp).toHaveBeenCalledTimes(1);
  });

  it('Tab nunca lo cancela la lista ni el registro de atajos, ni en la última celda ni en medio de la lista', async () => {
    // pocas filas, para que la última celda sea de verdad la última
    const h = renderGrid({ overlay: { saveMode: 'batch' }, params: { q: 'Correa' } });
    await h.ready();
    await waitFor(() => expect(h.rows().length).toBeLessThan(25));
    await h.user.click(h.cell(0, 'name'));
    await h.user.keyboard('{Control>}{End}{/Control}');
    const active = document.activeElement as HTMLElement;
    expect(active.closest('[data-kx-row]')).toBe(h.rows().at(-1));
    expect(fireEvent.keyDown(active, { key: 'Tab' })).toBe(true);
    // y en medio de la lista tampoco (antes la tecla movía la celda y atrapaba el foco)
    await h.user.click(h.cell(0, 'name'));
    expect(fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'Tab' })).toBe(true);
    expect(fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'Tab', shiftKey: true })).toBe(true);
  });

  it('Enter sobre la casilla de una celda no lo cancela el registro de atajos', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    const box = within(h.cell(0, '__select')).getByRole('checkbox');
    expect(fireEvent.keyDown(box, { key: 'Enter' })).toBe(true);
  });
});

describe('columnas', () => {
  it('Alt+flechas en la cabecera cambian el ancho y queda guardado en localStorage', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await h.user.click(h.cell(0, 'name'));
    await h.user.keyboard('{ArrowUp}');
    const before = h.grid().style.getPropertyValue('--kx-cols');
    expect(before).toBe('140px 260px 120px 90px 120px minmax(130px, 1fr)');
    await h.user.keyboard('{Alt>}{ArrowRight}{/Alt}');
    expect(h.grid().style.getPropertyValue('--kx-cols')).toBe('140px 276px 120px 90px 120px minmax(130px, 1fr)');
    await h.user.keyboard('{Alt>}{ArrowLeft}{ArrowLeft}{/Alt}');
    expect(h.grid().style.getPropertyValue('--kx-cols')).toBe('140px 244px 120px 90px 120px minmax(130px, 1fr)');
    const stored = JSON.parse(localStorage.getItem('karpo.grid.anon.product') as string);
    expect(stored.sizes).toEqual({ name: 244 });
  });

  it('el doble clic en el borde restablece el ancho', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await h.user.click(h.cell(0, 'name'));
    await h.user.keyboard('{ArrowUp}{Alt>}{ArrowRight}{/Alt}');
    expect(h.grid().style.getPropertyValue('--kx-cols')).toContain('276px');
    const separator = within(h.grid()).getByRole('separator', { name: 'Cambiar el ancho de Nombre' });
    await h.user.dblClick(separator);
    expect(h.grid().style.getPropertyValue('--kx-cols')).toContain('260px');
  });

  it('arrastrar el borde cambia el ancho', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    const separator = within(h.grid()).getByRole('separator', { name: 'Cambiar el ancho de Nombre' });
    fireEvent.pointerDown(separator, { clientX: 100, pointerId: 1 });
    const move = new MouseEvent('pointermove', { clientX: 150, bubbles: true });
    act(() => {
      separator.dispatchEvent(move);
    });
    expect(h.grid().style.getPropertyValue('--kx-cols')).toContain('310px');
    act(() => {
      separator.dispatchEvent(new MouseEvent('pointerup', { bubbles: true }));
    });
  });

  it('arrastrar una cabecera sobre otra reordena las columnas', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    const headers = () => within(h.grid()).getAllByRole('columnheader').map((header) => header.textContent);
    expect(headers().slice(1, 4)).toEqual(['Referencia', 'Nombre', 'Naturaleza']);
    const source = within(h.grid()).getByRole('columnheader', { name: /Naturaleza/ });
    const target = within(h.grid()).getByRole('columnheader', { name: /Referencia/ });
    const data: Record<string, string> = {};
    const dataTransfer = { setData: (k: string, v: string) => (data[k] = v), getData: (k: string) => data[k] ?? '', effectAllowed: '' };
    fireEvent.dragStart(source, { dataTransfer });
    fireEvent.dragOver(target, { dataTransfer });
    expect(target).toHaveAttribute('data-th-drop', 'over');
    fireEvent.drop(target, { dataTransfer });
    expect(headers().slice(1, 4)).toEqual(['Naturaleza', 'Referencia', 'Nombre']);
    expect(target).not.toHaveAttribute('data-th-drop');
  });

  it('el menú de columnas oculta, reordena, fija y restablece; todo se recuerda', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await h.user.click(screen.getByRole('button', { name: 'Columnas' }));
    const panel = await screen.findByRole('dialog', { name: 'Columnas de la lista' });
    await h.user.click(within(panel).getByRole('checkbox', { name: 'Unidad' }));
    const headerNames = () => within(h.grid()).getAllByRole('columnheader').map((header) => header.textContent);
    expect(headerNames()).not.toContain('Unidad');
    await h.user.click(within(panel).getByRole('button', { name: 'Mover Nombre antes' }));
    expect(headerNames().slice(1, 3)).toEqual(['Nombre', 'Referencia']);
    await h.user.click(within(panel).getByRole('button', { name: 'Fijar Precio base al final' }));
    expect(h.cell(0, 'basePrice')).toHaveAttribute('data-kx-pin', 'end');
    expect(within(panel).getByRole('button', { name: 'Fijar Precio base al final' })).toHaveAttribute('aria-pressed', 'true');
    await h.user.click(within(panel).getByRole('button', { name: 'Fijar Referencia al inicio' }));
    expect(h.cell(0, 'sku')).toHaveAttribute('data-kx-pin', 'start');
    // la columna fijada al inicio es la primera de datos
    expect(headerNames().slice(1, 3)).toEqual(['Referencia', 'Nombre']);

    h.unmount();
    const again = renderGrid({ overlay: { saveMode: 'batch' } });
    await again.ready();
    expect(within(again.grid()).getAllByRole('columnheader').map((header) => header.textContent)).not.toContain('Unidad');

    await again.user.click(screen.getByRole('button', { name: 'Columnas' }));
    const panel2 = await screen.findByRole('dialog', { name: 'Columnas de la lista' });
    await again.user.click(within(panel2).getByRole('button', { name: 'Restablecer columnas' }));
    expect(within(again.grid()).getAllByRole('columnheader').map((header) => header.textContent)).toContain('Unidad');
  });

  it('no se puede ocultar la última columna visible', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch', grid: { columns: [{ field: 'sku', hidden: false }] } } });
    await h.ready();
    await h.user.click(screen.getByRole('button', { name: 'Columnas' }));
    const panel = await screen.findByRole('dialog', { name: 'Columnas de la lista' });
    for (const name of ['Nombre', 'Naturaleza', 'Unidad', 'Precio base', 'Retirado desde']) {
      await h.user.click(within(panel).getByRole('checkbox', { name }));
    }
    expect(within(panel).getByRole('checkbox', { name: 'Referencia' })).toBeDisabled();
  });
});

describe('medida de la fila y tema', () => {
  it('mide la fila sonda y vuelve a medir cuando cambia el tema', async () => {
    const theme = createFakeTheme();
    let height = 36;
    const rect = vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
      const probe = (this as HTMLElement).dataset?.kxPart === 'probe';
      return { x: 0, y: 0, top: 0, left: 0, right: 0, bottom: 0, width: 0, height: probe ? height : 0, toJSON: () => ({}) } as DOMRect;
    });
    const h = renderGrid({ theme, overlay: { saveMode: 'batch' } });
    await h.ready();
    expect(rect).toHaveBeenCalled();
    expect(h.grid().style.getPropertyValue('--kx-row-height')).toBe('36px');
    expect(h.rows()[1]?.style.getPropertyValue('--kx-row-y')).toBe('36px');
    height = 52;
    act(() => theme.emit());
    await waitFor(() => expect(h.grid().style.getPropertyValue('--kx-row-height')).toBe('52px'));
    await waitFor(() => expect(h.rows()[1]?.style.getPropertyValue('--kx-row-y')).toBe('52px'));
  });

  it('la fila sonda no es accesible ni cuenta como fila de datos', async () => {
    const h = renderGrid();
    await h.ready();
    const probe = h.container.querySelector('[data-kx-part="probe"]') as HTMLElement;
    expect(probe).toHaveAttribute('aria-hidden', 'true');
    expect(within(h.grid()).getAllByRole('row').includes(probe)).toBe(false);
  });
});

describe('ayuda y atajos', () => {
  it('lleva las anclas de ayuda de la cuadrícula', async () => {
    const h = renderGrid();
    await h.ready();
    for (const anchor of ['toolbar', 'search', 'filters', 'columns', 'row-actions', 'batch-bar']) {
      expect(h.container.querySelector(`[data-kx-help="grid.${anchor}"]`), anchor).not.toBeNull();
    }
  });

  it('registra los atajos en el ámbito grid para el panel', async () => {
    function Registry() {
      const defs = useShortcutRegistry();
      return <output data-testid="registry">{defs.map((d) => `${d.scope}:${d.id}:${d.keys}`).join('|')}</output>;
    }
    const h = renderGrid({ extra: <Registry /> });
    await h.ready();
    const registry = screen.getByTestId('registry').textContent as string;
    for (const entry of ['grid:grid.undo:Mod+Z', 'grid:grid.redo:Mod+Shift+Z', 'grid:grid.save:Mod+S', 'grid:grid.fill:Mod+D', 'grid:grid.open:Mod+Enter', 'grid:grid.search:/']) {
      expect(registry).toContain(entry);
    }
    expect(h.grid()).toHaveAttribute('data-kx-scope', 'grid');
  });

  it('sin permiso de escritura la cuadrícula no edita', async () => {
    const session = { status: 'authenticated' as const, can: (permission: string) => permission === 'Products.Product.Read' };
    const h = renderGrid({ session, config: productConfig({ permissions: { read: 'Products.Product.Read', write: 'Products.Product.Update' } }) });
    await h.ready();
    await h.user.click(h.cell(0, 'name'));
    await h.user.keyboard('{F2}');
    expect(within(h.cell(0, 'name')).queryByRole('textbox')).toBeNull();
    expect(h.cell(0, 'name')).not.toHaveAttribute('data-kx-cell', 'readonly');
    expect(screen.queryByRole('button', { name: 'Deshacer' })).toBeNull();
    expect(h.container.querySelector('[data-kx-ui="grid"]')).toHaveAttribute('data-kx-editable', 'false');
  });
});
