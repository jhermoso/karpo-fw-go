// Utilidades de procesos para los scripts (modo local, capturas, e2e).
// Sin dependencias. Nunca devuelve el puerto 55432 (base de datos viva prohibida por el encargo).
import { spawn, spawnSync } from 'node:child_process';
import { mkdirSync, readFileSync, writeFileSync, existsSync } from 'node:fs';
import net from 'node:net';
import path from 'node:path';

/** Puertos que nunca se entregan ni se usan. */
export const FORBIDDEN_PORTS = Object.freeze(new Set([55432]));

const DEFAULT_HOST = '127.0.0.1';

/**
 * Comprueba de verdad si se puede escuchar en `port` (descarta los rangos que Windows reserva y
 * los puertos ocupados: un `listen` que falla con EACCES o EADDRINUSE descarta el puerto).
 * @param {number} port
 * @param {string} [host]
 * @returns {Promise<boolean>}
 */
export function canListen(port, host = DEFAULT_HOST) {
  return new Promise((resolve) => {
    const server = net.createServer();
    server.unref();
    server.once('error', () => resolve(false));
    server.listen({ port, host, exclusive: true }, () => {
      server.close(() => resolve(true));
    });
  });
}

/** Pide un puerto efímero al sistema operativo (sin garantías todavía). */
function ephemeralPort(host) {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.unref();
    server.once('error', reject);
    server.listen({ port: 0, host }, () => {
      const address = server.address();
      const port = typeof address === 'object' && address ? address.port : 0;
      server.close(() => resolve(port));
    });
  });
}

/**
 * Devuelve un puerto libre en 127.0.0.1, verificado con `listen`.
 * - Con `preferred`, prueba `preferred`, `preferred + 1`, … hasta `attempts` candidatos.
 * - Sin `preferred`, usa puertos efímeros del sistema y los vuelve a verificar de forma explícita.
 * Los puertos de `FORBIDDEN_PORTS` y los de `avoid` se descartan siempre.
 *
 * @param {{ preferred?: number, host?: string, avoid?: number[], attempts?: number,
 *           probe?: (port: number, host: string) => Promise<boolean> }} [options]
 * @returns {Promise<number>}
 */
export async function freePort(options = {}) {
  const { preferred, host = DEFAULT_HOST, avoid = [], attempts = 100, probe = canListen } = options;
  const banned = new Set([...FORBIDDEN_PORTS, ...avoid]);
  for (let i = 0; i < attempts; i += 1) {
    const candidate = preferred === undefined ? await ephemeralPort(host) : preferred + i;
    if (!Number.isInteger(candidate) || candidate < 1 || candidate > 65535) continue;
    if (banned.has(candidate)) continue;
    if (await probe(candidate, host)) return candidate;
  }
  throw new Error(`freePort: sin puerto libre tras ${attempts} intentos`);
}

/** Procesos arrancados por este proceso y todavía vivos. */
const tracked = new Map();

/**
 * Mata un proceso y todo su árbol. En Windows, `taskkill /T /F`; en el resto, el grupo de
 * procesos (si se lanzó con `detached`) y, si no, el proceso.
 *
 * @param {number} pid
 * @param {{ platform?: string,
 *           run?: (cmd: string, args: string[]) => { status: number | null },
 *           signal?: (pid: number, signal: string) => void }} [options]
 * @returns {boolean} true si se pudo enviar la orden
 */
export function killTree(pid, options = {}) {
  const {
    platform = process.platform,
    run = (cmd, args) => spawnSync(cmd, args, { stdio: 'ignore', windowsHide: true }),
    signal = (target, sig) => process.kill(target, sig),
  } = options;
  if (!Number.isInteger(pid) || pid <= 0) return false;
  if (platform === 'win32') {
    const result = run('taskkill', ['/pid', String(pid), '/T', '/F']);
    return result.status === 0;
  }
  try {
    signal(-pid, 'SIGKILL');
    return true;
  } catch {
    try {
      signal(pid, 'SIGKILL');
      return true;
    } catch {
      return false;
    }
  }
}

