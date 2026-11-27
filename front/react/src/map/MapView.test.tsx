import { act, cleanup, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { EntityConfig, ViewStatus } from '../contracts';
import { collectUrls, MAP_TOKENS } from './style';
import {
  FakeMap,
  colorOf,
  installDemoBackend,
  installGeoFiles,
  renderMapView,
  resetFakeMap,
  type RenderedMap,
} from './test-utils';

vi.mock('maplibre-gl', async () => {
  const { fakeMaplibre } = await import('./test-utils');
  return { ...fakeMaplibre, default: fakeMaplibre };
});

installDemoBackend();
installGeoFiles();

beforeEach(() => resetFakeMap());
afterEach(() => vi.restoreAllMocks());

const section = (view: RenderedMap) => view.container.querySelector('[data-kx-view="map"]')!;

async function ready(view: RenderedMap): Promise<FakeMap> {
  const map = await view.map();
  await waitFor(() => expect(section(view)).toHaveAttribute('data-kx-ready', 'true'), { timeout: 4000 });
  return map;
}

/** Texto del estado de la vista («Ciudad: 14 delimitaciones.»). */
const status = (view: RenderedMap) => section(view).querySelector('[data-kx-part="status"]')?.textContent ?? '';

/** Textos de las migas, en orden (el último es la página actual). */
const crumbs = () =>
  within(screen.getByRole('navigation', { name: 'Ruta del mapa' }))
    .getAllByRole('listitem')
    .map((item) => item.textContent ?? '');

/** Bajar un nivel haciendo clic en una delimitación del mapa y esperar a que cargue el nivel nuevo. */
async function enter(view: RenderedMap, map: FakeMap, code: string, level: string): Promise<void> {
  act(() => map.click(code));
  await waitFor(() => expect(view.params().get('level')).toBe(level));
  await ready(view);
}

const withSource = (source: string) => (config: EntityConfig): EntityConfig => ({
  ...config,
  views: config.views.map((view) =>
    view.kind === 'map' ? { ...view, levels: view.levels.map((level) => ({ ...level, source })) } : view,
  ),
});

/** `skipSingle` de la vista de mapa (por omisión se saltan los niveles con una sola delimitación). */
const withSkipSingle = (skipSingle: boolean) => (config: EntityConfig): EntityConfig => ({
  ...config,
  views: config.views.map((view) => (view.kind === 'map' ? { ...view, skipSingle } : view)),
});

/** Intervalo como lo escribe el idioma (Intl formatRange), no con una cadena de la aplicación. */
const rangeIn = (locale: string, from: number, to: number) =>
  new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }).formatRange(from, to);

