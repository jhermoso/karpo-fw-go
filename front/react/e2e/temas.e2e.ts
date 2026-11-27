// Temas de Theros Front (PLAN §4.5): modern-flat claro y oscuro, classic-1995 y glossy-2003 (y souq, de
// prueba) sin romper la rejilla: alturas desde las variables del tema, contraste AA, fuente del tema y
// el mapa que se repinta al cambiar de modo en caliente.
import type { Page } from 'playwright-core';
import { expect, it } from 'vitest';
import { findColorCenter } from './lib/demo.mjs';
import { attr, count, poll, text, visible } from './lib/esperar';
import { e2eSuite, type E2eApi } from './lib/suite';

const THEMES = [
  { label: 'Moderno', folder: 'modern-flat', modes: ['light', 'dark'] },
  { label: 'Clásico 1995', folder: 'classic-1995', modes: ['light'] },
  { label: 'Brillante 2003', folder: 'glossy-2003', modes: ['light'] },
  { label: 'Zoco', folder: 'souq', modes: ['dark'] },
] as const;

const gridRows = (page: Page) => page.locator('[role="row"][data-kx-row]');

async function chooseTheme(page: Page, label: string, folder: string) {
  await page.getByRole('button', { name: 'Tema', exact: true }).click();
  await page.getByRole('menuitemradio', { name: label, exact: true }).click();
  await count(page.locator(`link[rel="stylesheet"][href="/themes/${folder}/index.css"]`)).toBe(1);
  // Sólo queda la hoja del tema activo: el anterior se retira cuando el nuevo ya está cargado.
  await count(page.locator('link[rel="stylesheet"][href^="/themes/"]')).toBe(1);
}

async function chooseMode(page: Page, label: 'Claro' | 'Oscuro') {
  await page.getByRole('button', { name: 'Modo de color', exact: true }).click();
  await page.getByRole('menuitemradio', { name: label, exact: true }).click();
}

/** Medidas de la rejilla tal como las ve el usuario. */
async function measureGrid(page: Page) {
  return page.evaluate(() => {
    const grid = document.querySelector('[role="grid"]') as HTMLElement;
    const declared = Number.parseFloat(grid.style.getPropertyValue('--kx-row-height'));
    const rows = [...document.querySelectorAll('[role="row"][data-kx-row]')].slice(0, 6) as HTMLElement[];
    const boxes = rows.map((row) => row.getBoundingClientRect());
    const clipped = rows.flatMap((row) =>
      [...row.querySelectorAll('[role="gridcell"]')].filter((cell) => cell.scrollHeight > cell.clientHeight + 1).map((cell) => cell.getAttribute('data-kx-col')),
    );
    const header = grid.querySelector('[role="columnheader"][data-kx-col="name"]') as HTMLElement | null;
    const headerBox = header?.getBoundingClientRect();
    const scroller = document.scrollingElement as HTMLElement;
    return {
      declared,
      heights: boxes.map((box) => Math.round(box.height * 100) / 100),
      gaps: boxes.slice(1).map((box, index) => Math.round((box.top - (boxes[index]?.bottom ?? 0)) * 100) / 100),
      clipped,
      headerHeight: headerBox ? Math.round(headerBox.height * 100) / 100 : 0,
      overflowX: scroller.scrollWidth - scroller.clientWidth,
      bodyFont: getComputedStyle(document.body).fontFamily,
      themeFont: getComputedStyle(document.documentElement).getPropertyValue('--th-type-ui-family'),
    };
  });
}

/** Contraste WCAG entre el texto de una celda y el fondo efectivo que tiene detrás. */
async function cellContrastInfo(page: Page, column: string): Promise<{ ratio: number; background: string }> {
  return page.evaluate((col) => {
    const cell = document.querySelector(`[role="row"][data-kx-row] [data-kx-col="${col}"] [data-kx-ui="value"]`) as HTMLElement;
    const canvas = document.createElement('canvas');
    canvas.width = 1;
    canvas.height = 1;
    const context = canvas.getContext('2d', { willReadFrequently: true }) as CanvasRenderingContext2D;
    const toRgba = (color: string): [number, number, number, number] => {
      context.clearRect(0, 0, 1, 1);
      context.fillStyle = color;
      context.fillRect(0, 0, 1, 1);
      const [r = 0, g = 0, b = 0, a = 0] = context.getImageData(0, 0, 1, 1).data;
      return [r, g, b, a / 255];
    };
    const luminance = ([r, g, b]: number[]) => {
      const channel = (value: number) => {
        const v = value / 255;
        return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
      };
      return 0.2126 * channel(r ?? 0) + 0.7152 * channel(g ?? 0) + 0.0722 * channel(b ?? 0);
    };
    // Fondo efectivo: se compone hacia arriba hasta una capa opaca.
    let background: [number, number, number] = [255, 255, 255];
    const layers: [number, number, number, number][] = [];
    for (let node: HTMLElement | null = cell; node; node = node.parentElement) {
      const [r, g, b, a] = toRgba(getComputedStyle(node).backgroundColor);
      if (a > 0) layers.push([r, g, b, a]);
      if (a >= 1) break;
    }
    for (const [r, g, b, a] of layers.reverse()) {
      background = [r * a + background[0] * (1 - a), g * a + background[1] * (1 - a), b * a + background[2] * (1 - a)];
    }
    const [fr = 0, fg = 0, fb = 0] = toRgba(getComputedStyle(cell).color);
    const lighter = Math.max(luminance([fr, fg, fb]), luminance(background));
    const darker = Math.min(luminance([fr, fg, fb]), luminance(background));
    return { ratio: (lighter + 0.05) / (darker + 0.05), background: background.map(Math.round).join(',') };
  }, column);
}

