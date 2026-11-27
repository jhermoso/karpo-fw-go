// Ajuste de columnas al ancho (D-R34, M-3): la rejilla nunca pinta una columna a medias. Con el detalle abierto la lista
// se estrecha, las columnas se encogen y, si no caben, se ocultan las últimas con un contador `+n` en el pie. Con ancho
// de sobra no se oculta nada. Además, las casillas de selección son un objetivo táctil de verdad (M-9) y la barra de
// lote y el deshacer llevan sus anclas de ayuda.
import type { Page } from 'playwright-core';
import { describe, expect, it } from 'vitest';
import { attr, count, focused, poll, visible } from './lib/esperar';
import { e2eSuite, mobileOptions } from './lib/suite';

/** Cabeceras pintadas de datos (sin la columna de selección ni la de acciones). */
const dataHeaders = (page: Page) => page.locator('[role="columnheader"]:not([data-kx-col^="__"])');

/**
 * Celdas pintadas (de datos y cabeceras) cuyo contenido no cabe: `scrollWidth` mayor que `clientWidth`. La última
 * columna de datos absorbe el ancho que sobra y no cuenta.
 */
async function clippedCells(page: Page): Promise<{ column: string; scrollWidth: number; clientWidth: number }[]> {
  return page.evaluate(() => {
    const columns = [...document.querySelectorAll<HTMLElement>('[data-kx-part="header-row"] > [role="columnheader"]')]
      .map((header) => header.dataset.kxCol ?? '')
      .filter((id) => !id.startsWith('__'));
    const last = columns.at(-1);
    const cells = [
      ...document.querySelectorAll<HTMLElement>('[role="grid"] [role="gridcell"], [role="grid"] [role="columnheader"]'),
    ];
    return cells
      .filter((cell) => {
        const id = cell.dataset.kxCol ?? '';
        return !id.startsWith('__') && id !== last && cell.closest('[data-kx-part="probe"]') === null;
      })
      .filter((cell) => cell.scrollWidth > cell.clientWidth)
      .map((cell) => ({
        column: cell.dataset.kxCol ?? '',
        scrollWidth: cell.scrollWidth,
        clientWidth: cell.clientWidth,
      }));
  });
}

