// Organigrama (PLAN §4.11): listado ⇄ diagrama (la disposición viene de la URL; la cabecera de la entidad la
// cambia), plegado, zoom legible, roles de los nodos y teclado.
import type { Page } from 'playwright-core';
import { expect, it } from 'vitest';
import { attr, count, focused, poll, route, text, visible } from './lib/esperar';
import { e2eSuite } from './lib/suite';

const READY = 'section[data-kx-view="tree"][data-kx-ready="true"]';
const tree = (page: Page) => page.getByRole('tree', { name: 'Organigrama de Puestos' });
const item = (page: Page, title: string) => tree(page).getByRole('treeitem', { name: new RegExp(`^${title}`) });
const nodes = (page: Page) => page.locator('.react-flow__node');
const diagramRegion = (page: Page, name: string) => page.getByRole('region', { name });
const fitButton = (page: Page) => page.getByRole('button', { name: 'Ajustar a la pantalla' });
/** El diagrama de la entidad tiene `data-kx-zoom` (`near` o `far`). */
const zoomState = (page: Page) => page.locator('[data-kx-part="diagram"]').getAttribute('data-kx-zoom');

/** Tamaño de letra (px) del título de la primera tarjeta, tal como se calcula en el navegador. */
const labelSize = (page: Page) =>
  page
    .locator('[data-kx-part="diagram"] [data-kx-part="card"] [data-kx-part="label"]')
    .first()
    .evaluate((el) => Number.parseFloat(getComputedStyle(el).fontSize));

/** Escala actual del lienzo del diagrama (`transform: translate(..) scale(k)`). */
async function zoomOf(page: Page): Promise<number> {
  const transform = await page.locator('.react-flow__viewport').evaluate((el) => getComputedStyle(el).transform);
  const matrix = /matrix\(([^)]+)\)/.exec(transform);
  return matrix ? Number(matrix[1]?.split(',')[0]) : 1;
}

