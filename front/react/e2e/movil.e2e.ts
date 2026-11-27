// Teléfono (B-5): a 360 y 390 px, con pantalla táctil y agente de móvil, ninguna pantalla desborda en horizontal, la miga
// de la barra superior (el título que se ve; el `h1` único va en el contenido, M-1) se lee, no hay teclas de atajo a la vista y el menú «Más» (tema, modo, idioma, atajos, clave y
// salida) se abre y se cierra con el teclado. Rutas: inicio, una lista, una lista con control de vista, el organigrama
// y el mapa.
import { expect, it } from 'vitest';
import { attr, count, focused, poll, visible } from './lib/esperar';
import { e2eSuite, mobileOptions } from './lib/suite';

const SIZES = [
  { width: 360, height: 780 },
  { width: 390, height: 844 },
] as const;

const ROUTES = [
  { path: '/', title: 'Karpo', ready: false as const },
  { path: '/product', title: 'Productos' },
  { path: '/facility', title: 'Instalaciones' },
  { path: '/position?view=tree', title: 'Puestos' },
  { path: '/facility?view=map', title: 'Instalaciones' },
] as const;

e2eSuite('móvil', ({ open }) => {
  for (const size of SIZES) {
    for (const route of ROUTES) {
      it(`${size.width} px · ${route.path}: nada desborda, la miga se ve, hay un solo h1, no hay teclas y «Más» se maneja con el teclado`, async () => {
        const session = await open({
          ...mobileOptions(size),
          path: route.path,
          ...('ready' in route ? { ready: route.ready } : {}),
        });
        const { page } = session;
        // La pantalla está pintada: en el inicio, su título; en las demás, el indicador de la vista.
        if (route.path === '/') await visible(page.getByRole('heading', { level: 1, name: 'Inicio' })).toBe(true);

        const facts = await page.evaluate(() => {
          // El título que se ve en un teléfono es la miga de la barra; el `h1` del contenido sólo lo oye el lector.
          const heading = document.querySelector<HTMLElement>('[data-kx-part="topbar"] [data-kx-part="crumbs"]');
          const box = heading?.getBoundingClientRect();
          const keys = [...document.querySelectorAll<HTMLElement>('kbd')].filter(
            (key) => key.getClientRects().length > 0 && getComputedStyle(key).visibility !== 'hidden',
          );
          return {
            scrollWidth: document.documentElement.scrollWidth,
            innerWidth: window.innerWidth,
            headingText: heading?.textContent?.trim() ?? '',
            headingWidth: box?.width ?? 0,
            headings: [...document.querySelectorAll('h1')].map((h) => h.textContent?.trim() ?? ''),
            topbarHeadings: document.querySelectorAll('[data-kx-part="topbar"] :is(h1, h2, h3, h4, h5, h6, [role="heading"])').length,
            crumbHidden: heading?.getAttribute('aria-hidden') === 'true',
            keys: keys.map((key) => key.textContent),
            compact: document.querySelector('[data-kx-shell]')?.hasAttribute('data-kx-compact') ?? false,
          };
        });
        // Nada desborda: la página no se desplaza en horizontal.
        expect(facts.scrollWidth, `scrollWidth ${facts.scrollWidth} > innerWidth ${facts.innerWidth}`).toBeLessThanOrEqual(facts.innerWidth);
        // La miga no se comprime hasta desaparecer: mide algo y tiene texto; la barra no lleva ningún encabezado.
        expect(facts.headingText).not.toBe('');
        expect(facts.headingWidth).toBeGreaterThan(40);
        expect(facts.topbarHeadings).toBe(0);
        // Hay un único `h1`, con texto (en las listas va recortado a la vista: lo que se ve es la miga, que se oculta al lector).
        expect(facts.headings).toHaveLength(1);
        expect(facts.headings[0]).not.toBe('');
        if (route.path !== '/') expect(facts.crumbHidden).toBe(true);
        // Ninguna tecla de atajo a la vista (en un teléfono no hay teclado).
        expect(facts.keys).toEqual([]);
        expect(facts.compact).toBe(true);

        // La barra se reduce a menú, título, paleta, ayuda y «Más»: tema, modo e idioma viven dentro.
        const banner = page.locator('[data-kx-part="topbar"]');
        for (const gone of ['Tema', 'Modo de color', 'Idioma']) await count(banner.getByRole('button', { name: gone })).toBe(0);
        await visible(banner.getByRole('button', { name: 'Ayuda' })).toBe(true);
        await visible(banner.getByRole('button', { name: 'Buscar o ir a…' })).toBe(true);

        // «Más» se abre con el teclado, cabe en la pantalla y Esc lo cierra devolviendo el foco al botón.
        const more = banner.getByRole('button', { name: 'Más' });
        await visible(more).toBe(true);
        await more.focus();
        await focused(more).toBe(true);
        await page.keyboard.press('Enter');
        const menu = page.getByRole('menu');
        await visible(menu).toBe(true);
        const menuBox = await menu.evaluate((el) => {
          const box = el.getBoundingClientRect();
          return { left: box.left, right: box.right, top: box.top, bottom: box.bottom, width: window.innerWidth, height: window.innerHeight };
        });
        expect(menuBox.left).toBeGreaterThanOrEqual(0);
        expect(menuBox.right).toBeLessThanOrEqual(menuBox.width);
        expect(menuBox.bottom).toBeLessThanOrEqual(menuBox.height + 1);
        await visible(menu.getByRole('menuitem', { name: 'Cerrar sesión' })).toBe(true);
        await visible(menu.getByRole('menuitem', { name: 'Atajos de teclado' })).toBe(true);
        await visible(menu.getByRole('menuitem', { name: 'Cambiar la contraseña' })).toBe(true);
        await visible(menu.getByRole('menuitemradio', { name: 'English' })).toBe(true);
        // Con el menú abierto la página tampoco desborda.
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
        await page.keyboard.press('Escape');
        await count(page.getByRole('menu')).toBe(0);
        await focused(more).toBe(true);
        expect(session.pageErrors).toEqual([]);
      });
    }
  }

  it('en una lista con vistas la cabecera cabe: control de vista con iconos accesibles y «Nuevo» a la vista', async () => {
    const { page } = await open({ ...mobileOptions(), path: '/facility' });
    const group = page.getByRole('radiogroup', { name: 'Vista' });
    await visible(group).toBe(true);
    for (const name of ['Tabla', 'Árbol', 'Diagrama', 'Mapa']) {
      const radio = group.getByRole('radio', { name });
      await visible(radio).toBe(true);
      expect((await radio.boundingBox())?.width ?? 0).toBeGreaterThan(24);
    }
    await visible(page.getByRole('button', { name: 'Nuevo' })).toBe(true);
    const box = await group.boundingBox();
    expect((box?.x ?? 0) + (box?.width ?? 0)).toBeLessThanOrEqual(390);
    // El control se maneja con el teclado: la flecha cambia de vista y la URL lo recuerda.
    await group.getByRole('radio', { name: 'Tabla' }).focus();
    await page.keyboard.press('ArrowRight');
    await attr(group.getByRole('radio', { name: 'Árbol' }), 'aria-checked').toBe('true');
    await poll(() => page.evaluate(() => window.location.search)).toContain('view=tree');
  });

  it('con el menú lateral abierto como cajón la página tampoco desborda, y se cierra', async () => {
    const { page } = await open({ ...mobileOptions(), path: '/product' });
    await page.getByRole('button', { name: 'Abrir el menú' }).click();
    const nav = page.getByRole('navigation', { name: 'Menú principal' });
    await visible(nav).toBe(true);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await page.getByRole('button', { name: 'Cerrar el menú' }).click();
    await poll(() => nav.isVisible()).toBe(false);
  });
});
