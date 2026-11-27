// Mapa por niveles (PLAN §4.12): mundo → país → región → ciudad con clics reales sobre el mapa, migas, Esc,
// URL, «Ver como tabla», niveles de un solo elemento saltados, rótulos medidos, leyenda que no tapa nada,
// tipografía del tema y origen propio de los geodatos.
import type { Locator, Page } from 'playwright-core';
import { expect, it } from 'vitest';
import { findColorCenter } from './lib/demo.mjs';
import { attr, count, poll, route, text, visible } from './lib/esperar';
import { e2eSuite } from './lib/suite';

const READY = 'section[data-kx-view="map"][data-kx-ready="true"]';
const crumbs = (page: Page) => page.getByRole('navigation', { name: 'Ruta del mapa' });
const up = (page: Page) => page.getByRole('button', { name: /^Subir un nivel/ });
const mapSection = (page: Page) => page.locator('section[data-kx-view="map"]');
const toTable = (page: Page) => page.getByRole('button', { name: 'Ver como tabla' });
const toMap = (page: Page) => page.getByRole('button', { name: 'Ver como mapa' });

/** Texto de las migas, de la raíz a la hoja, separado por « › ». */
const trail = (page: Page) =>
  crumbs(page)
    .locator('li')
    .evaluateAll((items) => items.map((item) => item.textContent?.trim() ?? '').join(' › '));

/** Color del paso más alto de la leyenda con datos: el de la delimitación con más instalaciones. */
const dataColor = (page: Page) =>
  page.locator('ul[aria-label="Leyenda del mapa"] li').evaluateAll((items) => {
    let best = { step: 0, color: '' };
    for (const item of items) {
      const swatch = item.querySelector('[data-kx-part="swatch"]');
      const step = Number(swatch?.getAttribute('data-kx-step'));
      if (swatch && Number.isFinite(step) && step > best.step) best = { step, color: getComputedStyle(swatch).backgroundColor };
    }
    return best.color;
  });

/** Rectángulo de un elemento en píxeles de la página. */
const rectOf = (locator: Locator) =>
  locator.first().evaluate((el) => {
    const rect = el.getBoundingClientRect();
    return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
  });

/** Color de fondo del lienzo del mapa (`--th-surface-canvas-bg`) como `rgb(r, g, b)`, sea cual sea su sintaxis CSS. */
const canvasBackground = (page: Page) =>
  page.locator('[data-kx-part="viewport"]').evaluate((el) => {
    const canvas = document.createElement('canvas');
    canvas.width = 1;
    canvas.height = 1;
    const context = canvas.getContext('2d');
    if (!context) return '';
    context.fillStyle = getComputedStyle(el).backgroundColor;
    context.fillRect(0, 0, 1, 1);
    const [r, g, b] = context.getImageData(0, 0, 1, 1).data;
    return `rgb(${r}, ${g}, ${b})`;
  });

/**
 * Píxeles de una región de la página que NO son del color de fondo, contados sobre una captura real (el lienzo WebGL
 * no se puede leer de otra forma). Con la leyenda y los rótulos escondidos, lo que queda es el mapa: si hay un solo
 * píxel distinto, hay un polígono (o su borde) debajo.
 */
