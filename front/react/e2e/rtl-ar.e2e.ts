// Árabe real (D-R38, M-10): `lng=ar` sin `?dir=`. La dirección sale del idioma (`lang="ar"`, `dir="rtl"`), y con
// texto árabe de verdad se ve lo que con inglés forzado no se veía: el espejo del menú, de las migas, de los
// chevrons, del árbol, del diagrama y de las flechas de la rejilla, y el orden de las cifras dentro de las
// cadenas (las cinco del jurado) gracias a los aislantes FSI/PDI. Es traducción de demostración sin revisión
// nativa (Q-5): estas pruebas comprueban la mecánica, no el estilo del texto. Los datos de la demo (nombres de
// puestos, delimitaciones) no se traducen y salen en castellano.
import type { Locator, Page } from 'playwright-core';
import { expect, it } from 'vitest';
import { attr, count, focused, poll, route, text, visible } from './lib/esperar';
import { e2eSuite } from './lib/suite';

/** Aislantes Unicode que la interfaz pone alrededor de cada valor interpolado (M-12). */
const FSI = String.fromCodePoint(0x2068);
const PDI = String.fromCodePoint(0x2069);
const ISOLATES = new RegExp(`[${FSI}${PDI}]`, 'g');
const isolated = (value: string | number) => `${FSI}${value}${PDI}`;

/** Cifras indoarábigas: no deben salir nunca (D-R38: `numberingSystem: 'latn'` por omisión). */
const ARABIC_INDIC = /[٠-٩۰-۹]/;

const AR = { lng: 'ar', locale: 'ar' } as const;
const TREE_READY = 'section[data-kx-view="tree"][data-kx-ready="true"]';
const MAP_READY = 'section[data-kx-view="map"][data-kx-ready="true"]';

const rect = (page: Page, selector: string) =>
  page.locator(selector).first().evaluate((el) => {
    const box = el.getBoundingClientRect();
    return { left: box.left, right: box.right, top: box.top, bottom: box.bottom, width: box.width };
  });

/** Texto del primer elemento sin los aislantes: lo que lee quien no mira los caracteres de control. */
const plainText = (locator: Locator) => poll(async () => ((await locator.first().textContent({ timeout: 500 }).catch(() => null)) ?? '').replace(ISOLATES, ''));

/**
 * Posición horizontal (en pantalla) de `needle` dentro del texto de `locator`. Con `isolate`, se busca
 * `FSI needle PDI` y se mide sólo la cifra o el nombre de dentro: así se comprueba el ORDEN VISUAL de lo interpolado.
 */
const inkLeft = (locator: Locator, needle: string, isolate = false) =>
  locator.first().evaluate(
    (element, { needle: wanted, isolate: wrapped, fsi, pdi }) => {
      const search = wrapped ? `${fsi}${wanted}${pdi}` : wanted;
      const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        const content = node.textContent ?? '';
        const at = content.indexOf(search);
        if (at < 0) continue;
        const range = document.createRange();
        range.setStart(node, wrapped ? at + 1 : at);
        range.setEnd(node, (wrapped ? at + 1 : at) + wanted.length);
        const box = range.getBoundingClientRect();
        return box.left + box.width / 2;
      }
      return Number.NaN;
    },
    { needle, isolate, fsi: FSI, pdi: PDI },
  );

/** Transformación calculada de cada icono con `data-kx-mirror` visible en la página. */
const mirroredTransforms = (page: Page) =>
  page.evaluate(() =>
    [...document.querySelectorAll('svg[data-th-icon][data-kx-mirror="true"]')]
      .filter((el) => el.getBoundingClientRect().width > 0)
      .map((el) => ({ name: el.getAttribute('data-th-icon') ?? '', transform: getComputedStyle(el).transform })),
  );

const bodyText = (page: Page) => page.evaluate(() => document.body.innerText);

