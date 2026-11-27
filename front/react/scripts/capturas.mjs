// `npm run capturas` (PLAN §7 y PLAN-RONDA-2 R5): 41 PNG en docs/capturas/ y su README.md. Las 9 escenas en las
// variantes claro, oscuro, rtl e inglés (36) y cinco escenas en la variante de móvil (5).
//
//   1. compila la demo (`vite build --mode demo`) y la sirve con `vite preview` en un puerto libre;
//   2. lanza Edge y, si no, Chrome con playwright-core (WebGL por software; no descarga nada);
//   3. para cada escena y variante abre una sesión limpia (1440×900, o 390×844 en móvil, escala 1, movimiento
//      reducido), entra como el usuario de la demo, prepara el estado con interacciones reales (doble clic,
//      teclado), espera a `data-kx-ready` y a que la imagen deje de cambiar, y guarda
//      docs/capturas/<escena>-<variante>.png;
//   4. falla si falta alguna captura o si alguna petición salió del origen de la demo;
//   5. para el servidor y el navegador siempre, también si algo falla.
//
// Variantes: claro (modern-flat, light, es), oscuro (dark, es), rtl (light, ar: árabe real, sin `?dir=`; D-R38),
// ingles (light, en, LTR) y movil (390×844, isMobile, hasTouch, es, claro; sólo cinco escenas).
import { mkdirSync, readdirSync, rmSync, statSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import {
  ROOT,
  VIEWPORT,
  ensureDemoBuild,
  launchDemoBrowser,
  mobileOptions,
  openDemoPage,
  signIn,
  spa,
  startDemoServer,
  waitReady,
} from '../e2e/lib/demo.mjs';

export const OUT_DIR = path.join(ROOT, 'docs', 'capturas');

/** Escenas que también salen en la variante de móvil. */
export const MOBILE_SCENES = Object.freeze([
  'cuadricula-editable',
  'detalle-edicion',
  'organigrama-listado',
  'mapa-mundo',
  'ayuda-recorrido',
]);

/**
 * Variantes. `scenes` (opcional) limita las escenas de la variante; sin él salen todas.
 * El tamaño de la ventana es `options.viewport` (por omisión 1440×900).
 */
export const VARIANTS = Object.freeze([
  { id: 'claro', label: 'Claro', options: { theme: 'modern-flat', mode: 'light', lng: 'es' }, detail: 'modern-flat, modo claro, español' },
  { id: 'oscuro', label: 'Oscuro', options: { theme: 'modern-flat', mode: 'dark', lng: 'es' }, detail: 'modern-flat, modo oscuro, español' },
  {
    id: 'rtl',
    label: 'RTL',
    // Árabe real (D-R38): la dirección sale del idioma, sin `?dir=`; el navegador también en árabe.
    options: { theme: 'modern-flat', mode: 'light', lng: 'ar', locale: 'ar' },
    detail: 'modern-flat, modo claro, árabe (`lng=ar`, `dir=rtl` por el idioma; traducción de demostración sin revisión nativa; los datos de la demo no se traducen)',
  },
  { id: 'ingles', label: 'Inglés', options: { theme: 'modern-flat', mode: 'light', lng: 'en' }, detail: 'modern-flat, modo claro, inglés (LTR), 1440×900' },
  {
    id: 'movil',
    label: 'Móvil',
    options: { theme: 'modern-flat', mode: 'light', lng: 'es', ...mobileOptions({ width: 390, height: 844 }) },
    detail: 'modern-flat, modo claro, español, 390×844 con `isMobile` y `hasTouch` (sólo cinco escenas)',
    scenes: MOBILE_SCENES,
  },
]);

const GRID_READY = '[data-kx-ready="true"]';
const TREE_READY = 'section[data-kx-view="tree"][data-kx-ready="true"]';
const MAP_READY = 'section[data-kx-view="map"][data-kx-ready="true"]';

const row = (page, text) => page.locator('[role="row"][data-kx-row]', { hasText: text });
const cell = (page, text, column) => row(page, text).locator(`[data-kx-col="${column}"]`);
const helpButton = (page) => page.locator('button[data-kx-help="help.button"]');

/**
 * Puestos de la demo por idioma: el servidor de la demo traduce los títulos con `Accept-Language` sólo a es y en;
 * los datos no se traducen al árabe (D-R38), así que con `ar` salen los del conjunto de datos, en castellano.
 */
const POSITIONS = Object.freeze({
  es: { operations: 'Director/a de Operaciones', technology: 'Director/a de Tecnología', search: 'Almacén' },
  en: { operations: 'Operations Director', technology: 'Technology Director', search: 'Warehouse' },
});
const positionsFor = (variant) => POSITIONS[variant.options.lng === 'en' ? 'en' : 'es'];

/** ¿Se escribe de derecha a izquierda? Por `?dir=rtl` o por el idioma (árabe). */
export const isRtl = (variant) => variant.options.dir === 'rtl' || variant.options.lng === 'ar';
const escapeRegExp = (text) => text.replace(/[.*+?^${}()|[\]\\/]/g, '\\$&');

/** Espera a que la página deje de cambiar (dos capturas seguidas idénticas): mapas, disposición, fuentes. */
async function settle(page, { timeout = 20_000, interval = 300 } = {}) {
  await page.evaluate(() => document.fonts?.ready);
  const deadline = Date.now() + timeout;
  let previous = null;
  while (Date.now() < deadline) {
    const current = await page.screenshot();
    if (previous && current.equals(previous)) return;
    previous = current;
    await page.waitForTimeout(interval);
  }
  throw new Error('la página no se estabilizó a tiempo');
}

/** Escribe sobre la celda activa (F2, seleccionar todo, texto, Intro). */
async function editCell(page, target, text, { confirm = true } = {}) {
  await target.click();
  await page.keyboard.press('F2');
  await page.keyboard.press('Control+A');
  await page.keyboard.type(text);
  if (confirm) await page.keyboard.press('Enter');
}

/**
 * Escenas del PLAN §7. `prepare(page, variant)` deja la pantalla en el estado de la captura con
 * interacciones reales; `ready` es el selector de «vista lista» (`data-kx-ready`) y `readyAfter`, si lo hay, el de después de preparar; `hover(page)`
 * devuelve el elemento sobre el que queda el puntero (por omisión, una esquina vacía).
 */
export const SCENES = Object.freeze([
  {
    id: 'cuadricula-editable',
    title: 'Cuadrícula editable',
    shows: 'Productos con tres celdas sucias, una inválida con su error, una en edición y la barra de cambios por lotes (en el móvil, sólo con las columnas que caben y sin la celda inválida)',
    path: '/product',
    ready: GRID_READY,
    // El error de una celda inválida se muestra al pasar el puntero o al enfocarla.
    hover: (page) => cell(page, 'SKU-0004', 'basePrice'),
    async prepare(page) {
      // Con poco ancho (móvil) la rejilla oculta las columnas de menor prioridad (R3-3): sólo se edita lo que se ve.
      const shown = async (column) => (await page.locator(`[role="columnheader"][data-kx-col="${column}"]`).count()) > 0;
      await editCell(page, cell(page, 'SKU-0001', 'name'), 'Correa M10 reforzada');
      if (await shown('uom')) await editCell(page, cell(page, 'SKU-0002', 'uom'), 'kg');
      else await editCell(page, cell(page, 'SKU-0002', 'name'), 'Sensor industrial XL');
      if (await shown('basePrice')) await editCell(page, cell(page, 'SKU-0003', 'basePrice'), '129');
      else await editCell(page, cell(page, 'SKU-0003', 'name'), 'Rodamiento galvanizado XL');
      if (await shown('basePrice')) await editCell(page, cell(page, 'SKU-0004', 'basePrice'), 'abc');
      // La celda inválida conserva su error; otra celda queda abierta, a medio escribir.
      await editCell(page, cell(page, 'SKU-0006', 'name'), 'Junta compacto', { confirm: false });
    },
  },
  {
    id: 'detalle-edicion',
    title: 'Detalle en edición',
    shows: 'Instalación abierta con doble clic desde la lista: un campo cambiado y un error de campo',
    path: '/facility',
    ready: GRID_READY,
    // En el móvil el detalle es una página completa y la lista deja de estar en pantalla.
    readyAfter: 'section[data-th="sheet"][data-kx-mode="edit"]',
    async prepare(page) {
      await cell(page, 'Sala Atlas', 'name').dblclick();
      const sheet = page.locator('section[data-th="sheet"][data-kx-mode="edit"]');
      await sheet.waitFor({ state: 'visible', timeout: 20_000 });
      // Un campo cambiado (se desmarca «Activa») y un error de campo (el nombre obligatorio, vacío).
      await sheet.locator('input[type="checkbox"][id$="-f-active"]').uncheck();
      const name = sheet.locator('input[id$="-f-name"]');
      await name.fill('');
      await page.keyboard.press('Control+Enter');
      await sheet.locator('[aria-invalid="true"]').first().waitFor({ state: 'visible' });
    },
  },
  {
    id: 'organigrama-listado',
    title: 'Organigrama · listado',
    shows: 'Árbol de puestos con dos ramas desplegadas, una plegada y una búsqueda activa',
    path: '/position?view=tree&layout=list',
    ready: TREE_READY,
    async prepare(page, variant) {
      const names = positionsFor(variant);
      const tree = page.getByRole('tree');
      // En RTL la flecha que despliega es la izquierda.
      const expandKey = isRtl(variant) ? 'ArrowLeft' : 'ArrowRight';
      for (const title of [names.operations, names.technology]) {
        const item = tree.getByRole('treeitem', { name: new RegExp(`^${escapeRegExp(title)}`) });
        await item.focus();
        await page.keyboard.press(expandKey);
      }
      await page.locator('[data-kx-help="tree.search"] input[type="search"], input[type="search"][aria-label]').last().fill(names.search);
      await page.locator('mark').first().waitFor({ state: 'visible' });
    },
  },
  {
    id: 'organigrama-diagrama',
    title: 'Organigrama · diagrama',
    shows: 'Diagrama a tamaño legible, centrado en la raíz, con una rama desplegada, contadores de descendientes y el minimapa',
    path: '/position?view=tree&layout=diagram',
    ready: TREE_READY,
    async prepare(page, variant) {
      const names = positionsFor(variant);
      await page.locator('.react-flow__node').first().waitFor({ state: 'visible' });
      // En RTL el nombre interpolado va entre aislantes Unicode (U+2068, U+2069).
      const unfold = new RegExp(`(Desplegar la rama de|Expand the branch of|توسيع فرع)[\\s\\u2068]*${escapeRegExp(names.operations)}`);
      await page.getByRole('button', { name: unfold }).click();
      // «Ajustar» alterna entre ver todo y el tamaño legible: dos pulsaciones dejan el encuadre legible con la rama a la vista.
      const fit = page.getByRole('button', { name: /^(Ajustar a la pantalla|Fit to screen|ملاءمة الشاشة)$/ });
      await fit.click();
      await page.waitForTimeout(600);
      await fit.click();
      await page.waitForTimeout(600);
    },
  },
  { id: 'mapa-mundo', title: 'Mapa · mundo', shows: 'Instalaciones por país (coropletas)', path: '/facility?view=map&level=world', ready: MAP_READY },
  { id: 'mapa-pais', title: 'Mapa · país', shows: 'Comunidades autónomas de España', path: '/facility?view=map&level=country&code=ES', ready: MAP_READY },
  { id: 'mapa-region', title: 'Mapa · región', shows: 'Provincias de Cataluña (nivel región): cuatro delimitaciones, una con datos y tres sin dato', path: '/facility?view=map&level=region&code=ES51', ready: MAP_READY },
  { id: 'mapa-ciudad', title: 'Mapa · ciudad', shows: 'Municipios de Madrid con las migas completas', path: '/facility?view=map&level=city&code=ES300', ready: MAP_READY },
  {
    id: 'ayuda-recorrido',
    title: 'Ayuda guiada',
    shows: 'Recorrido de la pantalla de productos con el segundo paso abierto',
    path: '/product',
    ready: GRID_READY,
    async prepare(page) {
      await helpButton(page).click();
      const popover = page.locator('.driver-popover');
      await popover.waitFor({ state: 'visible' });
      await popover.locator('.driver-popover-next-btn').click();
      await page.locator('.driver-popover-progress-text', { hasText: /\b2\b/ }).waitFor({ state: 'visible' });
    },
  },
]);

/** Nombre de fichero de una captura. */
export const fileName = (scene, variant) => `${scene.id}-${variant.id}.png`;

/** Las escenas de una variante (todas, salvo que la variante las limite). */
export const scenesOf = (variant, scenes = SCENES) =>
  variant.scenes ? scenes.filter((scene) => variant.scenes.includes(scene.id)) : scenes;

/** Todas las capturas esperadas: `{ scene, variant, file }`. */
export const expectedCaptures = (scenes = SCENES, variants = VARIANTS) =>
  variants.flatMap((variant) => scenesOf(variant, scenes).map((scene) => ({ scene, variant, file: fileName(scene, variant) })));

/** Tamaño de la ventana de una variante. */
export const viewportOf = (variant) => variant.options.viewport ?? VIEWPORT;

/** Texto del README de las capturas. */
export function renderReadme({ date, browser, version, scenes = SCENES, variants = VARIANTS }) {
  const total = expectedCaptures(scenes, variants).length;
  const size = (variant) => `${viewportOf(variant).width}×${viewportOf(variant).height}`;
  const lines = [
    '# Capturas de la demo',
    '',
    'Generadas por `npm run capturas` (`scripts/capturas.mjs`, PLAN §7). No se editan a mano: se regeneran.',
    '',
    `- **Fecha:** ${date}`,
    `- **Navegador:** ${browser} ${version} (playwright-core, \`channel\`; WebGL por software con \`--enable-unsafe-swiftshader\`)`,
    '- **Ventana:** 1440×900 (390×844 con pantalla táctil en la variante `movil`), escala 1, movimiento reducido (`prefers-reduced-motion: reduce`), fuentes del sistema',
    '- **Árabe:** la variante `rtl` es árabe real (`lng=ar`); es una traducción de demostración **sin revisión nativa** (Q-5 sigue abierta) y los datos de la demo (nombres de puestos, delimitaciones) no se traducen',
    '- **Datos:** demo determinista (MSW en el navegador); ninguna petición salió del origen de la demo',
    '',
    '## Variantes',
    '',
    ...variants.map((variant) => `- \`${variant.id}\` (${size(variant)}, ${scenesOf(variant, scenes).length} escenas): ${variant.detail}`),
    '',
    '## Capturas',
    '',
    `| Escena | Qué enseña | ${variants.map((variant) => variant.label).join(' | ')} |`,
    `|---|---|${variants.map(() => '---').join('|')}|`,
    ...scenes.map((scene) => {
      const cells = variants.map((variant) =>
        scenesOf(variant, scenes).includes(scene) ? `[\`${fileName(scene, variant)}\`](${fileName(scene, variant)})` : '—',
      );
      return `| ${scene.title} | ${scene.shows} | ${cells.join(' | ')} |`;
    }),
    '',
    `Total: ${total} imágenes.`,
    '',
  ];
  return lines.join('\n');
}

/** Captura una escena en una variante con una sesión limpia. */
async function capture({ browser, origin, scene, variant, outFile, log }) {
  const session = await openDemoPage(browser, origin, variant.options);
  try {
    await signIn(session.page, session.startUrl);
    await spa(session.page, scene.path);
    await waitReady(session.page, scene.ready);
    if (scene.prepare) await scene.prepare(session.page, variant);
    // El puntero fuera de la acción (salvo que la escena pida otra cosa): sin resaltes accidentales.
    if (scene.hover && !variant.options.hasTouch) await scene.hover(session.page).hover();
    else {
      const { width, height } = viewportOf(variant);
      await session.page.mouse.move(width - 2, height - 2);
    }
    await waitReady(session.page, scene.readyAfter ?? scene.ready);
    await settle(session.page);
    await session.page.screenshot({ path: outFile });
    log(`  ${path.relative(ROOT, outFile)}`);
    return { external: session.external, pageErrors: session.pageErrors };
  } finally {
    await session.close().catch(() => undefined);
  }
}

export async function main({ log = console.log } = {}) {
  const launched = await launchDemoBrowser();
  if (!launched) {
    throw new Error('No hay Microsoft Edge ni Google Chrome instalados: playwright-core no puede lanzar ningún navegador (no se descarga ninguno).');
  }
  let server;
  const problems = [];
  try {
    await ensureDemoBuild({ force: true, log });
    server = await startDemoServer();
    log(`demo en ${server.origin} · ${launched.channel} ${launched.version}`);
    mkdirSync(OUT_DIR, { recursive: true });
    // Se borran las capturas anteriores: lo que quede en la carpeta es lo que acaba de salir.
    for (const file of readdirSync(OUT_DIR)) if (file.endsWith('.png')) rmSync(path.join(OUT_DIR, file));

    for (const variant of VARIANTS) {
      log(`variante ${variant.id}`);
      for (const scene of scenesOf(variant)) {
        const outFile = path.join(OUT_DIR, fileName(scene, variant));
        const result = await capture({ browser: launched.browser, origin: server.origin, scene, variant, outFile, log });
        for (const request of result.external) problems.push(`${fileName(scene, variant)}: petición fuera del origen: ${request}`);
        for (const message of result.pageErrors) problems.push(`${fileName(scene, variant)}: error de la página: ${message}`);
      }
    }

    const expected = expectedCaptures().map((capture) => capture.file);
    for (const name of expected) {
      let size = 0;
      try {
        size = statSync(path.join(OUT_DIR, name)).size;
      } catch {
        /* falta */
      }
      if (size < 10_000) problems.push(`${name}: falta o está vacía (${size} bytes)`);
    }
    const onDisk = readdirSync(OUT_DIR).filter((file) => file.endsWith('.png'));
    if (onDisk.length !== expected.length) problems.push(`hay ${onDisk.length} PNG y se esperaban ${expected.length}`);
    if (problems.length > 0) throw new Error(`Capturas con problemas:\n  - ${problems.join('\n  - ')}`);

    const date = new Date().toISOString().slice(0, 10);
    const browserName = launched.channel === 'msedge' ? 'Microsoft Edge' : 'Google Chrome';
    writeFileSync(path.join(OUT_DIR, 'README.md'), renderReadme({ date, browser: browserName, version: launched.version }));
    log(`${expected.length} capturas y README.md en ${path.relative(ROOT, OUT_DIR)}`);
  } finally {
    await launched.browser.close().catch(() => undefined);
    await server?.stop().catch(() => undefined);
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().then(
    () => process.exit(0),
    (error) => {
      console.error(`[capturas] ${error instanceof Error ? error.message : String(error)}`);
      process.exit(1);
    },
  );
}
