// Lanzador del navegador instalado (Edge o Chrome) con playwright-core.
// No descarga navegadores: usa los del sistema con `channel`. Si no hay ninguno, devuelve null.

/** Canales probados por orden. */
export const CHANNELS = Object.freeze(['msedge', 'chrome']);

/** WebGL sin GPU (mapas en las capturas y las pruebas e2e). */
export const BROWSER_ARGS = Object.freeze(['--enable-unsafe-swiftshader']);

/**
 * Lanza el primer navegador disponible y devuelve `{ browser, channel }`, o `null`.
 *
 * @param {{ headless?: boolean, args?: string[], channels?: readonly string[],
 *           chromium?: { launch(options: object): Promise<any> } }} [options]
 *   `chromium` permite inyectar un doble en las pruebas.
 */
export async function launchBrowserInfo(options = {}) {
  const { headless = true, args = [], channels = CHANNELS } = options;
  let { chromium } = options;
  if (!chromium) {
    try {
      ({ chromium } = await import('playwright-core'));
    } catch {
      return null;
    }
  }
  for (const channel of channels) {
    try {
      const browser = await chromium.launch({
        channel,
        headless,
        args: [...BROWSER_ARGS, ...args],
      });
      return { browser, channel };
    } catch {
      // Ese navegador no está instalado: se prueba el siguiente.
    }
  }
  return null;
}

/** Como `launchBrowserInfo`, pero devuelve sólo el navegador (o `null`). */
export async function launchBrowser(options = {}) {
  const info = await launchBrowserInfo(options);
  return info ? info.browser : null;
}
