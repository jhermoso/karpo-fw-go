// Organigrama: textos resueltos (B-2 a), roles y nombres del diagrama (M-13), contador «≥N» (M-1), «Ajustar»
// que alterna, zoom lejano y la cuenta que la vista da a la cabecera de la entidad (M-7).
import { screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { ViewStatus } from '../contracts';
import { getCodec } from '../fields';
import { readdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { plainFieldText, searchText, type NodeFieldDefs } from './fields';
import { installDemoBackend, renderEntityView, type RenderedView } from './test-utils';
import { TreeView } from './TreeView';

installDemoBackend();

async function openDiagram(entity = 'facility', options: { locale?: 'es' | 'en'; dir?: 'ltr' | 'rtl'; onStatus?: (s: ViewStatus) => void } = {}): Promise<RenderedView> {
  const view = await renderEntityView(TreeView, { entity, search: '?view=tree&layout=diagram', ...options });
  await waitFor(() => expect(view.container.querySelector('[data-kx-view="tree"]')).toHaveAttribute('data-kx-ready', 'true'), { timeout: 10_000 });
  return view;
}

describe('ningún texto en español sin resolver (B-2 a)', () => {
  it('con lng=en el listado y el diagrama nombran la entidad en inglés', async () => {
    const list = await renderEntityView(TreeView, { entity: 'facility', search: '?view=tree', locale: 'en' });
    await screen.findByRole('tree', {}, { timeout: 10_000 });
    expect(screen.getByRole('tree').getAttribute('aria-label')).not.toMatch(/instalaciones/i);
    expect(list.container.querySelector('[data-kx-view="tree"]')?.getAttribute('aria-label')).not.toMatch(/instalaciones/i);
    list.unmount();

    await openDiagram('facility', { locale: 'en' });
    const region = screen.getByRole('region', { name: /^Diagram of / });
    expect(region.getAttribute('aria-label')).not.toMatch(/instalaciones/i);
    expect(screen.getByRole('link', { name: 'View as list' })).toBeInTheDocument();
  });

  it('el texto de un nodo y de la búsqueda pasa por el resolvedor de textos de la configuración', () => {
    const defs: NodeFieldDefs = {
      label: { field: 'status', label: { literal: 'Estado' }, kind: 'badge', required: false, editable: false },
    };
    const row = { id: 'p1', status: 'filled' } as never;
    const codec = getCodec('badge');
    const format = vi.spyOn(codec, 'format');
    const resolve = (text: { literal: string; key?: string; byLang?: Record<string, string> }) => text.byLang?.en ?? text.key ?? 'literal';
    plainFieldText(row, defs.label, 'en', resolve);
    expect(format).toHaveBeenCalledWith('filled', defs.label, 'en', resolve);
    searchText(row, defs, 'en', resolve);
    expect(format).toHaveBeenLastCalledWith('filled', defs.label, 'en', resolve);
    format.mockRestore();
  });

  it('ni src/tree ni src/map leen el literal de un texto de la configuración: se resuelven con useText', () => {
    const sources: string[] = [];
    const walk = (dir: string) => {
      for (const entry of readdirSync(dir, { withFileTypes: true })) {
        const path = resolve(dir, entry.name);
        if (entry.isDirectory()) walk(path);
        else if (/\.(ts|tsx)$/.test(entry.name) && !/\.test\.tsx?$|test-utils/.test(entry.name)) sources.push(path);
      }
    };
    for (const folder of ['.', '../map']) walk(resolve(import.meta.dirname, folder));
    // (El patrón se arma por partes para que esta prueba tampoco contenga lo que busca.)
    const property = new RegExp(`\\.${'liter'}al\\b`);
    expect(sources.length).toBeGreaterThan(20);
    expect(sources.filter((file) => property.test(readFileSync(file, 'utf8')))).toEqual([]);
  });
});

describe('roles y nombres del diagrama (M-13)', () => {
  it('cada nodo es un grupo con descripción de rol y nombre; las aristas no se anuncian', async () => {
    const view = await openDiagram();
    const nodes = Array.from(view.container.querySelectorAll<HTMLElement>('.react-flow__node'));
    expect(nodes.length).toBeGreaterThan(10);
    for (const node of nodes) {
      expect(node).toHaveAttribute('role', 'group');
      expect(node).toHaveAttribute('aria-roledescription', 'nodo del organigrama');
      expect(node.getAttribute('aria-label')).toBeTruthy();
      expect(node).toHaveAccessibleName();
    }
    // El nombre del grupo es el del elemento (el mismo que la etiqueta de su tarjeta).
    const sede = nodes.find((node) => node.getAttribute('aria-label') === 'Sede Central Madrid');
    expect(sede?.querySelector('[data-kx-part="label"]')?.textContent).toBe('Sede Central Madrid');
    for (const edge of view.container.querySelectorAll('.react-flow__edge')) {
      expect(edge).toHaveAttribute('aria-hidden', 'true');
      expect(edge.getAttribute('aria-label') ?? '').not.toMatch(/Edge from/);
    }
    // Y existe el enlace a la alternativa accesible.
    expect(screen.getByRole('link', { name: 'Ver como lista' })).toBeInTheDocument();
  });

  it('todos los controles del diagrama tienen rol y nombre accesible', async () => {
    const view = await openDiagram();
    const region = screen.getByRole('region', { name: /^Diagrama de / });
    expect(region).toHaveAttribute('aria-roledescription', 'diagrama');
    const controls = Array.from(
      region.querySelectorAll<HTMLElement>(
        'button, a[href], input, select, textarea, [role="button"], [role="link"], [role="img"], [role="group"], [tabindex]:not([tabindex="-1"])',
      ),
    ).filter((element) => !element.closest('[aria-hidden="true"]'));
    expect(controls.length).toBeGreaterThan(20);
    for (const control of controls) {
      expect(control, control.outerHTML.slice(0, 120)).toHaveAccessibleName();
    }
    // Los controles de zoom, el minimapa y los pliegues, por nombre.
    const group = within(region).getByRole('group', { name: 'Controles del diagrama' });
    for (const name of ['Acercar', 'Alejar', 'Ajustar a la pantalla']) expect(within(group).getByRole('button', { name })).toBeInTheDocument();
    expect(within(region).getByRole('img', { name: 'Minimapa del diagrama' })).toBeInTheDocument();
    expect(within(region).getAllByRole('button', { name: /^Plegar la rama de / }).length).toBeGreaterThan(0);
    // Ningún elemento enfocable sin nombre en toda la vista.
    for (const element of view.container.querySelectorAll<HTMLElement>('button, a[href], input')) {
      if (element.closest('[aria-hidden="true"]')) continue;
      expect(element, element.outerHTML.slice(0, 120)).toHaveAccessibleName();
    }
  });

  it('en inglés los nodos se anuncian como «org chart node»', async () => {
    const view = await openDiagram('facility', { locale: 'en' });
    const node = view.container.querySelector<HTMLElement>('.react-flow__node');
    expect(node).toHaveAttribute('aria-roledescription', 'org chart node');
  });
});

describe('contador de descendientes parcial (M-1)', () => {
  it('«≥N» en <bdi> con su nombre accesible, y N a secas cuando está completo', async () => {
    const view = await openDiagram('position');
    const partial = view.container.querySelector<HTMLElement>('[data-kx-part="count"][data-kx-partial="true"]');
    expect(partial).not.toBeNull();
    expect(partial!.textContent).toMatch(/^≥\d+$/);
    expect(partial!.querySelector('bdi')).not.toBeNull();
    expect(partial).toHaveAttribute('role', 'img');
    expect(partial).toHaveAccessibleName(/^al menos \d+ descendientes?; hay ramas sin cargar$/);

    const full = await openDiagram('facility');
    const complete = full.container.querySelector<HTMLElement>('[data-kx-part="count"]:not([data-kx-partial])');
    expect(complete?.textContent).toMatch(/^\d+$/);
    expect(complete).toHaveAccessibleName(/^\d+ descendientes?$/);
  });
});

describe('«Ajustar» alterna entre legible y todo', () => {
  it('el botón es de dos estados: el encuadre legible y todo el organigrama, y un movimiento a mano lo suelta', async () => {
    const view = await openDiagram();
    const fit = screen.getByRole('button', { name: 'Ajustar a la pantalla' });
    expect(fit).toHaveAttribute('aria-pressed', 'false');
    await view.user.click(fit);
    expect(fit).toHaveAttribute('aria-pressed', 'true');
    expect(fit).toHaveAttribute('title', 'Volver al tamaño legible');
    await view.user.click(fit);
    expect(fit).toHaveAttribute('aria-pressed', 'false');
    expect(fit).toHaveAttribute('title', 'Ver todo el organigrama');
    // Acercar es un movimiento a mano: «Ajustar» vuelve al legible, no a «todo».
    await view.user.click(fit);
    expect(fit).toHaveAttribute('aria-pressed', 'true');
    await view.user.click(screen.getByRole('button', { name: 'Acercar' }));
    await view.user.click(fit);
    expect(fit).toHaveAttribute('aria-pressed', 'false');
  });

  it('por debajo de 0,6 de zoom el diagrama pasa a «lejos» y al volver al encuadre legible sale de él', async () => {
    const view = await openDiagram();
    const diagram = view.container.querySelector<HTMLElement>('[data-kx-part="diagram"]')!;
    expect(diagram).toHaveAttribute('data-kx-zoom', 'near');
    await view.user.click(screen.getByRole('button', { name: 'Ajustar a la pantalla' }));
    await waitFor(() => expect(diagram).toHaveAttribute('data-kx-zoom', 'far'));
    expect(diagram.style.getPropertyValue('--kx-far-font')).toMatch(/^[\d.]+px$/);
    await view.user.click(screen.getByRole('button', { name: 'Ajustar a la pantalla' }));
    await waitFor(() => expect(diagram).toHaveAttribute('data-kx-zoom', 'near'));
  });
});

describe('la vista cuenta a la cabecera cuántos elementos hay (M-7)', () => {
  it('informa del total y de las coincidencias de la búsqueda', async () => {
    const onStatus = vi.fn<(status: ViewStatus) => void>();
    const view = await renderEntityView(TreeView, { entity: 'facility', search: '?view=tree', onStatus });
    await screen.findByRole('tree', {}, { timeout: 10_000 });
    await waitFor(() => expect(onStatus).toHaveBeenCalled());
    const first = onStatus.mock.calls.find(([status]) => status.loading === false)![0];
    expect(first.total).toBeGreaterThan(20);
    expect(first.matched).toBe(first.total);

    await view.user.type(screen.getByRole('searchbox'), 'sala');
    await waitFor(() => expect(onStatus.mock.calls.at(-1)![0].matched).toBeLessThan(first.total!));
    expect(onStatus.mock.calls.at(-1)![0]).toMatchObject({ total: first.total, loading: false });
    expect(onStatus.mock.calls.at(-1)![0].matched).toBeGreaterThan(0);
    // Sin cambios no se repite el aviso.
    const calls = onStatus.mock.calls.length;
    await new Promise((done) => setTimeout(done, 30));
    expect(onStatus.mock.calls.length).toBe(calls);
  });
});

describe('tarjetas sin recortes (R3-4, M-7.3 y M-7.4)', () => {
  const css = readFileSync(resolve(import.meta.dirname, 'tree.css'), 'utf8');
  /** Cuerpo de la primera regla cuyo selector contiene `selector` (se busca por texto: jsdom no aplica esta hoja). */
  const rule = (selector: string): string => {
    const found = [...css.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(/([^{}]+)\{([^{}]*)\}/g)].find((match) => match[1]!.split(',').some((part) => part.trim() === selector));
    expect(found, selector).toBeDefined();
    return found![2]!;
  };

  it('sólo las tarjetas con hijos lo dicen y reservan sitio para el plegado; una hoja usa todo el ancho', async () => {
    const view = await openDiagram('position');
    const cards = Array.from(view.container.querySelectorAll<HTMLElement>('.react-flow__node [data-kx-part="card"]'));
    expect(cards.length).toBeGreaterThan(1);
    for (const card of cards) {
      const hasFold = card.querySelector('[data-kx-part="fold"]') !== null;
      expect(card.getAttribute('data-kx-expandable') === 'true', card.textContent ?? '').toBe(hasFold);
    }
    expect(cards.some((card) => card.hasAttribute('data-kx-expandable'))).toBe(true);
    // La reserva lateral está en la regla de las tarjetas con hijos, no en la general.
    expect(rule('[data-kx-part="diagram"] [data-kx-part="card"] > [data-kx-part="main"]')).not.toContain('padding-inline-end');
    expect(rule('[data-kx-part="diagram"] [data-kx-part="card"][data-kx-expandable="true"] > [data-kx-part="main"]')).toContain(
      'padding-inline-end: calc(var(--th-space-8) * 2)',
    );
  });

  it('el rótulo y el subtítulo caben en dos líneas (line-clamp) y llevan el texto entero en `title`', async () => {
    const view = await openDiagram('position');
    const label = view.container.querySelector<HTMLElement>('.react-flow__node [data-kx-part="label"]')!;
    expect(label.getAttribute('title')).toBe(label.textContent);
    // El subtítulo de un `lookup` (el titular del puesto) se resuelve en pantalla: su `title` llega al pasar el puntero.
    const subtitle = view.container.querySelector<HTMLElement>('.react-flow__node [data-kx-part="subtitle"]')!;
    await view.user.hover(subtitle);
    expect(subtitle.getAttribute('title')).toBe(subtitle.textContent?.trim());
    expect(subtitle.getAttribute('title')).not.toBe('');
    // Dos líneas con elipsis sólo al pasar de ahí; ya no `nowrap` con elipsis en una.
    const text = rule('[data-kx-part="diagram"] [data-kx-part="card"] [data-kx-part="label"]');
    expect(text).toContain('-webkit-line-clamp: 2');
    expect(text).toContain('line-clamp: 2');
    expect(text).not.toContain('nowrap');
    // La tarjeta mide cuatro módulos del tema: sitio para 2 + 2 líneas y la insignia; la disposición del diagrama la mide de ahí.
    expect(rule('[data-kx-part="diagram"] [data-kx-part="card"]')).toContain('block-size: calc(var(--th-space-8) * 4)');
  });

  it('el contador del listado va justo detrás de su etiqueta: sin `margin-inline-start: auto`', () => {
    const body = rule('[data-kx-part="tree"] [data-kx-part="count"]');
    expect(body).not.toContain('auto');
    expect(body).toContain('flex: none');
  });
});

describe('RTL', () => {
  it('los nombres de los nodos van en <bdi> y el enlace a la lista sigue ahí', async () => {
    const view = await openDiagram('facility', { dir: 'rtl' });
    const label = view.container.querySelector('[data-kx-part="card"] [data-kx-part="label"]')!;
    expect(label.querySelector('bdi')).not.toBeNull();
    expect(screen.getByRole('link', { name: 'Ver como lista' })).toBeInTheDocument();
  });
});
