// Backend falso en el navegador: service worker de MSW (demo/public/mockServiceWorker.js).
// Con sesión obligatoria y latencia pequeña determinista por ruta. Lo carga demo/main.tsx.
import { setupWorker } from 'msw/browser';
import { handlers } from './handlers';
import { resetDataset, setMockOptions } from './state';

setMockOptions({ requireAuth: true, latencyMs: 'auto' });

export const worker = setupWorker(...handlers);

/** Arranca el worker; lo que no es API (temas, geodatos, configuración, código) pasa sin tocar. */
export function startMockWorker(): Promise<unknown> {
  return worker.start({ onUnhandledFrame: 'bypass', quiet: true });
}

export { resetDataset, setMockOptions };
