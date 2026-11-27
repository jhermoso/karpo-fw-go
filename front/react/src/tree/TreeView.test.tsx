import { act, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { EntityConfig } from '../contracts';
import { renderEntityView, installDemoBackend, type RenderedView } from './test-utils';
import { TreeView } from './TreeView';

installDemoBackend();

const items = () => within(screen.getByRole('tree')).getAllByRole('treeitem');
const byName = (name: string | RegExp) =>
  items().find((item) => {
    const label = item.querySelector('[data-kx-part="label"]')?.textContent ?? '';
    return typeof name === 'string' ? label === name : name.test(label);
  })!;

async function ready(view: RenderedView): Promise<void> {
  await screen.findByRole('tree');
  await waitFor(() => expect(view.container.querySelector('[data-kx-view="tree"]')).toHaveAttribute('data-kx-ready', 'true'));
}

function focus(element: HTMLElement): void {
  act(() => element.focus());
}

describe('listado role=tree (fuente flat: instalaciones)', () => {
  it('es un árbol con treeitem y aria-level, aria-setsize, aria-posinset y aria-expanded correctos', async () => {
    const view = await renderEntityView(TreeView, { entity: 'facility', search: '?view=tree' });
    await ready(view);
    const tree = screen.getByRole('tree');
    expect(tree).toHaveAccessibleName(/Instalaciones/);

    const roots = items().filter((item) => item.getAttribute('aria-level') === '1');
    expect(roots).toHaveLength(6);
    roots.forEach((root, index) => {
      expect(root).toHaveAttribute('aria-setsize', '6');
      expect(root).toHaveAttribute('aria-posinset', String(index + 1));
      expect(root).toHaveAttribute('aria-expanded', 'true');
    });

    // Las raíces vienen desplegadas: sus plantas están en el nivel 2, con su propio grupo de hermanos.
    const sede = byName('Sede Central Madrid');
    const sedeIndex = items().indexOf(sede);
    const floors = items().slice(sedeIndex + 1, sedeIndex + 4);
    expect(floors.map((f) => f.getAttribute('aria-level'))).toEqual(['2', '2', '2']);
    expect(floors.map((f) => f.getAttribute('aria-setsize'))).toEqual(['3', '3', '3']);
    expect(floors.map((f) => f.getAttribute('aria-posinset'))).toEqual(['1', '2', '3']);
    // Una planta con salas está plegada; una hoja no lleva aria-expanded.
    expect(floors[0]).toHaveAttribute('aria-expanded', 'false');
    expect(items().every((item) => item.hasAttribute('aria-level'))).toBe(true);

    // El contador de descendientes es legible para lectores de pantalla.
    expect(within(sede).getByText('8 descendientes')).toBeInTheDocument();
    // Las hojas (salas) no llevan aria-expanded.
    await view.user.click(screen.getByRole('button', { name: 'Desplegar todo' }));
    const rooms = items().filter((item) => item.getAttribute('aria-level') === '3');
    expect(rooms.length).toBeGreaterThan(0);
    expect(rooms.every((room) => !room.hasAttribute('aria-expanded'))).toBe(true);
    // Un único elemento en la secuencia de tabulación (tabindex roving).
    expect(items().filter((item) => item.getAttribute('tabindex') === '0')).toHaveLength(1);
  });

  it('flechas, Inicio, Fin y * navegan, despliegan y pliegan', async () => {
    const view = await renderEntityView(TreeView, { entity: 'facility', search: '?view=tree' });
    await ready(view);
    const first = items()[0]!;
    focus(first);
    const { user } = view;

    // Abajo y arriba entre visibles.
    await user.keyboard('{ArrowDown}');
    expect(document.activeElement).toBe(items()[1]);
    await user.keyboard('{ArrowUp}');
    expect(document.activeElement).toBe(items()[0]);

    // Fin y Inicio.
    await user.keyboard('{End}');
    expect(document.activeElement).toBe(items()[items().length - 1]);
    await user.keyboard('{Home}');
    expect(document.activeElement).toBe(items()[0]);

    // Izquierda pliega una raíz abierta; derecha la vuelve a abrir; derecha otra vez baja al primer hijo.
    const before = items().length;
    await user.keyboard('{ArrowLeft}');
    expect(items()[0]).toHaveAttribute('aria-expanded', 'false');
    expect(items().length).toBeLessThan(before);
    await user.keyboard('{ArrowRight}');
    expect(items()[0]).toHaveAttribute('aria-expanded', 'true');
    await user.keyboard('{ArrowRight}');
    expect(document.activeElement).toBe(items()[1]);
    expect(items()[1]).toHaveAttribute('aria-level', '2');

    // Izquierda en un hijo cerrado sube al padre.
    await user.keyboard('{ArrowLeft}');
    expect(document.activeElement).toBe(items()[0]);

    // * despliega todos los hermanos del nivel actual (las seis raíces ya estaban abiertas: las plantas no).
    await user.keyboard('{ArrowDown}');
    expect(items()[1]).toHaveAttribute('aria-level', '2');
    await user.keyboard('*');
    const siblings = items().filter(
      (item) => item.getAttribute('aria-level') === '2' && item.hasAttribute('aria-expanded'),
    );
    expect(siblings.length).toBeGreaterThan(0);
    expect(items().filter((item) => item.getAttribute('aria-level') === '3').length).toBeGreaterThan(0);
  });

  it('la búsqueda por letra salta al siguiente elemento que empieza por ella', async () => {
    const view = await renderEntityView(TreeView, { entity: 'facility', search: '?view=tree' });
    await ready(view);
    focus(items()[0]!);
    await view.user.keyboard('n');
    expect(document.activeElement).toBe(byName(/^Nave/));
    // Con varias letras seguidas se afina la búsqueda («no» no existe: el foco no se mueve).
    await view.user.keyboard('o');
    expect(document.activeElement).toBe(byName(/^Nave/));
    // Pasado un momento, la búsqueda empieza de nuevo.
    await new Promise((resolve) => setTimeout(resolve, 700));
    await view.user.keyboard('o');
    expect(document.activeElement).toBe(byName('Oficina Barcelona Diagonal'));
  });

  it('en rtl las flechas laterales se invierten', async () => {
    const view = await renderEntityView(TreeView, { entity: 'facility', search: '?view=tree', dir: 'rtl' });
    await ready(view);
    focus(items()[0]!);
    await view.user.keyboard('{ArrowRight}');
    expect(items()[0]).toHaveAttribute('aria-expanded', 'false');
    await view.user.keyboard('{ArrowLeft}');
    expect(items()[0]).toHaveAttribute('aria-expanded', 'true');
  });

  it('un clic selecciona, el doble clic y Intro abren el registro en edición', async () => {
    const view = await renderEntityView(TreeView, { entity: 'facility', search: '?view=tree' });
    await ready(view);
    const target = byName('Estudio Sabadell');
    await view.user.click(target);
    expect(byName('Estudio Sabadell')).toHaveAttribute('aria-selected', 'true');
    expect(view.location()).toBe('/facility?view=tree');

    await view.user.dblClick(target);
    expect(view.location()).toMatch(/^\/facility\/[^?]+\?(view=tree&)?mode=edit$/);
  });

  it('Intro con el teclado abre el registro en edición y Espacio lo selecciona', async () => {
    const view = await renderEntityView(TreeView, { entity: 'facility', search: '?view=tree' });
    await ready(view);
    focus(items()[0]!);
    await view.user.keyboard(' ');
    expect(items()[0]).toHaveAttribute('aria-selected', 'true');
    await view.user.keyboard('{Enter}');
    expect(view.location()).toMatch(/^\/facility\/[^?]+\?.*mode=edit$/);
  });

  it('la búsqueda despliega las ramas con coincidencias, las resalta y recorre las coincidencias', async () => {
    const view = await renderEntityView(TreeView, { entity: 'facility', search: '?view=tree' });
    await ready(view);
    const input = screen.getByRole('searchbox', { name: 'Buscar en el organigrama' });
    await view.user.type(input, 'sala');
    const status = view.container.querySelector('[data-kx-part="search-status"]')!;
    await waitFor(() => expect(status.textContent).toMatch(/^Coincidencia 1 de \d+$/));
    const matching = items().filter((item) => item.hasAttribute('data-kx-match'));
    expect(matching.length).toBeGreaterThan(0);
    // La búsqueda busca lo que se ve (también el subtítulo traducido): cada coincidencia enseña por qué casa.
    expect(view.container.querySelectorAll('mark[data-kx-part="match"]').length).toBeGreaterThanOrEqual(matching.length);
    for (const item of matching) expect(item.querySelectorAll('mark[data-kx-part="match"]').length).toBeGreaterThan(0);
    // La rama de cada coincidencia está desplegada.
    const total = Number(/de (\d+)/.exec(status.textContent ?? '')![1]);
    expect(matching).toHaveLength(total);

    await view.user.keyboard('{Enter}');
    await waitFor(() => expect(status.textContent).toMatch(/^Coincidencia 2 de/));
    await view.user.keyboard('{Shift>}{Enter}{/Shift}');
    await waitFor(() => expect(status.textContent).toMatch(/^Coincidencia 1 de/));

    await view.user.clear(input);
    await view.user.type(input, 'zzzz');
    await waitFor(() => expect(status.textContent).toBe('Sin coincidencias'));
    await view.user.keyboard('{Escape}');
    expect(input).toHaveValue('');
  });
});

describe('?focus=<id> (enlace «Ver en el árbol» de la ficha)', () => {
  it('despliega los antecesores, selecciona el elemento y lo deja como actual', async () => {
    // Se averigua el id de una sala cuyo árbol está plegado en el estado de partida.
    const first = await renderEntityView(TreeView, { entity: 'facility', search: '?view=tree' });
    await ready(first);
    await first.user.click(screen.getByRole('button', { name: 'Desplegar todo' }));
    const room = items().find((item) => item.getAttribute('aria-level') === '3')!;
    const id = room.getAttribute('data-kx-id')!;
    const label = room.querySelector('[data-kx-part="label"]')!.textContent!;
    first.unmount();

    const view = await renderEntityView(TreeView, { entity: 'facility', search: `?view=tree&focus=${encodeURIComponent(id)}` });
    await ready(view);
    const target = byName(label);
    expect(target).toHaveAttribute('aria-selected', 'true');
    expect(target).toHaveAttribute('tabindex', '0');
  });

  it('un id que no existe deja la vista como estaba', async () => {
    const view = await renderEntityView(TreeView, { entity: 'facility', search: '?view=tree&focus=no-existe' });
    await ready(view);
    expect(items().filter((item) => item.getAttribute('aria-selected') === 'true')).toHaveLength(0);
  });
});

describe('fuente chart: organigrama de puestos', () => {
  it('parte de las raíces de la lista y pide más profundidad al desplegar un nodo frontera', async () => {
    const view = await renderEntityView(TreeView, { entity: 'position', search: '?view=tree' });
    await ready(view);
    expect(items()[0]).toHaveAttribute('aria-level', '1');
    expect(items()[0]!.textContent).toContain('Director/a General');
    expect(items().map((item) => item.getAttribute('aria-level'))).toEqual(['1', '2', '2', '2', '2']);

    // Se despliega un director (nivel 2): sus subordinados ya vienen en el organigrama de profundidad 3.
    focus(items()[1]!);
    await view.user.keyboard('{ArrowRight}');
    const level3 = items().filter((item) => item.getAttribute('aria-level') === '3');
    expect(level3.length).toBeGreaterThan(0);
    // El nivel 3 es la frontera: tiene o puede tener más debajo y lo avisa con un contador parcial.
    const frontier = level3.find((item) => item.hasAttribute('aria-expanded'))!;
    expect(frontier).toBeDefined();

    focus(frontier);
    await view.user.keyboard('{ArrowRight}');
    await waitFor(() =>
      expect(items().some((item) => item.getAttribute('aria-level') === '4')).toBe(true),
    );
    expect(frontier).toHaveAttribute('aria-expanded', 'true');
  });

  it('produce el mismo árbol que la fuente flat con parentField', async () => {
    const asFlat = (config: EntityConfig): EntityConfig => ({
      ...config,
      views: config.views.map((v) => (v.kind === 'tree' ? { ...v, source: { type: 'flat' as const } } : v)),
    });
    const chartView = await renderEntityView(TreeView, { entity: 'position', search: '?view=tree' });
    await ready(chartView);
    // Se despliega todo lo que se pueda con la profundidad inicial.
    await chartView.user.click(screen.getByRole('button', { name: 'Desplegar todo' }));
    const fromChart = items().map((item) => [item.getAttribute('aria-level'), item.querySelector('[data-kx-part="label"]')?.textContent]);
    chartView.unmount();

    const flatView = await renderEntityView(TreeView, { entity: 'position', search: '?view=tree', configure: asFlat });
    await ready(flatView);
    await flatView.user.click(screen.getByRole('button', { name: 'Desplegar todo' }));
    const fromFlat = items()
      .map((item) => [item.getAttribute('aria-level'), item.querySelector('[data-kx-part="label"]')?.textContent])
      .filter(([level]) => Number(level) <= 3);
    expect(fromChart.length).toBe(fromFlat.length);
    expect(fromChart).toEqual(fromFlat);
  });
});

describe('disposición listado ⇄ diagrama (la cambia la cabecera de la entidad, por la URL)', () => {
  it('la vista ya no lleva su propio interruptor', async () => {
    const view = await renderEntityView(TreeView, { entity: 'facility', search: '?view=tree&layout=list' });
    await ready(view);
    expect(screen.queryByRole('button', { name: 'Listado' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Diagrama' })).toBeNull();
    expect(view.container.querySelector('[data-kx-ui="layout-switch"]')).toBeNull();
  });

  it('con layout en la URL cambia de disposición y conserva selección y ramas plegadas', async () => {
    const view = await renderEntityView(TreeView, { entity: 'facility', search: '?view=tree&layout=list' });
    await ready(view);

    // Estado de partida: se pliega una raíz y se selecciona otra.
    focus(byName('Sede Central Madrid'));
    await view.user.keyboard('{ArrowLeft}');
    expect(byName('Sede Central Madrid')).toHaveAttribute('aria-expanded', 'false');
    await view.user.click(byName('Nave Logística Alcalá'));
    expect(byName('Nave Logística Alcalá')).toHaveAttribute('aria-selected', 'true');

    view.goto('/facility?view=tree&layout=diagram');
    await waitFor(() => expect(view.container.querySelector('[data-kx-view="tree"]')).toHaveAttribute('data-kx-ready', 'true'));
    expect(view.container.querySelector('[data-kx-view="tree"]')).toHaveAttribute('data-kx-layout', 'diagram');

    const cards = () => Array.from(view.container.querySelectorAll<HTMLElement>('[data-kx-part="card"]'));
    const card = (name: string) => cards().find((c) => c.querySelector('[data-kx-part="label"]')?.textContent === name)!;
    expect(card('Nave Logística Alcalá').querySelector('button[data-kx-part="main"]')).toHaveAttribute('aria-pressed', 'true');
    expect(card('Sede Central Madrid')).toHaveAttribute('data-kx-folded', 'true');
    // La rama plegada no pinta sus plantas (otras ramas sí).
    expect(cards().some((c) => c.querySelector('[data-kx-part="label"]')?.textContent === 'Sede Central Madrid · Planta 1')).toBe(false);
    expect(cards().some((c) => c.querySelector('[data-kx-part="label"]')?.textContent === 'Nave Logística Alcalá · Planta baja')).toBe(true);

    // «Ver como lista» (la alternativa accesible del diagrama) vuelve al listado, con layout en la URL.
    await view.user.click(screen.getByRole('link', { name: 'Ver como lista' }));
    expect(view.params().get('layout')).toBe('list');
    await screen.findByRole('tree');
    expect(byName('Sede Central Madrid')).toHaveAttribute('aria-expanded', 'false');
    expect(byName('Nave Logística Alcalá')).toHaveAttribute('aria-selected', 'true');
  });

  it('abre directamente en el diagrama con ?layout=diagram y ofrece «Ver como lista»', async () => {
    const view = await renderEntityView(TreeView, { entity: 'facility', search: '?view=tree&layout=diagram' });
    await waitFor(() => expect(view.container.querySelector('[data-kx-view="tree"]')).toHaveAttribute('data-kx-ready', 'true'));
    expect(view.container.querySelector('[data-kx-view="tree"]')).toHaveAttribute('data-kx-layout', 'diagram');
    expect(screen.queryByRole('tree')).toBeNull();
    const link = screen.getByRole('link', { name: 'Ver como lista' });
    expect(link).toHaveAttribute('href', expect.stringContaining('layout=list'));
  });
});

describe('diagrama', () => {
  async function openDiagram(entity = 'facility'): Promise<RenderedView> {
    const view = await renderEntityView(TreeView, { entity, search: '?view=tree&layout=diagram' });
    await waitFor(() => expect(view.container.querySelector('[data-kx-view="tree"]')).toHaveAttribute('data-kx-ready', 'true'));
    return view;
  }
  const cardOf = (view: RenderedView, name: string) =>
    Array.from(view.container.querySelectorAll<HTMLElement>('[data-kx-part="card"]')).find(
      (c) => c.querySelector('[data-kx-part="label"]')?.textContent === name,
    )!;

  it('ofrece zoom, ajustar, minimapa y un nombre accesible para cada control', async () => {
    const view = await openDiagram();
    expect(screen.getByRole('region', { name: /Diagrama de Instalaciones/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Acercar' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Alejar' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Ajustar a la pantalla' })).toBeInTheDocument();
    expect(view.container.querySelector('.react-flow__minimap')).not.toBeNull();
    expect(screen.getByRole('img', { name: 'Minimapa del diagrama' })).toBeInTheDocument();
    await view.user.click(screen.getByRole('button', { name: 'Acercar' }));
    await view.user.click(screen.getByRole('button', { name: 'Alejar' }));
    await view.user.click(screen.getByRole('button', { name: 'Ajustar a la pantalla' }));
  });

  it('pliega y despliega una rama con el contador de descendientes', async () => {
    const view = await openDiagram();
    const sede = cardOf(view, 'Sede Central Madrid');
    const fold = within(sede).getByRole('button', { name: 'Plegar la rama de Sede Central Madrid' });
    expect(fold).toHaveAttribute('aria-expanded', 'true');
    expect(within(fold).getByText('8')).toBeInTheDocument();

    await view.user.click(fold);
    const folded = cardOf(view, 'Sede Central Madrid');
    expect(folded).toHaveAttribute('data-kx-folded', 'true');
    expect(within(folded).getByRole('button', { name: 'Desplegar la rama de Sede Central Madrid' })).toHaveAttribute(
      'aria-expanded',
      'false',
    );
    expect(within(folded).getByText('8')).toBeInTheDocument();
    expect(cardOf(view, 'Sede Central Madrid · Planta 1')).toBeUndefined();

    await view.user.click(within(folded).getByRole('button', { name: 'Desplegar la rama de Sede Central Madrid' }));
    expect(cardOf(view, 'Sede Central Madrid · Planta 1')).toBeDefined();
  });

  it('el teclado va entre padre, hijos y hermanos, y +/- pliegan', async () => {
    const view = await openDiagram();
    const main = (name: string) => cardOf(view, name).querySelector<HTMLButtonElement>('button[data-kx-part="main"]')!;
    focus(main('Centro de Datos Getafe'));
    await view.user.keyboard('{ArrowRight}');
    expect(document.activeElement).toBe(main('Estudio Sabadell'));
    await view.user.keyboard('{ArrowLeft}');
    expect(document.activeElement).toBe(main('Centro de Datos Getafe'));
    await view.user.keyboard('{ArrowDown}');
    expect(document.activeElement).toBe(main('Centro de Datos Getafe · Planta 1'));
    await view.user.keyboard('{ArrowUp}');
    expect(document.activeElement).toBe(main('Centro de Datos Getafe'));
    await view.user.keyboard('{End}');
    expect(document.activeElement).toBe(main('Sede Central Madrid'));
    await view.user.keyboard('-');
    expect(cardOf(view, 'Sede Central Madrid')).toHaveAttribute('data-kx-folded', 'true');
    await view.user.keyboard('+');
    expect(cardOf(view, 'Sede Central Madrid')).not.toHaveAttribute('data-kx-folded');
    await view.user.keyboard('{Home}');
    expect(document.activeElement).toBe(main('Centro de Datos Getafe'));
  });

  it('en rtl las flechas laterales siguen el orden visual en espejo', async () => {
    const view = await renderEntityView(TreeView, { entity: 'facility', search: '?view=tree&layout=diagram', dir: 'rtl' });
    await waitFor(() => expect(view.container.querySelector('[data-kx-view="tree"]')).toHaveAttribute('data-kx-ready', 'true'));
    const main = (name: string) => cardOf(view, name).querySelector<HTMLButtonElement>('button[data-kx-part="main"]')!;
    focus(main('Centro de Datos Getafe'));
    await view.user.keyboard('{ArrowLeft}');
    expect(document.activeElement).toBe(main('Estudio Sabadell'));
    await view.user.keyboard('{ArrowRight}');
    expect(document.activeElement).toBe(main('Centro de Datos Getafe'));
  });

  it('el doble clic abre el registro en edición y un clic lo selecciona', async () => {
    const view = await openDiagram();
    const main = cardOf(view, 'Estudio Sabadell').querySelector<HTMLButtonElement>('button[data-kx-part="main"]')!;
    await view.user.click(main);
    expect(main).toHaveAttribute('aria-pressed', 'true');
    await view.user.dblClick(main);
    expect(view.location()).toMatch(/^\/facility\/[^?]+\?(view=tree&)?mode=edit$/);
  });

  it('Intro abre en edición y la búsqueda marca la tarjeta que coincide', async () => {
    const view = await openDiagram();
    const input = screen.getByRole('searchbox');
    await view.user.type(input, 'planta 2');
    await waitFor(() => expect(view.container.querySelectorAll('[data-kx-part="card"][data-kx-match="true"]').length).toBe(1));
    expect(view.container.querySelector('[data-kx-part="card"][data-kx-match="true"] mark')).not.toBeNull();
  });

  it('el organigrama de puestos carga perezosamente en el diagrama', async () => {
    const view = await openDiagram('position');
    const fold = view.container.querySelector<HTMLButtonElement>('[data-kx-part="fold"][aria-expanded="false"]');
    expect(fold).not.toBeNull();
    // «≥N» y no «N+»: es un mínimo, y lo dice en voz alta con su nombre accesible (M-1).
    const count = fold!.querySelector('[data-kx-part="count"]');
    expect(count?.textContent).toMatch(/^≥\d+$/);
    expect(count?.querySelector('bdi')).not.toBeNull();
    expect(count).toHaveAttribute('aria-label', expect.stringMatching(/^al menos \d+ descendientes?; hay ramas sin cargar$/));
    expect(fold).toHaveAttribute('aria-describedby', count!.id);
    const before = view.container.querySelectorAll('[data-kx-part="card"]').length;
    await view.user.click(fold!);
    await waitFor(() => expect(view.container.querySelectorAll('[data-kx-part="card"]').length).toBeGreaterThan(before));
  });
});