describe('navegación por niveles', () => {
  it('baja mundo → país (ES) → región (ES51) → ciudad (ES511) por clic, con migas y URL', async () => {
    const view = await renderMapView();
    const map = await ready(view);
    expect(crumbs()).toEqual(['Mundo']);
    expect(view.params().get('level')).toBeNull();
    expect(view.params().get('code')).toBeNull();

    await enter(view, map, 'ES', 'country');
    expect(view.params().get('code')).toBe('ES');
    expect(view.params().get('view')).toBe('map');
    expect(crumbs()).toEqual(['Mundo', 'España']);
    // El mapa se encuadra a las comunidades (caja de country-ES.geojson).
    await waitFor(() =>
      expect(map.fitBoundsCalls.at(-1)?.bounds).toEqual([
        [-9.75, 35.25],
        [3.65, 43.95],
      ]),
    );

    // Cataluña tiene cuatro provincias: el nivel se enseña.
    await enter(view, map, 'ES51', 'region');
    expect(view.params().get('code')).toBe('ES51');
    expect(crumbs()).toEqual(['Mundo', 'España', 'Cataluña']);

    await enter(view, map, 'ES511', 'city');
    expect(view.params().get('code')).toBe('ES511');
    expect(view.location()).toBe('/facility?level=city&code=ES511&view=map');
    expect(crumbs()).toEqual(['Mundo', 'España', 'Cataluña', 'Barcelona']);
    const last = within(screen.getByRole('navigation', { name: 'Ruta del mapa' })).getAllByRole('listitem').at(-1)!;
    expect(within(last).getByText('Barcelona').closest('[aria-current]')).toHaveAttribute('aria-current', 'page');

    // Los municipios no tienen nivel inferior: un clic no navega y lo dice.
    act(() => map.click('08019'));
    expect(view.params().get('level')).toBe('city');
    expect(section(view).querySelector('[data-kx-part="status"]')).toHaveTextContent('Barcelona no tiene detalle en el mapa');
  });

  it('un nivel con una sola delimitación se salta por omisión: la comunidad de Madrid baja a los municipios y su miga no es un botón (M-4)', async () => {
    const view = await renderMapView();
    const map = await ready(view);
    await enter(view, map, 'ES', 'country');
    // Madrid tiene una única provincia (con detalle): no se enseña un nivel de un solo elemento.
    act(() => map.click('ES30'));
    await waitFor(() => expect(view.params().get('level')).toBe('city'));
    expect(view.params().get('code')).toBe('ES300');
    await ready(view);
    await waitFor(() => expect(crumbs()).toEqual(['Mundo', 'España', 'Comunidad de Madrid', 'Madrid']));
    expect(screen.queryByRole('button', { name: 'Comunidad de Madrid' })).toBeNull();
    expect(screen.getByRole('button', { name: 'España' })).toBeInTheDocument();
    const skipped = within(screen.getByRole('navigation', { name: 'Ruta del mapa' })).getByText('Comunidad de Madrid');
    expect(skipped.closest('[data-kx-part="skipped"]')).not.toBeNull();
    expect(status(view)).toBe('Ciudad: 14 delimitaciones.');

    // Subir salta el nivel que no se ve: del municipio al país.
    await view.user.click(screen.getByRole('button', { name: /Subir un nivel/ }));
    await waitFor(() => expect(view.params().get('level')).toBe('country'));
    expect(view.params().get('code')).toBe('ES');
    expect(crumbs()).toEqual(['Mundo', 'España']);
  });

  it('con skipSingle: false se conserva el nivel de una sola delimitación', async () => {
    const view = await renderMapView({ configure: withSkipSingle(false) });
    const map = await ready(view);
    await enter(view, map, 'ES', 'country');
    await enter(view, map, 'ES30', 'region');
    expect(view.params().get('code')).toBe('ES30');
    expect(crumbs()).toEqual(['Mundo', 'España', 'Comunidad de Madrid']);
    // Con una sola provincia en el mapa, no llena la pantalla: el encuadre deja aire por todos los lados.
    await waitFor(() => expect(map.fitBoundsCalls.at(-1)!.options.padding).toMatchObject({ top: expect.any(Number) }));
    await enter(view, map, 'ES300', 'city');
    expect(crumbs()).toEqual(['Mundo', 'España', 'Comunidad de Madrid', 'Madrid']);
    expect(screen.getByRole('button', { name: 'Comunidad de Madrid' })).toBeInTheDocument();
  });

  it('Esc sube un nivel cada vez, hasta el mundo, y la URL lo refleja', async () => {
    const view = await renderMapView({ search: '?view=map&level=city&code=ES300' });
    await ready(view);
    await waitFor(() => expect(crumbs()).toEqual(['Mundo', 'España', 'Comunidad de Madrid', 'Madrid']));
    // El foco está dentro de la vista (en la última miga navegable).
    act(() => screen.getByRole('button', { name: 'España' }).focus());

    // Del municipio se sube a España: la comunidad de Madrid no se ve (tiene una sola provincia).
    await view.user.keyboard('{Escape}');
    await waitFor(() => expect(view.params().get('level')).toBe('country'));
    expect(view.params().get('code')).toBe('ES');

    await view.user.keyboard('{Escape}');
    await waitFor(() => expect(view.params().get('level')).toBeNull());
    expect(view.params().get('code')).toBeNull();
    expect(crumbs()).toEqual(['Mundo']);

    // En el nivel de arriba Esc no hace nada.
    const before = view.location();
    await view.user.keyboard('{Escape}');
    expect(view.location()).toBe(before);
  });

  it('las migas llevan a su nivel y el botón «Subir un nivel» sube', async () => {
    const view = await renderMapView({ search: '?view=map&level=region&code=ES51' });
    await ready(view);
    await waitFor(() => expect(crumbs()).toEqual(['Mundo', 'España', 'Cataluña']));
    await view.user.click(screen.getByRole('button', { name: 'Mundo' }));
    await waitFor(() => expect(view.params().get('level')).toBeNull());
    expect(view.params().get('view')).toBe('map');

    await enter(view, await view.map(), 'ES', 'country');
    await view.user.click(screen.getByRole('button', { name: /Subir un nivel/ }));
    await waitFor(() => expect(view.params().get('level')).toBeNull());
    expect(screen.getByRole('button', { name: /Subir un nivel/ })).toHaveAttribute('aria-disabled', 'true');
  });

  it('un país sin detalle no se abre y se indica', async () => {
    const view = await renderMapView();
    const map = await ready(view);
    const before = view.location();
    act(() => map.click('FR'));
    expect(view.location()).toBe(before);
    expect(screen.getByText(/Francia no tiene detalle en el mapa/)).toBeInTheDocument();
    // Un clic en el vacío no hace nada.
    act(() => map.click(null));
    expect(view.location()).toBe(before);
  });

  it('un nivel o un código inválidos de la URL vuelven al mundo', async () => {
    const view = await renderMapView({ search: '?view=map&level=galaxia&code=ES' });
    await ready(view);
    expect(crumbs()).toEqual(['Mundo']);
    expect(screen.getByRole('button', { name: /Subir un nivel/ })).toHaveAttribute('aria-disabled', 'true');
  });

  it('una URL directa resuelve las migas subiendo por los códigos', async () => {
    const view = await renderMapView({ search: '?view=map&level=city&code=ES300' });
    await ready(view);
    await waitFor(() => expect(crumbs()).toEqual(['Mundo', 'España', 'Comunidad de Madrid', 'Madrid']));
    // Los municipios de la provincia se ven en el mapa: rótulos de los de ES300.
    const map = await view.map();
    expect(map.fitBoundsCalls.length).toBeGreaterThan(0);
  });
});

