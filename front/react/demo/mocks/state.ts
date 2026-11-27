// Estado en memoria del backend falso. Se genera una vez desde el conjunto de datos determinista
// y se puede reiniciar (`resetDataset`) entre pruebas o desde la propia demostración.
import { cloneDataset, generateDataset, type Dataset } from '../data/dataset.mjs';
import credentials from '../data/credenciales-demo.json';

export interface DemoUser {
  username: string;
  password: string;
  displayName: string;
  userId: string;
  mustChangePassword: boolean;
  isGlobalAdmin: boolean;
  permissions: string[];
}

export interface Session {
  username: string;
  /** Instante (ms desde la época) en que caduca el token de acceso. */
  expiresAt: number;
}

export interface MockOptions {
  /** Exige `Authorization: Bearer` y permisos (por omisión sí en el navegador, no en nodo). */
  requireAuth: boolean;
  /** Latencia simulada en ms; en nodo 0. Con valor fijo es determinista; en el navegador, por ruta. */
  latencyMs: number | 'auto';
  /** Simula una empresa que exige aprobación al bloquear: las órdenes responden 202. */
  requireApproval: boolean;
}

export interface MockState {
  data: Dataset;
  users: Map<string, DemoUser>;
  /** Contraseñas cambiadas durante la sesión de demostración. */
  passwords: Map<string, string>;
  accessTokens: Map<string, Session>;
  refreshTokens: Map<string, string>;
  options: MockOptions;
  /** Contador para ids de altas y tokens: determinista mientras no se reinicie. */
  counter: number;
}

const pristine = generateDataset();

function createUsers(): Map<string, DemoUser> {
  const map = new Map<string, DemoUser>();
  for (const user of credentials.users) map.set(user.username, { ...user });
  return map;
}

export const mock: MockState = {
  data: cloneDataset(pristine),
  users: createUsers(),
  passwords: new Map(),
  accessTokens: new Map(),
  refreshTokens: new Map(),
  options: { requireAuth: true, latencyMs: 'auto', requireApproval: false },
  counter: 0,
};

/** Devuelve el conjunto de datos y las sesiones al estado inicial. No toca las opciones. */
export function resetDataset(): void {
  mock.data = cloneDataset(pristine);
  mock.users = createUsers();
  mock.passwords.clear();
  mock.accessTokens.clear();
  mock.refreshTokens.clear();
  mock.counter = 0;
}

/** Cambia opciones del backend falso (autenticación, latencia, aprobaciones). */
export function setMockOptions(options: Partial<MockOptions>): void {
  Object.assign(mock.options, options);
}

/** Siguiente identificador de un alta, con forma de UUID v7 y determinista por contador. */
export function nextId(): string {
  mock.counter += 1;
  const tail = mock.counter.toString(16).padStart(12, '0');
  return `0192a000-0000-7000-8000-${tail}`;
}
