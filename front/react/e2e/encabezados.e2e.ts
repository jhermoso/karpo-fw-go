// Un solo h1 por pantalla (M-1, D-R32) en un navegador de verdad: las 8 entidades de Go, las 4 de la demo, el inicio, el
// 403, el 404, la ficha como página y el marco compacto a 360 px. La barra superior no lleva ningún encabezado y su
// miga enseña la sección del menú. Se cuenta lo que el árbol de accesibilidad ofrece (un `h1` recortado a la vista,
// como el de la cabecera en un teléfono, cuenta) y los `h1` del documento.
import type { Page } from 'playwright-core';
import { expect, it } from 'vitest';
import { attr, count, poll, text, visible } from './lib/esperar';
import { e2eSuite, mobileOptions } from './lib/suite';

const READY = '[data-kx-ready="true"]';
const TREE_READY = 'section[data-kx-view="tree"][data-kx-ready="true"]';

/** Las ocho entidades de la configuración de Go y las cuatro propias de la demo. */
const GO_ENTITIES = ['facility', 'work-center', 'product', 'supplier', 'carrier', 'asset', 'country', 'holiday'] as const;
const DEMO_ENTITIES = ['boundary', 'facility-type', 'party', 'position'] as const;

interface Facts {
  documentH1: number;
  accessibleH1: number;
  topbarHeadings: number;
  crumbs: string;
  crumbTag: string;
  crumbHidden: boolean;
}

/** Lo que importa del encabezado de la pantalla actual. */
const facts = (page: Page): Promise<Facts> =>
  page.evaluate(() => {
    const topbar = document.querySelector('[data-kx-part="topbar"]');
    const crumbs = topbar?.querySelector<HTMLElement>('[data-kx-part="crumbs"]') ?? null;
    return {
      documentH1: document.querySelectorAll('h1').length,
      // `h1` sin `aria-hidden` ni `hidden` ni `display: none` (el recorte visual no lo oculta al lector).
      accessibleH1: [...document.querySelectorAll('h1')].filter((h) => {
        for (let el: Element | null = h; el; el = el.parentElement) {
          if (el.hasAttribute('hidden') || el.getAttribute('aria-hidden') === 'true') return false;
          const style = getComputedStyle(el);
          if (style.display === 'none' || style.visibility === 'hidden') return false;
        }
        return true;
      }).length,
      topbarHeadings: topbar?.querySelectorAll('h1, h2, h3, h4, h5, h6, [role="heading"]').length ?? -1,
      crumbs: crumbs?.textContent?.replace(/\s+/g, ' ').trim() ?? '',
      crumbTag: crumbs?.localName ?? '',
      crumbHidden: crumbs?.getAttribute('aria-hidden') === 'true',
    };
  });

/** Espera a que haya exactamente un `h1` (una pantalla que sale puede convivir un instante con la que entra). */
async function singleHeading(page: Page): Promise<Facts> {
  await poll(async () => {
    const found = await facts(page);
    return [found.documentH1, found.accessibleH1];
  }).toEqual([1, 1]);
  return facts(page);
}

