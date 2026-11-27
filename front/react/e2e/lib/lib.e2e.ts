// Las ayudas de e2e/lib con navegador (R0): una sesión de teléfono entra en la demo. Comprueba que
// `openDemoPage` pasa viewport, isMobile y hasTouch, y que `signIn` no depende de ver el menú.
import { expect, it } from 'vitest';
import { e2eSuite, mobileOptions } from './suite';

e2eSuite('ayudas de e2e/lib: sesión de teléfono', (api) => {
  it('abre la demo a 390x844 con pantalla táctil y entra con el menú como cajón o a la vista', async () => {
    const session = await api.open({ ...mobileOptions() });
    // `innerWidth` no sirve de prueba: una página más ancha que el teléfono ensancha la ventana de visualización.
    expect(session.page.viewportSize()).toEqual({ width: 390, height: 844 });
    const facts = await session.page.evaluate(() => ({
      touchPoints: navigator.maxTouchPoints,
      coarse: window.matchMedia('(pointer: coarse)').matches,
      hover: window.matchMedia('(hover: hover)').matches,
    }));
    expect(facts.touchPoints).toBeGreaterThan(0);
    expect(facts.coarse).toBe(true);
    expect(facts.hover).toBe(false);
    // El menú está en el árbol aunque no se vea (cajón cerrado).
    expect(await session.page.locator('nav[data-th="nav"]').count()).toBeGreaterThan(0);
    expect(session.pageErrors).toEqual([]);
  });

  it('a 360 de ancho también', async () => {
    const session = await api.open({ ...mobileOptions({ width: 360, height: 780 }) });
    expect(session.page.viewportSize()).toEqual({ width: 360, height: 780 });
    expect(await session.page.locator('nav[data-th="nav"]').count()).toBeGreaterThan(0);
  });

  it('sin opciones sigue siendo un escritorio de 1440x900 sin pantalla táctil', async () => {
    const session = await api.open();
    const facts = await session.page.evaluate(() => ({
      width: window.innerWidth,
      coarse: window.matchMedia('(pointer: coarse)').matches,
    }));
    expect(session.page.viewportSize()).toEqual({ width: 1440, height: 900 });
    expect(facts.width).toBe(1440);
    expect(facts.coarse).toBe(false);
    await expect(session.page.locator('nav[data-th="nav"]').isVisible()).resolves.toBe(true);
  });
});
