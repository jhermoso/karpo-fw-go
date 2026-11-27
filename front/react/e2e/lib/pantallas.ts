// Pantallas de la demo y lectura de lo que el usuario recibe de la página, para las pruebas transversales de
// idioma (`i18n.e2e.ts`: ninguna clave sin traducir; `ingles.e2e.ts`: ningún literal español con `lng=en`).
import type { Page } from 'playwright-core';

/** Una pantalla que se visita: ruta de la SPA y selector de «vista lista» (`false` = no esperar). */
export interface Screen {
  path: string;
  ready: string | false;
}

const READY = '[data-kx-ready="true"]';
const TREE_READY = 'section[data-kx-view="tree"][data-kx-ready="true"]';
const MAP_READY = 'section[data-kx-view="map"][data-kx-ready="true"]';

/** Todas las pantallas del front: inicio, cada entidad, sus vistas alternativas, el alta y las páginas de error. */
export const SCREENS: readonly Screen[] = [
  { path: '/', ready: false },
  { path: '/facility', ready: READY },
  { path: '/work-center', ready: READY },
  { path: '/product', ready: READY },
  { path: '/supplier', ready: READY },
  { path: '/carrier', ready: READY },
  { path: '/asset', ready: READY },
  { path: '/country', ready: READY },
  { path: '/boundary', ready: READY },
  { path: '/holiday', ready: READY },
  { path: '/position', ready: READY },
  { path: '/position?view=tree&layout=list', ready: TREE_READY },
  { path: '/position?view=tree&layout=diagram', ready: TREE_READY },
  { path: '/facility?view=tree&layout=list', ready: TREE_READY },
  { path: '/facility?view=map&level=world', ready: MAP_READY },
  { path: '/facility?view=map&level=country&code=ES', ready: MAP_READY },
  { path: '/facility?view=map&level=region&code=ES30', ready: MAP_READY },
  { path: '/facility?view=map&level=city&code=ES300&layout=list', ready: MAP_READY },
  { path: '/product/nuevo', ready: 'section[data-th="sheet"]' },
  { path: '/no-existe', ready: false },
  { path: '/403', ready: false },
];

/** Atributos con texto para el usuario o para su lector de pantalla. */
export const TEXT_ATTRIBUTES = ['aria-label', 'title', 'placeholder', 'alt', 'aria-description', 'aria-roledescription'] as const;

/** Todo lo que el usuario (o su lector de pantalla) recibe de la página: título, texto visible y atributos accesibles. */
export async function pageTexts(page: Page): Promise<string[]> {
  return page.evaluate((attributes) => {
    const out: string[] = [document.title, document.body.innerText];
    const selector = attributes.map((name) => `[${name}]`).join(', ');
    for (const el of document.querySelectorAll(selector)) {
      for (const name of attributes) {
        const value = el.getAttribute(name);
        if (value) out.push(value);
      }
    }
    return out;
  }, TEXT_ATTRIBUTES);
}
