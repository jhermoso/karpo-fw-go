// Marco común de las suites e2e: compila y sirve la demo, lanza el navegador y abre una sesión por
// prueba. Si no hay Edge ni Chrome, las suites se saltan con un mensaje claro (nunca se descarga
// un navegador). Cada prueba termina comprobando que ninguna petición salió del origen de la demo.
import { afterAll, afterEach, beforeAll, describe, expect } from 'vitest';
import {
  ensureDemoBuild,
  launchDemoBrowser,
  mobileOptions,
  openDemoPage,
  signIn,
  spa,
  startDemoServer,
  waitReady,
  type DemoPage,
  type DemoPageOptions,
} from './demo.mjs';

// Para abrir sesiones de teléfono: `api.open({ ...mobileOptions({ width: 360, height: 780 }), path: '/…' })`.
export { mobileOptions };

/**
 * Opciones de `api.open`. Heredan las de `openDemoPage` (`lng`, `dir`, `mode`, `theme`, `locale`, `viewport`,
 * `isMobile`, `hasTouch`): con ventana estrecha el menú es un cajón y `signIn` sólo espera a que exista.
 */
export interface OpenOptions extends DemoPageOptions {
  /** Ruta de la SPA a la que ir tras entrar (por omisión, la de inicio). */
  path?: string;
  /** Selector que indica que la vista está lista (`data-kx-ready`); `false` = no esperar. */
  ready?: string | false;
  /** Usuario de la demo (por omisión `demo`). */
  user?: string;
  /** Entrar con sesión (por omisión sí). Sin ella, sólo se abre la página de inicio de sesión. */
  login?: boolean;
}

export interface E2eSession extends DemoPage {
  origin: string;
  /** Peticiones de escritura (POST, PUT, PATCH, DELETE) hechas a la API de la demo. */
  writes: { method: string; url: string; body: unknown }[];
  /** Navega dentro de la SPA y espera a que la vista esté lista. */
  go(target: string, ready?: string | false): Promise<void>;
}

export interface E2eApi {
  readonly origin: string;
  channel(): string;
  open(options?: OpenOptions): Promise<E2eSession>;
}

// Se lanza al cargar el módulo para poder saltar la suite entera (describe.skipIf) si no hay navegador.
const launched = await launchDemoBrowser();

const SKIP_NOTICE =
  'e2e: no hay Microsoft Edge ni Google Chrome instalados (o playwright-core no los puede lanzar): ' +
  'las pruebas se saltan. Estas pruebas no descargan ningún navegador.';

/**
 * Declara una suite e2e contra la demo. `body` recibe la API para abrir sesiones.
 * Las sesiones se cierran solas al terminar cada prueba.
 */
export function e2eSuite(title: string, body: (api: E2eApi) => void): void {
  const state: { origin: string; open: E2eSession[]; stop: (() => Promise<void>) | null } = {
    origin: '',
    open: [],
    stop: null,
  };

  const api: E2eApi = {
    get origin() {
      return state.origin;
    },
    channel: () => launched?.channel ?? '',
    async open(options = {}) {
      if (!launched) throw new Error('el navegador no está disponible');
      const { path, ready, user, login = true, ...pageOptions } = options;
      const demo = await openDemoPage(launched.browser, state.origin, pageOptions);
      const writes: E2eSession['writes'] = [];
      demo.page.on('request', (request) => {
        const method = request.method();
        const url = new URL(request.url());
        // Escrituras de verdad: las búsquedas (POST …/search) y la sesión no cuentan.
        if (!url.pathname.startsWith('/api/') || url.pathname.startsWith('/api/auth/')) return;
        if (url.pathname.endsWith('/search')) return;
        if (method === 'GET' || method === 'HEAD' || method === 'OPTIONS') return;
        let parsed: unknown = request.postData();
        try {
          parsed = parsed ? JSON.parse(String(parsed)) : null;
        } catch {
          /* cuerpo que no es JSON: se deja el texto */
        }
        writes.push({ method, url: url.pathname + url.search, body: parsed });
      });
      const session: E2eSession = {
        ...demo,
        origin: state.origin,
        writes,
        async go(target, selector) {
          await spa(demo.page, target);
          if (selector !== false) {
            // `data-kx-ready` sigue en el DOM de la lista anterior hasta que React confirma la nueva ruta: si la
            // ruta cambia de entidad, se espera antes a la pantalla de la entidad de destino (integración R5).
            const entity = new URL(target, 'http://karpo.local').pathname.split('/')[1];
            if (entity) {
              await demo.page
                .locator(`[data-kx-screen="entity"][data-kx-entity="${entity}"]`)
                .first()
                .waitFor({ state: 'attached', timeout: 5_000 })
                .catch(() => undefined);
            }
            await waitReady(demo.page, selector);
          }
        },
      };
      state.open.push(session);
      if (login) {
        await signIn(demo.page, demo.startUrl, user);
        if (path) await session.go(path, ready);
      } else {
        await demo.page.goto(demo.startUrl);
      }
      return session;
    },
  };

  if (!launched) console.warn(SKIP_NOTICE);

  describe.skipIf(!launched)(title, () => {
    beforeAll(async () => {
      await ensureDemoBuild({ log: (line) => console.info(`e2e: ${line}`) });
      const server = await startDemoServer();
      state.origin = server.origin;
      state.stop = server.stop;
    });

    afterEach(async (context) => {
      const sessions = state.open.splice(0);
      const problems: string[] = [];
      for (const session of sessions) {
        problems.push(...session.external.map((request) => `petición fuera del origen: ${request}`));
        await session.close().catch(() => undefined);
      }
      if (context.task.result?.state === 'skip') return;
      expect(problems, 'ninguna petición puede salir del origen de la demo').toEqual([]);
    });

    afterAll(async () => {
      await launched?.browser.close().catch(() => undefined);
      await state.stop?.().catch(() => undefined);
    });

    body(api);
  });
}