e2eSuite('organigrama', ({ open }) => {
  it('la disposición viene de la URL: el diagrama ofrece «Ver como lista» y la vista ya no lleva interruptor propio', async () => {
    const { page, go } = await open({ path: '/position?view=tree&layout=list', ready: READY });
    await visible(tree(page)).toBe(true);
    // El control de vista es el de la cabecera de la entidad (M-6): aquí no hay «Listado | Diagrama».
    await count(page.getByRole('group', { name: 'Disposición del organigrama' })).toBe(0);
    await count(page.getByRole('button', { name: 'Diagrama', exact: true })).toBe(0);

    await go('/position?view=tree&layout=diagram', READY);
    await visible(diagramRegion(page, 'Diagrama de Puestos')).toBe(true);
    await count(nodes(page)).toBeGreaterThan(0);
    await count(tree(page)).toBe(0);

    // «Ver como lista»: un enlace de verdad (con su dirección) que vuelve al listado.
    const link = page.getByRole('link', { name: 'Ver como lista' });
    await visible(link).toBe(true);
    await attr(link, 'href').toMatch(/layout=list/);
    await link.click();
    await route(page).toMatch(/layout=list/);
    await visible(tree(page)).toBe(true);
  });

  it('el listado es un role=tree con niveles, plegado con el teclado y selección que sobrevive al diagrama', async () => {
    const { page, go } = await open({ path: '/position?view=tree&layout=list', ready: READY });
    const root = item(page, 'Director/a General');
    await attr(root, 'aria-level').toBe('1');
    await attr(root, 'aria-expanded').toBe('true');
    await attr(item(page, 'Director/a de Operaciones'), 'aria-level').toBe('2');

    // Flechas: abajo baja, derecha despliega, izquierda pliega.
    await root.focus();
    await page.keyboard.press('ArrowDown');
    const operations = item(page, 'Director/a de Operaciones');
    await focused(operations).toBe(true);
    await attr(operations, 'aria-expanded').toBe('false');
    await page.keyboard.press('ArrowRight');
    await attr(operations, 'aria-expanded').toBe('true');
    const sizeOpen = await tree(page).getByRole('treeitem').count();
    await page.keyboard.press('ArrowLeft');
    await attr(operations, 'aria-expanded').toBe('false');
    await poll(() => tree(page).getByRole('treeitem').count()).toBeLessThan(sizeOpen);

    // Inicio y Fin; Espacio elige.
    await page.keyboard.press('End');
    await attr(page.locator('[role="treeitem"][tabindex="0"]'), 'aria-level').not.toBe('');
    await page.keyboard.press('Home');
    await focused(root).toBe(true);
    await page.keyboard.press('ArrowDown');
    await page.keyboard.press('Space');
    await attr(operations, 'aria-selected').toBe('true');

    // Plegado y selección se conservan al pasar al diagrama y volver.
    await go('/position?view=tree&layout=diagram', READY);
    await visible(diagramRegion(page, 'Diagrama de Puestos')).toBe(true);
    await visible(page.getByRole('button', { name: 'Desplegar la rama de Director/a de Operaciones' })).toBe(true);
    await page.getByRole('link', { name: 'Ver como lista' }).click();
    await attr(item(page, 'Director/a de Operaciones'), 'aria-selected').toBe('true');
    await attr(item(page, 'Director/a de Operaciones'), 'aria-expanded').toBe('false');
  });

  it('el diagrama pliega y despliega ramas con su contador', async () => {
    const { page } = await open({ path: '/position?view=tree&layout=diagram', ready: READY });
    await count(nodes(page)).toBe(5);
    const fold = page.getByRole('button', { name: 'Desplegar la rama de Director/a de Operaciones' });
    // Completo («3») o, si hay ramas sin cargar, un mínimo («≥3») con su nombre accesible.
    await text(fold).toMatch(/^(≥\d+|\d+)$/);

    await fold.click();
    await poll(() => nodes(page).count()).toBeGreaterThan(5);
    const expanded = await nodes(page).count();
    await visible(page.getByRole('button', { name: 'Plegar la rama de Director/a de Operaciones' })).toBe(true);

    await page.getByRole('button', { name: 'Plegar la rama de Director/a de Operaciones' }).click();
    await count(nodes(page)).toBeLessThan(expanded);

    // Plegar la raíz deja sólo la raíz; «Desplegar todo» lo abre todo.
    await page.getByRole('button', { name: 'Plegar la rama de Director/a General' }).click();
    await count(nodes(page)).toBe(1);
    await page.getByRole('button', { name: 'Desplegar todo' }).click();
    await count(nodes(page)).toBeGreaterThan(expanded);
  });

  it('el diagrama hace zoom con los botones, con la rueda y se ajusta a la pantalla', async () => {
    const { page } = await open({ path: '/position?view=tree&layout=diagram', ready: READY });
    await page.locator('.react-flow__node').first().waitFor();
    // El encuadre inicial se aplica un fotograma después de montar.
    await poll(() => zoomOf(page)).toBeGreaterThan(0);
    const fitted = await zoomOf(page);

    await page.getByRole('button', { name: 'Acercar' }).click();
    await poll(() => zoomOf(page)).toBeGreaterThan(fitted);
    const zoomedIn = await zoomOf(page);
    await page.getByRole('button', { name: 'Alejar' }).click();
    await page.getByRole('button', { name: 'Alejar' }).click();
    await poll(() => zoomOf(page)).toBeLessThan(zoomedIn);

    // Rueda del ratón sobre el lienzo.
    const canvas = page.locator('.react-flow__pane');
    const box = await canvas.boundingBox();
    expect(box).not.toBeNull();
    const before = await zoomOf(page);
    await page.mouse.move((box?.x ?? 0) + (box?.width ?? 0) / 2, (box?.y ?? 0) + (box?.height ?? 0) / 2);
    await page.mouse.wheel(0, -400);
    await poll(() => zoomOf(page)).toBeGreaterThan(before);

    await page.getByRole('button', { name: 'Ajustar a la pantalla' }).click();
    await poll(() => zoomOf(page).then((zoom) => Math.abs(zoom - fitted) < 0.05)).toBe(true);
    await visible(page.getByRole('img', { name: 'Minimapa del diagrama' })).toBe(true);
  });

  it('el diagrama se recorre con el teclado y el doble clic abre el detalle en edición', async () => {
    const { page, go } = await open({ path: '/position?view=tree&layout=diagram', ready: READY });
    const rootNode = page.getByRole('button', { name: /^Director\/a General/ }).first();
    await rootNode.focus();
    await focused(rootNode).toBe(true);
    // ↓ primer hijo, → hermano siguiente, ← hermano anterior, ↑ padre.
    await page.keyboard.press('ArrowDown');
    await poll(() => page.evaluate(() => document.activeElement?.getAttribute('aria-label') ?? document.activeElement?.textContent ?? '')).toMatch(/Director\/a de Operaciones/);
    await page.keyboard.press('ArrowRight');
    await poll(() => page.evaluate(() => document.activeElement?.getAttribute('aria-label') ?? document.activeElement?.textContent ?? '')).toMatch(/Director\/a Financiero\/a/);
    await page.keyboard.press('ArrowLeft');
    await poll(() => page.evaluate(() => document.activeElement?.getAttribute('aria-label') ?? document.activeElement?.textContent ?? '')).toMatch(/Director\/a de Operaciones/);
    await page.keyboard.press('ArrowUp');
    await poll(() => page.evaluate(() => document.activeElement?.getAttribute('aria-label') ?? document.activeElement?.textContent ?? '')).toMatch(/Director\/a General/);

    // Doble clic = abrir el detalle. Los puestos son sólo lectura en la demo (Go no los edita): el
    // detalle se abre y la edición baja a lectura; con instalaciones, que sí se editan, abre en edición.
    const sheet = page.locator('section[data-th="sheet"]');
    await page.getByRole('button', { name: /^Director\/a Comercial/ }).first().dblclick();
    await page.waitForURL(/\/position\/[\w-]+\?.*mode=edit/);
    await text(sheet.getByRole('heading', { level: 2 })).toBe('Director/a Comercial');
    await attr(sheet, 'data-kx-mode').toBe('read');

    await go('/facility?view=tree&layout=diagram', READY);
    await page.locator('.react-flow__node').first().waitFor();
    await page.locator('.react-flow__node [data-kx-part="main"]').first().dblclick();
    await page.waitForURL(/\/facility\/[\w-]+\?.*mode=edit/);
    await attr(sheet, 'data-kx-mode').toBe('edit');
  });

  it('la búsqueda resalta la coincidencia en el listado y la mantiene en el diagrama', async () => {
    const { page, go } = await open({ path: '/position?view=tree&layout=list', ready: READY });
    const search = page.getByRole('searchbox', { name: 'Buscar en el organigrama' });
    await search.fill('Tecnología');
    await text(page.getByRole('status').filter({ hasText: 'Coincidencia' })).toBe('Coincidencia 1 de 1');
    await count(tree(page).locator('mark')).toBe(1);
    await page.getByRole('button', { name: 'Coincidencia siguiente' }).click();

    await go('/position?view=tree&layout=diagram', READY);
    await visible(diagramRegion(page, 'Diagrama de Puestos')).toBe(true);
    await text(page.getByRole('status').filter({ hasText: 'Coincidencia' })).toBe('Coincidencia 1 de 1');
    await attr(search, 'type').toBe('search');
  });

  it('el encuadre inicial es legible (zoom × letra ≥ 11 px) y «Ajustar» alterna entre legible y todo el organigrama', async () => {
    const { page } = await open({ path: '/facility?view=tree&layout=diagram', ready: READY });
    await page.locator('.react-flow__node').first().waitFor();
    await poll(() => zoomOf(page)).toBeGreaterThan(0);
    const legible = await zoomOf(page);
    // Un organigrama grande no se encoge hasta lo ilegible: el zoom del encuadre inicial no baja de 0,85...
    expect(legible).toBeGreaterThanOrEqual(0.85 - 0.01);
    // ...y a ese zoom la letra del título se ve a 11 px o más.
    expect(legible * (await labelSize(page))).toBeGreaterThanOrEqual(11 - 0.05);
    expect(await zoomState(page)).toBe('near');

    // «Ajustar» enseña todo el organigrama (más pequeño) y vuelve al legible con otro pulso.
    await attr(fitButton(page), 'aria-pressed').toBe('false');
    await fitButton(page).click();
    await attr(fitButton(page), 'aria-pressed').toBe('true');
    await poll(() => zoomOf(page).then((zoom) => zoom < legible - 0.05)).toBe(true);
    await poll(() => zoomState(page)).toBe('far');
    await fitButton(page).click();
    await attr(fitButton(page), 'aria-pressed').toBe('false');
    await poll(() => zoomOf(page).then((zoom) => Math.abs(zoom - legible) < 0.02)).toBe(true);
    await poll(() => zoomState(page)).toBe('near');
  });

  it('lejos (zoom < 0,6) sólo quedan el título y el contador, y el título se sigue viendo a 11 px', async () => {
    const { page } = await open({ path: '/facility?view=tree&layout=diagram', ready: READY });
    await page.locator('.react-flow__node').first().waitFor();
    await poll(() => zoomOf(page)).toBeGreaterThan(0.8);
    // Dos pasos de «Alejar» desde el encuadre legible (0,85 → ≈ 0,59).
    await page.getByRole('button', { name: 'Alejar' }).click();
    await page.getByRole('button', { name: 'Alejar' }).click();
    await poll(() => zoomState(page)).toBe('far');
    const zoom = await zoomOf(page);
    expect(zoom).toBeLessThan(0.6);
    expect(zoom * (await labelSize(page))).toBeGreaterThanOrEqual(11 - 0.05);
    // Se esconden el subtítulo y la insignia de las tarjetas.
    const hidden = await page
      .locator('[data-kx-part="diagram"] [data-kx-part="card"] [data-kx-part="subtitle"], [data-kx-part="diagram"] [data-kx-part="card"] [data-kx-part="badge"]')
      .evaluateAll((items) => items.every((item) => getComputedStyle(item).display === 'none'));
    expect(hidden).toBe(true);
    // El contador sigue a la vista.
    await visible(page.locator('[data-kx-part="diagram"] [data-kx-part="fold"] [data-kx-part="count"]')).toBe(true);
  });

  it('cada nodo del diagrama es un grupo con descripción de rol y nombre (M-13)', async () => {
    const { page } = await open({ path: '/position?view=tree&layout=diagram', ready: READY });
    await page.locator('.react-flow__node').first().waitFor();
    const facts = await nodes(page).evaluateAll((items) =>
      items.map((item) => ({
        role: item.getAttribute('role'),
        roledescription: item.getAttribute('aria-roledescription'),
        label: item.getAttribute('aria-label') ?? '',
      })),
    );
    expect(facts.length).toBeGreaterThan(0);
    for (const fact of facts) {
      expect(fact.role).toBe('group');
      expect(fact.roledescription).toBe('nodo del organigrama');
      expect(fact.label).not.toBe('');
    }
    // El navegador los resuelve por rol y nombre.
    await visible(page.getByRole('group', { name: 'Director/a General', exact: true })).toBe(true);
    // Las aristas no se anuncian.
    const edges = await page.locator('.react-flow__edge').evaluateAll((items) => items.map((item) => item.getAttribute('aria-hidden')));
    expect(edges.every((value) => value === 'true')).toBe(true);
  });

  it('una hoja con un título largo no se recorta si cabe en dos líneas; sólo las tarjetas con hijos reservan sitio para el plegado (R3-4, M-7.3)', async () => {
    const { page } = await open({ path: '/position?view=tree&layout=diagram', ready: READY });
    await page.getByRole('button', { name: 'Desplegar todo' }).click();
    await poll(() => nodes(page).count()).toBeGreaterThan(12);
    // «Desplegar todo» abre lo cargado; las ramas de abajo se piden al abrirlas: se abren hasta llegar a las hojas.
    for (let round = 0; round < 5; round += 1) {
      const clicked = await page.evaluate(() => {
        const closed = [...document.querySelectorAll<HTMLElement>('[data-kx-part="fold"][aria-expanded="false"]')];
        for (const button of closed) button.click();
        return closed.length;
      });
      if (clicked === 0) break;
      // Las ramas se piden al abrirlas: se deja un respiro para que lleguen antes de buscar las que siguen cerradas.
      await page.waitForTimeout(800);
    }
    await poll(() => page.locator('[data-kx-part="card"]:not([data-kx-expandable])').count()).toBeGreaterThan(2);

    const facts = await page.evaluate(() => {
      const lines = (el: Element): number => {
        const range = document.createRange();
        range.selectNodeContents(el);
        return new Set([...range.getClientRects()].filter((rect) => rect.width > 0).map((rect) => Math.round(rect.top))).size;
      };
      const cards = [...document.querySelectorAll<HTMLElement>('.react-flow__node [data-kx-part="card"]')];
      const clipped: string[] = [];
      for (const card of cards) {
        for (const part of card.querySelectorAll<HTMLElement>('[data-kx-part="label"], [data-kx-part="subtitle"]')) {
          // Recortado = el contenido pasa de la caja (la altura de dos líneas) o hay más de dos líneas.
          if (part.scrollHeight > part.clientHeight + 1 || lines(part) > 2) clipped.push(`${card.textContent}: ${part.textContent}`);
        }
        const main = card.querySelector<HTMLElement>('[data-kx-part="main"]')!;
        if (main.scrollHeight > main.clientHeight + 1) clipped.push(`${card.textContent}: la tarjeta se desborda`);
      }
      const leaf = cards.filter((card) => !card.hasAttribute('data-kx-expandable'));
      const parent = cards.filter((card) => card.hasAttribute('data-kx-expandable'));
      const reserve = (card: HTMLElement) => Number.parseFloat(getComputedStyle(card.querySelector('[data-kx-part="main"]')!).paddingInlineEnd);
      const label = leaf
        .map((card) => card.querySelector<HTMLElement>('[data-kx-part="label"]')!)
        .sort((a, b) => (b.textContent?.length ?? 0) - (a.textContent?.length ?? 0))[0]!;
      return {
        clipped,
        leaves: leaf.length,
        parents: parent.length,
        leafReserve: Math.max(...leaf.map(reserve)),
        parentReserve: Math.min(...parent.map(reserve)),
        longest: { text: label.textContent, lines: lines(label), title: label.getAttribute('title') },
        space8: Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--th-space-8')),
      };
    });
    expect(facts.clipped).toEqual([]);
    expect(facts.leaves).toBeGreaterThan(2);
    expect(facts.parents).toBeGreaterThan(3);
    // La hoja usa todo el ancho; la tarjeta con hijos reserva dos módulos del tema para el plegado y el contador.
    expect(facts.leafReserve).toBeLessThan(facts.space8);
    expect(facts.parentReserve).toBeGreaterThanOrEqual(facts.space8 * 2);
    // El título más largo de una hoja ('Técnico/a de Mantenimiento') cabe en dos líneas y lleva su `title`.
    expect(facts.longest.lines).toBeLessThanOrEqual(2);
    expect(facts.longest.title).toBe(facts.longest.text);

    // Un título que necesita dos líneas se ve entero, sin elipsis; uno que no cabe ni en dos se recorta con su `title`.
    const probe = await page.evaluate(() => {
      const card = [...document.querySelectorAll<HTMLElement>('.react-flow__node [data-kx-part="card"]')].find((c) => !c.hasAttribute('data-kx-expandable'))!;
      const label = card.querySelector<HTMLElement>('[data-kx-part="label"]')!;
      const bdi = label.querySelector('bdi')!;
      const lines = () => {
        const range = document.createRange();
        range.selectNodeContents(label);
        return new Set([...range.getClientRects()].filter((rect) => rect.width > 0).map((rect) => Math.round(rect.top))).size;
      };
      bdi.textContent = 'Responsable de Cuentas Regionales de la Zona Norte';
      const two = { lines: lines(), clipped: label.scrollHeight > label.clientHeight + 1, height: label.clientHeight };
      bdi.textContent = 'Responsable de Cuentas Regionales de la Zona Norte y del Sur de la Península Ibérica y las Islas Baleares';
      const long = { height: label.clientHeight, ellipsis: getComputedStyle(label).textOverflow };
      return { two, long };
    });
    expect(probe.two).toMatchObject({ lines: 2, clipped: false });
    // La caja es de dos líneas: lo que sobra se corta con elipsis y la tarjeta no crece.
    expect(probe.long.height).toBeLessThanOrEqual(probe.two.height + 1);
    expect(probe.long.ellipsis).toBe('ellipsis');
  });

  it('en la lista a 1440 px el contador va justo detrás de su etiqueta, no a la otra punta de la fila (R3-4, M-7.4)', async () => {
    const { page } = await open({ path: '/position?view=tree&layout=list', ready: READY, viewport: { width: 1440, height: 900 } });
    await visible(item(page, 'Director/a General')).toBe(true);
    const facts = await page.evaluate(() => {
      const space8 = Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--th-space-8'));
      const rows = [...document.querySelectorAll<HTMLElement>('[role="treeitem"]')].filter((row) => row.querySelector('[data-kx-part="count"]'));
      return {
        space8,
        rows: rows.map((row) => {
          const count = row.querySelector('[data-kx-part="count"]')!.getBoundingClientRect();
          // Lo que va justo antes del contador (la etiqueta con su subtítulo y su insignia).
          const before = row.querySelector('[data-kx-part="count"]')!.previousElementSibling!.getBoundingClientRect();
          const box = row.getBoundingClientRect();
          return { text: row.querySelector('[data-kx-part="label"]')?.textContent, gap: count.left - before.right, toEnd: box.right - count.right, width: box.width };
        }),
      };
    });
    expect(facts.space8).toBeGreaterThan(0);
    expect(facts.rows.length).toBeGreaterThan(0);
    for (const row of facts.rows) {
      expect(row.gap, `${row.text}: contador a ${row.gap}px de su etiqueta`).toBeGreaterThanOrEqual(0);
      expect(row.gap, `${row.text}: contador a ${row.gap}px de su etiqueta`).toBeLessThan(facts.space8);
      // Y no pegado al borde de la fila: la fila ocupa todo el ancho y el contador queda a la izquierda.
      expect(row.toEnd).toBeGreaterThan(row.width / 2);
    }
  });
});
