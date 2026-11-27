// Ayudas compartidas por las pruebas e2e (vitest) y por scripts/capturas.mjs:
//   - construir la demo (`vite build --mode demo`) y servirla con `vite preview` en un puerto libre;
//   - lanzar Edge o Chrome con playwright-core (nunca se descarga un navegador);
//   - abrir una sesión de navegador con registro de peticiones: toda petición que salga del origen
//     de la demo se anota y la prueba (o el script) falla (soberanía: ninguna URL de terceros);
//   - iniciar sesión con el usuario de la demo y navegar dentro de la SPA.
//
// La demo en modo `demo` guarda su estado y sus sesiones en la memoria de la página (MSW en el
// navegador): recargar la página cierra la sesión. Por eso se entra una vez y se navega con
// `spa()` (history.pushState + popstate), que no recarga.
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { launchBrowserInfo } from '../../scripts/lib/navegador.mjs';
import { freePort, installCleanup, killAllTracked, spawnTracked, waitForUrl } from '../../scripts/lib/procesos.mjs';

// Se reexportan para las suites en TypeScript (esos módulos no llevan declaraciones de tipos).
export { installCleanup, killAllTracked, spawnTracked };

export const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
export const DEMO_DIST = path.join(ROOT, 'demo', 'dist');
export const VIEWPORT = Object.freeze({ width: 1440, height: 900 });

/** Ancho a partir del cual el menú lateral está siempre a la vista; por debajo es un cajón (R4, B-5). */
const NAV_ALWAYS_VISIBLE_WIDTH = 1280;

/**
 * Opciones de `openDemoPage` para un teléfono: ventana pequeña, agente de móvil y pantalla táctil.
 * Con ella el menú es un cajón y `signIn` no espera a verlo (sólo a que esté en el árbol).
 *
 * @param {{ width?: number, height?: number }} [size] por omisión 390x844 (los de B-5 son 360 y 390)
 */
export function mobileOptions(size = {}) {
  const { width = 390, height = 844 } = size;
  return { viewport: { width, height }, isMobile: true, hasTouch: true };
}

const VITE_BIN = path.join(ROOT, 'node_modules', 'vite', 'bin', 'vite.js');
const SKIPPED_DIRS = new Set(['node_modules', 'dist', '.git', '.karpo-local', 'coverage']);

/** Fecha de modificación más reciente de lo que alimenta la compilación de la demo. */
function newestSourceTime() {
  let newest = 0;
  const walk = (dir) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      if (entry.isDirectory()) {
        if (!SKIPPED_DIRS.has(entry.name)) walk(path.join(dir, entry.name));
      } else {
        newest = Math.max(newest, statSync(path.join(dir, entry.name)).mtimeMs);
      }
    }
  };
  for (const folder of ['src', 'demo', 'build']) walk(path.join(ROOT, folder));
  for (const file of ['package.json', 'vite.config.ts']) newest = Math.max(newest, statSync(path.join(ROOT, file)).mtimeMs);
  return newest;
}

/** ¿Hay que volver a compilar la demo? (no existe, o hay fuentes más nuevas que `demo/dist`) */
export function demoBuildIsStale() {
  const index = path.join(DEMO_DIST, 'index.html');
  const manifest = path.join(DEMO_DIST, 'config', 'manifest.json');
  if (!existsSync(index) || !existsSync(manifest)) return true;
  return newestSourceTime() > statSync(index).mtimeMs;
}

/**
 * Compila la demo (`vite build --mode demo`) en `demo/dist`.
 *
 * @param {{ force?: boolean, log?: (line: string) => void }} [options]
 * @returns {Promise<boolean>} true si compiló, false si ya estaba al día
 */
export async function ensureDemoBuild(options = {}) {
  const { force = false, log = () => {} } = options;
  if (!force && !demoBuildIsStale()) return false;
  installCleanup();
  log('compilando la demo (vite build --mode demo)…');
  const build = spawnTracked(process.execPath, [VITE_BIN, 'build', '--mode', 'demo'], {
    name: 'vite-build',
    cwd: ROOT,
    // Bajo vitest `NODE_ENV` es «test» y Vite compilaría React en desarrollo (el doble de bytes y avisos que el
    // paquete publicado no tiene): se compila como `npm run build:demo`, en producción (integración R5).
    env: { ...process.env, NODE_ENV: 'production' },
    stdio: 'ignore',
  });
  const code = await build.exited;
  if (code !== 0) throw new Error(`vite build --mode demo terminó con código ${code}`);
  return true;
}

/**
 * Sirve `demo/dist` con `vite preview` en un puerto libre (nunca el 55432).
 *
 * @param {{ preferredPort?: number }} [options]
 * @returns {Promise<{ origin: string, port: number, stop: () => Promise<void> }>}
 */