e2eSuite('rtl-ar', ({ open }) => {
  it('lang=ar y dir=rtl salen del idioma, sin ?dir=; la interfaz está en árabe y las cifras son latinas', async () => {
    const { page } = await open({ ...AR, path: '/product' });
    await attr(page.locator('html'), 'lang').toBe('ar');
    await attr(page.locator('html'), 'dir').toBe('rtl');
    expect(new URL(page.url()).searchParams.has('dir'), page.url()).toBe(false);
    // Árabe de verdad, no inglés en espejo: el nombre accesible de la cuadrícula y el título de la pestaña.
    await visible(page.getByRole('grid', { name: 'المنتجات' })).toBe(true);
    expect(await page.title()).toContain('المنتجات');
    await visible(page.locator('[role="banner"] button[aria-label="اللغة"]')).toBe(true);
    await visible(page.getByRole('button', { name: 'مساعدة', exact: true })).toBe(true);
    // Ninguna cifra indoarábiga en toda la pantalla.
    const body = await bodyText(page);
    expect(body).toMatch(/[؀-ۿ]/);
    expect(body).not.toMatch(ARABIC_INDIC);
    expect(body).toMatch(/SKU-0001/);

    // Con español, LTR (el espejo depende del idioma, no se queda pegado).
    const es = await open({ lng: 'es', path: '/product' });
    await attr(es.page.locator('html'), 'dir').toBe('ltr');
    await attr(es.page.locator('html'), 'lang').toBe('es');
  });

  it('el selector de idioma se llama «العربية», cambia a es (LTR) y vuelve a ar (RTL) sin recargar', async () => {
    const { page } = await open({ ...AR, path: '/product' });
    const trigger = page.locator('[role="banner"] button[aria-label="اللغة"]');
    await trigger.click();
    const arabic = page.getByRole('menuitemradio', { name: 'العربية' });
    await visible(arabic).toBe(true);
    await attr(arabic, 'aria-checked').toBe('true');
    await visible(page.getByRole('menuitemradio', { name: 'Español' })).toBe(true);
    await visible(page.getByRole('menuitemradio', { name: 'English' })).toBe(true);
    await page.getByRole('menuitemradio', { name: 'Español' }).click();
    await attr(page.locator('html'), 'lang').toBe('es');
    await attr(page.locator('html'), 'dir').toBe('ltr');
    await visible(page.getByRole('grid', { name: 'Productos' })).toBe(true);

    await page.locator('[role="banner"] button[aria-label="Idioma"]').click();
    await page.getByRole('menuitemradio', { name: 'العربية' }).click();
    await attr(page.locator('html'), 'lang').toBe('ar');
    await attr(page.locator('html'), 'dir').toBe('rtl');
    await visible(page.getByRole('grid', { name: 'المنتجات' })).toBe(true);
  });

  it('la reserva ar → en: una entidad sin texto árabe generado enseña inglés, no el castellano de la configuración', async () => {
    const { page } = await open({ ...AR, path: '/asset' });
    // El nombre de la entidad sí está en árabe (escrito a mano); las cabeceras generadas caen al inglés.
    await text(page.locator('[data-kx-part="entity-title"]')).toBe('الأصول');
    const headers = page.locator('[role="columnheader"]');
    await poll(async () => (await headers.allTextContents()).join('|')).toContain('Code|Name|Class|Cost');
    expect((await headers.allTextContents()).join('|')).not.toMatch(/Código|Nombre|Clase/);
  });

  it('el menú pasa a la derecha, las migas empiezan a la derecha y la barra superior y la cuadrícula se espejan', async () => {
    const { page } = await open({ ...AR, path: '/product' });
    const viewport = page.viewportSize()?.width ?? 0;
    const nav = await rect(page, 'nav[data-th="nav"]');
    const main = await rect(page, 'main');
    expect(nav.right).toBeGreaterThan(viewport - 16);
    expect(nav.left).toBeGreaterThan(viewport / 2);
    expect(main.right).toBeLessThanOrEqual(nav.left + 1);

    // Migas de la barra: «التجارة ‹ المنتجات»; la primera a la derecha de la segunda y los chevrones de cara a la izquierda.
    const crumbs = page.getByRole('navigation', { name: 'المسار' }).locator('li');
    await count(crumbs).toBe(2);
    expect(await crumbs.allTextContents()).toEqual(['التجارة', 'المنتجات']);
    const lefts = await crumbs.evaluateAll((items) => items.map((item) => item.getBoundingClientRect().left));
    expect(lefts[0]).toBeGreaterThan(lefts[1] ?? Number.POSITIVE_INFINITY);

    // El título de la pantalla queda a la derecha de los botones de tema e idioma.
    const title = await rect(page, '[data-kx-part="entity-title"]');
    const language = await rect(page, '[role="banner"] button[aria-label="اللغة"]');
    expect(title.left).toBeGreaterThan(language.left);

    // Rejilla: la casilla de selección (primera columna) a la derecha, las acciones a la izquierda.
    const select = await rect(page, '[role="columnheader"][data-kx-col="__select"]');
    const first = await rect(page, '[role="columnheader"][data-kx-col="sku"]');
    const actions = await rect(page, '[role="columnheader"][data-kx-col="__actions"]');
    expect(select.left).toBeGreaterThan(first.left);
    expect(first.left).toBeGreaterThan(actions.left);
    expect(await page.evaluate(() => document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth)).toBeLessThanOrEqual(0);
  });

  it('los chevrones y flechas direccionales se espejan con el árabe y no con español', async () => {
    const rtl = await open({ ...AR, path: '/position?view=tree&layout=list', ready: TREE_READY });
    await poll(async () => (await mirroredTransforms(rtl.page)).length).toBeGreaterThan(1);
    const mirrored = await mirroredTransforms(rtl.page);
    expect(mirrored.every((icon) => /^matrix\(-1,\s*0,\s*0,\s*1,\s*0,\s*0\)$/.test(icon.transform)), JSON.stringify(mirrored)).toBe(true);
    expect(mirrored.map((icon) => icon.name)).toEqual(expect.arrayContaining(['sidebar']));

    const ltr = await open({ lng: 'es', path: '/position?view=tree&layout=list', ready: TREE_READY });
    await poll(async () => (await mirroredTransforms(ltr.page)).length).toBeGreaterThan(1);
    for (const icon of await mirroredTransforms(ltr.page)) expect(icon.transform, `${icon.name} en español`).toBe('none');
  });

  it('en la cuadrícula la flecha izquierda avanza a la columna siguiente (el orden visual manda)', async () => {
    const { page } = await open({ ...AR, path: '/product' });
    const column = () => attr(page.locator('[role="gridcell"][data-kx-active="true"]'), 'data-kx-col');
    await page.locator('[role="row"][data-kx-row] [data-kx-col="sku"]').first().click();
    await column().toBe('sku');
    await page.keyboard.press('ArrowLeft');
    await column().toBe('name');
    await page.keyboard.press('ArrowRight');
    await column().toBe('sku');
  });

  it('el árbol invierte las flechas laterales y el diagrama refleja los hermanos', async () => {
    const { page, go } = await open({ ...AR, path: '/position?view=tree&layout=list', ready: TREE_READY });
    const tree = page.getByRole('tree');
    const operations = tree.getByRole('treeitem', { name: /Director\/a de Operaciones/ });
    await tree.getByRole('treeitem').first().focus();
    await page.keyboard.press('ArrowDown');
    await focused(operations).toBe(true);
    await attr(operations, 'aria-expanded').toBe('false');
    // En RTL la flecha izquierda despliega y la derecha pliega.
    await page.keyboard.press('ArrowLeft');
    await attr(operations, 'aria-expanded').toBe('true');
    await page.keyboard.press('ArrowRight');
    await attr(operations, 'aria-expanded').toBe('false');
    // Los contadores de descendientes salen en árabe (categoría plural propia) y con la cifra aislada.
    await poll(() => tree.getByRole('treeitem').first().textContent()).toMatch(new RegExp(`${FSI}14${PDI} تابعًا على الأقل`));

    await go('/position?view=tree&layout=diagram', TREE_READY);
    await visible(page.locator('.react-flow__node').first()).toBe(true);
    await count(page.locator('.react-flow__node')).toBe(5);
    const xs = await page.locator('.react-flow__node').evaluateAll((nodes) =>
      nodes.map((node) => ({ label: node.textContent ?? '', x: node.getBoundingClientRect().left })),
    );
    const at = (part: string) => xs.find((entry) => entry.label.includes(part))?.x ?? Number.NaN;
    // Los hermanos van en espejo: el primero (Operaciones) a la derecha del último (Tecnología).
    expect(at('Operaciones')).toBeGreaterThan(at('Tecnología'));
    // El diagrama conserva sus controles con nombre en árabe.
    await visible(page.getByRole('button', { name: 'ملاءمة الشاشة', exact: true })).toBe(true);
  });

  it('las migas del mapa se leen de derecha a izquierda, con el país en árabe, y «subir un nivel» queda al otro extremo', async () => {
    const { page } = await open({ ...AR, path: '/facility?view=map&level=city&code=ES300', ready: MAP_READY });
    const crumbs = page.getByRole('navigation', { name: 'مسار الخريطة' }).locator('li');
    await count(crumbs).toBe(4);
    expect((await crumbs.allTextContents()).map((item) => item.trim())).toEqual(['العالم', 'إسبانيا', 'Comunidad de Madrid', 'Madrid']);
    const lefts = await crumbs.evaluateAll((items) => items.map((item) => item.getBoundingClientRect().left));
    expect([...lefts].sort((a, b) => b - a)).toEqual(lefts);
    expect(new Set(lefts).size).toBe(4);
    const up = await page.getByRole('button', { name: /صعود مستوى/ }).first().evaluate((el) => el.getBoundingClientRect().left);
    expect(up).toBeLessThan((page.viewportSize()?.width ?? 0) / 2);
  });

  it('las cinco cadenas del jurado: cada cifra va entre FSI y PDI, en orden lógico, y se pinta en el orden visual correcto', async () => {
    // 1. Recuento de la cabecera y 5. filas del pie.
    const grid = await open({ ...AR, path: '/product' });
    const header = grid.page.locator('[data-kx-part="entity-count"]');
    await poll(async () => (await header.textContent()) ?? '').toMatch(new RegExp(`^${FSI}\\d+${PDI} ${FSI}المنتجات${PDI}$`));
    const total = /\d+/.exec((await header.textContent()) ?? '')?.[0] ?? '';
    const footer = grid.page.locator('[data-kx-part="footer-count"]');
    await text(footer).toBe(`${isolated(50)} من ${isolated(total)} صفًا`);
    // Orden visual (de derecha a izquierda): 50, «من», total, «صفًا». La cifra que va primero queda a la derecha.
    const x = {
      loaded: await inkLeft(footer, '50', true),
      of: await inkLeft(footer, 'من'),
      total: await inkLeft(footer, total, true),
      rows: await inkLeft(footer, 'صفًا'),
    };
    expect(x.loaded, JSON.stringify(x)).toBeGreaterThan(x.of);
    expect(x.of, JSON.stringify(x)).toBeGreaterThan(x.total);
    expect(x.total, JSON.stringify(x)).toBeGreaterThan(x.rows);
    // En la cabecera, la cifra queda a la derecha del nombre.
    expect(await inkLeft(header, total, true)).toBeGreaterThan(await inkLeft(header, 'المنتجات', true));

    // 2. «الخطوة N من M» del recorrido de ayuda.
    await grid.page.getByRole('button', { name: 'مساعدة', exact: true }).click();
    const popover = grid.page.locator('.driver-popover');
    await visible(popover).toBe(true);
    await attr(popover, 'dir').toBe('rtl');
    const progress = popover.locator('.driver-popover-progress-text');
    await poll(async () => (await progress.textContent()) ?? '').toMatch(new RegExp(`^الخطوة ${FSI}1${PDI} من ${FSI}\\d+${PDI}$`));
    const steps = /من ⁨(\d+)⁩/.exec((await progress.textContent()) ?? '')?.[1] ?? '';
    expect(Number(steps), 'el recorrido de /product tiene al menos 7 pasos').toBeGreaterThanOrEqual(7);
    const step = {
      label: await inkLeft(progress, 'الخطوة'),
      current: await inkLeft(progress, '1', true),
      of: await inkLeft(progress, 'من'),
      steps: await inkLeft(progress, steps, true),
    };
    expect(step.label, JSON.stringify(step)).toBeGreaterThan(step.current);
    expect(step.current, JSON.stringify(step)).toBeGreaterThan(step.of);
    expect(step.of, JSON.stringify(step)).toBeGreaterThan(step.steps);
    // En RTL, ← es «siguiente» y → es «anterior».
    await grid.page.keyboard.press('ArrowLeft');
    await plainText(progress).toMatch(/^الخطوة 2 من \d+$/);
    await grid.page.keyboard.press('ArrowRight');
    await plainText(progress).toMatch(/^الخطوة 1 من \d+$/);
    await grid.page.keyboard.press('Escape');
    await count(popover.filter({ visible: true })).toBe(0);
    await focused(grid.page.getByRole('button', { name: 'مساعدة', exact: true })).toBe(true);

    // 3. Campos marcados del detalle.
    const detail = await open({ ...AR, path: '/facility' });
    await detail.page.locator('[role="row"][data-kx-row]').first().locator('[data-kx-col="name"]').dblclick();
    const sheet = detail.page.locator('section[data-th="sheet"]');
    await attr(sheet, 'data-kx-mode').toBe('edit');
    await detail.page.keyboard.press('Control+A');
    await detail.page.keyboard.press('Delete');
    await detail.page.keyboard.press('Control+Enter');
    const summary = sheet.getByText(/راجع الحقول المحددة/);
    await visible(summary).toBe(true);
    await text(summary).toBe(`راجع الحقول المحددة: ${isolated(1)}.`);
    // El detalle se espeja: la lista a la derecha (inicio) y la ficha a su izquierda.
    expect((await rect(detail.page, 'section[data-th="sheet"]')).left).toBeLessThan((await rect(detail.page, '[role="grid"]')).left);

    // 4. Límites del mundo y de la ciudad (delimitaciones del nivel), con la categoría plural «many».
    const world = await open({ ...AR, path: '/facility?view=map&level=world', ready: MAP_READY });
    const status = world.page.getByRole('status').filter({ hasText: 'حدًّا إداريًا' });
    await poll(async () => (await status.first().textContent()) ?? '').toMatch(new RegExp(`^${FSI}العالم${PDI}: ${FSI}\\d+${PDI} حدًّا إداريًا\\.$`));
    const legend = world.page.locator('ul[aria-label="مفتاح الخريطة"] li');
    await count(legend).toBeGreaterThan(1);
    const withDigits = (await legend.allTextContents()).filter((value) => /[0-9]/.test(value));
    expect(withDigits.length).toBeGreaterThan(0);
    for (const value of withDigits) expect(value, 'cada cifra de la leyenda va aislada').toMatch(new RegExp(`${FSI}[^${PDI}]*[0-9][^${PDI}]*${PDI}`));
    await world.go('/facility?view=map&level=city&code=ES300', MAP_READY);
    await poll(async () => (await world.page.getByRole('status').filter({ hasText: 'المدينة' }).first().textContent()) ?? '').toBe(
      `${isolated('المدينة')}: ${isolated(14)} حدًّا إداريًا.`,
    );
    expect(await bodyText(world.page)).not.toMatch(ARABIC_INDIC);
  });

  it('los recuentos de la barra de cambios usan las categorías plurales del árabe y aíslan la cifra', async () => {
    const { page } = await open({ ...AR, path: '/product' });
    const edit = async (sku: string, column: string, value: string) => {
      const target = page.locator('[role="row"][data-th="record"]', { hasText: sku }).locator(`[data-kx-col="${column}"]`);
      await target.click();
      await page.keyboard.press('F2');
      await page.keyboard.press('Control+A');
      await page.keyboard.type(value);
      await page.keyboard.press('Enter');
    };
    const changes = page.locator('[data-kx-part="count"]', { hasText: 'تغيير' });
    await edit('SKU-0001', 'uom', 'kg');
    await text(changes).toBe('تغيير واحد');
    await edit('SKU-0002', 'uom', 'kg');
    await text(changes).toBe('تغييران');
    await edit('SKU-0004', 'uom', 'min');
    await text(changes).toBe(`${isolated(3)} تغييرات`);
    await edit('SKU-0005', 'uom', 'min');
    await text(changes).toBe(`${isolated(4)} تغييرات`);
    // Un importe que no es número deja la fila sin poder guardarse.
    await edit('SKU-0003', 'basePrice', 'abc');
    await text(page.locator('[data-kx-part="blocked"]')).toBe('صف واحد لا يمكن حفظه');
    expect(await bodyText(page)).not.toMatch(ARABIC_INDIC);
  });

  it('el recorrido de ayuda en árabe: popover RTL, botones con texto árabe y el foco vuelve al botón de ayuda', async () => {
    const { page } = await open({ ...AR, path: '/product' });
    await page.getByRole('button', { name: 'مساعدة', exact: true }).click();
    const popover = page.locator('.driver-popover');
    await visible(popover).toBe(true);
    await text(popover.locator('.driver-popover-next-btn')).toBe('التالي');
    await attr(popover.locator('.driver-popover-close-btn'), 'aria-label').toBe('إغلاق الجولة');
    const box = await popover.evaluate((el) => {
      const bounds = el.getBoundingClientRect();
      return { left: bounds.left, right: bounds.right, width: window.innerWidth };
    });
    expect(box.left).toBeGreaterThanOrEqual(0);
    expect(box.right).toBeLessThanOrEqual(box.width);
    await page.keyboard.press('Escape');
    await count(popover.filter({ visible: true })).toBe(0);
  });

  it('la ruta se conserva al cambiar de idioma y la página no desborda en horizontal en ninguna pantalla clave', async () => {
    const { page, go } = await open({ ...AR, path: '/product' });
    for (const path of ['/product', '/facility', '/position?view=tree&layout=list', '/facility?view=map&level=world']) {
      await go(path, path.includes('view=tree') ? TREE_READY : path.includes('view=map') ? MAP_READY : undefined);
      await route(page).toBe(path);
      await poll(() => page.evaluate(() => document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth)).toBeLessThanOrEqual(0);
    }
  });
});