e2eSuite('encabezados', ({ open }) => {
  it('las 8 entidades de Go y las 4 de la demo tienen un único h1 y la barra superior lo deja a la cabecera', async () => {
    const session = await open({ path: '/facility', ready: READY });
    const { page, go } = session;
    for (const entity of [...GO_ENTITIES, ...DEMO_ENTITIES]) {
      // `position` abre su vista por omisión; el resto, la tabla.
      await go(`/${entity}`, entity === 'position' ? false : READY);
      const found = await singleHeading(page);
      expect(found.topbarHeadings, `${entity}: la barra no lleva encabezados`).toBe(0);
      await text(page.locator('[data-kx-part="entity-title"]')).not.toBe('');
      // La miga es el nombre de la entidad precedido por su sección del menú (si el menú la agrupa).
      await visible(page.getByRole('navigation', { name: 'Ruta' })).toBe(true);
      const heading = (await page.locator('h1').first().textContent())?.trim() ?? '';
      expect(found.crumbs, `${entity}: la miga acaba en el título`).toContain(heading);
    }
    expect(session.pageErrors).toEqual([]);
  });

  it('la miga de una entidad del menú empieza por su sección y la última miga es la pantalla actual', async () => {
    const { page } = await open({ path: '/facility', ready: READY });
    const items = page.getByRole('navigation', { name: 'Ruta' }).getByRole('listitem');
    await count(items).toBe(2);
    expect(((await items.nth(0).textContent()) ?? '').trim().toLowerCase()).toBe('organización');
    expect(((await items.nth(1).textContent()) ?? '').trim()).toBe('Instalaciones');
    await attr(items.nth(1), 'aria-current').toBe('page');
    // El separador es un icono, no un carácter: se espeja en RTL (data-kx-mirror) y no se anuncia.
    await attr(items.nth(1).locator('svg[data-th-icon]'), 'data-kx-mirror').toBe('true');
    await attr(items.nth(1).locator('svg[data-th-icon]'), 'aria-hidden').toBe('true');
  });

  it('el inicio, el 403 y el 404 tienen un único h1 propio y la barra enseña el nombre de la aplicación', async () => {
    const { page, go } = await open({ path: '/', ready: false });
    for (const [path, heading] of [
      ['/', 'Inicio'],
      ['/403', 'No tienes acceso'],
      ['/no-existe', 'No se encuentra la página'],
    ] as const) {
      await go(path, false);
      await visible(page.getByRole('heading', { level: 1, name: heading })).toBe(true);
      const found = await singleHeading(page);
      expect(found.topbarHeadings, path).toBe(0);
      expect(found.crumbTag, `${path}: sin entidad la miga es un párrafo`).toBe('p');
      expect(found.crumbs).toBe('Karpo');
    }
  });

  it('el detalle como página tiene su h1 (no hay cabecera de entidad) y en el panel la ficha es h2 bajo el h1 de la entidad', async () => {
    const { page, go } = await open({ path: '/facility', ready: READY });
    await page.locator('[role="row"][data-th="record"]', { hasText: 'Sala Atlas (Sede Central)' }).locator('[data-kx-col="name"]').dblclick();
    await page.waitForURL(/\/facility\/[\w-]+\?mode=edit/);
    const id = new URL(page.url()).pathname.split('/')[2] ?? '';
    const sheet = page.locator('section[data-th="sheet"]');
    // En el panel: el h1 es la cabecera de la entidad y el de la ficha, un h2.
    await visible(sheet.getByRole('heading', { level: 2, name: 'Sala Atlas (Sede Central)' })).toBe(true);
    expect((await singleHeading(page)).documentH1).toBe(1);
    await count(page.locator('[data-kx-part="entity-header"] h1')).toBe(1);
    await count(page.locator('section[data-th="sheet"] h1')).toBe(0);

    // Como página: la ficha pasa a h1 con el mismo id (aria-labelledby) y la cabecera de la entidad no se pinta.
    await go(`/facility/${id}?page=1`, 'section[data-th="sheet"][data-kx-mode]');
    const title = sheet.getByRole('heading', { level: 1, name: 'Sala Atlas (Sede Central)' });
    await visible(title).toBe(true);
    const found = await singleHeading(page);
    expect(found.topbarHeadings).toBe(0);
    await count(page.locator('[data-kx-part="entity-header"]')).toBe(0);
    expect(await sheet.getAttribute('aria-labelledby')).toBe(await title.getAttribute('id'));
    await count(sheet.getByRole('heading', { level: 2, name: 'Sala Atlas (Sede Central)' })).toBe(0);
  });

  it('las vistas de árbol y de mapa conservan el único h1 de la cabecera de la entidad', async () => {
    const { page, go } = await open({ path: '/position?view=tree&layout=list', ready: TREE_READY });
    expect((await singleHeading(page)).topbarHeadings).toBe(0);
    await go('/position?view=tree&layout=diagram', TREE_READY);
    expect((await singleHeading(page)).topbarHeadings).toBe(0);
    await go('/facility?view=map&level=world', 'section[data-kx-view="map"][data-kx-ready="true"]');
    expect((await singleHeading(page)).topbarHeadings).toBe(0);
  });

  it('a 360 px (marco compacto) sigue habiendo un h1 accesible, la miga es sólo la entidad y se oculta al lector', async () => {
    const { page, go } = await open({ ...mobileOptions({ width: 360, height: 780 }), path: '/facility', ready: READY });
    const list = await singleHeading(page);
    expect(list.topbarHeadings).toBe(0);
    expect(list.crumbHidden).toBe(true);
    expect(list.crumbs).toBe('Instalaciones');
    // El h1 de la cabecera sigue en el árbol de accesibilidad, recortado a la vista; la miga es lo que se ve.
    const box = await page.locator('[data-kx-part="entity-title"]').evaluate((el) => {
      const rect = el.getBoundingClientRect();
      return { width: rect.width, height: rect.height };
    });
    expect(box.width).toBeLessThanOrEqual(1);
    expect(box.height).toBeLessThanOrEqual(1);
    const crumb = await page.locator('[data-kx-part="crumbs"]').boundingBox();
    expect(crumb?.width ?? 0).toBeGreaterThan(40);

    await go('/', false);
    await visible(page.getByRole('heading', { level: 1, name: 'Inicio' })).toBe(true);
    expect((await singleHeading(page)).crumbTag).toBe('p');
  });

  it('a 360 px la ficha es la página y su h1 es el único', async () => {
    const { page } = await open({ ...mobileOptions({ width: 360, height: 780 }), path: '/facility', ready: READY });
    await page.locator('[role="row"][data-th="record"]', { hasText: 'Recepción (Sede Central)' }).locator('[data-kx-col="name"]').dblclick();
    await page.waitForURL(/\/facility\/[\w-]+/);
    await visible(page.locator('section[data-th="sheet"] h1')).toBe(true);
    const sheetFacts = await singleHeading(page);
    expect(sheetFacts.topbarHeadings).toBe(0);
    await count(page.locator('[data-kx-part="entity-header"]')).toBe(0);
  });
});