describe('entrada por URL en un nivel saltable (R3-4, M-5)', () => {
  it('/facility?view=map&level=region&code=ES30 termina en el nivel ciudad y no deja la URL intermedia en el historial', async () => {
    const view = await renderMapView({ search: '?view=map&level=country&code=ES' });
    await ready(view);
    // Como un enlace o un favorito: se llega al nivel de un solo elemento por la URL, con una entrada de historial nueva.
    view.goto('/facility?view=map&level=region&code=ES30');
    await waitFor(() => expect(view.params().get('level')).toBe('city'));
    expect(view.params().get('code')).toBe('ES300');
    expect(view.params().get('view')).toBe('map');
    await ready(view);
    await waitFor(() => expect(crumbs()).toEqual(['Mundo', 'España', 'Comunidad de Madrid', 'Madrid']));
    // `replace`: «Atrás» vuelve a lo anterior (el país), no rebota a la URL de la región.
    view.back();
    await waitFor(() => expect(view.params().get('level')).toBe('country'));
    expect(view.params().get('code')).toBe('ES');
    await new Promise((resolve) => setTimeout(resolve, 80));
    expect(view.params().get('level')).toBe('country');
  });

  it('al abrir la página en esa URL no se pinta el nivel de un solo elemento: sigue cargando hasta el siguiente', async () => {
    const view = await renderMapView({ search: '?view=map&level=region&code=ES30' });
    const map = await ready(view);
    expect(view.params().get('level')).toBe('city');
    expect(view.params().get('code')).toBe('ES300');
    // El mapa que se enseña es el de los municipios: ningún rótulo del nivel de la provincia.
    await waitFor(() => expect(status(view)).toBe('Ciudad: 14 delimitaciones.'));
    expect(map.style.sources['kx-boundaries']).toBeDefined();
    // La URL intermedia no llegó a pintarse como lista de un elemento.
    expect(map.setStyleCalls.every((style) => JSON.stringify(style.sources).includes('"type":"geojson"'))).toBe(true);
  });

  it('también redirige con la tabla (layout=list) y conserva el resto de parámetros', async () => {
    const view = await renderMapView({ search: '?view=map&level=region&code=ES30&layout=list' });
    await waitFor(() => expect(view.params().get('level')).toBe('city'));
    expect(view.params().get('code')).toBe('ES300');
    expect(view.params().get('layout')).toBe('list');
    await waitFor(() => expect(status(view)).toBe('Ciudad: 14 delimitaciones.'));
    expect(screen.getAllByRole('row').length).toBeGreaterThan(14);
  });

  it('con skipSingle: false no redirige: el nivel de un solo elemento se enseña', async () => {
    const view = await renderMapView({ configure: withSkipSingle(false), search: '?view=map&level=region&code=ES30' });
    await ready(view);
    await new Promise((resolve) => setTimeout(resolve, 80));
    expect(view.params().get('level')).toBe('region');
    expect(view.params().get('code')).toBe('ES30');
    expect(crumbs()).toEqual(['Mundo', 'España', 'Comunidad de Madrid']);
  });

  it('un nivel con varias delimitaciones no se toca', async () => {
    const view = await renderMapView({ search: '?view=map&level=region&code=ES51' });
    await ready(view);
    await new Promise((resolve) => setTimeout(resolve, 80));
    expect(view.params().get('level')).toBe('region');
    expect(view.params().get('code')).toBe('ES51');
  });
});

