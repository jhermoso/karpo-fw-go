// La rejilla montada con ancho justo: ajuste de columnas (D-R34), vacío único (D-R35), casillas táctiles (M-9) y
// anclas de ayuda. jsdom no mide nada: el ancho de la zona de desplazamiento y `ResizeObserver` se simulan.
import { act, screen, waitFor, within } from '@testing-library/react';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import enGrid from '../locales/en/grid.json';
import esGrid from '../locales/es/grid.json';
import { prefsKey } from './prefs';
import { resetGridStores } from './store';
import { productConfig, renderGrid, setupMockServer, stubLayout, type GridHarness } from './test-utils';

setupMockServer();

/** `ResizeObserver` que deja disparar a mano a todos los observadores vivos. */
class FakeResizeObserver {
  static live = new Set<FakeResizeObserver>();
  constructor(private readonly callback: ResizeObserverCallback) {
    FakeResizeObserver.live.add(this);
  }
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {
    FakeResizeObserver.live.delete(this);
  }
  static fire(): void {
    for (const observer of FakeResizeObserver.live) observer.callback([], observer as unknown as ResizeObserver);
  }
}

let restoreLayout: () => void;
let viewportWidth = 0;
let restoreWidth: () => void;

/** `clientWidth` sólo de la zona de desplazamiento de la rejilla (las demás cajas miden 0, como en jsdom). */
function stubViewportWidth(): () => void {
  const original = Object.getOwnPropertyDescriptor(Element.prototype, 'clientWidth');
  Object.defineProperty(Element.prototype, 'clientWidth', {
    configurable: true,
    get(this: Element) {
      return this.getAttribute('data-kx-part') === 'viewport' ? viewportWidth : 0;
    },
  });
  return () => {
    if (original) Object.defineProperty(Element.prototype, 'clientWidth', original);
    else delete (Element.prototype as unknown as Record<string, unknown>).clientWidth;
  };
}

async function resizeTo(width: number): Promise<void> {
  viewportWidth = width;
  await act(async () => {
    FakeResizeObserver.fire();
  });
}

function stubCoarsePointer(coarse: boolean): void {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: coarse && query.includes('pointer: coarse'),
    media: query,
    onchange: null,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    addListener: () => undefined,
    removeListener: () => undefined,
    dispatchEvent: () => false,
  }));
}

beforeEach(() => {
  restoreLayout = stubLayout();
  restoreWidth = stubViewportWidth();
  vi.stubGlobal('ResizeObserver', FakeResizeObserver);
});
afterEach(() => {
  restoreLayout();
  restoreWidth();
  resetGridStores();
  vi.unstubAllGlobals();
  FakeResizeObserver.live.clear();
  viewportWidth = 0;
});

const colcount = (h: GridHarness) => Number(h.grid().getAttribute('aria-colcount'));
const paintedHeaders = (h: GridHarness) =>
  within(h.grid())
    .getAllByRole('columnheader')
    .map((header) => header.dataset.kxCol);

