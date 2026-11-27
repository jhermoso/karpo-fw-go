// La barra de cambios flotante (M-9), el historial en grupo, el recuento que la lista cuenta a la cabecera de la
// entidad (M-7) y la sombra de las columnas fijadas (M-8). El diseño real (posición, solapes) lo comprueba la e2e.
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ViewStatus } from '../contracts';
import { resetGridStores } from './store';
import { renderGrid, setupMockServer, stubLayout, type GridHarness } from './test-utils';

setupMockServer();

const CSS = readFileSync(path.resolve(import.meta.dirname, 'grid.css'), 'utf8');

let restoreLayout: () => void;
beforeEach(() => {
  restoreLayout = stubLayout();
});
afterEach(() => {
  restoreLayout();
  resetGridStores();
  vi.restoreAllMocks();
});

async function edit(h: GridHarness, row: number, field: string, value: string) {
  await h.user.click(h.cell(row, field));
  await h.user.keyboard('{F2}');
  await h.user.keyboard('{Control>}a{/Control}');
  await h.user.keyboard(value);
  await h.user.keyboard('{Enter}');
}

describe('barra de cambios', () => {
  it('es una barra compacta con el punto de «sucio», Descartar (secundaria) y Guardar (primaria)', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    const bar = h.container.querySelector('[data-kx-ui="batch-bar"]') as HTMLElement;
    expect(bar).toHaveAttribute('hidden');
    await edit(h, 0, 'name', 'Otro nombre');
    await waitFor(() => expect(bar).not.toHaveAttribute('hidden'));
    expect(bar.querySelector('[data-kx-part="dirty-dot"]')).toHaveAttribute('aria-hidden', 'true');
    expect(within(bar).getByText('1 cambio')).toBeInTheDocument();
    const save = within(bar).getByRole('button', { name: /Guardar/ });
    const discard = within(bar).getByRole('button', { name: 'Descartar' });
    expect(save).toHaveAttribute('data-th-kind', 'primary');
    expect(discard).toHaveAttribute('data-th-kind', 'secondary');
    // Descartar va antes de Guardar: la acción principal queda al final, donde termina la mirada.
    expect(discard.compareDocumentPosition(save) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    // Está después de la lista: flota sobre su borde inferior.
    const table = h.grid();
    expect(table.compareDocumentPosition(bar) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('el número de cambios va interpolado en una sola cadena (para que el aislamiento RTL lo envuelva entero)', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    await edit(h, 0, 'name', 'Uno');
    await edit(h, 1, 'name', 'Dos');
    const count = h.container.querySelector('[data-kx-ui="batch-bar"] [data-kx-part="count"]') as HTMLElement;
    expect(count.textContent).toBe('2 cambios');
    expect(count.children).toHaveLength(0);
  });

  it('el CSS la hace flotante y compacta con los tokens del tema, y deja sitio al pie de la lista', () => {
    const block = /\[data-kx-ui="batch-bar"\]\s*\{([^}]*)\}/.exec(CSS)?.[1] ?? '';
    expect(block).toMatch(/position: sticky/);
    expect(block).toMatch(/inline-size: fit-content/);
    expect(block).toMatch(/border-radius: var\(--th-edge-rounding-large\)/);
    expect(block).toMatch(/box-shadow: var\(--th-depth-levels-overlay\)/);
    expect(block).toMatch(/background-color: var\(--th-surface-popover-bg\)/);
    expect(CSS).toMatch(/\[data-kx-batch\] \[data-kx-part="viewport"\]\s*\{[^}]*padding-block-end: calc\(var\(--kx-batch-height/);
  });

  it('con la barra visible, la lista publica su alto y deja ese relleno', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    const root = h.container.querySelector('[data-kx-ui="grid"]') as HTMLElement;
    expect(root).not.toHaveAttribute('data-kx-batch');
    await edit(h, 0, 'name', 'Otro');
    await waitFor(() => expect(root).toHaveAttribute('data-kx-batch'));
    expect(root.style.getPropertyValue('--kx-batch-height')).toMatch(/^\d+(\.\d+)?px$/);
    await h.user.click(screen.getByRole('button', { name: 'Descartar' }));
    await waitFor(() => expect(root).not.toHaveAttribute('data-kx-batch'));
    expect(root.style.getPropertyValue('--kx-batch-height')).toBe('0px');
  });
});

describe('historial', () => {
  it('deshacer y rehacer son acciones secundarias dentro de un grupo con nombre', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    const group = screen.getByRole('group', { name: 'Historial de cambios' });
    const undo = within(group).getByRole('button', { name: 'Deshacer' });
    const redo = within(group).getByRole('button', { name: 'Rehacer' });
    expect(undo).toHaveAttribute('data-th-kind', 'secondary');
    expect(redo).toHaveAttribute('data-th-kind', 'secondary');
    expect(undo).toHaveAttribute('aria-disabled', 'true');
    await edit(h, 0, 'name', 'Otro');
    await waitFor(() => expect(undo).not.toHaveAttribute('aria-disabled'));
    await h.user.click(undo);
    await waitFor(() => expect(redo).not.toHaveAttribute('aria-disabled'));
  });

  it('una lista que no se edita no enseña el historial', async () => {
    const h = renderGrid({ overlay: { editable: false } });
    await h.ready();
    expect(screen.queryByRole('group', { name: 'Historial de cambios' })).toBeNull();
  });
});

describe('recuento para la cabecera de la entidad', () => {
  const last = (onStatus: ReturnType<typeof vi.fn<(status: ViewStatus) => void>>): ViewStatus =>
    onStatus.mock.calls.at(-1)?.[0] as ViewStatus;

  it('cuenta que carga y luego el total, que sin filtro es también lo que coincide', async () => {
    const onStatus = vi.fn<(status: ViewStatus) => void>();
    const h = renderGrid({ onStatus });
    expect(onStatus.mock.calls[0]?.[0]).toEqual({ loading: true });
    await h.ready();
    await waitFor(() => expect(last(onStatus)).toEqual({ loading: false, matched: 120, total: 120 }));
  });

  it('con una búsqueda cuenta las coincidencias y recuerda el total sin filtrar', async () => {
    const onStatus = vi.fn<(status: ViewStatus) => void>();
    const h = renderGrid({ onStatus });
    await h.ready();
    await waitFor(() => expect(last(onStatus).total).toBe(120));
    fireEvent.change(screen.getByRole('searchbox', { name: 'Buscar' }), { target: { value: 'Correa' } });
    fireEvent.keyDown(screen.getByRole('searchbox', { name: 'Buscar' }), { key: 'Enter' });
    await waitFor(() => {
      const status = last(onStatus);
      expect(status.loading).toBe(false);
      expect(status.matched).toBeLessThan(120);
    });
    expect(last(onStatus).total).toBe(120);
    expect(last(onStatus).matched).toBeGreaterThan(0);
  });

  it('con un filtro puesto desde el principio no adivina el total: sólo cuenta las coincidencias', async () => {
    const onStatus = vi.fn<(status: ViewStatus) => void>();
    const h = renderGrid({ onStatus, params: { q: 'Correa' } });
    await h.ready();
    await waitFor(() => expect(last(onStatus).loading).toBe(false));
    expect(last(onStatus)).not.toHaveProperty('total');
    expect(last(onStatus).matched).toBeGreaterThan(0);
  });
});

describe('columnas fijadas', () => {
  it('marca con atributos cuándo hay más columnas detrás para que la sombra del borde aparezca', async () => {
    const h = renderGrid({ overlay: { saveMode: 'batch' } });
    await h.ready();
    const viewport = h.grid();
    // En jsdom no hay geometría: sin desbordamiento no hay marca.
    expect(viewport).not.toHaveAttribute('data-kx-more-end');
    Object.defineProperty(viewport, 'scrollWidth', { configurable: true, value: 1200 });
    Object.defineProperty(viewport, 'clientWidth', { configurable: true, value: 600 });
    fireEvent.scroll(viewport);
    expect(viewport).toHaveAttribute('data-kx-more-end');
    expect(viewport).not.toHaveAttribute('data-kx-more-start');
    viewport.scrollLeft = 300;
    fireEvent.scroll(viewport);
    expect(viewport).toHaveAttribute('data-kx-more-start');
    expect(viewport).toHaveAttribute('data-kx-more-end');
    viewport.scrollLeft = 600;
    fireEvent.scroll(viewport);
    expect(viewport).not.toHaveAttribute('data-kx-more-end');
  });

  it('el CSS pinta la sombra sólo con la marca y la invierte en RTL con [dir="rtl"], sin la pseudoclase de dirección (D-R22)', () => {
    expect(CSS).toMatch(/\[data-kx-more-end\] \[data-kx-col="__actions"\]::after/);
    expect(CSS).toMatch(/\[dir="rtl"\] \[data-kx-part="viewport"\] \[data-kx-col="__actions"\]::after/);
    expect(CSS).not.toContain(['', 'dir('].join(':'));
  });
});
