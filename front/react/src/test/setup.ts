// Preparación común de las pruebas con DOM (proyectos `components` y `contract`).
// Sólo para pruebas: no entra en el paquete.
import '@testing-library/jest-dom/vitest';
import { cleanup, configure } from '@testing-library/react';
import { afterEach } from 'vitest';
import { preloadAllLocales } from '../i18n/locales';
import { loadExtraIcons } from '../ui/icons';

// Recursos por demanda en la aplicación (D-R37), pero completos en las pruebas de componentes: todos los
// idiomas y espacios de nombres y todos los iconos, cargados antes de la primera prueba. Así `createI18n` y
// `<Icon>` se comportan de forma síncrona y ninguna prueba depende del orden en que llegan los trozos.
await Promise.all([preloadAllLocales(), loadExtraIcons()]);

// Las vistas se cargan en diferido (R4): con varios ficheros de pruebas en paralelo, 1 s se queda corto.
configure({ asyncUtilTimeout: 5_000 });

afterEach(() => {
  cleanup();
});

class ResizeObserverStub {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

class IntersectionObserverStub {
  readonly root = null;
  readonly rootMargin = '';
  readonly scrollMargin = '';
  readonly thresholds: readonly number[] = [];
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
  takeRecords(): IntersectionObserverEntry[] {
    return [];
  }
}

// jsdom no implementa estas API; Radix, TanStack Virtual y los recorridos las necesitan.
if (typeof window !== 'undefined') {
  const target = globalThis as Record<string, unknown>;
  target.ResizeObserver ??= ResizeObserverStub;
  target.IntersectionObserver ??= IntersectionObserverStub;

  window.matchMedia ??= (query: string): MediaQueryList => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    addListener: () => undefined,
    removeListener: () => undefined,
    dispatchEvent: () => false,
  });

  const elementProto = window.HTMLElement.prototype;
  elementProto.scrollIntoView ??= function scrollIntoView() {};
  elementProto.hasPointerCapture ??= () => false;
  elementProto.setPointerCapture ??= () => undefined;
  elementProto.releasePointerCapture ??= () => undefined;
}