describe('ajuste de columnas al ancho (D-R34)', () => {
  it('sin ancho medido (jsdom) pinta todas las columnas y no hay contador', async () => {
    const h = renderGrid();
    await h.ready();
    expect(colcount(h)).toBe(8);
    expect(document.querySelector('[data-kx-part="fit-badge"]')).toBeNull();
    expect(document.querySelector('[data-kx-part="fit-count"]')).toHaveAttribute('aria-live', 'polite');
  });

  it('con ancho suficiente no oculta nada', async () => {
    viewportWidth = 1000;
    const h = renderGrid();
    await h.ready();
    expect(colcount(h)).toBe(8);
    expect(document.querySelector('[data-kx-part="fit-badge"]')).toBeNull();
  });

  it('con poco ancho oculta las últimas: aria-colcount refleja las pintadas, aparece +n y prefs.hidden no cambia', async () => {
    viewportWidth = 500;
    const h = renderGrid();
    await h.ready();
    // selección + 4 de 6 columnas + acciones
    expect(colcount(h)).toBe(6);
    expect(paintedHeaders(h)).toEqual(['__select', 'sku', 'name', 'kind', 'uom', '__actions']);
    for (const row of h.rows()) expect(row.children).toHaveLength(6);
    expect(screen.getByText('+2')).toHaveAttribute('aria-hidden', 'true');
    expect(screen.getByText(esGrid.fit.hidden_other.replace('{{count}}', '2'))).toBeInTheDocument();
    // Estado derivado: nada en las preferencias del usuario.
    const stored = localStorage.getItem(prefsKey('product'));
    expect(stored === null || (JSON.parse(stored) as { hidden: string[] }).hidden.length === 0).toBe(true);
  });

  it('las flechas no alcanzan una columna oculta: tras la última pintada viene la de acciones', async () => {
    viewportWidth = 500;
    const h = renderGrid();
    await h.ready();
    h.cell(0, 'uom').focus();
    await h.user.keyboard('{ArrowRight}');
    expect(document.activeElement).toBe(h.cell(0, '__actions'));
    await h.user.keyboard('{ArrowRight}');
    expect(document.activeElement).toBe(h.cell(0, '__actions'));
    await h.user.keyboard('{End}');
    expect(document.activeElement).toBe(h.cell(0, '__actions'));
    await h.user.keyboard('{ArrowLeft}');
    expect(document.activeElement).toBe(h.cell(0, 'uom'));
  });

  it('al ensanchar la lista (ResizeObserver) vuelven las columnas y el contador se vacía; al estrechar, se ocultan', async () => {
    viewportWidth = 500;
    const h = renderGrid();
    await h.ready();
    expect(colcount(h)).toBe(6);
    await resizeTo(1000);
    expect(colcount(h)).toBe(8);
    expect(document.querySelector('[data-kx-part="fit-badge"]')).toBeNull();
    await resizeTo(420);
    expect(colcount(h)).toBe(5);
    expect(screen.getByText('+3')).toBeInTheDocument();
  });

  it('mientras hay hueco para todas encoge en lugar de ocultar', async () => {
    viewportWidth = 700;
    const h = renderGrid();
    await h.ready();
    expect(colcount(h)).toBe(8);
    // Las seis columnas de datos caben encogidas: la suma de sus anchos es justo el ancho disponible.
    const cols = h.grid().style.getPropertyValue('--kx-cols');
    const widths = [...cols.matchAll(/(\d+)px/g)].map((match) => Number(match[1]));
    expect(widths).toHaveLength(6);
    expect(widths.reduce((total, width) => total + width, 0)).toBe(700);
    expect(Math.min(...widths)).toBeGreaterThanOrEqual(64);
  });

  it('el menú de columnas marca las ocultas por falta de espacio y deja desactivar el ajuste', async () => {
    viewportWidth = 500;
    const h = renderGrid();
    await h.ready();
    await h.user.click(screen.getByRole('button', { name: 'Columnas' }));
    const panel = await screen.findByRole('dialog', { name: 'Columnas de la lista' });
    expect(within(panel).getAllByText(esGrid.columns.noRoom)).toHaveLength(2);
    // Siguen marcadas: son las que eligió el usuario.
    for (const label of ['Precio base', 'Retirado desde']) expect(within(panel).getByRole('checkbox', { name: new RegExp(label) })).toBeChecked();
    const autoFit = within(panel).getByRole('checkbox', { name: esGrid.columns.autoFit });
    expect(autoFit).toBeChecked();

    await h.user.click(autoFit);
    await waitFor(() => expect(colcount(h)).toBe(8));
    expect(document.querySelector('[data-kx-part="fit-badge"]')).toBeNull();
    expect(within(panel).queryByText(esGrid.columns.noRoom)).toBeNull();
    const saved = JSON.parse(localStorage.getItem(prefsKey('product')) ?? '{}') as { autoFit?: boolean; hidden: string[] };
    expect(saved.autoFit).toBe(false);
    expect(saved.hidden).toEqual([]);

    await h.user.click(autoFit);
    await waitFor(() => expect(colcount(h)).toBe(6));
  });

  it('ocultar a mano sigue yendo a prefs.hidden y se suma al ajuste', async () => {
    viewportWidth = 1000;
    const h = renderGrid();
    await h.ready();
    await h.user.click(screen.getByRole('button', { name: 'Columnas' }));
    const panel = await screen.findByRole('dialog', { name: 'Columnas de la lista' });
    await h.user.click(within(panel).getByRole('checkbox', { name: /Precio base/ }));
    await waitFor(() => expect(colcount(h)).toBe(7));
    const saved = JSON.parse(localStorage.getItem(prefsKey('product')) ?? '{}') as { hidden: string[] };
    expect(saved.hidden).toEqual(['basePrice']);
  });

  it('el contador en inglés', async () => {
    viewportWidth = 500;
    const h = renderGrid({ locale: 'en' });
    await h.ready();
    expect(screen.getByText(enGrid.fit.hidden_other.replace('{{count}}', '2'))).toBeInTheDocument();
  });
});