export async function startDemoServer(options = {}) {
  const { preferredPort = 4300 } = options;
  installCleanup();
  let lastError;
  for (let attempt = 0; attempt < 5; attempt += 1) {
    const port = await freePort({ preferred: preferredPort + attempt * 7, attempts: 200 });
    const server = spawnTracked(
      process.execPath,
      [VITE_BIN, 'preview', '--mode', 'demo', '--host', '127.0.0.1', '--port', String(port), '--strictPort'],
      { name: 'vite-preview', cwd: ROOT, stdio: 'ignore' },
    );
    const origin = `http://127.0.0.1:${port}`;
    try {
      await Promise.race([
        waitForUrl(`${origin}/config/manifest.json`, { timeoutMs: 30_000 }),
        server.exited.then((exitCode) => {
          throw new Error(`vite preview terminó con código ${exitCode}`);
        }),
      ]);
      return {
        origin,
        port,
        stop: async () => {
          server.stop();
          await Promise.race([server.exited, new Promise((resolve) => setTimeout(resolve, 5_000))]);
        },
      };
    } catch (error) {
      lastError = error;
      server.stop();
    }
  }
  throw lastError instanceof Error ? lastError : new Error('no se pudo arrancar vite preview');
}

/**
 * Lanza el navegador instalado (Edge y, si no, Chrome) con WebGL por software.
 * @returns {Promise<{ browser: import('playwright-core').Browser, channel: string, version: string } | null>}
 */
export async function launchDemoBrowser() {
  const info = await launchBrowserInfo();
  if (!info) return null;
  return { browser: info.browser, channel: info.channel, version: info.browser.version() };
}

/** Credenciales del usuario de la demo (valores de prueba de la propia demo). */
export function readDemoCredentials(username = 'demo') {
  const file = path.join(ROOT, 'demo', 'data', 'credenciales-demo.json');
  const parsed = JSON.parse(readFileSync(file, 'utf8'));
  const user = parsed.users.find((entry) => entry.username === username);
  if (!user) throw new Error(`credenciales-demo.json: no hay usuario «${username}»`);
  return { username: user.username, password: user.password };
}

/** Nombres de los espacios de nombres de i18n (ficheros de src/locales/es). */
export function readNamespaces() {
  return readdirSync(path.join(ROOT, 'src', 'locales', 'es'))
    .filter((file) => file.endsWith('.json'))
    .map((file) => file.replace(/\.json$/, ''));
}

/** ¿Esta URL sale del origen de la demo? (`data:`, `blob:` y `about:` no son red.) */
export function isExternalUrl(url, origin) {
  let parsed;
  try {
    parsed = new URL(url);
  } catch {
    return true;
  }
  if (parsed.protocol === 'data:' || parsed.protocol === 'blob:' || parsed.protocol === 'about:') return false;
  return parsed.origin !== origin;
}

/**
 * Abre un contexto y una página de la demo con registro de peticiones.
 *
 * @param {import('playwright-core').Browser} browser
 * @param {string} origin origen de la demo (`http://127.0.0.1:PUERTO`)
 * @param {{ lng?: string, dir?: 'ltr' | 'rtl', mode?: 'light' | 'dark', theme?: string,
 *           viewport?: { width: number, height: number }, locale?: string,
 *           isMobile?: boolean, hasTouch?: boolean }} [options]
 *   `lng` y `dir` viajan como `?lng=&dir=` en la primera carga (así los lee la app); `mode` y
 *   `theme` se guardan como preferencia del usuario antes de arrancar y se emula el esquema de
 *   color del navegador con el mismo valor. `viewport`, `isMobile` y `hasTouch` emulan un
 *   teléfono (véase `mobileOptions`): sin ellos, escritorio de 1440x900 sin pantalla táctil.
 */
export async function openDemoPage(browser, origin, options = {}) {
  const { lng = 'es', dir, mode = 'light', theme = 'modern-flat', viewport = VIEWPORT, isMobile, hasTouch } = options;
  const context = await browser.newContext({
    viewport,
    ...(isMobile === undefined ? {} : { isMobile }),
    ...(hasTouch === undefined ? {} : { hasTouch }),
    deviceScaleFactor: 1,
    reducedMotion: 'reduce',
    colorScheme: mode,
    locale: options.locale ?? (lng === 'en' ? 'en-GB' : 'es-ES'),
    serviceWorkers: 'allow',
  });
  await context.addInitScript(
    ({ themeName, modeName }) => {
      try {
        localStorage.setItem('karpo.theme', themeName);
        localStorage.setItem('karpo.theme.mode', modeName);
      } catch {
        /* sin almacenamiento: se arranca con las preferencias por omisión */
      }
    },
    { themeName: theme, modeName: mode },
  );
  /** @type {string[]} */
  const external = [];
  /** @type {string[]} */
  const pageErrors = [];
  context.on('request', (request) => {
    if (isExternalUrl(request.url(), origin)) external.push(`${request.method()} ${request.url()}`);
  });
  const page = await context.newPage();
  page.on('pageerror', (error) => pageErrors.push(error.message));
  const query = new URLSearchParams({ lng });
  if (dir) query.set('dir', dir);
  return {
    page,
    context,
    external,
    pageErrors,
    startUrl: `${origin}/?${query.toString()}`,
    close: () => context.close(),
  };
}

