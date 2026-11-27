// Humo contra Karpo Go de verdad (opcional): `KARPO_LOCAL=1 npm run test:e2e -- e2e/local.e2e.ts`.
// Arranca `npm run karpo:local` (cmd/karpo sobre un SQLite temporal, sembrado por HTTP, y Vite con proxy),
// entra con las credenciales de la ejecución y comprueba login real, la lista de instalaciones, un
// cambio con `version` y el árbol de puestos (tabla, árbol y diagrama con el control de vista de la cabecera).
// Sin KARPO_LOCAL=1 la suite se salta (compilar Go tarda).
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { ROOT, installCleanup, isExternalUrl, killAllTracked, launchDemoBrowser, spawnTracked } from './lib/demo.mjs';
import { attr, count, text, visible } from './lib/esperar';

const enabled = process.env.KARPO_LOCAL === '1';
const launched = enabled ? await launchDemoBrowser() : null;

describe.skipIf(!enabled || !launched)('local (Karpo Go real)', () => {
  let front = '';
  let credentials = { username: '', password: '' };
  let stop = () => {};

  beforeAll(async () => {
    installCleanup();
    const local = spawnTracked(process.execPath, [path.join(ROOT, 'scripts', 'karpo-local.mjs')], {
      name: 'karpo-local',
      cwd: ROOT,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    stop = () => {
      local.stop();
      killAllTracked();
    };
    let output = '';
    const url = await new Promise<string>((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`karpo:local no arrancó a tiempo:\n${output}`)), 300_000);
      const onData = (chunk: Buffer) => {
        output += chunk.toString();
        const match = /front en (http:\/\/127\.0\.0\.1:\d+)/.exec(output);
        if (match?.[1]) {
          clearTimeout(timer);
          resolve(match[1]);
        }
      };
      local.child.stdout?.on('data', onData);
      local.child.stderr?.on('data', onData);
      void local.exited.then((code) => {
        clearTimeout(timer);
        reject(new Error(`karpo:local terminó con código ${code}:\n${output}`));
      });
    });
    front = url;
    credentials = JSON.parse(readFileSync(path.join(ROOT, '.karpo-local', 'credenciales.json'), 'utf8'));
  }, 320_000);

  afterAll(async () => {
    await launched?.browser.close().catch(() => undefined);
    stop();
  });

  async function session() {
    if (!launched) throw new Error('sin navegador');
    const context = await launched.browser.newContext({ viewport: { width: 1440, height: 900 }, reducedMotion: 'reduce' });
    const external: string[] = [];
    context.on('request', (request) => {
      if (isExternalUrl(request.url(), front)) external.push(request.url());
    });
    const page = await context.newPage();
    // El usuario de la ejecución local es el del sembrador, no el de la demo: se entra a mano.
    await page.goto(`${front}/?lng=es`);
    await page.locator('input[autocomplete="username"]').fill(credentials.username);
    await page.locator('input[type="password"]').fill(credentials.password);
    await page.locator('button[type="submit"]').click();
    await page.locator('nav[data-th="nav"]').waitFor({ state: 'visible', timeout: 30_000 });
    return { page, context, external };
  }

  it('entra con login real y lista las instalaciones sembradas', async () => {
    const { page, context, external } = await session();
    try {
      await page.getByRole('link', { name: 'Instalaciones' }).first().click();
      await page.locator('[data-kx-ready="true"]').first().waitFor({ timeout: 30_000 });
      await count(page.locator('[role="row"][data-kx-row]')).toBeGreaterThan(5);
      expect(external).toEqual([]);
    } finally {
      await context.close();
    }
  }, 120_000);

  it('un cambio en el detalle viaja con la versión y vuelve guardado', async () => {
    const { page, context } = await session();
    try {
      const writes: { method: string; body: string | null }[] = [];
      page.on('request', (request) => {
        if (request.method() === 'PUT' || request.method() === 'PATCH') writes.push({ method: request.method(), body: request.postData() });
      });
      await page.getByRole('link', { name: 'Instalaciones' }).first().click();
      await page.locator('[data-kx-ready="true"]').first().waitFor({ timeout: 30_000 });
      await page.locator('[role="row"][data-kx-row]').first().locator('[data-kx-col="name"]').dblclick();
      const sheet = page.locator('section[data-th="sheet"]');
      await attr(sheet, 'data-kx-mode').toBe('edit');
      await page.keyboard.press('Control+A');
      await page.keyboard.type('Instalación e2e local');
      await page.keyboard.press('Control+Enter');
      await attr(sheet, 'data-kx-mode').toBe('read');
      await text(sheet.getByRole('heading', { level: 2 })).toBe('Instalación e2e local');
      expect(writes).toHaveLength(1);
      expect(JSON.parse(writes[0]?.body ?? '{}')).toMatchObject({ name: 'Instalación e2e local', version: expect.any(Number) });
    } finally {
      await context.close();
    }
  }, 120_000);

  it('el árbol de puestos de Go se ve como listado y como diagrama', async () => {
    const { page, context } = await session();
    try {
      await page.getByRole('link', { name: /Puestos/ }).first().click();
      // La vista por omisión es la tabla: el control único de la cabecera de la entidad (M-6) pasa al árbol
      // y luego al diagrama; no hay otro interruptor ni en la barra superior ni dentro de la vista.
      const views = page.getByRole('radiogroup', { name: 'Vista' });
      await views.waitFor({ timeout: 30_000 });
      await views.getByRole('radio', { name: 'Árbol' }).click();
      await visible(page.getByRole('tree')).toBe(true);
      await count(page.getByRole('treeitem')).toBeGreaterThan(0);
      await views.getByRole('radio', { name: 'Diagrama' }).click();
      await count(page.locator('.react-flow__node')).toBeGreaterThan(0);
      await count(page.getByRole('group', { name: 'Disposición del organigrama' })).toBe(0);
    } finally {
      await context.close();
    }
  }, 120_000);
});