describe('vacío único (D-R35) en la rejilla', () => {
  it('un emptyValue de guiones (\'-\') y la ausencia de emptyValue pintan «—» con «Sin valor»', async () => {
    const base = productConfig();
    const columns = base.columns.map((column) => {
      if (column.field !== 'uom') return column;
      const { emptyValue: _gone, ...without } = column;
      return without;
    });
    const h = renderGrid({ config: { ...base, columns } });
    await h.ready();
    const dashed = base.columns.find((column) => column.field === 'discontinued');
    expect(dashed?.emptyValue).toBe('-');
    const empties = h.rows().flatMap((row) => Array.from(row.querySelectorAll<HTMLElement>('[data-kx-empty="true"]')));
    expect(empties.length).toBeGreaterThan(0);
    for (const empty of empties) {
      expect(empty.querySelector('[aria-hidden="true"]')?.textContent).toBe('—');
      expect(empty.querySelector('[data-kx-part="sr-only"]')?.textContent).toBe('Sin valor');
    }
    expect(h.rows()[0]?.textContent).not.toContain('-Sin valor');
  });

  it('un texto propio («sin fecha») se respeta', async () => {
    const base = productConfig();
    const columns = base.columns.map((column) => (column.field === 'discontinued' ? { ...column, emptyValue: 'sin fecha' } : column));
    const h = renderGrid({ config: { ...base, columns } });
    await h.ready();
    const cell = h.cell(0, 'discontinued');
    expect(within(cell).getByText('sin fecha')).toHaveAttribute('data-kx-empty', 'true');
    expect(cell.querySelector('[data-kx-part="sr-only"]')).toBeNull();
  });
});

describe('cifras (D-R35)', () => {
  it('las celdas numéricas no llevan data-th-format=amount y se alinean al final', async () => {
    const h = renderGrid();
    await h.ready();
    expect(h.grid().querySelector('[data-th-format="amount"]')).toBeNull();
    const price = h.cell(0, 'basePrice');
    expect(price).toHaveAttribute('data-kx-align', 'end');
    expect(price.querySelector('[data-kx-ui="value"]')).toHaveAttribute('data-kx-align', 'end');
  });

  it('grid.css da tabular-nums y nowrap a las celdas alineadas al final, sin cambiar de familia', () => {
    const css = readFileSync(resolve(import.meta.dirname, 'grid.css'), 'utf8');
    const rule = /\[role="gridcell"\]\[data-kx-align="end"\]\s*\{\s*font-variant-numeric:([^}]*)\}/.exec(css);
    expect(rule?.[0]).toMatch(/tabular-nums/);
    expect(rule?.[0]).toMatch(/white-space:\s*nowrap/);
    expect(rule?.[0]).not.toMatch(/font-family/);
  });
});