async function nonBackgroundPixels(page: Page, region: { x: number; y: number; width: number; height: number }, background: string): Promise<number> {
  const png = await page.screenshot({ clip: region });
  return page.evaluate(
    async ({ base64, color }) => {
      const match = /rgba?\((\d+),\s*(\d+),\s*(\d+)/.exec(color);
      if (!match) return -1;
      const target = [Number(match[1]), Number(match[2]), Number(match[3])];
      const bytes = Uint8Array.from(atob(base64), (char) => char.charCodeAt(0));
      const bitmap = await createImageBitmap(new Blob([bytes], { type: 'image/png' }));
      const canvas = document.createElement('canvas');
      canvas.width = bitmap.width;
      canvas.height = bitmap.height;
      const context = canvas.getContext('2d');
      if (!context) return -1;
      context.drawImage(bitmap, 0, 0);
      const { data } = context.getImageData(0, 0, bitmap.width, bitmap.height);
      let different = 0;
      for (let index = 0; index < data.length; index += 4) {
        const delta = Math.max(
          Math.abs((data[index] ?? 0) - (target[0] ?? 0)),
          Math.abs((data[index + 1] ?? 0) - (target[1] ?? 0)),
          Math.abs((data[index + 2] ?? 0) - (target[2] ?? 0)),
        );
        if (delta > 4) different += 1;
      }
      return different;
    },
    { base64: png.toString('base64'), color: background },
  );
}

/** Esconde la leyenda, los rótulos y los avisos que se superponen al mapa, para ver sólo el lienzo. */
const hideOverlays = (page: Page) =>
  page.addStyleTag({
    content: '[data-kx-part="legend"], .kx-map-label, [data-kx-part="notice"], [data-kx-part="hover"] { visibility: hidden !important; }',
  });

/** Clic donde el usuario ve el color de datos sobre el lienzo (medido en una captura real). */
async function clickDataPolygon(page: Page): Promise<void> {
  const box = await page.getByRole('region', { name: /^Mapa de Instalaciones: / }).boundingBox();
  expect(box).not.toBeNull();
  const region = { x: box?.x ?? 0, y: box?.y ?? 0, width: box?.width ?? 0, height: box?.height ?? 0 };
  const color = await dataColor(page);
  expect(color).not.toBe('');
  // La leyenda y el rótulo de la fuente se superponen al lienzo y usan el mismo color: no cuentan.
  const overlays = await page.locator('figure, [role="note"]').evaluateAll((items) =>
    items.map((item) => {
      const rect = item.getBoundingClientRect();
      return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
    }),
  );
  // El lienzo WebGL se pinta un par de fotogramas después de cargar la fuente: se espera a que el
  // centro de la mancha deje de moverse antes de pulsar.
  let last: { x: number; y: number } | null = null;
  let stable: { x: number; y: number } | null = null;
  await poll(async () => {
    const found = await findColorCenter(page, color, region, overlays);
    stable = found && last && Math.hypot(found.x - last.x, found.y - last.y) < 2 ? found : null;
    last = found;
    return stable !== null;
  }, 20_000).toBe(true);
  const target = stable as { x: number; y: number } | null;
  expect(target).not.toBeNull();
  await page.mouse.click(target?.x ?? 0, target?.y ?? 0);
}

e2eSuite('mapa', ({ open }) => {
  it('baja de mundo a país y a ciudad con clics: el nivel de una sola provincia (Madrid) se salta, y las migas, la URL y Esc acompañan', async () => {
    const { page } = await open({ path: '/facility?view=map&level=world', ready: READY });
    await attr(mapSection(page), 'data-kx-level').toBe('world');
    await visible(page.getByRole('status').filter({ hasText: 'Mundo:' })).toBe(true);
    expect(await trail(page)).toBe('Mundo');
    await attr(up(page), 'disabled').toBe('');
    expect(await up(page).isDisabled()).toBe(true);
    // Rótulo honesto: la geometría de la demo es esquemática.
    await visible(page.getByText(/Geometría esquemática de demostración/)).toBe(true);

    // Mundo → España (nivel país: comunidades).
    await clickDataPolygon(page);
    await attr(mapSection(page), 'data-kx-level').toBe('country');
    await route(page).toMatch(/level=country/);
    await route(page).toMatch(/code=ES(&|$)/);
    await poll(() => trail(page)).toBe('Mundo › España');

    // España → Comunidad de Madrid: su nivel (provincias) tiene un solo elemento y se salta, directo a los municipios.
    await clickDataPolygon(page);
    await attr(mapSection(page), 'data-kx-level').toBe('city');
    await route(page).toMatch(/level=city/);
    await route(page).toMatch(/code=ES300/);
    await poll(() => trail(page)).toBe('Mundo › España › Comunidad de Madrid › Madrid');
    await text(page.getByRole('status').filter({ hasText: /delimitaciones/ })).toBe('Ciudad: 14 delimitaciones.');
    // La miga saltada nombra el lugar pero no es un botón; las demás sí lo son.
    await count(crumbs(page).getByRole('button', { name: 'Comunidad de Madrid' })).toBe(0);
    await count(crumbs(page).getByRole('button', { name: 'España' })).toBe(1);
    await visible(crumbs(page).locator('[data-kx-part="skipped"]')).toBe(true);

    // Esc sube un nivel que se vea: del municipio al país, sin parar en el nivel saltado; luego al mundo.
    await page.keyboard.press('Escape');
    await attr(mapSection(page), 'data-kx-level').toBe('country');
    await poll(() => trail(page)).toBe('Mundo › España');
    await up(page).click();
    await attr(mapSection(page), 'data-kx-level').toBe('world');
    expect(await trail(page)).toBe('Mundo');
    await route(page).not.toMatch(/code=/);
    await page.keyboard.press('Escape');
    await attr(mapSection(page), 'data-kx-level').toBe('world');
  });

  it('una región con varias provincias (Cataluña) sí se enseña: país → región → ciudad', async () => {
    const { page } = await open({ path: '/facility?view=map&level=region&code=ES51', ready: READY });
    await attr(mapSection(page), 'data-kx-level').toBe('region');
    await poll(() => trail(page)).toBe('Mundo › España › Cataluña');
    await text(page.getByRole('status').filter({ hasText: /delimitaciones/ })).toBe('Región: 4 delimitaciones.');
    // Sin nivel saltado: todas las migas anteriores son botones.
    await count(crumbs(page).locator('[data-kx-part="skipped"]')).toBe(0);

    await clickDataPolygon(page);
    await attr(mapSection(page), 'data-kx-level').toBe('city');
    await route(page).toMatch(/code=ES511/);
    await poll(() => trail(page)).toBe('Mundo › España › Cataluña › Barcelona');
    await page.keyboard.press('Escape');
    await attr(mapSection(page), 'data-kx-level').toBe('region');
    await poll(() => trail(page)).toBe('Mundo › España › Cataluña');
  });

  it('entrar por la URL en un nivel de un solo elemento (Madrid) redirige al siguiente con `replace`: «Atrás» no rebota (R3-4, M-5)', async () => {
    const { page, go } = await open({ path: '/facility?view=map&level=world', ready: READY });
    // Como un enlace o un favorito: una entrada de historial nueva con la URL del nivel de la provincia de Madrid.
    await go('/facility?view=map&level=region&code=ES30', READY);
    await attr(mapSection(page), 'data-kx-level').toBe('city');
    await route(page).toMatch(/level=city/);
    await route(page).toMatch(/code=ES300/);
    await route(page).not.toMatch(/level=region/);
    await poll(() => trail(page)).toBe('Mundo › España › Comunidad de Madrid › Madrid');
    await text(page.getByRole('status').filter({ hasText: /delimitaciones/ })).toBe('Ciudad: 14 delimitaciones.');
    // La URL intermedia no quedó en el historial: atrás vuelve al mundo, no a la región (que rebotaría otra vez).
    await page.goBack();
    await route(page).toMatch(/level=world/);
    await poll(() => page.evaluate(() => window.location.search)).not.toMatch(/level=region|level=city/);

    // Y al abrir la página directamente en esa URL pasa lo mismo, sin pintar nunca el cuadrado de un solo elemento.
    await go('/facility?view=map&level=region&code=ES30&layout=list', READY);
    await route(page).toMatch(/level=city.*code=ES300|code=ES300.*level=city/);
    await route(page).toMatch(/layout=list/);
  });

  it('el nivel de un solo elemento no se enseña ni un instante: mientras se redirige el mapa sigue cargando (R3-4, M-5)', async () => {
    const { page, go } = await open({ path: '/facility?view=map&level=world', ready: READY });
    // Se anotan todos los niveles que la vista llega a marcar como «listos» o a pintar con su lienzo.
    await page.evaluate(() => {
      const seen: string[] = [];
      (window as unknown as { __kxLevels: string[] }).__kxLevels = seen;
      new MutationObserver(() => {
        const section = document.querySelector('section[data-kx-view="map"]');
        const level = section?.getAttribute('data-kx-level');
        if (section?.getAttribute('data-kx-ready') === 'true' && level && seen.at(-1) !== level) seen.push(level);
      }).observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ['data-kx-ready', 'data-kx-level'] });
    });
    await go('/facility?view=map&level=region&code=ES30', READY);
    await attr(mapSection(page), 'data-kx-level').toBe('city');
    const levels = await page.evaluate(() => (window as unknown as { __kxLevels: string[] }).__kxLevels);
    expect(levels).not.toContain('region');
  });

  it('en el mundo todos los rótulos comparten una sola variante (`data-kx-kind`): un nivel, un criterio (R3-4, M-6)', async () => {
    const { page } = await open({ path: '/facility?view=map&level=world', ready: READY });
    await poll(() => page.locator('[data-kx-part="label"]:not([hidden])').count()).toBeGreaterThan(0);
    const facts = await page.evaluate(() => {
      const labels = [...document.querySelectorAll<HTMLElement>('[data-kx-part="map"] [data-kx-part="label"]')];
      const shown = labels.filter((el) => !el.hidden);
      return {
        shown: shown.length,
        kinds: [...new Set(shown.map((el) => el.getAttribute('data-kx-kind')))],
        hiddenWithKind: labels.filter((el) => el.hidden && el.hasAttribute('data-kx-kind')).length,
      };
    });
    expect(facts.shown).toBeGreaterThan(0);
    expect(facts.kinds).toHaveLength(1);
    expect(['full', 'short', 'code']).toContain(facts.kinds[0]);
    expect(facts.hiddenWithKind).toBe(0);
  });

  it('la trama de «sin dato» del mapa es la suave: el token existe y no es la de la leyenda (R3-4, M-2)', async () => {
    const { page } = await open({ path: '/facility?view=map&level=country&code=ES', ready: READY });
    const tokens = await page.evaluate(() => {
      const resolve = (token: string) => {
        const probe = document.createElement('span');
        probe.style.color = `var(${token})`;
        document.body.append(probe);
        const color = getComputedStyle(probe).color;
        probe.remove();
        return color;
      };
      return { soft: resolve('--kx-data-missing-hatch-map'), marked: resolve('--kx-data-missing-hatch'), fill: resolve('--kx-data-missing') };
    });
    expect(tokens.soft).not.toBe('');
    expect(tokens.soft).not.toBe(tokens.marked);
    expect(tokens.soft).not.toBe(tokens.fill);
  });

  it('una URL directa abre el nivel y reconstruye las migas; las migas son enlaces para subir', async () => {
    const { page } = await open({ path: '/facility?view=map&level=city&code=ES300', ready: READY });
    await attr(mapSection(page), 'data-kx-level').toBe('city');
    await poll(() => trail(page)).toBe('Mundo › España › Comunidad de Madrid › Madrid');

    await crumbs(page).getByRole('link', { name: 'España' }).or(crumbs(page).getByRole('button', { name: 'España' })).click();
    await attr(mapSection(page), 'data-kx-level').toBe('country');
    await route(page).toMatch(/code=ES(&|$)/);
    await poll(() => trail(page)).toBe('Mundo › España');
  });

  it('«Ver como tabla» es la alternativa accesible: tabla que también navega, y «Ver como mapa» vuelve', async () => {
    const { page } = await open({ path: '/facility?view=map&level=world', ready: READY });
    // Sin interruptor propio «Mapa | Listado» (M-6): un solo botón dentro del mapa.
    await count(page.getByRole('group', { name: 'Disposición del mapa' })).toBe(0);
    await toTable(page).click();
    await route(page).toMatch(/layout=list/);
    const table = page.getByRole('table', { name: /^Mundo/ });
    await visible(table).toBe(true);
    // Sólo España tiene detalle en Go: el resto de países se indica «Sin detalle».
    await visible(table.getByRole('button', { name: 'Entrar en España' })).toBe(true);
    await text(table.getByRole('row', { name: /Afganistán/ })).toContain('Sin detalle');
    await text(table.getByRole('row', { name: /Entrar en España/ })).toContain('30');

    await table.getByRole('button', { name: 'Entrar en España' }).click();
    await attr(mapSection(page), 'data-kx-level').toBe('country');
    await text(page.getByRole('table').locator('caption')).toContain('España');
    await poll(() => trail(page)).toBe('Mundo › España');

    await toMap(page).click();
    await route(page).not.toMatch(/layout=list/);
    await visible(page.getByRole('region', { name: /^Mapa de Instalaciones: / })).toBe(true);
    await count(page.getByRole('table')).toBe(0);
  });

  it('en el nivel país se rotula casi todo, ningún rótulo desborda su caja ni pisa a otro, y la letra es la del tema', async () => {
    const { page } = await open({ path: '/facility?view=map&level=country&code=ES', ready: READY });
    const statusLine = page.getByRole('status').filter({ hasText: /delimitaciones/ }).first();
    await visible(statusLine).toBe(true);
    const total = Number(/(\d+) delimitaciones/.exec((await statusLine.textContent()) ?? '')?.[1]);
    expect(total).toBe(19);
    // Los rótulos se colocan al terminar de pintar: se espera a que haya los que tiene que haber.
    await poll(() => page.locator('.kx-map-label:not([hidden])').count()).toBeGreaterThanOrEqual(Math.ceil(total * 0.9));

    const facts = await page.evaluate(() => {
      const body = getComputedStyle(document.body).fontFamily;
      const shown = [...document.querySelectorAll<HTMLElement>('.kx-map-label')].filter((el) => !el.hidden);
      const viewport = document.querySelector('[data-kx-part="viewport"]')!.getBoundingClientRect();
      const boxes = shown.map((el) => el.getBoundingClientRect());
      const overflow: string[] = [];
      shown.forEach((el, index) => {
        const box = boxes[index]!;
        // El texto no se sale del rótulo (se midió con la fuente del tema) ni éste del lienzo.
        if (el.scrollWidth > el.clientWidth + 1 || el.scrollHeight > el.clientHeight + 1) overflow.push(`${el.dataset.kxCode}: texto fuera de su caja`);
        for (const part of el.querySelectorAll('[data-kx-part]')) {
          const inner = part.getBoundingClientRect();
          if (inner.width > 0 && (inner.left < box.left - 1 || inner.right > box.right + 1)) overflow.push(`${el.dataset.kxCode}: parte fuera de su caja`);
        }
        if (box.left < viewport.left - 1 || box.right > viewport.right + 1 || box.top < viewport.top - 1 || box.bottom > viewport.bottom + 1) {
          overflow.push(`${el.dataset.kxCode}: fuera del lienzo`);
        }
      });
      const overlaps: string[] = [];
      for (let i = 0; i < boxes.length; i += 1) {
        for (let j = i + 1; j < boxes.length; j += 1) {
          const a = boxes[i]!;
          const b = boxes[j]!;
          if (a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top) overlaps.push(`${shown[i]!.dataset.kxCode} y ${shown[j]!.dataset.kxCode}`);
        }
      }
      return {
        shown: shown.length,
        overflow,
        overlaps,
        // B-3: la letra de los rótulos es la del tema (la de `body`), no la de MapLibre.
        fonts: [...new Set(shown.map((el) => getComputedStyle(el).fontFamily))],
        body,
        names: shown.map((el) => ({ code: el.dataset.kxCode, kind: el.dataset.kxKind, title: el.getAttribute('title') })),
      };
    });
    expect(facts.shown).toBeGreaterThanOrEqual(Math.ceil(total * 0.9));
    expect(facts.overflow).toEqual([]);
    expect(facts.overlaps).toEqual([]);
    expect(facts.fonts).toEqual([facts.body]);
    // Un nombre abreviado lleva el completo en el título.
    for (const label of facts.names.filter((name) => name.kind === 'short' || name.kind === 'code')) expect(label.title).toBeTruthy();
  });

  it('en el nivel de ciudad de una provincia (14 municipios) también se rotula casi todo', async () => {
    const { page } = await open({ path: '/facility?view=map&level=city&code=ES511', ready: READY });
    await poll(() => page.locator('.kx-map-label:not([hidden])').count()).toBeGreaterThanOrEqual(Math.ceil(14 * 0.9));
    const overflow = await page.evaluate(() =>
      [...document.querySelectorAll<HTMLElement>('.kx-map-label')]
        .filter((el) => !el.hidden && (el.scrollWidth > el.clientWidth + 1 || el.scrollHeight > el.clientHeight + 1))
        .map((el) => el.dataset.kxCode),
    );
    expect(overflow).toEqual([]);
  });

  for (const [title, options, level] of [
    ['país', {}, 'level=country&code=ES'],
    ['mundo', {}, 'level=world'],
    ['país, en RTL (la leyenda cambia de lado)', { dir: 'rtl' as const }, 'level=country&code=ES'],
  ] as const) {
    it(`la leyenda no tapa ningún polígono tras el ajuste (${title})`, async () => {
      const { page } = await open({ path: `/facility?view=map&${level}`, ready: READY, ...options });
      // El mapa está pintado cuando hay color de datos en el lienzo (o, en el mundo, cuando la leyenda ya lo explica).
      const background = await canvasBackground(page);
      expect(background).toMatch(/^rgb\(/);
      await poll(async () => {
        const box = await rectOf(page.locator('[data-kx-part="canvas"]'));
        const color = await dataColor(page);
        return color !== '' && (await findColorCenter(page, color, box)) !== null;
      }).toBe(true);
      const legend = await rectOf(page.locator('figure[data-kx-part="legend"]'));
      await hideOverlays(page);
      // El dibujo del mapa es independiente de la leyenda escondida: debajo de ella sólo hay fondo.
      await poll(() => nonBackgroundPixels(page, legend, background)).toBe(0);
    });
  }

  it('la hoja de MapLibre sólo se descarga con la vista del mapa y no cambia la tipografía de los rótulos', async () => {
    const { page, go } = await open({ path: '/facility' });
    const hasMapLibreCss = () =>
      page.evaluate(() =>
        [...document.styleSheets]
          .map((sheet) => {
            try {
              return [...sheet.cssRules].map((rule) => rule.cssText).join('');
            } catch {
              return '';
            }
          })
          .join('')
          .includes('.maplibregl-map'),
      );
    expect(await hasMapLibreCss()).toBe(false);
    await go('/facility?view=map&level=world', READY);
    await poll(hasMapLibreCss).toBe(true);
  });

  it('los geodatos salen del propio origen: GeoJSON por nivel y un .pmtiles alojado por el despliegue', async () => {
    const { page, go } = await open({ path: '/facility?view=map&level=world', ready: READY });
    await go('/facility?view=map&level=city&code=ES300', READY);
    await attr(mapSection(page), 'data-kx-level').toBe('city');

    const geo = (await page.requests()).map((request) => new URL(request.url())).filter((url) => url.pathname.startsWith('/geo/'));
    expect(geo.length).toBeGreaterThan(0);
    expect(geo.every((url) => /^\/geo\/[\w.-]+$/.test(url.pathname) && url.origin === new URL(page.url()).origin)).toBe(true);

    // El .pmtiles se sirve con Range desde el mismo origen (el despliegue lo aloja).
    const header = await page.evaluate(async () => {
      const response = await fetch('/geo/karpo-demo.pmtiles', { headers: { Range: 'bytes=0-6' } });
      return { status: response.status, magic: new TextDecoder().decode(await response.arrayBuffer()) };
    });
    expect(header.magic).toBe('PMTiles');
    expect([200, 206]).toContain(header.status);
  });
});