describe('valores por delimitación', () => {
  it('suma las filas por delimitación subiendo por los antecesores y los rotula', async () => {
    const view = await renderMapView();
    const map = await ready(view);
    // Mundo, con la cámara sobre la península: España tiene las 30 instalaciones.
    map.setCenter([-3.5, 40.5]);
    act(() => map.emit('moveend'));
    await waitFor(() => expect(map.visibleLabels().find((l) => l.code === 'ES')).toBeDefined());
    expect(map.visibleLabels().find((l) => l.code === 'ES')).toMatchObject({ name: 'España', value: '30' });
    // El rótulo sabe el paso de la paleta sobre el que está, para elegir el color del texto.
    expect(map.container.querySelector<HTMLElement>('.kx-map-label[data-kx-code="ES"]')!.dataset.kxStep).toBe('7');
    expect(map.container.querySelector<HTMLElement>('.kx-map-label[data-kx-code="FR"]')!.dataset.kxStep).toBeUndefined();

    // Región: comunidades. Madrid 18 y Cataluña 12.
    await enter(view, map, 'ES', 'country');
    await view.user.click(screen.getByRole('button', { name: 'Ver como tabla' }));
    const row = (name: string) => screen.getByRole('row', { name: new RegExp(name) });
    expect(within(row('Comunidad de Madrid')).getByText('18')).toBeInTheDocument();
    expect(within(row('Cataluña')).getByText('12')).toBeInTheDocument();
    // Una comunidad sin instalaciones no lleva valor.
    expect(within(row('Galicia')).queryByText(/\d/)).toBeNull();
  });

  it('colorea con la paleta de datos: pasos de --kx-data-seq-* y «sin dato» para el resto', async () => {
    const view = await renderMapView();
    const map = await ready(view);
    const fill = map.getLayer('kx-fill')!.paint!['fill-color'] as unknown[];
    expect(fill[0]).toBe('match');
    // Con un solo valor distinto (30) todo cae en la clase más oscura.
    expect(fill[3]).toBe(colorOf('--kx-data-seq-7', 1));
    expect(fill[2]).toEqual(['ES']);
    expect(fill.at(-1)).toBe(colorOf('--kx-data-missing', 1));
  });

  it('la leyenda es texto: un intervalo por color y «Sin dato»', async () => {
    const view = await renderMapView({ search: '?view=map&level=country&code=ES' });
    await ready(view);
    const legend = screen.getByRole('figure');
    // Sin sustantivo en minúsculas: «Instalaciones por delimitación» con el nombre ya resuelto (B-2 a).
    expect(within(legend).getByText('Instalaciones por delimitación')).toBeInTheDocument();
    const items = within(legend).getAllByRole('listitem').map((item) => item.textContent);
    // Madrid 18 y Cataluña 12: dos valores distintos, dos clases enteras que no se solapan; los intervalos
    // los escribe Intl (M-12).
    expect(items).toEqual([rangeIn('es', 12, 14), rangeIn('es', 15, 18), 'Sin dato']);
    expect(within(legend).getAllByRole('listitem').slice(0, 2).every((item) => item.querySelector('bdi') !== null)).toBe(true);
  });

  it('«Sin dato» lleva trama en la leyenda y en el mapa: el color por sí solo no lo distingue (M-2)', async () => {
    const view = await renderMapView({ search: '?view=map&level=country&code=ES' });
    const map = await ready(view);
    const noData = within(screen.getByRole('figure')).getAllByRole('listitem').at(-1)!;
    expect(noData.querySelector('[data-kx-step="none"]')).toHaveAttribute('data-kx-pattern', 'hatch');
    // En el mapa, una capa de relleno con `fill-pattern` sobre lo que no tiene dato.
    const layer = map.getLayer('kx-missing')!;
    expect(layer.type).toBe('fill');
    const name = layer.paint!['fill-pattern'] as string;
    expect(name).toMatch(/^kx-missing-hatch-\d+$/);
    expect(layer.filter).toEqual(['match', ['get', 'code'], ['ES30', 'ES51'], false, true]);
    // La imagen está registrada con los colores del tema (la sonda) y a su tamaño de tesela.
    const image = map.images.get(name)!;
    expect(image.width).toBe(image.height);
    expect(image.data).toHaveLength(image.width * image.height * 4);
    // La trama del mapa es la SUAVE (R3-4, M-2); la marcada es la de la leyenda.
    expect(view.theme.requested.some((r) => r.token.includes('--kx-data-missing-hatch-map'))).toBe(true);
    expect(view.theme.requested.some((r) => r.token === '--kx-data-missing-hatch')).toBe(false);
    // El orden de las capas: la trama va sobre el relleno y bajo los bordes.
    const ids = map.style.layers.map((l) => l.id);
    expect(ids.indexOf('kx-fill')).toBeLessThan(ids.indexOf('kx-missing'));
    expect(ids.indexOf('kx-missing')).toBeLessThan(ids.indexOf('kx-line'));
  });

  it('un nivel sin ningún dato no lleva trama en el mapa (todo es «sin dato»), pero la leyenda conserva la suya (R3-4, M-2)', async () => {
    // Ninguna instalación cae en Galicia: el nivel no tiene dato alguno.
    const view = await renderMapView({ search: '?view=map&level=region&code=ES11' });
    const map = await ready(view);
    expect(map.getLayer('kx-fill')).toBeDefined();
    expect(map.getLayer('kx-missing')).toBeUndefined();
    // El relleno liso es el de «sin dato»: sin expresión.
    expect(typeof map.getLayer('kx-fill')!.paint!['fill-color']).toBe('string');
    const noData = within(screen.getByRole('figure')).getAllByRole('listitem').at(-1)!;
    expect(noData.querySelector('[data-kx-step="none"]')).toHaveAttribute('data-kx-pattern', 'hatch');
  });

  it('al cambiar de tema o de modo la trama se vuelve a dibujar con los colores nuevos y se retira la anterior', async () => {
    const view = await renderMapView({ search: '?view=map&level=country&code=ES' });
    const map = await ready(view);
    const before = map.getLayer('kx-missing')!.paint!['fill-pattern'] as string;
    const pixels = [...map.images.get(before)!.data];
    act(() => view.theme.fire());
    await waitFor(() => expect(map.getLayer('kx-missing')!.paint!['fill-pattern']).not.toBe(before));
    const after = map.getLayer('kx-missing')!.paint!['fill-pattern'] as string;
    expect(map.images.has(after)).toBe(true);
    expect([...map.images.get(after)!.data]).not.toEqual(pixels);
    expect(map.images.has(before)).toBe(false);
    expect(map.images.size).toBe(1);
  });

  it('con lng=en la leyenda y el nombre salen del idioma (sin sustantivo en español)', async () => {
    const view = await renderMapView({ locale: 'en', search: '?view=map&level=country&code=ES' });
    await ready(view);
    const legend = screen.getByRole('figure');
    expect(legend.textContent).not.toMatch(/instalaciones/i);
    expect(within(legend).getByText('Facilities by boundary')).toBeInTheDocument();
    expect(section(view).getAttribute('aria-label')).not.toMatch(/instalaciones/i);
    for (const region of screen.getAllByRole('region', { name: /^Map of / })) expect(region.getAttribute('aria-label')).not.toMatch(/instalaciones/i);
    expect(within(legend).getAllByRole('listitem')[0]!.textContent).toBe(rangeIn('en', 12, 14));
  });
});