describe('casillas de selección en táctil (M-9)', () => {
  it('con ratón, un clic en la celda (fuera de la casilla) no alterna la selección', async () => {
    stubCoarsePointer(false);
    const h = renderGrid();
    await h.ready();
    const cell = h.cell(0, '__select');
    await h.user.click(cell);
    expect(h.rows()[0]).toHaveAttribute('aria-selected', 'false');
  });

  it('en táctil, un clic en el borde de la celda alterna la selección una sola vez', async () => {
    stubCoarsePointer(true);
    const h = renderGrid();
    await h.ready();
    const cell = h.cell(0, '__select');
    await h.user.click(cell);
    expect(h.rows()[0]).toHaveAttribute('aria-selected', 'true');
    await h.user.click(cell);
    expect(h.rows()[0]).toHaveAttribute('aria-selected', 'false');
  });

  it('en táctil, el clic en la propia casilla alterna una sola vez (sin doble disparo por la celda)', async () => {
    stubCoarsePointer(true);
    const h = renderGrid();
    await h.ready();
    const box = within(h.cell(0, '__select')).getByRole('checkbox');
    await h.user.click(box);
    expect(h.rows()[0]).toHaveAttribute('aria-selected', 'true');
    expect(h.rows()[1]).toHaveAttribute('aria-selected', 'false');
    await h.user.click(box);
    expect(h.rows()[0]).toHaveAttribute('aria-selected', 'false');
  });

  it('en táctil, la celda de la cabecera alterna todas las filas', async () => {
    stubCoarsePointer(true);
    const h = renderGrid();
    await h.ready();
    const head = within(h.grid()).getAllByRole('columnheader')[0] as HTMLElement;
    expect(head.dataset.kxCol).toBe('__select');
    await h.user.click(head);
    for (const row of h.rows()) expect(row).toHaveAttribute('aria-selected', 'true');
    await h.user.click(head);
    for (const row of h.rows()) expect(row).toHaveAttribute('aria-selected', 'false');
  });

  it('grid.css fija la casilla al tamaño de icono del tema, en táctil a la altura de control pequeño, con accent-color del tema', () => {
    const css = readFileSync(resolve(import.meta.dirname, 'grid.css'), 'utf8');
    const base = /\[data-kx-col="__select"\] input\[type="checkbox"\]\s*\{([^}]*)\}/.exec(css)?.[1] ?? '';
    expect(base).toMatch(/inline-size:\s*var\(--th-control-icon-size\)/);
    expect(base).toMatch(/block-size:\s*var\(--th-control-icon-size\)/);
    expect(base).toMatch(/accent-color:\s*var\(--th-/);
    const coarse = /@media \(pointer: coarse\)\s*\{([\s\S]*?)\n {2}\}\n/.exec(css)?.[1] ?? '';
    expect(coarse).toMatch(/inline-size:\s*var\(--th-control-height-small\)/);
    expect(coarse).toMatch(/block-size:\s*var\(--th-control-height-small\)/);
  });
});

describe('anclas de ayuda de la rejilla', () => {
  it('el par deshacer/rehacer lleva grid.undo y la barra de lote, su ancla de reserva grid.toolbar', async () => {
    const h = renderGrid();
    await h.ready();
    const history = h.container.querySelector('[data-kx-part="history"]');
    expect(history).toHaveAttribute('data-kx-help', 'grid.undo');
    const bar = h.container.querySelector('[data-kx-ui="batch-bar"]');
    expect(bar).toHaveAttribute('data-kx-help', 'grid.batch-bar');
    expect(bar).toHaveAttribute('data-kx-help-fallback', 'grid.toolbar');
    expect(bar).toHaveAttribute('hidden');
    expect(h.container.querySelector('[data-kx-help="grid.toolbar"]')).not.toBeNull();
  });

  it('hay texto en es y en para el deshacer y para la barra de lote en reposo', () => {
    for (const grid of [esGrid, enGrid]) {
      expect(grid.help.undo.title).toBeTruthy();
      expect(grid.help.undo.body).toBeTruthy();
      expect(grid.help['batch-bar'].idle.title).toBeTruthy();
      expect(grid.help['batch-bar'].idle.body).toBeTruthy();
    }
  });
});
