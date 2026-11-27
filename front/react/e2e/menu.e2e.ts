// Iconos del menú (B-1): el manifiesto de la demo usa nombres de Phosphor (`buildings`, `tree-structure`,
// `globe-hemisphere-west`, `map-trifold`, `calendar-dots`) y todas las entradas pintan su icono, no el
// círculo de reserva (`data-kx-icon="missing"`). Las entradas citadas por el jurado son Organización,
// Instalaciones, Puestos, Geografía, Países, Delimitaciones y Festivos.
import type { Locator, Page } from 'playwright-core';
import { expect, it } from 'vitest';
import { count, poll, visible } from './lib/esperar';
import { e2eSuite } from './lib/suite';

/** Entrada del menú: nombre visible en cada idioma y el icono que declara el manifiesto de la demo. */
const ENTRIES = [
  { es: 'Organización', en: 'Organization', icon: 'buildings', group: true },
  { es: 'Instalaciones', en: 'Facilities', icon: 'buildings', group: false },
  { es: 'Puestos y organigrama', en: 'Positions and org chart', icon: 'tree-structure', group: false },
  { es: 'Geografía', en: 'Geography', icon: 'globe-hemisphere-west', group: true },
  { es: 'Países', en: 'Countries', icon: 'globe-hemisphere-west', group: false },
  { es: 'Delimitaciones', en: 'Boundaries', icon: 'map-trifold', group: false },
  { es: 'Festivos', en: 'Holidays', icon: 'calendar-dots', group: false },
] as const;

const nav = (page: Page) => page.locator('nav[data-th="nav"]');

/** Elemento del menú (enlace de entidad o botón de grupo) por su nombre visible. */
function entry(page: Page, label: string, group: boolean): Locator {
  return group
    ? nav(page).locator('button[data-kx-part="group-toggle"]', { has: page.getByText(label, { exact: true }) })
    : nav(page).locator('a[data-th="nav-item"]', { has: page.getByText(label, { exact: true }) });
}

/** Icono principal de una entrada (el primero: el segundo del grupo es el chevron). */
const iconOf = (item: Locator) => item.locator('svg[data-th-icon]').first();

/** Iconos desconocidos pintados en toda la página, con el nombre pedido. */
const missingIcons = (page: Page) =>
  page.evaluate(() =>
    [...document.querySelectorAll('[data-kx-icon="missing"]')].map((el) => el.getAttribute('data-th-icon') ?? ''),
  );

e2eSuite('menú: iconos', ({ open }) => {
  it('las siete entradas citadas pintan su icono y ninguna el círculo de reserva', async () => {
    const { page } = await open({ path: '/product' });
    await visible(nav(page)).toBe(true);
    for (const item of ENTRIES) {
      const link = entry(page, item.es, item.group);
      await count(link).toBe(1);
      const icon = iconOf(link);
      await visible(icon).toBe(true);
      expect(await icon.getAttribute('data-th-icon'), item.es).toBe(item.icon);
      expect(await icon.getAttribute('data-kx-icon'), `${item.es}: icono desconocido`).toBeNull();
      // Dibujo real: trazados, no el círculo de reserva.
      expect(await icon.locator('path').count(), `${item.es}: sin trazados`).toBeGreaterThan(0);
      expect(await icon.locator('circle').count(), `${item.es}: círculo de reserva`).toBe(0);
      const box = await icon.boundingBox();
      expect(box?.width ?? 0, `${item.es}: icono sin tamaño`).toBeGreaterThan(8);
    }
  });

  it('ningún icono del menú entero (ni de la barra superior) es desconocido', async () => {
    const { page } = await open({ path: '/product' });
    await visible(nav(page)).toBe(true);
    await poll(() => nav(page).locator('svg[data-th-icon]').count()).toBeGreaterThan(10);
    expect(await nav(page).locator('[data-kx-icon="missing"]').count()).toBe(0);
    expect(await missingIcons(page)).toEqual([]);
  });

  it('los cinco iconos de las entradas son dibujos distintos (no el mismo círculo de reserva)', async () => {
    const { page } = await open({ path: '/product' });
    const shapes = new Set<string>();
    for (const item of ENTRIES.filter((e) => !e.group)) {
      const icon = iconOf(entry(page, item.es, false));
      shapes.add((await icon.locator('path').first().getAttribute('d')) ?? '');
    }
    // buildings, tree-structure, globe-hemisphere-west, map-trifold y calendar-dots.
    expect(shapes.size).toBe(5);
  });

  it('en inglés el menú conserva los mismos iconos', async () => {
    const { page } = await open({ lng: 'en', path: '/product' });
    await visible(nav(page)).toBe(true);
    for (const item of ENTRIES) {
      const icon = iconOf(entry(page, item.en, item.group));
      await visible(icon).toBe(true);
      expect(await icon.getAttribute('data-th-icon'), item.en).toBe(item.icon);
      expect(await icon.getAttribute('data-kx-icon'), item.en).toBeNull();
    }
    expect(await missingIcons(page)).toEqual([]);
  });

  it('el buscador del menú también pinta iconos reales en sus resultados', async () => {
    const { page } = await open({ path: '/product' });
    await nav(page).getByRole('searchbox').fill('fest');
    const results = nav(page).locator('[data-kx-part="nav-results"] a[data-th="nav-item"]');
    await count(results).toBeGreaterThan(0);
    expect(await results.first().locator('[data-kx-icon="missing"]').count()).toBe(0);
    expect(await results.first().locator('svg[data-th-icon="calendar-dots"] path').count()).toBeGreaterThan(0);
  });

  it('ninguna pantalla principal pinta un icono desconocido', async () => {
    const { page, go } = await open({ path: '/product' });
    const screens: [string, string][] = [
      ['/product', '[data-kx-ready="true"]'],
      ['/facility', '[data-kx-ready="true"]'],
      ['/position?view=tree&layout=list', 'section[data-kx-view="tree"][data-kx-ready="true"]'],
      ['/position?view=tree&layout=diagram', 'section[data-kx-view="tree"][data-kx-ready="true"]'],
      ['/facility?view=map&level=world', 'section[data-kx-view="map"][data-kx-ready="true"]'],
      ['/country', '[data-kx-ready="true"]'],
      ['/boundary', '[data-kx-ready="true"]'],
      ['/holiday', '[data-kx-ready="true"]'],
    ];
    for (const [path, ready] of screens) {
      await go(path, ready);
      expect(await missingIcons(page), path).toEqual([]);
    }
  });
});
