// RTL (PLAN §4.6, D-R16): `?dir=rtl` con es/en. `dir` en <html>, y menú, migas, cuadrícula, árbol,
// diagrama y recorrido de ayuda en espejo (propiedades lógicas, sin reglas a mano por pantalla).
import type { Locator, Page } from 'playwright-core';
import { expect, it } from 'vitest';
import { attr, count, focused, poll, text, visible } from './lib/esperar';
import { e2eSuite } from './lib/suite';

const rect = (page: Page, selector: string) =>
  page.locator(selector).first().evaluate((el) => {
    const box = el.getBoundingClientRect();
    return { left: box.left, right: box.right, top: box.top, bottom: box.bottom, width: box.width };
  });

/** Aislantes Unicode que la interfaz pone alrededor de lo interpolado en RTL (M-12). */
const FSI = String.fromCodePoint(0x2068);
const PDI = String.fromCodePoint(0x2069);

/** Los aislantes de un texto: en RTL la interfaz los pone alrededor de lo interpolado (M-12). */
const ISOLATES = new RegExp(`[${FSI}${PDI}]`, 'g');

/** Texto del primer elemento sin los aislantes: lo que lee quien no mira los caracteres de control. */
const plainText = (locator: Locator) =>
  poll(async () => ((await locator.first().textContent({ timeout: 500 }).catch(() => null)) ?? '').replace(ISOLATES, ''));

/** Transformación calculada de cada icono con `data-kx-mirror` visible en la página. */
const mirroredTransforms = (page: Page) =>
  page.evaluate(() =>
    [...document.querySelectorAll('svg[data-th-icon][data-kx-mirror="true"]')]
      .filter((el) => el.getBoundingClientRect().width > 0)
      .map((el) => ({ name: el.getAttribute('data-th-icon') ?? '', transform: getComputedStyle(el).transform })),
  );

const TREE_READY = 'section[data-kx-view="tree"][data-kx-ready="true"]';
const MAP_READY = 'section[data-kx-view="map"][data-kx-ready="true"]';