const cellContrast = async (page: Page, column: string) => (await cellContrastInfo(page, column)).ratio;

e2eSuite('temas', ({ open }) => {
  it('cada tema redimensiona la rejilla desde sus variables, sin recortes, huecos ni desbordes', async () => {
    const { page } = await open({ path: '/product' });
    const declaredHeights = new Map<string, number>();
    for (const theme of THEMES) {
      await chooseTheme(page, theme.label, theme.folder);
      await visible(gridRows(page).first()).toBe(true);
      // La altura de la fila se vuelve a medir al cambiar de tema.
      await poll(async () => {
        const grid = await measureGrid(page);
        return grid.heights.every((height) => Math.abs(height - grid.declared) < 0.6);
      }).toBe(true);
      const grid = await measureGrid(page);
      declaredHeights.set(theme.folder, grid.declared);
      expect(grid.declared, `${theme.folder}: altura de fila medida`).toBeGreaterThan(10);
      expect(grid.gaps.every((gap) => Math.abs(gap) < 0.6), `${theme.folder}: filas contiguas ${grid.gaps}`).toBe(true);
      expect(grid.clipped, `${theme.folder}: celdas recortadas`).toEqual([]);
      expect(grid.headerHeight, `${theme.folder}: cabecera visible`).toBeGreaterThan(10);
      expect(grid.overflowX, `${theme.folder}: la página no desborda en horizontal`).toBeLessThanOrEqual(0);
      // La tipografía sale del tema: la familia del cuerpo es la variable del tema.
      expect(grid.bodyFont.replace(/["']/g, ''), `${theme.folder}: familia de la interfaz`).toBe(grid.themeFont.replace(/["']/g, '').trim());
      // Y los datos siguen siendo legibles (WCAG AA, 4,5:1) en el tema.
      const contrast = await cellContrast(page, 'name');
      expect(contrast, `${theme.folder}: contraste del texto de la celda`).toBeGreaterThanOrEqual(4.5);
    }
    // Cada tema trae su propia densidad: las alturas no son una constante del código.
    expect(new Set(declaredHeights.values()).size).toBeGreaterThanOrEqual(3);
    expect(declaredHeights.get('classic-1995') ?? 0).toBeLessThan(declaredHeights.get('modern-flat') ?? 0);
  });

  it('modern-flat cambia entre claro y oscuro en caliente y el contraste se mantiene', async () => {
    const { page } = await open({ path: '/product' });
    const root = page.locator('html');
    const colors = async () =>
      page.evaluate(() => {
        const cell = document.querySelector('[role="row"][data-kx-row] [data-kx-col="name"]') as HTMLElement;
        return { text: getComputedStyle(cell).color };
      });
    const light = await colors();
    const lightInfo = await cellContrastInfo(page, 'name');

    await chooseMode(page, 'Oscuro');
    await attr(root, 'data-th-mode').toBe('dark');
    await poll(async () => (await colors()).text).not.toBe(light.text);
    const dark = await colors();
    expect(dark.text).not.toBe(light.text);
    const darkInfo = await cellContrastInfo(page, 'name');
    expect(darkInfo.background, 'el fondo cambia con el modo').not.toBe(lightInfo.background);
    expect(darkInfo.ratio).toBeGreaterThanOrEqual(4.5);
    expect(lightInfo.ratio).toBeGreaterThanOrEqual(4.5);

    await chooseMode(page, 'Claro');
    await attr(root, 'data-th-mode').toBe('light');
    await poll(async () => (await colors()).text).toBe(light.text);
    // La preferencia se guarda por usuario.
    expect(await page.evaluate(() => localStorage.getItem('karpo.theme.mode'))).toBe('light');
  });

  it('un tema que sólo tiene un modo no ofrece un interruptor que no haría nada', async () => {
    const { page } = await open({ path: '/product' });
    await visible(page.getByRole('button', { name: 'Modo de color', exact: true })).toBe(true);
    await chooseTheme(page, 'Clásico 1995', 'classic-1995');
    await count(page.getByRole('button', { name: 'Modo de color', exact: true })).toBe(0);
    await chooseTheme(page, 'Moderno', 'modern-flat');
    await visible(page.getByRole('button', { name: 'Modo de color', exact: true })).toBe(true);
    expect(await page.evaluate(() => localStorage.getItem('karpo.theme'))).toBe('modern-flat');
  });

  it('los colores de datos del mapa salen del tema y se repintan al cambiar de modo', async () => {
    const { page } = await open({ path: '/facility?view=map&level=world', ready: 'section[data-kx-view="map"][data-kx-ready="true"]' });
    const topStep = () =>
      page.locator('ul[aria-label="Leyenda del mapa"] li [data-kx-part="swatch"]').evaluateAll((swatches) => {
        let best = { step: 0, color: '' };
        for (const swatch of swatches) {
          const step = Number(swatch.getAttribute('data-kx-step'));
          if (Number.isFinite(step) && step > best.step) best = { step, color: getComputedStyle(swatch).backgroundColor };
        }
        return best.color;
      });
    const overlays = () =>
      page.locator('figure, [role="note"]').evaluateAll((items) =>
        items.map((item) => {
          const rect = item.getBoundingClientRect();
          return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
        }),
      );
    const canvasHasColor = async (color: string) => {
      const box = await page.getByRole('region', { name: /^Mapa de Instalaciones: / }).boundingBox();
      expect(box).not.toBeNull();
      const region = { x: box?.x ?? 0, y: box?.y ?? 0, width: box?.width ?? 0, height: box?.height ?? 0 };
      return (await findColorCenter(page, color, region, await overlays())) !== null;
    };

    const lightColor = await topStep();
    await poll(() => canvasHasColor(lightColor), 20_000).toBe(true);
    await chooseMode(page, 'Oscuro');
    await attr(page.locator('html'), 'data-th-mode').toBe('dark');
    await poll(topStep).not.toBe(lightColor);
    const darkColor = await topStep();
    // El lienzo WebGL no usa variables CSS: el mapa vuelve a resolverlas y se repinta con el color nuevo.
    await poll(() => canvasHasColor(darkColor), 20_000).toBe(true);
    await poll(() => canvasHasColor(lightColor), 20_000).toBe(false);
  });

  it('classic-1995 y glossy-2003 mantienen el diseño del tema en el detalle y los menús', async () => {
    const { page } = await open({ path: '/facility' });
    for (const theme of [THEMES[1], THEMES[2]]) {
      await chooseTheme(page, theme.label, theme.folder);
      await page.locator('[role="row"][data-kx-row]').first().locator('[data-kx-col="name"]').dblclick();
      const sheet = page.locator('section[data-th="sheet"]');
      await attr(sheet, 'data-kx-mode').toBe('edit');
      await visible(sheet.getByRole('textbox', { name: 'Nombre' })).toBe(true);
      const box = await sheet.boundingBox();
      expect(box?.width ?? 0).toBeGreaterThan(200);
      expect(await page.evaluate(() => document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth)).toBeLessThanOrEqual(0);
      await sheet.getByRole('button', { name: 'Cancelar' }).click();
      await text(sheet.getByRole('heading', { level: 2 })).not.toBe('');
    }
  });

  paletteTests(open);
});

// ---------------------------------------------------------------------------------------------
// M-2 y M-14 en el navegador real: los mismos pares que `src/theme/contrast.contract.test.ts` evalúa
// con su intérprete de CSS, medidos aquí con el motor de estilos de Edge o Chrome. Si el intérprete y el
// navegador discreparan, esta prueba lo vería.

type Rgb = [number, number, number];

/** Color calculado de un token (`--th-*` o `--kx-*`) sobre la raíz, en sRGB 0-255 (convertido con un lienzo). */
async function readToken(page: Page, token: string): Promise<Rgb> {
  return page.evaluate((name) => {
    const probe = document.createElement('span');
    probe.style.setProperty('color', `var(${name})`);
    document.documentElement.appendChild(probe);
    const computed = getComputedStyle(probe).color;
    probe.remove();
    const canvas = document.createElement('canvas');
    canvas.width = 1;
    canvas.height = 1;
    const context = canvas.getContext('2d', { willReadFrequently: true }) as CanvasRenderingContext2D;
    context.fillStyle = computed;
    context.fillRect(0, 0, 1, 1);
    const [r = 0, g = 0, b = 0] = context.getImageData(0, 0, 1, 1).data;
    return [r, g, b] as Rgb;
  }, token);
}

const linear = (value: number) => {
  const v = value / 255;
  return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
};

/** Luminosidad L de oklab (0 a 1) de un color sRGB 0-255. */
function oklabL([r, g, b]: Rgb): number {
  const [lr, lg, lb] = [linear(r), linear(g), linear(b)] as const;
  const l = Math.cbrt(0.4122214708 * lr + 0.5363325363 * lg + 0.0514459929 * lb);
  const m = Math.cbrt(0.2119034982 * lr + 0.6806995451 * lg + 0.1073969566 * lb);
  const s = Math.cbrt(0.0883024619 * lr + 0.2817188376 * lg + 0.6299787005 * lb);
  return 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s;
}

function wcagRatio(a: Rgb, b: Rgb): number {
  const lum = ([r, g, bl]: Rgb) => 0.2126 * linear(r) + 0.7152 * linear(g) + 0.0722 * linear(bl);
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}

// Dentro del mismo `e2eSuite` (un navegador por fichero): `afterAll` lo cierra y una segunda suite no podría abrir páginas.
function paletteTests(open: E2eApi['open']): void {
  it('«sin dato» se separa de seq-1 (ΔL ≥ 0,04), la escala crece y el contorno de la leyenda llega a 3:1 en cada tema y modo', async () => {
    const { page } = await open({ path: '/product' });
    const seen: string[] = [];
    for (const theme of THEMES) {
      await chooseTheme(page, theme.label, theme.folder);
      const modes = theme.folder === 'modern-flat' ? (['Claro', 'Oscuro'] as const) : ([undefined] as const);
      for (const mode of modes) {
        if (mode) await chooseMode(page, mode);
        const modeName = (await page.locator('html').getAttribute('data-th-mode')) ?? '';
        const label = `${theme.folder} · ${modeName}`;
        seen.push(label);
        const surface = await readToken(page, '--th-surface-content-bg');
        const seq = [] as Rgb[];
        for (const n of [1, 2, 3, 4, 5, 6, 7]) seq.push(await readToken(page, `--kx-data-seq-${n}`));
        const missing = await readToken(page, '--kx-data-missing');
        const delta = Math.abs(oklabL(missing) - oklabL(seq[0] as Rgb));
        expect(delta, `${label}: ΔL(missing, seq-1) = ${delta.toFixed(3)}`).toBeGreaterThanOrEqual(0.04);
        // La escala se aleja de la superficie paso a paso (intensidad creciente), en claro y en oscuro.
        const distance = seq.map((color) => Math.abs(oklabL(color) - oklabL(surface)));
        for (let i = 1; i < distance.length; i += 1) {
          expect(distance[i] as number, `${label}: seq-${i + 1} debe ser más intensa que seq-${i}`).toBeGreaterThan(distance[i - 1] as number);
        }
        // Contorno de la leyenda y trama de «sin dato».
        expect(wcagRatio(await readToken(page, '--kx-data-border'), surface), `${label}: contorno de la leyenda`).toBeGreaterThanOrEqual(3);
        expect(wcagRatio(await readToken(page, '--kx-data-missing-hatch'), missing), `${label}: trama de «sin dato»`).toBeGreaterThanOrEqual(3);
        // El texto principal y el secundario siguen a 4,5:1 sobre la superficie.
        for (const token of ['--th-text-primary', '--th-text-secondary', '--th-text-link']) {
          expect(wcagRatio(await readToken(page, token), surface), `${label}: ${token}`).toBeGreaterThanOrEqual(4.5);
        }
      }
    }
    expect(seen).toEqual(expect.arrayContaining(['modern-flat · light', 'modern-flat · dark', 'classic-1995 · light']));
  });

  it('los iconos se pintan con el peso del tema (relleno en glossy-2003) y ninguno es el círculo de reserva', async () => {
    const { page } = await open({ path: '/product' });
    for (const theme of THEMES) {
      await chooseTheme(page, theme.label, theme.folder);
      await visible(gridRows(page).first()).toBe(true);
      const info = await page.evaluate(() => ({
        total: document.querySelectorAll('svg[data-th-icon]').length,
        missing: document.querySelectorAll('[data-kx-icon="missing"]').length,
        weights: [...new Set([...document.querySelectorAll('svg[data-th-icon]')].map((el) => el.getAttribute('data-kx-weight')))],
      }));
      expect(info.total, `${theme.folder}: iconos`).toBeGreaterThan(5);
      expect(info.missing, `${theme.folder}: iconos desconocidos`).toBe(0);
      expect(info.weights, theme.folder).toEqual([theme.folder === 'glossy-2003' ? 'fill' : 'regular']);
    }
  });
}