e2eSuite('columnas, casillas, cifras y anclas de la rejilla', ({ open }) => {
  describe('ajuste de columnas', () => {
    it('con el detalle abierto en /facility ninguna celda pintada se recorta (salvo la absorbente) y aparece el contador +n', async () => {
      // Un portátil pequeño: con el detalle abierto, la lista no da para las cuatro columnas ni encogidas.
      const { page } = await open({
        path: '/facility',
        viewport: { width: 1100, height: 800 },
      });
      await count(dataHeaders(page)).toBe(4);
      await count(page.locator('[data-kx-part="fit-badge"]')).toBe(0);

      await page.locator('[role="row"][data-th="record"] [data-kx-col="name"]').first().dblclick();
      await page.waitForURL(/\/facility\/[\w-]+\?mode=edit/);
      await visible(page.locator('section[data-th="sheet"]')).toBe(true);

      // El contador +n existe y la rejilla tiene menos columnas que antes.
      const badge = page.locator('[data-kx-part="fit-badge"]');
      await count(badge).toBe(1);
      await poll(() => badge.textContent()).toMatch(/^\+\d+$/);
      const hidden = Number((await badge.textContent())?.replace('+', ''));
      await count(dataHeaders(page)).toBe(4 - hidden);
      // aria-colcount cuenta las pintadas (selección y acciones incluidas) y el contador anuncia el cambio.
      await attr(page.getByRole('grid'), 'aria-colcount').toBe(String(4 - hidden + 2));
      await attr(page.locator('[data-kx-part="fit-count"]'), 'aria-live').toBe('polite');
      const spoken = await page.locator('[data-kx-part="fit-count"] [data-kx-part="sr-only"]').textContent();
      expect(spoken).toMatch(/oculta/);

      // Ninguna celda pintada queda a medias.
      expect(await clippedCells(page)).toEqual([]);
      // La primera columna (nombre) sigue ahí.
      await count(page.locator('[role="columnheader"][data-kx-col="name"]')).toBe(1);
    });

    it('cerrar el detalle devuelve las columnas ocultas y el contador desaparece', async () => {
      const { page } = await open({
        path: '/facility',
        viewport: { width: 1100, height: 800 },
      });
      await page.locator('[role="row"][data-th="record"] [data-kx-col="name"]').first().dblclick();
      await page.waitForURL(/\/facility\/[\w-]+\?mode=edit/);
      await count(page.locator('[data-kx-part="fit-badge"]')).toBe(1);
      await page.goBack();
      await page.waitForURL(/\/facility(\?|$)/);
      await count(page.locator('[data-kx-part="fit-badge"]')).toBe(0);
      await count(dataHeaders(page)).toBe(4);
    });

    it('sin detalle, a 1440 px no hay columnas ocultas en /product (todas caben) ni celdas recortadas', async () => {
      const { page } = await open({ path: '/product' });
      await count(dataHeaders(page)).toBe(6);
      await count(page.locator('[data-kx-part="fit-badge"]')).toBe(0);
      await attr(page.getByRole('grid'), 'aria-colcount').toBe('8');
      expect(await clippedCells(page)).toEqual([]);
    });

    it('desactivar el ajuste en el menú de columnas vuelve a la lista desplazable y se recuerda', async () => {
      const { page } = await open({
        path: '/facility',
        viewport: { width: 1100, height: 800 },
      });
      await page.locator('[role="row"][data-th="record"] [data-kx-col="name"]').first().dblclick();
      await page.waitForURL(/\/facility\/[\w-]+\?mode=edit/);
      await count(page.locator('[data-kx-part="fit-badge"]')).toBe(1);
      // El detalle se abre con el foco en su primer campo: hasta entonces un menú abierto se cerraría solo.
      await focused(page.getByRole('textbox', { name: 'Nombre' })).toBe(true);
      await page.getByRole('button', { name: 'Columnas' }).click();
      const panel = page.getByRole('dialog', { name: 'Columnas de la lista' });
      await visible(panel.getByText('oculta por falta de espacio')).toBe(true);
      await panel
        .getByRole('checkbox', {
          name: 'Ajustar las columnas al ancho disponible',
        })
        .click();
      await count(page.locator('[data-kx-part="fit-badge"]')).toBe(0);
      await count(dataHeaders(page)).toBe(4);
      await page.keyboard.press('Escape');
      // Se recuerda para este usuario y esta entidad (y no toca las columnas ocultas a mano).
      const saved = await page.evaluate(() => {
        const key = Object.keys(localStorage).find(
          (name) => name.startsWith('karpo.grid.') && name.endsWith('.facility'),
        );
        return key ? (JSON.parse(localStorage.getItem(key) ?? '{}') as { autoFit?: boolean; hidden?: string[] }) : null;
      });
      expect(saved).toMatchObject({ autoFit: false, hidden: [] });
    });
  });

  describe('casillas de selección en táctil', () => {
    it('a 390 px con pantalla táctil, el objetivo de selección mide al menos 24x24 y un toque en el borde de la celda alterna una sola vez', async () => {
      const { page } = await open({
        ...mobileOptions({ width: 390, height: 844 }),
        path: '/product',
      });
      const select = page.locator('[role="row"][data-th="record"] [data-kx-col="__select"]').first();
      const box = await select.boundingBox();
      expect(box).not.toBeNull();
      expect(box?.width ?? 0).toBeGreaterThanOrEqual(24);
      expect(box?.height ?? 0).toBeGreaterThanOrEqual(24);
      // La casilla crece a la altura de un control pequeño del tema.
      const check = await select.locator('input[type="checkbox"]').boundingBox();
      expect(check?.width ?? 0).toBeGreaterThanOrEqual(20);
      expect(check?.height ?? 0).toBeGreaterThanOrEqual(20);

      const first = page.locator('[role="row"][data-th="record"][data-kx-row]').first();
      await attr(first, 'aria-selected').toBe('false');
      // Toque en la esquina de la celda, lejos de la casilla.
      await select.tap({ position: { x: 2, y: 2 } });
      await attr(first, 'aria-selected').toBe('true');
      // Y una sola vez: una segunda pulsación la quita (si hubiera doble disparo, quedaría igual).
      await select.tap({ position: { x: 2, y: 2 } });
      await attr(first, 'aria-selected').toBe('false');
      // El toque sobre la propia casilla también alterna una sola vez.
      await select.locator('input[type="checkbox"]').tap();
      await attr(first, 'aria-selected').toBe('true');
      await count(page.locator('[role="row"][data-th="record"][aria-selected="true"]')).toBe(1);
    });

    it('con ratón (escritorio) la casilla mide lo que el icono del tema y la celda no alterna', async () => {
      const { page } = await open({ path: '/product' });
      const select = page.locator('[role="row"][data-th="record"] [data-kx-col="__select"]').first();
      const sizes = await page.evaluate(() => {
        const input = document.querySelector<HTMLElement>(
          '[role="row"][data-th="record"] [data-kx-col="__select"] input',
        )!;
        const probe = document.createElement('div');
        probe.style.inlineSize = 'var(--th-control-icon-size)';
        document.body.append(probe);
        const icon = probe.getBoundingClientRect().width;
        probe.remove();
        return {
          box: input.getBoundingClientRect().width,
          icon,
          accent: getComputedStyle(input).accentColor,
        };
      });
      expect(sizes.box).toBeCloseTo(sizes.icon, 0);
      expect(sizes.accent).not.toBe('auto');
      const first = page.locator('[role="row"][data-th="record"][data-kx-row]').first();
      await select.click({ position: { x: 1, y: 1 } });
      await attr(first, 'aria-selected').toBe('false');
    });
  });

  describe('cifras y vacío', () => {
    it('las cifras de /product usan tabular-nums en la familia del cuerpo y ningún visor emite amount', async () => {
      const { page } = await open({ path: '/product' });
      await count(page.locator('[data-th-format="amount"]')).toBe(0);
      const facts = await page.evaluate(() => {
        const price = document.querySelector<HTMLElement>(
          '[role="row"][data-th="record"] [data-kx-col="basePrice"] [data-kx-ui="value"]',
        );
        const name = document.querySelector<HTMLElement>(
          '[role="row"][data-th="record"] [data-kx-col="name"] [data-kx-ui="value"]',
        );
        if (!price || !name) return null;
        const cs = getComputedStyle(price);
        return {
          numeric: cs.fontVariantNumeric,
          align: cs.textAlign,
          nowrap: cs.whiteSpace,
          family: cs.fontFamily,
          bodyFamily: getComputedStyle(name).fontFamily,
        };
      });
      expect(facts).not.toBeNull();
      expect(facts?.numeric).toContain('tabular-nums');
      expect(['end', 'right']).toContain(facts?.align);
      expect(facts?.nowrap).toBe('nowrap');
      expect(facts?.family).toBe(facts?.bodyFamily);
    });

    it('un valor vacío con «-» en la configuración se ve como «—» y se lee «Sin valor»', async () => {
      const { page } = await open({ path: '/product' });
      // «Retirado desde» está vacío en casi todos los productos y su configuración dice «-».
      const empty = page
        .locator('[role="row"][data-th="record"] [data-kx-col="discontinued"] [data-kx-empty="true"]')
        .first();
      await visible(empty).toBe(true);
      expect(await empty.locator('[aria-hidden="true"]').textContent()).toBe('—');
      expect(await empty.locator('[data-kx-part="sr-only"]').textContent()).toBe('Sin valor');
    });
  });

  describe('anclas de ayuda de la rejilla', () => {
    it('/product tiene grid.undo y la barra de lote lleva su ancla de reserva grid.toolbar, con textos en es y en', async () => {
      for (const lng of ['es', 'en'] as const) {
        const { page } = await open({ path: '/product', lng });
        await count(page.locator('[data-kx-help="grid.undo"]')).toBe(1);
        const bar = page.locator('[data-kx-ui="batch-bar"]');
        await attr(bar, 'data-kx-help').toBe('grid.batch-bar');
        await attr(bar, 'data-kx-help-fallback').toBe('grid.toolbar');
        await count(page.locator('[data-kx-help="grid.toolbar"]')).toBe(1);
      }
    });
  });
});