e2eSuite('rtl', ({ open }) => {
  it('pone dir=rtl y lang en <html>; con dir=ltr y es se queda en LTR', async () => {
    const rtl = await open({ lng: 'en', dir: 'rtl', path: '/product' });
    await attr(rtl.page.locator('html'), 'dir').toBe('rtl');
    await attr(rtl.page.locator('html'), 'lang').toBe('en');
    // La interfaz está en inglés (no sólo en espejo).
    await visible(rtl.page.getByRole('grid', { name: 'Products' })).toBe(true);

    const ltr = await open({ lng: 'es', path: '/product' });
    await attr(ltr.page.locator('html'), 'dir').toBe('ltr');
    await attr(ltr.page.locator('html'), 'lang').toBe('es');
  });

  it('el menú pasa a la derecha y la barra superior y la cuadrícula se espejan', async () => {
    const { page } = await open({ lng: 'en', dir: 'rtl', path: '/product' });
    const viewport = page.viewportSize()?.width ?? 0;
    const nav = await rect(page, 'nav[data-th="nav"]');
    const main = await rect(page, 'main');
    // El menú lateral va en el borde de inicio (derecha) y el contenido queda a su izquierda.
    expect(nav.right).toBeGreaterThan(viewport - 16);
    expect(nav.left).toBeGreaterThan(viewport / 2);
    expect(main.right).toBeLessThanOrEqual(nav.left + 1);

    // El título de la pantalla (el `h1` de la cabecera de la entidad) está a la derecha de los botones de tema e idioma de la barra.
    const title = await rect(page, '[data-kx-part="entity-title"]');
    const language = await rect(page, '[role="banner"] button[aria-label="Language"], [role="banner"] button[aria-label="Idioma"]');
    expect(title.left).toBeGreaterThan(language.left);

    // Cuadrícula: la casilla de selección (primera columna) está a la derecha; las acciones, a la izquierda.
    const select = await rect(page, '[role="columnheader"][data-kx-col="__select"]');
    const first = await rect(page, '[role="columnheader"][data-kx-col="sku"]');
    const actions = await rect(page, '[role="columnheader"][data-kx-col="__actions"]');
    expect(select.left).toBeGreaterThan(first.left);
    expect(first.left).toBeGreaterThan(actions.left);
    // Y la página no desborda en horizontal.
    expect(await page.evaluate(() => document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth)).toBeLessThanOrEqual(0);

    // El contenido de la celda se alinea al inicio (derecha) y los importes al final (izquierda).
    const alignments = await page.evaluate(() => {
      const row = document.querySelector('[role="row"][data-kx-row]') as HTMLElement;
      const name = row.querySelector('[data-kx-col="name"]') as HTMLElement;
      const price = row.querySelector('[data-kx-col="basePrice"] [data-kx-ui="value"]') as HTMLElement;
      const nameValue = name.querySelector('[data-kx-ui="value"]') as HTMLElement;
      const nameBox = name.getBoundingClientRect();
      const nameText = nameValue.getBoundingClientRect();
      const priceBox = (price.closest('[role="gridcell"]') as HTMLElement).getBoundingClientRect();
      const priceText = price.getBoundingClientRect();
      return {
        nameStartGap: nameBox.right - nameText.right,
        nameEndGap: nameText.left - nameBox.left,
        priceStartGap: priceBox.right - priceText.right,
        priceEndGap: priceText.left - priceBox.left,
      };
    });
    expect(alignments.nameStartGap).toBeLessThan(alignments.nameEndGap);
    expect(alignments.priceEndGap).toBeLessThan(alignments.priceStartGap);
  });

  it('en la cuadrícula las flechas siguen la dirección visual: ← avanza a la columna siguiente', async () => {
    const { page } = await open({ lng: 'en', dir: 'rtl', path: '/product' });
    const column = () => attr(page.locator('[role="gridcell"][data-kx-active="true"]'), 'data-kx-col');
    await page.locator('[role="row"][data-kx-row] [data-kx-col="sku"]').first().click();
    await column().toBe('sku');
    await page.keyboard.press('ArrowLeft');
    await column().toBe('name');
    await page.keyboard.press('ArrowRight');
    await column().toBe('sku');
  });

  it('las migas del mapa se leen de derecha a izquierda y la raíz queda a la derecha', async () => {
    const { page } = await open({ lng: 'en', dir: 'rtl', path: '/facility?view=map&level=city&code=ES300', ready: MAP_READY });
    const crumbs = page.getByRole('navigation', { name: 'Map path' }).locator('li');
    await count(crumbs).toBe(4);
    const names = await crumbs.evaluateAll((items) => items.map((item) => item.textContent?.trim()));
    // Los nombres de las delimitaciones son datos del servidor (no se traducen); los países usan Intl.
    expect(names).toEqual(['World', 'Spain', 'Comunidad de Madrid', 'Madrid']);
    const lefts = await crumbs.evaluateAll((items) => items.map((item) => item.getBoundingClientRect().left));
    // Cada miga siguiente queda a la izquierda de la anterior.
    expect([...lefts].sort((a, b) => b - a)).toEqual(lefts);
    expect(new Set(lefts).size).toBe(4);
    // «Subir un nivel» está en el extremo opuesto al del mapa en LTR.
    const up = await rect(page, 'button[aria-keyshortcuts*="Escape"], button:has-text("Up one level")');
    const viewport = page.viewportSize()?.width ?? 0;
    expect(up.left).toBeLessThan(viewport / 2);
    await plainText(page.getByRole('status').filter({ hasText: 'boundaries' })).toMatch(/^City: 14 boundaries\.$/);
  });

  it('el árbol invierte las flechas laterales y el diagrama refleja los hermanos', async () => {
    const { page, go } = await open({ lng: 'en', dir: 'rtl', path: '/position?view=tree&layout=list', ready: TREE_READY });
    const tree = page.getByRole('tree');
    const operations = tree.getByRole('treeitem', { name: new RegExp(`^${FSI}?(Operations Director|Director/a de Operaciones)`) });
    await tree.getByRole('treeitem').first().focus();
    await page.keyboard.press('ArrowDown');
    await focused(operations).toBe(true);
    await attr(operations, 'aria-expanded').toBe('false');
    // En RTL la flecha izquierda despliega y la derecha pliega.
    await page.keyboard.press('ArrowLeft');
    await attr(operations, 'aria-expanded').toBe('true');
    await page.keyboard.press('ArrowRight');
    await attr(operations, 'aria-expanded').toBe('false');

    // La disposición va en la URL (la pone el control único de vista de la cabecera): se navega a ella.
    await go('/position?view=tree&layout=diagram', TREE_READY);
    await visible(page.locator('.react-flow__node').first()).toBe(true);
    await count(page.locator('.react-flow__node')).toBe(5);
    const xs = await page.locator('.react-flow__node').evaluateAll((nodes) =>
      nodes.map((node) => ({ label: node.textContent ?? '', x: node.getBoundingClientRect().left })),
    );
    // Los nombres de los puestos son datos traducidos en inglés (Operations, Technology) y en español el literal.
    const find = (...parts: string[]) =>
      parts.map((part) => xs.find((entry) => entry.label.includes(part))?.x).find((x) => x !== undefined) ?? Number.NaN;
    // Los hermanos van en espejo: el primero (Operaciones) a la derecha del último (Tecnología).
    expect(find('Operations', 'Operaciones')).toBeGreaterThan(find('Technology', 'Tecnología'));
    // Con es y LTR, el orden normal.
    const ltr = await open({ lng: 'es', path: '/position?view=tree&layout=diagram', ready: TREE_READY });
    await ltr.page.locator('.react-flow__node').first().waitFor();
    const ltrXs = await ltr.page.locator('.react-flow__node').evaluateAll((nodes) =>
      nodes.map((node) => ({ label: node.textContent ?? '', x: node.getBoundingClientRect().left })),
    );
    const ltrFind = (part: string) => ltrXs.find((entry) => entry.label.includes(part))?.x ?? Number.NaN;
    expect(ltrFind('Operaciones')).toBeLessThan(ltrFind('Tecnología'));
  });

  it('el recorrido de ayuda invierte las flechas y mantiene el foco', async () => {
    const { page } = await open({ lng: 'en', dir: 'rtl', path: '/product' });
    await page.getByRole('button', { name: 'Help', exact: true }).click();
    const popover = page.locator('.driver-popover');
    await visible(popover).toBe(true);
    await attr(popover, 'dir').toBe('rtl');
    await plainText(popover.locator('.driver-popover-progress-text')).toMatch(/^Step 1 of \d+$/);
    // En RTL, ← es «siguiente» y → es «anterior».
    await page.keyboard.press('ArrowLeft');
    await plainText(popover.locator('.driver-popover-progress-text')).toMatch(/^Step 2 of \d+$/);
    await page.keyboard.press('ArrowRight');
    await plainText(popover.locator('.driver-popover-progress-text')).toMatch(/^Step 1 of \d+$/);
    await page.keyboard.press('Escape');
    await count(popover.filter({ visible: true })).toBe(0);
    await focused(page.getByRole('button', { name: 'Help', exact: true })).toBe(true);
  });

  it('el detalle se espeja: el panel abre a la izquierda de la lista y los campos siguen el flujo', async () => {
    const { page } = await open({ lng: 'en', dir: 'rtl', path: '/facility' });
    await page.locator('[role="row"][data-kx-row]').first().locator('[data-kx-col="name"]').dblclick();
    const sheet = page.locator('section[data-th="sheet"]');
    await attr(sheet, 'data-kx-mode').toBe('edit');
    const list = await rect(page, '[role="grid"]');
    const detail = await rect(page, 'section[data-th="sheet"]');
    // La lista queda a la derecha (inicio) y el detalle a su izquierda (final).
    expect(detail.left).toBeLessThan(list.left);
    await poll(() => page.evaluate(() => document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth)).toBeLessThanOrEqual(0);
  });

  it('los iconos direccionales se espejan con dir=rtl (transform distinto de none) y con ltr no (B-1, hallazgo :dir)', async () => {
    const rtl = await open({ lng: 'en', dir: 'rtl', path: '/position?view=tree&layout=list', ready: TREE_READY });
    await poll(async () => (await mirroredTransforms(rtl.page)).length).toBeGreaterThan(1);
    const mirroredRtl = await mirroredTransforms(rtl.page);
    for (const icon of mirroredRtl) expect(icon.transform, `${icon.name} con dir=rtl`).not.toBe('none');
    // El espejo es horizontal: matrix(-1, 0, 0, 1, 0, 0).
    expect(mirroredRtl.every((icon) => /^matrix\(-1,\s*0,\s*0,\s*1,\s*0,\s*0\)$/.test(icon.transform))).toBe(true);
    // También en la barra lateral (el botón de plegar es un icono direccional).
    expect(mirroredRtl.map((icon) => icon.name)).toContain('sidebar');

    const ltr = await open({ lng: 'en', path: '/position?view=tree&layout=list', ready: TREE_READY });
    await poll(async () => (await mirroredTransforms(ltr.page)).length).toBeGreaterThan(1);
    for (const icon of await mirroredTransforms(ltr.page)) expect(icon.transform, `${icon.name} con ltr`).toBe('none');
  });

  it('con dir=rtl el idioma sigue siendo el inglés: el espejo no depende de :lang()', async () => {
    const { page } = await open({ lng: 'en', dir: 'rtl', path: '/product' });
    await attr(page.locator('html'), 'lang').toBe('en');
    const transforms = await mirroredTransforms(page);
    expect(transforms.length).toBeGreaterThan(0);
    for (const icon of transforms) expect(icon.transform, icon.name).not.toBe('none');
  });

  it('«N changes» y «N row cannot be saved» llevan el número aislado con FSI/PDI en RTL y limpio en LTR (M-12)', async () => {
    const edit = async (page: Page, sku: string, column: string, value: string) => {
      const target = page.locator('[role="row"][data-th="record"]', { hasText: sku }).locator(`[data-kx-col="${column}"]`);
      await target.click();
      await page.keyboard.press('F2');
      await page.keyboard.press('Control+A');
      await page.keyboard.type(value);
      await page.keyboard.press('Enter');
    };
    const rtl = await open({ lng: 'en', dir: 'rtl', path: '/product' });
    for (const [sku, uom] of [['SKU-0001', 'kg'], ['SKU-0002', 'kg'], ['SKU-0004', 'min'], ['SKU-0005', 'min']]) {
      await edit(rtl.page, sku as string, 'uom', uom as string);
    }
    const changes = rtl.page.getByText(/changes/).first();
    await text(changes).toBe(`${FSI}4${PDI} changes`);

    // Un importe que no es número deja la fila sin poder guardarse: «1 row cannot be saved».
    await edit(rtl.page, 'SKU-0003', 'basePrice', 'abc');
    await text(rtl.page.getByText(/row cannot be saved/).first()).toBe(`${FSI}1${PDI} row cannot be saved`);

    const ltr = await open({ lng: 'en', path: '/product' });
    for (const [sku, uom] of [['SKU-0001', 'kg'], ['SKU-0002', 'kg']]) await edit(ltr.page, sku as string, 'uom', uom as string);
    await text(ltr.page.getByText(/changes/).first()).toBe('2 changes');
    expect(await ltr.page.evaluate(() => document.body.textContent?.includes(String.fromCodePoint(0x2068)) ?? false)).toBe(false);
  });

  it('«Check the marked fields: 1» lleva el número aislado en RTL (M-12)', async () => {
    const { page } = await open({ lng: 'en', dir: 'rtl', path: '/facility' });
    await page.locator('[role="row"][data-kx-row]').first().locator('[data-kx-col="name"]').dblclick();
    const sheet = page.locator('section[data-th="sheet"]');
    await attr(sheet, 'data-kx-mode').toBe('edit');
    await page.keyboard.press('Control+A');
    await page.keyboard.press('Delete');
    await page.keyboard.press('Control+Enter');
    const summary = sheet.getByText(/Check the marked fields/);
    await visible(summary).toBe(true);
    await text(summary).toBe(`Check the marked fields: ${FSI}1${PDI}.`);
  });

  it('los rangos de la leyenda del mapa llevan los números aislados en RTL (M-12)', async () => {
    const { page } = await open({ lng: 'en', dir: 'rtl', path: '/facility?view=map&level=world', ready: MAP_READY });
    const items = page.locator('ul[aria-label="Map legend"] li');
    await count(items).toBeGreaterThan(1);
    const texts = await items.evaluateAll((nodes) => nodes.map((node) => node.textContent ?? ''));
    const withDigits = texts.filter((value) => /[0-9]/.test(value));
    expect(withDigits.length, `leyenda: ${texts.join(' | ')}`).toBeGreaterThan(0);
    for (const value of withDigits) {
      expect(value, 'cada número de la leyenda va entre FSI y PDI').toMatch(new RegExp(`${FSI}[^${PDI}]*[0-9][^${PDI}]*${PDI}`));
    }
    // Y la misma leyenda en LTR no lleva aislantes.
    const ltr = await open({ lng: 'en', path: '/facility?view=map&level=world', ready: MAP_READY });
    const ltrTexts = await ltr.page.locator('ul[aria-label="Map legend"] li').evaluateAll((nodes) => nodes.map((node) => node.textContent ?? ''));
    expect(ltrTexts.join('')).not.toContain(FSI);
  });
});
