// Detalle (PLAN §4.10): abrir en edición, guardar con `version`, errores de campo y de negocio,
// conflicto 409 y alta.
import type { Page } from 'playwright-core';
import { expect, it } from 'vitest';
import { attr, count, focused, poll, text, value, visible } from './lib/esperar';
import { e2eSuite, mobileOptions } from './lib/suite';

const facilityRow = (page: Page, name: string) => page.locator('[role="row"][data-th="record"]', { hasText: name });
const sheetOf = (page: Page) => page.locator('section[data-th="sheet"]');
const nameField = (page: Page) => sheetOf(page).getByRole('textbox', { name: 'Nombre' });

/** Abre el detalle de una instalación con un doble clic real sobre su nombre. */
async function openFacility(page: Page, name: string): Promise<string> {
  await facilityRow(page, name).locator('[data-kx-col="name"]').dblclick();
  await page.waitForURL(/\/facility\/[\w-]+\?mode=edit/);
  await attr(sheetOf(page), 'data-kx-mode').toBe('edit');
  await focused(nameField(page)).toBe(true);
  return new URL(page.url()).pathname.split('/')[2] ?? '';
}

e2eSuite('detalle', ({ open }) => {
  it('guarda con la versión, escribe la respuesta en las cachés y pasa a lectura sin releer', async () => {
    const { page, writes } = await open({ path: '/facility' });
    const reads: string[] = [];
    const id = await openFacility(page, 'Recepción (Sede Central)');
    page.on('request', (request) => {
      if (request.method() === 'GET' && request.url().includes(`/facilities/${id}`)) reads.push(request.url());
    });

    await page.keyboard.press('Control+A');
    await page.keyboard.type('Recepción renovada');
    await page.keyboard.press('Control+Enter');

    await attr(sheetOf(page), 'data-kx-mode').toBe('read');
    await text(sheetOf(page).getByRole('heading', { level: 2 })).toBe('Recepción renovada');
    // Sólo viaja lo que cambió, con la versión leída.
    expect(writes).toHaveLength(1);
    expect(writes[0]?.method).toMatch(/PUT|PATCH/);
    expect(writes[0]?.url).toBe(`/api/v1/facilities/${id}`);
    expect(writes[0]?.body).toEqual({ name: 'Recepción renovada', version: 1 });
    // La lista (que sigue a la vista) refleja el cambio sin otra lectura del registro.
    await text(facilityRow(page, 'Recepción renovada').locator('[data-kx-col="name"]')).toBe('Recepción renovada');
    expect(reads).toEqual([]);
  });

  it('un campo obligatorio vacío marca el campo, lleva el foco y no envía nada', async () => {
    const { page, writes } = await open({ path: '/facility' });
    await openFacility(page, 'Sala Atlas (Sede Central)');
    await page.keyboard.press('Control+A');
    await page.keyboard.press('Delete');
    await page.keyboard.press('Control+Enter');

    const field = nameField(page);
    await attr(field, 'aria-invalid').toBe('true');
    const errorId = await field.getAttribute('aria-describedby');
    expect(errorId).toContain('-error-');
    await text(page.locator(`[id="${errorId}"]`)).not.toBe('');
    await focused(field).toBe(true);
    expect(writes).toEqual([]);
    // El resumen avisa al lector de pantalla; el error se quita al salir del campo ya corregido
    // (observación en docs/notas/T8.md: con el campo aún enfocado el error sigue mientras se escribe).
    await visible(sheetOf(page).getByText(/Revisa los campos marcados/)).toBe(true);
    await page.keyboard.type('Sala Atlas II');
    await page.keyboard.press('Tab');
    await attr(field, 'aria-invalid').not.toBe('true');
    await focused(field).toBe(false);
    await field.focus();
    await page.keyboard.press('Control+Enter');
    await attr(sheetOf(page), 'data-kx-mode').toBe('read');
    expect(writes).toHaveLength(1);
  });

  it('un error de negocio del servidor (422) sale en la ficha y no pierde lo escrito', async () => {
    const { page, go, writes } = await open({ path: '/facility' });
    const id = await openFacility(page, 'Sala Becario (Sede Central)');
    await go(`/facility/${id}?mode=edit&fallo=422`, false);
    await focused(nameField(page)).toBe(true);
    await page.keyboard.press('Control+A');
    await page.keyboard.type('Sala Becario II');
    await page.keyboard.press('Control+Enter');

    await visible(sheetOf(page).getByRole('alert').filter({ hasText: 'forced business rule violation' })).toBe(true);
    await attr(sheetOf(page), 'data-kx-mode').toBe('edit');
    await value(nameField(page)).toBe('Sala Becario II');
    expect(writes).toHaveLength(1);
  });

  it('un 409 abre el diálogo de conflicto con las diferencias y «Reaplicar mis cambios» deja guardar', async () => {
    const { page, go, writes } = await open({ path: '/facility' });
    const id = await openFacility(page, 'Sala Creativa (Estudio Sabadell)');
    await go(`/facility/${id}?mode=edit&fallo=409`, false);
    await page.keyboard.press('Control+A');
    await page.keyboard.type('Sala Creativa II');
    await page.keyboard.press('Control+Enter');

    const dialog = page.getByRole('dialog', { name: 'El registro ha cambiado' });
    await visible(dialog).toBe(true);
    const differences = dialog.getByRole('table', { name: 'Diferencias entre tus cambios y el servidor' });
    await text(differences).toContain('Sala Creativa II');
    await text(differences).toContain('Sala Creativa (Estudio Sabadell)');
    await visible(dialog.getByRole('button', { name: 'Recargar' })).toBe(true);

    // Se quita el fallo forzado y se reaplica: el formulario conserva lo mío y ahora guarda.
    await go(`/facility/${id}?mode=edit`, false);
    await dialog.getByRole('button', { name: 'Reaplicar mis cambios' }).click();
    await count(page.getByRole('dialog')).toBe(0);
    await value(nameField(page)).toBe('Sala Creativa II');
    await page.keyboard.press('Control+Enter');
    await attr(sheetOf(page), 'data-kx-mode').toBe('read');
    expect(writes.at(-1)?.body).toMatchObject({ name: 'Sala Creativa II', version: expect.any(Number) });
  });

  it('Esc con cambios pregunta antes de descartar; sin cambios cierra el detalle', async () => {
    const { page } = await open({ path: '/facility' });
    await openFacility(page, 'Recepción (Sede Central)');
    await page.keyboard.press('Control+A');
    await page.keyboard.type('Cambio a medias');
    await page.keyboard.press('Escape');
    const dialog = page.getByRole('dialog', { name: 'Cambios sin guardar' });
    await visible(dialog).toBe(true);
    await dialog.getByRole('button', { name: 'Seguir editando' }).click();
    await count(page.getByRole('dialog')).toBe(0);
    await focused(nameField(page)).toBe(true);
    await value(nameField(page)).toBe('Cambio a medias');

    await page.keyboard.press('Escape');
    await visible(page.getByRole('dialog', { name: 'Cambios sin guardar' })).toBe(true);
    await page.getByRole('button', { name: 'Descartar los cambios' }).click();
    // Cancelar vuelve a lectura con el valor del servidor.
    await attr(sheetOf(page), 'data-kx-mode').toBe('read');
    await text(sheetOf(page).getByRole('heading', { level: 2 })).toBe('Recepción (Sede Central)');
  });

  it('Alt+↓ y Alt+↑ pasan al registro siguiente y al anterior de la lista', async () => {
    const { page } = await open({ path: '/facility' });
    const first = await openFacility(page, 'Archivo (Sede Central)');
    await page.keyboard.press('Alt+ArrowDown');
    await poll(async () => new URL(page.url()).pathname.split('/')[2]).not.toBe(first);
    await text(sheetOf(page).getByRole('heading', { level: 2 })).toBe('Cámara Fría (Plataforma El)');
    await page.keyboard.press('Alt+ArrowUp');
    await text(sheetOf(page).getByRole('heading', { level: 2 })).toBe('Archivo (Sede Central)');
  });

  it('el alta valida lo obligatorio, avisa de un duplicado y navega al registro creado', async () => {
    const { page, go, writes } = await open({ path: '/product' });
    await go('/product/nuevo', false);
    const sheet = sheetOf(page);
    await visible(sheet.getByRole('heading', { level: 2, name: 'Nuevo producto' })).toBe(true);
    await focused(sheet.getByRole('textbox', { name: 'Referencia' })).toBe(true);

    await sheet.getByRole('button', { name: 'Crear' }).click();
    await attr(sheet.getByRole('textbox', { name: 'Referencia' }), 'aria-invalid').toBe('true');
    await attr(sheet.getByRole('textbox', { name: 'Nombre' }), 'aria-invalid').toBe('true');
    expect(writes).toEqual([]);

    await sheet.getByRole('textbox', { name: 'Referencia' }).fill('SKU-0001');
    await sheet.getByRole('textbox', { name: 'Nombre' }).fill('Duplicado');
    await sheet.getByRole('combobox', { name: 'Naturaleza' }).selectOption({ label: 'Bien' });
    await sheet.getByRole('textbox', { name: 'Unidad' }).fill('ea');
    await sheet.getByRole('button', { name: 'Crear' }).click();
    await visible(sheet.getByRole('alert')).toBe(true);
    expect(page.url()).toContain('/product/nuevo');

    await sheet.getByRole('textbox', { name: 'Referencia' }).fill('SKU-E2E-1');
    await sheet.getByRole('button', { name: 'Crear' }).click();
    await page.waitForURL(/\/product\/[\w-]+$/);
    await text(sheetOf(page).getByRole('heading', { level: 2 })).toBe('Duplicado');
    expect(writes.filter((write) => write.method === 'POST')).toHaveLength(2);
  });
  it('en edición el pie de acciones queda fijo abajo del panel y Guardar se ve sin desplazarse', async () => {
    const { page } = await open({ path: '/facility' });
    await openFacility(page, 'Recepción (Sede Central)');
    const footer = sheetOf(page).locator('[data-kx-part="sheet-footer"]');
    await visible(footer.getByRole('button', { name: /^Guardar/ })).toBe(true);
    await visible(footer.getByRole('button', { name: 'Cancelar' })).toBe(true);
    const box = await page.evaluate(() => {
      const foot = document.querySelector('[data-kx-part="sheet-footer"]') as HTMLElement;
      const pane = document.querySelector('[data-kx-part="detail"]') as HTMLElement;
      const footBox = foot.getBoundingClientRect();
      const paneBox = pane.getBoundingClientRect();
      return {
        position: getComputedStyle(foot).position,
        footBottom: footBox.bottom,
        paneBottom: paneBox.bottom,
        windowHeight: window.innerHeight,
        docScroll: document.documentElement.scrollHeight,
      };
    });
    expect(box.position).toBe('sticky');
    // Abajo del panel (formulario corto: sin espacio muerto debajo), a lo sumo a un relleno del borde, y dentro de la ventana.
    expect(box.paneBottom - box.footBottom).toBeGreaterThanOrEqual(-1);
    expect(box.paneBottom - box.footBottom).toBeLessThanOrEqual(24);
    expect(box.footBottom).toBeLessThanOrEqual(box.windowHeight);
    // La página no se desplaza: lo hacen los paneles.
    expect(box.docScroll).toBeLessThanOrEqual(box.windowHeight);
  });

  it('las pistas de la ficha en edición están enlazadas con su campo por aria-describedby', async () => {
    const { page } = await open({ path: '/facility' });
    await openFacility(page, 'Recepción (Sede Central)');
    const linked = await page.evaluate(() => {
      const hints = [...document.querySelectorAll<HTMLElement>('section[data-th="sheet"] [data-th="field"] > [data-th="hint"]')];
      return hints.map((hint) => {
        const field = hint.closest('[data-th="field"]') as HTMLElement;
        const control = field.querySelector<HTMLElement>('input, select, textarea, button[role="combobox"]');
        return { text: hint.textContent, id: hint.id, describedBy: control?.getAttribute('aria-describedby') ?? '' };
      });
    });
    expect(linked.length).toBeGreaterThan(0);
    for (const hint of linked) {
      expect(hint.text, 'la pista tiene texto').toBeTruthy();
      expect(hint.id, 'la pista tiene id').toBeTruthy();
      expect(hint.describedBy.split(' '), hint.text ?? '').toContain(hint.id);
    }
  });

  it('con el panel a su ancho por omisión la ficha usa 2 columnas, y en un móvil, una', async () => {
    const { page } = await open({ path: '/facility' });
    await openFacility(page, 'Recepción (Sede Central)');
    const measure = () =>
      page.evaluate(() => {
        const sheet = document.querySelector('section[data-th="sheet"]') as HTMLElement;
        const em = Number.parseFloat(getComputedStyle(sheet).fontSize);
        const cells = [...sheet.querySelectorAll<HTMLElement>('[data-kx-part="fields"] > [data-kx-part="cell"]')];
        const tops = cells.slice(0, 2).map((cell) => Math.round(cell.getBoundingClientRect().top));
        return { sheetEm: sheet.getBoundingClientRect().width / em, tops, cells: cells.length };
      });
    const wide = await measure();
    // El panel de detalle ocupa el 40 % de lo que queda junto al menú: entre 28em y 44em de ficha.
    expect(wide.sheetEm).toBeGreaterThan(28);
    expect(wide.sheetEm).toBeLessThan(44);
    expect(wide.cells).toBeGreaterThanOrEqual(2);
    expect(wide.tops[0], 'dos columnas: los dos primeros campos van en la misma fila').toBe(wide.tops[1]);
  });

  it('en una ventana estrecha (≤ 64em) el detalle ocupa la página entera y «Volver a la lista» devuelve la lista', async () => {
    const { page } = await open({ path: '/facility', viewport: { width: 800, height: 800 } });
    await facilityRow(page, 'Recepción (Sede Central)').locator('[data-kx-col="name"]').dblclick();
    await page.waitForURL(/\/facility\/[\w-]+\?mode=edit/);
    await attr(sheetOf(page), 'data-kx-mode').toBe('edit');
    await attr(page.locator('[data-kx-screen="entity"]'), 'data-kx-layout').toBe('page');
    await count(page.getByRole('grid')).toBe(0);
    const width = await sheetOf(page).evaluate((el) => el.getBoundingClientRect().width);
    expect(width).toBeGreaterThan(700);
    // 800 px son menos de 64em con el tamaño de letra de la demo (14 px por em).
    // Sin sitio para dos paneles no se ofrece pasar de panel a página.
    await count(page.getByRole('button', { name: /Abrir a pantalla completa|Volver al panel/ })).toBe(0);
    await focused(nameField(page)).toBe(true);
    await page.getByRole('button', { name: 'Volver a la lista' }).click();
    await count(page.getByRole('grid')).toBe(1);
    await count(sheetOf(page)).toBe(0);
  });

  it('a 390 px la ficha va a página completa, en una sola columna, con el pie de acciones a la vista', async () => {
    const { page } = await open({ ...mobileOptions(), path: '/facility' });
    await facilityRow(page, 'Recepción (Sede Central)').locator('[data-kx-col="name"]').dblclick();
    await page.waitForURL(/\/facility\/[\w-]+\?mode=edit/);
    await attr(sheetOf(page), 'data-kx-mode').toBe('edit');
    const facts = await page.evaluate(() => {
      const sheet = document.querySelector('section[data-th="sheet"]') as HTMLElement;
      const cells = [...sheet.querySelectorAll<HTMLElement>('[data-kx-part="fields"] > [data-kx-part="cell"]')];
      const tops = cells.slice(0, 2).map((cell) => Math.round(cell.getBoundingClientRect().top));
      const footer = document.querySelector('[data-kx-part="sheet-footer"]') as HTMLElement;
      return {
        scrollWidth: document.documentElement.scrollWidth,
        innerWidth: window.innerWidth,
        tops,
        footerVisible: footer.getBoundingClientRect().bottom <= window.innerHeight,
        sheetWidth: sheet.getBoundingClientRect().width,
      };
    });
    expect(facts.scrollWidth).toBeLessThanOrEqual(facts.innerWidth);
    expect(facts.tops[0], 'una columna: los campos van uno bajo otro').toBeLessThan(facts.tops[1] ?? 0);
    expect(facts.footerVisible).toBe(true);
    expect(facts.sheetWidth).toBeGreaterThan(300);
  });

  it('con dir=rtl el detalle sigue funcionando: doble clic abre la edición con el foco en el primer campo', async () => {
    const { page } = await open({ path: '/facility', lng: 'en', dir: 'rtl' });
    await facilityRow(page, 'Recepción (Sede Central)').locator('[data-kx-col="name"]').dblclick();
    await page.waitForURL(/\/facility\/[\w-]+\?mode=edit/);
    await attr(sheetOf(page), 'data-kx-mode').toBe('edit');
    await focused(sheetOf(page).getByRole('textbox', { name: 'Name' })).toBe(true);
  });

  it('con el detalle abierto a 1440 px la lista ocupa el 60 % y el detalle el 40 % (D-R34)', async () => {
    const { page } = await open({ path: '/facility' });
    await openFacility(page, 'Sala Atlas (Sede Central)');
    const share = () =>
      page.evaluate(() => {
        const list = (document.querySelector('[data-kx-part="list"]') as HTMLElement).getBoundingClientRect().width;
        const detail = (document.querySelector('[data-kx-part="detail"]') as HTMLElement).getBoundingClientRect().width;
        return list / (list + detail);
      });
    await poll(async () => (await share()) > 0.58 && (await share()) < 0.62).toBe(true);
  });

  it('en la ficha de una instalación el enlace de relación abre la ficha de destino', async () => {
    const { page } = await open({ path: '/facility' });
    const id = await openFacility(page, 'Sala Atlas (Sede Central)');
    const link = sheetOf(page).getByRole('link', { name: /^Abrir Instalación:/ });
    await visible(link).toBe(true);
    const label = (await link.getAttribute('aria-label')) ?? '';
    const parent = label.replace(/^Abrir Instalación: /, '');
    expect(parent).not.toBe('');
    await link.click();
    await poll(async () => new URL(page.url()).pathname.split('/')[2]).not.toBe(id);
    await text(sheetOf(page).getByRole('heading', { level: 2 })).toBe(parent);
    await attr(sheetOf(page), 'data-kx-mode').toBe('read');
    // Las tarjetas acompañan a cualquier ficha: identificador y versión del registro abierto.
    await visible(sheetOf(page).getByRole('heading', { level: 3, name: 'Registro' })).toBe(true);
  });

  it('los enlaces de relación son enlaces de verdad, con su dirección (se pueden abrir en otra pestaña)', async () => {
    const { page } = await open({ path: '/facility' });
    await openFacility(page, 'Sala Atlas (Sede Central)');
    const link = sheetOf(page).getByRole('link', { name: /^Abrir Instalación:/ });
    await visible(link).toBe(true);
    expect(await link.getAttribute('href')).toMatch(/\/facility\/[\w-]+$/);
    await visible(sheetOf(page).getByRole('link', { name: /en el árbol/ })).toBe(true);
    expect(await sheetOf(page).getByRole('link', { name: /en el árbol/ }).getAttribute('href')).toMatch(/view=tree/);
  });

  it('en la escena de la captura «detalle en edición» el hueco entre el último bloque y el pie es menor de 80 px', async () => {
    const { page } = await open({ path: '/facility' });
    await openFacility(page, 'Sala Atlas (Sede Central)');
    const sheet = sheetOf(page);
    await sheet.locator('input[type="checkbox"][id$="-f-active"]').uncheck();
    await nameField(page).fill('');
    await page.keyboard.press('Control+Enter');
    await attr(sheet.locator('[aria-invalid="true"]').first(), 'aria-invalid').toBe('true');
    await visible(sheet.getByRole('heading', { level: 3, name: 'Relaciones' })).toBe(true);
    const gap = await page.evaluate(() => {
      const body = document.querySelector('section[data-th="sheet"] > [data-kx-part="body"]') as HTMLElement;
      const last = body.lastElementChild as HTMLElement;
      const footer = document.querySelector('[data-kx-part="sheet-footer"]') as HTMLElement;
      return Math.round(footer.getBoundingClientRect().top - last.getBoundingClientRect().bottom);
    });
    expect(gap, `hueco de ${gap} px`).toBeLessThan(80);
  });
});
