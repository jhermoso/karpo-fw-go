// Backend falso para pruebas en Node (vitest): `server` de MSW con todos los handlers de la demo.
//
//   import { server, resetDataset, setMockOptions } from '../../demo/mocks/node';
//   beforeAll(() => server.listen({ onUnhandledFrame: 'error' }));
//   afterEach(() => { server.resetHandlers(); resetDataset(); });
//   afterAll(() => server.close());
//
// En Node, sin latencia y sin exigir sesión (para montar una vista sin pasar por el login). Una
// prueba de sesión o de permisos activa `setMockOptions({ requireAuth: true })`.
import { setupServer } from 'msw/node';
import { handlers } from './handlers';
import { mock, resetDataset, setMockOptions } from './state';

setMockOptions({ requireAuth: false, latencyMs: 0 });

export const server = setupServer(...handlers);

export { handlers, mock, resetDataset, setMockOptions };
