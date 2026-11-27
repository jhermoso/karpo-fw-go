// Ayuda guiada (PLAN §4.8): el botón lanza un recorrido por los elementos de la pantalla con driver.js,
// con textos traducidos; avanza y retrocede con botones y teclado, y devuelve el foco al cerrarse.
import { readFileSync } from 'node:fs';
import path from 'node:path';
import type { Page } from 'playwright-core';
import { expect, it } from 'vitest';
import { readNamespaces } from './lib/demo.mjs';
import { attr, count, focused, poll, text, visible } from './lib/esperar';
import { e2eSuite } from './lib/suite';

const helpButton = (page: Page) => page.getByRole('button', { name: 'Ayuda', exact: true });
const popover = (page: Page) => page.locator('.driver-popover');
const progress = (page: Page) => popover(page).locator('.driver-popover-progress-text');
const title = (page: Page) => popover(page).locator('.driver-popover-title');

/** Recursos de JavaScript que la página ha cargado hasta ahora cuyo nombre contiene `driver`. */
const driverChunks = (page: Page) =>
  page.evaluate(() =>
    performance
      .getEntriesByType('resource')
      .map((entry) => entry.name)
      .filter((name) => /driver/i.test(name) && /\.js(\?|$)/.test(name)),
  );

/** Una clave sin traducir se vería como `espacio:clave` o `ayuda.nombre.title`. */
function untranslated(content: string): string[] {
  const namespaces = readNamespaces().join('|');
  const patterns = [new RegExp(`\\b(?:${namespaces}):[A-Za-z][\\w.-]*`, 'g'), /\bhelp\.[\w-]+\.(?:title|body)\b/g];
  return patterns.flatMap((pattern) => content.match(pattern) ?? []);
}

const SCREENS: { name: string; path: string; ready: string }[] = [
  { name: 'cuadrícula de productos', path: '/product', ready: '[data-kx-view="grid"][data-kx-ready="true"], [data-kx-ready="true"]' },
  { name: 'cuadrícula de instalaciones', path: '/facility', ready: '[data-kx-ready="true"]' },
  { name: 'proveedores', path: '/supplier', ready: '[data-kx-ready="true"]' },
  { name: 'países', path: '/country', ready: '[data-kx-ready="true"]' },
  { name: 'organigrama (listado)', path: '/position?view=tree&layout=list', ready: 'section[data-kx-view="tree"][data-kx-ready="true"]' },
  { name: 'organigrama (diagrama)', path: '/position?view=tree&layout=diagram', ready: 'section[data-kx-view="tree"][data-kx-ready="true"]' },
  { name: 'mapa', path: '/facility?view=map&level=world', ready: 'section[data-kx-view="map"][data-kx-ready="true"]' },
  { name: 'listado del mapa', path: '/facility?view=map&level=world&layout=list', ready: 'section[data-kx-view="map"][data-kx-ready="true"]' },
];

