// Comprobaciones con reintento para Playwright dentro de vitest (playwright-core no trae `expect`).
// Cada una devuelve `expect.poll(...)`: se encadena con `.toBe(...)`, `.toMatch(...)`, etc., y vuelve a
// medir hasta que se cumple o se agota el tiempo (la vista tarda en reaccionar: no se usan pausas fijas).
import type { Locator, Page } from 'playwright-core';
import { expect } from 'vitest';

const TIMEOUT = 10_000;
const INTERVAL = 100;

/** Reintenta `read` hasta que la aserción encadenada se cumple. */
export function poll<T>(read: () => Promise<T>, timeout = TIMEOUT) {
  return expect.poll(read, { timeout, interval: INTERVAL });
}

/** Valor de un atributo (`''` si no existe o el elemento aún no está). */
export const attr = (locator: Locator, name: string) =>
  poll(async () => (await locator.first().getAttribute(name, { timeout: 500 }).catch(() => null)) ?? '');

/** ¿Está visible? */
export const visible = (locator: Locator) => poll(() => locator.first().isVisible());

/** Número de elementos. */
export const count = (locator: Locator) => poll(() => locator.count());

/** Texto del primer elemento (`''` si no existe). */
export const text = (locator: Locator) =>
  poll(async () => (await locator.first().textContent({ timeout: 500 }).catch(() => null)) ?? '');

/** Valor de un campo de formulario. */
export const value = (locator: Locator) => poll(() => locator.first().inputValue({ timeout: 500 }).catch(() => null));

/** ¿Tiene el foco? */
export const focused = (locator: Locator) =>
  poll(() => locator.first().evaluate((el) => el === document.activeElement, undefined, { timeout: 500 }).catch(() => false));

/** ¿Está habilitado? */
export const enabled = (locator: Locator) => poll(() => locator.first().isEnabled({ timeout: 500 }).catch(() => false));

/** Ruta y consulta actuales de la página (`/product?x=1`). */
export const route = (page: Page) => poll(() => page.evaluate(() => window.location.pathname + window.location.search));
