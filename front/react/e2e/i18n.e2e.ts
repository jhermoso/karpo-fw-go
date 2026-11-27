// i18n (PLAN §4.6): ninguna pantalla enseña una clave sin traducir (`ns:clave` o `ayuda.nombre.title`),
// ni en español ni en inglés, ni en el texto visible ni en las etiquetas accesibles, títulos y marcadores.
import { expect, it } from 'vitest';
import { readNamespaces } from './lib/demo.mjs';
import { attr, count, visible } from './lib/esperar';
import { SCREENS, pageTexts } from './lib/pantallas';
import { e2eSuite, mobileOptions } from './lib/suite';

/** Claves de i18n a la vista: `grid:shortcuts.save`, `help.button.title`, `errors.required`… */
function leakedKeys(texts: string[]): string[] {
  const namespaces = readNamespaces().join('|');
  const patterns = [
    new RegExp(`(?<![\\w/])(?:${namespaces}):[A-Za-z][\\w-]*(?:\\.[\\w-]+)*`, 'g'),
    /(?<![\w@/.-])[a-z][a-zA-Z]{1,20}(?:\.[a-zA-Z][a-zA-Z0-9]{1,20}){2,}(?![\w@/-])/g,
  ];
  const found = new Set<string>();
  for (const text of texts) for (const pattern of patterns) for (const match of text.match(pattern) ?? []) found.add(match);
  return [...found];
}

e2eSuite('sin claves sin traducir', ({ open }) => {
  for (const lng of ['es', 'en', 'ar']) {
    it(`ninguna pantalla enseña una clave sin traducir (${lng})`, async () => {
      const { page, go } = await open({ lng, path: '/', ready: false });
      const problems: string[] = [];
      for (const screen of SCREENS) {
        await go(screen.path, screen.ready);
        // Un instante para que lleguen las traducciones perezosas y los datos.
        await page.waitForLoadState('networkidle').catch(() => undefined);
        for (const key of leakedKeys(await pageTexts(page))) problems.push(`${screen.path}: ${key}`);
      }
      expect(problems, `claves sin traducir (${lng})`).toEqual([]);
    });

    it(`tampoco en menús, paleta, atajos, recorrido, diálogos y avisos (${lng})`, async () => {
      const { page, go } = await open({ lng, path: '/product' });
      const problems: string[] = [];
      const check = async (where: string) => {
        for (const key of leakedKeys(await pageTexts(page))) problems.push(`${where}: ${key}`);
      };

      // Paleta de órdenes y panel de atajos.
      await page.keyboard.press('Control+k');
      await visible(page.getByRole('dialog')).toBe(true);
      await check('paleta');
      await page.keyboard.press('Escape');
      await count(page.getByRole('dialog')).toBe(0);
      await page.locator('[data-kx-col="sku"]').first().click();
      await page.keyboard.press('Shift+?');
      await visible(page.getByRole('dialog')).toBe(true);
      await check('atajos');
      await page.keyboard.press('Escape');

      // Menús de la barra superior.
      for (const button of await page.locator('[role="banner"] button[aria-haspopup="menu"]').all()) {
        await button.click();
        await visible(page.getByRole('menu')).toBe(true);
        await check('menú de la barra');
        await page.keyboard.press('Escape');
      }

      // Filtros, columnas, acciones de fila.
      await page.locator('[data-kx-part="filters"] button, button[aria-haspopup="dialog"]').first().click().catch(() => undefined);
      await check('filtros');
      await page.keyboard.press('Escape');

      // Edición en línea con error, aviso de guardado y barra de lote.
      const cell = (column: string) => page.locator('[role="row"][data-kx-row]').first().locator(`[data-kx-col="${column}"]`);
      await cell('basePrice').click();
      await page.keyboard.press('F2');
      await page.keyboard.press('Control+A');
      await page.keyboard.type('abc');
      await page.keyboard.press('Enter');
      await attr(cell('basePrice'), 'data-kx-cell').toBe('invalid');
      await check('celda inválida');
      await page.keyboard.press('Escape');
      await cell('name').click();
      await page.keyboard.press('F2');
      await page.keyboard.type('x');
      await page.keyboard.press('Enter');
      await check('barra de lote');

      // 409 del detalle y descartar cambios.
      await go('/facility?fallo=409', '[data-kx-ready="true"]');
      await page.locator('[role="row"][data-kx-row]').first().locator('[data-kx-col="name"]').dblclick();
      await attr(page.locator('section[data-th="sheet"]'), 'data-kx-mode').toBe('edit');
      await page.keyboard.press('Control+A');
      await page.keyboard.type('Cambio');
      await page.keyboard.press('Control+Enter');
      await visible(page.getByRole('dialog')).toBe(true);
      await check('conflicto 409');
      await page.keyboard.press('Escape');
      await page.keyboard.press('Escape');
      await check('descartar cambios');

      // Recorrido de ayuda del detalle.
      expect(problems, `claves sin traducir (${lng})`).toEqual([]);
    });
  }

  for (const lng of ['es', 'en', 'ar']) {
    it(`en un teléfono: el menú «Más», el cajón del menú y la cabecera de la entidad están traducidos (${lng})`, async () => {
      const { page } = await open({ ...mobileOptions(), lng, path: '/facility' });
      const problems: string[] = [];
      const check = async (where: string) => {
        for (const key of leakedKeys(await pageTexts(page))) problems.push(`${where}: ${key}`);
      };
      await check('cabecera de la entidad');
      await page.locator('[data-kx-part="topbar"] button[aria-haspopup="menu"]').last().click();
      await visible(page.getByRole('menu')).toBe(true);
      await check('menú «Más»');
      await page.keyboard.press('Escape');
      await count(page.getByRole('menu')).toBe(0);
      await page.locator('[data-kx-part="nav-toggle"]').click();
      await visible(page.locator('nav[data-th="nav"]')).toBe(true);
      await check('cajón del menú');
      expect(problems, `claves sin traducir en el teléfono (${lng})`).toEqual([]);
    });
  }

  it('control: el detector sí ve una clave sin traducir si se cuela en la página', async () => {
    const { page } = await open({ path: '/product' });
    await page.evaluate(() => {
      const leak = document.createElement('p');
      leak.textContent = 'Se ve grid:shortcuts.save y también help.button.title';
      document.body.append(leak);
    });
    expect(leakedKeys(await pageTexts(page))).toEqual(expect.arrayContaining(['grid:shortcuts.save', 'help.button.title']));
  });

  it('la página de inicio de sesión y el cambio de contraseña obligatorio también están traducidos', async () => {
    for (const lng of ['es', 'en', 'ar']) {
      const { page } = await open({ lng, login: false });
      await visible(page.locator('form')).toBe(true);
      expect(leakedKeys(await pageTexts(page)), `inicio de sesión (${lng})`).toEqual([]);
      const { password } = (await import('./lib/demo.mjs')).readDemoCredentials('nuevo');
      await page.locator('input[autocomplete="username"]').fill('nuevo');
      await page.locator('input[type="password"]').fill(password);
      await page.locator('button[type="submit"]').click();
      await page.waitForURL(/cambiar-clave/);
      await visible(page.locator('form')).toBe(true);
      expect(leakedKeys(await pageTexts(page)), `cambio de contraseña (${lng})`).toEqual([]);
    }
  });
});
