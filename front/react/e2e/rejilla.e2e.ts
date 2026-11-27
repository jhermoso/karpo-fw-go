// Cuadrícula editable (PLAN §4.9): doble clic → detalle en edición, edición en línea con validación,
// deshacer y rehacer, guardado por lotes, conflicto 409 forzado y teclado.
import type { Page } from 'playwright-core';
import { expect, it } from 'vitest';
import { attr, count, enabled, focused, poll, text, visible } from './lib/esperar';
import { e2eSuite } from './lib/suite';

/** ¿El elemento con el foco está dentro de la lista (role=grid)? */
const insideGrid = (page: Page) => page.evaluate(() => Boolean(document.activeElement?.closest('[role="grid"]')));

/** Fila de datos por su referencia (SKU) y celda por columna. */
const row = (page: Page, sku: string) => page.locator('[role="row"][data-th="record"]', { hasText: sku });
const cell = (page: Page, sku: string, column: string) => row(page, sku).locator(`[data-kx-col="${column}"]`);
const activeColumn = (page: Page) => attr(page.locator('[role="gridcell"][data-kx-active="true"]'), 'data-kx-col');

e2eSuite('rejilla', ({ open }) => {
  it('doble clic en una fila abre el detalle en edición con el foco en el primer campo editable', async () => {
    const { page } = await open({ path: '/product' });
    await cell(page, 'SKU-0003', 'name').dblclick();

    await page.waitForURL(/\/product\/[\w-]+\?mode=edit/);
    const sheet = page.locator('section[data-th="sheet"]');
    await attr(sheet, 'data-kx-mode').toBe('edit');
    // La lista no se desmonta: sigue a la vista junto al panel de detalle.
    await count(page.getByRole('grid')).toBe(1);
    // La referencia no cambia tras el alta; el primer campo editable es el nombre.
    await poll(() => page.evaluate(() => document.activeElement?.id ?? '')).toMatch(/-f-name$/);
    const focused = await page.evaluate(() => ({
      tag: document.activeElement?.tagName,
      inSheet: Boolean(document.activeElement?.closest('section[data-th="sheet"]')),
      value: (document.activeElement as HTMLInputElement | null)?.value,
    }));
    expect(focused).toEqual({ tag: 'INPUT', inSheet: true, value: 'Rodamiento galvanizado' });
  });

  it('también con Ctrl+Intro desde la celda activa', async () => {
    const { page } = await open({ path: '/product' });
    await cell(page, 'SKU-0002', 'name').click();
    await page.keyboard.press('Control+Enter');
    await page.waitForURL(/\/product\/[\w-]+\?mode=edit/);
  });

  it('edita celdas en línea, deshace y rehace', async () => {
    const { page } = await open({ path: '/product' });

    // F2 abre el editor sobre la celda activa; Intro confirma.
    await cell(page, 'SKU-0001', 'name').click();
    await page.keyboard.press('F2');
    await page.keyboard.press('Control+A');
    await page.keyboard.type('Correa M10 revisada');
    await page.keyboard.press('Enter');
    await attr(cell(page, 'SKU-0001', 'name'), 'data-kx-cell').toBe('dirty');
    await attr(row(page, 'SKU-0001'), 'data-th-state').toMatch(/dirty/);
    await visible(page.getByText('1 cambio')).toBe(true);

    // Escribir sobre una celda la sobrescribe (sin pasar por F2).
    await cell(page, 'SKU-0002', 'uom').click();
    await page.keyboard.type('kg');
    await page.keyboard.press('Enter');
    await attr(cell(page, 'SKU-0002', 'uom'), 'data-kx-cell').toBe('dirty');
    await visible(page.getByText('2 cambios')).toBe(true);

    // Deshacer (Ctrl+Z) paso a paso y rehacer (Ctrl+Mayús+Z).
    await page.keyboard.press('Control+z');
    await attr(cell(page, 'SKU-0002', 'uom'), 'data-kx-cell').not.toBe('dirty');
    await visible(page.getByText('1 cambio')).toBe(true);
    await page.keyboard.press('Control+Shift+z');
    await attr(cell(page, 'SKU-0002', 'uom'), 'data-kx-cell').toBe('dirty');
    await visible(page.getByText('2 cambios')).toBe(true);
    // Los botones de la barra hacen lo mismo.
    await page.getByRole('button', { name: 'Deshacer' }).click();
    await page.getByRole('button', { name: 'Deshacer' }).click();
    await count(page.getByText(/cambio/)).toBe(0);
    await text(cell(page, 'SKU-0001', 'name')).toContain('Correa M10');
    await text(cell(page, 'SKU-0001', 'name')).not.toContain('revisada');
    await enabled(page.getByRole('button', { name: 'Rehacer' })).toBe(true);
  });

  it('valida al confirmar: un importe que no es número deja la celda inválida y Esc la revierte', async () => {
    const { page } = await open({ path: '/product' });
    await cell(page, 'SKU-0003', 'basePrice').click();
    await page.keyboard.press('F2');
    await page.keyboard.press('Control+A');
    await page.keyboard.type('abc');
    await page.keyboard.press('Enter');
    const price = cell(page, 'SKU-0003', 'basePrice');
    await attr(price, 'data-kx-cell').toBe('invalid');
    await attr(price, 'aria-invalid').toBe('true');
    // El error está enlazado a la celda y tiene texto traducido.
    const issueId = await price.getAttribute('aria-describedby');
    expect(issueId).toBeTruthy();
    await visible(page.locator(`[id="${issueId}"]`)).toBe(true);
    await text(page.locator(`[id="${issueId}"]`)).not.toBe('');
    // Un valor inválido no avanza: la celda sigue activa. Esc vuelve al valor del servidor.
    await activeColumn(page).toBe('basePrice');
    await page.keyboard.press('Escape');
    await attr(price, 'data-kx-cell').not.toBe('invalid');
    await text(price).toContain('66,94');
  });

  it('guarda por lotes con la versión de cada fila y deja la rejilla limpia', async () => {
    const { page, writes } = await open({ path: '/product' });
    await cell(page, 'SKU-0001', 'name').click();
    await page.keyboard.press('F2');
    await page.keyboard.press('Control+A');
    await page.keyboard.type('Correa M10 lote');
    await page.keyboard.press('Enter');
    await cell(page, 'SKU-0004', 'uom').click();
    await page.keyboard.type('min');
    await page.keyboard.press('Enter');
    await visible(page.getByText('2 cambios')).toBe(true);

    // En modo por lotes nada se envía hasta pulsar Guardar.
    expect(writes).toEqual([]);
    await page.keyboard.press('Control+s');

    await count(page.getByText(/cambio/)).toBe(0);
    await text(cell(page, 'SKU-0001', 'name')).toContain('Correa M10 lote');
    await attr(cell(page, 'SKU-0001', 'name'), 'data-kx-cell').not.toBe('dirty');
    expect(writes).toHaveLength(2);
    for (const write of writes) {
      expect(['PUT', 'PATCH']).toContain(write.method);
      expect(write.url).toMatch(/^\/api\/v1\/products\/[\w-]+$/);
      expect(write.body).toMatchObject({ version: expect.any(Number) });
    }
    const bodies = writes.map((write) => write.body as Record<string, unknown>);
    expect(bodies.find((body) => body.name === 'Correa M10 lote')).toBeDefined();
    expect(bodies.find((body) => body.uom === 'min')).toBeDefined();
  });

  it('un 409 forzado deja la fila en conflicto y «Recargar y reaplicar» recupera el guardado', async () => {
    const { page, go, writes } = await open({ path: '/product?fallo=409' });
    await cell(page, 'SKU-0005', 'name').click();
    await page.keyboard.press('F2');
    await page.keyboard.press('Control+A');
    await page.keyboard.type('Rodamiento en conflicto');
    await page.keyboard.press('Enter');
    await page.getByRole('button', { name: /^Guardar/ }).click();

    await attr(row(page, 'SKU-0005'), 'data-kx-state').toBe('conflict');
    expect(writes.length).toBeGreaterThan(0);
    const reapply = page.getByRole('button', { name: 'Recargar y reaplicar' });
    await visible(reapply).toBe(true);

    // Se quita el fallo forzado (la demo lo lee de la URL de la página) y se reaplica.
    await go('/product', false);
    await reapply.click();
    await attr(row(page, 'SKU-0005'), 'data-kx-state').not.toBe('conflict');
    await page.getByRole('button', { name: /^Guardar/ }).click().catch(() => undefined);
    await count(page.getByText(/cambio/)).toBe(0);
    await text(cell(page, 'SKU-0005', 'name')).toContain('Rodamiento en conflicto');
  });

  it('se maneja entera con el teclado', async () => {
    const { page } = await open({ path: '/product' });
    await cell(page, 'SKU-0001', 'sku').click();
    await activeColumn(page).toBe('sku');

    await page.keyboard.press('ArrowRight');
    await activeColumn(page).toBe('name');
    await page.keyboard.press('ArrowDown');
    await attr(cell(page, 'SKU-0002', 'name'), 'data-kx-active').toBe('true');
    await page.keyboard.press('ArrowUp');
    await attr(cell(page, 'SKU-0001', 'name'), 'data-kx-active').toBe('true');

    // Inicio y Fin van a los extremos de la fila (casilla de selección y acciones de la fila).
    await page.keyboard.press('End');
    await activeColumn(page).toBe('__actions');
    await page.keyboard.press('Home');
    await activeColumn(page).toBe('__select');
    await page.keyboard.press('ArrowRight');
    await page.keyboard.press('ArrowRight');
    await activeColumn(page).toBe('name');
    await page.keyboard.press('ArrowLeft');

    // Ctrl+Fin va a la última fila cargada; Ctrl+Inicio vuelve a la primera.
    await page.keyboard.press('Control+End');
    await visible(page.locator('[role="gridcell"][data-kx-active="true"]')).toBe(true);
    const lastRow = await page.locator('[role="gridcell"][data-kx-active="true"]').evaluate((el) => el.closest('[role="row"]')?.getAttribute('aria-rowindex'));
    expect(Number(lastRow)).toBeGreaterThan(20);
    await page.keyboard.press('Control+Home');
    await attr(cell(page, 'SKU-0001', '__select'), 'data-kx-active').toBe('true');

    // Espacio elige la fila; Mayús+flecha amplía el rango.
    await page.keyboard.press('Space');
    await attr(row(page, 'SKU-0001'), 'aria-selected').toBe('true');

    // Intro abre el editor del nombre y Esc lo cierra sin cambios.
    await page.keyboard.press('ArrowRight');
    await page.keyboard.press('ArrowRight');
    await activeColumn(page).toBe('name');
    await page.keyboard.press('Enter');
    await focused(page.locator('input[data-kx-ui="input"]')).toBe(true);
    await page.keyboard.press('Escape');
    await attr(cell(page, 'SKU-0001', 'name'), 'data-kx-cell').not.toBe('editing');
    await count(page.getByText(/cambio/)).toBe(0);

    // Tab ya no recorre las celdas (B-4): la lista es una sola parada y la celda activa no se mueve.
    await page.keyboard.press('Tab');
    await poll(() => insideGrid(page)).toBe(false);
    await page.keyboard.press('Shift+Tab');
    await poll(() => insideGrid(page)).toBe(true);
    await activeColumn(page).toBe('name');

    // Con el foco en la cabecera, Intro ordena la columna.
    await page.keyboard.press('ArrowUp');
    await focused(page.locator('[role="columnheader"][data-kx-col="name"]')).toBe(true);
    await page.keyboard.press('Enter');
    await attr(page.locator('[role="columnheader"][data-kx-col="name"]'), 'aria-sort').toBe('ascending');
    await attr(page.locator('[role="columnheader"][data-kx-col="sku"]'), 'aria-sort').not.toBe('ascending');
  });

  it('Tab saca el foco de la lista en tres pulsaciones como mucho y Mayús+Tab vuelve a la celda activa (B-4)', async () => {
    const { page } = await open({ path: '/product' });
    await cell(page, 'SKU-0003', 'name').click();
    await activeColumn(page).toBe('name');
    await poll(() => insideGrid(page)).toBe(true);

    // Antes el foco quedaba atrapado: 12 Tab seguían en la lista. Ahora basta una parada (con margen, tres).
    let pulses = 0;
    while (pulses < 3 && (await insideGrid(page))) {
      await page.keyboard.press('Tab');
      pulses += 1;
    }
    expect(await insideGrid(page), 'tras ≤ 3 Tab el foco ya no está en la lista').toBe(false);

    // Mayús+Tab vuelve a la celda que estaba activa, no a la primera de la lista.
    let back = 0;
    while (back < pulses && !(await insideGrid(page))) {
      await page.keyboard.press('Shift+Tab');
      back += 1;
    }
    expect(await insideGrid(page)).toBe(true);
    await attr(cell(page, 'SKU-0003', 'name'), 'data-kx-active').toBe('true');
    await focused(cell(page, 'SKU-0003', 'name')).toBe(true);
  });

  it('tras moverse con flechas, salir con Tab y volver con Mayús+Tab se está en la celda a la que se llegó', async () => {
    const { page } = await open({ path: '/product' });
    await cell(page, 'SKU-0001', 'sku').click();
    await page.keyboard.press('ArrowDown');
    await page.keyboard.press('ArrowRight');
    await page.keyboard.press('ArrowRight');
    await attr(cell(page, 'SKU-0002', 'kind'), 'data-kx-active').toBe('true');
    await page.keyboard.press('Tab');
    await poll(() => insideGrid(page)).toBe(false);
    await page.keyboard.press('Shift+Tab');
    await focused(cell(page, 'SKU-0002', 'kind')).toBe(true);
  });

  it('al editar, Tab confirma y deja el foco en la celda siguiente dentro de la lista; Mayús+Tab, en la anterior', async () => {
    const { page } = await open({ path: '/product' });
    await cell(page, 'SKU-0001', 'name').click();
    await page.keyboard.press('F2');
    await page.keyboard.press('Control+A');
    await page.keyboard.type('Correa por Tab');
    await page.keyboard.press('Tab');
    // Confirmó el valor (celda sucia), la celda activa es la siguiente y el foco sigue dentro de la lista.
    await attr(cell(page, 'SKU-0001', 'name'), 'data-kx-cell').toBe('dirty');
    await activeColumn(page).toBe('kind');
    await poll(() => insideGrid(page)).toBe(true);
    await focused(cell(page, 'SKU-0001', 'kind')).toBe(true);

    // Y hacia atrás: edita la unidad y Mayús+Tab pasa a la celda anterior de la fila.
    await cell(page, 'SKU-0002', 'uom').click();
    await page.keyboard.press('F2');
    await page.keyboard.press('Control+A');
    await page.keyboard.type('kg');
    await page.keyboard.press('Shift+Tab');
    await attr(cell(page, 'SKU-0002', 'uom'), 'data-kx-cell').toBe('dirty');
    await focused(cell(page, 'SKU-0002', 'kind')).toBe(true);
    await poll(() => insideGrid(page)).toBe(true);
  });

  it('la barra de cambios flota compacta sobre el borde inferior y no tapa la última fila al llegar al final', async () => {
    const { page } = await open({ path: '/product' });
    await cell(page, 'SKU-0001', 'name').click();
    await page.keyboard.press('F2');
    await page.keyboard.press('Control+A');
    await page.keyboard.type('Cambio para la barra');
    await page.keyboard.press('Enter');
    const bar = page.locator('[data-kx-ui="batch-bar"]');
    await visible(bar).toBe(true);
    const measures = await page.evaluate(() => {
      const barEl = document.querySelector('[data-kx-ui="batch-bar"]') as HTMLElement;
      const viewport = document.querySelector('[data-kx-part="viewport"]') as HTMLElement;
      const barBox = barEl.getBoundingClientRect();
      const list = viewport.getBoundingClientRect();
      return {
        barWidth: barBox.width,
        listWidth: list.width,
        barBottom: barBox.bottom,
        listBottom: list.bottom,
        barTop: barBox.top,
        paddingEnd: Number.parseFloat(getComputedStyle(viewport).paddingBlockEnd),
        barHeight: barBox.height,
        position: getComputedStyle(barEl).position,
        radius: getComputedStyle(barEl).borderTopLeftRadius,
        shadow: getComputedStyle(barEl).boxShadow,
      };
    });
    expect(measures.position).toBe('sticky');
    // Compacta: bastante más estrecha que la lista, no de lado a lado.
    expect(measures.barWidth).toBeLessThan(measures.listWidth * 0.6);
    // Flota sobre el borde inferior de la lista (dentro de ella) y la lista deja ese hueco como relleno.
    expect(measures.barBottom).toBeLessThanOrEqual(measures.listBottom + 1);
    expect(measures.paddingEnd).toBeGreaterThanOrEqual(measures.barHeight);
    expect(measures.radius).not.toBe('0px');
    expect(measures.shadow).not.toBe('none');
    // Deshacer, rehacer y Descartar son secundarios; sólo Guardar es primario.
    await attr(page.getByRole('button', { name: 'Deshacer' }), 'data-th-kind').toBe('secondary');
    await attr(page.getByRole('button', { name: 'Rehacer' }), 'data-th-kind').toBe('secondary');
    await attr(bar.getByRole('button', { name: 'Descartar' }), 'data-th-kind').toBe('secondary');
    await attr(bar.getByRole('button', { name: /^Guardar/ }), 'data-th-kind').toBe('primary');
    // La última fila, desplazada hasta el final, queda por encima de la barra.
    await page.locator('[data-kx-part="viewport"]').evaluate((el) => el.scrollTo({ top: el.scrollHeight }));
    await poll(async () =>
      page.evaluate(() => {
        const barBox = (document.querySelector('[data-kx-ui="batch-bar"]') as HTMLElement).getBoundingClientRect();
        const rows = [...document.querySelectorAll<HTMLElement>('[data-kx-part="body"] > [data-th="record"][data-kx-row]')];
        const last = rows.map((row) => row.getBoundingClientRect()).sort((a, b) => b.bottom - a.bottom)[0];
        return last ? last.bottom - barBox.top <= 1 : true;
      }),
    ).toBe(true);
  });

  it('la marca de celda sucia se espeja con dir=rtl', async () => {
    const { page } = await open({ path: '/product', lng: 'en', dir: 'rtl' });
    await cell(page, 'SKU-0001', 'name').click();
    await page.keyboard.press('F2');
    await page.keyboard.press('Control+A');
    await page.keyboard.type('RTL');
    await page.keyboard.press('Enter');
    await attr(cell(page, 'SKU-0001', 'name'), 'data-kx-cell').toBe('dirty');
    const rtl = await cell(page, 'SKU-0001', 'name').evaluate((el) => getComputedStyle(el, '::before').backgroundImage);
    expect(rtl).toContain('225deg');
  });

  it('la búsqueda rápida filtra desde el teclado (/) y el foco no se pierde', async () => {
    const { page } = await open({ path: '/product' });
    await cell(page, 'SKU-0001', 'sku').click();
    await page.keyboard.press('/');
    await focused(page.getByRole('searchbox', { name: 'Buscar', exact: true })).toBe(true);
    await page.keyboard.type('Consultoría');
    await visible(row(page, 'SKU-0004')).toBe(true);
    await count(row(page, 'SKU-0001')).toBe(0);
  });

  it('en modo optimista guarda al salir de la fila y revierte con aviso si el servidor falla', async () => {
    const { page, writes, go } = await open({ path: '/facility' });
    const facilityRow = (name: string) => page.locator('[role="row"][data-th="record"]', { hasText: name });
    const nameCell = (name: string) => facilityRow(name).locator('[data-kx-col="name"]');

    await nameCell('Archivo (Sede Central)').click();
    await page.keyboard.press('F2');
    await page.keyboard.press('Control+A');
    await page.keyboard.type('Archivo general');
    await page.keyboard.press('Enter');
    await page.keyboard.press('ArrowDown');
    await text(nameCell('Archivo general')).toContain('Archivo general');
    await poll(async () => writes.length).toBe(1);
    expect(writes[0]?.url).toMatch(/^\/api\/v1\/facilities\/[\w-]+$/);
    expect(writes[0]?.body).toMatchObject({ name: 'Archivo general', version: expect.any(Number) });
    await attr(facilityRow('Archivo general'), 'data-th-state').not.toMatch(/dirty/);

    // Con un fallo de servidor la fila vuelve al valor anterior y el aviso ofrece deshacer.
    await go('/facility?fallo=500', false);
    await nameCell('Recepción (Sede Central)').click();
    await page.keyboard.press('F2');
    await page.keyboard.press('Control+A');
    await page.keyboard.type('Recepción nueva');
    await page.keyboard.press('Enter');
    await page.keyboard.press('ArrowDown');
    await text(nameCell('Recepción (Sede Central)')).toContain('Recepción (Sede Central)');
    const toast = page.getByRole('alert').filter({ hasText: 'vuelve a su valor anterior' });
    await visible(toast).toBe(true);
    await visible(toast.getByRole('button', { name: 'Deshacer' })).toBe(true);
  });
});
