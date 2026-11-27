// Reúne los handlers de demo/mocks/handlers/*.ts (uno por área) con `import.meta.glob`: añadir un
// fichero nuevo a esa carpeta basta, sin tocar éste.
import type { HttpHandler } from 'msw';

const modules = import.meta.glob<{ handlers: HttpHandler[] }>('./handlers/*.ts', { eager: true });

export const handlers: HttpHandler[] = Object.keys(modules)
  .sort()
  .flatMap((key) => modules[key]?.handlers ?? []);