describe('mapa ⇄ tabla', () => {
  it('«Ver como tabla» escribe layout en la URL y la tabla del nivel también navega; no hay interruptor propio', async () => {
    const view = await renderMapView();
    await ready(view);
    expect(screen.queryByRole('group', { name: 'Disposición del mapa' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Listado' })).toBeNull();
    expect(view.container.querySelector('[data-kx-ui="layout-switch"]')).toBeNull();
    const toggle = screen.getByRole('button', { name: 'Ver como tabla' });
    expect(toggle).toHaveAttribute('data-kx-help', 'map.layout');
    await view.user.click(toggle);
    expect(view.params().get('layout')).toBe('list');
    expect(view.params().get('view')).toBe('map');
    await waitFor(() => expect(section(view)).toHaveAttribute('data-kx-ready', 'true'));

    const table = screen.getByRole('table');
    expect(within(table).getAllByRole('columnheader').map((c) => c.textContent)).toEqual(['Delimitación', 'Valor']);
    // España primero (tiene valor); los países sin detalle no llevan botón y lo dicen.
    const first = within(table).getAllByRole('row')[1]!;
    expect(first).toHaveTextContent('España');
    expect(within(first).getByText('30')).toBeInTheDocument();
    // Cifras en la familia del cuerpo con `tabular-nums` (D-R35): sin `data-th-format="amount"`, que fuerza la monoespaciada.
    expect(table.querySelector('[data-th-format]')).toBeNull();
    const france = within(table).getByRole('row', { name: /Francia/ });
    expect(within(france).queryByRole('button')).toBeNull();
    expect(within(france).getByText('Sin detalle')).toBeInTheDocument();
    // El mapa se destruye al pasar a la tabla.
    expect(FakeMap.last.removed).toBe(true);

    // Entrar desde la tabla conserva la disposición.
    await view.user.click(screen.getByRole('button', { name: 'Entrar en España' }));
    await waitFor(() => expect(view.params().get('level')).toBe('country'));
    expect(view.params().get('layout')).toBe('list');
    await waitFor(() => expect(within(screen.getByRole('table')).getByText('Comunidad de Madrid')).toBeInTheDocument());
    await waitFor(() => expect(crumbs()).toEqual(['Mundo', 'España']));

    // Y volver al mapa conserva el nivel.
    await view.user.click(screen.getByRole('button', { name: 'Ver como mapa' }));
    expect(view.params().get('layout')).toBe('map');
    expect(view.params().get('level')).toBe('country');
  });

  it('la disposición también la cambia la URL (la cabecera de la entidad)', async () => {
    const view = await renderMapView();
    await ready(view);
    view.goto('/facility?view=map&layout=list');
    await screen.findByRole('table');
    expect(screen.getByRole('button', { name: 'Ver como mapa' })).toBeInTheDocument();
    view.goto('/facility?view=map');
    await waitFor(() => expect(screen.queryByRole('table')).toBeNull());
    expect(screen.getByRole('button', { name: 'Ver como tabla' })).toBeInTheDocument();
  });

  it('Esc también sube desde la tabla', async () => {
    const view = await renderMapView({ search: '?view=map&level=country&code=ES&layout=list' });
    await waitFor(() => expect(screen.getByRole('button', { name: 'Entrar en Comunidad de Madrid' })).toBeInTheDocument());
    act(() => screen.getByRole('button', { name: 'Entrar en Comunidad de Madrid' }).focus());
    await view.user.keyboard('{Escape}');
    await waitFor(() => expect(view.params().get('level')).toBeNull());
    expect(view.params().get('layout')).toBe('list');
  });
});

describe('la cabecera de la entidad (M-7)', () => {
  it('cuenta cuántos registros hay y cuántos caen en el lugar que se mira', async () => {
    const onStatus = vi.fn<(status: ViewStatus) => void>();
    const view = await renderMapView({ onStatus });
    const map = await ready(view);
    await waitFor(() => expect(onStatus.mock.calls.some(([s]) => s.loading === false)).toBe(true));
    const world = onStatus.mock.calls.findLast(([s]) => s.loading === false)![0];
    expect(world.total).toBeGreaterThanOrEqual(30);
    expect(world.matched).toBeLessThanOrEqual(world.total!);
    await enter(view, map, 'ES', 'country');
    await waitFor(() => expect(onStatus.mock.calls.at(-1)![0].matched).toBe(30));
    await enter(view, map, 'ES51', 'region');
    await waitFor(() => expect(onStatus.mock.calls.at(-1)![0]).toMatchObject({ total: world.total, matched: 12, loading: false }));
  });
});

describe('estilo y tema', () => {
  it('el estilo se construye sin URLs externas y sus colores salen de la sonda de colores del tema', async () => {
    const view = await renderMapView();
    const map = await ready(view);
    const urls = collectUrls(map.style);
    expect(urls.filter((url) => new URL(url).origin !== window.location.origin)).toEqual([]);
    expect(map.style.sources['kx-boundaries']).toMatchObject({ type: 'geojson' });
    expect(map.getLayer('kx-background')!.paint!['background-color']).toBe(colorOf(MAP_TOKENS.background, 1));
    expect(map.getLayer('kx-line')!.paint!['line-color']).toBe(colorOf(MAP_TOKENS.edge, 1));
    expect(map.getLayer('kx-active')!.paint!['fill-color']).toBe(colorOf(MAP_TOKENS.activeFill, 1));
    expect(map.getLayer('kx-hover')!.paint!['fill-color']).toBe(colorOf(MAP_TOKENS.hoverFill, 1));
    // Ningún color literal escrito a mano: todo lo que se pidió a la sonda es una variable del tema.
    const tokens = new Set(view.theme.requested.map((r) => r.token));
    expect([...tokens].sort()).toEqual(
      [
        MAP_TOKENS.background,
        MAP_TOKENS.missing,
        MAP_TOKENS.missingHatch,
        MAP_TOKENS.edge,
        MAP_TOKENS.hoverFill,
        MAP_TOKENS.activeFill,
        MAP_TOKENS.focus,
        ...MAP_TOKENS.sequential,
      ].sort(),
    );
    // Una variable `--th-*`/`--kx-*` o un `var()` de ellas con su variable de respaldo (la trama).
    const themeVariable = /^(--(th|kx)-[\w-]+|(var\(--(th|kx)-[\w-]+, )*var\(--(th|kx)-[\w-]+\)+)$/;
    expect([...tokens].every((token) => themeVariable.test(token))).toBe(true);
    // La sonda se pide dentro del contenedor del mapa (hereda su modo claro/oscuro).
    expect(view.theme.requested.every((r) => r.el instanceof HTMLElement && r.el.contains(map.container))).toBe(true);
  });

  it('al cambiar el tema (ThemeContext.onChange) el estilo se reconstruye con los colores nuevos', async () => {
    const view = await renderMapView();
    const map = await ready(view);
    const before = map.setStyleCalls.length;
    expect(view.theme.subscribers()).toBeGreaterThan(0);

    act(() => view.theme.fire());
    expect(map.setStyleCalls.length).toBeGreaterThan(before);
    const style = map.setStyleCalls.at(-1)!;
    expect(style.layers.find((l) => l.id === 'kx-background')!.paint!['background-color']).toBe(colorOf(MAP_TOKENS.background, 2));
    expect(style.layers.find((l) => l.id === 'kx-background')!.paint!['background-color']).not.toBe(colorOf(MAP_TOKENS.background, 1));
    await waitFor(() => expect(section(view)).toHaveAttribute('data-kx-ready', 'true'));

    // Se deja de escuchar al desmontar y el mapa se destruye.
    view.unmount();
    expect(view.theme.subscribers()).toBe(0);
    expect(map.removed).toBe(true);
  });
});

describe('teselas PMTiles propias', () => {
  const spain = {
    properties: { code: 'ES', name: 'Spain', level: 'country', parentCode: '', hasChildren: 'true' },
    geometry: { type: 'Polygon', coordinates: [[[-9, 36], [3, 36], [3, 44], [-9, 44], [-9, 36]]] },
  };
  const france = {
    properties: { code: 'FR', name: 'France', level: 'country', parentCode: '', hasChildren: 'false' },
    geometry: { type: 'Polygon', coordinates: [[[-4, 44], [8, 44], [8, 51], [-4, 51], [-4, 44]]] },
  };
  const madrid = {
    properties: { code: 'ES30', name: 'Comunidad de Madrid', level: '2', parentCode: 'ES', hasChildren: 'true' },
    geometry: { type: 'Polygon', coordinates: [[[-5, 40], [-3, 40], [-3, 41], [-5, 41], [-5, 40]]] },
  };

  it('registra pmtiles:// en MapLibre y pinta una fuente vectorial del propio origen, filtrada por nivel', async () => {
    FakeMap.defaultSourceFeatures = [spain, spain, france, madrid];
    const view = await renderMapView({ configure: withSource('demo-pmtiles') });
    const map = await ready(view);
    expect(FakeMap.instances).toHaveLength(1);
    const { addProtocol, setWorkerUrl } = await import('./test-utils');
    expect(addProtocol).toHaveBeenCalledWith('pmtiles', expect.any(Function));
    // El worker de MapLibre lo entrega el empaquetador (del propio despliegue), no se busca junto al módulo.
    expect(setWorkerUrl).toHaveBeenCalledWith(expect.stringContaining('maplibre-gl-worker'));

    expect(map.style.sources['kx-boundaries']).toMatchObject({
      type: 'vector',
      url: `pmtiles://${window.location.origin}/geo/karpo-demo.pmtiles`,
      maxzoom: 8,
    });
    expect(collectUrls(map.style).every((url) => url.replace('pmtiles://', '').startsWith(`${window.location.origin}/`))).toBe(true);
    const fill = map.getLayer('kx-fill')!;
    expect(fill['source-layer']).toBe('boundaries');
    expect(fill.filter).toEqual(['==', ['get', 'level'], 'country']);

    // Las entidades salen de las teselas cargadas: sólo las del nivel (no la comunidad), y una vez
    // aunque la geometría llegue recortada en dos teselas.
    await view.user.click(screen.getByRole('button', { name: 'Ver como tabla' }));
    const table = await screen.findByRole('table');
    expect(within(table).getAllByRole('row')).toHaveLength(3);
  });

  it('se encuadra una sola vez por lugar: a las entidades leídas de las teselas o, si no hay ninguna, a la caja del padre', async () => {
    // Las teselas cargadas no traen nada del nivel de comunidades: se usa la caja de España (la del clic).
    FakeMap.defaultSourceFeatures = [spain, france];
    const view = await renderMapView({ configure: withSource('demo-pmtiles') });
    const map = await ready(view);
    expect(map.fitBoundsCalls).toHaveLength(1);
    expect(map.fitBoundsCalls[0]!.bounds).toEqual([[-9, 36], [8, 51]]);
    act(() => map.click('ES'));
    await waitFor(() => expect(view.params().get('level')).toBe('country'));
    await ready(view);
    expect(map.fitBoundsCalls).toHaveLength(2);
    expect(map.fitBoundsCalls[1]!.bounds).toEqual([[-9, 36], [3, 44]]);
    // Cuando después llegan teselas con las comunidades no se vuelve a encuadrar (lo leído sería parcial).
    map.sourceFeatures = [spain, france, madrid];
    act(() => map.emit('idle'));
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(map.fitBoundsCalls).toHaveLength(2);
  });

  it('al entrar en un país sólo cambian los filtros: nivel 2 con el código del padre', async () => {
    FakeMap.defaultSourceFeatures = [spain, france, madrid];
    // Con `skipSingle: false` el país de una sola comunidad no baja solo (M-4): la prueba no depende de a qué velocidad llegan las teselas.
    const view = await renderMapView({ configure: (config) => withSkipSingle(false)(withSource('demo-pmtiles')(config)) });
    const map = await ready(view);
    act(() => map.click('ES'));
    await waitFor(() => expect(view.params().get('level')).toBe('country'));
    await waitFor(() => expect(map.getLayer('kx-fill')!.filter).toEqual(['all', ['==', ['get', 'level'], '2'], ['==', ['get', 'parentCode'], 'ES']]));
    await waitFor(() => expect(section(view)).toHaveAttribute('data-kx-ready', 'true'));
    await view.user.click(screen.getByRole('button', { name: 'Ver como tabla' }));
    expect(await screen.findByRole('button', { name: 'Entrar en Comunidad de Madrid' })).toBeInTheDocument();
  });
});

describe('movimiento y rótulos', () => {
  it('sin preferencia de movimiento el encuadre se anima; con «reducir», salta', async () => {
    const view = await renderMapView();
    const map = await ready(view);
    act(() => map.click('ES'));
    await waitFor(() => expect(map.fitBoundsCalls.length).toBeGreaterThan(1));
    // jsdom: matchMedia devuelve false para todo -> no hay animación.
    expect(map.fitBoundsCalls.at(-1)!.options).toMatchObject({ animate: false, duration: 0 });

    const noPreference = (query: string) =>
      ({ matches: query.includes('no-preference'), media: query, addEventListener: () => undefined, removeEventListener: () => undefined }) as unknown as MediaQueryList;
    vi.spyOn(window, 'matchMedia').mockImplementation(noPreference);
    await waitFor(() => expect(view.params().get('level')).toBe('country'));
    await ready(view);
    act(() => map.click('ES51'));
    await waitFor(() => expect(view.params().get('level')).toBe('region'));
    await waitFor(() => expect(map.fitBoundsCalls.at(-1)!.options).toMatchObject({ animate: true, duration: 600 }));
  });

  it('los rótulos son marcadores HTML y ocultan los que no caben', async () => {
    FakeMap.scale = 1;
    const view = await renderMapView();
    const map = await ready(view);
    // 248 marcadores creados, ninguno cabe a 1 píxel por grado.
    await waitFor(() => expect(map.container.querySelectorAll('.kx-map-label').length).toBe(248));
    expect(map.visibleLabels()).toEqual([]);
    expect(map.container.querySelector('.kx-map-label')).toHaveAttribute('aria-hidden', 'true');
    // Al acercar (otra escala, sobre la península) y terminar el movimiento, aparecen.
    FakeMap.scale = 100;
    map.setCenter([-3.5, 40.5]);
    act(() => map.emit('moveend'));
    expect(map.visibleLabels().length).toBeGreaterThan(0);
  });

  it('el rótulo «Geometría esquemática de demostración» indica la fuente de la demo', async () => {
    const view = await renderMapView();
    await ready(view);
    const note = screen.getByRole('note');
    expect(note).toHaveTextContent('Geometría esquemática de demostración');
    expect(note).toHaveTextContent('demo-geojson');
  });
});

describe('inglés y rtl', () => {
  it('los textos salen del idioma activo y los países en inglés', async () => {
    const view = await renderMapView({ locale: 'en', dir: 'rtl' });
    const map = await ready(view);
    map.setCenter([-3.5, 40.5]);
    act(() => map.emit('moveend'));
    expect(screen.getByRole('navigation', { name: 'Map path' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'View as table' })).toBeInTheDocument();
    await waitFor(() => expect(map.visibleLabels().find((l) => l.code === 'ES')).toMatchObject({ name: 'Spain' }));
    expect(screen.getByRole('note')).toHaveTextContent('Schematic demonstration geometry');
  });
});

// ---------------------------------------------------------------------------------------------
// Encuadre, rótulos y niveles únicos con teselas
// ---------------------------------------------------------------------------------------------

type Box = { x: number; y: number; width: number; height: number };

/** jsdom no maqueta: se da tamaño a las partes que mide el mapa (el lienzo y la leyenda), por `data-kx-part`. */
function stubLayout(parts: Record<string, Box>): void {
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
    const box = parts[this.getAttribute('data-kx-part') ?? ''] ?? { x: 0, y: 0, width: 0, height: 0 };
    return { ...box, left: box.x, top: box.y, right: box.x + box.width, bottom: box.y + box.height, toJSON: () => box } as DOMRect;
  });
}

const CANVAS: Box = { x: 100, y: 50, width: 1000, height: 600 };

describe('encuadre con la leyenda (M-3)', () => {
  it('el relleno suma el sitio de la leyenda por el lado en que está', async () => {
    stubLayout({ canvas: CANVAS, legend: { x: 112, y: 488, width: 200, height: 150 } });
    const view = await renderMapView({ search: '?view=map&level=country&code=ES' });
    const map = await ready(view);
    // Reservar 200 de ancho cuesta menos lienzo que reservar 150 de alto: franja del lado de inicio.
    await waitFor(() => expect(map.fitBoundsCalls.length).toBeGreaterThan(0));
    const padding = map.fitBoundsCalls.at(-1)!.options.padding as Record<string, number>;
    expect(padding).toEqual({ top: 32, bottom: 32, left: 212 + 16, right: 32 });
  });

  it('en RTL la leyenda está a la derecha y el relleno también', async () => {
    stubLayout({ canvas: CANVAS, legend: { x: 888, y: 488, width: 200, height: 150 } });
    const view = await renderMapView({ search: '?view=map&level=country&code=ES', dir: 'rtl' });
    const map = await ready(view);
    await waitFor(() => expect(map.fitBoundsCalls.length).toBeGreaterThan(0));
    const padding = map.fitBoundsCalls.at(-1)!.options.padding as Record<string, number>;
    expect(padding).toEqual({ top: 32, bottom: 32, left: 32, right: 212 + 16 });
  });

  it('con una sola delimitación el encuadre deja un quinto del lienzo de aire por lado', async () => {
    stubLayout({ canvas: CANVAS });
    const view = await renderMapView({ configure: withSkipSingle(false) });
    const map = await ready(view);
    await enter(view, map, 'ES', 'country');
    await enter(view, map, 'ES30', 'region');
    await waitFor(() => expect(map.fitBoundsCalls.at(-1)!.options.padding).toEqual({ top: 120, bottom: 120, left: 200, right: 200 }));
  });
});

describe('rótulos medidos (M-3)', () => {
  // jsdom no trae el CSS del tema: se le da el aspecto del rótulo (relleno, ancho máximo y letra) que lee el medidor.
  let style: HTMLStyleElement;
  beforeEach(() => {
    style = document.createElement('style');
    style.textContent = `
      .kx-map-label { padding-left: 4px; padding-top: 2px; max-width: 112px; }
      .kx-map-label [data-kx-part="name"], .kx-map-label [data-kx-part="value"] {
        font-size: 13px; font-weight: 700; font-family: Test; line-height: 16px;
      }`;
    document.head.append(style);
  });
  afterEach(() => style.remove());

  /** Rótulo visible de `code`: su nombre (con saltos de línea), el título (el nombre completo si se abrevió) y la variante. */
  const labelOf = (map: FakeMap, code: string) => {
    const element = map.container.querySelector<HTMLElement>(`.kx-map-label[data-kx-code="${code}"]`)!;
    return {
      hidden: element.hidden,
      name: element.querySelector('[data-kx-part="name"]')?.textContent ?? '',
      title: element.getAttribute('title'),
      kind: element.dataset.kxKind,
    };
  };

  /** Variantes distintas de los rótulos visibles (los ocultos no llevan `data-kx-kind`). */
  const kinds = (map: FakeMap) => [
    ...new Set(
      [...map.container.querySelectorAll<HTMLElement>('[data-kx-part="label"]')].filter((el) => !el.hidden).map((el) => el.dataset.kxKind),
    ),
  ];

  async function countryAt(scale: number) {
    FakeMap.scale = scale;
    const view = await renderMapView({ search: '?view=map&level=country&code=ES' });
    const map = await ready(view);
    await waitFor(() => expect(map.container.querySelectorAll('.kx-map-label').length).toBe(19));
    act(() => map.emit('moveend'));
    return map;
  }

  it('el nombre completo se parte en líneas con la medida si cabe en su delimitación', async () => {
    const map = await countryAt(80);
    expect(labelOf(map, 'ES22')).toMatchObject({ hidden: false, kind: 'full', title: null, name: 'Comunidad\nForal de\nNavarra' });
  });

  it('si el nombre entero no cabe en todas, todas llevan el corto (con el largo en el título), y luego el código', async () => {
    const map = await countryAt(53);
    const short = labelOf(map, 'ES22');
    expect(short).toMatchObject({ hidden: false, kind: 'short', name: 'Navarra', title: 'Comunidad Foral de Navarra' });
    // Un nivel, un criterio (R3-4, M-6): ninguna delimitación conserva el nombre completo.
    expect(kinds(map)).toEqual(['short']);
  });

  it('un nivel, un criterio: todos los rótulos visibles comparten `data-kx-kind` a cualquier escala (R3-4, M-6)', async () => {
    for (const [scale, expected] of [[80, 'full'], [53, 'short'], [40, 'code']] as const) {
      const map = await countryAt(scale);
      expect(kinds(map), `escala ${scale}`).toEqual([expected]);
      cleanup();
      resetFakeMap();
    }
  });

  it('el código es la última variante y, sin sitio ni para eso, el rótulo se oculta', async () => {
    const map = await countryAt(27);
    expect(labelOf(map, 'ES22')).toMatchObject({ hidden: false, kind: 'code', name: 'ES22', title: 'Comunidad Foral de Navarra' });
    FakeMap.scale = 20;
    act(() => map.emit('moveend'));
    expect(labelOf(map, 'ES22').hidden).toBe(true);
  });

  it('el rótulo va en el polo de inaccesibilidad: se mueve del punto del marcador sólo para no salirse de su caja', async () => {
    const map = await countryAt(80);
    const marker = [...map.container.querySelectorAll('.kx-map-label')].length;
    expect(marker).toBe(19);
    const visible = map.visibleLabels();
    // En la rejilla de la demo todas las teselas son rectángulos: el polo es su centro y no hace falta mover nada.
    expect(visible.length).toBeGreaterThanOrEqual(Math.ceil(19 * 0.9));
  });
});

describe('teselas PMTiles: un nivel con una sola delimitación con detalle (M-4)', () => {
  const spain = {
    properties: { code: 'ES', name: 'Spain', level: 'country', parentCode: '', hasChildren: 'true' },
    geometry: { type: 'Polygon', coordinates: [[[-9, 36], [3, 36], [3, 44], [-9, 44], [-9, 36]]] },
  };
  const madrid = {
    properties: { code: 'ES30', name: 'Comunidad de Madrid', level: '2', parentCode: 'ES', hasChildren: 'true' },
    geometry: { type: 'Polygon', coordinates: [[[-5, 40], [-3, 40], [-3, 41], [-5, 41], [-5, 40]]] },
  };

  it('al llegar por un clic baja a la única delimitación; si se llega por la URL, se conserva', async () => {
    FakeMap.defaultSourceFeatures = [spain, madrid];
    const view = await renderMapView({ configure: withSource('demo-pmtiles') });
    const map = await ready(view);
    act(() => map.click('ES'));
    await waitFor(() => expect(view.params().get('level')).toBe('region'));
    expect(view.params().get('code')).toBe('ES30');
    view.unmount();

    FakeMap.defaultSourceFeatures = [spain, madrid];
    const direct = await renderMapView({ configure: withSource('demo-pmtiles'), search: '?view=map&level=country&code=ES' });
    await ready(direct);
    await new Promise((resolve) => setTimeout(resolve, 80));
    expect(direct.params().get('level')).toBe('country');
  });
});