function readPidFile(file) {
  try {
    const parsed = JSON.parse(readFileSync(file, 'utf8'));
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

function writePidFile(file, entries) {
  mkdirSync(path.dirname(file), { recursive: true });
  writeFileSync(file, `${JSON.stringify(entries, null, 2)}\n`);
}

/** Lee el registro de procesos (`.karpo-local/pids.json`), para `karpo:stop`. */
export function readPids(file) {
  return existsSync(file) ? readPidFile(file) : [];
}

/**
 * Lanza un proceso y lo registra para poder pararlo, también si el padre falla.
 *
 * @param {string} command
 * @param {string[]} args
 * @param {{ name?: string, cwd?: string, env?: NodeJS.ProcessEnv, stdio?: any,
 *           pidFile?: string, shell?: boolean }} [options]
 * @returns {{ child: import('node:child_process').ChildProcess, pid: number | undefined,
 *             name: string, exited: Promise<number | null>, stop: () => boolean }}
 */
export function spawnTracked(command, args = [], options = {}) {
  const { name = command, cwd, env, stdio = 'inherit', pidFile, shell = false } = options;
  const child = spawn(command, args, {
    cwd,
    env: env ? { ...process.env, ...env } : process.env,
    stdio,
    shell,
    windowsHide: true,
    detached: process.platform !== 'win32',
  });
  const pid = child.pid;
  if (pid !== undefined) {
    tracked.set(pid, name);
    if (pidFile) {
      const entries = readPidFile(pidFile).filter((entry) => entry.pid !== pid);
      entries.push({ pid, name, command: [command, ...args].join(' ') });
      writePidFile(pidFile, entries);
    }
  }
  const exited = new Promise((resolve) => {
    child.once('exit', (code) => {
      if (pid !== undefined) {
        tracked.delete(pid);
        if (pidFile) writePidFile(pidFile, readPidFile(pidFile).filter((entry) => entry.pid !== pid));
      }
      resolve(code);
    });
    child.once('error', () => resolve(null));
  });
  return {
    child,
    pid,
    name,
    exited,
    stop: () => (pid === undefined ? false : killTree(pid)),
  };
}

/** Para todos los procesos lanzados con `spawnTracked` que sigan vivos. */
export function killAllTracked() {
  for (const pid of [...tracked.keys()]) {
    killTree(pid);
    tracked.delete(pid);
  }
}

let cleanupInstalled = false;

/** Garantiza `killAllTracked` al salir, con Ctrl+C o ante una excepción. */
export function installCleanup() {
  if (cleanupInstalled) return;
  cleanupInstalled = true;
  process.on('exit', killAllTracked);
  for (const signal of ['SIGINT', 'SIGTERM', 'SIGHUP']) {
    process.on(signal, () => {
      killAllTracked();
      process.exit(130);
    });
  }
  process.on('uncaughtException', (error) => {
    console.error(error);
    killAllTracked();
    process.exit(1);
  });
}

/**
 * Espera a que una URL responda (por defecto, con estado 2xx).
 *
 * @param {string} url
 * @param {{ timeoutMs?: number, intervalMs?: number, accept?: (res: Response) => boolean,
 *           signal?: AbortSignal, fetchImpl?: typeof fetch }} [options]
 */
export async function waitForUrl(url, options = {}) {
  const {
    timeoutMs = 30_000,
    intervalMs = 250,
    accept = (res) => res.ok,
    signal,
    fetchImpl = fetch,
  } = options;
  const deadline = Date.now() + timeoutMs;
  let last = 'sin respuesta';
  while (Date.now() < deadline) {
    if (signal?.aborted) throw new Error(`waitForUrl: cancelado (${url})`);
    try {
      const res = await fetchImpl(url, { signal: AbortSignal.timeout(Math.min(2_000, timeoutMs)) });
      if (accept(res)) return res;
      last = `HTTP ${res.status}`;
    } catch (error) {
      last = error instanceof Error ? error.message : String(error);
    }
    await new Promise((resolve) => setTimeout(resolve, intervalMs));
  }
  throw new Error(`waitForUrl: ${url} no respondió en ${timeoutMs} ms (${last})`);
}