/**
 * Entra en la demo: abre la página de inicio de sesión, rellena el formulario como un usuario y
 * espera al menú. Con la sesión iniciada, navegar con `spa()`.
 *
 * El menú puede ser un cajón cerrado (ventana estrecha): entonces basta con que esté en el árbol
 * (`attached`). En una ventana ancha se exige además que se vea.
 */
export async function signIn(page, startUrl, username = 'demo') {
  const { password } = readDemoCredentials(username);
  await page.goto(startUrl);
  const form = page.locator('form').first();
  await form.waitFor({ state: 'visible', timeout: 30_000 });
  await form.locator('input[autocomplete="username"]').fill(username);
  await form.locator('input[type="password"]').fill(password);
  await form.locator('button[type="submit"]').click();
  const nav = page.locator('nav[data-th="nav"]');
  await nav.waitFor({ state: 'attached', timeout: 30_000 });
  const width = page.viewportSize()?.width ?? VIEWPORT.width;
  if (width >= NAV_ALWAYS_VISIBLE_WIDTH) await nav.waitFor({ state: 'visible', timeout: 30_000 });
}

/** Navega dentro de la SPA sin recargar (history.pushState y popstate), conservando la sesión. */
export async function spa(page, target) {
  await page.evaluate((url) => {
    window.history.pushState({}, '', url);
    window.dispatchEvent(new PopStateEvent('popstate'));
  }, target);
}

/** Espera al indicador `data-kx-ready="true"` de la vista en pantalla. */
export async function waitReady(page, selector = '[data-kx-ready="true"]', timeout = 30_000) {
  await page.locator(selector).first().waitFor({ state: 'attached', timeout });
}

/**
 * Centro de la mayor mancha continua de un color en una región de la página, medido sobre una
 * captura real (un lienzo WebGL no se puede leer de otra forma). Sirve para hacer clic donde el
 * usuario ve el color de datos de la leyenda. Las manchas dentro de `exclude` (la leyenda y los
 * rótulos que se superponen al mapa) no cuentan.
 *
 * @param {import('playwright-core').Page} page
 * @param {string} rgb color CSS tal como lo devuelve getComputedStyle (rgb, oklab…)
 * @param {{ x: number, y: number, width: number, height: number }} region en píxeles de página
 * @param {{ x: number, y: number, width: number, height: number }[]} [exclude]
 * @returns {Promise<{ x: number, y: number } | null>}
 */
export async function findColorCenter(page, rgb, region, exclude = []) {
  const png = await page.screenshot({ clip: region });
  return page.evaluate(
    async ({ base64, color, area, skip }) => {
      const match = /rgba?\((\d+),\s*(\d+),\s*(\d+)/.exec(color);
      if (!match) return null;
      const target = [Number(match[1]), Number(match[2]), Number(match[3])];
      const bytes = Uint8Array.from(atob(base64), (char) => char.charCodeAt(0));
      const bitmap = await createImageBitmap(new Blob([bytes], { type: 'image/png' }));
      const canvas = document.createElement('canvas');
      canvas.width = bitmap.width;
      canvas.height = bitmap.height;
      const context = canvas.getContext('2d');
      if (!context) return null;
      context.drawImage(bitmap, 0, 0);
      const { data, width, height } = context.getImageData(0, 0, bitmap.width, bitmap.height);
      const tolerance = 6;
      const inside = (px, py) => {
        const x = px + area.x;
        const y = py + area.y;
        return skip.some((r) => x >= r.x && x <= r.x + r.width && y >= r.y && y <= r.y + r.height);
      };
      const matches = (index) =>
        Math.abs((data[index] ?? 0) - (target[0] ?? 0)) <= tolerance &&
        Math.abs((data[index + 1] ?? 0) - (target[1] ?? 0)) <= tolerance &&
        Math.abs((data[index + 2] ?? 0) - (target[2] ?? 0)) <= tolerance;
      const seen = new Uint8Array(width * height);
      let best = null;
      for (let start = 0; start < width * height; start += 1) {
        if (seen[start] || !matches(start * 4) || inside(start % width, Math.floor(start / width))) continue;
        // Relleno por inundación de una mancha continua.
        const stack = [start];
        seen[start] = 1;
        let count = 0;
        let sumX = 0;
        let sumY = 0;
        while (stack.length > 0) {
          const current = stack.pop();
          const x = current % width;
          const y = Math.floor(current / width);
          count += 1;
          sumX += x;
          sumY += y;
          for (const [dx, dy] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) {
            const nx = x + dx;
            const ny = y + dy;
            if (nx < 0 || ny < 0 || nx >= width || ny >= height) continue;
            const next = ny * width + nx;
            if (seen[next] || !matches(next * 4) || inside(nx, ny)) continue;
            seen[next] = 1;
            stack.push(next);
          }
        }
        if (!best || count > best.count) best = { count, x: sumX / count, y: sumY / count };
      }
      if (!best || best.count < 20) return null;
      return { x: area.x + best.x, y: area.y + best.y };
    },
    { base64: png.toString('base64'), color: rgb, area: region, skip: exclude },
  );
}