e2eSuite('ayuda', ({ open }) => {
  it('abre el recorrido, avanza y retrocede con botones y teclado, cierra con Esc y devuelve el foco', async () => {
    const { page } = await open({ path: '/product' });
    // Recorrido sin ver: el botón lleva una marca hasta que se completa.
    await attr(helpButton(page), 'data-kx-help-unseen').toBe('');
    expect(await helpButton(page).evaluate((el) => el.hasAttribute('data-kx-help-unseen'))).toBe(true);

    await helpButton(page).click();
    const dialog = popover(page);
    await visible(dialog).toBe(true);
    await attr(dialog, 'role').toBe('dialog');
    await text(progress(page)).toMatch(/^Paso 1 de \d+$/);
    const total = Number(/de (\d+)$/.exec(await progress(page).innerText())?.[1]);
    expect(total).toBeGreaterThanOrEqual(2);
    // Los pasos salen de la configuración de la pantalla o, si no la trae, de las anclas visibles: en los dos
    // casos el primero tiene título y texto traducidos y resalta un elemento real con su ancla.
    await text(title(page)).not.toBe('');
    const firstTitle = await title(page).innerText();
    await text(dialog.locator('.driver-popover-description')).not.toBe('');
    await attr(page.locator('.driver-active-element'), 'data-kx-help').not.toBe('');
    const firstAnchor = await page.locator('.driver-active-element').first().getAttribute('data-kx-help');

    await dialog.getByRole('button', { name: 'Siguiente' }).click();
    await text(progress(page)).toMatch(/^Paso 2 de \d+$/);
    await text(title(page)).not.toBe(firstTitle);
    await attr(page.locator('.driver-active-element'), 'data-kx-help').not.toBe(firstAnchor ?? '');

    // El segundo paso conserva el estado de la pantalla; Anterior vuelve atrás.
    await dialog.getByRole('button', { name: 'Anterior' }).click();
    await text(progress(page)).toMatch(/^Paso 1 de \d+$/);
    // Teclado: → avanza, ← retrocede (en LTR).
    await page.keyboard.press('ArrowRight');
    await text(progress(page)).toMatch(/^Paso 2 de \d+$/);
    await page.keyboard.press('ArrowLeft');
    await text(progress(page)).toMatch(/^Paso 1 de \d+$/);

    await page.keyboard.press('Escape');
    await count(popover(page).filter({ visible: true })).toBe(0);
    await focused(helpButton(page)).toBe(true);
    // Cerrado a medias: sigue sin verse del todo (salvo que el recorrido sólo tenga dos pasos: llegar al
    // segundo es llegar al último, y eso lo da por visto).
    expect(await helpButton(page).evaluate((el) => el.hasAttribute('data-kx-help-unseen'))).toBe(total > 2);
  });

  it('recorre todos los pasos, el último dice «Terminar», marca el recorrido como visto y devuelve el foco', async () => {
    const { page } = await open({ path: '/product' });
    await helpButton(page).click();
    await visible(popover(page)).toBe(true);
    const total = Number(/de (\d+)$/.exec(await progress(page).innerText())?.[1]);
    expect(total).toBeGreaterThanOrEqual(2);
    const seen = new Set<string>();
    for (let step = 1; step < total; step += 1) {
      await text(progress(page)).toBe(`Paso ${step} de ${total}`);
      seen.add(await title(page).innerText());
      await popover(page).locator('.driver-popover-next-btn').click();
    }
    await text(progress(page)).toBe(`Paso ${total} de ${total}`);
    await text(popover(page).locator('.driver-popover-next-btn')).toBe('Terminar');
    await popover(page).locator('.driver-popover-next-btn').click();

    await count(popover(page).filter({ visible: true })).toBe(0);
    expect(seen.size).toBe(total - 1);
    await focused(helpButton(page)).toBe(true);
    await attr(helpButton(page), 'data-kx-help-unseen').toBe('');
    expect(await helpButton(page).evaluate((el) => el.hasAttribute('data-kx-help-unseen'))).toBe(false);
  });

  it('en /product el recorrido junta la configuración y las anclas: al menos 7 pasos, con el de la barra de lote sin cambios', async () => {
    const { page } = await open({ path: '/product' });
    // Los textos los escribe la tarea de la rejilla (grid.json): `help.batch-bar.idle.title`.
    const grid = JSON.parse(readFileSync(path.resolve(__dirname, '../src/locales/es/grid.json'), 'utf8')) as {
      help?: Record<string, { idle?: { title?: string } }>;
    };
    const idleTitle = grid.help?.['batch-bar']?.idle?.title;
    expect(idleTitle, 'grid.json (es) debe traer help.batch-bar.idle.title').toBeTruthy();

    await helpButton(page).click();
    await visible(popover(page)).toBe(true);
    const total = Number(/de (\d+)$/.exec(await progress(page).innerText())?.[1]);
    expect(total).toBeGreaterThanOrEqual(7);
    const titles: string[] = [];
    for (let step = 1; step <= total; step += 1) {
      await text(progress(page)).toBe(`Paso ${step} de ${total}`);
      titles.push(await title(page).innerText());
      if (step < total) await popover(page).locator('.driver-popover-next-btn').click();
    }
    expect(titles).toContain(idleTitle);
    expect(new Set(titles).size, 'ningún paso repetido').toBe(titles.length);
    await page.keyboard.press('Escape');
    await count(popover(page).filter({ visible: true })).toBe(0);
  });

  it('el cierre del popover es un icono SVG de al menos 28 x 28 px con nombre accesible', async () => {
    const { page } = await open({ path: '/product' });
    await helpButton(page).click();
    await visible(popover(page)).toBe(true);
    const close = popover(page).locator('.driver-popover-close-btn');
    await attr(close, 'aria-label').toBe('Cerrar el recorrido');
    expect(await close.locator('svg').count()).toBe(1);
    expect((await close.innerText()).trim()).toBe('');
    const box = await close.boundingBox();
    expect(box?.width ?? 0).toBeGreaterThanOrEqual(28);
    expect(box?.height ?? 0).toBeGreaterThanOrEqual(28);
    const icon = await close.locator('svg').boundingBox();
    expect(icon?.width ?? 0).toBeGreaterThan(8);
    expect(icon?.width ?? 0).toBeLessThan(box?.width ?? 0);
    // El botón cierra el recorrido y devuelve el foco al de ayuda.
    await close.click();
    await count(popover(page).filter({ visible: true })).toBe(0);
    await focused(helpButton(page)).toBe(true);
  });

  it('cada pantalla tiene su recorrido, con títulos y textos traducidos (sin claves a la vista)', async () => {
    const { page, go } = await open({ path: '/product' });
    for (const screen of SCREENS) {
      await go(screen.path, screen.ready);
      await helpButton(page).click();
      await visible(popover(page)).toBe(true);
      const content = await popover(page).innerText();
      expect(untranslated(content), `${screen.name}: claves sin traducir en el recorrido`).toEqual([]);
      await text(title(page)).not.toBe('');
      await text(popover(page).locator('.driver-popover-description')).not.toBe('');
      const total = Number(/de (\d+)$/.exec(await progress(page).innerText())?.[1]);
      // Una pantalla puede traer un solo paso en su configuración (vistas de mapa): al menos uno, y traducido.
      expect(total, `${screen.name}: el recorrido debe tener pasos`).toBeGreaterThanOrEqual(1);
      // Todos los pasos, uno a uno, con texto traducido.
      for (let step = 1; step < total; step += 1) {
        await popover(page).locator('.driver-popover-next-btn').click();
        await text(progress(page)).toBe(`Paso ${step + 1} de ${total}`);
        expect(untranslated(await popover(page).innerText()), `${screen.name}, paso ${step + 1}`).toEqual([]);
      }
      await page.keyboard.press('Escape');
      await count(popover(page).filter({ visible: true })).toBe(0);
    }
  });

  it('también ayuda en el detalle abierto', async () => {
    const { page } = await open({ path: '/facility' });
    await page.locator('[role="row"][data-kx-row]').first().locator('[data-kx-col="name"]').dblclick();
    await attr(page.locator('section[data-th="sheet"]'), 'data-kx-mode').toBe('edit');
    await page.locator('section[data-th="sheet"]').getByRole('button', { name: 'Cancelar' }).click();
    await helpButton(page).click();
    await visible(popover(page)).toBe(true);
    expect(untranslated(await popover(page).innerText())).toEqual([]);
    await page.keyboard.press('Escape');
    await count(popover(page).filter({ visible: true })).toBe(0);
  });

  it('driver.js no se carga al arrancar: llega con el primer recorrido (M-15.2) y el botón sigue funcionando sin precarga', async () => {
    const { page } = await open({ path: '/product' });
    // Con la pantalla ya pintada y sin haber tocado el botón de ayuda, no hay trozo de driver.js.
    expect(await driverChunks(page)).toEqual([]);
    // Clic programático: sin puntero ni foco, así que no hay precarga y la carga es la del propio recorrido.
    await helpButton(page).evaluate((el) => (el as HTMLButtonElement).click());
    await visible(popover(page)).toBe(true);
    await text(progress(page)).toMatch(/^Paso 1 de \d+$/);
    expect((await driverChunks(page)).length).toBeGreaterThan(0);
    // Cierra y el foco vuelve al botón de ayuda, como antes de la carga diferida.
    await page.keyboard.press('Escape');
    await count(popover(page).filter({ visible: true })).toBe(0);
    await focused(helpButton(page)).toBe(true);
  });

  it('pasar el puntero o enfocar el botón precarga driver.js sin abrir ningún recorrido', async () => {
    const { page } = await open({ path: '/product' });
    expect(await driverChunks(page)).toEqual([]);
    await helpButton(page).hover();
    await poll(async () => (await driverChunks(page)).length).toBeGreaterThan(0);
    await count(popover(page).filter({ visible: true })).toBe(0);
  });

  it('el botón de ayuda pinta el icono generado, no el círculo de reserva', async () => {
    const { page } = await open({ path: '/product' });
    const icon = helpButton(page).locator('svg[data-th-icon="help"]');
    await visible(icon).toBe(true);
    expect(await icon.getAttribute('data-kx-icon')).toBeNull();
    expect(await icon.locator('path').count()).toBeGreaterThan(0);
  });

  it('en inglés el recorrido sale traducido y el foco también vuelve', async () => {
    const { page } = await open({ lng: 'en', path: '/product' });
    const button = page.getByRole('button', { name: 'Help', exact: true });
    await button.click();
    await visible(popover(page)).toBe(true);
    await text(progress(page)).toMatch(/^Step 1 of \d+$/);
    expect(untranslated(await popover(page).innerText())).toEqual([]);
    await page.keyboard.press('Escape');
    await count(popover(page).filter({ visible: true })).toBe(0);
    await focused(button).toBe(true);
  });
});
